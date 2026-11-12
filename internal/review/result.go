package review

import (
	"strings"

	"github.com/mrz1836/nilbox/internal/textutil"
)

// result is what the action came to, for a pull request that was in the Review requested
// list when requested.
func (a *action) result(requested bool) Result {
	if a.outcome == OutcomePushedSince {
		return pushedSinceResult(a.ref.String(), a)
	}

	r := Result{
		Outcome: a.outcome, Reason: a.reason, Steps: textutil.NonNil(a.steps), LeftReview: a.acted.Reviewed && requested,
		LeftInbox: a.gone(), Message: a.message(), Commented: a.commentedFirst(),
	}

	if a.kind.merges() {
		r.Method = a.method
	}

	return r
}

// message says what the action came to, for the toast. A merge or a close that posted the
// user's text first says so, whatever it came to, so the user never posts it twice.
func (a *action) message() string {
	ref := a.ref.String()

	switch {
	case a.outcome == OutcomeReviewed:
		return reviewStep(a.reviewAs) + " " + ref
	case a.kind == ActionClose:
		return a.closedMessage(ref)
	case a.outcome == OutcomeMerged || a.outcome == OutcomeAutoMerge:
		return a.mergedMessage(ref)
	case a.outcome == OutcomeApprovedNotMerged || a.outcome == OutcomeCommentedNotMerged:
		return a.notMergedMessage(ref)
	case a.outcome == OutcomeBranchUpdating:
		return "Updating the branch of " + ref
	default:
		return "Asked Dependabot to rebase " + ref
	}
}

// notMergedMessage says what an action came to whose review GitHub took, an approval or
// the comment a merge posted first, and whose merge, or auto-merge, it would not make.
func (a *action) notMergedMessage(ref string) string {
	switch {
	case a.outcome == OutcomeApprovedNotMerged:
		return "Approved, but GitHub won't merge yet: " + a.reason
	case a.kind == ActionAutoMerge && !a.fellBack:
		return "Commented on " + ref + ", but auto-merge isn't on: " + a.reason
	default:
		return "Commented on " + ref + ", but it wasn't merged: " + a.reason
	}
}

// closedMessage says what a close came to, with the comment it posted first, if any.
func (a *action) closedMessage(ref string) string {
	switch {
	case a.outcome == OutcomeCommentedNotClosed:
		return "Commented on " + ref + ", but it wasn't closed: " + a.reason
	case a.commentedFirst():
		return "Closed " + ref + " with your comment"
	default:
		return "Closed " + ref
	}
}

// mergedMessage says what a merge, or auto-merge turned on, came to, with the review the
// action submitted before it.
func (a *action) mergedMessage(ref string) string {
	method := strings.ToLower(string(a.method))
	merged := a.outcome == OutcomeMerged

	switch {
	case a.approved() && merged:
		return "Approved and merged " + ref + " (" + method + ")"
	case a.approved():
		return "Approved " + ref + " · auto-merge is on"
	case a.commentedFirst() && merged:
		return "Commented and merged " + ref + a.past() + " (" + method + ")"
	case a.commentedFirst():
		return "Commented on " + ref + " · auto-merge is on (" + method + ")"
	case merged:
		return "Merged " + ref + a.past() + " (" + method + ")"
	default:
		return "Auto-merge is on for " + ref + " (" + method + ")"
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
		r.Steps, r.LeftReview, r.Commented = textutil.NonNil(a.steps), a.acted.Reviewed, a.commentedFirst()

		if a.reviewed != nil {
			r.Message = reviewStep(a.reviewAs) + ", but " + ref + " was pushed to since you opened it, so it wasn't merged."
		}
	}

	return r
}
