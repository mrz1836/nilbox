package review

import (
	"strings"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// ownAwaiting is a harness whose pull request is the user's own, its required check passed,
// that the rules hold back only for an approval from someone else, and that GitHub lets
// them merge past the rules; with workflows, its diff changes a workflow file too.
func ownAwaiting(t *testing.T, workflows bool) *harness {
	t.Helper()

	h := newHarness(t)
	h.inbox.pull.AuthorLogin, h.inbox.pull.ReviewRequested = octoUser, false
	h.github.review = pullReview(headA, github.MergeStateBlocked)
	h.github.review.ViewerDidAuthor, h.github.review.ViewerCanBypass = true, true

	if !workflows {
		h.github.diff.Files = h.github.diff.Files[:1]
		h.github.diff.Total = 1
	}

	h.refreshed(t)

	h.github.states = []*github.MergeState{held(true)}
	h.github.own = ownOK()

	return h
}

// ownOK is what a fresh read of the user's own pull request says when a routine may merge
// it past the rules: theirs, one commit they signed, its required check passed, and a
// ruleset whose every rule a routine accounts for.
func ownOK() *github.OwnMerge {
	return &github.OwnMerge{
		Viewer: octoUser, ViewerDidAuthor: true, ReviewDecision: github.ReviewDecisionRequired,
		Rollup: github.RollupSuccess, Contexts: []github.RollupContext{{Name: "test", State: "SUCCESS"}}, ContextCount: 1,
		Commits:     []github.SignedCommit{{OID: headA, Signed: true, Valid: true, Signer: octoUser}},
		CommitCount: 1, BaseRead: true, RulesRead: true, RuleCount: 5,
		Rules: []github.BranchRule{
			{Type: github.RulePullRequest, ParamsRead: true},
			{Type: github.RuleRequiredStatusChecks, Checks: []string{"test"}, ParamsRead: true},
			{Type: github.RuleNonFastForward, ParamsRead: true},
			{Type: github.RuleDeletion, ParamsRead: true},
			{Type: github.RuleCopilotCodeReview, ParamsRead: true},
		},
	}
}

// routineReq is a routine's merge past the rules, squashed, sensitive files allowed or not.
func routineReq(sensitive bool) ActRequest {
	return ActRequest{
		ItemID: pullID, Head: headA, Action: ActionMergeBypass, Method: MethodSquash, Accept: true,
		Routine: &RoutineMerge{PastReview: true, Sensitive: sensitive},
	}
}

func TestARoutineMergesYourOwnPullRequestPastTheRules(t *testing.T) {
	t.Parallel()

	h := ownAwaiting(t, false)

	res, err := h.svc.Act(t.Context(), routineReq(false))
	require.NoError(t, err)

	assert.Equal(t, OutcomeMerged, res.Outcome)
	assert.Equal(t, "Merged acme-org/example-repo#7 past the rules (squash)", res.Message)
	require.Len(t, h.github.merges, 1)
	assert.Equal(t, github.MergeInput{SHA: headA, Method: string(MethodSquash)}, h.github.merges[0])
	assert.Equal(t, 1, h.github.called(callOwn), "the guard read all a routine checks")
	assert.Zero(t, h.github.called(callSubmit), "nothing approves it")
}

func TestARoutineMergesNormallyWhatGitHubWouldMergeNow(t *testing.T) {
	t.Parallel()

	h := ownAwaiting(t, false)
	h.github.states = []*github.MergeState{open(github.MergeStateClean, headA)}

	res, err := h.svc.Act(t.Context(), routineReq(false))
	require.NoError(t, err)

	assert.Equal(t, "Merged acme-org/example-repo#7 (squash)", res.Message)
	require.Len(t, h.github.merges, 1)
}

func TestARoutineNeverMergesPastTheRulesWhatAnyCheckRefuses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		state func(s *github.MergeState)
		own   func(o *github.OwnMerge)
		want  string
	}{
		{name: "another's pull request", own: func(o *github.OwnMerge) { o.ViewerDidAuthor = false }, want: "isn't your own"},
		{
			name: "an unsigned commit", own: func(o *github.OwnMerge) { o.Commits[0].Signed, o.Commits[0].Valid = false, false },
			want: "isn't signed",
		},
		{
			name: "a signature GitHub didn't verify", own: func(o *github.OwnMerge) { o.Commits[0].Valid = false },
			want: "didn't verify",
		},
		{
			name: "a commit someone else signed", own: func(o *github.OwnMerge) { o.Commits[0].Signer = octoOther },
			want: "wasn't signed by you",
		},
		{
			name: "a commit with no signer", own: func(o *github.OwnMerge) { o.Commits[0].Signer = "" },
			want: "wasn't signed by you",
		},
		{name: "more than a hundred commits", own: func(o *github.OwnMerge) { o.CommitCount = 101 }, want: "more than 100 commits"},
		{name: "no commits read", own: func(o *github.OwnMerge) { o.Commits, o.CommitCount = nil, 0 }, want: "None of its commits"},
		{name: "rules not read", own: func(o *github.OwnMerge) { o.RulesRead = false }, want: "Not every rule"},
		{name: "a rule past the page", own: func(o *github.OwnMerge) { o.RuleCount = 6 }, want: "Not every rule"},
		{name: "no base branch", own: func(o *github.OwnMerge) { o.BaseRead = false }, want: "Not every rule"},
		{
			name: "an unknown rule",
			own: func(o *github.OwnMerge) {
				o.Rules, o.RuleCount = append(o.Rules, github.BranchRule{Type: "SOME_NEW_RULE", ParamsRead: true}), 6
			},
			want: "(some new rule)",
		},
		{
			name: "a merge queue",
			own: func(o *github.OwnMerge) {
				o.Rules, o.RuleCount = append(o.Rules, github.BranchRule{Type: "MERGE_QUEUE", ParamsRead: true}), 6
			},
			want: "(merge queue)",
		},
		{
			name: "required deployments",
			own: func(o *github.OwnMerge) {
				o.Rules, o.RuleCount = append(o.Rules, github.BranchRule{Type: "REQUIRED_DEPLOYMENTS", ParamsRead: true}), 6
			},
			want: "(required deployments)",
		},
		{
			name: "a locked branch",
			own: func(o *github.OwnMerge) {
				o.Rules, o.RuleCount = append(o.Rules, github.BranchRule{Type: "LOCK_BRANCH", ParamsRead: true}), 6
			},
			want: "(lock branch)",
		},
		{
			name: "classic protection that locks the branch",
			own:  func(o *github.OwnMerge) { o.Protection = &github.ClassicProtection{LockBranch: true} },
			want: "is locked",
		},
		{
			name: "classic protection that requires deployments",
			own:  func(o *github.OwnMerge) { o.Protection = &github.ClassicProtection{RequiresDeployments: true} },
			want: "requires deployments",
		},
		{name: "behind its base", state: func(s *github.MergeState) { s.Status = github.MergeStateBehind }, want: "behind its base"},
		{name: "conflicts", state: func(s *github.MergeState) { s.Status = github.MergeStateDirty }, want: "has conflicts"},
		{
			name: "optional checks failing", state: func(s *github.MergeState) { s.Status = github.MergeStateUnstable },
			want: "Not every check",
		},
		{name: "GitHub no longer letting you", state: func(s *github.MergeState) { s.CanBypass = false }, want: "doesn't let you"},
		{
			name: "changes requested", own: func(o *github.OwnMerge) { o.ReviewDecision = github.ReviewDecisionChangesRequested },
			want: "Changes were requested",
		},
		{
			name: "approved and still held back", own: func(o *github.OwnMerge) { o.ReviewDecision = github.ReviewDecisionApproved },
			want: "more than an approval",
		},
		{name: "no review required", own: func(o *github.OwnMerge) { o.ReviewDecision = "" }, want: "more than an approval"},
		{name: "a check failing", own: func(o *github.OwnMerge) { o.Rollup = "FAILURE" }, want: "Not every check passed"},
		{name: "more checks than a page", own: func(o *github.OwnMerge) { o.ContextCount = 101 }, want: "more checks than"},
		{
			name: "a required check missing", own: func(o *github.OwnMerge) { o.Contexts[0].Name = "lint" },
			want: "“test” didn't report",
		},
		{
			name: "a required check of classic protection missing",
			own:  func(o *github.OwnMerge) { o.Protection = &github.ClassicProtection{RequiredChecks: []string{"build"}} },
			want: "“build” didn't report",
		},
		{
			name: "a required check skipped",
			own: func(o *github.OwnMerge) {
				o.Contexts, o.ContextCount = append(o.Contexts, github.RollupContext{Name: "test", State: "SKIPPED"}), 2
			},
			want: "“test” didn't pass",
		},
		{name: "an unresolved thread", own: func(o *github.OwnMerge) { o.ThreadCount, o.ThreadsRead, o.Unresolved = 1, 1, 1 }, want: "isn't resolved"},
		{name: "more threads than a page", own: func(o *github.OwnMerge) { o.ThreadCount = 101 }, want: "more review threads"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := ownAwaiting(t, false)

			if tt.state != nil {
				tt.state(h.github.states[0])
			}

			if tt.own != nil {
				tt.own(h.github.own)
			}

			_, err := h.svc.Act(t.Context(), routineReq(false))
			require.ErrorIs(t, err, ErrPastRulesRefused)
			require.ErrorIs(t, err, ErrNotOffered, "it is no longer offered")
			require.NotErrorIs(t, err, ErrPastRulesNotYet)
			assert.Contains(t, uierr.Message(err), tt.want)
			assert.Zero(t, h.github.called(callMerge))
		})
	}
}

