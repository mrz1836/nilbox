package settings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/prefs/prefval"
	"github.com/mrz1836/nilbox/internal/store"
)

// ListKey names an inbox list, as the inbox snapshot names it: each list is a section of
// the inbox the user can collapse.
type ListKey string

// The inbox lists, in the order the inbox shows them.
const (
	ListReviewRequested ListKey = "reviewRequested"
	ListYours           ListKey = "yours"
)

// The lists the inbox showed before Review requested and Yours, whose window keys
// AdoptSections carries over: a former Authored and Assigned list, now one list, Yours.
// They are untyped, so they are no ListKey the window's bindings offer.
const (
	formerAuthored = "authored"
	formerAssigned = "assigned"
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
	return []ListKey{ListReviewRequested, ListYours}
}

// sections is the inbox lists as a choice, for the setters that name one: a list that
// is not one of them is refused with ErrUnknownSection.
func sections() choice[ListKey] {
	return choice[ListKey]{values: listKeys(), invalid: ErrUnknownSection}
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
		if prefval.Bool(stored[collapsedKey(list)], false) {
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
	if err := sections().check(section); err != nil {
		return Settings{}, err
	}

	return s.save(ctx, setting(collapsedKey(section), prefval.EncodeBool(collapsed)))
}

// AdoptSections carries the window's memory of the inbox's former lists, Authored and
// Assigned, over to Yours, which took both their places, in one transaction: for the
// collapsed sections, the status sorts, and the kinds each list shows, Yours takes the
// Assigned list's stored value when it has none of its own (Yours is Assigned with the
// user's own pull requests added, and the user narrowed Assigned to a kind, say, for what
// it holds), and the Authored and Assigned keys are then deleted. Run again, it finds no
// former key and changes nothing, so it runs at every start and settles once per database:
// a start that finds none, as every start after the first does, only reads, and takes no
// write lock.
func AdoptSections(ctx context.Context, pool *sql.DB, c clock.Clock) error {
	p := prefs.New(pool, c)

	former, err := p.GetMany(ctx, formerKeys()...)
	if err != nil {
		return fmt.Errorf("carry the inbox sections over: %w", err)
	}

	if len(former) == 0 {
		return nil
	}

	err = store.InTx(ctx, pool, func(tx *sql.Tx) error {
		inTx := p.WithTx(tx)

		for _, prefix := range formerPrefixes() {
			if adoptErr := adoptSection(ctx, inTx, prefix); adoptErr != nil {
				return adoptErr
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("carry the inbox sections over: %w", err)
	}

	return nil
}

// formerPrefixes start the window keys each inbox list had, which the former lists' keys
// were stored under.
func formerPrefixes() []string {
	return []string{keyCollapsedPrefix, keyStatusSortPrefix, keyListKindPrefix}
}

// formerKeys are every key the former lists were stored under.
func formerKeys() []string {
	keys := make([]string, 0, 2*len(formerPrefixes()))
	for _, prefix := range formerPrefixes() {
		keys = append(keys, prefix+formerAuthored, prefix+formerAssigned)
	}

	return keys
}

// adoptSection carries one window key of the former lists, the one prefix starts, over to
// Yours, and deletes the former lists' keys.
func adoptSection(ctx context.Context, p *prefs.Store, prefix string) error {
	yours, assigned := prefix+string(ListYours), prefix+formerAssigned

	stored, err := p.GetMany(ctx, yours, assigned)
	if err != nil {
		return err
	}

	value, wasAssigned := stored[assigned]
	if _, has := stored[yours]; !has && wasAssigned {
		if err = p.Set(ctx, yours, value); err != nil {
			return err
		}
	}

	for _, former := range []string{formerAuthored, formerAssigned} {
		if err = p.Delete(ctx, prefix+former); err != nil {
			return err
		}
	}

	return nil
}
