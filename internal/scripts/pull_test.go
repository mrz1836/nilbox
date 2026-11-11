package scripts

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/shellenv"
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

// ref returns the commit ref names in dir, or "" for none.
func (g *gitRig) ref(dir, ref string) string {
	g.t.Helper()

	out, _, err := execpath.Exec{Env: g.environ(), Dir: dir}.Run(g.t.Context(), "git", "rev-parse", "--verify", "-q", ref)
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(out))
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
	assert.Equal(t, third, g.ref(g.dir, "refs/remotes/origin/main"), "origin's branch as the clone keeps it moved too")
	assert.FileExists(t, filepath.Join(g.dir, "three.txt"))

	env := g.environ()
	git, err := execpath.Search{Path: shellenv.Lookup(env, "PATH"), Home: shellenv.Lookup(env, "HOME")}.Find("git")
	require.NoError(t, err)

	assert.Equal(t, git, run.Argv[0], "gitread's own git, by its absolute path")
	assert.Equal(t, gitread.Hardening(), run.Argv[1:7], "no hook, file system monitor, or submodule recursion")
	assert.Contains(t, strings.Join(run.Argv, " "), "-c http.savecookies=false", "gitread's ls-remote settings")

	i := slices.Index(run.Argv, "pull")
	require.Positive(t, i)
	assert.Equal(t, []string{
		"pull", "--ff-only", "--no-rebase", "--refmap=", "--no-tags", "--no-autostash", "--no-recurse-submodules",
		"--no-verify-signatures", "--no-stat", "--upload-pack=git-upload-pack", originRemote,
		"+refs/heads/main:refs/remotes/origin/main",
	}, run.Argv[i:])

	asked := g.asked.remote.Load()
	assert.Equal(t, BranchUpToDate, g.status(false).State, "what the pull fetched is kept as origin's")
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

// TestPullNeverMovesARefThroughTheClonesOwnRefspecs reproduces a clone whose own
// remote.origin.fetch maps origin's branches onto its own, forced: a plain pull's fetch
// would force the branch onto origin's commit, dropping the clone's own commit, before the
// fast-forward check ever ran.
func TestPullNeverMovesARefThroughTheClonesOwnRefspecs(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	testutil.RunGit(t, g.dir, "config", "remote.origin.fetch", "+refs/heads/*:refs/heads/*")
	mine := g.commit(g.dir, "mine.txt")
	theirs := g.pushed("theirs.txt")
	g.status(false)

	run := g.pull()
	assert.Equal(t, scriptrun.StatusFailed, run.Status)
	assert.Equal(t, "Couldn't pull main: Not possible to fast-forward, aborting.", run.Summary)
	assert.Equal(t, mine, g.head(g.dir), "the clone's own commit stays on its branch")
	assert.Equal(t, mine, g.ref(g.dir, "refs/heads/main"))
	assert.Equal(t, theirs, g.ref(g.dir, "refs/remotes/origin/main"), "only origin's branch's own ref moved")
}

// TestPullKeepsWhatItFetchedAsOrigins moves origin on after the pull's Before asked it, as
// git is about to fetch: the status afterwards is up to date with what git fetched, never
// ahead of an answer from before.
func TestPullKeepsWhatItFetchedAsOrigins(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	g.pushed("two.txt")
	require.Equal(t, BranchBehind, g.status(false).State)

	var newer string

	g.asked.afterSetup(func() { newer = g.pushed("three.txt") })

	run := g.pull()
	require.Equal(t, scriptrun.StatusSucceeded, run.Status)
	require.Equal(t, newer, g.head(g.dir), "git fetched what origin had then")

	st := g.status(false)
	assert.Equal(t, BranchUpToDate, st.State)
	assert.Equal(t, newer[:7], st.Origin)
}

// TestPullSaysWhenTheCloneMovedWhileItRan switches the clone to another branch after the
// pull's Before checked it, as git is about to run: git fast-forwards whatever is checked
// out, and the run says so rather than that the branch asked for was up to date.
func TestPullSaysWhenTheCloneMovedWhileItRan(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	g.pushed("two.txt")
	g.status(false)

	g.asked.afterSetup(func() { testutil.RunGit(t, g.dir, "checkout", "-q", "-b", "feature") })

	run := g.pull()
	assert.Equal(t, scriptrun.StatusFailed, run.Status)
	assert.Equal(t, "The clone was switched to feature while the pull ran, so git may have pulled into it rather "+
		"than main. Look at the clone before going on.", run.Summary)
}

func TestPullRunsWithGitreadsEnvironment(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	g.status(false)

	spec := g.branch.pullSpec(clone{dir: g.dir}, "main")
	argv, err := spec.Before(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, argv)

	env := spec.BeforeEnv()
	for _, want := range []string{
		"GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1", "GIT_ALLOW_PROTOCOL=file:http:https:ssh", "LC_ALL=C",
		"GIT_SSH_COMMAND=ssh -o BatchMode=yes -o ConnectTimeout=10",
	} {
		assert.Contains(t, env, want)
	}

	assert.Equal(t, 10*time.Minute, spec.Kind.Timeout(), "a pull gets time enough never to be cut off writing files")
}

func TestPullHoldsStopOnceGitWritesTheClonesFiles(t *testing.T) {
	t.Parallel()

	out := &pullOutput{}
	assert.Equal(t, scriptrun.Progress{StopConfirm: stopPullConfirm}, out.progress(), "fetching, Stop asks first")

	_, err := out.Write([]byte("From file:///srv/origin\n * branch main -> FETCH_HEAD\nUpdating 1a2b3c4..5d6e"))
	require.NoError(t, err)
	assert.Equal(t, scriptrun.Progress{StopConfirm: stopPullConfirm}, out.progress(), "not until the line ends")

	_, err = out.Write([]byte("7f8\n"))
	require.NoError(t, err)
	assert.Equal(t, scriptrun.Progress{Step: "updating", StopRefused: stopPullRefused}, out.progress())
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
