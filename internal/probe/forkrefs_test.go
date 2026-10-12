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

// refNode is one ref in the fake fork: compare status per SHA.
type refNode struct {
	name   string
	status map[string]string // sha -> IDENTICAL|BEHIND|AHEAD|DIVERGED|""(null)
}

type fakeGQL struct {
	mu    sync.Mutex
	calls int
	// handler returns (nodes, hasNext, endCursor, httpStatus, rawOverride)
	page func(repo, prefix, after string) (nodes []refNode, hasNext bool, cursor string)
	raw  func(repo, prefix string) (status int, body string, override bool)
}

func (f *fakeGQL) serve() (*ghapi.Client, func()) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" {
			w.WriteHeader(404)
			return
		}
		b, _ := io.ReadAll(r.Body)
		var req struct {
			Query     string
			Variables map[string]any
		}
		_ = json.Unmarshal(b, &req)
		prefix := prefixRe.FindStringSubmatch(req.Query)[1]
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
			if st, body, ok := f.raw(repo, prefix); ok {
				w.WriteHeader(st)
				w.Write([]byte(body))
				return
			}
		}
		nodes, hasNext, cursor := f.page(repo, prefix, after)
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
		resp := map[string]any{"data": map[string]any{"repository": map[string]any{"refs": map[string]any{
			"pageInfo": map[string]any{"hasNextPage": hasNext, "endCursor": cursor}, "nodes": out}}}}
		json.NewEncoder(w).Encode(resp)
	}))
	c := ghapi.New("test-token")
	c.BaseURL = srv.URL
	return c, srv.Close
}

func only(prefix, name string, status map[string]string) func(string, string, string) ([]refNode, bool, string) {
	return func(_, p, _ string) ([]refNode, bool, string) {
		if p == prefix {
			return []refNode{{name: name, status: status}}, false, ""
		}
		return nil, false, ""
	}
}

func TestForkRefHoldersStatuses(t *testing.T) {
	shaA, shaB, shaC, shaD := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40), strings.Repeat("d", 40)
	f := &fakeGQL{page: func(_, p, _ string) ([]refNode, bool, string) {
		if p == "refs/heads/" {
			return []refNode{
				{name: "main", status: map[string]string{shaA: "BEHIND", shaB: "AHEAD", shaC: "DIVERGED", shaD: "AHEAD"}},
				{name: "dev", status: map[string]string{shaA: "IDENTICAL", shaB: "DIVERGED", shaC: "AHEAD", shaD: "AHEAD"}},
			}, false, ""
		}
		return []refNode{{name: "v1", status: map[string]string{shaA: "BEHIND", shaB: "AHEAD", shaC: "AHEAD", shaD: "AHEAD"}}}, false, ""
	}}
	c, done := f.serve()
	defer done()
	got := forkRefHolders(context.Background(), c, "x/fork", []string{shaA, shaB, shaC, shaD})
	if v := got[shaA]; v.Status != model.Confirmed || v.Evidence != "refs/heads/main, refs/heads/dev, refs/tags/v1" {
		t.Errorf("A: %+v", v)
	}
	for _, s := range []string{shaB, shaC, shaD} {
		if got[s].Status != model.NotFound {
			t.Errorf("%s: AHEAD/DIVERGED must not count as holding: %+v", s[:1], got[s])
		}
	}
}

