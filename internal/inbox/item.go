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
// in; only the searches of a whole refresh can tell which lists those are. A pull request
// whose files are checked has them checked again when its head moved. An item
// merged, closed, or deleted since, or whose repository was archived, leaves every list,
// as a whole refresh would have it. New activity on a snoozed item can end the snooze.
//
// It never waits for a whole refresh, only for other work on the same item. A whole
// refresh that started earlier and stores later keeps what this one stored; one that
// started later and already stored the item makes this one store nothing.
//
// A failure is returned, not recorded as the inbox's last error, which reports whole
// refreshes, and the stored inbox is left as it was.
func (s *Service) RefreshItem(ctx context.Context, id int64) (Snapshot, error) {
	unlock, err := s.items.lock(ctx, id)
	if err != nil {
		return Snapshot{}, fmt.Errorf("refresh the item: %w", err)
	}

	defer unlock()

	wrote, err := s.refreshItem(ctx, id)
	if err != nil {
		return Snapshot{}, fmt.Errorf("refresh the item: %w", err)
	}

	if !wrote {
		return s.Snapshot(ctx)
	}

	return s.changed(ctx)
}

// refreshItem reads the stored item, asks GitHub about it, and stores the answer. It
// reports whether it stored it.
func (s *Service) refreshItem(ctx context.Context, id int64) (bool, error) {
	stored, err := s.q.GetStoredItem(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("%w (id %d)", item.ErrNotFound, id)
	}

	if err != nil {
		return false, fmt.Errorf("read the item: %w", err)
	}

	var seq uint64

	update, err := github.WithToken(ctx, s.tokens, func(tok github.Token) (itemUpdate, error) {
		seq = s.startFetch() // each try numbers its own fetch

		return s.fetchItem(ctx, tok, id, stored.NodeID)
	})
	if err != nil {
		return false, err
	}

	update.accountID = stored.AccountID

	wrote, err := s.storeItemWrite(ctx, itemWrite{id: id, seq: seq, update: &update})
	if err != nil {
		return false, fmt.Errorf("save the item: %w", err)
	}

	return wrote, nil
}

// fetchItem asks GitHub about the item with id and nodeID, and checks its files when a
// check is due.
func (s *Service) fetchItem(ctx context.Context, tok github.Token, id int64, nodeID string) (itemUpdate, error) {
	update, err := s.client.FetchItem(ctx, tok, nodeID)
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
// check, then ends the snoozes the new details give cause to.
func (w *writer) rewrite(ctx context.Context, id int64, it github.Item) error {
	err := w.q.UpdateItem(ctx, queries.UpdateItemParams{
		NodeID: it.NodeID, Title: it.Title, URL: it.URL, AuthorLogin: it.AuthorLogin, AuthorType: it.AuthorType,
		AuthorAssociation: it.AuthorAssociation, IsCrossRepository: it.IsCrossRepository, IsDraft: it.IsDraft,
		HeadRef: it.HeadRef, ItemCreatedAt: it.CreatedAt.UTC(), ItemUpdatedAt: it.UpdatedAt.UTC(),
		FetchedAt: w.now, ID: id,
	})
	if err != nil {
		return fmt.Errorf("update the item: %w", err)
	}

	if err = w.saveDetails(ctx, id, it); err != nil {
		return err
	}

	if err = w.saveFiles(ctx, id, it); err != nil {
		return err
	}

	return w.wakeSnoozed(ctx)
}

// unlist takes an item out of every list. An item in an archived repository records the
// repository first, so it is known as archived, as a whole refresh does.
func (w *writer) unlist(ctx context.Context, id int64, update *github.ItemUpdate) error {
	if update.Found && update.Item.Repo.IsArchived {
		if _, err := w.repoID(ctx, update.Item); err != nil {
			return err
		}
	}

	if err := w.q.UnlistItem(ctx, queries.UnlistItemParams{FetchedAt: w.now, ID: id}); err != nil {
		return fmt.Errorf("take the item out of the lists: %w", err)
	}

	return nil
}
