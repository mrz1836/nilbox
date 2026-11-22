package autopilot

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/eval"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// Cycle is what one of the engine's cycles did.
type Cycle struct {
	// At is when it ran, and TookMS how long it took, in milliseconds.
	At     time.Time `json:"at"`
	TookMS int64     `json:"tookMs"`

	// Waiting says why it evaluated nothing: "refresh" while the first refresh that
	// carries the routines' searches hasn't come, "" when it evaluated.
	Waiting string `json:"waiting"`

	// Routines is how many routines it evaluated, over Items items.
	Routines int `json:"routines"`
	Items    int `json:"items"`

	// Matches is how many matches it recorded, Starts how many runs it began, Closed how
	// many matches it closed, and Reads how many reads of GitHub it asked for.
	Matches int `json:"matches"`
	Starts  int `json:"starts"`
	Closed  int `json:"closed"`
	Reads   int `json:"reads"`

	// CatchUp reports a cycle after a long gap, in which some routine's agent and GitHub
	// steps wait for the user.
	CatchUp bool `json:"catchUp"`
}

// routineRun is one routine as a cycle runs it.
type routineRun struct {
	row   queries.AutopilotRoutine
	spec  spec.Spec
	mode  autopilotprefs.Mode
	hash  string
	keys  []string
	lists []catalog.ListSource

	// watched are the items it watches now, and decided its decision on each.
	watched []int64
	inWatch map[int64]bool
	decided map[int64]eval.Decision

	// matches are its matches by occurrence, and seen what it first saw of each item, the
	// ledger's keys the engine keeps (ledgerindex.go), which change as the engine writes
	// them; handsOff are the items it leaves alone, and reset when Try again last reset each
	// item's tries.
	matches  map[matchKey]keyRow
	seen     map[int64]baseline
	handsOff map[int64]bool
	reset    map[int64]time.Time

	// lastClosed is, by item, the last of its matches that closed, as they were when the
	// cycle began.
	lastClosed map[int64]keyRow

	// fresh reports that its searches were each read since it last changed, and
	// firstPass a first pass after it changed, which caps what it queues.
	fresh     bool
	firstPass bool

	// plan is its spec prepared for deciding, the first time it decides (decide). A run
	// is built afresh from the stored routine each cycle, so it is never out of date.
	plan *eval.Plan
}

// id returns the routine's ID.
func (r *routineRun) id() int64 { return r.row.ID }

// decide decides the routine on the item f describes, with history h, at now: its spec
// is prepared once, so deciding it on every item it watches works out what its spec
// alone says only once.
func (r *routineRun) decide(f facts.Facts, h eval.History, now time.Time) eval.Decision {
	if r.plan == nil {
		r.plan = eval.Prepare(r.spec)
	}

	return r.plan.Evaluate(f, h, now)
}

// passKey is the fingerprint of what a first pass ran under: the spec, when the routine
// was last switched on, and its mode, so switching it off and on, or changing its mode,
// is a first pass again, even in another copy of the app between two cycles of this one.
func (r *routineRun) passKey() string {
	var on string
	if r.row.EnabledAt != nil {
		on = r.row.EnabledAt.UTC().Format(time.RFC3339Nano)
	}

	return r.hash + "|" + on + "|" + string(r.mode)
}

// pass is one cycle's reads, shared by its steps.
type pass struct {
	e        *Engine
	now      time.Time
	cfg      autopilotprefs.Config
	inbox    InboxStatus
	routines []*routineRun
	byID     map[int64]*routineRun
	others   map[int64]queries.AutopilotRoutine
	facts    map[int64]facts.Facts
	states   map[int64]ItemState
	local    map[int64]string
	runs     map[int64][]itemRun
	counts   dayCounts
	cycle    *Cycle

	// checks are the pull requests whose decision waits on their check names, and
	// stateReads the items that left a watch whose state is read before they close.
	checks     []int64
	stateReads []int64

	// itemReads are the items a verifying match reads again, and verifying the agent's run
	// each verifying match follows, by match.
	itemReads []int64
	verifying map[int64]int64

	// active holds, by routine and item, the routine's match running or verifying on the
	// item: another of its matches there waits for it.
	active map[[2]int64]int64

	// notices are the notices the cycle's steps tell, handed over together as it ends.
	notices []Job

	// ready is this Mac's readiness as the last checks found it, and outside where agents
	// may run on outside contributors' items.
	ready   readySnapshot
	outside OutsideIndex

	// scripts are the user's own scripts by ID, read once a cycle when a routine runs one,
	// and scriptsFailed reports a read that failed (scriptsOf).
	scripts       map[int64]MyScript
	scriptsFailed bool
}

// cycle runs one cycle: it reads what the routines watch, evaluates each routine on its
// items, records and closes matches, and starts what may start.
func (e *Engine) cycle(ctx context.Context) {
	started := time.Now()
	c := &Cycle{At: e.clock.Now().UTC()}

	defer func() {
		c.TookMS = time.Since(started).Milliseconds()
		e.finishCycle(ctx, c)
	}()

	inbox, err := e.items.InboxStatus(ctx)
	if err != nil {
		e.logger.WarnContext(ctx, "Autopilot could not read the inbox's status", "error", err)

		return
	}

	e.setStatus(func(s *EngineStatus) { s.SearchesPaused, s.SignedOut = inbox.WatchPaused, inbox.SignedOut })

	if !e.prime(ctx, inbox) {
		c.Waiting = "refresh"

		return
	}

	p := &pass{
		e: e, now: e.clock.Now(), cfg: e.svc.settings(ctx), inbox: inbox, byID: map[int64]*routineRun{},
		others: map[int64]queries.AutopilotRoutine{}, cycle: c, ready: e.svc.ready.snapshot(),
		outside: e.outsideIndex(ctx), verifying: map[int64]int64{}, active: map[[2]int64]int64{},
	}

	if err = p.run(ctx); err != nil && ctx.Err() == nil {
		e.logger.WarnContext(ctx, "an Autopilot cycle failed", "error", err)
	}
}

