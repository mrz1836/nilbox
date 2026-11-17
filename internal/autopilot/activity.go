package autopilot

import (
	"context"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/eval"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// What the window reads of the ledger: Activity, a page of matches by day; each item's
// latest match, for the chip on its row, and an item's history, which the chip opens;
// Explain, why a routine did or didn't act on an item; and Engine,
// what the engine last did and whether this copy runs it. Each reads the database alone,
// and no list is nil.

const (
	// defaultActivity is how many matches a page of Activity holds when the window asks
	// for none, and maxActivity the most it may ask for.
	defaultActivity = 100
	maxActivity     = 500

	// maxItemHistory caps an item's history.
	maxItemHistory = 200

	// maxLatest caps the items one LatestMatches call reads, as many as the inbox's
	// ItemsByID reads.
	maxLatest = 500
)

// Match is one of a routine's matches, as Activity and an item's history show it.
type Match struct {
	ID int64 `json:"id"`

	// RoutineID and Routine name the routine, as it was named last.
	RoutineID int64  `json:"routineId"`
	Routine   string `json:"routine"`

	// Item is the item, as the ledger keeps a copy of it; its ID is 0 once it is gone.
	Item ItemRef `json:"item"`

	// FireKey is the occurrence it is for; DryRun reports a Dry run's record, and Manual
	// a run the user started by hand.
	FireKey string `json:"fireKey"`
	DryRun  bool   `json:"dryRun"`
	Manual  bool   `json:"manual"`

	// State is where it is: open (waiting, blocked, queued, running, verifying) or
	// closed. BlockedBy is what an open one waits on, Reason why a closed one closed, and
	// Words says either in the window's words.
	State     State   `json:"state"`
	BlockedBy Blocker `json:"blockedBy"`
	Reason    Reason  `json:"reason"`
	Words     string  `json:"words"`

	// Steps are the steps it started with and how each went, and Step where it is.
	Steps []MatchStep `json:"steps"`
	Step  int         `json:"step"`

	// Trace is the decision as it was when it matched.
	Trace []eval.Trace `json:"trace"`

	// Approved reports the user's OK, and Attempt how many runs it started.
	Approved bool `json:"approved"`
	Attempt  int  `json:"attempt"`

	// Hold is what holds its start back for the user's OK, while it waits on a hold
	// (BlockHold, BlockDirty): the launcher's own hold, which the card shows as the hold
	// dialog does, and whose Run confirms exactly it. Nil otherwise.
	Hold *Hold `json:"hold"`

	// Runs are the runs it started, oldest first.
	Runs []Run `json:"runs"`

	// Effects are what its local steps did to its item's triage, oldest first, each with
	// whether Undo can still take it back.
	Effects []Effect `json:"effects"`

	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
	DecidedAt time.Time  `json:"decidedAt"`
	ClosedAt  *time.Time `json:"closedAt"`
}

// MatchStep is one of a match's steps, and how it went.
type MatchStep struct {
	Kind      catalog.StepKind `json:"kind"`
	Label     string           `json:"label"`
	Status    string           `json:"status"`
	Outcome   string           `json:"outcome"`
	Note      string           `json:"note"`
	RunID     int64            `json:"runId"`
	StartedAt *time.Time       `json:"startedAt"`
	EndedAt   *time.Time       `json:"endedAt"`

	// Actions are a GitHub step's actions in order, and Sent how many of them went, so
	// the next to send is Actions[Sent]; none for any other step.
	Actions []catalog.GitHubActionKind `json:"actions"`
	Sent    int                        `json:"sent"`
}

// Run is one run a match started: an agent's, or a GitHub action the app sent.
type Run struct {
	ID      int64  `json:"id"`
	MatchID int64  `json:"matchId"`
	Step    int    `json:"step"`
	Runner  string `json:"runner"`
	Agent   string `json:"agent"`
	Action  string `json:"action"`
	Status  string `json:"status"`
	Outcome string `json:"outcome"`
	Verdict string `json:"verdict"`

	HeadBefore string `json:"headBefore"`
	Commit     string `json:"commit"`
	HeadAfter  string `json:"headAfter"`

	// Budget is what the start reserved, in Unit; Cost is what it reported, nil for none.
	Budget    float64  `json:"budget"`
	Unit      string   `json:"unit"`
	Cost      *float64 `json:"cost"`
	TokensIn  int64    `json:"tokensIn"`
	TokensOut int64    `json:"tokensOut"`
	CacheRead int64    `json:"cacheRead"`
	Turns     int64    `json:"turns"`

	Summary string `json:"summary"`
	Error   string `json:"error"`
	Manual  bool   `json:"manual"`

	// CostEstimated says Cost is the agent's estimate of what the API would have charged,
	// as for a run on the user's plan.
	CostEstimated bool `json:"costEstimated"`

	// ScriptRunID is the run in the run engine's history, SessionID its session, Preset
	// its tools preset, Command its command line, and LogCleared reports that its log is
	// gone, as Clear history or the retention leaves it.
	ScriptRunID int64  `json:"scriptRunId"`
	SessionID   string `json:"sessionId"`
	Preset      string `json:"preset"`
	Profile     string `json:"profile"`
	Command     string `json:"command"`
	LogCleared  bool   `json:"logCleared"`

	QueuedAt   time.Time  `json:"queuedAt"`
	StartedAt  *time.Time `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt"`
}

// ActivityPage is a page of Activity: the matches recorded before a moment, newest first,
// by local day.
type ActivityPage struct {
	Days []ActivityDay `json:"days"`

	// More reports matches before the page, which Next, the last match's ID, asks for.
	More bool  `json:"more"`
	Next int64 `json:"next"`

	// LeftAlone are the items the routines leave alone, since the user took back what they
	// did there, newest first: on the first page alone, each with Let it act again.
	LeftAlone []LeftAlone `json:"leftAlone"`
}

// ActivityDay is one local day of Activity.
type ActivityDay struct {
	// Date is the day, YYYY-MM-DD.
	Date    string  `json:"date"`
	Matches []Match `json:"matches"`

	// Groups are the day's local steps of the page's matches, a line for each routine and
	// kind of step ("Snoozed 6 · Undo"), each naming the matches it stands for.
	Groups []ActivityGroup `json:"groups"`
}

// ItemHistory is what the routines did with one item, newest first.
type ItemHistory struct {
	ItemID  int64   `json:"itemId"`
	Matches []Match `json:"matches"`

	// Open counts its open matches.
	Open int `json:"open"`
}

// EngineStatus is the engine as Settings → Autopilot shows it.
type EngineStatus struct {
	// Running reports the engine runs in this copy; Holds that this copy holds the lock,
	// and so runs the routines; Elsewhere that another copy of the app does.
	Running   bool `json:"running"`
	Holds     bool `json:"holds"`
	Elsewhere bool `json:"elsewhere"`

	// Primed reports the engine saw the refresh it waits for after a start.
	Primed bool `json:"primed"`

	// PausedSince is when Pause Autopilot began, nil while it is off.
	PausedSince *time.Time `json:"pausedSince"`

	// LastCycle is what the last cycle did, nil before one ran.
	LastCycle *Cycle `json:"lastCycle"`

	// SearchesPaused says why the last refresh sent none of the routines' searches, as the
	// inbox says it ("low_budget" while the GraphQL budget is low), "" when it sent them:
	// why the routines that use them wait.
	SearchesPaused string `json:"searchesPaused"`

	// SignedOut reports that the last refresh found no one signed in to GitHub: nothing
	// starts, local steps included, until someone is.
	SignedOut bool `json:"signedOut"`

	// Problems are the routines' problems, as the last cycle found them.
	Problems []Problem `json:"problems"`
}

// Explanation is why a routine did or didn't act on an item: what it decides now, and what
// the ledger says.
type Explanation struct {
	RoutineID int64 `json:"routineId"`
	ItemID    int64 `json:"itemId"`

	// Match, FireKey, Trace, and Needs are the decision now.
	Match   eval.Result  `json:"match"`
	FireKey string       `json:"fireKey"`
	Trace   []eval.Trace `json:"trace"`
	Needs   []facts.Need `json:"needs"`

	// InWatch reports the item is in the routine's watch.
	InWatch bool `json:"inWatch"`

	// LeftAlone reports the routine leaves the item alone, since the user took back what it
	// did there, until Let it act again.
	LeftAlone bool `json:"leftAlone"`

	// Ledger says, in sentences, what the ledger knows that holds the routine back or
	// let it act: "It already ran on this commit", "2 of 2 tries used".
	Ledger []string `json:"ledger"`

	// Record is the match for the occurrence now, nil when there is none.
	Record *Match `json:"record"`

	// Routine says, in sentences, what of the routine itself holds it back here: switched
	// off, Pause Autopilot, Dry run, another account's, not ready on this Mac.
	Routine []string `json:"routine"`

	// Watch says why the item isn't in the routine's watch, a sentence a source, when it
	// isn't: "Its author isn't dependabot."
	Watch []string `json:"watch"`

	// Records are every match the routine recorded on the item, newest first: the
	// ledger's full view of the two.
	Records []Match `json:"records"`
}

// Engine returns what the engine last did, and whether this copy runs it.
func (s *Service) Engine(context.Context) (EngineStatus, error) {
	if s.engine == nil {
		return EngineStatus{Problems: []Problem{}}, nil
	}

	s.engine.mu.Lock()
	defer s.engine.mu.Unlock()

	out := s.engine.status
	out.Problems = nonNil(slices.Clone(out.Problems))

	return out, nil
}

// Activity returns a page of the matches recorded before the match before (every one for 0),
// newest first, by local day: at most limit, 100 when 0. Each day groups the local steps of
// its matches by routine and kind of step, and the first page says which items the
// routines leave alone.
func (s *Service) Activity(ctx context.Context, before int64, limit int) (ActivityPage, error) {
	out := ActivityPage{Days: []ActivityDay{}, LeftAlone: []LeftAlone{}}

	if s.store.q == nil {
		return out, nil
	}

	first := before <= 0
	if first {
		before = math.MaxInt64
	}

	if limit <= 0 {
		limit = defaultActivity
	}

	limit = min(limit, maxActivity)

	rows, err := s.ledger().q.ListMatchesBefore(ctx, queries.ListMatchesBeforeParams{Before: before, RowLimit: int64(limit + 1)})
	if err != nil {
		return out, uierr.New("Autopilot's history couldn't be read.", err)
	}

	return s.activityPage(ctx, rows, limit, first)
}

// ActivityFrom returns Activity's matches recorded from the match from on (every one for
// 0) and before the match before (up to the newest for 0), newest first, by local day: a
// stretch of the pages the window has loaded, read again in one call, so the pages it read
// past them keep their place and nothing falls between. It holds at most limit (100 when 0,
// never more than 500); when more than that were recorded in the stretch, More reports the
// rest, which Next asks for as Activity's does, as the stretch's own before. Only a stretch
// up to the newest says which items the routines leave alone, as Activity's first page does.
func (s *Service) ActivityFrom(ctx context.Context, from, before int64, limit int) (ActivityPage, error) {
	out := ActivityPage{Days: []ActivityDay{}, LeftAlone: []LeftAlone{}}

	if s.store.q == nil {
		return out, nil
	}

	newest := before <= 0
	if newest {
		before = math.MaxInt64
	}

	if limit <= 0 {
		limit = defaultActivity
	}

	limit = min(limit, maxActivity)

	rows, err := s.ledger().q.ListMatchesFrom(ctx, queries.ListMatchesFromParams{
		FirstID: max(from, 0), BeforeID: before, RowLimit: int64(limit + 1),
	})
	if err != nil {
		return out, uierr.New("Autopilot's history couldn't be read.", err)
	}

	return s.activityPage(ctx, rows, limit, newest)
}

// activityPage makes a page of Activity of rows, newest first, read one past limit to
// tell whether there are more; withLeftAlone adds the items the routines leave alone,
// which the page that starts at the newest match says.
func (s *Service) activityPage(ctx context.Context, rows []queries.AutopilotMatch, limit int, withLeftAlone bool) (ActivityPage, error) {
	out := ActivityPage{Days: []ActivityDay{}, LeftAlone: []LeftAlone{}}

	if len(rows) > limit {
		out.More, rows = true, rows[:limit]
		out.Next = rows[len(rows)-1].ID
	}

	views, err := s.views(ctx, rows)
	if err != nil {
		return out, err
	}

	loc := s.clock.Now().Location()
	out.Days = groupLocal(byDay(views, loc), views, loc)

	if withLeftAlone {
		if out.LeftAlone, err = s.leftAlone(ctx, s.ledger()); err != nil {
			return out, err
		}
	}

	return out, nil
}

// byDay groups matches, newest first, by the local day each was recorded on.
func byDay(views []Match, loc *time.Location) []ActivityDay {
	out := []ActivityDay{}

	for _, v := range views {
		date := v.CreatedAt.In(loc).Format(time.DateOnly)

		if n := len(out); n > 0 && out[n-1].Date == date {
			out[n-1].Matches = append(out[n-1].Matches, v)
		} else {
			out = append(out, ActivityDay{Date: date, Matches: []Match{v}, Groups: []ActivityGroup{}})
		}
	}

	return out
}

// ItemHistory returns what the routines did with the item id, newest first.
func (s *Service) ItemHistory(ctx context.Context, id int64) (ItemHistory, error) {
	out := ItemHistory{ItemID: id, Matches: []Match{}}

	if s.store.q == nil {
		return out, nil
	}

	rows, err := s.ledger().q.ListItemMatches(ctx, queries.ListItemMatchesParams{ItemID: new(id), RowLimit: maxItemHistory})
	if err != nil {
		return out, uierr.New("The item's history couldn't be read.", err)
	}

	if out.Matches, err = s.views(ctx, rows); err != nil {
		return out, err
	}

	for _, m := range out.Matches {
		if m.State.Open() {
			out.Open++
		}
	}

	return out, nil
}

// LatestMatches returns, for each item among ids a routine touched, its newest match that
// did something there or waits to (open, or closed as done, skipped, or dismissed; never a
// baseline, nor one that closed as the item or the routine moved on before it acted),
// newest first: what the ⚡ chip on each of the window's rows says. At most maxLatest ids
// are read. It reads the database alone, and the list is never nil.
func (s *Service) LatestMatches(ctx context.Context, ids []int64) ([]Match, error) {
	out := []Match{}

	if s.store.q == nil || len(ids) == 0 {
		return out, nil
	}

	asked := make([]*int64, 0, min(len(ids), maxLatest))
	for _, id := range ids[:min(len(ids), maxLatest)] {
		asked = append(asked, new(id))
	}

	rows, err := s.ledger().q.ListLatestItemMatches(ctx, asked)
	if err != nil {
		return out, uierr.New("Autopilot's history of the items couldn't be read.", err)
	}

	return s.views(ctx, rows)
}

// maxNeedsYou caps the open matches NeedsYou returns.
const maxNeedsYou = 500

// NeedsYou returns the open matches that haven't started, real ones alone, newest first,
// for Activity's Needs you and Queued: those waiting for the user's decision (waiting),
// those that can't start until something on this Mac is fixed (blocked), and those that
// start by themselves (queued), each with what it waits on and, for a hold, the hold
// itself. At most maxNeedsYou. It reads the database alone, and the list is never nil.
func (s *Service) NeedsYou(ctx context.Context) ([]Match, error) {
	if s.store.q == nil {
		return []Match{}, nil
	}

	rows, err := s.ledger().q.ListOpenMatches(ctx)
	if err != nil {
		return []Match{}, uierr.New("What needs you couldn't be read.", err)
	}

	open := make([]queries.AutopilotMatch, 0, len(rows))

	for _, r := range slices.Backward(rows) {
		if r.DryRun || State(r.State).active() || len(open) >= maxNeedsYou {
			continue
		}

		open = append(open, r)
	}

	slices.SortStableFunc(open, func(a, b queries.AutopilotMatch) int { return b.UpdatedAt.Compare(a.UpdatedAt) })

	return s.views(ctx, open)
}

// ledger returns the ledger over the Service's database.
func (s *Service) ledger() *ledger {
	if s.engine != nil {
		return s.engine.ledger
	}

	return newLedger(s.store.db, s.clock)
}

// views builds the window's matches from stored rows, with their routines' names and their
// runs.
func (s *Service) views(ctx context.Context, rows []queries.AutopilotMatch) ([]Match, error) {
	out := make([]Match, 0, len(rows))
	if len(rows) == 0 {
		return out, nil
	}

	l := s.ledger()

	named, err := routineNames(ctx, l)
	if err != nil {
		return nil, err
	}

	ids := make([]int64, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}

	runs, err := l.q.ListMatchRuns(ctx, ids)
	if err != nil {
		return nil, uierr.New("The runs couldn't be read.", err)
	}

	byMatch := map[int64][]Run{}
	for _, r := range runs {
		byMatch[r.MatchID] = append(byMatch[r.MatchID], runView(r))
	}

	effects, err := s.effectsOf(ctx, l, ids)
	if err != nil {
		return nil, err
	}

	for _, row := range rows {
		m, readErr := readMatch(row)
		if readErr != nil {
			return nil, uierr.New("A match couldn't be read.", readErr)
		}

		v := matchView(m, named[row.RoutineID])
		v.Runs, v.Effects = nonNil(byMatch[row.ID]), nonNil(effects[row.ID])
		out = append(out, v)
	}

	return out, nil
}

// routineNames returns every routine's name by its ID, the deleted ones included.
func routineNames(ctx context.Context, l *ledger) (map[int64]string, error) {
	names, err := l.q.ListRoutineNames(ctx)
	if err != nil {
		return nil, uierr.New("The routines' names couldn't be read.", err)
	}

	named := make(map[int64]string, len(names))
	for _, n := range names {
		named[n.ID] = n.Name
	}

	return named, nil
}

// matchView builds the window's match from a ledger row.
func matchView(m *match, routine string) Match {
	v := Match{
		ID: m.ID, RoutineID: m.RoutineID, Routine: routine, FireKey: m.FireKey, DryRun: m.DryRun, Manual: m.Manual,
		State: m.state(), BlockedBy: Blocker(m.BlockedBy), Reason: Reason(m.Reason), Words: m.Detail,
		Steps: make([]MatchStep, 0, len(m.steps)), Step: int(m.Step), Trace: nonNil(m.trace), Approved: m.Approved,
		Attempt: int(m.Attempt), Runs: []Run{}, Effects: []Effect{}, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
		DecidedAt: m.DecidedAt, ClosedAt: m.ClosedAt,
		Item: ItemRef{
			ID: store.Deref(m.ItemID), Kind: item.Kind(m.Kind), Repo: m.RepoOwner + "/" + m.RepoName,
			Number: int(m.Number), Title: m.Title, URL: m.URL,
		},
	}

	if v.Words == "" {
		v.Words = defaultWords(v.State, v.BlockedBy)
	}

	for i := range m.steps {
		v.Steps = append(v.Steps, stepView(&m.steps[i]))
	}

	if b := Blocker(m.BlockedBy); (b == BlockHold || b == BlockDirty) && int(m.Step) < len(m.steps) {
		if h := m.steps[m.Step].Hold; h != nil {
			shown := *h
			shown.Files = nonNil(h.Files)
			v.Hold = &shown
		}
	}

	return v
}

// stepView builds the window's step from a match's stored step: a GitHub step's actions,
// with how many went (all of them once the step is done).
func stepView(sr *stepRun) MatchStep {
	kind := sr.Step.Kind()
	info, _ := catalog.StepByKind(kind)

	actions := make([]catalog.GitHubActionKind, 0, len(sr.Step.GitHub))
	for _, a := range sr.Step.GitHub {
		actions = append(actions, a.Kind())
	}

	sent := sr.Action
	if sr.Status == stepDone {
		sent = len(actions)
	}

	return MatchStep{
		Kind: kind, Label: info.Label, Status: sr.Status, Outcome: sr.Outcome, Note: sr.Note, RunID: sr.RunID,
		StartedAt: sr.Started, EndedAt: sr.Ended, Actions: actions, Sent: sent,
	}
}

// defaultWords says where a match is when nothing more was said.
func defaultWords(s State, b Blocker) string {
	switch {
	case b == BlockAsk:
		return "Waiting for your OK."
	case b == BlockPaused:
		return "Autopilot is paused."
	case s == StateQueued:
		return "Queued."
	case s == StateRunning:
		return "Running."
	case s == StateVerifying:
		return "Waiting for the checks on its commit."
	case s == StateDone:
		return "It ran."
	case s == StateDismissed:
		return "You skipped it."
	case s == StateBaseline:
		return "It already matched when you chose only what changes from now on."
	}

	return textutil.Sentence(string(s))
}

// runView builds the window's run from a ledger row.
func runView(r queries.AutopilotRun) Run {
	return Run{
		ID: r.ID, MatchID: r.MatchID, Step: int(r.Step), Runner: r.Runner, Agent: r.Agent, Action: r.GithubAction,
		Status: r.Status, Outcome: r.Outcome, Verdict: r.Verdict, HeadBefore: r.HeadBefore, Commit: r.RunCommit,
		HeadAfter: r.HeadAfter, Budget: r.BudgetReserved, Unit: r.BudgetUnit, Cost: r.Cost, TokensIn: r.TokensIn,
		TokensOut: r.TokensOut, CacheRead: r.CacheReadTokens, Turns: r.Turns, Summary: r.Summary, Error: r.Error,
		Manual: r.Manual, QueuedAt: r.QueuedAt, StartedAt: r.StartedAt, FinishedAt: r.FinishedAt,
		ScriptRunID: r.ScriptRunID, SessionID: r.SessionID, Preset: r.Preset, Profile: r.Profile,
		Command: r.DisplayCommand, LogCleared: r.LogCleared, CostEstimated: r.CostEstimated,
	}
}

// Explain says why the routine id did or didn't act on the item: what it decides now, from
// what is stored, and what the ledger knows of the two.
func (s *Service) Explain(ctx context.Context, id, itemID int64) (Explanation, error) {
	out := Explanation{
		RoutineID: id, ItemID: itemID, Trace: []eval.Trace{}, Needs: []facts.Need{}, Ledger: []string{},
		Routine: []string{}, Watch: []string{}, Records: []Match{},
	}

	r, err := s.Get(ctx, id)
	if err != nil {
		return out, err
	}

	out.Routine = s.routineLines(ctx, r)

	if r.BrokenReason != "" {
		out.Ledger = append(out.Ledger, r.Broken)

		return out, nil
	}

	if s.items == nil {
		return out, uierr.New("The inbox can't be read, so the routine can't be explained.", ErrNoItems)
	}

	keys := make([]string, 0, len(r.Spec.Watch.Sources))
	for _, q := range searchesOf(r.Spec, s.viewer(ctx)) {
		keys = append(keys, q.Key)
	}

	watched, err := s.items.WatchedItems(ctx, keys, listsOf(r.Spec))
	if err != nil {
		return out, uierr.New("The routine's items couldn't be read.", err)
	}

	out.InWatch = slices.Contains(watched, itemID)

	if !out.InWatch {
		out.Ledger = append(out.Ledger, "It isn't in this routine's watch.")
	}

	return s.explainLedger(ctx, r, out)
}

// explainLedger adds the decision now and the ledger's view to out.
func (s *Service) explainLedger(ctx context.Context, r Routine, out Explanation) (Explanation, error) {
	now := s.clock.Now()

	all, err := s.items.Facts(ctx, []int64{out.ItemID}, now.UTC())
	if err != nil {
		return out, uierr.New("The item's facts couldn't be read.", err)
	}

	if len(all) == 0 {
		out.Ledger = append(out.Ledger, "The item isn't stored on this Mac.")

		return out, nil
	}

	if !out.InWatch {
		out.Watch = watchMisses(r.Spec, all[0], s.viewer(ctx))
	}

	l := s.ledger()

	h, records, err := s.itemLedger(ctx, l, r.ID, out.ItemID)
	if err != nil {
		return out, err
	}

	d := eval.Evaluate(r.Spec, all[0], h, now)
	out.Match, out.FireKey, out.Trace, out.Needs = d.Match, d.FireKey, nonNil(d.Trace), nonNil(d.Needs)

	handsOff, err := l.handsOff(ctx, r.ID)
	if err != nil {
		return out, uierr.New("The items the routine leaves alone couldn't be read.", err)
	}

	if out.LeftAlone = handsOff[out.ItemID]; out.LeftAlone {
		out.Ledger = append(out.Ledger, leftAloneWords+" Let it act again to change that.")
	}

	if h.Tries > 0 {
		out.Ledger = append(out.Ledger, fmt.Sprintf("%d of %d tries used.", h.Tries, r.Spec.Limits.Tries()))
	}

	return withRecords(out, records, r.Name, d.FireKey), nil
}

// withRecords adds to out the ledger's full view of the routine named name on the item,
// records, newest first, and its record for the occurrence fireKey names now.
func withRecords(out Explanation, records []*match, name, fireKey string) Explanation {
	for _, rec := range records {
		view := matchView(rec, name)

		if len(out.Records) < maxExplainRecords {
			out.Records = append(out.Records, view)
		}

		if rec.FireKey == fireKey && !rec.DryRun {
			out.Record = &view
			out.Ledger = append(out.Ledger, "Its record for this: "+view.Words)
		}
	}

	return out
}

// itemLedger reads what the ledger knows of a routine and an item: its history for the
// evaluator, and the item's matches.
func (s *Service) itemLedger(ctx context.Context, l *ledger, routineID, itemID int64) (eval.History, []*match, error) {
	rows, err := l.q.ListItemMatches(ctx, queries.ListItemMatchesParams{ItemID: new(itemID), RowLimit: maxItemHistory})
	if err != nil {
		return eval.History{}, nil, uierr.New("The item's history couldn't be read.", err)
	}

	var records []*match

	for _, row := range rows {
		if row.RoutineID != routineID {
			continue
		}

		m, readErr := readMatch(row)
		if readErr != nil {
			return eval.History{}, nil, uierr.New("A match couldn't be read.", readErr)
		}

		records = append(records, m)
	}

	runs, err := l.itemRuns(ctx, []int64{itemID})
	if err != nil {
		return eval.History{}, nil, uierr.New("The item's runs couldn't be read.", err)
	}

	seen, err := l.baselines(ctx, routineID)
	if err != nil {
		return eval.History{}, nil, uierr.New("The routine's history couldn't be read.", err)
	}

	reset, err := l.tries(ctx, routineID)
	if err != nil {
		return eval.History{}, nil, uierr.New("The routine's tries couldn't be read.", err)
	}

	b, ok := seen[itemID]

	last := ""
	if len(records) > 0 {
		last = outcomeWord(records[0].state(), Reason(records[0].Reason))
	}

	return history(routineID, runs[itemID], b, ok, reset[itemID], last), records, nil
}
