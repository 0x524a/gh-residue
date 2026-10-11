package probe

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/0x524a/gh-residue/internal/model"
)

// Mirror is probe 2 (also covers probe 4, tags): a temporary bare
// `git clone --mirror` carries refs/pull/*, then `for-each-ref --contains`
// names every ref that reaches an old SHA, including as an ancestor of a PR tip.
//
// A SHA absent from a successful mirror is NotFound for these ref surfaces. That
// is only valid because the clone completed; a clone failure is Unknown. Gate 0
// showed dangling commits are invisible here and seen only by BaseObject.
type Mirror struct {
	CloneURL string // https://github.com/o/r.git, or a local path in tests
	Token    string
	TempRoot string // defaults to os.TempDir()
	Known    *Known
}

func (p Mirror) WithKnown(k *Known) Probe { p.Known = k; return p }

// pending returns the SHAs whose ref surfaces are not all already known.
func (p Mirror) pending(commits []model.OldCommit) []model.OldCommit {
	var out []model.OldCommit
	for _, c := range commits {
		for _, s := range refSurfaces {
			if !p.Known.SurfaceKnown(c.SHA, s) {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

func (Mirror) Name() string { return "mirror" }

var refSurfaces = []model.Surface{model.SurfaceBranch, model.SurfacePRRef, model.SurfaceTag}

func (p Mirror) Run(ctx context.Context, repo string, commits []model.OldCommit) Result {
	commits = p.pending(commits)
	if len(commits) == 0 { // everything already known: skip the clone entirely
		var res Result
		for _, s := range refSurfaces {
			res.Surfaces = append(res.Surfaces, model.SurfaceCoverage{Surface: s, Complete: true})
		}
		return res
	}
	dir, err := os.MkdirTemp(p.TempRoot, "gh-residue-*")
	if err != nil {
		return p.failed(commits, "tempdir: "+err.Error())
	}
	defer os.RemoveAll(dir) // always, including on error
	if err := os.Chmod(dir, 0o700); err != nil {
		return p.failed(commits, "chmod: "+err.Error())
	}
	env, err := p.gitEnv(dir)
	if err != nil {
		return p.failed(commits, err.Error())
	}
	mirror := filepath.Join(dir, "mirror.git")
	if _, err := p.git(ctx, env, "", "clone", "--mirror", "--quiet", p.CloneURL, mirror); err != nil {
		return p.failed(commits, "clone: "+err.Error())
	}

	var res Result
	for _, c := range commits {
		res.Findings = append(res.Findings, p.checkOne(ctx, env, mirror, c.SHA)...)
	}
	for _, s := range refSurfaces {
		res.Surfaces = append(res.Surfaces, model.SurfaceCoverage{Surface: s, Complete: complete(res.Findings, s)})
	}
	return res
}

func (p Mirror) checkOne(ctx context.Context, env []string, mirror, sha string) []model.Finding {
	unknown := func(ev string) []model.Finding {
		var out []model.Finding
		for _, s := range refSurfaces {
			out = append(out, finding(sha, s, "*", model.Unknown, ev))
		}
		return out
	}
	// rev-parse --quiet exits 1 for an absent object and >1 for real errors.
	_, err := p.git(ctx, env, mirror, "rev-parse", "--verify", "--quiet", sha+"^{commit}")
	var ee *exitError
	if errors.As(err, &ee) && ee.code == 1 {
		var out []model.Finding
		for _, s := range refSurfaces {
			out = append(out, finding(sha, s, "*", model.NotFound, "object not in mirror"))
		}
		return out
	}
	if err != nil {
		return unknown("rev-parse: " + err.Error())
	}
	refs, err := p.git(ctx, env, mirror, "for-each-ref", "--contains", sha, "--format=%(refname)")
	if err != nil {
		return unknown("for-each-ref: " + err.Error())
	}
	hit := map[model.Surface]bool{}
	var out []model.Finding
	for _, ref := range strings.Fields(refs) {
		s := surfaceOf(ref)
		hit[s] = true
		out = append(out, finding(sha, s, ref, model.Confirmed, "ref contains commit"))
	}
	for _, s := range refSurfaces {
		if !hit[s] {
			out = append(out, finding(sha, s, "*", model.NotFound, "no ref of this kind contains commit"))
		}
	}
	return out
}

// surfaceOf maps a ref to a surface. Refs that are neither tags nor pull refs
// (heads and any host-specific namespace) count as non-PR branch refs, which is
// what Support's precondition cares about.
func surfaceOf(ref string) model.Surface {
	switch {
	case strings.HasPrefix(ref, "refs/pull/"):
		return model.SurfacePRRef
	case strings.HasPrefix(ref, "refs/tags/"):
		return model.SurfaceTag
	default:
		return model.SurfaceBranch
	}
}

func (p Mirror) failed(commits []model.OldCommit, ev string) Result {
	res := Result{Failed: true}
	for _, c := range commits {
		for _, s := range refSurfaces {
			res.Findings = append(res.Findings, finding(c.SHA, s, "*", model.Unknown, ev))
		}
	}
	for _, s := range refSurfaces {
		res.Surfaces = append(res.Surfaces, model.SurfaceCoverage{Surface: s, Complete: false})
	}
	return res
}

// gitEnv writes a GIT_ASKPASS helper so the token never appears on a command
// line or in the remote URL. The helper reads it from the environment.
func (p Mirror) gitEnv(dir string) ([]string, error) {
	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if p.Token == "" {
		return env, nil
	}
	script := filepath.Join(dir, "askpass.sh")
	body := "#!/bin/sh\ncase \"$1\" in\n*sername*) echo x-access-token ;;\n*) echo \"$GH_RESIDUE_TOKEN\" ;;\nesac\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		return nil, fmt.Errorf("askpass: %w", err)
	}
	return append(env, "GIT_ASKPASS="+script, "GH_RESIDUE_TOKEN="+p.Token), nil
}

type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string { return fmt.Sprintf("exit %d: %s", e.code, e.msg) }

func (p Mirror) git(ctx context.Context, env []string, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = env
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if p.Token != "" {
			msg = strings.ReplaceAll(msg, p.Token, "***")
		}
		var x *exec.ExitError
		if errors.As(err, &x) {
			return "", &exitError{code: x.ExitCode(), msg: msg}
		}
		return "", fmt.Errorf("%v: %s", err, msg)
	}
	return out.String(), nil
}
