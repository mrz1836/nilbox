package inbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/trust"
)

// fakeToken is a placeholder that matches no real token format.
const fakeToken = "test-token-not-real"

var (
	errFakeNetwork = errors.New("network unreachable")
	errFakeBroken  = errors.New("refresher broke")
	errNotParallel = errors.New("the inbox query waited for a details request that never started")
)

// fakeTokens hands out a fixed token, or fails, and records the tokens it is told to
// forget. It is safe for concurrent use; a test reads its fields once the calls it made
// have returned.
type fakeTokens struct {
	mu     sync.Mutex
	err    error
	calls  int
	forgot []string
}

func (f *fakeTokens) Token(ctx context.Context) (github.Token, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls++

	if err := ctx.Err(); err != nil {
		return github.Token{}, err
	}

	if f.err != nil {
		return github.Token{}, f.err
	}

	return github.NewToken(fakeToken), nil
}

func (f *fakeTokens) Forget(tok github.Token) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.forgot = append(f.forgot, tok.Reveal())
}

// fakeClient returns canned results. The inbox and rate-limit fetches run concurrently.
type fakeClient struct {
	mu          sync.Mutex
	inbox       *github.Inbox
	limits      *github.RateLimits
	avatar      *github.Avatar
	details     map[string]github.ItemDetails
	detailsRate github.QueryRate
	inboxErr    error
	limitsErr   error
	avatarErr   error
	detailsErr  error
	items       map[string]*github.ItemUpdate
	itemErr     error
	gotTokens   []string
	avatarURLs  []string
	detailsIDs  [][]string
	itemIDs     []string

	// files holds each pull request's changed files by "owner/name#number"; its Head is
	// also what FetchPullHead reports. filesErr fails every file and head request.
	files     map[string]*github.ChangedFiles
	filesErr  error
	fileReads []string
	headReads []string

	// inboxGate, when set, holds the inbox query until a details request has started,
	// which only happens when the two run side by side.
	inboxGate  chan struct{}
	gateOpened sync.Once
}

