package core

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/budget"
	"github.com/mrz1836/nilbox/internal/ghthreads"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/issues"
	"github.com/mrz1836/nilbox/internal/item"
	prefsstore "github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/review"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// The GitHub step rig's values: the account signed in, the person who opened the items, and
// the two heads a pull request is read at.
const (
	ghViewer = "octo-user"
	ghAuthor = "octo-other"
	ghHeadA  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	ghHeadB  = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	// ghRepo is the repository every item of the rig is in, but ghReadOnly's.
	ghRepo     = "example-repo"
	ghReadOnly = "read-only-repo"

	// The items' numbers: a person's pull request, Dependabot's, an issue, and an issue in a
	// repository the account can only read.
	ghPullNumber   = 7
	ghBotNumber    = 8
	ghIssueNumber  = 12
	ghLockedNumber = 3
)

// ghNow is the time the rig's clock stands at, and every read says the items were last
// active an hour before.
func ghNow() time.Time {
	return time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)
}

// ghNode is the node ID GitHub gives the item numbered n.
func ghNode(n int) string {
	return "NODE_gh" + strconv.Itoa(n)
}

// ghTokens hands out one token, as a signed-in GitHub CLI would, and forgets nothing.
type ghTokens struct{}

func (ghTokens) Token(context.Context) (github.Token, error) {
	return github.NewToken("gho_fake_token"), nil
}

func (ghTokens) Forget(github.Token) {}

// ghInboxClient is GitHub as the inbox's Actor reaches it here: it says the token is the
// account the inbox shows. Any other request panics, as one the test didn't expect.
type ghInboxClient struct {
	inbox.Client
}

func (ghInboxClient) FetchViewer(context.Context, github.Token) (string, error) { return ghViewer, nil }

// ghCalls records the requests a fake GitHub was asked, in order, each with the purpose
// its context carried.
type ghCalls struct {
	mu       sync.Mutex
	calls    []string
	purposes []github.Purpose
}

// heard records the request call, made with ctx.
func (c *ghCalls) heard(ctx context.Context, call string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.calls = append(c.calls, call)
	c.purposes = append(c.purposes, github.PurposeOf(ctx))
}

// asked returns the requests made, in order.
func (c *ghCalls) asked() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	return append([]string{}, c.calls...)
}

// writes returns the requests made that write, in order: every one but a read.
func (c *ghCalls) writes() []string {
	out := []string{}

	for _, call := range c.asked() {
		if !strings.HasPrefix(call, "read ") && !strings.HasPrefix(call, "diff ") && !strings.HasPrefix(call, "state ") &&
			!strings.HasPrefix(call, "own ") && !strings.HasPrefix(call, "timeline ") {
			out = append(out, call)
		}
	}

	return out
}

// allAutopilots reports whether there were requests, and every one said it is Autopilot's.
func (c *ghCalls) allAutopilots() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, p := range c.purposes {
		if p != github.PurposeAutopilot {
			return false
		}
	}

	return len(c.purposes) > 0
}

// ghPullHub is GitHub as the review sheet reaches it: each pull request's read by node ID,
// its files, and its merge state, and every write recorded and answered. A request the test
// didn't expect panics through the nil review.GitHub.
type ghPullHub struct {
	review.GitHub
	ghCalls

	mu      sync.Mutex
	reads   map[string]*github.PullReview
	readErr error
	states  map[string]*github.MergeState

	// owns are what an own merge-state read adds to each pull request's merge state, by
	// node ID; none for a pull request not named.
	owns map[string]*github.OwnMerge

	// writeErr fails every write; reviews are the reviews GitHub took.
	writeErr error
	reviews  []github.ReviewInput
}

func (g *ghPullHub) FetchPullReview(ctx context.Context, _ github.Token, id, _ string) (*github.PullReview, error) {
	g.heard(ctx, "read "+id)

	g.mu.Lock()
	defer g.mu.Unlock()

	if g.readErr != nil {
		return nil, g.readErr
	}

	return g.reads[id], nil
}

func (g *ghPullHub) FetchPullDiff(ctx context.Context, _ github.Token, ref item.Ref, head string) (*github.PullDiff, error) {
	g.heard(ctx, "diff #"+strconv.Itoa(ref.Number))

	return &github.PullDiff{Head: head, Total: 1, Complete: true, Files: []github.PullFile{{
		Path: "cache.go", Status: "modified", Patch: "@@ -1,2 +1,3 @@\n line\n-old\n+new\n+added", HasPatch: true,
		Additions: 2, Deletions: 1,
	}}}, nil
}

