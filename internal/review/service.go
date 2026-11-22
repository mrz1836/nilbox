// Package review is the review sheet: it shows one pull request (what it changes, its
// description, checks, reviewers, its conversation, how far behind its base it is, and
// what it still needs to merge) and acts on it at the user's click: approve, request
// changes, comment, merge, turn on auto-merge, update the branch, ask Dependabot to rebase,
// ask for the user's own review again, or close it.
//
// Open shows what the sheet read before, from its own cache, with no request, and starts a
// visit, which tells what is new; every build takes from the inbox's card what it read
// since, its check counts, merge state, and held runs among them (inboxNewer), and says
// when the checks moved past what the sheet read (Review.Moved); Refresh reads the pull
// request again with one GraphQL request, its conversation included, and its files only
// when it was pushed to since;
// Earlier reads an earlier page of the conversation. Every action is pinned to the head commit the user saw, goes through
// the inbox's Actor (the item's lock and the account check) and the GitHub client's write
// gate, and is decided again (decide.go) before anything is sent. A merge never goes around
// branch protection: it reads the merge state fresh first and merges only what GitHub says
// can merge now. Agents never approve or merge; only the user's click does.
//
// Line comments are checked against the cached diff (hunks.go) before GitHub sees them.
// The sheet also manages who a pull request asks to review (reviewers.go): it suggests
// reviewers from the People directory, with no request, learns into it from every read, and
// asks or takes requests off with at most two REST requests. And it manages who a pull
// request is assigned to (assignees.go), as the issue sheet does an issue's, through the
// assignee Finder both sheets share.
// The package reaches the inbox, the settings, the prompt library, and the People
// directory only through the interfaces it declares, which the app's core adapts.
package review

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/assignees"
	"github.com/mrz1836/nilbox/internal/bump"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/flight"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/heldruns"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/timeline"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// refreshAfter is how long a context read from GitHub counts as current, so a sheet
// opened again at once asks nothing, unless the inbox card shows activity since.
const refreshAfter = 20 * time.Second

// ErrNotPull is returned for an item that is not a pull request.
var ErrNotPull = errors.New("the item is not a pull request")

// StoredPull is a pull request as the inbox stored it, with its repository's merge
// settings. Its fields match inbox.ReviewItem's, so the core converts one to the other.
type StoredPull struct {
	ID     int64
	Kind   item.Kind
	Number int
	NodeID string
	Title  string
	URL    string

	// State is the state the inbox last knew the pull request in: MERGED or CLOSED once a
	// merge, a close, or a read found it so, else empty or OPEN.
	State string

	AuthorLogin       string
	AuthorType        string
	AuthorAssociation string
	IsCrossRepository bool
	IsDraft           bool
	HeadRef           string
	CreatedAt         time.Time
	UpdatedAt         time.Time

	ReviewRequested bool
	Authored        bool
	Assigned        bool

	Owner         string
	Name          string
	DefaultBranch string

	ViewerPermission  string
	ViewerMergeMethod string

	MergeCommitAllowed  bool
	SquashMergeAllowed  bool
	RebaseMergeAllowed  bool
	AutoMergeAllowed    bool
	DeleteBranchOnMerge bool
	AllowUpdateBranch   bool

	Viewer string

	HasDetails bool
	Labels     []string

	Comments     int
	Additions    int
	Deletions    int
	ChangedFiles int
	Commits      int

	Mergeable      string
	ChecksState    string
	ChecksPassed   int
	ChecksFailed   int
	ChecksPending  int
	ChecksSkipped  int
	ReviewDecision string

	// ChecksAwaiting counts the workflow runs held for a maintainer's approval that GitHub
	// takes from the app (Checks.Awaiting), and ChecksAwaitingOnGitHub those it takes only
	// on its site (Checks.AwaitingOnGitHub), as GitHub reported them.
	ChecksAwaiting         int
	ChecksAwaitingOnGitHub int

	// HeldRuns are the held runs both count, each with its workflow's name, or none.
	HeldRuns []heldruns.Run

	// MergeState is GitHub's mergeStateStatus as the inbox's details read it, "" before
	// they did.
	MergeState string

	Reviewers        int
	Reviewed         int
	Approvals        int
	ChangesRequested int

	// ReviewUsers and ReviewTeams are who the pull request asks to review: the logins of
	// the people, and the slugs of the teams.
	ReviewUsers []string
	ReviewTeams []string

	HeadOID   string
	AutoMerge string

	DetailsAt time.Time
}

// ref names the pull request.
func (p StoredPull) ref() item.Ref {
	return item.Ref{Repo: repo.Name{Owner: p.Owner, Repo: p.Name}, Number: p.Number, Kind: item.KindPR}
}

// Target is the pull request a read or an action is for, as the inbox's Actor hands it
// over. Its fields match inbox.PullTarget's.
type Target struct {
	ID              int64
	Ref             item.Ref
	NodeID          string
	Login           string
	Head            string
	UpdatedAt       time.Time
	ReviewRequested bool
	Authored        bool
	Assigned        bool
}

// Acted is what an action made happen on GitHub. Its fields match inbox.Acted's.
// ReviewEvent is the review's event, as the action sent it (github.ReviewApprove,
// github.ReviewRequestChanges, or github.ReviewComment). Rerequested reports the user's
// review requested again, with who the pull request asks now (ReviewRequests), as of
// GitHub's time of the edit (RerequestedAt). RequestsEdited reports a reviewer edit GitHub
// made, with who the pull request asks after it (ReviewRequests).
type Acted struct {
	Reviewed       bool
	ReviewedAt     time.Time
	ReviewEvent    string
	Merged         bool
	Closed         bool
	Rerequested    bool
	ReviewRequests github.ReviewRequests
	RerequestedAt  time.Time
	RequestsEdited bool
}

