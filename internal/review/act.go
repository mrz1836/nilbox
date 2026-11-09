package review

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// guardReads and guardWait bound the fresh reads of the merge state a merge makes while
// GitHub still works it out (UNKNOWN), as it does just after an approval.
const (
	guardReads = 3
	guardWait  = time.Second
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
// the merge state fresh first and merges only what GitHub says can merge now. A step GitHub
// made stands, so an approval GitHub took and a merge it would not make yet is a result,
// not an error.
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

	s.settle(ctx, req.ItemID, run.acted.Merged)

	if err != nil {
		return Result{}, err
	}

	return run.result(pull.ReviewRequested), nil
}

// settle brings the sheet's cache in line with an action that may have reached GitHub,
// whatever it came to: a merged pull request's cache is dropped, and any other's context
// marked out of date, since the action changed what it says (its reviews, its auto-merge,
// or its head), so the sheet opened again reads it rather than show the offers made before.
func (s *Service) settle(ctx context.Context, id int64, merged bool) {
	update := s.cache.expire
	if merged {
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
// review has the text it needs, and its line comments sit on the diff.
func (s *Service) prepare(ctx context.Context, req ActRequest, plan Plan, c *cached) (*action, error) {
	if err := checkOffer(req, plan); err != nil {
		return nil, err
	}

	method := req.Method
	if method == "" {
		method = plan.Method
	}

	if req.Action.merges() && !containsMethod(plan.Methods, method) {
		return nil, uierr.New("The repository doesn't allow that merge method.",
			fmt.Errorf("%w: merge method %q", ErrNotOffered, method))
	}

	if err := s.checkComments(ctx, req, c); err != nil {
		return nil, err
	}

	return &action{
		kind: req.Action, head: c.head, body: req.Body, comments: req.Comments, method: method, unstable: req.Accept,
	}, nil
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
	case (req.Action == ActionRequestChanges || req.Action == ActionComment) && strings.TrimSpace(req.Body) == "" &&
		len(req.Comments) == 0:
		return uierr.New("Write something, or add a line comment, first.",
			fmt.Errorf("%w: %q needs text", ErrNotOffered, req.Action))
	default:
		return nil
	}
}

// checkComments checks each line comment against its file's cached diff.
func (s *Service) checkComments(ctx context.Context, req ActRequest, c *cached) error {
	if len(req.Comments) == 0 {
		return nil
	}

	if !req.Action.reviews() {
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

// containsMethod reports whether methods holds m.
func containsMethod(methods []MergeMethod, m MergeMethod) bool {
	for _, x := range methods {
		if x == m {
			return true
		}
	}

	return false
}

// action is one action as it runs: what was asked for, and what GitHub made happen so far,
// kept across a retry with a fresh token, so no step is ever made twice.
type action struct {
	kind     Action
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

// do runs the action with tok, from where an earlier try left off.
func (a *action) do(ctx context.Context, s *Service, tok github.Token) (Acted, error) {
	if a.kind.reviews() {
		if err := a.review(ctx, s, tok); err != nil {
			return a.acted, err
		}

		if a.outcome == OutcomePushedSince {
			return a.acted, nil
		}
	}

	return a.acted, a.follow(ctx, s, tok)
}

// follow makes the steps of the action that come after its review, if any.
func (a *action) follow(ctx context.Context, s *Service, tok github.Token) error {
	switch a.kind {
	case ActionApprove, ActionRequestChanges, ActionComment:
		return nil
	case ActionApproveMerge, ActionMerge, ActionMergeBypass:
		return a.guardedMerge(ctx, s, tok, a.reviewed != nil)
	case ActionApproveAutoMerge, ActionAutoMerge:
		return a.autoMerge(ctx, s, tok, a.reviewed != nil)
	case ActionUpdateBranch:
		return a.updateBranch(ctx, s, tok)
	case ActionRebaseBot:
		return a.askBot(ctx, s, tok)
	case ActionNone:
		return fmt.Errorf("%w: no action", ErrNotOffered)
	default:
		return fmt.Errorf("%w: %q", ErrNotOffered, a.kind)
	}
}

// review submits the action's review once, pinned to the head. GitHub takes a review of
// a commit older than the head, and an approval of one would count toward merging commits
// the user never saw, so an approval first reads the head and is not sent once it moved.
// A comment or a request for changes is about the code the user saw, and stays on it.
func (a *action) review(ctx context.Context, s *Service, tok github.Token) error {
	if a.reviewed != nil {
		return nil
	}

	if reviewEvent(a.kind) == github.ReviewApprove {
		a.step(s, "Checking nothing was pushed since you opened it")

		moved, err := a.headMoved(ctx, s, tok)
		if err != nil || moved {
			return err
		}
	}

	a.step(s, reviewDoing(a.kind))

	in := github.ReviewInput{Event: reviewEvent(a.kind), Body: a.body, CommitID: a.head}

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
	a.steps = append(a.steps, reviewStep(a.kind))

	return nil
}

// headMoved reads the pull request's head, and reports, as a pushed-since outcome, a head
// other than the one the user saw.
func (a *action) headMoved(ctx context.Context, s *Service, tok github.Token) (bool, error) {
	state, err := s.github.FetchMergeState(ctx, tok, a.nodeID)
	if err != nil {
		return false, err
	}

	if !state.Found || state.State != github.StateOpen {
		return false, uierr.New("It's no longer open.", fmt.Errorf("%w: %s", ErrNotOffered, state.State))
	}

	if !strings.EqualFold(state.Head, a.head) {
		a.outcome = OutcomePushedSince

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

// guardedMerge reads the merge state fresh and merges only what GitHub says can merge now
// (UNSTABLE only when the user accepted it). After an approval, a pull request still
// blocked on requirements gets auto-merge instead, where GitHub allows it, and anything
// GitHub would not merge is a result; without an approval, it is an error, since nothing
// happened, and the sheet offers auto-merge itself.
func (a *action) guardedMerge(ctx context.Context, s *Service, tok github.Token, approved bool) error {
	state, err := a.guard(ctx, s, tok)
	if err != nil {
		if github.IsBadCredentials(err) || ctx.Err() != nil {
			return err
		}

		return a.notMerged(approved, err)
	}

	switch a.decideMerge(state, approved) {
	case mergeStepPushed:
		a.outcome = OutcomePushedSince

		return nil
	case mergeStepMerge:
		a.bypassing = !a.mergesNow(state)

		return a.merge(ctx, s, tok, approved)
	case mergeStepAuto:
		return a.enableAutoMerge(ctx, s, tok, approved)
	case mergeStepRefuse:
		return a.notMerged(approved, uierr.New(stateReason(state.Status),
			fmt.Errorf("%w: %s", github.ErrNotMergeable, state.Status)))
	default:
		return fmt.Errorf("%w: merge step %d", ErrNotOffered, a.decideMerge(state, approved))
	}
}

// mergeStep is what a guarded merge does once it read the merge state.
type mergeStep int

const (
	mergeStepRefuse mergeStep = iota
	mergeStepPushed
	mergeStepMerge
	mergeStepAuto
)

// decideMerge says what a guarded merge does in state: refuse a head that moved, merge
// what GitHub would merge now (UNSTABLE only when the user accepted it), merge past the
// rules what they hold back, when that is what the user chose and GitHub still lets them,
// turn on auto-merge for what is blocked once approved, where GitHub allows it, and else
// refuse.
func (a *action) decideMerge(state *github.MergeState, approved bool) mergeStep {
	held := state.Status == github.MergeStateBlocked || state.Status == github.MergeStateBehind

	switch {
	case !strings.EqualFold(state.Head, a.head):
		return mergeStepPushed
	case a.mergesNow(state):
		return mergeStepMerge
	case a.kind == ActionMergeBypass && held && state.CanBypass:
		return mergeStepMerge
	case state.Status == github.MergeStateBlocked && a.canAuto && approved:
		return mergeStepAuto
	default:
		return mergeStepRefuse
	}
}

// guard reads the merge state, up to guardReads times guardWait apart while GitHub still
// works it out.
func (a *action) guard(ctx context.Context, s *Service, tok github.Token) (*github.MergeState, error) {
	a.step(s, "Checking that GitHub can merge it")

	for read := 1; ; read++ {
		state, err := s.github.FetchMergeState(ctx, tok, a.nodeID)
		if err != nil {
			return nil, err
		}

		if err = mergeableState(state); err != nil {
			return nil, err
		}

		if state.Status != github.MergeStateUnknown || read >= guardReads {
			return state, nil
		}

		a.step(s, "Waiting for GitHub to work out whether it can merge")

		if err = wait(ctx, guardWait); err != nil {
			return nil, err
		}
	}
}

// mergeableState refuses a pull request no merge can take: one no longer open, a draft,
// or one on a merge queue.
func mergeableState(state *github.MergeState) error {
	switch {
	case !state.Found || state.State != github.StateOpen:
		return uierr.New("It's no longer open.", fmt.Errorf("%w: %s", github.ErrNotMergeable, state.State))
	case state.IsDraft || state.MergeQueue:
		return uierr.New("It's a draft, or merges through a merge queue.",
			fmt.Errorf("%w: draft or queue", github.ErrNotMergeable))
	default:
		return nil
	}
}

// wait waits for d, or returns ctx's error once it ends.
func wait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// merge merges the pull request at the head, falling back to auto-merge when GitHub
// refuses for requirements it waits on.
func (a *action) merge(ctx context.Context, s *Service, tok github.Token, approved bool) error {
	a.step(s, strings.TrimSuffix(mergeLabel(a.method), "e")+"ing"+a.past())

	_, err := s.github.MergePull(ctx, tok, a.ref, github.MergeInput{SHA: a.head, Method: string(a.method)})

	switch {
	case err == nil:
		a.acted.Merged = true
		a.outcome = OutcomeMerged
		a.steps = append(a.steps, "Merged"+a.past()+" ("+strings.ToLower(string(a.method))+")")

		return nil
	case errors.Is(err, github.ErrPushedSince):
		a.outcome = OutcomePushedSince

		return nil
	case errors.Is(err, github.ErrNotMergeable) && a.canAuto && approved:
		return a.enableAutoMerge(ctx, s, tok, approved)
	case github.IsBadCredentials(err):
		// The token is refused: the actor tries again with a fresh one, and the approval,
		// made already, is not made again.
		return err
	default:
		return a.notMerged(approved, err)
	}
}

// mergesNow reports a state GitHub merges in without going past any rule: CLEAN or
// HAS_HOOKS, or UNSTABLE once the user accepted it.
func (a *action) mergesNow(state *github.MergeState) bool {
	return github.Mergeable(state.Status) || (state.Status == github.MergeStateUnstable && a.unstable)
}

// past is " past the rules" for a merge made past the branch's rules, for its step and
// its result, and "" for any other, a merge past the rules that GitHub would make anyway
// by the time it runs included.
func (a *action) past() string {
	if a.bypassing {
		return " past the rules"
	}

	return ""
}

// autoMerge turns on auto-merge, or merges at once when GitHub says the pull request can
// merge now.
func (a *action) autoMerge(ctx context.Context, s *Service, tok github.Token, approved bool) error {
	err := a.enableAutoMerge(ctx, s, tok, approved)
	if !errors.Is(err, github.ErrMergeableNow) {
		return err
	}

	return a.guardedMerge(ctx, s, tok, approved)
}

// enableAutoMerge turns on auto-merge at the head. ErrMergeableNow comes back as it is, for
// the caller to merge instead.
func (a *action) enableAutoMerge(ctx context.Context, s *Service, tok github.Token, approved bool) error {
	a.step(s, "Turning on auto-merge")

	_, err := s.github.EnableAutoMerge(ctx, tok, a.ref, a.nodeID, string(a.method), a.head)

	switch {
	case err == nil:
		a.outcome = OutcomeAutoMerge
		a.steps = append(a.steps, "Auto-merge on ("+strings.ToLower(string(a.method))+")")

		return nil
	case errors.Is(err, github.ErrMergeableNow):
		return err
	case errors.Is(err, github.ErrPushedSince):
		a.outcome = OutcomePushedSince

		return nil
	case github.IsBadCredentials(err):
		return err
	default:
		return a.notMerged(approved, err)
	}
}

// notMerged records that GitHub would not merge, with why: after an approval it is the
// action's result, otherwise the error.
func (a *action) notMerged(approved bool, err error) error {
	if !approved {
		return err
	}

	a.outcome, a.reason = OutcomeApprovedNotMerged, strings.TrimSuffix(uierr.Message(err), ".")

	return nil
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

// stateReason says why GitHub would not merge a pull request in a merge state.
func stateReason(status string) string {
	switch status {
	case github.MergeStateBlocked:
		return "Branch protection still blocks it."
	case github.MergeStateBehind:
		return "It's behind its base branch."
	case github.MergeStateDirty:
		return "It has conflicts."
	case github.MergeStateUnstable:
		return "Some checks are failing."
	default:
		return "GitHub is still working out whether it can merge."
	}
}

// result is what the action came to, for a pull request that was in the Review requested
// list when requested.
func (a *action) result(requested bool) Result {
	if a.outcome == OutcomePushedSince {
		return pushedSinceResult(a.ref.String(), a)
	}

	r := Result{
		Outcome: a.outcome, Reason: a.reason, Steps: nonNil(a.steps), LeftReview: a.acted.Reviewed && requested,
		LeftInbox: a.acted.Merged, Message: a.message(),
	}

	if a.kind.merges() {
		r.Method = a.method
	}

	return r
}

// message says what the action came to, for the toast.
func (a *action) message() string {
	ref, method := a.ref.String(), strings.ToLower(string(a.method))
	approved := a.reviewed != nil

	switch {
	case a.outcome == OutcomeReviewed:
		return reviewStep(a.kind) + " " + ref
	case a.outcome == OutcomeMerged && approved:
		return "Approved and merged " + ref + " (" + method + ")"
	case a.outcome == OutcomeMerged:
		return "Merged " + ref + a.past() + " (" + method + ")"
	case a.outcome == OutcomeAutoMerge && approved:
		return "Approved " + ref + " · auto-merge is on"
	case a.outcome == OutcomeAutoMerge:
		return "Auto-merge is on for " + ref + " (" + method + ")"
	case a.outcome == OutcomeApprovedNotMerged:
		return "Approved, but GitHub won't merge yet: " + a.reason
	case a.outcome == OutcomeBranchUpdating:
		return "Updating the branch of " + ref
	default:
		return "Asked Dependabot to rebase " + ref
	}
}

// pushedSinceResult is the result of an action refused because the pull request was
// pushed to: nothing was sent, or, when a is not nil, only its first steps.
func pushedSinceResult(ref string, a *action) Result {
	r := Result{
		Outcome: OutcomePushedSince, Steps: []string{},
		Message: ref + " was pushed to since you opened it. Reload it to see the new commits.",
	}

	if a != nil {
		r.Steps, r.LeftReview = nonNil(a.steps), a.acted.Reviewed

		if a.reviewed != nil {
			r.Message = reviewStep(a.kind) + ", but " + ref + " was pushed to since you opened it, so it wasn't merged."
		}
	}

	return r
}
