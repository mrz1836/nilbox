package issues

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// Test values.
const (
	octoUser  = "octo-user"
	octoOther = "octo-other"
	octoThird = "octo-third"
	nodeIssue = "I_fakeNodeId12"
	issueID   = int64(1)
	issueRef  = "acme-org/example-repo#12"
	issueURL  = "https://github.com/acme-org/example-repo/issues/12"
	cursor    = "Y3Vyc29yOnYyOpK5"
	parserBug = "Parser drops the trailing newline"

	// exampleRepo is #12's repository, and closingText the text a close posts first.
	exampleRepo = "acme-org/example-repo"
	closingText = "Done."
)

var (
	errFakeRead  = errors.New("the fake read failed")
	errNotCalled = errors.New("the fake was not set up for this call")
	testNow      = time.Date(2026, 11, 9, 17, 0, 0, 0, time.UTC) //nolint:gochecknoglobals // a fixed test value
	openedAt     = testNow.Add(-72 * time.Hour)                  //nolint:gochecknoglobals // a fixed test value
	mineAt       = testNow.Add(-2 * time.Hour)                   //nolint:gochecknoglobals // a fixed test value
	latestAt     = testNow.Add(-time.Hour)                       //nolint:gochecknoglobals // a fixed test value
)

// storeIssues stores, as a refresh of the inbox would, count issues of
// acme-org/example-repo numbered from first, in the Assigned list, which the cache rows
// reference by id, and returns their ids.
func storeIssues(t *testing.T, db *sql.DB, first, count int) []int64 {
	t.Helper()

	stored := inbox.Fixture{Viewer: octoUser, At: testNow}

	for number := first; number < first+count; number++ {
		stored.Items = append(stored.Items, inbox.FixtureItem{Item: github.Item{
			Number: number, CreatedAt: testNow, UpdatedAt: testNow, RepoOwner: "acme-org", RepoName: "example-repo",
		}, Assigned: true})
	}

	ids, err := inbox.StoreFixture(t.Context(), db, stored)
	require.NoError(t, err)

	return ids
}

// storedIssue is issue #12 as the inbox stores it: someone else's, assigned to the user,
// who has write access to its repository.
func storedIssue() StoredIssue {
	return StoredIssue{
		ID: issueID, Kind: item.KindIssue, Number: 12, NodeID: nodeIssue, Title: parserBug, URL: issueURL,
		AuthorLogin: octoOther, AuthorType: "User", AuthorAssociation: "MEMBER", CreatedAt: openedAt,
		UpdatedAt: latestAt, Assigned: true, Owner: "acme-org", Name: "example-repo", DefaultBranch: "main",
		ViewerPermission: "WRITE", Viewer: octoUser, HasDetails: true,
		Labels: []github.Label{{Name: "bug", Color: "#d73a4a"}}, Comments: 2, Projects: []github.ProjectItem{},
		SubIssues: github.SubIssues{Total: 3, Completed: 1},
	}
}

// issueView is what GitHub reads about #12, open: a description, the user's comment, a pull
// request that mentioned it, and octo-other's comment, the newest, an hour ago; 40 items in
// all, so earlier ones are left to read.
func issueView() *github.IssueView {
	return &github.IssueView{
		Update: github.ItemUpdate{
			Found: true,
			Item: github.Item{
				NodeID: nodeIssue, Number: 12, Title: parserBug, URL: issueURL, State: github.StateOpen,
				CreatedAt: openedAt, UpdatedAt: latestAt, AuthorLogin: octoOther, AuthorType: "User",
				RepoOwner: "acme-org", RepoName: "example-repo",
			},
			Details: github.ItemDetails{
				Comments: 2, Labels: []github.Label{{Name: "bug", Color: "#d73a4a"}},
				SubIssues: github.SubIssues{Total: 3, Completed: 1},
			},
		},
		Body: "It drops the newline at the end of the file.", ViewerCanClose: true, ViewerCanUpdate: true,
		Type:         github.IssueType{Name: "Bug", Color: "#d73a4a", Description: "Something isn't working"},
		Assignees:    github.People{Logins: []string{octoUser}, Total: 1},
		Participants: github.People{Logins: []string{octoOther, octoUser}, Total: 3},
		Reactions:    []github.Reaction{{Content: "THUMBS_UP", Count: 4}},
		LatestComment: &github.CommentMark{
			ID: "IC_2", CreatedAt: latestAt, Author: octoOther,
		},
		Timeline: github.TimelinePage{Items: []github.TimelineItem{
			{
				Type: github.TimelineComment, ID: "IC_1", CreatedAt: mineAt, Actor: octoUser, ActorType: "User",
				Body: "Looking into it.", Mine: true, URL: issueURL + "#issuecomment-1",
			},
			{
				Type: github.TimelineCrossReferenced, ID: "CRE_1", CreatedAt: mineAt.Add(time.Minute), Actor: octoOther,
				ActorType: "User", Ref: &github.ItemRef{
					IsPR: true, Repo: exampleRepo, Number: 14, Title: "Keep the trailing newline",
					URL: "https://github.com/acme-org/example-repo/pull/14", State: github.StateOpen,
				},
			},
			{
				Type: github.TimelineComment, ID: "IC_2", CreatedAt: latestAt, Actor: octoOther, ActorType: "User",
				Body: "Can repro on 1.4.", URL: issueURL + "#issuecomment-2", Association: "MEMBER",
			},
		}, HasEarlier: true, Before: cursor, Total: 40},
	}
}

