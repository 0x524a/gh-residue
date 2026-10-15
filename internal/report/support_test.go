package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/0x524a/gh-residue/internal/model"
)

func runWith(fs ...model.Finding) *model.Run {
	return &model.Run{Repo: "o/r", ToolVersion: "t", StartedAt: "now", Findings: fs,
		Coverage: model.Coverage{Surfaces: []model.SurfaceCoverage{{Surface: model.SurfacePRRef, Complete: true}}}}
}

func TestReadiness(t *testing.T) {
	pr := model.Finding{OldSHA: "a", Surface: model.SurfacePRRef, Location: "refs/pull/3/merge", Status: model.Confirmed}
	br := model.Finding{OldSHA: "a", Surface: model.SurfaceBranch, Location: "refs/heads/x", Status: model.Confirmed}

	if a, _ := Readiness(runWith(br)); a != "no" {
		t.Errorf("confirmed branch: %s", a)
	}
	// PR-only exposure is Support's job, but ref-holding forks are unprobed => unknown, never yes.
	if a, _ := Readiness(runWith(pr)); a != "unknown" {
		t.Errorf("fork refs unprobed must be unknown, got %s", a)
	}
	r := runWith(pr)
	r.Coverage.Surfaces = append(r.Coverage.Surfaces, model.SurfaceCoverage{Surface: model.SurfaceForkRef, Complete: true})
	if a, _ := Readiness(r); a != "yes" {
		t.Errorf("complete, only PR refs: %s", a)
	}
	r.Coverage.Surfaces[0].Complete = false
	if a, _ := Readiness(r); a != "unknown" {
		t.Errorf("incomplete must be unknown: %s", a)
	}
}

func TestReadinessGuards(t *testing.T) {
	pr := model.Finding{OldSHA: "a", Surface: model.SurfaceForkRef, Location: "x/f", Status: model.NotFound}
	done := func(reason string) *model.Run {
		return &model.Run{Findings: []model.Finding{pr}, Coverage: model.Coverage{
			ForksIncompleteReason: reason,
			Surfaces:              []model.SurfaceCoverage{{Surface: model.SurfaceForkRef, Complete: true}}}}
	}
	if a, _ := Readiness(done("")); a != "yes" {
		t.Errorf("complete and clean should be yes, got %s", a)
	}
	// Even if a probe claimed the surface complete, a recorded reason must block "yes".
	if a, why := Readiness(done("1 listed forks have forks of their own, which are not checked")); a != "unknown" || why == "" {
		t.Errorf("forks of forks: %s %q", a, why)
	}
	if a, _ := Readiness(&model.Run{Coverage: model.Coverage{Surfaces: []model.SurfaceCoverage{{Surface: model.SurfaceForkRef, Complete: true}}}}); a != "unknown" {
		t.Errorf("no findings must never be yes, got %s", a)
	}
}

func TestSupportDraft(t *testing.T) {
	r := runWith(
		model.Finding{OldSHA: "b", Surface: model.SurfacePRRef, Location: "refs/pull/7/head", Status: model.Confirmed},
		model.Finding{OldSHA: "b", Surface: model.SurfacePRRef, Location: "refs/pull/7/merge", Status: model.Confirmed},
		model.Finding{OldSHA: "b", Surface: model.SurfaceForkObject, Location: "someone/fork", Status: model.Confirmed},
	)
	var b bytes.Buffer
	SupportDraft(&b, r, SupportOpts{FirstChanged: []string{"abc"}, Source: "passed with --first-changed", LFSFile: "lfs.txt"})
	out := b.String()
	for _, want := range []string{"`o/r`", "1 (observed holding an old commit: #7)", "- `abc`", "Source: passed with --first-changed",
		"attach `lfs.txt`", "- `b`", "pr_ref: 2", "**Ready for Support?** unknown", "_TODO_"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "someone") {
		t.Error("fork owner names must be omitted")
	}
	var b2 bytes.Buffer
	SupportDraft(&b2, r, SupportOpts{})
	if !strings.Contains(b2.String(), "Not provided") {
		t.Error("missing first-changed must be called out")
	}
}

func TestReadinessNamesHiddenForks(t *testing.T) {
	r := &model.Run{
		Coverage: model.Coverage{ForksHidden: 2, ForksTotal: 5, ForksChecked: 5,
			Surfaces: []model.SurfaceCoverage{{Surface: model.SurfaceForkRef, Complete: false}}},
		Findings: []model.Finding{{OldSHA: "a", Surface: model.SurfaceForkRef, Location: "x/y", Status: model.Unknown}},
	}
	ans, why := Readiness(r)
	if ans != "unknown" || !strings.Contains(why, "2 listed forks are hidden by GitHub") {
		t.Fatalf("%q %q", ans, why)
	}
}
