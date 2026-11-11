package review

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// Test values.
const (
	octoUser  = "octo-user"
	octoOther = "octo-other"
	nodePR    = "PR_fakeNodeId7"
	pullID    = int64(1)

	// cacheFile is #7's source file, whose diff the sheet shows.
	cacheFile = "cache.go"
)

var (
	errFakeRead  = errors.New("the fake read failed")
	errGateShut  = errors.New("the write gate is shut")
	headA        = strings.Repeat("a", 40)                       //nolint:gochecknoglobals // a fixed test value
	headB        = strings.Repeat("b", 40)                       //nolint:gochecknoglobals // a fixed test value
	testNow      = time.Date(2026, 11, 9, 17, 0, 0, 0, time.UTC) //nolint:gochecknoglobals // a fixed test value
	errNotCalled = errors.New("the fake was not set up for this call")
)

// seedItems stores the items the cache rows reference: n pull requests with ids 1 to n.
func seedItems(t *testing.T, db *sql.DB, n int) {
	t.Helper()

	exec(t, db, `INSERT INTO accounts (id, login, created_at, updated_at) VALUES (1, 'octo-user', ?, ?)`, testNow, testNow)
	exec(t, db, `INSERT INTO repos (id, owner, name, created_at, updated_at) VALUES (1, 'acme-org', 'example-repo', ?, ?)`,
		testNow, testNow)

	for i := 1; i <= n; i++ {
		exec(t, db, `INSERT INTO items (id, account_id, repo_id, kind, number, node_id, item_created_at, item_updated_at,
			fetched_at) VALUES (?, 1, 1, 'pr', ?, ?, ?, ?, ?)`, i, i, "PR_"+string(rune('a'+i)), testNow, testNow, testNow)
	}
}

// exec runs a statement on db.
func exec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()

	_, err := db.ExecContext(t.Context(), query, args...)
	require.NoError(t, err)
}

// storedPull is pull request #7 as the inbox stores it: someone else's, asking the user's
// review, mergeable by squash, with the user's write access.
func storedPull() StoredPull {
	return StoredPull{
		ID: pullID, Kind: item.KindPR, Number: 7, NodeID: nodePR, Title: "Add the widget cache",
		URL: "https://github.com/acme-org/example-repo/pull/7", AuthorLogin: octoOther, AuthorType: "User",
		AuthorAssociation: "MEMBER", HeadRef: "widget-cache", ReviewRequested: true, Owner: "acme-org",
		Name: "example-repo", DefaultBranch: "main", ViewerPermission: "WRITE", ViewerMergeMethod: "SQUASH",
		SquashMergeAllowed: true, MergeCommitAllowed: true, AutoMergeAllowed: true, Viewer: octoUser,
		HasDetails: true, Labels: []string{"enhancement"}, Additions: 3, Deletions: 1, ChangedFiles: 2, Commits: 1,
		ReviewDecision: "REVIEW_REQUIRED", HeadOID: headA,
	}
}

// fakeInbox is the inbox as the sheet reaches it: one stored pull request, and reads and
// actions run at once, with a token. An action GitHub refuses the token of runs once more,
// as the inbox's Actor does, and what both tries did is recorded.
type fakeInbox struct {
	mu     sync.Mutex
	pull   StoredPull
	pulled error
	reads  int
	acts   int
	tries  int
	acted  []Acted
	read   []PullRead

	// retryErr fails the account check of the retry with a fresh token, and recordErr the
	// record of what an action made happen, as the Actor joins it to the action's error.
	retryErr  error
	recordErr error
}

func (f *fakeInbox) Pull(context.Context, int64) (StoredPull, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.pull, f.pulled
}

func (f *fakeInbox) Read(
	ctx context.Context, _ int64, read func(context.Context, github.Token, Target) (PullRead, error),
) error {
	f.mu.Lock()
	f.reads++
	target := f.target()
	f.mu.Unlock()

	got, err := read(ctx, github.NewToken("fake-token-1"), target)
	if err != nil {
		return err
	}

	f.mu.Lock()
	f.read = append(f.read, got)
	f.mu.Unlock()

	return nil
}

func (f *fakeInbox) Act(
	ctx context.Context, _ int64, act func(context.Context, github.Token, Target) (Acted, error),
) error {
	f.mu.Lock()
	f.acts++
	target := f.target()
	f.mu.Unlock()

	acted, err := act(ctx, github.NewToken("fake-token-1"), target)
	tries := 1

	switch {
	case github.IsBadCredentials(err) && f.retryErr != nil:
		err = f.retryErr
	case github.IsBadCredentials(err):
		// The action keeps what it made happen across tries, so the second's Acted holds both.
		acted, err = act(ctx, github.NewToken("fake-token-2"), target)
		tries++
	}

	if f.recordErr != nil && (acted.Reviewed || acted.Merged) {
		err = errors.Join(err, uierr.New(
			"GitHub made the change, but the inbox could not record it. The next refresh catches it up.", f.recordErr,
		))
	}

	f.mu.Lock()
	f.acted, f.tries = append(f.acted, acted), tries
	f.mu.Unlock()

	return err
}

