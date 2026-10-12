package model

import "testing"

func run(cov Coverage, st ...Status) *Run {
	r := &Run{Coverage: cov}
	for _, s := range st {
		r.Findings = append(r.Findings, Finding{Status: s})
	}
	return r
}

var complete = Coverage{Surfaces: []SurfaceCoverage{{Surface: SurfaceBaseObject, Complete: true}}}

func TestExitCode(t *testing.T) {
	sampled := complete
	sampled.ForksTotal, sampled.ForksChecked = 10, 5
	failed := complete
	failed.ProbesFailed = 1
	partial := Coverage{Surfaces: []SurfaceCoverage{{Surface: SurfacePRRef, Complete: false}}}
	cases := []struct {
		name string
		r    *Run
		want int
	}{
		{"all not_found complete", run(complete, NotFound, NotFound), 0},
		{"confirmed", run(complete, NotFound, Confirmed), 1},
		{"confirmed beats unknown", run(complete, Unknown, Confirmed), 1},
		{"unknown", run(complete, NotFound, Unknown), 2},
		{"unrecognised status", run(complete, Status("weird")), 2},
		{"sampled forks", run(sampled, NotFound), 2},
		{"failed probe", run(failed, NotFound), 2},
		{"incomplete surface", run(partial, NotFound), 2},
		{"no findings is not clean", run(complete), 2},
	}
	for _, c := range cases {
		if got := c.r.ExitCode(); got != c.want {
			t.Errorf("%s: got %d want %d", c.name, got, c.want)
		}
	}
}

func TestForkIncompleteReasonBlocksClean(t *testing.T) {
	cov := complete
	cov.ForksIncompleteReason = "forks of forks are not checked"
	r := run(cov, NotFound)
	if !r.Incomplete() || r.ExitCode() != 2 {
		t.Fatalf("a recorded fork-coverage reason must make the run incomplete: incomplete=%v exit=%d", r.Incomplete(), r.ExitCode())
	}
}

func TestSortDeterministic(t *testing.T) {
	r := &Run{Findings: []Finding{
		{OldSHA: "b", Surface: SurfacePRRef, Location: "2"},
		{OldSHA: "a", Surface: SurfaceTag, Location: "x"},
		{OldSHA: "a", Surface: SurfaceBranch, Location: "y"},
	}}
	r.Sort()
	if r.Findings[0].Surface != SurfaceBranch || r.Findings[2].OldSHA != "b" {
		t.Fatalf("bad order: %+v", r.Findings)
	}
}
