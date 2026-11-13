package inbox

import (
	"context"
	"fmt"
	"time"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox/internal/queries"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/reporules"
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

// Arrival is an item that newly needs the user after a whole refresh: a review request
// or an assignment that was not there at the last refresh, or a snooze that new activity
// ended early. A refresh reports none the first time it stores an account, since
// everything would be new, none for an item the user dismissed or that is still snoozed,
// and none for an item of a repository or org the user muted.
type Arrival struct {
	// Kind is ArrivalReviewRequested, ArrivalAssigned, or ArrivalWoke.
	Kind string

	// ItemID is the item's id in the snapshot.
	ItemID int64

	// ItemKind is the kind of item.
	ItemKind item.Kind

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

// listedFlags are the items in the review-requested and assigned lists before a refresh:
// an item in neither may be in the map, as false.
type listedFlags struct {
	reviewRequested map[int64]bool
	assigned        map[int64]bool
}

// set records the lists the item with id was in.
func (f *listedFlags) set(id int64, reviewRequested, assigned bool) {
	if f.reviewRequested == nil {
		f.reviewRequested, f.assigned = make(map[int64]bool), make(map[int64]bool)
	}

	f.reviewRequested[id], f.assigned[id] = reviewRequested, assigned
}

// arrivalWatch is what a refresh needs to tell arrivals: the lists as they were, when the
// last refresh was, who is signed in, the repositories and orgs the user muted, and which
// of the new lists GitHub truncated. A nil watch tells none.
type arrivalWatch struct {
	before      listedFlags
	lastRefresh time.Time
	viewer      string
	rules       reporules.Rules

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

// listedBefore is what the lists held before a refresh rewrote them, as the last refresh
// of the same account left them: which items were in the review-requested and the
// assigned lists, which arrivals compare with, and, while an outcome sink listens, every
// item listed (rows), which outcomes compare with.
type listedBefore struct {
	flags listedFlags
	rows  []listedRow
}

// readListed reads the lists as they are before the refresh rewrites them with inbox, from
// status, the inbox status stored. It returns nil when another account, or none, was
// refreshed last: this account's lists are then another time's, if it has any. While no
// outcome sink listens, it reads only what arrivals compare with, and only when they
// will.
func (w *writer) readListed(ctx context.Context, status map[string]string, inbox *github.Inbox) (*listedBefore, error) {
	if status[prefViewer] != inbox.Viewer.Login {
		return nil, nil //nolint:nilnil // no lists to compare with is the answer for another account
	}

	before := &listedBefore{}

	if w.tellOutcomes {
		rows, err := w.q.ListListed(ctx, w.accountID)
		if err != nil {
			return nil, fmt.Errorf("read the listed items: %w", err)
		}

		for _, r := range rows {
			before.flags.set(r.ID, r.ReasonReviewRequested, r.ReasonAssigned)
		}

		before.rows = rows

		return before, nil
	}

	if _, refreshed := lastRefresh(status); !refreshed {
		return nil, nil //nolint:nilnil // arrivals compare with nothing on a first refresh, and nothing else reads them
	}

	rows, err := w.q.ListListedFlags(ctx, w.accountID)
	if err != nil {
		return nil, fmt.Errorf("read the listed items: %w", err)
	}

	for _, r := range rows {
		before.flags.set(r.ID, r.ReasonReviewRequested, r.ReasonAssigned)
	}

	return before, nil
}

// watchArrivals is what telling the arrivals of a refresh that stores inbox needs, with
// the lists as they were before it (see readListed) and status, the inbox status stored.
// It returns nil, so no arrival is told, when the account was never refreshed or another
// account was refreshed last: everything in the lists would be new.
func watchArrivals(status map[string]string, inbox *github.Inbox, listed *listedBefore) *arrivalWatch {
	last, refreshed := lastRefresh(status)
	if !refreshed || listed == nil {
		return nil
	}

	return &arrivalWatch{
		before: listed.flags, lastRefresh: last, viewer: inbox.Viewer.Login, rules: reporules.Decode(status),
		reviewTruncated:   truncated(inbox.ReviewRequested),
		assignedTruncated: truncated(inbox.Assigned),
	}
}

// lastRefresh reads when the last successful refresh was, and whether there was one.
func lastRefresh(status map[string]string) (time.Time, bool) {
	at, err := time.Parse(time.RFC3339, status[prefLastRefreshAt])

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

// arrivalList collects arrivals, each item once, under its first kind, leaving out the
// items of the repositories and orgs rules mutes.
type arrivalList struct {
	items []Arrival
	told  map[int64]bool
	rules reporules.Rules
}

// add adds a stored item's arrival, unless the item has arrived already or its repository
// or org is muted.
func (l *arrivalList) add(kind string, s savedEntry, woke snooze.Reasons) {
	it := s.entry.item
	if l.told[s.id] || l.rules.IsMuted(repo.Name{Owner: it.RepoOwner, Repo: it.RepoName}) {
		return
	}

	l.told[s.id] = true
	l.items = append(l.items, Arrival{
		Kind: kind, ItemID: s.id, ItemKind: it.Ref().Kind, RepoOwner: it.RepoOwner, RepoName: it.RepoName,
		Number: it.Number, Title: it.Title, URL: it.URL, Author: it.AuthorLogin, Woke: woke,
	})
}

// arrivals tells what newly needs the user once the refresh's items are stored and its
// snoozes woken: in the order the lists returned them, review requests and assignments
// that were not there before, then the snoozes woken early. Each item arrives once, and an
// item of a muted repository or org never does.
func (w *writer) arrivals(ctx context.Context, watch *arrivalWatch) ([]Arrival, error) {
	if watch == nil {
		return nil, nil
	}

	list := arrivalList{told: make(map[int64]bool), rules: watch.rules}

	for _, s := range w.saved {
		if kind := watch.newKind(s); kind != "" {
			list.add(kind, s, snooze.Reasons{})
		}
	}

	if err := w.dropSetAside(ctx, &list.items); err != nil {
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
func (w *writer) dropSetAside(ctx context.Context, arrivals *[]Arrival) error {
	if len(*arrivals) == 0 {
		return nil
	}

	ids := make([]int64, 0, len(*arrivals))
	for _, a := range *arrivals {
		ids = append(ids, a.ItemID)
	}

	// w.now is in UTC, as every stored time is, so it compares with snoozed_until as text
	// in the same format.
	setAside, err := w.q.ListSetAside(ctx, queries.ListSetAsideParams{Now: new(w.now), Ids: ids})
	if err != nil {
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
