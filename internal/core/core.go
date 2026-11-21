// Package core assembles the app: it prepares the data directory, opens and migrates the
// database (starting over first when the user asked to), syncs the discovered agent
// profiles, brings back the prompt library, the launch defaults, and the user's own scripts
// the database lost, adds the starter prompts, decides once whether the first-run setup
// shows, and builds every service the window binds to.
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
	"github.com/mrz1836/nilbox/internal/assignees"
	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/avatars"
	"github.com/mrz1836/nilbox/internal/backup"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/budget"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/describeit"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/ghthreads"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/hooks"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/issues"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/launcher"
	"github.com/mrz1836/nilbox/internal/launcher/terminal"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/manifests"
	"github.com/mrz1836/nilbox/internal/milestones"
	"github.com/mrz1836/nilbox/internal/notify"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/people"
	prefsstore "github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/procs"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/pushwatch"
	"github.com/mrz1836/nilbox/internal/release"
	"github.com/mrz1836/nilbox/internal/review"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/scripts"
	"github.com/mrz1836/nilbox/internal/sessions"
	"github.com/mrz1836/nilbox/internal/sessions/observe/claude"
	"github.com/mrz1836/nilbox/internal/sessions/observe/codex"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/setup"
	"github.com/mrz1836/nilbox/internal/shellenv"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/svcstatus"
	"github.com/mrz1836/nilbox/internal/timing"
	"github.com/mrz1836/nilbox/internal/toolinstall"
	"github.com/mrz1836/nilbox/internal/update"
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

	// AgentFinder finds the coding agent's program a launch starts. Nil searches the PATH
	// and the well-known directories under Home; tests hand in a fake, so a launch never
	// depends on the agents installed on the developer's machine.
	AgentFinder execpath.Finder

	// Logger receives every service's log records. Without it they are not logged.
	Logger *slog.Logger

	// ShellStarter runs the user's login shell to read its environment, after the first
	// paint. Nil runs the real shell; tests hand in a fake, so no test runs the
	// developer's own shell.
	ShellStarter shellenv.Starter

	// Sandbox marks a run on a data directory named with --data-dir, such as the fresh one
	// magex app:fresh opens, in place of the user's own; the window says it shows test
	// data.
	Sandbox bool

	// Flavor is the kind of build (main.flavor): a release or a rehearsal updates itself,
	// and any other build never does. Release is the version it compares a newer one with.
	Flavor update.Flavor

	// UpdateFeed is the folder a rehearsal reads its releases from, and UpdateKey the raw
	// public key it trusts; a release ignores both and trusts update.ReleaseKey alone.
	UpdateFeed string
	UpdateKey  string

	// Bundle is the running app's bundle, as the desktop bootstrap finds it, or "" outside
	// one: an update is staged beside it, and the start cleans up what one leaves there.
	Bundle string
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
	Issues         *issues.Service
	Avatars        *avatars.Service
	Integrations   *integrations.Service
	ScriptRuns     *scriptrun.Service
	Release        *release.Service
	Hooks          *hooks.Service
	Scripts        *scripts.Service
	Manifests      *manifests.Service
	GHThreads      *ghthreads.Service
	Backup         *backup.Service
	ServiceStatus  *svcstatus.Service
	Setup          *setup.Service
	Updates        *update.Service
	Autopilot      *autopilot.Service

	// Milestones keeps the log of the inbox reaching nil, which says what each time is
	// worth to the party the window throws.
	Milestones *milestones.Service

	// DescribeIt is the Autopilot tab's conversation with a coding agent, which proposes
	// routines, prompts, settings, and rules for the user to review.
	DescribeIt *describeit.Service

	// DescribeItWorker keeps Describe it's folder, snapshot, and plugin current, and follows
	// the proposals from the user's own Claude Code; the desktop shell runs it as deferred
	// work. It is not bound.
	DescribeItWorker *describeit.Worker

	// AutopilotEngine runs the routines: it records what each matched, and starts what may
	// start, in the copy of the app that holds the lock in the data folder. The desktop
	// shell runs it as deferred work. It is not bound; the window's actions reach it
	// through Autopilot.
	AutopilotEngine *autopilot.Engine

	// ShellAutopilot is Autopilot as the desktop shell reads it, for the menu bar's
	// Autopilot group and the window's close, and Pause Autopilot, which the menu's
	// checkbox row writes through the settings. It is not bound.
	ShellAutopilot ShellAutopilot

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

	// WatchPasses stores what the routines' searches watch, which each refresh reads after
	// the inbox's own lists, once the refresh has stored the inbox; the desktop shell runs
	// it with the scheduler, before it. Without it running no refresh reads the searches.
	// It is not bound.
	WatchPasses *inbox.WatchPasses

	// InboxRoutines is the inbox's face for Autopilot's own reads, which the refresh
	// measurement also waits on the watch passes through. It is not bound.
	InboxRoutines *inbox.Routines

	// ChecksFollower reads again, as often as Settings → Refreshing says (45 seconds by
	// default, or never), the pull requests whose checks were still running, in one
	// request, and nothing while none runs; the desktop shell runs it with the scheduler.
	ChecksFollower *inbox.ChecksFollower

	// MergeFollower reads again, a few seconds after each merge the inbox learns of, the
	// repository's other open pull requests on the branch merged into, whose merge state
	// the merge changed, and again while GitHub is still working one out; the desktop
	// shell runs it with the other work. It is not bound.
	MergeFollower *inbox.MergeFollower

	// Rereads reads again, one at a time, the item of each agent session that stops
	// working, as the Watcher asks, and each pull request a push on this Mac moved, as the
	// PushWatcher asks (one GraphQL point each), so a pull request the agent reviewed leaves
	// Review requested at once; the desktop shell runs it with the other work. It is not
	// bound.
	Rereads *inbox.Rereads

	// PushWatcher follows, while Settings → Refreshing's switch is on, the branches of the
	// inbox's pull requests in the user's clones, and hands Rereads a pull request whose
	// branch a push or a fetch moved to a commit GitHub hadn't reported for it; the desktop
	// shell runs it with the deferred work. It is not bound.
	PushWatcher *pushwatch.Watcher

	// GHThreadsSyncer reads the user's GitHub notifications into GHThreads' mirror on
	// start, then as often as the notifications' refresh setting says, never sooner than
	// GitHub asks; the desktop shell runs it after the scheduler. It is not bound.
	GHThreadsSyncer *ghthreads.Syncer

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

	// ReleaseChecksFollower reads again, as often as Settings → Refreshing says (45
	// seconds by default, or never), the Release page's repositories whose newest commit's
	// checks were still running, and patches their rows into the list, quietly; nothing
	// while none runs, nor until the page has read the list from GitHub in this run. The
	// desktop shell runs it as a worker. It is not bound.
	ReleaseChecksFollower *release.ChecksFollower

	// ReleaseFollower follows each release that reached GitHub until its workflow
	// publishes it, fails, or the follow reaches its limits, and resumes the follows a quit
	// cut off. The desktop shell runs it as deferred work, beside Runs: a release Runs
	// verifies as on GitHub after a restart is handed to it once recorded. It is not bound.
	ReleaseFollower *release.Follower

	// ServiceStatusChecker checks the status pages of the services the user watches
	// (GitHub's, Claude's, OpenAI's) as often as Settings → Service status says (every 30
	// minutes by default, or never), so the window can say when a part watched has an
	// incident; ServiceStatus is its bound face. The desktop shell runs it as deferred
	// work. It is not bound.
	ServiceStatusChecker *svcstatus.Checker

	// UpdateEngine checks for, downloads, and installs the app's updates; Updates is its
	// bound face. The desktop shell attaches the Installer it restarts with (Attach) once
	// the desktop runtime is created. It is not bound.
	UpdateEngine *update.Engine

	// UpdateWorker reads how the last restart to update went, cleans up after it, and
	// runs the timed checks (a minute in, then every six hours by the wall clock). The
	// desktop shell runs it as deferred work. It is not bound.
	UpdateWorker *update.Worker

	// HooksScans holds the Hooks page's scans, which run apart from the window's calls, to
	// the app's lifetime: the app's quit ends them, and the desktop shell, which runs it as
	// a worker, waits for them before it closes the database. It is not bound.
	HooksScans hooks.Scans

	// ManifestsScans holds the Deps page's reads of the clones, which run apart from the
	// window's calls, to the app's lifetime, as HooksScans does the Hooks page's. It is not
	// bound.
	ManifestsScans manifests.Scans

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

	// dispatcher records every notification and shows its banner; Close stops it gathering
	// banners before the database closes.
	dispatcher *notify.Dispatcher
}

