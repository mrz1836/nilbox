package settings

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestInboxOrderStartsNewestFirst(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)

	order, err := svc.InboxOrder(t.Context())
	require.NoError(t, err)
	assert.Equal(t, InboxOrder{StatusSorted: []ListKey{}, StatusOrder: []string{}, Kinds: map[ListKey]ListKind{}}, order)
}

func TestSetListKind(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)

	got, err := svc.SetListKind(t.Context(), ListAssigned, ListKindIssues)
	require.NoError(t, err)
	assert.Equal(t, map[ListKey]ListKind{ListAssigned: ListKindIssues}, got.InboxOrder.Kinds)

	require.NoError(t, testutil.ErrOf(svc.SetListKind(t.Context(), ListAuthored, ListKindPRs)))

	order, err := svc.InboxOrder(t.Context())
	require.NoError(t, err)
	assert.Equal(t, map[ListKey]ListKind{ListAssigned: ListKindIssues, ListAuthored: ListKindPRs}, order.Kinds)

	got, err = svc.SetListKind(t.Context(), ListAssigned, ListKindAll)
	require.NoError(t, err)
	assert.Equal(t, map[ListKey]ListKind{ListAuthored: ListKindPRs}, got.InboxOrder.Kinds, "every item leaves it out")
}

func TestSetListKindRefusesAnUnknownListOrKind(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)

	_, err := svc.SetListKind(t.Context(), "starred", ListKindPRs)
	require.ErrorIs(t, err, ErrUnknownSection)

	_, err = svc.SetListKind(t.Context(), ListAssigned, "discussion")
	require.ErrorIs(t, err, ErrInvalidListKind)
	require.EqualError(t, err, `unknown kind of item "discussion": choose one of all, pr, issue`)

	order, err := svc.InboxOrder(t.Context())
	require.NoError(t, err)
	assert.Empty(t, order.Kinds, "nothing was stored")
}

func TestInboxOrderShowsEveryItemForAKindItCantRead(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	setPref(t, db, listKindKey(ListAssigned), "PR")

	order, err := svc.InboxOrder(t.Context())
	require.NoError(t, err)
	assert.Empty(t, order.Kinds)
}

func TestSetStatusSort(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)

	got, err := svc.SetStatusSort(t.Context(), ListAssigned, true)
	require.NoError(t, err)
	assert.Equal(t, []ListKey{ListAssigned}, got.InboxOrder.StatusSorted)

	require.NoError(t, testutil.ErrOf(svc.SetStatusSort(t.Context(), ListReviewRequested, true)))

	order, err := svc.InboxOrder(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []ListKey{ListReviewRequested, ListAssigned}, order.StatusSorted, "in the order the inbox shows them")

	got, err = svc.SetStatusSort(t.Context(), ListAssigned, false)
	require.NoError(t, err)
	assert.Equal(t, []ListKey{ListReviewRequested}, got.InboxOrder.StatusSorted)
}

func TestSetStatusSortRefusesAnUnknownList(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)

	_, err := svc.SetStatusSort(t.Context(), "starred", true)
	require.ErrorIs(t, err, ErrUnknownSection)
}

func TestSetStatusOrder(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)

	got, err := svc.SetStatusOrder(t.Context(), []string{" In progress ", "Ready", "", "in PROGRESS", "Blocked"})
	require.NoError(t, err)
	assert.Equal(t, []string{"In progress", "Ready", "Blocked"}, got.InboxOrder.StatusOrder,
		"names are trimmed, and empty and repeated ones left out")

	order, err := svc.InboxOrder(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{"In progress", "Ready", "Blocked"}, order.StatusOrder)

	got, err = svc.SetStatusOrder(t.Context(), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{}, got.InboxOrder.StatusOrder, "an empty order resets it")
}

func TestSetStatusOrderRefusesAnUnboundedOrder(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)

	many := make([]string, maxStatuses+1)
	for i := range many {
		many[i] = strings.Repeat("s", i+1)
	}

	_, err := svc.SetStatusOrder(t.Context(), many)
	require.ErrorIs(t, err, ErrInvalidStatusOrder)

	_, err = svc.SetStatusOrder(t.Context(), []string{strings.Repeat("x", maxStatusLength+1)})
	require.ErrorIs(t, err, ErrInvalidStatusOrder)

	order, err := svc.InboxOrder(t.Context())
	require.NoError(t, err)
	assert.Empty(t, order.StatusOrder, "nothing was stored")
}

func TestAnUnreadableInboxOrderIsNewestFirst(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	setPref(t, db, statusSortKey(ListAssigned), "sometimes")
	setPref(t, db, KeyStatusOrder, "not json")

	order, err := svc.InboxOrder(t.Context())
	require.NoError(t, err)
	assert.Equal(t, InboxOrder{StatusSorted: []ListKey{}, StatusOrder: []string{}, Kinds: map[ListKey]ListKind{}}, order)
}
