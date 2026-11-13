package ghthreads

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/threadrules"
)

const (
	// owner and exampleRepo name the fixtures' repository.
	owner       = "acme-org"
	exampleRepo = "example-repo"

	// viewerLogin is the account the fake GitHub's token belongs to, and otherLogin
	// another.
	viewerLogin = "octo-user"
	otherLogin  = "octo-other"

	// firstID is the first fixture thread's ID, past int32 as GitHub's are.
	firstID int64 = 20000000001
)

// start is the fixtures' first moment: the newest thread is updated then.
var start = time.Date(2026, 11, 12, 18, 0, 0, 0, time.UTC) //nolint:gochecknoglobals // a fixed test time

// fakeThread is one notification as the fake GitHub lists it.
type fakeThread struct {
	id      int64
	unread  bool
	reason  string
	updated time.Time
	title   string

	// repo is "owner/name"; subject is the subject's path under the repository's API URL,
	// as "pulls/42" or "issues/7", "" for a subject with no URL (a check suite's), and
	// typ GitHub's subject type.
	repo    string
	subject string
	typ     string
}

// prThread is pull request number's thread, unread, updated at.
func prThread(id int64, number int, at time.Time) fakeThread {
	return fakeThread{
		id: id, unread: true, reason: "review_requested", updated: at, title: "Retry the sync worker " + strconv.Itoa(number),
		repo: owner + "/" + exampleRepo, subject: "pulls/" + strconv.Itoa(number), typ: "PullRequest",
	}
}

// prThreads returns n pull request threads, each about its own pull request, the newest
// updated at newest and each a minute older than the last.
func prThreads(n int, newest time.Time) []fakeThread {
	out := make([]fakeThread, 0, n)

	for i := range n {
		out = append(out, prThread(firstID+int64(i), i+1, newest.Add(-time.Duration(i)*time.Minute)))
	}

	return out
}

// listedRequest is one request the fake GitHub received.
type listedRequest struct {
	// kind is list, viewer, or states.
	kind string

	// page is a listing's page, and ifNoneMatch the ETag it was sent with.
	page        int
	ifNoneMatch string

	// refs are the items a states query asked about, as owner/name#number.
	refs []string
}

// fakeGitHub answers the notifications listing and the GraphQL queries a pass sends, as
// the tests script them.
type fakeGitHub struct {
	srv *httptest.Server

	mu sync.Mutex

	// threads are the user's notifications, listed newest updated first, 50 to a page.
	threads []fakeThread

	// poll is the X-Poll-Interval every listing answer carries, "" for none.
	poll string

	// listStatus answers every listing with that status when set, listMessage being
	// GitHub's message; failPage answers that page 502.
	listStatus  int
	listMessage string
	failPage    int

	// viewer is the token's login.
	viewer string

	// states are the items' states by owner/name#number, an item missing is one GitHub
	// can't find; statesStatus answers every states query with that status when set.
	states       map[string]string
	statesStatus int

	got []listedRequest
}

// newFakeGitHub returns a fake GitHub for viewerLogin, with no notifications.
func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()

	f := &fakeGitHub{viewer: viewerLogin, states: map[string]string{}, poll: "60"}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)

	return f
}

// client returns a client of the fake GitHub's.
func (f *fakeGitHub) client() *github.Client {
	return &github.Client{BaseURL: f.srv.URL, HTTP: f.srv.Client()}
}

// set replaces the notifications.
func (f *fakeGitHub) set(threads []fakeThread) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.threads = slices.Clone(threads)
}

// update changes the thread id: it is updated at, and unread, so it moves up the listing.
func (f *fakeGitHub) update(id int64, at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()

	i := slices.IndexFunc(f.threads, func(t fakeThread) bool { return t.id == id })
	f.threads[i].updated, f.threads[i].unread = at, true
}

// remove takes the thread id out, as marking it done does.
func (f *fakeGitHub) remove(id int64) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.threads = slices.DeleteFunc(f.threads, func(t fakeThread) bool { return t.id == id })
}

// configure changes the fake under its lock.
func (f *fakeGitHub) configure(change func(f *fakeGitHub)) {
	f.mu.Lock()
	defer f.mu.Unlock()

	change(f)
}

// requests returns the requests received, in order, and forgets them.
func (f *fakeGitHub) requests() []listedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()

	got := f.got
	f.got = nil

	return got
}

// listings returns the listing requests among got.
func listings(got []listedRequest) []listedRequest {
	return slices.DeleteFunc(slices.Clone(got), func(r listedRequest) bool { return r.kind != "list" })
}

