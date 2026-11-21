package autopilot

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/eval"
	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
)

// syncHead is the commit a sync pull request's checks passed on.
const syncHead = "5c4557f4c0df7df7ec81ddc365e68cc6e79208ca"

// pastSpec is a routine that replies, then merges past the rules where an approval is all
// a pull request lacks, sensitive files included or not, as stored text.
func pastSpec(t *testing.T, sensitive bool) (spec.Spec, string) {
	t.Helper()

	sp := spec.Spec{
		Version: spec.Version,
		Watch:   spec.Watch{Sources: []spec.Source{{Lists: []catalog.ListSource{catalog.SourceAuthored}}}},
		Trigger: spec.Trigger{Event: catalog.EventChecksPassed},
		Steps: []spec.Step{
			{GitHub: []spec.GitHubAction{{Reply: &spec.ReplyAction{Prompt: spec.PromptRef{Nickname: "Sync OK"}}}}},
			{Merge: &spec.MergeStep{
				How: catalog.MergeWhenMergeable, Method: catalog.MergeSquash, AllowBypass: true, AllowSensitive: sensitive,
			}},
		},
	}

	data, err := spec.Encode(spec.Normalize(sp))
	require.NoError(t, err)

	return sp, string(data)
}

// syncMatch is a's routine's match on item 7 at syncHead, fired when its checks passed,
// its reply sent, at its merge.
func syncMatch(sp spec.Spec, hash string) *match {
	m := &match{steps: stepsOf(sp)}
	m.ItemID, m.HeadOID, m.SpecHash = new(int64(7)), syncHead, hash
	m.FireKey = eval.CommitKey(catalog.EventChecksPassed, syncHead)
	m.Step, m.steps[0].Status = 1, stepDone

	return m
}

// acceptedRun is the routine run of sp's stored text, accepted as one that merges past the
// rules.
func acceptedRun(data string) *routineRun {
	return &routineRun{
		row:  queries.AutopilotRoutine{ID: 3, Spec: data, PastRulesHash: pastRulesHash(data)},
		hash: specHash(data), decided: map[int64]eval.Decision{},
	}
}

func TestAPastRulesHashIsTheWholeSHAOfTheSpecText(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", pastRulesHash(""))
	assert.Len(t, pastRulesHash(`{"version":1}`), 64)
	assert.NotEqual(t, pastRulesHash(`{"version":1}`), pastRulesHash(`{"version":1} `))
}

func TestARoutineStandsOnMergingPastTheRulesAsItsUserAcceptedIt(t *testing.T) {
	t.Parallel()

	sp, data := pastSpec(t, false)

	assert.Equal(t, PastRulesAllowed, pastRulesOf(queries.AutopilotRoutine{Spec: data, PastRulesHash: pastRulesHash(data)}, sp))
	assert.Equal(t, PastRulesNeedsTick, pastRulesOf(queries.AutopilotRoutine{Spec: data}, sp), "never accepted")
	assert.Equal(t, PastRulesNeedsTick, pastRulesOf(queries.AutopilotRoutine{
		Spec: data, PastRulesHash: pastRulesHash(data + " "),
	}, sp), "accepted as another text")

	plain := sp
	plain.Steps = []spec.Step{{Merge: &spec.MergeStep{How: catalog.MergeWhenMergeable}}}
	assert.Equal(t, PastRulesNone, pastRulesOf(queries.AutopilotRoutine{Spec: data, PastRulesHash: pastRulesHash(data)}, plain))
}

func TestSaveStoresTheAcceptanceOfAMergePastTheRules(t *testing.T) {
	t.Parallel()

	sp, data := pastSpec(t, true)
	hash := pastRulesHash(data)

	got, err := pastRulesToWrite(sp, data, "", true)
	require.NoError(t, err)
	assert.Equal(t, hash, got, "accepted now")

	got, err = pastRulesToWrite(sp, data, hash, false)
	require.NoError(t, err)
	assert.Equal(t, hash, got, "the very text accepted before, as when only its name changed")

	_, err = pastRulesToWrite(sp, data, pastRulesHash("another spec"), false)
	require.Error(t, err, "a spec accepted as another text")

	var problems *ProblemsError
	require.ErrorAs(t, err, &problems)
	assert.Equal(t, "steps[1].merge.allowBypass", problems.err.Problems[0].Path())

	_, err = pastRulesToWrite(sp, data, "", false)
	require.Error(t, err, "never accepted")

	plain := sp
	plain.Steps = []spec.Step{{Merge: &spec.MergeStep{How: catalog.MergeWhenMergeable}}}

	got, err = pastRulesToWrite(plain, data, hash, true)
	require.NoError(t, err)
	assert.Empty(t, got, "a spec with no tick keeps no acceptance")
}

