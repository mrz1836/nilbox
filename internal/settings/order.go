package settings

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mrz1836/nilbox/internal/store"
)

// keyStatusSortPrefix starts the key that stores whether an inbox list is sorted by
// project status rather than by newest activity: "ui.statusSort.<list key>", holding
// "true" or "false". Each list has its own key, as each collapsed section does.
const keyStatusSortPrefix = "ui.statusSort."

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

// InboxOrder is how the inbox orders its lists: which are sorted by project status, and
// the order of the statuses. Every field is a plain value.
type InboxOrder struct {
	// StatusSorted lists the inbox lists sorted by project status, in the order the inbox
	// shows them. Every other list keeps its newest activity first.
	StatusSorted []ListKey `json:"statusSorted"`

	// StatusOrder is the user's order of project status names, first ranked highest. It
	// is empty until the user changes it.
	StatusOrder []string `json:"statusOrder"`
}

// statusSortKey is the preference that stores whether list is sorted by project status.
func statusSortKey(list ListKey) string {
	return keyStatusSortPrefix + string(list)
}

// orderKeys returns the preferences the inbox order reads.
func orderKeys() []string {
	keys := make([]string, 0, len(listKeys())+1)
	for _, list := range listKeys() {
		keys = append(keys, statusSortKey(list))
	}

	return append(keys, KeyStatusOrder)
}

// decodeInboxOrder reads the inbox order from the stored preferences. A sort that is not
// a boolean is newest activity first, and an order that can't be read is empty.
func decodeInboxOrder(stored map[string]string) InboxOrder {
	out := InboxOrder{StatusSorted: []ListKey{}, StatusOrder: []string{}}

	for _, list := range listKeys() {
		if on, err := strconv.ParseBool(stored[statusSortKey(list)]); err == nil && on {
			out.StatusSorted = append(out.StatusSorted, list)
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

	return s.save(ctx, setting(statusSortKey(section), strconv.FormatBool(on)))
}

// SetStatusOrder stores the user's order of project status names, first ranked highest,
// and returns the settings with it. Names are trimmed, and empty and repeated ones (ignoring
// case) are left out. An empty order puts every status back where the window would place
// it.
func (s *Service) SetStatusOrder(ctx context.Context, order []string) (Settings, error) {
	clean := cleanStatusOrder(order)
	if len(clean) > maxStatuses {
		return Settings{}, fmt.Errorf("%w: %d statuses; keep it to %d", ErrInvalidStatusOrder, len(clean), maxStatuses)
	}

	for _, name := range clean {
		if utf8.RuneCountInString(name) > maxStatusLength {
			return Settings{}, fmt.Errorf("%w: a status name is longer than %d characters", ErrInvalidStatusOrder,
				maxStatusLength)
		}
	}

	encoded, err := store.EncodeJSON(clean)
	if err != nil {
		return Settings{}, err
	}

	return s.save(ctx, setting(KeyStatusOrder, encoded))
}