func (c *fakeClient) FetchInbox(_ context.Context, tok github.Token) (*github.Inbox, error) {
	if c.inboxGate != nil {
		select {
		case <-c.inboxGate:
		case <-time.After(2 * time.Second):
			return nil, errNotParallel
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.gotTokens = append(c.gotTokens, tok.Reveal())

	return c.inbox, c.inboxErr
}

func (c *fakeClient) FetchRateLimits(_ context.Context, tok github.Token) (*github.RateLimits, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.gotTokens = append(c.gotTokens, tok.Reveal())

	return c.limits, c.limitsErr
}

// FetchDetails answers with the canned details of the IDs asked for, as one request.
func (c *fakeClient) FetchDetails(_ context.Context, tok github.Token, ids []string) (*github.Details, error) {
	if c.inboxGate != nil {
		c.gateOpened.Do(func() { close(c.inboxGate) })
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.gotTokens = append(c.gotTokens, tok.Reveal())
	c.detailsIDs = append(c.detailsIDs, ids)

	if c.detailsErr != nil {
		return nil, c.detailsErr
	}

	out := &github.Details{Items: map[string]github.ItemDetails{}, RateLimit: c.detailsRate}
	if len(ids) > 0 {
		out.Requests = 1
	}

	for _, id := range ids {
		if d, ok := c.details[id]; ok {
			out.Items[id] = d
		}
	}

	return out, nil
}

// FetchItem answers with the canned update for the ID, or with one that did not find it.
func (c *fakeClient) FetchItem(_ context.Context, tok github.Token, id string) (*github.ItemUpdate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.gotTokens = append(c.gotTokens, tok.Reveal())
	c.itemIDs = append(c.itemIDs, id)

	if c.itemErr != nil {
		return nil, c.itemErr
	}

	if update, ok := c.items[id]; ok {
		return update, nil
	}

	return &github.ItemUpdate{}, nil
}

// FetchPullHead answers with the head of the canned files.
func (c *fakeClient) FetchPullHead(_ context.Context, tok github.Token, ref item.Ref) (*github.PullHead, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.gotTokens = append(c.gotTokens, tok.Reveal())
	c.headReads = append(c.headReads, ref.String())

	if c.filesErr != nil {
		return nil, c.filesErr
	}

	files, ok := c.files[ref.String()]
	if !ok {
		return nil, &github.StatusError{StatusCode: 404, Message: "Not Found"}
	}

	return &github.PullHead{Head: files.Head, ChangedFiles: files.Total, State: "open"}, nil
}

// FetchChangedFiles answers with the canned files, whatever head was asked for.
func (c *fakeClient) FetchChangedFiles(
	_ context.Context, tok github.Token, ref item.Ref, _ string,
) (*github.ChangedFiles, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.gotTokens = append(c.gotTokens, tok.Reveal())
	c.fileReads = append(c.fileReads, ref.String())

	if c.filesErr != nil {
		return nil, c.filesErr
	}

	files, ok := c.files[ref.String()]
	if !ok {
		return nil, &github.StatusError{StatusCode: 404, Message: "Not Found"}
	}

	return files, nil
}

func (c *fakeClient) FetchAvatar(_ context.Context, url string) (*github.Avatar, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.avatarURLs = append(c.avatarURLs, url)

	return c.avatar, c.avatarErr
}

// harness is a Service over a temp database with fakes for everything else.
type harness struct {
	svc    *Service
	pool   *sql.DB
	path   string
	tokens *fakeTokens
	client *fakeClient
	clock  *testutil.Clock
	events *testutil.Events
}

// newHarness opens a fresh database and a Service over it.
func newHarness(t *testing.T) *harness {
	t.Helper()

	path := filepath.Join(t.TempDir(), "app.db")

	h := &harness{
		pool:   testutil.DBAt(t, path),
		path:   path,
		tokens: &fakeTokens{},
		client: &fakeClient{limits: restLimits(), avatar: fakeAvatar()},
		clock:  testutil.NewClock(time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)),
		events: &testutil.Events{},
	}

	h.svc = h.service()

	return h
}

// service builds a Service over the harness's database and fakes.
func (h *harness) service() *Service {
	return NewService(Config{
		Pool: h.pool, Tokens: h.tokens, Client: h.client, Clock: h.clock, Events: h.events,
	})
}

// refresh runs a refresh that must succeed.
func (h *harness) refresh(t *testing.T) Snapshot {
	t.Helper()

	snap, err := h.svc.Refresh(t.Context())
	require.NoError(t, err)

	return snap
}

// exec runs a statement on the harness's database. The statement writes past the
// service, so it also empties the service's snapshot cache, as the service's own writes
// do.
func (h *harness) exec(t *testing.T, query string, args ...any) {
	t.Helper()

	_, err := h.pool.ExecContext(t.Context(), query, args...)
	require.NoError(t, err)

	h.svc.snapshots.empty()
}

// storedItem is a stored item's row, as the tests read it.
type storedItem struct {
	ID                    int64
	Number                int
	Kind                  item.Kind
	AuthorLogin           string
	AuthorType            string
	AuthorAssociation     string
	HeadRef               string
	IsCrossRepository     bool
	ReasonReviewRequested bool
	ReasonAuthored        bool
	ReasonAssigned        bool
}

// storedItems returns every stored item, by number.
func (h *harness) storedItems(t *testing.T) []storedItem {
	t.Helper()

	rows, err := h.pool.QueryContext(t.Context(), `SELECT id, number, kind, author_login, author_type,
    author_association, head_ref, is_cross_repository, reason_review_requested, reason_authored, reason_assigned
FROM items ORDER BY number`)
	require.NoError(t, err)

	defer func() { _ = rows.Close() }()

	var items []storedItem

	for rows.Next() {
		var it storedItem
		require.NoError(t, rows.Scan(&it.ID, &it.Number, &it.Kind, &it.AuthorLogin, &it.AuthorType,
			&it.AuthorAssociation, &it.HeadRef, &it.IsCrossRepository, &it.ReasonReviewRequested, &it.ReasonAuthored,
			&it.ReasonAssigned))
		items = append(items, it)
	}

	require.NoError(t, rows.Err())

	return items
}

// storedItemNumbered returns the one stored item with a number.
func (h *harness) storedItemNumbered(t *testing.T, number int) storedItem {
	t.Helper()

	var found []storedItem

	for _, it := range h.storedItems(t) {
		if it.Number == number {
			found = append(found, it)
		}
	}

	require.Len(t, found, 1)

	return found[0]
}

// reasons returns an item's three reason flags by number.
func (h *harness) reasons(t *testing.T, number int) [3]bool {
	t.Helper()

	it := h.storedItemNumbered(t, number)

	return [3]bool{it.ReasonReviewRequested, it.ReasonAuthored, it.ReasonAssigned}
}

// count returns the number of rows in a table.
func (h *harness) count(t *testing.T, table string) int64 {
	t.Helper()

	var n int64
	require.NoError(t, h.pool.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table).Scan(&n))

	return n
}

// column returns the text of the first column of every row a query returns.
func (h *harness) column(t *testing.T, query string, args ...any) []string {
	t.Helper()

	var out []string

	for _, row := range h.rows(t, query, args...) {
		for _, v := range row {
			out = append(out, fmt.Sprint(v))
		}
	}

	return out
}

// rows returns every row a query returns, each as a map of column to value.
func (h *harness) rows(t *testing.T, query string, args ...any) []map[string]any {
	t.Helper()

	rows, err := h.pool.QueryContext(t.Context(), query, args...)
	require.NoError(t, err)

	defer func() { _ = rows.Close() }()

	cols, err := rows.Columns()
	require.NoError(t, err)

	var out []map[string]any

	for rows.Next() {
		values := make([]any, len(cols))
		dest := make([]any, len(cols))

		for i := range values {
			dest[i] = &values[i]
		}

		require.NoError(t, rows.Scan(dest...))

		row := make(map[string]any, len(cols))
		for i, col := range cols {
			row[col] = values[i]
		}

		out = append(out, row)
	}

	require.NoError(t, rows.Err())

	return out
}

// insertRate stores a core rate reading for the stored account, taken at takenAt, and
// returns its id.
func (h *harness) insertRate(t *testing.T, limit int, takenAt time.Time) int64 {
	t.Helper()

	var id int64

	require.NoError(t, h.pool.QueryRowContext(t.Context(), `INSERT INTO rate_snapshots
(account_id, resource, limit_total, remaining, used, cost, reset_at, taken_at)
SELECT id, ?, ?, 0, 0, 0, ?, ? FROM accounts RETURNING id`, resourceCore, limit, takenAt, takenAt).Scan(&id))

	h.svc.snapshots.empty()

	return id
}

// pr builds a fetched pull request in example-repo.
func pr(number int, assoc string, crossRepo bool) github.Item {
	return githubItem("example-repo", number, assoc, true, crossRepo)
}

// issue builds a fetched issue.
func issue(repo string, number int, assoc string) github.Item {
	return githubItem(repo, number, assoc, false, false)
}

// nodeID is the fake node ID of the item with a number.
func nodeID(number int) string {
	return fmt.Sprintf("NODE_%d", number)
}

// githubItem builds a fetched item in acme-org.
func githubItem(repo string, number int, assoc string, isPR, crossRepo bool) github.Item {
	updated := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(number) * time.Hour)

	it := github.Item{
		IsPR:              isPR,
		NodeID:            nodeID(number),
		Number:            number,
		Title:             fmt.Sprintf("Item %d", number),
		URL:               fmt.Sprintf("https://github.com/acme-org/%s/pull/%d", repo, number),
		CreatedAt:         updated.Add(-time.Hour),
		UpdatedAt:         updated,
		AuthorLogin:       octoUser,
		AuthorAssociation: assoc,
		IsCrossRepository: crossRepo,
		RepoOwner:         "acme-org",
		RepoName:          repo,
	}

	if isPR {
		it.HeadRef = fmt.Sprintf("branch-%d", number)
	}

	return it
}

// octoUser is the signed-in account's login in the fake inbox.
const octoUser = "octo-user"

// Avatar URLs on GitHub's avatar host for the fake accounts.
const (
	octoUserAvatar  = "https://avatars.githubusercontent.com/u/101?v=4&s=128"
	octoOtherAvatar = "https://avatars.githubusercontent.com/u/202?v=4&s=128"
)

// fakeAvatar is a downloaded avatar; the bytes need not be a real image here.
func fakeAvatar() *github.Avatar {
	return &github.Avatar{ContentType: "image/png", Data: []byte("not-really-a-png")}
}

// fetched builds an inbox whose totals equal the items returned.
func fetched(review, authored, assigned []github.Item) *github.Inbox {
	return &github.Inbox{
		Viewer: github.Viewer{
			Login:     octoUser,
			Name:      "Octo User",
			URL:       "https://github.com/octo-user",
			AvatarURL: octoUserAvatar,
		},
		ReviewRequested: github.SearchResult{IssueCount: len(review), Items: review},
		Authored:        github.SearchResult{IssueCount: len(authored), Items: authored},
		Assigned:        github.SearchResult{IssueCount: len(assigned), Items: assigned},
		RateLimit: github.QueryRate{
			Cost: 1, Limit: 5000, Remaining: 4990, Used: 10,
			ResetAt: time.Date(2026, 1, 10, 13, 0, 0, 0, time.UTC),
		},
		OAuthScopes: "repo, read:org",
	}
}