func (g *ghPullHub) FetchMergeState(ctx context.Context, _ github.Token, id string) (*github.MergeState, error) {
	g.heard(ctx, "state "+id)

	g.mu.Lock()
	defer g.mu.Unlock()

	if s, ok := g.states[id]; ok {
		return s, nil
	}

	return &github.MergeState{Found: true, Status: github.MergeStateClean, Head: ghHeadA, State: github.StateOpen}, nil
}

// FetchOwnMergeState answers as FetchMergeState does, with what owns holds for the pull
// request beside it.
func (g *ghPullHub) FetchOwnMergeState(ctx context.Context, _ github.Token, id string) (*github.MergeState, error) {
	g.heard(ctx, "own "+id)

	g.mu.Lock()
	defer g.mu.Unlock()

	out := github.MergeState{Found: true, Status: github.MergeStateClean, Head: ghHeadA, State: github.StateOpen}
	if s, ok := g.states[id]; ok {
		out = *s
	}

	if own, ok := g.owns[id]; ok {
		copied := *own
		out.Own = &copied
	}

	return &out, nil
}

func (g *ghPullHub) SubmitReview(
	ctx context.Context, _ github.Token, ref item.Ref, in github.ReviewInput,
) (*github.SubmittedReview, error) {
	g.heard(ctx, "review #"+strconv.Itoa(ref.Number)+" "+in.Event)

	g.mu.Lock()
	defer g.mu.Unlock()

	if g.writeErr != nil {
		return nil, g.writeErr
	}

	g.reviews = append(g.reviews, in)

	return &github.SubmittedReview{ID: 1, State: in.Event, CommitID: in.CommitID, SubmittedAt: ghNow()}, nil
}

func (g *ghPullHub) MergePull(ctx context.Context, _ github.Token, ref item.Ref, in github.MergeInput,
) (*github.Merged, error) {
	g.heard(ctx, "merge #"+strconv.Itoa(ref.Number)+" "+in.Method)

	if err := g.failing(); err != nil {
		return nil, err
	}

	return &github.Merged{SHA: ghHeadB}, nil
}

func (g *ghPullHub) EnableAutoMerge(
	ctx context.Context, _ github.Token, ref item.Ref, _, method, _ string,
) (*github.AutoMerge, error) {
	g.heard(ctx, "auto-merge #"+strconv.Itoa(ref.Number)+" "+method)

	if err := g.failing(); err != nil {
		return nil, err
	}

	return &github.AutoMerge{Method: method, EnabledAt: ghNow()}, nil
}

func (g *ghPullHub) UpdateBranch(ctx context.Context, _ github.Token, ref item.Ref, _ string) (*github.BranchUpdate, error) {
	g.heard(ctx, "update #"+strconv.Itoa(ref.Number))

	if err := g.failing(); err != nil {
		return nil, err
	}

	return &github.BranchUpdate{Message: "Updating pull request branch."}, nil
}

func (g *ghPullHub) AddComment(ctx context.Context, _ github.Token, ref item.Ref, body string) (*github.Comment, error) {
	g.heard(ctx, "comment #"+strconv.Itoa(ref.Number)+": "+body)

	if err := g.failing(); err != nil {
		return nil, err
	}

	return &github.Comment{ID: 1}, nil
}

func (g *ghPullHub) AddLabels(ctx context.Context, _ github.Token, ref item.Ref, names []string) ([]github.Label, error) {
	g.heard(ctx, "label #"+strconv.Itoa(ref.Number)+" +"+strings.Join(names, ","))

	if err := g.failing(); err != nil {
		return nil, err
	}

	return []github.Label{}, nil
}

func (g *ghPullHub) RemoveLabel(ctx context.Context, _ github.Token, ref item.Ref, name string) ([]github.Label, error) {
	g.heard(ctx, "label #"+strconv.Itoa(ref.Number)+" -"+name)

	if err := g.failing(); err != nil {
		return nil, err
	}

	return []github.Label{}, nil
}

func (g *ghPullHub) FetchTimeline(ctx context.Context, _ github.Token, _ item.Kind, id, before string,
) (*github.TimelinePage, error) {
	g.heard(ctx, "timeline "+id+" before "+before)

	return &github.TimelinePage{Total: 3, Items: []github.TimelineItem{{
		Type: github.TimelineComment, ID: "IC_1", CreatedAt: ghNow().Add(-48 * time.Hour), Actor: ghAuthor,
		ActorType: "User", Body: "An early comment.",
	}}}, nil
}

// failing returns the error every write fails with, nil while none does.
func (g *ghPullHub) failing() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.writeErr
}

