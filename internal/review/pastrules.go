package review

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// A routine merges past the rules in one case alone: the user's own pull request, every
// commit of it signed by them, its checks all passed, that the base branch's rules hold
// back only for an approval from someone else, which GitHub never lets the author give
// (ActRequest.Routine). Autopilot decides whether the routine may (its step's ticks, the
// user's switch, the routine they accepted, and no routine having pushed to it), and the
// review sheet holds the merge to what it is, on its own fresh read: Act refuses one the
// sheet's read doesn't find the user's own, lacking only that approval (OwnAwaitsApproval),
// with no warning but the approval's and, when the routine allows it, the sensitive files';
// and the merge's guard reads the merge state again with all FetchOwnMergeState reads, and
// merges only when every check holds (pastRules). A pull request GitHub would merge anyway
// merges as any other does. What it refuses is worded for the routine's Activity: refused
// for good (ErrPastRulesRefused), or not yet, while GitHub still works it out or its checks
// still report (ErrPastRulesNotYet), which Autopilot tries again.

var (
	// ErrPastRulesRefused is a routine's merge past the rules refused for what the pull
	// request is: it is no longer offered.
	ErrPastRulesRefused = fmt.Errorf("%w: a routine may not merge it past the rules", ErrNotOffered)

	// ErrPastRulesNotYet is a routine's merge past the rules refused for now: GitHub still
	// works out its merge state, or its checks haven't all reported. Nothing was sent.
	ErrPastRulesNotYet = errors.New("a routine's merge past the rules waits for GitHub")
)

// RoutineMerge is what a routine's Merge it allows, sent with ActionMergeBypass alone:
// PastReview lets it past the rules on the user's own pull request that lacks only an
// approval from someone else, and Sensitive lets it do so when the pull request changes
// sensitive files too.
type RoutineMerge struct {
	PastReview bool
	Sensitive  bool
}

// OwnApprovalSkipped is the rule a merge past the rules goes around on the user's own pull
// request that needs an approval, which GitHub never lets them give: the one warning a
// routine's merge past the rules accepts on its own.
const OwnApprovalSkipped = "It needs an approval from someone else."

// sensitivePrefix starts the warning of a pull request that changes sensitive files.
const sensitivePrefix = "It changes sensitive files: "

// SensitiveWarning is the warning a merge asks the user to accept for a pull request that
// changes sensitive files of categories, as in "It changes sensitive files: Workflows."
func SensitiveWarning(categories []string) string {
	return sensitivePrefix + strings.Join(categories, ", ") + "."
}

// StrayWarning returns the first of a merge past the rules' warnings that a routine's
// doesn't accept, "" for none: OwnApprovalSkipped it accepts, and the sensitive files'
// only when sensitive allows them.
func StrayWarning(warnings []string, sensitive bool) string {
	for _, w := range warnings {
		switch {
		case w == OwnApprovalSkipped:
		case sensitive && strings.HasPrefix(w, sensitivePrefix):
		default:
			return w
		}
	}

	return ""
}

// ownAwaitsApproval reports the user's own pull request that the rules hold back only for
// an approving review, which nobody else will give it, and that GitHub offers to merge past
// the rules now: what Review.OwnAwaitsApproval says.
func ownAwaitsApproval(f Facts, offers []Offer) bool {
	return f.Mine && f.CanBypass && waitsOnReview(f) &&
		slices.ContainsFunc(offers, func(o Offer) bool { return o.Action == ActionMergeBypass && o.Enabled })
}

// checkRoutine holds a routine's request to what a routine may ask: nothing but a merge
// past the rules (PastReview, ActionMergeBypass), of the user's own pull request that the
// sheet's read finds lacking only an approval from someone else, with no warning but the
// approval's and, when Sensitive allows it, the sensitive files'. A request of the user's
// own passes.
func checkRoutine(req ActRequest, f Facts, plan Plan) error {
	r := req.Routine
	if r == nil {
		return nil
	}

	if !r.PastReview || req.Action != ActionMergeBypass {
		return uierr.New("A routine merges past the rules only to merge your own pull request that lacks only an approval.",
			fmt.Errorf("%w: a routine's %q", ErrPastRulesRefused, req.Action))
	}

	if !ownAwaitsApproval(f, plan.Offers) {
		return uierr.New(ownRefusal(f), fmt.Errorf("%w: it doesn't lack only an approval", ErrPastRulesRefused))
	}

	offer, _ := plan.offer(ActionMergeBypass)
	if stray := StrayWarning(offer.Warnings, r.Sensitive); stray != "" {
		return uierr.New("A routine doesn't merge past this: "+textutil.Sentence(strings.TrimSuffix(stray, ".")),
			fmt.Errorf("%w: a warning it doesn't accept: %s", ErrPastRulesRefused, stray))
	}

	return nil
}

