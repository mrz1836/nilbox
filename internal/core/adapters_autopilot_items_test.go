package core

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/budget"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/item"
	prefsstore "github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// errSearchFailed is a search GitHub failed alone.
var errSearchFailed = errors.New("the search failed")

// fakeTokens borrows a token through a runner that prints one, so nothing runs gh.
func fakeTokens() *github.TokenSource {
	return github.NewTokenSource(&testutil.Runner{Stdout: "test-token-not-real\n"}, testutil.Finder{Path: "/opt/fake/bin/gh"})
}

// watchGitHub is GitHub as a routine's test reads it: an inbox refresh that reads the
// account and the GraphQL budget left, the searches, and the checks. Anything else the
// inbox asks it is a test failure: the embedded Client is nil.
type watchGitHub struct {
	inbox.Client

	mu        sync.Mutex
	remaining int
	results   map[string]github.WatchResult
	searched  []string
	checked   []string
}

func (g *watchGitHub) FetchRateLimits(context.Context, github.Token) (*github.RateLimits, error) {
	return &github.RateLimits{}, nil
}

func (g *watchGitHub) FetchInbox(context.Context, github.Token) (*github.Inbox, error) {
	return &github.Inbox{Viewer: github.Viewer{Login: rigViewer}, RateLimit: g.rate(1)}, nil
}

func (g *watchGitHub) FetchDetails(context.Context, github.Token, []string, github.DetailOptions) (*github.Details,
	error,
) {
	return &github.Details{}, nil
}

func (g *watchGitHub) FetchWatches(_ context.Context, _ github.Token, searches []github.WatchSearch) (*github.Watches,
	error,
) {
	out := &github.Watches{Results: map[string]github.WatchResult{}, RateLimit: g.rate(3), Requests: 1}

	g.mu.Lock()
	defer g.mu.Unlock()

	for _, s := range searches {
		g.searched = append(g.searched, s.Query)
		out.Results[s.Key] = g.results[s.Query]
	}

	return out, nil
}

func (g *watchGitHub) FetchChecks(_ context.Context, _ github.Token, ids []string) (*github.ChecksRead, error) {
	g.mu.Lock()
	g.checked = append(g.checked, ids...)
	g.mu.Unlock()

	return &github.ChecksRead{Items: map[string]github.HeadChecks{}, RateLimit: g.rate(2), Requests: 1}, nil
}

// rate is the GraphQL budget after a request that cost cost.
func (g *watchGitHub) rate(cost int) github.QueryRate {
	g.mu.Lock()
	defer g.mu.Unlock()

	return github.QueryRate{Cost: cost, Limit: 5000, Remaining: g.remaining, ResetAt: time.Now().Add(time.Hour)}
}

// asked returns the searches and the pull requests' node IDs GitHub was asked about.
func (g *watchGitHub) asked() (searches, checks []string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	return append([]string(nil), g.searched...), append([]string(nil), g.checked...)
}

// setRemaining has the next inbox refresh read remaining points left of 5,000.
func (g *watchGitHub) setRemaining(remaining int) {
	g.mu.Lock()
	g.remaining = remaining
	g.mu.Unlock()
}

// watchRig is Autopilot's reads of the inbox over a real inbox service whose GitHub is
// watchGitHub, refreshed once for octo-user, with one pull request stored.
type watchRig struct {
	items  autopilotItems
	inbox  *inbox.Service
	github *watchGitHub
	pr     int64
}

// newWatchRig builds the rig with plenty of the GraphQL budget left.
func newWatchRig(t *testing.T) *watchRig {
	t.Helper()

	db := testutil.DB(t)
	gh := &watchGitHub{remaining: 4900, results: map[string]github.WatchResult{
		"is:pr author:app/dependabot": {IssueCount: 0},
		"is:pr label:broken":          {Err: errSearchFailed},
	}}
	svc := inbox.NewService(inbox.Config{Pool: db, Tokens: fakeTokens(), Client: gh})

	_, err := svc.Refresh(t.Context())
	require.NoError(t, err)

	ids, err := inbox.StoreFixture(t.Context(), db, inbox.Fixture{Viewer: rigViewer, Items: []inbox.FixtureItem{{
		Item: github.Item{
			IsPR: true, NodeID: "PR_NODE_7", Number: 7, Title: "Bump zod", URL: "https://github.com/acme-org/example-repo/pull/7",
			CreatedAt: time.Now(), UpdatedAt: time.Now(), AuthorLogin: "dependabot", RepoOwner: boundOwner, RepoName: boundRepo,
		},
		ReviewRequested: true,
	}}})
	require.NoError(t, err)

	return &watchRig{
		items: autopilotItems{store: inbox.NewStore(db, nil), inbox: inbox.NewRoutines(svc)}, inbox: svc, github: gh,
		pr: ids[0],
	}
}