// setRead has GitHub read the pull request numbered n as read says from now on.
func (g *ghPullHub) setRead(n int, read *github.PullReview) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.reads[ghNode(n)] = read
}

// failWrites has every write fail with err from now on.
func (g *ghPullHub) failWrites(err error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.writeErr = err
}

// failReads has every read of a pull request fail with err from now on.
func (g *ghPullHub) failReads(err error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.readErr = err
}

// ghIssueHub is GitHub as the issue sheet reaches it: each issue's read by node ID, and
// every write recorded and answered. A request the test didn't expect panics through the
// nil issues.GitHub.
type ghIssueHub struct {
	issues.GitHub
	ghCalls

	mu       sync.Mutex
	views    map[string]*github.IssueView
	readErr  error
	writeErr error
}

func (g *ghIssueHub) FetchIssueView(ctx context.Context, _ github.Token, id string, _ github.DetailOptions,
) (*github.IssueView, error) {
	g.heard(ctx, "read "+id)

	g.mu.Lock()
	defer g.mu.Unlock()

	if g.readErr != nil {
		return nil, g.readErr
	}

	return g.views[id], nil
}

func (g *ghIssueHub) AddComment(ctx context.Context, _ github.Token, ref item.Ref, body string) (*github.Comment, error) {
	g.heard(ctx, "comment #"+strconv.Itoa(ref.Number)+": "+body)

	if err := g.failing(); err != nil {
		return nil, err
	}

	return &github.Comment{ID: 2}, nil
}

func (g *ghIssueHub) AddLabels(ctx context.Context, _ github.Token, ref item.Ref, names []string) ([]github.Label, error) {
	g.heard(ctx, "label #"+strconv.Itoa(ref.Number)+" +"+strings.Join(names, ","))

	if err := g.failing(); err != nil {
		return nil, err
	}

	return []github.Label{}, nil
}

func (g *ghIssueHub) RemoveLabel(ctx context.Context, _ github.Token, ref item.Ref, name string) ([]github.Label, error) {
	g.heard(ctx, "label #"+strconv.Itoa(ref.Number)+" -"+name)

	if err := g.failing(); err != nil {
		return nil, err
	}

	return []github.Label{}, nil
}

// failing returns the error every write fails with, nil while none does.
func (g *ghIssueHub) failing() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.writeErr
}

// setView has GitHub read the issue numbered n as view says from now on.
func (g *ghIssueHub) setView(n int, view *github.IssueView) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.views[ghNode(n)] = view
}

// ghPreviews fills a reply in for the rig's items as a launch on them would: the item's
// kind, number, and author.
type ghPreviews struct {
	kinds map[int64]item.Kind
}

func (p ghPreviews) PreviewData(_ context.Context, itemID int64) (prompts.Data, error) {
	kind := p.kinds[itemID]

	return prompts.Data{Kind: kind.Noun(), IsPR: kind == item.KindPR, Number: int(itemID), Author: ghAuthor}, nil
}

// ghRig is the GitHub step over the real sheets: the inbox's store, Actor, and service on a
// fresh database holding the rig's items, the review and issue sheets over fake GitHubs, and
// the prompt library with its starters.
type ghRig struct {
	steps  githubSteps
	pulls  *ghPullHub
	issues *ghIssueHub
	store  *inbox.Store
	clock  *testutil.Clock

	// The items' stored IDs.
	pull, bot, issue, locked int64
}

