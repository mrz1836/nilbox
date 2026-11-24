package settings

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/prefs/prefval"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestInboxOrderStartsNewestFirst(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)

	order, err := svc.InboxOrder(t.Context())
	require.NoError(t, err)
	assert.Equal(t, InboxOrder{Sorts: map[ListKey]ListSort{}, StatusOrder: []string{}, Kinds: map[ListKey]ListKind{}}, order)
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

func TestSetListSort(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)

	for _, sort := range []ListSort{ListSortStatus, ListSortRepo, ListSortAuthor} {
		got, err := svc.SetListSort(t.Context(), ListYours, sort)
		require.NoError(t, err)
		assert.Equal(t, map[ListKey]ListSort{ListYours: sort}, got.InboxOrder.Sorts, "it stores %s", sort)

		stored, ok, err := prefs.New(db, nil).Get(t.Context(), listSortKey(ListYours))
		require.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, string(sort), stored)
	}

	require.NoError(t, testutil.ErrOf(svc.SetListSort(t.Context(), ListReviewRequested, ListSortRepo)))

	order, err := svc.InboxOrder(t.Context())
	require.NoError(t, err)
	assert.Equal(t, map[ListKey]ListSort{ListReviewRequested: ListSortRepo, ListYours: ListSortAuthor}, order.Sorts,
		"each list keeps its own")

	got, err := svc.SetListSort(t.Context(), ListYours, ListSortRecent)
	require.NoError(t, err)
	assert.Equal(t, map[ListKey]ListSort{ListReviewRequested: ListSortRepo}, got.InboxOrder.Sorts,
		"newest activity first leaves it out")
}

func TestSetListSortRefusesAnUnknownListOrSort(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)

	_, err := svc.SetListSort(t.Context(), "starred", ListSortRepo)
	require.ErrorIs(t, err, ErrUnknownSection)

	_, err = svc.SetListSort(t.Context(), formerAssigned, ListSortRepo)
	require.ErrorIs(t, err, ErrUnknownSection, "a former list is no section")

	_, err = svc.SetListSort(t.Context(), ListYours, "label")
	require.ErrorIs(t, err, ErrInvalidListSort)
	require.EqualError(t, err, `unknown sort "label": choose one of recent, status, repo, author`)

	order, err := svc.InboxOrder(t.Context())
	require.NoError(t, err)
	assert.Empty(t, order.Sorts, "nothing was stored")
}

func TestInboxOrderReadsALegacyStatusSort(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	setPref(t, db, statusSortKey(ListYours), prefval.EncodeBool(true))
	setPref(t, db, statusSortKey(ListReviewRequested), prefval.EncodeBool(false))

	order, err := svc.InboxOrder(t.Context())
	require.NoError(t, err)
	assert.Equal(t, map[ListKey]ListSort{ListYours: ListSortStatus}, order.Sorts,
		"a status sort stored before a list could be sorted any other way stays on")

	got, err := svc.SetListSort(t.Context(), ListYours, ListSortRecent)
	require.NoError(t, err)
	assert.Empty(t, got.InboxOrder.Sorts, "the list's own sort wins over the legacy one")

	got, err = svc.SetListSort(t.Context(), ListYours, ListSortAuthor)
	require.NoError(t, err)
	assert.Equal(t, map[ListKey]ListSort{ListYours: ListSortAuthor}, got.InboxOrder.Sorts)
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
	setPref(t, db, listSortKey(ListReviewRequested), "by label")
	setPref(t, db, KeyStatusOrder, "not json")

	order, err := svc.InboxOrder(t.Context())
	require.NoError(t, err)
	assert.Equal(t, InboxOrder{Sorts: map[ListKey]ListSort{}, StatusOrder: []string{}, Kinds: map[ListKey]ListKind{}}, order)
}

func TestAnUnreadableListSortIsNewestFirstEvenWithALegacyStatusSort(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	setPref(t, db, statusSortKey(ListYours), prefval.EncodeBool(true))
	setPref(t, db, listSortKey(ListYours), "by label")

	order, err := svc.InboxOrder(t.Context())
	require.NoError(t, err)
	assert.Empty(t, order.Sorts, "the list's own sort, once stored, wins, and one that can't be read is recent")
}

