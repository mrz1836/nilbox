// Package event defines what happens in the app that another part of it reacts to: one
// Go type per event, the name each is known by, and the Sink that receives them.
//
// Services emit events into the Sink they are given; the desktop shell is the one Sink
// of the running app. A WindowEvent is passed on to the frontend under its Name, with
// the event itself as the data, and the desktop bootstrap registers each one's type
// under that name, so the generated bindings type the frontend's listeners. The other
// events stay in Go: the shell acts on them itself.
package event

import (
	"sync/atomic"
	"time"

	"github.com/mrz1836/nilbox/internal/timing"
)

// The events' names. A window event's name is part of the frontend's contract: the
// generated bindings key its type by it, and the frontend listens for it.
const (
	// NameInboxUpdated is InboxUpdated's name.
	NameInboxUpdated = "inbox:updated"

	// NameRevealItem is RevealItem's name.
	NameRevealItem = "inbox:reveal"

	// NameWorktreesUpdated is WorktreesUpdated's name.
	NameWorktreesUpdated = "worktrees:updated"

	// NameNotificationsUpdated is NotificationsUpdated's name.
	NameNotificationsUpdated = "notifications:updated"

	// NameBudgetUpdated is BudgetUpdated's name.
	NameBudgetUpdated = "budget:updated"

	// NameSessionsUpdated is SessionsUpdated's name.
	NameSessionsUpdated = "sessions:updated"

	// NameReviewProgress is ReviewProgress's name.
	NameReviewProgress = "review:progress"

	// NameIssueProgress is IssueProgress's name.
	NameIssueProgress = "issue:progress"

	// NameThemeChanged is ThemeChanged's name.
	NameThemeChanged = "settings:theme"

	// NameTextSizeChanged is TextSizeChanged's name.
	NameTextSizeChanged = "settings:textSize"

	// NameMenuBarChanged is MenuBarChanged's name.
	NameMenuBarChanged = "settings:menuBar"

	// NameFirstPaint is FirstPaint's name.
	NameFirstPaint = "app:firstPaint"

	// NameToolsUpdated is ToolsUpdated's name.
	NameToolsUpdated = "tools:updated"

	// NameIntegrationsChanged is IntegrationsChanged's name.
	NameIntegrationsChanged = "integrations:changed"

	// NameScriptRunsUpdated is ScriptRunsUpdated's name.
	NameScriptRunsUpdated = "scriptruns:updated"

	// NameScriptRunProgress is ScriptRunProgress's name.
	NameScriptRunProgress = "scriptruns:progress"

	// NameQuitRequested is QuitRequested's name.
	NameQuitRequested = "app:quitRequested"

	// NameQuitConfirmed is QuitConfirmed's name.
	NameQuitConfirmed = "app:quitConfirmed"

	// NameQuitKept is QuitKept's name.
	NameQuitKept = "app:quitKept"

	// NameReleasesUpdated is ReleasesUpdated's name.
	NameReleasesUpdated = "releases:updated"

	// NameReleasesProgress is ReleasesProgress's name.
	NameReleasesProgress = "releases:progress"

	// NameHooksUpdated is HooksUpdated's name.
	NameHooksUpdated = "hooks:updated"

	// NameWorktreesProgress is WorktreesProgress's name.
	NameWorktreesProgress = "worktrees:progress"

	// NameIntervalsChanged is IntervalsChanged's name.
	NameIntervalsChanged = "settings:intervalsChanged"

	// NameRepoRulesChanged is RepoRulesChanged's name.
	NameRepoRulesChanged = "settings:repoRulesChanged"

	// NameThreadRulesChanged is ThreadRulesChanged's name.
	NameThreadRulesChanged = "settings:threadRulesChanged"

	// NameStatusRulesChanged is StatusRulesChanged's name.
	NameStatusRulesChanged = "settings:statusRulesChanged"

	// NameGHThreadsUpdated is GHThreadsUpdated's name.
	NameGHThreadsUpdated = "ghthreads:updated"

	// NameServiceStatusUpdated is ServiceStatusUpdated's name.
	NameServiceStatusUpdated = "servicestatus:updated"

	// NameSetupFinished is SetupFinished's name.
	NameSetupFinished = "setup:finished"

	// NameResetRequested is ResetRequested's name.
	NameResetRequested = "app:resetRequested"

	// NameUpdatesChanged is UpdatesChanged's name.
	NameUpdatesChanged = "settings:updatesChanged"

	// NameAutopilotChanged is AutopilotChanged's name.
	NameAutopilotChanged = "settings:autopilotChanged"

	// NameInboxItemsChanged is InboxItemsChanged's name.
	NameInboxItemsChanged = "inbox:itemsChanged"

	// NameAutopilotUpdated is AutopilotUpdated's name.
	NameAutopilotUpdated = "autopilot:updated"

	// NameAutopilotRun is AutopilotRun's name.
	NameAutopilotRun = "autopilot:run"

	// NameUpdatesUpdated is UpdatesUpdated's name.
	NameUpdatesUpdated = "updates:updated"

	// NameUpdateRestartRequested is UpdateRestartRequested's name.
	NameUpdateRestartRequested = "updates:restart"

	// NameUpdateRestartFailed is UpdateRestartFailed's name.
	NameUpdateRestartFailed = "updates:restartFailed"

	// NameUpdateQuitHeld is UpdateQuitHeld's name.
	NameUpdateQuitHeld = "updates:quitHeld"

	// NameKeptRunning is KeptRunning's name.
	NameKeptRunning = "app:keptRunning"
)

