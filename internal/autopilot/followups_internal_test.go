package autopilot

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/trust"
)

// prFacts are a pull request's facts: its author's trust class, and the sensitive
// categories its files touch, read in full unless partial; unread files for nil.
func prFacts(class trust.Class, sensitive []string, partial bool) facts.Facts {
	f := facts.Facts{Values: map[catalog.FieldID]facts.Value{catalog.FieldTrust: facts.Text(string(class))}}
	if sensitive != nil {
		f.Values[catalog.FieldSensitive] = facts.List(sensitive, partial)
	}

	return f
}

func TestTheReviewARoutinePostsIsItsVerdictOnlyWhereApprovingIsAllowed(t *testing.T) {
	t.Parallel()

	verdict := spec.ReviewStep{As: catalog.ReviewAsVerdict, AllowApprove: true}
	approve := AgentReview{Verdict: ReviewApprove, Body: "Looks good."}
	clean := guardOf(prFacts(trust.ClassMember, []string{}, false), true, false)

	for _, c := range []struct {
		name   string
		st     spec.ReviewStep
		rv     AgentReview
		g      approveGuard
		postAs string
		event  ReviewEvent
		why    string
	}{
		{name: "an approval, allowed, on a member's clean pull request", st: verdict, rv: approve, g: clean, event: EventApprove},
		{
			name: "a trusted bot's", st: verdict, rv: approve, event: EventApprove,
			g: guardOf(prFacts(trust.ClassBot, []string{}, false), true, false),
		},
		{
			name: "a request for changes", st: verdict, g: clean, event: EventRequestChanges,
			rv: AgentReview{Verdict: ReviewRequestChanges},
		},
		{name: "a comment", st: verdict, g: clean, rv: AgentReview{Verdict: ReviewComment}, event: EventComment},
		{
			name: "posted as a comment, whatever it says", st: spec.ReviewStep{As: catalog.ReviewAsComment}, g: clean,
			rv: AgentReview{Verdict: ReviewRequestChanges}, event: EventComment,
		},
		{name: "the user chose a comment", st: verdict, rv: approve, g: clean, postAs: PostAsComment, event: EventComment},
		{
			name: "approving not ticked", st: spec.ReviewStep{As: catalog.ReviewAsVerdict}, rv: approve, g: clean,
			event: EventComment, why: "doesn't allow approving",
		},
		{
			name: "an outside contributor's", st: verdict, rv: approve, event: EventComment, why: "a member's or a trusted bot's",
			g: guardOf(prFacts(trust.ClassOutside, []string{}, false), true, false),
		},
		{
			name: "files not checked", st: verdict, rv: approve, event: EventComment, why: "every file was checked",
			g: guardOf(prFacts(trust.ClassMember, nil, false), true, false),
		},
		{
			name: "files checked in part", st: verdict, rv: approve, event: EventComment, why: "every file was checked",
			g: guardOf(prFacts(trust.ClassMember, []string{}, true), true, false),
		},
		{
			name: "a sensitive file", st: verdict, rv: approve, event: EventComment, why: "none is sensitive",
			g: guardOf(prFacts(trust.ClassMember, []string{"workflows"}, false), true, false),
		},
		{
			name: "a pull request a routine pushed to", st: verdict, rv: approve, event: EventComment,
			why: "never approves a pull request a routine pushed to", g: guardOf(prFacts(trust.ClassMember, []string{}, false), true, true),
		},
		{name: "an item not stored", st: verdict, rv: approve, g: guardOf(facts.Facts{}, false, false), event: EventComment},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			event, why := reviewEvent(c.st, c.rv, c.g, c.postAs)
			assert.Equal(t, c.event, event)
			assert.Contains(t, why, c.why)

			if c.event == EventApprove {
				assert.Empty(t, why)
			}
		})
	}
}