func TestARoutineMergesPastTheRulesOnlyWithTheGuardsRead(t *testing.T) {
	t.Parallel()

	h := ownAwaiting(t, false)
	h.github.own = nil

	_, err := h.svc.Act(t.Context(), routineReq(false))
	require.ErrorIs(t, err, ErrPastRulesRefused)
	assert.Zero(t, h.github.called(callMerge))
}

func TestARoutineWaitsWhileGitHubWorksItOut(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		own  func(o *github.OwnMerge)
	}{
		{name: "checks still running", own: func(o *github.OwnMerge) { o.Rollup = github.RollupPending }},
		{name: "checks expected", own: func(o *github.OwnMerge) { o.Rollup = github.RollupExpected }},
		{name: "no rollup yet", own: func(o *github.OwnMerge) { o.Rollup = "" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := ownAwaiting(t, false)
			tt.own(h.github.own)

			_, err := h.svc.Act(t.Context(), routineReq(false))
			require.ErrorIs(t, err, ErrPastRulesNotYet)
			require.NotErrorIs(t, err, ErrNotOffered, "it may go once GitHub is done")
			assert.Contains(t, uierr.Message(err), "haven't all reported")
			assert.Zero(t, h.github.called(callMerge))
		})
	}

	t.Run("the merge state unknown", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			h := ownAwaiting(t, false)
			h.github.states[0].Status = github.MergeStateUnknown

			_, err := h.svc.Act(t.Context(), routineReq(false))
			require.ErrorIs(t, err, ErrPastRulesNotYet)
			assert.Contains(t, uierr.Message(err), "still working out")
			assert.Equal(t, guardReads, h.github.called(callOwn), "read again while GitHub works it out")
			assert.Zero(t, h.github.called(callMerge))
		})
	})
}