// newGHRig stores the rig's items and builds the GitHub step over them, with GitHub reading
// each item open, unchanged since the inbox stored it, at ghHeadA.
func newGHRig(t *testing.T) *ghRig {
	t.Helper()

	ctx := t.Context()
	db := testutil.DB(t)
	clk := testutil.NewClock(ghNow())

	writable := github.Repository{
		ViewerPermission: "WRITE", ViewerMergeMethod: "SQUASH", DefaultBranch: "main", SquashMergeAllowed: true,
		MergeCommitAllowed: true, AutoMergeAllowed: true,
	}
	readOnly := github.Repository{ViewerPermission: "READ", DefaultBranch: "main"}

	ids, err := inbox.StoreFixture(ctx, db, inbox.Fixture{Viewer: ghViewer, At: ghNow(), Items: []inbox.FixtureItem{
		{
			Item: ghItem(ghPullNumber, ghRepo, ghAuthor, "User", writable), ReviewRequested: true,
			Details: &github.ItemDetails{HeadOID: ghHeadA},
		},
		{
			Item: ghItem(ghBotNumber, ghRepo, "dependabot", "Bot", writable), ReviewRequested: true,
			Details: &github.ItemDetails{HeadOID: ghHeadA},
		},
		{
			Item: ghItem(ghIssueNumber, ghRepo, ghAuthor, "User", writable), Assigned: true,
			Details: &github.ItemDetails{},
		},
		{
			Item: ghItem(ghLockedNumber, ghReadOnly, ghAuthor, "User", readOnly), Assigned: true,
			Details: &github.ItemDetails{},
		},
	}})
	require.NoError(t, err)

	// The inbox shows the account's items, as its last refresh left it.
	require.NoError(t, prefsstore.New(db, clk).Set(ctx, "inbox.viewer", ghViewer))

	r := &ghRig{
		pulls:  &ghPullHub{reads: map[string]*github.PullReview{}, states: map[string]*github.MergeState{}},
		issues: &ghIssueHub{views: map[string]*github.IssueView{}}, store: inbox.NewStore(db, clk),
		clock: clk, pull: ids[0], bot: ids[1], issue: ids[2], locked: ids[3],
	}

	r.pulls.setRead(ghPullNumber, ghPullRead(ghPullNumber, ghAuthor, "User", ghHeadA, github.MergeStateClean))
	r.pulls.setRead(ghBotNumber, ghPullRead(ghBotNumber, "dependabot", "Bot", ghHeadA, github.MergeStateClean))
	r.issues.setView(ghIssueNumber, ghIssueView(ghIssueNumber, ghRepo, "bug"))
	r.issues.setView(ghLockedNumber, ghIssueView(ghLockedNumber, ghReadOnly))

	svc := inbox.NewService(inbox.Config{
		Pool: db, Tokens: ghTokens{}, Client: ghInboxClient{}, Clock: clk, Store: r.store,
	})
	actor := inbox.NewActor(svc)

	library := prompts.NewStore(db, clk)
	require.NoError(t, prompts.Seed(ctx, library))

	fill := replyFiller{store: library, source: ghPreviews{kinds: map[int64]item.Kind{
		r.pull: item.KindPR, r.bot: item.KindPR, r.issue: item.KindIssue, r.locked: item.KindIssue,
	}}}

	r.steps = githubSteps{
		review: review.NewService(review.Config{
			Pool: db, Inbox: reviewInbox{store: r.store, actor: actor}, GitHub: r.pulls,
			Settings: reviewSettings{settings: settings.NewService(db, profiles.NewStore(db, clk), t.TempDir())},
			Replies:  reviewReplies{fill}, Clock: clk,
		}),
		issues: issues.NewService(issues.Config{
			Pool: db, Inbox: issuesInbox{store: r.store, actor: actor}, GitHub: r.issues, Replies: issuesReplies{fill},
			Clock: clk,
		}),
		threads: ghthreads.NewService(ghthreads.Config{Pool: db, Clock: clk}),
		prompts: library, replies: fill,
	}

	return r
}

// ghItem is the open item numbered n in acme-org/name, opened by author, of GraphQL type
// authorType, as GitHub lists it: a pull request unless n is an issue's number.
func ghItem(n int, name, author, authorType string, repository github.Repository) github.Item {
	isPR := n == ghPullNumber || n == ghBotNumber
	kind := "issues"

	if isPR {
		kind = "pull"
	}

	return github.Item{
		IsPR: isPR, NodeID: ghNode(n), Number: n, Title: "Item " + strconv.Itoa(n),
		URL:       "https://github.com/acme-org/" + name + "/" + kind + "/" + strconv.Itoa(n),
		CreatedAt: ghNow().Add(-72 * time.Hour), UpdatedAt: ghNow().Add(-time.Hour), State: github.StateOpen,
		AuthorLogin: author, AuthorType: authorType, RepoOwner: boundOwner, RepoName: name, Repo: repository,
	}
}

// ghPullRead is what GitHub reads of the pull request numbered n at head, in merge state
// status, with labels: the account may update its branch while it is behind.
func ghPullRead(n int, author, authorType, head, status string, labels ...string) *github.PullReview {
	details := github.ItemDetails{HeadOID: head, Labels: []github.Label{}}
	for _, name := range labels {
		details.Labels = append(details.Labels, github.Label{Name: name})
	}

	return &github.PullReview{
		Update: github.ItemUpdate{
			Found: true, Item: ghItem(n, ghRepo, author, authorType, github.Repository{}), Details: details,
		},
		BaseRef: "main", MergeState: status, ViewerCanEnableAutoMerge: true,
		ViewerCanUpdateBranch: status == github.MergeStateBehind, ViewerCanClose: true,
	}
}

