package inbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox/internal/queries"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/store"
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
// A failure is returned, not recorded as the inbox's last error, which reports whole
// refreshes, and the stored inbox is left as it was.
func (s *Service) RefreshItem(ctx context.Context, id int64) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.refreshItem(ctx, id); err != nil {
		return Snapshot{}, fmt.Errorf("refresh the item: %w", err)
	}

	s.notify()

	return s.Snapshot(ctx)
}

// refreshItem reads the stored item, asks GitHub about it, and stores the answer.
func (s *Service) refreshItem(ctx context.Context, id int64) error {
	stored, err := s.q.GetStoredItem(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w (id %d)", item.ErrNotFound, id)
	}

	if err != nil {
		return fmt.Errorf("read the item: %w", err)
	}

	tok, err := s.tokens.Token(ctx)
	if err != nil {
		return err
	}

	update, err := s.client.FetchItem(ctx, tok, stored.NodeID)
	if err != nil {
		return err
	}

	if update == nil {
		return ErrIncompleteFetch
	}

	files, err := s.checkUpdatedFiles(ctx, tok, id, update)
	if err != nil {
		return github.Redact(err, tok)
	}

	return github.Redact(s.saveItemUpdate(ctx, id, stored.AccountID, update, files), tok)
}

// saveItemUpdate stores what one item's refresh read, in one transaction, with the
// query's budget reading and the item's file check, if one ran. accountID is the account
// the item is stored for.
func (s *Service) saveItemUpdate(
	ctx context.Context, id, accountID int64, update *github.ItemUpdate, files map[string]fileCheck,
) error {
	it := update.Item

	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		w := s.newWriter(tx)
		w.accountID, w.files = accountID, files
		w.details = map[string]github.ItemDetails{it.NodeID: update.Details}

		if err := w.saveGraphQLRate(ctx, update.RateLimit); err != nil {
			return err
		}

		if !update.Found || it.Closed() || it.Repo.IsArchived {
			return w.unlist(ctx, id, update)
		}

		return w.rewrite(ctx, id, it)
	})
	if err != nil {
		return fmt.Errorf("save the item: %w", err)
	}

	return nil
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
