// Package review is the review sheet: it shows one pull request (what it changes, its
// description, checks, reviews, and what it still needs to merge) and acts on it at the
// user's click: approve, request changes, comment, merge, turn on auto-merge, update the
// branch, ask Dependabot to rebase, or close it.
//
// Open shows what the sheet read before, from its own cache, with no request; Refresh
// reads the pull request again with one GraphQL request, and its files only when it was
// pushed to since. Every action is pinned to the head commit the user saw, goes through
// the inbox's Actor (the item's lock and the account check) and the GitHub client's write
// gate, and is decided again (decide.go) before anything is sent. A merge never goes around
// branch protection: it reads the merge state fresh first and merges only what GitHub says
// can merge now. Agents never approve or merge; only the user's click does.
//
// Line comments are checked against the cached diff (hunks.go) before GitHub sees them.
// The package reaches the inbox, the settings, and the prompt library only through the
// interfaces it declares, which the app's core adapts.
package review

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/bump"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/flight"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// refreshAfter is how long a context read from GitHub counts as current, so a sheet
// opened again at once asks nothing.
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

	Reviewers        int
	Reviewed         int
	Approvals        int
	ChangesRequested int

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
type Acted struct {
	Reviewed   bool
	ReviewedAt time.Time
	Merged     bool
	Closed     bool
}

// PullRead is what a read found. Its fields match inbox.PullRead's.
type PullRead struct {
	Update *github.ItemUpdate
	Files  *github.ChangedFiles
}

// Inbox is what the sheet needs of the inbox: a stored pull request, and a read and an
// action run with the inbox's footing (inbox.Actor).
type Inbox interface {
	Pull(ctx context.Context, id int64) (StoredPull, error)
	Read(ctx context.Context, id int64, read func(context.Context, github.Token, Target) (PullRead, error)) error
	Act(ctx context.Context, id int64, act func(context.Context, github.Token, Target) (Acted, error)) error
}

// GitHub is the GitHub client's reads and writes the sheet uses. *github.Client
// implements it.
type GitHub interface {
	FetchPullReview(ctx context.Context, tok github.Token, id string) (*github.PullReview, error)
	FetchPullDiff(ctx context.Context, tok github.Token, ref item.Ref, head string) (*github.PullDiff, error)
	FetchMergeState(ctx context.Context, tok github.Token, id string) (*github.MergeState, error)
	SubmitReview(ctx context.Context, tok github.Token, ref item.Ref, in github.ReviewInput) (*github.SubmittedReview, error)
	MergePull(ctx context.Context, tok github.Token, ref item.Ref, in github.MergeInput) (*github.Merged, error)
	EnableAutoMerge(
		ctx context.Context, tok github.Token, ref item.Ref, nodeID, method, expectedHead string,
	) (*github.AutoMerge, error)
	UpdateBranch(ctx context.Context, tok github.Token, ref item.Ref, expectedHead string) (*github.BranchUpdate, error)
	AddComment(ctx context.Context, tok github.Token, ref item.Ref, body string) (*github.Comment, error)
	ClosePull(ctx context.Context, tok github.Token, ref item.Ref) (*github.Closed, error)
}

// Settings reads the settings the sheet follows: how to merge, and which files are
// sensitive.
type Settings interface {
	MergeMethod(ctx context.Context) (MethodPref, error)
	Sensitive(ctx context.Context) (sensitive.Config, error)
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

// Config holds a Service's dependencies. Every one but Merges, Clock, and Logger is
// required.
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
	clock    clock.Clock
	logger   *slog.Logger

	// refreshes reads each pull request once at a time.
	refreshes flight.Group[Review]
}

// NewService returns a Service over cfg.
func NewService(cfg Config) *Service {
	return &Service{
		inbox: cfg.Inbox, github: cfg.GitHub, settings: cfg.Settings, replies: cfg.Replies,
		cache: newCacheStore(cfg.Pool), events: event.Or(cfg.Events), merges: cfg.Merges,
		clock: clock.Or(cfg.Clock), logger: logging.Or(cfg.Logger),
	}
}

// Open returns the pull request with id as the sheet last read it, from its cache and the
// inbox, with no request: at once, and offline. Stale says whether a Refresh is due.
func (s *Service) Open(ctx context.Context, id int64) (Review, error) {
	return s.open(ctx, id, openPatchBytes)
}