// lowBudget has the inbox read that the GraphQL budget is nearly spent.
func (r *watchRig) lowBudget(t *testing.T) {
	t.Helper()

	r.github.setRemaining(100)

	_, err := r.inbox.Refresh(t.Context())
	require.NoError(t, err)
}

func TestARoutinesTestSearchesGitHubNowAndSaysWhatEachSearchFound(t *testing.T) {
	t.Parallel()

	r := newWatchRig(t)

	got, points, err := r.items.SearchNow(t.Context(), []string{"is:pr author:app/dependabot", "is:pr label:broken"})
	require.NoError(t, err)
	assert.Equal(t, 3, points, "what the searches cost, in GraphQL points")

	require.Contains(t, got, "is:pr author:app/dependabot")
	assert.Zero(t, got["is:pr author:app/dependabot"].Count)
	assert.NotNil(t, got["is:pr author:app/dependabot"].IDs)
	require.NoError(t, got["is:pr author:app/dependabot"].Err)

	require.Contains(t, got, "is:pr label:broken")
	require.ErrorIs(t, got["is:pr label:broken"].Err, errSearchFailed, "a search GitHub failed says why, alone")

	searched, _ := r.github.asked()
	assert.ElementsMatch(t, []string{"is:pr author:app/dependabot", "is:pr label:broken"}, searched)
}

func TestARoutinesReadsOfGitHubWaitWhileTheBudgetIsLow(t *testing.T) {
	t.Parallel()

	r := newWatchRig(t)

	points, err := r.items.ReadChecks(t.Context(), []int64{r.pr})
	require.NoError(t, err)
	assert.Equal(t, 2, points, "what reading the checks cost")

	_, checked := r.github.asked()
	assert.Equal(t, []string{"PR_NODE_7"}, checked)

	r.lowBudget(t)

	_, _, err = r.items.SearchNow(t.Context(), []string{"is:pr author:app/dependabot"})
	require.ErrorIs(t, err, autopilot.ErrLowBudget, "in Autopilot's words")
	require.ErrorIs(t, err, inbox.ErrLowBudget, "and the inbox's")

	_, err = r.items.ReadChecks(t.Context(), []int64{r.pr})
	require.ErrorIs(t, err, autopilot.ErrLowBudget)

	searched, checked := r.github.asked()
	assert.Empty(t, searched, "nothing is sent while the budget is low")
	assert.Len(t, checked, 1)
}

func TestARoutinesTestThatCantReachGitHubSaysSo(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)
	svc := inbox.NewService(inbox.Config{
		Pool: db, Tokens: github.NewTokenSource(noPrograms(), testutil.Finder{Path: "/opt/fake/bin/gh"}),
		Client: &watchGitHub{},
	})
	items := autopilotItems{store: inbox.NewStore(db, nil), inbox: inbox.NewRoutines(svc)}

	_, _, err := items.SearchNow(t.Context(), []string{"is:pr author:app/dependabot"})
	require.Error(t, err, "no token, no search")
	assert.NotErrorIs(t, err, autopilot.ErrLowBudget)
}

func TestARoutinesTestEstimatesWhatItsSearchesAddToEachHour(t *testing.T) {
	t.Parallel()

	r := newWatchRig(t)
	tracker := budget.NewTracker(nil, nil)
	gauge := budget.NewService(budget.Config{Tracker: tracker})
	costs := watchCosts{
		inbox: r.items.inbox, every: func(context.Context) time.Duration { return 5 * time.Minute }, budget: gauge,
	}

	got, err := costs.Estimate(t.Context(), []int{10, 20})
	require.NoError(t, err)
	assert.Equal(t, 1, got.Requests, "two small searches go in one request")
	assert.Equal(t, 1, got.PointsPerRefresh, "a point a request before any watch pass ran")
	assert.Equal(t, 12, got.PointsPerHour, "at a refresh every five minutes")
	assert.InDelta(t, 12.0*100/5000, got.Percent, 1e-9, "of the 5,000 points a token has before the budget is read")

	tracker.ObserveRate(github.RateReading{
		Resource: github.ResourceGraphQL, Limit: 10000, Remaining: 9000,
		ResetAt: time.Now().Add(time.Hour),
	}, 0)

	got, err = costs.Estimate(t.Context(), []int{10, 20})
	require.NoError(t, err)
	assert.InDelta(t, 12.0*100/10000, got.Percent, 1e-9, "of the budget as last read")

	costs.every = func(context.Context) time.Duration { return 0 }

	got, err = costs.Estimate(t.Context(), []int{10})
	require.NoError(t, err)
	assert.Zero(t, got.PointsPerHour, "with no refreshes on their own, nothing an hour")
	assert.Zero(t, got.Percent)
}

