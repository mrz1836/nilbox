package review

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/review/internal/queries"
	"github.com/mrz1836/nilbox/internal/store"
)

// The sheet keeps what it read of each pull request it opened, in review_diffs and
// review_diff_files, which this package owns: the files with their diffs at one head, and
// the rest of what it read as JSON. So a pull request opens at once, offline, and a refresh
// that finds nothing pushed reads no files again. The cache is bounded, and pruned in the
// same transaction that grows it.
const (
	// maxPullPatchBytes caps the diffs kept for one pull request; the files past it keep no
	// diff (PatchCapped).
	maxPullPatchBytes = 8 << 20

	// maxCacheBytes and maxCachedPulls cap the whole cache: the least recently used pull
	// requests past either are dropped.
	maxCacheBytes  = 64 << 20
	maxCachedPulls = 100

	// unusedFor is how long a pull request the sheet has not opened is kept.
	unusedFor = 14 * 24 * time.Hour

	// openPatchBytes caps the diffs Open returns at once; the rest are read with Patches.
	openPatchBytes = 2 << 20
)

// The stored patch states.
const (
	storedNoContent = "no_content"
	storedCapped    = "cache_cap"
)

// pullContext is what the sheet read about a pull request beyond its inbox card.
type pullContext struct {
	// Head is the head commit it was read at, and State OPEN, MERGED, or CLOSED.
	Head  string `json:"head"`
	State string `json:"state"`

	Body       string `json:"body"`
	BaseRef    string `json:"baseRef"`
	MergeState string `json:"mergeState"`
	MergeQueue bool   `json:"mergeQueue"`

	// Mine reports the viewer's own pull request.
	Mine bool `json:"mine"`

	CanUpdateBranch bool `json:"canUpdateBranch"`
	CanAutoMerge    bool `json:"canAutoMerge"`
	CanBypass       bool `json:"canBypass"`

	// CanClose is GitHub's say that the viewer may close the pull request; false in a
	// context an older build cached, until the sheet reads it again.
	CanClose bool `json:"canClose"`

	// ViewerReview is the state of the viewer's latest review and ViewerReviewAt the
	// commit it was left on.
	ViewerReview   string `json:"viewerReview"`
	ViewerReviewAt string `json:"viewerReviewAt"`

	PendingReviews int    `json:"pendingReviews"`
	AutoMerge      string `json:"autoMerge"`

	Reviews []contextReview `json:"reviews"`
	Checks  []Check         `json:"checks"`

	// Closes are the issues it closes once it merges, and CloseCount how many there are;
	// none in a context an older build cached, until the sheet reads it again.
	Closes     []Closing `json:"closes"`
	CloseCount int       `json:"closeCount"`
}

// contextReview is one reviewer's latest review, as the cache keeps it.
type contextReview struct {
	Author      string    `json:"author"`
	State       string    `json:"state"`
	Body        string    `json:"body"`
	URL         string    `json:"url"`
	Commit      string    `json:"commit"`
	SubmittedAt time.Time `json:"submittedAt"`
}

// contextOf converts what FetchPullReview read.
func contextOf(r *github.PullReview) pullContext {
	c := pullContext{
		Head: r.Update.Details.HeadOID, State: r.Update.Item.State, Body: r.Body, BaseRef: r.BaseRef,
		MergeState: r.MergeState, MergeQueue: r.MergeQueue, Mine: r.ViewerDidAuthor,
		CanUpdateBranch: r.ViewerCanUpdateBranch, CanAutoMerge: r.ViewerCanEnableAutoMerge, CanBypass: r.ViewerCanBypass,
		ViewerReview: r.ViewerReview.State, ViewerReviewAt: r.ViewerReview.Commit, PendingReviews: r.PendingReviews,
		AutoMerge: r.AutoMerge.Method, CanClose: r.ViewerCanClose,
		Reviews: make([]contextReview, 0, len(r.Reviews)), Checks: make([]Check, 0, len(r.Checks)),
		Closes: make([]Closing, 0, len(r.Closes)), CloseCount: r.CloseCount,
	}

	for _, ref := range r.Closes {
		c.Closes = append(c.Closes, Closing{
			Repo: ref.Repo, Number: ref.Number, Title: ref.Title, URL: ref.URL, State: IssueState(ref.State),
			StateReason: IssueReason(ref.StateReason),
		})
	}

	for _, rev := range r.Reviews {
		c.Reviews = append(c.Reviews, contextReview{
			Author: rev.Author, State: rev.State, Body: rev.Body, URL: rev.URL, Commit: rev.Commit,
			SubmittedAt: rev.SubmittedAt,
		})
	}

	for _, ch := range r.Checks {
		c.Checks = append(c.Checks, Check{
			Name: ch.Name, Workflow: ch.Workflow, State: ch.State, Outcome: ch.Outcome, URL: ch.URL, Required: ch.Required,
		})
	}

	return c
}

