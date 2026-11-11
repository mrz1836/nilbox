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
	"path/filepath"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/appinfo"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/budget"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/hooks"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/launcher"
	"github.com/mrz1836/nilbox/internal/launcher/terminal"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/notify"
	"github.com/mrz1836/nilbox/internal/paths"
	prefsstore "github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/procs"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/release"
	"github.com/mrz1836/nilbox/internal/review"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/scripts"
	"github.com/mrz1836/nilbox/internal/sessions"
	"github.com/mrz1836/nilbox/internal/sessions/observe/claude"
	"github.com/mrz1836/nilbox/internal/sessions/observe/codex"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/shellenv"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/timing"
	"github.com/mrz1836/nilbox/internal/toolinstall"
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

	// ShellStarter runs the user's login shell to read its environment, after the first
	// paint. Nil runs the real shell; tests hand in a fake, so no test runs the
	// developer's own shell.
	ShellStarter shellenv.Starter
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
	Sessions       *sessions.Service
	Review         *review.Service
	Integrations   *integrations.Service
	ScriptRuns     *scriptrun.Service
	Release        *release.Service
	Hooks          *hooks.Service
	Scripts        *scripts.Service

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

	// Watcher follows the agent sessions launches start, once Run is called; the desktop
	// shell runs it with the other work. ShellSessions are the sessions the menu bar
	// lists, and the terminals a click on one brings forward. Neither is bound.
	Watcher       *sessions.Watcher
	ShellSessions ShellSessions

	// BudgetSaver keeps the API budget across runs: it saves it soon after each change
	// once Run is called, and once more as Run's context ends. The desktop shell runs it
	// with the other work, which it stops before it closes the database.
	BudgetSaver *budget.Saver

	// Timing records the first paint.
	Timing *timing.Recorder

	// ShellEnv holds the user's login shell's environment once Deferred has read it: the
	// PATH the tools the app finds and runs come from. It is not bound.
	ShellEnv *shellenv.Reader

	// Deferred is the work kept off the cold start, which the desktop shell runs once
	// after the window's first paint (desktop.Config.Deferred). It is not bound.
	Deferred *Deferred

	// ToolFinder looks for the tools the integrations drive once the shell's environment is
	// read, again when woken, and checks for their updates once a day. The desktop shell
	// runs it as deferred work too. It is not bound.
	ToolFinder *integrations.Worker

	// Runs starts the runs (releases, hooks actions, checks, and scripts) and follows
	// them; ScriptRuns is its bound face. The desktop shell runs it as deferred work, which
	// picks up the runs left from before a restart and stops the app's own at quit, and
	// asks it before quitting whether runs are going (desktop.QuitGuard). It is not bound.
	Runs *scriptrun.Engine

	// ReleaseRefresher reads the Release page's list again behind the page when, after a
	// restart, the page was shown the list kept from the last start. The desktop shell runs
	// it as a worker. It is not bound.
	ReleaseRefresher *release.Refresher

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
		c.AppInfo, c.Budget, c.Sessions, c.Review, c.Integrations, c.ScriptRuns, c.Release, c.Hooks, c.Scripts,
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
	shellEnv := shellenv.New(shellenv.Config{Start: opts.ShellStarter, Home: opts.Home, Logger: logger})
	search := execpath.Search{Home: opts.Home}

	runner := opts.Runner
	gitRunner := opts.Runner

	versionRunner := runner
	agentRunners := func([]string) execpath.Runner { return opts.Runner }
	toolRunners := func([]string) execpath.Runner { return opts.Runner }

	if runner == nil {
		runner, gitRunner, versionRunner = execpath.Exec{Search: search}, worktreeRunner(search), agentRunner{search: search}
		agentRunners = func(env []string) execpath.Runner { return agentRunner{search: search, env: env} }
		toolRunners = nil // the integrations run each tool by its path, with the shell's environment
	}

	profileStore := profiles.NewStore(pool, opts.Clock)
	promptStore := info.kept.prompts
	defaultsStore := info.kept.defaults
	inboxStore := inbox.NewStore(pool, opts.Clock)
	followed := &followSwitch{sink: event.Or(opts.Events)}
	prefs := settings.NewService(pool, profileStore, opts.Home, settings.WithClock(opts.Clock),
		settings.WithEvents(followed), settings.WithLogger(logger))
	tokens := github.NewTokenSource(runner, search,
		github.WithTokenClock(opts.Clock), github.WithHostsFile(github.GHHostsFile(opts.Home)))
	// One tracker hears every response the shared client gets, whichever service asked.
	tracker := budget.NewTracker(opts.Clock, opts.Events)
	keeper := newBudgetKeeper(pool, opts.Clock)
	client := github.NewClient(opts.Version)
	client.Rates = tracker
	// The same tracker paces the edits the client makes for the user (budget.Writes).
	client.Writes = tracker
	workLog := worklog.New(pool)

	notices := notify.Config{
		Pool: pool, Poster: opts.Banners, Events: opts.Events, Clock: opts.Clock, Logger: logger,
	}
	// One dispatcher records the inbox's arrivals and the notifications about no item, such
	// as a release that reached GitHub.
	dispatcher := notify.NewDispatcher(notices)

	inboxSvc := inbox.NewService(inbox.Config{
		Pool:     pool,
		Tokens:   tokens,
		Client:   client,
		Clock:    opts.Clock,
		Events:   opts.Events,
		Arrivals: dispatcher,
		Usage:    tracker,
		Logger:   logger,
	})

	recorder := timing.NewRecorder(timing.Config{Logger: logger, LogsDir: opts.Paths.Logs})

	registry := terminal.DefaultRegistry(runner)
	follow := followSessions(pool, prefs, opts, logger, runner, registry, agentRunners)
	followed.watcher = follow.watcher

	// clones is the app's one set of clone locks: every job that changes a clone takes
	// its clone's, so a worktree is never made or removed while a run is using it.
	clones := clonelock.New()

	sweeps := worktrees.NewService(worktrees.Config{
		Settings: settingsView[worktrees.SweepSettings]{settings: prefs, part: sweepSettings},
		Git:      gitRunner,
		Finder:   search,
		Home:     opts.Home,
		Clock:    opts.Clock,
		GitHub:   itemStates{tokens: tokens, client: client},
		Records:  workLog,
		Procs:    procs.System{Run: runner},
		History:  workLog,
		Agents:   follow.ledger,
		Events:   opts.Events,
		Clones:   clones,
		Logger:   logger,
	})

	launch := launcher.NewService(launcher.Config{
		Items:     launchItems{Store: inboxStore},
		Settings:  settingsView[launcher.LaunchSettings]{settings: prefs, part: launchSettings},
		Profiles:  profileStore,
		Prompts:   promptStore,
		Defaults:  defaultsStore,
		Git:       runner,
		GH:        runner,
		Finder:    search,
		Registry:  registry,
		Paths:     opts.Paths,
		Home:      opts.Home,
		Clock:     opts.Clock,
		Worktrees: worktreeMaker{maker: worktrees.NewMaker(sweeps)},
		Sessions:  workLog,
		Ledger:    sessionLedger{ledger: follow.ledger},
		Trust:     trusters(opts.Home, logger),
		Files:     inboxSvc,
		Logger:    logger,
	})

	previews := launcher.NewPromptSource(launch)
	// One terminal opens the runs in the user's terminal, and the terminal in a run's folder
	// or a clone, for the runs and for Release alike.
	terminals := terminalRuns{
		launch: opts.Paths.Launch, opener: launcher.NewCommandOpener(launch), clock: opts.Clock, logger: logger,
		shell: shellEnv.Shell,
	}
	runs := newRuns(pool, opts, clones, terminals, runner, logger)
	scriptRuns := scriptrun.NewService(runs)

	tools := integrations.NewService(integrations.Config{
		Prefs: prefsstore.New(pool, opts.Clock), Env: shellEnv, Releases: latestReleases{tokens: tokens, client: client},
		Home: opts.Home, Clock: opts.Clock, Events: opts.Events, Logger: logger, Runner: toolRunners, DB: pool,
		// Releases download straight from GitHub's download hosts, never through the API.
		Installer: &toolinstall.Installer{
			Downloads: filepath.Join(opts.Paths.Root, "downloads"), Download: client, Slug: brand.Slug,
		},
		Commands: runner,
		// A copy that can update itself runs its own updater as a run the window follows, and
		// an install holds the folder it places a program in, as that run does.
		Runs: runs, Locks: clones,
		// An install waits while a run is using the tool.
		InUse: func(id integrationprefs.ID) bool { return runs.InUse(string(id)) },
	})
	// A tool's update left from before a restart is checked as one that just ended.
	runs.RegisterVerifier(scriptrun.KindUpdate, integrations.NewVerifier(tools))
	toolFinder := integrations.NewWorker(tools)
	followed.tools = toolFinder

	// The clones are read with the git the user's shell finds, as magex runs it.
	releaseGit := gitread.New(gitread.Config{
		Env: func() []string { env, _ := shellEnv.Env(); return env }, Runner: toolRunners,
	})
	releases := release.NewService(release.Config{
		GitHub:  releaseGitHub{tokens: tokens, client: client},
		Clones:  releaseClones{settings: prefs, home: opts.Home, git: runner},
		Checker: release.GitChecker{Git: releaseGit},
		Git:     releaseGit, Origins: releaseOrigins{git: runner}, Tools: releaseTool{tools: tools}, Locks: clones,
		Env: shellEnv.Env, Ready: shellEnv.Ready, Runner: toolRunners, Runs: runs, Places: releaseSettings{settings: prefs},
		History: scriptRuns, Notifier: releaseNotices{dispatcher: dispatcher},
		// A merge's offer reads the setting, magex, the clones, and the default branch the
		// inbox stored: nothing of GitHub's.
		AfterMerge: releaseSettings{settings: prefs}, Branches: inboxStore,
		// A check's Open in terminal opens the terminal chosen for launches in the clone.
		Terminal: terminals,
		// The last list is kept in the prefs, so the next start shows it at once.
		Keeper: prefsstore.New(pool, opts.Clock), Home: opts.Home, Clock: opts.Clock, Events: opts.Events, Logger: logger,
	})
	// A release left from before a restart is read and verified as one started now.
	runs.RegisterVerifier(scriptrun.KindRelease, release.NewVerifier(releases))

	deps := hookDeps{
		settings: prefs, tools: tools, git: releaseGit, runner: runner, clones: clones, runs: runs, history: scriptRuns,
		shellEnv: shellEnv, terminal: terminals,
	}
	hookPage := newHooks(pool, opts, logger, deps)
	repoScripts := newScripts(pool, opts, logger, deps, inboxStore)

	return &Core{
		DB:       pool,
		Settings: prefs,
		Profiles: profiles.NewService(profileStore, opts.Home, search),
		Prompts: prompts.NewService(prompts.Config{
			Store: promptStore, Usage: defaultsStore, Source: previews,
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
			Finder:    search,
			Runner:    versionRunner,
			Home:      opts.Home,
		}),
		Budget: budget.NewService(budget.Config{
			Tracker: tracker, Reader: rateReader{tokens: tokens, client: client}, Clock: opts.Clock,
			RefreshInterval: inbox.DefaultInterval,
		}),
		Sessions: follow.service,
		Review: review.NewService(review.Config{
			Pool: pool, Inbox: reviewInbox{store: inboxStore, actor: inbox.NewActor(inboxSvc)}, GitHub: client,
			Settings: reviewSettings{settings: prefs}, Replies: reviewReplies{store: promptStore, source: previews},
			Events: opts.Events, Clock: opts.Clock, Logger: logger,
		}),
		Watcher:          follow.watcher,
		ShellSessions:    ShellSessions{ledger: follow.ledger, service: follow.service},
		ShellSettings:    newShellSettings(prefs, pool, opts.Clock),
		ProfileStore:     profileStore,
		Scheduler:        inbox.NewScheduler(inboxSvc, logger),
		Sweeper:          worktrees.NewSweeper(sweeps, worktrees.DefaultSweepInterval),
		Timing:           recorder,
		ShellEnv:         shellEnv,
		Deferred:         &Deferred{steps: []func(context.Context){func(ctx context.Context) { _ = shellEnv.Read(ctx) }}},
		Integrations:     tools,
		ToolFinder:       toolFinder,
		Runs:             runs,
		ScriptRuns:       scriptRuns,
		Release:          releases,
		ReleaseRefresher: release.NewRefresher(releases),
		Hooks:            hookPage,
		Scripts:          repoScripts,
		tokens:           tokens,
		BudgetSaver:      budget.NewSaver(tracker, keeper, logger),
		client:           client,
		budget:           tracker,
		budgetKeeper:     keeper,
	}
}

