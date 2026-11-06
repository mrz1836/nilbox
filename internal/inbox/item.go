package inbox

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/mrz1836/nilbox/internal/github"
)

// storedItemSQL reads what a single refresh needs about a stored item. An item without a
// node ID cannot be asked about on its own, so it reads as not found.
const storedItemSQL = `SELECT account_id, node_id FROM items WHERE id = ? AND node_id <> ''`

// updateItemSQL rewrites what GitHub says about a stored item, in place. Its repository,
// number, and lists stay as they are.
const updateItemSQL = `UPDATE items SET
    node_id = ?, title = ?, url = ?, author_login = ?, author_type = ?, author_association = ?,
    is_cross_repository = ?, is_draft = ?, head_ref = ?, item_created_at = ?, item_updated_at = ?, fetched_at = ?
WHERE id = ?`

// unlistItemSQL takes an item out of every list, as a refresh does with an item no
// search returns. The row stays, since local state may reference it.
const unlistItemSQL = `UPDATE items
SET reason_review_requested = 0, reason_authored = 0, reason_assigned = 0, fetched_at = ?
WHERE id = ?`

// storedItem is a stored item as storedItemSQL reads it.
type storedItem struct {
	AccountID int64  `gorm:"column:account_id"`
	NodeID    string `gorm:"column:node_id"`
}

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
	var rows []storedItem
	if err := s.db.WithContext(ctx).Raw(storedItemSQL, id).Scan(&rows).Error; err != nil {
		return fmt.Errorf("read the item: %w", err)
	}

	if len(rows) == 0 {
		return fmt.Errorf("%w (id %d)", ErrItemNotFound, id)
	}

	tok, err := s.tokens.Token(ctx)
	if err != nil {
		return err
	}

	update, err := s.client.FetchItem(ctx, tok, rows[0].NodeID)
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

	return github.Redact(s.saveItemUpdate(ctx, id, rows[0], update, files), tok)
}

// saveItemUpdate stores what one item's refresh read, in one transaction, with the
// query's budget reading and the item's file check, if one ran.
func (s *Service) saveItemUpdate(
	ctx context.Context, id int64, stored storedItem, update *github.ItemUpdate, files map[string]fileCheck,
) error {
	now := s.now()
	it := update.Item

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		w := &writer{
			tx: tx, now: now, accountID: stored.AccountID, repos: make(map[string]int64),
			details: map[string]github.ItemDetails{it.NodeID: update.Details}, files: files,
		}

		if err := w.saveGraphQLRate(update.RateLimit); err != nil {
			return err
		}

		if !update.Found || it.Closed() || it.Repo.IsArchived {
			return w.unlist(id, update)
		}

		return w.rewrite(id, it)
	})
	if err != nil {
		return fmt.Errorf("save the item: %w", err)
	}

	return nil
}

// rewrite stores what GitHub says about an open item in place, with its details and file
// check, then ends the snoozes the new details give cause to.
func (w *writer) rewrite(id int64, it github.Item) error {
	err := w.exec("update the item", updateItemSQL,
		it.NodeID, it.Title, it.URL, it.AuthorLogin, it.AuthorType, it.AuthorAssociation, it.IsCrossRepository,
		it.IsDraft, it.HeadRef, it.CreatedAt.UTC(), it.UpdatedAt.UTC(), w.now, id)
	if err != nil {
		return err
	}

	if err = w.saveDetails(id, it); err != nil {
		return err
	}

	if err = w.saveFiles(id, it); err != nil {
		return err
	}

	return w.wakeSnoozed()
}

// unlist takes an item out of every list. An item in an archived repository records the
// repository first, so it is known as archived, as a whole refresh does.
func (w *writer) unlist(id int64, update *github.ItemUpdate) error {
	if update.Found && update.Item.Repo.IsArchived {
		if _, err := w.repoID(update.Item); err != nil {
			return err
		}
	}

	return w.exec("take the item out of the lists", unlistItemSQL, w.now, id)
}
