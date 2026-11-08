package worklog_test

import (
	"database/sql"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/mrz1836/nilbox/internal/worklog"
)

// noon is the time the tests' cleanups count from.
var noon = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC) //nolint:gochecknoglobals // a fixed test time

// refuseTrim makes every delete from worktree_cleanups fail, the way a full disk would, so
// a test can reach the error trimming reports.
const refuseTrim = `CREATE TRIGGER refuse_trim BEFORE DELETE ON worktree_cleanups
BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`

// cleanup is a cleanup of example-repo's pr-<n>, n minutes after noon.
func cleanup(n int) worklog.Cleanup {
	return worklog.Cleanup{
		CleanedAt:     noon.Add(time.Duration(n) * time.Minute),
		Automatic:     n%2 == 0,
		Repo:          "example-repo",
		Worktree:      "pr-" + strconv.Itoa(n),
		DisplayPath:   "~/projects/example-repo/.claude/worktrees/pr-" + strconv.Itoa(n),
		DeletedBranch: "worktree-pr-" + strconv.Itoa(n),
		Head:          "fc1c3975011e66491785ace891c893e4fc716587",
		ItemKind:      "pr",
		ItemOwner:     "acme-org",
		ItemName:      "example-repo",
		ItemNumber:    n,
		ItemTitle:     "Fix things",
		ItemURL:       "https://github.com/acme-org/example-repo/pull/" + strconv.Itoa(n),
		ItemState:     "merged",
	}
}

// worktrees returns the worktree names of cleanups, in order.
func worktrees(cleanups []worklog.Cleanup) []string {
	out := make([]string, 0, len(cleanups))
	for _, c := range cleanups {
		out = append(out, c.Worktree)
	}

	return out
}

// countCleanups returns how many cleanups are stored.
func countCleanups(t *testing.T, db *sql.DB) int {
	t.Helper()

	var n int
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM worktree_cleanups").Scan(&n))

	return n
}

func TestRecordAndRecentCleanupsNewestFirst(t *testing.T) {
	t.Parallel()

	s, _ := openStore(t)
	ctx := t.Context()

	got, err := s.RecentCleanups(ctx, 10)
	require.NoError(t, err)
	assert.NotNil(t, got, "no cleanups is an empty list")
	assert.Empty(t, got)

	require.NoError(t, s.RecordCleanup(ctx, cleanup(1)))
	require.NoError(t, s.RecordCleanup(ctx, cleanup(3)))
	require.NoError(t, s.RecordCleanup(ctx, cleanup(2)))

	got, err = s.RecentCleanups(ctx, 10)
	require.NoError(t, err)
	require.Len(t, got, 3)

	for i, want := range []worklog.Cleanup{cleanup(3), cleanup(2), cleanup(1)} {
		assert.Positive(t, got[i].ID)
		assert.Equal(t, time.UTC, got[i].CleanedAt.Location())

		want.ID = got[i].ID
		assert.Equal(t, want, got[i])
	}
}

func TestRecentCleanupsLimit(t *testing.T) {
	t.Parallel()

	s, _ := openStore(t)
	ctx := t.Context()

	for n := 1; n <= 3; n++ {
		require.NoError(t, s.RecordCleanup(ctx, cleanup(n)))
	}

	for _, tt := range []struct {
		limit int
		want  []string
	}{
		{limit: 2, want: []string{"pr-3", "pr-2"}},
		{limit: 0, want: []string{}},
		{limit: -1, want: []string{"pr-3", "pr-2", "pr-1"}},
	} {
		got, err := s.RecentCleanups(ctx, tt.limit)
		require.NoError(t, err)
		assert.Equal(t, tt.want, worktrees(got), "limit %d", tt.limit)
	}
}

func TestRecordCleanupKeepsTheNewest(t *testing.T) {
	t.Parallel()

	s, db := openStore(t)
	ctx := t.Context()

	for n := range worklog.KeepCleanups + 5 {
		require.NoError(t, s.RecordCleanup(ctx, cleanup(n)))
	}

	assert.Equal(t, worklog.KeepCleanups, countCleanups(t, db), "exactly the newest are kept")

	got, err := s.RecentCleanups(ctx, -1)
	require.NoError(t, err)
	require.Len(t, got, worklog.KeepCleanups)
	assert.Equal(t, "pr-"+strconv.Itoa(worklog.KeepCleanups+4), got[0].Worktree)
	assert.Equal(t, "pr-5", got[len(got)-1].Worktree, "the five oldest are dropped")

	// A cleanup older than every one kept is dropped as soon as it is recorded.
	older := cleanup(0)
	older.CleanedAt = noon.Add(-time.Minute)
	require.NoError(t, s.RecordCleanup(ctx, older))

	again, err := s.RecentCleanups(ctx, -1)
	require.NoError(t, err)
	assert.Equal(t, worktrees(got), worktrees(again))
}