// PullRead is what a read found. Its fields match inbox.PullRead's.
type PullRead struct {
	Update *github.ItemUpdate
	Files  *github.ChangedFiles
}

// Inbox is what the sheet needs of the inbox: a stored pull request, and a read and an
// action run with the inbox's footing (inbox.Actor), and a read that stores nothing, such
// as an earlier page of the conversation.
type Inbox interface {
	Pull(ctx context.Context, id int64) (StoredPull, error)
	Read(ctx context.Context, id int64, read func(context.Context, github.Token, Target) (PullRead, error)) error
	Act(ctx context.Context, id int64, act func(context.Context, github.Token, Target) (Acted, error)) error
	Peek(ctx context.Context, id int64, peek func(context.Context, github.Token, Target) error) error
}

// GitHub is the GitHub client's reads and writes the sheet uses. *github.Client
// implements it.
type GitHub interface {
	FetchPullReview(ctx context.Context, tok github.Token, id, compareHead string) (*github.PullReview, error)
	FetchPullDiff(ctx context.Context, tok github.Token, ref item.Ref, head string) (*github.PullDiff, error)
	FetchMergeState(ctx context.Context, tok github.Token, id string) (*github.MergeState, error)
	FetchOwnMergeState(ctx context.Context, tok github.Token, id string) (*github.MergeState, error)
	FetchTimeline(ctx context.Context, tok github.Token, kind item.Kind, id, before string) (*github.TimelinePage, error)
	SubmitReview(ctx context.Context, tok github.Token, ref item.Ref, in github.ReviewInput) (*github.SubmittedReview, error)
	MergePull(ctx context.Context, tok github.Token, ref item.Ref, in github.MergeInput) (*github.Merged, error)
	EnableAutoMerge(
		ctx context.Context, tok github.Token, ref item.Ref, nodeID, method, expectedHead string,
	) (*github.AutoMerge, error)
	UpdateBranch(ctx context.Context, tok github.Token, ref item.Ref, expectedHead string) (*github.BranchUpdate, error)
	AddComment(ctx context.Context, tok github.Token, ref item.Ref, body string) (*github.Comment, error)
	ClosePull(ctx context.Context, tok github.Token, ref item.Ref) error
	ApproveWorkflowRun(ctx context.Context, tok github.Token, name repo.Name, runID int64) error
	AddLabels(ctx context.Context, tok github.Token, ref item.Ref, names []string) ([]github.Label, error)
	RemoveLabel(ctx context.Context, tok github.Token, ref item.Ref, name string) ([]github.Label, error)
	RequestReview(ctx context.Context, tok github.Token, ref item.Ref, login string) (*github.Edited, error)
	RequestReviewers(ctx context.Context, tok github.Token, ref item.Ref, want github.ReviewRequests) (*github.Edited, error)
	RemoveReviewRequests(
		ctx context.Context, tok github.Token, ref item.Ref, drop github.ReviewRequests,
	) (*github.Edited, error)
	FetchRepoPeople(ctx context.Context, tok github.Token, id string) (*github.RepoPeople, error)
	FetchRepoTeams(ctx context.Context, tok github.Token, name repo.Name) ([]github.Team, error)
	FetchAssignable(
		ctx context.Context, tok github.Token, kind item.Kind, id, query string,
	) (*github.AssignableUsers, error)
	AddAssignees(ctx context.Context, tok github.Token, ref item.Ref, logins []string) (*github.Edited, error)
	RemoveAssignees(ctx context.Context, tok github.Token, ref item.Ref, logins []string) (*github.Edited, error)
}

// Settings reads the settings the sheet follows: how to merge and whether to merge the
// user's own pull requests past the rules (Prefs), which files are sensitive, and the
// workflows whose held runs never count (heldruns.KeyIgnoredWorkflows).
type Settings interface {
	MergePrefs(ctx context.Context) (Prefs, error)
	Sensitive(ctx context.Context) (sensitive.Config, error)
	IgnoredWorkflows(ctx context.Context) ([]string, error)
}

// Replies reads the review replies of the prompt library, and fills one in for a pull
// request.
type Replies interface {
	Offered(ctx context.Context, action ReplyAction) (ReplyList, error)
	Fill(ctx context.Context, replyID, itemID int64) (string, error)
}

// Merges hears of each pull request a merge in the sheet merged, by its repository: core
// hands it the Release page's Refresher, which reads that repository's row again, since the
// merge may give it commits to release. It must not wait.
type Merges interface {
	Merged(name repo.Name)
}

// Config holds a Service's dependencies. Every one but Merges, People, Assignees, Clock,
// and Logger is required.
type Config struct {
	// Pool is the database, which holds the sheet's cache.
	Pool *sql.DB

	Inbox    Inbox
	GitHub   GitHub
	Settings Settings
	Replies  Replies

	// Events hears what an action is doing as it goes (event.ReviewProgress), for the
	// sheet to show while the user waits.
	Events event.Sink

	// Merges hears of each merge the sheet makes; without it, no one does.
	Merges Merges

	// People is the directory of who works on each repository, which the sheet learns
	// into and suggests reviewers from; without it, the sheet learns no one and suggests
	// only GitHub's suggestions.
	People People

	// Assignees offers people to assign, and learns and searches who can be, shared with
	// the issue sheet; without it, the assignee picker offers no one.
	Assignees *assignees.Finder

	Clock  clock.Clock
	Logger *slog.Logger
}