func (f *fakeInbox) target() Target {
	p := f.pull

	return Target{
		ID: p.ID, Ref: item.Ref{Repo: repo.Name{Owner: p.Owner, Repo: p.Name}, Number: p.Number, Kind: item.KindPR},
		NodeID: p.NodeID, Login: p.Viewer, Head: p.HeadOID, ReviewRequested: p.ReviewRequested,
	}
}

// The fake's names for GitHub's requests, as called and requests give them.
const (
	callReview  = "review"
	callDiff    = "diff"
	callState   = "state"
	callSubmit  = "submit"
	callMerge   = "merge"
	callAuto    = "auto"
	callUpdate  = "update"
	callComment = "comment"
)

// fakeGitHub answers the sheet's requests and counts them.
type fakeGitHub struct {
	mu sync.Mutex

	review   *github.PullReview
	reviews  []*github.PullReview
	diff     *github.PullDiff
	states   []*github.MergeState
	stateErr error
	// stateOK is how many merge-state reads succeed before stateErr fails the rest.
	stateOK int

	submitErr error
	// submitted runs once GitHub took a review, before the fake answers.
	submitted  func()
	mergeErr   error
	mergeOnce  error
	autoErr    error
	updateErr  error
	commentErr error

	calls map[string]int
	// order names every request, in the order they came.
	order    []string
	sent     []github.ReviewInput
	merges   []github.MergeInput
	autos    []string
	comments []string
}

func (g *fakeGitHub) FetchPullReview(context.Context, github.Token, string) (*github.PullReview, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.count(callReview)

	if len(g.reviews) > 0 {
		r := g.reviews[0]
		g.reviews = g.reviews[1:]

		return r, nil
	}

	if g.review == nil {
		return nil, errNotCalled
	}

	return g.review, nil
}

func (g *fakeGitHub) FetchPullDiff(context.Context, github.Token, item.Ref, string) (*github.PullDiff, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.count(callDiff)

	if g.diff == nil {
		return nil, errNotCalled
	}

	return g.diff, nil
}

func (g *fakeGitHub) FetchMergeState(ctx context.Context, _ github.Token, _ string) (*github.MergeState, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.count(callState)

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if g.stateErr != nil && g.calls[callState] > g.stateOK {
		return nil, g.stateErr
	}

	if len(g.states) == 0 {
		return open(github.MergeStateBlocked, headA), nil
	}

	s := g.states[0]
	if len(g.states) > 1 {
		g.states = g.states[1:]
	}

	return s, nil
}

func (g *fakeGitHub) SubmitReview(
	_ context.Context, _ github.Token, _ item.Ref, in github.ReviewInput,
) (*github.SubmittedReview, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.count(callSubmit)

	if g.submitErr != nil {
		return nil, g.submitErr
	}

	g.sent = append(g.sent, in)

	if g.submitted != nil {
		g.submitted()
	}

	return &github.SubmittedReview{ID: 1, State: "APPROVED", CommitID: in.CommitID, SubmittedAt: testNow}, nil
}

func (g *fakeGitHub) MergePull(_ context.Context, _ github.Token, _ item.Ref, in github.MergeInput) (*github.Merged, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.count(callMerge)

	if g.mergeOnce != nil {
		err := g.mergeOnce
		g.mergeOnce = nil

		return nil, err
	}

	if g.mergeErr != nil {
		return nil, g.mergeErr
	}

	g.merges = append(g.merges, in)

	return &github.Merged{SHA: headB}, nil
}

func (g *fakeGitHub) EnableAutoMerge(
	_ context.Context, _ github.Token, _ item.Ref, _, method, head string,
) (*github.AutoMerge, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.count(callAuto)

	if g.autoErr != nil {
		err := g.autoErr
		g.autoErr = nil

		return nil, err
	}

	g.autos = append(g.autos, method+"@"+head)

	return &github.AutoMerge{Method: method, EnabledAt: testNow}, nil
}

func (g *fakeGitHub) UpdateBranch(context.Context, github.Token, item.Ref, string) (*github.BranchUpdate, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.count(callUpdate)

	if g.updateErr != nil {
		return nil, g.updateErr
	}

	return &github.BranchUpdate{Message: "Updating pull request branch."}, nil
}

func (g *fakeGitHub) AddComment(_ context.Context, _ github.Token, _ item.Ref, body string) (*github.Comment, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.count(callComment)

	if g.commentErr != nil {
		return nil, g.commentErr
	}

	g.comments = append(g.comments, body)

	return &github.Comment{ID: 1}, nil
}

