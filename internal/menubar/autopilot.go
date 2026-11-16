package menubar

import (
	"strconv"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// The menu's Autopilot group: a heading, the agent runs Autopilot has working (each a row
// that opens the run), how many of its matches wait for the user (a row that opens
// Activity), and Pause Autopilot, the menu's checkbox row. It shows only while the user
// has a routine, so a user without one never sees Autopilot named.

// AutopilotTitle heads the group.
const AutopilotTitle = "Autopilot"

// PauseLabel is the group's checkbox row, ticked while Autopilot is paused.
const PauseLabel = "Pause Autopilot"

// AutopilotState is what the group is drawn from, as Autopilot reads it from its database
// (core adapts autopilot.Glance).
type AutopilotState struct {
	// Routines counts the user's routines: the group shows only while there is one.
	Routines int

	// Paused reports Pause Autopilot.
	Paused bool

	// NeedsYou counts the matches waiting for the user's decision.
	NeedsYou int

	// Working are the agent runs going, oldest first.
	Working []AgentRun
}

// AgentRun is one agent run Autopilot has working.
type AgentRun struct {
	// RunID names the run, which a click opens, and CreatedAt when it was recorded, which
	// tells it from a later run given its ID; RoutineID and MatchID are its routine and
	// match.
	RunID     int64
	RoutineID int64
	MatchID   int64
	CreatedAt time.Time

	// Repo ("owner/name"), Number, and Title are its item.
	Repo   string
	Number int
	Title  string

	// Agent is the agent that runs, and StartedAt when it started.
	Agent     agent.ID
	StartedAt time.Time
}

// Autopilot is the group as the menu shows it.
type Autopilot struct {
	// Shown reports whether the menu shows the group at all.
	Shown bool

	// Working are the first PerSection agent runs, each a row that opens its run, and
	// WorkingMore how many more run.
	Working     []Working
	WorkingMore int

	// NeedsYou is the row of the matches waiting for the user ("2 need your OK"), which
	// opens Activity, or "" for none.
	NeedsYou string

	// Paused ticks the Pause Autopilot row.
	Paused bool

	// NextChange is when a run's time in its row next changes, zero while none runs.
	NextChange time.Time
}

// Working is one agent run's row.
type Working struct {
	// RunID, RoutineID, and MatchID name the run a click opens, and Created when it was
	// recorded, as RFC 3339.
	RunID     int64
	RoutineID int64
	MatchID   int64
	Created   string

	// Label is the row: "example-repo #88 · Claude · 6m".
	Label string

	// Tooltip is the item in full: "acme-org/example-repo #88: Fix the login redirect · Claude
	// working for 6m".
	Tooltip string
}

// AutopilotAction is what a click on one of the group's rows does, through the shell.
type AutopilotAction string

// The group's actions.
const (
	// AutopilotWatch opens an agent run: the window comes forward at it.
	AutopilotWatch AutopilotAction = "watch"

	// AutopilotActivity opens Activity, where the matches waiting for the user are.
	AutopilotActivity AutopilotAction = "activity"

	// AutopilotPause flips Pause Autopilot.
	AutopilotPause AutopilotAction = "pause"
)

// AutopilotGroup works out the group for st at now: shown only while the user has a
// routine, its runs' times as of now.
func AutopilotGroup(st AutopilotState, now time.Time) Autopilot {
	if st.Routines <= 0 {
		return Autopilot{}
	}

	out := Autopilot{Shown: true, Paused: st.Paused, NeedsYou: NeedsYouLabel(st.NeedsYou)}

	for i, r := range st.Working {
		if next := nextMinute(r.StartedAt, now); out.NextChange.IsZero() || next.Before(out.NextChange) {
			out.NextChange = next
		}

		if i >= PerSection {
			out.WorkingMore++

			continue
		}

		out.Working = append(out.Working, workingRow(r, now))
	}

	return out
}

// NeedsYouLabel is the row of n matches waiting for the user: "1 needs your OK", "3 need
// your OK", or "" for none.
func NeedsYouLabel(n int) string {
	switch {
	case n <= 0:
		return ""
	case n == 1:
		return "1 needs your OK"
	default:
		return strconv.Itoa(n) + " need your OK"
	}
}

// workingRow is an agent run's row at now.
func workingRow(r AgentRun, now time.Time) Working {
	name := r.Repo
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}

	number := "#" + strconv.Itoa(r.Number)
	who := agentName(r.Agent)
	took := Elapsed(now.Sub(r.StartedAt))

	tooltip := r.Repo + " " + number
	if title := strings.Join(strings.Fields(r.Title), " "); title != "" {
		tooltip += ": " + textutil.Truncate(title, maxTitleRunes*2)
	}

	return Working{
		RunID: r.RunID, RoutineID: r.RoutineID, MatchID: r.MatchID, Created: created(r.CreatedAt),
		Label:   name + " " + number + " · " + who + " · " + took,
		Tooltip: tooltip + " · " + who + " working for " + took,
	}
}

// created is when a run was recorded, as RFC 3339 in UTC, or "" for none.
func created(at time.Time) string {
	if at.IsZero() {
		return ""
	}

	return at.UTC().Format(time.RFC3339)
}

// Elapsed says how long a run has been going, as its row does: "<1m", "6m", "1h", or
// "2h 5m".
func Elapsed(d time.Duration) string {
	minutes := int(d / time.Minute)

	switch {
	case minutes < 1:
		return "<1m"
	case minutes < 60:
		return strconv.Itoa(minutes) + "m"
	case minutes%60 == 0:
		return strconv.Itoa(minutes/60) + "h"
	default:
		return strconv.Itoa(minutes/60) + "h " + strconv.Itoa(minutes%60) + "m"
	}
}

// nextMinute is when a run started at started next shows another minute, after now.
func nextMinute(started, now time.Time) time.Time {
	if now.Before(started) {
		return started.Add(time.Minute)
	}

	minutes := int64(now.Sub(started)/time.Minute) + 1

	return started.Add(time.Duration(minutes) * time.Minute)
}

// SameAutopilot reports whether two groups draw the same rows, so a change that leaves
// them as they were redraws nothing.
func SameAutopilot(a, b Autopilot) bool {
	if a.Shown != b.Shown || a.Paused != b.Paused || a.NeedsYou != b.NeedsYou || a.WorkingMore != b.WorkingMore ||
		len(a.Working) != len(b.Working) {
		return false
	}

	for i := range a.Working {
		if a.Working[i] != b.Working[i] {
			return false
		}
	}

	return true
}
