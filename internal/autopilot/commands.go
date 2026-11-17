package autopilot

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/eval"
	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// The window's actions on the ledger. Each is a command the engine applies on its own
// goroutine, then answers, so a double click, a banner's click racing a cycle, or an
// Approve during a refresh changes a match once; Stop and Stop all go straight to the
// runner.

var (
	// ErrNoEngine is returned for an action on the ledger with no engine to apply it.
	ErrNoEngine = errors.New("autopilot's engine isn't running")

	// ErrRunning is returned for a Skip of a match whose run is under way.
	ErrRunning = errors.New("the match is running")

	// ErrNoRun is returned for a Stop of a run that isn't going.
	ErrNoRun = errors.New("no such run going")
)

// Current is what to do with the items a routine matches now that it has no record of,
// when it is turned on, leaves Dry run, or its Watch, When, If, or Once per changes.
type Current string

// The choices for what matches now.
const (
	// CurrentRun runs on them too, as on anything that matches later: the default, and
	// what a routine with nothing matching now does.
	CurrentRun Current = "run"

	// CurrentFromNow records them as already handled (baseline): only what changes from
	// now on runs.
	CurrentFromNow Current = "fromNow"
)

// Valid reports one of the choices, "" among them.
func (c Current) Valid() bool {
	return c == "" || c == CurrentRun || c == CurrentFromNow
}

// engineOrErr returns the engine, or the error a ledger action without one returns.
func (s *Service) engineOrErr() (*Engine, error) {
	if s.engine == nil {
		return nil, uierr.New("Autopilot isn't running.", ErrNoEngine)
	}

	return s.engine, nil
}

// Approve gives the user's OK to the matches ids that wait for it (an Ask first routine's,
// a catch-up's, a backlog's, an interrupted run's, a warning's), which then start once
// the limits let them, and returns them as they are now. A match that waits for nothing,
// or is closed, is left as it is, so a second Approve starts nothing more.
func (s *Service) Approve(ctx context.Context, ids []int64) ([]Match, error) {
	e, err := s.engineOrErr()
	if err != nil {
		return []Match{}, err
	}

	err = e.do(ctx, func(ctx context.Context) error {
		for _, id := range ids {
			if approveErr := e.approve(ctx, id); approveErr != nil {
				return approveErr
			}
		}

		e.Wake()

		return nil
	})
	if err != nil {
		return []Match{}, err
	}

	return s.matchesByID(ctx, ids)
}

// approve gives the user's OK to the match id: one that waits starts once the limits let
// it, and one queued or blocked keeps the OK for when it goes on. Any other is left as it
// is.
func (e *Engine) approve(ctx context.Context, id int64) error {
	m, err := e.ledger.get(ctx, id)
	if err != nil {
		return uierr.New("That match is gone.", err)
	}

	switch {
	case m.state() == StateWaiting:
		confirmHold(m)
		acceptWarnings(m)
		m.set(StateQueued, "", "")
	case m.Approved || m.state() != StateQueued && m.state() != StateBlocked:
		return nil
	}

	m.Approved = true
	e.save(ctx, m)

	return nil
}

// confirmHold records the user's OK of the hold m's step waits on, exactly as it was shown
// (its Key: the head commit for a file hold, the worktree and its files for a dirty one),
// so the next start goes ahead past it, and past nothing else.
func confirmHold(m *match) {
	if int(m.Step) >= len(m.steps) {
		return
	}

	sr := &m.steps[m.Step]
	if sr.Hold == nil || sr.Hold.Key == "" || slices.Contains(sr.Confirmed, sr.Hold.Key) {
		return
	}

	if b := Blocker(m.BlockedBy); b == BlockHold || b == BlockDirty {
		sr.Confirmed = append(sr.Confirmed, sr.Hold.Key)
	}
}

// acceptWarnings records, on a match waiting on the warnings its GitHub action's sheet
// offers it with, that the user accepted them: the action then goes with those warnings,
// and waits again for any other.
func acceptWarnings(m *match) {
	if Blocker(m.BlockedBy) == BlockWarning && int(m.Step) < len(m.steps) {
		m.steps[m.Step].Accepted = m.Detail
	}
}

// matchesByID returns the matches ids, those still recorded.
func (s *Service) matchesByID(ctx context.Context, ids []int64) ([]Match, error) {
	rows := make([]queries.AutopilotMatch, 0, len(ids))

	for _, id := range ids {
		row, err := s.ledger().q.GetMatch(ctx, id)
		if err == nil {
			rows = append(rows, row)
		}
	}

	return s.views(ctx, rows)
}

