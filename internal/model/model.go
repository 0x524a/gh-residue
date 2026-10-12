// Package model holds the data types shared by inputs, probes and reporters.
package model

import "sort"

const SchemaVersion = 1

// Status is the outcome of one check. Only a probe that positively completed a
// check may return NotFound; every failure path must use Unknown (ADR-0002).
type Status string

const (
	Confirmed Status = "confirmed"
	NotFound  Status = "not_found"
	Unknown   Status = "unknown"
)

type Surface string

const (
	SurfaceBaseObject Surface = "base_object"
	SurfaceBranch     Surface = "branch"
	SurfacePRRef      Surface = "pr_ref"
	SurfaceTag        Surface = "tag"
	SurfaceForkObject Surface = "fork_object"
	SurfaceForkRef    Surface = "fork_ref"
	SurfaceWebView    Surface = "web_view"
)

// Enumerated reports whether a probe lists every ref holding a SHA on this
// surface (so an absent ref is genuinely gone). Other surfaces are checked at
// fixed locations (a repo or fork name): absent there means "not checked".
func (s Surface) Enumerated() bool {
	return s == SurfaceBranch || s == SurfacePRRef || s == SurfaceTag
}

type OldCommit struct {
	SHA    string `json:"old_sha"`
	NewSHA string `json:"new_sha,omitempty"`
}

type Finding struct {
	OldSHA    string  `json:"old_sha"`
	Surface   Surface `json:"surface"`
	Location  string  `json:"location"`
	Status    Status  `json:"status"`
	Evidence  string  `json:"evidence"`
	CheckedAt string  `json:"checked_at"`
}

type SurfaceCoverage struct {
	Surface  Surface `json:"surface"`
	Complete bool    `json:"complete"`
}

type Coverage struct {
	ProbesRun    int `json:"probes_run"`
	ProbesFailed int `json:"probes_failed"`
	ForksTotal   int `json:"forks_total"`
	ForksChecked int `json:"forks_checked"`
	// Why fork coverage cannot be called complete even when every listed fork was
	// checked (truncated listing, forks of forks). Non-empty means incomplete.
	ForksIncompleteReason string `json:"forks_incomplete_reason,omitempty"`
	// Commits not requested per fork: covered through the first changed commits.
	ForkCommitsSkipped int               `json:"fork_commits_skipped,omitempty"`
	Surfaces           []SurfaceCoverage `json:"surfaces"`
}

type Inputs struct {
	Source string `json:"source"`
	Count  int    `json:"count"`
}

type Run struct {
	SchemaVersion int       `json:"schema_version"`
	ToolVersion   string    `json:"tool_version"`
	Repo          string    `json:"repo"`
	StartedAt     string    `json:"started_at"`
	Inputs        Inputs    `json:"inputs"`
	Coverage      Coverage  `json:"coverage"`
	Findings      []Finding `json:"findings"`
	// Set only when resumed from a checkpoint: results carried over unchanged
	// (keeping their original checked_at) and the start time of the source run.
	Carried     int    `json:"carried,omitempty"`
	ResumedFrom string `json:"resumed_from,omitempty"`
}

// Sort puts findings in a deterministic order so two runs diff cleanly.
func (r *Run) Sort() {
	sort.SliceStable(r.Findings, func(i, j int) bool {
		a, b := r.Findings[i], r.Findings[j]
		if a.OldSHA != b.OldSHA {
			return a.OldSHA < b.OldSHA
		}
		if a.Surface != b.Surface {
			return a.Surface < b.Surface
		}
		return a.Location < b.Location
	})
	sort.SliceStable(r.Coverage.Surfaces, func(i, j int) bool {
		return r.Coverage.Surfaces[i].Surface < r.Coverage.Surfaces[j].Surface
	})
}

// Incomplete reports whether any check did not finish: a failed probe, sampled
// forks, an incomplete surface, or any finding that is not confirmed/not_found.
// Confirmed exposures do not hide incompleteness.
func (r *Run) Incomplete() bool {
	if r.Coverage.ProbesFailed > 0 || r.Coverage.ForksChecked < r.Coverage.ForksTotal || r.Coverage.ForksIncompleteReason != "" {
		return true
	}
	for _, s := range r.Coverage.Surfaces {
		if !s.Complete {
			return true
		}
	}
	for _, f := range r.Findings {
		if f.Status != Confirmed && f.Status != NotFound { // Unknown or unrecognised is never clean.
			return true
		}
	}
	return false
}

// ExitCode: 1 if anything is confirmed (an exposure is proven even if other
// checks are incomplete); 2 if incomplete or empty; 0 only when every check
// completed and nothing was found.
func (r *Run) ExitCode() int {
	for _, f := range r.Findings {
		if f.Status == Confirmed {
			return 1
		}
	}
	if r.Incomplete() || len(r.Findings) == 0 {
		return 2
	}
	return 0
}
