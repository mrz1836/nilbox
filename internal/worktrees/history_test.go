package worktrees

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/worklog"
)

var errFakeHistory = errors.New("fake: history unavailable")

// openHistory opens a fresh, migrated database and a cleanup history over it.
func openHistory(t *testing.T) *worklog.Store {
	t.Helper()

	return worklog.New(testutil.DB(t))
}

// failingHistory fails every call.
type failingHistory struct{}

func (failingHistory) RecordCleanup(context.Context, worklog.Cleanup) error { return errFakeHistory }

func (failingHistory) RecentCleanups(context.Context, int) ([]worklog.Cleanup, error) {
	return nil, errFakeHistory
}

func (failingHistory) ClearCleanups(context.Context) error { return errFakeHistory }

func TestCleanUpsAreRecorded(t *testing.T) {
	e := newEnv(t)
	repo := e.newGitHubRepo(t, "example-repo")
	history := openHistory(t)
	gh := &fakeGitHub{}

	byHand, byHandHead := agentWorktree(t, repo, "pr-1")
	pushed(t, repo, "one", byHandHead)
	gh.set("example-repo", 1, pr(github.StateMerged, byHandHead))

	_, autoHead := agentWorktree(t, repo, "issue-2")
	pushed(t, repo, "two", autoHead)
	gh.set("example-repo", 2, closedIssue())

	launches := &fakeLaunches{records: []worklog.Launch{launchOf(repo, "issue-2", item.KindIssue, 2)}}
	svc := e.linked(linkDeps{gh: gh, launches: launches, history: history, auto: true})

	none, err := svc.RecentCleanups(t.Context())
	require.NoError(t, err)
	assert.Empty(t, none)

	_, err = svc.CleanUp(t.Context(), repo, byHand)
	require.NoError(t, err)

	report := svc.Sweep(t.Context())
	require.Len(t, report.Cleaned, 1, "the sweep takes the launched issue's worktree")

	got, err := svc.RecentCleanups(t.Context())
	require.NoError(t, err)
	require.Len(t, got, 2)

	at := listedAt().UTC().Format("2006-01-02T15:04:05Z07:00")

	for _, c := range got {
		assert.Positive(t, c.ID)
		assert.Equal(t, at, c.CleanedAt)
		assert.Equal(t, "example-repo", c.Repo)
	}

	byWorktree := map[string]Cleanup{got[0].Worktree: got[0], got[1].Worktree: got[1]}

	manual := byWorktree["pr-1"]
	assert.False(t, manual.Automatic)
	assert.Equal(t, "~/projects/example-repo/.claude/worktrees/pr-1", manual.DisplayPath)
	assert.Equal(t, "worktree-pr-1", manual.DeletedBranch)
	assert.Equal(t, byHandHead, manual.Head)
	assert.Equal(t, &LinkedItem{
		Kind: string(item.KindPR), Owner: "acme-org", Name: "example-repo", Number: 1, Title: "Fix things",
		URL: "https://github.com/acme-org/example-repo/pull/1", State: ItemMerged,
	}, manual.Item)

	swept := byWorktree["issue-2"]
	assert.True(t, swept.Automatic)
	assert.Equal(t, "worktree-issue-2", swept.DeletedBranch)
	require.NotNil(t, swept.Item)
	assert.Equal(t, string(item.KindIssue), swept.Item.Kind)
	assert.Equal(t, ItemClosed, swept.Item.State)

	require.NoError(t, svc.ClearCleanups(t.Context()))

	got, err = svc.RecentCleanups(t.Context())
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestCleanupOfAnUnlinkedRecord(t *testing.T) {
	c := cleanupOf(worklog.Cleanup{ID: 3, Repo: "example-repo", Worktree: "scratch"})
	assert.Nil(t, c.Item, "a record without an item has none")
	assert.Equal(t, "scratch", c.Worktree)
}

func TestHistoryFailuresAndAbsence(t *testing.T) {
	e := newEnv(t)
	repo := e.newGitHubRepo(t, "example-repo")
	path, head := agentWorktree(t, repo, "pr-1")
	pushed(t, repo, "one", head)

	gh := &fakeGitHub{}
	gh.set("example-repo", 1, pr(github.StateMerged, head))

	svc := e.linked(linkDeps{gh: gh, history: failingHistory{}})

	res, err := svc.CleanUp(t.Context(), repo, path)
	require.NoError(t, err, "a cleanup that could not be recorded still succeeds")
	assert.True(t, res.Removed)

	_, err = svc.RecentCleanups(t.Context())
	require.ErrorIs(t, err, errFakeHistory)
	require.ErrorIs(t, svc.ClearCleanups(t.Context()), errFakeHistory)

	without := e.linked(linkDeps{})

	got, err := without.RecentCleanups(t.Context())
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.NotNil(t, got, "an empty list, never null")
	require.NoError(t, without.ClearCleanups(t.Context()))
}
