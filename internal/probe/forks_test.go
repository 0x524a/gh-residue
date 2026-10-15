package probe

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/0x524a/gh-residue/internal/model"
)

func forksServer(total int, holder string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case p == "/repos/o/r":
			fmt.Fprintf(w, `{"forks_count":%d}`, total)
		case p == "/repos/o/r/forks":
			fmt.Fprint(w, `[{"full_name":"a/old","pushed_at":"2020-01-01T00:00:00Z","stargazers_count":9},`+
				`{"full_name":"b/hot","pushed_at":"2026-01-01T00:00:00Z","stargazers_count":1},`+
				`{"full_name":"c/mid","pushed_at":"2024-01-01T00:00:00Z","stargazers_count":5}]`)
		case strings.HasPrefix(p, "/repos/") && strings.Contains(p, "/commits/"):
			if strings.HasPrefix(p, "/repos/"+holder+"/") {
				w.WriteHeader(200)
				return
			}
			w.WriteHeader(422)
			w.Write([]byte(`{"message":"No commit found for SHA"}`))
		default:
			w.WriteHeader(404)
		}
	}
}

func TestForksSampledPrioritisedAndDisclosed(t *testing.T) {
	c, done := apiClient(forksServer(10, "b/hot"))
	defer done()
	res := Forks{Client: c, Cap: 2}.Run(context.Background(), "o/r", []model.OldCommit{{SHA: sha}})
	if res.ForksTotal != 10 || res.ForksChecked != 2 {
		t.Fatalf("coverage %d/%d", res.ForksChecked, res.ForksTotal)
	}
	got := map[string]model.Status{}
	for _, f := range res.Findings {
		got[f.Location] = f.Status
	}
	if len(got) != 2 || got["b/hot"] != model.Confirmed || got["c/mid"] != model.NotFound {
		t.Fatalf("most recently pushed forks first: %+v", got)
	}
	if res.Surfaces[0].Complete {
		t.Fatal("sampled coverage must not be complete")
	}
}

func TestForksFullCoverageComplete(t *testing.T) {
	c, done := apiClient(forksServer(3, "nobody"))
	defer done()
	res := Forks{Client: c}.Run(context.Background(), "o/r", []model.OldCommit{{SHA: sha}})
	if !res.Surfaces[0].Complete || res.ForksChecked != 3 {
		t.Fatalf("%+v", res)
	}
}

func TestForksListFailureIsUnknown(t *testing.T) {
	c, done := apiClient(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/o/r" {
			fmt.Fprint(w, `{"forks_count":5}`)
			return
		}
		w.WriteHeader(500)
	})
	defer done()
	res := Forks{Client: c}.Run(context.Background(), "o/r", []model.OldCommit{{SHA: sha}})
	if !res.Failed || res.Findings[0].Status != model.Unknown || res.Surfaces[0].Complete {
		t.Fatalf("%+v", res)
	}
}

func TestForksAnchorsLimitPerForkRequests(t *testing.T) {
	anchor := strings.Repeat("a", 40)
	var others []model.OldCommit
	for i := 0; i < 50; i++ {
		others = append(others, model.OldCommit{SHA: fmt.Sprintf("%040x", i+1)})
	}
	commits := append([]model.OldCommit{{SHA: anchor}}, others...)
	hits := 0
	h := forksServer(3, "nobody")
	c, done := apiClient(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/commits/") {
			hits++
		}
		h(w, r)
	})
	defer done()
	res := Forks{Client: c, Anchors: []string{anchor}}.Run(context.Background(), "o/r", commits)
	if hits != 3 {
		t.Fatalf("want one request per fork for the anchor, got %d", hits)
	}
	if res.ForkCommitsSkipped != 50 {
		t.Fatalf("skipped %d", res.ForkCommitsSkipped)
	}
	for _, f := range res.Findings {
		if f.OldSHA != anchor {
			t.Fatalf("non-anchor finding %+v", f)
		}
	}
}

func TestForksAnchorNotInCommitsFallsBackToAll(t *testing.T) {
	c, done := apiClient(forksServer(3, "nobody"))
	defer done()
	res := Forks{Client: c, Anchors: []string{strings.Repeat("b", 40)}}.Run(context.Background(), "o/r",
		[]model.OldCommit{{SHA: sha}})
	if res.ForkCommitsSkipped != 0 || len(res.Findings) != 3 {
		t.Fatalf("%d skipped, %d findings", res.ForkCommitsSkipped, len(res.Findings))
	}
}

