package autopilot

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/eval"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilot/watch"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// The builder's reads of the inbox: what the stored items suggest, the items a routine
// watches, and a draft tried on its items now (Test).

const (
	// testFor is how long a test's searches stand before a test of the same searches reads
	// them again.
	testFor = time.Minute

	// maxTestChecks caps the pull requests a test reads the check names of, one batch.
	maxTestChecks = 25

	// maxTestItems caps the items a test lists.
	maxTestItems = 200

	// defaultRefreshes is how many refreshes an hour an estimate assumes without Costs.
	defaultRefreshes = 12

	// defaultLimit is the GraphQL budget an estimate assumes without Costs.
	defaultLimit = 5000

	// newSearch is an estimate's count for a search with no answer, which the refresh packs
	// as a full page, and fullPage the most a search returns.
	newSearch = -1
	fullPage  = 100
)

// ErrNoItems is returned by a Service with no Items for a read that needs the inbox.
var ErrNoItems = errors.New("the inbox can't be read")

// ErrLowBudget is Items' SearchNow sending nothing while the GraphQL budget is under a
// quarter of its size, as the refresh's watch passes pause then.
var ErrLowBudget = errors.New("the GraphQL budget is low")

// lowBudgetError is a search's error in a test that sent none for want of budget.
const lowBudgetError = "Your GitHub API budget is under a quarter of its size, so this search wasn't read. " +
	"Try again once it refills."

// Items reads the inbox for the routines. Core adapts the inbox.
type Items interface {
	// Suggestions returns what the stored items hold that the builder suggests.
	Suggestions(ctx context.Context) (Suggestions, error)

	// WatchedItems returns the stored items of the account signed in that the inbox lists
	// name, or a search with one of keys returns, newest activity first.
	WatchedItems(ctx context.Context, keys []string, lists []catalog.ListSource) ([]int64, error)

	// Facts returns the facts of the stored items among ids, as of now, by id.
	Facts(ctx context.Context, ids []int64, now time.Time) ([]facts.Facts, error)

	// KnownOwners returns, in lower case, every owner of a repository the inbox has seen.
	KnownOwners(ctx context.Context) (map[string]bool, error)

	// SearchNow reads searches from GitHub now and stores what they return, as the
	// refresh's watch pass does, and returns each one's results by query, and what it cost
	// in GraphQL points. While the GraphQL budget is low it sends nothing, and its error
	// is ErrLowBudget.
	SearchNow(ctx context.Context, queries []string) (map[string]SearchResult, int, error)

	// ReadChecks reads and stores the check names of the pull requests among ids, and
	// returns what that cost in GraphQL points. While the GraphQL budget is low it sends
	// nothing, and its error is ErrLowBudget.
	ReadChecks(ctx context.Context, ids []int64) (int, error)
}

// SearchResult is what one search read now found.
type SearchResult struct {
	// IDs are its open results, stored, in its order.
	IDs []int64

	// Count is how many items GitHub says it matches; Truncated reports it returned only
	// the 100 most recently updated.
	Count     int
	Truncated bool

	// Err is why GitHub didn't answer it.
	Err error
}

// Costs gives what the routines' searches cost a refresh, as recorded. Core adapts the
// inbox's last watch pass, the settings, and the API budget.
type Costs interface {
	// Estimate returns what searches cost a refresh and an hour, packed by counts: what
	// each matched, at most a page, or -1 for one with no answer, packed as a full page.
	Estimate(ctx context.Context, counts []int) (Estimate, error)
}

// Estimate is what a routine's searches add to the API budget.
type Estimate struct {
	// Requests is how many requests a refresh sends for them, packed as the refresh packs
	// them, and PointsPerRefresh what those cost, as recorded.
	Requests         int `json:"requests"`
	PointsPerRefresh int `json:"pointsPerRefresh"`

	// PointsPerHour is what they cost an hour at the refresh's pace, and Percent that
	// share of the hourly GraphQL budget.
	PointsPerHour int     `json:"pointsPerHour"`
	Percent       float64 `json:"percent"`
}

// Suggestions is what the stored items hold that the builder suggests, each most common
// first: the people and the bots who opened them, the owners of their repositories, their
// labels, and their check names.
type Suggestions struct {
	Authors []string `json:"authors"`
	Bots    []string `json:"bots"`
	Owners  []string `json:"owners"`
	Labels  []string `json:"labels"`
	Checks  []string `json:"checks"`
}

