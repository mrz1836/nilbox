package review

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// dependabotRebase is the comment that asks Dependabot to rebase its pull request.
const dependabotRebase = "@dependabot rebase"

var (
	// ErrNotOffered is returned for an action the sheet does not offer for the pull request
	// now; nothing is sent.
	ErrNotOffered = errors.New("the action isn't available for the pull request now")

	// ErrNotRead is returned for an action on a pull request the sheet has not read from
	// GitHub yet.
	ErrNotRead = errors.New("the pull request has not been read yet")

	// ErrWarningsNotAccepted is returned for an action whose warnings the user did not
	// accept.
	ErrWarningsNotAccepted = errors.New("the action's warnings were not accepted")
)

// Act runs the action the user asked for on a pull request, and returns what it came to.
// It runs only what the sheet offers now, decided again on what the sheet read, only at
// the head the user saw (a pull request pushed to since returns OutcomePushedSince and
// sends nothing), and with its line comments checked against the diff. Every merge reads
// the merge state fresh first and merges only what GitHub says can merge now. A merge with
// no review of its own posts the user's text and line comments, when there are any, as a
// comment once that read says GitHub would merge (or, for auto-merge, that the pull request
// is open at the head the user saw), the way a comment is posted, and then merges as it
// would without them. A close reads the pull request first, and closes only one still open
// at the head the user saw, posting the text and line comments first the same way. A step
// GitHub made stands, so an approval or a comment GitHub took and a merge it would not
// make yet is a result, not an error, and once a merge's or a close's comment is posted
// nothing that fails after it is an error either.
func (s *Service) Act(ctx context.Context, req ActRequest) (Result, error) {
	pull, c, err := s.loadAct(ctx, req)
	if err != nil {
		return Result{}, err
	}

	if c == nil {
		// The sheet saw another head than the one cached: the reload it offers must read.
		s.settle(ctx, req.ItemID, false)

		return pushedSinceResult(pull.ref().String(), nil), nil
	}

	cfg, err := s.settings.Sensitive(ctx)
	if err != nil {
		return Result{}, err
	}

	pref, err := s.settings.MergeMethod(ctx)
	if err != nil {
		return Result{}, err
	}

	facts := factsOf(pull, c, build(pull, c, sensitive.NewMatcher(cfg), pref))
	plan := Decide(facts, pref)

	run, err := s.prepare(ctx, req, plan, c)
	if err != nil {
		return Result{}, err
	}

	run.ref, run.nodeID, run.canAuto = pull.ref(), pull.NodeID, facts.CanAutoMerge && !facts.AutoMergeOn
	run.itemID = req.ItemID
	run.step(s, "Checking your account")

	err = s.inbox.Act(ctx, req.ItemID, func(ctx context.Context, tok github.Token, _ Target) (Acted, error) {
		return run.do(ctx, s, tok)
	})

	s.settle(ctx, req.ItemID, run.gone())
	s.tellMerged(run.acted.Merged, pull.ref().Repo)

	if err != nil && !run.commentedFirst() {
		return Result{}, err
	}

	if err != nil {
		// The comment is on GitHub: the sheet must hear it was posted, whatever failed after.
		run.failedAfterComment(ctx, s, err)
	}

	return run.result(pull.ReviewRequested), nil
}

// tellMerged tells Merges, when there is one, the repository a merge that went through went
// into, so the Release page reads its row again.
func (s *Service) tellMerged(merged bool, name repo.Name) {
	if merged && s.merges != nil {
		s.merges.Merged(name)
	}
}

// settle brings the sheet's cache in line with an action that may have reached GitHub,
// whatever it came to: the cache of a pull request merged or closed (gone) is dropped, and
// any other's context marked out of date, since the action changed what it says (its
// reviews, its auto-merge, or its head), so the sheet opened again reads it rather than
// show the offers made before.
func (s *Service) settle(ctx context.Context, id int64, gone bool) {
	update := s.cache.expire
	if gone {
		update = s.cache.drop
	}

	if err := update(ctx, id); err != nil {
		s.logger.DebugContext(ctx, "could not update the cached review after an action", "error", err)
	}
}

