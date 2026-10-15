// Command gh-residue verifies where old commits are still reachable on GitHub
// after a history rewrite. Read-only: it never purges, rotates or rewrites.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	"github.com/0x524a/gh-residue/internal/app"
	"github.com/0x524a/gh-residue/internal/ghapi"
	"github.com/0x524a/gh-residue/internal/input"
	"github.com/0x524a/gh-residue/internal/model"
	"github.com/0x524a/gh-residue/internal/probe"
	"github.com/0x524a/gh-residue/internal/report"
)

// version is the release line; bump it with each tag. The commit is appended at
// runtime from Go's VCS build info, so no -ldflags are needed (the extension
// release action only takes plain go build options).
const baseVersion = "0.2.1"

var version = buildVersion(baseVersion)

func buildVersion(base string) string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return base
	}
	rev, dirty := "", false
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return base
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	if dirty {
		rev += "-dirty"
	}
	return base + "+" + rev
}

var repoRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

const estimateThreshold = 1000

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && (args[0] == "--version" || args[0] == "version") {
		fmt.Fprintln(stdout, "gh-residue", version)
		return 0
	}
	if len(args) == 0 || args[0] != "verify" {
		fmt.Fprintln(stderr, "usage: gh-residue [--version] verify OWNER/REPO --input PATH [--json] [--max-forks N] [--concurrency N] [--no-forks] [--fork-refs] [--no-mirror] [--yes]\n       [--support-draft FILE] [--first-changed SHA,...] [--lfs-file NAME] [--against PREV.json]\n       [--checkpoint FILE] [--checkpoint-max-age 24h]")
		return 2
	}
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	fs.SetOutput(stderr)
	inPath := fs.String("input", "", "filter-repo working dir, or file with one full 40-char SHA per line")
	asJSON := fs.Bool("json", false, "print JSON instead of the table")
	maxForks := fs.Int("max-forks", 200, "forks to probe (sampled by recent push, then stars)")
	concurrency := fs.Int("concurrency", 1, "parallel fork requests; GitHub advises serial (1) to avoid secondary rate limits")
	forkRefs := fs.Bool("fork-refs", false, "also check whether any branch or tag in each sampled fork contains an old commit (needs a token; GraphQL)")
	noForks := fs.Bool("no-forks", false, "skip the fork probe")
	noMirror := fs.Bool("no-mirror", false, "skip the mirror-clone probe (PR refs, branches, tags)")
	yes := fs.Bool("yes", false, "proceed past the request-estimate threshold")
	draftPath := fs.String("support-draft", "", "write a Support request draft (Markdown, mode 0600) to FILE; never sent")
	firstChanged := fs.String("first-changed", "", "comma-separated 'First Changed Commit(s)' from git-filter-repo output")
	lfsFile := fs.String("lfs-file", "", "name of the LFS-orphans file filter-repo reported, if any")
	against := fs.String("against", "", "previous --json run to diff against (diff goes to stdout, or stderr with --json)")
	checkpoint := fs.String("checkpoint", "", "resume from FILE if it exists (re-running only unknown checks) and always write the result there")
	maxAge := fs.Duration("checkpoint-max-age", 24*time.Hour, "refuse to carry over results older than this")
	if len(args) < 2 {
		fmt.Fprintln(stderr, "missing OWNER/REPO")
		return 2
	}
	repo := args[1]
	if err := fs.Parse(args[2:]); err != nil {
		return 2
	}
	if !repoRe.MatchString(repo) {
		fmt.Fprintf(stderr, "invalid repo %q, want OWNER/REPO\n", repo)
		return 2
	}
	if *inPath == "" {
		fmt.Fprintln(stderr, "--input is required")
		return 2
	}
	var prev *model.Run
	if *against != "" {
		f, err := os.Open(*against)
		if err == nil {
			prev, err = report.LoadRun(f)
			f.Close()
		}
		if err != nil {
			fmt.Fprintln(stderr, "--against:", err)
			return 2
		}
	}
	var prevCheckpoint *model.Run
	if *checkpoint != "" {
		pc, err := loadCheckpoint(*checkpoint, repo, *maxAge, time.Now())
		if err != nil {
			fmt.Fprintln(stderr, "--checkpoint:", err)
			return 2
		}
		prevCheckpoint = pc
	}
	commits, source, err := input.Load(*inPath)
	if err != nil {
		fmt.Fprintln(stderr, "input:", err)
		return 2
	}
	if len(commits) == 0 {
		fmt.Fprintln(stderr, "input contains no rewritten commits; nothing to verify")
		return 2
	}

	token := ghapi.ResolveToken()
	if token == "" {
		fmt.Fprintln(stderr, "warning: no token (GH_TOKEN, GITHUB_TOKEN or `gh auth token`); public repos only, 60 requests/hour")
	}
	if *forkRefs && *noForks {
		fmt.Fprintln(stderr, "--fork-refs checks forks, so it cannot be combined with --no-forks")
		return 2
	}
	if *forkRefs && token == "" {
		fmt.Fprintln(stderr, "--fork-refs needs a token: GitHub's GraphQL API rejects anonymous requests (set GH_TOKEN or run `gh auth login`)")
		return 2
	}
	client := ghapi.New(token)

	draftFirst, draftSrc, err := firstChangedCommits(*inPath, source, *firstChanged)
	if err != nil {
		fmt.Fprintln(stderr, "warning: first-changed-commits:", err)
	}
	// Every rewritten commit descends from a first changed commit, so a fork ref
	// holding any of them holds one of these. Only filter-repo's own file for this
	// rewrite is trusted for that: a hand-typed --first-changed or a SHA list could
	// leave commits that descend from no listed anchor, which would then go unchecked.
	var anchors []string
	forkScope := commits
	if source == "filter-repo" {
		fileFirst, ferr := input.LoadFirstChanged(*inPath)
		if ferr != nil {
			fmt.Fprintln(stderr, "warning: first-changed-commits:", ferr, "(fork checks will cover every commit)")
		}
		if sub := anchorCommits(commits, fileFirst); sub != nil && len(sub) < len(commits) {
			forkScope = sub
			for _, c := range sub {
				anchors = append(anchors, c.SHA)
			}
			fmt.Fprintf(stderr, "note: forks are checked for the %d first changed commit(s) from filter-repo's file, not all %d rewritten commits\n", len(sub), len(commits))
		}
	}
	est, needClone := estimate(repo, commits, forkScope, prevCheckpoint, *maxForks, !*noMirror, !*noForks, *forkRefs)
	probes := []probe.Probe{probe.BaseObject{Client: client}}
	if !*noMirror {
		probes = append(probes, probe.Mirror{CloneURL: "https://github.com/" + repo + ".git", Token: token})
		if needClone {
			fmt.Fprintln(stderr, "note: the mirror probe clones the repository into a temporary directory")
		}
	} else {
		probes = append(probes, probe.Skipped{Label: "mirror", Surfaces: []model.Surface{model.SurfaceBranch, model.SurfacePRRef, model.SurfaceTag}})
	}
	if !*noForks {
		probes = append(probes, probe.Forks{Client: client, Cap: *maxForks, Concurrency: *concurrency, Refs: *forkRefs, Anchors: anchors})
	} else {
		probes = append(probes, probe.Skipped{Label: "forks", Surfaces: []model.Surface{model.SurfaceForkObject}})
	}
	if prevCheckpoint != nil {
		fmt.Fprintf(stderr, "estimated API requests: up to ~%d (results carried over from the checkpoint are not re-requested)\n", est)
	} else {
		fmt.Fprintf(stderr, "estimated API requests: up to ~%d\n", est)
	}
	if est > estimateThreshold && !*yes {
		fmt.Fprintf(stderr, "estimate exceeds %d; re-run with --yes to proceed\n", estimateThreshold)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	r := app.Verify(ctx, repo, source, version, commits, probes, prevCheckpoint)
	if *checkpoint != "" {
		// Written before any output so a closed pipe can't lose it. Interrupted or
		// rate-limited runs end in unknown results, which the next run re-checks.
		if err := writeCheckpoint(*checkpoint, r); err != nil {
			fmt.Fprintln(stderr, "--checkpoint:", err)
			return 2
		}
		fmt.Fprintf(stderr, "checkpoint written to %s (contains old SHAs: treat as sensitive)\n", *checkpoint)
	}
	if *asJSON {
		if err := report.JSON(stdout, r); err != nil {
			fmt.Fprintln(stderr, "output:", err)
			return 2
		}
	} else {
		report.Table(stdout, r)
	}
	if prev != nil {
		d := report.Diff(prev, r)
		if *asJSON {
			report.WriteDiff(stderr, d)
		} else {
			fmt.Fprintln(stdout)
			report.WriteDiff(stdout, d)
		}
	}
	if *draftPath != "" {
		opts := report.SupportOpts{LFSFile: *lfsFile, FirstChanged: draftFirst, Source: draftSrc}
		f, err := os.OpenFile(*draftPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			fmt.Fprintln(stderr, "support-draft:", err)
			return 2
		}
		report.SupportDraft(f, r, opts)
		if err := f.Close(); err != nil {
			fmt.Fprintln(stderr, "support-draft:", err)
			return 2
		}
		fmt.Fprintf(stderr, "support draft written to %s (contains old SHAs: treat as sensitive)\n", *draftPath)
	}
	return r.ExitCode()
}

