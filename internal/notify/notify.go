// Package notify tells the user about the items that newly need them, and keeps a log of
// everything it told.
//
// After each refresh, the inbox reports its arrivals (see inbox.Arrival): pull requests
// that newly ask for the user's review, items newly assigned to them, and snoozes that new
// activity ended early. The Dispatcher records each one in the notifications table, then
// shows a native banner for each kind the user has not turned off, through the desktop
// shell's Poster. More than MaxBanners at once are shown as one banner that sums them up,
// so a morning's backlog does not bury the screen. The log is written whether or not a
// banner is shown, and whether or not the shell can show banners at all, so the window's
// notification history always has every one. Only the newest Keep entries are kept.
//
// Service is what the window binds: the log, marking it read, clearing it, the state of
// the banners, and a test banner. Nothing here leaves the machine: banners are posted to
// the operating system, and the log is a local table.
package notify

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/notify/internal/queries"
	"github.com/mrz1836/nilbox/internal/notifyprefs"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/store"
)

// Kinds of notification. They are the inbox's kinds of arrival, which notifyprefs lists
// for the settings.
const (
	// KindReviewRequested is a pull request that newly asks for the user's review.
	KindReviewRequested = notifyprefs.Kind(inbox.ArrivalReviewRequested)

	// KindAssigned is an issue or pull request newly assigned to the user.
	KindAssigned = notifyprefs.Kind(inbox.ArrivalAssigned)

	// KindWoke is a snoozed item that new activity brought back early.
	KindWoke = notifyprefs.Kind(inbox.ArrivalWoke)
)

// Keep is how many notifications the log keeps; recording more drops the oldest.
const Keep = 200

// Notification is one entry in the log, as the window shows it. Every field is a plain
// value.
type Notification struct {
	ID int64 `json:"id"`

	// Kind is KindReviewRequested, KindAssigned, or KindWoke.
	Kind notifyprefs.Kind `json:"kind"`

	// CreatedAt is when it was recorded, as RFC 3339.
	CreatedAt string `json:"createdAt"`

	// Read reports whether the user has seen it in the history.
	Read bool `json:"read"`

	// ItemID is the item's id in the inbox snapshot, or 0 once the item is no longer
	// stored.
	ItemID int64 `json:"itemId"`

	ItemKind item.Kind `json:"itemKind"`

	// Repo is "owner/name".
	Repo   string `json:"repo"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	Author string `json:"author"`

	// Detail is what ended a snooze early, in a few words ("approved, 2 new commits"), for
	// KindWoke, and "" otherwise.
	Detail string `json:"detail"`
}

// Log is the notification history: the newest first, and how many are unread.
type Log struct {
	Items  []Notification `json:"items"`
	Unread int            `json:"unread"`
}

// Store reads and writes the notification log.
type Store struct {
	db *sql.DB
	q  *queries.Queries
}

// NewStore returns a Store over the pool db.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db, q: queries.New(db)}
}

// Record stores the notifications for arrivals at now, in order, and drops the oldest
// past Keep, in one transaction. It returns what it stored, with the ids.
func (s *Store) Record(ctx context.Context, now time.Time, arrivals []inbox.Arrival) ([]Notification, error) {
	if len(arrivals) == 0 {
		return nil, nil
	}

	now = now.UTC()
	out := make([]Notification, 0, len(arrivals))

	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		q := s.q.WithTx(tx)

		for _, a := range arrivals {
			itemID := a.ItemID

			row, err := q.InsertNotification(ctx, queries.InsertNotificationParams{
				Kind: a.Kind, CreatedAt: now, ItemID: &itemID, ItemKind: string(a.ItemKind), RepoOwner: a.RepoOwner,
				RepoName: a.RepoName, Number: int64(a.Number), Title: a.Title, URL: a.URL, Author: a.Author,
				Detail: WakeDetail(a.Woke),
			})
			if err != nil {
				return err
			}

			out = append(out, fromRow(row))
		}

		return q.TrimNotifications(ctx, Keep)
	})
	if err != nil {
		return nil, fmt.Errorf("record the notifications: %w", err)
	}

	return out, nil
}

// Log returns the stored notifications, newest first, and how many are unread. A
// notification of a kind this build does not know, as another build may have written, is
// left out.
func (s *Store) Log(ctx context.Context) (Log, error) {
	rows, err := s.q.ListNotifications(ctx, Keep)
	if err != nil {
		return Log{}, fmt.Errorf("read the notifications: %w", err)
	}

	out := Log{Items: make([]Notification, 0, len(rows))}

	for _, r := range rows {
		n := fromRow(r)
		if !n.Kind.Valid() {
			continue
		}

		if !n.Read {
			out.Unread++
		}

		out.Items = append(out.Items, n)
	}

	return out, nil
}

// MarkAllRead marks every unread notification read at now.
func (s *Store) MarkAllRead(ctx context.Context, now time.Time) error {
	readAt := now.UTC()
	if err := s.q.MarkAllRead(ctx, &readAt); err != nil {
		return fmt.Errorf("mark the notifications read: %w", err)
	}

	return nil
}

// Clear forgets every notification.
func (s *Store) Clear(ctx context.Context) error {
	if err := s.q.ClearNotifications(ctx); err != nil {
		return fmt.Errorf("clear the notifications: %w", err)
	}

	return nil
}

// fromRow converts a stored notification to the window's.
func fromRow(r queries.Notification) Notification {
	var itemID int64
	if r.ItemID != nil {
		itemID = *r.ItemID
	}

	return Notification{
		ID: r.ID, Kind: notifyprefs.Kind(r.Kind), CreatedAt: clock.Format(r.CreatedAt), Read: r.ReadAt != nil,
		ItemID: itemID, ItemKind: item.Kind(r.ItemKind), Repo: repoName(r.RepoOwner, r.RepoName), Number: int(r.Number),
		Title: r.Title, URL: r.URL, Author: r.Author, Detail: r.Detail,
	}
}

// repoName is "owner/name", or "" when the repository is unknown.
func repoName(owner, name string) string {
	if owner == "" && name == "" {
		return ""
	}

	return repo.Name{Owner: owner, Repo: name}.String()
}
