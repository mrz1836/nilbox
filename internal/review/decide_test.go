package review

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/sensitive"
)

// facts are a pull request someone else opened, which the user may merge by squash or
// merge commit, with auto-merge allowed, in merge state status, and may update while it is
// behind: GitHub's viewerCanUpdateBranch is false for a branch up to date with its base.
func facts(status string) Facts {
	return Facts{
		MergeState: status, CanWrite: true, CanUpdateBranch: status == github.MergeStateBehind, CanAutoMerge: true,
		Base:    "main",
		Methods: []MergeMethod{MethodMerge, MethodSquash}, ViewerMethod: MethodSquash, Sensitive: []string{},
	}
}

// enabled lists the plan's enabled actions.
func enabled(p Plan) []Action {
	var out []Action

	for _, o := range p.Offers {
		if o.Enabled {
			out = append(out, o.Action)
		}
	}

	return out
}

func TestDecide(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		facts   func() Facts
		primary Action
		enabled []Action
		ready   ReadyState
	}{
		{
			name: "clean", facts: func() Facts { return facts(github.MergeStateClean) }, primary: ActionApproveMerge,
			enabled: []Action{ActionApproveMerge, ActionMerge, ActionApprove, ActionRequestChanges, ActionComment},
			ready:   ReadyMerge,
		},
		{
			name: "waits on the approval", primary: ActionApproveMerge, ready: ReadyBlocked,
			facts: func() Facts {
				f := facts(github.MergeStateBlocked)
				f.ReviewRequired = true

				return f
			},
			enabled: []Action{ActionApproveMerge, ActionApproveAutoMerge, ActionApprove, ActionRequestChanges, ActionComment},
		},
		{
			name: "required checks running", primary: ActionApproveAutoMerge, ready: ReadyBlocked,
			facts: func() Facts {
				f := facts(github.MergeStateBlocked)
				f.ReviewRequired, f.RequiredPending, f.Pending = true, 1, 1

				return f
			},
			enabled: []Action{ActionApproveAutoMerge, ActionApprove, ActionRequestChanges, ActionComment},
		},
		{
			name: "an optional check failing", primary: ActionApproveMerge, ready: ReadyUnstable,
			facts: func() Facts {
				f := facts(github.MergeStateUnstable)
				f.Failing = 1

				return f
			},
			enabled: []Action{ActionApproveMerge, ActionMerge, ActionApprove, ActionRequestChanges, ActionComment},
		},
		{
			name: "failing checks", primary: ActionComment, ready: ReadyBlocked,
			facts: func() Facts {
				f := facts(github.MergeStateBlocked)
				f.RequiredFailing, f.Failing = 1, 1

				return f
			},
			enabled: []Action{ActionApproveAutoMerge, ActionAutoMerge, ActionApprove, ActionRequestChanges, ActionComment},
		},
		{
			name: "behind", primary: ActionUpdateBranch, ready: ReadyBehind,
			facts:   func() Facts { return facts(github.MergeStateBehind) },
			enabled: []Action{ActionApprove, ActionRequestChanges, ActionComment, ActionUpdateBranch},
		},
		{
			name: "dependabot behind", primary: ActionRebaseBot, ready: ReadyBehind,
			facts: func() Facts {
				f := facts(github.MergeStateBehind)
				f.Dependabot = true

				return f
			},
			enabled: []Action{ActionApprove, ActionRequestChanges, ActionComment, ActionRebaseBot, ActionUpdateBranch},
		},
		{
			name: "dependabot with conflicts", primary: ActionRebaseBot, ready: ReadyConflicts,
			facts: func() Facts {
				f := facts(github.MergeStateDirty)
				f.Dependabot = true

				return f
			},
			enabled: []Action{ActionApprove, ActionRequestChanges, ActionComment, ActionRebaseBot},
		},
		{
			name: "already approved", primary: ActionMerge, ready: ReadyMerge,
			facts: func() Facts {
				f := facts(github.MergeStateClean)
				f.ApprovedAtHead = true

				return f
			},
			enabled: []Action{ActionMerge, ActionRequestChanges, ActionComment},
		},
		{
			name: "approved and waiting", primary: ActionAutoMerge, ready: ReadyBlocked,
			facts: func() Facts {
				f := facts(github.MergeStateBlocked)
				f.ApprovedAtHead, f.RequiredPending = true, 1

				return f
			},
			enabled: []Action{ActionAutoMerge, ActionRequestChanges, ActionComment},
		},
		{
			name: "my own", primary: ActionMerge, ready: ReadyMerge,
			facts: func() Facts {
				f := facts(github.MergeStateClean)
				f.Mine = true

				return f
			},
			enabled: []Action{ActionMerge, ActionComment},
		},
		{
			name: "a pending review", primary: ActionMerge, ready: ReadyMerge,
			facts: func() Facts {
				f := facts(github.MergeStateClean)
				f.PendingReview = true

				return f
			},
			enabled: []Action{ActionMerge},
		},
		{
			name: "a draft", primary: ActionApprove, ready: ReadyDraft,
			facts: func() Facts {
				f := facts(github.MergeStateClean)
				f.Draft = true

				return f
			},
			enabled: []Action{ActionApprove, ActionRequestChanges, ActionComment},
		},
		{
			name: "a merge queue", primary: ActionApprove, ready: ReadyQueue,
			facts: func() Facts {
				f := facts(github.MergeStateClean)
				f.MergeQueue = true

				return f
			},
			enabled: []Action{ActionApprove, ActionRequestChanges, ActionComment},
		},
		{
			name: "read-only access", primary: ActionApprove, ready: ReadyMerge,
			facts: func() Facts {
				f := facts(github.MergeStateClean)
				f.CanWrite = false

				return f
			},
			enabled: []Action{ActionApprove, ActionRequestChanges, ActionComment},
		},
		{
			name: "still working it out", primary: ActionApprove, ready: ReadyUnknown,
			facts:   func() Facts { return facts(github.MergeStateUnknown) },
			enabled: []Action{ActionApprove, ActionRequestChanges, ActionComment},
		},
		{
			name: "auto-merge already on", primary: ActionApprove, ready: ReadyAutoMerge,
			facts: func() Facts {
				f := facts(github.MergeStateBlocked)
				f.AutoMergeOn, f.RequiredPending = true, 1

				return f
			},
			enabled: []Action{ActionApprove, ActionRequestChanges, ActionComment},
		},
		{
			name: "closed", primary: ActionNone, ready: ReadyClosed,
			facts: func() Facts {
				f := facts(github.MergeStateClean)
				f.Closed = true

				return f
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := Decide(tt.facts(), PrefRepo)
			assert.Equal(t, tt.primary, p.Primary)
			assert.ElementsMatch(t, tt.enabled, enabled(p))
			assert.Equal(t, tt.ready, p.Readiness.State)

			// A user who may close it gets Close as well, in the menu: the button stays.
			f := tt.facts()
			f.CanClose = true
			withClose := Decide(f, PrefRepo)
			assert.Equal(t, tt.primary, withClose.Primary, "a close is never the button")

			if f.Closed {
				assert.Empty(t, withClose.Offers)

				return
			}

			assert.ElementsMatch(t, append(slices.Clone(tt.enabled), ActionClose), enabled(withClose))
			assert.Equal(t, ActionClose, withClose.Offers[len(withClose.Offers)-1].Action, "last in the menu")
		})
	}
}