func TestARoutineMergesPastTheRulesOnlyWhatTheSheetFindsYoursAwaitingApproval(t *testing.T) {
	t.Parallel()

	t.Run("someone else's pull request", func(t *testing.T) {
		t.Parallel()

		h := bypassHarness(t)
		h.github.states = []*github.MergeState{held(true)}
		h.github.own = ownOK()

		_, err := h.svc.Act(t.Context(), routineReq(true))
		require.ErrorIs(t, err, ErrPastRulesRefused)
		assert.Contains(t, uierr.Message(err), "isn't your own")
		assert.Zero(t, h.github.called(callState), "refused before GitHub is asked")
	})

	t.Run("another action", func(t *testing.T) {
		t.Parallel()

		h := ownAwaiting(t, false)
		req := routineReq(false)
		req.Action = ActionAutoMerge

		_, err := h.svc.Act(t.Context(), req)
		require.ErrorIs(t, err, ErrPastRulesRefused)
		assert.Zero(t, h.github.called(callAuto))
	})

	t.Run("no merge past the review asked", func(t *testing.T) {
		t.Parallel()

		h := ownAwaiting(t, false)
		req := routineReq(false)
		req.Routine.PastReview = false

		_, err := h.svc.Act(t.Context(), req)
		require.ErrorIs(t, err, ErrPastRulesRefused)
		assert.Zero(t, h.github.called(callMerge))
	})

	t.Run("sensitive files the routine doesn't allow", func(t *testing.T) {
		t.Parallel()

		h := ownAwaiting(t, true)

		_, err := h.svc.Act(t.Context(), routineReq(false))
		require.ErrorIs(t, err, ErrPastRulesRefused)
		assert.Contains(t, uierr.Message(err), "sensitive files")
		assert.Zero(t, h.github.called(callMerge))
	})

	t.Run("sensitive files the routine allows", func(t *testing.T) {
		t.Parallel()

		h := ownAwaiting(t, true)

		res, err := h.svc.Act(t.Context(), routineReq(true))
		require.NoError(t, err)
		assert.Equal(t, OutcomeMerged, res.Outcome)
	})

	t.Run("unaccepted", func(t *testing.T) {
		t.Parallel()

		h := ownAwaiting(t, false)
		req := routineReq(false)
		req.Accept = false

		_, err := h.svc.Act(t.Context(), req)
		require.ErrorIs(t, err, ErrWarningsNotAccepted)
	})
}

