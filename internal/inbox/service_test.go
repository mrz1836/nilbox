package inbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox/model"
	"github.com/mrz1836/nilbox/internal/store"
)

// fakeToken is a placeholder that matches no real token format.
const fakeToken = "test-token-not-real"

var (
	errFakeNetwork = errors.New("network unreachable")
	errFakeBroken  = errors.New("refresher broke")
)

// fakeTokens hands out a fixed token, or fails.
type fakeTokens struct {
	err   error
	calls int
}

func (f *fakeTokens) Token(ctx context.Context) (github.Token, error) {
	f.calls++

	if err := ctx.Err(); err != nil {
		return github.Token{}, err
	}

	if f.err != nil {
		return github.Token{}, f.err
	}

	return github.NewToken(fakeToken), nil
}

// fakeClient returns canned results. Both fetches run concurrently.
type fakeClient struct {
	mu         sync.Mutex
	inbox      *github.Inbox
	limits     *github.RateLimits
	avatar     *github.Avatar
	inboxErr   error
	limitsErr  error
	avatarErr  error
	gotTokens  []string
	avatarURLs []string
}

func (c *fakeClient) FetchInbox(_ context.Context, tok github.Token) (*github.Inbox, error) {
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

func (c *fakeClient) FetchAvatar(_ context.Context, url string) (*github.Avatar, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.avatarURLs = append(c.avatarURLs, url)

	return c.avatar, c.avatarErr
}

// fakeClock returns a settable time.
type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time { return c.now }

// fakeNotifier counts notifications.
type fakeNotifier struct {
	calls int
}

func (n *fakeNotifier) InboxUpdated() { n.calls++ }

// harness is a Service over a temp database with fakes for everything else.
type harness struct {
	svc      *Service
	db       *gorm.DB
	path     string
	tokens   *fakeTokens
	client   *fakeClient
	clock    *fakeClock
	notifier *fakeNotifier
}

// newHarness opens a fresh database and a Service over it.
func newHarness(t *testing.T) *harness {
	t.Helper()

	path := filepath.Join(t.TempDir(), "app.db")

	db, err := store.Open(t.Context(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close(db) })

	h := &harness{
		db:       db,
		path:     path,
		tokens:   &fakeTokens{},
		client:   &fakeClient{limits: restLimits(), avatar: fakeAvatar()},
		clock:    &fakeClock{now: time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)},
		notifier: &fakeNotifier{},
	}

	h.svc = h.service()

	return h
}

// service builds a Service over the harness's database and fakes.
func (h *harness) service() *Service {
	return NewService(Config{DB: h.db, Tokens: h.tokens, Client: h.client, Clock: h.clock, Notifier: h.notifier})
}

// refresh runs a refresh that must succeed.
func (h *harness) refresh(t *testing.T) Snapshot {
	t.Helper()

	snap, err := h.svc.Refresh(t.Context())
	require.NoError(t, err)

	return snap
}

// reasons returns an item's three reason flags by number.
func (h *harness) reasons(t *testing.T, number int) [3]bool {
	t.Helper()

	var rows []model.Item
	require.NoError(t, h.db.Where("number = ?", number).Find(&rows).Error)
	require.Len(t, rows, 1)

	return [3]bool{rows[0].ReasonReviewRequested, rows[0].ReasonAuthored, rows[0].ReasonAssigned}
}

// count returns the number of rows in a table.
func (h *harness) count(t *testing.T, table string) int64 {
	t.Helper()

	var n int64
	require.NoError(t, h.db.Table(table).Count(&n).Error)

	return n
}

// pr builds a fetched pull request in example-repo.
func pr(number int, assoc string, crossRepo bool) github.Item {
	return item("example-repo", number, assoc, true, crossRepo)
}

// issue builds a fetched issue.
func issue(repo string, number int, assoc string) github.Item {
	return item(repo, number, assoc, false, false)
}