// restLimits returns REST budgets for the gauge.
func restLimits() *github.RateLimits {
	reset := time.Date(2026, 1, 10, 13, 0, 0, 0, time.UTC)

	return &github.RateLimits{
		Core:    github.Rate{Limit: 5000, Used: 20, Remaining: 4980, ResetAt: reset},
		GraphQL: github.Rate{Limit: 5000, Used: 10, Remaining: 4990, ResetAt: reset},
		Search:  github.Rate{Limit: 30, Used: 2, Remaining: 28, ResetAt: reset.Add(-59 * time.Minute)},
	}
}

// numbers lists a list's item numbers in order.
func numbers(l List) []int {
	out := make([]int, 0, len(l.Items))
	for _, it := range l.Items {
		out = append(out, it.Number)
	}

	return out
}

func TestRefreshUpsertsItemsWithReasonFlags(t *testing.T) {
	h := newHarness(t)

	a := pr(1, "OWNER", false)
	b := pr(2, "OWNER", false)
	c := issue("other-repo", 3, "MEMBER")

	// A is both review-requested and assigned.
	h.client.inbox = fetched([]github.Item{a}, []github.Item{b}, []github.Item{a, c})
	snap := h.refresh(t)

	assert.Equal(t, []int{1}, numbers(snap.ReviewRequested))
	assert.Equal(t, []int{2}, numbers(snap.Authored))
	assert.Equal(t, []int{3, 1}, numbers(snap.Assigned), "newest activity first")
	assert.Equal(t, [3]bool{true, false, true}, h.reasons(t, 1))
	assert.Equal(t, [3]bool{false, true, false}, h.reasons(t, 2))
	assert.Equal(t, [3]bool{false, false, true}, h.reasons(t, 3))
	assert.Equal(t, int64(2), h.count(t, "repos"))

	// Local state keeps B's row alive after it drops out of every list.
	h.exec(t, "INSERT INTO item_local_state (item_id, updated_at) VALUES (?, ?)", h.storedItemNumbered(t, 2).ID,
		h.clock.Now())

	// Next refresh: A is only review-requested (retitled), B is gone, C is still assigned.
	a.Title = "Item 1, retitled"

	h.clock.Advance(DefaultInterval)
	h.client.inbox = fetched([]github.Item{a}, nil, []github.Item{c})
	snap = h.refresh(t)

	assert.Equal(t, []int{1}, numbers(snap.ReviewRequested))
	assert.Empty(t, snap.Authored.Items)
	assert.NotNil(t, snap.Authored.Items, "an empty list is [] for the UI, not null")
	assert.Equal(t, []int{3}, numbers(snap.Assigned))
	assert.Equal(t, "Item 1, retitled", snap.ReviewRequested.Items[0].Title)

	assert.Equal(t, [3]bool{true, false, false}, h.reasons(t, 1))
	assert.Equal(t, [3]bool{false, false, false}, h.reasons(t, 2))
	assert.Equal(t, [3]bool{false, false, true}, h.reasons(t, 3))
	assert.Equal(t, int64(3), h.count(t, "items"), "items that drop out keep their row")
	assert.Equal(t, int64(1), h.count(t, "item_local_state"))
}

func TestRefreshStoresTrustFields(t *testing.T) {
	h := newHarness(t)

	fork := pr(12, "CONTRIBUTOR", true)
	bot := pr(15, "NONE", false)
	bot.AuthorLogin, bot.AuthorType = "dependabot", "Bot"
	impostor := pr(16, "NONE", false)
	impostor.AuthorLogin, impostor.AuthorType = "dependabot", "User"
	unknownBot := pr(17, "NONE", false)
	unknownBot.AuthorLogin, unknownBot.AuthorType = "random-helper", "Bot"
	own := pr(7, "OWNER", false)
	ownFork := pr(8, "MEMBER", true)
	memberIssue := issue("other-repo", 21, "MEMBER")

	h.client.inbox = fetched([]github.Item{fork, bot, ownFork, impostor, unknownBot}, []github.Item{own},
		[]github.Item{memberIssue})
	snap := h.refresh(t)

	stored := h.storedItems(t)
	require.Len(t, stored, 7)

	got := make(map[int]storedItem)
	for _, it := range stored {
		got[it.Number] = it
	}

	assert.Equal(t, "CONTRIBUTOR", got[12].AuthorAssociation)
	assert.True(t, got[12].IsCrossRepository)
	assert.Equal(t, "NONE", got[15].AuthorAssociation)
	assert.Equal(t, "dependabot", got[15].AuthorLogin)
	assert.Equal(t, "Bot", got[15].AuthorType)
	assert.Equal(t, "User", got[16].AuthorType)
	assert.Equal(t, "OWNER", got[7].AuthorAssociation)
	assert.False(t, got[7].IsCrossRepository)
	assert.Equal(t, item.KindIssue, got[21].Kind)
	assert.Equal(t, "branch-7", got[7].HeadRef)

	classes := make(map[int]trust.Class)
	trusted := make(map[int]bool)
	bots := make(map[int]bool)

	for _, l := range []List{snap.ReviewRequested, snap.Authored, snap.Assigned} {
		for _, it := range l.Items {
			classes[it.Number], trusted[it.Number], bots[it.Number] = it.Trust, it.Trusted, it.AuthorIsBot
		}
	}

	assert.Equal(t, map[int]trust.Class{
		12: trust.ClassOutside, 15: trust.ClassBot, 16: trust.ClassOutside, 17: trust.ClassOutside,
		8: trust.ClassOutside, 7: trust.ClassMember, 21: trust.ClassMember,
	}, classes, "Dependabot is a trusted bot by default; a person named like it and an unlisted bot are not")
	assert.Equal(t, map[int]bool{12: false, 15: true, 16: false, 17: false, 8: false, 7: true, 21: true}, trusted)
	assert.Equal(t, map[int]bool{12: false, 15: true, 16: false, 17: true, 8: false, 7: false, 21: false}, bots)

	assert.Equal(t, []int{17, 16, 15, 12, 8}, numbers(snap.ReviewRequested), "newest activity first")

	fromFork := snap.ReviewRequested.Items[3]
	assert.Equal(t, Item{
		ID: fromFork.ID, Kind: item.KindPR, Number: 12, Title: "Item 12",
		URL:  "https://github.com/acme-org/example-repo/pull/12",
		Repo: "acme-org/example-repo", Author: octoUser, UpdatedAt: "2026-01-01T12:00:00Z",
		Trust: trust.ClassOutside, Trusted: false, IsCrossRepository: true, AuthorAssociation: "CONTRIBUTOR",
		Branch: "branch-12",
	}, fromFork)

	// The trusted list is the user's: with it emptied, Dependabot is an outside contributor.
	h.setPrefs(t, map[string]string{trust.PrefBots: "[]"})

	snap, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)

	for _, it := range snap.ReviewRequested.Items {
		if it.Number == 15 {
			assert.Equal(t, trust.ClassOutside, it.Trust)
			assert.True(t, it.AuthorIsBot)
		}
	}
}

