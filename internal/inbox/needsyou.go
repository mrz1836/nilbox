package inbox

import "time"

// Dismissed reports whether the item was dismissed.
func (it Item) Dismissed() bool {
	return it.DismissedAt != ""
}

// Snoozed reports whether the item's snooze is still running at now. A dismissed item is
// not snoozed, whatever its snooze says, and neither is one whose snooze time cannot be
// read.
func (it Item) Snoozed(now time.Time) bool {
	if it.Dismissed() || it.SnoozedUntil == "" {
		return false
	}

	until, err := time.Parse(time.RFC3339, it.SnoozedUntil)

	return err == nil && until.After(now)
}

// NeedsYou reports whether the item is in its lists' own tab at now: neither dismissed
// nor snoozed.
func (it Item) NeedsYou(now time.Time) bool {
	return !it.Dismissed() && !it.Snoozed(now)
}

// Waiting returns the list's items that need the user at now, in the list's order: the
// pinned items first, then the rest by newest activity.
func (l List) Waiting(now time.Time) []Item {
	var out []Item

	for _, it := range l.Items {
		if it.NeedsYou(now) {
			out = append(out, it)
		}
	}

	return out
}

// Lists returns the three lists in the order the inbox shows them.
func (s *Snapshot) Lists() []List {
	return []List{s.ReviewRequested, s.Authored, s.Assigned}
}

// NeedsYou counts the items that need the user at now: those in any list, neither
// dismissed nor snoozed, each once however many lists it is in. It is the count the Inbox
// tab shows (inboxCount in the frontend's lib/triage.ts), so the menu bar and the window
// always agree.
func (s *Snapshot) NeedsYou(now time.Time) int {
	seen := make(map[int64]struct{})

	for _, l := range s.Lists() {
		for _, it := range l.Items {
			if it.NeedsYou(now) {
				seen[it.ID] = struct{}{}
			}
		}
	}

	return len(seen)
}

// NextSnoozeEnd is when the soonest running snooze ends after now, which is when
// NeedsYou next changes without anything being stored, or the zero time when no snooze
// is running.
func (s *Snapshot) NextSnoozeEnd(now time.Time) time.Time {
	var next time.Time

	for _, l := range s.Lists() {
		for _, it := range l.Items {
			if !it.Snoozed(now) {
				continue
			}

			until, _ := time.Parse(time.RFC3339, it.SnoozedUntil) // Snoozed parsed it

			if next.IsZero() || until.Before(next) {
				next = until
			}
		}
	}

	return next
}