// ownRefusal says why the sheet's read doesn't find a pull request the user's own, lacking
// only an approval from someone else.
func ownRefusal(f Facts) string {
	switch {
	case !f.Mine:
		return "It isn't your own pull request."
	case !f.CanBypass:
		return "GitHub doesn't let you merge it past the rules."
	case f.ChangesRequested:
		return "Changes were requested, and the request stands."
	case f.RequiredFailing > 0 || f.RequiredPending > 0:
		return "Not every required check has passed."
	case !waitsOnReview(f):
		return "The rules hold it back for more than an approval."
	default:
		return "GitHub doesn't offer to merge it past the rules now."
	}
}

// pastRules says whether a routine's merge past the rules goes on in state, the merge state
// read fresh with all FetchOwnMergeState reads, at the head the routine is pinned to: ""
// when it does, else why not, and whether that may pass on its own (notYet). It merges only
// the user's own pull request, every commit of which they signed, on a base branch whose
// rules are each one a routine can account for, that GitHub holds back (BLOCKED) only for
// an approving review, lets the user merge past the rules, and whose checks all passed,
// every required one among them, with no review thread left unresolved.
func (a *action) pastRules(state *github.MergeState) (string, bool) {
	own := state.Own
	if own == nil {
		return "GitHub's answer didn't say all a routine checks before it merges past the rules.", false
	}

	for _, check := range []func() string{
		func() string { return authored(own) },
		func() string { return signedByViewer(own) },
		func() string { return rulesKnown(own, a.method) },
	} {
		if why := check(); why != "" {
			return why, false
		}
	}

	if why, notYet := heldForReview(state); why != "" {
		return why, notYet
	}

	if why, notYet := checksPassed(own); why != "" {
		return why, notYet
	}

	return threadsResolved(own), false
}

// authored refuses a pull request the viewer didn't open.
func authored(own *github.OwnMerge) string {
	if !own.ViewerDidAuthor {
		return "It isn't your own pull request."
	}

	return ""
}

// signedByViewer refuses a pull request with a commit the viewer didn't sign, one whose
// signature GitHub didn't verify, or more commits than were read: a commit's author can be
// anyone, its signature can't.
func signedByViewer(own *github.OwnMerge) string {
	if !own.CommitsWhole() {
		return fmt.Sprintf("It has more than %d commits, so not every one's signature could be checked.",
			github.PullCommitsMax)
	}

	if len(own.Commits) == 0 {
		return "None of its commits could be read."
	}

	for _, c := range own.Commits {
		switch {
		case !c.Signed:
			return "Its commit " + short(c.OID) + " isn't signed."
		case !c.Valid:
			return "Its commit " + short(c.OID) + " has a signature GitHub didn't verify."
		case !strings.EqualFold(c.Signer, own.Viewer):
			return "Its commit " + short(c.OID) + " wasn't signed by you."
		}
	}

	return ""
}

// allowedRules are the rule types a routine's merge past the rules can account for: the
// pull request rule it goes past, those whose requirement it checks itself (the required
// checks and signatures), those about pushes, which a merge doesn't make (no force push,
// no deletion, no creation, and the file rules), and Copilot's automatic review, which
// blocks nothing.
func allowedRules() []string {
	return []string{
		github.RulePullRequest, github.RuleRequiredStatusChecks, github.RuleRequiredSignatures, github.RuleNonFastForward,
		github.RuleDeletion, github.RuleCreation, github.RuleCopilotCodeReview, github.RuleFilePathRestriction,
		github.RuleMaxFilePathLength, github.RuleFileExtension, github.RuleMaxFileSize,
	}
}

// linearWords say a base branch wants a linear history, which a merge commit isn't.
const linearWords = "Its base branch wants a linear history, which a merge commit isn't."

// rulesKnown refuses a base branch with a rule a routine can't account for, one it couldn't
// read in full, a linear history wanted of a merge commit, or classic protection that
// requires deployments or locks the branch.
func rulesKnown(own *github.OwnMerge, method MergeMethod) string {
	if !own.RulesWhole() {
		return "Not every rule on its base branch could be read."
	}

	linear := method == MethodSquash || method == MethodRebase

	for _, r := range own.Rules {
		if why := ruleKnown(r.Type, linear); why != "" {
			return why
		}
	}

	return classicKnown(own.Protection, linear)
}

