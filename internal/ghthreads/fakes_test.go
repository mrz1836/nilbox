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

	"github.com/mrz1836/nilbox/internal/budget"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
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

	// kindDone and kindRead name a thread marked done (a DELETE) and one marked read (a
	// PATCH), as the fake records them and the ledger's action says.
	kindDone = "done"
	kindRead = "read"
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

	// comment is the latest comment's path under the repository's API URL, as
	// "issues/comments/900"; "" gives the subject's own URL, as a merge or a close leaves
	// it, and noComment gives none.
	comment   string
	noComment bool
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
	// kind is list, viewer, states, comment, done (a DELETE of a thread), or read (a PATCH
	// of one).
	kind string

	// page is a listing's page, and ifNoneMatch the ETag it was sent with.
	page        int
	ifNoneMatch string

	// refs are the items a states query asked about, as owner/name#number.
	refs []string

	// id is the thread a write was about, or the comment read.
	id int64

	// at is when a write arrived, on the rig's clock.
	at time.Time
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
	// can't find; statesStatus answers every states query with that status when set; and
	// closedAt is when an item closed, start when missing.
	states       map[string]string
	statesStatus int
	closedAt     map[string]time.Time

	// commenters are the types of the latest comments' authors by comment ID (User, Bot,
	// or "" for a deleted account); writeStatus answers every write to a thread with that
	// status when set, and commentStatus every read of a comment.
	commenters    map[int64]string
	writeStatus   int
	commentStatus int

	// gate is the client's chore lane, and now the clock writes are stamped with.
	gate github.ChoreGate
	now  func() time.Time

	got []listedRequest
}

// newFakeGitHub returns a fake GitHub for viewerLogin, with no notifications, whose
// client's chore lane allows every clear.
func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()

	f := &fakeGitHub{
		viewer: viewerLogin, states: map[string]string{}, closedAt: map[string]time.Time{}, poll: "60",
		commenters: map[int64]string{}, gate: &fakeGate{}, now: time.Now,
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)

	return f
}

// client returns a client of the fake GitHub's, with its chore lane.
func (f *fakeGitHub) client() *github.Client {
	f.mu.Lock()
	defer f.mu.Unlock()

	return &github.Client{BaseURL: f.srv.URL, HTTP: f.srv.Client(), Chores: f.gate}
}

// commented gives the thread id a new latest comment, commentID by an author of typ, at,
// unread, so it moves up the listing.
func (f *fakeGitHub) commented(id, commentID int64, typ string, at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()

	i := slices.IndexFunc(f.threads, func(t fakeThread) bool { return t.id == id })
	f.threads[i].updated, f.threads[i].unread = at, true
	f.threads[i].comment, f.threads[i].noComment = "issues/comments/"+strconv.FormatInt(commentID, 10), false
	f.commenters[commentID] = typ
}