// ghIssueView is what GitHub reads of the open issue numbered n in acme-org/name, with labels.
func ghIssueView(n int, name string, labels ...string) *github.IssueView {
	details := github.ItemDetails{Labels: []github.Label{}}
	for _, l := range labels {
		details.Labels = append(details.Labels, github.Label{Name: l})
	}

	return &github.IssueView{
		Update: github.ItemUpdate{
			Found: true, Item: ghItem(n, name, ghAuthor, "User", github.Repository{}), Details: details,
		},
		ViewerCanClose: true, ViewerCanUpdate: true,
	}
}

// ref is the rig's item with id, numbered n, of kind, as a routine names it.
func (r *ghRig) ref(id int64, n int) autopilot.ItemRef {
	kind, name := item.KindIssue, ghRepo
	if n == ghPullNumber || n == ghBotNumber {
		kind = item.KindPR
	}

	if n == ghLockedNumber {
		name = ghReadOnly
	}

	return autopilot.ItemRef{ID: id, Kind: kind, Repo: "acme-org/" + name, Number: n}
}

// later lets a minute pass, so the sheets read their items from GitHub again rather than
// serve the read they made just now.
func (r *ghRig) later() {
	r.clock.Advance(time.Minute)
}

// send sends action to the rig's item with id, numbered n, pinned to ghHeadA.
func (r *ghRig) send(t *testing.T, id int64, n int, action spec.GitHubAction) (autopilot.GitHubSent, error) {
	t.Helper()

	return r.steps.Send(t.Context(), autopilot.GitHubSend{Item: r.ref(id, n), Head: ghHeadA, Action: action})
}

// sent sends action as send does, which must not fail.
func (r *ghRig) sent(t *testing.T, id int64, n int, action spec.GitHubAction) autopilot.GitHubSent {
	t.Helper()

	got, err := r.send(t, id, n, action)
	require.NoError(t, err)

	return got
}

// question is a GitHub step's reply: the Question starter, a comment's reply for pull
// requests.
func question() spec.GitHubAction {
	return spec.GitHubAction{Reply: &spec.ReplyAction{Prompt: spec.PromptRef{Starter: prompts.StarterReplyQuestion}}}
}

// needsRepro is a GitHub step's reply: the Needs repro starter, a comment's reply for issues.
func needsRepro() spec.GitHubAction {
	return spec.GitHubAction{Reply: &spec.ReplyAction{Prompt: spec.PromptRef{Starter: prompts.StarterReplyNeedsRepro}}}
}

// labels is a GitHub step's labels action.
func labels(add, remove []string) spec.GitHubAction {
	return spec.GitHubAction{Labels: &spec.LabelsAction{Add: add, Remove: remove}}
}

// paced is the write gate refusing a write until the pace lets the next go.
func paced() error {
	return uierr.New("The app made 30 changes on GitHub in the last minute.", budget.ErrWritePace)
}

func TestAGitHubStepReadsTheItemAgainThroughItsOwnSheet(t *testing.T) {
	t.Parallel()

	r := newGHRig(t)

	require.NoError(t, r.steps.Read(t.Context(), r.ref(r.pull, ghPullNumber)))
	require.NoError(t, r.steps.Read(t.Context(), r.ref(r.issue, ghIssueNumber)))

	assert.Equal(t, []string{"read " + ghNode(ghPullNumber), "diff #7"}, r.pulls.asked(),
		"a pull request is read through the review sheet, its files with it the first time")
	assert.Equal(t, []string{"read " + ghNode(ghIssueNumber)}, r.issues.asked(), "an issue through the issue sheet")
	assert.True(t, r.pulls.allAutopilots(), "each read says it is Autopilot's")
	assert.True(t, r.issues.allAutopilots())

	require.NoError(t, r.steps.Read(t.Context(), r.ref(r.pull, ghPullNumber)))
	assert.Len(t, r.pulls.asked(), 3, "the read right before an action always asks GitHub, however recent the last")

	r.pulls.failReads(errDropped)
	require.ErrorIs(t, r.steps.Read(t.Context(), r.ref(r.pull, ghPullNumber)), errDropped)
}

func TestAGitHubStepCommentsItsReplyOnAPullRequest(t *testing.T) {
	t.Parallel()

	r := newGHRig(t)

	got := r.sent(t, r.pull, ghPullNumber, question())
	assert.Equal(t, autopilot.GitHubSentIt, got.Outcome)
	assert.NotEmpty(t, got.Words, "it says what the sheet did")

	require.Len(t, r.pulls.reviews, 1, "the reply goes as the sheet's comment, a review of one event")
	assert.Equal(t, github.ReviewComment, r.pulls.reviews[0].Event)
	assert.Equal(t, ghHeadA, r.pulls.reviews[0].CommitID, "pinned to the head the step named")
	assert.Equal(t, "Thanks, @octo-other. I have a few questions before approving; see the comments below.",
		r.pulls.reviews[0].Body, "the reply filled in for the pull request")
	assert.True(t, r.pulls.allAutopilots(), "every request says it is Autopilot's")
}