func (g *fakeGitHub) count(name string) {
	if g.calls == nil {
		g.calls = map[string]int{}
	}

	g.calls[name]++
	g.order = append(g.order, name)
}

func (g *fakeGitHub) called(name string) int {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.calls[name]
}

// requests names the requests made since the sheet last read the pull request, in order.
func (g *fakeGitHub) requests() []string {
	g.mu.Lock()
	defer g.mu.Unlock()

	last := -1

	for i, name := range g.order {
		if name == callReview || name == callDiff {
			last = i
		}
	}

	return append([]string{}, g.order[last+1:]...)
}

// fakeSettings is the settings the sheet follows.
type fakeSettings struct {
	pref MethodPref
	cfg  sensitive.Config
	err  error
}

func (s fakeSettings) MergeMethod(context.Context) (MethodPref, error) {
	if s.pref == "" {
		return PrefRepo, s.err
	}

	return s.pref, s.err
}

func (s fakeSettings) Sensitive(context.Context) (sensitive.Config, error) {
	if s.cfg.Scope == "" {
		return sensitive.DefaultConfig(), s.err
	}

	return s.cfg, s.err
}

// fakeReplies is the prompt library's replies.
type fakeReplies struct {
	list ReplyList
	fill string
	err  error
}

func (r fakeReplies) Offered(context.Context, ReplyAction) (ReplyList, error) {
	return r.list, r.err
}

func (r fakeReplies) Fill(context.Context, int64, int64) (string, error) {
	return r.fill, r.err
}

// harness is a Service over a fresh database holding pull request #7, with fakes.
type harness struct {
	svc    *Service
	db     *sql.DB
	inbox  *fakeInbox
	github *fakeGitHub
	clock  *testutil.Clock
	events *testutil.Events
	logs   *testutil.Logs
}

// newHarness returns a harness whose GitHub answers with review and diff at headA.
func newHarness(t *testing.T) *harness {
	t.Helper()

	db := testutil.DB(t)
	seedItems(t, db, 1)

	h := &harness{
		db: db, inbox: &fakeInbox{pull: storedPull()}, clock: testutil.NewClock(testNow), events: &testutil.Events{},
		github: &fakeGitHub{review: pullReview(headA, github.MergeStateClean), diff: pullDiff(headA)},
		logs:   &testutil.Logs{},
	}

	h.svc = h.service(fakeSettings{}, fakeReplies{})

	return h
}

// service builds the Service over the harness's fakes with settings and replies.
func (h *harness) service(settings Settings, replies Replies) *Service {
	return NewService(Config{
		Pool: h.db, Inbox: h.inbox, GitHub: h.github, Settings: settings, Replies: replies, Events: h.events,
		Clock: h.clock, Logger: h.logs.Logger(),
	})
}

// refreshed refreshes the pull request, which must succeed.
func (h *harness) refreshed(t *testing.T) Review {
	t.Helper()

	r, err := h.svc.Refresh(t.Context(), pullID)
	require.NoError(t, err)

	return r
}

// pullReview is what GitHub says about #7 at head in merge state status, which the user may
// update while it is behind.
func pullReview(head, status string) *github.PullReview {
	return &github.PullReview{
		Update: github.ItemUpdate{
			Found: true,
			Item: github.Item{
				IsPR: true, NodeID: nodePR, Number: 7, Title: "Add the widget cache", State: github.StateOpen,
				AuthorLogin: octoOther, RepoOwner: "acme-org", RepoName: "example-repo",
			},
			Details: github.ItemDetails{HeadOID: head},
		},
		Body: "Adds a cache.\n\n![chart](https://example.com/x.png)", BaseRef: "main", MergeState: status,
		ViewerCanEnableAutoMerge: true, ViewerCanUpdateBranch: status == github.MergeStateBehind,
		Reviews: []github.Review{{Author: "octo-reviewer", State: "APPROVED", Commit: headB, SubmittedAt: testNow}},
		Checks: []github.Check{
			{Kind: github.CheckKindRun, Name: "test", State: "SUCCESS", Outcome: github.CheckPassed, Required: true},
		},
	}
}

// pullDiff is #7's files at head: a source file, and a workflow.
func pullDiff(head string) *github.PullDiff {
	return &github.PullDiff{Head: head, Total: 3, Complete: true, Files: []github.PullFile{
		{Path: cacheFile, Status: "modified", Patch: "@@ -1,2 +1,3 @@\n line\n-old\n+new\n+added", HasPatch: true, Additions: 2, Deletions: 1},
		{Path: ".github/workflows/ci.yml", Status: "modified", Patch: "@@ -1 +1 @@\n-a\n+b", HasPatch: true, Additions: 1, Deletions: 1},
		{Path: "logo.png", Status: "added"},
	}}
}

// badCredentials is GitHub refusing the token.
func badCredentials() error {
	return &github.StatusError{StatusCode: http.StatusUnauthorized, Message: "Bad credentials"}
}