// Event is something that happened, or was asked for, that another part of the app
// reacts to.
type Event interface {
	// Name is the name the event is known by.
	Name() string
}

// WindowEvent is an event the frontend hears too. Only this package's types are window
// events, so the set the desktop bootstrap registers is the set WindowEvents returns.
type WindowEvent interface {
	Event

	// window marks a window event.
	window()
}

// Sink receives events. Emit must return promptly and must not call back into the
// service that emitted: services emit while they hold their own locks.
type Sink interface {
	Emit(e Event)
}

// Or returns s, or Discard when s is nil, so a service given no Sink can still emit.
func Or(s Sink) Sink {
	if s == nil {
		return Discard{}
	}

	return s
}

// WindowEvents returns one of each window event. The repository's checks compare it with
// the events the desktop bootstrap registers.
func WindowEvents() []WindowEvent {
	return []WindowEvent{
		InboxUpdated{},
		RevealItem{},
		WorktreesUpdated{},
		NotificationsUpdated{},
		BudgetUpdated{},
		SessionsUpdated{},
		ReviewProgress{},
		IssueProgress{},
		ToolsUpdated{},
		ScriptRunsUpdated{},
		ScriptRunProgress{},
		QuitRequested{},
		ReleasesUpdated{},
		ReleasesProgress{},
		HooksUpdated{},
		WorktreesProgress{},
		GHThreadsUpdated{},
		ServiceStatusUpdated{},
		UpdatesUpdated{},
		UpdateRestartRequested{},
		AutopilotUpdated{},
		AutopilotRun{},
		KeptRunning{},
	}
}

// InboxUpdated says the stored inbox, or the status of its last refresh, changed: the
// window reads the inbox again, and the menu bar redraws.
type InboxUpdated struct{}

// Name returns NameInboxUpdated.
func (InboxUpdated) Name() string { return NameInboxUpdated }

func (InboxUpdated) window() {}

// RevealItem asks for the window to come forward with an item in view, for a click on
// the item in the menu bar or on its notification, or with the run a notification is about
// open in Scripts → Runs, or with what an Autopilot notification is about. A click on a
// banner also names the notification it stands for, which the window marks read. A
// request that names no item, URL, run, Autopilot target, or notification, such as a click
// on a banner that stands for many items, only brings the window forward; the frontend is
// not told.
type RevealItem struct {
	// ItemID is the stored item to show, or 0 for none.
	ItemID int64 `json:"itemId"`

	// URL is the item on GitHub, which the window opens when the item is no longer in
	// the inbox, or the page of a notification about no item.
	URL string `json:"url"`

	// RunID is the run to open, which a failed run's notification names, or 0 for none, and
	// RunStarted when it started, as RFC 3339: the window opens the run only while the run
	// with that ID started then.
	RunID      int64  `json:"runId"`
	RunStarted string `json:"runStarted"`

	// NotificationID is the notification a clicked banner stands for, which the window
	// marks read, or 0 for none: a summary's, a session's, the menu bar's, or a session's
	// focus. The window marks it only while the log's entry with that ID is about this
	// item, URL, run, or Autopilot target, since the log hands an ID out again once it is
	// cleared.
	NotificationID int64 `json:"notificationId"`

	// Autopilot is what an Autopilot notification opens in the Autopilot tab, or the zero
	// target for none.
	Autopilot AutopilotTarget `json:"autopilot"`
}