func TestRefreshWritesRateSnapshot(t *testing.T) {
	h := newHarness(t)
	h.client.inbox = fetched(nil, nil, nil)
	h.refresh(t)

	// Readings older than the retention window are pruned on the next refresh.
	old := h.insertRate(t, 1, h.clock.Now().Add(-8*24*time.Hour))
	recent := h.insertRate(t, 2, h.clock.Now().Add(-6*24*time.Hour))

	h.clock.Advance(DefaultInterval)
	snap := h.refresh(t)

	rows, err := h.pool.QueryContext(t.Context(),
		"SELECT resource, limit_total, remaining, cost FROM rate_snapshots WHERE taken_at = ? ORDER BY resource",
		h.clock.Now())
	require.NoError(t, err)

	defer func() { _ = rows.Close() }()

	type reading struct {
		resource                    string
		limitTotal, remaining, cost int
	}

	var readings []reading

	for rows.Next() {
		var r reading
		require.NoError(t, rows.Scan(&r.resource, &r.limitTotal, &r.remaining, &r.cost))
		readings = append(readings, r)
	}

	require.NoError(t, rows.Err())
	require.Len(t, readings, 3, "one row per resource per refresh")

	assert.Equal(t, resourceCore, readings[0].resource)
	assert.Equal(t, 4980, readings[0].remaining)
	assert.Equal(t, resourceGraphQL, readings[1].resource)
	assert.Equal(t, 1, readings[1].cost, "the GraphQL row carries the query cost")
	assert.Equal(t, 4990, readings[1].remaining)
	assert.Equal(t, resourceSearch, readings[2].resource)
	assert.Equal(t, 30, readings[2].limitTotal)

	var kept []int64

	keptRows, err := h.pool.QueryContext(t.Context(), "SELECT id FROM rate_snapshots WHERE id IN (?, ?)", old, recent)
	require.NoError(t, err)

	defer func() { _ = keptRows.Close() }()

	for keptRows.Next() {
		var id int64
		require.NoError(t, keptRows.Scan(&id))
		kept = append(kept, id)
	}

	require.NoError(t, keptRows.Err())
	assert.Equal(t, []int64{recent}, kept)

	assert.Equal(t, Rate{
		Known: true, Limit: 5000, Remaining: 4990, Used: 10, Cost: 1,
		ResetAt: "2026-01-10T13:00:00Z", TakenAt: "2026-01-10T12:05:00Z",
	}, snap.Rate.GraphQL)
	assert.Equal(t, 4980, snap.Rate.Core.Remaining)
	assert.Equal(t, 28, snap.Rate.Search.Remaining)
	assert.Equal(t, "2026-01-10T12:01:00Z", snap.Rate.Search.ResetAt)
}

func TestRefreshMissingScopesError(t *testing.T) {
	h := newHarness(t)
	h.client.inbox = fetched([]github.Item{pr(1, "OWNER", false)}, nil, nil)
	h.client.inbox.OAuthScopes = "gist"

	snap, err := h.svc.Refresh(t.Context())
	require.ErrorIs(t, err, github.ErrMissingScopes)
	assert.Contains(t, snap.LastError, "gh auth refresh -s repo,read:org")
	assert.Equal(t, int64(0), h.count(t, "items"), "nothing is stored without the needed scopes")
	assert.Equal(t, []event.Event{event.InboxUpdated{}}, h.events.All(), "the UI hears about the failure")

	// A restart still shows the error.
	snap, err = h.service().Snapshot(t.Context())
	require.NoError(t, err)
	assert.Contains(t, snap.LastError, "read:org")

	// The next good refresh clears it.
	h.client.inbox.OAuthScopes = "repo, read:org"
	snap = h.refresh(t)
	assert.Empty(t, snap.LastError)
	assert.Equal(t, []int{1}, numbers(snap.ReviewRequested))
}

func TestRefreshScopeWarning(t *testing.T) {
	h := newHarness(t)
	h.client.inbox = fetched(nil, nil, nil)
	h.client.inbox.OAuthScopes = ""

	snap := h.refresh(t)
	assert.Equal(t, string(github.ScopesUnverifiable), snap.ScopeWarning)
	assert.Empty(t, snap.LastError)
}

func TestSnapshotReportsNofM(t *testing.T) {
	h := newHarness(t)

	review := []github.Item{pr(1, "OWNER", false), pr(2, "OWNER", false)}
	h.client.inbox = fetched(review, []github.Item{pr(3, "OWNER", false)}, nil)
	h.client.inbox.ReviewRequested.IssueCount = 7
	h.client.inbox.Authored.IssueCount = 0 // a total below what was shown falls back to it

	snap := h.refresh(t)
	assert.Equal(t, 2, snap.ReviewRequested.Shown)
	assert.Equal(t, 7, snap.ReviewRequested.Total)
	assert.Equal(t, 1, snap.Authored.Shown)
	assert.Equal(t, 1, snap.Authored.Total)
	assert.Equal(t, 0, snap.Assigned.Total)

	// The totals and refresh status survive a restart.
	again, err := h.service().Snapshot(t.Context())
	require.NoError(t, err)
	assert.Equal(t, snap, again)
	assert.Equal(t, "2026-01-10T12:00:00Z", again.LastRefreshedAt)
	assert.Equal(t, octoUser, again.Account.Login)
}

func TestRefreshNeverPersistsToken(t *testing.T) {
	h := newHarness(t)
	h.client.inbox = fetched([]github.Item{pr(1, "OWNER", false)}, nil, []github.Item{issue("other-repo", 2, "MEMBER")})
	h.refresh(t)

	assert.Equal(t, []string{fakeToken, fakeToken, fakeToken}, h.client.gotTokens, "the token reaches the client")

	// A failure whose message echoes the token is recorded without it.
	h.client.inboxErr = fmt.Errorf("%w: upstream said: %s", errFakeNetwork, fakeToken)

	_, err := h.svc.Refresh(t.Context())
	require.Error(t, err)
	assert.NotContains(t, err.Error(), fakeToken)

	tables := h.column(t, "SELECT name FROM sqlite_master WHERE type = 'table'")
	require.NotEmpty(t, tables)

	for _, table := range tables {
		for _, row := range h.rows(t, "SELECT * FROM "+table) {
			for col, v := range row {
				assert.NotContains(t, fmt.Sprint(v), fakeToken, "%s.%s", table, col)
			}
		}
	}

	// Nothing on disk either: the database file and its write-ahead log.
	for _, path := range []string{h.path, h.path + "-wal"} {
		data, readErr := os.ReadFile(path) //nolint:gosec // the test's own temp database
		if errors.Is(readErr, os.ErrNotExist) {
			continue
		}

		require.NoError(t, readErr)
		assert.NotContains(t, string(data), fakeToken, path)
	}
}

