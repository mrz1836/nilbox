package autopilot

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/eval"
	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/store"
)

// The ledger records what each routine did with each item it matched, in
// autopilot_matches: one row per routine, item, and occurrence (the evaluator's fire key),
// apart for a Dry run's record. The row is written before anything is done for it, and
// an existing row blocks a repeat, which is what makes a routine act once per occurrence
// across refreshes, check re-runs, and restarts. A row is open or closed: only the
// routine's own outcomes close one (it ran, the user skipped it, it ran out of tries, or
// the item moved on), and everything that only delays a start keeps it open with what it
// waits on (BlockedBy), so no limit, pause, or missing tool ever loses a match. Every
// change is a compare-and-set on the state the row was read in.

// State is where a match is.
type State string

// The open states.
const (
	// StateWaiting needs a decision only the user can make: an OK, a hold, a warning.
	StateWaiting State = "waiting"

	// StateBlocked can't start until something on this Mac is fixed.
	StateBlocked State = "blocked"

	// StateQueued starts by itself once what it waits on frees.
	StateQueued State = "queued"

	// StateRunning has an agent working, or a GitHub action being sent.
	StateRunning State = "running"

	// StateVerifying has a run that pushed, whose commit's checks haven't finished.
	StateVerifying State = "verifying"
)

// The closed states.
const (
	// StateDone ran, or was sent; for a Dry run, it would have.
	StateDone State = "done"

	// StateSkipped was closed by the routine's own limit: its tries, or the outside
	// contributor setting.
	StateSkipped State = "skipped"

	// StateDismissed was skipped by the user.
	StateDismissed State = "dismissed"

	// StateClosed was closed because the item or the routine moved on; Reason says how.
	StateClosed State = "closed"

	// StateBaseline already matched when the user chose "only what changes from now on".
	StateBaseline State = "baseline"
)

// States returns every state, the open ones first.
func States() []State {
	return []State{
		StateWaiting, StateBlocked, StateQueued, StateRunning, StateVerifying,
		StateDone, StateSkipped, StateDismissed, StateClosed, StateBaseline,
	}
}

// Open reports an open state: one the engine still acts on.
func (s State) Open() bool {
	switch s {
	case StateWaiting, StateBlocked, StateQueued, StateRunning, StateVerifying:
		return true
	case StateDone, StateSkipped, StateDismissed, StateClosed, StateBaseline:
		return false
	}

	return false
}

// active reports a state whose run is under way: running or verifying, which only the
// run's end, the verdict, or the item closing moves.
func (s State) active() bool {
	return s == StateRunning || s == StateVerifying
}

// Blocker is what an open match waits on.
type Blocker string

// What a waiting match waits for the user about.
const (
	BlockAsk         Blocker = "ask"
	BlockHold        Blocker = "hold"
	BlockWarning     Blocker = "warning"
	BlockNoClone     Blocker = "no_clone"
	BlockDirty       Blocker = "dirty_worktree"
	BlockCatchUp     Blocker = "catch_up"
	BlockInterrupted Blocker = "interrupted"
	BlockOutside     Blocker = "outside_contributor"
	BlockBacklog     Blocker = "backlog"

	// BlockScriptAsks is a script set to ask before each run, which runs on the user's OK
	// alone, every time.
	BlockScriptAsks Blocker = "script_asks"

	// BlockReviewRequired is a Merge it GitHub holds for an approving review, which only a
	// person gives: the user approves it, then lets the merge go.
	BlockReviewRequired Blocker = "review_required"
)

// What a blocked match waits on this Mac for.
const (
	BlockNotReady    Blocker = "not_ready"
	BlockMissingTool Blocker = "missing_tool"
	BlockCrossRepo   Blocker = "cross_repository"
)

