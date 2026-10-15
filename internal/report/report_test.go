package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/0x524a/gh-residue/internal/model"
)

func sample() *model.Run {
	r := &model.Run{
		SchemaVersion: 1, ToolVersion: "t", Repo: "o/r",
		Inputs: model.Inputs{Source: "sha-list", Count: 2},
		Coverage: model.Coverage{ForksTotal: 10, ForksChecked: 4,
			Surfaces: []model.SurfaceCoverage{{Surface: model.SurfaceForkObject, Complete: false}}},
		Findings: []model.Finding{
			{OldSHA: "b", Surface: model.SurfacePRRef, Location: "refs/pull/1/merge", Status: model.Confirmed, Evidence: "ref contains commit"},
			{OldSHA: "a", Surface: model.SurfaceBaseObject, Location: "o/r", Status: model.Unknown, Evidence: "HTTP 404"},
		},
	}
	r.Sort()
	return r
}

func TestTable(t *testing.T) {
	var b bytes.Buffer
	Table(&b, sample())
	out := b.String()
	for _, want := range []string{
		"2 SHAs, 1 still exposed (1 via pr_ref); 1 check unknown. Exit code 1.",
		"checked 4 of 10 forks",
		"Incomplete surfaces: fork_object",
		"Rotate the credential first",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Index(out, "\na\n") > strings.Index(out, "\nb\n") {
		t.Error("SHAs not grouped in sorted order")
	}
}

func TestJSONStableAndVersioned(t *testing.T) {
	var a, b bytes.Buffer
	if err := JSON(&a, sample()); err != nil {
		t.Fatal(err)
	}
	JSON(&b, sample())
	if a.String() != b.String() {
		t.Fatal("JSON not deterministic")
	}
	var back map[string]any
	if err := json.Unmarshal(a.Bytes(), &back); err != nil || back["schema_version"] != float64(1) {
		t.Fatalf("%v %v", err, back)
	}
}

func TestTableCollapsesLongRuns(t *testing.T) {
	r := &model.Run{Repo: "o/r", Inputs: model.Inputs{Source: "x", Count: 1}}
	for i := 0; i < 12; i++ {
		r.Findings = append(r.Findings, model.Finding{OldSHA: "a", Surface: model.SurfacePRRef,
			Location: fmt.Sprintf("refs/pull/%02d/head", i), Status: model.Confirmed})
	}
	r.Sort()
	var b bytes.Buffer
	Table(&b, r)
	out := b.String()
	if strings.Count(out, "refs/pull/") != 5 || !strings.Contains(out, "+7 more") {
		t.Errorf("not collapsed:\n%s", out)
	}
	if !strings.Contains(out, "12 via pr_ref") {
		t.Errorf("summary must count all:\n%s", out)
	}
}

func TestTableNotesResume(t *testing.T) {
	r := sample()
	r.Carried = 3
	r.Findings[0].CheckedAt = "2026-10-11T10:00:00Z"
	r.Findings[1].CheckedAt = "2026-10-11T09:00:00Z"
	var b bytes.Buffer
	Table(&b, r)
	if !strings.Contains(b.String(), "3 results carried over unchanged (oldest checked 2026-10-11T09:00:00Z)") {
		t.Errorf("\n%s", b.String())
	}
	b.Reset()
	Table(&b, sample())
	if strings.Contains(b.String(), "Resumed") {
		t.Error("no note on a fresh run")
	}
}

func TestTableWordingWhenForkRefsChecked(t *testing.T) {
	r := sample()
	r.Coverage.Surfaces = append(r.Coverage.Surfaces, model.SurfaceCoverage{Surface: model.SurfaceForkRef, Complete: false})
	var b bytes.Buffer
	Table(&b, r)
	out := b.String()
	if !strings.Contains(out, "(object-resolvable and ref-holding)") || strings.Contains(out, "ref-holding not probed") {
		t.Errorf("fork coverage wording:\n%s", out)
	}
	if strings.Contains(out, "ref-holding forks (--fork-refs)") {
		t.Errorf("must not list ref-holding forks as not probed when they were:\n%s", out)
	}
}

func TestTableDisclosesForkAnchorScope(t *testing.T) {
	r := sample()
	r.Coverage.ForkCommitsSkipped = 40
	var b bytes.Buffer
	Table(&b, r)
	if !strings.Contains(b.String(), "first changed commits only; 40 other commits were not checked one by one") {
		t.Fatalf("scope not disclosed:\n%s", b.String())
	}
}

func TestTableDoesNotSayZeroOfZeroForks(t *testing.T) {
	r := sample()
	r.Coverage.ForksTotal, r.Coverage.ForksChecked = 0, 0
	r.Coverage.ForksIncompleteReason = "the repository is itself a fork of up/r, and the rest of that network is not checked"
	var b bytes.Buffer
	Table(&b, r)
	out := b.String()
	if strings.Contains(out, "0 of 0") || !strings.Contains(out, "has no forks of its own") || !strings.Contains(out, "itself a fork of up/r") {
		t.Fatalf("\n%s", out)
	}
}