func TestRefreshRecordsTokenErrors(t *testing.T) {
	h := newHarness(t)
	h.tokens.err = github.ErrGHNotFound

	snap, err := h.svc.Refresh(t.Context())
	require.ErrorIs(t, err, github.ErrGHNotFound)
	assert.Equal(t, github.ErrGHNotFound.Error(), snap.LastError)
	assert.Empty(t, h.client.gotTokens, "nothing is fetched without a token")
	assert.Empty(t, snap.ReviewRequested.Items)
	assert.Empty(t, snap.LastRefreshedAt)
}

func TestRefreshFetchErrorsKeepTheStoredInbox(t *testing.T) {
	h := newHarness(t)
	h.client.inbox = fetched([]github.Item{pr(1, "OWNER", false)}, nil, nil)
	h.refresh(t)

	h.client.limitsErr = errFakeNetwork
	h.clock.Advance(DefaultInterval)

	snap, err := h.svc.Refresh(t.Context())
	require.ErrorIs(t, err, errFakeNetwork)
	assert.Equal(t, errFakeNetwork.Error(), snap.LastError)
	assert.Equal(t, []int{1}, numbers(snap.ReviewRequested), "the last good inbox is still shown")
	assert.Equal(t, "2026-01-10T12:00:00Z", snap.LastRefreshedAt)
}

func TestRefreshRequiresViewer(t *testing.T) {
	h := newHarness(t)
	h.client.inbox = fetched(nil, nil, nil)
	h.client.inbox.Viewer.Login = ""

	_, err := h.svc.Refresh(t.Context())
	require.ErrorIs(t, err, ErrIncompleteFetch)
}

func TestRefreshCanceledIsNotRecorded(t *testing.T) {
	h := newHarness(t)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := h.svc.Refresh(ctx)
	require.ErrorIs(t, err, context.Canceled)
	assert.Zero(t, h.events.Count())

	snap, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)
	assert.Empty(t, snap.LastError, "shutting down is not a refresh failure")
}

func TestRefreshNotifies(t *testing.T) {
	h := newHarness(t)
	h.client.inbox = fetched(nil, nil, nil)
	h.refresh(t)
	assert.Equal(t, 1, h.events.Count())
	assert.Equal(t, 1, h.tokens.calls, "the token is borrowed on every refresh")

	h.refresh(t)
	assert.Equal(t, 2, h.events.Count())
	assert.Equal(t, 2, h.tokens.calls)
}

func TestSnapshotBeforeFirstRefresh(t *testing.T) {
	h := newHarness(t)

	snap, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)
	assert.Equal(t, Snapshot{
		ReviewRequested: List{Items: []Item{}},
		Authored:        List{Items: []Item{}},
		Assigned:        List{Items: []Item{}},
	}, snap)
}

func TestSnapshotSwitchesWithTheViewer(t *testing.T) {
	h := newHarness(t)
	h.client.inbox = fetched([]github.Item{pr(1, "OWNER", false)}, nil, nil)
	h.refresh(t)

	// Signing in as another account shows only that account's items.
	h.client.inbox = fetched(nil, []github.Item{pr(2, "MEMBER", false)}, nil)
	h.client.inbox.Viewer = github.Viewer{Login: "octo-other", URL: "https://github.com/octo-other", AvatarURL: octoOtherAvatar}
	snap := h.refresh(t)

	assert.Equal(t, "octo-other", snap.Account.Login)
	assert.Empty(t, snap.ReviewRequested.Items)
	assert.Equal(t, []int{2}, numbers(snap.Authored))
	assert.Equal(t, int64(2), h.count(t, "accounts"))
}

func TestRefreshStoresTheAccount(t *testing.T) {
	h := newHarness(t)
	h.client.inbox = fetched(nil, nil, nil)

	snap := h.refresh(t)

	assert.Equal(t, Account{
		Login:      octoUser,
		Name:       "Octo User",
		ProfileURL: "https://github.com/octo-user",
		Avatar:     "data:image/png;base64,bm90LXJlYWxseS1hLXBuZw==",
	}, snap.Account)
	assert.Equal(t, []string{octoUserAvatar}, h.client.avatarURLs)

	// The account survives a restart.
	again, err := h.service().Snapshot(t.Context())
	require.NoError(t, err)
	assert.Equal(t, snap.Account, again.Account)
}

func TestRefreshDownloadsTheAvatarOnlyWhenItChanges(t *testing.T) {
	h := newHarness(t)
	h.client.inbox = fetched(nil, nil, nil)

	h.refresh(t)
	h.refresh(t)
	assert.Equal(t, []string{octoUserAvatar}, h.client.avatarURLs, "a cached avatar is not downloaded again")

	// A new picture has a new URL, and is downloaded once.
	changed := "https://avatars.githubusercontent.com/u/101?v=5&s=128"
	h.client.inbox.Viewer.AvatarURL = changed
	h.client.avatar = &github.Avatar{ContentType: "image/jpeg", Data: []byte("new")}

	snap := h.refresh(t)
	h.refresh(t)

	assert.Equal(t, []string{octoUserAvatar, changed}, h.client.avatarURLs)
	assert.Equal(t, "data:image/jpeg;base64,bmV3", snap.Account.Avatar)
}

func TestRefreshAvatarFailureIsBestEffort(t *testing.T) {
	h := newHarness(t)
	h.client.inbox = fetched([]github.Item{pr(1, "OWNER", false)}, nil, nil)
	h.client.avatarErr = errFakeNetwork

	snap := h.refresh(t)
	assert.Empty(t, snap.LastError, "a missing picture is not a refresh failure")
	assert.Equal(t, []int{1}, numbers(snap.ReviewRequested))
	assert.Equal(t, octoUser, snap.Account.Login)
	assert.Empty(t, snap.Account.Avatar)

	// The next refresh tries again.
	h.client.avatarErr = nil
	snap = h.refresh(t)

	assert.Len(t, h.client.avatarURLs, 2)
	assert.NotEmpty(t, snap.Account.Avatar)
}

