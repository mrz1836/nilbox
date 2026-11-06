package launchlog_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/mrz1836/nilbox/internal/launchlog"
	"github.com/mrz1836/nilbox/internal/store"
)

// openStore opens a fresh, migrated database and a Store over it.
func openStore(t *testing.T) (*launchlog.Store, *gorm.DB) {
	t.Helper()

	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close(db) })

	return launchlog.NewStore(db), db
}

// launch is a valid launch for PR 184, launched at the given minute.
func launch(minute int) launchlog.Launch {
	return launchlog.Launch{
		ClonePath:  "/srv/projects/example-repo",
		Worktree:   "pr-184",
		Owner:      "acme-org",
		Name:       "example-repo",
		Kind:       launchlog.KindPR,
		Number:     184,
		LaunchedAt: time.Date(2026, 11, 6, 12, minute, 0, 0, time.UTC),
	}
}

func TestRecordListAndForget(t *testing.T) {
	s, _ := openStore(t)
	ctx := t.Context()

	issue := launch(5)
	issue.Worktree, issue.Kind, issue.Number = "issue-7", launchlog.KindIssue, 7

	require.NoError(t, s.Record(ctx, issue))
	require.NoError(t, s.Record(ctx, launch(1)))

	got, err := s.List(ctx)
	require.NoError(t, err)
	assert.Equal(t, []launchlog.Launch{launch(1), issue}, got, "oldest first")

	require.NoError(t, s.Forget(ctx, issue.ClonePath, issue.Worktree))
	require.NoError(t, s.Forget(ctx, issue.ClonePath, "issue-404"), "forgetting an unknown worktree is fine")

	got, err = s.List(ctx)
	require.NoError(t, err)
	assert.Equal(t, []launchlog.Launch{launch(1)}, got)
}

func TestRecordReplacesTheSameWorktree(t *testing.T) {
	s, _ := openStore(t)
	ctx := t.Context()

	require.NoError(t, s.Record(ctx, launch(1)))

	again := launch(9)
	again.Owner = "upstream-org"
	require.NoError(t, s.Record(ctx, again))

	got, err := s.List(ctx)
	require.NoError(t, err)
	assert.Equal(t, []launchlog.Launch{again}, got)
}

func TestRecordRejectsIncompleteLaunches(t *testing.T) {
	s, _ := openStore(t)

	tests := []struct {
		name   string
		change func(*launchlog.Launch)
	}{
		{name: "no clone", change: func(l *launchlog.Launch) { l.ClonePath = "" }},
		{name: "no worktree", change: func(l *launchlog.Launch) { l.Worktree = "" }},
		{name: "no owner", change: func(l *launchlog.Launch) { l.Owner = "" }},
		{name: "no name", change: func(l *launchlog.Launch) { l.Name = "" }},
		{name: "unknown kind", change: func(l *launchlog.Launch) { l.Kind = "discussion" }},
		{name: "number not positive", change: func(l *launchlog.Launch) { l.Number = 0 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := launch(1)
			tt.change(&l)

			require.ErrorIs(t, s.Record(t.Context(), l), launchlog.ErrInvalidLaunch)
		})
	}
}

func TestStoreReportsDatabaseFailures(t *testing.T) {
	s, db := openStore(t)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	ctx := t.Context()

	require.ErrorContains(t, s.Record(ctx, launch(1)), "record the worktree launch")

	_, err = s.List(ctx)
	require.ErrorContains(t, err, "read the worktree launches")

	require.ErrorContains(t, s.Forget(ctx, "/x", "pr-1"), "forget the worktree launch")
}
