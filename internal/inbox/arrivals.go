package inbox

import (
	"context"
	"fmt"
	"time"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/snooze"
)

// Kinds of arrival, in the order one item's arrivals are told: an item that newly asks
// for a review and was assigned in the same refresh arrives once, for the review.
const (
	// ArrivalReviewRequested is a pull request that newly asks for the user's review,
	// including a review requested again after the user reviewed it.
	ArrivalReviewRequested = "review_requested"

	// ArrivalAssigned is an issue or pull request newly assigned to the user by someone
	// else's item. An item the user wrote is not told when it is assigned to them.
	ArrivalAssigned = "assigned"

	// ArrivalWoke is a snoozed item that new activity brought back early, under the
	// user's wake rules.
	ArrivalWoke = "woke"
)

// truncationSlack widens the window in which an item of a truncated list must have moved
// to count as arriving, so a clock a little behind GitHub's does not lose one.
const truncationSlack = 5 * time.Minute

// listedFlagsSQL reads which items were in the review-requested and assigned lists before
// a refresh rewrites the flags.
const listedFlagsSQL = `SELECT id, reason_review_requested, reason_assigned FROM items
WHERE account_id = ? AND (reason_review_requested = 1 OR reason_assigned = 1)`

// setAsideSQL reads which of some items the user has set aside at a time: dismissed, or
// snoozed past it.
const setAsideSQL = `SELECT item_id FROM item_local_state
WHERE item_id IN ? AND (dismissed_at IS NOT NULL OR snoozed_until > ?)`

// Arrival is an item that newly needs the user after a whole refresh: a review request
// or an assignment that was not there at the last refresh, or a snooze that new activity
// ended early. A refresh reports none the first time it stores an account, since
// everything would be new, and none for an item the user dismissed or that is still
// snoozed.
type Arrival struct {
	// Kind is ArrivalReviewRequested, ArrivalAssigned, or ArrivalWoke.
	Kind string

	// ItemID is the item's id in the snapshot.
	ItemID int64

	// ItemKind is "pr" or "issue".
	ItemKind string

	// RepoOwner and RepoName name the item's repository.
	RepoOwner string
	RepoName  string

	Number int
	Title  string
	URL    string
	Author string

	// Woke is the new activity that ended a snooze, for ArrivalWoke.
	Woke snooze.Reasons
}

// ArrivalSink is told about the items that newly need the user, after the refresh that
// found them is stored. It runs on the refresh's goroutine, outside the inbox's lock.
type ArrivalSink interface {
	Arrived(ctx context.Context, arrivals []Arrival)
}

// listedFlags are the items in the review-requested and assigned lists before a refresh.
type listedFlags struct {
	reviewRequested map[int64]bool
	assigned        map[int64]bool
}

// arrivalWatch is what a refresh needs to tell arrivals: the lists as they were, when the
// last refresh was, who is signed in, and which of the new lists GitHub truncated. A nil
// watch tells none.
type arrivalWatch struct {
	before      listedFlags
	lastRefresh time.Time
	viewer      string

	// reviewTruncated and assignedTruncated report that GitHub matched more items for the
	// list than it returned.
	reviewTruncated   bool
	assignedTruncated bool
}

// savedEntry is one item a refresh stored, with its id.
type savedEntry struct {
	id    int64
	entry *entry
}

// watchArrivals reads the lists as they are before the refresh rewrites them with inbox.
// It returns nil, so no arrival is told, when the account was never refreshed or another
// account was refreshed last: everything in the lists would be new.
func (w *writer) watchArrivals(inbox *github.Inbox) (*arrivalWatch, error) {
	prefs, err := readPrefs(w.tx)
	if err != nil {
		return nil, err
	}

	viewer := inbox.Viewer.Login

	last, refreshed := lastRefresh(prefs)
	if !refreshed || prefs[prefViewer] != viewer {
		return nil, nil //nolint:nilnil // no watch is the answer for a first refresh
	}

	var rows []struct {
		ID              int64 `gorm:"column:id"`
		ReviewRequested bool  `gorm:"column:reason_review_requested"`
		Assigned        bool  `gorm:"column:reason_assigned"`
	}

	if err = w.tx.Raw(listedFlagsSQL, w.accountID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("read the listed items: %w", err)
	}

	before := listedFlags{reviewRequested: make(map[int64]bool), assigned: make(map[int64]bool)}

	for _, r := range rows {
		before.reviewRequested[r.ID] = r.ReviewRequested
		before.assigned[r.ID] = r.Assigned
	}

	return &arrivalWatch{
		before: before, lastRefresh: last, viewer: viewer,
		reviewTruncated:   inbox.ReviewRequested.IssueCount > len(inbox.ReviewRequested.Items),
		assignedTruncated: inbox.Assigned.IssueCount > len(inbox.Assigned.Items),
	}, nil
}