func TestAGitHubStepSendsNothingToAPullRequestItCantPinOrRead(t *testing.T) {
	t.Parallel()

	r := newGHRig(t)

	// Pushed to since the routine matched it.
	r.pulls.setRead(ghPullNumber, ghPullRead(ghPullNumber, ghAuthor, "User", ghHeadB, github.MergeStateClean))

	got := r.sent(t, r.pull, ghPullNumber, question())
	assert.Equal(t, autopilot.GitHubPushedSince, got.Outcome)
	assert.Empty(t, r.pulls.writes(), "nothing goes to a head the step didn't name")

	r.later()
	r.pulls.failReads(errDropped)

	got = r.sent(t, r.pull, ghPullNumber, question())
	assert.Equal(t, autopilot.GitHubUnread, got.Outcome, "a read that fails sends nothing, and is decided again")
	assert.Equal(t, "It couldn't be read from GitHub right before it went, so nothing was sent: The connection dropped.",
		got.Words, "with why, in the window's words")
	assert.Empty(t, r.pulls.writes())
}

func TestAGitHubStepAsksDependabotToRebaseOnlyAsTheSheetOffersIt(t *testing.T) {
	t.Parallel()

	r := newGHRig(t)
	rebase := spec.GitHubAction{Rebase: &spec.Empty{}}

	got := r.sent(t, r.bot, ghBotNumber, rebase)
	assert.Equal(t, autopilot.GitHubNotOffered, got.Outcome, "a branch up to date has nothing to rebase")
	assert.Equal(t, "It's up to date with main, so there's nothing to rebase.", got.Words, "in the sheet's words")

	got = r.sent(t, r.pull, ghPullNumber, rebase)
	assert.Equal(t, autopilot.GitHubNotOffered, got.Outcome, "a person's pull request takes no command for Dependabot")
	assert.Equal(t, "The review sheet doesn't offer it for this pull request now.", got.Words)
	assert.Empty(t, r.pulls.writes())

	r.later()
	r.pulls.setRead(ghBotNumber, ghPullRead(ghBotNumber, "dependabot", "Bot", ghHeadA, github.MergeStateBehind))

	got = r.sent(t, r.bot, ghBotNumber, rebase)
	assert.Equal(t, autopilot.GitHubSentIt, got.Outcome)
	assert.Equal(t, []string{"comment #8: " + review.DependabotRebase}, r.pulls.writes(), "behind its base, it asks")
}

func TestAGitHubStepUpdatesABranchOnlyOnceTheUserAcceptedItsWarning(t *testing.T) {
	t.Parallel()

	r := newGHRig(t)
	update := spec.GitHubAction{UpdateBranch: &spec.Empty{}}

	r.pulls.setRead(ghBotNumber, ghPullRead(ghBotNumber, "dependabot", "Bot", ghHeadA, github.MergeStateBehind))

	got := r.sent(t, r.bot, ghBotNumber, update)
	require.Equal(t, autopilot.GitHubWarning, got.Outcome, "Dependabot's branch updated by hand warns first")
	assert.NotEmpty(t, got.Words, "with the sheet's warning")
	assert.Empty(t, r.pulls.writes(), "and waits for the user")

	accepted, err := r.steps.Send(t.Context(), autopilot.GitHubSend{
		Item: r.ref(r.bot, ghBotNumber), Head: ghHeadA, Action: update, Accept: got.Words,
	})
	require.NoError(t, err)
	assert.Equal(t, autopilot.GitHubSentIt, accepted.Outcome, "the warning as the user accepted it lets it go")
	assert.Equal(t, []string{"update #8"}, r.pulls.writes())
}

func TestAGitHubStepChangesAPullRequestsLabelsOnlyWhereTheyDiffer(t *testing.T) {
	t.Parallel()

	r := newGHRig(t)
	r.pulls.setRead(ghPullNumber, ghPullRead(ghPullNumber, ghAuthor, "User", ghHeadA, github.MergeStateClean, "triage"))

	got := r.sent(t, r.pull, ghPullNumber, labels([]string{"Triage"}, []string{"stale"}))
	assert.Equal(t, autopilot.GitHubNothing, got.Outcome, "labels already so send nothing")
	assert.Equal(t, "Its labels were already so.", got.Words)
	assert.Empty(t, r.pulls.writes())

	got = r.sent(t, r.pull, ghPullNumber, labels([]string{"needs-review"}, []string{"triage"}))
	assert.Equal(t, autopilot.GitHubSentIt, got.Outcome)
	assert.Equal(t, "Added “needs-review”; took off “triage”.", got.Words)
	assert.Equal(t, []string{"label #7 +needs-review", "label #7 -triage"}, r.pulls.writes())
}

