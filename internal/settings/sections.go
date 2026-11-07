package settings

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Inbox sections, named as the inbox snapshot names its lists.
const (
	SectionReviewRequested = "reviewRequested"
	SectionAuthored        = "authored"
	SectionAssigned        = "assigned"
)

// keyCollapsedPrefix starts the key that stores whether an inbox section is collapsed:
// "inbox.collapsed.<section>", holding "true" or "false". Each section has its own key, so
// collapsing one never rewrites another.
const keyCollapsedPrefix = "inbox.collapsed."

// ErrUnknownSection is returned for a section that is not one of the inbox lists.
var ErrUnknownSection = errors.New("unknown inbox section")

// sectionNames returns the inbox sections in the order the inbox shows them.
func sectionNames() []string {
	return []string{SectionReviewRequested, SectionAuthored, SectionAssigned}
}

// CollapsedSections returns the inbox sections the user collapsed, in the order the inbox
// shows them. None is collapsed on a fresh database, and a stored value that is not a
// boolean counts as open.
func (s *Service) CollapsedSections(ctx context.Context) ([]string, error) {
	sections := sectionNames()

	keys := make([]string, len(sections))
	for i, section := range sections {
		keys[i] = keyCollapsedPrefix + section
	}

	var rows []struct {
		Key   string
		Value string
	}

	if err := s.db.WithContext(ctx).Raw("SELECT key, value FROM prefs WHERE key IN ?", keys).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("read the collapsed inbox sections: %w", err)
	}

	collapsed := make(map[string]bool, len(rows))
	for _, r := range rows {
		if v, err := strconv.ParseBool(r.Value); err == nil {
			collapsed[strings.TrimPrefix(r.Key, keyCollapsedPrefix)] = v
		}
	}

	// A non-nil slice, so the UI receives [] rather than null.
	out := make([]string, 0, len(sections))
	for _, section := range sections {
		if collapsed[section] {
			out = append(out, section)
		}
	}

	return out, nil
}

// SetSectionCollapsed stores whether an inbox section is collapsed, so the inbox opens the
// same way at the next launch.
func (s *Service) SetSectionCollapsed(ctx context.Context, section string, collapsed bool) error {
	if !slices.Contains(sectionNames(), section) {
		return fmt.Errorf("%w %q: choose one of %s", ErrUnknownSection, section, strings.Join(sectionNames(), ", "))
	}

	return s.set(ctx, keyCollapsedPrefix+section, strconv.FormatBool(collapsed))
}