// ADR-0002 property: no failure shape may produce NotFound.
func TestForkRefHoldersFailuresAreUnknown(t *testing.T) {
	sha := strings.Repeat("a", 40)
	okPage := only("refs/heads/", "main", map[string]string{sha: "AHEAD"})
	cases := map[string]*fakeGQL{
		"http 500":      {page: okPage, raw: func(string, string) (int, string, bool) { return 500, "", true }},
		"http 502 html": {page: okPage, raw: func(string, string) (int, string, bool) { return 502, "<html>", true }},
		"graphql errors": {page: okPage, raw: func(string, string) (int, string, bool) {
			return 200, `{"errors":[{"message":"Something went wrong"}]}`, true
		}},
		"repository null": {page: okPage, raw: func(string, string) (int, string, bool) { return 200, `{"data":{"repository":null}}`, true }},
		"bad json":        {page: okPage, raw: func(string, string) (int, string, bool) { return 200, `{not json`, true }},
		"compare null":    {page: only("refs/heads/", "main", map[string]string{sha: ""})},
		"weird status":    {page: only("refs/heads/", "main", map[string]string{sha: "UNEXPECTED"})},
		"endless pages": {page: func(string, string, string) ([]refNode, bool, string) {
			return []refNode{{name: "b", status: map[string]string{sha: "AHEAD"}}}, true, "next"
		}},
		"tags query fails only": {page: okPage, raw: func(_, p string) (int, string, bool) {
			if p == "refs/tags/" {
				return 500, "", true
			}
			return 0, "", false
		}},
	}
	for name, f := range cases {
		c, done := f.serve()
		got := forkRefHolders(context.Background(), c, "x/fork", []string{sha})
		done()
		if got[sha].Status != model.Unknown {
			t.Errorf("%s: got %s (%s), want unknown", name, got[sha].Status, got[sha].Evidence)
		}
	}
	// An anonymous client cannot call GraphQL at all.
	anon := ghapi.New("")
	if got := forkRefHolders(context.Background(), anon, "x/fork", []string{sha}); got[sha].Status != model.Unknown {
		t.Errorf("anonymous: %+v", got[sha])
	}
	// A malformed SHA is never inlined into a query.
	f := &fakeGQL{page: okPage}
	c, done := f.serve()
	defer done()
	if got := forkRefHolders(context.Background(), c, "x/fork", []string{`abc"){x}`}); got[`abc"){x}`].Status != model.Unknown || f.calls != 0 {
		t.Errorf("bad sha must be unknown without a request: %+v calls=%d", got, f.calls)
	}
}

func TestForkRefHoldersProvenHolderSurvivesLaterFailure(t *testing.T) {
	sha := strings.Repeat("a", 40)
	f := &fakeGQL{
		page: only("refs/heads/", "main", map[string]string{sha: "BEHIND"}),
		raw: func(_, p string) (int, string, bool) {
			if p == "refs/tags/" {
				return 500, "", true
			}
			return 0, "", false
		},
	}
	c, done := f.serve()
	defer done()
	if v := forkRefHolders(context.Background(), c, "x/fork", []string{sha})[sha]; v.Status != model.Confirmed {
		t.Errorf("a ref already proven to hold the SHA must stand: %+v", v)
	}
}

func TestForkRefHoldersPaginates(t *testing.T) {
	sha := strings.Repeat("a", 40)
	f := &fakeGQL{page: func(_, p, after string) ([]refNode, bool, string) {
		if p != "refs/heads/" {
			return nil, false, ""
		}
		if after == "" {
			return []refNode{{name: "first", status: map[string]string{sha: "AHEAD"}}}, true, "cur1"
		}
		return []refNode{{name: "second", status: map[string]string{sha: "BEHIND"}}}, false, ""
	}}
	c, done := f.serve()
	defer done()
	if v := forkRefHolders(context.Background(), c, "x/fork", []string{sha})[sha]; v.Status != model.Confirmed || v.Evidence != "refs/heads/second" {
		t.Errorf("%+v", v)
	}
}

func TestForkRefHoldersChunksManySHAs(t *testing.T) {
	var shas []string
	for i := 0; i < 45; i++ {
		shas = append(shas, fmt.Sprintf("%040x", i+1))
	}
	status := map[string]string{}
	for _, s := range shas {
		status[s] = "AHEAD"
	}
	status[shas[44]] = "BEHIND"
	f := &fakeGQL{page: only("refs/heads/", "main", status)}
	c, done := f.serve()
	defer done()
	got := forkRefHolders(context.Background(), c, "x/fork", shas)
	if f.calls != 3*2 { // 45 SHAs = 3 chunks of 20, each queried for heads and tags
		t.Errorf("calls=%d", f.calls)
	}
	if got[shas[44]].Status != model.Confirmed || got[shas[0]].Status != model.NotFound {
		t.Errorf("last=%+v first=%+v", got[shas[44]], got[shas[0]])
	}
}

