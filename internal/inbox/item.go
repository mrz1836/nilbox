package inbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox/internal/queries"
	"github.com/mrz1836/nilbox/internal/item"
)

// RefreshItem brings one item up to date from GitHub and returns the new snapshot. It
// asks about that item alone, for one GraphQL point, so a change the user knows of, such
// as a pull request they just merged, shows without refreshing the whole inbox.
//
// An item still open gets its card and details rewritten and stays in the lists it is
// in; only the searches of a whole refresh can tell which lists those are, with one
// exception: a pull request in Review requested that GitHub no longer asks the user to
// review, by name or through a team, leaves that list (see leaveAnsweredReview), as once
// the user, or an agent they launched, reviewed it, and it tells no outcome for that. An
// issue's projects are read when the last whole refresh could read them; only a whole
// refresh changes what the snapshot says about that. A pull request
// whose files are checked has them checked again when its head moved. An item
// merged, closed, or deleted since, or whose repository was archived, leaves every list,
// as a whole refresh would have it, and a merge has the repository's other open pull
// requests on its base read again (see noteMerged). New activity on a snoozed item can end
// the snooze.
//
// It never waits for a whole refresh, only for other work on the same item. A whole
// refresh that started earlier and stores later keeps what this one stored; one that
// started later and already stored the item makes this one store nothing.
//
// A failure is returned, not recorded as the inbox's last error, which reports whole
// refreshes, and the stored inbox is left as it was.
//
// Once it has let go of the item, it tells the outcome sink of a merge or a close the
// answer it stored shows, and the Changed seam that it stored the item.
func (s *Service) RefreshItem(ctx context.Context, id int64) (Snapshot, error) {
	snap, told, wrote, err := s.refreshItemLocked(ctx, id)
	s.settle(ctx, told)

	if wrote {
		s.tellChanged([]int64{id})
	}

	return snap, err
}

// refreshItemLocked is RefreshItem under the item's lock. It returns what to tell once the
// lock is let go, and whether it stored the item.
func (s *Service) refreshItemLocked(ctx context.Context, id int64) (Snapshot, []Outcome, bool, error) {
	unlock, err := s.items.lock(ctx, id)
	if err != nil {
		return Snapshot{}, nil, false, fmt.Errorf("refresh the item: %w", err)
	}

	defer unlock()

	told, wrote, err := s.refreshItem(ctx, id)
	if err != nil {
		return Snapshot{}, nil, false, fmt.Errorf("refresh the item: %w", err)
	}

	if !wrote {
		snap, snapErr := s.Snapshot(ctx)

		return snap, nil, false, snapErr
	}

	snap, err := s.changed(ctx)

	return snap, told, true, err
}

// refreshItem reads the stored item, asks GitHub about it, and stores the answer. It
// reports whether it stored it, and what the answer stored tells (see seenClosed).
func (s *Service) refreshItem(ctx context.Context, id int64) ([]Outcome, bool, error) {
	stored, err := s.q.GetStoredItem(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, fmt.Errorf("%w (id %d)", item.ErrNotFound, id)
	}

	if err != nil {
		return nil, false, fmt.Errorf("read the item: %w", err)
	}

	last, err := s.projectAccess(ctx)
	if err != nil {
		return nil, false, err
	}

	var seq uint64

	update, err := github.WithToken(ctx, s.tokens, func(tok github.Token) (itemUpdate, error) {
		seq = s.startFetch() // each try numbers its own fetch

		got, fetchErr := s.fetchItem(ctx, tok, id, stored.NodeID, readOptions(last))
		got.viewer, _ = s.viewers.Get(tok)

		return got, fetchErr
	})
	if err != nil {
		return nil, false, err
	}

	update.accountID, update.login = stored.AccountID, stored.AccountLogin
	w := itemWrite{id: id, seq: seq, update: &update}

	did, err := s.storeItem(ctx, w)
	if err != nil {
		return nil, false, fmt.Errorf("save the item: %w", err)
	}

	if !did.stored {
		return nil, false, nil
	}

	ref := refOf(stored.RepoOwner, stored.RepoName, stored.Number, item.Kind(stored.Kind))
	s.noteMergedRead(w, ref)

	told := reviewedOutcome(did.reviewed, stored.AccountLogin, ref)

	return append(told, seenClosed(w, stored.AccountLogin, ref, s.now())...), true, nil
}

// fetchItem asks GitHub about the item with id and nodeID, with what opts adds to its
// details, and checks its files when a check is due.
func (s *Service) fetchItem(
	ctx context.Context, tok github.Token, id int64, nodeID string, opts github.DetailOptions,
) (itemUpdate, error) {
	update, err := s.client.FetchItem(ctx, tok, nodeID, opts)
	if err != nil {
		return itemUpdate{}, err
	}

	if update == nil {
		return itemUpdate{}, ErrIncompleteFetch
	}

	files, err := s.checkUpdatedFiles(ctx, tok, id, update)
	if err != nil {
		return itemUpdate{}, err
	}

	return itemUpdate{update: update, files: files}, nil
}