// loadAct reads what an action needs: the stored pull request and the sheet's cache of
// it. The cache is nil, with no error, when the pull request was pushed to since the user
// saw it; an error means the action can't run at all.
func (s *Service) loadAct(ctx context.Context, req ActRequest) (StoredPull, *cached, error) {
	pull, err := s.pull(ctx, req.ItemID)
	if err != nil {
		return StoredPull{}, nil, err
	}

	c, err := s.cache.load(ctx, req.ItemID, 0)
	if err != nil {
		return StoredPull{}, nil, err
	}

	switch {
	case c == nil:
		return StoredPull{}, nil, uierr.New("Wait for the pull request to load from GitHub, then try again.",
			fmt.Errorf("%w (id %d)", ErrNotRead, req.ItemID))
	case !strings.EqualFold(req.Head, c.head) || (pull.HeadOID != "" && !strings.EqualFold(pull.HeadOID, c.head)):
		return pull, nil, nil
	default:
		return pull, c, nil
	}
}

// prepare checks a request against the plan and the cache before anything is sent: the
// action is offered and enabled, its warnings are accepted, its method is allowed, its
// review has the text it needs (and the comment a merge posts first is offered), and its
// line comments sit on the diff.
func (s *Service) prepare(ctx context.Context, req ActRequest, plan Plan, c *cached) (*action, error) {
	if err := checkOffer(req, plan); err != nil {
		return nil, err
	}

	method := cmp.Or(req.Method, plan.Method)

	if req.Action.merges() && !slices.Contains(plan.Methods, method) {
		return nil, uierr.New("The repository doesn't allow that merge method.",
			fmt.Errorf("%w: merge method %q", ErrNotOffered, method))
	}

	offer, _ := plan.offer(req.Action)

	reviewAs, err := reviewOf(req, offer)
	if err != nil {
		return nil, err
	}

	if err = s.checkComments(ctx, req, c, reviewAs); err != nil {
		return nil, err
	}

	return &action{
		kind: req.Action, reviewAs: reviewAs, commentFirst: offer.CommentsFirst, head: c.head, body: req.Body,
		comments: req.Comments, method: method, unstable: req.Accept,
	}, nil
}

// reviewOf is the review a request submits, as its offer says: the action's own, for one
// that reviews; a comment, for a merge that posts its text and line comments first and has
// either to post, while the offer says GitHub can take one; and ActionNone for anything
// else.
func reviewOf(req ActRequest, offer Offer) (Action, error) {
	switch {
	case offer.Reviews:
		return req.Action, nil
	case !offer.CommentsFirst || (!textutil.HasText(req.Body) && len(req.Comments) == 0):
		return ActionNone, nil
	case offer.CommentBlocked != "":
		return ActionNone, uierr.New(offer.CommentBlocked,
			fmt.Errorf("%w: a comment before %q: %s", ErrNotOffered, req.Action, offer.CommentBlocked))
	default:
		return ActionComment, nil
	}
}

// checkOffer checks that the plan offers the request's action now, that its warnings are
// accepted, and that a request for changes or a comment has something to say.
func checkOffer(req ActRequest, plan Plan) error {
	offer, ok := plan.offer(req.Action)

	switch {
	case !ok:
		return uierr.New("That isn't something the sheet can do with this pull request.",
			fmt.Errorf("%w: %q", ErrNotOffered, req.Action))
	case !offer.Enabled:
		return uierr.New(offer.Reason, fmt.Errorf("%w: %q: %s", ErrNotOffered, req.Action, offer.Reason))
	case len(offer.Warnings) > 0 && !req.Accept:
		return uierr.New("Accept the warnings first.", fmt.Errorf("%w: %q", ErrWarningsNotAccepted, req.Action))
	case (req.Action == ActionRequestChanges || req.Action == ActionComment) && !textutil.HasText(req.Body) &&
		len(req.Comments) == 0:
		return uierr.New("Write something, or add a line comment, first.",
			fmt.Errorf("%w: %q needs text", ErrNotOffered, req.Action))
	default:
		return nil
	}
}

