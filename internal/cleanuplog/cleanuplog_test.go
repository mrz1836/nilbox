package cleanuplog_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/mrz1836/nilbox/internal/cleanuplog"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// openStore opens a fresh, migrated database and a Store over it.
func openStore(t *testing.T) (*cleanuplog.Store, *gorm.DB) {
	t.Helper()

	db := testutil.DB(t)

	return cleanuplog.NewStore(db), db
}

// cleanup is a cleanup of example-repo's pr-<n>, n minutes after noon.
func cleanup(n int) cleanuplog.Cleanup {
	return cleanuplog.Cleanup{
		CleanedAt:     time.Date(2026, 11, 6, 12, n, 0, 0, time.UTC),
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

func TestRecordAndRecentNewestFirst(t *testing.T) {
	s, _ := openStore(t)
	ctx := t.Context()

	require.NoError(t, s.Record(ctx, cleanup(1)))
	require.NoError(t, s.Record(ctx, cleanup(3)))
	require.NoError(t, s.Record(ctx, cleanup(2)))

	got, err := s.Recent(ctx, 10)
	require.NoError(t, err)
	require.Len(t, got, 3)

	for i, want := range []cleanuplog.Cleanup{cleanup(3), cleanup(2), cleanup(1)} {
		assert.Positive(t, got[i].ID)

		want.ID = got[i].ID
		assert.Equal(t, want, got[i])
	}

	got, err = s.Recent(ctx, 2)
	require.NoError(t, err)
	assert.Len(t, got, 2, "the limit applies")
}

func TestRecordKeepsTheNewest(t *testing.T) {
	s, _ := openStore(t)
	ctx := t.Context()

	for n := range cleanuplog.Keep + 5 {
		c := cleanup(0)
		c.CleanedAt = c.CleanedAt.Add(time.Duration(n) * time.Minute)
		c.Worktree = "pr-" + strconv.Itoa(n)
		require.NoError(t, s.Record(ctx, c))
	}

	got, err := s.Recent(ctx, cleanuplog.Keep*2)
	require.NoError(t, err)
	require.Len(t, got, cleanuplog.Keep)
	assert.Equal(t, "pr-"+strconv.Itoa(cleanuplog.Keep+4), got[0].Worktree)
	assert.Equal(t, "pr-5", got[len(got)-1].Worktree, "the five oldest are dropped")
}

func TestClear(t *testing.T) {
	s, _ := openStore(t)
	ctx := t.Context()

	require.NoError(t, s.Record(ctx, cleanup(1)))
	require.NoError(t, s.Clear(ctx))

	got, err := s.Recent(ctx, 10)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestRecordRejectsIncompleteCleanups(t *testing.T) {
	s, _ := openStore(t)

	noRepo := cleanup(1)
	noRepo.Repo = ""
	require.ErrorIs(t, s.Record(t.Context(), noRepo), cleanuplog.ErrInvalidCleanup)

	noWorktree := cleanup(1)
	noWorktree.Worktree = ""
	require.ErrorIs(t, s.Record(t.Context(), noWorktree), cleanuplog.ErrInvalidCleanup)
}

func TestStoreReportsDatabaseFailures(t *testing.T) {
	s, db := openStore(t)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	ctx := t.Context()

	require.ErrorContains(t, s.Record(ctx, cleanup(1)), "record the worktree cleanup")

	_, err = s.Recent(ctx, 10)
	require.ErrorContains(t, err, "read the worktree cleanups")

	require.ErrorContains(t, s.Clear(ctx), "clear the worktree cleanups")
}