// finishCycle records and logs what a cycle did, keeps the engine's state, and tells the
// window.
func (e *Engine) finishCycle(ctx context.Context, c *Cycle) {
	e.state.LastCycle = c
	e.keepCycle(ctx)
	e.tellWaits(ctx)

	e.setStatus(func(s *EngineStatus) {
		s.Primed, s.LastCycle = e.primed, new(*c)
		s.Problems = slices.Clone(e.state.Problems)
		s.PausedSince = e.state.PausedSince
	})

	args := []any{
		"took_ms", c.TookMS, "routines", c.Routines, "items", c.Items, "matches", c.Matches, "starts", c.Starts,
		"closed", c.Closed, "reads", c.Reads, "waiting", c.Waiting, "catch_up", c.CatchUp,
	}

	if c.Matches+c.Starts+c.Closed > 0 {
		e.logger.InfoContext(ctx, "Autopilot cycle", args...)
	} else {
		e.logger.DebugContext(ctx, "Autopilot cycle", args...)
	}

	e.notify()
}

// keepCycle keeps the engine's state once a cycle ran, when it changed, the last cycle
// aside, or the last cycle kept is a tick old: a steady cycle writes nothing, so it adds
// no write to every tick, and leaves the database's pages cached for the next.
func (e *Engine) keepCycle(ctx context.Context) {
	rest := e.state
	rest.LastCycle = nil

	key, err := store.EncodeJSON(rest)
	if err == nil && key == e.keptKey && e.clock.Now().Sub(e.keptAt) < e.tick {
		return
	}

	if err = writeEngineState(ctx, e.prefs, e.state); err != nil {
		e.logger.WarnContext(ctx, "could not save Autopilot's engine state", "error", err)

		return
	}

	e.keptKey, e.keptAt = key, e.clock.Now()
}

// prime reports whether the engine has seen a refresh that carried its searches since it
// was built, and a watch pass since Autopilot was last turned on, as the inbox kept
// refreshing while it was off but sent no search. Until it has, it asks the inbox for the
// searches once a refresh stored the account: the very first refresh stores the account
// before any search can be built.
func (e *Engine) prime(ctx context.Context, inbox InboxStatus) bool {
	if e.primed {
		return true
	}

	if inbox.LastRefresh.Before(e.since) {
		return false
	}

	set := e.svc.currentWatches()
	if !sameLogin(set.Login, inbox.Viewer) {
		set = e.svc.rebuildWatches(ctx)
	}

	if len(set.Searches) == 0 || !inbox.WatchedAt.Before(e.onSince) {
		e.primed = true
		e.logger.InfoContext(ctx, "Autopilot saw its first refresh")

		return true
	}

	e.askWatches(ctx)

	return false
}

// askWatches has the inbox read the routines' searches now, at most once every
// askAgainAfter.
func (e *Engine) askWatches(ctx context.Context) {
	if now := time.Now(); now.Sub(e.asked) >= askAgainAfter || e.asked.IsZero() {
		e.asked = now

		e.goWork(ctx, func(ctx context.Context) {
			if err := e.items.WatchNow(ctx); err != nil && ctx.Err() == nil {
				e.logger.InfoContext(ctx, "Autopilot could not read the routines' searches", "error", err)
			}
		})
	}
}

// run is one cycle's work, once the engine is primed. The notices its steps tell go out as
// it ends, however it ends, so the matches that tell them never wait on a later cycle.
func (p *pass) run(ctx context.Context) error {
	defer p.tellNotices(ctx)

	if err := p.loadRoutines(ctx); err != nil {
		return err
	}

	p.watchGaps()
	p.cycle.CatchUp = len(p.e.state.CatchingUp) > 0

	if err := p.readRoutines(ctx); err != nil {
		return err
	}

	if err := p.resumePastRules(ctx); err != nil {
		return err
	}

	open, err := p.e.ledger.open(ctx)
	if err != nil {
		return err
	}

	if err = p.read(ctx, open); err != nil {
		return err
	}

	p.scriptsOf(ctx)

	for _, r := range p.routines {
		if err = p.evaluate(ctx, r); err != nil {
			return err
		}
	}

	p.settleOpen(ctx, open)
	p.askReads(ctx)
	p.e.checkReady(ctx, p)
	p.e.updateProblems(ctx, p)
	p.e.prune(ctx, p.now, p.cfg)
	p.endCatchUp()
	p.e.forgetFreshReads(p.now)

	return nil
}