// A repository that is itself a fork has siblings and a parent that share its Git
// data but are never listed, so an empty fork list must not read as full coverage.
func TestForkOfAForkIsIncomplete(t *testing.T) {
	c, done := apiClient(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r":
			fmt.Fprint(w, `{"forks_count":0,"fork":true,"parent":{"full_name":"up/r"},"source":{"full_name":"root/r"}}`)
		case "/repos/o/r/forks":
			fmt.Fprint(w, `[]`)
		default:
			w.WriteHeader(404)
		}
	})
	defer done()
	res := Forks{Client: c}.Run(context.Background(), "o/r", []model.OldCommit{{SHA: sha}})
	if !strings.Contains(res.ForksIncompleteReason, "itself a fork of root/r") {
		t.Fatalf("reason %q", res.ForksIncompleteReason)
	}
	for _, s := range res.Surfaces {
		if s.Complete {
			t.Fatalf("fork surfaces must not be complete: %+v", res.Surfaces)
		}
	}
}

// Listed forks whose repository and owner both 404 are hidden by GitHub: counted
// and explained, but never clean.
func TestHiddenForksAreCountedAndStayUnknown(t *testing.T) {
	c, done := apiClient(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case p == "/repos/o/r":
			fmt.Fprint(w, `{"forks_count":3}`)
		case p == "/repos/o/r/forks":
			fmt.Fprint(w, `[{"full_name":"ok/r"},{"full_name":"gone/r"},{"full_name":"noaccess/r"}]`)
		case p == "/users/gone":
			w.WriteHeader(404)
		case p == "/users/noaccess":
			fmt.Fprint(w, `{"login":"noaccess"}`)
		case strings.HasPrefix(p, "/repos/ok/"):
			w.WriteHeader(422)
			w.Write([]byte(`{"message":"No commit found for SHA"}`))
		default:
			w.WriteHeader(404)
		}
	})
	defer done()
	res := Forks{Client: c}.Run(context.Background(), "o/r", []model.OldCommit{{SHA: sha}})
	if res.ForksHidden != 1 {
		t.Fatalf("hidden %d, want 1 (the owner that still exists is only an access problem)", res.ForksHidden)
	}
	for _, f := range res.Findings {
		if f.Status == model.NotFound && f.Location != "ok/r" {
			t.Fatalf("an unreachable fork must never be not_found: %+v", f)
		}
		if f.Location == "gone/r" && (f.Status != model.Unknown || !strings.Contains(f.Evidence, "hidden")) {
			t.Fatalf("%+v", f)
		}
	}
	if res.Surfaces[0].Complete {
		t.Fatal("hidden forks keep the surface incomplete")
	}
}

// With no fork answering normally, a 404 may be a broken token, not hiding.
func TestNothingIsHiddenWhenNoForkAnswers(t *testing.T) {
	c, done := apiClient(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r":
			fmt.Fprint(w, `{"forks_count":1}`)
		case "/repos/o/r/forks":
			fmt.Fprint(w, `[{"full_name":"gone/r"}]`)
		default:
			w.WriteHeader(404)
		}
	})
	defer done()
	if res := (Forks{Client: c}).Run(context.Background(), "o/r", []model.OldCommit{{SHA: sha}}); res.ForksHidden != 0 {
		t.Fatalf("hidden %d", res.ForksHidden)
	}
}

// Forks of forks are listed and checked, and only those that cannot be listed are
// a coverage gap.
func TestNestedForksAreEnumeratedAndChecked(t *testing.T) {
	c, done := apiClient(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case p == "/repos/o/r":
			fmt.Fprint(w, `{"forks_count":2}`)
		case p == "/repos/o/r/forks":
			fmt.Fprint(w, `[{"full_name":"a/f","forks_count":1},{"full_name":"b/g","forks_count":0}]`)
		case p == "/repos/a/f/forks":
			fmt.Fprint(w, `[{"full_name":"c/h","forks_count":0}]`)
		case strings.Contains(p, "/commits/"):
			w.WriteHeader(422)
			w.Write([]byte(`{"message":"No commit found for SHA"}`))
		default:
			w.WriteHeader(404)
		}
	})
	defer done()
	res := Forks{Client: c}.Run(context.Background(), "o/r", []model.OldCommit{{SHA: sha}})
	if res.ForksIncompleteReason != "" {
		t.Fatalf("every fork was listed, reason %q", res.ForksIncompleteReason)
	}
	got := map[string]bool{}
	for _, f := range res.Findings {
		got[f.Location] = true
	}
	if !got["c/h"] || res.ForksTotal != 3 || res.ForksChecked != 3 {
		t.Fatalf("nested fork not checked: %v total=%d", got, res.ForksTotal)
	}
	if !res.Surfaces[0].Complete {
		t.Fatal("complete when all nested forks were listed and clean")
	}
}

