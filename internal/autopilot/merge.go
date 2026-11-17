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
//   - Merge it comes only after a Wait for the checks that passed on the agent's own push
//     (the spec holds the order), and is pinned to that commit: a push since merges
//     nothing. It turns GitHub's auto-merge back on when it was on as the routine matched
//     (Match.AutoMergeAtMatch) and the step says so, and otherwise merges with the step's
//     method, only once the review sheet's fresh read says GitHub would merge now. A pull
//     request GitHub holds for an approving review waits for the user under Needs your OK
//     (review_required), never approved by a routine; one an agent's commits changed
//     workflow, agent, or tooling files in, or an outside contributor's, is never merged.
//     There is no merge past the rules: the step can't name one.
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
	if st.Merge != nil {
		return "Its checks passed: waiting for your OK to merge it."
	}

	return "The agent's review is ready: waiting for your OK to post it."
}

// mergeHead returns the commit m's checks last passed on, from its latest Wait for the
// checks before step, "" for none.
func mergeHead(m *match, step int) string {
	for i := min(step, len(m.steps)) - 1; i >= 0; i-- {
		if sr := m.steps[i]; sr.Step.WaitForChecks != nil {
			return sr.Commit
		}
	}

	return ""
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

// mergeWords says what Merge it does: "Merges it (squash).", or turns auto-merge back on.
func mergeWords(auto bool, method catalog.MergeMethod) string {
	if auto {
		return "Turns auto-merge back on, as it was."
	}

	if method == catalog.MergeDefault {
		return "Merges it."
	}

	return "Merges it (" + strings.ToLower(string(method)) + ")."
}
