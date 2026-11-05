package inbox

import (
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox/model"
)

// The upserts below key on each table's unique columns. RETURNING id gives back the row's
// id whether it was inserted or updated.
const (
	upsertAccountSQL = `INSERT INTO accounts (login, created_at, updated_at) VALUES (?, ?, ?)
ON CONFLICT (login) DO UPDATE SET login = excluded.login, updated_at = excluded.updated_at
RETURNING id`

	upsertRepoSQL = `INSERT INTO repos (owner, name, created_at, updated_at) VALUES (?, ?, ?, ?)
ON CONFLICT (owner, name) DO UPDATE SET owner = excluded.owner, name = excluded.name, updated_at = excluded.updated_at
RETURNING id`

	clearReasonsSQL = `UPDATE items
SET reason_review_requested = 0, reason_authored = 0, reason_assigned = 0
WHERE account_id = ?`

	upsertItemSQL = `INSERT INTO items (
    account_id, repo_id, kind, number, node_id, title, url, author_login, author_association,
    is_cross_repository, is_draft, head_ref, item_created_at, item_updated_at,
    reason_review_requested, reason_authored, reason_assigned, fetched_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (account_id, repo_id, kind, number) DO UPDATE SET
    node_id = excluded.node_id,
    title = excluded.title,
    url = excluded.url,
    author_login = excluded.author_login,
    author_association = excluded.author_association,
    is_cross_repository = excluded.is_cross_repository,
    is_draft = excluded.is_draft,
    head_ref = excluded.head_ref,
    item_created_at = excluded.item_created_at,
    item_updated_at = excluded.item_updated_at,
    reason_review_requested = excluded.reason_review_requested,
    reason_authored = excluded.reason_authored,
    reason_assigned = excluded.reason_assigned,
    fetched_at = excluded.fetched_at
RETURNING id`

	upsertPrefSQL = `INSERT INTO prefs (key, value, updated_at) VALUES (?, ?, ?)
ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`
)

// writer stores one refresh inside a transaction.
type writer struct {
	tx        *gorm.DB
	now       time.Time
	accountID int64
	repos     map[string]int64

	// details holds the items' details by node ID. An item without an entry keeps the
	// details stored before.
	details map[string]github.ItemDetails
}

// saveAccount upserts the signed-in account and remembers its id.
func (w *writer) saveAccount(login string) error {
	if err := w.tx.Raw(upsertAccountSQL, login, w.now, w.now).Scan(&w.accountID).Error; err != nil {
		return fmt.Errorf("save the account: %w", err)
	}

	return nil
}

// saveItems clears every reason flag for the account, then upserts each fetched item with
// the flags of the lists that returned it. Items no longer returned keep their row (local
// state may reference it) with all three flags off.
func (w *writer) saveItems(inbox *github.Inbox) error {
	if err := w.tx.Exec(clearReasonsSQL, w.accountID).Error; err != nil {
		return fmt.Errorf("clear the reason flags: %w", err)
	}

	for _, e := range mergeLists(inbox) {
		repoID, err := w.repoID(e.item.RepoOwner, e.item.RepoName)
		if err != nil {
			return err
		}

		if err = w.saveItem(repoID, e); err != nil {
			return err
		}
	}

	return nil
}

// repoID upserts a repository once per refresh and returns its id.
func (w *writer) repoID(owner, name string) (int64, error) {
	key := repoKey(owner, name)
	if id, ok := w.repos[key]; ok {
		return id, nil
	}

	var id int64
	if err := w.tx.Raw(upsertRepoSQL, owner, name, w.now, w.now).Scan(&id).Error; err != nil {
		return 0, fmt.Errorf("save the repository %s/%s: %w", owner, name, err)
	}

	w.repos[key] = id

	return id, nil
}

// saveItem upserts one item, then its details when this refresh read them.
func (w *writer) saveItem(repoID int64, e *entry) error {
	it := e.item

	var id int64

	err := w.tx.Raw(upsertItemSQL,
		w.accountID, repoID, kindOf(it), it.Number, it.NodeID, it.Title, it.URL, it.AuthorLogin, it.AuthorAssociation,
		it.IsCrossRepository, it.IsDraft, it.HeadRef, it.CreatedAt.UTC(), it.UpdatedAt.UTC(),
		e.reviewRequested, e.authored, e.assigned, w.now,
	).Scan(&id).Error
	if err != nil {
		return fmt.Errorf("save the item %s/%s#%d: %w", it.RepoOwner, it.RepoName, it.Number, err)
	}

	d, ok := w.details[it.NodeID]
	if !ok {
		return nil
	}

	row := detailsRow(id, d, w.now)

	err = w.tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "item_id"}}, UpdateAll: true}).Create(&row).Error
	if err != nil {
		return fmt.Errorf("save the details of %s/%s#%d: %w", it.RepoOwner, it.RepoName, it.Number, err)
	}

	return nil
}