// What a queued match waits on before it starts by itself.
const (
	BlockSlot         Blocker = "slot"
	BlockCapRuns      Blocker = "cap_runs"
	BlockCapActions   Blocker = "cap_actions"
	BlockCapSpend     Blocker = "cap_spend"
	BlockActiveHours  Blocker = "active_hours"
	BlockPaused       Blocker = "paused"
	BlockBusyWorktree Blocker = "busy_worktree"
	BlockGitHubBudget Blocker = "github_budget"
	BlockWritesHeld   Blocker = "writes_held"
	BlockOutage       Blocker = "provider_outage"
	BlockSettling     Blocker = "settling"
	BlockWait         Blocker = "wait"

	// BlockOutOfUsage is every profile an agent step may run with resting until its
	// usage limit resets: it starts by itself once one's rest ends.
	BlockOutOfUsage Blocker = "out_of_usage"

	// BlockSignedOut is no one signed in to GitHub: nothing starts, local steps included.
	BlockSignedOut Blocker = "signed_out"

	// BlockStarting is a start under way: the item read fresh, then the start itself.
	BlockStarting Blocker = "starting"
)

// stateOf returns the open state a blocker keeps a match in.
func stateOf(b Blocker) State {
	switch {
	case slices.Contains([]Blocker{
		BlockAsk, BlockHold, BlockWarning, BlockNoClone, BlockDirty, BlockCatchUp, BlockInterrupted, BlockOutside,
		BlockBacklog, BlockScriptAsks, BlockReviewRequired,
	}, b):
		return StateWaiting
	case slices.Contains([]Blocker{BlockNotReady, BlockMissingTool, BlockCrossRepo}, b):
		return StateBlocked
	default:
		return StateQueued
	}
}

// Reason is why a match closed.
type Reason string

// The reasons a match closes.
const (
	ReasonRan             Reason = "ran"
	ReasonWouldRun        Reason = "would_run"
	ReasonTries           Reason = "tries"
	ReasonOutside         Reason = "outside_contributor"
	ReasonYouSkipped      Reason = "you_skipped"
	ReasonYouStopped      Reason = "you_stopped"
	ReasonMerged          Reason = "merged"
	ReasonItemClosed      Reason = "item_closed"
	ReasonGone            Reason = "gone"
	ReasonLeftWatch       Reason = "left_watch"
	ReasonMovedOn         Reason = "moved_on"
	ReasonNoLongerMatches Reason = "no_longer_matches"
	ReasonRoutineChanged  Reason = "routine_changed"
	ReasonRoutineOff      Reason = "routine_off"
	ReasonNotOffered      Reason = "not_offered"
	ReasonFailed          Reason = "failed"
	ReasonFromNow         Reason = "from_now"

	// ReasonHandsOff closes a match that hadn't started on an item its routine leaves
	// alone, since the user took back what it did there.
	ReasonHandsOff Reason = "hands_off"

	// ReasonTakenOver closes a match, done, whose agent's run the user took over: its
	// conversation carries on in the user's terminal, where the session that resumed it is
	// followed as any is.
	ReasonTakenOver Reason = "taken_over"
)

// reopens reports a reason that closes a match before it ran, which reopens on its own
// when its occurrence matches again, if it truly never ran (reopensRow): a check that
// failed, passed when run again, and failed again on the same commit, for one; or a match
// a routine let act on its item again goes on with.
func (r Reason) reopens() bool {
	return slices.Contains([]Reason{
		ReasonNoLongerMatches, ReasonLeftWatch, ReasonMovedOn, ReasonRoutineChanged, ReasonRoutineOff, ReasonHandsOff,
	}, r)
}

// reopensRow reports a closed match that reopens on its own when its occurrence matches
// again: one closed for a reason that reopens, none of whose steps ever started. A match
// that started a step, a local one included, is a record of the occurrence, which only Try
// again opens again; so a routine switched off and on never runs on it twice.
func reopensRow(row keyRow) bool {
	return row.State == StateClosed && row.Reason.reopens() && !row.Started
}

// startedAny reports a match that started a step: a run, a step it moved past, or a step
// its stored steps record as running, done, or failed.
func startedAny(attempt, step int64, steps string) bool {
	if attempt > 0 || step > 0 {
		return true
	}

	for _, s := range []string{stepRunning, stepDone, stepFailed} {
		if strings.Contains(steps, `"status":"`+s+`"`) {
			return true
		}
	}

	return false
}

// Step statuses, as a match's steps record each one.
const (
	stepPending = ""
	stepRunning = "running"
	stepDone    = "done"
	stepSkipped = "skipped"
	stepFailed  = "failed"
)