// rewrite stores what GitHub says about an open item in place, with its details and file
// check, takes a pull request GitHub no longer asks the account to review out of Review
// requested (see leaveAnsweredReview), stores the user's verdict when the read brought it
// (see noteReadVerdict), then ends the snoozes the new details give cause to.
func (w *writer) rewrite(ctx context.Context, id int64, it github.Item) error {
	err := w.q.UpdateItem(ctx, queries.UpdateItemParams{
		NodeID: it.NodeID, Title: it.Title, URL: it.URL, AuthorLogin: it.AuthorLogin, AuthorType: it.AuthorType,
		AuthorAvatarURL: it.AuthorAvatarURL, AuthorAssociation: it.AuthorAssociation,
		IsCrossRepository: it.IsCrossRepository, IsDraft: it.IsDraft,
		HeadRef: it.HeadRef, ItemCreatedAt: it.CreatedAt.UTC(), ItemUpdatedAt: it.UpdatedAt.UTC(),
		State: it.State, ClosedAt: timeOrNil(it.ClosedAt), FetchedAt: w.now, ID: id,
	})
	if err != nil {
		return fmt.Errorf("update the item: %w", err)
	}

	if err = w.saveDetails(ctx, id, it); err != nil {
		return err
	}

	if err = w.leaveAnsweredReview(ctx, id, it); err != nil {
		return err
	}

	if w.verdict {
		if err = w.noteReadVerdict(ctx, id, it); err != nil {
			return err
		}
	}

	if err = w.saveFiles(ctx, id, it); err != nil {
		return err
	}

	return w.wakeSnoozed(ctx)
}

// leaveAnsweredReview takes the pull request with id out of the Review requested list when
// the details read with it say GitHub no longer asks the writer's account to review it (see
// answeredReview), as once the user, or an agent they launched, reviewed it. It is stamped
// with GitHub's time for the pull request as read, as the inbox's own edits are (see
// UpsertItem), so a whole refresh whose search still lists it, as GitHub's index trails
// the review, keeps it out and tells no arrival, until it moves: a review asked again then
// arrives. An item not in the list is left alone. It tells no outcome, as LeaveReview
// tells none of a list left.
func (w *writer) leaveAnsweredReview(ctx context.Context, id int64, it github.Item) error {
	d, ok := w.details[it.NodeID]
	if !ok || !answeredReview(it, d, w.login) {
		return nil
	}

	stamp := it.UpdatedAt.UTC()
	if stamp.IsZero() {
		stamp = w.now
	}

	err := w.q.LeaveAnsweredReview(ctx, queries.LeaveAnsweredReviewParams{ReviewLeftAt: &stamp, ID: id})
	if err != nil {
		return fmt.Errorf("take %s out of Review requested: %w", it.Ref(), err)
	}

	return nil
}

// answeredReview reports whether d, read with the pull request it, says GitHub no longer
// asks login to review it: not by name, and through no team, since login may be on one, as
// RemoveReviewRequest's answer counts it. A read that can't tell keeps the pull request
// where it is: no login, an issue, request lists the read didn't bring (nil), or a list as
// long as the read lists (MaxReviewRequests), which may have left login out.
func answeredReview(it github.Item, d github.ItemDetails, login string) bool {
	r := d.ReviewRequests

	switch {
	case !it.IsPR || login == "":
		return false
	case r.Users == nil || r.Teams == nil:
		return false
	case len(r.Teams) > 0 || len(r.Users) >= github.MaxReviewRequests:
		return false
	default:
		return !r.Asks(login)
	}
}

// unlistRead takes an item a read found gone, closed, or archived out of the lists (see
// unlist), and stores the user's verdict on it when the read brought it, since a pull
// request approved and merged in one go was still reviewed first.
func (w *writer) unlistRead(ctx context.Context, id int64, update *github.ItemUpdate) error {
	if err := w.unlist(ctx, id, update); err != nil {
		return err
	}

	if !w.verdict || !update.Found {
		return nil
	}

	return w.noteReadVerdict(ctx, id, update.Item)
}

// unlistActed takes an item the user just merged or closed out of every list, with the
// state the action left it in (state, "" when it didn't say), and out of every routine's
// search, which asks for open items only.
func (w *writer) unlistActed(ctx context.Context, id int64, state string) error {
	err := w.q.UnlistItem(ctx, queries.UnlistItemParams{
		State: state, ClosedAt: stampIf(state != "", w.now), FetchedAt: w.now, ID: id,
	})
	if err != nil {
		return fmt.Errorf("take the merged or closed item out of the lists: %w", err)
	}

	if err = w.q.DeleteItemWatches(ctx, id); err != nil {
		return fmt.Errorf("take the merged or closed item out of the routines' searches: %w", err)
	}

	return nil
}

// unlist takes an item out of every list. An item in an archived repository records the
// repository first, so it is known as archived, as a whole refresh does. An item the read
// found merged, closed, or gone records its state, and leaves every routine's search too,
// since they ask for open items only.
func (w *writer) unlist(ctx context.Context, id int64, update *github.ItemUpdate) error {
	if update.Found && update.Item.Repo.IsArchived {
		if _, err := w.repoID(ctx, update.Item); err != nil {
			return err
		}
	}

	state := ""
	if update.Found {
		state = update.Item.State
	}

	err := w.q.UnlistItem(ctx, queries.UnlistItemParams{
		State: state, ClosedAt: timeOrNil(update.Item.ClosedAt), FetchedAt: w.now, ID: id,
	})
	if err != nil {
		return fmt.Errorf("take the item out of the lists: %w", err)
	}

	if !update.Found || update.Item.Closed() {
		if err = w.q.DeleteItemWatches(ctx, id); err != nil {
			return fmt.Errorf("take the item out of the routines' searches: %w", err)
		}
	}

	return nil
}