// cached is one pull request's cache.
type cached struct {
	head     string
	total    int
	complete bool
	context  pullContext

	// contextAt is when the context was read, and fetchedAt when the files were listed;
	// zero when not known.
	contextAt time.Time
	fetchedAt time.Time

	files []cachedFile
}

// cachedFile is one cached file.
type cachedFile struct {
	position     int
	path         string
	previousPath string
	status       string
	additions    int
	deletions    int
	blobSHA      string

	// state is the stored patch state ("", storedNoContent, or storedCapped), patchBytes
	// the size of the patch kept, and patch the patch, once read.
	state      string
	patchBytes int64
	patch      string
}

// cacheStore reads and writes the cache.
type cacheStore struct {
	db *sql.DB
	q  *queries.Queries
}

// newCacheStore returns the cache over the pool db.
func newCacheStore(db *sql.DB) *cacheStore {
	return &cacheStore{db: db, q: queries.New(db)}
}

// load reads the cache of the item with id, with the patches of its first files up to
// budget bytes, or returns nil when there is none.
func (c *cacheStore) load(ctx context.Context, id, budget int64) (*cached, error) {
	row, err := c.q.GetDiff(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil //nolint:nilnil // no cache is not an error
	}

	if err != nil {
		return nil, fmt.Errorf("read the cached review: %w", err)
	}

	pc, err := store.DecodeJSON[pullContext](row.Context)
	if err != nil {
		return nil, fmt.Errorf("read the cached review: %w", err)
	}

	rows, err := c.q.ListDiffFiles(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("read the cached files: %w", err)
	}

	out := &cached{
		head: row.HeadOID, total: int(row.Total), complete: row.Complete, context: pc,
		contextAt: store.Deref(row.ContextAt), fetchedAt: store.Deref(row.FetchedAt), files: make([]cachedFile, 0, len(rows)),
	}

	for _, r := range rows {
		out.files = append(out.files, cachedFile{
			position: int(r.Position), path: r.Path, previousPath: r.PreviousPath, status: r.Status,
			additions: int(r.Additions), deletions: int(r.Deletions), blobSHA: r.BlobSha, state: r.PatchState,
			patchBytes: r.PatchBytes,
		})
	}

	if err = c.fillPatches(ctx, id, out.files, budget); err != nil {
		return nil, err
	}

	return out, nil
}

// fillPatches reads the patches of the first files whose patches fit in budget bytes, in
// order, into files.
func (c *cacheStore) fillPatches(ctx context.Context, id int64, files []cachedFile, budget int64) error {
	var (
		positions []int64
		spent     int64
	)

	for _, f := range files {
		if f.state != "" {
			continue
		}

		if spent+f.patchBytes > budget {
			break
		}

		spent += f.patchBytes
		positions = append(positions, int64(f.position))
	}

	patches, err := c.patches(ctx, id, positions)
	if err != nil {
		return err
	}

	for i := range files {
		if p, ok := patches[files[i].position]; ok {
			files[i].patch = p
		}
	}

	return nil
}

// patches reads the stored patches at positions, by position. A file with no stored
// patch is left out.
func (c *cacheStore) patches(ctx context.Context, id int64, positions []int64) (map[int]string, error) {
	out := map[int]string{}
	if len(positions) == 0 {
		return out, nil
	}

	rows, err := c.q.ListDiffPatches(ctx, queries.ListDiffPatchesParams{ItemID: id, Positions: positions})
	if err != nil {
		return nil, fmt.Errorf("read the cached diffs: %w", err)
	}

	for _, r := range rows {
		if r.PatchState == "" {
			out[int(r.Position)] = r.Patch
		}
	}

	return out, nil
}

