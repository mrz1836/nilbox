package autopilot

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/agentrun/runevent"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// What the run view reads of one of Autopilot's agent runs, beside Activity: the run and
// its match, with the routine's steps; its timeline, as RunEvents follows it and
// RunEventsBefore pages it back; and its worktree's changes since it started. The run
// view's Take over is the launcher's, by the run's session, and its Stop is StopRun.

// RunView is an agent's run as the run view shows it.
type RunView struct {
	// Run is the run, and Match the match it is for, with the routine's steps and where
	// it stands.
	Run   Run   `json:"run"`
	Match Match `json:"match"`

	// Live reports a run still going: starting, running, or stopping.
	Live bool `json:"live"`

	// CanTakeOver reports a run in the background whose agent resumes its conversation in
	// the terminal, by its session: the run view's Take over.
	CanTakeOver bool `json:"canTakeOver"`
}

// RunView returns the ledger's run runID, an agent's, as the run view shows it.
func (s *Service) RunView(ctx context.Context, runID int64) (RunView, error) {
	if s.store.q == nil {
		return RunView{}, uierr.New("That run isn't in Autopilot's history.", ErrNoRun)
	}

	row, err := s.ledger().q.GetRun(ctx, runID)
	if err != nil {
		return RunView{}, uierr.New("That run is no longer in Autopilot's history.", errors.Join(ErrNoRun, err))
	}

	m, err := s.matchByID(ctx, row.MatchID)
	if err != nil {
		return RunView{}, err
	}

	run := runView(row)

	return RunView{
		Run: run, Match: m, Live: row.Status != runEnded, CanTakeOver: canTakeOver(run),
	}, nil
}

// RunBySession returns the ledger's run recorded as the session sessionID, as a session's
// Watch opens its run view, or 0 for a session that is no run of Autopilot's.
func (s *Service) RunBySession(ctx context.Context, sessionID string) (int64, error) {
	if s.store.q == nil || sessionID == "" {
		return 0, nil
	}

	row, err := s.ledger().q.GetRunBySession(ctx, sessionID)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return 0, nil
	case err != nil:
		return 0, uierr.New("Autopilot's runs couldn't be read.", err)
	default:
		return row.ID, nil
	}
}

// RunEventsBefore returns the page of the ledger's run runID's events read back from its
// log before the offset before, as a long timeline pages back: its first line's offset is
// the Before of the page the window shows first.
func (s *Service) RunEventsBefore(ctx context.Context, runID, before int64) (runevent.Page, error) {
	if page, elsewhere := s.followedElsewhere(ctx, runID); elsewhere {
		return page, nil
	}

	e := s.engine
	if e == nil || e.views == nil {
		return emptyPage(), uierr.New("This run's events aren't kept here.", ErrNoViews)
	}

	page, err := e.views.EventsBefore(ctx, runID, before)
	if err != nil {
		return emptyPage(), uierr.New("This run's earlier events couldn't be read.", err)
	}

	return nonNilPage(page), nil
}

// RunDiff returns the ledger's run runID's worktree's changes since the head it started
// on, committed or not, and the files git doesn't track there: the run view's Diff,
// read-only.
func (s *Service) RunDiff(ctx context.Context, runID int64) (runevent.Diff, error) {
	empty := runevent.Diff{Untracked: []string{}}

	e := s.engine
	if e == nil || e.views == nil {
		return empty, uierr.New("This run's changes can't be read here.", ErrNoViews)
	}

	diff, err := e.views.Diff(ctx, runID)
	if err != nil {
		return empty, uierr.New("This run's changes couldn't be read. Its worktree may be gone.", err)
	}

	if diff.Untracked == nil {
		diff.Untracked = []string{}
	}

	return diff, nil
}

// followedElsewhere returns, for a run of the ledger's that isn't an agent's in the
// background (one in the terminal, whose session panel follows it, or a script's, whose
// run Scripts → Runs shows), an empty page, done once the run ended, and true: no runner
// here keeps its timeline. A run the ledger doesn't hold is left to the runner to refuse.
func (s *Service) followedElsewhere(ctx context.Context, runID int64) (runevent.Page, bool) {
	if s.store.q == nil {
		return runevent.Page{}, false
	}

	row, err := s.ledger().q.GetRun(ctx, runID)
	if err != nil || row.Runner == runnerBackground {
		return runevent.Page{}, false
	}

	page := emptyPage()
	page.Done = row.Status == runEnded

	return page, true
}

// canTakeOver reports a run in the background, recorded as a session, whose agent resumes
// a conversation: the run view offers Take over for it.
func canTakeOver(r Run) bool {
	if r.Runner != runnerBackground || r.SessionID == "" {
		return false
	}

	h, err := agent.HeadlessOf(agent.ID(r.Agent))

	return err == nil && h.Resume
}

// emptyPage is a page with nothing on it.
func emptyPage() runevent.Page {
	return runevent.Page{Events: []runevent.Event{}, Progress: runevent.Progress{Todos: []runevent.Todo{}}}
}

// nonNilPage returns page with its lists never nil, as the window gets them.
func nonNilPage(page runevent.Page) runevent.Page {
	if page.Events == nil {
		page.Events = []runevent.Event{}
	}

	if page.Progress.Todos == nil {
		page.Progress.Todos = []runevent.Todo{}
	}

	if e := page.Ending; e != nil {
		if e.Todos == nil {
			e.Todos = []runevent.Todo{}
		}

		if e.Changed == nil {
			e.Changed = []string{}
		}

		if e.Models == nil {
			e.Models = []string{}
		}
	}

	return page
}