func TestTheRoutinesReadsOfTheInboxFailWhenItCantBeRead(t *testing.T) {
	t.Parallel()

	closed := testutil.ClosedDB(t)
	items := autopilotItems{store: inbox.NewStore(closed, nil)}

	_, err := items.Facts(t.Context(), []int64{1}, time.Now())
	require.Error(t, err, "items that can't be read decide nothing")

	_, err = items.Facts(t.Context(), nil, time.Now())
	require.Error(t, err, "nor does an account that can't be read")

	engine := engineItems{autopilotItems: items}

	_, err = engine.ItemStates(t.Context(), []int64{1})
	require.Error(t, err)

	_, err = engine.InboxStatus(t.Context())
	require.Error(t, err)

	require.Error(t, engine.ReadStates(t.Context(), []int64{1}))

	_, err = autopilotPrefs{prefs: prefsstore.New(closed, nil)}.Autopilot(t.Context())
	require.ErrorContains(t, err, "read Autopilot's settings")

	_, err = starterNames{prompts: prompts.NewStore(closed, nil)}.StarterNames(t.Context())
	require.Error(t, err)

	_, err = promptLibrary{prompts: prompts.NewStore(closed, nil)}.Prompts(t.Context())
	require.Error(t, err)

	assert.Nil(t, (&autopilotWatches{}).Watches(t.Context(), rigViewer), "before the routines are built, the inbox reads no search")
	assert.True(t, (&autopilotWatches{}).HoldsWatches())
}

// engineRig is Autopilot's engine's reads of the inbox over a fresh database holding a
// pull request and an issue of octo-user's.
type engineRig struct {
	db     *sql.DB
	store  *inbox.Store
	pr     int64
	issue  int64
	engine engineItems
}

// newEngineRig builds the rig, reading GitHub through client.
func newEngineRig(t *testing.T, client *github.Client) *engineRig {
	t.Helper()

	db := testutil.DB(t)
	stored := func(pr bool, number int) inbox.FixtureItem {
		return inbox.FixtureItem{Item: github.Item{
			IsPR: pr, NodeID: "NODE_" + strconv.Itoa(number), Number: number, Title: "Change " + strconv.Itoa(number),
			URL:       "https://github.com/acme-org/example-repo/pull/" + strconv.Itoa(number),
			CreatedAt: time.Now(), UpdatedAt: time.Now(), AuthorLogin: rigViewer, RepoOwner: boundOwner, RepoName: boundRepo,
		}, Authored: true}
	}

	ids, err := inbox.StoreFixture(t.Context(), db, inbox.Fixture{
		Viewer: rigViewer, Items: []inbox.FixtureItem{stored(true, 7), stored(false, 3)},
	})
	require.NoError(t, err)

	store := inbox.NewStore(db, nil)

	return &engineRig{
		db: db, store: store, pr: ids[0], issue: ids[1],
		engine: engineItems{autopilotItems: autopilotItems{store: store}, tokens: fakeTokens(), client: client},
	}
}

func TestTheEngineReadsWhatIsStoredOfItsItemsStates(t *testing.T) {
	t.Parallel()

	r := newEngineRig(t, nil)

	got, err := r.engine.ItemStates(t.Context(), []int64{r.pr, r.issue, r.issue + 100})
	require.NoError(t, err)
	require.Len(t, got, 2, "an item no longer stored is left out")
	assert.True(t, got[r.pr].Listed, "an item in one of the inbox's lists is listed")
	assert.False(t, got[r.pr].Watched)
	assert.False(t, got[r.pr].FetchedAt.IsZero())
}