func TestRefreshWithoutAnAvatarURL(t *testing.T) {
	h := newHarness(t)
	h.client.inbox = fetched(nil, nil, nil)
	h.client.inbox.Viewer.AvatarURL = ""

	snap := h.refresh(t)

	assert.Empty(t, h.client.avatarURLs)
	assert.Empty(t, snap.Account.Avatar)
}

func TestSnapshotNeverShowsAnotherAccountsAvatar(t *testing.T) {
	h := newHarness(t)
	h.client.inbox = fetched(nil, nil, nil)
	require.NotEmpty(t, h.refresh(t).Account.Avatar)

	// The new account's picture cannot be downloaded, so it shows none rather than the old one.
	h.client.inbox.Viewer = github.Viewer{Login: "octo-other", AvatarURL: octoOtherAvatar}
	h.client.avatarErr = errFakeNetwork

	snap := h.refresh(t)

	assert.Equal(t, "octo-other", snap.Account.Login)
	assert.Empty(t, snap.Account.Avatar)
}

func TestSnapshotFallsBackToTheGitHubProfileURL(t *testing.T) {
	h := newHarness(t)
	h.client.inbox = fetched(nil, nil, nil)
	h.client.inbox.Viewer.URL = ""

	snap := h.refresh(t)

	assert.Equal(t, "https://github.com/octo-user", snap.Account.ProfileURL)
}

func TestSnapshotDatabaseErrors(t *testing.T) {
	h := newHarness(t)
	h.client.inbox = fetched(nil, nil, nil)
	h.refresh(t)
	require.NoError(t, h.pool.Close())

	_, err := h.svc.Snapshot(t.Context())
	require.Error(t, err)

	_, err = h.svc.Refresh(t.Context())
	require.Error(t, err)
}

func TestNewServiceUsesTheSystemClock(t *testing.T) {
	h := newHarness(t)
	svc := NewService(Config{Pool: h.pool, Tokens: h.tokens, Client: h.client})
	h.client.inbox = fetched(nil, nil, nil)

	before := time.Now().UTC().Truncate(time.Second)

	snap, err := svc.Refresh(t.Context())
	require.NoError(t, err)

	refreshed, err := time.Parse(time.RFC3339, snap.LastRefreshedAt)
	require.NoError(t, err)
	assert.False(t, refreshed.Before(before))
}

// prDetails returns the details of a pull request with passing checks and one approval.
func prDetails() github.ItemDetails {
	return github.ItemDetails{
		Labels:       []github.Label{{Name: "enhancement", Color: "#a2eeef"}},
		Comments:     4,
		Additions:    120,
		Deletions:    30,
		ChangedFiles: 6,
		Commits:      3,
		Mergeable:    "MERGEABLE",
		Checks:       github.Checks{State: "SUCCESS", Passed: 12, Skipped: 3},
		Reviews:      github.Reviews{Decision: "APPROVED", Reviewers: 2, Reviewed: 1, Approvals: 1},
	}
}

// itemByNumber finds an item in a list.
func itemByNumber(t *testing.T, l List, number int) Item {
	t.Helper()

	for _, it := range l.Items {
		if it.Number == number {
			return it
		}
	}

	require.Failf(t, "missing item", "#%d", number)

	return Item{}
}

func TestRefreshStoresEachItemsDetails(t *testing.T) {
	h := newHarness(t)

	a := pr(1, "OWNER", false)
	b := pr(2, "OWNER", false)
	c := issue("docs-repo", 3, "COLLABORATOR")

	h.client.inbox = fetched([]github.Item{a}, []github.Item{b}, []github.Item{a, c})
	h.client.details = map[string]github.ItemDetails{
		nodeID(1): prDetails(),
		nodeID(3): {Labels: []github.Label{{Name: "bug", Color: "#d73a4a"}}, Comments: 7},
	}

	snap := h.refresh(t)

	assert.Equal(t, [][]string{{nodeID(1), nodeID(2), nodeID(3)}}, h.client.detailsIDs, "each item is asked for once")

	assert.Equal(t, &Details{
		Labels:       []Label{{Name: "enhancement", Color: "#a2eeef"}},
		Comments:     4,
		Additions:    120,
		Deletions:    30,
		ChangedFiles: 6,
		Commits:      3,
		Mergeable:    "MERGEABLE",
		Checks:       Checks{State: "SUCCESS", Passed: 12, Skipped: 3},
		Reviews:      Reviews{Decision: "APPROVED", Reviewers: 2, Reviewed: 1, Approvals: 1},
		FetchedAt:    "2026-01-10T12:00:00Z",
	}, itemByNumber(t, snap.ReviewRequested, 1).Details)
	assert.Equal(t, itemByNumber(t, snap.ReviewRequested, 1).Details, itemByNumber(t, snap.Assigned, 1).Details)

	assert.Equal(t, &Details{
		Labels:    []Label{{Name: "bug", Color: "#d73a4a"}},
		Comments:  7,
		FetchedAt: "2026-01-10T12:00:00Z",
	}, itemByNumber(t, snap.Assigned, 3).Details)

	assert.Nil(t, itemByNumber(t, snap.Authored, 2).Details, "GitHub sent nothing for it")
}

func TestRefreshReplacesTheDetails(t *testing.T) {
	h := newHarness(t)
	h.client.inbox = fetched([]github.Item{pr(1, "OWNER", false), pr(2, "OWNER", false)}, nil, nil)
	h.client.details = map[string]github.ItemDetails{nodeID(1): prDetails(), nodeID(2): prDetails()}
	h.refresh(t)

	// Checks finished failing on #1; GitHub sends nothing for #2 this time.
	failing := prDetails()
	failing.Labels = nil
	failing.Checks = github.Checks{State: "FAILURE", Passed: 10, Failed: 2}
	h.client.details = map[string]github.ItemDetails{nodeID(1): failing}
	h.clock.Advance(DefaultInterval)

	snap := h.refresh(t)

	first := itemByNumber(t, snap.ReviewRequested, 1).Details
	require.NotNil(t, first)
	assert.Equal(t, Checks{State: "FAILURE", Passed: 10, Failed: 2}, first.Checks)
	assert.Empty(t, first.Labels)
	assert.NotNil(t, first.Labels, "no labels is an empty list, not null")
	assert.Equal(t, "2026-01-10T12:05:00Z", first.FetchedAt)

	second := itemByNumber(t, snap.ReviewRequested, 2).Details
	require.NotNil(t, second, "details GitHub did not send again are kept")
	assert.Equal(t, "2026-01-10T12:00:00Z", second.FetchedAt)
	assert.Equal(t, int64(2), h.count(t, "item_details"))
}