// emptySuggestions is Suggestions with every list empty, never nil.
func emptySuggestions() Suggestions {
	return Suggestions{Authors: []string{}, Bots: []string{}, Owners: []string{}, Labels: []string{}, Checks: []string{}}
}

// Suggest returns what the stored items hold that the builder suggests: the authors, bots,
// owners, labels, and check names it has seen. It reads the database alone, and no list is
// nil.
func (s *Service) Suggest(ctx context.Context) (Suggestions, error) {
	if s.items == nil {
		return emptySuggestions(), nil
	}

	got, err := s.items.Suggestions(ctx)
	if err != nil {
		return emptySuggestions(), uierr.New("The inbox's items couldn't be read.", err)
	}

	out := emptySuggestions()

	for _, list := range []struct{ dst, src *[]string }{
		{&out.Authors, &got.Authors},
		{&out.Bots, &got.Bots},
		{&out.Owners, &got.Owners},
		{&out.Labels, &got.Labels},
		{&out.Checks, &got.Checks},
	} {
		*list.dst = append(*list.dst, *list.src...)
	}

	return out, nil
}

// Watching returns the IDs of the stored items the routine id watches, newest activity
// first: those in the inbox lists it names and those its searches return, as the last
// refresh read them, a search that failed keeping its items. The inbox's ItemsByID turns
// them into rows. A routine that is gone or deleted is refused; one that is broken
// watches nothing.
func (s *Service) Watching(ctx context.Context, id int64) ([]int64, error) {
	r, err := s.Get(ctx, id)
	if err != nil {
		return []int64{}, err
	}

	if r.BrokenReason != "" || s.items == nil {
		return []int64{}, nil
	}

	keys := make([]string, 0, len(r.Spec.Watch.Sources))
	for _, q := range searchesOf(r.Spec, s.viewer(ctx)) {
		keys = append(keys, q.Key)
	}

	ids, err := s.items.WatchedItems(ctx, keys, listsOf(r.Spec))
	if err != nil {
		return []int64{}, uierr.New("The routine's items couldn't be read.", err)
	}

	return nonNil(ids), nil
}

// nonNil returns ids, or an empty list for nil.
func nonNil[T any](list []T) []T {
	if list == nil {
		return []T{}
	}

	return list
}

// TestResult is a draft tried on its items now: what each search found, and what the
// routine decides about each item, with what the test spent and what the searches would
// cost a refresh.
type TestResult struct {
	// Searches are the draft's searches, each with what it found and any warning.
	Searches []TestSearch `json:"searches"`

	// Items are the items it watches, the matches first, each with the routine's decision;
	// at most maxTestItems.
	Items []TestItem `json:"items"`

	// Matched, Unknown, and Watched count the items the routine matches, can't decide yet,
	// and watches in all.
	Matched int `json:"matched"`
	Unknown int `json:"unknown"`
	Watched int `json:"watched"`

	// Points is what the test spent of the GraphQL budget: its searches, the details of
	// the items they returned, and the check names it read. Cached says its searches were
	// read within the last minute, and cost nothing again.
	Points int  `json:"points"`
	Cached bool `json:"cached"`

	// LowBudget says the GraphQL budget was under a quarter of its size, so the test sent
	// nothing: each search it had no answer for says so, and no check name was read, even
	// when every search came from the last minute's cache.
	LowBudget bool `json:"lowBudget"`

	// Estimate is what the draft's searches would add to each refresh.
	Estimate Estimate `json:"estimate"`

	// Warnings are what the test found may make the draft do less than meant, each where it
	// is in the spec, beside each search's own.
	Warnings []spec.Problem `json:"warnings"`
}

// TestSearch is one of a draft's searches, as a test read it.
type TestSearch struct {
	// Query is what GitHub was sent.
	Query string `json:"query"`

	// Count is how many items it matches, Returned how many it returned, and Truncated
	// reports it returned only the 100 most recently updated.
	Count     int  `json:"count"`
	Returned  int  `json:"returned"`
	Truncated bool `json:"truncated"`

	// Error is why GitHub didn't answer it, in the window's words, "" when it did.
	Error string `json:"error"`

	// Warnings are what may make it find less than meant, such as an owner the inbox has
	// never seen, which GitHub answers with nothing rather than an error.
	Warnings []string `json:"warnings"`
}

