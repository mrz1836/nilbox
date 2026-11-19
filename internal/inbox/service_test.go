package inbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
	"github.com/mrz1836/nilbox/internal/uierr"
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
// Like the real client, it scrubs the token out of every error it returns, so a canned
// error that names the token checks that nothing past the client puts it back.
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
	itemsIDs    [][]string
	purposes    []github.Purpose

	// projects holds each issue's projects by node ID, answered only when a request asks
	// for them. refuseProjects answers such a request as the real client does once GitHub
	// refused them for want of a scope: without them, saying so. detailsOpts and itemOpts
	// record what each request asked for.
	projects       map[string][]github.ProjectItem
	refuseProjects bool
	detailsOpts    []github.DetailOptions
	itemOpts       []github.DetailOptions

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

	// viewer is the account FetchViewer reports, octoUser when "", and viewerErr fails
	// it; viewerReads counts its calls. edited is what an edit answers (an empty answer
	// when nil), editErr fails every edit, and edits records each as "unassign <ref>
	// <login>" or "unreview <ref> <login>".
	viewer      string
	viewerErr   error
	viewerReads int
	edited      *github.Edited
	editErr     error
	edits       []string

	// watches holds each routine search's canned result by query, and watchErr fails the
	// whole watch request; watchRate is its budget reading, watchCalls records the searches
	// of each request, and watchGate, when set, holds the request until it opens. checks
	// holds each pull request's canned checks by node ID, and checksIDs records each read.
	watches    map[string]github.WatchResult
	watchErr   error
	watchRate  github.QueryRate
	watchCalls [][]github.WatchSearch
	watchGate  *gate
	checks     map[string]github.HeadChecks
	checksIDs  [][]string

	// watchDetailsGate, when set, holds a details request that reads the repositories, as
	// only the watch pass asks, until it opens, and watchDetailsErr fails such a request.
	// watchPanic makes the watch request panic.
	watchDetailsGate *gate
	watchDetailsErr  error
	watchPanic       bool

	// itemsFail fails, as with a network error, a FetchItems request that holds this node
	// ID; the others are answered.
	itemsFail string

	// commits holds each pull request's newest commits with their authors by node ID,
	// commitsErr fails every such read, and commitIDs records the pull requests each asked
	// about.
	commits    map[string][]github.CommitAuthor
	commitsErr error
	commitIDs  [][]string
}

func (c *fakeClient) FetchCommitAuthors(
	_ context.Context, _ github.Token, ids []string,
) (map[string][]github.CommitAuthor, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.commitIDs = append(c.commitIDs, slices.Clone(ids))

	if c.commitsErr != nil {
		return nil, c.commitsErr
	}

	out := map[string][]github.CommitAuthor{}

	for _, id := range ids {
		if got, ok := c.commits[id]; ok {
			out[id] = got
		}
	}

	return out, nil
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

	return c.inbox, github.Redact(c.inboxErr, tok)
}

func (c *fakeClient) FetchRateLimits(_ context.Context, tok github.Token) (*github.RateLimits, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.gotTokens = append(c.gotTokens, tok.Reveal())

	return c.limits, github.Redact(c.limitsErr, tok)
}

// FetchDetails answers with the canned details of the IDs asked for, as one request, with
// their canned projects when opts asks for them.
func (c *fakeClient) FetchDetails(
	ctx context.Context, tok github.Token, ids []string, opts github.DetailOptions,
) (*github.Details, error) {
	if err := c.passDetailsGates(ctx, opts); err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.gotTokens = append(c.gotTokens, tok.Reveal())
	c.detailsIDs = append(c.detailsIDs, ids)

	// What it was asked is recorded without the count it tells, which it tells as the real
	// client does, once its one request is answered.
	recorded := opts
	recorded.Progress = nil
	c.detailsOpts = append(c.detailsOpts, recorded)

	if c.detailsErr != nil {
		return nil, github.Redact(c.detailsErr, tok)
	}

	if opts.Progress != nil {
		defer opts.Progress(len(ids))
	}

	if c.watchDetailsErr != nil && opts.Repository {
		return nil, github.Redact(c.watchDetailsErr, tok)
	}

	out := &github.Details{
		Items: map[string]github.ItemDetails{}, RateLimit: c.detailsRate,
		ProjectsRefused: opts.Projects && c.refuseProjects,
	}
	if len(ids) > 0 {
		out.Requests = 1
	}

	for _, id := range ids {
		if d, ok := c.details[id]; ok {
			d.Projects = c.projectsOf(id, opts)
			out.Items[id] = d
		}
	}

	return out, nil
}

// FetchItem answers with the canned update for the ID, with its canned projects when
// opts asks for them, or with one that did not find it.
func (c *fakeClient) FetchItem(
	_ context.Context, tok github.Token, id string, opts github.DetailOptions,
) (*github.ItemUpdate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.gotTokens = append(c.gotTokens, tok.Reveal())
	c.itemIDs = append(c.itemIDs, id)
	c.itemOpts = append(c.itemOpts, opts)

	if c.itemErr != nil {
		return nil, github.Redact(c.itemErr, tok)
	}

	update, ok := c.items[id]
	if !ok {
		return &github.ItemUpdate{}, nil
	}

	// A canned nil is an answer with nothing in it.
	if update == nil {
		return update, nil
	}

	answer := *update
	answer.Details.Projects = c.projectsOf(id, opts)
	answer.ProjectsRefused = opts.Projects && c.refuseProjects

	return &answer, nil
}

