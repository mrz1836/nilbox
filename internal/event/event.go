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

	// NameReleasesUpdated is ReleasesUpdated's name.
	NameReleasesUpdated = "releases:updated"
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
		ToolsUpdated{},
		ScriptRunsUpdated{},
		ScriptRunProgress{},
		QuitRequested{},
		ReleasesUpdated{},
	}
}

// InboxUpdated says the stored inbox, or the status of its last refresh, changed: the
// window reads the inbox again, and the menu bar redraws.
type InboxUpdated struct{}

// Name returns NameInboxUpdated.
func (InboxUpdated) Name() string { return NameInboxUpdated }

func (InboxUpdated) window() {}

// RevealItem asks for the window to come forward with an item in view, for a click on
// the item in the menu bar or on its notification. A request that names neither an item
// nor a URL, such as a click on a banner that stands for many items, only brings the
// window forward; the frontend is not told.
type RevealItem struct {
	// ItemID is the stored item to show, or 0 for none.
	ItemID int64 `json:"itemId"`

	// URL is the item on GitHub, which the window opens when the item is no longer in
	// the inbox.
	URL string `json:"url"`
}

// Name returns NameRevealItem.
func (RevealItem) Name() string { return NameRevealItem }

func (RevealItem) window() {}

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

// QuitConfirmed says the user chose to stop the runs and quit. The desktop shell hears it,
// lets the next quit through, and quits; shutdown stops the runs.
type QuitConfirmed struct{}

// Name returns NameQuitConfirmed.
func (QuitConfirmed) Name() string { return NameQuitConfirmed }

// ReleasesUpdated says the Release page's list was read again from GitHub, or went out of
// date, as when a release ended: the window reads it, and a list out of date is read again
// from GitHub then.
type ReleasesUpdated struct{}

// Name returns NameReleasesUpdated.
func (ReleasesUpdated) Name() string { return NameReleasesUpdated }

func (ReleasesUpdated) window() {}

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