// watchGaps keeps when Pause Autopilot began and the last refresh seen, and begins a
// catch-up after a long gap (longer than the catch-up setting): on Resume after a long
// pause, on the first refresh after a long time without one, and, at once, on a cycle that
// finds the last refresh long ago, as on waking from a long sleep before any refresh came,
// so no step is decided on what was read before the gap without the user.
func (p *pass) watchGaps() {
	st := &p.e.state
	gap := p.catchUpAfter()
	last := p.inbox.LastRefresh.UTC()

	switch {
	case p.cfg.Paused && st.PausedSince == nil:
		st.PausedSince = new(p.now.UTC())
	case !p.cfg.Paused && st.PausedSince != nil:
		if p.now.Sub(*st.PausedSince) > gap {
			p.beginCatchUp(p.now)
		}

		st.PausedSince = nil
	}

	if !st.LastRefresh.IsZero() && last.Sub(st.LastRefresh) > gap {
		p.beginCatchUpAfter(st.LastRefresh, last)
	}

	if p.now.Sub(last) > gap {
		p.beginCatchUpAfter(last, p.now)
	}

	if last.After(st.LastRefresh) {
		st.LastRefresh = last
	}
}

// catchUpAfter is how long a gap makes the engine ask before it catches up.
func (p *pass) catchUpAfter() time.Duration {
	return time.Duration(p.cfg.CatchUpHours) * time.Hour
}

// beginCatchUpAfter begins a catch-up at, after a gap that began at gapStart, unless one
// began within that gap already.
func (p *pass) beginCatchUpAfter(gapStart, at time.Time) {
	if since := p.e.state.CatchUpSince; since != nil && since.After(gapStart) {
		return
	}

	p.beginCatchUp(at)
}

// beginCatchUp begins a catch-up at: every routine on now is in it, until what it watches
// was read after then (endCatchUp).
func (p *pass) beginCatchUp(at time.Time) {
	st := &p.e.state
	st.CatchUpSince = new(at.UTC())
	st.CatchingUp = make(map[int64]bool, len(p.routines))

	for _, r := range p.routines {
		st.CatchingUp[r.id()] = true
	}
}

// catchingUp reports a routine in the catch-up under way, whose agent and GitHub steps
// wait for the user's OK.
func (p *pass) catchingUp(r *routineRun) bool {
	return p.e.state.CatchingUp[r.id()]
}

// endCatchUp ends the catch-up of each routine that this cycle, which still asked, decided
// on what was read after it began (readSince). A routine whose own search keeps failing
// stays in it, and the others go on.
func (p *pass) endCatchUp() {
	st := &p.e.state
	if st.CatchUpSince == nil || len(st.CatchingUp) == 0 {
		return
	}

	for _, r := range p.routines {
		if st.CatchingUp[r.id()] && p.readSince(r, *st.CatchUpSince) {
			delete(st.CatchingUp, r.id())
		}
	}
}

// readSince reports whether what r watches was read after since: each of its searches
// answered, with no error, and the inbox refreshed, when it watches the inbox's lists or
// has no search.
func (p *pass) readSince(r *routineRun, since time.Time) bool {
	if (len(r.keys) == 0 || len(r.lists) > 0) && p.inbox.LastRefresh.Before(since.Truncate(time.Second)) {
		return false
	}

	for _, key := range r.keys {
		if s := p.inbox.Searches[key]; !s.Answered || s.Error != "" || s.At.Before(since) {
			return false
		}
	}

	return true
}

// searchFailing reports a routine whose last read of one of its searches failed.
func (p *pass) searchFailing(r *routineRun) bool {
	for _, key := range r.keys {
		if p.inbox.Searches[key].Error != "" {
			return true
		}
	}

	return false
}

// loadRoutines loads the enabled routines of the account signed in, in the user's order;
// one that won't load is recorded as a problem and skipped.
func (p *pass) loadRoutines(ctx context.Context) error {
	rows, err := p.e.svc.store.all(ctx)
	if err != nil {
		return err
	}

	for _, row := range rows {
		l := load(row.Spec)

		switch {
		case !row.Enabled || otherAccount(row.AccountLogin, p.inbox.Viewer):
			p.others[row.ID] = row

			continue
		case l.broken != nil:
			p.others[row.ID] = row

			continue
		}

		r := &routineRun{
			row: row, spec: l.spec, mode: storedMode(row.Mode), hash: specHash(row.Spec), lists: listsOf(l.spec),
			inWatch: map[int64]bool{}, decided: map[int64]eval.Decision{},
		}

		for _, q := range searchesOf(l.spec, p.inbox.Viewer) {
			r.keys = append(r.keys, q.Key)
		}

		p.routines = append(p.routines, r)
		p.byID[row.ID] = r
	}

	p.cycle.Routines = len(p.routines)
	p.e.state.forget(rows)

	return nil
}

// forget drops what the state keeps of routines that no longer need it: a deleted
// routine's first pass, baseline to come, and catch-up, a routine switched off's too, and
// the first pass of one in Dry run.
func (st *engineState) forget(rows []queries.AutopilotRoutine) {
	live := make(map[int64]queries.AutopilotRoutine, len(rows))
	for _, row := range rows {
		live[row.ID] = row
	}

	off := func(id int64) bool {
		row, ok := live[id]

		return !ok || !row.Enabled
	}

	maps.DeleteFunc(st.FirstPass, func(id int64, _ string) bool {
		return off(id) || storedMode(live[id].Mode) == autopilotprefs.ModeDryRun
	})
	maps.DeleteFunc(st.FromNow, func(id int64, _ fromNow) bool { return off(id) })
	maps.DeleteFunc(st.CatchingUp, func(id int64, _ bool) bool { return off(id) })
}