// FetchItems answers, in one request, with the canned update of each ID it holds one
// for, as FetchItem would, and leaves out the rest, as GitHub's nulls are.
func (c *fakeClient) FetchItems(
	ctx context.Context, tok github.Token, ids []string, opts github.DetailOptions,
) (*github.ItemUpdates, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.gotTokens = append(c.gotTokens, tok.Reveal())
	c.itemsIDs = append(c.itemsIDs, slices.Clone(ids))
	c.purposes = append(c.purposes, github.PurposeOf(ctx))
	c.itemOpts = append(c.itemOpts, opts)

	if c.itemErr != nil {
		return nil, github.Redact(c.itemErr, tok)
	}

	if c.itemsFail != "" && slices.Contains(ids, c.itemsFail) {
		return nil, errFakeNetwork
	}

	out := &github.ItemUpdates{Items: map[string]github.ItemUpdate{}, ProjectsRefused: opts.Projects && c.refuseProjects}

	for _, id := range ids {
		update := c.items[id]
		if update == nil || !update.Found {
			continue
		}

		answer := *update
		answer.Details.Projects = c.projectsOf(id, opts)
		out.Items[id] = answer
		out.RateLimit = answer.RateLimit
	}

	return out, nil
}

// FetchPullHead answers with the head of the canned files.
func (c *fakeClient) FetchPullHead(_ context.Context, tok github.Token, ref item.Ref) (*github.PullHead, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.gotTokens = append(c.gotTokens, tok.Reveal())
	c.headReads = append(c.headReads, ref.String())

	if c.filesErr != nil {
		return nil, github.Redact(c.filesErr, tok)
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
		return nil, github.Redact(c.filesErr, tok)
	}

	files, ok := c.files[ref.String()]
	if !ok {
		return nil, &github.StatusError{StatusCode: 404, Message: "Not Found"}
	}

	return files, nil
}