// Name returns NameRevealItem.
func (RevealItem) Name() string { return NameRevealItem }

// Named reports whether the request names anything for the window to show or mark: an
// item, a URL, a run, a notification, or an Autopilot target.
func (r RevealItem) Named() bool {
	return r.ItemID != 0 || r.URL != "" || r.RunID != 0 || r.NotificationID != 0 || !r.Autopilot.IsZero()
}

func (RevealItem) window() {}

// AutopilotTarget is what an Autopilot notification opens in the Autopilot tab: its
// routine, and the match (its card in Activity) or the agent run (the run view) when it
// names one. CreatedAt is when the most specific of them was created, as RFC 3339 (the
// run's, else the match's, else the routine's), which tells it from a later one given its
// ID, since Clear history hands IDs out again. Activity asks for the tab's Activity, at
// the matches waiting for the user, as the menu bar's row of them does; no notification
// names it. The zero target names nothing.
type AutopilotTarget struct {
	RoutineID int64  `json:"routineId"`
	MatchID   int64  `json:"matchId"`
	RunID     int64  `json:"runId"`
	CreatedAt string `json:"createdAt"`
	Activity  bool   `json:"activity"`
}

// IsZero reports a target that names nothing.
func (t AutopilotTarget) IsZero() bool {
	return t.RoutineID == 0 && t.MatchID == 0 && t.RunID == 0 && !t.Activity
}

// WorktreesUpdated says an automatic worktree sweep removed a worktree or changed how many
// need attention: the window reads the latest sweep.
type WorktreesUpdated struct{}

// Name returns NameWorktreesUpdated.
func (WorktreesUpdated) Name() string { return NameWorktreesUpdated }

func (WorktreesUpdated) window() {}

// NotificationsUpdated says the notification log changed: the window reads it again.
type NotificationsUpdated struct{}

// Name returns NameNotificationsUpdated.
func (NotificationsUpdated) Name() string { return NameNotificationsUpdated }

func (NotificationsUpdated) window() {}

// BudgetUpdated says a GitHub API budget changed, as a response reported it, or what the
// app spent from one: the window reads the budgets again.
type BudgetUpdated struct{}

// Name returns NameBudgetUpdated.
func (BudgetUpdated) Name() string { return NameBudgetUpdated }

func (BudgetUpdated) window() {}

// SessionsUpdated says an agent session was launched, or changed what it is doing: the
// window reads the sessions again, and the menu bar redraws.
type SessionsUpdated struct{}

// Name returns NameSessionsUpdated.
func (SessionsUpdated) Name() string { return NameSessionsUpdated }

func (SessionsUpdated) window() {}

// ReviewProgress says what the review sheet's action on a pull request is doing now, as
// in "Approving" or "Checking that GitHub can merge it", so the sheet can show each step
// while the user waits for GitHub.
type ReviewProgress struct {
	// ItemID is the stored pull request the action is on.
	ItemID int64 `json:"itemId"`

	// Step says what the action is doing now.
	Step string `json:"step"`
}

// Name returns NameReviewProgress.
func (ReviewProgress) Name() string { return NameReviewProgress }

func (ReviewProgress) window() {}

// IssueProgress says what the issue sheet's action on an issue is doing now, as in
// "Checking for new comments since you opened it" or "Closing as completed", so the sheet
// can show each step while the user waits for GitHub.
type IssueProgress struct {
	// ItemID is the stored issue the action is on.
	ItemID int64 `json:"itemId"`

	// Step says what the action is doing now.
	Step string `json:"step"`
}