func TestRefreshDetailsFailureKeepsTheStoredInbox(t *testing.T) {
	h := newHarness(t)
	h.client.inbox = fetched([]github.Item{pr(1, "OWNER", false)}, nil, nil)
	h.client.details = map[string]github.ItemDetails{nodeID(1): prDetails()}
	h.refresh(t)

	h.client.inbox = fetched([]github.Item{pr(1, "OWNER", false), pr(2, "OWNER", false)}, nil, nil)
	h.client.detailsErr = fmt.Errorf("%w: upstream said: %s", errFakeNetwork, fakeToken)
	h.clock.Advance(DefaultInterval)

	snap, err := h.svc.Refresh(t.Context())
	require.ErrorIs(t, err, errFakeNetwork)
	assert.Contains(t, err.Error(), "read the item details")
	assert.NotContains(t, err.Error(), fakeToken)
	assert.Contains(t, snap.LastError, "read the item details")
	assert.NotContains(t, snap.LastError, fakeToken)

	assert.Equal(t, []int{1}, numbers(snap.ReviewRequested), "the last good inbox is still shown")
	assert.NotNil(t, itemByNumber(t, snap.ReviewRequested, 1).Details)
	assert.Equal(t, "2026-01-10T12:00:00Z", snap.LastRefreshedAt)
}

func TestRefreshRecordsTheCostOfBothQueries(t *testing.T) {
	h := newHarness(t)
	h.client.inbox = fetched([]github.Item{pr(1, "OWNER", false)}, nil, nil)
	h.client.detailsRate = github.QueryRate{
		Cost: 1, Limit: 5000, Remaining: 4988, Used: 12, ResetAt: time.Date(2026, 1, 10, 13, 0, 0, 0, time.UTC),
	}

	snap := h.refresh(t)

	assert.Equal(t, 2, snap.Rate.GraphQL.Cost, "the inbox query and the details query")
	assert.Equal(t, 4988, snap.Rate.GraphQL.Remaining, "what is left after the details query")
}

func TestGraphQLReading(t *testing.T) {
	inbox := github.QueryRate{Cost: 1, Limit: 5000, Remaining: 4990}
	later := github.QueryRate{Cost: 2, Limit: 5000, Remaining: 4988}

	tests := []struct {
		name    string
		details *github.Details
		want    github.QueryRate
	}{
		{name: "no details query", details: nil, want: inbox},
		{name: "no items to ask about", details: &github.Details{}, want: inbox},
		{name: "no budget sent", details: &github.Details{Requests: 1}, want: inbox},
		{
			name:    "details query",
			details: &github.Details{Requests: 2, RateLimit: later},
			want:    github.QueryRate{Cost: 3, Limit: 5000, Remaining: 4988},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, graphQLReading(inbox, tt.details))
		})
	}
}

func TestSaveWithoutDetails(t *testing.T) {
	h := newHarness(t)
	h.client.inbox = fetched([]github.Item{pr(1, "OWNER", false)}, nil, nil)

	_, err := h.svc.save(t.Context(), h.svc.startRefresh(), refreshed{inbox: h.client.inbox, limits: restLimits()})
	require.NoError(t, err)

	snap, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)
	assert.Nil(t, itemByNumber(t, snap.ReviewRequested, 1).Details)
}

func TestRefreshReadsTheListedDetailsAlongsideTheInbox(t *testing.T) {
	h := newHarness(t)
	h.client.inbox = fetched([]github.Item{pr(1, "OWNER", false)}, nil, nil)
	h.refresh(t)

	// The inbox query now answers only once a details request has started.
	h.client.inboxGate = make(chan struct{})
	h.client.details = map[string]github.ItemDetails{nodeID(1): prDetails()}

	snap := h.refresh(t)

	assert.NotNil(t, itemByNumber(t, snap.ReviewRequested, 1).Details)
}

func TestRefreshAsksAfterTheInboxOnlyAboutNewItems(t *testing.T) {
	h := newHarness(t)
	a := pr(1, "OWNER", false)
	b := pr(2, "OWNER", false)
	c := pr(3, "OWNER", false)

	// The first refresh has nothing listed yet, so it asks about everything afterwards.
	h.client.inbox = fetched([]github.Item{a, b}, nil, nil)
	h.refresh(t)

	// Then A leaves the lists and C joins: the listed A and B are read alongside the
	// inbox query (newest first), and only C afterwards.
	h.client.inbox = fetched([]github.Item{b, c}, nil, nil)
	h.refresh(t)

	// Nothing new: one request, alongside the inbox query.
	h.refresh(t)

	assert.Equal(t, [][]string{
		{nodeID(1), nodeID(2)},
		{nodeID(2), nodeID(1)},
		{nodeID(3)},
		{nodeID(3), nodeID(2)},
	}, h.client.detailsIDs)
}

func TestRefreshNewItemDetailsFailure(t *testing.T) {
	h := newHarness(t)
	h.client.inbox = fetched([]github.Item{pr(1, "OWNER", false)}, nil, nil)
	h.client.detailsErr = errFakeNetwork

	snap, err := h.svc.Refresh(t.Context())
	require.ErrorIs(t, err, errFakeNetwork)
	assert.Contains(t, snap.LastError, "read the item details")
	assert.Empty(t, snap.ReviewRequested.Items, "nothing is stored")
	assert.Empty(t, snap.LastRefreshedAt)
}

func TestListedNodeIDsError(t *testing.T) {
	h := newHarness(t)
	h.exec(t, "DROP TABLE items")

	_, err := h.svc.listedNodeIDs(t.Context())
	require.ErrorContains(t, err, "read the listed items")

	_, err = h.svc.Refresh(t.Context())
	require.ErrorContains(t, err, "read the listed items")
}

func TestNewIDs(t *testing.T) {
	assert.Equal(t, []string{"c"}, newIDs([]string{"a", "c", "b"}, []string{"b", "a"}))
	assert.Empty(t, newIDs([]string{"a"}, []string{"a"}))
	assert.Equal(t, []string{"a"}, newIDs([]string{"a"}, nil))
}