// stepRun is one of the steps a match started with, and how it went. A GitHub step sends
// its actions one run at a time: Action counts those that went, so the next run sends the
// next, and Accepted holds the warnings the user accepted for the one it is on.
type stepRun struct {
	Step     spec.Step  `json:"step"`
	Status   string     `json:"status,omitempty"`
	Outcome  string     `json:"outcome,omitempty"`
	Note     string     `json:"note,omitempty"`
	RunID    int64      `json:"runId,omitempty"`
	Started  *time.Time `json:"started,omitempty"`
	Ended    *time.Time `json:"ended,omitempty"`
	Action   int        `json:"action,omitempty"`
	Accepted string     `json:"accepted,omitempty"`

	// Hold is what held its start back for the user to confirm, while it waits on one,
	// and Confirmed the Keys of the holds the user confirmed for it, which its next start
	// goes ahead past.
	Hold      *Hold    `json:"hold,omitempty"`
	Confirmed []string `json:"confirmed,omitempty"`

	// Verdict is what a Wait for the checks found of the agent's run, once it did, Seen
	// when it first saw the run's commit as the pull request's head, and Commit the commit
	// its verdict is on, which a Merge it after it is pinned to.
	Verdict Verdict    `json:"verdict,omitempty"`
	Seen    *time.Time `json:"seen,omitempty"`
	Commit  string     `json:"commit,omitempty"`

	// Review is the review a read-only agent step's report held, which a Post the agent's
	// review after it posts, and PostAs the user's choice of how a Post the agent's review
	// waiting for their OK posts it (PostAsComment), "" for the step's own.
	Review *AgentReview `json:"review,omitempty"`
	PostAs string       `json:"postAs,omitempty"`

	// OK records the user's OK for this step itself: a step that follows a run (Merge it,
	// Post the agent's review) asks for its own in Ask first, showing what the run came to.
	OK bool `json:"ok,omitempty"`
}

// match is a ledger row, with its steps and trace read, and the row as stored, so a
// cycle writes only what moved.
type match struct {
	queries.AutopilotMatch

	steps []stepRun
	trace []eval.Trace
	was   string

	orig      queries.AutopilotMatch
	origSteps string
}

// dirty reports whether m differs from the row as stored.
func (m *match) dirty() bool {
	steps, _, err := encodeMatch(m)

	return err != nil || steps != m.origSteps || m.State != m.orig.State || m.BlockedBy != m.orig.BlockedBy ||
		m.Reason != m.orig.Reason || m.Detail != m.orig.Detail || m.Step != m.orig.Step ||
		m.SpecHash != m.orig.SpecHash || m.Approved != m.orig.Approved || m.Attempt != m.orig.Attempt
}

// stored records m as written.
func (m *match) stored() {
	m.was, m.orig = m.State, m.AutopilotMatch
	m.origSteps, _, _ = encodeMatch(m)
}

// state returns the row's state.
func (m *match) state() State { return State(m.State) }

// set moves the row to an open state with what it waits on, or to running.
func (m *match) set(s State, b Blocker, detail string) {
	m.State, m.BlockedBy, m.Detail = string(s), string(b), detail
}

// wait keeps the row open, waiting on b.
func (m *match) wait(b Blocker, detail string) {
	m.set(stateOf(b), b, detail)
}

// unstarted takes back the run its start counted, for a start that never reached its
// runner: it counts as no run, so the match is decided again like any queued one.
func (m *match) unstarted() {
	if m.Attempt > 0 {
		m.Attempt--
	}
}

// close closes the row.
func (m *match) close(s State, r Reason, detail string, now time.Time) {
	m.State, m.BlockedBy, m.Reason, m.Detail = string(s), "", string(r), detail
	m.ClosedAt = new(now)
}

// key names one occurrence of one routine on one item.
type matchKey struct {
	item   int64
	fire   string
	dryRun bool
}

// ledger reads and writes the matches, the runs, and what the routines keep of each item.
// The engine is its only writer, whose ledger keeps their keys in memory (index, see
// ledgerindex.go), which every write of a match or a baseline notes; nil keeps none.
type ledger struct {
	db    *sql.DB
	q     *queries.Queries
	clock clock.Clock
	index *ledgerIndex
}