// loadCheckpoint returns nil, nil when the file does not exist (a fresh run).
// It refuses a checkpoint for another repo, and one whose oldest carried result
// is older than maxAge: stale "not_found" must not be reused as current.
func loadCheckpoint(path, repo string, maxAge time.Duration, now time.Time) (*model.Run, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	run, err := report.LoadRun(f)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(run.Repo, repo) {
		return nil, fmt.Errorf("checkpoint is for %q, not %q", run.Repo, repo)
	}
	for _, fd := range run.Findings {
		if fd.Status != model.Confirmed && fd.Status != model.NotFound {
			continue
		}
		t, err := time.Parse(time.RFC3339, fd.CheckedAt)
		if err != nil || now.Sub(t) > maxAge {
			return nil, fmt.Errorf("checkpoint holds results older than %s (checked_at %q); delete it to start fresh or raise --checkpoint-max-age", maxAge, fd.CheckedAt)
		}
	}
	return run, nil
}

// writeCheckpoint writes atomically (temp file in the same dir, then rename) with
// mode 0600 so a crash never leaves a truncated checkpoint.
func writeCheckpoint(path string, r *model.Run) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".gh-residue-checkpoint-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if err := report.JSON(tmp, r); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil { // CreateTemp already uses mode 0600
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// estimate returns an upper bound on API requests and whether a clone is needed.
// Results already known from a checkpoint are not counted: they are not re-requested.
func estimate(repo string, commits, forkCommits []model.OldCommit, prev *model.Run, maxForks int, mirror, forks, forkRefs bool) (requests int, needClone bool) {
	var known *probe.Known
	var forksDone, refsDone bool
	if prev != nil {
		known = probe.NewKnown(prev.Findings, commits)
		for _, s := range prev.Coverage.Surfaces {
			if s.Surface == model.SurfaceForkObject && s.Complete {
				forksDone = true
			}
			if s.Surface == model.SurfaceForkRef && s.Complete {
				refsDone = true
			}
		}
	}
	for _, c := range commits {
		if !known.Has(c.SHA, model.SurfaceBaseObject, repo) {
			requests++
		}
		for _, s := range []model.Surface{model.SurfaceBranch, model.SurfacePRRef, model.SurfaceTag} {
			if !known.SurfaceKnown(c.SHA, s) {
				needClone = true
			}
		}
	}
	if !mirror {
		needClone = false
	}
	if forks {
		if forksDone {
			requests += 10 // fork list pages only; every fork/SHA pair is already known
		} else {
			requests += len(forkCommits)*maxForks + 10
		}
		if forkRefs && !refsDone {
			// Upper bound: per fork, ceil(SHAs/20) GraphQL queries (more for >100 refs).
			requests += maxForks * ((len(forkCommits) + 19) / 20)
		}
	}
	return requests, needClone
}

// firstChangedCommits returns the first changed commits and where they came from:
// --first-changed wins, else filter-repo's own file. It never infers them.
func firstChangedCommits(inPath, source, flagValue string) ([]string, string, error) {
	if flagValue != "" {
		var out []string
		for _, c := range strings.Split(flagValue, ",") {
			if c = strings.TrimSpace(c); c != "" {
				out = append(out, c)
			}
		}
		return out, "passed with --first-changed", nil
	}
	if source != "filter-repo" {
		return nil, "", nil
	}
	fc, err := input.LoadFirstChanged(inPath)
	if err != nil || len(fc) == 0 {
		return nil, "", err
	}
	return fc, "read from filter-repo's first-changed-commits file", nil
}

// anchorCommits returns the input commits that are first changed commits, or nil
// unless every first changed commit is among them.
func anchorCommits(commits []model.OldCommit, first []string) []model.OldCommit {
	if len(first) == 0 {
		return nil
	}
	want := map[string]bool{}
	for _, f := range first {
		want[f] = true
	}
	var out []model.OldCommit
	for _, c := range commits {
		if want[c.SHA] {
			out = append(out, c)
		}
	}
	if len(out) != len(want) {
		return nil
	}
	return out
}
