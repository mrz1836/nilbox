package draft_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/draft"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilot/watch"
	"github.com/mrz1836/nilbox/internal/item"
)

// agentStep is an agent step that runs the prompt ref names.
func agentStep(ref spec.PromptRef) spec.Step {
	return spec.Step{Agent: &spec.AgentStep{Prompt: ref}}
}

// replyStep is a GitHub step that comments the reply ref names.
func replyStep(ref spec.PromptRef) spec.Step {
	return spec.Step{GitHub: []spec.GitHubAction{{Reply: &spec.ReplyAction{Prompt: ref}}}}
}

func TestAStepsPromptsAreCheckedAgainstTheLibrary(t *testing.T) {
	t.Parallel()

	prs, issues := []item.Kind{item.KindPR}, []item.Kind{item.KindIssue}

	for _, c := range []struct {
		name  string
		step  spec.Step
		kinds []item.Kind
		field string
		want  string
	}{
		{
			name: "a starter the library lacks", step: agentStep(spec.PromptRef{Starter: "quick-look"}), kinds: prs,
			field: "[0].agent.prompt", want: "that starter prompt isn't in your library any more: pick another prompt",
		},
		{
			name: "a nickname no prompt has", step: agentStep(spec.PromptRef{Nickname: "Gone"}), kinds: prs,
			field: "[0].agent.prompt", want: `no prompt in your library has the nickname "Gone": pick one that does`,
		},
		{
			name: "a reply where an agent's prompt goes", step: agentStep(spec.PromptRef{Nickname: "lgtm"}), kinds: prs,
			field: "[0].agent.prompt", want: `"LGTM" is a review reply: an agent runs a prompt`,
		},
		{
			name: "an agent's prompt where a reply goes", step: replyStep(spec.PromptRef{Nickname: "Triage"}), kinds: prs,
			field: "[0].github[0].reply.prompt", want: `"Triage" is a prompt for an agent: a GitHub step comments a reply`,
		},
		{
			name: "a reply that approves", step: replyStep(spec.PromptRef{Starter: "reply-lgtm"}), kinds: prs,
			field: "[0].github[0].reply.prompt", want: `"LGTM" is a reply that approves: a GitHub step only comments, ` +
				"so it never posts an approval's words; pick a comment reply",
		},
		{
			name: "a reply that requests changes", step: replyStep(spec.PromptRef{Nickname: "Needs changes"}), kinds: prs,
			field: "[0].github[0].reply.prompt",
			want:  `"Needs changes" is a reply that requests changes: a GitHub step only comments; pick a comment reply`,
		},
		{
			name: "a prompt for the other kind", step: agentStep(spec.PromptRef{Starter: "fix-checks"}), kinds: issues,
			field: "[0].agent.prompt", want: `"Fix checks" is written for pull requests, and this routine acts only on ` +
				"issues: pick a prompt for issues",
		},
		{
			name: "a prompt for one kind of both", step: agentStep(spec.PromptRef{Starter: "fix-checks"}), kinds: item.Kinds(),
			field: "[0].agent.prompt", want: `"Fix checks" is written for pull requests, and this routine acts on issues ` +
				"too: pick a prompt for both, or watch only pull requests",
		},
	} {
		problems := draft.StepPromptProblems(library(), c.step, c.kinds, "[0]")
		require.Len(t, problems, 1, c.name)
		assert.Equal(t, spec.Problem{Part: spec.PartSteps, Field: c.field, Message: c.want}, problems[0], c.name)
	}

	for name, step := range map[string]spec.Step{
		"the item's own default needs no check":   agentStep(spec.PromptRef{ItemDefault: true}),
		"a nickname, ignoring case, for any item": agentStep(spec.PromptRef{Nickname: "triage"}),
		"a comment's reply":                       replyStep(spec.PromptRef{Starter: "reply-question"}),
		"a step that names no prompt":             {Notify: &spec.NotifyStep{}},
	} {
		assert.Empty(t, draft.StepPromptProblems(library(), step, item.Kinds(), "[0]"), name)
	}

	labelsThenReply := spec.Step{GitHub: []spec.GitHubAction{
		{Labels: &spec.LabelsAction{Add: []string{"needs-repro"}}},
		{Reply: &spec.ReplyAction{Prompt: spec.PromptRef{Nickname: "Gone"}}},
	}}

	problems := draft.StepPromptProblems(library(), labelsThenReply, prs, "[2]")
	require.Len(t, problems, 1)
	assert.Equal(t, "[2].github[1].reply.prompt", problems[0].Field, "each reply at its own place")
}

func TestFindPromptFindsAStartersCopyOrANickname(t *testing.T) {
	t.Parallel()

	p, ok := draft.FindPrompt(library(), spec.PromptRef{Starter: "fix-checks"})
	require.True(t, ok)
	assert.Equal(t, "Fix checks", p.Nickname, "the user's copy of the starter")

	p, ok = draft.FindPrompt(library(), spec.PromptRef{Nickname: "needs CHANGES"})
	require.True(t, ok)
	assert.Equal(t, "reply-needs-changes", p.Starter, "a nickname, ignoring case")

	_, ok = draft.FindPrompt(library(), spec.PromptRef{Starter: "Fix checks"})
	assert.False(t, ok, "a starter is named by its key, never its nickname")

	_, ok = draft.FindPrompt(library(), spec.PromptRef{})
	assert.False(t, ok, "a reference that names nothing finds nothing")
}

func TestARoutineActsOnTheKindsItsWatchAndItsTriggerTake(t *testing.T) {
	t.Parallel()

	both := notifySpec()
	both.Watch = spec.Watch{Sources: []spec.Source{
		{Lists: []catalog.ListSource{catalog.SourceAssigned}},
		{Search: &watch.Search{Kind: item.KindIssue, Where: []string{watch.Me}}},
	}}

	both.Trigger = spec.Trigger{Event: catalog.EventAppears}
	assert.Equal(t, item.Kinds(), draft.ActsOn(both), "an item appears, of either kind")

	both.Trigger = spec.Trigger{Event: catalog.EventChecksFailed}
	assert.Equal(t, []item.Kind{item.KindPR}, draft.ActsOn(both), "only a pull request's checks fail")

	both.Trigger = spec.Trigger{Event: "no_such_event"}
	assert.Equal(t, item.Kinds(), draft.ActsOn(both), "an event the catalog doesn't know takes what the watch does")
}

func TestKindsAreNamedInASentence(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "pull requests", draft.KindsWords([]item.Kind{item.KindPR}))
	assert.Equal(t, "pull requests and issues", draft.KindsWords(item.Kinds()))

	assert.Equal(t, "its steps", draft.Series(nil), "no words")
	assert.Equal(t, "a", draft.Series([]string{"a"}))
	assert.Equal(t, "a and b", draft.Series([]string{"a", "b"}))
	assert.Equal(t, "a, b, and c", draft.Series([]string{"a", "b", "c"}))
}
