package scripts

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// pull pulls the clone's branch and waits for the run to end.
func (g *gitRig) pull() scriptrun.Run {
	g.t.Helper()

	id, err := g.branch.Pull(g.t.Context(), g.shown(g.dir))
	require.NoError(g.t, err)

	return g.ended(id)
}

func TestPullFastForwardsTheBranch(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	g.pushed("two.txt")
	third := g.pushed("three.txt")
	require.Equal(t, BranchBehind, g.status(false).State)

	run := g.pull()
	assert.Equal(t, scriptrun.StatusSucceeded, run.Status)
	assert.Equal(t, "Updated main from "+g.first[:7]+" to "+third[:7]+", 2 new commits.", run.Summary)
	assert.Equal(t, scriptrun.KindPull, run.Kind)
	assert.Equal(t, "Pull main", run.Title)
	assert.Equal(t, scriptrun.RunsInApp, run.RunsIn)
	assert.Equal(t, map[string]any{"branch": "main"}, run.Result.Intent)
	assert.Equal(t, third, g.head(g.dir))
	assert.FileExists(t, filepath.Join(g.dir, "three.txt"))

	git := run.Argv[0]
	assert.True(t, filepath.IsAbs(git), "git by its absolute path")
	assert.Equal(t, []string{
		git, "-c", "core.hooksPath=" + os.DevNull, "-c", "core.fsmonitor=false", "-c", "submodule.recurse=false",
		"pull", "--ff-only", "--no-rebase", "--no-autostash", "--no-recurse-submodules", "--upload-pack=git-upload-pack",
		"origin", "refs/heads/main",
	}, run.Argv)

	asked := g.asked.remote.Load()
	assert.Equal(t, BranchUpToDate, g.status(false).State, "the pull's own look at origin is kept")
	assert.Equal(t, asked, g.asked.remote.Load(), "and origin isn't asked again")
}

func TestPullOfABranchUpToDate(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	g.status(false)

	run := g.pull()
	assert.Equal(t, scriptrun.StatusSucceeded, run.Status)
	assert.Equal(t, "main was already up to date.", run.Summary)
	assert.Equal(t, g.first, g.head(g.dir))
}

func TestPullNeverRebasesOrMergesADivergedBranch(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	mine := g.commit(g.dir, "mine.txt")
	g.pushed("theirs.txt")
	testutil.RunGit(t, g.dir, "config", "pull.rebase", "true")
	g.status(false)

	run := g.pull()
	assert.Equal(t, scriptrun.StatusFailed, run.Status)
	assert.Equal(t, "Couldn't pull main: Not possible to fast-forward, aborting.", run.Summary, "git's own words")
	assert.Equal(t, mine, g.head(g.dir), "the branch is as it was")
	assert.NoFileExists(t, filepath.Join(g.dir, "theirs.txt"))
}

func TestPullRunsNoHook(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	ran := filepath.Join(g.home, "hook-ran")
	hook := filepath.Join(g.dir, ".git", "hooks", "post-merge")
	require.NoError(t, os.WriteFile(hook, []byte("#!/bin/sh\ntouch '"+ran+"'\n"), 0o700)) //nolint:gosec // a hook must be executable
	g.pushed("two.txt")
	g.status(false)

	assert.Equal(t, scriptrun.StatusSucceeded, g.pull().Status)
	assert.NoFileExists(t, ran, "the clone's hooks don't run")
}

func TestPullRefusesACloneOnAnotherBranch(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	g.pushed("two.txt")
	require.Equal(t, "main", g.status(false).Branch)

	testutil.RunGit(t, g.dir, "checkout", "-q", "-b", "feature")

	_, err := g.branch.Pull(t.Context(), g.shown(g.dir))
	require.ErrorIs(t, err, ErrCantPull)
	assert.Equal(t, "main is no longer checked out here: the clone is on feature now. Check the branch again before "+
		"pulling.", uierr.Message(err))
	assert.Zero(t, g.recorded(), "refused before anything ran")

	testutil.RunGit(t, g.dir, "checkout", "-q", "--detach")

	_, err = g.branch.Pull(t.Context(), g.shown(g.dir))
	require.ErrorIs(t, err, ErrCantPull)
	assert.Equal(t, "main is no longer checked out here: HEAD is detached now. Check the branch again before pulling.",
		uierr.Message(err))
}