// newRuns builds the engine that starts and follows the runs, over the app's one set of
// clone locks, with its logs in <data>/script-runs. Its changes reach the window as
// event.ScriptRunsUpdated, and as event.ToolsUpdated too, since a run starting or ending
// can block or free a tool's update; a run's progress alone, as a release's next step,
// reaches it as event.ScriptRunProgress, which names the run. A run in the terminal opens
// through terminals, and the user's choice to stop the runs and quit reaches the desktop
// shell as event.QuitConfirmed.
func newRuns(
	pool *sql.DB, opts Options, clones *clonelock.Set, terminals terminalRuns, runner execpath.Runner,
	logger *slog.Logger,
) *scriptrun.Engine {
	events := event.Or(opts.Events)

	return scriptrun.NewEngine(scriptrun.Config{
		DB: pool, Locks: clones, LogDir: filepath.Join(opts.Paths.Root, "script-runs"), Home: opts.Home,
		Clock: opts.Clock, Logger: logger,
		Changed: func() {
			events.Emit(event.ScriptRunsUpdated{})
			events.Emit(event.ToolsUpdated{})
		},
		Progressed: func(id int64) { events.Emit(event.ScriptRunProgress{ID: id}) },
		Terminal:   terminals,
		Reveal:     revealer(runner),
		Quit:       func() { events.Emit(event.QuitConfirmed{}) },
	})
}

