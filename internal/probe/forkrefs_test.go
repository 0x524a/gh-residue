package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/0x524a/gh-residue/internal/ghapi"
	"github.com/0x524a/gh-residue/internal/model"
)

var (
	prefixRe = regexp.MustCompile(`refPrefix:"([^"]+)"`)
	headRe   = regexp.MustCompile(`headRef:"([0-9a-f]{40})"`)
)

// refNode is one ref in the fake fork. name is relative to "refs/", as GitHub
// returns it for refPrefix "refs/": "heads/main", "tags/v1", "notes/amlog".
type refNode struct {
	name   string
	status map[string]string // sha -> IDENTICAL|BEHIND|AHEAD|DIVERGED|""(null)
}

type fakeGQL struct {
	mu    sync.Mutex
	calls int
	prs   func(repo string) int // pull requests in the fork
	page  func(repo, after string) (nodes []refNode, hasNext bool, cursor string)
	raw   func(repo string) (status int, body string, override bool)
	rest  http.HandlerFunc // REST routes (matching-refs, compare), if the test needs them
}

func (f *fakeGQL) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var req struct {
			Query     string
			Variables map[string]any
		}
		_ = json.Unmarshal(b, &req)
		if m := prefixRe.FindStringSubmatch(req.Query); m == nil || m[1] != "refs/" {
			w.WriteHeader(400) // the check must ask for every non-pull namespace, not just heads/tags
			return
		}
		var shas []string
		for _, m := range headRe.FindAllStringSubmatch(req.Query, -1) {
			shas = append(shas, m[1])
		}
		repo := fmt.Sprint(req.Variables["o"]) + "/" + fmt.Sprint(req.Variables["n"])
		after, _ := req.Variables["after"].(string)
		f.mu.Lock()
		f.calls++
		f.mu.Unlock()
		if f.raw != nil {
			if st, body, ok := f.raw(repo); ok {
				w.WriteHeader(st)
				w.Write([]byte(body))
				return
			}
		}
		nodes, hasNext, cursor := f.page(repo, after)
		var out []map[string]any
		for _, n := range nodes {
			m := map[string]any{"name": n.name}
			for i, s := range shas {
				if st := n.status[s]; st != "" {
					m[fmt.Sprintf("c%d", i)] = map[string]string{"status": st}
				} else {
					m[fmt.Sprintf("c%d", i)] = nil
				}
			}
			out = append(out, m)
		}
		prs := 0
		if f.prs != nil {
			prs = f.prs(repo)
		}
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"repository": map[string]any{
			"pullRequests": map[string]any{"totalCount": prs},
			"refs":         map[string]any{"pageInfo": map[string]any{"hasNextPage": hasNext, "endCursor": cursor}, "nodes": out},
		}}})
	})
}

func (f *fakeGQL) serve() (*ghapi.Client, func()) {
	mux := http.NewServeMux()
	mux.Handle("/graphql", f.handler())
	if f.rest != nil {
		mux.Handle("/repos/", f.rest)
	}
	srv := httptest.NewServer(mux)
	c := ghapi.New("test-token")
	c.BaseURL = srv.URL
	return c, srv.Close
}

func one(name string, status map[string]string) func(string, string) ([]refNode, bool, string) {
	return func(string, string) ([]refNode, bool, string) {
		return []refNode{{name: name, status: status}}, false, ""
	}
}

func TestForkRefHoldersStatuses(t *testing.T) {
	shaA, shaB, shaC, shaD := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40), strings.Repeat("d", 40)
	f := &fakeGQL{page: func(string, string) ([]refNode, bool, string) {
		return []refNode{
			{name: "heads/main", status: map[string]string{shaA: "BEHIND", shaB: "AHEAD", shaC: "DIVERGED", shaD: "AHEAD"}},
			{name: "heads/dev", status: map[string]string{shaA: "IDENTICAL", shaB: "DIVERGED", shaC: "AHEAD", shaD: "AHEAD"}},
			{name: "tags/v1", status: map[string]string{shaA: "BEHIND", shaB: "AHEAD", shaC: "AHEAD", shaD: "AHEAD"}},
		}, false, ""
	}}
	c, done := f.serve()
	defer done()
	got := forkRefHolders(context.Background(), c, "o/r", "x/fork", []string{shaA, shaB, shaC, shaD})
	if v := got[shaA]; v.Status != model.Confirmed || v.Evidence != "refs/heads/main, refs/heads/dev, refs/tags/v1" {
		t.Errorf("A: %+v", v)
	}
	for _, s := range []string{shaB, shaC, shaD} {
		if got[s].Status != model.NotFound {
			t.Errorf("%s: AHEAD/DIVERGED must not count as holding: %+v", s[:1], got[s])
		}
	}
}

