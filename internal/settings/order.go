package settings

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"unicode/utf8"

	"github.com/mrz1836/nilbox/internal/prefs/prefval"
	"github.com/mrz1836/nilbox/internal/store"
)

// keyListSortPrefix starts the key that stores how an inbox list is sorted:
// "ui.listSort.<list key>", holding a ListSort. Each list has its own key, as each
// collapsed section does.
const keyListSortPrefix = "ui.listSort."

// keyStatusSortPrefix starts the key that stored whether an inbox list was sorted by
// project status, before a list could be sorted any other way: "ui.statusSort.<list key>",
// holding "true" or "false". It is only read now, for a list that has no
// "ui.listSort.<list key>" yet, and true reads as ListSortStatus.
const keyStatusSortPrefix = "ui.statusSort."

// ListSort is how an inbox list is sorted: newest activity first, or grouped by project
// status, repository, or author, each group newest first.
type ListSort string

// The sorts a list can have.
const (
	ListSortRecent ListSort = "recent"
	ListSortStatus ListSort = "status"
	ListSortRepo   ListSort = "repo"
	ListSortAuthor ListSort = "author"
)

// ErrInvalidListSort is returned for a sort that is not one a list can have.
var ErrInvalidListSort = errors.New("unknown sort")

// listSorts is the sorts as a choice: a stored one that can't be read is newest activity
// first.
func listSorts() choice[ListSort] {
	return choice[ListSort]{
		values:   []ListSort{ListSortRecent, ListSortStatus, ListSortRepo, ListSortAuthor},
		fallback: ListSortRecent, invalid: ErrInvalidListSort,
	}
}

// keyListKindPrefix starts the key that stores which kind of item an inbox list shows on
// its own tab: "ui.listKind.<list key>", holding a ListKind. Each list has its own key.
const keyListKindPrefix = "ui.listKind."

// ListKind is which kind of item an inbox list shows on its own tab: every one, or only
// its pull requests, or only its issues.
type ListKind string

// The kinds a list can show.
const (
	ListKindAll    ListKind = "all"
	ListKindPRs    ListKind = "pr"
	ListKindIssues ListKind = "issue"
)

// ErrInvalidListKind is returned for a kind that is not one a list can show.
var ErrInvalidListKind = errors.New("unknown kind of item")

// listKinds is the kinds as a choice: a stored one that can't be read shows every item.
func listKinds() choice[ListKind] {
	return choice[ListKind]{
		values: []ListKind{ListKindAll, ListKindPRs, ListKindIssues}, fallback: ListKindAll, invalid: ErrInvalidListKind,
	}
}

// KeyStatusOrder is the order the user put project statuses in, such as "In progress"
// before "Blocked", as a JSON array of their names. A list sorted by status ranks each
// issue by the first of its projects' statuses here. Statuses not in it are placed by the
// window, which knows them from the inbox; an empty order leaves every one to it.
const KeyStatusOrder = "ui.statusOrder"

// Bounds on a status order. GitHub caps a project's options well below them; they only
// keep a stray call from storing anything unbounded.
const (
	maxStatuses     = 200
	maxStatusLength = 256
)

// ErrInvalidStatusOrder is returned for a status order that is too long, or holds a name
// that is.
var ErrInvalidStatusOrder = errors.New("invalid status order")

// InboxOrder is how the inbox orders and narrows its lists: how each is sorted, the order
// of the project statuses, and which show only one kind of item. Every field is a plain
// value.
type InboxOrder struct {
	// Sorts holds each list sorted other than by newest activity first, with its sort. A
	// list it leaves out keeps its newest activity first.
	Sorts map[ListKey]ListSort `json:"sorts"`

	// StatusOrder is the user's order of project status names, first ranked highest. It
	// is empty until the user changes it.
	StatusOrder []string `json:"statusOrder"`

	// Kinds holds each list that shows only one kind of item on its own tab, with that
	// kind. A list it leaves out shows every one.
	Kinds map[ListKey]ListKind `json:"kinds"`
}

// listKindKey is the preference that stores which kind of item list shows.
func listKindKey(list ListKey) string {
	return keyListKindPrefix + string(list)
}