// hookDeps are the services and building blocks the Hooks page and Repo scripts share with
// the rest of the app.
type hookDeps struct {
	settings *settings.Service
	tools    *integrations.Service
	git      *gitread.Git
	runner   execpath.Runner
	clones   *clonelock.Set
	runs     *scriptrun.Engine
	history  *scriptrun.Service
	shellEnv *shellenv.Reader
	terminal terminalRuns
}

// newHooks builds the Hooks page's service: the clones under the clone root, read with the
// same read-only git as Release's, go-pre-commit as the integrations found it, its actions
// and checks run through the runner with the shell's environment, and the hooks.*
// preferences. A row's Open in terminal opens the terminal the runs open in, and its Open
// the config shows the config in Finder, as Show the log file does. Run again works a
// hooks action or a checks run out afresh through its Rerunner.
func newHooks(pool *sql.DB, opts Options, logger *slog.Logger, deps hookDeps) *hooks.Service {
	var runner func([]string, string) execpath.Runner

	if opts.Runner != nil {
		runner = func([]string, string) execpath.Runner { return opts.Runner }
	}

	svc := hooks.NewService(hooks.Config{
		Clones: hooksClones{settings: deps.settings, home: opts.Home, git: deps.runner}, Git: deps.git,
		Tools: hooksTool{tools: deps.tools}, Locks: deps.clones, Runs: deps.runs, History: deps.history,
		Places: hooksSettings{settings: deps.settings}, Memory: prefsstore.New(pool, opts.Clock), Env: deps.shellEnv.Env,
		Runner: runner, Terminal: deps.terminal, Reveal: revealer(deps.runner), Home: opts.Home, Clock: opts.Clock,
		Events: opts.Events, Logger: logger,
	})

	rerun := hooks.NewRerunner(svc)
	deps.runs.RegisterRerunner(scriptrun.KindHooks, rerun)
	deps.runs.RegisterRerunner(scriptrun.KindChecks, rerun)

	return svc
}

