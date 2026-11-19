package core

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/review"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// Merge it and Post the agent's review go through the review sheet as the GitHub step does
// (githubSteps): each read through the sheet right before it (ReadAgain, the fresh read),
// decided again on that read by the sheet's own Decide, pinned to the head the engine names
// (the commit the checks passed on, the commit the agent read), and sent through Act, under
// the inbox's Actor and the write gate. A merge is only ever merge_now or auto_merge, a
// review only comment, request_changes, or approve: never merge_bypass, approve_merge,
// approve_auto_merge, a close, or approving workflow runs, which a guard test holds to.

// mergeActionOf is the review sheet's action a Merge it sends: auto-merge turned back on,
// or a merge now.
func mergeActionOf(auto bool) review.Action {
	if auto {
		return review.ActionAutoMerge
	}

	return review.ActionMerge
}

// reviewActionOfEvent is the review sheet's action a Post the agent's review sends.
func reviewActionOfEvent(e autopilot.ReviewEvent) (review.Action, bool) {
	switch e {
	case autopilot.EventComment:
		return review.ActionComment, true
	case autopilot.EventRequestChanges:
		return review.ActionRequestChanges, true
	case autopilot.EventApprove:
		return review.ActionApprove, true
	}

	return review.ActionNone, false
}

// mergeMethodOf is the review sheet's merge method a step names, "" for the repository's
// own, as Settings picks it.
func mergeMethodOf(m catalog.MergeMethod) review.MergeMethod {
	switch m {
	case catalog.MergeCommit:
		return review.MethodMerge
	case catalog.MergeSquash:
		return review.MethodSquash
	case catalog.MergeRebase:
		return review.MethodRebase
	case catalog.MergeDefault:
	}

	return ""
}

// Merge merges the pull request, or turns its auto-merge back on, through the review sheet:
// decided on the sheet's read just made, only at req.Head, never where an agent's commits
// changed workflow, agent, or tooling files, and only as the sheet offers it now, so GitHub
// itself says it would merge; one it holds for an approving review waits for the user.
func (g githubSteps) Merge(ctx context.Context, req autopilot.MergeSend) (autopilot.GitHubSent, error) {
	ctx = github.WithPurpose(ctx, github.PurposeAutopilot)

	rev, err := g.review.Refresh(ctx, req.Item.ID)
	if err != nil {
		return unread(err), nil
	}

	if words, held, heldErr := g.agentChangedAt(ctx, req.Item.ID, req.Head); heldErr != nil || held {
		return notOffered(words), heldErr
	}

	action, sent, decided := mergeDecision(rev, req)
	if decided {
		return sent, nil
	}

	res, err := g.review.Act(ctx, review.ActRequest{
		ItemID: req.Item.ID, Action: action, Head: req.Head, Method: mergeMethodOf(req.Method), Accept: req.Accept != "",
	})

	return mergeSent(res, err)
}

// mergeDecision decides a Merge it on the review sheet's read rev, as it stands: the action
// to send, or the answer it comes to without one (decided). A head that moved merges
// nothing; auto-merge already on is nothing to do; a merge GitHub holds for an approving
// review waits for the user; and anything else goes only as the sheet offers it now, with
// warnings only the user accepted. It is pure.
func mergeDecision(rev review.Review, req autopilot.MergeSend) (review.Action, autopilot.GitHubSent, bool) {
	action := mergeActionOf(req.AutoMerge)

	switch {
	case !strings.EqualFold(rev.Head, req.Head):
		return action, notOffered("It was pushed to after its checks passed, so it isn't merged."), true
	case req.AutoMerge && rev.AutoMerge != "":
		return action, autopilot.GitHubSent{Outcome: autopilot.GitHubNothing, Words: "Auto-merge was on already."}, true
	case !req.AutoMerge && waitsOnReview(rev.Offers):
		return action, autopilot.GitHubSent{Outcome: autopilot.GitHubNeedsReview}, true
	}

	sent, decided := pullOffer(rev.Offers, action, req.Accept)

	return action, sent, decided
}

// waitsOnReview reports a pull request the sheet won't merge now but would approve and
// merge: GitHub holds it for an approving review alone.
func waitsOnReview(offers []review.Offer) bool {
	enabled := func(a review.Action) bool {
		return slices.ContainsFunc(offers, func(o review.Offer) bool { return o.Action == a && o.Enabled })
	}

	return !enabled(review.ActionMerge) && enabled(review.ActionApproveMerge)
}

// mergeSent is how a merge Act came to, in the GitHub step's words. GitHub refusing the
// merge (github.ErrNotMergeable), whether the merge state read fresh right before it said
// so or the merge itself was refused, merged nothing, so it is no longer offered, in the
// sheet's words, rather than an error after which GitHub may have taken it.
func mergeSent(res review.Result, err error) (autopilot.GitHubSent, error) {
	switch {
	case err != nil:
		return refusal(err, review.ErrNotOffered, review.ErrWarningsNotAccepted, github.ErrNotMergeable)
	case res.Outcome == review.OutcomePushedSince:
		return notOffered("It was pushed to after its checks passed, so it isn't merged."), nil
	case res.Outcome == review.OutcomeMerged, res.Outcome == review.OutcomeAutoMerge:
		return autopilot.GitHubSent{Outcome: autopilot.GitHubSentIt, Words: res.Message}, nil
	default:
		return notOffered(textutil.Sentence("GitHub didn't merge it: " + res.Reason)), nil
	}
}

