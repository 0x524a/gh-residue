package probe

import (
	"context"

	"github.com/0x524a/gh-residue/internal/model"
)

// Snapshot receives a copy of the findings a probe has produced so far. The app
// uses it to keep a checkpoint current during a long run, so a killed process
// loses minutes, not the whole run. Probes that call it must not hold their own
// locks across the call.
type Snapshot func(partial []model.Finding)

type snapshotKey struct{}

// snapshotEvery is how many findings pass between snapshots.
const snapshotEvery = 50

func WithSnapshot(ctx context.Context, fn Snapshot) context.Context {
	return context.WithValue(ctx, snapshotKey{}, fn)
}

// snapshot reports progress if the context carries a Snapshot and n lands on the interval.
func snapshot(ctx context.Context, fs []model.Finding) {
	if len(fs) == 0 || len(fs)%snapshotEvery != 0 {
		return
	}
	if fn, ok := ctx.Value(snapshotKey{}).(Snapshot); ok {
		fn(append([]model.Finding(nil), fs...))
	}
}
