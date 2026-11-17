package settings

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/prefs/prefval"
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

	got, err := svc.SetListKind(t.Context(), ListYours, ListKindIssues)
	require.NoError(t, err)
	assert.Equal(t, map[ListKey]ListKind{ListYours: ListKindIssues}, got.InboxOrder.Kinds)

	require.NoError(t, testutil.ErrOf(svc.SetListKind(t.Context(), ListReviewRequested, ListKindPRs)))

	order, err := svc.InboxOrder(t.Context())
	require.NoError(t, err)
	assert.Equal(t, map[ListKey]ListKind{ListYours: ListKindIssues, ListReviewRequested: ListKindPRs}, order.Kinds)

	got, err = svc.SetListKind(t.Context(), ListYours, ListKindAll)
	require.NoError(t, err)
	assert.Equal(t, map[ListKey]ListKind{ListReviewRequested: ListKindPRs}, got.InboxOrder.Kinds,
		"every item leaves it out")
}

func TestSetListKindRefusesAnUnknownListOrKind(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)

	_, err := svc.SetListKind(t.Context(), "starred", ListKindPRs)
	require.ErrorIs(t, err, ErrUnknownSection)

	_, err = svc.SetListKind(t.Context(), formerAssigned, ListKindPRs)
	require.ErrorIs(t, err, ErrUnknownSection, "a former list is no section")

	_, err = svc.SetListKind(t.Context(), ListYours, "discussion")
	require.ErrorIs(t, err, ErrInvalidListKind)
	require.EqualError(t, err, `unknown kind of item "discussion": choose one of all, pr, issue`)

	order, err := svc.InboxOrder(t.Context())
	require.NoError(t, err)
	assert.Empty(t, order.Kinds, "nothing was stored")
}

func TestInboxOrderShowsEveryItemForAKindItCantRead(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	setPref(t, db, listKindKey(ListYours), "PR")

	order, err := svc.InboxOrder(t.Context())
	require.NoError(t, err)
	assert.Empty(t, order.Kinds)
}

func TestSetStatusSort(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)

	got, err := svc.SetStatusSort(t.Context(), ListYours, true)
	require.NoError(t, err)
	assert.Equal(t, []ListKey{ListYours}, got.InboxOrder.StatusSorted)

	require.NoError(t, testutil.ErrOf(svc.SetStatusSort(t.Context(), ListReviewRequested, true)))

	order, err := svc.InboxOrder(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []ListKey{ListReviewRequested, ListYours}, order.StatusSorted, "in the order the inbox shows them")

	got, err = svc.SetStatusSort(t.Context(), ListYours, false)
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
	setPref(t, db, statusSortKey(ListYours), "sometimes")
	setPref(t, db, KeyStatusOrder, "not json")

	order, err := svc.InboxOrder(t.Context())
	require.NoError(t, err)
	assert.Equal(t, InboxOrder{StatusSorted: []ListKey{}, StatusOrder: []string{}, Kinds: map[ListKey]ListKind{}}, order)
}

func TestAdoptFormerOrder(t *testing.T) {
	t.Parallel()

	current := InboxOrder{
		StatusSorted: []ListKey{ListYours}, StatusOrder: []string{"Ready"},
		Kinds: map[ListKey]ListKind{ListReviewRequested: ListKindPRs},
	}
	assert.Equal(t, current, adoptFormerOrder(current), "an order that names no former list is as it was")

	older := InboxOrder{
		StatusSorted: []ListKey{ListReviewRequested, formerAuthored, formerAssigned}, StatusOrder: []string{"Ready"},
		Kinds: map[ListKey]ListKind{formerAuthored: ListKindIssues, formerAssigned: ListKindPRs},
	}
	assert.Equal(t, InboxOrder{
		StatusSorted: []ListKey{ListReviewRequested, ListYours}, StatusOrder: []string{"Ready"},
		Kinds: map[ListKey]ListKind{ListYours: ListKindPRs},
	}, adoptFormerOrder(older), "Yours takes Assigned's, and Authored's are dropped")

	both := InboxOrder{
		StatusSorted: []ListKey{formerAssigned, ListYours}, StatusOrder: []string{},
		Kinds: map[ListKey]ListKind{formerAssigned: ListKindPRs, ListYours: ListKindIssues},
	}
	assert.Equal(t, InboxOrder{
		StatusSorted: []ListKey{ListYours}, StatusOrder: []string{}, Kinds: map[ListKey]ListKind{ListYours: ListKindIssues},
	}, adoptFormerOrder(both), "Yours's own kind wins, and it is sorted once")

	values, err := checkInboxOrder(adoptFormerOrder(older))
	require.NoError(t, err, "what it gives, the check takes")
	assert.Equal(t, "pr", values[listKindKey(ListYours)])
	assert.Equal(t, prefval.EncodeBool(true), values[statusSortKey(ListYours)])
}