// agentChangedAt says whether an agent's commits at head changed workflow, agent, or tooling
// files, or what they changed isn't all known, which no merge follows, and why.
func (g githubSteps) agentChangedAt(ctx context.Context, itemID int64, head string) (string, bool, error) {
	if g.changes == nil {
		return "", false, nil
	}

	check, noted, err := g.changes.AgentChanged(ctx, itemID, head)

	switch {
	case err != nil:
		return "", false, fmt.Errorf("read what the agent's run changed: %w", err)
	case !noted:
		return "", false, nil
	case len(check.Groups) > 0:
		return "Its agent's commits changed " + groupWords(check.Groups) + ", so it isn't merged: a person reviews those.",
			true, nil
	case !check.Complete:
		return "Not every file its agent's commits changed could be checked, so it isn't merged.", true, nil
	}

	return "", false, nil
}

// groupWords names a check's sensitive categories: "workflow files and agent files".
func groupWords(groups []sensitive.Group) string {
	names := make([]string, 0, len(groups))
	for _, gr := range groups {
		names = append(names, strings.ToLower(gr.Label))
	}

	switch len(names) {
	case 1:
		return names[0]
	case 2:
		return names[0] + " and " + names[1]
	}

	return strings.Join(names[:len(names)-1], ", ") + ", and " + names[len(names)-1]
}

// errNoReviewEvent is a review event this build doesn't know.
var errNoReviewEvent = errors.New("no such review")

// PostReview posts the agent's review through the review sheet: decided on the sheet's read
// just made, only at req.Head, as the sheet offers the review now, with the line comments its
// diff shows (the rest left out, and counted).
func (g githubSteps) PostReview(ctx context.Context, req autopilot.ReviewSend) (autopilot.GitHubSent, error) {
	ctx = github.WithPurpose(ctx, github.PurposeAutopilot)

	action, ok := reviewActionOfEvent(req.Event)
	if !ok {
		return autopilot.GitHubSent{}, fmt.Errorf("%w: %q", errNoReviewEvent, req.Event)
	}

	rev, err := g.review.Refresh(ctx, req.Item.ID)
	if err != nil {
		return unread(err), nil
	}

	if sent, decided := reviewDecision(rev, req, action); decided {
		return sent, nil
	}

	comments, dropped, err := review.NewFitter(g.review).Fit(ctx, req.Item.ID, req.Head, lineComments(req.Comments))

	switch {
	case errors.Is(err, review.ErrPushedSince):
		return notOffered("It was pushed to after the agent read it, so its review isn't posted."), nil
	case err != nil:
		return autopilot.GitHubSent{}, err
	}

	res, err := g.review.Act(ctx, review.ActRequest{
		ItemID: req.Item.ID, Action: action, Head: req.Head, Body: req.Body, Comments: comments, Accept: req.Accept != "",
	})

	switch {
	case err != nil:
		return refusal(err, review.ErrNotOffered, review.ErrWarningsNotAccepted)
	case res.Outcome == review.OutcomePushedSince:
		return notOffered("It was pushed to after the agent read it, so its review isn't posted."), nil
	}

	words := res.Message
	if dropped > 0 {
		words = strings.TrimSpace(words + " " + textutil.Counted(dropped, "line comment", "line comments") +
			" left out: the diff doesn't show those lines.")
	}

	return autopilot.GitHubSent{Outcome: autopilot.GitHubSentIt, Words: words}, nil
}

// reviewDecision decides a Post the agent's review on the review sheet's read rev, as it
// stands: posted nowhere but at the commit the agent read, and only as the sheet offers the
// review now, with warnings only the user accepted; decided when it comes to an answer
// without sending. It is pure.
func reviewDecision(rev review.Review, req autopilot.ReviewSend, action review.Action) (autopilot.GitHubSent, bool) {
	if !strings.EqualFold(rev.Head, req.Head) {
		return notOffered("It was pushed to after the agent read it, so its review isn't posted."), true
	}

	return pullOffer(rev.Offers, action, req.Accept)
}

// lineComments are the agent's line comments as the review sheet takes them: a range's
// start on the same side as its end.
func lineComments(lines []autopilot.ReviewLine) []review.LineComment {
	out := make([]review.LineComment, 0, len(lines))

	for _, l := range lines {
		side := review.SideRight
		if strings.EqualFold(l.Side, string(review.SideLeft)) {
			side = review.SideLeft
		}

		c := review.LineComment{Path: l.Path, Line: l.Line, Side: side, Body: l.Body}
		if l.StartLine > 0 && l.StartLine < l.Line {
			c.StartLine, c.StartSide = l.StartLine, side
		}

		out = append(out, c)
	}

	return out
}
