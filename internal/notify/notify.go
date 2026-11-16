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
// Some notifications are about no item: a release whose tag reached GitHub, for one, or a
// run from the Scripts tab that failed, which names the run a click on it opens. The
// Dispatcher's Notify records such an Entry and shows its banner the same way. Autopilot's
// notices are Entries too, of its own kinds (notifyprefs.AutopilotKinds): about the item a
// routine matched, or about none for a problem, headed by the routine's name for an alert,
// and naming what the Autopilot tab opens at (event.AutopilotTarget), with a banner the
// routine may turn on or off over the settings' switch (Entry.Banner).
//
// Service is what the window binds: the log, marking entries read or unread, clearing it,
// the state of the banners, and a test banner. Opening the history marks nothing: an entry
// is marked read when the user opens it, clicks its banner, opens its item's sheet, or
// marks it done, and marked unread again only by the user. Nothing here leaves the machine: banners are posted to
// the operating system, and the log is a local table.
package notify

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/notify/internal/queries"
	"github.com/mrz1836/nilbox/internal/notifyprefs"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/store"
)

// Kinds of notification, which notifyprefs lists for the settings: the inbox's kinds of
// arrival, and the kinds about no item.
const (
	// KindReviewRequested is a pull request that newly asks for the user's review.
	KindReviewRequested = notifyprefs.Kind(inbox.ArrivalReviewRequested)

	// KindAssigned is an issue or pull request newly assigned to the user.
	KindAssigned = notifyprefs.Kind(inbox.ArrivalAssigned)

	// KindWoke is a snoozed item that new activity brought back early.
	KindWoke = notifyprefs.Kind(inbox.ArrivalWoke)

	// KindReleaseDone is a release whose tag reached GitHub. It is about no item.
	KindReleaseDone = notifyprefs.KindReleaseDone

	// KindReleasePublished is a release its workflow on GitHub published, and
	// KindReleaseFailed one whose workflow failed. Both are about no item.
	KindReleasePublished = notifyprefs.KindReleasePublished
	KindReleaseFailed    = notifyprefs.KindReleaseFailed

	// KindRunFailed is a run from the Scripts tab that failed or ran past its time limit. It
	// is about no item, and names the run.
	KindRunFailed = notifyprefs.KindRunFailed

	// KindServiceStatus is a part of a service the user watches going down, or coming back.
	// It is about no item, and opens the incident or the status page.
	KindServiceStatus = notifyprefs.KindServiceStatus

	// KindUpdateReady is a new version of the app, ready to restart into, or available. It
	// is about no item, and opens nothing: a click brings the window forward, where the
	// update's banner shows.
	KindUpdateReady = notifyprefs.KindUpdateReady

	// KindAutopilotAlert is a routine's Notify me, or its Tell me: headed by the routine's
	// name, about the item it matched.
	KindAutopilotAlert = notifyprefs.KindAutopilotAlert

	// KindAutopilotNeedsYou is a routine's match waiting for the user's decision.
	KindAutopilotNeedsYou = notifyprefs.KindAutopilotNeedsYou

	// KindAutopilotFinished is a routine's agent run in the background that ended.
	KindAutopilotFinished = notifyprefs.KindAutopilotFinished

	// KindAutopilotChecks is the verdict on the checks after a routine's fix.
	KindAutopilotChecks = notifyprefs.KindAutopilotChecks

	// KindAutopilotProblem is something wrong with a routine, which may be about no item.
	KindAutopilotProblem = notifyprefs.KindAutopilotProblem
)

// Keep is how many notifications the log keeps; recording more drops the oldest.
const Keep = 1000

