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