// Service is the review sheet as the window binds it. It is safe for concurrent use.
type Service struct {
	inbox    Inbox
	github   GitHub
	settings Settings
	replies  Replies
	cache    *cacheStore
	events   event.Sink
	merges   Merges
	people   People
	finder   *assignees.Finder
	clock    clock.Clock
	logger   *slog.Logger

	// refreshes reads each pull request once at a time, and learns each repository's
	// recent pull requests once at a time, by its key.
	refreshes flight.Group[Review]
	learns    flight.Group[struct{}]

	// onGitHub holds the workflow runs GitHub refused to approve from the sheet for what
	// they are (github.ErrRunNotApprovable), by run ID, which GitHub numbers across every
	// repository: from then on, while the app runs, the sheet shows each as waiting for
	// approval on GitHub and never offers to approve it again. Nothing stores it.
	onGitHub   map[int64]bool
	onGitHubMu sync.Mutex
}

// NewService returns a Service over cfg.
func NewService(cfg Config) *Service {
	return &Service{
		inbox: cfg.Inbox, github: cfg.GitHub, settings: cfg.Settings, replies: cfg.Replies,
		cache: newCacheStore(cfg.Pool), events: event.Or(cfg.Events), merges: cfg.Merges, people: cfg.People,
		finder: cfg.Assignees, clock: clock.Or(cfg.Clock), logger: logging.Or(cfg.Logger), onGitHub: map[int64]bool{},
	}
}

// Open returns the pull request with id as the sheet last read it, from its cache and the
// inbox, with no request: at once, and offline. It starts a visit: LastSeenAt is GitHub's
// time of the newest activity in the read it shows, so what a refresh brings that is newer
// is new to the user, and every Refresh and ReadAgain returns the same until the next Open.
// Stale says whether a Refresh is due.
func (s *Service) Open(ctx context.Context, id int64) (Review, error) {
	return s.open(ctx, id, openPatchBytes, true)
}

// Refresh reads the pull request with id from GitHub, stores it, and returns it: one
// GraphQL request, and its files only when it was pushed to since the sheet listed them.
// A pull request read in the last 20 seconds is not read again, unless its card shows
// activity since. Two refreshes of one pull request never run side by side. One the read
// finds merged or closed is returned as read this once, its State saying which, and the
// sheet forgets it.
func (s *Service) Refresh(ctx context.Context, id int64) (Review, error) {
	return s.refresh(ctx, id, false)
}

// ReadAgain reads the pull request with id from GitHub as Refresh does, however recently
// it was read: the user asked for it, from the sheet's refresh button. A read already
// under way for the pull request is joined rather than run beside it.
func (s *Service) ReadAgain(ctx context.Context, id int64) (Review, error) {
	return s.refresh(ctx, id, true)
}

// Rebuild builds the sheet of the pull request with id again from what it read before, as
// Refresh returns it, with no request and no new visit: the sheet asks for it when a setting
// it follows changed while it is open, as the workflows whose held runs never count.
func (s *Service) Rebuild(ctx context.Context, id int64) (Review, error) {
	return s.open(ctx, id, openPatchBytes, false)
}

// Patches returns the diffs of the files at paths of the pull request with id, as the
// sheet cached them at head, for the files Open left out. A head the cache no longer
// holds returns an error saying to reload.
func (s *Service) Patches(ctx context.Context, id int64, head string, paths []string) ([]FilePatch, error) {
	c, err := s.cache.load(ctx, id, 0)
	if err != nil {
		return nil, err
	}

	if c == nil || !strings.EqualFold(c.head, head) {
		return nil, uierr.New("This pull request changed since the sheet read it. Reload it to see the new diff.",
			fmt.Errorf("%w: patches of item %d", github.ErrPushedSince, id))
	}

	byPath := filesByPath(c.files)
	asked := make([]cachedFile, 0, len(paths))
	positions := make([]int64, 0, len(paths))

	for _, p := range paths {
		if f, ok := byPath[p]; ok {
			asked = append(asked, f)
			positions = append(positions, int64(f.position))
		}
	}

	patches, err := s.cache.patches(ctx, id, positions)
	if err != nil {
		return nil, err
	}

	out := make([]FilePatch, 0, len(asked))

	for _, f := range asked {
		patch, state := patchOf(f, patches)
		out = append(out, FilePatch{Path: f.path, Patch: patch, PatchState: state})
	}

	return out, nil
}

// Earlier reads the page of the conversation before the cursor before, which a page the
// sheet holds gave: one GraphQL request, which stores nothing, not even in the cache.
func (s *Service) Earlier(ctx context.Context, id int64, before string) (timeline.Page, error) {
	pull, err := s.pull(ctx, id)
	if err != nil {
		return timeline.Page{}, err
	}

	var page *github.TimelinePage

	err = s.inbox.Peek(ctx, id, func(ctx context.Context, tok github.Token, t Target) error {
		got, readErr := s.github.FetchTimeline(ctx, tok, item.KindPR, t.NodeID, before)
		page = got

		return readErr
	})

	switch {
	case errors.Is(err, github.ErrInvalidCursor):
		return timeline.Page{}, uierr.New(
			"That part of the conversation can't be read. Reload the pull request, then try again.", err,
		)
	case err != nil:
		return timeline.Page{}, err
	}

	return timeline.Page{
		Items: timeline.Entries(page.Items, pull.Viewer), Total: max(page.Total, len(page.Items)), Before: page.Before,
	}, nil
}

