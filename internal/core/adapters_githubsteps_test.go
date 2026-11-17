package core

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/budget"
	"github.com/mrz1836/nilbox/internal/issues"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/review"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// errDropped is a connection that dropped after a request may have reached GitHub.
var errDropped = errors.New("the connection dropped")

// TestTheGitHubStepReachesNoForbiddenAction is the guard over the GitHub step's dispatch
// path: the adapter names no sheet action but asking Dependabot to rebase, updating the
// branch, and a comment, so no merge (merge_now, auto_merge, merge_bypass, approve_merge,
// approve_auto_merge), approval, request for changes, close, or approval of workflow runs
// is reachable from a routine's GitHub step; Merge it and Post the agent's review are
// steps of their own, behind their own guards.
func TestTheGitHubStepReachesNoForbiddenAction(t *testing.T) {
	t.Parallel()

	allowed := []string{
		"review.ActionRebaseBot", "review.ActionUpdateBranch", "review.ActionComment", "review.ActionNone",
		"issues.ActionComment",
	}

	named := sheetActionsNamed(t, "adapters_githubsteps.go")
	require.NotEmpty(t, named, "the guard reads the adapter")

	for _, name := range named {
		assert.Contains(t, allowed, name, "the GitHub step must never name %s", name)
	}

	safe := []review.Action{review.ActionRebaseBot, review.ActionUpdateBranch, review.ActionComment}

	for _, a := range catalog.GitHubActions() {
		got, ok := reviewActionOf(a.Kind)
		if a.Kind == catalog.GitHubLabels {
			assert.False(t, ok, "labels go through the label edit")

			continue
		}

		require.True(t, ok, a.Kind)
		assert.Contains(t, safe, got, a.Kind)
	}
}

// sheetActionsNamed returns every review or issue sheet action the Go file at path names,
// as "review.ActionRebaseBot", each once.
func sheetActionsNamed(t *testing.T, path string) []string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	require.NoError(t, err)

	var named []string

	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		pkg, isIdent := sel.X.(*ast.Ident)
		if !isIdent || (pkg.Name != "review" && pkg.Name != "issues") ||
			!strings.HasPrefix(sel.Sel.Name, "Action") || sel.Sel.Name == "Action" {
			return true
		}

		if name := pkg.Name + "." + sel.Sel.Name; !slices.Contains(named, name) {
			named = append(named, name)
		}

		return true
	})

	return named
}

func TestTheReviewSheetsOffersDecideAPullRequestsAction(t *testing.T) {
	t.Parallel()

	offers := []review.Offer{
		{Action: review.ActionRebaseBot, Enabled: false, Reason: "It's up to date with main, so there's nothing to rebase."},
		{Action: review.ActionUpdateBranch, Enabled: true, Warnings: []string{"Dependabot stops rebasing it.", "Sure?"}},
		{Action: review.ActionComment, Enabled: true, Warnings: []string{}},
	}

	got, decided := pullOffer(offers, review.ActionRebaseBot, "")
	assert.True(t, decided)
	assert.Equal(t, autopilot.GitHubNotOffered, got.Outcome)
	assert.Equal(t, "It's up to date with main, so there's nothing to rebase.", got.Words)

	got, decided = pullOffer(offers, review.ActionUpdateBranch, "")
	assert.True(t, decided)
	assert.Equal(t, autopilot.GitHubWarning, got.Outcome, "a routine never accepts a warning itself")
	assert.Equal(t, "Dependabot stops rebasing it. Sure?", got.Words)

	_, decided = pullOffer(offers, review.ActionUpdateBranch, "Dependabot stops rebasing it. Sure?")
	assert.False(t, decided, "the warnings the user accepted let it go")

	got, decided = pullOffer(offers, review.ActionUpdateBranch, "An older warning.")
	assert.True(t, decided)
	assert.Equal(t, autopilot.GitHubWarning, got.Outcome, "other warnings wait again")

	_, decided = pullOffer(offers, review.ActionComment, "")
	assert.False(t, decided)

	got, decided = pullOffer(offers[1:], review.ActionRebaseBot, "")
	assert.True(t, decided)
	assert.Equal(t, autopilot.GitHubNotOffered, got.Outcome, "an action the sheet doesn't list isn't offered")
}