func TestDecideOffersACloseToWhoeverMayClose(t *testing.T) {
	t.Parallel()

	f := facts(github.MergeStateClean)
	f.CanClose = true

	o, ok := Decide(f, PrefRepo).offer(ActionClose)
	require.True(t, ok)
	assert.Equal(t, Offer{
		Action: ActionClose, Label: "Close pull request", Enabled: true, Confirm: true, Warnings: []string{},
		CommentsFirst: true,
	}, o, "it asks first, and posts the text first, as a comment")

	// A draft, a merge queue, no merge method, checks failing, and conflicts hold back no
	// close.
	f.Draft, f.MergeQueue, f.Methods, f.RequiredFailing, f.MergeState = true, true, nil, 1, github.MergeStateDirty
	o, ok = Decide(f, PrefRepo).offer(ActionClose)
	require.True(t, ok)
	assert.True(t, o.Enabled)

	f.CanClose = false
	_, ok = Decide(f, PrefRepo).offer(ActionClose)
	assert.False(t, ok, "never offered to a user GitHub doesn't let close it")

	f.CanClose, f.Closed = true, true
	_, ok = Decide(f, PrefRepo).offer(ActionClose)
	assert.False(t, ok, "never offered once merged or closed")
}

func TestACloseWaitsForTheSheetToReadThePullRequest(t *testing.T) {
	t.Parallel()

	// Before the sheet read it, GitHub hasn't said whether the user may close it: the close
	// waits, as the rebase does, whatever else is known.
	for _, canClose := range []bool{false, true} {
		f := facts("")
		f.CanClose = canClose

		o, ok := Decide(f, PrefRepo).offer(ActionClose)
		require.True(t, ok)
		assert.False(t, o.Enabled)
		assert.Equal(t, "Wait for it to load from GitHub.", o.Reason)
		assert.NotEqual(t, ActionClose, Decide(f, PrefRepo).Primary)
	}
}