// detailsRow converts fetched details to their row.
func detailsRow(itemID int64, d github.ItemDetails, now time.Time) model.ItemDetails {
	labels := make(model.Labels, 0, len(d.Labels))
	for _, l := range d.Labels {
		labels = append(labels, model.Label{Name: l.Name, Color: l.Color})
	}

	return model.ItemDetails{
		ItemID:           itemID,
		Labels:           labels,
		Comments:         d.Comments,
		Additions:        d.Additions,
		Deletions:        d.Deletions,
		ChangedFiles:     d.ChangedFiles,
		Commits:          d.Commits,
		Mergeable:        d.Mergeable,
		ChecksState:      d.Checks.State,
		ChecksPassed:     d.Checks.Passed,
		ChecksFailed:     d.Checks.Failed,
		ChecksPending:    d.Checks.Pending,
		ChecksSkipped:    d.Checks.Skipped,
		ReviewDecision:   d.Reviews.Decision,
		Reviewers:        d.Reviews.Reviewers,
		Reviewed:         d.Reviews.Reviewed,
		Approvals:        d.Reviews.Approvals,
		ChangesRequested: d.Reviews.ChangesRequested,
		FetchedAt:        now,
	}
}

// saveRates records one snapshot per resource and prunes snapshots past the retention
// window. The GraphQL row comes from the query itself, so it carries the query's cost.
func (w *writer) saveRates(query github.QueryRate, limits *github.RateLimits) error {
	rows := []model.RateSnapshot{
		{
			AccountID: w.accountID, Resource: resourceGraphQL, LimitTotal: query.Limit, Remaining: query.Remaining,
			Used: query.Used, Cost: query.Cost, ResetAt: query.ResetAt.UTC(), TakenAt: w.now,
		},
		w.restSnapshot(resourceCore, limits.Core),
		w.restSnapshot(resourceSearch, limits.Search),
	}

	if err := w.tx.Create(&rows).Error; err != nil {
		return fmt.Errorf("save the rate limits: %w", err)
	}

	err := w.tx.Where("taken_at < ?", w.now.Add(-snapshotRetention)).Delete(&model.RateSnapshot{}).Error
	if err != nil {
		return fmt.Errorf("prune old rate limits: %w", err)
	}

	return nil
}

// restSnapshot builds a snapshot row from a REST budget.
func (w *writer) restSnapshot(resource string, r github.Rate) model.RateSnapshot {
	return model.RateSnapshot{
		AccountID: w.accountID, Resource: resource, LimitTotal: r.Limit, Remaining: r.Remaining,
		Used: r.Used, ResetAt: r.ResetAt.UTC(), TakenAt: w.now,
	}
}

// pref is one key/value pair for the prefs table.
type pref struct {
	key, value string
}

// listedNodeIDsSQL reads the node IDs of the signed-in account's listed items, newest
// activity first.
const listedNodeIDsSQL = `SELECT i.node_id
FROM items i
JOIN accounts a ON a.id = i.account_id
WHERE a.login = (SELECT value FROM prefs WHERE key = ?)
  AND i.node_id <> ''
  AND (i.reason_review_requested = 1 OR i.reason_authored = 1 OR i.reason_assigned = 1)
ORDER BY i.item_updated_at DESC, i.id DESC`

// listedNodeIDs returns the node IDs of the items the last refresh listed.
func listedNodeIDs(db *gorm.DB) ([]string, error) {
	var ids []string
	if err := db.Raw(listedNodeIDsSQL, prefViewer).Scan(&ids).Error; err != nil {
		return nil, fmt.Errorf("read the listed items: %w", err)
	}

	return ids, nil
}

// setPrefs upserts prefs, in order.
func setPrefs(db *gorm.DB, now time.Time, prefs []pref) error {
	for _, p := range prefs {
		if err := db.Exec(upsertPrefSQL, p.key, p.value, now).Error; err != nil {
			return fmt.Errorf("save the %s pref: %w", p.key, err)
		}
	}

	return nil
}

// readPrefs returns the inbox status prefs that are set.
func readPrefs(db *gorm.DB) (map[string]string, error) {
	keys := []string{
		prefViewer, prefViewerName, prefViewerURL, prefViewerAvatarURL, prefAvatar, prefAvatarSource,
		prefTotalReviewRequested, prefTotalAuthored, prefTotalAssigned,
		prefLastRefreshAt, prefLastError, prefScopeWarning,
	}

	var rows []struct {
		Key   string `gorm:"column:key"`
		Value string `gorm:"column:value"`
	}

	if err := db.Raw("SELECT key, value FROM prefs WHERE key IN ?", keys).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("read the inbox status: %w", err)
	}

	prefs := make(map[string]string, len(rows))
	for _, r := range rows {
		prefs[r.Key] = r.Value
	}

	return prefs, nil
}