// Name returns NameIssueProgress.
func (IssueProgress) Name() string { return NameIssueProgress }

func (IssueProgress) window() {}

// ThemeChanged says the user chose a theme and it was stored. The shell brings the
// window's native appearance along with it; the page has already switched itself.
type ThemeChanged struct {
	// Theme is the stored theme: one of the settings package's theme choices.
	Theme string
}

// Name returns NameThemeChanged.
func (ThemeChanged) Name() string { return NameThemeChanged }

// TextSizeChanged says the user chose a text size and it was stored. The shell sets the
// window's smallest size to suit it; the page shows the size itself.
type TextSizeChanged struct {
	// Size is the stored text size: one of the settings package's text size choices.
	Size string
}

// Name returns NameTextSizeChanged.
func (TextSizeChanged) Name() string { return NameTextSizeChanged }

// MenuBarChanged says the user turned the menu bar icon on or off and it was stored.
type MenuBarChanged struct {
	// On is whether the icon shows.
	On bool
}

// Name returns NameMenuBarChanged.
func (MenuBarChanged) Name() string { return NameMenuBarChanged }

// FirstPaint says the window painted its first frame. It comes once per run, and ends a
// startup measurement.
type FirstPaint struct {
	// Measurement is the time from process start to the first paint.
	Measurement timing.Measurement
}

// Name returns NameFirstPaint.
func (FirstPaint) Name() string { return NameFirstPaint }

// ToolsUpdated says what the integrations service lists changed: a detection or an update
// check finished, or a run started or ended, which can block or free a tool's update.
// The window reads the list again.
type ToolsUpdated struct{}

// Name returns NameToolsUpdated.
func (ToolsUpdated) Name() string { return NameToolsUpdated }

func (ToolsUpdated) window() {}

// IntegrationsChanged says the user flipped an integration's switch or chose a copy of a
// tool, and it was stored. Core has the integrations look for the tools again when a copy
// was chosen, and otherwise tells the window that what they list changed.
type IntegrationsChanged struct {
	// CopyChosen reports a chosen copy of a tool, rather than a switch.
	CopyChosen bool
}

// Name returns NameIntegrationsChanged.
func (IntegrationsChanged) Name() string { return NameIntegrationsChanged }

// IntervalsChanged says the user changed how often the app reads something again on its
// own (the inbox, the running checks, the Release page's running checks, the worktrees,
// the GitHub notifications, the status pages), and it was stored. It stays in Go: core wakes what reads them
// again, which works its next wait out from the new interval.
type IntervalsChanged struct{}

// Name returns NameIntervalsChanged.
func (IntervalsChanged) Name() string { return NameIntervalsChanged }

// RepoRulesChanged says the user pinned or muted a repository or an org, or took the rule
// off, and it was stored. It stays in Go: core tells the window the inbox changed, which
// the menu bar shows too, and has the Release page regroup its list.
type RepoRulesChanged struct{}

// Name returns NameRepoRulesChanged.
func (RepoRulesChanged) Name() string { return NameRepoRulesChanged }

// ThreadRulesChanged says the user changed a rule for their GitHub notifications (whether
// they are read, whether, how, and when they are cleared), and it was stored. It stays in
// Go: core has the mirror of the notifications decide every thread again at once, so
// clearing turned off stops at once, and turned on clears what is finished.
type ThreadRulesChanged struct{}

// Name returns NameThreadRulesChanged.
func (ThreadRulesChanged) Name() string { return NameThreadRulesChanged }

// StatusRulesChanged says the user turned a status page on or off, or ticked or unticked
// a part of one to watch, and it was stored. It stays in Go: core has the service status
// check follow it, forgetting a provider turned off and checking one turned on at once;
// the window has the settings from the setter already, and hears the status changed.
type StatusRulesChanged struct{}

// Name returns NameStatusRulesChanged.
func (StatusRulesChanged) Name() string { return NameStatusRulesChanged }

