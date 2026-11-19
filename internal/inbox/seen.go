package inbox

import (
	"context"
	"fmt"

	"github.com/mrz1836/nilbox/internal/inbox/internal/queries"
)

// The user's seen marks. An item is unread while the activity the user last saw on it
// (items.seen_activity_at) is unset or older than its last activity on GitHub (see
// Item.Unread). Apart from a new item's insert (see 0101_inbox), only these marks
// move it: the window sends them as an item's sheet opens, steps away, or closes, as the
// user opens it on GitHub, after a card's own edits on GitHub, and from Mark as read and
// Mark as unread, whatever the cards' unread setting says. The window, not the Actor,
// marks the user's own actions, since the Actor is shared with Autopilot and most of its
// actions record no time of GitHub's.
//
// A mark that changed an item empties the kept snapshot, so the next read shows it, but
// tells no one the inbox changed: the window changes the card itself, and InboxUpdated
// would wake Autopilot's engine on every sheet opening. Nothing is written to GitHub.

// MarkSeen records that the user saw the items with ids: each one's seen stamp becomes the
// later of its last activity and now, so a read that lands after the mark with activity
// from before it leaves the item seen. Only the signed-in account's items are marked; an
// id with no stored item, or another account's, is left alone.
func (s *Service) MarkSeen(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}

	accountID, err := s.signedIn(ctx)
	if err != nil || accountID == 0 {
		return err
	}

	n, err := s.q.MarkSeen(ctx, queries.MarkSeenParams{Now: s.now(), AccountID: accountID, Ids: ids})
	if err != nil {
		return fmt.Errorf("mark the items read: %w", err)
	}

	s.marked(n)

	return nil
}

// MarkUnseen forgets that the user saw the item with id, so it reads unread until they see
// it again. Only the signed-in account's item is marked; an id with no stored item, or
// another account's, is left alone.
func (s *Service) MarkUnseen(ctx context.Context, id int64) error {
	accountID, err := s.signedIn(ctx)
	if err != nil || accountID == 0 {
		return err
	}

	n, err := s.q.MarkUnseen(ctx, queries.MarkUnseenParams{AccountID: accountID, ID: id})
	if err != nil {
		return fmt.Errorf("mark the item unread: %w", err)
	}

	s.marked(n)

	return nil
}

// signedIn returns the id of the signed-in account, as the last refresh stored it, or 0
// before any.
func (s *Service) signedIn(ctx context.Context) (int64, error) {
	login, _, err := s.prefs.Get(ctx, prefViewer)
	if err != nil {
		return 0, fmt.Errorf("read the signed-in account: %w", err)
	}

	return lookupAccount(ctx, s.q, login)
}

// marked empties the kept snapshot once a mark changed n items, so the next read shows
// them, and tells no one.
func (s *Service) marked(n int64) {
	if n > 0 {
		s.snapshots.empty()
	}
}