// storedRepo is a stored repository's row, as the tests read it.
type storedRepo struct {
	ID                 int64
	NodeID             string
	Description        string
	Visibility         string
	IsArchived         bool
	ArchivedAt         *time.Time
	IsFork             bool
	Parent             string
	DefaultBranch      string
	ViewerPermission   string
	ViewerMergeMethod  string
	MergeCommitAllowed bool
	SquashMergeAllowed bool
	AutoMergeAllowed   bool
	AllowUpdateBranch  bool
	Language           string
	LanguageColor      string
	PushedAt           *time.Time
	FetchedAt          *time.Time
	Pinned             bool
	Muted              bool
	LocalPath          string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// repoRow reads a stored repository by name.
func (h *harness) repoRow(t *testing.T, name string) storedRepo {
	t.Helper()

	var r storedRepo

	require.NoError(t, h.pool.QueryRowContext(t.Context(), `SELECT id, node_id, description, visibility,
    is_archived, archived_at, is_fork, parent, default_branch, viewer_permission, viewer_merge_method,
    merge_commit_allowed, squash_merge_allowed, auto_merge_allowed, allow_update_branch, language, language_color,
    pushed_at, fetched_at, pinned, muted, local_path, created_at, updated_at
FROM repos WHERE name = ?`, name).Scan(&r.ID, &r.NodeID, &r.Description, &r.Visibility, &r.IsArchived,
		&r.ArchivedAt, &r.IsFork, &r.Parent, &r.DefaultBranch, &r.ViewerPermission, &r.ViewerMergeMethod,
		&r.MergeCommitAllowed, &r.SquashMergeAllowed, &r.AutoMergeAllowed, &r.AllowUpdateBranch, &r.Language,
		&r.LanguageColor, &r.PushedAt, &r.FetchedAt, &r.Pinned, &r.Muted, &r.LocalPath, &r.CreatedAt, &r.UpdatedAt))

	return r
}

func TestRefreshStoresWhatGitHubSaysAboutEachRepository(t *testing.T) {
	h := newHarness(t)

	a := pr(1, "OWNER", false)
	a.Repo = github.Repository{
		NodeID: "R_fakeRepoExample", Description: "An example repository", Visibility: "PRIVATE",
		IsFork: true, Parent: "acme-org/upstream-repo", DefaultBranch: "main", ViewerPermission: "WRITE",
		ViewerMergeMethod: "SQUASH", SquashMergeAllowed: true, AutoMergeAllowed: true, AllowUpdateBranch: true,
		Language: "Go", LanguageColor: "#00add8", PushedAt: time.Date(2026, 1, 9, 8, 0, 0, 0, time.UTC),
	}
	h.client.inbox = fetched([]github.Item{a}, nil, nil)
	h.refresh(t)

	repo := h.repoRow(t, "example-repo")
	assert.Equal(t, "R_fakeRepoExample", repo.NodeID)
	assert.Equal(t, "An example repository", repo.Description)
	assert.Equal(t, "PRIVATE", repo.Visibility)
	assert.True(t, repo.IsFork)
	assert.Equal(t, "acme-org/upstream-repo", repo.Parent)
	assert.Equal(t, "main", repo.DefaultBranch)
	assert.Equal(t, "WRITE", repo.ViewerPermission)
	assert.Equal(t, "SQUASH", repo.ViewerMergeMethod)
	assert.True(t, repo.SquashMergeAllowed)
	assert.False(t, repo.MergeCommitAllowed)
	assert.True(t, repo.AutoMergeAllowed)
	assert.True(t, repo.AllowUpdateBranch)
	assert.Equal(t, "Go", repo.Language)
	assert.Equal(t, "#00add8", repo.LanguageColor)
	require.NotNil(t, repo.PushedAt)
	assert.True(t, a.Repo.PushedAt.Equal(*repo.PushedAt))
	assert.Nil(t, repo.ArchivedAt)
	require.NotNil(t, repo.FetchedAt)
	assert.True(t, h.clock.Now().Equal(*repo.FetchedAt))

	// The local columns are the user's, and a refresh leaves them alone while it rewrites
	// GitHub's.
	h.exec(t, "UPDATE repos SET pinned = 1, muted = 1, local_path = ? WHERE id = ?", "~/projects/example-repo",
		repo.ID)

	a.Repo.Description = "Renamed description"
	a.Repo.PushedAt = time.Time{}
	h.client.inbox = fetched([]github.Item{a}, nil, nil)
	h.refresh(t)

	repo = h.repoRow(t, "example-repo")
	assert.Equal(t, "Renamed description", repo.Description)
	assert.Nil(t, repo.PushedAt, "an unknown push time is stored as unknown")
	assert.True(t, repo.Pinned)
	assert.True(t, repo.Muted)
	assert.Equal(t, "~/projects/example-repo", repo.LocalPath)
}

func TestRefreshLeavesArchivedRepositoriesOut(t *testing.T) {
	h := newHarness(t)

	live := pr(1, "OWNER", false)
	old := issue("old-repo", 2, "MEMBER")

	h.client.inbox = fetched([]github.Item{live}, nil, []github.Item{old})
	snap := h.refresh(t)
	assert.Equal(t, []int{2}, numbers(snap.Assigned))

	// old-repo is archived; GitHub's index has not caught up, so the search still returns
	// its issue, and in two lists.
	archivedAt := time.Date(2026, 1, 10, 11, 0, 0, 0, time.UTC)
	old.Repo = github.Repository{IsArchived: true, ArchivedAt: archivedAt}
	h.client.inbox = fetched([]github.Item{live, old}, nil, []github.Item{old})
	h.client.detailsIDs = nil

	snap = h.refresh(t)

	assert.Equal(t, []int{1}, numbers(snap.ReviewRequested))
	assert.Empty(t, snap.Assigned.Items, "the archived repository's issue is no longer listed")
	assert.Equal(t, [][]string{{nodeID(2), nodeID(1)}}, h.client.detailsIDs,
		"it was listed before, so it was asked about alongside the inbox (newest first), and nothing new after")
	assert.Equal(t, [3]bool{false, false, false}, h.reasons(t, 2))

	repo := h.repoRow(t, "old-repo")
	assert.True(t, repo.IsArchived)
	require.NotNil(t, repo.ArchivedAt)
	assert.True(t, archivedAt.Equal(*repo.ArchivedAt))

	// The next refresh no longer asks about it at all.
	h.client.detailsIDs = nil
	h.refresh(t)
	assert.Equal(t, [][]string{{nodeID(1)}}, h.client.detailsIDs)
}

func TestDropArchived(t *testing.T) {
	keep := pr(1, "OWNER", false)
	gone := pr(2, "OWNER", false)
	gone.Repo.IsArchived = true

	inbox := fetched([]github.Item{keep, gone}, []github.Item{gone}, nil)
	inbox.ReviewRequested.IssueCount = 2

	dropped := dropArchived(inbox)

	assert.Equal(t, []github.Item{gone, gone}, dropped)
	assert.Equal(t, []github.Item{keep}, inbox.ReviewRequested.Items)
	assert.Empty(t, inbox.Authored.Items)
	assert.Empty(t, inbox.Assigned.Items)
	assert.Equal(t, 2, inbox.ReviewRequested.IssueCount, "totals stay as GitHub counted them")
}