// UpdatesChanged says the user chose what the app does when a new version is out, and it
// was stored. It stays in Go: core wakes the update check, which follows the new choice at
// once (a version found while the app only told is downloaded once the user asks for
// downloads); the window has the settings from the setter, and hears the update's status
// changed.
type UpdatesChanged struct{}

// Name returns NameUpdatesChanged.
func (UpdatesChanged) Name() string { return NameUpdatesChanged }

// AutopilotChanged says the user changed one of Autopilot's settings (Pause Autopilot,
// the mode new routines start in, a limit, the daily cap, or how long the history is
// kept), and it was stored. It stays in Go, for what follows the settings to read them
// again; the window has the settings from the setter.
type AutopilotChanged struct{}

// Name returns NameAutopilotChanged.
func (AutopilotChanged) Name() string { return NameAutopilotChanged }

// InboxItemsChanged says the inbox stored items outside a whole refresh: what the
// routines' searches watch, a round of the checks follower, or an item's own read or an
// action on it. It stays in Go, for what acts on the items to read them again; the window
// hears the inbox's own changes as InboxUpdated.
type InboxItemsChanged struct {
	// IDs are the items stored, each once, in order.
	IDs []int64
}

// Name returns NameInboxItemsChanged.
func (InboxItemsChanged) Name() string { return NameInboxItemsChanged }

// AutopilotUpdated says Autopilot changed: its routines, what they matched and ran, or
// its engine (a cycle ended, or this copy took or lost the engine). It comes at most four
// times a second; the window reads what it shows again.
type AutopilotUpdated struct{}

// Name returns NameAutopilotUpdated.
func (AutopilotUpdated) Name() string { return NameAutopilotUpdated }

func (AutopilotUpdated) window() {}

// AutopilotRun says an agent's background run has new events, as its stream grows: the
// window asks for its events after the last it has (autopilot.Service.RunEvents), so the
// event never carries a log. It comes at most four times a second for a run.
type AutopilotRun struct {
	// RunID is the ledger's run.
	RunID int64 `json:"runId"`
}

// Name returns NameAutopilotRun.
func (AutopilotRun) Name() string { return NameAutopilotRun }

func (AutopilotRun) window() {}

// UpdatesUpdated says the app's update changed: a check started or ended, a download got
// further (a few times a second at most), a version is ready, a restart began or failed,
// or Later was chosen or lifted. The window reads the update's status again, and the menu
// bar redraws.
type UpdatesUpdated struct{}

// Name returns NameUpdatesUpdated.
func (UpdatesUpdated) Name() string { return NameUpdatesUpdated }

func (UpdatesUpdated) window() {}

// UpdateRestartRequested asks the window to restart to update, as the menu bar's Restart
// to Update does: every restart starts in the window, which asks first when restarting
// would lose something.
type UpdateRestartRequested struct{}

// Name returns NameUpdateRestartRequested.
func (UpdateRestartRequested) Name() string { return NameUpdateRestartRequested }

func (UpdateRestartRequested) window() {}

// UpdateRestartFailed says a restart to update failed after its QuitConfirmed let the
// quit through, and the app runs on. It stays in Go: the shell holds a quit while runs go
// again.
type UpdateRestartFailed struct{}

// Name returns NameUpdateRestartFailed.
func (UpdateRestartFailed) Name() string { return NameUpdateRestartFailed }

// UpdateQuitHeld says a restart to update staged the update and started the runtime's
// helper, but the quit that should have ended the app was held (a modal panel, say), so the
// app runs on while the helper waits for it to exit. It stays in Go: the shell asks to quit
// again a few times and, failing that, shuts down in order and exits, so the helper
// installs the update instead of giving up and opening this copy again beside it.
type UpdateQuitHeld struct {
	// HelperStarted is when the restart began starting the helper, just before the
	// runtime's restart was called: the helper's wait for the app to exit starts soon
	// after, so the shell times its exit from here, never from when this arrived, which a
	// quit slow to return delays.
	HelperStarted time.Time
}

// Name returns NameUpdateQuitHeld.
func (UpdateQuitHeld) Name() string { return NameUpdateQuitHeld }