func TestAnAgentlessMergeMergesTheCommitItsChecksPassedOn(t *testing.T) {
	t.Parallel()

	sp, data := pastSpec(t, false)
	m := syncMatch(sp, specHash(data))

	assert.Equal(t, syncHead, agentlessHead(m))
	assert.Equal(t, syncHead, mergeHead(m, 1), "with no wait, the commit the trigger fired on")

	manual := syncMatch(sp, specHash(data))
	manual.FireKey, manual.Manual = "manual:abc", true
	assert.Empty(t, agentlessHead(manual), "a click's run has no commit its checks passed on")
	assert.Empty(t, mergeHead(manual, 1))

	day := syncMatch(sp, specHash(data))
	day.FireKey = "checks_passed:day:2026-11-21"
	assert.Empty(t, agentlessHead(day), "a fire key that isn't the commit's")

	agent := syncMatch(sp, specHash(data))
	agent.steps = append([]stepRun{{Step: spec.Step{Agent: &spec.AgentStep{}}}}, agent.steps...)
	assert.Empty(t, agentlessHead(agent), "an agent's routine merges what the agent pushed")

	noHead := syncMatch(sp, specHash(data))
	noHead.HeadOID = ""
	assert.Empty(t, agentlessHead(noHead))
}

func TestARoutineMergesPastTheRulesOnlyWithEveryConsent(t *testing.T) {
	t.Parallel()

	sp, data := pastSpec(t, true)
	on := autopilotprefs.Defaults()
	on.MergePastRules = true

	tests := []struct {
		name      string
		edit      func(p *pass, r *routineRun, m *match, sr *stepRun)
		past      bool
		sensitive bool
		why       string
	}{
		{name: "every consent stands", past: true, sensitive: true},
		{
			name: "sensitive files not allowed", past: true,
			edit: func(_ *pass, _ *routineRun, _ *match, sr *stepRun) { sr.Step.Merge.AllowSensitive = false },
		},
		{
			name: "the step doesn't allow it",
			edit: func(_ *pass, _ *routineRun, _ *match, sr *stepRun) {
				sr.Step.Merge.AllowBypass, sr.Step.Merge.AllowSensitive = false, false
			},
		},
		{
			name: "only sensitive files ticked", why: "doesn't allow merging past the rules",
			edit: func(_ *pass, _ *routineRun, _ *match, sr *stepRun) { sr.Step.Merge.AllowBypass = false },
		},
		{
			name: "the switch is off", why: "is off in Autopilot's settings",
			edit: func(p *pass, _ *routineRun, _ *match, _ *stepRun) { p.cfg.MergePastRules = false },
		},
		{
			name: "never accepted", why: "haven't confirmed",
			edit: func(_ *pass, r *routineRun, _ *match, _ *stepRun) { r.row.PastRulesHash = "" },
		},
		{
			name: "accepted as another text", why: "haven't confirmed",
			edit: func(_ *pass, r *routineRun, _ *match, _ *stepRun) { r.row.Spec += " " },
		},
		{
			name: "the routine changed since it matched", why: "changed since it matched",
			edit: func(_ *pass, _ *routineRun, m *match, _ *stepRun) { m.SpecHash = "older" },
		},
		{
			name: "a routine pushed to it", why: "pushed to it",
			edit: func(p *pass, _ *routineRun, _ *match, _ *stepRun) {
				p.runs = map[int64][]itemRun{7: {{RunCommit: "abc"}}}
			},
		},
		{
			name: "a click's run", why: "by hand",
			edit: func(_ *pass, _ *routineRun, m *match, _ *stepRun) { m.Manual, m.FireKey = true, "manual:abc" },
		},
		{
			name: "no commit its checks passed on", why: "only the commit its checks passed on",
			edit: func(_ *pass, _ *routineRun, m *match, _ *stepRun) { m.FireKey = "checks_passed:item" },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := &pass{cfg: on}
			r := acceptedRun(data)
			m := syncMatch(sp, r.hash)
			step := *m.steps[1].Step.Merge
			m.steps[1].Step.Merge = &step
			sr := &m.steps[1]

			if tt.edit != nil {
				tt.edit(p, r, m, sr)
			}

			past, sensitive, why := p.pastReview(r, m, sr)
			assert.Equal(t, tt.past, past)
			assert.Equal(t, tt.sensitive, sensitive)
			assert.Contains(t, why, tt.why)

			if tt.past {
				assert.Empty(t, why)
			}
		})
	}
}

