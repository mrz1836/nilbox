package inbox

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// agentChangesHarness lists a member's pull request #2 at headA, and returns the harness,
// its id, and a Store over its database.
func agentChangesHarness(t *testing.T) (*harness, int64, *Store) {
	t.Helper()

	h := filesHarness(t, from(2, "member", "MEMBER", false))
	id := itemByNumber(t, h.refresh(t).ReviewRequested, 2).ID

	return h, id, NewStore(h.pool, h.clock)
}

func TestANotedListThatCantBeReadIsStartedAgain(t *testing.T) {
	t.Parallel()

	h, id, st := agentChangesHarness(t)

	require.NoError(t, st.NoteAgentChanges(t.Context(), id, []string{headA}, []string{"main.go"}, 7))
	h.exec(t, `UPDATE item_agent_changes SET files = '[1]'`)

	_, _, err := st.AgentChanged(t.Context(), id, headA)
	require.ErrorContains(t, err, "read what agent runs changed", "a launch never holds on what it can't read")

	require.NoError(t, st.NoteAgentChanges(t.Context(), id, []string{headA}, []string{".claude/settings.json"}, 8))

	check, found, err := st.AgentChanged(t.Context(), id, headA)
	require.NoError(t, err)
	require.True(t, found)

	agent, flagged := check.Category(sensitive.CategoryAgent)
	require.True(t, flagged, "the next run's paths are noted afresh")
	assert.Equal(t, ".claude/settings.json", agent.Files[0].Path)
}

func TestNotingWhatARunChangedThatCantBeStoredNotesNothing(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name, event, want string
	}{
		{"the paths", "BEFORE INSERT ON item_agent_changes", "note what an agent run changed"},
		{"the old heads", "BEFORE DELETE ON item_agent_changes", "prune what agent runs changed"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			h, id, st := agentChangesHarness(t)

			// Enough heads are noted that the next one prunes the oldest.
			for i := range maxAgentChangeHeads {
				head := fmt.Sprintf("%040x", i+1)
				require.NoError(t, st.NoteAgentChanges(t.Context(), id, []string{head}, []string{"main.go"}, int64(i)))
			}

			noted := h.count(t, "item_agent_changes")

			refuse(t, h, c.event, "")

			err := st.NoteAgentChanges(t.Context(), id, []string{headA}, []string{".claude/settings.json"}, 99)
			require.ErrorContains(t, err, c.want)
			assert.Equal(t, noted, h.count(t, "item_agent_changes"))

			_, found, err := st.AgentChanged(t.Context(), id, headA)
			require.NoError(t, err)
			assert.False(t, found, "the run's paths were rolled back with the prune")
		})
	}
}

func TestWhatRunsChangedReportsWhatItCantRead(t *testing.T) {
	t.Parallel()

	closed := NewStore(testutil.ClosedDB(t), nil)

	_, _, err := closed.AgentChanged(t.Context(), 1, headA)
	require.ErrorContains(t, err, "read the item")

	h, id, st := agentChangesHarness(t)

	h.exec(t, "ALTER TABLE item_agent_changes RENAME TO item_agent_changes_gone")

	_, _, err = st.AgentChanged(t.Context(), id, headA)
	require.ErrorContains(t, err, "read what agent runs changed")

	err = st.NoteAgentChanges(t.Context(), id, []string{headA}, []string{"main.go"}, 7)
	require.ErrorContains(t, err, "read what agent runs changed")

	h.exec(t, "ALTER TABLE prefs RENAME TO prefs_gone")

	_, _, err = st.AgentChanged(t.Context(), id, headA)
	require.ErrorContains(t, err, "read the inbox status")
}