// read reads what every routine watches, those items' facts, the states of the open
// matches' items, the runs on all of them, and the day's counts.
func (p *pass) read(ctx context.Context, open []*match) error {
	all, err := p.readWatched(ctx)
	if err != nil {
		return err
	}

	every := union(all, p.openItems(open))

	// The items' triage is read before their facts, so a triage step's compare-and-set is
	// on a state no newer than what the routine decided on.
	if p.local, err = p.e.items.LocalStates(ctx, every); err != nil {
		return err
	}

	// The open matches' items are read too: one verifying, or a later step's, may have
	// left every watch.
	list, err := p.e.items.Facts(ctx, every, p.now.UTC())
	if err != nil {
		return err
	}

	p.facts = make(map[int64]facts.Facts, len(list))
	for _, f := range list {
		p.facts[f.Item.ID] = f
	}

	if p.states, err = p.e.items.ItemStates(ctx, every); err != nil {
		return err
	}

	if p.runs, err = p.e.ledger.itemRuns(ctx, every); err != nil {
		return err
	}

	p.counts, err = p.e.dayCounts(ctx, p.now)

	return err
}

// openItems returns the items of the open matches, and notes each routine's match running
// or verifying on an item (p.active).
func (p *pass) openItems(open []*match) []int64 {
	out := make([]int64, 0, len(open))

	for _, m := range open {
		if m.ItemID == nil {
			continue
		}

		out = append(out, *m.ItemID)

		if m.state().active() {
			p.active[[2]int64{m.RoutineID, *m.ItemID}] = m.ID
		}
	}

	return out
}

// readWatched reads the items each routine watches, and returns them all, each once.
func (p *pass) readWatched(ctx context.Context) ([]int64, error) {
	var all []int64

	for _, r := range p.routines {
		ids, err := p.e.items.WatchedItems(ctx, r.keys, r.lists)
		if err != nil {
			return nil, err
		}

		r.watched = ids
		for _, id := range ids {
			r.inWatch[id] = true
		}

		all = append(all, ids...)
	}

	all = union(all, nil)
	p.cycle.Items = len(all)

	return all, nil
}

// readRoutines reads what the ledger keeps of every routine the cycle runs (readLedger).
func (p *pass) readRoutines(ctx context.Context) error {
	return p.readLedger(ctx, p.routines)
}

// readLedger reads what the ledger keeps of each of routines: its matches and what it first
// saw of each item from the keys the engine keeps in memory (ledgerindex.go), which one
// count of the rows tells are current, and the items each leaves alone and its resets of
// the tries in two reads, rather than two for each routine, since another copy of the app
// records the items a routine leaves alone too.
func (p *pass) readLedger(ctx context.Context, routines []*routineRun) error {
	if len(routines) == 0 {
		return nil
	}

	x := p.e.ledger.index
	if err := x.ensure(ctx, p.e.ledger.q); err != nil {
		return err
	}

	byID := make(map[int64]*routineRun, len(routines))
	ids := make([]int64, 0, len(routines))

	for _, r := range routines {
		byID[r.id()] = r
		ids = append(ids, r.id())
		r.matches, r.seen = x.matchesOf(r.id()), x.seenOf(r.id())
		r.lastClosed = lastClosed(r.matches)
		r.handsOff, r.reset = map[int64]bool{}, map[int64]time.Time{}
	}

	q := p.e.ledger.q

	off, err := q.ListRoutinesHandsOff(ctx, ids)
	if err != nil {
		return fmt.Errorf("read the items the routines leave alone: %w", err)
	}

	for _, h := range off {
		byID[h.RoutineID].handsOff[h.ItemID] = true
	}

	tries, err := q.ListRoutinesTries(ctx, ids)
	if err != nil {
		return fmt.Errorf("read the routines' tries: %w", err)
	}

	for _, t := range tries {
		byID[t.RoutineID].reset[t.ItemID] = t.ResetAt
	}

	return nil
}

// evaluate decides the routine r on each item it watches, recording what it first sees
// of each, and records each new match.
func (p *pass) evaluate(ctx context.Context, r *routineRun) error {
	_, fromNowAsked := p.e.state.FromNow[r.id()]
	if p.waitsForSearches(ctx, r) {
		return nil
	}

	var made []*match

	for _, id := range r.watched {
		m, err := p.evaluateItem(ctx, r, id, fromNowAsked)
		if err != nil {
			return err
		}

		if m != nil {
			made = append(made, m)
		}
	}

	if fromNowAsked {
		delete(p.e.state.FromNow, r.id())
	}

	return p.queue(ctx, r, made)
}

// waitsForSearches reports whether r waits for its searches to be read since it changed:
// a routine whose current matches become its baseline, and one with a GitHub action on
// its first pass, so what they take as matching now is what GitHub says now, all of it.
// The inbox's status is read before the items, so searches read since say every item they
// returned is there. While it waits, its items are decided, and nothing is recorded.
func (p *pass) waitsForSearches(ctx context.Context, r *routineRun) bool {
	pending, fromNowAsked := p.e.state.FromNow[r.id()]
	r.fresh = p.searchesRead(r, pending.Since)
	firstPass := hasGitHubStep(r.spec) && r.mode != autopilotprefs.ModeDryRun && p.e.state.FirstPass[r.id()] != r.passKey()
	r.firstPass = firstPass && r.fresh

	if (!fromNowAsked && !firstPass) || r.fresh {
		return false
	}

	p.e.askWatches(ctx)
	p.decideAll(r)

	return true
}