func TestOnlyGitHubSaysWhoMayClose(t *testing.T) {
	t.Parallel()

	c := &cached{head: headA, context: pullContext{MergeState: github.MergeStateBlocked}}

	for _, permission := range []string{"ADMIN", "WRITE", "TRIAGE", "READ", ""} {
		pull := storedPull()
		pull.ViewerPermission = permission

		assert.False(t, factsOf(pull, c, Review{State: github.StateOpen, Mine: true}).CanClose,
			"neither the author nor %q access closes what GitHub says the user can't", permission)
	}

	// A deleted author's pull request, whose login is empty, as the viewer's may be: GitHub's
	// say alone counts.
	ghost := storedPull()
	ghost.AuthorLogin, ghost.Viewer, ghost.ViewerPermission = "", "", "READ"
	sheet := build(ghost, c, sensitive.NewMatcher(sensitive.DefaultConfig()), PrefRepo)
	assert.False(t, factsOf(ghost, c, sheet).CanClose)
	assert.False(t, slices.ContainsFunc(sheet.Offers, func(o Offer) bool { return o.Action == ActionClose }),
		"so the sheet offers no close")

	c.context.CanClose = true
	assert.True(t, factsOf(storedPull(), c, Review{State: github.StateOpen}).CanClose)
	assert.False(t, factsOf(storedPull(), nil, Review{State: github.StateOpen}).CanClose, "nothing read, nothing said")
}

func TestACloseIsTheOnlyActionLeft(t *testing.T) {
	t.Parallel()

	// Someone else's pull request, for a user with triage access, who may close it but not
	// merge it, with a review of theirs pending on GitHub: only the close runs, from the
	// menu, and it says why it can't post a comment first.
	f := facts(github.MergeStateBlocked)
	f.CanWrite, f.CanAutoMerge, f.PendingReview, f.CanClose = false, false, true, true

	p := Decide(f, PrefRepo)
	assert.Equal(t, []Action{ActionClose}, enabled(p))
	assert.Equal(t, ActionNone, p.Primary, "the button only opens the menu")

	o, _ := p.offer(ActionClose)
	assert.Equal(t, "You have a review of this pull request started on GitHub. Submit or discard it there first. "+
		"Or clear your text and line comments to close it without a comment.", o.CommentBlocked)
}

func TestDecideSaysWhatKeepsAMergeFromPassing(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		failing, required, pending, approval, notStarted int
		sentence, warning                                string
	}{
		{
			0, 0, 0, 0, 0, "Ready to merge · a status isn't passing",
			"GitHub counts a status of the head commit as not passing, though no check the sheet read is failing.",
		},
		{1, 0, 0, 0, 0, "Ready to merge · 1 optional check failing", "1 optional check is failing."},
		{3, 1, 0, 0, 0, "Ready to merge · 2 optional checks failing", "2 optional checks are failing."},
		{0, 0, 2, 0, 0, "Ready to merge · 2 optional checks running", "2 optional checks are still running."},
		{
			0, 0, 0, 3, 1, "Ready to merge · 3 workflow runs awaiting approval · 1 workflow run that didn't start",
			"3 workflow runs are waiting for a maintainer's approval to run, as a fork's runs do. 1 workflow run " +
				"failed to start. No check shows them, and every check that ran passed: GitHub merges it as it is.",
		},
		{
			0, 0, 0, 1, 0, "Ready to merge · 1 workflow run awaiting approval",
			"1 workflow run is waiting for a maintainer's approval to run, as a fork's runs do. No check shows " +
				"them, and every check that ran passed: GitHub merges it as it is.",
		},
		{
			1, 0, 0, 0, 2, "Ready to merge · 1 optional check failing · 2 workflow runs that didn't start",
			"1 optional check is failing. 2 workflow runs failed to start.",
		},
	} {
		f := facts(github.MergeStateUnstable)
		f.Failing, f.RequiredFailing, f.Pending = c.failing, c.required, c.pending
		f.AwaitingApproval, f.NotStarted = c.approval, c.notStarted

		assert.Equal(t, c.sentence, readiness(f).Sentence)
		assert.Equal(t, c.warning, unstableWarning(f))
	}
}

func TestDecideWarnsBeforeARiskyMerge(t *testing.T) {
	t.Parallel()

	f := facts(github.MergeStateUnstable)
	f.Sensitive = []string{"Workflows"}
	f.Failing = 1

	p := Decide(f, PrefRepo)

	merge, ok := p.offer(ActionMerge)
	require.True(t, ok)
	assert.True(t, merge.Enabled)
	assert.True(t, merge.Confirm, "every merge asks first")
	assert.Equal(t, []string{"1 optional check is failing.", "It changes sensitive files: Workflows."}, merge.Warnings)
	assert.Equal(t, ReadyUnstable, p.Readiness.State)
	assert.Equal(t, "Ready to merge · 1 optional check failing", p.Readiness.Sentence)

	approve, _ := p.offer(ActionApprove)
	assert.False(t, approve.Confirm, "an approval is one click")
	assert.Empty(t, approve.Warnings)
}