// ScriptRunsUpdated says a run started, ended, or changed status, or the history changed:
// the window lists the runs again and pulls what it shows. It never carries output.
type ScriptRunsUpdated struct{}

// Name returns NameScriptRunsUpdated.
func (ScriptRunsUpdated) Name() string { return NameScriptRunsUpdated }

func (ScriptRunsUpdated) window() {}

// ScriptRunProgress says one run going now changed its progress (scriptrun.Progress): it
// reached a step, or Stop turned on or off. The window reads that run again, and only that
// one; the history is as it was. It never carries output.
type ScriptRunProgress struct {
	// ID is the run's ID.
	ID int64 `json:"id"`
}

// Name returns NameScriptRunProgress.
func (ScriptRunProgress) Name() string { return NameScriptRunProgress }

func (ScriptRunProgress) window() {}

// QuitRequested says the user asked to quit while runs are going in the app: the window
// asks whether to stop them and quit, or keep running.
type QuitRequested struct{}

// Name returns NameQuitRequested.
func (QuitRequested) Name() string { return NameQuitRequested }

func (QuitRequested) window() {}

// KeptRunning says the window shows again after a close only hid it, the menu bar icon
// off, because Autopilot has a routine on or an agent working: the window says, once, that
// the app kept running and how to bring it back or quit.
type KeptRunning struct{}

// Name returns NameKeptRunning.
func (KeptRunning) Name() string { return NameKeptRunning }

func (KeptRunning) window() {}

// QuitConfirmed says the user chose to stop the runs and quit. The desktop shell hears it,
// lets the next quit through, and quits; shutdown stops the runs.
//
// A restart to update sends it with Restarting set, just before the update's installer
// quits the app itself: the shell then lets every quit through, whatever runs go, but
// quits nothing, since a quit of its own could come before the installer's helper stands
// by. A held quit would leave that helper to open this copy again beside the new one.
type QuitConfirmed struct {
	// Restarting marks the restart to update's confirmation, which quits nothing itself.
	Restarting bool
}

// Name returns NameQuitConfirmed.
func (QuitConfirmed) Name() string { return NameQuitConfirmed }

// QuitKept says the user chose to keep the app running when its quit asked first (Keep
// running). It stays in Go: the desktop shell forgets the copy a move to Applications asked
// to open once the app quits, so a later quit opens nothing.
type QuitKept struct{}

// Name returns NameQuitKept.
func (QuitKept) Name() string { return NameQuitKept }

// ReleasesUpdated says the Release page's list changed: it was read again from GitHub, in
// full or, after a release ended, for the repository released alone; a release ended, and
// its repository is being read again, so the list reads as refreshing until that lands; or
// the list went out of date. The window reads it, and a list out of date is read again from
// GitHub then.
type ReleasesUpdated struct{}

// Name returns NameReleasesUpdated.
func (ReleasesUpdated) Name() string { return NameReleasesUpdated }

func (ReleasesUpdated) window() {}

// ReleasesPhase is how far a read of the Release page's list has got.
type ReleasesPhase string

// The phases of a read of the Release page's list, in order.
const (
	// ReleasesFindingClones is the clone root being searched for clones.
	ReleasesFindingClones ReleasesPhase = "clones"

	// ReleasesAskingGitHub is GitHub being asked about each clone's repository.
	ReleasesAskingGitHub ReleasesPhase = "github"

	// ReleasesComparing is GitHub being asked how far each default branch is ahead of its
	// newest version.
	ReleasesComparing ReleasesPhase = "compare"

	// ReleasesCheckingClones is the unreleased clones being read.
	ReleasesCheckingClones ReleasesPhase = "checking"
)

// ReleasesProgress says how far a read of the Release page's list has got, while it runs:
// its phase, and how many of the phase's repositories or clones are done. Finding the
// clones counts nothing, and the read of a released repository alone finds none. It comes
// a few times a second at most, and ReleasesUpdated follows once the read lands.
type ReleasesProgress struct {
	Phase ReleasesPhase `json:"phase"`
	Done  int           `json:"done"`
	Total int           `json:"total"`
}

// Name returns NameReleasesProgress.
func (ReleasesProgress) Name() string { return NameReleasesProgress }