// checkComments checks each line comment against its file's cached diff: they go with the
// review the action submits, reviewAs, and are refused when it submits none.
func (s *Service) checkComments(ctx context.Context, req ActRequest, c *cached, reviewAs Action) error {
	if len(req.Comments) == 0 {
		return nil
	}

	if reviewAs == ActionNone {
		return uierr.New("Line comments go with a review.", fmt.Errorf("%w: comments on %q", ErrNotOffered, req.Action))
	}

	byPath, patches, err := s.commentedPatches(ctx, req, c)
	if err != nil {
		return err
	}

	for _, cm := range req.Comments {
		f, ok := byPath[cm.Path]
		patch, has := patches[f.position]

		if err = checkComment(cm, patch, ok && has); err != nil {
			return err
		}
	}

	return nil
}

// commentedPatches reads the cached patches of the files the request's comments are on,
// with the cached files by path.
func (s *Service) commentedPatches(
	ctx context.Context, req ActRequest, c *cached,
) (map[string]cachedFile, map[int]string, error) {
	byPath := filesByPath(c.files)
	positions := make([]int64, 0, len(req.Comments))

	for _, cm := range req.Comments {
		if f, ok := byPath[cm.Path]; ok {
			positions = append(positions, int64(f.position))
		}
	}

	patches, err := s.cache.patches(ctx, req.ItemID, positions)

	return byPath, patches, err
}

// filesByPath indexes cached files by path.
func filesByPath(files []cachedFile) map[string]cachedFile {
	out := make(map[string]cachedFile, len(files))
	for _, f := range files {
		out[f.path] = f
	}

	return out
}

// action is one action as it runs: what was asked for, and what GitHub made happen so far,
// kept across a retry with a fresh token, so no step is ever made twice.
type action struct {
	kind Action

	// reviewAs is the review the action submits: its own, the comment a merge or a close
	// posts first, or ActionNone. commentFirst reports a merge or a close whose offer says it
	// posts the text first, with or without any to post, and fellBack an auto-merge GitHub
	// said could merge now, which merges instead.
	reviewAs     Action
	commentFirst bool
	fellBack     bool

	head     string
	body     string
	comments []LineComment
	method   MergeMethod

	// unstable reports that the user accepted merging with checks failing that are not
	// required, and canAuto that GitHub lets them turn on auto-merge.
	unstable bool
	canAuto  bool

	ref    item.Ref
	nodeID string
	itemID int64

	// bypassing reports a merge going past the branch's rules, as the user chose.
	bypassing bool

	// What GitHub made happen.
	reviewed *github.SubmittedReview
	acted    Acted
	outcome  Outcome
	reason   string
	steps    []string
}

// step tells the window what the action is doing now.
func (a *action) step(s *Service, doing string) {
	s.events.Emit(event.ReviewProgress{ItemID: a.itemID, Step: doing})
}

// do runs the action with tok, from where an earlier try left off. A review goes first,
// but for the comment a merge posts first, which goes only once the merge state says
// GitHub would make the merge (postFirst), so a merge GitHub refuses strands no comment.
func (a *action) do(ctx context.Context, s *Service, tok github.Token) (Acted, error) {
	if a.reviewAs != ActionNone && !a.commentFirst {
		if err := a.review(ctx, s, tok); err != nil {
			return a.acted, err
		}

		if a.outcome == OutcomePushedSince {
			return a.acted, nil
		}
	}

	return a.acted, a.follow(ctx, s, tok)
}

// approved reports an approval GitHub took, after which a merge GitHub holds back for its
// requirements turns on auto-merge instead, and anything it won't merge is a result. The
// comment a merge posts first is no approval, so a merge after it does only what it would
// without it.
func (a *action) approved() bool {
	return a.reviewed != nil && reviewEvent(a.reviewAs) == github.ReviewApprove
}

// gone reports a pull request the action merged or closed, which leaves every list.
func (a *action) gone() bool {
	return a.acted.Merged || a.acted.Closed
}