// newScripts builds Repo scripts' service over the same clones, read-only git, runner, and
// shell environment as the Hooks page's: magex as the integrations found it, the settings'
// sources and where runs go, the default branches the inbox stored, and the trusted
// commands in script_trust. Run again works a script out afresh through its Rerunner.
func newScripts(pool *sql.DB, opts Options, logger *slog.Logger, deps hookDeps, branches scripts.DefaultBranches,
) *scripts.Service {
	var runner func([]string, string) execpath.Runner

	if opts.Runner != nil {
		runner = func([]string, string) execpath.Runner { return opts.Runner }
	}

	svc := scripts.NewService(scripts.Config{
		Clones: hooksClones{settings: deps.settings, home: opts.Home, git: deps.runner}, Git: deps.git,
		Branches: branches, Tools: scriptsTool{tools: deps.tools}, Settings: integrationSettings{settings: deps.settings},
		Runs: deps.runs, History: deps.history, DB: pool, Env: deps.shellEnv.Env, Shell: deps.shellEnv.Shell,
		Runner: runner, Home: opts.Home, Clock: opts.Clock, Logger: logger,
	})

	deps.runs.RegisterRerunner(scriptrun.KindScript, scripts.NewRerunner(svc))

	return svc
}

// following is what follows the agent sessions: the ledger launches record them in, the
// watcher that follows them, and the service the window binds.
type following struct {
	ledger  *sessions.Ledger
	watcher *sessions.Watcher
	service *sessions.Service
}