// Skip closes the open match id as skipped by the user, and returns it. A match whose
// run is under way is refused: stop the run instead.
func (s *Service) Skip(ctx context.Context, id int64) (Match, error) {
	e, err := s.engineOrErr()
	if err != nil {
		return Match{}, err
	}

	err = e.do(ctx, func(ctx context.Context) error {
		m, getErr := e.ledger.get(ctx, id)
		if getErr != nil {
			return uierr.New("That match is gone.", getErr)
		}

		switch {
		case m.state().active() || e.inflight[m.ID] && m.state() == StateRunning:
			return uierr.New("It's running: stop the run instead.", ErrRunning)
		case !m.state().Open():
			return nil
		}

		m.close(StateDismissed, ReasonYouSkipped, "You skipped it.", e.clock.Now())
		e.save(ctx, m)

		return nil
	})
	if err != nil {
		return Match{}, err
	}

	return s.matchByID(ctx, id)
}

// matchByID returns the match id.
func (s *Service) matchByID(ctx context.Context, id int64) (Match, error) {
	got, err := s.matchesByID(ctx, []int64{id})
	if err != nil {
		return Match{}, err
	}

	if len(got) == 0 {
		return Match{}, uierr.New("That match is gone.", ErrNoMatch)
	}

	return got[0], nil
}

// RunNow runs the routine id on the item now, as the user asked from the item's menu, and
// returns its match: once per click, whatever its trigger says, on an item its conditions
// match, or, with anyway (the menu's Run anyway, which the user chose knowing why it
// doesn't match), on any item of a kind it acts on. It counts toward no daily cap or try,
// needs no OK, and keeps to the routine's mode, so a Dry run records what it would have
// done. Every limit that keeps a start from happening still holds it, as for any match:
// the outside contributor rule, this Mac's readiness, the item read fresh, the holds.
func (s *Service) RunNow(ctx context.Context, id, itemID int64, anyway bool) (Match, error) {
	e, err := s.engineOrErr()
	if err != nil {
		return Match{}, err
	}

	var made int64

	err = e.do(ctx, func(ctx context.Context) error {
		made, err = e.runNow(ctx, id, itemID, anyway)

		return err
	})
	if err != nil {
		return Match{}, err
	}

	return s.matchByID(ctx, made)
}

// runNow records a manual match of the routine id on the item, on one its conditions don't
// match too when anyway.
func (e *Engine) runNow(ctx context.Context, id, itemID int64, anyway bool) (int64, error) {
	row, err := e.svc.store.get(ctx, e.svc.store.q, id)
	if err != nil {
		return 0, err
	}

	l := load(row.Spec)
	if l.broken != nil {
		return 0, uierr.New(brokenWords(l.broken)+" It can't run.", fmt.Errorf("%w: %w", ErrBroken, l.broken))
	}

	now := e.clock.Now()

	all, err := e.items.Facts(ctx, []int64{itemID}, now.UTC())
	if err != nil {
		return 0, uierr.New("The item's facts couldn't be read.", err)
	}

	if len(all) == 0 {
		return 0, uierr.New("The item isn't stored on this Mac.", ErrNoItems)
	}

	token := strconv.FormatInt(now.UnixNano(), 36)
	d := eval.Evaluate(l.spec, all[0], eval.History{Manual: token, OwnHeads: []string{}}, now)

	switch {
	case d.Match == eval.Yes:
	case !anyway:
		return 0, uierr.New(textutil.Sentence("It doesn't match it: "+firstNo(d)), ErrNoMatch)
	case !slices.Contains(actsOn(l.spec), all[0].Item.Kind):
		return 0, uierr.New(fmt.Sprintf("It acts only on %s.", kindsWords(actsOn(l.spec))), ErrNoMatch)
	default:
		d.FireKey = "manual:" + token
	}

	r := &routineRun{row: row, spec: l.spec, mode: storedMode(row.Mode), hash: specHash(row.Spec)}
	p := &pass{e: e, now: now, cycle: &Cycle{}}
	viewer := e.svc.viewer(ctx)
	p.inbox.Viewer = viewer

	m := p.newMatch(r, all[0], d, r.mode == autopilotprefs.ModeDryRun)
	m.Manual, m.Approved = true, true

	inserted, err := e.ledger.insert(ctx, m)
	if err != nil || !inserted {
		return 0, uierr.New("The run couldn't be recorded.", errors.Join(err, errLostRace))
	}

	e.Wake()

	return m.ID, nil
}