func TestAGitHubStepWaitsOutTheWriteGateAndFailsOnWhatMayHaveGone(t *testing.T) {
	t.Parallel()

	r := newGHRig(t)

	r.pulls.failWrites(paced())

	got := r.sent(t, r.pull, ghPullNumber, question())
	assert.Equal(t, autopilot.GitHubHeld, got.Outcome, "the write gate's refusal holds the step")
	assert.Equal(t, "The app made 30 changes on GitHub in the last minute.", got.Words)

	got = r.sent(t, r.pull, ghPullNumber, labels([]string{"needs-review"}, nil))
	assert.Equal(t, autopilot.GitHubHeld, got.Outcome, "a label edit's too")

	r.pulls.failWrites(errDropped)

	_, err := r.send(t, r.pull, ghPullNumber, question())
	require.ErrorIs(t, err, errDropped, "a write that may have reached GitHub fails the step")

	_, err = r.send(t, r.pull, ghPullNumber, labels([]string{"needs-review"}, nil))
	require.ErrorIs(t, err, errDropped)
}

func TestAGitHubStepRefusesWhatItCantSendToAPullRequest(t *testing.T) {
	t.Parallel()

	r := newGHRig(t)

	_, err := r.send(t, r.pull, ghPullNumber, spec.GitHubAction{})
	require.ErrorIs(t, err, errNoGitHubAction, "an action this build doesn't know")

	approval := spec.GitHubAction{Reply: &spec.ReplyAction{Prompt: spec.PromptRef{Starter: prompts.StarterReplyLGTM}}}
	_, err = r.send(t, r.pull, ghPullNumber, approval)
	require.ErrorIs(t, err, ErrNotACommentReply, "an approval's words are never posted")

	assert.Empty(t, r.pulls.writes())
}

func TestAGitHubStepCommentsItsReplyOnAnIssue(t *testing.T) {
	t.Parallel()

	r := newGHRig(t)

	got := r.sent(t, r.issue, ghIssueNumber, needsRepro())
	assert.Equal(t, autopilot.GitHubSentIt, got.Outcome)
	assert.NotEmpty(t, got.Words)

	writes := r.issues.writes()
	require.Len(t, writes, 1)
	assert.True(t, strings.HasPrefix(writes[0], "comment #12: Thanks for reporting this, @octo-other."), writes[0])
	assert.True(t, r.issues.allAutopilots(), "every request says it is Autopilot's")

	_, err := r.send(t, r.issue, ghIssueNumber, question())
	require.ErrorIs(t, err, prompts.ErrWrongKind, "a reply for pull requests is never posted on an issue")
}

func TestAGitHubStepSendsAnIssueOnlyWhatItsSheetOffers(t *testing.T) {
	t.Parallel()

	r := newGHRig(t)

	got := r.sent(t, r.issue, ghIssueNumber, spec.GitHubAction{UpdateBranch: &spec.Empty{}})
	assert.Equal(t, autopilot.GitHubNotOffered, got.Outcome)
	assert.Equal(t, "Only a pull request has a branch.", got.Words)

	locked := ghIssueView(ghLockedNumber, ghReadOnly)
	locked.Locked = true
	r.issues.setView(ghLockedNumber, locked)

	got = r.sent(t, r.locked, ghLockedNumber, needsRepro())
	assert.Equal(t, autopilot.GitHubNotOffered, got.Outcome, "a locked issue takes no comment from a reader")
	assert.NotEmpty(t, got.Words, "in the sheet's words")

	r.later()

	closed := ghIssueView(ghIssueNumber, ghRepo)
	closed.Update.Item.State = github.StateClosed
	r.issues.setView(ghIssueNumber, closed)

	got = r.sent(t, r.issue, ghIssueNumber, needsRepro())
	assert.Equal(t, autopilot.GitHubNotOffered, got.Outcome)
	assert.Equal(t, "The issue sheet doesn't offer a comment on it now.", got.Words, "a closed issue offers none")

	assert.Empty(t, r.issues.writes())

	r.issues.mu.Lock()
	r.issues.readErr = errDropped
	r.issues.mu.Unlock()

	got = r.sent(t, r.issue, ghIssueNumber, needsRepro())
	assert.Equal(t, autopilot.GitHubUnread, got.Outcome)
}

