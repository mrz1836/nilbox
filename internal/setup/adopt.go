package setup

import (
	"context"
	"fmt"

	"github.com/mrz1836/nilbox/internal/prefs/prefval"
)

// Viewer reads the account the inbox last signed in with, "" before its first refresh:
// *inbox.Store.
type Viewer interface {
	ViewerLogin(ctx context.Context) (string, error)
}

// Adopt decides, once per database, whether the setup shows, and returns the state. While
// the setup has stored nothing, as on the first start of a build that has it, it stores
// the setup done when the inbox ever signed in, which only an install from before the setup
// has (a failed refresh never clears the account), and not done otherwise, as on a new
// install, after a reset, or in a sandbox. Once decided it never decides again, so a user
// who signs in outside the app and quits on Welcome still sees the setup at the next start.
// Core calls it once, at startup, before anything refreshes; it is never bound.
func Adopt(ctx context.Context, memory Memory, viewer Viewer) (State, error) {
	stored, err := memory.GetMany(ctx, KeyDone)
	if err != nil {
		return State{}, fmt.Errorf("read the setup's state: %w", err)
	}

	if _, decided := stored[KeyDone]; decided {
		return readState(ctx, memory)
	}

	login, err := viewer.ViewerLogin(ctx)
	if err != nil {
		return State{}, fmt.Errorf("decide whether the setup shows: %w", err)
	}

	if err = memory.SetMany(ctx, map[string]string{KeyDone: prefval.EncodeBool(login != "")}); err != nil {
		return State{}, fmt.Errorf("decide whether the setup shows: %w", err)
	}

	return readState(ctx, memory)
}
