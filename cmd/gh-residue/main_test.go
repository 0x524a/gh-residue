package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/0x524a/gh-residue/internal/model"
)

func TestUsageErrorsExit2(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty")
	os.WriteFile(empty, []byte("# nothing\n"), 0o600)
	for name, args := range map[string][]string{
		"no args":      {},
		"no repo":      {"verify"},
		"bad repo":     {"verify", "nope", "--input", empty},
		"no input":     {"verify", "o/r"},
		"missing file": {"verify", "o/r", "--input", filepath.Join(dir, "x")},
		"empty input":  {"verify", "o/r", "--input", empty},
	} {
		var out, errb bytes.Buffer
		if code := run(args, &out, &errb); code != 2 {
			t.Errorf("%s: exit %d, want 2 (stderr: %s)", name, code, errb.String())
		}
	}
}

func TestEstimateGate(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "shas")
	os.WriteFile(f, []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\nbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n"), 0o600)
	var out, errb bytes.Buffer
	if code := run([]string{"verify", "o/r", "--input", f, "--no-mirror", "--max-forks", "1000"}, &out, &errb); code != 2 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if !bytes.Contains(errb.Bytes(), []byte("--yes")) {
		t.Errorf("expected --yes hint: %s", errb.String())
	}
}

func writeRun(t *testing.T, path string, r *model.Run) {
	t.Helper()
	if err := writeCheckpoint(path, r); err != nil {
		t.Fatal(err)
	}
}

func TestCheckpointRoundTripAndMode(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cp.json")
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	r := &model.Run{SchemaVersion: model.SchemaVersion, Repo: "O/R", Findings: []model.Finding{
		{OldSHA: "a", Status: model.NotFound, CheckedAt: now.Add(-time.Hour).Format(time.RFC3339)},
		{OldSHA: "b", Status: model.Unknown}, // unknown results are ignored by the age check
	}}
	writeRun(t, p, r)
	if st, err := os.Stat(p); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v err %v", st.Mode(), err)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(p), ".gh-residue-checkpoint-*")); len(left) != 0 {
		t.Errorf("temp file left behind: %v", left)
	}
	got, err := loadCheckpoint(p, "o/r", 24*time.Hour, now) // repo match is case-insensitive
	if err != nil || got == nil || len(got.Findings) != 2 {
		t.Fatalf("%v %v", got, err)
	}
}

func TestCheckpointRefusals(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)

	if got, err := loadCheckpoint(filepath.Join(dir, "absent.json"), "o/r", time.Hour, now); got != nil || err != nil {
		t.Errorf("missing file is a fresh run: %v %v", got, err)
	}

	p := filepath.Join(dir, "cp.json")
	writeRun(t, p, &model.Run{SchemaVersion: model.SchemaVersion, Repo: "o/r", Findings: []model.Finding{
		{OldSHA: "a", Status: model.Confirmed, CheckedAt: now.Add(-48 * time.Hour).Format(time.RFC3339)},
	}})
	if _, err := loadCheckpoint(p, "other/repo", 100*time.Hour, now); err == nil {
		t.Error("different repo must be refused")
	}
	if _, err := loadCheckpoint(p, "o/r", 24*time.Hour, now); err == nil {
		t.Error("stale result must be refused")
	}
	writeRun(t, p, &model.Run{SchemaVersion: model.SchemaVersion, Repo: "o/r", Findings: []model.Finding{
		{OldSHA: "a", Status: model.NotFound, CheckedAt: "garbage"},
	}})
	if _, err := loadCheckpoint(p, "o/r", 24*time.Hour, now); err == nil {
		t.Error("unparseable checked_at must count as stale")
	}
	os.WriteFile(p, []byte("{not json"), 0o600)
	if _, err := loadCheckpoint(p, "o/r", 24*time.Hour, now); err == nil {
		t.Error("corrupt file must be an error, not a silent fresh run")
	}
}

func TestVersion(t *testing.T) {
	for _, a := range []string{"--version", "version"} {
		var out, errb bytes.Buffer
		if code := run([]string{a}, &out, &errb); code != 0 {
			t.Fatalf("%s: exit %d", a, code)
		}
		if !bytes.HasPrefix(out.Bytes(), []byte("gh-residue "+baseVersion)) {
			t.Errorf("%s: %q", a, out.String())
		}
	}
	if got := buildVersion("1.2.3"); !strings.HasPrefix(got, "1.2.3") {
		t.Errorf("buildVersion: %q", got)
	}
}