// decideAll decides r on each item it watches, recording nothing: what its open matches
// are settled with while it waits for its searches.
func (p *pass) decideAll(r *routineRun) {
	for _, id := range r.watched {
		if f, ok := p.facts[id]; ok && !r.handsOff[id] {
			r.decided[id] = r.decide(f, p.historyOf(r, id), p.now)
		}
	}
}

// evaluateItem decides r on the item id, records what r first sees of it, and returns
// the match it records, nil for none. A decision that waits on check names asks for them.
func (p *pass) evaluateItem(ctx context.Context, r *routineRun, id int64, fromNow bool) (*match, error) {
	f, ok := p.facts[id]
	if !ok || r.handsOff[id] {
		return nil, nil //nolint:nilnil // an item not stored, or left alone, makes no match
	}

	d := r.decide(f, p.historyOf(r, id), p.now)
	r.decided[id] = d

	if err := p.see(ctx, r, f, d); err != nil {
		return nil, err
	}

	switch d.Match {
	case eval.Yes:
		return p.record(ctx, r, f, d, fromNow)
	case eval.Unknown:
		if slices.Contains(d.Needs, facts.NeedChecks) && f.Item.Kind == item.KindPR {
			p.checks = append(p.checks, id)
		}
	case eval.No:
	}

	return nil, nil //nolint:nilnil // a decision that isn't yes makes no match
}

// searchesRead reports whether each of r's searches was read since r last changed, and
// since a moment, so what it matches now is what GitHub says now.
func (p *pass) searchesRead(r *routineRun, after time.Time) bool {
	since := r.row.UpdatedAt
	if after.After(since) {
		since = after
	}

	for _, key := range r.keys {
		s, ok := p.inbox.Searches[key]
		if !ok || !s.Answered || s.At.Before(since) {
			return false
		}
	}

	return true
}

// hasGitHubStep reports a routine with a GitHub action, whose first pass is capped.
func hasGitHubStep(sp spec.Spec) bool {
	return slices.ContainsFunc(sp.Steps, func(st spec.Step) bool { return st.Kind() == catalog.StepGitHub })
}

// historyOf returns what the ledger knows of r and the item, for the evaluator.
func (p *pass) historyOf(r *routineRun, id int64) eval.History {
	b, seen := r.seen[id]

	return history(r.id(), p.runs[id], b, seen, r.reset[id], p.lastOutcome(r, id))
}

// lastOutcome says how r's last closed match on the item ended, as r's matches were read
// as the cycle began (lastClosed).
func (p *pass) lastOutcome(r *routineRun, id int64) string {
	last, ok := r.lastClosed[id]
	if !ok {
		return ""
	}

	return outcomeWord(last.State, last.Reason)
}

// lastClosed returns, by item, the last of matches that closed, a Dry run's and a
// baseline's apart: what a routine's history says it last did there, found in one pass
// rather than once per item.
func lastClosed(matches map[matchKey]keyRow) map[int64]keyRow {
	out := map[int64]keyRow{}

	for k, row := range matches {
		if k.dryRun || row.State.Open() || row.State == StateBaseline {
			continue
		}

		if last, ok := out[k.item]; !ok || row.ID > last.ID {
			out[k.item] = row
		}
	}

	return out
}

// outcomeWord says how a match closed, as the catalog's outcome choices name it.
func outcomeWord(s State, r Reason) string {
	switch {
	case s == StateSkipped && r == ReasonTries:
		return catalog.OutcomeGaveUp
	case s == StateDismissed:
		return catalog.OutcomeDismissed
	case s == StateDone && r == ReasonFailed:
		return catalog.OutcomeFailed
	case s == StateDone:
		return catalog.OutcomeDone
	}

	return ""
}

// see records what r first sees of an item (its head commit and its comments), the head
// once it is known, and the comments a New comments match fired at.
func (p *pass) see(ctx context.Context, r *routineRun, f facts.Facts, d eval.Decision) error {
	b, seen := r.seen[f.Item.ID]
	fired := d.Match == eval.Yes && r.spec.Trigger.Event == catalog.EventNewComments
	next := seeing(b, f, fired)

	switch {
	case seen && next.Head == b.Head && sameCount(next.Comments, b.Comments):
		return nil
	case !seen && next.Head == "" && next.Comments == nil:
		return nil
	}

	// The ledger notes what it wrote in the keys, r.seen among them.
	return p.e.ledger.saveBaseline(ctx, r.id(), f.Item.ID, next, !seen)
}

// seeing returns what a routine keeps of an item after seeing f: the head once it is
// known, and the comments it first saw, or, when it fired on them, counted now.
func seeing(b baseline, f facts.Facts, fired bool) baseline {
	next := b

	if next.Head == "" && f.Head.Known() {
		next.Head = f.Head.Text
	}

	if comments := f.Value(catalog.FieldComments); comments.Known() && (next.Comments == nil || fired) {
		next.Comments = new(comments.Number)
	}

	return next
}

// sameCount reports two counts the same, nil alike.
func sameCount(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}

	return *a == *b
}