// Replies returns the review replies for action, with the defaults.
func (s *Service) Replies(ctx context.Context, action ReplyAction) (ReplyList, error) {
	list, err := s.replies.Offered(ctx, action)
	if err != nil {
		return ReplyList{}, err
	}

	list.Replies = textutil.NonNil(list.Replies)

	return list, nil
}

// FillReply fills in the reply with replyID for the pull request with itemID, as the
// text the review starts with.
func (s *Service) FillReply(ctx context.Context, itemID, replyID int64) (string, error) {
	return s.replies.Fill(ctx, replyID, itemID)
}

// rememberOnGitHub remembers the workflow run with runID, which GitHub refused to approve
// from the sheet for what it is, as one it approves only on its site.
func (s *Service) rememberOnGitHub(runID int64) {
	s.onGitHubMu.Lock()
	defer s.onGitHubMu.Unlock()

	s.onGitHub[runID] = true
}

// markOnGitHub marks the held runs of the cache c that GitHub refused to approve from the
// sheet (rememberOnGitHub) as waiting for approval on GitHub, whatever the read that cached
// them made of them. c may be nil.
func (s *Service) markOnGitHub(c *cached) {
	if c == nil {
		return
	}

	s.onGitHubMu.Lock()
	defer s.onGitHubMu.Unlock()

	for i, u := range c.context.Unrun {
		if u.Approval && s.onGitHub[u.RunID] {
			c.context.Unrun[i].OnGitHub = true
		}
	}
}

// markIgnored marks the held runs of the cache c that don't count toward where the pull
// request stands (heldruns.Ignored), with the workflows the settings ignore and the merge
// state the cache read, and keeps those workflows with c, for the held runs of a newer read
// of the inbox's (inboxNewer). c may be nil.
func (s *Service) markIgnored(ctx context.Context, c *cached) error {
	if c == nil {
		return nil
	}

	ignored, err := s.settings.IgnoredWorkflows(ctx)
	if err != nil {
		return err
	}

	c.ignored = ignored

	for i, u := range c.context.Unrun {
		c.context.Unrun[i].Ignored = u.Approval &&
			heldruns.Ignored(heldruns.Run{Workflow: u.Name, OnGitHub: u.OnGitHub}, c.context.MergeState, ignored)
	}

	return nil
}

// refresh reads the pull request with id, skipping one read recently unless force is set,
// and returns it as Open builds it, or, when the read found it merged or closed, as the
// read found it (ended).
func (s *Service) refresh(ctx context.Context, id int64, force bool) (Review, error) {
	return s.refreshes.Do(ctx, strconv.FormatInt(id, 10), func(ctx context.Context) (Review, error) {
		ended, err := s.read(ctx, id, force)
		if err != nil {
			return Review{}, err
		}

		if ended != nil {
			return s.ended(ctx, id, ended)
		}

		return s.open(ctx, id, openPatchBytes, false)
	})
}

// open builds the sheet from the inbox's stored pull request and the cache, with up to
// budget bytes of patches, and stamps the cache's use; visit says the user opened the
// sheet, which starts a visit from the read the cache holds.
func (s *Service) open(ctx context.Context, id, budget int64, visit bool) (Review, error) {
	pull, err := s.pull(ctx, id)
	if err != nil {
		return Review{}, err
	}

	c, err := s.cache.load(ctx, id, budget)
	if err != nil {
		return Review{}, err
	}

	if c != nil {
		s.stampUse(ctx, id, c, visit)
	}

	s.markOnGitHub(c)

	if err = s.markIgnored(ctx, c); err != nil {
		return Review{}, err
	}

	return s.built(ctx, pull, c)
}

// ended builds the sheet of the pull request with id from read, the read that found it
// merged or closed, which the cache no longer holds: its State says which, so the window
// learns it from the read it asked for, with no request more.
func (s *Service) ended(ctx context.Context, id int64, read *cached) (Review, error) {
	pull, err := s.pull(ctx, id)
	if err != nil {
		return Review{}, err
	}

	return s.built(ctx, pull, read)
}

// built makes the sheet from the stored pull request and c, judged by the settings.
func (s *Service) built(ctx context.Context, pull StoredPull, c *cached) (Review, error) {
	cfg, err := s.settings.Sensitive(ctx)
	if err != nil {
		return Review{}, err
	}

	prefs, err := s.settings.MergePrefs(ctx)
	if err != nil {
		return Review{}, err
	}

	return build(pull, c, sensitive.NewMatcher(cfg), prefs), nil
}

// stampUse stamps the use of the cache c of the pull request with id, and, when visit is
// set, the visit it starts: the read the cache holds, by GitHub's time of its newest
// activity, which c.seenAt holds from then on. A read that doesn't say when it was leaves
// the last visit's as it was.
func (s *Service) stampUse(ctx context.Context, id int64, c *cached, visit bool) {
	if !visit {
		if err := s.cache.touch(ctx, id, s.now()); err != nil {
			s.logger.DebugContext(ctx, "could not stamp the cached review", "error", err)
		}

		return
	}

	at, ok := parseStamp(c.context.UpdatedAt)
	if !ok {
		at, ok = parseStamp(c.seenAt)
	}

	var seenAt *time.Time

	c.seenAt = ""
	if ok {
		seenAt, c.seenAt = &at, clock.Format(at)
	}

	if err := s.cache.open(ctx, id, seenAt, s.now()); err != nil {
		s.logger.DebugContext(ctx, "could not stamp the cached review", "error", err)
	}
}

// pull reads the stored pull request with id.
func (s *Service) pull(ctx context.Context, id int64) (StoredPull, error) {
	pull, err := s.inbox.Pull(ctx, id)
	if err != nil {
		return StoredPull{}, err
	}

	if pull.Kind != item.KindPR || pull.NodeID == "" {
		return StoredPull{}, fmt.Errorf("%w (id %d)", ErrNotPull, id)
	}

	return pull, nil
}