// newLedger returns the ledger over db.
func newLedger(db *sql.DB, clk clock.Clock) *ledger {
	return &ledger{db: db, q: queries.New(db), clock: clock.Or(clk)}
}

// read converts a stored row.
func readMatch(row queries.AutopilotMatch) (*match, error) {
	m := &match{AutopilotMatch: row, was: row.State, orig: row, origSteps: row.Steps}

	steps, err := store.DecodeJSON[[]stepRun](row.Steps)
	if err != nil {
		return nil, fmt.Errorf("read the steps of match %d: %w", row.ID, err)
	}

	trace, err := store.DecodeJSON[[]eval.Trace](row.Trace)
	if err != nil {
		return nil, fmt.Errorf("read the trace of match %d: %w", row.ID, err)
	}

	m.steps, m.trace = nonNil(steps), nonNil(trace)

	return m, nil
}

// readMatches converts stored rows.
func readMatches(rows []queries.AutopilotMatch) ([]*match, error) {
	return store.ConvertRows(rows, readMatch)
}

// open returns every open match.
func (l *ledger) open(ctx context.Context) ([]*match, error) {
	rows, err := l.q.ListOpenMatches(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the open matches: %w", err)
	}

	return readMatches(rows)
}

// get returns one match.
func (l *ledger) get(ctx context.Context, id int64) (*match, error) {
	return l.getIn(ctx, l.q, id)
}

// getIn returns one match, read through q.
func (l *ledger) getIn(ctx context.Context, q *queries.Queries, id int64) (*match, error) {
	row, err := q.GetMatch(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoMatch
	}

	if err != nil {
		return nil, fmt.Errorf("read match %d: %w", id, err)
	}

	return readMatch(row)
}

// keys returns each of a routine's matches by the occurrence it is for, read from the
// database, as a test of a routine reads them off the engine's loop.
func (l *ledger) keys(ctx context.Context, routineID int64) (map[matchKey]keyRow, error) {
	rows, err := l.q.ListRoutineMatchKeys(ctx, routineID)
	if err != nil {
		return nil, fmt.Errorf("read the matches of routine %d: %w", routineID, err)
	}

	out := make(map[matchKey]keyRow, len(rows))
	for _, r := range rows {
		out[matchKey{item: store.Deref(r.ItemID), fire: r.FireKey, dryRun: r.DryRun}] = keyRowOf(r.ID, r.State, r.Reason,
			r.Attempt, r.Step, r.Steps)
	}

	return out, nil
}

// insert records m, and reports false when a row for its occurrence is there already.
func (l *ledger) insert(ctx context.Context, m *match) (bool, error) {
	steps, trace, err := encodeMatch(m)
	if err != nil {
		return false, err
	}

	id, err := l.q.InsertMatch(ctx, queries.InsertMatchParams{
		RoutineID: m.RoutineID, ItemID: m.ItemID, AccountLogin: m.AccountLogin, RepoOwner: m.RepoOwner,
		RepoName: m.RepoName, Kind: m.Kind, Number: m.Number, Title: m.Title, URL: m.URL, FireKey: m.FireKey,
		DryRun: m.DryRun, Manual: m.Manual, State: m.State, BlockedBy: m.BlockedBy, Reason: m.Reason,
		Detail: m.Detail, Trace: trace, Steps: steps, Step: m.Step, SpecHash: m.SpecHash, HeadOID: m.HeadOID,
		AutoMergeAtMatch: m.AutoMergeAtMatch, Approved: m.Approved, Attempt: m.Attempt, CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt, DecidedAt: m.DecidedAt, ClosedAt: m.ClosedAt,
	})

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("record a match: %w", err)
	}

	m.ID = id
	m.stored()
	l.index.wroteMatch(l.q, l.q, m, steps)

	return true, nil
}

// encodeMatch encodes a match's steps and trace.
func encodeMatch(m *match) (string, string, error) {
	steps, err := store.EncodeJSON(nonNil(m.steps))
	if err != nil {
		return "", "", err
	}

	trace, err := store.EncodeJSON(nonNil(m.trace))
	if err != nil {
		return "", "", err
	}

	return steps, trace, nil
}

// errLostRace is a change to a match that another change got to first.
var errLostRace = errors.New("the match changed meanwhile")