// record records r's match d on an item, unless it is there already, and returns the new
// or reopened match, nil for none. A match that already matched when the user chose "only
// what changes from now on" is recorded as baseline; a Dry run's is recorded closed, as
// what would have run.
func (p *pass) record(ctx context.Context, r *routineRun, f facts.Facts, d eval.Decision, fromNow bool,
) (*match, error) {
	dry := r.mode == autopilotprefs.ModeDryRun
	key := matchKey{item: f.Item.ID, fire: d.FireKey, dryRun: dry}

	if row, ok := r.matches[key]; ok {
		if fromNow || !reopensRow(row) {
			return nil, nil //nolint:nilnil // a match there already is no new match
		}

		return p.reopen(ctx, r, row.ID, d)
	}

	m := p.newMatch(r, f, d, dry)

	if fromNow {
		m.steps = []stepRun{}
		m.close(StateBaseline, ReasonFromNow, "It already matched when you chose only what changes from now on.", p.now)
	}

	// The ledger notes what it inserted in the keys, r.matches among them.
	inserted, err := p.e.ledger.insert(ctx, m)
	if err != nil || !inserted {
		return nil, err
	}

	if m.state().Open() {
		p.cycle.Matches++

		return m, nil
	}

	if m.state() == StateDone {
		p.cycle.Matches++
	}

	return nil, nil //nolint:nilnil // a closed record is no match to start
}

// newMatch builds the ledger row for r's match d on f's item: open and queued, or for a
// Dry run closed, as what would have run.
func (p *pass) newMatch(r *routineRun, f facts.Facts, d eval.Decision, dry bool) *match {
	ref := itemRefOf(f.Item)
	owner, name := splitRepo(ref.Repo)

	m := &match{
		AutopilotMatch: queries.AutopilotMatch{
			RoutineID: r.id(), ItemID: new(f.Item.ID), AccountLogin: p.inbox.Viewer, RepoOwner: owner, RepoName: name,
			Kind: string(f.Item.Kind), Number: int64(f.Item.Number), Title: f.Item.Title, URL: f.Item.URL,
			FireKey: d.FireKey, DryRun: dry, State: string(StateQueued), SpecHash: r.hash, CreatedAt: p.now,
			UpdatedAt: p.now, DecidedAt: p.now,
		},
		steps: stepsOf(r.spec), trace: textutil.NonNil(d.Trace),
	}

	if f.Head.Known() {
		m.HeadOID = f.Head.Text
	}

	if am := f.Value(catalog.FieldAutoMerge); am.Known() && f.Item.Kind == "pr" {
		m.AutoMergeAtMatch = new(am.Bool)
	}

	if dry {
		for i := range m.steps {
			m.steps[i].Status, m.steps[i].Note = stepSkipped, "Dry run"
		}

		m.close(StateDone, ReasonWouldRun, "Dry run: it would have run.", p.now)
	}

	return m
}

// itemRefOf copies an item's facts.
func itemRefOf(it facts.Item) ItemRef {
	return ItemRef{ID: it.ID, Kind: it.Kind, Repo: it.Repo, Number: it.Number, Title: it.Title, URL: it.URL}
}

// splitRepo splits "owner/name".
func splitRepo(full string) (string, string) {
	for i := range len(full) {
		if full[i] == '/' {
			return full[:i], full[i+1:]
		}
	}

	return full, ""
}

// reopen opens again a match that closed without having run, now that its occurrence
// matches again: it starts over from its first step, under the routine as it is.
func (p *pass) reopen(ctx context.Context, r *routineRun, id int64, d eval.Decision) (*match, error) {
	m, err := p.e.ledger.get(ctx, id)
	if err != nil {
		return nil, err
	}

	m.State, m.BlockedBy, m.Reason, m.Detail, m.ClosedAt = string(StateQueued), "", "", "", nil
	m.steps, m.Step, m.SpecHash, m.DecidedAt, m.trace = stepsOf(r.spec), 0, r.hash, p.now, textutil.NonNil(d.Trace)

	if err = p.e.ledger.update(ctx, p.e.ledger.q, m); err != nil {
		return nil, err
	}

	p.cycle.Matches++

	return m, nil
}

// queue sets each of r's new matches on its way: on a first pass over FirstCap they wait
// as a backlog, after a long gap an Autopilot routine's wait for the user, and the rest
// advance. It records the first pass as done.
func (p *pass) queue(ctx context.Context, r *routineRun, made []*match) error {
	backlog := r.firstPass && len(made) > FirstCap

	if r.firstPass {
		p.e.state.FirstPass[r.id()] = r.passKey()
	}

	for _, m := range made {
		if backlog && asksFirstAhead(m) {
			m.wait(BlockBacklog, strconv.Itoa(len(made))+" matched at once: run on them from Activity.")

			if err := p.e.ledger.update(ctx, p.e.ledger.q, m); err != nil {
				return err
			}

			continue
		}

		p.advance(ctx, r, m)
	}

	return nil
}

// asksFirstAhead reports a match with an agent or GitHub step still to run.
func asksFirstAhead(m *match) bool {
	for _, sr := range m.steps[min(int(m.Step), len(m.steps)):] {
		if info, ok := catalog.StepByKind(sr.Step.Kind()); ok && info.AsksFirst {
			return true
		}
	}

	return false
}