// item builds a fetched item in acme-org.
func item(repo string, number int, assoc string, isPR, crossRepo bool) github.Item {
	updated := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(number) * time.Hour)

	it := github.Item{
		IsPR:              isPR,
		NodeID:            fmt.Sprintf("NODE_%d", number),
		Number:            number,
		Title:             fmt.Sprintf("Item %d", number),
		URL:               fmt.Sprintf("https://github.com/acme-org/%s/pull/%d", repo, number),
		CreatedAt:         updated.Add(-time.Hour),
		UpdatedAt:         updated,
		AuthorLogin:       "octo-user",
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
			Login:     "octo-user",
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
	var bRow model.Item
	require.NoError(t, h.db.Where("number = ?", 2).First(&bRow).Error)
	require.NoError(t, h.db.Create(&model.ItemLocalState{ItemID: bRow.ID, UpdatedAt: h.clock.now}).Error)

	// Next refresh: A is only review-requested (retitled), B is gone, C is still assigned.
	a.Title = "Item 1, retitled"
	h.clock.now = h.clock.now.Add(DefaultInterval)
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
	bot.AuthorLogin = "dependabot[bot]"
	own := pr(7, "OWNER", false)
	ownFork := pr(8, "MEMBER", true)
	memberIssue := issue("other-repo", 21, "MEMBER")

	h.client.inbox = fetched([]github.Item{fork, bot, ownFork}, []github.Item{own}, []github.Item{memberIssue})
	snap := h.refresh(t)

	var stored []model.Item
	require.NoError(t, h.db.Order("number").Find(&stored).Error)
	require.Len(t, stored, 5)

	got := make(map[int]model.Item)
	for _, it := range stored {
		got[it.Number] = it
	}

	assert.Equal(t, "CONTRIBUTOR", got[12].AuthorAssociation)
	assert.True(t, got[12].IsCrossRepository)
	assert.Equal(t, "NONE", got[15].AuthorAssociation)
	assert.Equal(t, "dependabot[bot]", got[15].AuthorLogin)
	assert.Equal(t, "OWNER", got[7].AuthorAssociation)
	assert.False(t, got[7].IsCrossRepository)
	assert.Equal(t, model.KindIssue, got[21].Kind)
	assert.Equal(t, "branch-7", got[7].HeadRef)

	trusted := make(map[int]bool)

	for _, l := range []List{snap.ReviewRequested, snap.Authored, snap.Assigned} {
		for _, it := range l.Items {
			trusted[it.Number] = it.Trusted
		}
	}

	assert.Equal(t, map[int]bool{12: false, 15: false, 8: false, 7: true, 21: true}, trusted)

	assert.Equal(t, []int{15, 12, 8}, numbers(snap.ReviewRequested), "newest activity first")

	fromFork := snap.ReviewRequested.Items[1]
	assert.Equal(t, Item{
		ID: fromFork.ID, Kind: model.KindPR, Number: 12, Title: "Item 12",
		URL:  "https://github.com/acme-org/example-repo/pull/12",
		Repo: "acme-org/example-repo", Author: "octo-user", UpdatedAt: "2026-01-01T12:00:00Z",
		Trusted: false, IsCrossRepository: true, AuthorAssociation: "CONTRIBUTOR", Branch: "branch-12",
	}, fromFork)
}