// Bootstrap prepares the data directory, opens the database (backing it up and applying
// any pending migrations, setting it aside for a fresh one when it cannot be used, or when
// the user asked to start over), syncs the agent profiles found under the home directory,
// brings back the prompt library and the launch defaults when the database holds a new
// library (from the database set aside, or else from the copies kept beside it), the
// Autopilot routines, switched off, when it holds none (from the same places), and the
// user's own scripts from a database set aside, adds any starter prompt not added before,
// decides once whether the first-run setup shows, and builds every service. It contacts no
// network; the scheduler does that once started.
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

	opened, err := openData(ctx, opts.Paths, logger)
	if err != nil {
		return nil, err
	}

	pool := opened.DB
	reset := dataReset(ctx, opened.Reset, opts.Home, logger)

	if err = profiles.SyncHome(ctx, profiles.NewStore(pool, opts.Clock), opts.Home, logger); err != nil {
		_ = pool.Close()

		return nil, err
	}

	keeper := newKept(pool, opts, logger)
	restored := keeper.restore(ctx, opened.Reset)
	restored.scripts = restoreScripts(ctx, pool, opts, opened.Reset, logger)

	if err = prompts.Seed(ctx, keeper.prompts); err != nil {
		_ = pool.Close()

		return nil, fmt.Errorf("add the starter prompts: %w", err)
	}

	keeper.keep(ctx, restored)
	adoptSetup(ctx, pool, opts.Clock, logger)
	adoptSections(ctx, pool, opts.Clock, logger)

	info := startup{reset: reset, restored: restored.info(opts.Home, opened.Reset), kept: keeper}

	c := assemble(ctx, pool, info, opts, logger)
	c.restoreBudget(ctx, logger)

	return c, nil
}

// Services returns the services the frontend binds to, in a stable order.
func (c *Core) Services() []any {
	return []any{
		c.Settings, c.Profiles, c.Prompts, c.LaunchDefaults, c.Inbox, c.Launcher, c.Worktrees, c.Notifications,
		c.AppInfo, c.Budget, c.Sessions, c.Review, c.Issues, c.Avatars, c.Integrations, c.ScriptRuns, c.Release,
		c.Hooks, c.Scripts, c.GHThreads, c.Backup, c.ServiceStatus, c.Setup, c.Updates, c.Autopilot, c.Milestones,
		c.DescribeIt, c.Manifests,
	}
}

