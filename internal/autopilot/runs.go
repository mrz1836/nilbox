package autopilot

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/mrz1836/nilbox/internal/agentrun/runevent"
	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// What the engine hears of an agent's run in the background beside its start and its end:
// its tokens so far; how long a start its runner held back waits before it is tried again;
// and what the window reads of the runs going and the ones queued, and of a run's events.

// ErrNoViews is returned for a run's events where no runner keeps them.
var ErrNoViews = errors.New("no run views")

// RunUsage is a run's tokens so far.
type RunUsage struct {
	TokensIn  int64
	TokensOut int64
	CacheRead int64
}

// RunViews reads what a runner keeps of the agents' runs, by the ledger's run: how far one
// going has got, its events, a page of them read back from its log before an offset, and
// its worktree's changes since it started (core adapts the background runner).
type RunViews interface {
	Progress(runID int64) (runevent.Progress, bool)
	Events(ctx context.Context, runID, after int64) (runevent.Page, error)
	EventsBefore(ctx context.Context, runID, before int64) (runevent.Page, error)
	Diff(ctx context.Context, runID int64) (runevent.Diff, error)
}

// RunLogs clears the agents' runs' logs and folders, as Clear history does.
type RunLogs interface {
	ClearAgentHistory(ctx context.Context) error
}

// RunUsage records the ledger's run's tokens so far, on the loop, while it goes: what a
// crash leaves of it, and what the window shows.
func (e *Engine) RunUsage(runID int64, u RunUsage) {
	e.mail.post(func(ctx context.Context) {
		_, err := e.ledger.q.SetRunUsage(ctx, queries.SetRunUsageParams{
			TokensIn: u.TokensIn, TokensOut: u.TokensOut, CacheReadTokens: u.CacheRead, UpdatedAt: e.clock.Now(),
			ID: runID,
		})
		if err != nil {
			e.logger.DebugContext(ctx, "Autopilot could not record a run's tokens", "run", runID, "error", err)
		}
	})
}

// heldBack is a start its runner held back, and when, and until when it waits, zero for
// a tick.
type heldBack struct {
	at      time.Time
	until   time.Time
	blocker Blocker
	words   string
}

// holdBack keeps a match whose start its runner held back on something working where it
// would start (Result.Wait, a queued blocker) from being tried again until a tick passes:
// each try reads the item from GitHub again first, so a session in its worktree would
// otherwise cost a point every cycle until it ends.
func (e *Engine) holdBack(m *match, now, until time.Time) {
	switch {
	case !until.IsZero() && m.BlockedBy != "" && m.state() != StateWaiting:
		e.heldBack[m.ID] = heldBack{at: now, until: until, blocker: Blocker(m.BlockedBy), words: m.Detail}
	case m.state() == StateQueued && m.BlockedBy != "":
		e.heldBack[m.ID] = heldBack{at: now, blocker: Blocker(m.BlockedBy), words: m.Detail}
	}
}

// backOff keeps a match whose start its runner held back less than a tick ago waiting on
// what held it, and reports whether it did; a later cycle tries it again.
func (p *pass) backOff(m *match) bool {
	h, held := p.e.heldBack[m.ID]

	switch {
	case !held:
		return false
	case !h.until.IsZero() && !p.now.Before(h.until), h.until.IsZero() && p.now.Sub(h.at) >= p.e.tick:
		delete(p.e.heldBack, m.ID)

		return false
	case h.blocker == "":
		m.set(StateQueued, "", h.words)
	default:
		m.wait(h.blocker, h.words)
	}

	return true
}

