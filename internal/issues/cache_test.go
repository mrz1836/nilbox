package issues

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/issues/internal/queries"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// comments returns count comments of size bytes each.
func comments(count, size int) []TimelineItem {
	out := make([]TimelineItem, 0, count)
	for range count {
		out = append(out, TimelineItem{Kind: EventComment, Body: strings.Repeat("a", size)})
	}

	return out
}

func TestFitCutsLongTextsInTurn(t *testing.T) {
	t.Parallel()

	small := cachedView{Body: "Short.", Timeline: comments(3, 100)}
	encoded, err := fit(small)
	require.NoError(t, err)
	assert.NotContains(t, encoded, `"bodyTrimmed":true`, "a view that fits is kept whole")

	tests := []struct {
		name  string
		view  cachedView
		limit int
	}{
		{name: "a long description", view: cachedView{Body: strings.Repeat("b", 2<<20)}, limit: 16 << 10},
		{name: "long comments", view: cachedView{Timeline: comments(100, 20<<10)}, limit: 4 << 10},
		{name: "many comments", view: cachedView{Timeline: comments(300, 5<<10)}, limit: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			encoded, err := fit(tt.view)
			require.NoError(t, err)
			assert.LessOrEqual(t, len(encoded), maxViewBytes)

			view, err := store.DecodeJSON[cachedView](encoded)
			require.NoError(t, err)
			assert.Len(t, view.Timeline, len(tt.view.Timeline), "no item is dropped")

			if tt.view.Body != "" {
				assert.True(t, view.BodyTrimmed)
				assert.Len(t, view.Body, tt.limit)
			}

			for _, it := range view.Timeline {
				assert.True(t, it.Trimmed)
				assert.Len(t, it.Body, tt.limit)
			}
		})
	}
}

func TestCutTextKeepsWholeRunes(t *testing.T) {
	t.Parallel()

	got, cut := cutText("héllo", 2)
	assert.True(t, cut)
	assert.Equal(t, "h", got, "é is two bytes: the cut backs off to its start")

	got, cut = cutText("hello", 5)
	assert.False(t, cut)
	assert.Equal(t, "hello", got)
}

func TestTheCacheStaysWithinItsCaps(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)
	ids := storeIssues(t, db, 1, maxCachedIssues+2)
	c := newCacheStore(db)
	q := queries.New(db)
	ctx := t.Context()

	// Every issue but the last two, each used a second after the one before.
	for i, id := range ids[:maxCachedIssues] {
		require.NoError(t, q.UpsertView(ctx, queries.UpsertViewParams{
			ItemID: id, View: "{}", Bytes: 2, UsedAt: testNow.Add(time.Duration(i) * time.Second),
		}))
	}

	// One more read makes 201: the least recently used goes.
	require.NoError(t, c.save(ctx, ids[maxCachedIssues], cachedView{}, testNow.Add(time.Hour)))
	assert.Equal(t, maxCachedIssues, countViews(t, c))
	assertCached(t, c, ids[0], false)
	assertCached(t, c, ids[1], true)

	// A big one pushes the bytes past 32 MiB: the least recently used go until they fit,
	// never the one just read.
	require.NoError(t, q.UpsertView(ctx, queries.UpsertViewParams{
		ItemID: ids[1], View: "{}", Bytes: maxCacheBytes - 100, UsedAt: testNow.Add(2 * time.Hour),
	}))
	require.NoError(t, c.save(ctx, ids[maxCachedIssues+1], cachedView{Body: strings.Repeat("c", 200)},
		testNow.Add(3*time.Hour)))

	assertCached(t, c, ids[maxCachedIssues+1], true)
	assertCached(t, c, ids[1], false, "the big one, older than the one just read, goes")
	assertCached(t, c, ids[maxCachedIssues], true)
}