// A ref outside heads/ and tags/ (refs/notes, refs/remotes, refs/original...) must be seen.
func TestForkRefHoldersSeesOtherNamespaces(t *testing.T) {
	sha := strings.Repeat("a", 40)
	for _, ref := range []string{"notes/amlog", "remotes/origin/main", "original/refs/heads/main"} {
		f := &fakeGQL{page: func(string, string) ([]refNode, bool, string) {
			return []refNode{
				{name: "heads/main", status: map[string]string{sha: "AHEAD"}},
				{name: ref, status: map[string]string{sha: "BEHIND"}},
			}, false, ""
		}}
		c, done := f.serve()
		v := forkRefHolders(context.Background(), c, "o/r", "x/fork", []string{sha})[sha]
		done()
		if v.Status != model.Confirmed || v.Evidence != "refs/"+ref {
			t.Errorf("%s: %+v", ref, v)
		}
	}
}

// A fork's own refs/pull/* are invisible to GraphQL, so they are listed over REST
// and compared through the user's own repository.
func TestForkRefHoldersChecksForkPullRefs(t *testing.T) {
	sha := strings.Repeat("a", 40)
	tipA, tipB := strings.Repeat("b", 40), strings.Repeat("c", 40)
	pullList := fmt.Sprintf(`[{"ref":"refs/pull/1/head","object":{"sha":"%s","type":"commit"}},`+
		`{"ref":"refs/pull/2/head","object":{"sha":"%s","type":"commit"}}]`, tipA, tipB)
	serve := func(status map[string]string, prs int) (*ghapi.Client, func()) {
		f := &fakeGQL{prs: func(string) int { return prs }, page: one("heads/main", map[string]string{sha: "AHEAD"}),
			rest: func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/git/matching-refs/pull/"):
					fmt.Fprint(w, pullList)
				case strings.Contains(r.URL.Path, "/compare/"):
					if !strings.Contains(r.URL.RawQuery, "page=100000") || !strings.HasPrefix(r.URL.Path, "/repos/o/r/") {
						w.WriteHeader(400)
						return
					}
					for tip, st := range status {
						if strings.HasSuffix(r.URL.Path, "..."+tip) {
							if st == "404" {
								w.WriteHeader(404)
								return
							}
							fmt.Fprintf(w, `{"status":%q}`, st)
							return
						}
					}
					w.WriteHeader(500)
				}
			}}
		return f.serve()
	}
	run := func(c *ghapi.Client) refVerdict {
		return forkRefHolders(context.Background(), c, "o/r", "x/fork", []string{sha})[sha]
	}

	c, done := serve(map[string]string{tipA: "behind", tipB: "diverged"}, 2)
	if v := run(c); v.Status != model.NotFound {
		t.Errorf("no pull ref contains it: %+v", v)
	}
	done()
	c, done = serve(map[string]string{tipA: "diverged", tipB: "ahead"}, 2)
	if v := run(c); v.Status != model.Confirmed || v.Evidence != "refs/pull/2/head" {
		t.Errorf("a pull ref that is ahead of the old commit contains it: %+v", v)
	}
	done()
	c, done = serve(map[string]string{tipA: "identical", tipB: "404"}, 2)
	if v := run(c); v.Status != model.Confirmed {
		t.Errorf("proven holder must stand despite another tip failing: %+v", v)
	}
	done()
	c, done = serve(map[string]string{tipA: "behind", tipB: "404"}, 2)
	if v := run(c); v.Status != model.Unknown {
		t.Errorf("a failed compare must be unknown: %+v", v)
	}
	done()
	c, done = serve(map[string]string{tipA: "behind", tipB: "weird"}, 2)
	if v := run(c); v.Status != model.Unknown {
		t.Errorf("an unexpected status must be unknown: %+v", v)
	}
	done()
	// More pull requests than listed head refs: the listing is incomplete.
	c, done = serve(map[string]string{tipA: "behind", tipB: "behind"}, 5)
	if v := run(c); v.Status != model.Unknown || !strings.Contains(v.Evidence, "5 pull requests") {
		t.Errorf("incomplete pull ref listing must be unknown: %+v", v)
	}
	done()
	// A holder already proven over GraphQL stands without any REST call.
	f2 := &fakeGQL{prs: func(string) int { return 3 }, page: one("heads/main", map[string]string{sha: "BEHIND"})}
	c2, done2 := f2.serve()
	defer done2()
	if v := run(c2); v.Status != model.Confirmed {
		t.Errorf("proven holder must stand: %+v", v)
	}
	// No pull requests: no extra requests, clean means clean.
	f3 := &fakeGQL{page: one("heads/main", map[string]string{sha: "AHEAD"})}
	c3, done3 := f3.serve()
	defer done3()
	if v := run(c3); v.Status != model.NotFound {
		t.Errorf("%+v", v)
	}
}

