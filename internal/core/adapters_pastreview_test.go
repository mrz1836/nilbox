package core

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/review"
)

// pastSwitch is Autopilot's settings, with the switch for merging past the rules as on
// says, or a read that fails with err.
type pastSwitch struct {
	on  bool
	err error
}

func (s pastSwitch) Autopilot(context.Context) (autopilotprefs.Config, error) {
	c := autopilotprefs.Defaults()
	c.MergePastRules = s.on

	return c, s.err
}

var errSwitchUnread = errors.New("the settings can't be read")

// ownAwaitingRig is a rig whose pull request #7 is the user's own, held back only for an
// approval from someone else, which GitHub lets them merge past the rules, with a fresh read
// that says it may go, and Autopilot's switch for it as sw says.
func ownAwaitingRig(t *testing.T, sw pastSwitch) *ghRig {
	t.Helper()

	r := newGHRig(t)

	read := ghPullRead(ghPullNumber, ghViewer, "User", ghHeadA, github.MergeStateBlocked)
	read.ViewerDidAuthor, read.ViewerCanBypass = true, true
	read.Update.Details.Reviews = github.Reviews{Decision: github.ReviewDecisionRequired}
	r.pulls.setRead(ghPullNumber, read)

	node := ghNode(ghPullNumber)
	r.pulls.states[node] = &github.MergeState{
		Found: true, Status: github.MergeStateBlocked, Head: ghHeadA, State: github.StateOpen, CanBypass: true,
	}
	r.pulls.owns = map[string]*github.OwnMerge{node: {
		Viewer: ghViewer, ViewerDidAuthor: true, ReviewDecision: github.ReviewDecisionRequired,
		Rollup: github.RollupSuccess, Contexts: []github.RollupContext{{Name: "ci", State: "SUCCESS"}}, ContextCount: 1,
		Commits:     []github.SignedCommit{{OID: ghHeadA, Signed: true, Valid: true, Signer: ghViewer}},
		CommitCount: 1, BaseRead: true, RulesRead: true, RuleCount: 2,
		Rules: []github.BranchRule{
			{Type: github.RulePullRequest, ParamsRead: true},
			{Type: github.RuleRequiredStatusChecks, Checks: []string{"ci"}, ParamsRead: true},
		},
	}}
	r.steps.autopilot = sw

	return r
}

// pastMerge is a Merge it of the rig's pull request, squashed, that the engine lets past
// the rules where an approval is all it lacks.
func (r *ghRig) pastMerge() autopilot.MergeSend {
	req := r.mergeOf(false, catalog.MergeSquash)
	req.PastReview = true

	return req
}

func TestARoutineMergesYourOwnPullRequestPastTheRulesThroughTheSheet(t *testing.T) {
	t.Parallel()

	r := ownAwaitingRig(t, pastSwitch{on: true})

	got, err := r.steps.Merge(t.Context(), r.pastMerge())
	require.NoError(t, err)
	assert.Equal(t, autopilot.GitHubSentIt, got.Outcome)
	assert.Equal(t, "Squash-merged #7 past the rules: it lacked only an approval from someone else.", got.Words)

	writes := r.pulls.writes()
	require.Len(t, writes, 1, "one merge, and no approval")
	assert.True(t, strings.HasPrefix(writes[0], "merge #7 "), writes[0])
	assert.Contains(t, r.pulls.asked(), "own "+ghNode(ghPullNumber), "the guard read all a routine checks")
	assert.True(t, r.pulls.allAutopilots())
}

