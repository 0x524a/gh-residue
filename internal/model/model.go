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
	ForkCommitsSkipped int `json:"fork_commits_skipped,omitempty"`
	// Listed forks hidden by GitHub (repository and owner both 404); never counted as clean.
	ForksHidden int `json:"forks_hidden,omitempty"`
	// Their names, and whether the user accepted them for the Support readiness line
	// (--accept-hidden-forks). Acceptance never changes the exit code.
	HiddenForks    []string `json:"hidden_forks,omitempty"`
	HiddenAccepted bool     `json:"hidden_forks_accepted,omitempty"`
	// The fork-coverage reason that remains once accepted hidden forks are set aside.
	ForksReasonAccepted string            `json:"forks_incomplete_reason_accepted,omitempty"`
	Surfaces            []SurfaceCoverage `json:"surfaces"`
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
func (r *Run) Incomplete() bool { return r.incomplete(false) }

// IncompleteForSupport is Incomplete, except that when the user accepted hidden
// forks their (unknown) fork findings do not count. Only the "Ready for Support?"
// line uses it; the exit code never does.
func (r *Run) IncompleteForSupport() bool { return r.incomplete(r.Coverage.HiddenAccepted) }

func (r *Run) incomplete(skipHidden bool) bool {
	hidden := map[string]bool{}
	if skipHidden {
		for _, h := range r.Coverage.HiddenForks {
			hidden[h] = true
		}
	}
	reason := r.Coverage.ForksIncompleteReason
	if skipHidden {
		reason = r.Coverage.ForksReasonAccepted
	}
	if r.Coverage.ProbesFailed > 0 || r.Coverage.ForksChecked < r.Coverage.ForksTotal || reason != "" {
		return true
	}
	for _, s := range r.Coverage.Surfaces {
		if !s.Complete {
			return true
		}
	}
	for _, f := range r.Findings {
		if hidden[f.Location] && (f.Surface == SurfaceForkObject || f.Surface == SurfaceForkRef) {
			continue
		}
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
