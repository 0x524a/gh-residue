package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/0x524a/gh-residue/internal/ghapi"
	"github.com/0x524a/gh-residue/internal/model"
	"github.com/0x524a/gh-residue/internal/probe"
)

type fake struct{ res probe.Result }

func (fake) Name() string                                                  { return "fake" }
func (f fake) Run(context.Context, string, []model.OldCommit) probe.Result { return f.res }

func TestFailedProbeDoesNotDiscardOthers(t *testing.T) {
	good := probe.Result{
		Findings: []model.Finding{{OldSHA: "a", Surface: model.SurfaceBaseObject, Status: model.Confirmed}},
		Surfaces: []model.SurfaceCoverage{{Surface: model.SurfaceBaseObject, Complete: true}},
	}
	bad := probe.Result{
		Failed:   true,
		Findings: []model.Finding{{OldSHA: "a", Surface: model.SurfacePRRef, Status: model.Unknown}},
		Surfaces: []model.SurfaceCoverage{{Surface: model.SurfacePRRef, Complete: false}},
	}
	run := Verify(context.Background(), "o/r", "sha-list", "t", nil, []probe.Probe{fake{bad}, fake{good}}, nil)
	if len(run.Findings) != 2 || run.Coverage.ProbesFailed != 1 || run.Coverage.ProbesRun != 2 {
		t.Fatalf("%+v", run)
	}
	if run.ExitCode() != 1 {
		t.Fatalf("confirmed exposure must still exit 1, got %d", run.ExitCode())
	}
}

func TestAllFailedIsExit2(t *testing.T) {
	bad := probe.Result{Failed: true, Findings: []model.Finding{{Status: model.Unknown}}}
	run := Verify(context.Background(), "o/r", "x", "t", nil, []probe.Probe{fake{bad}}, nil)
	if run.ExitCode() != 2 {
		t.Fatalf("got %d", run.ExitCode())
	}
}

func TestSkippedProbeNeverExitsZero(t *testing.T) {
	ok := probe.Result{
		Findings: []model.Finding{{OldSHA: "a", Surface: model.SurfaceBaseObject, Status: model.NotFound}},
		Surfaces: []model.SurfaceCoverage{{Surface: model.SurfaceBaseObject, Complete: true}},
	}
	skipped := probe.Skipped{Label: "mirror", Surfaces: []model.Surface{model.SurfacePRRef}}
	run := Verify(context.Background(), "o/r", "x", "t", nil, []probe.Probe{fake{ok}, skipped}, nil)
	if run.ExitCode() != 2 {
		t.Fatalf("skipped probe must give exit 2, got %d", run.ExitCode())
	}
}

type recorder struct {
	known *probe.Known
	res   probe.Result
}

func (r *recorder) Name() string                                                { return "rec" }
func (r *recorder) Run(context.Context, string, []model.OldCommit) probe.Result { return r.res }
func (r *recorder) WithKnown(k *probe.Known) probe.Probe                        { r.known = k; return r }