// TestItem is one item a draft watches, and what the routine decides about it.
type TestItem struct {
	ID     int64     `json:"id"`
	Kind   item.Kind `json:"kind"`
	Repo   string    `json:"repo"`
	Number int       `json:"number"`
	Title  string    `json:"title"`
	URL    string    `json:"url"`

	// Match is yes, no, or unknown; FireKey is the occurrence it fires for while the
	// trigger holds; Needs is what a read would have to bring for an unknown to decide.
	Match   eval.Result  `json:"match"`
	FireKey string       `json:"fireKey"`
	Needs   []facts.Need `json:"needs"`

	// Trace is each step of the decision: the trigger, then each condition that ran.
	Trace []eval.Trace `json:"trace"`

	// LeftAlone reports an item the routine leaves alone, since the user took back what it
	// did there: it doesn't act on it, matching or not, until Let it act again. A new
	// routine leaves none alone.
	LeftAlone bool `json:"leftAlone"`

	// Record says what the routine's history holds of the occurrence it fires for now, in
	// the window's words ("It already ran on this.", "Waiting for your OK."), "" for none:
	// a match the routine has no record of is one it would run on. A new routine has none.
	Record string `json:"record"`

	// Listed reports an item in one of the inbox lists, where triage shows.
	Listed bool `json:"listed"`

	// RunsNow reports an item Run on the matches now would run on: it matches, the routine
	// doesn't leave it alone, and the routine has no record of its occurrence but one that
	// closed before it ran, or that already matched when the user chose only what changes
	// from now on. A new routine runs on nothing until it is saved.
	RunsNow bool `json:"runsNow"`
}

// testCache keeps the searches tests read, by query, for testFor.
type testCache struct {
	mu   sync.Mutex
	read map[string]cachedSearch
}

// cachedSearch is one search a test read, and when.
type cachedSearch struct {
	result SearchResult
	at     time.Time
}

// Test tries the draft d on its items now: its searches are read from GitHub, unless a
// test read them within the last minute, and the items they and its lists return are
// stored as the refresh stores watched items; the routine is evaluated on each, with the
// check names of the pull requests that need them read too (one batch); the searches are
// checked for an owner the inbox has never seen; and the result says what the test spent
// and what the searches would cost each refresh. While the GraphQL budget is low it sends
// nothing, and says so (LowBudget). A draft whose spec has problems is refused with them,
// as Save refuses it. It changes no routine, and runs nothing. While Autopilot is off, or
// its settings can't be read, it asks GitHub nothing and is refused (ErrOff).
func (s *Service) Test(ctx context.Context, d Draft) (TestResult, error) {
	out := TestResult{Searches: []TestSearch{}, Items: []TestItem{}, Warnings: []spec.Problem{}}

	if err := s.refuseOff(ctx); err != nil {
		return out, err
	}

	_, sp, err := checkSpec(d.Spec)
	if err != nil {
		return out, err
	}

	if s.items == nil {
		return out, uierr.New("The inbox can't be read, so the routine can't be tried.", ErrNoItems)
	}

	viewer := s.viewer(ctx)
	queries := searchesOf(sp, viewer)

	found, err := s.testSearches(ctx, queries, &out)
	if err != nil {
		return out, uierr.New("GitHub couldn't be asked about the searches.", err)
	}

	if err = s.warnOwners(ctx, sp, &out); err != nil {
		return out, err
	}

	listed, err := s.items.WatchedItems(ctx, nil, listsOf(sp))
	if err != nil {
		return out, uierr.New("The inbox's lists couldn't be read.", err)
	}

	ids := union(listed, found)
	out.Watched = len(ids)

	if out.Items, err = s.evaluate(ctx, sp, ids, &out); err != nil {
		return out, err
	}

	if err = s.markLeftAlone(ctx, d.ID, out.Items); err != nil {
		return out, err
	}

	if err = s.markRecords(ctx, d.ID, out.Items); err != nil {
		return out, err
	}

	out.Matched, out.Unknown = counts(out.Items)
	out.Estimate = s.estimate(ctx, out.Searches)
	warnTruncated(out.Searches)
	out.Warnings = testWarnings(sp, out.Items)

	return out, nil
}

// warnTruncated warns of each search that matches more than a page: only the most
// recently updated are checked.
func warnTruncated(searches []TestSearch) {
	for i, ts := range searches {
		if ts.Truncated {
			searches[i].Warnings = append(searches[i].Warnings, fmt.Sprintf(
				"This search matches %s; only the newest %d are checked.",
				textutil.Counted(ts.Count, "item", "items"), fullPage,
			))
		}
	}
}