// commentedFirst reports a merge or a close that posted the user's text as a comment
// before it merged or closed.
func (a *action) commentedFirst() bool {
	return a.reviewed != nil && a.commentFirst
}

// postFirst posts the comment a merge with no review of its own, or a close, posts first,
// once, when it has one: right before the action's own write, after the pull request was
// read.
func (a *action) postFirst(ctx context.Context, s *Service, tok github.Token) error {
	if !a.commentFirst || a.reviewAs == ActionNone {
		return nil
	}

	return a.review(ctx, s, tok)
}

// failedAfterComment turns an error that came after the comment a merge or a close posted
// first into the action's result, so the sheet hears the comment stands and never posts it
// twice. A step GitHub had answered (the merge, auto-merge, the close, a refusal, or a
// push it found) stands, and the error, which then came from the inbox's own bookkeeping,
// is only logged; any other is OutcomeCommentedNotMerged, or OutcomeCommentedNotClosed for
// a close, with the error's words as its reason.
func (a *action) failedAfterComment(ctx context.Context, s *Service, err error) {
	if a.outcome != OutcomeReviewed {
		s.logger.WarnContext(ctx, "an action's step failed after GitHub answered it", "outcome", a.outcome, "error", err)

		return
	}

	a.outcome, a.reason = a.notDone(), strings.TrimSuffix(uierr.Message(err), ".")
}

// notDone is the outcome of an action whose comment GitHub took and whose own step it
// didn't make: a close's, or a merge's.
func (a *action) notDone() Outcome {
	if a.kind == ActionClose {
		return OutcomeCommentedNotClosed
	}

	return OutcomeCommentedNotMerged
}

// follow makes the steps of the action that come after its review, if any.
func (a *action) follow(ctx context.Context, s *Service, tok github.Token) error {
	switch a.kind {
	case ActionApprove, ActionRequestChanges, ActionComment:
		return nil
	case ActionApproveMerge, ActionMerge, ActionMergeBypass:
		return a.guardedMerge(ctx, s, tok)
	case ActionApproveAutoMerge, ActionAutoMerge:
		return a.autoMerge(ctx, s, tok)
	case ActionUpdateBranch:
		return a.updateBranch(ctx, s, tok)
	case ActionRebaseBot:
		return a.askBot(ctx, s, tok)
	case ActionClose:
		return a.closePull(ctx, s, tok)
	case ActionNone:
		return fmt.Errorf("%w: no action", ErrNotOffered)
	default:
		return fmt.Errorf("%w: %q", ErrNotOffered, a.kind)
	}
}

// review submits the action's review once, pinned to the head. GitHub takes a review of
// a commit older than the head, and an approval of one would count toward merging commits
// the user never saw, so an approval first reads the head and is not sent once it moved.
// A comment or a request for changes is about the code the user saw, and stays on it, the
// comment a merge posts first included.
func (a *action) review(ctx context.Context, s *Service, tok github.Token) error {
	if a.reviewed != nil {
		return nil
	}

	if reviewEvent(a.reviewAs) == github.ReviewApprove {
		a.step(s, "Checking nothing was pushed since you opened it")

		moved, err := a.headMoved(ctx, s, tok)
		if err != nil || moved {
			return err
		}
	}

	if a.commentFirst {
		a.step(s, "Posting your comment")
	} else {
		a.step(s, reviewDoing(a.reviewAs))
	}

	in := github.ReviewInput{Event: reviewEvent(a.reviewAs), Body: a.body, CommitID: a.head}

	for _, cm := range a.comments {
		in.Comments = append(in.Comments, github.LineComment{
			Path: cm.Path, Line: cm.Line, Side: string(cm.Side), StartLine: cm.StartLine, StartSide: string(cm.StartSide),
			Body: cm.Body,
		})
	}

	submitted, err := s.github.SubmitReview(ctx, tok, a.ref, in)
	if err != nil {
		return err
	}

	a.reviewed = submitted
	a.acted.Reviewed, a.acted.ReviewedAt = true, submitted.SubmittedAt
	a.outcome = OutcomeReviewed
	a.steps = append(a.steps, reviewStep(a.reviewAs))

	return nil
}

