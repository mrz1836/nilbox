package review

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
)

// facts are a pull request someone else opened, which the user may merge by squash or
// merge commit, with auto-merge allowed, in merge state status.
func facts(status string) Facts {
	return Facts{
		MergeState: status, CanWrite: true, CanUpdateBranch: true, CanAutoMerge: true, Base: "main",
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
			enabled: []Action{ActionApprove, ActionRequestChanges, ActionComment, ActionRebaseBot},
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
		})
	}
}

func TestDecideSaysWhatKeepsAMergeFromPassing(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		failing, required int
		sentence, warning string
	}{
		{0, 0, "Ready to merge · optional checks not passing", "Optional checks aren't passing."},
		{1, 0, "Ready to merge · 1 optional check failing", "1 optional check is failing."},
		{3, 1, "Ready to merge · 2 optional checks failing", "2 optional checks are failing."},
	} {
		f := facts(github.MergeStateUnstable)
		f.Failing, f.RequiredFailing = c.failing, c.required

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
// changes on their own pull request; a pending review blocks every review; and the
// primary is one enabled offer, there whenever any offer is enabled.
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

	if len(on) == 0 {
		assert.Equal(t, ActionNone, p.Primary)
	} else {
		assert.Contains(t, on, p.Primary, "the primary is enabled")
	}

	assert.NotEqual(t, ActionMergeBypass, p.Primary, "going past the rules is never the button")
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
	f.Mine, f.CanBypass, f.ReviewRequired = true, true, true

	commentsFirst := []Action{ActionMerge, ActionMergeBypass, ActionAutoMerge}
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

		assert.Equal(t, "You have a review of this pull request started on GitHub. Submit or discard it there first. "+
			"Or clear your text and line comments to merge without a comment.", o.CommentBlocked, o.Action)
	}
}