func TestDecideLabelsSayWhatHappens(t *testing.T) {
	t.Parallel()

	p := Decide(facts(github.MergeStateClean), PrefRepo)

	o, _ := p.offer(ActionApproveMerge)
	assert.Equal(t, "Approve & squash-merge", o.Label)

	p = Decide(facts(github.MergeStateClean), PrefMerge)
	assert.Equal(t, MethodMerge, p.Method)

	o, _ = p.offer(ActionMerge)
	assert.Equal(t, "Merge", o.Label)
	assert.Equal(t, "Rebase-merge", mergeLabel(MethodRebase))
	assert.Equal(t, "Merge", mergeLabel(""))
}

func TestReadinessSentences(t *testing.T) {
	t.Parallel()

	f := facts(github.MergeStateBlocked)
	f.ReviewRequired, f.RequiredPending = true, 2
	assert.Equal(t, "An approval · checks running · auto-merge available", readiness(f).Sentence)

	f = facts(github.MergeStateBlocked)
	f.ChangesRequested, f.RequiredFailing = true, 2
	assert.Equal(t, "Changes requested · 2 required checks failing", readiness(f).Sentence)
	assert.Equal(t, []string{"changes requested", "2 required checks failing"}, readiness(f).Needs)

	assert.Equal(t, "Blocked by branch protection", readiness(facts(github.MergeStateBlocked)).Sentence)
	assert.Equal(t, "Behind main", readiness(facts(github.MergeStateBehind)).Sentence)

	f = facts(github.MergeStateBlocked)
	f.AutoMergeOn, f.RequiredPending = true, 1
	assert.Equal(t, "Auto-merge is on · waiting for checks running", readiness(f).Sentence)

	f = facts(github.MergeStateDirty)
	f.Base = ""
	assert.Equal(t, "Conflicts with its base branch", readiness(f).Sentence)
}

func TestNotNowReasons(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "It's behind main.", notNowReason(facts(github.MergeStateBehind)))
	assert.Equal(t, "It has conflicts with main.", notNowReason(facts(github.MergeStateDirty)))
	assert.Equal(t, "Branch protection blocks it.", notNowReason(facts(github.MergeStateBlocked)))
	assert.Contains(t, notNowReason(facts(github.MergeStateUnknown)), "still working out")

	f := facts(github.MergeStateBlocked)
	f.RequiredFailing = 1
	assert.Equal(t, "It still needs 1 required check failing.", notNowReason(f))
}

func TestChooseMethod(t *testing.T) {
	t.Parallel()

	all := []MergeMethod{MethodMerge, MethodSquash, MethodRebase}

	assert.Equal(t, MethodRebase, chooseMethod(all, PrefRebase, MethodSquash), "the user's choice")
	assert.Equal(t, MethodSquash, chooseMethod(all, PrefRepo, MethodSquash), "the repository's first")
	assert.Equal(t, MethodMerge, chooseMethod([]MergeMethod{MethodMerge}, PrefSquash, MethodSquash),
		"a choice the repository doesn't allow falls back")
	assert.Equal(t, MethodSquash, chooseMethod(all, PrefRepo, ""), "squash first when GitHub offers none")
	assert.Equal(t, MethodRebase, chooseMethod([]MergeMethod{MethodRebase}, PrefRepo, ""))
	assert.Empty(t, chooseMethod(nil, PrefRepo, MethodSquash))

	p := Decide(Facts{MergeState: github.MergeStateClean, CanWrite: true}, PrefRepo)
	merge, _ := p.offer(ActionMerge)
	assert.False(t, merge.Enabled)
	assert.Equal(t, "The repository allows no merge method.", merge.Reason)
	assert.Equal(t, []MergeMethod{}, p.Methods)
}

// FuzzDecide checks the rules every plan keeps, whatever the facts: nothing merges what
// GitHub would not merge now, a draft, or a merge queue's; nobody approves or requests
// changes on their own pull request; a pending review blocks every review; the primary is
// an enabled offer, or none only while every enabled offer is one the button never runs on
// its own; an offer with warnings asks first; and a close is offered only to whoever may
// close the pull request, while it is open, asks first, and is never the button.
func FuzzDecide(f *testing.F) {
	f.Add(uint16(0), uint8(0), uint8(0), uint8(0))
	f.Add(uint16(0xffff), uint8(1), uint8(2), uint8(3))

	states := []string{
		github.MergeStateClean, github.MergeStateHasHooks, github.MergeStateUnstable, github.MergeStateBlocked,
		github.MergeStateBehind, github.MergeStateDirty, github.MergeStateUnknown, "",
	}

	f.Fuzz(func(t *testing.T, bits uint16, state, failing, pending uint8) {
		fa := Facts{
			Mine: bits&1 != 0, Draft: bits&2 != 0, PendingReview: bits&4 != 0, MergeQueue: bits&8 != 0,
			CanWrite: bits&16 != 0, CanUpdateBranch: bits&32 != 0, CanAutoMerge: bits&64 != 0, AutoMergeOn: bits&128 != 0,
			Dependabot: bits&256 != 0, ApprovedAtHead: bits&512 != 0, ReviewRequired: bits&1024 != 0,
			ChangesRequested: bits&2048 != 0, Closed: bits&4096 != 0, CanBypass: bits&8192 != 0,
			CanClose:        bits&16384 != 0,
			MergeState:      states[int(state)%len(states)],
			RequiredFailing: int(failing % 3), Failing: int(failing % 5), RequiredPending: int(pending % 3),
			Pending: int(pending % 5), Methods: []MergeMethod{MethodSquash},
		}

		checkPlan(t, fa, Decide(fa, PrefRepo))
	})
}

