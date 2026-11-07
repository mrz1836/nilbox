// Package core assembles the app: it prepares the data directory, opens and migrates the
// database, syncs the discovered agent profiles, adds the starter prompts, and builds
// every service the window binds to.
//
// Core is plain Go with no desktop dependency, so the whole startup path is tested on any
// OS. The desktop shell only adds the window, binds Services, and runs the scheduler.
package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"gorm.io/gorm"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/appinfo"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/launcher"
	"github.com/mrz1836/nilbox/internal/notify"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/timing"
	"github.com/mrz1836/nilbox/internal/worklog"
	"github.com/mrz1836/nilbox/internal/worktrees"
)

var (
	// ErrNoDataDir is returned when Options.Paths has no root directory.
	ErrNoDataDir = errors.New("the data directory is not set")

	// ErrNoHomeDir is returned when Options.Home is empty.
	ErrNoHomeDir = errors.New("the home directory is not set")
)

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

	// Release is the release version and Build the build number; both are reported to
	// the frontend.
	Release string
	Build   string

	// Clock stamps refreshes, launches, and worktree listings. It defaults to the system
	// clock.
	Clock clock.Clock

	// Notifier is told when the stored inbox changed.
	Notifier inbox.Notifier

	// WorktreeNotifier is told when an automatic worktree sweep changed something.
	WorktreeNotifier worktrees.Notifier

	// ThemeNotifier is told when the theme was changed and stored.
	ThemeNotifier settings.ThemeNotifier

	// MenuBarNotifier is told when the menu bar setting was changed and stored.
	MenuBarNotifier settings.MenuBarNotifier

	// Banners shows native notifications; without it the notification log is still kept.
	Banners notify.Poster

	// NotificationsNotifier is told when the notification log changed.
	NotificationsNotifier notify.Notifier

	// Runner runs external programs: the GitHub token source, the clone scan, the
	// confirmed clone of a missing repository, the terminal drivers, and the worktree
	// service all run theirs through it. It defaults to an execpath.Exec whose finder
	// searches the well-known directories under Home.
	Runner execpath.Runner

	// Logger receives scheduler and timing records. It defaults to slog.Default().
	Logger *slog.Logger

	// OnFirstPaint is called once when the frontend reports its first paint.
	OnFirstPaint func(timing.Measurement)
}

// Core is the assembled app: the database and every service.
type Core struct {
	// DB is a GORM handle over the open, migrated database.
	DB *gorm.DB

	// pool is the database's connection pool, which DB shares.
	pool *sql.DB

	// The services bound to the frontend.
	Settings       *settings.Service
	Profiles       *profiles.Service
	Prompts        *prompts.Service
	LaunchDefaults *launchdefaults.Service
	Inbox          *inbox.Service
	Launcher       *launcher.Service
	Worktrees      *worktrees.Service
	Notifications  *notify.Service
	AppInfo        *appinfo.Service

	// ProfileStore holds the stored agent profiles that Profiles lists. It is not bound;
	// code that seeds the database, such as the launch measurement, writes through it.
	ProfileStore *profiles.Store

	// Scheduler refreshes the inbox on start and every five minutes once Run is called.
	// A worktrees.Sweeper over Worktrees does the same for the automatic worktree
	// cleanup.
	Scheduler *inbox.Scheduler

	// Timing records the first paint.
	Timing *timing.Recorder
}

// Bootstrap prepares the data directory, opens the database (backing it up and applying
// any pending migrations, or setting it aside for a fresh one when it cannot be used),
// syncs the agent profiles found under the home directory, adds any starter prompt not
// added before, and builds every service. It contacts no network; the scheduler does that
// once started.
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

	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	opened, err := store.Open(ctx, opts.Paths.DB)
	if err != nil {
		return nil, err
	}

	pool := opened.DB

	db, err := store.Gorm(pool)
	if err != nil {
		_ = pool.Close()

		return nil, err
	}

	reset := dataReset(ctx, opened.Reset, opts.Home, logger)

	if err = syncProfiles(ctx, profiles.NewStore(pool, opts.Clock), opts.Home, logger); err != nil {
		_ = pool.Close()

		return nil, err
	}

	if err = prompts.Seed(ctx, prompts.NewStore(pool, opts.Clock)); err != nil {
		_ = pool.Close()

		return nil, fmt.Errorf("add the starter prompts: %w", err)
	}

	return assemble(pool, db, reset, opts, logger), nil
}

// Services returns the services the frontend binds to, in a stable order.
func (c *Core) Services() []any {
	return []any{
		c.Settings, c.Profiles, c.Prompts, c.LaunchDefaults, c.Inbox, c.Launcher, c.Worktrees, c.Notifications,
		c.AppInfo,
	}
}

// Close closes the database.
func (c *Core) Close() error {
	if err := c.pool.Close(); err != nil {
		return fmt.Errorf("close database: %w", err)
	}

	return nil
}