func TestTheSheetSaysYourOwnAwaitsApproval(t *testing.T) {
	t.Parallel()

	assert.True(t, ownAwaiting(t, false).refreshed(t).OwnAwaitsApproval)
	assert.False(t, bypassHarness(t).refreshed(t).OwnAwaitsApproval, "someone else's")
	assert.False(t, ownHarness(t, github.MergeStateBlocked).refreshed(t).OwnAwaitsApproval, "GitHub doesn't let you past")
}

func TestARoutinesAcceptedWarnings(t *testing.T) {
	t.Parallel()

	sensitive := SensitiveWarning([]string{"Workflows", "Agent config"})
	assert.Equal(t, "It changes sensitive files: Workflows, Agent config.", sensitive)

	assert.Empty(t, StrayWarning([]string{OwnApprovalSkipped}, false))
	assert.Empty(t, StrayWarning([]string{OwnApprovalSkipped, sensitive}, true))
	assert.Equal(t, sensitive, StrayWarning([]string{OwnApprovalSkipped, sensitive}, false))
	assert.Equal(t, "Required checks are still running.", StrayWarning([]string{"Required checks are still running."}, true))
	assert.Empty(t, StrayWarning(nil, false))

	require.ErrorIs(t, ErrPastRulesRefused, ErrNotOffered)
	assert.NotErrorIs(t, ErrPastRulesNotYet, ErrNotOffered)
}

func TestTheOwnApprovalSentenceIsTheOneTheSheetSkips(t *testing.T) {
	t.Parallel()

	f := Facts{
		Mine: true, ReviewRequired: true, MergeState: github.MergeStateBlocked, CanBypass: true, CanWrite: true,
		Methods: []MergeMethod{MethodSquash},
	}

	past, ok := Decide(f, Prefs{Method: PrefRepo}).offer(ActionMergeBypass)
	require.True(t, ok)
	assert.Equal(t, []string{OwnApprovalSkipped}, past.Warnings)

	f.Sensitive = []string{"Workflows"}
	past, _ = Decide(f, Prefs{Method: PrefRepo}).offer(ActionMergeBypass)
	assert.Equal(t, []string{OwnApprovalSkipped, SensitiveWarning([]string{"Workflows"})}, past.Warnings)
	assert.True(t, strings.HasPrefix(past.Warnings[1], sensitivePrefix))
}
