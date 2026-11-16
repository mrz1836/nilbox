package core

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/periodic"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/sessions"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/setup"
)

// ShellSettings are the settings the desktop shell reads when the app launches: the theme,
// whether the menu bar icon shows, the window size, the text size, and whether the
// first-run setup shows. It is not bound to the window, so the size, which only the shell
// reads on its own, stays out of the frontend's API.
type ShellSettings struct {
	settings *settings.Service
	setup    *setup.Service
	prefs    *prefs.Store
}

// newShellSettings returns the shell's settings over the settings service, the setup, and
// the preferences in pool.
func newShellSettings(svc *settings.Service, setupSvc *setup.Service, pool *sql.DB, c clock.Clock) ShellSettings {
	return ShellSettings{settings: svc, setup: setupSvc, prefs: prefs.New(pool, c)}
}

// SetupPending reports whether the first-run setup shows when the window opens: the
// window then opens on it, and the shell holds the question to allow banners until it ends.
func (s ShellSettings) SetupPending(ctx context.Context) (bool, error) {
	state, err := s.setup.State(ctx)
	if err != nil {
		return false, err
	}

	return !state.Done, nil
}

// Theme returns the stored theme, or the default.
func (s ShellSettings) Theme(ctx context.Context) (settings.Theme, error) {
	return s.settings.Theme(ctx)
}

// MenuBar returns whether the menu bar icon shows.
func (s ShellSettings) MenuBar(ctx context.Context) (bool, error) {
	return s.settings.MenuBar(ctx)
}

// WindowSize returns the size the window opens at.
func (s ShellSettings) WindowSize(ctx context.Context) (settings.WindowSize, error) {
	return settings.ReadWindowSize(ctx, s.prefs)
}

// TextSize returns the text size the window opens at.
func (s ShellSettings) TextSize(ctx context.Context) (settings.TextSize, error) {
	return s.settings.TextSize(ctx)
}

// followSwitch passes the settings service's events on, wakes what reads things again on
// its own when the user changes how often (an event.IntervalsChanged, which stays in Go),
// and wakes the sessions watcher
// when the user turns following on or off, which the settings service says with an
// event.SessionsUpdated, so the watcher starts or stops following at once instead of at
// its next look at the switch. An event.IntegrationsChanged stays in Go: a chosen copy of a
// tool wakes the tool finder, which tells the window once it has looked, and a flipped
// switch, which changes no detection, tells the window at once. An event.RepoRulesChanged
// stays in Go too: the inbox's snapshot is built again under the new rules, so the window
// and the menu bar hear the inbox changed, and the Release page regroups its list. An
// event.ThreadRulesChanged stays in Go as well: the mirror of the user's GitHub
// notifications decides every thread again at once under the new rules. So does an
// event.StatusRulesChanged: the service status check forgets a status page turned off,
// checks one turned on, and tells the window. An event.UpdatesChanged stays in Go too: the
// update's check follows the new choice at once, and the window hears the update changed.
// An event.AutopilotChanged stays in Go as well: Autopilot's engine runs a cycle under the
// new settings, and the shell and the window hear Autopilot changed, so the menu bar's
// Pause row follows a change made anywhere, in a copy that doesn't hold the engine too. The
// watcher, the finder, the Release page, the mirror, the status check,
// the update's check, and the engine are set once, before any setting can change.
type followSwitch struct {
	sink        event.Sink
	watcher     *sessions.Watcher
	tools       waker
	intervals   []waker
	releases    rulesFollower
	threadRules rulesFollower
	statusRules rulesFollower
	updates     waker
	autopilot   waker
}

// rulesFollower hears that the user changed rules it follows: the pinned and muted
// repositories and orgs (*release.Refresher), the GitHub notification rules
// (*ghthreads.Syncer), or what the service status check watches (*svcstatus.Checker).
type rulesFollower interface {
	RulesChanged()
}

// tellRulesChanged tells f the rules changed, when there is an f.
func tellRulesChanged(f rulesFollower) {
	if f != nil {
		f.RulesChanged()
	}
}

// waker is woken to look again: *integrations.Worker, *update.Worker, and what reads
// things again on its own (*inbox.Scheduler, *inbox.ChecksFollower,
// *release.ChecksFollower, worktrees.Sweeper, *ghthreads.Syncer, *svcstatus.Checker).
type waker interface {
	Wake()
}

// Emit passes e on, waking the watcher first for an event.SessionsUpdated, turns an
// event.IntegrationsChanged into a wake of the tool finder or an event.ToolsUpdated, an
// event.RepoRulesChanged into an event.InboxUpdated and a word to the Release page, an
// event.ThreadRulesChanged into a word to the mirror of the GitHub notifications, an
// event.StatusRulesChanged into a word to the service status check, and an
// event.UpdatesChanged into a wake of the update's check and an event.UpdatesUpdated.
func (f *followSwitch) Emit(e event.Event) {
	if f.followRules(e) {
		return
	}

	switch e := e.(type) {
	case event.IntervalsChanged:
		for _, w := range f.intervals {
			w.Wake()
		}

		return
	case event.SessionsUpdated:
		if f.watcher != nil {
			f.watcher.Wake()
		}
	case event.IntegrationsChanged:
		if e.CopyChosen && f.tools != nil {
			f.tools.Wake()

			return
		}

		f.sink.Emit(event.ToolsUpdated{})

		return
	}

	f.sink.Emit(e)
}

// followRules passes a change of rules, or of what to do with a new version, on to what
// follows them, and reports whether e was one; none reaches the window as it is.
func (f *followSwitch) followRules(e event.Event) bool {
	switch e.(type) {
	case event.ThreadRulesChanged:
		tellRulesChanged(f.threadRules)
	case event.StatusRulesChanged:
		tellRulesChanged(f.statusRules)
	case event.RepoRulesChanged:
		f.sink.Emit(event.InboxUpdated{})
		tellRulesChanged(f.releases)
	case event.UpdatesChanged:
		if f.updates != nil {
			f.updates.Wake()
		}

		f.sink.Emit(event.UpdatesUpdated{})
	case event.AutopilotChanged:
		if f.autopilot != nil {
			f.autopilot.Wake()
		}

		// The menu bar's Pause row and the window read Autopilot again at once: a copy of
		// the app that doesn't hold the engine runs no cycle that would tell them.
		f.sink.Emit(event.AutopilotUpdated{})
	default:
		return false
	}

	return true
}

// refreshEvery says how often the app reads kind again on its own, as the settings say
// now; settings that can't be read count as the defaults, logged, so a passing problem
// never stops the refreshes.
func refreshEvery(prefs *settings.Service, kind settings.RefreshKind, logger *slog.Logger) periodic.Every {
	return func(ctx context.Context) time.Duration {
		s, err := prefs.Get(ctx)
		if err != nil {
			if ctx.Err() == nil {
				logger.WarnContext(ctx, "could not read how often to refresh; using the default", "kind", kind,
					"error", err)
			}

			return settings.DefaultRefreshIntervals().Every(kind)
		}

		return s.Refresh.Every(kind)
	}
}
