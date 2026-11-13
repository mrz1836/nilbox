package issues

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/issues/internal/queries"
	"github.com/mrz1836/nilbox/internal/store"
)

// The sheet keeps what it read of each issue it opened in issue_views, which this package
// owns, so an issue opens at once, offline. The cache is bounded, and pruned in the same
// transaction that grows it.
const (
	// maxViewBytes is the size one issue's view is cut to fit: its long texts are cut in
	// turn, down to nothing (fit). What is left, names and links in the pages GitHub gives,
	// stays far below it.
	maxViewBytes = 1 << 20

	// maxCacheBytes and maxCachedIssues cap the whole cache: the least recently used issues
	// past either are dropped.
	maxCacheBytes   = 32 << 20
	maxCachedIssues = 200

	// unusedFor is how long an issue the sheet has not opened is kept.
	unusedFor = 14 * 24 * time.Hour
)

// trimSteps are the lengths, in bytes, the texts of a view too large to keep are cut to in
// turn, the last emptying them.
var trimSteps = []int{16 << 10, 4 << 10, 0} //nolint:gochecknoglobals // fixed lengths, read only

// cachedView is what the sheet keeps of one read beyond the inbox card. An older build's
// row reads with zeros for what it lacks. Times are RFC 3339, "" for none.
type cachedView struct {
	// UpdatedAt is GitHub's time of the issue's newest activity, as of the read.
	UpdatedAt string `json:"updatedAt"`

	State       RefState    `json:"state"`
	StateReason StateReason `json:"stateReason"`
	ClosedAt    string      `json:"closedAt"`
	ClosedBy    string      `json:"closedBy"`

	Body        string     `json:"body"`
	BodyTrimmed bool       `json:"bodyTrimmed"`
	EditedAt    string     `json:"editedAt"`
	Reactions   []Reaction `json:"reactions"`

	Locked     bool   `json:"locked"`
	LockReason string `json:"lockReason"`
	CanClose   bool   `json:"canClose"`
	CanUpdate  bool   `json:"canUpdate"`
	Mine       bool   `json:"mine"`

	Type        *IssueType `json:"type"`
	Milestone   *Milestone `json:"milestone"`
	Parent      *Linked    `json:"parent"`
	DuplicateOf *Linked    `json:"duplicateOf"`

	Assignees         []string `json:"assignees"`
	AssigneesTotal    int      `json:"assigneesTotal"`
	Participants      []string `json:"participants"`
	ParticipantsTotal int      `json:"participantsTotal"`

	SubIssues []Linked `json:"subIssues"`
	Fixes     []Fix    `json:"fixes"`
	FixCount  int      `json:"fixCount"`

	// Comments counts the comments as of the read, Latest and LatestAt name the newest, and
	// LatestMine says the user wrote it.
	Comments   int    `json:"comments"`
	Latest     string `json:"latest"`
	LatestAt   string `json:"latestAt"`
	LatestMine bool   `json:"latestMine"`

	// Timeline is the newest page of the conversation, oldest first; Total counts the whole
	// conversation's items, and Before reads the ones before the page.
	Timeline []TimelineItem `json:"timeline"`
	Total    int            `json:"total"`
	Before   string         `json:"before"`
}

// cached is one issue's row: its view, when it was read (zero once an action made it out
// of date), and seenAt, GitHub's time of the read the sheet showed when it last opened it.
type cached struct {
	view      cachedView
	fetchedAt time.Time
	seenAt    string
}

// lastSeen is GitHub's time of the read the sheet showed when it last opened the issue, ""
// for none, or for no cache at all.
func (c *cached) lastSeen() string {
	if c == nil {
		return ""
	}

	return c.seenAt
}

// cacheStore reads and writes issue_views.
type cacheStore struct {
	db *sql.DB
	q  *queries.Queries
}

func newCacheStore(db *sql.DB) *cacheStore {
	return &cacheStore{db: db, q: queries.New(db)}
}

// load returns the cached view of the issue with id, or nil when the sheet never read it.
func (c *cacheStore) load(ctx context.Context, id int64) (*cached, error) {
	row, err := c.q.GetView(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil //nolint:nilnil // no row is no cache, which is no error
	}

	if err != nil {
		return nil, fmt.Errorf("read the cached issue: %w", err)
	}

	view, err := store.DecodeJSON[cachedView](row.View)
	if err != nil {
		return nil, fmt.Errorf("read the cached issue: %w", err)
	}

	out := &cached{view: view, fetchedAt: store.Deref(row.FetchedAt).UTC()}
	if row.SeenAt != nil {
		out.seenAt = clock.Format(*row.SeenAt)
	}

	return out, nil
}