// TryAgain resets the routine id's tries on the item, and opens again its match that gave
// up, when the occurrence it was for still holds, with the user's OK; it returns the
// matches it opened.
func (s *Service) TryAgain(ctx context.Context, id, itemID int64) ([]Match, error) {
	e, err := s.engineOrErr()
	if err != nil {
		return []Match{}, err
	}

	var opened []int64

	err = e.do(ctx, func(ctx context.Context) error {
		opened, err = e.tryAgain(ctx, id, itemID)

		return err
	})
	if err != nil {
		return []Match{}, err
	}

	return s.matchesByID(ctx, opened)
}

// tryAgain resets the tries and reopens what gave up.
func (e *Engine) tryAgain(ctx context.Context, id, itemID int64) ([]int64, error) {
	row, err := e.svc.store.get(ctx, e.svc.store.q, id)
	if err != nil {
		return nil, err
	}

	now := e.clock.Now()

	if err = e.ledger.q.UpsertTries(ctx, queries.UpsertTriesParams{RoutineID: id, ItemID: itemID, ResetAt: now}); err != nil {
		return nil, uierr.New("The tries couldn't be reset.", err)
	}

	sp, loads := loadedSpec(row.Spec)
	if !loads {
		return []int64{}, nil
	}

	key, keyErr := e.fireKeyNow(ctx, sp, id, itemID)
	if keyErr != nil || key == "" {
		return []int64{}, keyErr
	}

	opened, err := e.reopenGaveUp(ctx, id, itemID, key)
	e.Wake()

	return opened, err
}

// reopenGaveUp opens again, with the user's OK, the routine id's match on the item for
// the occurrence key that gave up, and returns those it opened.
func (e *Engine) reopenGaveUp(ctx context.Context, id, itemID int64, key string) ([]int64, error) {
	rows, err := e.ledger.q.ListItemMatches(ctx, queries.ListItemMatchesParams{ItemID: new(itemID), RowLimit: maxItemHistory})
	if err != nil {
		return nil, uierr.New("The item's history couldn't be read.", err)
	}

	opened := []int64{}

	for _, row := range rows {
		if row.RoutineID != id || row.FireKey != key || State(row.State) != StateSkipped ||
			Reason(row.Reason) != ReasonTries {
			continue
		}

		m, readErr := readMatch(row)
		if readErr != nil {
			return nil, readErr
		}

		m.State, m.Reason, m.Detail, m.ClosedAt, m.Approved = string(StateQueued), "", "", nil, true
		e.save(ctx, m)
		opened = append(opened, m.ID)
	}

	return opened, nil
}

// fireKeyNow returns the occurrence the routine matches the item for now, "" when it
// doesn't.
func (e *Engine) fireKeyNow(ctx context.Context, sp spec.Spec, id, itemID int64) (string, error) {
	at := e.clock.Now()

	all, err := e.items.Facts(ctx, []int64{itemID}, at.UTC())
	if err != nil || len(all) == 0 {
		return "", err
	}

	h, _, err := e.svc.itemLedger(ctx, e.ledger, id, itemID)
	if err != nil {
		return "", err
	}

	h.Tries = 0

	d := eval.Evaluate(sp, all[0], h, at)
	if d.Match != eval.Yes {
		return "", nil
	}

	return d.FireKey, nil
}

// LetActAgain lets the routine id act on the item again, which it left alone since the
// user reversed what it did there.
func (s *Service) LetActAgain(ctx context.Context, id, itemID int64) error {
	e, err := s.engineOrErr()
	if err != nil {
		return err
	}

	return e.do(ctx, func(ctx context.Context) error {
		if _, delErr := e.ledger.q.DeleteHandsOff(ctx, queries.DeleteHandsOffParams{RoutineID: id, ItemID: itemID}); delErr != nil {
			return uierr.New("The routine couldn't be let act again.", delErr)
		}

		e.Wake()

		return nil
	})
}

// ClearHistory removes, at once rather than after the history's days, every closed match
// whose item is done with (gone, merged or closed, or out of every list and watch), with
// its runs; a closed match that still blocks its occurrence stays, without its trace and
// summaries, so nothing fires again. Open matches and the routines stay.
func (s *Service) ClearHistory(ctx context.Context) (Cleared, error) {
	e, err := s.engineOrErr()
	if err != nil {
		return Cleared{}, err
	}

	var out Cleared

	err = e.do(ctx, func(ctx context.Context) error {
		now := e.clock.Now()
		gone := now.Add(-time.Duration(e.svc.settings(ctx).HistoryDays) * 24 * time.Hour)
		cleared, clearErr := e.clear(ctx, now.Add(1), gone, true)
		out = cleared

		if clearErr != nil {
			return uierr.New("The history couldn't be cleared.", clearErr)
		}

		return nil
	})

	return out, err
}