// checkPlan checks the rules every plan keeps.
func checkPlan(t *testing.T, fa Facts, p Plan) {
	t.Helper()

	on := enabled(p)
	checkMerges(t, fa, on)

	if fa.Mine {
		for _, a := range []Action{ActionApprove, ActionApproveMerge, ActionApproveAutoMerge, ActionRequestChanges} {
			assert.NotContains(t, on, a, "approving your own pull request")
		}
	}

	if fa.PendingReview {
		for _, a := range on {
			assert.False(t, a.reviews(), "a review while one is pending: %s", a)
		}
	}

	checkPrimary(t, p, on)
	checkAsksFirst(t, p)
	checkKeepingUp(t, fa, p, on)
	checkClose(t, fa, p)
}

// checkClose checks that a close is offered while the pull request is open, held back
// until the sheet read it and then only to a user GitHub lets close it, that it runs
// exactly then, asks first and posts the text as a comment first, and that it is never the
// button.
func checkClose(t *testing.T, fa Facts, p Plan) {
	t.Helper()

	assert.NotEqual(t, ActionClose, p.Primary, "a close is never the button")

	read := fa.MergeState != ""
	o, ok := p.offer(ActionClose)
	assert.Equal(t, !fa.Closed && (!read || fa.CanClose), ok, "offered while open, once read only to whoever may close it")

	if ok {
		assert.Equal(t, read && fa.CanClose, o.Enabled, "it runs once read")
		assert.Equal(t, o.Enabled, o.Reason == "", "a reason exactly when it can't run")
		assert.True(t, o.Confirm && o.CommentsFirst && !o.Reviews, "it asks first, and comments first")
		assert.Equal(t, fa.PendingReview, o.CommentBlocked != "", "a pending review holds back only its comment")
	}
}

// checkPrimary checks that the primary is an enabled offer, or none, and that with none,
// only actions the button never runs on its own are enabled: the sheet's button then only
// opens the menu.
func checkPrimary(t *testing.T, p Plan, on []Action) {
	t.Helper()

	assert.NotEqual(t, ActionMergeBypass, p.Primary, "going past the rules is never the button")

	if p.Primary != ActionNone {
		assert.Contains(t, on, p.Primary, "the primary is enabled")

		return
	}

	// The writes the button never runs unless the user picks them: a merge past the rules
	// and a close, which only ever run on purpose, and the two that write to the branch,
	// which are the button only while the pull request is behind (or, for the rebase, has
	// conflicts).
	neverDefault := []Action{ActionMergeBypass, ActionClose, ActionRebaseBot, ActionUpdateBranch}

	for _, a := range on {
		assert.Contains(t, neverDefault, a, "with no primary, %s is enabled", a)
	}
}

// checkAsksFirst checks that every offer with warnings to accept asks first, as Update
// branch on Dependabot's pull request does: Act refuses it unless they were accepted.
func checkAsksFirst(t *testing.T, p Plan) {
	t.Helper()

	for _, o := range p.Offers {
		assert.True(t, len(o.Warnings) == 0 || o.Confirm, "an action with warnings to accept asks first: %s", o.Action)
	}
}

// checkKeepingUp checks the two ways of bringing a branch up to date: asking Dependabot to
// rebase (checkRebase), and Update branch, offered whenever GitHub lets the user; and that
// each is the button only while the pull request is behind (the rebase also while it has
// conflicts).
func checkKeepingUp(t *testing.T, fa Facts, p Plan, on []Action) {
	t.Helper()

	if fa.Closed {
		assert.Empty(t, p.Offers, "a pull request no longer open offers nothing")

		return
	}

	checkRebase(t, fa, p)

	if fa.CanUpdateBranch {
		assert.Contains(t, on, ActionUpdateBranch, "GitHub lets the user update the branch")
	}

	behind := fa.MergeState == github.MergeStateBehind

	if p.Primary == ActionRebaseBot {
		assert.True(t, behind || fa.MergeState == github.MergeStateDirty,
			"the rebase is the button only while it is behind or has conflicts")
	}

	if p.Primary == ActionUpdateBranch {
		assert.True(t, behind, "Update branch is the button only while it is behind")
		assert.NotContains(t, on, ActionRebaseBot, "Dependabot's own rebase comes first")
	}
}