// lastRefresh reads when the last successful refresh was, and whether there was one.
func lastRefresh(prefs map[string]string) (time.Time, bool) {
	at, err := time.Parse(time.RFC3339, prefs[prefLastRefreshAt])

	return at, err == nil
}

// newKind is the kind a stored item arrives as for a review request or an assignment that
// was not there before, or "" when it is neither.
//
// A list GitHub truncated (it matched more than it returned) can gain an item only
// because another left, so an item of a truncated list counts only when it moved since
// the last refresh.
func (watch *arrivalWatch) newKind(s savedEntry) string {
	e := s.entry

	switch {
	case e.reviewRequested && !watch.before.reviewRequested[s.id] && watch.moved(e.item, watch.reviewTruncated):
		return ArrivalReviewRequested
	case e.assigned && !watch.before.assigned[s.id] && watch.moved(e.item, watch.assignedTruncated) &&
		e.item.AuthorLogin != watch.viewer:
		return ArrivalAssigned
	default:
		return ""
	}
}

// moved reports whether an item of a list counts as new: always for a whole list, and for
// a truncated one only when the item moved since the last refresh.
func (watch *arrivalWatch) moved(it github.Item, truncated bool) bool {
	return !truncated || it.UpdatedAt.After(watch.lastRefresh.Add(-truncationSlack))
}

// arrivalList collects arrivals, each item once, under its first kind.
type arrivalList struct {
	items []Arrival
	told  map[int64]bool
}

// add adds a stored item's arrival, unless the item has arrived already.
func (l *arrivalList) add(kind string, s savedEntry, woke snooze.Reasons) {
	if l.told[s.id] {
		return
	}

	l.told[s.id] = true
	it := s.entry.item
	l.items = append(l.items, Arrival{
		Kind: kind, ItemID: s.id, ItemKind: kindOf(it), RepoOwner: it.RepoOwner, RepoName: it.RepoName,
		Number: it.Number, Title: it.Title, URL: it.URL, Author: it.AuthorLogin, Woke: woke,
	})
}

// arrivals tells what newly needs the user once the refresh's items are stored and its
// snoozes woken: in the order the lists returned them, review requests and assignments
// that were not there before, then the snoozes woken early. Each item arrives once.
func (w *writer) arrivals(watch *arrivalWatch) ([]Arrival, error) {
	if watch == nil {
		return nil, nil
	}

	list := arrivalList{told: make(map[int64]bool)}

	for _, s := range w.saved {
		if kind := watch.newKind(s); kind != "" {
			list.add(kind, s, snooze.Reasons{})
		}
	}

	if err := w.dropSetAside(&list.items); err != nil {
		return nil, err
	}

	for _, s := range w.saved {
		if reasons, ok := w.woke[s.id]; ok {
			list.add(ArrivalWoke, s, reasons)
		}
	}

	return list.items, nil
}

// dropSetAside removes the arrivals of items the user dismissed or that are still
// snoozed after the refresh's wake-ups.
func (w *writer) dropSetAside(arrivals *[]Arrival) error {
	if len(*arrivals) == 0 {
		return nil
	}

	ids := make([]int64, 0, len(*arrivals))
	for _, a := range *arrivals {
		ids = append(ids, a.ItemID)
	}

	var setAside []int64
	if err := w.tx.Raw(setAsideSQL, ids, w.now).Scan(&setAside).Error; err != nil {
		return fmt.Errorf("read the triage of the new items: %w", err)
	}

	if len(setAside) == 0 {
		return nil
	}

	hidden := make(map[int64]bool, len(setAside))
	for _, id := range setAside {
		hidden[id] = true
	}

	kept := (*arrivals)[:0]

	for _, a := range *arrivals {
		if !hidden[a.ItemID] {
			kept = append(kept, a)
		}
	}

	*arrivals = kept

	return nil
}

// announce tells the sink about arrivals, if there are any and a sink.
func (s *Service) announce(ctx context.Context, arrivals []Arrival) {
	if s.arrivals != nil && len(arrivals) > 0 {
		s.arrivals.Arrived(ctx, arrivals)
	}
}