func TestALabelsActionSendsOnlyWhatChangesTheLabels(t *testing.T) {
	t.Parallel()

	add, remove := labelChanges(spec.LabelsAction{Add: []string{"needs-repro", "bug"}, Remove: []string{"triage", "p1"}},
		[]string{"Bug", "triage"})
	assert.Equal(t, []string{"needs-repro"}, add, "a label it has, in any case, isn't added again")
	assert.Equal(t, []string{"triage"}, remove, "a label it hasn't isn't taken off")

	assert.Equal(t, "Added “needs-repro”; took off “triage”.", labelWords(add, remove, false))
	assert.Equal(t, "Adds “needs-repro”.", labelWords(add, nil, true))

	sent := labelsSent(add, remove, 1, "GitHub kept “triage”.")
	assert.Equal(t, autopilot.GitHubSentIt, sent.Outcome, "a change GitHub made stands")
	assert.Contains(t, sent.Words, "1 of its changes went, then: GitHub kept “triage”.")
}

func TestARefusalBeforeAnythingIsSentHoldsOrIsNotOffered(t *testing.T) {
	t.Parallel()

	paced := uierr.New("The app made 30 changes on GitHub in the last minute.", fmt.Errorf("%w", budget.ErrWritePace))

	got, err := refusal(paced, review.ErrNotOffered)
	require.NoError(t, err)
	assert.Equal(t, autopilot.GitHubHeld, got.Outcome, "the write gate's refusal holds the GitHub actions")
	assert.Equal(t, "The app made 30 changes on GitHub in the last minute.", got.Words)

	gone := uierr.New("That isn't something the sheet can do with this pull request.",
		fmt.Errorf("%w: %q", review.ErrNotOffered, review.ActionRebaseBot))

	got, err = refusal(gone, review.ErrNotOffered)
	require.NoError(t, err)
	assert.Equal(t, autopilot.GitHubNotOffered, got.Outcome)

	_, err = refusal(errDropped, review.ErrNotOffered)
	require.ErrorIs(t, err, errDropped, "anything else may have reached GitHub, and fails the step")

	labels := fmt.Errorf("%w: permission %q", issues.ErrCannotEdit, "READ")
	got, err = refusal(labels, issues.ErrCannotEdit)
	require.NoError(t, err)
	assert.Equal(t, autopilot.GitHubNotOffered, got.Outcome)
}

func TestAGitHubStepPostsACommentsReplyAlone(t *testing.T) {
	t.Parallel()

	store := prompts.NewStore(testutil.DB(t), testutil.NewClock(time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)))
	require.NoError(t, prompts.Seed(t.Context(), store))

	g := githubSteps{prompts: store}

	for _, ref := range []spec.PromptRef{
		{Starter: prompts.StarterReplyLGTM},
		{Starter: prompts.StarterReplyUpdateOK},
		{Nickname: "needs changes"},
		{Starter: prompts.StarterFixChecks},
	} {
		_, err := g.reply(t.Context(), ref)
		require.ErrorIs(t, err, ErrNotACommentReply, "%+v: a GitHub step never posts an approval's words", ref)
		assert.Contains(t, uierr.Message(err), "a GitHub step only comments")
	}

	got, err := g.reply(t.Context(), spec.PromptRef{Starter: prompts.StarterReplyQuestion})
	require.NoError(t, err)
	assert.Equal(t, "Question", got.Nickname)

	_, err = g.reply(t.Context(), spec.PromptRef{Nickname: "Gone"})
	require.ErrorIs(t, err, errNoReply)
}

func TestTheLibraryTellsEachReplysReview(t *testing.T) {
	t.Parallel()

	store := prompts.NewStore(testutil.DB(t), testutil.NewClock(time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)))
	require.NoError(t, prompts.Seed(t.Context(), store))

	library, err := promptLibrary{prompts: store}.Prompts(t.Context())
	require.NoError(t, err)

	byStarter := map[string]autopilot.ReplyFor{}
	for _, p := range library {
		byStarter[p.Starter] = p.ReplyFor
	}

	assert.Equal(t, autopilot.ReplyForApproval, byStarter[prompts.StarterReplyLGTM])
	assert.Equal(t, autopilot.ReplyForApproval, byStarter[prompts.StarterReplyUpdateOK])
	assert.Equal(t, autopilot.ReplyForRequestChanges, byStarter[prompts.StarterReplyNeedsChanges])
	assert.Equal(t, autopilot.ReplyForComment, byStarter[prompts.StarterReplyQuestion])
	assert.True(t, slices.ContainsFunc(library, func(p autopilot.LibraryPrompt) bool { return !p.Reply }))
}