// checkRebase checks that asking Dependabot to rebase is offered on every open pull request
// of its, and only there, and runs only once the sheet read the pull request, for a user
// with push access, while the branch is behind its base or has conflicts with it.
func checkRebase(t *testing.T, fa Facts, p Plan) {
	t.Helper()

	rebase, ok := p.offer(ActionRebaseBot)
	if !fa.Dependabot {
		assert.False(t, ok, "asking Dependabot to rebase a pull request it didn't open")

		return
	}

	require.True(t, ok, "Dependabot's pull request, in any merge state")

	behind := fa.MergeState == github.MergeStateBehind || fa.MergeState == github.MergeStateDirty || fa.CanUpdateBranch
	assert.Equal(t, fa.MergeState != "" && fa.CanWrite && behind, rebase.Enabled, rebase.Reason)
	assert.Equal(t, rebase.Enabled, rebase.Reason == "", "a reason exactly when it can't run")
}

// checkMerges checks that nothing merges what GitHub would not merge now, a draft, or a
// merge queue's.
func checkMerges(t *testing.T, fa Facts, on []Action) {
	t.Helper()

	mergesNow := github.Mergeable(fa.MergeState) || fa.MergeState == github.MergeStateUnstable

	if slices.Contains(on, ActionMerge) {
		assert.True(t, mergesNow && !fa.Draft && !fa.MergeQueue && fa.CanWrite, "a merge GitHub would not make now")
	}

	if slices.Contains(on, ActionApproveMerge) {
		assert.False(t, fa.Draft || fa.MergeQueue || fa.Mine)
		assert.NotContains(t, []string{github.MergeStateBehind, github.MergeStateDirty, github.MergeStateUnknown, ""},
			fa.MergeState)
	}

	checkBypass(t, fa, on)
}

// checkBypass checks that a merge past the rules is offered only when GitHub lets the user,
// and only past what the rules hold back.
func checkBypass(t *testing.T, fa Facts, on []Action) {
	t.Helper()

	if !slices.Contains(on, ActionMergeBypass) {
		return
	}

	held := fa.MergeState == github.MergeStateBlocked || fa.MergeState == github.MergeStateBehind
	assert.True(t, fa.CanBypass && held && !fa.Draft && !fa.MergeQueue && fa.CanWrite,
		"past the rules only when GitHub lets the user, and only past what they hold back")
}

func TestDecideOffersAMergePastTheRules(t *testing.T) {
	t.Parallel()

	// The user's own pull request, held back only for an approval nobody gave.
	f := facts(github.MergeStateBlocked)
	f.Mine, f.ReviewRequired, f.CanBypass = true, true, true

	p := Decide(f, PrefRepo)

	past, ok := p.offer(ActionMergeBypass)
	require.True(t, ok)
	assert.True(t, past.Enabled)
	assert.True(t, past.Confirm)
	assert.Equal(t, "Squash-merge past the rules", past.Label)
	assert.Equal(t, []string{"It needs an approval from someone else."}, past.Warnings)
	assert.Equal(t, ActionAutoMerge, p.Primary, "never the button, though it is the only way to merge now")
	assert.Equal(t, "An approval from someone else · auto-merge available", p.Readiness.Sentence)

	auto, _ := p.offer(ActionAutoMerge)
	assert.Equal(t, []string{
		"It waits for an approval from someone else: GitHub doesn't let you approve your own pull request.",
	}, auto.Warnings)

	// Not offered when GitHub doesn't let the user, nor when the rules hold nothing back.
	f.CanBypass = false
	_, ok = Decide(f, PrefRepo).offer(ActionMergeBypass)
	assert.False(t, ok)

	for _, status := range []string{
		github.MergeStateClean, github.MergeStateUnstable, github.MergeStateDirty, github.MergeStateUnknown,
	} {
		g := facts(status)
		g.CanBypass = true
		_, ok = Decide(g, PrefRepo).offer(ActionMergeBypass)
		assert.False(t, ok, status)
	}

	// A draft is held back however the rules stand.
	f.CanBypass, f.Draft = true, true
	past, _ = Decide(f, PrefRepo).offer(ActionMergeBypass)
	assert.False(t, past.Enabled)
	assert.Equal(t, "It's a draft.", past.Reason)
}

