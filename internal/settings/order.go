package settings

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/mrz1836/nilbox/internal/prefs/prefval"
	"github.com/mrz1836/nilbox/internal/store"
)

// keyStatusSortPrefix starts the key that stores whether an inbox list is sorted by
// project status rather than by newest activity: "ui.statusSort.<list key>", holding
// "true" or "false". Each list has its own key, as each collapsed section does.
const keyStatusSortPrefix = "ui.statusSort."

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

// InboxOrder is how the inbox orders and narrows its lists: which are sorted by project
// status, the order of the statuses, and which show only one kind of item. Every field is
// a plain value.
type InboxOrder struct {
	// StatusSorted lists the inbox lists sorted by project status, in the order the inbox
	// shows them. Every other list keeps its newest activity first.
	StatusSorted []ListKey `json:"statusSorted"`

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

// statusSortKey is the preference that stores whether list is sorted by project status.
func statusSortKey(list ListKey) string {
	return keyStatusSortPrefix + string(list)
}

// orderKeys returns the preferences the inbox order reads.
func orderKeys() []string {
	keys := make([]string, 0, 2*len(listKeys())+1)
	for _, list := range listKeys() {
		keys = append(keys, statusSortKey(list), listKindKey(list))
	}

	return append(keys, KeyStatusOrder)
}

// decodeInboxOrder reads the inbox order from the stored preferences. A sort that is not
// a boolean is newest activity first, a kind that can't be read shows every item, and an
// order that can't be read is empty.
func decodeInboxOrder(stored map[string]string) InboxOrder {
	out := InboxOrder{StatusSorted: []ListKey{}, StatusOrder: []string{}, Kinds: map[ListKey]ListKind{}}

	for _, list := range listKeys() {
		if prefval.Bool(stored[statusSortKey(list)], false) {
			out.StatusSorted = append(out.StatusSorted, list)
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

// SetStatusSort stores whether an inbox list is sorted by project status, rather than by
// newest activity, and returns the settings with it.
func (s *Service) SetStatusSort(ctx context.Context, section ListKey, on bool) (Settings, error) {
	if err := sections().check(section); err != nil {
		return Settings{}, err
	}

	return s.save(ctx, setting(statusSortKey(section), prefval.EncodeBool(on)))
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

// checkInboxOrder refuses an inbox order that names a list that isn't one, a kind a list
// can't show, or a status order SetStatusOrder would refuse, and returns the writes of
// the whole order: every list's sort and kind, and the status order.
func checkInboxOrder(order InboxOrder) (map[string]string, error) {
	values, err := checkStatusOrder(order.StatusOrder)
	if err != nil {
		return nil, err
	}

	for _, list := range order.StatusSorted {
		if err = sections().check(list); err != nil {
			return nil, err
		}
	}

	for list, kind := range order.Kinds {
		if err = sections().check(list); err != nil {
			return nil, err
		}

		if err = listKinds().check(kind); err != nil {
			return nil, err
		}
	}

	for _, list := range listKeys() {
		values[statusSortKey(list)] = prefval.EncodeBool(slices.Contains(order.StatusSorted, list))
		values[listKindKey(list)] = string(cmp.Or(order.Kinds[list], ListKindAll))
	}

	return values, nil
}