func TestTheEngineReadsTheInboxsLastRefreshAndSearches(t *testing.T) {
	t.Parallel()

	r := newEngineRig(t, nil)
	prefs := prefsstore.New(r.db, nil)

	empty, err := r.engine.InboxStatus(t.Context())
	require.NoError(t, err)
	assert.Empty(t, empty.Viewer)
	assert.NotNil(t, empty.Searches)

	at := time.Date(2026, time.November, 19, 8, 30, 0, 0, time.UTC)
	require.NoError(t, prefs.SetMany(t.Context(), map[string]string{
		"inbox.viewer": rigViewer, "inbox.last_refresh_at": at.Format(time.RFC3339), "inbox.signed_out": "1",
		"inbox.watches": `{"searches": {"routine:1": {"answered": true, "count": 120, "returned": 100, "truncated": true,` +
			` "at": "2026-11-19T08:30:00Z"}, "routine:2": {"answered": false, "error": "rate limited"}},` +
			` "paused": "low_budget", "at": "2026-11-19T08:30:00Z"}`,
	}))

	got, err := r.engine.InboxStatus(t.Context())
	require.NoError(t, err)
	assert.Equal(t, rigViewer, got.Viewer)
	assert.True(t, got.LastRefresh.Equal(at))
	assert.True(t, got.WatchedAt.Equal(at))
	assert.Equal(t, "low_budget", got.WatchPaused)
	assert.True(t, got.SignedOut)
	require.Len(t, got.Searches, 2)
	assert.Equal(t, autopilot.SearchState{Answered: true, At: at, Truncated: true}, got.Searches["routine:1"])
	assert.Equal(t, "rate limited", got.Searches["routine:2"].Error)
	assert.False(t, got.Searches["routine:2"].Answered)
}

func TestTheEngineReadsItsItemsStatesFromGitHubAndTheInboxKeepsThem(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)

		_, _ = io.WriteString(w, `{"data": {"r0": {"item": {"__typename": "PullRequest", "title": "Change 7",`+
			` "url": "https://github.com/acme-org/example-repo/pull/7", "prState": "MERGED",`+
			` "mergedAt": "2026-11-18T10:00:00Z", "headRefOid": "abc123", "headRefName": "feature-branch"}},`+
			` "rateLimit": {"cost": 1}}}`)
	}))
	t.Cleanup(srv.Close)

	r := newEngineRig(t, &github.Client{BaseURL: srv.URL, HTTP: srv.Client()})

	require.NoError(t, r.engine.ReadStates(t.Context(), []int64{r.pr}))
	assert.Equal(t, int32(1), requests.Load(), "one request for Autopilot")

	got, err := r.engine.ItemStates(t.Context(), []int64{r.pr})
	require.NoError(t, err)
	assert.Equal(t, github.StateMerged, got[r.pr].State, "the inbox keeps the state Autopilot read")

	require.NoError(t, r.engine.ReadStates(t.Context(), []int64{r.issue + 100}))
	assert.Equal(t, int32(1), requests.Load(), "items no longer stored send nothing")
}

func TestTheEnginesReadOfStatesFailsWithoutAToken(t *testing.T) {
	t.Parallel()

	r := newEngineRig(t, github.NewClient("test"))
	r.engine.tokens = github.NewTokenSource(noPrograms(), testutil.Finder{Path: "/opt/fake/bin/gh"})

	require.Error(t, r.engine.ReadStates(t.Context(), []int64{r.pr}))
}

func TestStatesReadForAnotherServiceAreKeptByTheInboxWhenItCan(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data": {"r0": {"item": {"__typename": "Issue", "title": "Change 3",`+
			` "url": "https://github.com/acme-org/example-repo/issues/3", "issueState": "CLOSED",`+
			` "closedAt": "2026-11-18T10:00:00Z"}}}}`)
	}))
	t.Cleanup(srv.Close)

	var logs bytes.Buffer

	noter := stateNoter{
		Client: &github.Client{BaseURL: srv.URL, HTTP: srv.Client()}, store: inbox.NewStore(testutil.ClosedDB(t), nil),
		logger: slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	refs := []item.Ref{{Repo: repo.Name{Owner: boundOwner, Repo: boundRepo}, Number: 3, Kind: item.KindIssue}}

	tok, err := fakeTokens().Token(t.Context())
	require.NoError(t, err)

	got, err := noter.FetchItemStates(t.Context(), tok, refs)
	require.NoError(t, err, "an inbox that can't keep the states doesn't fail the read")
	assert.Equal(t, github.StateClosed, got[refs[0].Key()].State)
	assert.Contains(t, logs.String(), "could not record the items' states in the inbox")

	bare := stateNoter{Client: &github.Client{BaseURL: srv.URL, HTTP: srv.Client()}}
	got, err = bare.FetchItemStates(t.Context(), tok, refs)
	require.NoError(t, err, "with no inbox, the states are only read")
	assert.Len(t, got, 1)
}