// later is what GitHub reads about #12 once octo-third commented, at, after the view:
// its timeline and its card's update move on.
func later(view *github.IssueView, at time.Time) *github.IssueView {
	next := *view
	next.Update.Item.UpdatedAt = at
	next.Update.Details.Comments++
	next.LatestComment = &github.CommentMark{ID: "IC_3", CreatedAt: at, Author: octoThird}
	next.Timeline.Items = append(slices.Clone(view.Timeline.Items), github.TimelineItem{
		Type: github.TimelineComment, ID: "IC_3", CreatedAt: at, Actor: octoThird, ActorType: "User",
		Body: "Here's a trace.",
	})
	next.Timeline.Total++

	return &next
}

// fakeInbox is the inbox as the sheet reaches it: one stored issue, and reads, peeks, and
// actions run at once, with a token. One GitHub refuses the token of runs once more, as the
// inbox's Actor does, and what every try did is recorded.
type fakeInbox struct {
	mu     sync.Mutex
	issue  StoredIssue
	issued error

	// projects is what each target says of reading the projects.
	projects bool

	reads int
	peeks int
	acts  int
	tries int
	acted []Acted
	read  []IssueRead

	// retryErr fails the account check of the retry with a fresh token, and recordErr the
	// record of what an action made happen, as the Actor joins it to the action's error.
	retryErr  error
	recordErr error
}

func (f *fakeInbox) Issue(context.Context, int64) (StoredIssue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.issue, f.issued
}

func (f *fakeInbox) Read(
	ctx context.Context, _ int64, read func(context.Context, github.Token, Target) (IssueRead, error),
) error {
	f.mu.Lock()
	f.reads++
	target := f.target()
	f.mu.Unlock()

	got, err := read(ctx, github.NewToken("fake-token-1"), target)
	if github.IsBadCredentials(err) {
		got, err = read(ctx, github.NewToken("fake-token-2"), target)
	}

	if err != nil {
		return err
	}

	f.mu.Lock()
	f.read = append(f.read, got)
	f.mu.Unlock()

	return nil
}

func (f *fakeInbox) Peek(ctx context.Context, _ int64, peek func(context.Context, github.Token, Target) error) error {
	f.mu.Lock()
	f.peeks++
	target := f.target()
	target.Projects = false
	f.mu.Unlock()

	return peek(ctx, github.NewToken("fake-token-1"), target)
}