// syncProfiles brings the stored agent profiles in line with home. It first removes
// unedited profiles whose directory holds no agent config (another tool's folder that
// happens to be named ~/.claude-<name>), then records every profile discovered under
// home. It never changes a stored profile, and keeps any that was edited or whose
// directory is gone. Pruning comes first so a name freed by a removed row can be taken
// by the directory that now owns it. A home directory that cannot be read only skips
// discovery, with a warning: the profiles already stored still work, and the inbox does
// not need them.
func syncProfiles(ctx context.Context, stored *profiles.Store, home string, logger *slog.Logger) error {
	removed, err := stored.Prune(ctx)
	if err != nil {
		return fmt.Errorf("prune profiles: %w", err)
	}

	if len(removed) > 0 {
		logger.InfoContext(ctx, "removed profiles whose directory holds no agent config", "profiles", removed)
	}

	found, err := profiles.Discover(home)
	if err != nil {
		logger.WarnContext(ctx, "profile discovery skipped", "error", err)

		return nil
	}

	if err = stored.Sync(ctx, found); err != nil {
		return fmt.Errorf("sync profiles: %w", err)
	}

	return nil
}

// dataReset logs a database Open set aside, and returns what the window tells the user
// about it, or nil when there was none.
func dataReset(ctx context.Context, reset *store.Reset, home string, logger *slog.Logger) *appinfo.DataReset {
	if reset == nil {
		return nil
	}

	logger.WarnContext(ctx, "the database could not be used; it was set aside and a fresh one started",
		"path", reset.Path, "reason", reset.Reason, "error", reset.Err)

	return &appinfo.DataReset{Path: paths.Tilde(reset.Path, home), Reason: string(reset.Reason)}
}

// assemble builds every service over the open database: pool is its connection pool and
// db the GORM handle over it. reset, when set, tells the window the database was set
// aside at startup.
func assemble(pool *sql.DB, db *gorm.DB, reset *appinfo.DataReset, opts Options, logger *slog.Logger) *Core {
	finder := execpath.Finder{Home: opts.Home}

	runner := opts.Runner
	if runner == nil {
		runner = execpath.Exec{Finder: finder}
	}

	profileStore := profiles.NewStore(pool, opts.Clock)
	promptStore := prompts.NewStore(pool, opts.Clock)
	defaultsStore := launchdefaults.NewStore(pool, opts.Clock)
	prefs := settings.NewService(pool, profileStore, opts.Home, settings.WithClock(opts.Clock),
		settings.WithThemeNotifier(opts.ThemeNotifier), settings.WithMenuBarNotifier(opts.MenuBarNotifier))
	tokens := github.NewTokenSource(runner, finder)
	client := github.NewClient(opts.Version)
	workLog := worklog.New(pool)

	notices := notify.Config{
		Pool: pool, Poster: opts.Banners, Notifier: opts.NotificationsNotifier, Clock: opts.Clock, Logger: logger,
	}

	inboxSvc := inbox.NewService(inbox.Config{
		Pool:     pool,
		DB:       db,
		Tokens:   tokens,
		Client:   client,
		Clock:    opts.Clock,
		Notifier: opts.Notifier,
		Arrivals: notify.NewDispatcher(notices),
	})

	recorder := timing.NewRecorder(timing.Config{Logger: logger, LogsDir: opts.Paths.Logs})

	launch := launcher.NewService(launcher.Config{
		DB:       db,
		Settings: prefs,
		Profiles: profileStore,
		Prompts:  promptStore,
		Defaults: defaultsStore,
		Git:      runner,
		GH:       runner,
		Finder:   finder,
		Registry: launcher.DefaultRegistry(runner),
		Paths:    opts.Paths,
		Home:     opts.Home,
		Clock:    opts.Clock,
		Launches: workLog,
		Files:    inboxSvc,
	})

	return &Core{
		DB:       db,
		pool:     pool,
		Settings: prefs,
		Profiles: profiles.NewService(profileStore, opts.Home),
		Prompts: prompts.NewService(prompts.Config{
			Store: promptStore, Usage: defaultsStore, Source: launcher.NewPromptSource(launch),
		}),
		LaunchDefaults: launchdefaults.NewService(launchdefaults.Config{
			Store: defaultsStore, Profiles: profileStore, Prompts: promptStore, Repos: inboxRepos{db: db},
		}),
		Inbox:    inboxSvc,
		Launcher: launch,
		Worktrees: worktrees.NewService(worktrees.Config{
			Settings: prefs,
			Git:      runner,
			Finder:   finder,
			Home:     opts.Home,
			Clock:    opts.Clock,
			GitHub:   itemStates{tokens: tokens, client: client},
			Launches: workLog,
			History:  workLog,
			Notifier: opts.WorktreeNotifier,
			Logger:   logger,
		}),
		Notifications: notify.NewService(notices),
		AppInfo: appinfo.NewService(appinfo.Config{
			Version:      opts.Version,
			Release:      opts.Release,
			Build:        opts.Build,
			Recorder:     recorder,
			OnFirstPaint: opts.OnFirstPaint,
			DataReset:    reset,
		}),
		ProfileStore: profileStore,
		Scheduler:    inbox.NewScheduler(inboxSvc, logger),
		Timing:       recorder,
	}
}

// itemStates reads the state of the pull requests and issues worktrees are linked to,
// with the GitHub CLI's token, for the worktree service.
type itemStates struct {
	tokens *github.TokenSource
	client *github.Client
}

// ItemStates asks GitHub about refs.
func (s itemStates) ItemStates(ctx context.Context, refs []item.Ref) (map[string]github.ItemState, error) {
	tok, err := s.tokens.Token(ctx)
	if err != nil {
		return nil, err
	}

	return s.client.FetchItemStates(ctx, tok, refs)
}
