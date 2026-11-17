package sessions_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/sessions"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestARunsSessionIsLinkedOnceToTheConversationItsStreamNames(t *testing.T) {
	t.Parallel()

	store := sessions.NewStore(testutil.DB(t))
	ledger := sessions.NewLedger(sessions.Config{Store: store, Clock: testutil.NewClock(launched().Add(time.Minute))})

	codex := runRecord("r1")
	codex.Agent, codex.AgentSessionID = "codex", ""
	require.NoError(t, ledger.Start(t.Context(), codex))

	got, err := store.Get(t.Context(), "r1")
	require.NoError(t, err)
	assert.False(t, got.Session("/srv").CanResume, "a run whose agent hasn't named its conversation has nothing to resume")

	const thread = "01a1e66a-b48a-7f43-a547-901d3ca2bb33"

	require.NoError(t, ledger.RunState(t.Context(), "r1", sessions.RunUpdate{
		State: sessions.StateWorking, AgentSessionID: thread,
	}))

	got, err = store.Get(t.Context(), "r1")
	require.NoError(t, err)
	assert.Equal(t, thread, got.AgentSessionID, "Codex picks its thread's ID, which the stream names")

	require.NoError(t, ledger.RunState(t.Context(), "r1", sessions.RunUpdate{
		State: sessions.StateEnded, AgentSessionID: "01a1e66a-b48a-7f43-a547-901d3ca2bb34",
	}))

	got, err = store.Get(t.Context(), "r1")
	require.NoError(t, err)
	assert.Equal(t, thread, got.AgentSessionID, "linked once")
	assert.True(t, got.Session("/srv").CanResume, "so the ended run can be taken over")
}