// FetchWatches answers each search with its canned result, recording the searches asked.
// A search with no canned result fails, as one GitHub didn't answer.
func (c *fakeClient) FetchWatches(
	ctx context.Context, tok github.Token, searches []github.WatchSearch,
) (*github.Watches, error) {
	c.mu.Lock()
	gate := c.watchGate
	c.mu.Unlock()

	if gate != nil {
		if err := gate.pass(ctx); err != nil {
			return nil, err
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.gotTokens = append(c.gotTokens, tok.Reveal())
	c.watchCalls = append(c.watchCalls, slices.Clone(searches))

	if c.watchPanic {
		panic("the watch request broke")
	}

	if c.watchErr != nil {
		return nil, github.Redact(c.watchErr, tok)
	}

	out := &github.Watches{Results: map[string]github.WatchResult{}, RateLimit: c.watchRate, Requests: 1}

	for _, s := range searches {
		r, ok := c.watches[s.Query]
		if !ok {
			r = github.WatchResult{Err: github.ErrSearchFailed, Items: []github.WatchItem{}}
		}

		out.Results[s.Key] = r
	}

	return out, nil
}

// FetchChecks answers with the canned checks of the IDs asked for.
func (c *fakeClient) FetchChecks(_ context.Context, tok github.Token, ids []string) (*github.ChecksRead, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.gotTokens = append(c.gotTokens, tok.Reveal())
	c.checksIDs = append(c.checksIDs, slices.Clone(ids))

	out := &github.ChecksRead{Items: map[string]github.HeadChecks{}, RateLimit: github.QueryRate{Cost: 1}, Requests: 1}

	for _, id := range ids {
		if h, ok := c.checks[id]; ok {
			out.Items[id] = h
		}
	}

	return out, nil
}

func (c *fakeClient) FetchAvatar(_ context.Context, url string) (*github.Avatar, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.avatarURLs = append(c.avatarURLs, url)

	return c.avatar, c.avatarErr
}

// passDetailsGates opens the inbox query's gate, which waits for a details request, and
// holds a request that reads the repositories, as only the watch pass's does, at its gate.
func (c *fakeClient) passDetailsGates(ctx context.Context, opts github.DetailOptions) error {
	if c.inboxGate != nil {
		c.gateOpened.Do(func() { close(c.inboxGate) })
	}

	if c.watchDetailsGate == nil || !opts.Repository {
		return nil
	}

	return c.watchDetailsGate.pass(ctx)
}

// projectsOf returns the canned projects of the item with id when opts asks for them and
// they are not refused. The caller holds mu.
func (c *fakeClient) projectsOf(id string, opts github.DetailOptions) []github.ProjectItem {
	if !opts.Projects || c.refuseProjects {
		return nil
	}

	return c.projects[id]
}

// harness is a Service over a temp database with fakes for everything else.
type harness struct {
	svc    *Service
	pool   *sql.DB
	path   string
	tokens *fakeTokens
	client *fakeClient
	clock  *testutil.Clock
	events *inboxEvents
	logs   *testutil.Logs
}

// inboxEvents records what the inbox tells: its updates and its other events in order, as
// testutil.Events does, and the refreshes' progress apart, in progress, so a test that
// counts what changed counts the changes alone; every event is in all too, in order.
type inboxEvents struct {
	testutil.Events

	progress testutil.Events
	all      testutil.Events
}

// Emit records e with its kind.
func (e *inboxEvents) Emit(ev event.Event) {
	e.all.Emit(ev)

	if _, ok := ev.(event.InboxProgress); ok {
		e.progress.Emit(ev)

		return
	}

	e.Events.Emit(ev)
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
		events: &inboxEvents{},
		logs:   &testutil.Logs{},
	}

	h.svc = h.service()

	return h
}

// service builds a Service over the harness's database and fakes.
func (h *harness) service() *Service {
	return NewService(Config{
		Pool: h.pool, Tokens: h.tokens, Client: h.client, Clock: h.clock, Events: h.events, Logger: h.logs.Logger(),
	})
}

// refresh runs a refresh that must succeed.
func (h *harness) refresh(t *testing.T) Snapshot {
	t.Helper()

	snap, err := h.svc.Refresh(t.Context())
	require.NoError(t, err)

	return snap
}

// seed has the inbox query answer with the lists fetched builds from review, authored,
// and assigned, then runs a refresh that must succeed.
func (h *harness) seed(t *testing.T, review, authored, assigned []github.Item) Snapshot {
	t.Helper()

	h.client.inbox = fetched(review, authored, assigned)

	return h.refresh(t)
}

// exec runs a statement on the harness's database. The statement writes past the
// service, so it also empties the service's snapshot cache, as the service's own writes
// do.
func (h *harness) exec(t *testing.T, query string, args ...any) {
	t.Helper()

	testutil.Exec(t, h.pool, query, args...)
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

// assignedNumbers is the numbers of the items assigned to the user, in either list, the
// Review requested list's first.
func assignedNumbers(s Snapshot) []int {
	out := []int{}

	for _, l := range s.Lists() {
		for _, it := range l.Items {
			if it.Assigned {
				out = append(out, it.Number)
			}
		}
	}

	return out
}

// anyItem finds an item in either list.
func anyItem(t *testing.T, s Snapshot, number int) Item {
	t.Helper()

	for _, l := range s.Lists() {
		for _, it := range l.Items {
			if it.Number == number {
				return it
			}
		}
	}

	require.Failf(t, "missing item", "#%d", number)

	return Item{}
}

func TestRefreshUpsertsItemsWithReasonFlags(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	a := pr(1, "OWNER", false)
	b := pr(2, "OWNER", false)
	c := byOther(issue("other-repo", 3, "MEMBER"))

	// A is both review-requested and assigned.
	snap := h.seed(t, []github.Item{a}, []github.Item{b}, []github.Item{a, c})

	assert.Equal(t, []int{1}, numbers(snap.ReviewRequested), "review requested, whatever else it is")
	assert.Equal(t, []int{3, 2}, numbers(snap.Yours), "the rest, newest activity first")
	assert.True(t, itemByNumber(t, snap.ReviewRequested, 1).Assigned, "its card says it is assigned too")
	assert.True(t, itemByNumber(t, snap.Yours, 2).Authored)
	assert.False(t, itemByNumber(t, snap.Yours, 2).Assigned)
	assert.True(t, itemByNumber(t, snap.Yours, 3).Assigned)
	assert.False(t, itemByNumber(t, snap.Yours, 3).Authored)
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
	snap = h.seed(t, []github.Item{a}, nil, []github.Item{c})

	assert.Equal(t, []int{1}, numbers(snap.ReviewRequested))
	assert.Equal(t, []int{3}, numbers(snap.Yours))
	assert.Equal(t, "Item 1, retitled", snap.ReviewRequested.Items[0].Title)

	assert.Equal(t, [3]bool{true, false, false}, h.reasons(t, 1))
	assert.Equal(t, [3]bool{false, false, false}, h.reasons(t, 2))
	assert.Equal(t, [3]bool{false, false, true}, h.reasons(t, 3))
	assert.Equal(t, int64(3), h.count(t, "items"), "items that drop out keep their row")
	assert.Equal(t, int64(1), h.count(t, "item_local_state"))
}

func TestTheListsPartitionTheInbox(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	// Every mix of the three reasons: 1 all three, 2 review requested and authored, 3
	// review requested and assigned, 4 authored and assigned, 5 to 7 one each.
	one, two, three, four := pr(1, "OWNER", false), pr(2, "OWNER", false), pr(3, "OWNER", false), pr(4, "OWNER", false)
	five, six, seven := pr(5, "OWNER", false), pr(6, "OWNER", false), issue("other-repo", 7, "MEMBER")

	snap := h.seed(t, []github.Item{one, two, three, five}, []github.Item{one, two, four, six},
		[]github.Item{one, three, four, seven})

	assert.ElementsMatch(t, []int{1, 2, 3, 5}, numbers(snap.ReviewRequested))
	assert.ElementsMatch(t, []int{4, 6, 7}, numbers(snap.Yours))

	inReview := map[int64]bool{}
	for _, it := range snap.ReviewRequested.Items {
		inReview[it.ID] = true
	}

	for _, it := range snap.Yours.Items {
		assert.False(t, inReview[it.ID], "#%d is in both lists", it.Number)
	}

	assert.Equal(t, len(snap.ReviewRequested.Items)+len(snap.Yours.Items), snap.NeedsYou(h.clock.Now()),
		"the lists' counts add up to the inbox's")

	mine := anyItem(t, snap, 4)
	assert.True(t, mine.Authored)
	assert.True(t, mine.Assigned)
	assert.Equal(t, ReviewRequestNone, mine.ReviewRequest)

	assert.Equal(t, 4, snap.ReviewRequested.Total)
	assert.Equal(t, 3, snap.Yours.Total)
}

func TestAnItemTheUserOpenedSaysSoWhicheverSearchFoundIt(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	// The authored search reads pull requests alone: an issue the user opened reaches the
	// inbox only once it is assigned to them, and so does their own pull request past the
	// authored search's page.
	mine, theirs := issue("example-repo", 1, "OWNER"), byOther(issue("example-repo", 2, "MEMBER"))
	mine.AuthorLogin = "Octo-User"
	snap := h.seed(t, nil, nil, []github.Item{mine, theirs, pr(3, "OWNER", false)})

	assert.True(t, itemByNumber(t, snap.Yours, 1).Authored, "the author is the user, in any case")
	assert.False(t, itemByNumber(t, snap.Yours, 2).Authored)
	assert.True(t, itemByNumber(t, snap.Yours, 3).Authored)
	assert.Equal(t, [3]bool{false, false, true}, h.reasons(t, 1), "the stored flags are the searches' alone")
}

func TestYoursTotalCountsBothSearchesBeyondTheirPage(t *testing.T) {
	t.Parallel()

	snap := Snapshot{ReviewRequested: List{Items: []Item{{ID: 1}}}, Yours: List{Items: []Item{{ID: 2}, {ID: 3}}}}
	snap.applyTotals(map[string]string{
		prefBeyondReviewRequested: "4", prefBeyondAuthored: "10", prefBeyondAssigned: "2",
	})

	assert.Equal(t, List{Items: snap.ReviewRequested.Items, Shown: 1, Total: 5}, snap.ReviewRequested)
	assert.Equal(t, List{Items: snap.Yours.Items, Shown: 2, Total: 14}, snap.Yours,
		"an upper bound: the authored and the assigned searches' counts past their pages")

	snap.applyTotals(map[string]string{prefBeyondAuthored: "bad", prefBeyondAssigned: "-3"})
	assert.Equal(t, 2, snap.Yours.Total, "a missing or bad count adds none")
}

func TestRefreshStoresTrustFields(t *testing.T) {
	t.Parallel()

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

	for _, l := range []List{snap.ReviewRequested, snap.Yours, snap.Yours} {
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
		Branch: "branch-12", Authored: true, ReviewRequest: ReviewRequestYou, ReviewTeams: []string{},
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
	t.Parallel()

	h := newHarness(t)
	h.seed(t, nil, nil, nil)

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
	t.Parallel()

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
	t.Parallel()

	h := newHarness(t)
	h.client.inbox = fetched(nil, nil, nil)
	h.client.inbox.OAuthScopes = ""

	snap := h.refresh(t)
	assert.Equal(t, string(github.ScopesUnverifiable), snap.ScopeWarning)
	assert.Empty(t, snap.LastError)
}

func TestSnapshotReportsNofM(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	review := []github.Item{pr(1, "OWNER", false), pr(2, "OWNER", false)}
	h.client.inbox = fetched(review, []github.Item{pr(3, "OWNER", false)}, nil)
	h.client.inbox.ReviewRequested.IssueCount = 7
	h.client.inbox.Authored.IssueCount = 0 // a count below what was returned adds none

	snap := h.refresh(t)
	assert.Equal(t, 2, snap.ReviewRequested.Shown)
	assert.Equal(t, 7, snap.ReviewRequested.Total)
	assert.Equal(t, 1, snap.Yours.Shown)
	assert.Equal(t, 1, snap.Yours.Total)

	// The totals and refresh status survive a restart.
	again, err := h.service().Snapshot(t.Context())
	require.NoError(t, err)
	assert.Equal(t, snap, again)
	assert.Equal(t, "2026-01-10T12:00:00Z", again.LastRefreshedAt)
	assert.Equal(t, octoUser, again.Account.Login)
}

func TestAnItemThatLeavesAListLeavesItsTotal(t *testing.T) {
	t.Parallel()

	h, id := itemHarness(t)

	// Review requested matched and returned #1 and #2; Assigned, #1 alone.
	before, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 2, before.ReviewRequested.Total)

	snap, err := NewActor(h.svc).Act(t.Context(), id, func(context.Context, github.Token, PullTarget) (Acted, error) {
		return Acted{Reviewed: true}, nil
	})
	require.NoError(t, err)

	assert.Equal(t, []int{2}, numbers(snap.ReviewRequested), "a review takes #1 out of Review requested")
	assert.Equal(t, 1, snap.ReviewRequested.Total, "and out of its total, which reads 1, not 1 of 2")
	assert.Equal(t, 1, snap.Yours.Total, "the lists it stays in keep theirs")
}

func TestALongListKeepsWhatItsSearchMatchedPastOnePage(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	review := []github.Item{pr(1, "OWNER", false), pr(2, "OWNER", false)}
	h.client.inbox = fetched(review, nil, nil)
	h.client.inbox.ReviewRequested.IssueCount = 120

	snap := h.refresh(t)
	assert.Equal(t, 120, snap.ReviewRequested.Total, "the 2 returned and the 118 past them")

	id := itemByNumber(t, snap.ReviewRequested, 1).ID
	merged := pr(1, "OWNER", false)
	merged.State = github.StateMerged
	h.client.items = map[string]*github.ItemUpdate{nodeID(1): {Found: true, Item: merged, RateLimit: itemRate()}}

	snap, err := h.svc.RefreshItem(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, 1, snap.ReviewRequested.Shown)
	assert.Equal(t, 119, snap.ReviewRequested.Total, "the merged item leaves the total; the 118 past the page stay")
}

func TestARefreshForgetsTheWholeCountsAnOlderBuildKept(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	require.NoError(t, h.svc.prefs.Set(t.Context(), "inbox.total.authored", "9"))

	snap := h.seed(t, nil, []github.Item{pr(1, "OWNER", false)}, nil)

	assert.Equal(t, 1, snap.Yours.Total, "an old whole count is never read as a count past the page")

	_, found, err := h.svc.prefs.Get(t.Context(), "inbox.total.authored")
	require.NoError(t, err)
	assert.False(t, found, "and the next whole refresh removes it")
}

func TestRefreshNeverPersistsToken(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.seed(t, []github.Item{pr(1, "OWNER", false)}, nil, []github.Item{issue("other-repo", 2, "MEMBER")})

	assert.Equal(t, []string{fakeToken, fakeToken, fakeToken}, h.client.gotTokens, "the token reaches the client")

	// A failure whose message echoes the token is recorded without it.
	h.client.inboxErr = fmt.Errorf("%w: upstream said: %s", errFakeNetwork, fakeToken)

	_, err := h.svc.Refresh(t.Context())
	require.Error(t, err)
	assert.NotContains(t, err.Error(), fakeToken)
	assert.NotContains(t, uierr.Message(err), fakeToken)

	// Nor is it logged, by the service or by a caller that logs the error.
	h.logs.Logger().Warn("inbox refresh failed", "error", err)
	assert.NotContains(t, h.logs.String(), fakeToken)

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
	t.Parallel()

	h := newHarness(t)
	h.tokens.err = uierr.New("Install the GitHub CLI.", github.ErrGHNotFound)

	// The error keeps its whole chain for the logs; the snapshot keeps what the user is
	// told.
	snap, err := h.svc.Refresh(t.Context())
	require.ErrorIs(t, err, github.ErrGHNotFound)
	assert.Equal(t, "refresh the inbox: "+github.ErrGHNotFound.Error(), err.Error())
	assert.Equal(t, "Install the GitHub CLI.", snap.LastError)
	assert.Empty(t, h.client.gotTokens, "nothing is fetched without a token")
	assert.Empty(t, snap.ReviewRequested.Items)
	assert.Empty(t, snap.LastRefreshedAt)
}

func TestRefreshFetchErrorsKeepTheStoredInbox(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.seed(t, []github.Item{pr(1, "OWNER", false)}, nil, nil)

	h.client.limitsErr = errFakeNetwork
	h.clock.Advance(DefaultInterval)

	// An error with no message for the user is shown as its own text, as a sentence.
	snap, err := h.svc.Refresh(t.Context())
	require.ErrorIs(t, err, errFakeNetwork)
	assert.Equal(t, "Refresh the inbox: network unreachable.", snap.LastError)
	assert.Equal(t, []int{1}, numbers(snap.ReviewRequested), "the last good inbox is still shown")
	assert.Equal(t, "2026-01-10T12:00:00Z", snap.LastRefreshedAt)
}

func TestRefreshRequiresViewer(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.client.inbox = fetched(nil, nil, nil)
	h.client.inbox.Viewer.Login = ""

	_, err := h.svc.Refresh(t.Context())
	require.ErrorIs(t, err, ErrIncompleteFetch)
}

func TestRefreshCanceledIsNotRecorded(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	h := newHarness(t)
	h.seed(t, nil, nil, nil)
	assert.Equal(t, 1, h.events.Count())
	assert.Equal(t, 1, h.tokens.calls, "the token is borrowed on every refresh")

	h.refresh(t)
	assert.Equal(t, 2, h.events.Count())
	assert.Equal(t, 2, h.tokens.calls)
}

func TestSnapshotBeforeFirstRefresh(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	snap, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)
	assert.Equal(t, Snapshot{
		ReviewRequested: List{Items: []Item{}},
		Yours:           List{Items: []Item{}},
	}, snap)
}

func TestSnapshotSwitchesWithTheViewer(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.seed(t, []github.Item{pr(1, "OWNER", false)}, nil, nil)

	// Signing in as another account shows only that account's items.
	h.client.inbox = fetched(nil, []github.Item{pr(2, "MEMBER", false)}, nil)
	h.client.inbox.Viewer = github.Viewer{Login: "octo-other", URL: "https://github.com/octo-other", AvatarURL: octoOtherAvatar}
	snap := h.refresh(t)

	assert.Equal(t, "octo-other", snap.Account.Login)
	assert.Empty(t, snap.ReviewRequested.Items)
	assert.Equal(t, []int{2}, numbers(snap.Yours))
	assert.Equal(t, int64(2), h.count(t, "accounts"))
}

func TestRefreshStoresTheAccount(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

	h := newHarness(t)
	h.client.inbox = fetched([]github.Item{pr(1, "OWNER", false)}, nil, nil)
	h.client.avatarErr = errFakeNetwork

	snap := h.refresh(t)
	assert.Empty(t, snap.LastError, "a missing picture is not a refresh failure")
	assert.Equal(t, []int{1}, numbers(snap.ReviewRequested))
	assert.Equal(t, octoUser, snap.Account.Login)
	assert.Empty(t, snap.Account.Avatar)
	assert.Contains(t, h.logs.String(), `level=DEBUG msg="could not download the avatar" error="network unreachable"`)

	// The next refresh tries again.
	h.client.avatarErr = nil
	snap = h.refresh(t)

	assert.Len(t, h.client.avatarURLs, 2)
	assert.NotEmpty(t, snap.Account.Avatar)
}

func TestRefreshWithoutAnAvatarURL(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.client.inbox = fetched(nil, nil, nil)
	h.client.inbox.Viewer.AvatarURL = ""

	snap := h.refresh(t)

	assert.Empty(t, h.client.avatarURLs)
	assert.Empty(t, snap.Account.Avatar)
}

func TestSnapshotNeverShowsAnotherAccountsAvatar(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	require.NotEmpty(t, h.seed(t, nil, nil, nil).Account.Avatar)

	// The new account's picture cannot be downloaded, so it shows none rather than the old one.
	h.client.inbox.Viewer = github.Viewer{Login: "octo-other", AvatarURL: octoOtherAvatar}
	h.client.avatarErr = errFakeNetwork

	snap := h.refresh(t)

	assert.Equal(t, "octo-other", snap.Account.Login)
	assert.Empty(t, snap.Account.Avatar)
}

func TestSnapshotFallsBackToTheGitHubProfileURL(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.client.inbox = fetched(nil, nil, nil)
	h.client.inbox.Viewer.URL = ""

	snap := h.refresh(t)

	assert.Equal(t, "https://github.com/octo-user", snap.Account.ProfileURL)
}

func TestSnapshotDatabaseErrors(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.seed(t, nil, nil, nil)
	require.NoError(t, h.pool.Close())

	_, err := h.svc.Snapshot(t.Context())
	require.Error(t, err)

	_, err = h.svc.Refresh(t.Context())
	require.Error(t, err)
}

func TestNewServiceUsesTheSystemClock(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	h := newHarness(t)

	a := pr(1, "OWNER", false)
	b := pr(2, "OWNER", false)
	c := issue("docs-repo", 3, "COLLABORATOR")

	h.client.inbox = fetched([]github.Item{a}, []github.Item{b}, []github.Item{a, c})
	h.client.details = map[string]github.ItemDetails{
		nodeID(1): prDetails(),
		nodeID(3): {
			Labels: []github.Label{{Name: "bug", Color: "#d73a4a"}}, Comments: 7,
			Issue: github.IssueInfo{
				Type:   github.IssueType{Name: "Bug", Color: "#fa4549"},
				Fields: []github.IssueField{{Name: "Priority", Value: "High", Color: "#fa4549"}, {Name: "Estimate", Value: "3"}},
				Milestone: &github.Milestone{
					Title: "v2", URL: "https://github.com/acme-org/docs-repo/milestone/2",
					DueOn: time.Date(2026, 7, 1, 7, 0, 0, 0, time.UTC), State: github.StateClosed, Progress: 100,
				},
				Parent: &github.ItemRef{
					Repo: "acme-org/docs-repo", Number: 1, Title: "Docs overhaul",
					URL: "https://github.com/acme-org/docs-repo/issues/1",
				},
				BlockedBy: 1,
				Blocking:  2,
			},
			ClosingPulls: []github.ClosingPull{
				{
					Repo: "acme-org/docs-repo", Number: 4, Title: "Fix the docs", URL: "https://github.com/acme-org/docs-repo/pull/4",
					AuthorLogin: "octo-member", AuthorType: "User", AuthorAssociation: "MEMBER",
				},
				{
					Repo: "acme-org/docs-repo", Number: 5, Title: "A stranger's fix", URL: "https://github.com/acme-org/docs-repo/pull/5",
					IsDraft: true, AuthorLogin: "stranger", AuthorType: "User", AuthorAssociation: "MEMBER", IsCrossRepository: true,
				},
				{
					Repo: "acme-org/other-repo", Number: 6, Title: "My fix", URL: "https://github.com/acme-org/other-repo/pull/6",
					Merged: true, AuthorLogin: "Octo-User", AuthorType: "User", AuthorAssociation: "CONTRIBUTOR",
					IsCrossRepository: true,
				},
			},
		},
	}

	snap := h.refresh(t)

	assert.Equal(t, [][]string{{nodeID(1), nodeID(2), nodeID(3)}}, h.client.detailsIDs, "each item is asked for once")

	assert.Equal(t, &Details{
		Labels:       []Label{{Name: "enhancement", Color: "#a2eeef"}},
		Projects:     []Project{},
		Comments:     4,
		Additions:    120,
		Deletions:    30,
		ChangedFiles: 6,
		Commits:      3,
		Mergeable:    "MERGEABLE",
		Checks:       Checks{State: "SUCCESS", Passed: 12, Skipped: 3},
		Reviews:      Reviews{Decision: "APPROVED", Reviewers: 2, Reviewed: 1, Approvals: 1},
		Issue:        IssueInfo{Fields: []IssueField{}},
		ClosingPulls: []ClosingPull{},
		FetchedAt:    "2026-01-10T12:00:00Z",
	}, itemByNumber(t, snap.ReviewRequested, 1).Details)

	assert.Equal(t, &Details{
		Labels:   []Label{{Name: "bug", Color: "#d73a4a"}},
		Projects: []Project{},
		Comments: 7,
		Issue: IssueInfo{
			Type: "Bug", TypeColor: "#fa4549",
			Fields: []IssueField{{Name: "Priority", Value: "High", Color: "#fa4549"}, {Name: "Estimate", Value: "3"}},
			Milestone: &Milestone{
				Title: "v2", URL: "https://github.com/acme-org/docs-repo/milestone/2", DueOn: "2026-07-01",
				Closed: true, Progress: 100,
			},
			Parent: &IssueRef{
				Repo: "acme-org/docs-repo", Number: 1, Title: "Docs overhaul",
				URL: "https://github.com/acme-org/docs-repo/issues/1",
			},
			BlockedBy: 1,
			Blocking:  2,
		},
		ClosingPulls: []ClosingPull{
			{
				Repo: "acme-org/docs-repo", Number: 4, Title: "Fix the docs", URL: "https://github.com/acme-org/docs-repo/pull/4",
				Trusted: true,
			},
			{
				Repo: "acme-org/docs-repo", Number: 5, Title: "A stranger's fix", URL: "https://github.com/acme-org/docs-repo/pull/5",
				IsDraft: true,
			},
			{
				Repo: "acme-org/other-repo", Number: 6, Title: "My fix", URL: "https://github.com/acme-org/other-repo/pull/6",
				Merged: true, Trusted: true,
			},
		},
		FetchedAt: "2026-01-10T12:00:00Z",
	}, itemByNumber(t, snap.Yours, 3).Details,
		"an issue's planning comes back as it was read, its milestone due on its day, and its closing pull requests "+
			"with their trust: a fork's is never trusted, unless it is the user's own")

	assert.Nil(t, itemByNumber(t, snap.Yours, 2).Details, "GitHub sent nothing for it")
}

func TestRefreshReplacesTheDetails(t *testing.T) {
	t.Parallel()

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

func TestRefreshStoresTheRunsHeldForApproval(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.client.inbox = fetched([]github.Item{pr(1, "OWNER", false)}, nil, nil)

	// The rollup reads SUCCESS while a fork's workflow runs wait for a maintainer, and one a
	// review started in the base repository waits for approval on GitHub.
	held := prDetails()
	held.Checks = github.Checks{
		State: "SUCCESS", Passed: 1, Awaiting: 4, AwaitingOnGitHub: 1,
		Held: []github.HeldRun{
			{Workflow: "CI"},
			{Workflow: "Lint"},
			{Workflow: "Docs"},
			{Workflow: "Deploy Preview"},
			{Workflow: "Auto-merge Trigger", OnGitHub: true},
		},
	}
	h.client.details = map[string]github.ItemDetails{nodeID(1): held}

	snap := h.refresh(t)

	first := itemByNumber(t, snap.ReviewRequested, 1)
	require.NotNil(t, first.Details)
	assert.Equal(t, Checks{State: "SUCCESS", Passed: 1, Awaiting: 4}, first.Details.Checks,
		"the runs the user can approve count, and the one approved on GitHub doesn't")
	assert.JSONEq(t, `[{"workflow":"CI","onGitHub":false},{"workflow":"Lint","onGitHub":false},`+
		`{"workflow":"Docs","onGitHub":false},{"workflow":"Deploy Preview","onGitHub":false},`+
		`{"workflow":"Auto-merge Trigger","onGitHub":true}]`,
		h.column(t, `SELECT checks_held FROM item_details`)[0], "each held run is stored with its workflow")

	stored, err := NewStore(h.pool, h.clock).ReviewItem(t.Context(), first.ID)
	require.NoError(t, err)
	assert.Equal(t, 4, stored.ChecksAwaiting, "the review sheet reads the count too")

	rows, err := NewStore(h.pool, h.clock).FactsRows(t.Context(), []int64{first.ID})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.NotNil(t, rows[0].Details)
	assert.Equal(t, 5, rows[0].Details.ChecksAwaiting, "a routine counts every held run as checks not all run")

	// Approved: the runs started, and the count is replaced with the rest.
	running := prDetails()
	running.Checks = github.Checks{State: "PENDING", Passed: 1, Pending: 6}
	h.client.details = map[string]github.ItemDetails{nodeID(1): running}
	h.clock.Advance(DefaultInterval)

	snap = h.refresh(t)
	assert.Equal(t, Checks{State: "PENDING", Passed: 1, Pending: 6}, itemByNumber(t, snap.ReviewRequested, 1).Details.Checks)
}

func TestRefreshDetailsFailureKeepsTheStoredInbox(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
		{
			// The inbox query runs alongside the first details requests and can answer
			// after them.
			name:    "an inbox query that answered last",
			details: &github.Details{Requests: 1, RateLimit: github.QueryRate{Cost: 2, Limit: 5000, Remaining: 4995}},
			want:    github.QueryRate{Cost: 3, Limit: 5000, Remaining: 4990},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, graphQLReading(inbox, tt.details))
		})
	}
}