// listSortKey is the preference that stores how list is sorted.
func listSortKey(list ListKey) string {
	return keyListSortPrefix + string(list)
}

// statusSortKey is the legacy preference that stored whether list was sorted by project
// status, read only while list has no listSortKey.
func statusSortKey(list ListKey) string {
	return keyStatusSortPrefix + string(list)
}

// orderKeys returns the preferences the inbox order reads.
func orderKeys() []string {
	keys := make([]string, 0, 3*len(listKeys())+1)
	for _, list := range listKeys() {
		keys = append(keys, listSortKey(list), statusSortKey(list), listKindKey(list))
	}

	return append(keys, KeyStatusOrder)
}

// decodeListSort reads how list is sorted from the stored preferences: its own sort, once
// one is stored, else status for a legacy status sort that is on, else newest activity
// first. A sort that can't be read is newest activity first.
func decodeListSort(stored map[string]string, list ListKey) ListSort {
	if raw, ok := stored[listSortKey(list)]; ok {
		return listSorts().decode(raw)
	}

	if prefval.Bool(stored[statusSortKey(list)], false) {
		return ListSortStatus
	}

	return ListSortRecent
}

// decodeInboxOrder reads the inbox order from the stored preferences. A sort that can't
// be read is newest activity first, a kind that can't be read shows every item, and an
// order that can't be read is empty.
func decodeInboxOrder(stored map[string]string) InboxOrder {
	out := InboxOrder{Sorts: map[ListKey]ListSort{}, StatusOrder: []string{}, Kinds: map[ListKey]ListKind{}}

	for _, list := range listKeys() {
		if sort := decodeListSort(stored, list); sort != ListSortRecent {
			out.Sorts[list] = sort
		}

		if kind := listKinds().decode(stored[listKindKey(list)]); kind != ListKindAll {
			out.Kinds[list] = kind
		}
	}

	if raw := stored[KeyStatusOrder]; raw != "" {
		if order, err := store.DecodeJSON[[]string](raw); err == nil {
			out.StatusOrder = cleanStatusOrder(order)
		}
	}

	return out
}

// cleanStatusOrder trims each name and leaves out the empty ones and those already in it,
// ignoring case, keeping the first.
func cleanStatusOrder(order []string) []string {
	out := make([]string, 0, len(order))
	seen := make(map[string]bool, len(order))

	for _, name := range order {
		name = strings.TrimSpace(name)
		key := strings.ToLower(name)

		if name == "" || seen[key] {
			continue
		}

		seen[key] = true

		out = append(out, name)
	}

	return out
}

// InboxOrder returns how the inbox orders its lists. On a fresh database every list is
// newest activity first, and the status order is empty.
func (s *Service) InboxOrder(ctx context.Context) (InboxOrder, error) {
	stored, err := s.prefs.GetMany(ctx, orderKeys()...)
	if err != nil {
		return InboxOrder{}, fmt.Errorf("read the inbox order: %w", err)
	}

	return decodeInboxOrder(stored), nil
}

// SetListSort stores how an inbox list is sorted, newest activity first or grouped by
// project status, repository, or author, and returns the settings with it.
func (s *Service) SetListSort(ctx context.Context, section ListKey, sort ListSort) (Settings, error) {
	if err := sections().check(section); err != nil {
		return Settings{}, err
	}

	if err := listSorts().check(sort); err != nil {
		return Settings{}, err
	}

	return s.save(ctx, setting(listSortKey(section), string(sort)))
}

// SetListKind stores which kind of item an inbox list shows on its own tab, every one or
// only its pull requests or its issues, and returns the settings with it.
func (s *Service) SetListKind(ctx context.Context, section ListKey, kind ListKind) (Settings, error) {
	if err := sections().check(section); err != nil {
		return Settings{}, err
	}

	if err := listKinds().check(kind); err != nil {
		return Settings{}, err
	}

	return s.save(ctx, setting(listKindKey(section), string(kind)))
}