// testWarnings returns what a test found may make sp do less than meant: triage steps
// when most of the items it watches aren't in the inbox lists, so triage won't change
// what the user sees. A watch of searches alone gets none, since Describe already warns
// each triage step of it (triageWarnings), and the step would say it twice.
func testWarnings(sp spec.Spec, items []TestItem) []spec.Problem {
	out := []spec.Problem{}

	if onlySearches(sp.Watch) {
		return out
	}

	outside := 0

	for _, it := range items {
		if !it.Listed {
			outside++
		}
	}

	if len(items) == 0 || outside*2 <= len(items) {
		return out
	}

	for i, st := range sp.Steps {
		if slices.Contains(triageSteps(), st.Kind()) {
			info, _ := catalog.StepByKind(st.Kind())
			out = append(out, spec.Problem{
				Part: spec.PartSteps, Field: fmt.Sprintf("[%d]", i),
				Message: fmt.Sprintf("%d of the %d items it watches now aren't in your inbox, so %s won't "+
					"change what you see of them.", outside, len(items), info.Label),
			})
		}
	}

	return out
}

// markRecords says, of each item, what the routine id's history holds of the occurrence
// it fires for now: its real record, else what a Dry run recorded. A new routine (0) has
// none.
func (s *Service) markRecords(ctx context.Context, id int64, items []TestItem) error {
	if id == 0 || s.store.db == nil {
		return nil
	}

	keys, err := s.ledger().keys(ctx, id)
	if err != nil {
		return uierr.New("The routine's history couldn't be read.", err)
	}

	for i := range items {
		markRecord(&items[i], keys)
	}

	return nil
}

// markRecord says, of it, what keys, a routine's matches, hold of the occurrence it fires
// for now, and whether Run on the matches now would run on it.
func markRecord(it *TestItem, keys map[matchKey]keyRow) {
	if it.FireKey == "" {
		return
	}

	row, recorded := keys[matchKey{item: it.ID, fire: it.FireKey}]
	it.RunsNow = it.Match == eval.Yes && !it.LeftAlone && (!recorded || runsNow(row))

	if recorded && !reopensRow(row) {
		it.Record = recordWords(row.State, row.Reason)
	} else if _, ok := keys[matchKey{item: it.ID, fire: it.FireKey, dryRun: true}]; ok {
		it.Record = "Dry run recorded what it would do."
	}
}

// recordWords says what a routine's record of an occurrence holds, as a test's row shows
// it: a record that closed, was skipped, or was dismissed for a reason says it in
// Activity's words (reasonWords); any other says where it stands.
func recordWords(state State, reason Reason) string {
	ended := state == StateClosed || state == StateSkipped || state == StateDismissed

	if w, ok := reasonWords()[reason]; ended && ok && reason != ReasonRan && reason != ReasonWouldRun {
		return w + "."
	}

	words := map[State]string{
		StateWaiting: "Waiting for your OK.", StateBlocked: "Can't start yet.", StateQueued: "Queued.",
		StateRunning: "Working now.", StateVerifying: "Waiting for the checks on its commit.",
		StateDone: "It already ran on this.", StateClosed: "Closed.", StateSkipped: "Skipped.",
		StateDismissed: "You skipped it.",
		StateBaseline:  "It already matched when you chose only what changes from now on.",
	}

	if w, ok := words[state]; ok {
		return w
	}

	return textutil.Sentence(string(state))
}

// reasonWords are Activity's words for why a match closed, each reason's label in the
// window's reasonChips (frontend/src/lib/autopilot.ts), which a test holds them to.
func reasonWords() map[Reason]string {
	return map[Reason]string{
		ReasonRan:             "Ran",
		ReasonWouldRun:        "Would run",
		ReasonTries:           "Gave up",
		ReasonOutside:         "Skipped: an outside contributor's",
		ReasonYouSkipped:      "You skipped it",
		ReasonYouStopped:      "Stopped by you",
		ReasonMerged:          "Closed: it merged",
		ReasonItemClosed:      "Closed: the item closed",
		ReasonGone:            "Closed: the item is gone",
		ReasonLeftWatch:       "Closed: it left the watch",
		ReasonMovedOn:         "Closed: it moved on",
		ReasonNoLongerMatches: "Closed: no longer matches",
		ReasonRoutineChanged:  "Closed: the routine changed",
		ReasonRoutineOff:      "Closed: the routine was off",
		ReasonNotOffered:      "Closed: its step isn't offered",
		ReasonFailed:          "A step failed",
		ReasonFromNow:         "Already matched when you turned it on",
		ReasonHandsOff:        "Left alone",
		ReasonTakenOver:       "Taken over",
	}
}

