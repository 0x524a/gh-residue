// Package app wires inputs, probes and a Run together.
package app

import (
	"context"
	"sync"
	"time"

	"github.com/0x524a/gh-residue/internal/model"
	"github.com/0x524a/gh-residue/internal/probe"
)

// Verify runs every probe and aggregates the results. Probes are independent:
// one failing never discards another's findings.
//
// If prev is non-nil, its confirmed and not_found results for the current SHAs
// are carried over unchanged (keeping their checked_at) and only unknown or
// missing checks are re-run. Unknown results are never carried.
func Verify(ctx context.Context, repo, source, version string, commits []model.OldCommit, probes []probe.Probe, prev *model.Run) *model.Run {
	return VerifyWithSave(ctx, repo, source, version, commits, probes, prev, nil)
}

// VerifyWithSave is Verify, and also calls save with a partial run after each
// probe and every few dozen findings within one, so a checkpoint stays current
// during a long run. Partial runs are incomplete by construction; only their
// confirmed and not_found findings are ever carried over. save is never called
// concurrently.
func VerifyWithSave(ctx context.Context, repo, source, version string, commits []model.OldCommit, probes []probe.Probe, prev *model.Run, save func(*model.Run)) *model.Run {
	run := &model.Run{
		SchemaVersion: model.SchemaVersion,
		ToolVersion:   version,
		Repo:          repo,
		StartedAt:     time.Now().UTC().Format(time.RFC3339),
		Inputs:        model.Inputs{Source: source, Count: len(commits)},
		Findings:      []model.Finding{},
	}
	var known *probe.Known
	if prev != nil {
		known = probe.NewKnown(prev.Findings, commits)
	}
	var mu sync.Mutex
	snap := func(partial []model.Finding) {
		if save == nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		part := *run
		part.Findings = append(append([]model.Finding(nil), run.Findings...), partial...)
		part.Coverage.Surfaces = append([]model.SurfaceCoverage{}, run.Coverage.Surfaces...)
		if known != nil {
			part.Findings, _ = merge(part.Findings, known.Findings())
		}
		part.Sort()
		save(&part)
	}
	ctx = probe.WithSnapshot(ctx, snap)
	for _, p := range probes {
		if r, ok := p.(probe.Resumable); ok && known != nil {
			p = r.WithKnown(known)
		}
		res := p.Run(ctx, repo, commits)
		run.Coverage.ProbesRun++
		if res.Failed {
			run.Coverage.ProbesFailed++
		}
		run.Findings = append(run.Findings, res.Findings...)
		run.Coverage.Surfaces = append(run.Coverage.Surfaces, res.Surfaces...)
		run.Coverage.ForksTotal += res.ForksTotal
		run.Coverage.ForksChecked += res.ForksChecked
		run.Coverage.ForksHidden = max(run.Coverage.ForksHidden, res.ForksHidden)
		if len(res.HiddenForks) > 0 {
			run.Coverage.HiddenForks = res.HiddenForks
			run.Coverage.HiddenAccepted = res.HiddenAccepted
			run.Coverage.ForksReasonAccepted = res.ForksReasonAccepted
		}
		if res.ForkCommitsSkipped > run.Coverage.ForkCommitsSkipped {
			run.Coverage.ForkCommitsSkipped = res.ForkCommitsSkipped
		}
		if res.ForksIncompleteReason != "" {
			run.Coverage.ForksIncompleteReason = res.ForksIncompleteReason
		}
		snap(nil)
	}
	if known != nil {
		run.Findings, run.Carried = merge(run.Findings, known.Findings())
		if run.Carried > 0 {
			run.ResumedFrom = prev.StartedAt
		}
	}
	if run.Coverage.Surfaces == nil {
		run.Coverage.Surfaces = []model.SurfaceCoverage{}
	}
	run.Sort()
	return run
}

type shaSurface struct {
	sha     string
	surface model.Surface
}

type exact struct {
	sha      string
	surface  model.Surface
	location string
}

// merge adds carried findings unless a fresh finding covers the same ground:
// for enumerated ref surfaces that is any fresh finding for (sha, surface), so a
// re-checked SHA never keeps stale rows next to new ones; otherwise the exact key.
func merge(fresh, carried []model.Finding) ([]model.Finding, int) {
	covered := map[shaSurface]bool{}
	keys := map[exact]bool{}
	for _, f := range fresh {
		covered[shaSurface{f.OldSHA, f.Surface}] = true
		keys[exact{f.OldSHA, f.Surface, f.Location}] = true
	}
	n := 0
	for _, f := range carried {
		if f.Surface.Enumerated() && covered[shaSurface{f.OldSHA, f.Surface}] {
			continue
		}
		if keys[exact{f.OldSHA, f.Surface, f.Location}] {
			continue
		}
		fresh = append(fresh, f)
		n++
	}
	return fresh, n
}