// End to end through the Forks probe.
func TestForksProbeWithRefs(t *testing.T) {
	obj := func(w http.ResponseWriter, r *http.Request, repo, sha string) bool {
		switch repo {
		case "a/gone": // cannot resolve the object
			w.WriteHeader(422)
			w.Write([]byte(`{"message":"No commit found for SHA"}`))
		case "d/flaky": // object check itself fails
			w.WriteHeader(502)
		default:
			w.WriteHeader(200)
		}
		return true
	}
	gql := &fakeGQL{page: func(repo, p, _ string) ([]refNode, bool, string) {
		sha := strings.Repeat("a", 40)
		if repo == "b/holder" && p == "refs/heads/" {
			return []refNode{{name: "main", status: map[string]string{sha: "BEHIND"}}}, false, ""
		}
		return []refNode{{name: "main", status: map[string]string{sha: "AHEAD"}}}, false, ""
	}, raw: func(repo, p string) (int, string, bool) {
		if repo == "c/broken" {
			return 500, "", true
		}
		return 0, "", false
	}}
	gc, gdone := gql.serve()
	defer gdone()
	rest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case p == "/repos/o/r":
			fmt.Fprint(w, `{"forks_count":5}`)
		case p == "/repos/o/r/forks":
			fmt.Fprint(w, `[{"full_name":"a/gone","pushed_at":"2026-05-01T00:00:00Z"},{"full_name":"b/holder","pushed_at":"2026-04-01T00:00:00Z"},{"full_name":"c/broken","pushed_at":"2026-03-01T00:00:00Z"},{"full_name":"d/flaky","pushed_at":"2026-02-01T00:00:00Z"},{"full_name":"e/clean","pushed_at":"2026-01-01T00:00:00Z"}]`)
		case strings.Contains(p, "/commits/"):
			parts := strings.Split(p, "/")
			obj(w, r, parts[2]+"/"+parts[3], parts[len(parts)-1])
		default:
			w.WriteHeader(404)
		}
	}))
	defer rest.Close()
	client := ghapi.New("test-token")
	client.BaseURL = rest.URL
	// GraphQL goes to its own fake: wire it by pointing GraphQL at the same base via a mux.
	mux := http.NewServeMux()
	mux.Handle("/graphql", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := http.NewRequest("POST", gc.BaseURL+"/graphql", r.Body)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			w.WriteHeader(502)
			return
		}
		defer resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}))
	mux.Handle("/", rest.Config.Handler)
	both := httptest.NewServer(mux)
	defer both.Close()
	client.BaseURL = both.URL

	sha := strings.Repeat("a", 40)
	res := Forks{Client: client, Refs: true}.Run(context.Background(), "o/r", []model.OldCommit{{SHA: sha}})

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
	var refCov *model.SurfaceCoverage
	for i := range res.Surfaces {
		if res.Surfaces[i].Surface == model.SurfaceForkRef {
			refCov = &res.Surfaces[i]
		}
	}
	if refCov == nil || refCov.Complete {
		t.Errorf("unknown results must leave fork_ref incomplete: %+v", refCov)
	}
}

func TestForksWithoutRefsEmitsNoForkRefSurface(t *testing.T) {
	c, done := apiClient(forksServer(3, "nobody"))
	defer done()
	res := Forks{Client: c}.Run(context.Background(), "o/r", []model.OldCommit{{SHA: sha}})
	for _, s := range res.Surfaces {
		if s.Surface == model.SurfaceForkRef {
			t.Fatal("fork_ref coverage must only exist when the check was requested")
		}
	}
}

func TestForkRefsResumeSkipsKnownPairs(t *testing.T) {
	f := &fakeGQL{page: only("refs/heads/", "main", map[string]string{sha: "BEHIND"})}
	gc, gdone := f.serve()
	defer gdone()
	_ = gc
	k := NewKnown([]model.Finding{
		{OldSHA: sha, Surface: model.SurfaceForkObject, Location: "b/hot", Status: model.Confirmed},
		{OldSHA: sha, Surface: model.SurfaceForkRef, Location: "b/hot", Status: model.Confirmed},
	}, []model.OldCommit{{SHA: sha}})
	c, done := apiClient(forksServer(1, "b/hot"))
	defer done()
	c.Token = "test-token"
	res := Forks{Client: c, Refs: true, Known: k}.Run(context.Background(), "o/r", []model.OldCommit{{SHA: sha}})
	for _, fd := range res.Findings {
		if fd.Surface == model.SurfaceForkRef && fd.Location == "b/hot" {
			t.Errorf("known fork_ref pair was re-checked: %+v", fd)
		}
	}
}
