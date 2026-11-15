// Package setup is the first-run setup's service: where the user is in it and whether it
// is done, and the checks its steps make that no other service makes for them: whether the
// GitHub CLI's sign-in gives the inbox what it needs, which terminals are installed, and
// how many of the user's clones use the tools the Tools step offers. It also starts a reset
// the user asked for, which the next start carries out.
//
// It keeps its state in the setup.* preferences, which it alone writes: setup.done and
// setup.step. They are the window's memory, so a backup never holds them. Adopt decides,
// once per database, whether the setup shows at all: an install from before it, whose inbox
// ever signed in, is adopted as done, and a fresh database, as a new install, a reset, or a
// sandbox has, shows it.
//
// It reaches every other part of the app through the narrow interfaces it declares, which
// core adapts: the GitHub sign-in through Account, the clones through Clones, the runs
// through Runs, and the backup saved before a reset through Saver.
package setup

import (
	"context"
	"log/slog"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/launcher/terminal"
	"github.com/mrz1836/nilbox/internal/logging"
)

// Memory is where the setup keeps its state, the setup.* preferences: a *prefs.Store.
type Memory interface {
	GetMany(ctx context.Context, keys ...string) (map[string]string, error)
	SetMany(ctx context.Context, values map[string]string) error
}

// Account reads who the GitHub CLI's token belongs to, and the scopes the token reports:
// core adapts the token source and the shared client (github.WithToken and FetchSignIn).
// Its errors are the token source's and the client's, which GitHub tells apart.
type Account interface {
	SignIn(ctx context.Context) (github.SignIn, error)
}

// CLI reads the GitHub CLI's version, as Settings → About shows it: core adapts appinfo's
// ToolVersions. installed is false when gh isn't found.
type CLI interface {
	GHVersion(ctx context.Context) (version string, installed bool)
}

// Clones lists the folders of the user's clones, in every repository location, with the
// locations that didn't answer in time skipped: core adapts the settings' locations and
// the shared localrepos.Reader.
type Clones interface {
	Clones(ctx context.Context) ([]string, error)
}

// Runs says whether runs are going in the app, which quitting for a reset would stop:
// *scriptrun.Engine.
type Runs interface {
	Active() bool
}

// Saver saves the user's whole setup before a reset, and returns where, with ~ for home:
// *backup.Saver.
type Saver interface {
	SaveBeforeReset(ctx context.Context) (string, error)
}

// Config holds a Service's dependencies. Memory is required; a check whose dependency is
// missing reads as having found nothing.
type Config struct {
	// Memory keeps the setup's state.
	Memory Memory

	// Account and CLI read the GitHub step's state.
	Account Account
	CLI     CLI

	// Terminals lists the terminals launches can open in, and whether each is installed
	// (core: the terminal registry's Installed, over /Applications and ~/Applications).
	Terminals func() []terminal.Choice

	// Clones lists the user's clones, whose markers Signals counts.
	Clones Clones

	// Runs holds a reset back while runs are going, and Saver saves the setup before one.
	Runs  Runs
	Saver Saver

	// ResetRequest is where Reset writes its request (appdata.Paths.ResetRequest).
	ResetRequest string

	// Clock stamps the reset request. Nil is the system clock.
	Clock clock.Clock

	// Events hears an event.SetupFinished when the setup ends, and an
	// event.ResetRequested once a reset is asked for.
	Events event.Sink

	// Logger hears what a check could not read. Nil discards.
	Logger *slog.Logger
}

// Service is the setup the window binds.
type Service struct {
	memory    Memory
	account   Account
	cli       CLI
	terminals func() []terminal.Choice
	clones    Clones
	runs      Runs
	saver     Saver
	request   string
	clock     clock.Clock
	events    event.Sink
	logger    *slog.Logger
}

// NewService returns a Service over cfg.
func NewService(cfg Config) *Service {
	return &Service{
		memory: cfg.Memory, account: cfg.Account, cli: cfg.CLI, terminals: cfg.Terminals, clones: cfg.Clones,
		runs: cfg.Runs, saver: cfg.Saver, request: cfg.ResetRequest, clock: clock.Or(cfg.Clock),
		events: event.Or(cfg.Events), logger: logging.Or(cfg.Logger),
	}
}
