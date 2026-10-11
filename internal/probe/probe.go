// Package probe holds one probe per exposure surface. Every failure path must
// yield model.Unknown, never model.NotFound (ADR-0002).
package probe

import (
	"context"
	"time"

	"github.com/0x524a/gh-residue/internal/model"
)

// Result is what one probe contributes to a Run.
type Result struct {
	Findings     []model.Finding
	Surfaces     []model.SurfaceCoverage
	ForksTotal   int
	ForksChecked int
	Failed       bool // the probe could not run at all
}

type Probe interface {
	Name() string
	Run(ctx context.Context, repo string, commits []model.OldCommit) Result
}

// Now is replaceable in tests.
var Now = func() time.Time { return time.Now().UTC() }

func finding(sha string, s model.Surface, loc string, st model.Status, ev string) model.Finding {
	return model.Finding{
		OldSHA: sha, Surface: s, Location: loc, Status: st, Evidence: ev,
		CheckedAt: Now().Format(time.RFC3339),
	}
}

// complete reports whether no finding on the given surface is unknown.
func complete(fs []model.Finding, s model.Surface) bool {
	for _, f := range fs {
		if f.Surface == s && f.Status != model.NotFound && f.Status != model.Confirmed {
			return false
		}
	}
	return true
}

// Skipped stands in for a probe the user disabled. It reports its surfaces as
// incomplete so a run with skipped probes can never exit 0 (ADR-0002).
type Skipped struct {
	Label    string
	Surfaces []model.Surface
}

func (s Skipped) Name() string { return "skipped:" + s.Label }

func (s Skipped) Run(context.Context, string, []model.OldCommit) Result {
	var res Result
	for _, sf := range s.Surfaces {
		res.Surfaces = append(res.Surfaces, model.SurfaceCoverage{Surface: sf, Complete: false})
	}
	return res
}