func TestARoutinesMergePastTheRulesStopsWhereItMayNot(t *testing.T) {
	t.Parallel()

	// A merge the switch keeps back waits for it (past_rules_off); any other closes.
	const (
		closes = autopilot.GitHubNotOffered
		waits  = autopilot.GitHubPastRulesOff
	)

	tests := []struct {
		name    string
		sw      pastSwitch
		edit    func(r *ghRig, req *autopilot.MergeSend)
		want    string
		err     error
		outcome autopilot.GitHubOutcome
	}{
		{
			name: "the engine says why not", sw: pastSwitch{on: true}, want: "never merges past the rules",
			outcome: closes,
			edit: func(_ *ghRig, req *autopilot.MergeSend) {
				req.PastReview, req.PastReviewWhy = false, "A run you started by hand never merges past the rules."
			},
		},
		{
			name: "the engine says the switch is off", sw: pastSwitch{on: true}, want: "is off in Autopilot's settings",
			outcome: waits,
			edit: func(_ *ghRig, req *autopilot.MergeSend) {
				req.PastReview, req.PastReviewWhy, req.PastRulesOff = false, autopilot.PastRulesOffWords, true
			},
		},
		{name: "the switch is off", sw: pastSwitch{}, want: "is off in Autopilot's settings", outcome: waits},
		{
			name: "the switch can't be read", sw: pastSwitch{on: true, err: errSwitchUnread}, want: "is off",
			err: errSwitchUnread, outcome: waits,
		},
		{
			name: "a commit it didn't sign", sw: pastSwitch{on: true}, want: "wasn't signed by you",
			outcome: closes,
			edit: func(r *ghRig, _ *autopilot.MergeSend) {
				r.pulls.owns[ghNode(ghPullNumber)].Commits[0].Signer = ghAuthor
			},
		},
		{
			name: "a rule it can't account for", sw: pastSwitch{on: true}, want: "can't account for",
			outcome: closes,
			edit: func(r *ghRig, _ *autopilot.MergeSend) {
				own := r.pulls.owns[ghNode(ghPullNumber)]
				own.Rules, own.RuleCount = append(own.Rules, github.BranchRule{Type: "MERGE_QUEUE", ParamsRead: true}), 3
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := ownAwaitingRig(t, tt.sw)
			req := r.pastMerge()

			if tt.edit != nil {
				tt.edit(r, &req)
			}

			got, err := r.steps.Merge(t.Context(), req)

			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.outcome, got.Outcome)
			assert.Contains(t, got.Words, tt.want)
			assert.Empty(t, r.pulls.writes(), "nothing merged, nothing asked of the user")
		})
	}
}

func TestARoutinesMergePastTheRulesWaitsWhileGitHubIsntReady(t *testing.T) {
	t.Parallel()

	r := ownAwaitingRig(t, pastSwitch{on: true})
	r.pulls.owns[ghNode(ghPullNumber)].Rollup = github.RollupPending

	got, err := r.steps.Merge(t.Context(), r.pastMerge())
	require.NoError(t, err)
	assert.Equal(t, autopilot.GitHubNotYet, got.Outcome)
	assert.Contains(t, got.Words, "haven't all reported")
	assert.Empty(t, r.pulls.writes())
}

func TestAMergeItWithNoAllowanceNeverGoesPastTheRules(t *testing.T) {
	t.Parallel()

	r := ownAwaitingRig(t, pastSwitch{on: true})

	got, err := r.steps.Merge(t.Context(), r.mergeOf(false, catalog.MergeSquash))
	require.NoError(t, err)
	assert.Equal(t, autopilot.GitHubNotOffered, got.Outcome, "GitHub won't merge it, and nothing goes past the rules")
	assert.Empty(t, r.pulls.writes())
	assert.NotContains(t, r.pulls.asked(), "own "+ghNode(ghPullNumber))
}

func TestAMergePastTheRulesOfAPullRequestGitHubWouldMergeMergesItAsAnyOther(t *testing.T) {
	t.Parallel()

	r := newGHRig(t)
	r.steps.autopilot = pastSwitch{on: true}

	got, err := r.steps.Merge(t.Context(), r.pastMerge())
	require.NoError(t, err)
	assert.Equal(t, autopilot.GitHubSentIt, got.Outcome)
	assert.NotContains(t, got.Words, "past the rules")
	assert.NotContains(t, r.pulls.asked(), "own "+ghNode(ghPullNumber), "someone else's: no merge past the rules")
}