func TestPullRefusesWhatTheStatusWouldNotOffer(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)

	_, err := g.branch.Pull(t.Context(), g.shown(g.dir))
	require.ErrorIs(t, err, ErrCantPull)
	assert.Equal(t, "Check sdk's branch again before pulling it.", uierr.Message(err), "no status, no pull")

	g.status(false)
	require.NoError(t, os.WriteFile(filepath.Join(g.dir, ".git", "MERGE_HEAD"), []byte(g.first+"\n"), 0o600))

	_, err = g.branch.Pull(t.Context(), g.shown(g.dir))
	require.ErrorIs(t, err, ErrCantPull)
	assert.Equal(t, "git is merging here. Finish or abort that before pulling.", uierr.Message(err))

	require.NoError(t, os.Remove(filepath.Join(g.dir, ".git", "MERGE_HEAD")))
	testutil.RunGit(t, g.dir, "config", "--unset", "branch.main.merge")

	_, err = g.branch.Pull(t.Context(), g.shown(g.dir))
	require.ErrorIs(t, err, ErrCantPull)
	assert.Equal(t, "main doesn't track a branch on origin.", uierr.Message(err))
	assert.Zero(t, g.recorded())
}

func TestPullIsRefusedWhileScriptsAreOff(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	g.pushed("two.txt")
	g.status(false)

	g.settings.scripts(integrationprefs.SwitchOff)

	_, err := g.branch.Pull(t.Context(), g.shown(g.dir))
	require.ErrorIs(t, err, ErrCantRun)
	assert.Equal(t, "Scripts are turned off in Settings → Integrations.", uierr.Message(err))
	assert.Zero(t, g.recorded())
	assert.Equal(t, g.first, g.head(g.dir))
}

func TestPullAsksOriginAgainUnderTheLock(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	g.status(false)

	pwned := filepath.Join(g.home, "pwned")
	testutil.RunGit(t, g.dir, "remote", "set-url", "origin", "ext::sh -c touch% "+pwned)

	_, err := g.branch.Pull(t.Context(), g.shown(g.dir))
	require.ErrorIs(t, err, ErrCantPull)
	assert.Contains(t, uierr.Message(err), "remote helper", "gitread's guard refuses origin before git runs")
	assert.Zero(t, g.recorded())
	assert.NoFileExists(t, pwned)
}

func TestPullFailsAtOnceOnABusyClone(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	g.status(false)

	free, _, ok := g.locks.TryLock(g.dir, clonelock.Holder{Kind: clonelock.KindScript, Repo: "sdk", Title: "Test"})
	require.True(t, ok)
	t.Cleanup(free)

	_, err := g.branch.Pull(t.Context(), g.shown(g.dir))
	require.ErrorIs(t, err, clonelock.ErrBusy)
	assert.Equal(t, "A script is using sdk right now.", uierr.Message(err))
	assert.Zero(t, g.recorded())
}

func TestPullRunsAgainOnTheSameBranch(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	g.pushed("two.txt")
	g.status(false)

	first := g.pull()
	require.Equal(t, scriptrun.StatusSucceeded, first.Status)
	require.True(t, first.CanRunAgain)

	third := g.pushed("three.txt")

	id, err := g.runs.RunAgain(t.Context(), first.ID)
	require.NoError(t, err)

	again := g.ended(id)
	assert.Equal(t, scriptrun.StatusSucceeded, again.Status)
	assert.Equal(t, third, g.head(g.dir))

	testutil.RunGit(t, g.dir, "checkout", "-q", "-b", "feature")

	_, err = g.runs.RunAgain(t.Context(), again.ID)
	require.ErrorIs(t, err, ErrCantPull, "Run again checks the branch afresh")
}

func TestPullEnv(t *testing.T) {
	t.Parallel()

	got := pullEnv([]string{
		"PATH=/usr/bin", "SSH_AUTH_SOCK=/tmp/agent", "GIT_DIR=/elsewhere/.git", "GIT_WORK_TREE=/elsewhere",
		"GIT_INDEX_FILE=/elsewhere/index", "GIT_TERMINAL_PROMPT=1", "GIT_CONFIG_GLOBAL=/dev/null",
	})
	assert.Equal(t, []string{
		"PATH=/usr/bin", "SSH_AUTH_SOCK=/tmp/agent", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0",
	}, got)
}

func TestPullOutputLastError(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		out  string
		want string
	}{
		{out: "", want: ""},
		{
			out: "From file:///srv/origin\n * branch main -> FETCH_HEAD\nhint: Diverging branches can't be " +
				"fast-forwarded\nhint: x\nfatal: Not possible to fast-forward, aborting.\n",
			want: "Not possible to fast-forward, aborting.",
		},
		{
			out: "error: Your local changes to the following files would be overwritten by merge:\n\tone.txt\n" +
				"Please commit your changes or stash them before you merge.\nAborting",
			want: "Your local changes to the following files would be overwritten by merge:",
		},
		{out: "schwerwiegend: kein Repository\nhint: x\n", want: "schwerwiegend: kein Repository"},
	} {
		out := &pullOutput{}

		for chunk := range slices.Chunk([]byte(tc.out), 7) {
			_, err := out.Write(chunk)
			require.NoError(t, err)
		}

		assert.Equal(t, tc.want, out.lastError(), "%q", tc.out)
	}
}