// pagesOf returns the pages the listing requests among got asked for, nil for none.
func pagesOf(got []listedRequest) []int {
	listed := listings(got)
	if len(listed) == 0 {
		return nil
	}

	pages := make([]int, 0, len(listed))

	for _, r := range listed {
		pages = append(pages, r.page)
	}

	return pages
}

// serve answers one request.
func (f *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/notifications":
		f.serveListing(w, r)
	case "/graphql":
		f.serveGraphQL(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// serveListing answers a page of the listing: 304 for page 1 sent with its ETag, the page
// with a Link to the next while there is one.
func (f *fakeGitHub) serveListing(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	ifNoneMatch := r.Header.Get("If-None-Match")

	f.mu.Lock()
	defer f.mu.Unlock()

	f.got = append(f.got, listedRequest{kind: "list", page: page, ifNoneMatch: ifNoneMatch})

	if f.poll != "" {
		w.Header().Set("X-Poll-Interval", f.poll)
	}

	switch {
	case f.listStatus != 0:
		w.WriteHeader(f.listStatus)
		_, _ = fmt.Fprintf(w, `{"message": %q}`, f.listMessage)

		return
	case page == f.failPage:
		w.WriteHeader(http.StatusBadGateway)

		return
	}

	// GitHub lists the newest updated first.
	sorted := slices.Clone(f.threads)
	slices.SortStableFunc(sorted, func(a, b fakeThread) int { return b.updated.Compare(a.updated) })

	pages := slices.Collect(slices.Chunk(sorted, github.ThreadsPerPage))

	body, err := f.pageBody(pages, page)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)

		return
	}

	first, err := f.pageBody(pages, 1)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)

		return
	}

	etag := `W/"` + hashOf(first) + `"`

	if page == 1 && ifNoneMatch == etag {
		w.Header().Set("ETag", etag)
		w.WriteHeader(http.StatusNotModified)

		return
	}

	w.Header().Set("ETag", etag)

	if page < len(pages) {
		w.Header().Set("Link", fmt.Sprintf(`<%s/notifications?all=true&is_done=false&per_page=50&page=%d>; rel="next"`,
			f.srv.URL, page+1))
	}

	_, _ = io.WriteString(w, body)
}

// pageBody is page as GitHub lists it, "[]" past the last.
func (f *fakeGitHub) pageBody(pages [][]fakeThread, page int) (string, error) {
	if page < 1 || page > len(pages) {
		return "[]", nil
	}

	entries := make([]map[string]any, 0, len(pages[page-1]))

	for _, t := range pages[page-1] {
		subject := map[string]any{"title": t.title, "type": t.typ, "url": nil, "latest_comment_url": nil}
		if t.subject != "" {
			url := f.srv.URL + "/repos/" + t.repo + "/" + t.subject
			subject["url"], subject["latest_comment_url"] = url, url
		}

		entries = append(entries, map[string]any{
			"id": strconv.FormatInt(t.id, 10), "unread": t.unread, "reason": t.reason,
			"updated_at": t.updated.Format(time.RFC3339), "last_read_at": nil, "subject": subject,
			"repository": map[string]any{"full_name": t.repo},
		})
	}

	raw, err := json.Marshal(entries)

	return string(raw), err
}

// statesAlias reads one item a states query asks about.
var statesAlias = regexp.MustCompile(`(r\d+): repository\(owner: "([^"]+)", name: "([^"]+)"\) \{ item: issueOrPullRequest\(number: (\d+)\)`)

// serveGraphQL answers the viewer query and the states query.
func (f *fakeGitHub) serveGraphQL(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query string `json:"query"`
	}

	_ = json.NewDecoder(r.Body).Decode(&req)

	f.mu.Lock()
	defer f.mu.Unlock()

	if strings.Contains(req.Query, "viewer { login }") {
		f.got = append(f.got, listedRequest{kind: "viewer"})
		_, _ = fmt.Fprintf(w, `{"data": {"viewer": {"login": %q}, "rateLimit": {"cost": 1}}}`, f.viewer)

		return
	}

	asked := listedRequest{kind: "states"}
	data := map[string]any{"rateLimit": map[string]any{"cost": 1}}

	for _, m := range statesAlias.FindAllStringSubmatch(req.Query, -1) {
		ref := m[2] + "/" + m[3] + "#" + m[4]
		asked.refs = append(asked.refs, ref)

		state, ok := f.states[ref]
		if !ok {
			data[m[1]] = nil

			continue
		}

		node := map[string]any{"__typename": "PullRequest", "prState": state, "title": "An item", "url": ""}
		if state != github.StateOpen {
			node["closedAt"] = start.Format(time.RFC3339)
		}

		if state == github.StateMerged {
			node["mergedAt"] = start.Format(time.RFC3339)
		}

		data[m[1]] = map[string]any{"item": node}
	}

	f.got = append(f.got, asked)

	if f.statesStatus != 0 {
		w.WriteHeader(f.statesStatus)

		return
	}

	raw, err := json.Marshal(map[string]any{"data": data})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)

		return
	}

	_, _ = w.Write(raw)
}