// SetStatusOrder stores the user's order of project status names, first ranked highest,
// and returns the settings with it. Names are trimmed, and empty and repeated ones (ignoring
// case) are left out. An empty order puts every status back where the window would place
// it.
func (s *Service) SetStatusOrder(ctx context.Context, order []string) (Settings, error) {
	values, err := checkStatusOrder(order)
	if err != nil {
		return Settings{}, err
	}

	return s.save(ctx, setMany(values, ""))
}

// checkStatusOrder refuses a status order that is too long, or holds a name that is, and
// returns its write, cleaned.
func checkStatusOrder(order []string) (map[string]string, error) {
	clean := cleanStatusOrder(order)
	if len(clean) > maxStatuses {
		return nil, fmt.Errorf("%w: %d statuses; keep it to %d", ErrInvalidStatusOrder, len(clean), maxStatuses)
	}

	for _, name := range clean {
		if utf8.RuneCountInString(name) > maxStatusLength {
			return nil, fmt.Errorf("%w: a status name is longer than %d characters", ErrInvalidStatusOrder,
				maxStatusLength)
		}
	}

	encoded, err := store.EncodeJSON(clean)
	if err != nil {
		return nil, err
	}

	return map[string]string{KeyStatusOrder: encoded}, nil
}

// adoptFormerOrder reads an inbox order a backup holds. An older backup names its status
// sorts in a list of their own, StatusSorted (see adoptStatusSorted), and one older still
// names the former Authored and Assigned lists, which it reads as AdoptSections reads their
// stored keys: Yours takes the Assigned list's status sort and kind, unless the order gives
// Yours a sort or a kind of its own, and the Authored list's are dropped. Sorts are kept as
// they are, and an order that names none of these comes back as it is.
func adoptFormerOrder(order PortableInboxOrder) InboxOrder {
	out := InboxOrder{
		Sorts: adoptStatusSorted(order), StatusOrder: order.StatusOrder,
		Kinds: make(map[ListKey]ListKind, len(order.Kinds)),
	}

	_, ownKind := order.Kinds[ListYours]

	for list, kind := range order.Kinds {
		if now, ok := currentList(list); ok && (list != formerAssigned || !ownKind) {
			out.Kinds[now] = kind
		}
	}

	return out
}

// adoptStatusSorted returns the order's sorts with each list its StatusSorted names, read
// through currentList, sorted by status, unless the sorts give that list one of its own.
func adoptStatusSorted(order PortableInboxOrder) map[ListKey]ListSort {
	out := make(map[ListKey]ListSort, len(order.Sorts)+len(order.StatusSorted))
	maps.Copy(out, order.Sorts)

	for _, list := range order.StatusSorted {
		now, ok := currentList(list)
		if _, has := out[now]; ok && !has {
			out[now] = ListSortStatus
		}
	}

	return out
}

// currentList is the list an older backup's list stands for now, and false for the former
// Authored list, which none does.
func currentList(list ListKey) (ListKey, bool) {
	if list == formerAuthored {
		return "", false
	}

	if list == formerAssigned {
		return ListYours, true
	}

	return list, true
}

// checkInboxOrder refuses an inbox order that names a list that isn't one, a sort a list
// can't have, a kind a list can't show, or a status order SetStatusOrder would refuse, and
// returns the writes of the whole order: every list's sort and kind, and the status order.
func checkInboxOrder(order InboxOrder) (map[string]string, error) {
	values, err := checkStatusOrder(order.StatusOrder)
	if err != nil {
		return nil, err
	}

	if err = checkPerList(order.Sorts, listSorts()); err != nil {
		return nil, err
	}

	if err = checkPerList(order.Kinds, listKinds()); err != nil {
		return nil, err
	}

	for _, list := range listKeys() {
		values[listSortKey(list)] = string(cmp.Or(order.Sorts[list], ListSortRecent))
		values[listKindKey(list)] = string(cmp.Or(order.Kinds[list], ListKindAll))
	}

	return values, nil
}

// checkPerList refuses a value per list that names a list that isn't one, or holds a value
// that isn't one of the choices.
func checkPerList[T ~string](perList map[ListKey]T, choices choice[T]) error {
	for list, v := range perList {
		if err := sections().check(list); err != nil {
			return err
		}

		if err := choices.check(v); err != nil {
			return err
		}
	}

	return nil
}