func TestNestedForksPastTheDepthLimitAreAGap(t *testing.T) {
	c, done := apiClient(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case p == "/repos/o/r":
			fmt.Fprint(w, `{"forks_count":1}`)
		case strings.HasSuffix(p, "/forks"): // every fork has one more fork below it
			parts := strings.Split(p, "/")
			fmt.Fprintf(w, `[{"full_name":"%sx/r","forks_count":1}]`, parts[2])
		case strings.Contains(p, "/commits/"):
			w.WriteHeader(422)
			w.Write([]byte(`{"message":"No commit found for SHA"}`))
		default:
			w.WriteHeader(404)
		}
	})
	defer done()
	res := Forks{Client: c}.Run(context.Background(), "o/r", []model.OldCommit{{SHA: sha}})
	if !strings.Contains(res.ForksIncompleteReason, "could not be listed") || res.ForksTotal != maxForkDepth {
		t.Fatalf("total=%d reason=%q", res.ForksTotal, res.ForksIncompleteReason)
	}
}

func TestAcceptHiddenOnlyAffectsCompletenessOfTheForkSurfaces(t *testing.T) {
	h := func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case p == "/repos/o/r":
			fmt.Fprint(w, `{"forks_count":2}`)
		case p == "/repos/o/r/forks":
			fmt.Fprint(w, `[{"full_name":"ok/r"},{"full_name":"gone/r"}]`)
		case p == "/users/gone":
			w.WriteHeader(404)
		case strings.HasPrefix(p, "/repos/ok/"):
			w.WriteHeader(422)
			w.Write([]byte(`{"message":"No commit found for SHA"}`))
		default:
			w.WriteHeader(404)
		}
	}
	c, done := apiClient(h)
	defer done()
	commits := []model.OldCommit{{SHA: sha}}
	strict := Forks{Client: c}.Run(context.Background(), "o/r", commits)
	if strict.Surfaces[0].Complete || strict.HiddenAccepted {
		t.Fatalf("without the flag hidden forks keep the surface incomplete: %+v", strict)
	}
	acc := Forks{Client: c, AcceptHidden: true}.Run(context.Background(), "o/r", commits)
	if !acc.Surfaces[0].Complete || !acc.HiddenAccepted || len(acc.HiddenForks) != 1 || acc.HiddenForks[0] != "gone/r" {
		t.Fatalf("%+v", acc)
	}
	for _, f := range acc.Findings {
		if f.Location == "gone/r" && f.Status != model.Unknown {
			t.Fatalf("accepting must never change a finding: %+v", f)
		}
	}
}

// Forks below a hidden fork cannot be listed; accepting hidden forks sets them aside
// for the readiness line only.
func TestAcceptHiddenCoversForksBelowAHiddenFork(t *testing.T) {
	c, done := apiClient(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case p == "/repos/o/r":
			fmt.Fprint(w, `{"forks_count":3}`)
		case p == "/repos/o/r/forks":
			fmt.Fprint(w, `[{"full_name":"ok/r"},{"full_name":"gone/r","forks_count":1},{"full_name":"vis/r","forks_count":2}]`)
		case p == "/users/gone":
			w.WriteHeader(404)
		case strings.HasPrefix(p, "/repos/ok/") || strings.HasPrefix(p, "/repos/vis/") && strings.Contains(p, "/commits/"):
			w.WriteHeader(422)
			w.Write([]byte(`{"message":"No commit found for SHA"}`))
		default:
			w.WriteHeader(404) // includes /repos/gone/r/forks and /repos/vis/r/forks
		}
	})
	defer done()
	commits := []model.OldCommit{{SHA: sha}}
	res := Forks{Client: c, AcceptHidden: true}.Run(context.Background(), "o/r", commits)
	if !strings.Contains(res.ForksIncompleteReason, "2 listed forks") {
		t.Fatalf("strict reason %q", res.ForksIncompleteReason)
	}
	if !strings.Contains(res.ForksReasonAccepted, "1 listed forks") {
		t.Fatalf("a visible fork whose forks cannot be listed stays a gap: %q", res.ForksReasonAccepted)
	}
	if res.Surfaces[0].Complete {
		t.Fatal("the visible gap must keep the surface incomplete")
	}
}
