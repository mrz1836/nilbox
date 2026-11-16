package autopilot

import (
	"context"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// Glance is Autopilot at a glance, read from the database alone: what the menu bar's
// Autopilot group shows, whether the window names Autopilot at all (only while a routine
// exists), and whether closing the window only hides it (KeepsRunning).
type Glance struct {
	// Routines counts the routines not deleted, and Enabled those the engine runs: switched
	// on, of the account signed in, and loading; none while no one is signed in.
	Routines int `json:"routines"`
	Enabled  int `json:"enabled"`

	// Paused reports Pause Autopilot.
	Paused bool `json:"paused"`

	// NeedsYou counts the open matches waiting for the user's decision: an OK, a
	// catch-up's, a backlog's, a warning's, an interrupted run's.
	NeedsYou int `json:"needsYou"`

	// Working are the agent runs going, in the background or in the user's terminal,
	// oldest first.
	Working []WorkingRun `json:"working"`
}

// WorkingRun is one agent run going.
type WorkingRun struct {
	RunID     int64  `json:"runId"`
	MatchID   int64  `json:"matchId"`
	RoutineID int64  `json:"routineId"`
	Routine   string `json:"routine"`

	// Item is the item it works on, as the ledger keeps a copy of it.
	Item ItemRef `json:"item"`

	// Agent is the agent's ID, and Where where it runs: background or terminal.
	Agent string `json:"agent"`
	Where string `json:"where"`

	// StartedAt is when it started, or was queued when it hasn't said.
	StartedAt time.Time `json:"startedAt"`

	// CreatedAt is when the run was recorded, which tells it from a later run given its
	// ID, as a notice's target does.
	CreatedAt time.Time `json:"createdAt"`
}

// KeepsRunning reports whether Autopilot has work while the window is closed: a routine
// the engine runs, or an agent run going. Closing the window then only hides it, so the app
// keeps running them; another account's routines keep nothing running.
func (g Glance) KeepsRunning() bool {
	return g.Enabled > 0 || len(g.Working) > 0
}

// Glance returns Autopilot at a glance. It never returns a nil list.
func (s *Service) Glance(ctx context.Context) (Glance, error) {
	out := Glance{Working: []WorkingRun{}}

	if s.store.q == nil {
		return out, nil
	}

	routines, err := s.store.all(ctx)
	if err != nil {
		return out, uierr.New("Autopilot's routines couldn't be read.", err)
	}

	names := make(map[int64]string, len(routines))
	ranFor := s.runsAny(ctx)

	for _, r := range routines {
		out.Routines++
		names[r.ID] = r.Name

		if r.Enabled && ranFor(r.AccountLogin) && load(r.Spec).broken == nil {
			out.Enabled++
		}
	}

	l := s.ledger()

	if out.NeedsYou, err = needsYou(ctx, l); err != nil {
		return out, uierr.New("Autopilot's matches couldn't be read.", err)
	}

	runs, err := l.q.ListActiveRuns(ctx)
	if err != nil {
		return out, uierr.New("Autopilot's runs couldn't be read.", err)
	}

	for _, r := range runs {
		if r.Runner != "github" {
			out.Working = append(out.Working, workingRun(r, names[r.RoutineID]))
		}
	}

	out.Paused = s.settings(ctx).Paused

	return out, nil
}

// runsAny returns whether the engine would run a routine of an account: the account signed
// in's (a routine of no account is the editor's), and none while no one is signed in, or the
// engine's last cycle found the sign-in gone.
func (s *Service) runsAny(ctx context.Context) func(account string) bool {
	viewer := s.viewer(ctx)
	signedOut := viewer == ""

	if e := s.engine; e != nil {
		e.mu.Lock()
		signedOut = signedOut || e.status.SignedOut
		e.mu.Unlock()
	}

	return func(account string) bool {
		return !signedOut && !otherAccount(account, viewer)
	}
}

// needsYou counts the open matches waiting for the user's decision.
func needsYou(ctx context.Context, l *ledger) (int, error) {
	open, err := l.q.ListOpenMatches(ctx)
	if err != nil {
		return 0, err
	}

	n := 0

	for _, m := range open {
		if State(m.State) == StateWaiting {
			n++
		}
	}

	return n, nil
}

// workingRun is an agent run going, of the routine named routine.
func workingRun(r queries.AutopilotRun, routine string) WorkingRun {
	started := r.QueuedAt
	if r.StartedAt != nil {
		started = *r.StartedAt
	}

	return WorkingRun{
		RunID: r.ID, MatchID: r.MatchID, RoutineID: r.RoutineID, Routine: routine, Agent: r.Agent, Where: r.Runner,
		StartedAt: started, CreatedAt: r.QueuedAt,
		Item: ItemRef{
			ID: store.Deref(r.ItemID), Kind: item.Kind(r.Kind), Repo: r.RepoOwner + "/" + r.RepoName,
			Number: int(r.Number), Title: r.Title, URL: r.URL,
		},
	}
}