// Without REST routes the pull refs cannot be read, which must read as unknown.
func TestForkRefHoldersPullRefsUnavailableIsUnknown(t *testing.T) {
	sha := strings.Repeat("a", 40)
	f := &fakeGQL{prs: func(string) int { return 3 }, page: one("heads/main", map[string]string{sha: "AHEAD"})}
	c, done := f.serve()
	defer done()
	if v := forkRefHolders(context.Background(), c, "o/r", "x/fork", []string{sha})[sha]; v.Status != model.Unknown {
		t.Errorf("%+v", v)
	}
}

// ADR-0002 property: no failure shape may produce NotFound.
func TestForkRefHoldersFailuresAreUnknown(t *testing.T) {
	sha := strings.Repeat("a", 40)
	okPage := one("heads/main", map[string]string{sha: "AHEAD"})
	cases := map[string]*fakeGQL{
		"http 500":        {page: okPage, raw: func(string) (int, string, bool) { return 500, "", true }},
		"http 502 html":   {page: okPage, raw: func(string) (int, string, bool) { return 502, "<html>", true }},
		"graphql errors":  {page: okPage, raw: func(string) (int, string, bool) { return 200, `{"errors":[{"message":"Something went wrong"}]}`, true }},
		"repository null": {page: okPage, raw: func(string) (int, string, bool) { return 200, `{"data":{"repository":null}}`, true }},
		"bad json":        {page: okPage, raw: func(string) (int, string, bool) { return 200, `{not json`, true }},
		"compare null":    {page: one("heads/main", map[string]string{sha: ""})},
		"weird status":    {page: one("heads/main", map[string]string{sha: "UNEXPECTED"})},
		"endless pages": {page: func(string, string) ([]refNode, bool, string) {
			return []refNode{{name: "heads/b", status: map[string]string{sha: "AHEAD"}}}, true, "next"
		}},
	}
	for name, f := range cases {
		c, done := f.serve()
		got := forkRefHolders(context.Background(), c, "o/r", "x/fork", []string{sha})
		done()
		if got[sha].Status != model.Unknown {
			t.Errorf("%s: got %s (%s), want unknown", name, got[sha].Status, got[sha].Evidence)
		}
	}
	anon := ghapi.New("") // an anonymous client cannot call GraphQL at all
	if got := forkRefHolders(context.Background(), anon, "o/r", "x/fork", []string{sha}); got[sha].Status != model.Unknown {
		t.Errorf("anonymous: %+v", got[sha])
	}
	f := &fakeGQL{page: okPage} // a malformed SHA is never inlined into a query
	c, done := f.serve()
	defer done()
	bad := `abc"){x}`
	if got := forkRefHolders(context.Background(), c, "o/r", "x/fork", []string{bad}); got[bad].Status != model.Unknown || f.calls != 0 {
		t.Errorf("bad sha must be unknown without a request: %+v calls=%d", got, f.calls)
	}
}

func TestForkRefHoldersProvenHolderSurvivesLaterFailure(t *testing.T) {
	sha := strings.Repeat("a", 40)
	calls := 0
	f := &fakeGQL{
		page: func(string, string) ([]refNode, bool, string) {
			return []refNode{{name: "heads/main", status: map[string]string{sha: "BEHIND"}}}, true, "c1"
		},
		raw: func(string) (int, string, bool) {
			calls++
			if calls > 1 { // second page fails
				return 500, "", true
			}
			return 0, "", false
		},
	}
	c, done := f.serve()
	defer done()
	if v := forkRefHolders(context.Background(), c, "o/r", "x/fork", []string{sha})[sha]; v.Status != model.Confirmed {
		t.Errorf("a ref already proven to hold the SHA must stand: %+v", v)
	}
}

func TestForkRefHoldersPaginates(t *testing.T) {
	sha := strings.Repeat("a", 40)
	f := &fakeGQL{page: func(_, after string) ([]refNode, bool, string) {
		if after == "" {
			return []refNode{{name: "heads/first", status: map[string]string{sha: "AHEAD"}}}, true, "cur1"
		}
		return []refNode{{name: "heads/second", status: map[string]string{sha: "BEHIND"}}}, false, ""
	}}
	c, done := f.serve()
	defer done()
	if v := forkRefHolders(context.Background(), c, "o/r", "x/fork", []string{sha})[sha]; v.Status != model.Confirmed || v.Evidence != "refs/heads/second" {
		t.Errorf("%+v", v)
	}
}

