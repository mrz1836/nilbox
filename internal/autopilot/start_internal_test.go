package autopilot

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/item"
)

// promptLibrary is the prompt library as a start reads it, or err.
type promptLibrary struct {
	prompts []LibraryPrompt
	err     error
}

func (l promptLibrary) Prompts(context.Context) ([]LibraryPrompt, error) { return l.prompts, l.err }

// outsideRead is the launch defaults' rules on outside contributors' items, as read: idx,
// or err.
type outsideRead struct {
	idx OutsideIndex
	err error
}

func (o outsideRead) Outside(context.Context) (OutsideIndex, error) { return o.idx, o.err }

func TestTriggerWordsNameATriggerAsTheBuilderDoes(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Checks fail", TriggerWords(catalog.EventChecksFailed))
	assert.Equal(t, "teleported", TriggerWords("teleported"), "one this build doesn't know is named as it is")
}

func TestOutsideRulesThatCantBeReadAllowAgentsNowhere(t *testing.T) {
	t.Parallel()

	r := newCmdRig(t, EngineConfig{Outside: outsideRead{err: errBroken}})

	allowed, where := r.e.outsideIndex(t.Context()).Allowed("acme-org/example-repo")
	assert.False(t, allowed)
	assert.Empty(t, where)
	assert.Contains(t, r.logs.String(), "Autopilot could not read where agents may run on outside contributors' items")

	r.e.outside = outsideRead{}
	allowed, _ = r.e.outsideIndex(t.Context()).Allowed("acme-org/example-repo")
	assert.False(t, allowed, "no rules read is none")
}

func TestAStartChecksItsPromptsAgainstTheLibraryRightBefore(t *testing.T) {
	t.Parallel()

	r := newCmdRig(t, EngineConfig{})
	sp := checksFailed()
	agent := spec.Step{Agent: &spec.AgentStep{Prompt: spec.PromptRef{Nickname: "Fix"}}}
	reply := spec.Step{GitHub: []spec.GitHubAction{{Reply: &spec.ReplyAction{Prompt: spec.PromptRef{Nickname: "Thanks"}}}}}
	fix := LibraryPrompt{Nickname: "Fix", Kinds: []item.Kind{item.KindPR}}

	blocker, words := r.e.promptHold(t.Context(), sp, agent)
	assert.Empty(t, blocker, "without a library to read, nothing is held")
	assert.Empty(t, words)

	r.svc.library = promptLibrary{prompts: []LibraryPrompt{fix}}

	blocker, _ = r.e.promptHold(t.Context(), sp, agent)
	assert.Empty(t, blocker, "the prompt is still there")

	blocker, _ = r.e.promptHold(t.Context(), sp, spec.Step{Notify: &spec.NotifyStep{}})
	assert.Empty(t, blocker, "a step that names no prompt")

	blocker, words = r.e.promptHold(t.Context(), sp, reply)
	assert.Equal(t, BlockNotReady, blocker)
	assert.Equal(t, `Its reply can't be sent: no prompt in your library has the nickname "Thanks": pick one that does.`, words)

	r.svc.library = promptLibrary{err: errBroken}

	blocker, _ = r.e.promptHold(t.Context(), sp, agent)
	assert.Empty(t, blocker, "a library that can't be read holds nothing; the launch finds what isn't there")
	assert.Contains(t, r.logs.String(), "Autopilot could not read the prompt library before a start")
}

func TestNeedsYourOKTellsOfAMatchThatStillWaits(t *testing.T) {
	t.Parallel()

	sink := &told{}
	r := newCmdRig(t, EngineConfig{NeedsYou: sink})
	id := r.checks()
	waiting := r.match(id, StateWaiting, BlockAsk, step(catalog.StepNotify, ""))
	queued := r.match(id, StateQueued, "", step(catalog.StepNotify, ""))

	n, ok := r.e.waitNotice(t.Context(), waiting)
	require.True(t, ok)
	assert.Equal(t, NoticeNeedsYou, n.Kind)
	assert.Equal(t, "Checks: Waiting for your OK.", n.Body)
	assert.Equal(t, "Checks", n.Routine)
	assert.Nil(t, n.Banner, "a routine with no banner of its own follows the settings")

	for _, matchID := range []int64{queued, 999} {
		_, ok = r.e.waitNotice(t.Context(), matchID)
		assert.False(t, ok, "match %d doesn't wait for the user", matchID)
	}

	sink.err = errBroken

	r.e.tellWait(t.Context(), n)
	assert.Contains(t, r.logs.String(), "Autopilot could not tell that a match waits for you")
}

func TestAStartItsRunnerHeldGoesBackToTheQueueOrWaits(t *testing.T) {
	t.Parallel()

	e := &Engine{}
	m := &match{steps: []stepRun{{Step: spec.Step{Notify: &spec.NotifyStep{}}, Status: stepRunning, RunID: 7}}}
	m.State = string(StateRunning)

	e.heldAt(m, &m.steps[0], Result{Requeue: true, Wait: BlockBusyWorktree})
	assert.Equal(t, StateQueued, m.state(), "a requeue is decided again")
	assert.Equal(t, stepPending, m.steps[0].Status)
	assert.Zero(t, m.steps[0].RunID, "its run taken back")
}