// settleOpen closes the open matches that moved on, takes the new settings into the rest,
// and advances those that may go on.
func (p *pass) settleOpen(ctx context.Context, open []*match) {
	for _, m := range open {
		if p.e.inflight[m.ID] {
			continue
		}

		r, ok := p.byID[m.RoutineID]

		if !ok {
			p.routineGone(ctx, m)

			continue
		}

		if !p.settle(ctx, r, m) && goesOn(m) {
			p.advance(ctx, r, m)
		}
	}
}

// judgeVerifying judges m, verifying, once its item is still there: a verdict moves it on
// to its next step, which r, its routine, runs, nil for one that no longer runs, whose
// match the next cycle closes.
func (p *pass) judgeVerifying(ctx context.Context, r *routineRun, m *match) {
	if m.state() == StateVerifying && p.verifyMatch(ctx, r, m) && r != nil {
		p.advance(ctx, r, m)
	}
}

// goesOn reports an open match the engine moves on by itself: one queued or blocked, and
// one waiting for its routine's OK, which a change of mode may give.
func goesOn(m *match) bool {
	return m.state() == StateQueued || m.state() == StateBlocked ||
		m.state() == StateWaiting && Blocker(m.BlockedBy) == BlockAsk
}

// routineGone closes the waiting, blocked, and queued matches of a routine switched off
// or deleted; another account's, or a broken one's, are left as they are.
func (p *pass) routineGone(ctx context.Context, m *match) {
	if m.state().active() {
		p.watchActive(ctx, m, nil)
		p.judgeVerifying(ctx, nil, m)

		return
	}

	if row, ok := p.others[m.RoutineID]; ok && (row.Enabled || otherAccount(row.AccountLogin, p.inbox.Viewer)) {
		return
	}

	m.close(StateClosed, ReasonRoutineOff, "The routine was switched off.", p.now)
	p.save(ctx, m)
}

// settle closes m when it moved on, and reports whether it did, or is under way; the rest
// take the routine's new settings. A match whose run is under way, or that started one,
// as a manual one, is closed only by its item being done with: its fire key moves by
// design, as the agent pushes.
func (p *pass) settle(ctx context.Context, r *routineRun, m *match) bool {
	switch {
	case m.state().active():
		p.watchActive(ctx, m, r.inWatch)
		p.judgeVerifying(ctx, r, m)

		return true
	case p.closeDone(ctx, r, m):
		return true
	case m.Manual || m.Attempt > 0:
		return false
	}

	return p.closeMoved(ctx, r, m)
}

// closeDone closes m when its item is done with, or its routine runs in Dry run now, and
// reports whether it did.
func (p *pass) closeDone(ctx context.Context, r *routineRun, m *match) bool {
	if reason, words, closed := p.itemDone(m); closed {
		m.close(StateClosed, reason, words, p.now)
		p.save(ctx, m)

		return true
	}

	if m.DryRun || r.mode == autopilotprefs.ModeDryRun {
		m.close(StateClosed, ReasonRoutineChanged, "The routine is in Dry run now.", p.now)
		p.save(ctx, m)

		return true
	}

	return false
}

// closeMoved closes m when the routine leaves its item alone, its item left the routine's
// watch, its fire key moved, or the routine no longer matches it, and reports whether it
// did; otherwise m takes the routine's new settings.
func (p *pass) closeMoved(ctx context.Context, r *routineRun, m *match) bool {
	id := store.Deref(m.ItemID)

	if r.handsOff[id] {
		m.close(StateClosed, ReasonHandsOff, leftAloneWords, p.now)
		p.save(ctx, m)

		return true
	}

	if !r.inWatch[id] {
		return p.leftWatch(ctx, m, r, id)
	}

	d, ok := r.decided[id]
	if !ok {
		return false
	}

	switch {
	case d.Match == eval.Yes && d.FireKey != m.FireKey:
		m.close(StateClosed, ReasonMovedOn, "It moved on: "+firstActual(d), p.now)
	case d.Match == eval.No && m.SpecHash != r.hash:
		m.close(StateClosed, ReasonRoutineChanged, "The routine changed, and no longer matches it.", p.now)
	case d.Match == eval.No:
		m.close(StateClosed, ReasonNoLongerMatches, "It no longer matches: "+firstNo(d), p.now)
	default:
		if takeSteps(m, r.spec, r.hash) {
			p.save(ctx, m)
		}

		return false
	}

	p.save(ctx, m)

	return true
}

// firstActual returns the trigger's actual value.
func firstActual(d eval.Decision) string {
	if len(d.Trace) == 0 {
		return ""
	}

	return d.Trace[0].Actual
}

// firstNo returns the first step of d that said no.
func firstNo(d eval.Decision) string {
	for _, t := range d.Trace {
		if t.Result == eval.No {
			return t.Actual
		}
	}

	return ""
}

// itemDone says why m's item is done with: merged, closed, or gone.
func (p *pass) itemDone(m *match) (Reason, string, bool) {
	if m.ItemID == nil {
		return ReasonGone, "The item is gone.", true
	}

	st, ok := p.states[*m.ItemID]

	switch {
	case !ok:
		return ReasonGone, "The item is gone.", true
	case st.State == "MERGED":
		return ReasonMerged, "It was merged.", true
	case st.State == "CLOSED":
		return ReasonItemClosed, "It was closed.", true
	}

	return "", "", false
}