// tellFinished tells the user that a background run ended, finished or failed, as Agent
// finished: a run stopped or interrupted tells nothing, as the user stopped it or Needs
// you shows it.
func (e *Engine) tellFinished(ctx context.Context, run queries.AutopilotRun, end RunEnd) {
	if e.finished == nil || run.Runner != runnerBackground || (end.Outcome != RunDone && end.Outcome != RunFailed) {
		return
	}

	routine := e.routineName(ctx, run.RoutineID)

	body := end.Summary
	if end.Outcome == RunFailed {
		body = textutil.Sentence(end.Error)
	}

	if words := costWords(end.Cost, end.CostEstimated); words != "" {
		body += " " + words
	}

	ref := ItemRef{
		ID: store.Deref(run.ItemID), Kind: item.Kind(run.Kind), Repo: run.RepoOwner + "/" + run.RepoName,
		Number: int(run.Number), Title: run.Title, URL: run.URL,
	}

	err := e.finished.Notice(ctx, Notice{
		Kind: NoticeFinished, RoutineID: run.RoutineID, Routine: routine, MatchID: run.MatchID, RunID: run.ID,
		CreatedAt: run.QueuedAt, Item: &ref, Title: run.Title, Body: routine + ": " + body,
		Key: "finished:" + strconv.FormatInt(run.ID, 10) + ":" + run.QueuedAt.UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		e.logger.WarnContext(ctx, "Autopilot could not tell that an agent finished", "run", run.ID, "error", err)
	}
}

// costWords says what a run cost: "Cost $0.12.", or for an estimate, as a run on the
// user's plan reports, "About $0.12, estimated."; "" when it reported none.
func costWords(cost *float64, estimated bool) string {
	if cost == nil {
		return ""
	}

	amount := "$" + strconv.FormatFloat(*cost, 'f', 2, 64)
	if estimated {
		return "About " + amount + ", estimated."
	}

	return "Cost " + amount + "."
}

// routineName returns the routine id's name, "" when it can't be read.
func (e *Engine) routineName(ctx context.Context, id int64) string {
	names, err := e.ledger.q.ListRoutineNames(ctx)
	if err != nil {
		return ""
	}

	for _, n := range names {
		if n.ID == id {
			return n.Name
		}
	}

	return ""
}

// runnerBackground is the runner of an agent's run in the background.
const runnerBackground = "background"

// WorkingList is what Activity's Working shows: the agents' runs going, each with how far
// it has got. The starts queued are Needs you's (NeedsYou).
type WorkingList struct {
	Working []Working `json:"working"`
}

// Working is an agent's run going.
type Working struct {
	WorkingRun

	// Status is starting, running, or stopping.
	Status string `json:"status"`

	// Progress is how far it has got, as its runner reads its stream; zero for a run its
	// runner doesn't follow, as one in the terminal.
	Progress runevent.Progress `json:"progress"`
}

// Working returns the agents' runs going, oldest first. It reads the database, and the
// runner's memory of the runs it follows; the list is never nil.
func (s *Service) Working(ctx context.Context) (WorkingList, error) {
	out := WorkingList{Working: []Working{}}

	if s.store.q == nil {
		return out, nil
	}

	names, err := s.routineNameMap(ctx)
	if err != nil {
		return out, err
	}

	runs, err := s.ledger().q.ListActiveRuns(ctx)
	if err != nil {
		return out, uierr.New("Autopilot's runs couldn't be read.", err)
	}

	for _, r := range runs {
		if r.Runner == runnerGitHub {
			continue
		}

		w := Working{WorkingRun: workingRun(r, names[r.RoutineID]), Status: r.Status, Progress: s.progressOf(r)}
		out.Working = append(out.Working, w)
	}

	return out, nil
}

// progressOf is how far a run has got, as its runner says, or its tokens as last recorded.
func (s *Service) progressOf(r queries.AutopilotRun) runevent.Progress {
	if e := s.engine; e != nil && e.views != nil {
		if p, ok := e.views.Progress(r.ID); ok {
			return p
		}
	}

	return runevent.Progress{
		TokensIn: r.TokensIn, TokensOut: r.TokensOut, CacheRead: r.CacheReadTokens, Todos: []runevent.Todo{},
	}
}

// routineNameMap returns every routine's name, by its ID.
func (s *Service) routineNameMap(ctx context.Context) (map[int64]string, error) {
	rows, err := s.ledger().q.ListRoutineNames(ctx)
	if err != nil {
		return nil, uierr.New("The routines' names couldn't be read.", err)
	}

	out := make(map[int64]string, len(rows))
	for _, r := range rows {
		out[r.ID] = r.Name
	}

	return out, nil
}

// RunsToday counts the runs started today, from local midnight: the agents', in the
// background and in the terminal, and the GitHub actions sent, as a bug report words them.
type RunsToday struct {
	Background int `json:"background"`
	Terminal   int `json:"terminal"`
	Actions    int `json:"actions"`
}

// RunsToday counts today's runs, those that never started left out.
func (s *Service) RunsToday(ctx context.Context) (RunsToday, error) {
	out := RunsToday{}

	if s.store.q == nil {
		return out, nil
	}

	rows, err := s.ledger().q.ListRunsSince(ctx, dayStart(s.clock.Now()))
	if err != nil {
		return out, uierr.New("Autopilot's runs couldn't be read.", err)
	}

	day := dayStart(s.clock.Now())

	for _, r := range rows {
		if RunOutcome(r.Outcome) == RunNotStarted || r.QueuedAt.Before(day) {
			continue
		}

		switch r.Runner {
		case runnerBackground:
			out.Background++
		case runnerGitHub:
			out.Actions++
		default:
			out.Terminal++
		}
	}

	return out, nil
}

// RunEvents returns the events of the ledger's run runID after the one numbered after:
// what the run view shows. The window asks again after each autopilot:run.
func (s *Service) RunEvents(ctx context.Context, runID, after int64) (runevent.Page, error) {
	if page, elsewhere := s.followedElsewhere(ctx, runID); elsewhere {
		return page, nil
	}

	e := s.engine
	if e == nil || e.views == nil {
		return emptyPage(), uierr.New("This run's events aren't kept here.", ErrNoViews)
	}

	page, err := e.views.Events(ctx, runID, after)
	if err != nil {
		return emptyPage(), uierr.New("This run's events couldn't be read.", err)
	}

	return nonNilPage(page), nil
}
