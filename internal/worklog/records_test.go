package worklog_test

import (
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/brand"
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

// record is a valid record of PR 184's worktree, made at the given minute.
func record(minute int) worklog.Record {
	return worklog.Record{
		ClonePath: "/srv/projects/example-repo",
		Worktree:  "pr-184",
		Path:      "/srv/projects/." + brand.Slug + "/worktrees/example-repo/pr-184",
		Branch:    "worktree-pr-184",
		Owner:     "acme-org",
		Name:      "example-repo",
		Kind:      item.KindPR,
		Number:    184,
		CreatedAt: time.Date(2026, 11, 6, 12, minute, 0, 0, time.UTC),
	}
}

func TestRecordListAndForgetWorktrees(t *testing.T) {
	t.Parallel()

	s, _ := openStore(t)
	ctx := t.Context()

	got, err := s.Worktrees(ctx)
	require.NoError(t, err)
	assert.NotNil(t, got, "no worktrees is an empty list")
	assert.Empty(t, got)

	issue := record(5)
	issue.Worktree, issue.Kind, issue.Number, issue.Branch = "issue-7", item.KindIssue, 7, "worktree-issue-7"
	issue.Path = "/srv/projects/." + brand.Slug + "/worktrees/example-repo/issue-7"

	require.NoError(t, s.Record(ctx, issue))
	require.NoError(t, s.Record(ctx, record(1)))

	got, err = s.Worktrees(ctx)
	require.NoError(t, err)
	assert.Equal(t, []worklog.Record{record(1), issue}, got, "oldest first, with no session yet")

	require.NoError(t, s.Forget(ctx, issue.ClonePath, issue.Worktree))
	require.NoError(t, s.Forget(ctx, issue.ClonePath, "issue-404"), "forgetting an unknown worktree is fine")

	got, err = s.Worktrees(ctx)
	require.NoError(t, err)
	assert.Equal(t, []worklog.Record{record(1)}, got)
}

func TestTouchRecordsEachSession(t *testing.T) {
	t.Parallel()

	s, _ := openStore(t)
	ctx := t.Context()

	require.NoError(t, s.Record(ctx, record(1)))

	first := time.Date(2026, 11, 6, 13, 0, 0, 0, time.FixedZone("UTC-5", -5*60*60))
	require.NoError(t, s.Touch(ctx, record(1).ClonePath, "pr-184", agent.Claude, first))
	require.NoError(t, s.Touch(ctx, record(1).ClonePath, "pr-184", agent.Codex, first.Add(time.Hour)))

	got, err := s.Worktrees(ctx)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, agent.Codex, got[0].Agent, "the latest session's agent")
	assert.Equal(t, 2, got[0].Launches)
	assert.True(t, first.Add(time.Hour).Equal(got[0].LastAgentAt))
	assert.Equal(t, time.UTC, got[0].LastAgentAt.Location())

	// Recording the worktree again, as when it moves, keeps its sessions.
	moved := record(30)
	moved.Path = "/srv/elsewhere/pr-184"
	require.NoError(t, s.Record(ctx, moved))

	got, err = s.Worktrees(ctx)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "/srv/elsewhere/pr-184", got[0].Path)
	assert.Equal(t, 2, got[0].Launches)
	assert.Equal(t, agent.Codex, got[0].Agent)
	assert.True(t, record(1).CreatedAt.Equal(got[0].CreatedAt), "when it was made stays")

	err = s.Touch(ctx, record(1).ClonePath, "pr-999", agent.Claude, first)
	require.ErrorIs(t, err, worklog.ErrNotRecorded)
}