func TestAReadOnlyAgentsVerdictIsItsStepsOutcome(t *testing.T) {
	t.Parallel()

	assert.Equal(t, catalog.OutcomeApprove, reviewOutcome(&AgentReview{Verdict: ReviewApprove}))
	assert.Equal(t, catalog.OutcomeRequestChanges, reviewOutcome(&AgentReview{Verdict: ReviewRequestChanges}))
	assert.Empty(t, reviewOutcome(&AgentReview{Verdict: ReviewComment}))
	assert.Empty(t, reviewOutcome(nil))

	sr := &stepRun{Step: spec.Step{Agent: &spec.AgentStep{Fence: &spec.Fence{Tools: catalog.PresetReadOnly}}}}
	ranTo(sr, RunEnd{Outcome: RunDone, Review: &AgentReview{Verdict: ReviewRequestChanges, Body: "Fix it."}})
	assert.Equal(t, string(catalog.OutcomeRequestChanges), sr.Outcome)
	assert.Equal(t, "Fix it.", sr.Review.Body, "kept for the post after it")

	push := &stepRun{Step: spec.Step{Agent: &spec.AgentStep{Fence: &spec.Fence{Tools: catalog.PresetEditTestPush}}}}
	ranTo(push, RunEnd{Outcome: RunDone, Commit: "abc", Review: &AgentReview{Verdict: ReviewApprove}})
	assert.Equal(t, string(catalog.OutcomePushed), push.Outcome, "an agent that may push says whether it did")
	assert.Nil(t, push.Review)
}

func TestMergeItIsPinnedToTheVerifiedCommitAndBringsAutoMergeBackOnlyWhenItWasOn(t *testing.T) {
	t.Parallel()

	m := &match{steps: []stepRun{
		{Step: spec.Step{Agent: &spec.AgentStep{}}},
		{Step: spec.Step{WaitForChecks: &spec.WaitStep{}}, Commit: "fix"},
		{Step: spec.Step{Merge: &spec.MergeStep{How: catalog.MergeAutoIfItWasOn}}},
	}}

	assert.Equal(t, "fix", mergeHead(m, 2), "the commit its checks passed on")
	assert.Empty(t, mergeHead(m, 1), "none before a wait")

	assert.False(t, autoMergeBack(m, catalog.MergeAutoIfItWasOn), "not known at the match: it merges now")

	m.AutoMergeAtMatch = new(true)
	assert.True(t, autoMergeBack(m, catalog.MergeAutoIfItWasOn))
	assert.False(t, autoMergeBack(m, catalog.MergeWhenMergeable), "the step says to merge when GitHub allows")

	m.AutoMergeAtMatch = new(false)
	assert.False(t, autoMergeBack(m, catalog.MergeAutoIfItWasOn))

	assert.Equal(t, "Merges it (squash).", mergeWords(false, catalog.MergeSquash))
	assert.Equal(t, "Merges it.", mergeWords(false, catalog.MergeDefault))
	assert.Equal(t, "Turns auto-merge back on, as it was.", mergeWords(true, catalog.MergeSquash))
}

func TestMergeItNeverMergesAnOutsideContributorsPullRequest(t *testing.T) {
	t.Parallel()

	merge := &stepRun{Step: spec.Step{Merge: &spec.MergeStep{How: catalog.MergeWhenMergeable}}}
	p := &pass{facts: map[int64]facts.Facts{
		1: prFacts(trust.ClassOutside, []string{}, false), 2: prFacts(trust.ClassMember, []string{}, false),
	}}

	v := p.mergeRules(1, merge)
	assert.Equal(t, StateClosed, v.closeAs)
	assert.Equal(t, ReasonNotOffered, v.reason)
	assert.Contains(t, v.words, "outside contributor")

	assert.True(t, p.mergeRules(2, merge).through())
	assert.True(t, p.mergeRules(1, &stepRun{Step: spec.Step{Notify: &spec.NotifyStep{}}}).through(), "Merge it's alone")
}

func TestTheStepsThatFollowARunAreGitHubWrites(t *testing.T) {
	t.Parallel()

	for _, k := range []catalog.StepKind{catalog.StepGitHub, catalog.StepMerge, catalog.StepPostReview} {
		info, _ := catalog.StepByKind(k)
		assert.True(t, writesGitHub(info), "%s is paced and capped as a GitHub action", k)
	}

	for _, k := range []catalog.StepKind{catalog.StepMarkDone, catalog.StepAgent, catalog.StepNotify} {
		info, _ := catalog.StepByKind(k)
		assert.False(t, writesGitHub(info), k)
	}

	assert.True(t, followsRun(spec.Step{Merge: &spec.MergeStep{}}))
	assert.True(t, followsRun(spec.Step{PostReview: &spec.ReviewStep{}}))
	assert.False(t, followsRun(spec.Step{WaitForChecks: &spec.WaitStep{}}))
}