// ruleKnown refuses a ruleset's rule of kind a routine can't account for, merging with a
// linear history or not.
func ruleKnown(kind string, linear bool) string {
	switch {
	case kind == github.RuleRequiredLinear && !linear:
		return linearWords
	case kind == github.RuleRequiredLinear, slices.Contains(allowedRules(), kind):
		return ""
	default:
		return "Its base branch has a rule a routine can't account for (" + ruleWords(kind) + ")."
	}
}

// classicKnown refuses classic branch protection that requires deployments, locks the
// branch, or wants a linear history of a merge commit; none refuses nothing.
func classicKnown(p *github.ClassicProtection, linear bool) string {
	switch {
	case p == nil:
		return ""
	case p.RequiresDeployments:
		return "Its base branch requires deployments first."
	case p.LockBranch:
		return "Its base branch is locked."
	case p.RequiresLinear && !linear:
		return linearWords
	default:
		return ""
	}
}

// heldForReview refuses a merge state other than one the rules hold back (BLOCKED), which
// GitHub lets the user merge past: while GitHub still works it out it is not yet; behind
// its base, with conflicts, or anything else, it is refused.
func heldForReview(state *github.MergeState) (string, bool) {
	switch state.Status {
	case github.MergeStateBlocked:
	case github.MergeStateUnknown, "":
		return "GitHub is still working out whether it can merge.", true
	case github.MergeStateBehind:
		return "It's behind its base branch: a routine merges past the rules only what lacks an approval alone.", false
	default:
		return stateReason(state.Status), false
	}

	switch own := state.Own; {
	case !state.CanBypass:
		return "GitHub doesn't let you merge it past the rules.", false
	case own.ReviewDecision == github.ReviewDecisionChangesRequested:
		return "Changes were requested, and the request stands.", false
	case own.ReviewDecision != github.ReviewDecisionRequired:
		return "The rules hold it back for more than an approval.", false
	}

	return "", false
}

// checksPassed refuses a head whose checks didn't all pass, or that lacks a required check
// by name: while they still report, or GitHub has no rollup of them yet, it is not yet.
func checksPassed(own *github.OwnMerge) (string, bool) {
	switch own.Rollup {
	case github.RollupSuccess:
	case "", github.RollupPending, github.RollupExpected:
		return "Its checks haven't all reported yet.", true
	default:
		return "Not every check passed.", false
	}

	if !own.ChecksWhole() {
		return "It has more checks than a routine reads, so not every required one could be found.", false
	}

	for _, name := range requiredChecks(own) {
		if why := requiredPassed(own.Contexts, name); why != "" {
			return why, false
		}
	}

	return "", false
}

// requiredPassed refuses a required check, by name, that didn't report among contexts, or
// that any of its runs failed.
func requiredPassed(contexts []github.RollupContext, name string) string {
	reported := false

	for _, c := range contexts {
		if c.Name != name {
			continue
		}

		if !c.Passed() {
			return "The required check “" + name + "” didn't pass."
		}

		reported = true
	}

	if !reported {
		return "The required check “" + name + "” didn't report on it."
	}

	return ""
}

// requiredChecks are the checks the base branch requires, by name: its rulesets' and its
// classic protection's, each once.
func requiredChecks(own *github.OwnMerge) []string {
	var out []string

	add := func(names []string) {
		for _, n := range names {
			if !slices.Contains(out, n) {
				out = append(out, n)
			}
		}
	}

	for _, r := range own.Rules {
		add(r.Checks)
	}

	if own.Protection != nil {
		add(own.Protection.RequiredChecks)
	}

	return out
}

// threadsResolved refuses a pull request with a review thread not resolved, or more than
// were read.
func threadsResolved(own *github.OwnMerge) string {
	switch {
	case !own.ThreadsWhole():
		return "It has more review threads than a routine reads, so not every one could be checked."
	case own.Unresolved > 0:
		return textutil.Counted(own.Unresolved, "review thread isn't", "review threads aren't") + " resolved."
	}

	return ""
}

// ruleWords names a rule type in words: "merge queue" for MERGE_QUEUE.
func ruleWords(kind string) string {
	return strings.ToLower(strings.ReplaceAll(kind, "_", " "))
}

// short is a commit's SHA as the sheet shows it.
func short(oid string) string {
	return oid[:min(len(oid), shortSHA)]
}
