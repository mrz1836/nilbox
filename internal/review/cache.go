package review

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/review/internal/queries"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/timeline"
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

	// maxContextBytes is the size one pull request's context is cut to fit: the long texts
	// of its description and its conversation are cut in turn, down to nothing
	// (fitContext). What is left, names and links in the pages GitHub gives, stays far
	// below it.
	maxContextBytes = 1 << 20
)

// trimSteps are the lengths, in bytes, the texts of a context too large to keep are cut to
// in turn, the last emptying them.
var trimSteps = []int{16 << 10, 4 << 10, 0} //nolint:gochecknoglobals // fixed lengths, read only

// The stored patch states.
const (
	storedNoContent = "no_content"
	storedCapped    = "cache_cap"
)

// pullContext is what the sheet read about a pull request beyond its inbox card. A context
// an older build cached reads with zeros for what it lacks, until the sheet reads it again.
// Times are RFC 3339, "" for none.
type pullContext struct {
	// Head is the head commit it was read at, and State OPEN, MERGED, or CLOSED.
	Head  string `json:"head"`
	State string `json:"state"`

	// UpdatedAt is GitHub's time of the pull request's newest activity, as of the read.
	UpdatedAt string `json:"updatedAt"`

	// Body is the description, BodyTrimmed whether the cache cut it short, and EditedAt
	// when it was last edited.
	Body        string              `json:"body"`
	BodyTrimmed bool                `json:"bodyTrimmed"`
	EditedAt    string              `json:"editedAt"`
	Reactions   []timeline.Reaction `json:"reactions"`
	BaseRef     string              `json:"baseRef"`
	MergeState  string              `json:"mergeState"`
	MergeQueue  bool                `json:"mergeQueue"`

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

	// Unrun are the workflow runs that ended without a check and without passing; none in a
	// context an older build cached, until the sheet reads it again.
	Unrun []Unrun `json:"unrun"`

	// Closes are the issues it closes once it merges, and CloseCount how many there are;
	// none in a context an older build cached, until the sheet reads it again.
	Closes     []Closing `json:"closes"`
	CloseCount int       `json:"closeCount"`

	// Timeline is the newest page of the conversation, oldest first; Total counts the whole
	// conversation's entries, and Before reads the ones before the page.
	Timeline []timeline.Entry `json:"timeline"`
	Total    int              `json:"total"`
	Before   string           `json:"before"`
}

// contextReview is one reviewer's latest review, as the cache keeps it. Its text is in the
// conversation; an older build's cache holds it too, which nothing reads.
type contextReview struct {
	Author      string    `json:"author"`
	State       string    `json:"state"`
	URL         string    `json:"url"`
	Commit      string    `json:"commit"`
	SubmittedAt time.Time `json:"submittedAt"`
}

// contextOf converts what FetchPullReview read, the entries of viewer, the signed-in
// account, marked as theirs.
func contextOf(r *github.PullReview, viewer string) pullContext {
	c := pullContext{
		Head: r.Update.Details.HeadOID, State: r.Update.Item.State, UpdatedAt: stamp(r.Update.Item.UpdatedAt),
		Body: r.Body, EditedAt: stamp(r.LastEditedAt), Reactions: timeline.ReactionsOf(r.Reactions), BaseRef: r.BaseRef,
		MergeState: r.MergeState, MergeQueue: r.MergeQueue, Mine: r.ViewerDidAuthor,
		CanUpdateBranch: r.ViewerCanUpdateBranch, CanAutoMerge: r.ViewerCanEnableAutoMerge, CanBypass: r.ViewerCanBypass,
		ViewerReview: r.ViewerReview.State, ViewerReviewAt: r.ViewerReview.Commit, PendingReviews: r.PendingReviews,
		AutoMerge: r.AutoMerge.Method, CanClose: r.ViewerCanClose,
		Reviews: make([]contextReview, 0, len(r.Reviews)), Checks: make([]Check, 0, len(r.Checks)),
		Unrun: make([]Unrun, 0, len(r.Unrun)), Closes: make([]Closing, 0, len(r.Closes)), CloseCount: r.CloseCount,
		Timeline: timeline.Entries(r.Timeline.Items, viewer), Total: r.Timeline.Total, Before: r.Timeline.Before,
	}

	for _, s := range r.Unrun {
		c.Unrun = append(c.Unrun, Unrun{
			Name: s.Name, Approval: s.Conclusion == github.SuiteAwaitsApproval, RunID: s.RunID, URL: s.URL,
		})
	}

	for _, ref := range r.Closes {
		c.Closes = append(c.Closes, Closing{
			Repo: ref.Repo, Number: ref.Number, Title: ref.Title, URL: ref.URL, State: IssueState(ref.State),
			StateReason: IssueReason(ref.StateReason),
		})
	}

	for _, rev := range r.Reviews {
		c.Reviews = append(c.Reviews, contextReview{
			Author: rev.Author, State: rev.State, URL: rev.URL, Commit: rev.Commit,
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

	// seenAt is GitHub's time of the newest activity in the read the sheet showed when it
	// last opened the pull request, "" before then.
	seenAt string

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

	if row.SeenAt != nil {
		out.seenAt = clock.Format(*row.SeenAt)
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

// save stores what the sheet read of the item with id at now: the context, cut to fit,
// and, when diff is not nil, the files in place of those cached. It prunes the cache in the
// same transaction, and leaves seen_at as it was.
func (c *cacheStore) save(ctx context.Context, id int64, pc pullContext, diff *github.PullDiff, now time.Time) error {
	encoded, err := fitContext(pc)
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

// fitContext encodes pc, cutting its long texts in turn (trimSteps) until it fits
// maxContextBytes: the description, and each comment, review, and line comment of the
// conversation. Each text cut is marked, and keeps its page, where the whole text is; no
// entry is dropped.
func fitContext(pc pullContext) (string, error) {
	encoded, err := store.EncodeJSON(pc)
	if err != nil {
		return "", fmt.Errorf("encode the cached review: %w", err)
	}

	for _, limit := range trimSteps {
		if len(encoded) <= maxContextBytes {
			break
		}

		pc.Timeline = timeline.Cut(&pc.Body, &pc.BodyTrimmed, pc.Timeline, limit)

		if encoded, err = store.EncodeJSON(pc); err != nil {
			return "", fmt.Errorf("encode the cached review: %w", err)
		}
	}

	return encoded, nil
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

// open stamps the sheet's opening of the item with id, now: it shows the read whose newest
// activity GitHub timed at seenAt, nil for a read that doesn't say.
func (c *cacheStore) open(ctx context.Context, id int64, seenAt *time.Time, now time.Time) error {
	if err := c.q.OpenDiff(ctx, queries.OpenDiffParams{SeenAt: seenAt, UsedAt: now, ItemID: id}); err != nil {
		return fmt.Errorf("stamp the cached review: %w", err)
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
