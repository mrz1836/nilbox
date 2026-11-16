// Package autopilot keeps the user's Autopilot routines and serves them to the window:
// each routine's definition (its spec, see the spec package), its name and description,
// whether it is on, its mode (Dry run, Ask first, or Autopilot), and the GitHub account it
// belongs to, in the autopilot_routines table it owns.
//
// A routine's spec is checked strictly on every save and every load. One that won't load,
// as one a newer build wrote, is broken, not fatal: the list carries it with why, it
// can't be switched on, and nothing here rewrites it. A routine whose steps include a kind
// this build can't run yet (the catalog's Offered) is saved in Dry run, and can't leave
// it. A routine is deleted softly, so the history that names it keeps its name.
//
// The routines outlive the database, as the prompt library does: after every change the
// copy beside the database (File, autopilot.json, in the export format, see Bundle) is
// written again, and Restore brings them back, switched off, into a table that is new. A
// backup of the user's setup holds them through BackupPart, a type of its own, so the
// window, which binds the Service, never reaches it.
//
// The package reaches the rest of the app only through the narrow interfaces it declares
// (Prefs, Accounts, Names), which core adapts; it never imports the inbox, the sheets,
// the launcher, the worktrees, the sessions, the runs, the notifications, the settings,
// the desktop shell, or core.
package autopilot

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/logging"
)

var (
	// ErrNotSetUp is returned by a Service with no database.
	ErrNotSetUp = errors.New("routines are not set up")

	// ErrNotFound is returned for a routine that is gone or deleted.
	ErrNotFound = errors.New("no such routine")

	// ErrNameTaken is returned for a name another routine has, ignoring case.
	ErrNameTaken = errors.New("another routine has that name")

	// ErrInvalidName is returned for a name that is empty, too long, or not one line.
	ErrInvalidName = errors.New("invalid routine name")

	// ErrInvalidDescription is returned for a description that is too long or holds a
	// control character.
	ErrInvalidDescription = errors.New("invalid routine description")

	// ErrInvalidTemplate is returned for a template key that isn't one.
	ErrInvalidTemplate = errors.New("invalid template key")

	// ErrBroken is returned for a change a broken routine can't take: switching it on,
	// choosing its mode, or saving over a spec a newer build wrote.
	ErrBroken = errors.New("the routine is broken")

	// ErrDryRunOnly is returned for a mode other than Dry run for a routine with a step
	// this build can't run yet.
	ErrDryRunOnly = errors.New("the routine runs in Dry run only")
)

// Prefs reads Autopilot's settings, for the mode new routines start in. Core adapts the
// prefs, which settings writes.
type Prefs interface {
	Autopilot(ctx context.Context) (autopilotprefs.Config, error)
}

// Accounts says which GitHub account is signed in, the last refresh's, "" for none. A
// new routine belongs to it, and a routine of another account is marked. *inbox.Store
// implements it.
type Accounts interface {
	ViewerLogin(ctx context.Context) (string, error)
}

// Names names the starter prompts a routine's sentence names: each starter's key to the
// nickname of the user's copy of it. Core adapts the prompt library.
type Names interface {
	StarterNames(ctx context.Context) (map[string]string, error)
}

// Config holds a Service's dependencies. Pool is required; the rest may be nil.
type Config struct {
	// Pool is the database the routines are kept in.
	Pool *sql.DB

	// Prefs gives the mode new routines start in; nil starts them in the default mode.
	Prefs Prefs

	// Accounts gives the account signed in; nil is none.
	Accounts Accounts

	// Names names the starter prompts in the sentences; nil writes a starter's key as
	// words.
	Names Names

	// File is the copy of the routines kept beside the database, written again after
	// every change; nil keeps none.
	File *File

	// Clock stamps the routines. Nil is the system clock.
	Clock clock.Clock

	// Logger hears what a change couldn't do beside it, such as writing the copy. Nil
	// discards.
	Logger *slog.Logger
}

// Service keeps the routines, and is the window's face of Autopilot.
type Service struct {
	store    routineStore
	prefs    Prefs
	accounts Accounts
	names    Names
	file     *File
	clock    clock.Clock
	logger   *slog.Logger
}

// NewService returns a Service over cfg.
func NewService(cfg Config) *Service {
	clk := clock.Or(cfg.Clock)

	return &Service{
		store: newRoutineStore(cfg.Pool, clk), prefs: cfg.Prefs, accounts: cfg.Accounts, names: cfg.Names,
		file: cfg.File, clock: clk, logger: logging.Or(cfg.Logger),
	}
}

// Catalog returns everything the builder renders: the triggers, the fields and their
// operators, the steps (each Offered once this build runs it), the presets, the agent
// step's settings, and the limits.
func (s *Service) Catalog() catalog.Catalog {
	return catalog.All()
}
