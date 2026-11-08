package worklog_test

import (
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/worklog"
)

// storedStamp matches a timestamp as the database stores it: UTC text that sorts in time
// order.
var storedStamp = regexp.MustCompile(`^\d{4}-\d\d-\d\d \d\d:\d\d:\d\d(\.\d+)?\+00:00$`)

// openStore opens a fresh, migrated database and a Store over it.
func openStore(t *testing.T) (*worklog.Store, *sql.DB) {
	t.Helper()

	db := testutil.DB(t)

	return worklog.New(db), db
}

// closedStore returns a Store over a database that is already closed.
func closedStore(t *testing.T) *worklog.Store {
	t.Helper()

	s, db := openStore(t)
	require.NoError(t, db.Close())

	return s
}

// launch is a valid launch for PR 184, launched at the given minute.
func launch(minute int) worklog.Launch {
	return worklog.Launch{
		ClonePath:  "/srv/projects/example-repo",
		Worktree:   "pr-184",
		Owner:      "acme-org",
		Name:       "example-repo",
		Kind:       item.KindPR,
		Number:     184,
		LaunchedAt: time.Date(2026, 11, 6, 12, minute, 0, 0, time.UTC),
	}
}

func TestRecordListAndForgetLaunches(t *testing.T) {
	t.Parallel()

	s, _ := openStore(t)
	ctx := t.Context()

	got, err := s.Launches(ctx)
	require.NoError(t, err)
	assert.NotNil(t, got, "no launches is an empty list")
	assert.Empty(t, got)

	issue := launch(5)
	issue.Worktree, issue.Kind, issue.Number = "issue-7", item.KindIssue, 7

	require.NoError(t, s.RecordLaunch(ctx, issue))
	require.NoError(t, s.RecordLaunch(ctx, launch(1)))

	got, err = s.Launches(ctx)
	require.NoError(t, err)
	assert.Equal(t, []worklog.Launch{launch(1), issue}, got, "oldest first")

	require.NoError(t, s.ForgetLaunch(ctx, issue.ClonePath, issue.Worktree))
	require.NoError(t, s.ForgetLaunch(ctx, issue.ClonePath, "issue-404"), "forgetting an unknown worktree is fine")

	got, err = s.Launches(ctx)
	require.NoError(t, err)
	assert.Equal(t, []worklog.Launch{launch(1)}, got)
}

func TestRecordLaunchReplacesTheSameWorktree(t *testing.T) {
	t.Parallel()

	s, _ := openStore(t)
	ctx := t.Context()

	require.NoError(t, s.RecordLaunch(ctx, launch(1)))

	again := launch(9)
	again.Owner, again.Name, again.Kind, again.Number = "upstream-org", "upstream-repo", item.KindIssue, 9
	require.NoError(t, s.RecordLaunch(ctx, again))

	got, err := s.Launches(ctx)
	require.NoError(t, err)
	assert.Equal(t, []worklog.Launch{again}, got, "every field and the launch time are replaced")

	// Another clone's worktree of the same name is a launch of its own.
	other := launch(3)
	other.ClonePath = "/srv/projects/fork-repo"
	require.NoError(t, s.RecordLaunch(ctx, other))

	got, err = s.Launches(ctx)
	require.NoError(t, err)
	assert.Equal(t, []worklog.Launch{other, again}, got)
}

func TestLaunchesAtTheSameTimeKeepTheirFirstOrder(t *testing.T) {
	t.Parallel()

	s, _ := openStore(t)
	ctx := t.Context()

	first, second := launch(1), launch(1)
	second.Worktree = "pr-185"

	require.NoError(t, s.RecordLaunch(ctx, first))
	require.NoError(t, s.RecordLaunch(ctx, second))
	require.NoError(t, s.RecordLaunch(ctx, first), "recording again keeps the record's place")

	got, err := s.Launches(ctx)
	require.NoError(t, err)
	assert.Equal(t, []worklog.Launch{first, second}, got)
}

func TestLaunchTimesAreStoredInUTC(t *testing.T) {
	t.Parallel()

	s, db := openStore(t)
	ctx := t.Context()

	l := launch(1)
	l.LaunchedAt = l.LaunchedAt.In(time.FixedZone("UTC-5", -5*60*60))
	require.NoError(t, s.RecordLaunch(ctx, l))

	var text string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT CAST(launched_at AS TEXT) FROM worktree_launches").Scan(&text))
	assert.Regexp(t, storedStamp, text)
	assert.Equal(t, "2026-11-06 12:01:00+00:00", text)

	got, err := s.Launches(ctx)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, time.UTC, got[0].LaunchedAt.Location())
	assert.True(t, l.LaunchedAt.Equal(got[0].LaunchedAt))
}

func TestRecordLaunchRejectsIncompleteLaunches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(*worklog.Launch)
	}{
		{name: "no clone", change: func(l *worklog.Launch) { l.ClonePath = "" }},
		{name: "no worktree", change: func(l *worklog.Launch) { l.Worktree = "" }},
		{name: "no owner", change: func(l *worklog.Launch) { l.Owner = "" }},
		{name: "no name", change: func(l *worklog.Launch) { l.Name = "" }},
		{name: "unknown kind", change: func(l *worklog.Launch) { l.Kind = "discussion" }},
		{name: "number not positive", change: func(l *worklog.Launch) { l.Number = 0 }},
	}

	s, _ := openStore(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			l := launch(1)
			tt.change(&l)

			require.ErrorIs(t, s.RecordLaunch(t.Context(), l), worklog.ErrInvalidLaunch)
		})
	}
}

func TestLaunchesReportDatabaseFailures(t *testing.T) {
	t.Parallel()

	s := closedStore(t)
	ctx := t.Context()

	require.ErrorContains(t, s.RecordLaunch(ctx, launch(1)), "record the worktree launch")

	_, err := s.Launches(ctx)
	require.ErrorContains(t, err, "read the worktree launches")

	require.ErrorContains(t, s.ForgetLaunch(ctx, "/x", "pr-1"), "forget the worktree launch")
}
