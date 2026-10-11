package probe

import "github.com/0x524a/gh-residue/internal/model"

type knownKey struct {
	sha      string
	surface  model.Surface
	location string
}

// Known holds results from a checkpoint that need not be re-checked. Only
// confirmed and not_found results ever enter it: an unknown must be retried.
// A nil Known means "nothing known" and is safe to use.
type Known struct {
	m map[knownKey]model.Finding
}

func NewKnown(prev []model.Finding, shas []model.OldCommit) *Known {
	want := map[string]bool{}
	for _, c := range shas {
		want[c.SHA] = true
	}
	k := &Known{m: map[knownKey]model.Finding{}}
	for _, f := range prev {
		if !want[f.OldSHA] || (f.Status != model.Confirmed && f.Status != model.NotFound) {
			continue
		}
		k.m[knownKey{f.OldSHA, f.Surface, f.Location}] = f
	}
	return k
}

// Has reports a known result for exactly this (sha, surface, location).
func (k *Known) Has(sha string, s model.Surface, location string) bool {
	if k == nil {
		return false
	}
	_, ok := k.m[knownKey{sha, s, location}]
	return ok
}

// SurfaceKnown reports any known result for (sha, surface), at any location.
// Enumerated surfaces always carry at least one row once checked (a "*" row
// when nothing was found), so this means that surface was fully checked.
func (k *Known) SurfaceKnown(sha string, s model.Surface) bool {
	if k == nil {
		return false
	}
	for key := range k.m {
		if key.sha == sha && key.surface == s {
			return true
		}
	}
	return false
}

// Findings returns the known results, for the caller to merge.
func (k *Known) Findings() []model.Finding {
	if k == nil {
		return nil
	}
	out := make([]model.Finding, 0, len(k.m))
	for _, f := range k.m {
		out = append(out, f)
	}
	return out
}

// Resumable is implemented by probes that can skip already-known checks.
type Resumable interface {
	Probe
	WithKnown(*Known) Probe
}