func TestAdoptFormerOrder(t *testing.T) {
	t.Parallel()

	current := InboxOrder{
		Sorts:       map[ListKey]ListSort{ListYours: ListSortStatus, ListReviewRequested: ListSortAuthor},
		StatusOrder: []string{"Ready"}, Kinds: map[ListKey]ListKind{ListReviewRequested: ListKindPRs},
	}
	assert.Equal(t, current, adoptFormerOrder(PortableInboxOrder{InboxOrder: current}),
		"an order that names no former list is as it was")

	statusSorted := PortableInboxOrder{
		StatusOrder: []string{"Ready"}, StatusSorted: []ListKey{ListReviewRequested, ListYours},
	}
	assert.Equal(t, InboxOrder{
		Sorts:       map[ListKey]ListSort{ListReviewRequested: ListSortStatus, ListYours: ListSortStatus},
		StatusOrder: []string{"Ready"}, Kinds: map[ListKey]ListKind{},
	}, adoptFormerOrder(statusSorted), "a file from before the sorts names its status sorts in a list")

	older := PortableInboxOrder{
		StatusOrder:  []string{"Ready"},
		Kinds:        map[ListKey]ListKind{formerAuthored: ListKindIssues, formerAssigned: ListKindPRs},
		StatusSorted: []ListKey{ListReviewRequested, formerAuthored, formerAssigned},
	}
	assert.Equal(t, InboxOrder{
		Sorts:       map[ListKey]ListSort{ListReviewRequested: ListSortStatus, ListYours: ListSortStatus},
		StatusOrder: []string{"Ready"}, Kinds: map[ListKey]ListKind{ListYours: ListKindPRs},
	}, adoptFormerOrder(older), "Yours takes Assigned's, and Authored's are dropped")

	both := PortableInboxOrder{
		StatusOrder:  []string{},
		Kinds:        map[ListKey]ListKind{formerAssigned: ListKindPRs, ListYours: ListKindIssues},
		StatusSorted: []ListKey{formerAssigned, ListYours},
	}
	assert.Equal(t, InboxOrder{
		Sorts: map[ListKey]ListSort{ListYours: ListSortStatus}, StatusOrder: []string{},
		Kinds: map[ListKey]ListKind{ListYours: ListKindIssues},
	}, adoptFormerOrder(both), "Yours's own kind wins, and it is sorted once")

	own := PortableInboxOrder{
		Sorts: map[ListKey]ListSort{ListYours: ListSortRepo}, StatusSorted: []ListKey{ListYours},
	}
	assert.Equal(t, map[ListKey]ListSort{ListYours: ListSortRepo}, adoptFormerOrder(own).Sorts,
		"a list's own sort wins over a legacy status sort")

	values, err := checkInboxOrder(adoptFormerOrder(older))
	require.NoError(t, err, "what it gives, the check takes")
	assert.Equal(t, "pr", values[listKindKey(ListYours)])
	assert.Equal(t, string(ListSortStatus), values[listSortKey(ListYours)])
}

func TestCheckInboxOrder(t *testing.T) {
	t.Parallel()

	values, err := checkInboxOrder(InboxOrder{Sorts: map[ListKey]ListSort{ListReviewRequested: ListSortAuthor}})
	require.NoError(t, err)
	assert.Equal(t, string(ListSortAuthor), values[listSortKey(ListReviewRequested)])
	assert.Equal(t, string(ListSortRecent), values[listSortKey(ListYours)], "every list's sort is written")
	assert.NotContains(t, values, statusSortKey(ListYours), "the legacy key is never written")

	_, err = checkInboxOrder(InboxOrder{Sorts: map[ListKey]ListSort{ListYours: "label"}})
	require.ErrorIs(t, err, ErrInvalidListSort)

	_, err = checkInboxOrder(InboxOrder{Sorts: map[ListKey]ListSort{"starred": ListSortRepo}})
	require.ErrorIs(t, err, ErrUnknownSection)
}