// leftWatch closes m, whose item left every watch of its routine, once its state was read
// after it left: an item that merged or closed closes as such. While one of the routine's
// searches answered only in part, the item may only have dropped past what it returned, so
// its state is still read, every stateReadFor, but it isn't closed for leaving.
func (p *pass) leftWatch(ctx context.Context, m *match, r *routineRun, id int64) bool {
	at, read := p.e.stateReads[id]
	if !read || p.now.Sub(at) > stateReadFor {
		p.stateReads = append(p.stateReads, id)

		return true
	}

	if !p.searchesComplete(r) {
		return true
	}

	m.close(StateClosed, ReasonLeftWatch, "It left the routine's watch.", p.now)
	p.save(ctx, m)

	return true
}

// searchesComplete reports whether each of r's searches last answered in full: an item
// that isn't among the results of a search that failed, was truncated, or was read in part
// may only have dropped past what it returned, so it is stale, not gone.
func (p *pass) searchesComplete(r *routineRun) bool {
	for _, key := range r.keys {
		if !p.inbox.Searches[key].complete() {
			return false
		}
	}

	return true
}

// save writes m, logging a change that failed: the next cycle reads it again.
func (p *pass) save(ctx context.Context, m *match) {
	if !m.state().Open() && State(m.was).Open() {
		p.cycle.Closed++
	}

	p.e.save(ctx, m)
}

// save writes m when it changed, logging a change that failed: the next cycle reads it
// again.
func (e *Engine) save(ctx context.Context, m *match) {
	if err := e.saveIn(ctx, e.ledger.q, m); err != nil {
		e.logger.WarnContext(ctx, "Autopilot could not save a match", "match", m.ID, "error", err)
	}
}

// saveIn writes m through q when it changed, stamping when it closed.
func (e *Engine) saveIn(ctx context.Context, q *queries.Queries, m *match) error {
	if !m.dirty() {
		return nil
	}

	if !m.state().Open() && m.ClosedAt == nil {
		m.ClosedAt = new(e.clock.Now())
	}

	e.noteWait(m)

	return e.ledger.update(ctx, q, m)
}

// askReads asks, on workers, for the check names of the pull requests whose decision
// waits on them, the states of the items that left a watch, and the items a verifying
// match reads again.
func (p *pass) askReads(ctx context.Context) {
	p.askChecks(ctx)
	p.askStates(ctx)
	p.askItems(ctx)
}

// askItems reads again, on a worker, one at a time, the items verifying matches wait on;
// the cycle each read wakes judges them again.
func (p *pass) askItems(ctx context.Context) {
	ids := union(p.itemReads, nil)
	if len(ids) == 0 {
		return
	}

	p.cycle.Reads += len(ids)

	p.e.goWork(ctx, func(ctx context.Context) {
		for _, id := range ids {
			if err := p.e.items.ReadItem(ctx, id); err != nil && ctx.Err() == nil {
				p.e.logger.InfoContext(ctx, "Autopilot could not read an item it verifies", "item", id, "error", err)
			}
		}

		p.e.Wake()
	})
}

// askChecks asks for the check names of the pull requests whose decision waits on them,
// each at most every askAgainAfter, in one read; the cycle after it decides them again.
func (p *pass) askChecks(ctx context.Context) {
	now := time.Now()

	var checks []int64

	for _, id := range union(p.checks, nil) {
		if at, ok := p.e.checksRead[id]; !ok || now.Sub(at) >= askAgainAfter {
			p.e.checksRead[id] = now
			checks = append(checks, id)
		}
	}

	if len(checks) == 0 {
		return
	}

	p.cycle.Reads++

	p.e.goWork(ctx, func(ctx context.Context) {
		// While the GraphQL budget is low the inbox sends nothing, and the names are read
		// again once they are due.
		if _, err := p.e.items.ReadChecks(ctx, checks); err != nil && ctx.Err() == nil && !errors.Is(err, ErrLowBudget) {
			p.e.logger.InfoContext(ctx, "Autopilot could not read check names", "error", err)
		}

		p.e.Wake()
	})
}

// askStates asks for the states of the items that left a watch, in one read; the cycle
// after it closes their matches.
func (p *pass) askStates(ctx context.Context) {
	var states []int64

	for _, id := range union(p.stateReads, nil) {
		if !p.e.statesAsked[id] {
			p.e.statesAsked[id] = true
			states = append(states, id)
		}
	}

	if len(states) == 0 {
		return
	}

	p.cycle.Reads++

	p.e.goWork(ctx, func(ctx context.Context) {
		err := p.e.items.ReadStates(ctx, states)
		if err != nil && ctx.Err() == nil {
			p.e.logger.InfoContext(ctx, "Autopilot could not read the items' states", "error", err)
		}

		p.e.mail.post(func(context.Context) { p.e.statesRead(states, err == nil) })
	})
}

// statesRead records that the read of the states of the items among ids ended, and when
// it read them, when it did.
func (e *Engine) statesRead(ids []int64, read bool) {
	at := e.clock.Now()

	for _, id := range ids {
		delete(e.statesAsked, id)

		if read {
			e.stateReads[id] = at
		}
	}
}

// forgetFreshReads forgets the reads right before a start that no start can use any
// more, its match closed or the read too old.
func (e *Engine) forgetFreshReads(now time.Time) {
	for id, got := range e.freshReads {
		if now.Sub(got.at) > freshFor {
			delete(e.freshReads, id)
		}
	}
}