// hashOf is a short hash of s.
func hashOf(s string) string {
	sum := sha256.Sum256([]byte(s))

	return hex.EncodeToString(sum[:8])
}

// fakeTokens lends a token that never reaches GitHub, and counts its loans.
type fakeTokens struct {
	mu       sync.Mutex
	value    string
	borrowed int
}

// Token lends the token.
func (f *fakeTokens) Token(context.Context) (github.Token, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.borrowed++

	return github.NewToken(f.value), nil
}

// Forget does nothing: the fake GitHub never refuses the token.
func (*fakeTokens) Forget(github.Token) {}

// loans returns how many tokens were lent.
func (f *fakeTokens) loans() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.borrowed
}

// swap lends another token from now on, as gh auth switch does.
func (f *fakeTokens) swap(value string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.value = value
}

// fakeRules are the user's rules, reading on until turned off.
type fakeRules struct {
	off atomic.Bool
}

// ThreadRules returns the defaults, with Read as set.
func (r *fakeRules) ThreadRules(context.Context) (threadrules.Rules, error) {
	rules := threadrules.Defaults()
	rules.Read = !r.off.Load()

	return rules, nil
}

// rig is a Service over a fresh database, a fake GitHub, and a clock the test moves.
type rig struct {
	t      *testing.T
	gh     *fakeGitHub
	db     *sql.DB
	clock  *testutil.Clock
	tokens *fakeTokens
	rules  *fakeRules
	events *testutil.Events
	svc    *Service
}

// newRig returns a rig with no notifications, at start.
func newRig(t *testing.T) *rig {
	t.Helper()

	r := &rig{
		t: t, gh: newFakeGitHub(t), db: testutil.DB(t), clock: testutil.NewClock(start),
		tokens: &fakeTokens{value: "test-token-not-real"}, rules: &fakeRules{}, events: &testutil.Events{},
	}
	r.restart()

	return r
}

// restart builds the Service again over the same database, as the app's next start does.
func (r *rig) restart() {
	r.svc = NewService(Config{
		Pool: r.db, Tokens: r.tokens, Client: r.gh.client(), Rules: r.rules, Clock: r.clock, Events: r.events,
	})
}

// pass runs a pass as the Syncer does, and returns the requests it sent.
func (r *rig) pass() ([]listedRequest, error) {
	r.t.Helper()

	err := r.svc.runPass(r.t.Context(), passRequest{})

	return r.gh.requests(), err
}

// mustPass runs a pass that must go through, and returns the requests it sent.
func (r *rig) mustPass() []listedRequest {
	r.t.Helper()

	got, err := r.pass()
	require.NoError(r.t, err)

	return got
}

// snapshot reads the Snapshot.
func (r *rig) snapshot() Snapshot {
	r.t.Helper()

	snap, err := r.svc.Snapshot(r.t.Context())
	require.NoError(r.t, err)

	return snap
}

// state reads the state of the passes.
func (r *rig) state() syncState {
	r.t.Helper()

	st, err := r.svc.loadState(r.t.Context())
	require.NoError(r.t, err)

	return st
}

// storedRow is what a row holds of a thread's item and its checks.
type storedRow struct {
	state     string
	checkedAt *time.Time
	seenAt    time.Time
}

// row reads the thread id's row of the account login; ok is false when there is none.
func (r *rig) row(login string, id int64) (storedRow, bool) {
	r.t.Helper()

	var out storedRow

	err := r.db.QueryRowContext(r.t.Context(),
		`SELECT subject_state, state_checked_at, seen_at FROM gh_threads WHERE account_login = ? AND thread_id = ?`,
		login, id).Scan(&out.state, &out.checkedAt, &out.seenAt)
	if errors.Is(err, sql.ErrNoRows) {
		return storedRow{}, false
	}

	require.NoError(r.t, err)

	return out, true
}

// rows counts the account login's rows.
func (r *rig) rows(login string) int {
	r.t.Helper()

	var n int

	require.NoError(r.t, r.db.QueryRowContext(r.t.Context(),
		`SELECT COUNT(*) FROM gh_threads WHERE account_login = ?`, login).Scan(&n))

	return n
}

// assertNoRequests checks that got holds no request at all.
func assertNoRequests(t *testing.T, got []listedRequest, msg string) {
	t.Helper()

	assert.Empty(t, got, msg)
}