func TestSaveWithoutDetails(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.client.inbox = fetched([]github.Item{pr(1, "OWNER", false)}, nil, nil)

	_, err := h.svc.save(t.Context(), h.svc.startRefresh(), refreshed{inbox: h.client.inbox, limits: restLimits()})
	require.NoError(t, err)

	snap, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)
	assert.Nil(t, itemByNumber(t, snap.ReviewRequested, 1).Details)
}

func TestRefreshReadsTheListedDetailsAlongsideTheInbox(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.seed(t, []github.Item{pr(1, "OWNER", false)}, nil, nil)

	// The inbox query now answers only once a details request has started.
	h.client.inboxGate = make(chan struct{})
	h.client.details = map[string]github.ItemDetails{nodeID(1): prDetails()}

	snap := h.refresh(t)

	assert.NotNil(t, itemByNumber(t, snap.ReviewRequested, 1).Details)
}

func TestRefreshAsksAfterTheInboxOnlyAboutNewItems(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	a := pr(1, "OWNER", false)
	b := pr(2, "OWNER", false)
	c := pr(3, "OWNER", false)

	// The first refresh has nothing listed yet, so it asks about everything afterwards.
	h.seed(t, []github.Item{a, b}, nil, nil)

	// Then A leaves the lists and C joins: the listed A and B are read alongside the
	// inbox query (newest first), and only C afterwards.
	h.seed(t, []github.Item{b, c}, nil, nil)

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
	t.Parallel()

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
	t.Parallel()

	h := newHarness(t)
	h.exec(t, "DROP TABLE items")

	_, err := h.svc.listedNodeIDs(t.Context())
	require.ErrorContains(t, err, "read the listed items")

	_, err = h.svc.Refresh(t.Context())
	require.ErrorContains(t, err, "read the listed items")
}