func (f *fakeInbox) Act(
	ctx context.Context, _ int64, act func(context.Context, github.Token, Target) (Acted, error),
) error {
	f.mu.Lock()
	f.acts++
	target := f.target()
	target.Projects = false
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

	if f.recordErr != nil && acted.Closed {
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
	i := f.issue

	return Target{
		ID: i.ID, Ref: item.Ref{Repo: repo.Name{Owner: i.Owner, Repo: i.Name}, Number: i.Number, Kind: item.KindIssue},
		NodeID: i.NodeID, Login: i.Viewer, UpdatedAt: i.UpdatedAt, Assigned: i.Assigned, Projects: f.projects,
	}
}

// The fake's names for GitHub's requests, as calls and requests give them.
const (
	callView     = "view"
	callState    = "state"
	callTimeline = "timeline"
	callComment  = "comment"
	callClose    = "close"
	callLabels   = "labels"
	callPeople   = "people"
)

// fakeGitHub answers the sheet's requests and counts them.
type fakeGitHub struct {
	mu sync.Mutex

	// views are answered in turn, the last one again and again; viewErr fails every read.
	views    []*github.IssueView
	viewErr  error
	viewOpts []github.DetailOptions

	// gate, when set, holds every view read until it is closed, after telling arrived.
	gate    chan struct{}
	arrived chan struct{}

	// states are answered in turn, the last one again and again; stateErr fails every read.
	states   []*github.IssueState
	stateErr error

	timeline    *github.TimelinePage
	timelineErr error
	befores     []string

	commentErr  error
	commentOnce error
	comments    []string

	closeErr  error
	closeOnce error
	// closedAs is the reason GitHub says the issue is closed as, "" for the one asked, and
	// closedBy who it says closed it.
	closedAs string
	closedBy string
	reasons  []string

	// repoLabels answers a read of the repository's labels, and people a search for whom
	// the issue can be assigned to, which searched records.
	repoLabels *github.RepoLabels
	people     []github.Assignable
	searched   []string

	// editErr fails the edit whose name it is keyed by, as "+label", "-label:bug",
	// "+assignee", or "-assignee:octo-other", once; edits records each edit made.
	editErr map[string]error
	edits   []string

	calls map[string]int
	// order names every request, in the order they came.
	order []string
}

func (g *fakeGitHub) FetchRepoLabels(context.Context, github.Token, string) (*github.RepoLabels, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.count(callLabels)

	if g.repoLabels == nil {
		return nil, errNotCalled
	}

	return g.repoLabels, nil
}

func (g *fakeGitHub) FetchAssignable(_ context.Context, _ github.Token, _, query string) ([]github.Assignable, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.count(callPeople)
	g.searched = append(g.searched, query)

	return g.people, nil
}

func (g *fakeGitHub) AddLabels(_ context.Context, _ github.Token, _ item.Ref, names []string) ([]github.Label, error) {
	return nil, g.made("+label:" + strings.Join(names, ","))
}

func (g *fakeGitHub) RemoveLabel(_ context.Context, _ github.Token, _ item.Ref, name string) ([]github.Label, error) {
	return nil, g.made("-label:" + name)
}

func (g *fakeGitHub) AddAssignees(_ context.Context, _ github.Token, _ item.Ref, logins []string) (*github.Edited, error) {
	return &github.Edited{}, g.made("+assignee:" + strings.Join(logins, ","))
}

func (g *fakeGitHub) RemoveAssignee(_ context.Context, _ github.Token, _ item.Ref, login string) (*github.Edited, error) {
	return &github.Edited{}, g.made("-assignee:" + login)
}

func (g *fakeGitHub) FetchIssueView(
	ctx context.Context, _ github.Token, _ string, opts github.DetailOptions,
) (*github.IssueView, error) {
	g.mu.Lock()
	gate, arrived := g.gate, g.arrived
	g.mu.Unlock()

	if gate != nil {
		arrived <- struct{}{}

		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	g.count(callView)
	g.viewOpts = append(g.viewOpts, opts)

	if g.viewErr != nil {
		return nil, g.viewErr
	}

	if len(g.views) == 0 {
		return nil, errNotCalled
	}

	v := g.views[0]
	if len(g.views) > 1 {
		g.views = g.views[1:]
	}

	return v, nil
}

func (g *fakeGitHub) FetchIssueState(context.Context, github.Token, string) (*github.IssueState, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.count(callState)

	if g.stateErr != nil {
		return nil, g.stateErr
	}

	if len(g.states) == 0 {
		return openState(seenComment()), nil
	}

	s := g.states[0]
	if len(g.states) > 1 {
		g.states = g.states[1:]
	}

	return s, nil
}

func (g *fakeGitHub) FetchTimeline(
	_ context.Context, _ github.Token, _ item.Kind, _, before string,
) (*github.TimelinePage, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.count(callTimeline)
	g.befores = append(g.befores, before)

	if g.timelineErr != nil {
		return nil, g.timelineErr
	}

	if g.timeline == nil {
		return nil, errNotCalled
	}

	return g.timeline, nil
}

func (g *fakeGitHub) AddComment(_ context.Context, _ github.Token, _ item.Ref, body string) (*github.Comment, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.count(callComment)

	if g.commentOnce != nil {
		err := g.commentOnce
		g.commentOnce = nil

		return nil, err
	}

	if g.commentErr != nil {
		return nil, g.commentErr
	}

	g.comments = append(g.comments, body)

	return &github.Comment{ID: 3}, nil
}

func (g *fakeGitHub) CloseIssue(_ context.Context, _ github.Token, _ item.Ref, reason string) (*github.IssueClosed, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.count(callClose)

	if g.closeOnce != nil {
		err := g.closeOnce
		g.closeOnce = nil

		return nil, err
	}

	if g.closeErr != nil {
		return nil, g.closeErr
	}

	g.reasons = append(g.reasons, reason)

	if g.closedAs != "" {
		reason = g.closedAs
	}

	return &github.IssueClosed{Reason: reason, ClosedBy: g.closedBy}, nil
}

// made records the edit named name, or fails it once as editErr says.
func (g *fakeGitHub) made(name string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.count(name)

	if err := g.editErr[name]; err != nil {
		delete(g.editErr, name)

		return err
	}

	g.edits = append(g.edits, name)

	return nil
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

// requests names the requests made since the sheet last read the issue, in order.
func (g *fakeGitHub) requests() []string {
	g.mu.Lock()
	defer g.mu.Unlock()

	last := -1

	for i, name := range g.order {
		if name == callView {
			last = i
		}
	}

	return append([]string{}, g.order[last+1:]...)
}

// seenComment is octo-other's comment the sheet read, the newest when the user looked.
func seenComment() github.CommentMark {
	return github.CommentMark{ID: "IC_2", CreatedAt: latestAt, Author: octoOther}
}

// openState is #12 read before a close: open, the user may close it, and its newest
// comments are the user's own and then recent, oldest first.
func openState(recent ...github.CommentMark) *github.IssueState {
	mine := github.CommentMark{ID: "IC_1", CreatedAt: mineAt, Author: octoUser, Mine: true}

	return &github.IssueState{
		Found: true, State: github.StateOpen, CanClose: true, Comments: 1 + len(recent),
		Recent: append([]github.CommentMark{mine}, recent...),
	}
}

// fakeReplies is the prompt library's replies: the ones it offers, or err, and each
// reply's filled-in text by its ID.
type fakeReplies struct {
	offered []Reply
	err     error
	filled  map[int64]string
}

func (f *fakeReplies) Offered(context.Context) ([]Reply, error) {
	return f.offered, f.err
}

func (f *fakeReplies) Fill(_ context.Context, replyID, _ int64) (string, error) {
	text, ok := f.filled[replyID]
	if !ok {
		return "", errNoSuchReply
	}

	return text, nil
}

// errNoSuchReply is the fake library's answer for a reply it doesn't hold.
var errNoSuchReply = errors.New("no such reply")

// harness is a Service over a fresh database holding issue #12, with fakes.
type harness struct {
	svc     *Service
	db      *sql.DB
	inbox   *fakeInbox
	github  *fakeGitHub
	replies *fakeReplies
	clock   *testutil.Clock
	events  *testutil.Events
	logs    *testutil.Logs
}

// newHarness returns a harness whose GitHub answers with issueView.
func newHarness(t *testing.T) *harness {
	t.Helper()

	db := testutil.DB(t)
	require.Equal(t, []int64{issueID}, storeIssues(t, db, 12, 1), "the issue the sheet shows")

	h := &harness{
		db: db, inbox: &fakeInbox{issue: storedIssue()}, github: &fakeGitHub{views: []*github.IssueView{issueView()}},
		replies: &fakeReplies{}, clock: testutil.NewClock(testNow), events: &testutil.Events{}, logs: &testutil.Logs{},
	}

	h.svc = NewService(Config{
		Pool: db, Inbox: h.inbox, GitHub: h.github, Replies: h.replies, Events: h.events, Clock: h.clock,
		Logger: h.logs.Logger(),
	})

	return h
}

// readHarness is a harness whose issue the sheet has read.
func readHarness(t *testing.T) *harness {
	t.Helper()

	h := newHarness(t)
	h.refreshed(t)

	return h
}

// refreshed refreshes the issue, which must succeed.
func (h *harness) refreshed(t *testing.T) Issue {
	t.Helper()

	is, err := h.svc.Refresh(t.Context(), issueID)
	require.NoError(t, err)

	return is
}

// opened opens the issue, which must succeed.
func (h *harness) opened(t *testing.T) Issue {
	t.Helper()

	is, err := h.svc.Open(t.Context(), issueID)
	require.NoError(t, err)

	return is
}

// act runs req for #12, pinned to what the sheet read, which must not fail.
func (h *harness) act(t *testing.T, req ActRequest) Result {
	t.Helper()

	req.ItemID = issueID
	if req.Seen == (Seen{}) {
		req.Seen = sawLatest()
	}

	res, err := h.svc.Act(t.Context(), req)
	require.NoError(t, err)

	return res
}

// cached returns the sheet's cache of #12, nil when it holds none.
func (h *harness) cached(t *testing.T) *cached {
	t.Helper()

	c, err := newCacheStore(h.db).load(t.Context(), issueID)
	require.NoError(t, err)

	return c
}

// steps returns the steps the actions reported, in order.
func (h *harness) steps(t *testing.T) []string {
	t.Helper()

	all := h.events.All()
	out := make([]string, 0, len(all))

	for _, e := range all {
		p, ok := e.(event.IssueProgress)
		require.True(t, ok, "only an action's steps are emitted: %T", e)
		require.Equal(t, issueID, p.ItemID)

		out = append(out, p.Step)
	}

	return out
}

// badCredentials is GitHub refusing the token.
func badCredentials() error {
	return &github.StatusError{StatusCode: http.StatusUnauthorized, Message: "Bad credentials"}
}