func (ReleasesProgress) window() {}

// HooksUpdated says the Hooks page's list changed: a clone was scanned again, or a hooks
// action or a checks run ended. The window reads the list again, which serves what is kept
// and scans again only the clones that changed.
type HooksUpdated struct{}

// Name returns NameHooksUpdated.
func (HooksUpdated) Name() string { return NameHooksUpdated }

func (HooksUpdated) window() {}

// WorktreesPhase is how far a listing of the worktrees has got.
type WorktreesPhase string

// The phases of a listing of the worktrees, in order.
const (
	// WorktreesFindingClones is the clone root being searched for clones.
	WorktreesFindingClones WorktreesPhase = "clones"

	// WorktreesListing is each clone's worktrees being listed.
	WorktreesListing WorktreesPhase = "listing"

	// WorktreesChecking is each worktree being read: its changes, its branch, and its
	// commits.
	WorktreesChecking WorktreesPhase = "checking"

	// WorktreesAskingGitHub is GitHub being asked about the pull requests and issues the
	// worktrees belong to.
	WorktreesAskingGitHub WorktreesPhase = "github"
)

// WorktreesProgress says how far the window's listing of the worktrees has got, while it
// runs: its phase, and how many of the phase's clones, worktrees, or pull requests and
// issues are done. Finding the clones counts nothing. It comes a few times a second at
// most, and the listing's own answer follows it; a listing another job runs, such as the
// sweep, tells nothing.
type WorktreesProgress struct {
	Phase WorktreesPhase `json:"phase"`
	Done  int            `json:"done"`
	Total int            `json:"total"`
}

// Name returns NameWorktreesProgress.
func (WorktreesProgress) Name() string { return NameWorktreesProgress }

func (WorktreesProgress) window() {}

// GHThreadsUpdated says what the window shows of the user's GitHub notifications changed:
// a pass read them, or checked their items' states, and found something new; reading them
// was turned off; or whether GitHub lets the token read them, or the last pass's error,
// changed. The window reads them again. A pass that changed nothing tells nothing.
type GHThreadsUpdated struct{}

// Name returns NameGHThreadsUpdated.
func (GHThreadsUpdated) Name() string { return NameGHThreadsUpdated }

func (GHThreadsUpdated) window() {}

// ServiceStatusUpdated says a pass of the service status check ended, whether its checks
// worked or not, or what the user watches changed: the window reads the status again.
type ServiceStatusUpdated struct{}

// Name returns NameServiceStatusUpdated.
func (ServiceStatusUpdated) Name() string { return NameServiceStatusUpdated }

func (ServiceStatusUpdated) window() {}

// SetupFinished says the user finished the first-run setup, or skipped it. The desktop
// shell hears it and asks once to allow notification banners, which it held while the setup
// showed, so the question comes when the user lands in the inbox, not before they know what
// the app does.
type SetupFinished struct{}

// Name returns NameSetupFinished.
func (SetupFinished) Name() string { return NameSetupFinished }

// ResetRequested says the user asked to start over, and the request is written: the
// desktop shell lets the quit through, quits, and opens the app again, whose start carries
// the reset out.
type ResetRequested struct{}

// Name returns NameResetRequested.
func (ResetRequested) Name() string { return NameResetRequested }

// Discard is a Sink that drops every event.
type Discard struct{}

// Emit drops e.
func (Discard) Emit(Event) {}

// Relay is a Sink that passes each event on to the Sink it is connected to, and drops
// events until it is. It lets the services that emit be built before the Sink that hears
// them, as the desktop shell is, which needs those services. It is safe for concurrent
// use; the zero value is ready.
type Relay struct {
	to atomic.Pointer[connection]
}

// connection holds the Sink a Relay passes events on to.
type connection struct {
	sink Sink
}

// Connect passes every later event on to s.
func (r *Relay) Connect(s Sink) {
	r.to.Store(&connection{sink: s})
}

// Emit passes e on, or drops it when nothing is connected yet.
func (r *Relay) Emit(e Event) {
	if c := r.to.Load(); c != nil {
		c.sink.Emit(e)
	}
}
