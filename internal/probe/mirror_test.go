package probe

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0x524a/gh-residue/internal/model"
)

func sh(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// fixture mimics Gate 0: B survives only as a parent of refs/pull/1/merge; C is
// dangling (never fetched by a mirror clone); T is tagged.
func fixture(t *testing.T) (bare, b, c, tagged, gone string) {
	t.Helper()
	root := t.TempDir()
	work := filepath.Join(root, "w")
	bare = filepath.Join(root, "origin.git")
	sh(t, root, "init", "-q", "-b", "main", work)
	sh(t, work, "commit", "-q", "--allow-empty", "-m", "base")
	base := sh(t, work, "rev-parse", "HEAD")
	sh(t, work, "commit", "-q", "--allow-empty", "-m", "B")
	b = sh(t, work, "rev-parse", "HEAD")
	sh(t, work, "reset", "-q", "--hard", base)
	sh(t, work, "commit", "-q", "--allow-empty", "-m", "T")
	tagged = sh(t, work, "rev-parse", "HEAD")
	sh(t, work, "tag", "v1")
	sh(t, work, "reset", "-q", "--hard", base)
	sh(t, work, "commit", "-q", "--allow-empty", "-m", "merge")
	sh(t, work, "merge", "-q", "--no-ff", "-m", "m", b)
	mergeSHA := sh(t, work, "rev-parse", "HEAD")
	sh(t, work, "reset", "-q", "--hard", base)
	sh(t, root, "clone", "-q", "--bare", work, bare)
	sh(t, bare, "update-ref", "refs/pull/1/merge", mergeSHA)
	// C: object exists in the origin but is on no ref.
	sh(t, work, "commit", "-q", "--allow-empty", "-m", "C")
	c = sh(t, work, "rev-parse", "HEAD")
	sh(t, work, "push", "-q", bare, c+":refs/heads/tmp")
	sh(t, bare, "update-ref", "-d", "refs/heads/tmp")
	return bare, b, c, tagged, "1234567890123456789012345678901234567890"
}

func find(fs []model.Finding, sha string, s model.Surface) (out []model.Finding) {
	for _, f := range fs {
		if f.OldSHA == sha && f.Surface == s {
			out = append(out, f)
		}
	}
	return
}

func TestMirrorProbe(t *testing.T) {
	bare, b, c, tagged, gone := fixture(t)
	root := t.TempDir()
	res := Mirror{CloneURL: bare, TempRoot: root}.Run(context.Background(), "o/r",
		[]model.OldCommit{{SHA: b}, {SHA: c}, {SHA: tagged}, {SHA: gone}})
	if res.Failed {
		t.Fatalf("failed: %+v", res.Findings)
	}
	if f := find(res.Findings, b, model.SurfacePRRef); len(f) != 1 || f[0].Status != model.Confirmed || f[0].Location != "refs/pull/1/merge" {
		t.Errorf("B via merge ref: %+v", f)
	}
	if f := find(res.Findings, b, model.SurfaceBranch); f[0].Status != model.NotFound {
		t.Errorf("B must not be on a branch: %+v", f)
	}
	if f := find(res.Findings, tagged, model.SurfaceTag); f[0].Status != model.Confirmed || f[0].Location != "refs/tags/v1" {
		t.Errorf("tag: %+v", f)
	}
	for _, sha := range []string{c, gone} { // dangling or never existed: invisible to a mirror
		for _, s := range refSurfaces {
			if f := find(res.Findings, sha, s); f[0].Status != model.NotFound {
				t.Errorf("%s/%s: %+v", sha, s, f)
			}
		}
	}
	for _, s := range res.Surfaces {
		if !s.Complete {
			t.Errorf("surface incomplete: %+v", s)
		}
	}
	if left, _ := os.ReadDir(root); len(left) != 0 {
		t.Errorf("temp dir not cleaned: %v", left)
	}
}

func TestMirrorCloneFailureIsUnknownAndCleansUp(t *testing.T) {
	root := t.TempDir()
	res := Mirror{CloneURL: filepath.Join(root, "nope.git"), TempRoot: root}.Run(context.Background(), "o/r",
		[]model.OldCommit{{SHA: sha}})
	if !res.Failed {
		t.Fatal("expected Failed")
	}
	for _, f := range res.Findings {
		if f.Status != model.Unknown {
			t.Errorf("clone failure produced %s", f.Status)
		}
	}
	for _, s := range res.Surfaces {
		if s.Complete {
			t.Error("surface must be incomplete")
		}
	}
	if left, _ := os.ReadDir(root); len(left) != 0 {
		t.Errorf("temp dir not cleaned: %v", left)
	}
}