// read marks the thread id read, as the user reading it on GitHub does.
func (f *fakeGitHub) read(id int64) {
	f.mu.Lock()
	defer f.mu.Unlock()

	i := slices.IndexFunc(f.threads, func(t fakeThread) bool { return t.id == id })
	f.threads[i].unread = false
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

// threadWrite reads a write to a thread's path.
var threadWrite = regexp.MustCompile(`^/notifications/threads/(\d+)$`)

// commentRead reads a read of a comment's path.
var commentRead = regexp.MustCompile(`^/repos/[^/]+/[^/]+/(?:issues|pulls)/comments/(\d+)$`)

// serve answers one request.
func (f *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/notifications":
		f.serveListing(w, r)
	case r.URL.Path == "/graphql":
		f.serveGraphQL(w, r)
	case threadWrite.MatchString(r.URL.Path):
		f.serveWrite(w, r)
	case commentRead.MatchString(r.URL.Path):
		f.serveComment(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// serveWrite answers a thread marked done (DELETE, 204), which takes it out of the listing,
// or read (PATCH, 205); 404 for a thread it doesn't list.
func (f *fakeGitHub) serveWrite(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(threadWrite.FindStringSubmatch(r.URL.Path)[1], 10, 64)

	f.mu.Lock()
	defer f.mu.Unlock()

	kind := map[string]string{http.MethodDelete: kindDone, http.MethodPatch: kindRead}[r.Method]
	f.got = append(f.got, listedRequest{kind: kind, id: id, at: f.now()})

	if f.writeStatus != 0 {
		w.WriteHeader(f.writeStatus)
		_, _ = io.WriteString(w, `{"message": "Server Error"}`)

		return
	}

	i := slices.IndexFunc(f.threads, func(t fakeThread) bool { return t.id == id })
	if i < 0 {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message": "Not Found"}`)

		return
	}

	if r.Method == http.MethodDelete {
		f.threads = slices.Delete(f.threads, i, i+1)

		w.WriteHeader(http.StatusNoContent)

		return
	}

	f.threads[i].unread = false

	w.WriteHeader(http.StatusResetContent)
}

// serveComment answers a comment's author, as commenters say.
func (f *fakeGitHub) serveComment(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(commentRead.FindStringSubmatch(r.URL.Path)[1], 10, 64)

	f.mu.Lock()
	defer f.mu.Unlock()

	f.got = append(f.got, listedRequest{kind: "comment", id: id})

	if f.commentStatus != 0 {
		w.WriteHeader(f.commentStatus)
		_, _ = io.WriteString(w, `{"message": "API rate limit exceeded for user ID 1."}`)

		return
	}

	typ, ok := f.commenters[id]
	if !ok {
		w.WriteHeader(http.StatusNotFound)

		return
	}

	if typ == "" {
		_, _ = io.WriteString(w, `{"id": 1, "user": null}`)

		return
	}

	_, _ = fmt.Fprintf(w, `{"id": 1, "user": {"login": "someone", "type": %q}}`, typ)
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

		switch {
		case t.noComment:
			subject["latest_comment_url"] = nil
		case t.comment != "":
			subject["latest_comment_url"] = f.srv.URL + "/repos/" + t.repo + "/" + t.comment
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

		data[m[1]] = f.stateNode(ref)
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

// stateNode is what the states query answers of ref, as owner/name#number: nil for one
// GitHub can't find. The caller holds the lock.
func (f *fakeGitHub) stateNode(ref string) any {
	state, ok := f.states[ref]
	if !ok {
		return nil
	}

	closed, ok := f.closedAt[ref]
	if !ok {
		closed = start
	}

	node := map[string]any{"__typename": "PullRequest", "prState": state, "title": "An item", "url": ""}
	if state != github.StateOpen {
		node["closedAt"] = closed.Format(time.RFC3339)
	}

	if state == github.StateMerged {
		node["mergedAt"] = closed.Format(time.RFC3339)
	}

	return map[string]any{"item": node}
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

// fakeRules are the user's rules: the defaults, with reading on until turned off, unless
// changed.
type fakeRules struct {
	off atomic.Bool

	mu      sync.Mutex
	changed *threadrules.Rules
}

// ThreadRules returns the rules, with Read as set.
func (r *fakeRules) ThreadRules(context.Context) (threadrules.Rules, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	rules := threadrules.Defaults()
	if r.changed != nil {
		rules = *r.changed
	}

	rules.Read = rules.Read && !r.off.Load()

	return rules, nil
}

// change changes the rules with edit.
func (r *fakeRules) change(edit func(*threadrules.Rules)) {
	r.mu.Lock()
	defer r.mu.Unlock()

	rules := threadrules.Defaults()
	if r.changed != nil {
		rules = *r.changed
	}

	edit(&rules)
	r.changed = &rules
}

// fakeGate is a chore lane that allows every clear, unless told to refuse.
type fakeGate struct {
	mu      sync.Mutex
	refuse  error
	allowed int

	// paced is how many chores to come it refuses for the app's pace before it allows one.
	paced int
}

// AllowChore allows the clear, or refuses it as told.
func (g *fakeGate) AllowChore(string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.refuse != nil {
		return g.refuse
	}

	if g.paced > 0 {
		g.paced--

		return budget.ErrWritePace
	}

	g.allowed++

	return nil
}

// HoldWrites hears nothing.
func (*fakeGate) HoldWrites(time.Duration) {}

// fakeHolds is the toast setting, in seconds, or a failure to read it.
type fakeHolds struct {
	seconds int
	err     error
}

// ToastSeconds returns the toast setting.
func (h fakeHolds) ToastSeconds(context.Context) (int, error) {
	return h.seconds, h.err
}

// rig is a Service over a fresh database, a fake GitHub, and a clock the test moves.
type rig struct {
	t      *testing.T
	gh     *fakeGitHub
	db     *sql.DB
	clock  *testutil.Clock
	tokens *fakeTokens
	rules  *fakeRules
	holds  HoldSource
	events *testutil.Events
	svc    *Service
}

// newRig returns a rig with no notifications, at start, whose toasts stay 12 seconds.
func newRig(t *testing.T) *rig {
	t.Helper()

	r := &rig{
		t: t, gh: newFakeGitHub(t), db: testutil.DB(t), clock: testutil.NewClock(start),
		tokens: &fakeTokens{value: "test-token-not-real"}, rules: &fakeRules{}, holds: fakeHolds{seconds: 12},
		events: &testutil.Events{},
	}
	r.gh.now = r.clock.Now
	r.restart()

	return r
}

// restart builds the Service again over the same database, as the app's next start does.
func (r *rig) restart() {
	r.svc = NewService(Config{
		Pool: r.db, Tokens: r.tokens, Client: r.gh.client(), Rules: r.rules, Holds: r.holds, Clock: r.clock,
		Events: r.events,
	})
}

// clearing turns clearing on its own on, as the user does in Settings.
func (r *rig) clearing() {
	r.rules.change(func(rules *threadrules.Rules) { rules.Auto = true })
}

// settle tells the Service what the inbox told, then runs the run it was nudged to, and
// returns the requests it sent.
func (r *rig) settle(outcomes ...Outcome) []listedRequest {
	r.t.Helper()

	NewSettler(r.svc).Settle(r.t.Context(), outcomes)

	return r.nudged()
}

// nudged runs the run the Service was nudged to, which must be waiting, and returns the
// requests it sent.
func (r *rig) nudged() []listedRequest {
	r.t.Helper()

	req, ok := r.svc.takeNudge()
	require.True(r.t, ok, "a nudge waits")
	require.NoError(r.t, r.svc.runPass(r.t.Context(), req))

	return r.gh.requests()
}

// send runs a run that sends the clears due, as the Syncer's timer does, and returns the
// requests it sent.
func (r *rig) send() []listedRequest {
	r.t.Helper()

	require.NoError(r.t, r.svc.runPass(r.t.Context(), passRequest{send: true}))

	return r.gh.requests()
}

// untilSend returns how long until the next clear may be sent, which must wait on a time.
func (r *rig) untilSend() time.Duration {
	r.t.Helper()

	wait, ok := r.svc.untilSend()
	require.True(r.t, ok, "a send waits on a time")

	return wait
}

// ledgerRow is what the ledger holds of one clear.
type ledgerRow struct {
	id        int64
	thread    int64
	trigger   string
	action    string
	state     string
	sendAfter time.Time
	queuedAt  time.Time
	attempts  int
	err       string
	reason    string
}

// ledger returns the ledger's rows, the first queued first.
func (r *rig) ledger() []ledgerRow {
	r.t.Helper()

	rows, err := r.db.QueryContext(r.t.Context(), `SELECT id, thread_id, trigger, action, state, send_after,
		queued_at, attempts, error, skip_reason FROM gh_thread_clears ORDER BY id`)
	require.NoError(r.t, err)

	defer func() { require.NoError(r.t, rows.Close()) }()

	var out []ledgerRow

	for rows.Next() {
		var l ledgerRow
		require.NoError(r.t, rows.Scan(&l.id, &l.thread, &l.trigger, &l.action, &l.state, &l.sendAfter, &l.queuedAt,
			&l.attempts, &l.err, &l.reason))

		out = append(out, l)
	}

	require.NoError(r.t, rows.Err())

	return out
}

// writes returns the writes to threads among got: those marked done or read.
func writes(got []listedRequest) []listedRequest {
	return slices.DeleteFunc(slices.Clone(got), func(r listedRequest) bool { return r.kind != kindDone && r.kind != kindRead })
}

// ofKind returns the requests of kind among got.
func ofKind(got []listedRequest, kind string) []listedRequest {
	return slices.DeleteFunc(slices.Clone(got), func(r listedRequest) bool { return r.kind != kind })
}

// idsOf returns the IDs the requests among got were about.
func idsOf(got []listedRequest) []int64 {
	out := make([]int64, 0, len(got))

	for _, r := range got {
		out = append(out, r.id)
	}

	return out
}

// mergedThreads returns n pull request threads, each about its own pull request, which the
// fake says GitHub merged at start, the newest updated at start and each a second older.
func (f *fakeGitHub) mergedThreads(n int) []fakeThread {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]fakeThread, 0, n)

	for i := range n {
		f.states[exampleRef(strconv.Itoa(i+1))] = github.StateMerged
		out = append(out, prThread(firstID+int64(i), i+1, start.Add(-time.Duration(i)*time.Second)))
	}

	return out
}

// outcome is what the inbox tells of the fixtures' item number of kind.
func outcome(kind OutcomeKind, itemKind item.Kind, number int, at time.Time) Outcome {
	return Outcome{
		Kind: kind, Login: viewerLogin, At: at,
		Ref: item.Ref{Repo: repo.Name{Owner: owner, Repo: exampleRepo}, Number: number, Kind: itemKind},
	}
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