// read reads the pull request with id from GitHub, through the inbox, which stores the
// card, and stores the rest in the cache. A context read in the last refreshAfter, of the
// head the inbox knows, whose card shows no activity since, is not read again, unless
// GitHub was still working out whether it can merge (the sheet reads again for the answer)
// or force is set. It returns what a read that found the pull request merged or closed
// read (endedRead), which the cache drops, and nil for any other.
func (s *Service) read(ctx context.Context, id int64, force bool) (*cached, error) {
	c, err := s.cache.load(ctx, id, 0)
	if err != nil {
		return nil, err
	}

	pull, err := s.pull(ctx, id)
	if err != nil {
		return nil, err
	}

	if !force && recent(pull, c, s.now()) {
		return nil, nil //nolint:nilnil // a read skipped found nothing ended
	}

	var got struct {
		review *github.PullReview
		diff   *github.PullDiff
	}

	err = s.inbox.Read(ctx, id, func(ctx context.Context, tok github.Token, t Target) (PullRead, error) {
		review, diff, readErr := s.fetch(ctx, tok, t, c)
		got.review, got.diff = review, diff

		if readErr != nil {
			return PullRead{}, readErr
		}

		return PullRead{Update: &review.Update, Files: changedFiles(diff, c, review)}, nil
	})
	if err != nil {
		return nil, err
	}

	if err = s.store(ctx, id, pull.Viewer, got.review, got.diff); err != nil {
		return nil, err
	}

	s.learn(ctx, pull, got.review)

	return endedRead(got.review, c, pull.Viewer, s.now()), nil
}

// endedRead is what review, a read of a pull request, found when it found it merged or
// closed, for the sheet to show this once, since the cache drops it (store): the context
// it read, of viewer, the signed-in account, as of now, with the visit c started (the
// cache before the read, nil when there was none). No files: a pull request no longer
// open is never listed again (fetch). It is nil for a read that found the pull request
// open, or that GitHub can't find any more.
func endedRead(review *github.PullReview, c *cached, viewer string, now time.Time) *cached {
	if review == nil || !review.Update.Found || !review.Update.Item.Closed() {
		return nil
	}

	read := &cached{
		head: review.Update.Details.HeadOID, total: review.Update.Details.ChangedFiles,
		context: contextOf(review, viewer), contextAt: now,
	}

	if c != nil {
		read.seenAt = c.seenAt
	}

	return read
}

// recent reports that the cache c of pull holds a read recent enough to serve at now: of
// the head the inbox knows, under refreshAfter old, with the card showing no activity since
// (a comment the read doesn't have would never show), with the merge state worked out, and
// with the card's checks and held runs counted as the read counted them. A check that ends
// moves no activity, so a card the checks follower rewrote with checks ended since is read
// again, however recently the sheet read it.
func recent(pull StoredPull, c *cached, now time.Time) bool {
	if c == nil || !strings.EqualFold(c.head, pull.HeadOID) || now.Sub(c.contextAt) >= refreshAfter ||
		c.context.MergeState == github.MergeStateUnknown || (pull.HasDetails && !c.context.Counts.matches(pull)) {
		return false
	}

	at, ok := parseStamp(c.context.UpdatedAt)

	return ok && !pull.UpdatedAt.Truncate(time.Second).After(at)
}

// parseStamp reads a time stamp the cache wrote; ok is false for "" and for one it can't
// read.
func parseStamp(s string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, s)

	return t, err == nil
}

// stamp writes t as every time the window reads is written (clock.Format), or "" for the
// zero time.
func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	return clock.Format(t)
}

// fetch reads the pull request's context, with how far its branch is behind its base, and,
// unless c holds its files at the head the context names, its files. When it was pushed to
// between the two reads, the context is read again for the new head.
func (s *Service) fetch(
	ctx context.Context, tok github.Token, t Target, c *cached,
) (*github.PullReview, *github.PullDiff, error) {
	compareHead := github.PullHeadRef(t.Ref.Number)

	review, err := s.github.FetchPullReview(ctx, tok, t.NodeID, compareHead)
	if err != nil {
		return nil, nil, err
	}

	head := review.Update.Details.HeadOID
	if !review.Update.Found || review.Update.Item.Closed() || head == "" ||
		(c != nil && strings.EqualFold(c.head, head)) {
		return review, nil, nil
	}

	diff, err := s.github.FetchPullDiff(ctx, tok, t.Ref, head)
	if err != nil {
		return nil, nil, err
	}

	if !strings.EqualFold(diff.Head, head) {
		if review, err = s.github.FetchPullReview(ctx, tok, t.NodeID, compareHead); err != nil {
			return nil, nil, err
		}
	}

	return review, diff, nil
}

// changedFiles is the list of files a read serves as the pull request's sensitive-file
// check: the files just listed, or those cached at the head the context names; nil when
// neither is at that head.
func changedFiles(diff *github.PullDiff, c *cached, review *github.PullReview) *github.ChangedFiles {
	head := review.Update.Details.HeadOID

	switch {
	case diff != nil:
		out := &github.ChangedFiles{Head: diff.Head, Total: diff.Total, Complete: diff.Complete}
		for _, f := range diff.Files {
			out.Files = append(out.Files, github.ChangedFile{Path: f.Path, PreviousPath: f.PreviousPath, Status: f.Status})
		}

		return out
	case c != nil && head != "" && strings.EqualFold(c.head, head):
		out := &github.ChangedFiles{Head: c.head, Total: c.total, Complete: c.complete}
		for _, f := range c.files {
			out.Files = append(out.Files, github.ChangedFile{Path: f.path, PreviousPath: f.previousPath, Status: f.status})
		}

		return out
	default:
		return nil
	}
}

