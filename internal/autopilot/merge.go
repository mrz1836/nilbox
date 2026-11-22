package autopilot

import (
	"context"
	"strings"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/trust"
)

// Merge it and Post the agent's review are the two steps that follow a run, D7's and D8's,
// each opt in and held in Go, whatever the routine says:
//
//   - Merge it comes after a Wait for the checks that passed on the agent's own push, and is
//     pinned to that commit: a push since merges nothing. Or, in a routine with no agent step
//     that fires when the checks pass, it merges the commit they passed on (agentlessHead),
//     never on a click's run, and only once the cycle still decides the routine matches it on
//     that commit, right before it starts (recheck). It turns GitHub's auto-merge back on when
//     it was on as the routine matched (Match.AutoMergeAtMatch) and the step says so, and
//     otherwise merges with the step's method, only once the review sheet's fresh read says
//     GitHub would merge now. A pull request GitHub holds for an approving review waits for
//     the user under Needs your OK (review_required), never approved by a routine; one an
//     agent's commits changed workflow, agent, or tooling files in, or an outside
//     contributor's, is never merged. The one merge past the rules is the user's own signed
//     pull request that lacks only an approval from someone else, by a routine with no agent
//     step whose step allows it, that the user accepted so, while their switch for it is on
//     (pastrules.go): nothing else merges past them, and no step can name another.
//   - Post the agent's review comes only after a Read only agent step, and is pinned to the
//     commit the agent read: a push since posts nothing. It posts the review the agent's
//     report held, as a comment, or as its verdict: a request for changes, or an approval
//     only with Allow approving ticked, on a member's or a trusted bot's pull request whose
//     files were all checked and none is sensitive, and that no routine pushed to; any other
//     approval posts as a comment, saying why.
//
// Both go through the review sheet's own service, which core adapts (GitHubActions.Merge,
// GitHubActions.PostReview): the item's lock, the account check, the write gate, and the
// sheet's Decide, pinned to the head; each is its own run, a GitHub action against the
// day's and the pace's limits, recorded before it is sent.

// MergeSend is one Merge it to send.
type MergeSend struct {
	Item ItemRef

	// Head is the commit the checks passed on, which the merge is pinned to.
	Head string

	// AutoMerge turns GitHub's auto-merge back on, as it was when the routine matched,
	// rather than merging now.
	AutoMerge bool

	// Method is the merge method, "" for the repository's own as Settings picks it.
	Method catalog.MergeMethod

	// Accept is the warnings the user accepted, as GitHubSent.Words worded them.
	Accept string

	// PastReview lets it go past the rules where an approval from someone else is all the
	// user's own pull request lacks, and Sensitive when it changes sensitive files too
	// (pastReview); PastReviewWhy says why a step that allows it may not, "" otherwise, and
	// PastRulesOff reports that why is the user's switch being off: a merge that needs to go
	// past the rules then answers GitHubPastRulesOff, so it waits for the switch.
	PastReview    bool
	Sensitive     bool
	PastReviewWhy string
	PastRulesOff  bool
}

// mergeDispatcher runs Merge it (catalog.DispatchMerge).
type mergeDispatcher struct {
	actions GitHubActions
}

// ReadItem reads the item through its sheet, as the read right before the merge.
func (d mergeDispatcher) ReadItem(ctx context.Context, job Job) error {
	return d.actions.Read(ctx, job.Item)
}

// Dispatch sends job's Merge it.
func (d mergeDispatcher) Dispatch(ctx context.Context, job Job) (Result, error) {
	st := job.Step.Merge
	if st == nil || job.Head == "" {
		return Result{NotOffered: "It has no commit its checks passed on to merge."}, nil
	}

	got, err := d.actions.Merge(ctx, MergeSend{
		Item: job.Item, Head: job.Head, AutoMerge: job.AutoMerge, Method: st.Method, Accept: job.Accept,
		PastReview: job.PastReview, Sensitive: job.Sensitive, PastReviewWhy: job.PastReviewWhy,
		PastRulesOff: job.PastRulesOff,
	})
	if err != nil {
		return Result{}, err
	}

	return resultOf(got)
}

// followsRun reports a step decided on what a run before it came to: Merge it, and Post the
// agent's review, which an Ask first routine asks about again, showing it.
func followsRun(st spec.Step) bool {
	return st.Merge != nil || st.PostReview != nil
}

// followWords says what a step that follows a run waits for the user's OK to do.
func followWords(st spec.Step) string {
	switch {
	case st.Merge != nil && st.Merge.AllowBypass:
		return "Its checks passed: waiting for your OK to merge it, past the rules if an approval from someone " +
			"else is all it lacks."
	case st.Merge != nil:
		return "Its checks passed: waiting for your OK to merge it."
	}

	return "The agent's review is ready: waiting for your OK to post it."
}

// mergeHead returns the commit m's checks last passed on, from its latest Wait for the
// checks before step, or, with none, the commit an agentless Merge it merges
// (agentlessHead); "" for neither.
func mergeHead(m *match, step int) string {
	for i := min(step, len(m.steps)) - 1; i >= 0; i-- {
		if sr := m.steps[i]; sr.Step.WaitForChecks != nil {
			return sr.Commit
		}
	}

	return agentlessHead(m)
}

// autoMergeBack reports whether a Merge it turns auto-merge back on: the step says to when
// it was on, and it was as the routine matched.
func autoMergeBack(m *match, how catalog.MergeHow) bool {
	return how == catalog.MergeAutoIfItWasOn && m.AutoMergeAtMatch != nil && *m.AutoMergeAtMatch
}

// outsideContributor reports an outside contributor's item, by its facts.
func outsideContributor(f facts.Facts) bool {
	t := f.Value(catalog.FieldTrust)

	return t.Known() && t.Text == string(trust.ClassOutside)
}

// mergeWords says what Merge it, st, does: "Merges it (squash).", past the rules where an
// approval from someone else is all it lacks when st allows it, or turns auto-merge back on.
func mergeWords(auto bool, st spec.MergeStep) string {
	if auto {
		return "Turns auto-merge back on, as it was."
	}

	merges := "Merges it"
	if st.Method != catalog.MergeDefault {
		merges += " (" + strings.ToLower(string(st.Method)) + ")"
	}

	switch {
	case st.AllowBypass && st.AllowSensitive:
		return merges + ", past the rules when only an approval from someone else is missing, sensitive files included."
	case st.AllowBypass:
		return merges + ", past the rules when only an approval from someone else is missing."
	}

	return merges + "."
}