// update writes m's next state, only while it is in the state it was read in, on q.
func (l *ledger) update(ctx context.Context, q *queries.Queries, m *match) error {
	steps, _, err := encodeMatch(m)
	if err != nil {
		return err
	}

	m.UpdatedAt = l.clock.Now()

	n, err := q.UpdateMatch(ctx, queries.UpdateMatchParams{
		State: m.State, BlockedBy: m.BlockedBy, Reason: m.Reason, Detail: m.Detail, Steps: steps, Step: m.Step,
		SpecHash: m.SpecHash, Approved: m.Approved, Attempt: m.Attempt, UpdatedAt: m.UpdatedAt, DecidedAt: m.DecidedAt,
		ClosedAt: m.ClosedAt, ID: m.ID, Was: m.was,
	})

	switch {
	case err != nil:
		return fmt.Errorf("save match %d: %w", m.ID, err)
	case n == 0:
		return fmt.Errorf("%w: match %d", errLostRace, m.ID)
	}

	m.stored()
	l.index.wroteMatch(q, l.q, m, steps)

	return nil
}

// inTx runs fn in one transaction over the ledger's queries; what it writes of the
// matches and the baselines reaches the keys in memory once it commits.
func (l *ledger) inTx(ctx context.Context, fn func(q *queries.Queries) error) error {
	var txq *queries.Queries

	committed := false

	defer func() { l.index.end(txq, committed) }()

	err := store.InTx(ctx, l.db, func(tx *sql.Tx) error {
		txq = l.q.WithTx(tx)
		l.index.begin(txq)

		return fn(txq)
	})
	committed = err == nil

	return err
}

// specHash names a routine's spec as stored, so a match can tell the routine changed.
func specHash(data string) string {
	sum := sha256.Sum256([]byte(data))

	return hex.EncodeToString(sum[:8])
}

// stepsOf returns the steps a match starts with, from a spec.
func stepsOf(sp spec.Spec) []stepRun {
	out := make([]stepRun, 0, len(sp.Steps))
	for _, st := range sp.Steps {
		out = append(out, stepRun{Step: st})
	}

	return out
}

// takeSteps gives m the steps of sp from where m is: the steps it already ran, or is
// running, stay as they went, so a routine saved with other steps never changes what a
// match has done. It reports whether anything changed.
func takeSteps(m *match, sp spec.Spec, hash string) bool {
	if m.SpecHash == hash {
		return false
	}

	next := stepsOf(sp)
	keep := min(int(m.Step), len(m.steps), len(next))

	for i := range keep {
		next[i] = m.steps[i]
	}

	if int(m.Step) < len(m.steps) && m.steps[m.Step].Status == stepRunning {
		return false
	}

	m.steps, m.SpecHash = next, hash

	return true
}

// handsOff returns the items a routine leaves alone.
func (l *ledger) handsOff(ctx context.Context, routineID int64) (map[int64]bool, error) {
	rows, err := l.q.ListHandsOff(ctx, routineID)
	if err != nil {
		return nil, fmt.Errorf("read the items routine %d leaves alone: %w", routineID, err)
	}

	out := make(map[int64]bool, len(rows))
	for _, r := range rows {
		out[r.ItemID] = true
	}

	return out, nil
}

// tries returns when Try again last reset each of a routine's counts, by item.
func (l *ledger) tries(ctx context.Context, routineID int64) (map[int64]time.Time, error) {
	rows, err := l.q.ListTries(ctx, routineID)
	if err != nil {
		return nil, fmt.Errorf("read the tries of routine %d: %w", routineID, err)
	}

	out := make(map[int64]time.Time, len(rows))
	for _, r := range rows {
		out[r.ItemID] = r.ResetAt
	}

	return out, nil
}

// baseline is what a routine saw of an item when it first saw it, and since.
type baseline struct {
	// Head is the head commit it first saw.
	Head string `json:"head,omitempty"`

	// Comments is the comment count it first saw, or last fired at.
	Comments *int `json:"comments,omitempty"`
}

