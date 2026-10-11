package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/0x524a/gh-residue/internal/model"
)

type DiffClass string

const (
	Resolved     DiffClass = "resolved"
	StillExposed DiffClass = "still-exposed"
	New          DiffClass = "new"
	NowUnknown   DiffClass = "now-unknown"
)

type DiffEntry struct {
	OldSHA   string
	Surface  model.Surface
	Location string
	Class    DiffClass
}

type key struct {
	sha      string
	surface  model.Surface
	location string
}

// LoadRun reads a Run previously written with --json.
func LoadRun(r io.Reader) (*model.Run, error) {
	var run model.Run
	if err := json.NewDecoder(r).Decode(&run); err != nil {
		return nil, err
	}
	if run.SchemaVersion != model.SchemaVersion {
		return nil, fmt.Errorf("schema_version %d, this build reads %d", run.SchemaVersion, model.SchemaVersion)
	}
	return &run, nil
}

// Diff classifies each previously confirmed or newly confirmed (SHA, surface,
// location). Unchanged clean results are omitted. Anything the new run could not
// establish is now-unknown, never resolved.
func Diff(prev, cur *model.Run) []DiffEntry {
	pm, cm := map[key]model.Status{}, map[key]model.Status{}
	for _, f := range prev.Findings {
		pm[key{f.OldSHA, f.Surface, f.Location}] = f.Status
	}
	for _, f := range cur.Findings {
		cm[key{f.OldSHA, f.Surface, f.Location}] = f.Status
	}
	complete := map[model.Surface]bool{}
	for _, s := range cur.Coverage.Surfaces {
		complete[s.Surface] = s.Complete
	}
	// A surface-wide unknown ("*") in the new run taints every previous location on it.
	wildUnknown := func(k key) bool {
		st, ok := cm[key{k.sha, k.surface, "*"}]
		return ok && st != model.NotFound && st != model.Confirmed
	}

	var out []DiffEntry
	add := func(k key, c DiffClass) { out = append(out, DiffEntry{k.sha, k.surface, k.location, c}) }

	for k, ps := range pm {
		if k.location == "*" {
			continue
		}
		cs, present := cm[k]
		switch {
		case ps == model.Confirmed && present && cs == model.Confirmed:
			add(k, StillExposed)
		case ps == model.Confirmed && present && cs == model.NotFound:
			add(k, Resolved)
		case ps == model.Confirmed && present:
			add(k, NowUnknown)
		case ps == model.Confirmed: // absent from the new run
			if k.surface.Enumerated() && complete[k.surface] && !wildUnknown(k) {
				add(k, Resolved)
			} else {
				add(k, NowUnknown)
			}
		case ps == model.NotFound && present && cs != model.NotFound && cs != model.Confirmed:
			add(k, NowUnknown)
		}
	}
	for k, cs := range cm {
		if k.location == "*" {
			if cs != model.NotFound && cs != model.Confirmed && hadKnown(pm, k) {
				// surface-wide failure on a surface that previously had known results
				add(k, NowUnknown)
			}
			continue
		}
		if cs == model.Confirmed && pm[k] != model.Confirmed {
			add(k, New)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Class != b.Class {
			return rank(a.Class) < rank(b.Class)
		}
		if a.OldSHA != b.OldSHA {
			return a.OldSHA < b.OldSHA
		}
		if a.Surface != b.Surface {
			return a.Surface < b.Surface
		}
		return a.Location < b.Location
	})
	return out
}

func hadKnown(pm map[key]model.Status, k key) bool {
	for pk, st := range pm {
		if pk.sha == k.sha && pk.surface == k.surface && (st == model.Confirmed || st == model.NotFound) {
			return true
		}
	}
	return false
}

// rank orders loudest first: regressions in knowledge, then new exposure.
func rank(c DiffClass) int {
	switch c {
	case NowUnknown:
		return 0
	case New:
		return 1
	case StillExposed:
		return 2
	default:
		return 3
	}
}

func WriteDiff(w io.Writer, entries []DiffEntry) {
	fmt.Fprintln(w, "Diff against previous run:")
	if len(entries) == 0 {
		fmt.Fprintln(w, "  no changes")
		return
	}
	counts := map[DiffClass]int{}
	shown := map[DiffClass]int{}
	for _, e := range entries {
		counts[e.Class]++
		if shown[e.Class] >= maxRowsPerGroup {
			continue
		}
		shown[e.Class]++
		tag := string(e.Class)
		if e.Class == NowUnknown {
			tag = "!! now-unknown"
		}
		fmt.Fprintf(w, "  %-15s %s %-11s %s\n", tag, e.OldSHA, e.Surface, e.Location)
	}
	for _, c := range []DiffClass{NowUnknown, New, StillExposed, Resolved} {
		if counts[c] > shown[c] {
			fmt.Fprintf(w, "  %-15s ... +%d more %s\n", "", counts[c]-shown[c], c)
		}
	}
	fmt.Fprintf(w, "  totals: %d resolved, %d still-exposed, %d new, %d now-unknown\n",
		counts[Resolved], counts[StillExposed], counts[New], counts[NowUnknown])
	if counts[NowUnknown] > 0 {
		fmt.Fprintln(w, "  WARNING: some previously known results could not be re-established. Do not read them as resolved.")
	}
}