func TestRecordReplacesTheSameWorktreesItem(t *testing.T) {
	t.Parallel()

	s, _ := openStore(t)
	ctx := t.Context()

	require.NoError(t, s.Record(ctx, record(1)))

	again := record(9)
	again.Owner, again.Name, again.Kind, again.Number = "upstream-org", "upstream-repo", item.KindIssue, 9

	require.NoError(t, s.Record(ctx, again))

	got, err := s.Worktrees(ctx)
	require.NoError(t, err)

	want := again
	want.CreatedAt = record(1).CreatedAt
	assert.Equal(t, []worklog.Record{want}, got, "the place and item are replaced; when it was made stays")

	// Another clone's worktree of the same name is a record of its own.
	other := record(3)
	other.ClonePath = "/srv/projects/fork-repo"
	require.NoError(t, s.Record(ctx, other))

	got, err = s.Worktrees(ctx)
	require.NoError(t, err)
	assert.Equal(t, []worklog.Record{want, other}, got)
}

func TestRecordsMadeAtTheSameTimeKeepTheirFirstOrder(t *testing.T) {
	t.Parallel()

	s, _ := openStore(t)
	ctx := t.Context()

	first, second := record(1), record(1)
	second.Worktree = "pr-185"

	require.NoError(t, s.Record(ctx, first))
	require.NoError(t, s.Record(ctx, second))
	require.NoError(t, s.Record(ctx, first), "recording again keeps the record's place")

	got, err := s.Worktrees(ctx)
	require.NoError(t, err)
	assert.Equal(t, []worklog.Record{first, second}, got)
}

func TestRecordTimesAreStoredInUTC(t *testing.T) {
	t.Parallel()

	s, db := openStore(t)
	ctx := t.Context()

	r := record(1)
	r.CreatedAt = r.CreatedAt.In(time.FixedZone("UTC-5", -5*60*60))
	require.NoError(t, s.Record(ctx, r))

	var text string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT CAST(created_at AS TEXT) FROM worktree_records").Scan(&text))
	assert.Regexp(t, storedStamp, text)
	assert.Equal(t, "2026-11-06 12:01:00+00:00", text)

	got, err := s.Worktrees(ctx)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, time.UTC, got[0].CreatedAt.Location())
	assert.True(t, r.CreatedAt.Equal(got[0].CreatedAt))
	assert.True(t, got[0].LastAgentAt.IsZero(), "no session yet")
}

func TestRecordRejectsIncompleteRecords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(*worklog.Record)
	}{
		{name: "no clone", change: func(r *worklog.Record) { r.ClonePath = "" }},
		{name: "no worktree", change: func(r *worklog.Record) { r.Worktree = "" }},
		{name: "no owner", change: func(r *worklog.Record) { r.Owner = "" }},
		{name: "no name", change: func(r *worklog.Record) { r.Name = "" }},
		{name: "unknown kind", change: func(r *worklog.Record) { r.Kind = "discussion" }},
		{name: "number not positive", change: func(r *worklog.Record) { r.Number = 0 }},
		{name: "relative path", change: func(r *worklog.Record) { r.Path = "worktrees/pr-184" }},
		{name: "no time", change: func(r *worklog.Record) { r.CreatedAt = time.Time{} }},
	}

	s, _ := openStore(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := record(1)
			tt.change(&r)

			require.ErrorIs(t, s.Record(t.Context(), r), worklog.ErrInvalidRecord)
		})
	}
}

func TestRecordDir(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "/srv/projects/."+brand.Slug+"/worktrees/example-repo/pr-184", record(1).Dir())

	legacy := record(1)
	legacy.Path = ""
	assert.Equal(t, "/srv/projects/example-repo/.claude/worktrees/pr-184", legacy.Dir(),
		"a record kept before the app made worktrees names Claude's")
}

func TestRecordsReportDatabaseFailures(t *testing.T) {
	t.Parallel()

	s := closedStore(t)
	ctx := t.Context()

	require.ErrorContains(t, s.Record(ctx, record(1)), "record the worktree")

	require.ErrorContains(t, s.Touch(ctx, "/x", "pr-1", agent.Claude, time.Now()), "record the session")

	_, err := s.Worktrees(ctx)
	require.ErrorContains(t, err, "read the worktree records")

	require.ErrorContains(t, s.Forget(ctx, "/x", "pr-1"), "forget the worktree")
}