// save stores what the sheet read of the item with id at now: the context, and, when
// diff is not nil, the files in place of those cached. It prunes the cache in the same
// transaction.
func (c *cacheStore) save(ctx context.Context, id int64, pc pullContext, diff *github.PullDiff, now time.Time) error {
	encoded, err := store.EncodeJSON(pc)
	if err != nil {
		return err
	}

	err = store.InTx(ctx, c.db, func(tx *sql.Tx) error {
		q := c.q.WithTx(tx)

		if diff == nil {
			if setErr := q.SetDiffContext(ctx, queries.SetDiffContextParams{
				Context: encoded, ContextAt: &now, UsedAt: now, ItemID: id,
			}); setErr != nil {
				return setErr
			}
		} else if saveErr := saveDiff(ctx, q, id, encoded, diff, now); saveErr != nil {
			return saveErr
		}

		return prune(ctx, q, id, now)
	})
	if err != nil {
		return fmt.Errorf("save the review: %w", err)
	}

	return nil
}

// saveDiff stores the files of diff, keeping each patch while the pull request's total
// stays within maxPullPatchBytes.
func saveDiff(ctx context.Context, q *queries.Queries, id int64, encoded string, diff *github.PullDiff, now time.Time) error {
	if err := q.DeleteDiffFiles(ctx, id); err != nil {
		return err
	}

	var kept int64

	files := make([]queries.InsertDiffFileParams, 0, len(diff.Files))

	for i, f := range diff.Files {
		row := queries.InsertDiffFileParams{
			ItemID: id, Position: int64(i), Path: f.Path, PreviousPath: f.PreviousPath, Status: f.Status,
			Additions: int64(f.Additions), Deletions: int64(f.Deletions), BlobSha: f.BlobSHA,
		}

		switch size := int64(len(f.Patch)); {
		case !f.HasPatch:
			row.PatchState = storedNoContent
		case kept+size > maxPullPatchBytes:
			row.PatchState = storedCapped
		default:
			row.Patch, kept = f.Patch, kept+size
		}

		files = append(files, row)
	}

	err := q.UpsertDiff(ctx, queries.UpsertDiffParams{
		ItemID: id, HeadOID: diff.Head, Total: int64(diff.Total), Complete: diff.Complete, Bytes: kept,
		Context: encoded, ContextAt: &now, FetchedAt: &now, UsedAt: now,
	})
	if err != nil {
		return err
	}

	for _, f := range files {
		if err = q.InsertDiffFile(ctx, f); err != nil {
			return err
		}
	}

	return nil
}

// prune drops the cached reviews unused for unusedFor, then the least recently used past
// maxCachedPulls or maxCacheBytes, never keep's.
func prune(ctx context.Context, q *queries.Queries, keep int64, now time.Time) error {
	if _, err := q.PruneUnusedDiffs(ctx, now.Add(-unusedFor)); err != nil {
		return err
	}

	usage, err := q.ListDiffUsage(ctx)
	if err != nil {
		return err
	}

	var (
		count int
		bytes int64
	)

	for _, u := range usage {
		count++
		bytes += u.Bytes

		if u.ItemID == keep || (count <= maxCachedPulls && bytes <= maxCacheBytes) {
			continue
		}

		if err = q.DeleteDiff(ctx, u.ItemID); err != nil {
			return err
		}

		count--
		bytes -= u.Bytes
	}

	return nil
}

// touch stamps the use of the cache of the item with id.
func (c *cacheStore) touch(ctx context.Context, id int64, now time.Time) error {
	if err := c.q.TouchDiff(ctx, queries.TouchDiffParams{UsedAt: now, ItemID: id}); err != nil {
		return fmt.Errorf("stamp the cached review: %w", err)
	}

	return nil
}

// expire marks the cached context of the item with id as out of date, as after an action
// changed what it says, so the next refresh reads it again rather than skip as recent.
func (c *cacheStore) expire(ctx context.Context, id int64) error {
	if err := c.q.ExpireDiffContext(ctx, id); err != nil {
		return fmt.Errorf("expire the cached review: %w", err)
	}

	return nil
}

// drop removes the cache of the item with id, as after a merge.
func (c *cacheStore) drop(ctx context.Context, id int64) error {
	if err := c.q.DeleteDiff(ctx, id); err != nil {
		return fmt.Errorf("drop the cached review: %w", err)
	}

	return nil
}