func TestDecideNamesEachRuleAMergePastThemSkips(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name string
		edit func(f *Facts)
		want []string
	}{
		{"nothing named", func(*Facts) {}, []string{"Branch protection blocks it."}},
		{"another's approval", func(f *Facts) { f.ReviewRequired = true }, []string{"It needs an approval."}},
		{
			"changes requested", func(f *Facts) { f.ChangesRequested, f.ReviewRequired = true, true },
			[]string{"Changes were requested, and the request stands."},
		},
		{
			"checks", func(f *Facts) { f.RequiredFailing, f.RequiredPending = 2, 1 },
			[]string{"2 required checks are failing.", "Required checks are still running."},
		},
		{"one check", func(f *Facts) { f.RequiredFailing = 1 }, []string{"1 required check is failing."}},
		{
			"behind", func(f *Facts) { f.MergeState = github.MergeStateBehind },
			[]string{"It's behind main, and the rules want it up to date."},
		},
		{
			"sensitive files", func(f *Facts) { f.Sensitive = []string{"Workflows"} },
			[]string{"Branch protection blocks it.", "It changes sensitive files: Workflows."},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			f := facts(github.MergeStateBlocked)
			f.CanBypass = true
			c.edit(&f)

			past, ok := Decide(f, PrefRepo).offer(ActionMergeBypass)
			require.True(t, ok)
			assert.Equal(t, c.want, past.Warnings)
		})
	}
}

func TestEachOfferSaysWhatItsTextGoesWith(t *testing.T) {
	t.Parallel()

	f := facts(github.MergeStateBlocked)
	f.Mine, f.CanBypass, f.ReviewRequired, f.CanClose = true, true, true, true

	commentsFirst := []Action{ActionMerge, ActionMergeBypass, ActionAutoMerge, ActionClose}
	reviews := []Action{ActionApproveMerge, ActionApproveAutoMerge, ActionApprove, ActionRequestChanges, ActionComment}
	plan := Decide(f, PrefRepo)

	for _, a := range commentsFirst {
		_, ok := plan.offer(a)
		require.True(t, ok, "the user's own pull request offers %q", a)
	}

	for _, o := range plan.Offers {
		assert.Equal(t, slices.Contains(reviews, o.Action), o.Reviews, o.Action)
		assert.Equal(t, slices.Contains(commentsFirst, o.Action), o.CommentsFirst, o.Action)
		assert.Empty(t, o.CommentBlocked, "GitHub takes a comment on one's own pull request: %q", o.Action)
	}

	f.PendingReview = true

	for _, o := range Decide(f, PrefRepo).Offers {
		if !o.CommentsFirst {
			assert.Empty(t, o.CommentBlocked, o.Action)

			continue
		}

		without := "merge"
		if o.Action == ActionClose {
			without = "close it"
		}

		assert.Equal(t, "You have a review of this pull request started on GitHub. Submit or discard it there first. "+
			"Or clear your text and line comments to "+without+" without a comment.", o.CommentBlocked, o.Action)
	}
}

// dependabot are Dependabot's pull request in merge state status.
func dependabot(status string) Facts {
	f := facts(status)
	f.Dependabot = true

	return f
}

// behindBot is Dependabot's pull request in merge state status, behind its base, which the
// user may update.
func behindBot(status string) Facts {
	f := dependabot(status)
	f.CanUpdateBranch = true

	return f
}

func TestDecideAsksDependabotToRebaseInAnyMergeState(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name    string
		facts   Facts
		primary Action
	}{
		{"behind", dependabot(github.MergeStateBehind), ActionRebaseBot},
		{"with conflicts", dependabot(github.MergeStateDirty), ActionRebaseBot},
		{"clean", behindBot(github.MergeStateClean), ActionApproveMerge},
		{"an optional check failing", func() Facts {
			f := behindBot(github.MergeStateUnstable)
			f.Failing = 1

			return f
		}(), ActionApproveMerge},
		{"a required check failing", func() Facts {
			f := behindBot(github.MergeStateBlocked)
			f.RequiredFailing, f.Failing = 1, 1

			return f
		}(), ActionComment},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			p := Decide(c.facts, PrefRepo)

			rebase, ok := p.offer(ActionRebaseBot)
			require.True(t, ok)
			assert.True(t, rebase.Enabled)
			assert.Equal(t, "Ask Dependabot to rebase", rebase.Label)
			assert.False(t, rebase.Confirm, "one click")
			assert.Empty(t, rebase.Warnings)
			assert.Equal(t, c.primary, p.Primary, "the button only while it is behind or has conflicts")
		})
	}

	// Never anyone else's pull request.
	_, ok := Decide(facts(github.MergeStateBehind), PrefRepo).offer(ActionRebaseBot)
	assert.False(t, ok)
}

func TestDecideOffersTheRebaseOnlyWhenItCanDoSomething(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name, reason string
		facts        Facts
	}{
		{"before the sheet read it", "Wait for it to load from GitHub.", dependabot("")},
		{
			"without push access", "Dependabot only takes commands from people with push access to the repository.",
			func() Facts {
				f := dependabot(github.MergeStateBehind)
				f.CanWrite = false

				return f
			}(),
		},
		{"up to date and blocked", "It's up to date with main, so there's nothing to rebase.", dependabot(github.MergeStateBlocked)},
		{"up to date and clean", "It's up to date with main, so there's nothing to rebase.", dependabot(github.MergeStateClean)},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			p := Decide(c.facts, PrefRepo)

			rebase, ok := p.offer(ActionRebaseBot)
			require.True(t, ok, "in the menu, with why it can't run")
			assert.False(t, rebase.Enabled)
			assert.Equal(t, c.reason, rebase.Reason)
			assert.NotEqual(t, ActionRebaseBot, p.Primary)
		})
	}
}