// markLeftAlone marks the items the routine id leaves alone, none for a new routine (0).
func (s *Service) markLeftAlone(ctx context.Context, id int64, items []TestItem) error {
	if id == 0 || s.store.db == nil {
		return nil
	}

	handsOff, err := s.ledger().handsOff(ctx, id)
	if err != nil {
		return uierr.New("The items the routine leaves alone couldn't be read.", err)
	}

	for i := range items {
		items[i].LeftAlone = handsOff[items[i].ID]
	}

	return nil
}

// counts counts the items the routine matches, and those it can't decide yet.
func counts(items []TestItem) (matched, unknown int) {
	for _, it := range items {
		switch it.Match {
		case eval.Yes:
			matched++
		case eval.Unknown:
			unknown++
		case eval.No:
		}
	}

	return matched, unknown
}

// testSearches reads each query, from the cache when a test read it within testFor, and
// records each one's outcome in out. It returns the IDs they found, in order. While the
// budget is low, the queries it has no answer for say so, and out does.
func (s *Service) testSearches(ctx context.Context, queries []watch.Query, out *TestResult) ([]int64, error) {
	now := s.clock.Now()
	results, missing := s.tests.fresh(queries, now)

	if len(missing) > 0 {
		read, points, err := s.items.SearchNow(ctx, missing)

		switch {
		case errors.Is(err, ErrLowBudget):
			out.LowBudget = true
		case err != nil:
			return nil, err
		}

		out.Points += points

		s.tests.keep(read, now)

		for q, r := range read {
			results[q] = r
		}
	}

	out.Cached = len(queries) > 0 && len(missing) == 0

	var ids []int64

	for _, q := range queries {
		r, answered := results[q.Text]
		ts := TestSearch{Query: q.Text, Count: r.Count, Returned: len(r.IDs), Truncated: r.Truncated, Warnings: []string{}}

		switch {
		case !answered && out.LowBudget:
			ts.Error = lowBudgetError
		case r.Err != nil:
			ts.Error = uierr.Message(r.Err)
		}

		out.Searches = append(out.Searches, ts)
		ids = append(ids, r.IDs...)
	}

	return ids, nil
}

// fresh returns the cached results of the queries read since now less testFor, by query,
// and the queries it has none for.
func (c *testCache) fresh(queries []watch.Query, now time.Time) (map[string]SearchResult, []string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	out := make(map[string]SearchResult, len(queries))

	var missing []string

	for _, q := range queries {
		if cached, ok := c.read[q.Text]; ok && now.Sub(cached.at) < testFor {
			out[q.Text] = cached.result
		} else if !slices.Contains(missing, q.Text) {
			missing = append(missing, q.Text)
		}
	}

	return out, missing
}

// keep caches the results read at now, and forgets what is older than testFor.
func (c *testCache) keep(read map[string]SearchResult, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for q, cached := range c.read {
		if now.Sub(cached.at) >= testFor {
			delete(c.read, q)
		}
	}

	for q, r := range read {
		if r.Err == nil {
			c.read[q] = cachedSearch{result: r, at: now}
		}
	}
}

// warnOwners warns of each user or org a search names that the inbox has never seen: one
// GitHub doesn't know is answered with nothing, and no error, so a typo looks like a
// search that finds nothing.
func (s *Service) warnOwners(ctx context.Context, sp spec.Spec, out *TestResult) error {
	known, err := s.items.KnownOwners(ctx)
	if err != nil {
		return uierr.New("The inbox's repositories couldn't be read.", err)
	}

	owners := ownersByQuery(sp, s.viewer(ctx))

	for i, ts := range out.Searches {
		for _, owner := range owners[ts.Query] {
			if !known[strings.ToLower(owner)] {
				out.Searches[i].Warnings = append(out.Searches[i].Warnings, fmt.Sprintf(
					"No item of %s's has been in your inbox, so GitHub may not know the name: it answers a name it "+
						"doesn't know with nothing.", owner,
				))
			}
		}
	}

	return nil
}

// ownersByQuery returns, for each of sp's searches built for the account viewer, by query,
// the owners it names in Where.
func ownersByQuery(sp spec.Spec, viewer string) map[string][]string {
	out := map[string][]string{}

	for _, src := range sp.Watch.Sources {
		if src.Search == nil {
			continue
		}

		if q, err := watch.Build(*src.Search, viewer); err == nil {
			out[q.Text] = append(out[q.Text], namedOwners(*src.Search)...)
		}
	}

	return out
}