func TestForkRefHoldersChunksManySHAs(t *testing.T) {
	var shas []string
	status := map[string]string{}
	for i := 0; i < 45; i++ {
		s := fmt.Sprintf("%040x", i+1)
		shas = append(shas, s)
		status[s] = "AHEAD"
	}
	status[shas[44]] = "BEHIND"
	f := &fakeGQL{page: one("heads/main", status)}
	c, done := f.serve()
	defer done()
	got := forkRefHolders(context.Background(), c, "o/r", "x/fork", shas)
	if f.calls != 3 { // 45 SHAs = 3 chunks of 20, one query each
		t.Errorf("calls=%d", f.calls)
	}
	if got[shas[44]].Status != model.Confirmed || got[shas[0]].Status != model.NotFound {
		t.Errorf("last=%+v first=%+v", got[shas[44]], got[shas[0]])
	}
}

// restAndGraphQL serves the REST fork endpoints and a fake GraphQL on one server.
func restAndGraphQL(t *testing.T, forksJSON string, forksCount int, objectStatus func(repo string) int, g *fakeGQL) *ghapi.Client {
	t.Helper()
	mux := http.NewServeMux()
	if g != nil {
		mux.Handle("/graphql", g.handler())
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case p == "/repos/o/r":
			fmt.Fprintf(w, `{"forks_count":%d}`, forksCount)
		case p == "/repos/o/r/forks":
			fmt.Fprint(w, forksJSON)
		case strings.Contains(p, "/commits/"):
			parts := strings.Split(p, "/")
			st := objectStatus(parts[2] + "/" + parts[3])
			w.WriteHeader(st)
			if st == 422 {
				w.Write([]byte(`{"message":"No commit found for SHA"}`))
			}
		default:
			w.WriteHeader(404)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := ghapi.New("test-token")
	c.BaseURL = srv.URL
	return c
}

func surfaceDone(res Result, s model.Surface) (complete, present bool) {
	for _, c := range res.Surfaces {
		if c.Surface == s {
			return c.Complete, true
		}
	}
	return false, false
}

// End to end through the Forks probe.
func TestForksProbeWithRefs(t *testing.T) {
	sha := strings.Repeat("a", 40)
	g := &fakeGQL{
		page: func(repo, _ string) ([]refNode, bool, string) {
			if repo == "b/holder" {
				return []refNode{{name: "heads/main", status: map[string]string{sha: "BEHIND"}}}, false, ""
			}
			return []refNode{{name: "heads/main", status: map[string]string{sha: "AHEAD"}}}, false, ""
		},
		raw: func(repo string) (int, string, bool) {
			if repo == "c/broken" {
				return 500, "", true
			}
			return 0, "", false
		},
	}
	c := restAndGraphQL(t,
		`[{"full_name":"a/gone","pushed_at":"2026-05-01T00:00:00Z"},{"full_name":"b/holder","pushed_at":"2026-04-01T00:00:00Z"},{"full_name":"c/broken","pushed_at":"2026-03-01T00:00:00Z"},{"full_name":"d/flaky","pushed_at":"2026-02-01T00:00:00Z"},{"full_name":"e/clean","pushed_at":"2026-01-01T00:00:00Z"}]`,
		5,
		func(repo string) int {
			switch repo {
			case "a/gone":
				return 422 // cannot resolve the object
			case "d/flaky":
				return 502 // object check itself fails
			}
			return 200
		}, g)
	res := Forks{Client: c, Refs: true}.Run(context.Background(), "o/r", []model.OldCommit{{SHA: sha}})

	got := map[string]model.Status{}
	for _, f := range res.Findings {
		if f.Surface == model.SurfaceForkRef {
			got[f.Location] = f.Status
		}
	}
	want := map[string]model.Status{
		"a/gone":   model.NotFound,  // object unresolvable => no ref can hold it
		"b/holder": model.Confirmed, // compare says BEHIND
		"c/broken": model.Unknown,   // GraphQL failed
		"d/flaky":  model.Unknown,   // object check failed, so no ref verdict
		"e/clean":  model.NotFound,  // resolvable, but every ref AHEAD
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %q want %q", k, got[k], v)
		}
	}
	if done, present := surfaceDone(res, model.SurfaceForkRef); !present || done {
		t.Errorf("unknown results must leave fork_ref incomplete: done=%v present=%v", done, present)
	}
}

