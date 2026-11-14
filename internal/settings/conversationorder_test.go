package settings

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheSheetsListAConversationInTheOrderLastChosen(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, ConversationOldest, got.ConversationOrder, "a fresh database lists the oldest first, as GitHub does")

	got, err = svc.SetConversationOrder(t.Context(), ConversationNewest)
	require.NoError(t, err)
	assert.Equal(t, ConversationNewest, got.ConversationOrder)

	got, err = svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, ConversationNewest, got.ConversationOrder, "the choice is stored")

	_, err = svc.SetConversationOrder(t.Context(), "random")
	require.ErrorIs(t, err, ErrInvalidConversationOrder)

	got, err = svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, ConversationNewest, got.ConversationOrder, "a refused order stores nothing")
}

func TestAnUnreadableConversationOrderListsTheOldestFirst(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	setPref(t, db, KeyConversationOrder, "random")

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, ConversationOldest, got.ConversationOrder)
}
