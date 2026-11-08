// Package core assembles the app: it prepares the data directory, opens and migrates the
// database, syncs the discovered agent profiles, brings back the prompt library and
// launch defaults the database lost, adds the starter prompts, and builds every service
// the window binds to.
//
// Core is plain Go with no desktop dependency, so the whole startup path is tested on any
// OS. The desktop shell only adds the window, binds Services, hears the services' events,
// and runs the scheduler and the sweeper.
package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/appinfo"
	"github.com/mrz1836/nilbox/internal/budget"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/launcher"
	"github.com/mrz1836/nilbox/internal/launcher/terminal"
	"github.com/mrz1836/nilbox/internal/logging"
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

	// Events hears every service's events: the inbox, the worktree sweep, and the
	// notification log changed; the theme or the menu bar setting was stored; the window
	// painted its first frame. Without it they are dropped.
	Events event.Sink

	// Banners shows native notifications; without it the notification log is still kept.
	Banners notify.Poster

	// Runner runs external programs: the GitHub token source, the clone scan, the
	// confirmed clone of a missing repository, the terminal drivers, and the worktree
	// service all run theirs through it. It defaults to an execpath.Exec whose search
	// covers the well-known directories under Home.
	Runner execpath.Runner

	// Logger receives every service's log records. Without it they are not logged.
	Logger *slog.Logger
}

// Core is the assembled app: the database and every service.
type Core struct {
	// DB is the open, migrated database's connection pool, which every service shares.
	// It is not bound; code that seeds the database, such as the launch measurement,
	// writes through it.
	DB *sql.DB

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
	Budget         *budget.Service

	// ShellSettings are the settings the desktop shell reads when the app launches. They
	// are not bound.
	ShellSettings ShellSettings

	// ProfileStore holds the stored agent profiles that Profiles lists. It is not bound;
	// code that seeds the database, such as the launch measurement, writes through it.
	ProfileStore *profiles.Store

	// Scheduler refreshes the inbox on start and every five minutes once Run is called,
	// and Sweeper does the same for the automatic worktree cleanup. Each runs until its
	// context is canceled; the desktop shell runs both while the app is open.
	Scheduler *inbox.Scheduler
	Sweeper   worktrees.Sweeper

	// BudgetSaver keeps the API budget across runs: it saves it soon after each change
	// once Run is called, and once more as Run's context ends. The desktop shell runs it
	// with the other work, which it stops before it closes the database.
	BudgetSaver *budget.Saver

	// Timing records the first paint.
	Timing *timing.Recorder

	// tokens holds the GitHub token the inbox and the worktree service borrow, until
	// Close forgets it.
	tokens *github.TokenSource

	// client is the one GitHub client every service shares, which tells budget about
	// every response it gets.
	client *github.Client

	// budget hears every GitHub response's budget reading, and each inbox refresh's cost,
	// and budgetKeeper keeps its state between runs.
	budget       *budget.Tracker
	budgetKeeper budget.Keeper
}

// Bootstrap prepares the data directory, opens the database (backing it up and applying
// any pending migrations, or setting it aside for a fresh one when it cannot be used),
// syncs the agent profiles found under the home directory, brings back the prompt
// library and the launch defaults when the database holds a new library (from the
// database set aside, or else from the copies kept beside it), adds any starter prompt
// not added before, and builds every service. It contacts no network; the scheduler does that
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

	logger := logging.Or(opts.Logger)

	opened, err := store.Open(ctx, opts.Paths.DB)
	if err != nil {
		return nil, err
	}

	pool := opened.DB
	reset := dataReset(ctx, opened.Reset, opts.Home, logger)

	if err = syncProfiles(ctx, profiles.NewStore(pool, opts.Clock), opts.Home, logger); err != nil {
		_ = pool.Close()

		return nil, err
	}

	keeper := newKept(pool, opts, logger)
	restored := keeper.restore(ctx, opened.Reset)

	if err = prompts.Seed(ctx, keeper.prompts); err != nil {
		_ = pool.Close()

		return nil, fmt.Errorf("add the starter prompts: %w", err)
	}

	keeper.keep(ctx, restored.prompts.From != "")

	info := startup{reset: reset, restored: restored.info(opts.Home), kept: keeper}

	c := assemble(pool, info, opts, logger)
	c.restoreBudget(ctx, logger)

	return c, nil
}