// store keeps what a read found: nothing for a pull request that is gone, merged, or
// closed, whose cache is dropped; else its context, the entries of viewer, the signed-in
// account, marked, and its files when they were listed.
func (s *Service) store(
	ctx context.Context, id int64, viewer string, review *github.PullReview, diff *github.PullDiff,
) error {
	if review == nil {
		return nil
	}

	if !review.Update.Found || review.Update.Item.Closed() {
		return s.cache.drop(ctx, id)
	}

	return s.cache.save(ctx, id, contextOf(review, viewer), diff, s.now())
}

// now returns the current time in UTC.
func (s *Service) now() time.Time {
	return s.clock.Now().UTC()
}

// build makes the sheet from the stored pull request and its cache (nil when the sheet
// never read it), judging its files with matcher and its merge with prefs.
func build(pull StoredPull, c *cached, matcher *sensitive.Matcher, prefs Prefs) Review {
	r := Review{
		ID: pull.ID, Repo: pull.ref().Repo.String(), Number: pull.Number, Title: pull.Title, URL: pull.URL,
		State: github.StateOpen, Author: pull.AuthorLogin, IsBot: strings.EqualFold(pull.AuthorType, "Bot"),
		Mine: strings.EqualFold(pull.AuthorLogin, pull.Viewer), Draft: pull.IsDraft, HeadRef: pull.HeadRef,
		BaseRef: pull.DefaultBranch, ReviewRequested: pull.ReviewRequested, Labels: textutil.NonNil(pull.Labels),
		Additions: pull.Additions, Deletions: pull.Deletions, Commits: pull.Commits, Comments: pull.Comments,
		AutoMerge: MergeMethod(pull.AutoMerge), Head: pull.HeadOID, Files: []File{}, Checks: []Check{}, Unrun: []Unrun{},
		Reviews: []ReviewEntry{}, Closes: []Closing{}, TotalFiles: pull.ChangedFiles, Stale: true,
		OpenedAt: stamp(pull.CreatedAt), Reactions: []timeline.Reaction{}, Timeline: timeline.Page{Items: []timeline.Entry{}},
		CommitList: []Commit{}, Assignees: []Assignee{},
	}

	if u, ok := bump.Parse(pull.Title); ok && r.IsBot {
		r.Bump = &u
	}

	if c != nil {
		r.fill(c, matcher, pull.Viewer)
	}

	// A merge or a close the inbox recorded since, such as a routine's merge, which dropped the
	// cache, ends the sheet as the read that finds it would: the window moves on from it.
	if pull.State == github.StateMerged || pull.State == github.StateClosed {
		r.State = pull.State
	}

	r.Counts, r.MoreChecks = shownCounts(pull, c), 0

	if !r.Counts.Newer && c != nil {
		r.MoreChecks = max(c.context.Counts.total()-len(r.Checks), 0)
	}

	// The inbox read it since the sheet did: auto-merge is as it read it, and whatever moved
	// of the checks is the next read's.
	if inboxNewer(pull, c) {
		r.AutoMerge = MergeMethod(pull.AutoMerge)
		r.Moved = !strings.EqualFold(c.head, pull.HeadOID) || !c.context.Counts.matches(pull)
	}

	r.Change = changeOf(r.Files, r.Bump != nil)
	r.Reviews = withRequests(r.Reviews, pull)
	r.CanEditReviewers = canEditReviewers(pull, c)
	r.Assignees, r.MoreAssignees = shownAssignees(c, pull.Viewer)
	r.CanEditAssignees = canEditAssignees(pull, c)

	facts := factsOf(pull, c, r)
	plan := Decide(facts, prefs)
	r.Offers, r.Primary, r.Readiness = textutil.NonNil(plan.Offers), plan.Primary, plan.Readiness
	r.OwnAwaitsApproval = ownAwaitsApproval(facts, plan.Offers)
	r.MergeMethods, r.DefaultMethod = plan.Methods, plan.Method

	return r
}

// fill adds what the cache holds to r, the review of viewer, the signed-in account, told
// from the others'.
func (r *Review) fill(c *cached, matcher *sensitive.Matcher, viewer string) {
	pc := c.context
	r.Head, r.State, r.Body, r.Stale = c.head, cmp.Or(pc.State, github.StateOpen), pc.Body, !strings.EqualFold(c.head, r.Head)
	r.BodyTrimmed, r.EditedAt, r.Reactions = pc.BodyTrimmed, pc.EditedAt, textutil.NonNil(pc.Reactions)
	r.Timeline = timeline.Page{
		Items: timeline.NonNil(pc.Timeline), Total: max(pc.Total, len(pc.Timeline)), Before: pc.Before,
	}
	r.LastSeenAt = c.seenAt
	r.Mine = r.Mine || pc.Mine
	r.TotalFiles, r.FilesComplete = c.total, c.complete
	r.Checks = textutil.NonNil(pc.Checks)
	r.Unrun = textutil.NonNil(pc.Unrun)
	r.Closes, r.MoreCloses = textutil.NonNil(pc.Closes), max(pc.CloseCount-len(pc.Closes), 0)
	r.CommitList = commitsOf(pc.Commits, r.URL)
	r.EarlierCommits = max(pc.CommitTotal-len(pc.Commits), 0)
	r.YourReview = yourReview(pc.Opinion, pc.Commits, pc.CommitTotal, pc.Timeline, viewer)
	r.BehindBy = pc.BehindBy
	r.Behind = pc.behind()

	if pc.CommitTotal > 0 {
		r.Commits = pc.CommitTotal
	}

	if pc.BaseRef != "" {
		r.BaseRef = pc.BaseRef
	}

	if !c.contextAt.IsZero() {
		r.FetchedAt = clock.Format(c.contextAt)
	}

	if pc.AutoMerge != "" {
		r.AutoMerge = MergeMethod(pc.AutoMerge)
	}

	for _, f := range c.files {
		r.Files = append(r.Files, fileOf(f, matcher))
	}

	for _, rev := range pc.Reviews {
		entry := ReviewEntry{
			Author: rev.Author, State: rev.State, URL: rev.URL, Bot: rev.Bot,
			Outdated: rev.Commit != "" && !strings.EqualFold(rev.Commit, c.head),
		}

		if !rev.SubmittedAt.IsZero() {
			entry.SubmittedAt = clock.Format(rev.SubmittedAt)
		}

		r.Reviews = append(r.Reviews, entry)
	}
}