func TestAPastReviewOfferWarnsOfNothingButWhatTheRoutineAllows(t *testing.T) {
	t.Parallel()

	sensitive := review.SensitiveWarning([]string{"Workflows"})
	offer := func(warnings ...string) []review.Offer {
		return []review.Offer{{Action: review.ActionMergeBypass, Enabled: true, Warnings: warnings}}
	}

	_, decided := pastReviewOffer(offer(review.OwnApprovalSkipped), false)
	assert.False(t, decided, "the approval it goes past")

	_, decided = pastReviewOffer(offer(review.OwnApprovalSkipped, sensitive), true)
	assert.False(t, decided, "sensitive files, which the step allows")

	got, decided := pastReviewOffer(offer(review.OwnApprovalSkipped, sensitive), false)
	assert.True(t, decided)
	assert.Equal(t, "Its step doesn't merge it past the rules when it changes sensitive files: Workflows.", got.Words)

	got, decided = pastReviewOffer([]review.Offer{{Action: review.ActionMergeBypass, Reason: "It's a draft."}}, true)
	assert.True(t, decided)
	assert.Equal(t, "It's a draft.", got.Words)

	_, decided = pastReviewOffer(nil, true)
	assert.True(t, decided, "no offer to merge past the rules")
}

// TestOnlyThePastReviewAdapterMergesPastTheRules is the guard: no file of core but
// adapters_pastreview.go names review.ActionMergeBypass, and every ActRequest there that
// does sets Routine, so the window's own merge past the rules is the review sheet's alone
// and a routine's always goes through its guard.
func TestOnlyThePastReviewAdapterMergesPastTheRules(t *testing.T) {
	t.Parallel()

	files, err := filepath.Glob("*.go")
	require.NoError(t, err)

	requests := 0

	for _, path := range files {
		if !strings.HasSuffix(path, "_test.go") {
			requests += bypassRequestsIn(t, path)
		}
	}

	assert.Equal(t, 1, requests, "one merge past the rules, a routine's")
}

// bypassRequestsIn checks the Go file at path: it names review.ActionMergeBypass only when
// it is adapters_pastreview.go, and each ActRequest there that does sets Routine. It returns
// how many such requests the file holds.
func bypassRequestsIn(t *testing.T, path string) int {
	t.Helper()

	src, err := os.ReadFile(path) //nolint:gosec // the package's own files
	require.NoError(t, err)

	file, err := parser.ParseFile(token.NewFileSet(), path, src, parser.SkipObjectResolution)
	require.NoError(t, err)

	requests := 0

	ast.Inspect(file, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && isBypass(sel) {
			assert.Equal(t, "adapters_pastreview.go", path, "%s names review.ActionMergeBypass", path)
		}

		if lit, ok := n.(*ast.CompositeLit); ok && isActRequest(lit.Type) && namesBypass(lit) {
			requests++

			assert.True(t, setsRoutine(lit), "%s sends a merge past the rules with no Routine", path)
		}

		return true
	})

	return requests
}

// isBypass reports review.ActionMergeBypass.
func isBypass(sel *ast.SelectorExpr) bool {
	pkg, ok := sel.X.(*ast.Ident)

	return ok && pkg.Name == "review" && sel.Sel.Name == "ActionMergeBypass"
}

// isActRequest reports the type review.ActRequest.
func isActRequest(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	pkg, ok := sel.X.(*ast.Ident)

	return ok && pkg.Name == "review" && sel.Sel.Name == "ActRequest"
}

// namesBypass reports a literal one of whose values is review.ActionMergeBypass.
func namesBypass(lit *ast.CompositeLit) bool {
	for _, elt := range lit.Elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			if sel, isSel := kv.Value.(*ast.SelectorExpr); isSel && isBypass(sel) {
				return true
			}
		}
	}

	return false
}

// setsRoutine reports a literal that sets its Routine to something.
func setsRoutine(lit *ast.CompositeLit) bool {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}

		if key, isIdent := kv.Key.(*ast.Ident); isIdent && key.Name == "Routine" {
			ident, isNil := kv.Value.(*ast.Ident)

			return !isNil || ident.Name != "nil"
		}
	}

	return false
}