// baselines returns what a routine keeps of each item, by item.
func (l *ledger) baselines(ctx context.Context, routineID int64) (map[int64]baseline, error) {
	rows, err := l.q.ListBaselines(ctx, routineID)
	if err != nil {
		return nil, fmt.Errorf("read what routine %d saw first: %w", routineID, err)
	}

	out := make(map[int64]baseline, len(rows))

	for _, r := range rows {
		b, decodeErr := store.DecodeJSON[baseline](r.State)
		if decodeErr != nil {
			continue
		}

		out[r.ItemID] = b
	}

	return out, nil
}

// saveBaseline records b for a routine and an item, first or again.
func (l *ledger) saveBaseline(ctx context.Context, routineID, itemID int64, b baseline, first bool) error {
	raw, err := store.EncodeJSON(b)
	if err != nil {
		return err
	}

	now := l.clock.Now()

	var n int64

	if first {
		n, err = l.q.InsertBaseline(ctx, queries.InsertBaselineParams{
			RoutineID: routineID, ItemID: itemID, State: raw, FirstSeenAt: now,
		})
	} else {
		n, err = l.q.UpdateBaseline(ctx, queries.UpdateBaselineParams{
			State: raw, UpdatedAt: now, RoutineID: routineID, ItemID: itemID,
		})
	}

	if err != nil {
		return fmt.Errorf("record what routine %d saw of item %d: %w", routineID, itemID, err)
	}

	// The keys note only what was written: a first sight recorded already, or a row gone
	// since, writes nothing, which the counts of the rows then tell.
	if n > 0 {
		l.index.wroteBaseline(l.q, l.q, routineID, itemID, b, first)
	}

	return nil
}

// itemRun is one run on an item, for the routines' history of it.
type itemRun = queries.ListItemRunsRow

// itemRuns returns the runs on the items among ids, by item, oldest first.
func (l *ledger) itemRuns(ctx context.Context, ids []int64) (map[int64][]itemRun, error) {
	out := map[int64][]itemRun{}
	if len(ids) == 0 {
		return out, nil
	}

	refs := make([]*int64, 0, len(ids))
	for _, id := range ids {
		refs = append(refs, new(id))
	}

	rows, err := l.q.ListItemRuns(ctx, refs)
	if err != nil {
		return nil, fmt.Errorf("read the runs on the items: %w", err)
	}

	for _, r := range rows {
		id := store.Deref(r.ItemID)
		out[id] = append(out[id], r)
	}

	return out, nil
}

// history works out what the ledger knows of a routine and an item, for the evaluator:
// the routine's tries since the last Try again, its last outcome, the head it first saw,
// the comments it last counted, the commits every routine's runs pushed to the item, and
// whether an agent of any routine's works on it now.
// A try is one match's runs, however many it started: an agent's and the steps after it,
// or a GitHub step's actions, each a run of its own.
func history(routineID int64, runs []itemRun, b baseline, seen bool, resetAt time.Time, last string) eval.History {
	h := eval.History{OwnHeads: []string{}, LastOutcome: last}

	if seen {
		h.FirstHead, h.CommentsSeen = b.Head, b.Comments
	}

	tried := map[int64]bool{}

	for _, r := range runs {
		h.OwnHeads = addHeads(h.OwnHeads, r.RunCommit, r.HeadAfter)
		h.Working = h.Working || (isAgentRunner(r.Runner) && activeStatus(r.Status))

		if r.RoutineID == routineID && countsTry(r) && r.QueuedAt.After(resetAt) && !tried[r.MatchID] {
			tried[r.MatchID] = true
			h.Tries++
		}
	}

	return h
}

// addHeads adds each of heads to list once, but "".
func addHeads(list []string, heads ...string) []string {
	for _, head := range heads {
		if head != "" && !slices.Contains(list, head) {
			list = append(list, head)
		}
	}

	return list
}

// activeStatus reports a run's status while it goes: starting, running, or stopping.
func activeStatus(status string) bool {
	return status == runStarting || status == runRunning || status == runStopping
}

// countsTry reports a run that counts as one of its routine's tries on the item: not a
// manual run, and not one that was interrupted, stopped because the item closed, or that
// sent nothing.
func countsTry(r itemRun) bool {
	return !r.Manual && r.Counted
}

// ErrNoMatch is returned for a match that is gone.
var ErrNoMatch = errors.New("no such match")