func TestAMergeThatDoesntGoPastTheRulesSaysNothingOfThem(t *testing.T) {
	t.Parallel()

	p := &pass{}
	m := &match{steps: []stepRun{{Step: spec.Step{Merge: &spec.MergeStep{How: catalog.MergeWhenMergeable}}}}}

	past, sensitive, why := p.pastReview(nil, m, &m.steps[0])
	assert.False(t, past)
	assert.False(t, sensitive)
	assert.Empty(t, why)
}

func TestAnAgentlessMergeIsDecidedAgainRightBeforeItStarts(t *testing.T) {
	t.Parallel()

	sp, data := pastSpec(t, false)
	key := eval.CommitKey(catalog.EventChecksPassed, syncHead)
	yes := eval.Trace{Path: "trigger", Result: eval.Yes, Actual: "every check passed on 5c4557f"}

	tests := []struct {
		name     string
		decision *eval.Decision
		closeAs  State
		reason   Reason
		block    Blocker
		words    string
	}{
		{name: "it still matches on the same commit", decision: &eval.Decision{Match: eval.Yes, FireKey: key, Trace: []eval.Trace{yes}}},
		{
			name: "pushed since", closeAs: StateClosed, reason: ReasonMovedOn, words: "It moved on",
			decision: &eval.Decision{Match: eval.Yes, FireKey: "checks_passed:commit:abc", Trace: []eval.Trace{yes}},
		},
		{
			name: "its label taken off", closeAs: StateClosed, reason: ReasonNoLongerMatches, words: "the labels don't include",
			decision: &eval.Decision{Match: eval.No, Trace: []eval.Trace{yes, {
				Path: "conditions.of[1]", Result: eval.No, Actual: "the labels don't include automerge",
			}}},
		},
		{
			name: "its checks running again", block: BlockSettling, words: "trigger to hold again",
			decision: &eval.Decision{Match: eval.No, Trace: []eval.Trace{{
				Path: "trigger", Result: eval.No, Actual: "1 check is still running",
			}}},
		},
		{
			name: "a condition not known yet", block: BlockSettling,
			decision: &eval.Decision{Match: eval.Unknown, Trace: []eval.Trace{yes, {Result: eval.Unknown}}},
		},
		{name: "not decided this cycle", block: BlockSettling, words: "decide it again"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := &pass{}
			r := acceptedRun(data)
			m := syncMatch(sp, r.hash)

			if tt.decision != nil {
				r.decided[7] = *tt.decision
			}

			v := p.recheck(r, m, &m.steps[1])
			assert.Equal(t, tt.closeAs, v.closeAs)
			assert.Equal(t, tt.reason, v.reason)
			assert.Equal(t, tt.block, v.block)
			assert.Contains(t, v.words, tt.words)
		})
	}

	t.Run("only an agentless merge", func(t *testing.T) {
		t.Parallel()

		p := &pass{}
		r := acceptedRun(data)
		m := syncMatch(sp, r.hash)

		assert.True(t, p.recheck(r, m, &m.steps[0]).through(), "the reply isn't a merge")

		m.steps = append([]stepRun{{Step: spec.Step{Agent: &spec.AgentStep{}}}}, m.steps...)
		assert.True(t, p.recheck(r, m, &m.steps[2]).through(), "an agent's merge follows its own checks")

		manual := syncMatch(sp, r.hash)
		manual.Manual = true
		assert.True(t, p.recheck(r, manual, &manual.steps[1]).through(), "a click's run merges nothing past the rules")
	})
}

func TestAnAskFirstRoutineAsksBeforeAMergePastTheRules(t *testing.T) {
	t.Parallel()

	past := spec.Step{Merge: &spec.MergeStep{How: catalog.MergeWhenMergeable, AllowBypass: true}}
	assert.True(t, strings.HasSuffix(followWords(past), "past the rules if an approval from someone else is all it lacks."))
	assert.Equal(t, "Its checks passed: waiting for your OK to merge it.",
		followWords(spec.Step{Merge: &spec.MergeStep{How: catalog.MergeWhenMergeable}}))

	sp, data := pastSpec(t, false)
	r := acceptedRun(data)
	r.mode = autopilotprefs.ModeAsk

	m := syncMatch(sp, r.hash)
	m.Manual = true

	v := (&pass{}).askFirst(r, m, &m.steps[1])
	assert.Equal(t, BlockAsk, v.block, "a click's run still asks before it merges")
}

func TestAPastRulesRefusalIsTheBuildersProblem(t *testing.T) {
	t.Parallel()

	sp, data := pastSpec(t, false)

	_, err := pastRulesToWrite(sp, data, "", false)

	var problems *ProblemsError
	require.ErrorAs(t, err, &problems)
	assert.Equal(t, map[string][]spec.Problem{"problems": problems.err.Problems}, problems.Detail())
	assert.Equal(t, pastRulesUnaccepted, problems.err.Problems[0].Message)
}
