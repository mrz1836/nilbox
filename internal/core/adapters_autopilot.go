package core

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/inbox"
	prefsstore "github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/prompts"
)

// autopilotDeps are what Autopilot's service is built over: the inbox's store, whose last
// refresh says the account signed in, the prompt library, which names the starters, and
// the copy of the routines kept beside the database.
type autopilotDeps struct {
	inbox   *inbox.Store
	prompts *prompts.Store
	file    *autopilot.File
}

// newAutopilot builds Autopilot's service over pool and deps.
func newAutopilot(pool *sql.DB, opts Options, deps autopilotDeps, logger *slog.Logger) *autopilot.Service {
	return autopilot.NewService(autopilot.Config{
		Pool: pool, Prefs: autopilotPrefs{prefs: prefsstore.New(pool, opts.Clock)}, Accounts: deps.inbox,
		Names: starterNames{prompts: deps.prompts}, File: deps.file, Clock: opts.Clock, Logger: logger,
	})
}

// autopilotPrefs reads Autopilot's settings from the prefs, which settings writes
// (autopilot.Prefs).
type autopilotPrefs struct {
	prefs *prefsstore.Store
}

// Autopilot returns Autopilot's settings as stored, each one missing or refused as its
// default.
func (p autopilotPrefs) Autopilot(ctx context.Context) (autopilotprefs.Config, error) {
	stored, err := p.prefs.GetMany(ctx, autopilotprefs.Keys()...)
	if err != nil {
		return autopilotprefs.Config{}, fmt.Errorf("read Autopilot's settings: %w", err)
	}

	return autopilotprefs.Decode(stored), nil
}

// starterNames names each starter prompt by the nickname of the user's copy of it, for a
// routine's sentence (autopilot.Names).
type starterNames struct {
	prompts *prompts.Store
}

// StarterNames returns each starter's key with the nickname of the user's copy of it; a
// starter the user deleted is left out.
func (n starterNames) StarterNames(ctx context.Context) (map[string]string, error) {
	all, err := n.prompts.All(ctx)
	if err != nil {
		return nil, err
	}

	out := map[string]string{}

	for _, p := range all {
		if p.StarterKey != "" {
			out[p.StarterKey] = p.Nickname
		}
	}

	return out, nil
}
