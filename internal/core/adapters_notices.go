package core

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/desktop"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/menubar"
	"github.com/mrz1836/nilbox/internal/notify"
	"github.com/mrz1836/nilbox/internal/notifyprefs"
	"github.com/mrz1836/nilbox/internal/settings"
)

// noticeKinds are the notifications' kinds of Autopilot's notices.
//
//nolint:gochecknoglobals // a fixed table
var noticeKinds = map[autopilot.NoticeKind]notifyprefs.Kind{
	autopilot.NoticeAlert:    notify.KindAutopilotAlert,
	autopilot.NoticeNeedsYou: notify.KindAutopilotNeedsYou,
	autopilot.NoticeFinished: notify.KindAutopilotFinished,
	autopilot.NoticeChecks:   notify.KindAutopilotChecks,
	autopilot.NoticeProblem:  notify.KindAutopilotProblem,
}

// errUnknownNotice is a notice of a kind the notifications have no kind for.
var errUnknownNotice = errors.New("no kind of notification for the notice")

// autopilotNotices tells the user of Autopilot's notices through the notifications
// (autopilot.NoticeSink): recorded at once, with the routine's name heading an alert, its
// item, and what it opens in the Autopilot tab, and its banner shown at once, or, for one
// of a burst, gathered with the others.
type autopilotNotices struct {
	dispatcher *notify.Dispatcher
}

// Notice records n and shows its banner.
func (a autopilotNotices) Notice(ctx context.Context, n autopilot.Notice) error {
	entry, err := noticeEntry(n)
	if err != nil {
		return err
	}

	return a.dispatcher.Tell(ctx, entry, n.Burst)
}

// noticeEntry is the notification n is recorded as.
func noticeEntry(n autopilot.Notice) (notify.Entry, error) {
	kind, ok := noticeKinds[n.Kind]
	if !ok {
		return notify.Entry{}, fmt.Errorf("%w: %q", errUnknownNotice, n.Kind)
	}

	entry := notify.Entry{
		Kind: kind, Title: n.Title, Body: n.Body, Banner: n.Banner, Key: n.Key,
		Autopilot: event.AutopilotTarget{RoutineID: n.RoutineID, MatchID: n.MatchID, RunID: n.RunID},
	}

	if !n.CreatedAt.IsZero() {
		entry.Autopilot.CreatedAt = clock.Format(n.CreatedAt)
	}

	if n.Kind == autopilot.NoticeAlert {
		entry.Heading = n.Routine
	}

	if it := n.Item; it != nil {
		owner, name, _ := strings.Cut(it.Repo, "/")
		entry.ItemID, entry.ItemKind, entry.Number, entry.URL = it.ID, it.Kind, it.Number, it.URL
		entry.RepoOwner, entry.RepoName = owner, name
	}

	return entry, nil
}

// ShellAutopilot is Autopilot as the desktop shell reads it and asks of it
// (desktop.Autopilot): Autopilot's glance, for the menu bar's group and the window's close,
// and Pause Autopilot, which it writes through the settings' own setter, the one writer of
// Autopilot's settings.
type ShellAutopilot struct {
	routines *autopilot.Service
	settings *settings.Service
}

// State reads Autopilot at a glance.
func (a ShellAutopilot) State(ctx context.Context) (desktop.AutopilotState, error) {
	if a.routines == nil {
		return desktop.AutopilotState{}, nil
	}

	g, err := a.routines.Glance(ctx)
	if err != nil {
		return desktop.AutopilotState{}, err
	}

	return shellAutopilotState(g), nil
}

// shellAutopilotState is Autopilot's glance as the shell reads it.
func shellAutopilotState(g autopilot.Glance) desktop.AutopilotState {
	out := desktop.AutopilotState{
		Menu: menubar.AutopilotState{
			Routines: g.Routines, Paused: g.Paused, NeedsYou: g.NeedsYou,
			Working: make([]menubar.AgentRun, 0, len(g.Working)),
		},
		KeepsRunning: g.KeepsRunning(),
	}

	for _, w := range g.Working {
		out.Menu.Working = append(out.Menu.Working, menubar.AgentRun{
			RunID: w.RunID, RoutineID: w.RoutineID, MatchID: w.MatchID, CreatedAt: w.CreatedAt, Repo: w.Item.Repo,
			Number: w.Item.Number, Title: w.Item.Title, Agent: agent.ID(w.Agent), StartedAt: w.StartedAt,
		})
	}

	return out
}

// SetPaused turns Pause Autopilot on or off, as Settings does.
func (a ShellAutopilot) SetPaused(ctx context.Context, paused bool) error {
	if a.settings == nil {
		return nil
	}

	_, err := a.settings.SetAutopilotPaused(ctx, paused)

	return err
}