// followSessions builds what follows the agent sessions over the database: an observer
// per agent, each through the agent's own supported interface, the user's master switch,
// the session banners, and the terminal drivers that bring a session's tab forward.
func followSessions(
	pool *sql.DB, prefs *settings.Service, opts Options, logger *slog.Logger, runner execpath.Runner,
	registry terminal.Registry, agentRunners func([]string) execpath.Runner,
) following {
	search := execpath.Search{Home: opts.Home}
	store := sessions.NewStore(pool)
	processes := procs.System{Run: runner}

	watcher := sessions.NewWatcher(sessions.WatcherConfig{
		Store: store,
		Observers: map[agent.ID]sessions.Observer{
			agent.Claude: claude.New(claude.Config{
				Finder: search, Run: agentRunners, Home: opts.Home, Procs: processes, Logger: logger,
			}),
			agent.Codex: codex.New(codex.Config{
				Dialer: codex.ProxyDialer{
					Finder: search, Info: codex.ClientInfo{Name: brand.Slug, Title: brand.Name, Version: opts.Version},
				},
				Home: opts.Home, Procs: processes, Logger: logger,
			}),
		},
		Switch:    prefs,
		Attention: notify.NewSessionBanners(notify.Config{Pool: pool, Poster: opts.Banners, Clock: opts.Clock, Logger: logger}),
		Events:    opts.Events,
		Clock:     opts.Clock,
		Home:      opts.Home,
		Logger:    logger,
	})

	cfg := sessions.Config{
		Store: store, Watcher: watcher, Switch: prefs, Focuser: terminalFocuser{registry: registry}, PS: runner,
		Home: opts.Home, Events: opts.Events, Clock: opts.Clock, Logger: logger,
		Words: map[agent.ID]sessions.WordsReader{
			agent.Claude: claude.Words{Home: opts.Home}, agent.Codex: codex.Words{Home: opts.Home},
		},
	}

	return following{ledger: sessions.NewLedger(cfg), watcher: watcher, service: sessions.NewService(cfg)}
}