// Services returns the services the frontend binds to, in a stable order.
func (c *Core) Services() []any {
	return []any{
		c.Settings, c.Profiles, c.Prompts, c.LaunchDefaults, c.Inbox, c.Launcher, c.Worktrees, c.Notifications,
		c.AppInfo, c.Budget,
	}
}

// Close forgets the GitHub token held in memory and closes the database.
func (c *Core) Close() error {
	c.tokens.Clear()

	if err := c.DB.Close(); err != nil {
		return fmt.Errorf("close database: %w", err)
	}

	return nil
}

// restoreBudget starts the API budget where the last run left it: its readings and what
// the app spent from each budget, as the BudgetSaver kept them. Without a kept state, as
// on the first run of a build that keeps one, it starts from the readings the last
// refresh stored. Failures are only logged: the first response reads the budgets anyway.
func (c *Core) restoreBudget(ctx context.Context, logger *slog.Logger) {
	state, err := c.budgetKeeper.Load(ctx)
	if err == nil && state != "" {
		if err = c.budget.Restore(state); err == nil {
			return
		}
	}

	if err != nil {
		logger.WarnContext(ctx, "could not read the kept API budget", "error", err)
	}

	snap, err := c.Inbox.Snapshot(ctx)
	if err != nil {
		logger.WarnContext(ctx, "could not read the stored API budget", "error", err)

		return
	}

	c.budget.Seed(storedRates(snap.Rate))
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
	removed, err := stored.Prune(ctx, logger)
	if err != nil {
		return fmt.Errorf("prune profiles: %w", err)
	}

	if len(removed) > 0 {
		logger.InfoContext(ctx, "removed profiles whose directory holds no agent config", "profiles", removed)
	}

	found, err := profiles.Discover(home, logger)
	if err != nil {
		logger.WarnContext(ctx, "profile discovery skipped", "error", err)

		return nil
	}

	if err = stored.Sync(ctx, found); err != nil {
		return fmt.Errorf("sync profiles: %w", err)
	}

	return nil
}

// startup is what Bootstrap learned before the services were built.
type startup struct {
	// reset is set when the database was set aside, and restored when the prompt library
	// and launch defaults were brought back.
	reset    *appinfo.DataReset
	restored *appinfo.Restored

	// kept holds the stores whose data is kept outside the database, with their copies.
	kept *kept
}

// dataReset logs a database Open set aside, and returns what the window tells the user
// about it, or nil when there was none.
func dataReset(ctx context.Context, reset *store.Reset, home string, logger *slog.Logger) *appinfo.DataReset {
	if reset == nil {
		return nil
	}

	logger.WarnContext(ctx, "the database could not be used; it was set aside and a fresh one started",
		"path", reset.Path, "reason", reset.Reason, "error", reset.Err)

	return &appinfo.DataReset{Path: paths.Tilde(reset.Path, home), Reason: appinfo.ResetReason(reset.Reason)}
}