func TestAGitHubStepChangesAnIssuesLabelsAsItsAccountMay(t *testing.T) {
	t.Parallel()

	r := newGHRig(t)

	got := r.sent(t, r.locked, ghLockedNumber, labels([]string{"bug"}, nil))
	assert.Equal(t, autopilot.GitHubNotOffered, got.Outcome)
	assert.Equal(t, "Your account can't change this issue's labels: that takes triage access.", got.Words)

	got = r.sent(t, r.issue, ghIssueNumber, labels([]string{"BUG"}, []string{"wontfix"}))
	assert.Equal(t, autopilot.GitHubNothing, got.Outcome, "labels already so send nothing")

	got = r.sent(t, r.issue, ghIssueNumber, labels([]string{"needs-repro"}, []string{"bug"}))
	assert.Equal(t, autopilot.GitHubSentIt, got.Outcome)
	assert.Equal(t, "Added “needs-repro”; took off “bug”.", got.Words)
	assert.Equal(t, []string{"label #12 +needs-repro", "label #12 -bug"}, r.issues.writes())
}

func TestAGitHubStepOnAnIssueWaitsOutTheWriteGateAndFailsOnWhatMayHaveGone(t *testing.T) {
	t.Parallel()

	r := newGHRig(t)

	r.issues.mu.Lock()
	r.issues.writeErr = paced()
	r.issues.mu.Unlock()

	got := r.sent(t, r.issue, ghIssueNumber, needsRepro())
	assert.Equal(t, autopilot.GitHubHeld, got.Outcome)

	got = r.sent(t, r.issue, ghIssueNumber, labels([]string{"needs-repro"}, nil))
	assert.Equal(t, autopilot.GitHubHeld, got.Outcome)

	r.issues.mu.Lock()
	r.issues.writeErr = errDropped
	r.issues.mu.Unlock()

	_, err := r.send(t, r.issue, ghIssueNumber, needsRepro())
	require.ErrorIs(t, err, errDropped)

	_, err = r.send(t, r.issue, ghIssueNumber, spec.GitHubAction{
		Reply: &spec.ReplyAction{Prompt: spec.PromptRef{Nickname: "Gone"}},
	})
	require.ErrorIs(t, err, errNoReply, "a reply the library lost sends nothing")
}

func TestAGitHubStepsPreviewSaysWhatItWouldSend(t *testing.T) {
	t.Parallel()

	r := newGHRig(t)
	ctx := t.Context()
	pr := r.ref(r.pull, ghPullNumber)

	got, err := r.steps.Preview(ctx, pr, spec.GitHubAction{Rebase: &spec.Empty{}})
	require.NoError(t, err)
	assert.Equal(t, review.DependabotRebase, got)

	got, err = r.steps.Preview(ctx, pr, spec.GitHubAction{UpdateBranch: &spec.Empty{}})
	require.NoError(t, err)
	assert.Equal(t, "Merges its base branch into its branch, as the review sheet's Update branch does.", got)

	got, err = r.steps.Preview(ctx, pr, question())
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(got, "Thanks, @octo-other."), "the reply filled in for the item: %q", got)

	got, err = r.steps.Preview(ctx, pr, labels([]string{"needs-review"}, []string{"triage", "stale"}))
	require.NoError(t, err)
	assert.Equal(t, "Adds “needs-review”; takes off “triage”, “stale”.", got)

	got, err = r.steps.Preview(ctx, pr, labels(nil, nil))
	require.NoError(t, err)
	assert.Equal(t, "It changes no label.", got)

	_, err = r.steps.Preview(ctx, pr, spec.GitHubAction{})
	require.ErrorIs(t, err, errNoGitHubAction)

	assert.Empty(t, r.pulls.asked(), "a preview builds what it would send and asks GitHub nothing")

	unread := githubSteps{prompts: prompts.NewStore(testutil.ClosedDB(t), nil)}
	_, err = unread.Preview(ctx, pr, question())
	require.Error(t, err, "a library that can't be read fills in no reply")
}

func TestMarkDoneQueuesNothingForAnItemTheMirrorListsNoNotificationOf(t *testing.T) {
	t.Parallel()

	r := newGHRig(t)

	queued, err := r.steps.MarkDone(t.Context(), r.ref(r.pull, ghPullNumber))
	require.NoError(t, err)
	assert.Zero(t, queued, "before the mirror read any notification, there is nothing to clear")
	assert.Empty(t, r.pulls.asked(), "and nothing is asked of GitHub")
}