// namedOwners returns the users, orgs, and repositories' owners a search names in Where,
// your account left out.
func namedOwners(sr watch.Search) []string {
	var out []string

	for _, w := range sr.Where {
		q, v, ok := strings.Cut(w, ":")
		if !ok || w == watch.Me {
			continue
		}

		if q == "repo" {
			v, _, _ = strings.Cut(v, "/")
		}

		if v != "" && !slices.ContainsFunc(out, func(o string) bool { return sameLogin(o, v) }) {
			out = append(out, v)
		}
	}

	return out
}

// union returns a's IDs, then b's not in a, each once.
func union(a, b []int64) []int64 {
	out := make([]int64, 0, len(a)+len(b))
	seen := make(map[int64]bool, len(a)+len(b))

	for _, id := range slices.Concat(a, b) {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}

	return out
}

// evaluate decides the routine sp on each item of ids, reading the check names of the pull
// requests whose decision needs them (one batch, added to out's points; none while the
// budget is low), the matches first, then the unknowns.
func (s *Service) evaluate(ctx context.Context, sp spec.Spec, ids []int64, out *TestResult) ([]TestItem, error) {
	ids = ids[:min(len(ids), maxTestItems)]
	if len(ids) == 0 {
		return []TestItem{}, nil
	}

	now := s.clock.Now().UTC()

	items, need, err := s.decide(ctx, sp, ids, now)
	if err != nil {
		return nil, err
	}

	if len(need) > 0 && !out.LowBudget {
		points, readErr := s.items.ReadChecks(ctx, need[:min(len(need), maxTestChecks)])
		out.Points += points

		switch {
		case errors.Is(readErr, ErrLowBudget):
			out.LowBudget = true // the searches came from the cache, so this is the first to say
		case readErr != nil:
			s.logger.WarnContext(ctx, "a routine's test could not read the check names", "error", readErr)
		default:
			if items, _, err = s.decide(ctx, sp, ids, now); err != nil {
				return nil, err
			}
		}
	}

	rank := map[eval.Result]int{eval.Yes: 0, eval.Unknown: 1, eval.No: 2}

	slices.SortStableFunc(items, func(a, b TestItem) int { return rank[a.Match] - rank[b.Match] })

	return items, nil
}

// decide evaluates sp on each item of ids as of now, and returns the decisions and the
// pull requests whose decision waits on their check names.
func (s *Service) decide(ctx context.Context, sp spec.Spec, ids []int64, now time.Time) ([]TestItem, []int64, error) {
	all, err := s.items.Facts(ctx, ids, now)
	if err != nil {
		return nil, nil, uierr.New("The items' facts couldn't be read.", err)
	}

	out := make([]TestItem, 0, len(all))

	var need []int64

	for _, f := range all {
		d := eval.Evaluate(sp, f, eval.History{}, now)
		lists := f.Value(catalog.FieldLists)

		out = append(out, TestItem{
			ID: f.Item.ID, Kind: f.Item.Kind, Repo: f.Item.Repo, Number: f.Item.Number, Title: f.Item.Title,
			URL: f.Item.URL, Match: d.Match, FireKey: d.FireKey, Needs: nonNil(d.Needs), Trace: nonNil(d.Trace),
			Listed: lists.Known() && len(lists.List) > 0,
		})

		if d.Match == eval.Unknown && slices.Contains(d.Needs, facts.NeedChecks) {
			need = append(need, f.Item.ID)
		}
	}

	return out, need, nil
}

// estimate works out what the searches would add to each refresh, packed as the refresh
// packs them, by what each matched (at most a page; a full page for one with no answer),
// and the recorded costs.
func (s *Service) estimate(ctx context.Context, searches []TestSearch) Estimate {
	counts := make([]int, 0, len(searches))

	for _, sr := range searches {
		if sr.Error != "" {
			counts = append(counts, newSearch)
		} else {
			counts = append(counts, min(sr.Count, fullPage))
		}
	}

	if len(counts) == 0 {
		return Estimate{}
	}

	if s.costs != nil {
		est, err := s.costs.Estimate(ctx, counts)
		if err == nil {
			return est
		}

		s.logger.WarnContext(ctx, "could not estimate what a routine's searches cost", "error", err)
	}

	requests := 1
	perHour := requests * defaultRefreshes

	return Estimate{
		Requests: requests, PointsPerRefresh: requests, PointsPerHour: perHour,
		Percent: float64(perHour) * 100 / defaultLimit,
	}
}
