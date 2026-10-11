package report

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/0x524a/gh-residue/internal/model"
)

var update = flag.Bool("update", false, "rewrite golden files in testdata/")

// golden compares got with testdata/<name>.golden. Run `go test ./internal/report
// -update` after an intentional output change, then review the diff by hand.
func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (create it with -update)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs from golden file.\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

const (
	shaA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" // exposed everywhere
	shaB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" // clean
	shaC = "cccccccccccccccccccccccccccccccccccccccc" // unknown
	t0   = "2026-10-10T09:00:00Z"
	t1   = "2026-10-10T10:00:00Z"
)

func f(sha string, s model.Surface, loc string, st model.Status, ev, at string) model.Finding {
	return model.Finding{OldSHA: sha, Surface: s, Location: loc, Status: st, Evidence: ev, CheckedAt: at}
}

// fixtureRun is deliberately awkward: 7 PR refs (table collapses past 5), a
// branch exposure, sampled forks, an unknown, a clean SHA and a carried result.
func fixtureRun() *model.Run {
	r := &model.Run{
		SchemaVersion: model.SchemaVersion,
		ToolVersion:   "test",
		Repo:          "acme/widgets",
		StartedAt:     t1,
		Inputs:        model.Inputs{Source: "filter-repo", Count: 3},
		Carried:       1,
		ResumedFrom:   t0,
		Coverage: model.Coverage{
			ProbesRun: 3, ProbesFailed: 0, ForksTotal: 40, ForksChecked: 2,
			Surfaces: []model.SurfaceCoverage{
				{Surface: model.SurfaceForkObject, Complete: false},
				{Surface: model.SurfaceBaseObject, Complete: true},
				{Surface: model.SurfaceBranch, Complete: true},
				{Surface: model.SurfacePRRef, Complete: true},
				{Surface: model.SurfaceTag, Complete: true},
			},
		},
	}
	r.Findings = []model.Finding{
		f(shaA, model.SurfaceBaseObject, "acme/widgets", model.Confirmed, "HTTP 200", t0), // carried
		f(shaA, model.SurfaceBranch, "refs/heads/release", model.Confirmed, "ref contains commit", t1),
		f(shaA, model.SurfaceTag, "*", model.NotFound, "no ref of this kind contains commit", t1),
		f(shaA, model.SurfaceForkObject, "fork-1/widgets", model.Confirmed, "HTTP 200", t1),
		f(shaA, model.SurfaceForkObject, "fork-2/widgets", model.NotFound, "HTTP 422 No commit found", t1),
		f(shaB, model.SurfaceBaseObject, "acme/widgets", model.NotFound, "HTTP 422 No commit found", t1),
		f(shaB, model.SurfaceBranch, "*", model.NotFound, "object not in mirror", t1),
		f(shaB, model.SurfacePRRef, "*", model.NotFound, "object not in mirror", t1),
		f(shaB, model.SurfaceTag, "*", model.NotFound, "object not in mirror", t1),
		f(shaC, model.SurfaceBaseObject, "acme/widgets", model.Unknown, "HTTP 404", t1),
	}
	for i := 1; i <= 7; i++ {
		r.Findings = append(r.Findings, f(shaA, model.SurfacePRRef,
			fmt.Sprintf("refs/pull/%d/merge", i*10), model.Confirmed, "ref contains commit", t1))
	}
	r.Sort()
	return r
}

func TestGoldenTable(t *testing.T) {
	var b bytes.Buffer
	Table(&b, fixtureRun())
	golden(t, "table", b.Bytes())
}

func TestGoldenJSON(t *testing.T) {
	var b bytes.Buffer
	if err := JSON(&b, fixtureRun()); err != nil {
		t.Fatal(err)
	}
	golden(t, "run.json", b.Bytes())
}

func TestGoldenSupportDraft(t *testing.T) {
	var b bytes.Buffer
	SupportDraft(&b, fixtureRun(), SupportOpts{FirstChanged: []string{shaA}, Inferred: true, LFSFile: "lfs-orphans.txt"})
	golden(t, "support-full", b.Bytes())

	b.Reset()
	SupportDraft(&b, fixtureRun(), SupportOpts{})
	golden(t, "support-minimal", b.Bytes())
}

func TestGoldenDiff(t *testing.T) {
	prev := fixtureRun()
	cur := fixtureRun()
	cur.Carried, cur.ResumedFrom = 0, ""
	cur.StartedAt = "2026-10-11T10:00:00Z"
	// Support acted on the base repo; one fork is now unknown; one PR ref vanished;
	// a new tag exposure appeared.
	for i := range cur.Findings {
		x := &cur.Findings[i]
		switch {
		case x.OldSHA == shaA && x.Surface == model.SurfaceBaseObject:
			x.Status, x.Evidence = model.NotFound, "HTTP 422 No commit found"
		case x.Surface == model.SurfaceForkObject && x.Location == "fork-1/widgets":
			x.Status, x.Evidence = model.Unknown, "HTTP 502"
		}
	}
	var kept []model.Finding
	for _, x := range cur.Findings {
		if x.Location == "refs/pull/70/merge" {
			continue
		}
		if x.OldSHA == shaA && x.Surface == model.SurfaceTag {
			x = f(shaA, model.SurfaceTag, "refs/tags/v1", model.Confirmed, "ref contains commit", "2026-10-11T10:00:00Z")
		}
		kept = append(kept, x)
	}
	cur.Findings = kept
	cur.Sort()

	var b bytes.Buffer
	WriteDiff(&b, Diff(prev, cur))
	golden(t, "diff", b.Bytes())

	b.Reset()
	WriteDiff(&b, Diff(prev, prev))
	golden(t, "diff-unchanged", b.Bytes())
}
