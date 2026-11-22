package core

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/review"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// A routine's Merge it goes past the rules here alone: this is the one adapter that names
// review.ActionMergeBypass, and every ActRequest that does sets Routine, which a guard test
// holds to. It sends one only for the user's own pull request that the review sheet's read
// just made finds lacking only an approval from someone else (Review.OwnAwaitsApproval),
// when the engine says the routine may (MergeSend.PastReview: the step's tick, the user's
// acceptance, no routine having pushed to it), Autopilot's switch for it is on as read again
// right before, and the sheet's offer warns of nothing but the approval and, as the step
// allows it, sensitive files. The sheet then holds the merge to the pull request itself, on
// a fresh read of all it checks (internal/review's pastrules.go). Anything short of that
// is no longer offered, saying why, or, while GitHub isn't ready, tried again later: no
// routine ever waits for the user's click here.

// autopilotSettings reads Autopilot's settings as they are now.
type autopilotSettings interface {
	Autopilot(ctx context.Context) (autopilotprefs.Config, error)
}

// pastReview decides a Merge it of the user's own pull request that lacks only an approval
// from someone else (rev.OwnAwaitsApproval), and reports whether it did (handled): a merge
// past the rules, when req and the switch allow it, or why not. Any other merge, auto-merge
// turned back on, and one at another head than the sheet read, are left to mergeDecision.
func (g githubSteps) pastReview(ctx context.Context, rev review.Review, req autopilot.MergeSend,
) (autopilot.GitHubSent, bool, error) {
	if req.AutoMerge || !rev.OwnAwaitsApproval || !strings.EqualFold(rev.Head, req.Head) ||
		(!req.PastReview && req.PastReviewWhy == "") {
		return autopilot.GitHubSent{}, false, nil
	}

	if !req.PastReview {
		return engineRefused(req), true, nil
	}

	if on, err := g.pastRulesOn(ctx); err != nil || !on {
		return pastRulesOff(), true, err
	}

	if sent, decided := pastReviewOffer(rev.Offers, req.Sensitive); decided {
		return sent, true, nil
	}

	res, err := g.review.Act(ctx, review.ActRequest{
		ItemID: req.Item.ID, Action: review.ActionMergeBypass, Head: req.Head, Method: mergeMethodOf(req.Method),
		Accept: true, Routine: &review.RoutineMerge{PastReview: true, Sensitive: req.Sensitive},
	})

	sent, err := pastReviewSent(res, err, req.Item.Number)

	return sent, true, err
}

// pastRulesOn reads Autopilot's switch for merging past the rules as it is now; with no
// settings to read, it is off.
func (g githubSteps) pastRulesOn(ctx context.Context) (bool, error) {
	if g.autopilot == nil {
		return false, nil
	}

	c, err := g.autopilot.Autopilot(ctx)
	if err != nil {
		return false, err
	}

	return c.MergePastRules, nil
}

// engineRefused is a merge past the rules the engine said may not go: one the switch keeps
// back waits for it, and any other closes with the engine's reason.
func engineRefused(req autopilot.MergeSend) autopilot.GitHubSent {
	if req.PastRulesOff {
		return pastRulesOff()
	}

	return notOffered(req.PastReviewWhy)
}

// pastRulesOff is a merge past the rules the switch kept from being sent: it waits for the
// user to turn the switch on.
func pastRulesOff() autopilot.GitHubSent {
	return autopilot.GitHubSent{Outcome: autopilot.GitHubPastRulesOff, Words: autopilot.PastRulesOffWords}
}

// pastReviewOffer decides a merge past the rules on the sheet's offers before it is sent:
// one not offered, or offered with a warning beyond the approval's and, as sensitive allows,
// the sensitive files', goes no further; decided when it comes to an answer without
// sending. It is pure.
func pastReviewOffer(offers []review.Offer, sensitive bool) (autopilot.GitHubSent, bool) {
	sent, decided := pullOffer(offers, review.ActionMergeBypass, "")
	if decided && sent.Outcome != autopilot.GitHubWarning {
		return sent, true
	}

	for _, o := range offers {
		if o.Action != review.ActionMergeBypass {
			continue
		}

		if stray := review.StrayWarning(o.Warnings, sensitive); stray != "" {
			return notOffered(textutil.Sentence("Its step doesn't merge it past the rules when " +
				lowerFirst(strings.TrimSuffix(stray, ".")))), true
		}
	}

	return autopilot.GitHubSent{}, false
}

// pastReviewSent is how a merge past the rules came to, in the GitHub step's words: merged,
// saying it went past the rules when it did, or no longer offered, saying why; or, while
// GitHub isn't ready, to be tried again.
func pastReviewSent(res review.Result, err error, number int) (autopilot.GitHubSent, error) {
	switch {
	case errors.Is(err, review.ErrPastRulesNotYet):
		return autopilot.GitHubSent{Outcome: autopilot.GitHubNotYet, Words: uierr.Message(err)}, nil
	case err != nil:
		return refusal(err, review.ErrNotOffered, review.ErrWarningsNotAccepted, github.ErrNotMergeable)
	case res.Outcome == review.OutcomeMerged && strings.Contains(res.Message, " past the rules"):
		return autopilot.GitHubSent{
			Outcome: autopilot.GitHubSentIt,
			Words:   mergedLabel(res.Method) + " #" + strconv.Itoa(number) + " past the rules: it lacked only an approval from someone else.",
		}, nil
	}

	return mergeSent(res, nil)
}

// mergedLabel names a merge made with method, as in "Squash-merged".
func mergedLabel(method review.MergeMethod) string {
	switch method {
	case review.MethodSquash:
		return "Squash-merged"
	case review.MethodRebase:
		return "Rebase-merged"
	case review.MethodMerge, review.MethodNone:
		return "Merged"
	default:
		return "Merged"
	}
}

// lowerFirst lowers the first letter of s.
func lowerFirst(s string) string {
	if s == "" {
		return s
	}

	return strings.ToLower(s[:1]) + s[1:]
}