func TestResumeCarriesKnownDropsStaleAndNeverCarriesUnknown(t *testing.T) {
	const a = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	prev := &model.Run{StartedAt: "T0", Findings: []model.Finding{
		{OldSHA: a, Surface: model.SurfaceBaseObject, Location: "o/r", Status: model.Confirmed, CheckedAt: "T0"},
		{OldSHA: a, Surface: model.SurfaceForkObject, Location: "x/f", Status: model.NotFound, CheckedAt: "T0"},
		{OldSHA: a, Surface: model.SurfaceForkObject, Location: "y/f", Status: model.Unknown},
		{OldSHA: a, Surface: model.SurfacePRRef, Location: "*", Status: model.NotFound, CheckedAt: "T0"},
		{OldSHA: "dddddddddddddddddddddddddddddddddddddddd", Surface: model.SurfaceBaseObject, Location: "o/r", Status: model.Confirmed},
	}}
	// Fresh run re-checks y/f (now known) and re-evaluates the pr_ref surface for a.
	rec := &recorder{res: probe.Result{Findings: []model.Finding{
		{OldSHA: a, Surface: model.SurfaceForkObject, Location: "y/f", Status: model.Confirmed},
		{OldSHA: a, Surface: model.SurfacePRRef, Location: "refs/pull/1/merge", Status: model.Confirmed},
	}, Surfaces: []model.SurfaceCoverage{{Surface: model.SurfaceForkObject, Complete: true}, {Surface: model.SurfacePRRef, Complete: true}}}}
	run := Verify(context.Background(), "o/r", "x", "t", []model.OldCommit{{SHA: a}}, []probe.Probe{rec}, prev)

	got := map[string]model.Status{}
	for _, f := range run.Findings {
		got[string(f.Surface)+"|"+f.Location] = f.Status
	}
	want := map[string]model.Status{
		"base_object|o/r":          model.Confirmed, // carried
		"fork_object|x/f":          model.NotFound,  // carried
		"fork_object|y/f":          model.Confirmed, // fresh (was unknown)
		"pr_ref|refs/pull/1/merge": model.Confirmed, // fresh
	}
	if len(got) != len(want) {
		t.Fatalf("got %v", got) // notably: stale pr_ref|* not_found must be gone, other SHA dropped
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %s want %s", k, got[k], v)
		}
	}
	if run.Carried != 2 || run.ResumedFrom != "T0" {
		t.Errorf("carried=%d from=%q", run.Carried, run.ResumedFrom)
	}
	for _, f := range run.Findings {
		if f.Surface == model.SurfaceBaseObject && f.CheckedAt != "T0" {
			t.Error("carried results must keep their original checked_at")
		}
	}
}

func TestNoPrevMeansNoResumeFields(t *testing.T) {
	run := Verify(context.Background(), "o/r", "x", "t", nil, nil, nil)
	if run.Carried != 0 || run.ResumedFrom != "" {
		t.Fatalf("%+v", run)
	}
}

func TestSaveKeepsACheckpointCurrentDuringAProbe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer srv.Close()
	c := ghapi.New("")
	c.BaseURL = srv.URL
	var commits []model.OldCommit
	for i := 0; i < 120; i++ {
		commits = append(commits, model.OldCommit{SHA: fmt.Sprintf("%040x", i+1)})
	}
	var sizes []int
	var inSave bool
	run := VerifyWithSave(context.Background(), "o/r", "sha-list", "t", commits,
		[]probe.Probe{probe.BaseObject{Client: c}}, nil, func(part *model.Run) {
			if inSave {
				t.Error("save called concurrently")
			}
			inSave = true
			sizes = append(sizes, len(part.Findings))
			inSave = false
		})
	// 50 and 100 findings during the probe, then 120 when it finishes.
	if len(sizes) != 3 || sizes[0] != 50 || sizes[1] != 100 || sizes[2] != 120 {
		t.Fatalf("snapshots %v", sizes)
	}
	if len(run.Findings) != 120 {
		t.Fatal(len(run.Findings))
	}
}

// A partial run must keep results carried from the previous checkpoint, or the
// interim write would erase them.
func TestSaveKeepsCarriedResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer srv.Close()
	c := ghapi.New("")
	c.BaseURL = srv.URL
	old := model.OldCommit{SHA: strings.Repeat("a", 40)}
	prev := &model.Run{Repo: "o/r", Findings: []model.Finding{
		{OldSHA: old.SHA, Surface: model.SurfaceBaseObject, Location: "o/r", Status: model.NotFound, CheckedAt: "2026-01-01T00:00:00Z"},
	}}
	var last *model.Run
	VerifyWithSave(context.Background(), "o/r", "sha-list", "t", []model.OldCommit{old},
		[]probe.Probe{probe.BaseObject{Client: c}}, prev, func(part *model.Run) { last = part })
	if last == nil || len(last.Findings) != 1 || last.Findings[0].CheckedAt != "2026-01-01T00:00:00Z" {
		t.Fatalf("carried result lost: %+v", last)
	}
}
