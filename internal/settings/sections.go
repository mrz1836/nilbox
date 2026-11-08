package settings

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/mrz1836/nilbox/internal/textutil"
)

// ListKey names an inbox list, as the inbox snapshot names it: each list is a section of
// the inbox the user can collapse.
type ListKey string

// The inbox lists, in the order the inbox shows them.
const (
	ListReviewRequested ListKey = "reviewRequested"
	ListAuthored        ListKey = "authored"
	ListAssigned        ListKey = "assigned"
)

// keyCollapsedPrefix starts the key that stores whether an inbox section is collapsed:
// "ui.collapsed.<list key>", holding "true" or "false". Each section has its own key, so
// collapsing one never rewrites another. The keys are the user's, so they sit under ui.*
// with the other window settings rather than under the inbox's own inbox.* keys.
const keyCollapsedPrefix = "ui.collapsed."

// ErrUnknownSection is returned for a section that is not one of the inbox lists.
var ErrUnknownSection = errors.New("unknown inbox section")

// listKeys returns the inbox lists in the order the inbox shows them.
func listKeys() []ListKey {
	return []ListKey{ListReviewRequested, ListAuthored, ListAssigned}
}

// collapsedKey is the preference that stores whether the section for list is collapsed.
func collapsedKey(list ListKey) string {
	return keyCollapsedPrefix + string(list)
}

// collapsedKeys returns the preference of every section, in the order the inbox shows
// them.
func collapsedKeys() []string {
	keys := make([]string, 0, len(listKeys()))
	for _, list := range listKeys() {
		keys = append(keys, collapsedKey(list))
	}

	return keys
}

// decodeCollapsed reads the collapsed sections from the stored preferences, in the order
// the inbox shows them. A stored value that is not a boolean counts as open.
func decodeCollapsed(stored map[string]string) []ListKey {
	out := []ListKey{}

	for _, list := range listKeys() {
		if collapsed, err := strconv.ParseBool(stored[collapsedKey(list)]); err == nil && collapsed {
			out = append(out, list)
		}
	}

	return out
}

// CollapsedSections returns the inbox sections the user collapsed, in the order the inbox
// shows them. None is collapsed on a fresh database, and a stored value that is not a
// boolean counts as open.
func (s *Service) CollapsedSections(ctx context.Context) ([]ListKey, error) {
	stored, err := s.prefs.GetMany(ctx, collapsedKeys()...)
	if err != nil {
		return nil, fmt.Errorf("read the collapsed inbox sections: %w", err)
	}

	return decodeCollapsed(stored), nil
}

// SetSectionCollapsed stores whether an inbox section is collapsed, so the inbox opens the
// same way at the next launch, and returns the settings with it.
func (s *Service) SetSectionCollapsed(ctx context.Context, section ListKey, collapsed bool) (Settings, error) {
	if !slices.Contains(listKeys(), section) {
		return Settings{}, fmt.Errorf("%w %q: choose one of %s", ErrUnknownSection, section,
			textutil.Join(listKeys(), ", "))
	}

	return s.save(ctx, setting(collapsedKey(section), strconv.FormatBool(collapsed)))
}