// StopRun stops the run id at once, through the runner: never queued behind the engine,
// so it works before the first refresh, offline, or signed out. The engine hears of the
// end from the runner.
func (s *Service) StopRun(ctx context.Context, id int64) error {
	e, err := s.engineOrErr()
	if err != nil {
		return err
	}

	if e.runner == nil {
		return uierr.New("There's no run to stop.", ErrNoRunner)
	}

	run, err := e.ledger.q.GetRun(ctx, id)
	if err != nil || run.Status == runEnded {
		return uierr.New("That run isn't going.", errors.Join(ErrNoRun, err))
	}

	if err = e.runner.Stop(ctx, id); err != nil {
		return uierr.New("The run couldn't be stopped.", err)
	}

	return nil
}

// StopAll stops every agent run at once, through the runner, and leaves it open for later
// starts.
func (s *Service) StopAll(ctx context.Context) error {
	e, err := s.engineOrErr()
	if err != nil {
		return err
	}

	if e.runner == nil {
		return nil
	}

	if err = e.runner.StopAll(ctx); err != nil {
		return uierr.New("The runs couldn't be stopped.", err)
	}

	return nil
}

// Matching is what the question about current matches says: how many items the routine
// matches now that it has no record of, by kind. Only with none, none it can't decide, and
// nothing left unread, is nothing asked.
type Matching struct {
	Count        int `json:"count"`
	PullRequests int `json:"pullRequests"`
	Issues       int `json:"issues"`

	// Unknown counts the items it can't decide yet.
	Unknown int `json:"unknown"`

	// Incomplete reports a count that may be short, since not everything the routine
	// watches was read: a search wasn't (the budget was low, or GitHub didn't answer it), or
	// matched more than the page a search returns, or the routine watches more items than a
	// test tries. The engine, reading them later, may act on more than Count says.
	Incomplete bool `json:"incomplete"`
}

// Matching counts the items the draft d matches now that its routine (d.ID, 0 for a new
// one) has no record of, reading its searches now unless a test read them within a minute,
// for the question asked when it is turned on, leaves Dry run, or its Watch, When, If, or
// Once per changes: "3 pull requests match right now", and says when the count may be
// short (Incomplete). It changes nothing.
func (s *Service) Matching(ctx context.Context, d Draft) (Matching, error) {
	res, err := s.Test(ctx, d)
	if err != nil {
		return Matching{}, err
	}

	recorded, err := s.recorded(ctx, d.ID)
	if err != nil {
		return Matching{}, err
	}

	out := Matching{Incomplete: incomplete(res)}

	for _, it := range res.Items {
		out.add(it, recorded)
	}

	return out, nil
}

// incomplete reports a test that didn't read everything its routine watches: a search with
// no answer, or past a page, the budget too low to read, or more items than it tries.
func incomplete(res TestResult) bool {
	return res.LowBudget || res.Watched > len(res.Items) ||
		slices.ContainsFunc(res.Searches, func(ts TestSearch) bool { return ts.Error != "" || ts.Truncated })
}

// add counts it, unless the routine has a record of its occurrence, or leaves it alone.
func (m *Matching) add(it TestItem, recorded map[matchKey]bool) {
	if it.LeftAlone {
		return
	}

	switch it.Match {
	case eval.Yes:
		if recorded[matchKey{item: it.ID, fire: it.FireKey}] {
			return
		}

		m.Count++

		if it.Kind == item.KindIssue {
			m.Issues++
		} else {
			m.PullRequests++
		}
	case eval.Unknown:
		m.Unknown++
	case eval.No:
	}
}

// recorded returns the occurrences the routine id has a real record of: a Dry run's, and a
// match that closed without having run, which opens again when it matches, are none.
func (s *Service) recorded(ctx context.Context, id int64) (map[matchKey]bool, error) {
	out := map[matchKey]bool{}
	if id == 0 {
		return out, nil
	}

	got, err := s.ledger().keys(ctx, id)
	if err != nil {
		return nil, uierr.New("The routine's history couldn't be read.", err)
	}

	for k, row := range got {
		if !k.dryRun && !reopensRow(row) {
			out[matchKey{item: k.item, fire: k.fire}] = true
		}
	}

	return out, nil
}

// fromNow records that the routine id's current matches become its baseline, once its
// searches are read: the user chose only what changes from now on.
func (e *Engine) fromNow(ctx context.Context, id int64, dry bool) {
	e.state.FromNow[id] = fromNow{Since: e.clock.Now().UTC(), DryRun: dry}
	e.saveState(ctx)
	e.Wake()
}