// withRequests adds who pull asks to review to reviews, each reviewer's latest review: a
// reviewer asked again is marked on their review, and each person and team asked who hasn't
// reviewed follows the reviews, people first, in GitHub's order. A team is named
// "org/team", its org the repository's owner. The signed-in user's own entry is marked.
func withRequests(reviews []ReviewEntry, pull StoredPull) []ReviewEntry {
	out := slices.Clone(reviews)

	for _, login := range pull.ReviewUsers {
		at := slices.IndexFunc(out, func(e ReviewEntry) bool { return !e.Team && strings.EqualFold(e.Author, login) })
		if at >= 0 {
			out[at].Requested = true

			continue
		}

		out = append(out, ReviewEntry{Author: login, Requested: true})
	}

	for _, slug := range pull.ReviewTeams {
		out = append(out, ReviewEntry{Author: pull.Owner + "/" + slug, Requested: true, Team: true})
	}

	for i := range out {
		out[i].Mine = !out[i].Team && pull.Viewer != "" && strings.EqualFold(out[i].Author, pull.Viewer)
	}

	return textutil.NonNil(out)
}

// fileOf is a cached file as the sheet shows it, with its sensitive category.
func fileOf(f cachedFile, matcher *sensitive.Matcher) File {
	out := File{
		Path: f.path, PreviousPath: f.previousPath, Status: f.status, Additions: f.additions, Deletions: f.deletions,
	}

	switch {
	case f.state == storedNoContent:
		out.PatchState = PatchNone
	case f.state == storedCapped:
		out.PatchState = PatchCapped
	case f.patch == "" && f.patchBytes > 0:
		out.PatchState = PatchOmitted
	default:
		out.Patch = f.patch
	}

	cat, _, ok := matcher.Match(f.path)
	if !ok && f.previousPath != "" {
		cat, _, ok = matcher.Match(f.previousPath)
	}

	if ok {
		out.Category, out.Level = cat.Short, cat.Level
	}

	return out
}

// patchOf is a cached file's patch from patches, and its state.
func patchOf(f cachedFile, patches map[int]string) (string, PatchState) {
	switch f.state {
	case storedNoContent:
		return "", PatchNone
	case storedCapped:
		return "", PatchCapped
	default:
		return patches[f.position], PatchShown
	}
}

// factsOf gathers what Decide needs from the stored pull request, its cache, and the
// sheet built from them.
func factsOf(pull StoredPull, c *cached, r Review) Facts {
	f := Facts{
		Mine: r.Mine, Draft: pull.IsDraft, CanWrite: github.CanWrite(pull.ViewerPermission),
		Dependabot:     r.IsBot && strings.HasPrefix(strings.ToLower(pull.AuthorLogin), "dependabot"),
		ReviewRequired: pull.ReviewDecision == "REVIEW_REQUIRED", ChangesRequested: pull.ReviewDecision == "CHANGES_REQUESTED",
		Base: r.BaseRef, Methods: methodsOf(pull), ViewerMethod: MergeMethod(pull.ViewerMergeMethod),
		AutoMerge: r.AutoMerge, Closed: r.State != github.StateOpen, Sensitive: []string{},
		AskedAgain:            github.ReviewRequests{Users: pull.ReviewUsers}.Asks(pull.Viewer),
		ChangesRequestedSince: r.YourReview.changesRequestedSince(),
	}

	workflows := workflowsCategory()

	for _, file := range r.Files {
		if file.Level == sensitive.LevelDanger && !slices.Contains(f.Sensitive, file.Category) {
			f.Sensitive = append(f.Sensitive, file.Category)
		}

		f.ChangesWorkflows = f.ChangesWorkflows || (workflows != "" && file.Category == workflows)
	}

	if c != nil {
		f.fromCache(pull, c)
	}

	return f
}

// fromCache takes into f what the cache c read of pull, and then what the inbox read of it
// since, when it did (inboxNewer).
func (f *Facts) fromCache(pull StoredPull, c *cached) {
	pc := c.context
	f.PendingReview, f.MergeState, f.MergeQueue = pc.PendingReviews > 0, pc.MergeState, pc.MergeQueue
	f.CanUpdateBranch, f.CanAutoMerge = pc.CanUpdateBranch, pc.CanAutoMerge && pull.AutoMergeAllowed
	f.CanBypass, f.CanClose, f.BehindBy = pc.CanBypass, pc.CanClose, pc.BehindBy
	f.ApprovedAtHead = pc.ViewerReview == "APPROVED" && strings.EqualFold(pc.ViewerReviewAt, c.head)
	f.countChecks(pc.Checks)
	f.countUnrun(pc.Unrun)

	if inboxNewer(pull, c) {
		f.fromInbox(pull, c)
	}
}

