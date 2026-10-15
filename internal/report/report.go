// Package report renders a Run. Pure functions, no network.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/0x524a/gh-residue/internal/model"
)

// maxRowsPerGroup caps table rows per (SHA, status, surface); JSON keeps all.
const maxRowsPerGroup = 5

func JSON(w io.Writer, r *model.Run) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// Table prints one row per (SHA, surface, location), grouped by SHA, a summary
// line, and a footer that always says to rotate first.
func Table(w io.Writer, r *model.Run) {
	fmt.Fprintf(w, "gh-residue: %s  (%d old SHAs from %s)\n\n", r.Repo, r.Inputs.Count, r.Inputs.Source)

	last := ""
	var runKey string
	runLen := 0
	flush := func() {
		if runLen > maxRowsPerGroup {
			fmt.Fprintf(w, "  %s ... +%d more (full list in --json)\n", strings.Repeat(" ", 12), runLen-maxRowsPerGroup)
		}
		runLen = 0
	}
	for _, f := range r.Findings {
		if f.OldSHA != last {
			flush()
			if last != "" {
				fmt.Fprintln(w)
			}
			fmt.Fprintf(w, "%s\n", f.OldSHA)
			last, runKey = f.OldSHA, ""
		}
		key := string(f.Status) + "/" + string(f.Surface)
		if key != runKey {
			flush()
			runKey = key
		}
		runLen++
		if runLen <= maxRowsPerGroup {
			fmt.Fprintf(w, "  %-12s %-11s %-28s %s\n", f.Status, f.Surface, f.Location, f.Evidence)
		}
	}
	flush()
	if last != "" {
		fmt.Fprintln(w)
	}

	if r.Carried > 0 {
		fmt.Fprintf(w, "Resumed from checkpoint: %s carried over unchanged (oldest checked %s); the rest were re-checked.\n", plural(r.Carried, "result", "results"), oldestCheck(r))
	}
	fmt.Fprintln(w, summary(r))
	if r.Coverage.ForksTotal > 0 || r.Coverage.ForksIncompleteReason != "" {
		what := "object-resolvable only; ref-holding not probed"
		if hasSurface(r, model.SurfaceForkRef) {
			what = "object-resolvable and ref-holding"
		}
		fmt.Fprintf(w, "Fork coverage: checked %d of %d forks (%s).\n",
			r.Coverage.ForksChecked, r.Coverage.ForksTotal, what)
		if r.Coverage.ForksIncompleteReason != "" {
			fmt.Fprintf(w, "Fork coverage is incomplete: %s.\n", r.Coverage.ForksIncompleteReason)
		}
		if n := r.Coverage.ForksHidden; n > 0 {
			fmt.Fprintf(w, "%s listed but hidden by GitHub (the repository and its owner both answer 404); only GitHub Support can check them, so they count as unknown.\n", plural(n, "fork is", "forks are"))
		}
		if n := r.Coverage.ForkCommitsSkipped; n > 0 {
			fmt.Fprintf(w, "Forks were checked for the first changed commits only; %s not checked one by one. A ref that holds any rewritten commit also holds a first changed commit.\n", plural(n, "other commit was", "other commits were"))
		}
	}
	var inc []string
	for _, s := range r.Coverage.Surfaces {
		if !s.Complete {
			inc = append(inc, string(s.Surface))
		}
	}
	if len(inc) > 0 {
		fmt.Fprintf(w, "Incomplete surfaces: %s. Treat as NOT clean.\n", strings.Join(inc, ", "))
	}
	notProbed := "web commit view (opt-in), ref-holding forks (--fork-refs), local clones, CI caches, third-party mirrors, GH Archive"
	if hasSurface(r, model.SurfaceForkRef) {
		notProbed = "web commit view (opt-in), local clones, CI caches, third-party mirrors, GH Archive"
	}
	fmt.Fprintf(w, "Not probed: %s.\n", notProbed)
	fmt.Fprintln(w, "Rotate the credential first. This tool only verifies; only GitHub Support can purge cached views and PR refs.")
}

func summary(r *model.Run) string {
	exposed := map[string]bool{}
	byKind := map[model.Surface]int{}
	unknown := 0
	for _, f := range r.Findings {
		switch f.Status {
		case model.Confirmed:
			exposed[f.OldSHA] = true
			byKind[f.Surface]++
		case model.NotFound:
		default:
			unknown++
		}
	}
	shas := map[string]bool{}
	for _, f := range r.Findings {
		shas[f.OldSHA] = true
	}
	var parts []string
	kinds := make([]string, 0, len(byKind))
	for k := range byKind {
		kinds = append(kinds, string(k))
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		parts = append(parts, fmt.Sprintf("%d via %s", byKind[model.Surface(k)], k))
	}
	s := fmt.Sprintf("%d SHAs, %d still exposed", len(shas), len(exposed))
	if len(parts) > 0 {
		s += " (" + strings.Join(parts, ", ") + ")"
	}
	if unknown > 0 {
		s += "; " + plural(unknown, "check", "checks") + " unknown"
	}
	return s + fmt.Sprintf(". Exit code %d.", r.ExitCode())
}

func oldestCheck(r *model.Run) string {
	oldest := ""
	for _, f := range r.Findings {
		if f.CheckedAt != "" && (oldest == "" || f.CheckedAt < oldest) { // RFC3339 UTC sorts lexically
			oldest = f.CheckedAt
		}
	}
	return oldest
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func hasSurface(r *model.Run, s model.Surface) bool {
	for _, c := range r.Coverage.Surfaces {
		if c.Surface == s {
			return true
		}
	}
	return false
}
