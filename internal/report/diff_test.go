package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/0x524a/gh-residue/internal/model"
)

func fnd(sha string, s model.Surface, loc string, st model.Status) model.Finding {
	return model.Finding{OldSHA: sha, Surface: s, Location: loc, Status: st}
}

func mk(complete bool, fs ...model.Finding) *model.Run {
	return &model.Run{SchemaVersion: 1, Findings: fs, Coverage: model.Coverage{Surfaces: []model.SurfaceCoverage{
		{Surface: model.SurfacePRRef, Complete: complete},
		{Surface: model.SurfaceBaseObject, Complete: true},
		{Surface: model.SurfaceForkObject, Complete: true},
	}}}
}

func classes(es []DiffEntry) map[string]DiffClass {
	m := map[string]DiffClass{}
	for _, e := range es {
		m[string(e.Surface)+"|"+e.Location] = e.Class
	}
	return m
}

func TestDiffClasses(t *testing.T) {
	prev := mk(true,
		fnd("a", model.SurfaceBaseObject, "o/r", model.Confirmed),
		fnd("a", model.SurfacePRRef, "refs/pull/1/merge", model.Confirmed),
		fnd("a", model.SurfacePRRef, "refs/pull/2/merge", model.Confirmed),
		fnd("a", model.SurfaceForkObject, "x/fork", model.Confirmed),
		fnd("a", model.SurfaceForkObject, "y/fork", model.Confirmed),
		fnd("a", model.SurfacePRRef, "*", model.NotFound),
	)
	cur := mk(true,
		fnd("a", model.SurfaceBaseObject, "o/r", model.NotFound),           // Support acted
		fnd("a", model.SurfacePRRef, "refs/pull/1/merge", model.Confirmed), // unchanged
		fnd("a", model.SurfacePRRef, "refs/pull/9/merge", model.Confirmed), // new ref
		fnd("a", model.SurfaceForkObject, "x/fork", model.Unknown),         // lost knowledge
		// refs/pull/2/merge absent from a complete enumeration => resolved
		// y/fork absent from sample => now-unknown
	)
	got := classes(Diff(prev, cur))
	want := map[string]DiffClass{
		"base_object|o/r":          Resolved,
		"pr_ref|refs/pull/1/merge": StillExposed,
		"pr_ref|refs/pull/9/merge": New,
		"pr_ref|refs/pull/2/merge": Resolved,
		"fork_object|x/fork":       NowUnknown,
		"fork_object|y/fork":       NowUnknown,
	}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %s want %s", k, got[k], v)
		}
	}
}

// Core safety property: a ref vanishing from an INCOMPLETE run is never "resolved".
func TestDiffAbsentFromIncompleteRunIsNotResolved(t *testing.T) {
	prev := mk(true, fnd("a", model.SurfacePRRef, "refs/pull/1/merge", model.Confirmed))
	cur := mk(false)
	if c := classes(Diff(prev, cur))["pr_ref|refs/pull/1/merge"]; c != NowUnknown {
		t.Fatalf("got %s", c)
	}
	// Even if the surface claims complete, a surface-wide unknown must taint it.
	cur = mk(true, fnd("a", model.SurfacePRRef, "*", model.Unknown))
	if c := classes(Diff(prev, cur))["pr_ref|refs/pull/1/merge"]; c != NowUnknown {
		t.Fatalf("wildcard unknown: got %s", c)
	}
}

func TestWriteDiffWarnsLoudly(t *testing.T) {
	var b bytes.Buffer
	WriteDiff(&b, []DiffEntry{{"a", model.SurfacePRRef, "refs/pull/1/head", NowUnknown}})
	if !strings.Contains(b.String(), "!! now-unknown") || !strings.Contains(b.String(), "WARNING") {
		t.Error(b.String())
	}
	b.Reset()
	WriteDiff(&b, nil)
	if !strings.Contains(b.String(), "no changes") {
		t.Error(b.String())
	}
}

func TestLoadRunRejectsOtherSchema(t *testing.T) {
	if _, err := LoadRun(strings.NewReader(`{"schema_version":99}`)); err == nil {
		t.Fatal("must reject unknown schema")
	}
	if _, err := LoadRun(strings.NewReader(`{"schema_version":1,"repo":"o/r","findings":[]}`)); err != nil {
		t.Fatal(err)
	}
}