// Close forgets the GitHub token held in memory, drops the banners of failed runs still
// gathered (their notifications are kept), and closes the database.
func (c *Core) Close() error {
	c.tokens.Clear()

	if c.dispatcher != nil {
		c.dispatcher.Close()
	}

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

// startup is what Bootstrap learned before the services were built.
type startup struct {
	// reset is set when the database was set aside, and restored when the prompt library
	// and launch defaults were brought back.
	reset    *appinfo.DataReset
	restored *appinfo.Restored

	// kept holds the stores whose data is kept outside the database, with their copies.
	kept *kept
}

// dataReset logs a database set aside at startup, as one that could not be used or one
// the user asked to start over from, and returns what the window tells the user about it,
// or nil when there was none.
func dataReset(ctx context.Context, reset *store.Reset, home string, logger *slog.Logger) *appinfo.DataReset {
	if reset == nil {
		return nil
	}

	if reset.Reason == store.ResetRequested {
		logger.InfoContext(ctx, "the database was set aside and a fresh one started, as the user asked",
			"path", reset.Path)
	} else {
		logger.WarnContext(ctx, "the database could not be used; it was set aside and a fresh one started",
			"path", reset.Path, "reason", reset.Reason, "error", reset.Err)
	}

	return &appinfo.DataReset{Path: paths.Tilde(reset.Path, home), Reason: appinfo.ResetReason(reset.Reason)}
}

// assemble builds every service over the open database's connection pool. info says
// what happened at startup, which the window is told, and holds the stores whose data is
// kept outside the database, with their copies.
func assemble(ctx context.Context, pool *sql.DB, info startup, opts Options, logger *slog.Logger) *Core {
	// Describe it's worker hears, through every service's events, each change of what its
	// snapshot holds: the routines, Autopilot's settings, the rules, and the inbox. It is
	// built last, once what it reads is, and the waker tells it from then on.
	describe := &describeWaker{}
	opts.Events = describeOnChanges{sink: opts.Events, changed: describe}

	shellEnv := shellenv.New(shellenv.Config{Start: opts.ShellStarter, Home: opts.Home, Logger: logger})
	search := execpath.Search{Home: opts.Home}

	runner := opts.Runner
	gitRunner := opts.Runner

	versionRunner := runner
	agentRunners := func([]string) execpath.Runner { return opts.Runner }
	toolRunners := func([]string) execpath.Runner { return opts.Runner }
	// gitWith is gitRunner with an environment of its own, for the worktree commands that
	// run under gitread's guards, each by its Setup's absolute path.
	gitWith := func([]string) execpath.Runner { return opts.Runner }

	if runner == nil {
		runner, gitRunner, versionRunner = execpath.Exec{Search: search}, worktreeRunner(search), agentRunner{search: search}
		agentRunners = func(env []string) execpath.Runner { return agentRunner{search: search, env: env} }
		toolRunners = nil // the integrations run each tool by its path, with the shell's environment
		gitWith = func(env []string) execpath.Runner { return execpath.Exec{Search: search, Env: env} }
	}

	profileStore := profiles.NewStore(pool, opts.Clock)
	promptStore := info.kept.prompts
	defaultsStore := info.kept.defaults
	inboxStore := inbox.NewStore(pool, opts.Clock)
	followed := &followSwitch{sink: event.Or(opts.Events)}
	// One reader bounds every read of the other repository locations, whichever service
	// asks, so a location on a share that hangs is skipped, and read once at a time.
	locations := localrepos.NewReader(localrepos.DefaultReadLimit)
	prefs := settings.NewService(pool, profileStore, opts.Home, settings.WithClock(opts.Clock),
		settings.WithEvents(followed), settings.WithLogger(logger), settings.WithGit(runner), settings.WithReader(locations))
	tokens := github.NewTokenSource(runner, search,
		github.WithTokenClock(opts.Clock), github.WithHostsFile(github.GHHostsFile(opts.Home)))
	// One tracker hears every response the shared client gets, whichever service asked.
	tracker := budget.NewTracker(opts.Clock, opts.Events)
	keeper := newBudgetKeeper(pool, opts.Clock)
	client := github.NewClient(opts.Version)
	client.Rates = tracker
	// The same tracker paces the edits the client makes for the user (budget.Writes), and,
	// in a lane of its own, the notification writes of its chore lane.
	client.Writes = tracker
	client.Chores = tracker
	workLog := worklog.New(pool, opts.Clock)

	notices := notify.Config{
		Pool: pool, Poster: opts.Banners, Events: opts.Events, Clock: opts.Clock, Logger: logger,
	}
	// One dispatcher records the inbox's arrivals and the notifications about no item, such
	// as a release that reached GitHub or a run that failed.
	dispatcher := notify.NewDispatcher(notices)

	// The user's GitHub notifications are mirrored through the shared client while the Read
	// rule settings writes is on, as often as the notifications' refresh setting says, never
	// sooner than GitHub asks, and cleared by the rules, through the client's chore lane, once
	// the user turns clearing on or clears one by hand; the panel's clicks mark one done or
	// read through the same lane. What the user finishes from the inbox reaches the
	// mirror as the inbox's outcomes, and a change to the rules through the settings' events.
	ghThreads := ghthreads.NewService(ghthreads.Config{
		Pool: pool, Tokens: tokens, Client: stateNoter{Client: client, store: inboxStore, logger: logger},
		Rules: ghThreadRules{prefs: prefsstore.New(pool, opts.Clock)},
		Holds: ghThreadHolds{settings: prefs}, Clock: opts.Clock, Events: opts.Events, Logger: logger,
	})
	ghThreadsSyncer := ghthreads.NewSyncer(ghThreads, refreshEvery(prefs, settings.RefreshNotifications, logger), logger)
	followed.threadRules = ghThreadsSyncer

	// The routines' searches are read after the inbox's own lists, from the snapshot
	// Autopilot's service keeps, which is filled in once the service is built, below; what
	// the inbox stores of them, outside a whole refresh, is told as a Go-only event.
	watchSource := &autopilotWatches{}
	events := event.Or(opts.Events)
	// Autopilot's engine hears what the inbox stores and sees, and keeps the items its
	// ledger refers to; it is built once its service is, below.
	engine := &engineWaker{}
	followed.autopilot = engine
	// The agent sessions record what came of their items on GitHub; they are followed
	// once the inbox is built, below.
	sessionNotes := &sessionOutcomes{logger: logger}
	// The push watch hears every change to the inbox, so it watches a pull request newly
	// listed at once; it is built once the inbox is, below.
	pushes := &pushWaker{}

	inboxSvc := inbox.NewService(inbox.Config{
		Pool:     pool,
		Tokens:   tokens,
		Client:   client,
		Clock:    opts.Clock,
		Events:   pushesOnInbox{sink: engineOnInbox{sink: opts.Events, engine: engine}, pushes: pushes},
		Arrivals: dispatcher,
		Outcomes: outcomesFanOut{
			threads: ghThreadOutcomes{settler: ghthreads.NewSettler(ghThreads)}, engine: engine, sessions: sessionNotes,
		},
		Usage:   tracker,
		Watches: watchSource,
		Changed: func(ids []int64) {
			events.Emit(event.InboxItemsChanged{IDs: ids})
			engine.ItemsChanged(ids)
		},
		Keep:   itemKeepers(pool, engine),
		Marks:  engine,
		Store:  inboxStore,
		Logger: logger,
	})

	recorder := timing.NewRecorder(timing.Config{Logger: logger, LogsDir: opts.Paths.Logs})

	// A session that stops working has its item read again, one item at a time, so a
	// review, a comment, or a push the agent made shows at once.
	rereads := inbox.NewRereads(inboxSvc, logger)

	registry := terminal.DefaultRegistry(runner)
	follow := followSessions(pool, prefs, opts, logger, runner, registry, agentRunners, rereads,
		backgroundTokens{engine: engine})
	followed.watcher = follow.watcher
	sessionNotes.ledger = follow.ledger

	// clones is the app's one set of clone locks: every job that changes a clone takes
	// its clone's, so a worktree is never made or removed while a run is using it.
	clones := clonelock.New()

	// The clones are read with the git the user's shell finds, as magex runs it, and the
	// git commands that reach a clone's remote or write its files run under the same
	// guards: Release's and Hooks' reads, Repo scripts' branch status and pull, and
	// worktree creation's fetches and checkouts.
	shellGit := gitread.New(gitread.Config{
		Env: func() []string { env, _ := shellEnv.Env(); return env }, Runner: toolRunners,
	})

	// A push made on this Mac to a pull request the inbox lists has it read again at once,
	// through the same Rereads, rather than at the next refresh.
	pushes.watcher = newPushWatcher(pushWatchDeps{
		settings: prefs, store: inboxStore, rereads: rereads, runner: runner, git: shellGit, locations: locations,
		home: opts.Home, logger: logger,
	})

	// The routines' agents run in the background through the launcher's own background
	// plan; their runs count as in use wherever a session would, since the process list
	// leaves the app's own children out. The runner is filled in once the runs are built.
	background := newBackgroundAgents(opts, backgroundDeps{
		ledger: follow.ledger, git: shellGit, shellEnv: shellEnv.Env, touch: workLog, changes: inboxStore,
	}, logger)

	// A worktree's Open in Terminal goes through the terminal the runs open in, which is
	// built once the launcher is, below.
	worktreeTerm := &worktreeTerminal{}

	// The worktrees find git with gitread's own finder, so the git a guarded command runs
	// (its Setup's) is the one every other worktree command runs: one clone, one git build.
	sweeps := worktrees.NewService(worktrees.Config{
		Settings: settingsView[worktrees.SweepSettings]{settings: prefs, part: sweepSettings},
		Git:      gitRunner,
		Finder:   shellGit.GitFinder(),
		Guard:    shellGit,
		GitWith:  gitWith,
		Home:     opts.Home,
		Reader:   locations,
		Clock:    opts.Clock,
		GitHub:   itemStates{tokens: tokens, client: client, inbox: inboxStore, logger: logger},
		Base:     shellGit,
		Terminal: worktreeTerm,
		Reveal:   revealer(runner),
		Records:  workLog,
		Copies:   workLog,
		Procs:    procs.System{Run: runner},
		History:  workLog,
		Agents:   follow.ledger,
		Runs:     background,
		Events:   opts.Events,
		Clones:   clones,
		Logger:   logger,
	})

	// A launch's file check is the inbox's, with what agent runs changed on the item.
	launchChecks := agentFiles{
		files: launchFiles{Service: inboxSvc, routines: inbox.NewRoutines(inboxSvc)}, changes: inboxStore,
	}

	launch := launcher.NewService(launcher.Config{
		Items:      launchItems{Store: inboxStore},
		Settings:   settingsView[launcher.LaunchSettings]{settings: prefs, part: launchSettings},
		Profiles:   profileStore,
		Prompts:    promptStore,
		Defaults:   defaultsStore,
		Git:        runner,
		GH:         runner,
		Finder:     agentFinder(opts, search),
		Registry:   registry,
		Paths:      opts.Paths,
		Home:       opts.Home,
		Clock:      opts.Clock,
		Worktrees:  worktreeMaker{maker: worktrees.NewMaker(sweeps)},
		Sessions:   workLog,
		Ledger:     sessionLedger{ledger: follow.ledger, runs: background},
		Trust:      trusters(opts.Home, logger),
		Files:      launchChecks,
		Reader:     locations,
		RunFolders: background,
		TakeOvers:  background,
		RepoFacts:  repoFacts{store: inboxStore},
		Logger:     logger,
	})
	background.launch, background.maker = launcher.NewBackgrounds(launch), worktreeMaker{maker: worktrees.NewMaker(sweeps)}

	previews := launcher.NewPromptSource(launch)
	// One terminal opens the runs in the user's terminal, and the terminal in a run's folder
	// or a clone, for the runs and for Release alike.
	terminals := terminalRuns{
		launch: opts.Paths.Launch, opener: launcher.NewCommandOpener(launch), clock: opts.Clock, logger: logger,
		shell: shellEnv.Shell,
	}
	worktreeTerm.opener = terminals
	runs := newRuns(pool, opts, clones, terminals, runner, dispatcher, logger)
	scriptRuns := scriptrun.NewService(runs)
	background.runner, background.runs = newAgentRunner(pool, opts, agentRunnerDeps{
		runs: runs, history: scriptRuns, git: shellGit, ledger: follow.ledger, agents: background, engine: engine,
		runner: runner,
	}, logger), runs
	background.engine = func() runEngine {
		if engine.engine == nil {
			return nil
		}

		return engine.engine
	}

	// The app's updates read GitHub's release download hosts through the shared client, no
	// token and no budget, in a release build alone, and a change of what to do when a new
	// version is out wakes their check.
	updateEngine, updateWorker := newUpdates(pool, opts, updateDeps{
		client: client, runs: runs, history: scriptRuns, dispatcher: dispatcher, reveal: revealer(runner),
	}, logger)
	followed.updates = updateWorker

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

	releases := release.NewService(release.Config{
		GitHub:  releaseGitHub{tokens: tokens, client: client},
		Clones:  releaseClones{settings: prefs, home: opts.Home, git: runner, reader: locations},
		Checker: release.GitChecker{Git: shellGit},
		Git:     shellGit, Origins: releaseOrigins{git: runner}, Tools: releaseTool{tools: tools}, Locks: clones,
		Env: shellEnv.Env, Ready: shellEnv.Ready, Runner: toolRunners, Runs: runs, Places: releaseSettings{settings: prefs},
		History: scriptRuns, Notifier: releaseNotices{dispatcher: dispatcher},
		// A release on GitHub is followed until its workflow publishes it: each poll is one
		// query through the shared client, and goes into the release's record.
		Workflows: releaseGitHub{tokens: tokens, client: client}, Records: runs,
		// A merge's offer reads the setting, magex, the clones, and the default branch the
		// inbox stored: nothing of GitHub's.
		AfterMerge: releaseSettings{settings: prefs}, Branches: inboxStore,
		// A check's Open in terminal opens the terminal chosen for launches in the clone.
		Terminal: terminals,
		// The last list is kept in the prefs, so the next start shows it at once.
		Keeper: prefsstore.New(pool, opts.Clock), Home: opts.Home, Clock: opts.Clock, Events: opts.Events, Logger: logger,
		// A muted repository is listed apart and never asked about; a pinned one comes first.
		Rules: releaseRules{prefs: prefsstore.New(pool, opts.Clock)},
	})
	// A release left from before a restart is read and verified as one started now.
	runs.RegisterVerifier(scriptrun.KindRelease, release.NewVerifier(releases))
	// The Refresher reads the Release page's rows of a released repository again, and of
	// one a merge in the review sheet went into.
	releaseRefresher := release.NewRefresher(releases)
	// A pinned or muted repository regroups the Release page's list.
	followed.releases = releaseRefresher
	// The Follower follows each release on GitHub to published, and resumes the follows a
	// quit cut off.
	releaseFollower := release.NewFollower(releases)

	deps := hookDeps{
		settings: prefs, tools: tools, git: shellGit, runner: runner, clones: clones, runs: runs, history: scriptRuns,
		shellEnv: shellEnv, terminal: terminals, locations: locations,
	}
	hookPage := newHooks(pool, opts, logger, deps)
	repoScripts := newScripts(pool, opts, logger, deps, inboxStore)
	depsPage := newManifests(pool, opts, logger, deps, inboxStore)

	// What reads the inbox, the running checks, the Release page's running checks, and the
	// worktrees again on its own does so as often as the settings say, and works its next
	// wait out again once they change.
	scheduler := inbox.NewScheduler(inboxSvc, refreshEvery(prefs, settings.RefreshInbox, logger), logger)
	checksFollower := inbox.NewChecksFollower(inboxSvc, refreshEvery(prefs, settings.RefreshChecks, logger), logger)
	releaseChecks := release.NewChecksFollower(releases, refreshEvery(prefs, settings.RefreshReleaseChecks, logger),
		logger)
	sweeper := worktrees.NewSweeper(sweeps, refreshEvery(prefs, settings.RefreshWorktrees, logger))
	// The status pages of the services the user watches are read through the shared client,
	// outside any API, so they cost no budget and carry no token; a part watched going down,
	// or coming back, is told as a notification about no item.
	serviceStatus := svcstatus.NewService(svcstatus.Config{
		Fetcher: client, Rules: statusRules(prefs, logger), Notifier: statusAlerts{dispatcher: dispatcher},
		Clock: opts.Clock, Events: opts.Events, Logger: logger,
	})
	serviceStatusChecker := svcstatus.NewChecker(serviceStatus)
	followed.intervals = []waker{
		scheduler, checksFollower, releaseChecks, sweeper, ghThreadsSyncer, serviceStatusChecker, pushes.watcher,
	}
	// A status page turned on or off, or a part ticked, reaches the check at once.
	followed.statusRules = serviceStatusChecker

	// The review sheet and the issue sheet read and act through the inbox's one Actor, and
	// so do Autopilot's GitHub steps, through them. One avatars service keeps the pictures
	// the sheets show (through the issue sheet's Avatars) and the inbox's cards show (bound
	// itself), so a picture is downloaded, kept, and backed off once for both.
	actor := inbox.NewActor(inboxSvc)
	pictures := avatars.NewService(avatars.Config{Pool: pool, Fetcher: client, Clock: opts.Clock, Logger: logger})
	// The review sheet learns who works on each repository into the people directory, and
	// its reviewer picker suggests from it. Both sheets' assignee pickers offer people from
	// it through one Finder, so a repository's assignable users are read at most once at a
	// time, whichever sheet asks.
	directory := people.NewService(people.Config{Pool: pool, Clock: opts.Clock})
	finder := assignees.NewFinder(assigneesDirectory{directory: directory}, logger)
	reviewSheet := review.NewService(review.Config{
		Pool: pool, Inbox: reviewInbox{store: inboxStore, actor: actor}, GitHub: client,
		Settings: reviewSettings{settings: prefs}, Replies: reviewReplies{replyFiller{store: promptStore, source: previews}},
		Events: opts.Events, Merges: releaseRefresher, People: reviewPeople{directory: directory}, Assignees: finder,
		Clock: opts.Clock, Logger: logger,
	})
	issueSheet := issues.NewService(issues.Config{
		Pool: pool, Inbox: issuesInbox{store: inboxStore, actor: actor}, GitHub: client,
		Replies: issuesReplies{replyFiller{store: promptStore, source: previews}}, Events: opts.Events,
		Pictures: pictures, Assignees: finder, Clock: opts.Clock, Logger: logger,
	})

	budgetSvc := budget.NewService(budget.Config{
		Tracker: tracker, Reader: rateReader{tokens: tokens, client: client}, Clock: opts.Clock,
		RefreshEvery: refreshEvery(prefs, settings.RefreshInbox, logger),
	})

	// The routines belong to the account the inbox last signed in as, start in the mode
	// Settings says, name the starter prompts as the library does, are copied beside the
	// database after every change, and read the inbox for the builder. Their searches'
	// snapshot is built once now, before the scheduler starts, so the first refresh already
	// carries them.
	inboxRoutines := inbox.NewRoutines(inboxSvc)
	// A routine's agent step is launched through the launcher, exactly as a click is, once
	// this Mac has what it needs: its agents, profiles, tools, and terminal, checked through
	// the agents' own commands on the login shell's PATH.
	agents := agentDeps{
		launch: launch, prompts: promptStore, profiles: profileStore, settings: prefs, defaults: defaultsStore,
		inbox: inboxStore, status: serviceStatus, shell: shellEnv, search: search, registry: registry,
		banners: opts.Banners, home: opts.Home, logger: logger, runners: agentRunners, environ: shellEnv.Env,
	}
	readiness := newReadyChecks(agents)
	background.requests = func(ctx context.Context, run autopilot.RunRequest) (launcher.LaunchRequest, error) {
		return routineRequest(ctx, agents, run)
	}
	// The gallery's suggestions, an export's prompts, and an import's profiles read
	// services built after this one, which core fills in below.
	signals := &templateSignals{inbox: inboxStore}
	transfer := &routinePromptTransfer{}
	routineAgents := &routineProfiles{}
	routines := newAutopilot(pool, opts, autopilotDeps{
		inbox: inboxStore, service: inboxRoutines, prompts: promptStore, file: info.kept.routinesFile,
		costs: watchCosts{inbox: inboxRoutines, every: refreshEvery(prefs, settings.RefreshInbox, logger), budget: budgetSvc},
		ready: readiness, agents: agents, background: background, signals: signals, transfer: transfer,
		profiles: routineAgents, scripts: repoScripts,
		github: githubSteps{
			review: reviewSheet, issues: issueSheet, threads: ghThreads, prompts: promptStore, changes: inboxStore,
			replies:   replyFiller{store: promptStore, source: previews},
			autopilot: autopilotPrefs{prefs: prefsstore.New(pool, opts.Clock)},
		},
	}, logger)
	watchSource.searches = autopilot.NewSearches(routines)
	watchSource.searches.Rebuild(ctx)
	// The engine runs the routines, in the one copy of the app that holds the lock in the
	// data folder, while Autopilot (beta) is on, as the build just now read it; it runs as
	// deferred work, and its first cycle waits for a refresh that carried the searches built
	// just now.
	engine.engine = newAutopilotEngine(autopilotEngine{
		store: inboxStore, inbox: inboxSvc, faces: inboxRoutines, tokens: tokens, client: client,
		lock: opts.Paths.AutopilotLock, events: opts.Events, logger: logger, service: routines, notices: dispatcher,
		agents: agents, ready: readiness, defaults: defaultsStore,
		prefs: autopilotPrefs{prefs: prefsstore.New(pool, opts.Clock)}, background: background,
		// A routine's Run one of my scripts runs through Repo scripts' own run, in the app.
		scripts: repoScripts, history: scriptRuns, waker: engine,
	})
	routinePromptUses := routinePrompts{follower: autopilot.NewPromptFollower(routines), logger: logger}
	// A bug report words the settings the services below keep too, so they are wired in
	// once they are built.
	report := &bugReportState{settings: prefs, tools: tools, updates: updateEngine, routines: routines, logger: logger}

	c := &Core{
		DB:         pool,
		Settings:   prefs,
		Milestones: milestones.NewService(pool, milestones.WithClock(opts.Clock), milestones.WithLogger(logger)),
		Profiles:   profiles.NewService(profileStore, opts.Home, search, profiles.WithLogger(logger)),
		Prompts: prompts.NewService(prompts.Config{
			Store: promptStore, Usage: defaultsStore, Source: previews,
			File: info.kept.promptFile, Database: opts.Paths.DB, App: opts.Version, Clock: opts.Clock, Logger: logger,
			Changed: info.kept.saveDefaults, Routines: routinePromptUses, Renamed: routinePromptUses.Renamed,
		}),
		LaunchDefaults: launchdefaults.NewService(launchdefaults.Config{
			Store: defaultsStore, Profiles: profileStore, Prompts: promptStore, Repos: inboxRepos{store: inboxStore},
			Clones: repoScripts, File: info.kept.defaultsFile, Logger: logger,
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
			Sandbox:   opts.Sandbox,
			Finder:    search,
			Runner:    versionRunner,
			Home:      opts.Home,
			State:     report,
		}),
		Budget:                budgetSvc,
		Sessions:              follow.service,
		Review:                reviewSheet,
		Issues:                issueSheet,
		Avatars:               pictures,
		Watcher:               follow.watcher,
		ShellSessions:         ShellSessions{ledger: follow.ledger, service: follow.service},
		ProfileStore:          profileStore,
		Scheduler:             scheduler,
		WatchPasses:           inbox.NewWatchPasses(inboxSvc, logger),
		InboxRoutines:         inboxRoutines,
		GHThreads:             ghThreads,
		GHThreadsSyncer:       ghThreadsSyncer,
		ChecksFollower:        checksFollower,
		MergeFollower:         inbox.NewMergeFollower(inboxSvc, logger),
		Rereads:               rereads,
		PushWatcher:           pushes.watcher,
		Sweeper:               sweeper,
		Timing:                recorder,
		ShellEnv:              shellEnv,
		Deferred:              &Deferred{steps: []func(context.Context){func(ctx context.Context) { _ = shellEnv.Read(ctx) }}},
		Integrations:          tools,
		ToolFinder:            toolFinder,
		Runs:                  runs,
		ScriptRuns:            scriptRuns,
		Release:               releases,
		ReleaseRefresher:      releaseRefresher,
		ReleaseFollower:       releaseFollower,
		ServiceStatus:         serviceStatus,
		ServiceStatusChecker:  serviceStatusChecker,
		ReleaseChecksFollower: releaseChecks,
		Hooks:                 hookPage,
		HooksScans:            hooks.NewScans(hookPage),
		Scripts:               repoScripts,
		Manifests:             depsPage,
		ManifestsScans:        manifests.NewScans(depsPage),
		Updates:               update.NewService(updateEngine),
		Autopilot:             routines,
		AutopilotEngine:       engine.engine,
		ShellAutopilot:        ShellAutopilot{routines: routines, settings: prefs},
		UpdateEngine:          updateEngine,
		UpdateWorker:          updateWorker,
		tokens:                tokens,
		BudgetSaver:           budget.NewSaver(tracker, keeper, logger),
		client:                client,
		budget:                tracker,
		budgetKeeper:          keeper,
		dispatcher:            dispatcher,
	}

	// The backup reaches every category through its owner, so it is built once they are.
	c.Backup = newBackup(pool, c, promptStore, opts, logger)

	// The setup saves a backup before a reset, so it is built once the backup is.
	c.Setup = newSetup(pool, opts, setupDeps{
		tokens: tokens, client: client, info: c.AppInfo, registry: registry, settings: prefs, locations: locations,
		runs: runs, backup: c.Backup,
	}, logger)
	// Check again asks GitHub how the sign-in stands as the setup's own check does.
	readiness.deps.github = c.Setup.GitHub
	// The gallery's suggestions read the setup's look at the clones; an export carries the
	// library's prompts, and an import maps profiles to this Mac's.
	signals.setup = c.Setup
	transfer.prompts = prompts.NewRoutinePrompts(c.Prompts)
	routineAgents.profiles = c.Profiles
	c.ShellSettings = newShellSettings(prefs, c.Setup, pool, opts.Clock)

	// Describe it reads the setup and the inbox through the services above, so it is built
	// once they are: Claude Code as readiness checks it, the routines, Autopilot (beta), which
	// its worker waits for, the update engine's word on where the app runs, and the waker its
	// worker hears the app's changes through.
	c.DescribeIt, c.DescribeItWorker = newDescribeIt(ctx, opts, describeItDeps{
		agents: agents, ready: readiness, routines: routines,
		autopilot: autopilotPrefs{prefs: prefsstore.New(pool, opts.Clock)}, updates: updateEngine, waker: describe,
	}, logger)
	report.prompts, report.launchDefaults, report.profiles, report.describe = c.Prompts, c.LaunchDefaults,
		c.Profiles, c.DescribeIt

	return c
}

// newRuns builds the engine that starts and follows the runs, over the app's one set of
// clone locks, with its logs in <data>/script-runs. Its changes reach the window as
// event.ScriptRunsUpdated, and as event.ToolsUpdated too, since a run starting or ending
// can block or free a tool's update; a run's progress alone, as a release's next step,
// reaches it as event.ScriptRunProgress, which names the run. A run that failed is told to
// the user through dispatcher (run_failed). A run in the terminal opens through terminals,
// and the user's choice to stop the runs and quit reaches the desktop shell as
// event.QuitConfirmed, or to keep running as event.QuitKept.
func newRuns(
	pool *sql.DB, opts Options, clones *clonelock.Set, terminals terminalRuns, runner execpath.Runner,
	dispatcher *notify.Dispatcher, logger *slog.Logger,
) *scriptrun.Engine {
	events := event.Or(opts.Events)

	return scriptrun.NewEngine(scriptrun.Config{
		DB: pool, Locks: clones, LogDir: filepath.Join(opts.Paths.Root, "script-runs"), Home: opts.Home,
		Clock: opts.Clock, Logger: logger, Folders: opts.Paths.AgentRuns,
		AgentLogsFor: agentLogsFor(prefsstore.New(pool, opts.Clock)),
		Changed: func() {
			events.Emit(event.ScriptRunsUpdated{})
			events.Emit(event.ToolsUpdated{})
		},
		Progressed: func(id int64) { events.Emit(event.ScriptRunProgress{ID: id}) },
		Failed:     runFailures{dispatcher: dispatcher}.Failed,
		Terminal:   terminals,
		Reveal:     revealer(runner),
		Quit:       func() { events.Emit(event.QuitConfirmed{}) },
		Kept:       func() { events.Emit(event.QuitKept{}) },
	})
}

// hookDeps are the services and building blocks the Hooks page and Repo scripts share with
// the rest of the app.
type hookDeps struct {
	settings  *settings.Service
	tools     *integrations.Service
	git       *gitread.Git
	runner    execpath.Runner
	clones    *clonelock.Set
	runs      *scriptrun.Engine
	history   *scriptrun.Service
	shellEnv  *shellenv.Reader
	terminal  terminalRuns
	locations *localrepos.Reader
}

// newHooks builds the Hooks page's service: the clones under the clone root, read with the
// same read-only git as Release's, go-pre-commit as the integrations found it, its actions
// and checks run through the runner with the shell's environment, and the hooks.*
// preferences. A row's Open in terminal opens the terminal the runs open in, and its Open
// the config shows the config in Finder, as Show the log file does. Run again works a
// hooks action or a checks run out afresh through its Rerunner, and one left from before a
// restart is read and judged, once the shell's environment is read, through its Verifier.
func newHooks(pool *sql.DB, opts Options, logger *slog.Logger, deps hookDeps) *hooks.Service {
	var runner func([]string, string) execpath.Runner

	if opts.Runner != nil {
		runner = func([]string, string) execpath.Runner { return opts.Runner }
	}

	svc := hooks.NewService(hooks.Config{
		Clones: hooksClones{settings: deps.settings, home: opts.Home, git: deps.runner, reader: deps.locations}, Git: deps.git,
		Tools: hooksTool{tools: deps.tools}, Locks: deps.clones, Runs: deps.runs, History: deps.history,
		Places: integrationSettings{settings: deps.settings}, Memory: prefsstore.New(pool, opts.Clock), Env: deps.shellEnv.Env,
		Ready: deps.shellEnv.Ready, Runner: runner, Terminal: deps.terminal, Reveal: revealer(deps.runner), Home: opts.Home,
		Clock: opts.Clock, Events: opts.Events, Logger: logger,
	})

	rerun := hooks.NewRerunner(svc)
	deps.runs.RegisterRerunner(scriptrun.KindHooks, rerun)
	deps.runs.RegisterRerunner(scriptrun.KindChecks, rerun)

	verify := hooks.NewVerifier(svc)
	deps.runs.RegisterVerifier(scriptrun.KindHooks, verify)
	deps.runs.RegisterVerifier(scriptrun.KindChecks, verify)

	return svc
}

// newScripts builds Repo scripts' service over the same clones, read-only git, runner, and
// shell environment as the Hooks page's: magex as the integrations found it, the settings'
// sources and where runs go, the default branches the inbox stored, and the trusted
// commands in script_trust. The same read-only git reads a clone's branch status, and the
// runner runs its pull. Run again works a script, or a pull, out afresh through its
// Rerunner.
func newScripts(pool *sql.DB, opts Options, logger *slog.Logger, deps hookDeps, branches scripts.DefaultBranches,
) *scripts.Service {
	var runner func([]string, string) execpath.Runner

	if opts.Runner != nil {
		runner = func([]string, string) execpath.Runner { return opts.Runner }
	}

	svc := scripts.NewService(scripts.Config{
		Clones: hooksClones{settings: deps.settings, home: opts.Home, git: deps.runner, reader: deps.locations}, Git: deps.git,
		BranchGit: deps.git, Branches: branches, Tools: scriptsTools{tools: deps.tools},
		Settings: integrationSettings{settings: deps.settings}, Runs: deps.runs, History: deps.history, DB: pool,
		Env: deps.shellEnv.Env, Shell: deps.shellEnv.Shell, Runner: runner, Home: opts.Home, Clock: opts.Clock,
		Logger: logger,
	})

	rerun := scripts.NewRerunner(svc)
	deps.runs.RegisterRerunner(scriptrun.KindScript, rerun)
	deps.runs.RegisterRerunner(scriptrun.KindPull, rerun)
	// A pull left going at a quit comes back with Stop held and its own words.
	deps.runs.RegisterVerifier(scriptrun.KindPull, scripts.NewVerifier(svc))

	return svc
}

// newManifests builds the Deps page's service over the same clones and read-only git as
// the Hooks page's: the pinned and muted repositories Release reads, the default branches
// the inbox stored, the terminal the runs open in for a row's Open in terminal, and Finder
// for a manifest's Reveal in Finder. It reads every clone from its own git objects alone,
// so it needs no runner, lock, or shell environment beyond git's.
func newManifests(pool *sql.DB, opts Options, logger *slog.Logger, deps hookDeps, branches manifests.DefaultBranches,
) *manifests.Service {
	return manifests.NewService(manifests.Config{
		Clones: hooksClones{settings: deps.settings, home: opts.Home, git: deps.runner, reader: deps.locations},
		Git:    deps.git, Rules: releaseRules{prefs: prefsstore.New(pool, opts.Clock)}, Branches: branches,
		Terminal: &worktreeTerminal{opener: deps.terminal}, Reveal: revealer(deps.runner), Home: opts.Home,
		Clock: opts.Clock, Events: opts.Events, Logger: logger,
	})
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
// the session banners, rereader, which reads a session's item again once it stops
// working, the terminal drivers that bring a session's tab forward, and tokens, which
// reads a background run's tokens from its run's record.
func followSessions(
	pool *sql.DB, prefs *settings.Service, opts Options, logger *slog.Logger, runner execpath.Runner,
	registry terminal.Registry, agentRunners func([]string) execpath.Runner, rereader sessions.Rereader,
	tokens sessions.RunTokens,
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
		Rereader:  rereader,
		Events:    opts.Events,
		Clock:     opts.Clock,
		Home:      opts.Home,
		Logger:    logger,
	})

	cfg := sessions.Config{
		Store: store, Watcher: watcher, Switch: prefs, Focuser: terminalFocuser{registry: registry}, PS: runner,
		Reveal: revealer(runner), Home: opts.Home, Events: opts.Events, Clock: opts.Clock, Logger: logger,
		Rereader: rereader,
		Words: map[agent.ID]sessions.WordsReader{
			agent.Claude: claude.Words{Home: opts.Home}, agent.Codex: codex.Words{Home: opts.Home},
		},
		Usage: map[agent.ID]sessions.UsageReader{
			agent.Claude: claude.NewUsage(opts.Home), agent.Codex: codex.Usage{Home: opts.Home},
		},
		RunTokens: tokens,
	}

	return following{ledger: sessions.NewLedger(cfg), watcher: watcher, service: sessions.NewService(cfg)}
}