// assemble builds every service over the open database's connection pool. info says
// what happened at startup, which the window is told, and holds the stores whose data is
// kept outside the database, with their copies.
func assemble(pool *sql.DB, info startup, opts Options, logger *slog.Logger) *Core {
	search := execpath.Search{Home: opts.Home}

	runner := opts.Runner
	if runner == nil {
		runner = execpath.Exec{Search: search}
	}

	profileStore := profiles.NewStore(pool, opts.Clock)
	promptStore := info.kept.prompts
	defaultsStore := info.kept.defaults
	inboxStore := inbox.NewStore(pool, opts.Clock)
	prefs := settings.NewService(pool, profileStore, opts.Home, settings.WithClock(opts.Clock),
		settings.WithEvents(opts.Events), settings.WithLogger(logger))
	tokens := github.NewTokenSource(runner, search,
		github.WithTokenClock(opts.Clock), github.WithHostsFile(github.GHHostsFile(opts.Home)))
	// One tracker hears every response the shared client gets, whichever service asked.
	tracker := budget.NewTracker(opts.Clock, opts.Events)
	keeper := newBudgetKeeper(pool, opts.Clock)
	client := github.NewClient(opts.Version)
	client.Rates = tracker
	workLog := worklog.New(pool)

	notices := notify.Config{
		Pool: pool, Poster: opts.Banners, Events: opts.Events, Clock: opts.Clock, Logger: logger,
	}

	inboxSvc := inbox.NewService(inbox.Config{
		Pool:     pool,
		Tokens:   tokens,
		Client:   client,
		Clock:    opts.Clock,
		Events:   opts.Events,
		Arrivals: notify.NewDispatcher(notices),
		Usage:    tracker,
		Logger:   logger,
	})

	recorder := timing.NewRecorder(timing.Config{Logger: logger, LogsDir: opts.Paths.Logs})

	launch := launcher.NewService(launcher.Config{
		Items:    launchItems{Store: inboxStore},
		Settings: settingsView[launcher.LaunchSettings]{settings: prefs, part: launchSettings},
		Profiles: profileStore,
		Prompts:  promptStore,
		Defaults: defaultsStore,
		Git:      runner,
		GH:       runner,
		Finder:   search,
		Registry: terminal.DefaultRegistry(runner),
		Paths:    opts.Paths,
		Home:     opts.Home,
		Clock:    opts.Clock,
		Launches: workLog,
		Files:    inboxSvc,
		Logger:   logger,
	})

	sweeps := worktrees.NewService(worktrees.Config{
		Settings: settingsView[worktrees.SweepSettings]{settings: prefs, part: sweepSettings},
		Git:      runner,
		Finder:   search,
		Home:     opts.Home,
		Clock:    opts.Clock,
		GitHub:   itemStates{tokens: tokens, client: client},
		Launches: workLog,
		History:  workLog,
		Events:   opts.Events,
		Logger:   logger,
	})

	return &Core{
		DB:       pool,
		Settings: prefs,
		Profiles: profiles.NewService(profileStore, opts.Home),
		Prompts: prompts.NewService(prompts.Config{
			Store: promptStore, Usage: defaultsStore, Source: launcher.NewPromptSource(launch),
			File: info.kept.promptFile, Database: opts.Paths.DB, App: opts.Version, Clock: opts.Clock, Logger: logger,
			Changed: info.kept.saveDefaults,
		}),
		LaunchDefaults: launchdefaults.NewService(launchdefaults.Config{
			Store: defaultsStore, Profiles: profileStore, Prompts: promptStore, Repos: inboxRepos{store: inboxStore},
			File: info.kept.defaultsFile, Logger: logger,
		}),
		Inbox:         inboxSvc,
		Launcher:      launch,
		Worktrees:     sweeps,
		Notifications: notify.NewService(notices),
		AppInfo: appinfo.NewService(appinfo.Config{
			Version:   opts.Version,
			Release:   opts.Release,
			Build:     opts.Build,
			Recorder:  recorder,
			Events:    opts.Events,
			DataReset: info.reset,
			Restored:  info.restored,
		}),
		Budget: budget.NewService(budget.Config{
			Tracker: tracker, Reader: rateReader{tokens: tokens, client: client}, Clock: opts.Clock,
			RefreshInterval: inbox.DefaultInterval,
		}),
		ShellSettings: newShellSettings(prefs, pool, opts.Clock),
		ProfileStore:  profileStore,
		Scheduler:     inbox.NewScheduler(inboxSvc, logger),
		Sweeper:       worktrees.NewSweeper(sweeps, worktrees.DefaultSweepInterval),
		Timing:        recorder,
		tokens:        tokens,
		BudgetSaver:   budget.NewSaver(tracker, keeper, logger),
		client:        client,
		budget:        tracker,
		budgetKeeper:  keeper,
	}
}