func TestForksWithoutRefsEmitsNoForkRefSurface(t *testing.T) {
	c, done := apiClient(forksServer(3, "nobody"))
	defer done()
	res := Forks{Client: c}.Run(context.Background(), "o/r", []model.OldCommit{{SHA: sha}})
	if _, present := surfaceDone(res, model.SurfaceForkRef); present {
		t.Fatal("fork_ref coverage must only exist when the check was requested")
	}
}

func TestForkRefsResumeSkipsKnownPairs(t *testing.T) {
	g := &fakeGQL{page: one("heads/main", map[string]string{sha: "BEHIND"})}
	k := NewKnown([]model.Finding{
		{OldSHA: sha, Surface: model.SurfaceForkObject, Location: "b/hot", Status: model.Confirmed},
		{OldSHA: sha, Surface: model.SurfaceForkRef, Location: "b/hot", Status: model.Confirmed},
	}, []model.OldCommit{{SHA: sha}})
	c := restAndGraphQL(t, `[{"full_name":"b/hot","pushed_at":"2026-01-01T00:00:00Z"}]`, 1, func(string) int { return 200 }, g)
	res := Forks{Client: c, Refs: true, Known: k}.Run(context.Background(), "o/r", []model.OldCommit{{SHA: sha}})
	if g.calls != 0 {
		t.Errorf("known fork_ref pair triggered %d GraphQL calls", g.calls)
	}
	for _, fd := range res.Findings {
		if fd.Surface == model.SurfaceForkRef && fd.Location == "b/hot" {
			t.Errorf("known fork_ref pair was re-checked: %+v", fd)
		}
	}
}

// --- fork coverage: these are the "Ready for Support? yes on incomplete data" guards ---

// forks_count undercounts in the wild (1014 reported, 1056 real). Completeness must
// come from the listing, not the counter.
func TestForksCoverageIgnoresUndercountedForksCount(t *testing.T) {
	list := `[{"full_name":"a/f1"},{"full_name":"b/f2"},{"full_name":"c/f3"}]`
	c := restAndGraphQL(t, list, 2 /* counter says 2, three exist */, func(string) int { return 422 }, nil)
	res := Forks{Client: c, Cap: 2}.Run(context.Background(), "o/r", []model.OldCommit{{SHA: sha}})
	if res.ForksTotal != 3 || res.ForksChecked != 2 {
		t.Fatalf("checked %d of %d", res.ForksChecked, res.ForksTotal)
	}
	if done, _ := surfaceDone(res, model.SurfaceForkObject); done {
		t.Error("a capped sample of 2 of 3 real forks must not be complete, whatever forks_count says")
	}
	// Full coverage with an undercounting counter is complete.
	res = Forks{Client: c, Cap: 10}.Run(context.Background(), "o/r", []model.OldCommit{{SHA: sha}})
	if done, _ := surfaceDone(res, model.SurfaceForkObject); !done || res.ForksIncompleteReason != "" {
		t.Errorf("all listed forks checked: done=%v reason=%q", done, res.ForksIncompleteReason)
	}
}

func TestForksOfForksMakeCoverageIncomplete(t *testing.T) {
	list := `[{"full_name":"a/f1","forks_count":0},{"full_name":"b/f2","forks_count":3}]`
	c := restAndGraphQL(t, list, 2, func(string) int { return 422 }, nil)
	res := Forks{Client: c}.Run(context.Background(), "o/r", []model.OldCommit{{SHA: sha}})
	if !strings.Contains(res.ForksIncompleteReason, "1 listed forks have forks of their own") {
		t.Fatalf("reason=%q", res.ForksIncompleteReason)
	}
	if done, _ := surfaceDone(res, model.SurfaceForkObject); done {
		t.Error("fork_object must be incomplete when a listed fork has forks")
	}
}

func TestForkListingTruncationIsIncompleteNotSilent(t *testing.T) {
	// Every page is full (100 forks), so the listing never ends within MaxListPages.
	var b strings.Builder
	b.WriteString("[")
	for i := 0; i < 100; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"full_name":"u%d/r"}`, i)
	}
	b.WriteString("]")
	// The same 100 names on every page: duplicates must be dropped, and the listing still truncated.
	c := restAndGraphQL(t, b.String(), 100, func(string) int { return 422 }, nil)
	res := Forks{Client: c, MaxListPages: 2, Cap: 1000}.Run(context.Background(), "o/r", []model.OldCommit{{SHA: sha}})
	if res.ForksTotal != 100 {
		t.Errorf("duplicate names must not inflate the count: total=%d", res.ForksTotal)
	}
	if !strings.Contains(res.ForksIncompleteReason, "only the first 200 forks") {
		t.Errorf("truncated listing must say so: %q", res.ForksIncompleteReason)
	}
}