// headMoved reads the pull request's head, and reports a head other than the one the user
// saw: a pushed-since outcome while the action has posted nothing yet. After a close's
// comment, the caller words it (closePull).
func (a *action) headMoved(ctx context.Context, s *Service, tok github.Token) (bool, error) {
	state, err := s.github.FetchMergeState(ctx, tok, a.nodeID)
	if err != nil {
		return false, err
	}

	if !state.Found || state.State != github.StateOpen {
		return false, uierr.New("It's no longer open.", fmt.Errorf("%w: %s", ErrNotOffered, state.State))
	}

	if !strings.EqualFold(state.Head, a.head) {
		if a.reviewed == nil {
			a.outcome = OutcomePushedSince
		}

		return true, nil
	}

	return false, nil
}

// reviewEvent is the review an action submits.
func reviewEvent(kind Action) string {
	if kind == ActionRequestChanges {
		return github.ReviewRequestChanges
	}

	if kind == ActionComment {
		return github.ReviewComment
	}

	return github.ReviewApprove
}

// reviewDoing says the review an action is submitting.
func reviewDoing(kind Action) string {
	if kind == ActionRequestChanges {
		return "Requesting changes"
	}

	if kind == ActionComment {
		return "Commenting"
	}

	return "Approving"
}

// reviewStep names the review an action submitted.
func reviewStep(kind Action) string {
	if kind == ActionRequestChanges {
		return "Requested changes"
	}

	if kind == ActionComment {
		return "Commented"
	}

	return "Approved"
}

// updateBranch merges the base branch into the pull request's branch.
func (a *action) updateBranch(ctx context.Context, s *Service, tok github.Token) error {
	a.step(s, "Updating the branch")

	if _, err := s.github.UpdateBranch(ctx, tok, a.ref, a.head); err != nil {
		if errors.Is(err, github.ErrPushedSince) {
			a.outcome = OutcomePushedSince

			return nil
		}

		return err
	}

	a.outcome = OutcomeBranchUpdating
	a.steps = append(a.steps, "Updating the branch")

	return nil
}

// askBot asks Dependabot to rebase.
func (a *action) askBot(ctx context.Context, s *Service, tok github.Token) error {
	a.step(s, "Asking Dependabot to rebase")

	if _, err := s.github.AddComment(ctx, tok, a.ref, dependabotRebase); err != nil {
		return err
	}

	a.outcome = OutcomeAskedBot
	a.steps = append(a.steps, "Asked Dependabot to rebase")

	return nil
}

// closePull closes the pull request without merging it. GitHub takes no head to pin a
// close to, so every try, a retry with a fresh token included, first reads the pull
// request: one no longer open is refused, and a head other than the one the user saw stops
// it, as a pushed-since outcome while nothing was posted, so neither the comment nor the
// close goes. The comment it posts first, if any, goes next, once, and the close last. Any
// failure is returned as it is: the inbox's Actor tries a refused token again, and once the
// comment is posted, failedAfterComment makes whatever failed after it the result.
func (a *action) closePull(ctx context.Context, s *Service, tok github.Token) error {
	a.step(s, "Checking nothing was pushed since you opened it")

	moved, err := a.headMoved(ctx, s, tok)

	switch {
	case err != nil || (moved && a.reviewed == nil):
		return err
	case moved:
		// The comment went on an earlier try and stands; the close doesn't go.
		return uierr.New("It was pushed to since you opened it.",
			fmt.Errorf("close %s after the comment: %w", a.ref, github.ErrPushedSince))
	}

	if err = a.postFirst(ctx, s, tok); err != nil {
		return err
	}

	a.step(s, "Closing")

	if err = s.github.ClosePull(ctx, tok, a.ref); err != nil {
		return err
	}

	a.acted.Closed = true
	a.outcome = OutcomeClosed
	a.steps = append(a.steps, "Closed")

	return nil
}
