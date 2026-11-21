package autopilot

import (
	"context"
	"fmt"
	"time"

	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// KeyEngine is the preference that holds the engine's own state, as JSON: when Pause
// began, the last cycle and the last successful refresh it saw, the fingerprints its first
// passes ran under, the routines waiting to take what matches now as their baseline, and
// the problems it found. Only the engine writes it, and only while it holds the lock.
const KeyEngine = "autopilot.engine"

// engineState is what KeyEngine holds.
type engineState struct {
	// PausedSince is when the engine first saw Pause Autopilot on, nil while it is off.
	PausedSince *time.Time `json:"pausedSince,omitempty"`

	// LastRefresh is the last successful refresh of the inbox the engine saw, so a start
	// after a long gap asks before it catches up.
	LastRefresh time.Time `json:"lastRefresh,omitzero"`

	// CatchUpSince is when the last catch-up began: the first refresh after a long gap, the
	// first cycle that found the last refresh long ago, or a Resume after a long pause; nil
	// with none yet. It is kept once the catch-up ends, so one gap begins one catch-up.
	CatchUpSince *time.Time `json:"catchUpSince,omitempty"`

	// CatchingUp holds the routines still in that catch-up: each waits for the user's OK
	// before its agent and GitHub steps until what it watches was read after it began.
	CatchingUp map[int64]bool `json:"catchingUp,omitempty"`

	// LastCycle is what the last cycle did.
	LastCycle *Cycle `json:"lastCycle,omitempty"`

	// FirstPass holds, by routine, the fingerprint its first pass ran under (its spec,
	// when it was switched on, and its mode): a routine with a GitHub action that has
	// another queues at most FirstCap on its next cycle.
	FirstPass map[int64]string `json:"firstPass,omitempty"`

	// FromNow holds the routines whose current matches become their baseline once their
	// searches were read after Since: the user chose "only what changes from now on".
	FromNow map[int64]fromNow `json:"fromNow,omitempty"`

	// Problems are the routines' problems as last found.
	Problems []Problem `json:"problems,omitempty"`

	// LastPrune is when the retention last ran.
	LastPrune time.Time `json:"lastPrune,omitzero"`

	// Resting holds the agents' profiles out of usage, by agent and profile
	// ("claude/work"), each until its limit resets.
	Resting map[string]Resting `json:"resting,omitempty"`
}

// fromNow is a routine waiting to record what matches now as its baseline.
type fromNow struct {
	Since  time.Time `json:"since"`
	DryRun bool      `json:"dryRun,omitempty"`
}

// readEngineState reads the engine's state; one never written, or that can't be read, is
// empty.
func readEngineState(ctx context.Context, p *prefs.Store) (engineState, error) {
	raw, ok, err := p.Get(ctx, KeyEngine)
	if err != nil {
		return engineState{}, fmt.Errorf("read the engine's state: %w", err)
	}

	empty := engineState{FirstPass: map[int64]string{}, FromNow: map[int64]fromNow{}, CatchingUp: map[int64]bool{}}
	if !ok || raw == "" {
		return empty, nil
	}

	st, err := store.DecodeJSON[engineState](raw)
	if err != nil {
		return empty, nil //nolint:nilerr // a state that can't be read is started again
	}

	st.FirstPass = textutil.NonNilMap(st.FirstPass)
	st.FromNow = textutil.NonNilMap(st.FromNow)
	st.CatchingUp = textutil.NonNilMap(st.CatchingUp)

	return st, nil
}

// writeEngineState keeps st.
func writeEngineState(ctx context.Context, p *prefs.Store, st engineState) error {
	raw, err := store.EncodeJSON(st)
	if err != nil {
		return err
	}

	if err = p.Set(ctx, KeyEngine, raw); err != nil {
		return fmt.Errorf("save the engine's state: %w", err)
	}

	return nil
}