func TestTheCacheDropsWhatItHasNotOpenedForTwoWeeks(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)
	ids := storeIssues(t, db, 1, 3)
	c := newCacheStore(db)

	require.NoError(t, c.save(t.Context(), ids[0], cachedView{}, testNow.Add(-unusedFor-time.Minute)))
	require.NoError(t, c.save(t.Context(), ids[1], cachedView{}, testNow.Add(-unusedFor+time.Minute)))
	require.NoError(t, c.save(t.Context(), ids[2], cachedView{}, testNow))

	assertCached(t, c, ids[0], false)
	assertCached(t, c, ids[1], true)

	at := testNow.Add(-time.Hour)
	require.NoError(t, c.open(t.Context(), ids[1], &at, testNow.Add(unusedFor)))
	require.NoError(t, c.save(t.Context(), ids[2], cachedView{}, testNow.Add(unusedFor)))
	assertCached(t, c, ids[1], true, "opening it keeps it")
}

func TestTheCacheRemembersTheVisit(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)
	id := storeIssues(t, db, 1, 1)[0]
	c := newCacheStore(db)
	ctx := t.Context()

	got, err := c.load(ctx, id)
	require.NoError(t, err)
	assert.Nil(t, got, "no row is no cache")

	require.NoError(t, c.save(ctx, id, cachedView{UpdatedAt: stamp(latestAt)}, testNow))
	got, err = c.load(ctx, id)
	require.NoError(t, err)
	assert.Empty(t, got.seenAt, "a read alone is no visit")

	require.NoError(t, c.open(ctx, id, &latestAt, testNow))
	require.NoError(t, c.save(ctx, id, cachedView{UpdatedAt: stamp(testNow)}, testNow))

	got, err = c.load(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, stamp(latestAt), got.seenAt, "a later read leaves the visit as it was")
	assert.Equal(t, testNow, got.fetchedAt)

	require.NoError(t, c.expire(ctx, id))
	got, err = c.load(ctx, id)
	require.NoError(t, err)
	assert.True(t, got.fetchedAt.IsZero(), "an action put it out of date")

	require.NoError(t, c.open(ctx, id, nil, testNow))
	got, err = c.load(ctx, id)
	require.NoError(t, err)
	assert.Empty(t, got.seenAt, "a read that doesn't say when it was")

	_, err = db.ExecContext(ctx, `UPDATE issue_views SET view = '{"comments":"many"}' WHERE item_id = ?`, id)
	require.NoError(t, err)
	_, err = c.load(ctx, id)
	require.Error(t, err, "a view this build can't read")

	require.NoError(t, c.drop(ctx, id))
	got, err = c.load(ctx, id)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestTheCacheFailsWithItsDatabase(t *testing.T) {
	t.Parallel()

	c := newCacheStore(testutil.ClosedDB(t))
	ctx := t.Context()

	_, err := c.load(ctx, issueID)
	require.Error(t, err)
	require.Error(t, c.save(ctx, issueID, cachedView{}, testNow))
	require.Error(t, c.open(ctx, issueID, nil, testNow))
	require.Error(t, c.expire(ctx, issueID))
	require.Error(t, c.drop(ctx, issueID))
}

func TestPruneFailsWithItsQueries(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)
	ids := storeIssues(t, db, 1, 2)
	c := newCacheStore(db)

	require.NoError(t, c.save(t.Context(), ids[0], cachedView{}, testNow.Add(-unusedFor-time.Hour)))

	_, err := db.ExecContext(t.Context(), `CREATE TRIGGER no_prune BEFORE DELETE ON issue_views
		BEGIN SELECT RAISE(ABORT, 'refused'); END`)
	require.NoError(t, err)

	require.Error(t, c.save(t.Context(), ids[1], cachedView{}, testNow), "the prune fails, so the save does")
	assertCached(t, c, ids[1], false, "and nothing of it is kept")
}

// countViews counts the cached views.
func countViews(t *testing.T, c *cacheStore) int {
	t.Helper()

	rows, err := c.q.ListViewUsage(t.Context())
	require.NoError(t, err)

	return len(rows)
}

// assertCached checks whether the cache holds the issue with id.
func assertCached(t *testing.T, c *cacheStore, id int64, want bool, msg ...any) {
	t.Helper()

	got, err := c.load(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, want, got != nil, msg...)
}