// Notification is one entry in the log, as the window shows it. Every field is a plain
// value.
type Notification struct {
	ID int64 `json:"id"`

	// Kind is KindReviewRequested, KindAssigned, KindWoke, one of a release's kinds
	// (KindReleaseDone, KindReleasePublished, or KindReleaseFailed), KindRunFailed,
	// KindServiceStatus, KindUpdateReady, or one of Autopilot's (notifyprefs.AutopilotKinds).
	Kind notifyprefs.Kind `json:"kind"`

	// CreatedAt is when it was recorded, as RFC 3339.
	CreatedAt string `json:"createdAt"`

	// Read reports whether the user is done with it: opened it, clicked its banner, opened
	// its item, or marked it done. Opening the history marks nothing.
	Read bool `json:"read"`

	// ItemID is the item's id in the inbox snapshot, or 0 once the item is no longer
	// stored, or for an entry about no item.
	ItemID int64 `json:"itemId"`

	// ItemKind is the item's kind, or "" for an entry about no item (an Entry), such as a
	// release: its Number and Author are then 0 and "".
	ItemKind item.Kind `json:"itemKind"`

	// Repo is "owner/name", or "" when the repository is unknown. Title is the item's
	// title, or an entry's own sentence ("v1.4.3 is on GitHub"), and URL the page a click
	// opens: the item, or the entry's.
	Repo   string `json:"repo"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	Author string `json:"author"`

	// Detail is what ended a snooze early, in a few words ("approved, 2 new commits"), for
	// KindWoke; an entry's body ("Tagged a1b2c3d on master."); and "" otherwise.
	Detail string `json:"detail"`

	// RunID is the run an entry is about, which a click on it opens in Scripts → Runs, or 0
	// for none: every entry but a KindRunFailed one. RunStarted is when that run started, as
	// RFC 3339, or "": the window opens the run only while the run with that id started then,
	// since the history hands an id out again once it is cleared.
	RunID      int64  `json:"runId"`
	RunStarted string `json:"runStarted"`

	// Heading heads the entry in place of its kind's title, as an Autopilot alert is headed
	// by its routine's name, or "" for the kind's own.
	Heading string `json:"heading"`

	// Autopilot is what an Autopilot notification opens in the Autopilot tab, or the zero
	// target for every other: the window opens it only while the one with its ID was
	// created at its CreatedAt.
	Autopilot event.AutopilotTarget `json:"autopilot"`

	// banner is the Entry's own choice of banner, which the stored entry doesn't keep: it is
	// shown, or not, as it is recorded.
	banner *bool
}

// Entry is a notification recorded on its own rather than as an inbox arrival: one about
// no item, as a release whose tag reached GitHub or a run that failed, or one of
// Autopilot's, which may be about an item. RecordEvent stores it.
type Entry struct {
	// Kind is the kind of notification, one of notifyprefs.Kinds.
	Kind notifyprefs.Kind

	// Heading heads it and its banner in place of its kind's title, as an Autopilot alert
	// is headed by its routine's name, or "" for the kind's own.
	Heading string

	// Title is its sentence ("v1.4.3 is on GitHub"), or its item's title, Body what follows
	// it, which may be "", and URL the page a click on it opens, which may be "".
	Title string
	Body  string
	URL   string

	// RepoOwner and RepoName are the repository it is about, or "" for none.
	RepoOwner string
	RepoName  string

	// ItemID is the stored item it is about, which a click shows and opening whose sheet
	// marks it read, or 0 for none; ItemKind and Number name that item ("" and 0 for
	// none), so its banner says "example-repo #12".
	ItemID   int64
	ItemKind item.Kind
	Number   int

	// RunID is the run it is about, which a click on it opens, or 0 for none, and RunStarted
	// when that run started, as RFC 3339, which tells it from a later run given its id.
	RunID      int64
	RunStarted string

	// Autopilot is what it opens in the Autopilot tab, for one of Autopilot's kinds; the
	// zero target for none.
	Autopilot event.AutopilotTarget

	// Banner is its own choice of banner, over Settings → Notifications': true shows one
	// whatever its kind's switch says, false shows none, and nil follows the switch, as
	// every entry but an Autopilot routine's does. The user's allowing banners holds either
	// way.
	Banner *bool
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

	// keep is how many notifications the log keeps: Keep, or fewer in tests that need
	// to run past the cap with few writes.
	keep int64
}

// NewStore returns a Store over the pool db.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db, q: queries.New(db), keep: Keep}
}

// KeptItems returns each inbox item a notification is about, once, which the inbox's
// prune of old items keeps while the log names it (inbox.ItemKeeper), so an entry still
// opens its item's sheet.
func (s *Store) KeptItems(ctx context.Context) ([]int64, error) {
	rows, err := s.q.ListNotifiedItems(ctx)
	if err != nil {
		return nil, fmt.Errorf("read the notifications' items: %w", err)
	}

	ids := make([]int64, 0, len(rows))

	for _, id := range rows {
		if id != nil {
			ids = append(ids, *id)
		}
	}

	return ids, nil
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
			row, err := q.InsertNotification(ctx, queries.InsertNotificationParams{
				Kind: a.Kind, CreatedAt: now, ItemID: new(a.ItemID), ItemKind: string(a.ItemKind), RepoOwner: a.RepoOwner,
				RepoName: a.RepoName, Number: int64(a.Number), Title: a.Title, URL: a.URL, Author: a.Author,
				Detail: WakeDetail(a.Woke),
			})
			if err != nil {
				return err
			}

			out = append(out, fromRow(row))
		}

		return q.TrimNotifications(ctx, s.keep)
	})
	if err != nil {
		return nil, fmt.Errorf("record the notifications: %w", err)
	}

	return out, nil
}

// RecordEvent stores entry, a notification recorded on its own, at now, and drops the
// oldest past Keep, in one transaction. It returns what it stored, with its id. A kind
// that isn't one of notifyprefs.Kinds, or an item's kind that isn't one, is refused, and
// nothing is stored. An item no longer stored is recorded without its id, as the log
// forgets an item that goes, its kind and number kept for the entry's line.
func (s *Store) RecordEvent(ctx context.Context, now time.Time, entry Entry) (Notification, error) {
	if !entry.Kind.Valid() {
		return Notification{}, fmt.Errorf("record the notification: %w %q", notifyprefs.ErrUnknownKind, entry.Kind)
	}

	if entry.ItemKind != "" && !entry.ItemKind.Valid() {
		return Notification{}, fmt.Errorf("record the notification: %w %q", ErrUnknownItemKind, entry.ItemKind)
	}

	out, err := s.recordEvent(ctx, now, entry)
	if err != nil && entry.ItemID != 0 && store.IsForeignKeyViolation(err) {
		entry.ItemID = 0
		out, err = s.recordEvent(ctx, now, entry)
	}

	if err != nil {
		return Notification{}, fmt.Errorf("record the notification: %w", err)
	}

	out.banner = entry.Banner

	return out, nil
}

// ErrUnknownItemKind is returned for an entry about an item of a kind that is neither a
// pull request nor an issue.
var ErrUnknownItemKind = errors.New("unknown kind of item")

// Log returns the stored notifications, newest first, and how many are unread. A
// notification of a kind this build does not know, as another build may have written, is
// left out.
func (s *Store) Log(ctx context.Context) (Log, error) {
	rows, err := s.q.ListNotifications(ctx, s.keep)
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
	if err := s.q.MarkAllRead(ctx, new(now.UTC())); err != nil {
		return fmt.Errorf("mark the notifications read: %w", err)
	}

	return nil
}

// MarkRead marks the unread notifications among ids read at now, and reports how many it
// marked. An id the log doesn't hold, or one already read, is left alone.
func (s *Store) MarkRead(ctx context.Context, now time.Time, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	n, err := s.q.MarkRead(ctx, queries.MarkReadParams{ReadAt: new(now.UTC()), Ids: ids})
	if err != nil {
		return 0, fmt.Errorf("mark the notifications read: %w", err)
	}

	return n, nil
}

// MarkUnread marks the read notifications among ids unread again, and reports how many it
// marked. An id the log doesn't hold, or one already unread, is left alone.
func (s *Store) MarkUnread(ctx context.Context, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	n, err := s.q.MarkUnread(ctx, ids)
	if err != nil {
		return 0, fmt.Errorf("mark the notifications unread: %w", err)
	}

	return n, nil
}

// Clear forgets every notification.
func (s *Store) Clear(ctx context.Context) error {
	if err := s.q.ClearNotifications(ctx); err != nil {
		return fmt.Errorf("clear the notifications: %w", err)
	}

	return nil
}

// recordEvent stores entry at now and trims the log, in one transaction.
func (s *Store) recordEvent(ctx context.Context, now time.Time, entry Entry) (Notification, error) {
	var out Notification

	var itemID *int64
	if entry.ItemID != 0 {
		itemID = new(entry.ItemID)
	}

	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		q := s.q.WithTx(tx)

		row, err := q.InsertNotification(ctx, queries.InsertNotificationParams{
			Kind: string(entry.Kind), CreatedAt: now.UTC(), ItemID: itemID, ItemKind: string(entry.ItemKind),
			RepoOwner: entry.RepoOwner, RepoName: entry.RepoName, Number: int64(max(entry.Number, 0)),
			Title: entry.Title, URL: entry.URL, Detail: entry.Body, RunID: entry.RunID,
			RunStarted: entry.RunStarted, Heading: entry.Heading, AutopilotRoutine: entry.Autopilot.RoutineID,
			AutopilotMatch: entry.Autopilot.MatchID, AutopilotRun: entry.Autopilot.RunID,
			AutopilotCreated: entry.Autopilot.CreatedAt,
		})
		if err != nil {
			return err
		}

		out = fromRow(row)

		return q.TrimNotifications(ctx, s.keep)
	})

	return out, err
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
		Title: r.Title, URL: r.URL, Author: r.Author, Detail: r.Detail, RunID: r.RunID,
		RunStarted: r.RunStarted, Heading: r.Heading,
		Autopilot: event.AutopilotTarget{
			RoutineID: r.AutopilotRoutine, MatchID: r.AutopilotMatch, RunID: r.AutopilotRun, CreatedAt: r.AutopilotCreated,
		},
	}
}

// repoName is "owner/name", or "" when the repository is unknown.
func repoName(owner, name string) string {
	if owner == "" && name == "" {
		return ""
	}

	return repo.Name{Owner: owner, Repo: name}.String()
}
