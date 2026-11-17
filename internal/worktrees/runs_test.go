package worktrees

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/procs"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// fakeRuns names the app's own runs.
type fakeRuns []RunFolder

func (f fakeRuns) RunFolders(context.Context) []RunFolder {
	return append([]RunFolder(nil), f...)
}

func TestAWorktreeAnAgentRunWorksInIsInUseAndNeverRemoved(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	records := &fakeLaunches{}

	got, err := NewMaker(e.linked(linkDeps{launches: records})).Ensure(t.Context(), f.request(item.KindPR, 7))
	require.NoError(t, err)

	// The process list leaves the app's own children out, so the run shows only through
	// the app's own run folders: an orphan after a quit, whose PID is known, or a run still
	// starting, whose PID isn't yet.
	for _, run := range []RunFolder{
		{Dir: got.Path, PID: 4242, Agent: agent.Claude, By: "⚡ Fix checks"},
		{Dir: filepath.Join(got.Path, "src"), Agent: agent.Claude, By: "⚡ Fix checks"},
	} {
		svc := e.linked(linkDeps{launches: records, running: fakeRunning{list: []procs.Process{}}, runs: fakeRuns{run}})

		snap, err := svc.List(t.Context())
		require.NoError(t, err)

		row := mustRow(t, snap, got.Path)
		assert.Equal(t, []Session{{PID: run.PID, Command: "claude", Agent: agent.Claude, By: "⚡ Fix checks"}}, row.InUse,
			"the app's own run is in use, named by its routine")
		assert.Equal(t, StageInUse, row.Stage)
		assert.False(t, row.CanRemove)

		err = testutil.ErrOf(svc.Remove(t.Context(), f.clone, got.Path, nil))
		require.ErrorIs(t, err, ErrInUse)
		require.ErrorContains(t, err, "⚡ Fix checks")
		assert.DirExists(t, got.Path)
	}
}

func TestTheSweepLeavesAWorktreeAnAgentRunWorksIn(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newSweepFixture(t, e)
	busy := e.linked(linkDeps{
		gh: f.gh, launches: f.launches, auto: true, running: fakeRunning{list: []procs.Process{}},
		runs: fakeRuns{{Dir: f.launchedPR, PID: 99, Agent: agent.Claude, By: "⚡ Review"}},
	})

	report := busy.Sweep(t.Context())

	cleaned := make([]string, 0, len(report.Cleaned))
	for _, r := range report.Cleaned {
		cleaned = append(cleaned, r.Item)
	}

	assert.NotContains(t, cleaned, "acme-org/example-repo#1", "a worktree an agent's run works in stays, followed or not")
	assert.Contains(t, gitWorktreePaths(t, f.repo), resolvedPath(f.launchedPR))
}