// Refresh reads the pull request with id from GitHub, stores it, and returns it: one
// GraphQL request, and its files only when it was pushed to since the sheet listed them.
// A pull request read in the last 20 seconds is not read again. Two refreshes of one pull
// request never run side by side.
func (s *Service) Refresh(ctx context.Context, id int64) (Review, error) {
	return s.refresh(ctx, id, false)
}

// ReadAgain reads the pull request with id from GitHub as Refresh does, however recently
// it was read: the user asked for it, from the sheet's refresh button. A read already
// under way for the pull request is joined rather than run beside it.
func (s *Service) ReadAgain(ctx context.Context, id int64) (Review, error) {
	return s.refresh(ctx, id, true)
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

// Replies returns the review replies for action, with the defaults.
func (s *Service) Replies(ctx context.Context, action ReplyAction) (ReplyList, error) {
	list, err := s.replies.Offered(ctx, action)
	if err != nil {
		return ReplyList{}, err
	}

	if list.Replies == nil {
		list.Replies = []Reply{}
	}

	return list, nil
}

// FillReply fills in the reply with replyID for the pull request with itemID, as the
// text the review starts with.
func (s *Service) FillReply(ctx context.Context, itemID, replyID int64) (string, error) {
	return s.replies.Fill(ctx, replyID, itemID)
}

// refresh reads the pull request with id, skipping one read recently unless force is set,
// and returns it as Open builds it.
func (s *Service) refresh(ctx context.Context, id int64, force bool) (Review, error) {
	return s.refreshes.Do(ctx, strconv.FormatInt(id, 10), func(ctx context.Context) (Review, error) {
		if err := s.read(ctx, id, force); err != nil {
			return Review{}, err
		}

		return s.open(ctx, id, openPatchBytes)
	})
}

// open builds the sheet from the inbox's stored pull request and the cache, with up to
// budget bytes of patches, and stamps the cache's use.
func (s *Service) open(ctx context.Context, id, budget int64) (Review, error) {
	pull, err := s.pull(ctx, id)
	if err != nil {
		return Review{}, err
	}

	c, err := s.cache.load(ctx, id, budget)
	if err != nil {
		return Review{}, err
	}

	if c != nil {
		if touchErr := s.cache.touch(ctx, id, s.now()); touchErr != nil {
			s.logger.DebugContext(ctx, "could not stamp the cached review", "error", touchErr)
		}
	}

	cfg, err := s.settings.Sensitive(ctx)
	if err != nil {
		return Review{}, err
	}

	pref, err := s.settings.MergeMethod(ctx)
	if err != nil {
		return Review{}, err
	}

	return build(pull, c, sensitive.NewMatcher(cfg), pref), nil
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
// head the inbox knows, is not read again, unless GitHub was still working out whether it
// can merge (the sheet reads again for the answer) or force is set.
func (s *Service) read(ctx context.Context, id int64, force bool) error {
	c, err := s.cache.load(ctx, id, 0)
	if err != nil {
		return err
	}

	pull, err := s.pull(ctx, id)
	if err != nil {
		return err
	}

	if !force && c != nil && strings.EqualFold(c.head, pull.HeadOID) && s.now().Sub(c.contextAt) < refreshAfter &&
		c.context.MergeState != github.MergeStateUnknown {
		return nil
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
		return err
	}

	return s.store(ctx, id, got.review, got.diff)
}

// fetch reads the pull request's context and, unless c holds its files at the head the
// context names, its files. When it was pushed to between the two reads, the context is
// read again for the new head.
func (s *Service) fetch(
	ctx context.Context, tok github.Token, t Target, c *cached,
) (*github.PullReview, *github.PullDiff, error) {
	review, err := s.github.FetchPullReview(ctx, tok, t.NodeID)
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
		if review, err = s.github.FetchPullReview(ctx, tok, t.NodeID); err != nil {
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
// closed, whose cache is dropped; else its context, and its files when they were listed.
func (s *Service) store(ctx context.Context, id int64, review *github.PullReview, diff *github.PullDiff) error {
	if review == nil {
		return nil
	}

	if !review.Update.Found || review.Update.Item.Closed() {
		return s.cache.drop(ctx, id)
	}

	return s.cache.save(ctx, id, contextOf(review), diff, s.now())
}

// now returns the current time in UTC.
func (s *Service) now() time.Time {
	return s.clock.Now().UTC()
}

// build makes the sheet from the stored pull request and its cache (nil when the sheet
// never read it), judging its files with matcher and its merge with pref.
func build(pull StoredPull, c *cached, matcher *sensitive.Matcher, pref MethodPref) Review {
	r := Review{
		ID: pull.ID, Repo: pull.ref().Repo.String(), Number: pull.Number, Title: pull.Title, URL: pull.URL,
		State: github.StateOpen, Author: pull.AuthorLogin, IsBot: strings.EqualFold(pull.AuthorType, "Bot"),
		Mine: strings.EqualFold(pull.AuthorLogin, pull.Viewer), Draft: pull.IsDraft, HeadRef: pull.HeadRef,
		BaseRef: pull.DefaultBranch, ReviewRequested: pull.ReviewRequested, Labels: nonNil(pull.Labels),
		Additions: pull.Additions, Deletions: pull.Deletions, Commits: pull.Commits, Comments: pull.Comments,
		AutoMerge: MergeMethod(pull.AutoMerge), Head: pull.HeadOID, Files: []File{}, Checks: []Check{},
		Reviews: []ReviewEntry{}, TotalFiles: pull.ChangedFiles, Stale: true,
	}

	if u, ok := bump.Parse(pull.Title); ok && r.IsBot {
		r.Bump = &u
	}

	if c != nil {
		r.fill(c, matcher)
	}

	r.Change = changeOf(r.Files, r.Bump != nil)

	plan := Decide(factsOf(pull, c, r), pref)
	r.Offers, r.Primary, r.Readiness = nonNil(plan.Offers), plan.Primary, plan.Readiness
	r.MergeMethods, r.DefaultMethod = plan.Methods, plan.Method

	return r
}

// fill adds what the cache holds to r.
func (r *Review) fill(c *cached, matcher *sensitive.Matcher) {
	pc := c.context
	r.Head, r.State, r.Body, r.Stale = c.head, nonEmpty(pc.State, github.StateOpen), pc.Body, !strings.EqualFold(c.head, r.Head)
	r.Mine = r.Mine || pc.Mine
	r.TotalFiles, r.FilesComplete = c.total, c.complete
	r.Checks = nonNil(pc.Checks)

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
			Author: rev.Author, State: rev.State, Body: rev.Body, URL: rev.URL,
			Outdated: rev.Commit != "" && !strings.EqualFold(rev.Commit, c.head),
		}

		if !rev.SubmittedAt.IsZero() {
			entry.SubmittedAt = clock.Format(rev.SubmittedAt)
		}

		r.Reviews = append(r.Reviews, entry)
	}
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
		Mine: r.Mine, Draft: pull.IsDraft, CanWrite: canWrite(pull.ViewerPermission),
		CanClose:       r.Mine || canClose(pull.ViewerPermission),
		Dependabot:     r.IsBot && strings.HasPrefix(strings.ToLower(pull.AuthorLogin), "dependabot"),
		ReviewRequired: pull.ReviewDecision == "REVIEW_REQUIRED", ChangesRequested: pull.ReviewDecision == "CHANGES_REQUESTED",
		Base: r.BaseRef, Methods: methodsOf(pull), ViewerMethod: MergeMethod(pull.ViewerMergeMethod),
		AutoMergeOn: r.AutoMerge != "", Closed: r.State != github.StateOpen, Sensitive: []string{},
	}

	for _, file := range r.Files {
		if file.Level == sensitive.LevelDanger && !slices.Contains(f.Sensitive, file.Category) {
			f.Sensitive = append(f.Sensitive, file.Category)
		}
	}

	if c == nil {
		return f
	}

	pc := c.context
	f.PendingReview, f.MergeState, f.MergeQueue = pc.PendingReviews > 0, pc.MergeState, pc.MergeQueue
	f.CanUpdateBranch, f.CanAutoMerge = pc.CanUpdateBranch, pc.CanAutoMerge && pull.AutoMergeAllowed
	f.CanBypass = pc.CanBypass
	f.ApprovedAtHead = pc.ViewerReview == "APPROVED" && strings.EqualFold(pc.ViewerReviewAt, c.head)
	f.countChecks(pc.Checks)

	return f
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

// canWrite reports write access or more.
func canWrite(permission string) bool {
	switch permission {
	case "ADMIN", "MAINTAIN", "WRITE":
		return true
	default:
		return false
	}
}

// canClose reports triage access or more, which lets a user close anyone's pull request.
func canClose(permission string) bool {
	return permission == "TRIAGE" || canWrite(permission)
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

// nonNil returns s, or an empty slice for nil.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}

	return s
}

// nonEmpty returns s, or fallback when s is "".
func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}

	return s
}
