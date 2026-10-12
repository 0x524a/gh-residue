package input

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const a = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const b = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
const c = "cccccccccccccccccccccccccccccccccccccccc"
const z = "0000000000000000000000000000000000000000"

func TestSHAList(t *testing.T) {
	got, err := ParseSHAList(strings.NewReader("# c\n" + a + "\n\n" + strings.ToUpper(b) + "\n" + a + "\n"))
	if err != nil || len(got) != 2 || got[1].SHA != b {
		t.Fatalf("got %v err %v", got, err)
	}
	if _, err := ParseSHAList(strings.NewReader("abc1234\n")); err == nil {
		t.Fatal("short SHA must be rejected")
	}
}

func TestCommitMap(t *testing.T) {
	m := "old new\n" + a + " " + b + "\n" + c + " " + c + "\n" + b + " " + z + "\n"
	got, err := ParseCommitMap(strings.NewReader(m))
	if err != nil || len(got) != 2 {
		t.Fatalf("got %v err %v", got, err)
	}
	if got[0].SHA != a || got[0].NewSHA != b || got[1].SHA != b || got[1].NewSHA != "" {
		t.Fatalf("bad parse %+v", got)
	}
	if _, err := ParseCommitMap(strings.NewReader(a + " short\n")); err == nil {
		t.Fatal("malformed line must error")
	}
}

func TestFirstChanged(t *testing.T) {
	got, err := parseFirstChanged(strings.NewReader(a + " " + b + "\n\n" + c + " " + b + "\n"))
	if err != nil || len(got) != 2 || got[0] != a || got[1] != c {
		t.Fatalf("got %v err %v", got, err)
	}
	if _, err := parseFirstChanged(strings.NewReader("short " + b + "\n")); err == nil {
		t.Fatal("malformed line must error, not be skipped")
	}
}

func TestLoadFirstChangedMissingFileIsNilNotGuess(t *testing.T) {
	got, err := LoadFirstChanged(t.TempDir())
	if got != nil || err != nil {
		t.Fatalf("got %v err %v", got, err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git", "filter-repo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "filter-repo", "first-changed-commits"), []byte(a+" "+b+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadFirstChanged(dir); err != nil || len(got) != 1 || got[0] != a {
		t.Fatalf("got %v err %v", got, err)
	}
}
