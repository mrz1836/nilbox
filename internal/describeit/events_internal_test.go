package describeit

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agentrun/runevent"
	"github.com/mrz1836/nilbox/internal/uierr"
)

func TestEventsReadAnEndedTurnsStreamAgain(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	id, _, _ := firstTurn(t, r, Message{Text: pinText})

	page, err := r.svc.Events(t.Context(), id, 1, 0)
	require.NoError(t, err)
	assert.True(t, page.Done, "an ended turn adds nothing more")
	require.NotEmpty(t, page.Events)
	assert.Equal(t, int64(1), page.Events[0].Seq, "numbered as its follower numbered them")
	assert.Equal(t, runevent.KindStarted, page.Events[0].Kind)
	assert.Equal(t, runevent.KindResult, page.Events[len(page.Events)-1].Kind)

	last := page.Events[len(page.Events)-1].Seq

	page, err = r.svc.Events(t.Context(), id, 1, last-1)
	require.NoError(t, err)
	require.Len(t, page.Events, 1, "after a sequence number, only what came after")

	page, err = r.svc.Events(t.Context(), id, 1, last)
	require.NoError(t, err)
	assert.NotNil(t, page.Events)
	assert.Empty(t, page.Events)
	assert.NotNil(t, page.Progress.Todos)
}

func TestEventsOfATurnThatIsntThereAreRefused(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	id, _, _ := firstTurn(t, r, Message{Text: pinText})

	for name, tt := range map[string]struct {
		id   string
		turn int
	}{
		"no conversation":         {id: "", turn: 1},
		"a conversation's path":   {id: "../" + id, turn: 1},
		"turn 0":                  {id: id, turn: 0},
		"a turn not yet":          {id: id, turn: 2},
		"a conversation not kept": {id: uuid.NewString(), turn: 1},
	} {
		page, err := r.svc.Events(t.Context(), tt.id, tt.turn, 0)
		require.ErrorIs(t, err, errNoConversation, name)
		assert.Equal(t, "That turn isn't one of Describe it's any more.", uierr.Message(err), name)
		assert.NotNil(t, page.Events, name)
	}

	_, err := r.svc.NewConversation(t.Context(), AgentChoice{})
	require.NoError(t, err)

	page, err := r.svc.Events(t.Context(), id, 1, 0)
	require.NoError(t, err, "a conversation set aside still reads its turns")
	assert.NotEmpty(t, page.Events)
}

func TestAConversationStartedMoreThanThirtyDaysAgoIsGone(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	id, proposal, _ := firstTurn(t, r, Message{Text: pinText})

	r.clock.Advance(keepConversations + time.Second)

	conv, err := r.svc.Conversation(t.Context())
	require.NoError(t, err)
	assert.Empty(t, conv.ID, "it is gone")
	assert.NotNil(t, conv.Turns)
	assert.NoDirExists(t, r.path("conversations", id))
	assert.NoFileExists(t, r.path("proposals", id, proposal+".json"), "with its proposals")
}

func TestTheCurrentConversationIsTheNewestKept(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	older := newRecord(uuid.NewString(), AgentChoice{Model: "opus"}, Start{}, testNow.Add(-time.Hour))
	newer := newRecord(uuid.NewString(), AgentChoice{Model: "sonnet"}, Start{}, testNow)

	require.NoError(t, r.svc.saveRecord(older))
	require.NoError(t, r.svc.saveRecord(newer))

	require.NoError(t, os.MkdirAll(r.path("conversations", uuid.NewString()), 0o700), "a folder with no record")
	require.NoError(t, os.MkdirAll(r.path("conversations", "notes"), 0o700), "a folder that isn't a conversation's")

	conv, err := r.svc.Conversation(t.Context())
	require.NoError(t, err)
	assert.Equal(t, newer.ID, conv.ID)
	assert.Equal(t, "sonnet", conv.Agent.Model)
}

func TestARecordIsReadOnlyForItsOwnConversation(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	id, other := uuid.NewString(), uuid.NewString()

	rec := newRecord(id, AgentChoice{}, Start{}, testNow)
	rec.Turns = []turnRecord{{N: 7, State: TurnDone, Proposals: []string{"../../etc", proposalID(t, testNow, 0)}}}
	require.NoError(t, r.svc.saveRecord(rec))

	got, err := r.svc.readRecord(id)
	require.NoError(t, err)
	assert.Equal(t, 1, got.Turns[0].N, "a turn's number is its place")
	assert.Equal(t, []string{proposalID(t, testNow, 0)}, got.Turns[0].Proposals, "a proposal named as none is dropped")
	assert.NotNil(t, got.Turns[0].Attach)

	data, err := json.Marshal(record{Version: recordVersion, ID: other})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(r.svc.recordFile(id), data, 0o600))

	_, err = r.svc.readRecord(id)
	require.ErrorIs(t, err, errBadRecord, "a record naming another conversation is refused")

	require.NoError(t, os.WriteFile(r.svc.recordFile(id), []byte(`{"version":2,"id":"`+id+`"}`), 0o600))

	_, err = r.svc.readRecord(id)
	require.ErrorIs(t, err, errBadRecord, "and one of another version")

	require.NoError(t, os.WriteFile(r.svc.recordFile(id), []byte("{"), 0o600))

	_, err = r.svc.readRecord(id)
	require.ErrorIs(t, err, errBadRecord)
}

func TestFillListsLeavesNoNullTheEncodingWrites(t *testing.T) {
	t.Parallel()

	type inner struct {
		List []string `json:"list"`
	}

	type outer struct {
		Embedded `json:"embedded"`

		List    []string          `json:"list"`
		Skipped []string          `json:"skipped,omitempty"`
		Map     map[string]string `json:"map"`
		Raw     json.RawMessage   `json:"raw"`
		Bytes   []byte            `json:"bytes"`
		Hidden  []string          `json:"-"`
		Inner   inner             `json:"inner"`
		Pointer *inner            `json:"pointer"`
		Nil     *inner            `json:"nil"`
		Inners  []inner           `json:"inners"`
		Any     any               `json:"any"`
		unshown []string
	}

	v := outer{Pointer: &inner{}, Inners: []inner{{}}, Any: &inner{}}
	fillLists(&v)

	assert.NotNil(t, v.List)
	assert.Nil(t, v.Skipped, "omitempty leaves a nil list out")
	assert.NotNil(t, v.Map)
	assert.Nil(t, v.Raw, "JSON as text stays as it is")
	assert.Nil(t, v.Bytes)
	assert.Nil(t, v.Hidden, "a field the encoding skips is left alone")
	assert.NotNil(t, v.Inner.List)
	assert.NotNil(t, v.Pointer.List)
	assert.Nil(t, v.Nil)
	assert.NotNil(t, v.Inners[0].List)
	assert.NotNil(t, v.Any.(*inner).List, "and through an interface") //nolint:forcetypeassert // the test set it
	assert.Nil(t, v.unshown)
	assert.NotNil(t, v.Names)

	_, err := json.Marshal(v)
	require.NoError(t, err)
}

// Embedded is a struct a test embeds.
type Embedded struct {
	Names []string `json:"names"`
}