func TestRefreshWritesRateSnapshot(t *testing.T) {
	h := newHarness(t)
	h.client.inbox = fetched(nil, nil, nil)
	h.refresh(t)

	var acct model.Account
	require.NoError(t, h.db.First(&acct).Error)

	// Readings older than the retention window are pruned on the next refresh.
	old := model.RateSnapshot{AccountID: acct.ID, Resource: resourceCore, LimitTotal: 1, TakenAt: h.clock.now.Add(-8 * 24 * time.Hour)}
	recent := model.RateSnapshot{AccountID: acct.ID, Resource: resourceCore, LimitTotal: 2, TakenAt: h.clock.now.Add(-6 * 24 * time.Hour)}
	require.NoError(t, h.db.Create(&old).Error)
	require.NoError(t, h.db.Create(&recent).Error)

	h.clock.now = h.clock.now.Add(DefaultInterval)
	snap := h.refresh(t)

	var rows []model.RateSnapshot
	require.NoError(t, h.db.Where("taken_at = ?", h.clock.now).Order("resource").Find(&rows).Error)
	require.Len(t, rows, 3, "one row per resource per refresh")

	assert.Equal(t, resourceCore, rows[0].Resource)
	assert.Equal(t, 4980, rows[0].Remaining)
	assert.Equal(t, resourceGraphQL, rows[1].Resource)
	assert.Equal(t, 1, rows[1].Cost, "the GraphQL row carries the query cost")
	assert.Equal(t, 4990, rows[1].Remaining)
	assert.Equal(t, resourceSearch, rows[2].Resource)
	assert.Equal(t, 30, rows[2].LimitTotal)

	var kept []model.RateSnapshot
	require.NoError(t, h.db.Where("id IN ?", []int64{old.ID, recent.ID}).Find(&kept).Error)
	require.Len(t, kept, 1)
	assert.Equal(t, recent.ID, kept[0].ID)

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
	assert.Equal(t, 1, h.notifier.calls, "the UI hears about the failure")

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
	assert.Equal(t, "octo-user", again.Account.Login)
}

func TestRefreshNeverPersistsToken(t *testing.T) {
	h := newHarness(t)
	h.client.inbox = fetched([]github.Item{pr(1, "OWNER", false)}, nil, []github.Item{issue("other-repo", 2, "MEMBER")})
	h.refresh(t)

	assert.Equal(t, []string{fakeToken, fakeToken}, h.client.gotTokens, "the token reaches the client")

	// A failure whose message echoes the token is recorded without it.
	h.client.inboxErr = fmt.Errorf("%w: upstream said: %s", errFakeNetwork, fakeToken)

	_, err := h.svc.Refresh(t.Context())
	require.Error(t, err)
	assert.NotContains(t, err.Error(), fakeToken)

	var tables []string
	require.NoError(t, h.db.Raw("SELECT name FROM sqlite_master WHERE type = 'table'").Scan(&tables).Error)
	require.NotEmpty(t, tables)

	for _, table := range tables {
		var rows []map[string]any
		require.NoError(t, h.db.Table(table).Find(&rows).Error)

		for _, row := range rows {
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
	h.clock.now = h.clock.now.Add(DefaultInterval)

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
	assert.Zero(t, h.notifier.calls)

	snap, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)
	assert.Empty(t, snap.LastError, "shutting down is not a refresh failure")
}

func TestRefreshNotifies(t *testing.T) {
	h := newHarness(t)
	h.client.inbox = fetched(nil, nil, nil)
	h.refresh(t)
	assert.Equal(t, 1, h.notifier.calls)
	assert.Equal(t, 1, h.tokens.calls, "the token is borrowed on every refresh")

	h.refresh(t)
	assert.Equal(t, 2, h.notifier.calls)
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
		Login:      "octo-user",
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
	assert.Equal(t, "octo-user", snap.Account.Login)
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
	require.NoError(t, store.Close(h.db))

	_, err := h.svc.Snapshot(t.Context())
	require.Error(t, err)

	_, err = h.svc.Refresh(t.Context())
	require.Error(t, err)
}

func TestNewServiceUsesTheSystemClock(t *testing.T) {
	h := newHarness(t)
	svc := NewService(Config{DB: h.db, Tokens: h.tokens, Client: h.client})
	h.client.inbox = fetched(nil, nil, nil)

	before := time.Now().UTC().Truncate(time.Second)

	snap, err := svc.Refresh(t.Context())
	require.NoError(t, err)

	refreshed, err := time.Parse(time.RFC3339, snap.LastRefreshedAt)
	require.NoError(t, err)
	assert.False(t, refreshed.Before(before))
}