func TestTheEngineReadsAndTriagesThroughTheInbox(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, testOptions(t))
	ctx := t.Context()

	ids, err := inbox.StoreFixture(ctx, c.DB, inbox.Fixture{Viewer: rigViewer, Items: []inbox.FixtureItem{{
		Item: github.Item{
			IsPR: true, NodeID: "NODE_7", Number: 7, Title: "Bump zod", URL: "https://github.com/acme-org/example-repo/pull/7",
			CreatedAt: time.Now(), UpdatedAt: time.Now(), AuthorLogin: "dependabot", RepoOwner: boundOwner, RepoName: boundRepo,
		},
		ReviewRequested: true,
	}}})
	require.NoError(t, err)

	pr := ids[0]
	engine := engineItems{
		autopilotItems: autopilotItems{store: inbox.NewStore(c.DB, nil), inbox: c.InboxRoutines}, service: c.Inbox,
	}

	local, err := engine.LocalStates(ctx, []int64{pr})
	require.NoError(t, err)
	require.Contains(t, local, pr, "the engine decides on the item's triage as stored")

	routine, err := c.Autopilot.Save(ctx, autopilot.Draft{Name: "Pin the bumps", Spec: boundSpec()})
	require.NoError(t, err)

	triage := inboxTriage{faces: c.InboxRoutines}
	recorded := 0

	change, err := triage.Apply(ctx, autopilot.TriageRequest{
		RoutineID: routine.ID, ItemID: pr, Action: catalog.StepPin, Expect: local[pr],
	}, func(_ context.Context, tx *sql.Tx, change autopilot.TriageChange) error {
		recorded++

		assert.NotNil(t, tx, "the effect is recorded in the triage's own transaction")
		assert.True(t, change.Changed())

		return nil
	})
	require.NoError(t, err)
	assert.True(t, change.Changed(), "a routine pins the item")
	assert.NotEqual(t, change.Before, change.After)
	assert.Equal(t, 1, recorded)

	again, err := triage.Apply(ctx, autopilot.TriageRequest{RoutineID: routine.ID, ItemID: pr, Action: catalog.StepPin},
		func(context.Context, *sql.Tx, autopilot.TriageChange) error { return nil })
	require.NoError(t, err)
	assert.False(t, again.Changed(), "an item pinned already is kept as it is")
	assert.NotEmpty(t, again.Kept)

	undo := []autopilot.TriageUndo{{RoutineID: routine.ID, ItemID: pr, Action: catalog.StepPin, Before: change.Before, After: change.After}}

	left, err := triage.AsLeft(ctx, undo)
	require.NoError(t, err)
	assert.Equal(t, []bool{true}, left, "the item is still as the routine left it")

	put, err := triage.Undo(ctx, undo, func(_ context.Context, _ *sql.Tx, put []bool) error {
		assert.Equal(t, []bool{true}, put)

		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, []bool{true}, put, "Undo puts the item back")

	left, err = triage.AsLeft(ctx, undo)
	require.NoError(t, err)
	assert.Equal(t, []bool{false}, left, "and then it isn't as the routine left it")

	require.NoError(t, engine.WatchNow(ctx), "with no account signed in, no search goes out")
	require.Error(t, engine.ReadItem(ctx, pr), "an item can't be read again without a token")
}

func TestTheEngineIsWokenOnlyOnceItIsBuilt(t *testing.T) {
	t.Parallel()

	var waker *engineWaker
	assert.Nil(t, waker.Built(), "a core with no waker has no engine")

	unbuilt := &engineWaker{}
	assert.Nil(t, unbuilt.Built())

	unbuilt.ItemsChanged([]int64{1})
	unbuilt.Wake()
	unbuilt.Outcomes(t.Context(), []autopilot.Outcome{{Kind: "closed"}})

	kept, err := unbuilt.KeptItems(t.Context())
	require.NoError(t, err)
	assert.Nil(t, kept, "before the engine is built, the prune keeps no item for it")

	marks, err := unbuilt.KeptMarks(t.Context())
	require.NoError(t, err)
	assert.Nil(t, marks)

	c := bootstrap(t, testOptions(t))
	built := &engineWaker{engine: c.AutopilotEngine}
	assert.Same(t, c.AutopilotEngine, built.Built())

	built.ItemsChanged([]int64{1})
	built.ItemsChanged([]int64{2})

	marks, err = built.KeptMarks(t.Context())
	require.NoError(t, err)
	assert.Empty(t, marks, "no routine's Restore to undo yet")
}