// inboxNewer reports that the inbox read the details of pull after the sheet last read it
// into c, as its refresh and its checks follower do while the sheet stays open or closed:
// their merge state, checks, and held runs are newer than the cache's. A cache whose read
// time is unknown (expire) is never older.
func inboxNewer(pull StoredPull, c *cached) bool {
	return c != nil && pull.HasDetails && !c.contextAt.IsZero() && pull.DetailsAt.After(c.contextAt)
}

// shownCounts are the check counts the sheet shows of pull: the inbox's while they are newer
// than the cache c, or the sheet never read the pull request, else what the cache's read
// counted, none from an older build's.
func shownCounts(pull StoredPull, c *cached) CheckCounts {
	if inboxNewer(pull, c) || (c == nil && pull.HasDetails) {
		return CheckCounts{
			State: pull.ChecksState, Passed: pull.ChecksPassed, Failed: pull.ChecksFailed, Pending: pull.ChecksPending,
			Skipped: pull.ChecksSkipped, At: stamp(pull.DetailsAt), Newer: true,
		}
	}

	if c == nil || c.context.Counts == nil {
		return CheckCounts{}
	}

	n := c.context.Counts

	return CheckCounts{
		State: n.State, Passed: n.Passed, Failed: n.Failed, Pending: n.Pending, Skipped: n.Skipped, At: stamp(c.contextAt),
	}
}

// fromInbox takes into f what the inbox's details of pull, newer than the cache c, say
// about where the pull request stands: GitHub's merge state, unless it was still working
// it out, the checks failing and running, and the held runs. The inbox counts no check as
// required, so the required ones are none while the merge state says nothing required
// holds the merge (CLEAN, HAS_HOOKS, or UNSTABLE), and otherwise the list's, at most as
// many as fail or run now. An approval names the runs the cache read, so it waits for a
// read while the inbox holds runs the cache doesn't, and a run that failed to start on
// another head than the inbox's says nothing of this one.
func (f *Facts) fromInbox(pull StoredPull, c *cached) {
	if pull.MergeState != "" && pull.MergeState != github.MergeStateUnknown {
		f.MergeState = pull.MergeState
	}

	f.Failing, f.Pending = pull.ChecksFailed, pull.ChecksPending

	if github.Mergeable(f.MergeState) || f.MergeState == github.MergeStateUnstable {
		f.RequiredFailing, f.RequiredPending = 0, 0
	} else {
		f.RequiredFailing, f.RequiredPending = min(f.RequiredFailing, f.Failing), min(f.RequiredPending, f.Pending)
	}

	f.countHeld(pull.HeldRuns, c.ignored)

	if !strings.EqualFold(c.head, pull.HeadOID) {
		f.NotStarted = 0
	}
}

// countHeld counts into f, in place of the runs the cache read, the held runs of a newer
// read, as countUnrun does, those of the workflows ignored named not counting. Approvable
// stays the runs the cache read, at most as many as an approval asks for now.
func (f *Facts) countHeld(runs []heldruns.Run, ignored []string) {
	read := f.Approvable
	f.AwaitingApproval, f.OnGitHub, f.Ignored, f.IgnoredApprovable = len(runs), 0, 0, 0

	for _, run := range runs {
		left := heldruns.Ignored(run, f.MergeState, ignored)
		if left {
			f.Ignored++
		}

		switch {
		case run.OnGitHub:
			f.OnGitHub++
		case left:
			f.IgnoredApprovable++
		}
	}

	f.Approvable = min(read, approvalAsked(*f))
}

// countUnrun counts the workflow runs that ended without a check into f: those awaiting
// approval, those of them GitHub approves only on its site, those that don't count (Ignored)
// and of them those the sheet could otherwise approve, those an approval can name (a run ID
// read, approved from the sheet, and not ignored), and those that failed to start.
func (f *Facts) countUnrun(unrun []Unrun) {
	for _, u := range unrun {
		if !u.Approval {
			f.NotStarted++

			continue
		}

		f.AwaitingApproval++

		if u.Ignored {
			f.Ignored++
		}

		switch {
		case u.OnGitHub:
			f.OnGitHub++
		case u.Ignored:
			f.IgnoredApprovable++
		case u.RunID > 0:
			f.Approvable++
		}
	}
}

// workflowsCategory is the short name a file in the workflows category carries
// (File.Category), or "" when the build has no such category.
func workflowsCategory() string {
	for _, c := range sensitive.Categories() {
		if c.ID == sensitive.CategoryWorkflows {
			return c.Short
		}
	}

	return ""
}

// countChecks counts the checks failing and pending into f, and those of them required.
func (f *Facts) countChecks(checks []Check) {
	for _, ch := range checks {
		switch ch.Outcome {
		case github.CheckFailed:
			f.Failing++

			if ch.Required {
				f.RequiredFailing++
			}
		case github.CheckPending:
			f.Pending++

			if ch.Required {
				f.RequiredPending++
			}
		}
	}
}

// methodsOf lists the merge methods the repository allows, in GitHub's order.
func methodsOf(pull StoredPull) []MergeMethod {
	out := []MergeMethod{}

	if pull.MergeCommitAllowed {
		out = append(out, MethodMerge)
	}

	if pull.SquashMergeAllowed {
		out = append(out, MethodSquash)
	}

	if pull.RebaseMergeAllowed {
		out = append(out, MethodRebase)
	}

	return out
}