// save stores the view of the issue with id, read at now, cut to fit, and prunes the cache
// in the same transaction. It leaves seen_at as it was.
func (c *cacheStore) save(ctx context.Context, id int64, view cachedView, now time.Time) error {
	encoded, err := fit(view)
	if err != nil {
		return err
	}

	return store.InTx(ctx, c.db, func(tx *sql.Tx) error {
		q := c.q.WithTx(tx)

		err := q.UpsertView(ctx, queries.UpsertViewParams{
			ItemID: id, View: encoded, Bytes: int64(len(encoded)), FetchedAt: &now, UsedAt: now,
		})
		if err != nil {
			return fmt.Errorf("cache the issue: %w", err)
		}

		return prune(ctx, q, id, now)
	})
}

// fit encodes view, cutting its long texts in turn (trimSteps) until it fits maxViewBytes.
// Each text cut is marked, and keeps its page, where the whole text is; no item is dropped.
func fit(view cachedView) (string, error) {
	encoded, err := store.EncodeJSON(view)
	if err != nil {
		return "", fmt.Errorf("encode the cached issue: %w", err)
	}

	for _, limit := range trimSteps {
		if len(encoded) <= maxViewBytes {
			break
		}

		view = trimmed(view, limit)

		if encoded, err = store.EncodeJSON(view); err != nil {
			return "", fmt.Errorf("encode the cached issue: %w", err)
		}
	}

	return encoded, nil
}

// trimmed cuts the description and every comment of view longer than limit bytes.
func trimmed(view cachedView, limit int) cachedView {
	if body, cut := cutText(view.Body, limit); cut {
		view.Body, view.BodyTrimmed = body, true
	}

	items := make([]TimelineItem, len(view.Timeline))
	copy(items, view.Timeline)

	for i := range items {
		if body, cut := cutText(items[i].Body, limit); cut {
			items[i].Body, items[i].Trimmed = body, true
		}
	}

	view.Timeline = items

	return view
}

// cutText cuts s to at most limit bytes, on a rune boundary, and reports whether it did.
func cutText(s string, limit int) (string, bool) {
	if len(s) <= limit {
		return s, false
	}

	end := limit
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}

	return s[:end], true
}

// prune drops the views unused for unusedFor, then, from the most recently used down, each
// one that would take the cache past maxCachedIssues or maxCacheBytes, never keep, the one
// just written.
func prune(ctx context.Context, q *queries.Queries, keep int64, now time.Time) error {
	if _, err := q.PruneUnusedViews(ctx, now.Add(-unusedFor)); err != nil {
		return fmt.Errorf("prune the cached issues: %w", err)
	}

	rows, err := q.ListViewUsage(ctx)
	if err != nil {
		return fmt.Errorf("prune the cached issues: %w", err)
	}

	var (
		count int
		total int64
	)

	for _, r := range rows {
		count++
		total += r.Bytes

		if r.ItemID == keep || (count <= maxCachedIssues && total <= maxCacheBytes) {
			continue
		}

		if err = q.DeleteView(ctx, r.ItemID); err != nil {
			return fmt.Errorf("prune the cached issues: %w", err)
		}

		// What is dropped no longer counts toward the caps.
		count--
		total -= r.Bytes
	}

	return nil
}

// open stamps the sheet's opening of the issue with id, now: it shows the read whose newest
// activity GitHub timed at seenAt, nil for a read that doesn't say.
func (c *cacheStore) open(ctx context.Context, id int64, seenAt *time.Time, now time.Time) error {
	if err := c.q.OpenView(ctx, queries.OpenViewParams{SeenAt: seenAt, UsedAt: now, ItemID: id}); err != nil {
		return fmt.Errorf("stamp the cached issue: %w", err)
	}

	return nil
}

// expire marks the cached view of the issue with id out of date, so the next refresh reads.
func (c *cacheStore) expire(ctx context.Context, id int64) error {
	if err := c.q.ExpireView(ctx, id); err != nil {
		return fmt.Errorf("expire the cached issue: %w", err)
	}

	return nil
}

// drop forgets the cached view of the issue with id.
func (c *cacheStore) drop(ctx context.Context, id int64) error {
	if err := c.q.DeleteView(ctx, id); err != nil {
		return fmt.Errorf("forget the cached issue: %w", err)
	}

	return nil
}
