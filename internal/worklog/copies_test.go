package worklog_test

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/worklog"
)

// copiedAt is when the copy tests' clock starts.
func copiedAt() time.Time { return time.Date(2026, 11, 7, 8, 0, 0, 0, time.UTC) }

// copyStore opens a fresh, migrated database and a Store over it whose clock the test moves.
func copyStore(t *testing.T) (*worklog.Store, *sql.DB, *testutil.Clock) {
	t.Helper()

	db := testutil.DB(t)
	clk := testutil.NewClock(copiedAt())

	return worklog.New(db, clk), db, clk
}

// copied is a valid copy record of an agent's worktree, at head, copied onto feature-x,
// whose pull request is acme-org/example-repo#199.
func copied(head string) worklog.Copy {
	return worklog.Copy{
		ClonePath:    "/srv/projects/example-repo",
		WorktreePath: "/srv/projects/example-repo/.claude/worktrees/agent-1",
		Head:         head,
		Ref:          "refs/remotes/origin/feature-x",
		Owner:        "acme-org",
		Name:         "example-repo",
		Number:       199,
		CopyTip:      strings.Repeat("c", 40),
		Commits:      []string{head, strings.Repeat("b", 40)},
	}
}

func TestRecordListAndForgetCopies(t *testing.T) {
	t.Parallel()

	s, _, clk := copyStore(t)
	ctx := t.Context()

	got, err := s.Copies(ctx)
	require.NoError(t, err)
	assert.NotNil(t, got, "no record is an empty list")
	assert.Empty(t, got)

	first := copied(strings.Repeat("a", 40))
	require.NoError(t, s.RecordCopy(ctx, first))

	other := copied(strings.Repeat("d", 40))
	other.WorktreePath = "/srv/projects/example-repo/.claude/worktrees/agent-2"

	clk.Advance(time.Minute)
	require.NoError(t, s.RecordCopy(ctx, other))

	first.CreatedAt, first.UpdatedAt = copiedAt(), copiedAt()
	other.CreatedAt, other.UpdatedAt = copiedAt().Add(time.Minute), copiedAt().Add(time.Minute)

	got, err = s.Copies(ctx)
	require.NoError(t, err)
	assert.Equal(t, []worklog.Copy{first, other}, got, "in the order they were first written, stamped by the clock")

	require.NoError(t, s.ForgetCopy(ctx, other.WorktreePath))
	require.NoError(t, s.ForgetCopy(ctx, "/srv/gone"), "forgetting a worktree with no record is fine")

	got, err = s.Copies(ctx)
	require.NoError(t, err)
	assert.Equal(t, []worklog.Copy{first}, got)
}

func TestRecordCopyReplacesTheWorktreesRecord(t *testing.T) {
	t.Parallel()

	s, _, clk := copyStore(t)
	ctx := t.Context()
	head := strings.Repeat("a", 40)

	require.NoError(t, s.RecordCopy(ctx, copied(head)))

	// The branch moved on, still holding every copy: the same HEAD keeps its first time.
	moved := copied(head)
	moved.CopyTip = strings.Repeat("e", 40)

	clk.Advance(time.Hour)
	require.NoError(t, s.RecordCopy(ctx, moved))

	got, err := s.Copies(ctx)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, strings.Repeat("e", 40), got[0].CopyTip)
	assert.Equal(t, copiedAt(), got[0].CreatedAt)
	assert.Equal(t, copiedAt().Add(time.Hour), got[0].UpdatedAt)

	// Another HEAD is a new record of the same worktree.
	again := copied(strings.Repeat("f", 40))
	again.Number, again.Ref, again.Owner, again.Name = 201, "refs/remotes/upstream/feature-y", "upstream-org", "other"

	clk.Advance(time.Hour)
	require.NoError(t, s.RecordCopy(ctx, again))

	again.CreatedAt, again.UpdatedAt = copiedAt().Add(2*time.Hour), copiedAt().Add(2*time.Hour)

	got, err = s.Copies(ctx)
	require.NoError(t, err)
	assert.Equal(t, []worklog.Copy{again}, got)
}

func TestCopyTimesAreStoredInUTC(t *testing.T) {
	t.Parallel()

	s, db, _ := copyStore(t)
	require.NoError(t, s.RecordCopy(t.Context(), copied(strings.Repeat("a", 40))))

	var created, updated string

	require.NoError(t, db.QueryRowContext(t.Context(),
		`SELECT CAST(created_at AS TEXT), CAST(updated_at AS TEXT) FROM worktree_copies`).Scan(&created, &updated))
	assert.Regexp(t, storedStamp, created)
	assert.Regexp(t, storedStamp, updated)
}

func TestRecordCopyRejectsIncompleteRecords(t *testing.T) {
	t.Parallel()

	s, _, _ := copyStore(t)

	for name, change := range map[string]func(*worklog.Copy){
		"no clone":          func(c *worklog.Copy) { c.ClonePath = "" },
		"a relative clone":  func(c *worklog.Copy) { c.ClonePath = "example-repo" },
		"a relative path":   func(c *worklog.Copy) { c.WorktreePath = "agent-1" },
		"no head":           func(c *worklog.Copy) { c.Head = "" },
		"no branch":         func(c *worklog.Copy) { c.Ref = "" },
		"no owner":          func(c *worklog.Copy) { c.Owner = "" },
		"no name":           func(c *worklog.Copy) { c.Name = "" },
		"no number":         func(c *worklog.Copy) { c.Number = 0 },
		"no tip":            func(c *worklog.Copy) { c.CopyTip = "" },
		"no commit":         func(c *worklog.Copy) { c.Commits = nil },
		"no commit at all":  func(c *worklog.Copy) { c.Commits = []string{} },
		"a negative number": func(c *worklog.Copy) { c.Number = -1 },
	} {
		c := copied(strings.Repeat("a", 40))
		change(&c)
		require.ErrorIs(t, s.RecordCopy(t.Context(), c), worklog.ErrInvalidCopy, name)
	}

	got, err := s.Copies(t.Context())
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestCopiesReportDatabaseFailures(t *testing.T) {
	t.Parallel()

	s := closedStore(t)
	ctx := t.Context()

	require.Error(t, s.RecordCopy(ctx, copied(strings.Repeat("a", 40))))
	require.Error(t, s.ForgetCopy(ctx, "/srv/projects/example-repo/.claude/worktrees/agent-1"))

	_, err := s.Copies(ctx)
	require.Error(t, err)

	// A record whose commits aren't commit IDs, which no store wrote, fails the read.
	open, db, _ := copyStore(t)
	_, err = db.ExecContext(ctx, `INSERT INTO worktree_copies (clone_path, worktree_path, head, ref, repo_owner, repo_name,
		pr_number, copy_tip, commits, created_at, updated_at) VALUES ('/srv/c', '/srv/w', 'h', 'r', 'o', 'n', 1, 't', '[1]',
		'2026-11-07 08:00:00+00:00', '2026-11-07 08:00:00+00:00')`)
	require.NoError(t, err)

	_, err = open.Copies(ctx)
	require.ErrorContains(t, err, "read the copies of /srv/w")
}