func TestDecideOffersUpdateBranchWheneverGitHubLets(t *testing.T) {
	t.Parallel()

	// Behind its base, which the rules don't mind: a required check failing holds it back.
	f := facts(github.MergeStateBlocked)
	f.CanUpdateBranch, f.RequiredFailing, f.Failing = true, 1, 1

	p := Decide(f, PrefRepo)

	update, ok := p.offer(ActionUpdateBranch)
	require.True(t, ok)
	assert.True(t, update.Enabled)
	assert.Equal(t, "Update branch", update.Label)
	assert.False(t, update.Confirm, "one click")
	assert.Empty(t, update.Warnings)
	assert.Equal(t, ActionComment, p.Primary, "in the menu, not the button")

	// The user's own, which merges now: the button merges.
	f = facts(github.MergeStateClean)
	f.Mine, f.CanUpdateBranch = true, true

	p = Decide(f, PrefRepo)
	assert.Contains(t, enabled(p), ActionUpdateBranch)
	assert.Equal(t, ActionMerge, p.Primary)

	// Up to date, or GitHub doesn't let the user: not offered, but for a branch the rules
	// hold back for being behind, which says why.
	_, ok = Decide(facts(github.MergeStateBlocked), PrefRepo).offer(ActionUpdateBranch)
	assert.False(t, ok)
}

func TestDecideOffersBothOnDependabotsBranchAndWarnsBeforeTheUpdate(t *testing.T) {
	t.Parallel()

	for _, status := range []string{github.MergeStateBlocked, github.MergeStateClean, github.MergeStateBehind} {
		t.Run(status, func(t *testing.T) {
			t.Parallel()

			f := dependabot(status)
			f.CanUpdateBranch = true

			p := Decide(f, PrefRepo)

			rebase := slices.IndexFunc(p.Offers, func(o Offer) bool { return o.Action == ActionRebaseBot })
			update := slices.IndexFunc(p.Offers, func(o Offer) bool { return o.Action == ActionUpdateBranch })

			require.GreaterOrEqual(t, rebase, 0)
			require.Greater(t, update, rebase, "the menu lists the rebase first")

			o := p.Offers[update]
			assert.True(t, o.Enabled)
			assert.True(t, o.Confirm, "it asks first")
			assert.Equal(t, []string{
				"Dependabot stops rebasing a pull request once someone else changes its branch. " +
					"Ask Dependabot to rebase instead to keep it managed.",
			}, o.Warnings)
			assert.NotEqual(t, ActionUpdateBranch, p.Primary)
		})
	}

	// Behind, with GitHub not letting the user update it: only the rebase.
	f := dependabot(github.MergeStateBehind)
	f.CanUpdateBranch = false

	_, ok := Decide(f, PrefRepo).offer(ActionUpdateBranch)
	assert.False(t, ok)
}

func TestDecideOffersNeitherOnceMergedOrClosed(t *testing.T) {
	t.Parallel()

	for _, status := range []string{github.MergeStateBehind, github.MergeStateDirty, github.MergeStateBlocked} {
		f := dependabot(status)
		f.CanUpdateBranch, f.Closed = true, true

		p := Decide(f, PrefRepo)
		assert.Empty(t, p.Offers, status)
		assert.Equal(t, ActionNone, p.Primary, status)
		assert.Equal(t, ReadyClosed, p.Readiness.State, status)
	}
}

func TestNeitherIsTheButtonWhenNothingElseCanRun(t *testing.T) {
	t.Parallel()

	// A review of the user's started on GitHub holds back every review, and a required
	// check failing every merge: only the two that write to the branch can run, and the
	// sheet's button only opens the menu that holds them.
	f := dependabot(github.MergeStateBlocked)
	f.PendingReview, f.RequiredFailing, f.Failing, f.CanAutoMerge, f.CanUpdateBranch = true, 1, 1, false, true

	p := Decide(f, PrefRepo)
	assert.ElementsMatch(t, []Action{ActionRebaseBot, ActionUpdateBranch}, enabled(p))
	assert.Equal(t, ActionNone, p.Primary)

	f.Dependabot = false

	p = Decide(f, PrefRepo)
	assert.Equal(t, []Action{ActionUpdateBranch}, enabled(p))
	assert.Equal(t, ActionNone, p.Primary)

	// Behind, which the rules mind: the rebase is the button again.
	f = dependabot(github.MergeStateBehind)
	f.PendingReview = true

	assert.Equal(t, ActionRebaseBot, Decide(f, PrefRepo).Primary)
}