func TestNewIDs(t *testing.T) {
	t.Parallel()

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
    pushed_at, fetched_at, local_path, created_at, updated_at
FROM repos WHERE name = ?`, name).Scan(&r.ID, &r.NodeID, &r.Description, &r.Visibility, &r.IsArchived,
		&r.ArchivedAt, &r.IsFork, &r.Parent, &r.DefaultBranch, &r.ViewerPermission, &r.ViewerMergeMethod,
		&r.MergeCommitAllowed, &r.SquashMergeAllowed, &r.AutoMergeAllowed, &r.AllowUpdateBranch, &r.Language,
		&r.LanguageColor, &r.PushedAt, &r.FetchedAt, &r.LocalPath, &r.CreatedAt, &r.UpdatedAt))

	return r
}

func TestRefreshStoresWhatGitHubSaysAboutEachRepository(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	a := pr(1, "OWNER", false)
	a.Repo = github.Repository{
		NodeID: "R_fakeRepoExample", Description: "An example repository", Visibility: "PRIVATE",
		IsFork: true, Parent: "acme-org/upstream-repo", DefaultBranch: "main", ViewerPermission: "WRITE",
		ViewerMergeMethod: "SQUASH", SquashMergeAllowed: true, AutoMergeAllowed: true, AllowUpdateBranch: true,
		Language: "Go", LanguageColor: "#00add8", PushedAt: time.Date(2026, 1, 9, 8, 0, 0, 0, time.UTC),
	}
	h.seed(t, []github.Item{a}, nil, nil)

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

	// The local column is the app's, and a refresh leaves it alone while it rewrites
	// GitHub's.
	h.exec(t, "UPDATE repos SET local_path = ? WHERE id = ?", "~/projects/example-repo", repo.ID)

	a.Repo.Description = "Renamed description"
	a.Repo.PushedAt = time.Time{}
	h.seed(t, []github.Item{a}, nil, nil)

	repo = h.repoRow(t, "example-repo")
	assert.Equal(t, "Renamed description", repo.Description)
	assert.Nil(t, repo.PushedAt, "an unknown push time is stored as unknown")
	assert.Equal(t, "~/projects/example-repo", repo.LocalPath)
}

func TestRefreshLeavesArchivedRepositoriesOut(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	live := pr(1, "OWNER", false)
	old := issue("old-repo", 2, "MEMBER")

	snap := h.seed(t, []github.Item{live}, nil, []github.Item{old})
	assert.Equal(t, []int{2}, numbers(snap.Yours))

	// old-repo is archived; GitHub's index has not caught up, so the search still returns
	// its issue, and in two lists.
	archivedAt := time.Date(2026, 1, 10, 11, 0, 0, 0, time.UTC)
	old.Repo = github.Repository{IsArchived: true, ArchivedAt: archivedAt}
	h.client.inbox = fetched([]github.Item{live, old}, nil, []github.Item{old})
	h.client.detailsIDs = nil

	snap = h.refresh(t)

	assert.Equal(t, []int{1}, numbers(snap.ReviewRequested))
	assert.Empty(t, snap.Yours.Items, "the archived repository's issue is no longer listed")
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
	t.Parallel()

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
	assert.Equal(t, 1, inbox.ReviewRequested.IssueCount, "the archived item is counted out of GitHub's count")
	assert.Equal(t, 0, inbox.Authored.IssueCount)
}
