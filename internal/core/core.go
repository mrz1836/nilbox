// Package core assembles the app: it prepares the data directory, opens and migrates the
// database, syncs the discovered agent profiles, and builds every service the window
// binds to.
//
// Core is plain Go with no desktop dependency, so the whole startup path is tested on any
// OS. The desktop shell only adds the window, binds Services, and runs the scheduler.
package core

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/appinfo"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/launcher"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/timing"
	"github.com/mrz1836/nilbox/internal/worktrees"
)

var (
	// ErrNoDataDir is returned when Options.Paths has no root directory.
	ErrNoDataDir = errors.New("the data directory is not set")

	// ErrNoHomeDir is returned when Options.Home is empty.
	ErrNoHomeDir = errors.New("the home directory is not set")
)

// Runner runs a program as an argv, never through a shell. execpath.Runner implements it;
// the GitHub token source, the clone scan, the terminal drivers, and the worktree service
// all run their programs through it.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error)
}

// Clock tells the time.
type Clock interface {
	Now() time.Time
}

// Options configure Bootstrap. Paths and Home are required; the rest are optional.
type Options struct {
	// Paths is where the database, launch files, and logs live.
	Paths appdata.Paths

	// Home is the user's home directory: profiles are discovered there, and a ~ clone
	// root expands to it.
	Home string

	// Version is the build's version; it is reported to the frontend and to GitHub as
	// part of the user agent.
	Version string

	// Clock stamps refreshes, launches, and worktree listings. It defaults to the system
	// clock.
	Clock Clock

	// Notifier is told when the stored inbox changed.
	Notifier inbox.Notifier

	// Runner runs external programs. It defaults to an execpath.Runner whose finder
	// searches the well-known directories under Home.
	Runner Runner

	// Logger receives scheduler and timing records. It defaults to slog.Default().
	Logger *slog.Logger

	// OnFirstPaint is called once when the frontend reports its first paint.
	OnFirstPaint func(timing.Measurement)
}

// Core is the assembled app: the database and every service.
type Core struct {
	// DB is the open, migrated database.
	DB *gorm.DB

	// The services bound to the frontend.
	Settings  *settings.Service
	Profiles  *profiles.Service
	Inbox     *inbox.Service
	Launcher  *launcher.Service
	Worktrees *worktrees.Service
	AppInfo   *appinfo.Service

	// Scheduler refreshes the inbox on start and every five minutes once Run is called.
	Scheduler *inbox.Scheduler

	// Timing records the first paint.
	Timing *timing.Recorder
}

// Bootstrap prepares the data directory, opens the database (backing it up and applying
// any pending migrations), syncs the agent profiles found under the home directory, and
// builds every service. It contacts no network; the scheduler does that once started.
func Bootstrap(ctx context.Context, opts Options) (*Core, error) {
	if opts.Paths.Root == "" {
		return nil, ErrNoDataDir
	}

	if opts.Home == "" {
		return nil, ErrNoHomeDir
	}

	if err := opts.Paths.Ensure(); err != nil {
		return nil, err
	}

	db, err := store.Open(ctx, opts.Paths.DB)
	if err != nil {
		return nil, err
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	if err = syncProfiles(ctx, db, opts.Home, logger); err != nil {
		_ = store.Close(db)

		return nil, err
	}

	return assemble(db, opts, logger), nil
}

// Services returns the services the frontend binds to, in a stable order.
func (c *Core) Services() []any {
	return []any{c.Settings, c.Profiles, c.Inbox, c.Launcher, c.Worktrees, c.AppInfo}
}

// Close closes the database.
func (c *Core) Close() error {
	return store.Close(c.DB)
}

// syncProfiles records every agent profile discovered under home. Existing rows are never
// changed or removed. A home directory that cannot be read only skips discovery, with a
// warning: the profiles already stored still work, and the inbox does not need them.
func syncProfiles(ctx context.Context, db *gorm.DB, home string, logger *slog.Logger) error {
	found, err := profiles.Discover(home)
	if err != nil {
		logger.WarnContext(ctx, "profile discovery skipped", "error", err)

		return nil
	}

	if err = profiles.Sync(ctx, db, found); err != nil {
		return fmt.Errorf("sync profiles: %w", err)
	}

	return nil
}

// assemble builds every service over the open database.
func assemble(db *gorm.DB, opts Options, logger *slog.Logger) *Core {
	finder := execpath.Finder{Home: opts.Home}

	runner := opts.Runner
	if runner == nil {
		runner = execpath.Runner{Finder: finder}
	}

	prefs := settings.NewService(db, opts.Home)

	inboxSvc := inbox.NewService(inbox.Config{
		DB:       db,
		Tokens:   github.NewTokenSource(runner, finder),
		Client:   github.NewClient(opts.Version),
		Clock:    opts.Clock,
		Notifier: opts.Notifier,
	})

	recorder := timing.NewRecorder(timing.Config{Logger: logger, LogsDir: opts.Paths.Logs})

	return &Core{
		DB:       db,
		Settings: prefs,
		Profiles: profiles.NewService(db),
		Inbox:    inboxSvc,
		Launcher: launcher.NewService(launcher.Config{
			DB:       db,
			Settings: prefs,
			Git:      runner,
			Finder:   finder,
			Registry: launcher.DefaultRegistry(runner),
			Paths:    opts.Paths,
			Home:     opts.Home,
			Clock:    opts.Clock,
		}),
		Worktrees: worktrees.NewService(worktrees.Config{
			Settings: prefs,
			Git:      runner,
			Finder:   finder,
			Home:     opts.Home,
			Clock:    opts.Clock,
		}),
		AppInfo: appinfo.NewService(appinfo.Config{
			Version:      opts.Version,
			Recorder:     recorder,
			OnFirstPaint: opts.OnFirstPaint,
		}),
		Scheduler: inbox.NewScheduler(inboxSvc, inbox.WithLogger(logger)),
		Timing:    recorder,
	}
}
