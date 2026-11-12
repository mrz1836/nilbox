package review

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// guardReads and guardWait bound the fresh reads of the merge state a merge makes while
// GitHub still works it out (UNKNOWN), as it does just after an approval.
const (
	guardReads = 3
	guardWait  = time.Second
)

// checkAhead reads the merge state before auto-merge's comment is posted, as a merge's
// guard does before its own: a pull request no longer open, a draft, or one on a merge
// queue is refused, and a head other than the one the user saw reported as a pushed-since
// outcome, so neither gets a comment. It reports whether the action stops there.
func (a *action) checkAhead(ctx context.Context, s *Service, tok github.Token) (bool, error) {
	a.step(s, "Checking nothing was pushed since you opened it")

	state, err := s.github.FetchMergeState(ctx, tok, a.nodeID)
	if err != nil {
		return true, err
	}

	if err = mergeableState(state); err != nil {
		return true, err
	}

	if !strings.EqualFold(state.Head, a.head) {
		a.outcome = OutcomePushedSince

		return true, nil
	}

	return false, nil
}

// guardedMerge reads the merge state fresh and merges only what GitHub says can merge now
// (UNSTABLE only when the user accepted it). After an approval, a pull request still
// blocked on requirements gets auto-merge instead, where GitHub allows it. After a review,
// an approval or the comment a merge posts first, anything GitHub would not merge is a
// result; without one, it is an error, since nothing happened, and the sheet offers
// auto-merge itself.
func (a *action) guardedMerge(ctx context.Context, s *Service, tok github.Token) error {
	state, err := a.guard(ctx, s, tok)
	if err != nil {
		if github.IsBadCredentials(err) || ctx.Err() != nil {
			return err
		}

		return a.notMerged(err)
	}

	switch a.decideMerge(state) {
	case mergeStepPushed:
		a.outcome = OutcomePushedSince

		return nil
	case mergeStepMerge:
		a.bypassing = !a.mergesNow(state)

		if err = a.postFirst(ctx, s, tok); err != nil {
			return err
		}

		return a.merge(ctx, s, tok)
	case mergeStepAuto:
		return a.enableAutoMerge(ctx, s, tok)
	case mergeStepRefuse:
		return a.notMerged(uierr.New(stateReason(state.Status),
			fmt.Errorf("%w: %s", github.ErrNotMergeable, state.Status)))
	default:
		return fmt.Errorf("%w: merge step %d", ErrNotOffered, a.decideMerge(state))
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
func (a *action) decideMerge(state *github.MergeState) mergeStep {
	held := state.Status == github.MergeStateBlocked || state.Status == github.MergeStateBehind

	switch {
	case !strings.EqualFold(state.Head, a.head):
		return mergeStepPushed
	case a.mergesNow(state):
		return mergeStepMerge
	case a.kind == ActionMergeBypass && held && state.CanBypass:
		return mergeStepMerge
	case state.Status == github.MergeStateBlocked && a.canAuto && a.approved():
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

		if err = clock.Sleep(ctx, guardWait); err != nil {
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

// merge merges the pull request at the head, falling back to auto-merge when GitHub
// refuses for requirements it waits on.
func (a *action) merge(ctx context.Context, s *Service, tok github.Token) error {
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
	case errors.Is(err, github.ErrNotMergeable) && a.canAuto && a.approved():
		return a.enableAutoMerge(ctx, s, tok)
	case github.IsBadCredentials(err):
		// The token is refused: the actor tries again with a fresh one, and the approval or
		// comment, made already, is not made again.
		return err
	default:
		return a.notMerged(err)
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
// merge now. The comment it posts first, if any, goes once the merge state says the pull
// request is still open, not a draft, and at the head the user saw.
func (a *action) autoMerge(ctx context.Context, s *Service, tok github.Token) error {
	if a.commentFirst && a.reviewAs != ActionNone && a.reviewed == nil {
		if stop, err := a.checkAhead(ctx, s, tok); stop {
			return err
		}

		if err := a.postFirst(ctx, s, tok); err != nil {
			return err
		}
	}

	err := a.enableAutoMerge(ctx, s, tok)
	if !errors.Is(err, github.ErrMergeableNow) {
		return err
	}

	a.fellBack = true

	return a.guardedMerge(ctx, s, tok)
}

// enableAutoMerge turns on auto-merge at the head. ErrMergeableNow comes back as it is, for
// the caller to merge instead.
func (a *action) enableAutoMerge(ctx context.Context, s *Service, tok github.Token) error {
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
		return a.notMerged(err)
	}
}

// notMerged records that GitHub would not merge, with why: after a review GitHub took, an
// approval or the comment a merge posts first, it is the action's result, which says the
// review stands, otherwise the error.
func (a *action) notMerged(err error) error {
	if a.reviewed == nil {
		return err
	}

	a.outcome, a.reason = OutcomeApprovedNotMerged, strings.TrimSuffix(uierr.Message(err), ".")
	if a.commentedFirst() {
		a.outcome = OutcomeCommentedNotMerged
	}

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
