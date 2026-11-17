package inbox

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/fence"
	"github.com/mrz1836/nilbox/internal/sensitive"
)

// headC is the commit an agent run pushed on top of headA.
const headC = "cccccccccccccccccccccccccccccccccccccccc"

func TestARunsChangesAreNotedAtBothItsHeads(t *testing.T) {
	t.Parallel()

	h := filesHarness(t, from(2, "member", "MEMBER", false))
	id := itemByNumber(t, h.refresh(t).ReviewRequested, 2).ID
	st := NewStore(h.pool, h.clock)

	_, err := h.svc.StoredFiles(t.Context(), id)
	require.ErrorIs(t, err, sensitive.ErrNotChecked, "a member's pull request isn't checked on its own")

	_, found, err := st.AgentChanged(t.Context(), id, "")
	require.NoError(t, err)
	assert.False(t, found, "no run noted anything yet")

	require.NoError(t, st.NoteAgentChanges(t.Context(), id, []string{strings.ToUpper(headA), headC, ""},
		[]string{"main.go", ".claude/settings.json", "main.go", " "}, 7))

	for _, head := range []string{"", headA, strings.ToUpper(headC)} {
		check, ok, getErr := st.AgentChanged(t.Context(), id, head)
		require.NoError(t, getErr)
		require.True(t, ok, "head %q", head)

		agent, flagged := check.Category(sensitive.CategoryAgent)
		require.True(t, flagged, "a run that wrote agent settings holds the next click at %q", head)
		assert.Equal(t, ".claude/settings.json", agent.Files[0].Path)
		assert.Equal(t, 2, check.Total, "each path once")
		assert.True(t, check.Complete)
	}

	_, found, err = st.AgentChanged(t.Context(), id, headB)
	require.NoError(t, err)
	assert.False(t, found, "a head the run never had is held for nothing it did")

	require.NoError(t, st.NoteAgentChanges(t.Context(), id, []string{headC}, []string{".github/workflows/ci.yml"}, 8))

	check, _, err := st.AgentChanged(t.Context(), id, headC)
	require.NoError(t, err)
	assert.Equal(t, 3, check.Total, "a second run at a head adds its paths")

	_, ok := check.Category(sensitive.CategoryWorkflows)
	assert.True(t, ok)

	require.NoError(t, st.NoteAgentChanges(t.Context(), id, []string{headB}, nil, 9))

	_, found, err = st.AgentChanged(t.Context(), id, headB)
	require.NoError(t, err)
	assert.False(t, found, "a run that changed nothing notes nothing")
}

func TestAnItemKeepsItsNewestRunHeads(t *testing.T) {
	t.Parallel()

	h := filesHarness(t, from(3, "member", "MEMBER", false))
	id := itemByNumber(t, h.refresh(t).ReviewRequested, 3).ID
	st := NewStore(h.pool, h.clock)

	for i := range maxAgentChangeHeads + 5 {
		h.clock.Advance(1)

		head := fmt.Sprintf("%040d", i+1)
		require.NoError(t, st.NoteAgentChanges(t.Context(), id, []string{head}, []string{"CLAUDE.md"}, int64(i+1)))
	}

	assert.Equal(t, int64(maxAgentChangeHeads), h.count(t, "item_agent_changes"))

	_, found, err := st.AgentChanged(t.Context(), id, fmt.Sprintf("%040d", 1))
	require.NoError(t, err)
	assert.False(t, found, "the oldest heads are pruned")

	_, found, err = st.AgentChanged(t.Context(), id, fmt.Sprintf("%040d", maxAgentChangeHeads+5))
	require.NoError(t, err)
	assert.True(t, found)
}

func TestARunsLongListIsIncomplete(t *testing.T) {
	t.Parallel()

	h := filesHarness(t, from(4, "member", "MEMBER", false))
	id := itemByNumber(t, h.refresh(t).ReviewRequested, 4).ID
	st := NewStore(h.pool, h.clock)

	many := make([]string, fence.MaxChanged+10)
	for i := range many {
		many[i] = fmt.Sprintf("pkg/f%d.go", i)
	}

	require.NoError(t, st.NoteAgentChanges(t.Context(), id, []string{headA}, many, 1))

	check, found, err := st.AgentChanged(t.Context(), id, headA)
	require.NoError(t, err)
	require.True(t, found)
	assert.False(t, check.Complete, "a run that changed more than the cap can't be wholly checked")
	assert.Equal(t, fence.MaxChanged+10, check.Total)
}