func TestRecordCleanupKeepsTheLastRecordedOfTheSameTime(t *testing.T) {
	t.Parallel()

	s, db := openStore(t)
	ctx := t.Context()

	for n := range worklog.KeepCleanups + 3 {
		c := cleanup(n)
		c.CleanedAt = noon
		require.NoError(t, s.RecordCleanup(ctx, c))
	}

	assert.Equal(t, worklog.KeepCleanups, countCleanups(t, db))

	got, err := s.RecentCleanups(ctx, -1)
	require.NoError(t, err)
	require.Len(t, got, worklog.KeepCleanups)
	assert.Equal(t, "pr-"+strconv.Itoa(worklog.KeepCleanups+2), got[0].Worktree, "the last recorded comes first")
	assert.Equal(t, "pr-3", got[len(got)-1].Worktree, "the first three recorded are dropped")
}

func TestRecordCleanupConcurrently(t *testing.T) {
	t.Parallel()

	// The cap is lowered so the writers run past it in few transactions: SQLite takes
	// one writer at a time, and hundreds of back-to-back transactions on a loaded machine
	// can leave one writer waiting longer than the busy timeout.
	const (
		writers   = 8
		perWriter = 5
		keep      = 20
	)

	_, db := openStore(t)
	s := worklog.NewKeeping(db, keep)

	var g errgroup.Group

	for w := range writers {
		g.Go(func() error {
			for i := range perWriter {
				// Every cleanup has its own time, so which are newest does not depend on
				// the order the writers ran in.
				if err := s.RecordCleanup(t.Context(), cleanup(w*perWriter+i)); err != nil {
					return err
				}
			}

			return nil
		})
	}

	require.NoError(t, g.Wait())
	assert.Equal(t, keep, countCleanups(t, db))

	got, err := s.RecentCleanups(t.Context(), -1)
	require.NoError(t, err)

	want := make([]string, 0, keep)
	for n := writers*perWriter - 1; n >= writers*perWriter-keep; n-- {
		want = append(want, "pr-"+strconv.Itoa(n))
	}

	assert.Equal(t, want, worktrees(got), "exactly the newest are kept, whatever order they were recorded in")
}

func TestRecordCleanupIsAllOrNothing(t *testing.T) {
	t.Parallel()

	s, db := openStore(t)
	ctx := t.Context()

	for n := range worklog.KeepCleanups {
		require.NoError(t, s.RecordCleanup(ctx, cleanup(n)))
	}

	_, err := db.ExecContext(ctx, refuseTrim)
	require.NoError(t, err)

	err = s.RecordCleanup(ctx, cleanup(worklog.KeepCleanups))
	require.ErrorContains(t, err, "record the worktree cleanup")
	require.ErrorContains(t, err, "refused by the test")

	got, err := s.RecentCleanups(ctx, 1)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "pr-"+strconv.Itoa(worklog.KeepCleanups-1), got[0].Worktree,
		"a cleanup that could not be trimmed after is not stored")
	assert.Equal(t, worklog.KeepCleanups, countCleanups(t, db))
}

func TestClearCleanups(t *testing.T) {
	t.Parallel()

	s, _ := openStore(t)
	ctx := t.Context()

	require.NoError(t, s.RecordCleanup(ctx, cleanup(1)))
	require.NoError(t, s.ClearCleanups(ctx))
	require.NoError(t, s.ClearCleanups(ctx), "clearing nothing is fine")

	got, err := s.RecentCleanups(ctx, 10)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestRecordCleanupRejectsInvalidCleanups(t *testing.T) {
	t.Parallel()

	s, db := openStore(t)
	ctx := t.Context()

	noRepo := cleanup(1)
	noRepo.Repo = ""
	require.ErrorIs(t, s.RecordCleanup(ctx, noRepo), worklog.ErrInvalidCleanup)

	noWorktree := cleanup(1)
	noWorktree.Worktree = ""
	require.ErrorIs(t, s.RecordCleanup(ctx, noWorktree), worklog.ErrInvalidCleanup)

	// The schema refuses what the store does not check.
	unknownKind := cleanup(1)
	unknownKind.ItemKind = "discussion"
	require.ErrorContains(t, s.RecordCleanup(ctx, unknownKind), "record the worktree cleanup")

	assert.Zero(t, countCleanups(t, db))
}

func TestCleanupsReportDatabaseFailures(t *testing.T) {
	t.Parallel()

	s := closedStore(t)
	ctx := t.Context()

	require.ErrorContains(t, s.RecordCleanup(ctx, cleanup(1)), "record the worktree cleanup")

	_, err := s.RecentCleanups(ctx, 10)
	require.ErrorContains(t, err, "read the worktree cleanups")

	require.ErrorContains(t, s.ClearCleanups(ctx), "clear the worktree cleanups")
}
