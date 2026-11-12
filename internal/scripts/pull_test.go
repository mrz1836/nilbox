package scripts

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/execpath"
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
	assert.Equal(t, map[string]any{"branch": "main", "upstream": "main", "before": g.first}, run.Result.Intent,
		"with what its Before read, for a pull followed again after a restart")
	assert.Equal(t, third, g.head(g.dir))
	assert.Equal(t, third, g.ref(g.dir, "refs/remotes/origin/main"), "origin's branch as the clone keeps it moved too")
	assert.FileExists(t, filepath.Join(g.dir, "three.txt"))

	env := g.environ()
	git, err := execpath.Search{Path: shellenv.Lookup(env, "PATH"), Home: shellenv.Lookup(env, "HOME")}.Find("git")
	require.NoError(t, err)

	assert.Equal(t, []string{
		git, "pull", "--ff-only", "--no-rebase", "--refmap=", "--no-tags", "--no-autostash", "--no-recurse-submodules",
		"--no-verify-signatures", "--no-stat", "--upload-pack=git-upload-pack", originRemote,
		"+refs/heads/main:refs/remotes/origin/main",
	}, run.Argv, "gitread's own git, by its absolute path, and the pull's own words alone: its settings are in its environment")

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

	dir, err := filepath.EvalSymlinks(g.dir)
	require.NoError(t, err)

	// Only git's own variables are looked at, so a failure never prints the rest of the
	// test's environment.
	env := slices.DeleteFunc(spec.BeforeEnv(), func(kv string) bool {
		return !strings.HasPrefix(kv, "GIT_") && !strings.HasPrefix(kv, "LC_ALL=")
	})

	for _, want := range []string{
		"GIT_TERMINAL_PROMPT=0", "GIT_NO_LAZY_FETCH=1", "GIT_ALLOW_PROTOCOL=file:http:https:ssh", "LC_ALL=C",
		"GIT_SSH_COMMAND=ssh -o BatchMode=yes -o ConnectTimeout=10", "GIT_DIR=" + filepath.Join(dir, ".git"),
		"GIT_WORK_TREE=" + dir, "GIT_NO_REPLACE_OBJECTS=1", "GIT_CONFIG_KEY_0=core.hooksPath",
		"GIT_CONFIG_VALUE_0=" + os.DevNull,
	} {
		assert.Contains(t, env, want)
	}

	settings := configSettings(env)
	for _, want := range []string{
		"core.alternateRefsCommand=true", "maintenance.auto=false", "gc.auto=0", "hook.post-merge.enabled=false",
		"credential.protectProtocol=true",
	} {
		assert.Contains(t, settings, want)
	}

	assert.Equal(t, []string{"pull"}, argv[1:2], "no -c setting on the command line")

	assert.Equal(t, 30*time.Minute, spec.Kind.Timeout(), "a pull gets time enough never to be cut off writing files")
}

// TestPullHoldsStopForItsWholeRun: git writes its output only as it exits, so nothing
// read from it says in time that it has begun writing the clone's files. Stop, and so a
// quit, are held back from the start, saying the pull ends on its own within its limit.
func TestPullHoldsStopForItsWholeRun(t *testing.T) {
	t.Parallel()

	spec := (&Service{}).pullSpec(clone{dir: "/srv/projects/sdk"}, "main")
	require.NotNil(t, spec.Progress)
	assert.Equal(t, scriptrun.Progress{StopRefused: stopPullRefused}, spec.Progress())
	assert.Contains(t, stopPullRefused, "within thirty minutes")
	assert.Equal(t, 30*time.Minute, spec.Kind.Timeout())
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
	assert.Equal(t, "Check acme-org/sdk's branch again before pulling it.", uierr.Message(err), "no status, no pull")

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

// TestPullWeighsOriginAgainUnderTheLockAskingItNothing: the pull's Before weighs origin
// with gitread's guards, from the clone alone, and the pull's own fetch is the one time
// origin is asked.
func TestPullWeighsOriginAgainUnderTheLockAskingItNothing(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	g.pushed("two.txt")
	g.status(false)
	asked := g.asked.remote.Load()

	require.Equal(t, scriptrun.StatusSucceeded, g.pull().Status)
	assert.Equal(t, asked, g.asked.remote.Load(), "no ls-remote before the pull's own fetch")

	pwned := filepath.Join(g.home, "pwned")
	testutil.RunGit(t, g.dir, "remote", "set-url", "origin", "ext::sh -c touch% "+pwned)

	_, err := g.branch.Pull(t.Context(), g.shown(g.dir))
	require.ErrorIs(t, err, ErrCantPull)
	assert.Contains(t, uierr.Message(err), "remote helper", "gitread's guard refuses origin before git runs")
	assert.Equal(t, 1, g.recorded(), "only the first pull ran")
	assert.NoFileExists(t, pwned)
}

func TestPullOfABranchOriginNoLongerHas(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	testutil.RunGit(t, g.dir, "checkout", "-q", "-b", "feature")
	testutil.RunGit(t, g.dir, "config", "branch.feature.remote", "origin")
	testutil.RunGit(t, g.dir, "config", "branch.feature.merge", "refs/heads/feature")
	g.status(false)

	run := g.pull()
	assert.Equal(t, scriptrun.StatusFailed, run.Status)
	assert.Equal(t, "Couldn't pull feature: couldn't find remote ref refs/heads/feature.", run.Summary,
		"git's own words, from the pull's own fetch")
}

// TestPullOfABloblessClone pulls into a partial clone that has none of its files'
// contents: the fast-forward fetches those it writes from origin, the clone's promisor
// remote, under the same guards.
func TestPullOfABloblessClone(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	testutil.RunGit(t, g.origin, "config", "uploadpack.allowFilter", "true")
	require.NoError(t, os.RemoveAll(g.dir))
	testutil.RunGit(t, g.root, "clone", "-q", "--filter=blob:none", g.url(), g.dir)

	third := g.pushed("three.txt")
	require.Equal(t, BranchBehind, g.status(false).State)

	run := g.pull()
	require.Equal(t, scriptrun.StatusSucceeded, run.Status, run.Summary)
	assert.Equal(t, third, g.head(g.dir))

	got, err := os.ReadFile(filepath.Join(g.dir, "three.txt"))
	require.NoError(t, err)
	assert.Equal(t, "three.txt\n", string(got))
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

// configSettings returns the settings env passes git through GIT_CONFIG_COUNT, key=value
// each.
func configSettings(env []string) []string {
	var out []string

	for i := 0; ; i++ {
		key := lookup(env, "GIT_CONFIG_KEY_"+strconv.Itoa(i))
		if key == "" {
			return out
		}

		out = append(out, key+"="+lookup(env, "GIT_CONFIG_VALUE_"+strconv.Itoa(i)))
	}
}

// lookup returns the value env gives key, or "".
func lookup(env []string, key string) string {
	for _, kv := range env {
		if value, ok := strings.CutPrefix(kv, key+"="); ok {
			return value
		}
	}

	return ""
}

// TestPullKeepsWhenOriginWasAsked pulls a minute after origin was asked: the status
// afterwards takes the commit the pull's fetch stored, but still says origin was asked a
// minute before, and goes on asking it again when that answer is OriginKept old.
func TestPullKeepsWhenOriginWasAsked(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	two := g.pushed("two.txt")
	asked := g.status(false).CheckedAt

	g.clock.Advance(time.Minute)
	require.Equal(t, scriptrun.StatusSucceeded, g.pull().Status)

	st := g.status(false)
	assert.Equal(t, BranchUpToDate, st.State)
	assert.Equal(t, two[:7], st.Origin, "the commit the pull's fetch stored")
	assert.Equal(t, asked, st.CheckedAt, "origin was asked then, not now")

	calls := g.asked.remote.Load()
	g.clock.Advance(OriginKept)
	g.status(false)
	assert.Equal(t, calls+1, g.asked.remote.Load(), "an answer OriginKept old is asked again")
}

// TestAPullItsLimitCutOffSaysWhatItLeft ends a pull at its limit, as git writes the
// clone's files, its index still locked: the run says it was cut off partway, what it found,
// and what to do, rather than the runner's words alone.
func TestAPullItsLimitCutOffSaysWhatItLeft(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	g.pushed("two.txt")
	g.status(false)

	lock := filepath.Join(g.dir, ".git", "index.lock")
	require.NoError(t, os.WriteFile(lock, nil, 0o600))

	v := NewVerifier(g.branch).Verify(g.pullRecord(g.first))
	got := v.Finish(t.Context(), scriptrun.Ending{TimedOut: true}, scriptrun.Outcome{
		Status: scriptrun.StatusFailed, Summary: "It ran past its limit of 30m, so it was stopped.",
	})

	assert.Equal(t, scriptrun.Outcome{
		Status: scriptrun.StatusFailed,
		Summary: "It ran past its limit of 30m, so it was stopped. The pull was cut off partway, so the clone may be " +
			"partly updated: main is still at " + g.first[:7] + ", git's index is still locked (.git/index.lock). Open " +
			"the clone in a terminal and run git status to see where it stands.",
	}, got)

	require.NoError(t, os.Remove(lock))
	testutil.RunGit(t, g.dir, "checkout", "-q", "--detach")

	got = v.Finish(t.Context(), scriptrun.Ending{TimedOut: true}, scriptrun.Outcome{Status: scriptrun.StatusFailed, Summary: "x"})
	assert.Contains(t, got.Summary, "cut off partway, so the clone may be partly updated: HEAD is detached now, main is "+
		"still at "+g.first[:7]+".")
}

// pullRecord is the record of a pull of main in the rig's clone from before a restart, as
// its Before left it: main at before.
func (g *gitRig) pullRecord(before string) scriptrun.Record {
	return scriptrun.Record{
		ID: 7, Kind: scriptrun.KindPull, Dir: g.dir, Title: "Pull main", Status: scriptrun.StatusOrphaned,
		Result: scriptrun.Result{Intent: map[string]any{"branch": "main", "upstream": "main", "before": before}},
	}
}

// TestAPullFollowedAgainAfterARestart verifies a pull left going at a quit, as the runner
// picks it up after a restart: Stop is held back again, as git may still be writing, and
// its end is worded from the branch, as a pull's own Finish words it.
func TestAPullFollowedAgainAfterARestart(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	two := g.pushed("two.txt")
	g.status(false)

	v := NewVerifier(g.branch).Verify(g.pullRecord(g.first))
	require.NotNil(t, v.Progress)
	assert.Equal(t, scriptrun.Progress{StopRefused: stopPullRefused}, v.Progress(), "Stop is held from the start")
	require.NotNil(t, v.Output)
	require.NotNil(t, v.Finish)

	unseen := scriptrun.Outcome{Status: scriptrun.StatusInterrupted, Summary: "Its exit status is unknown."}

	got := v.Finish(t.Context(), scriptrun.Ending{}, unseen)
	assert.Equal(t, scriptrun.StatusInterrupted, got.Status)
	assert.Equal(t, "Its exit status is unknown. main is where it was, at "+g.first[:7]+".", got.Summary)

	// git, left going at the quit, fast-forwards the branch while the app is away.
	testutil.RunGit(t, g.dir, "fetch", "-q", "origin")
	testutil.RunGit(t, g.dir, "merge", "-q", "--ff-only", "origin/main")

	got = v.Finish(t.Context(), scriptrun.Ending{}, unseen)
	assert.Equal(t, scriptrun.Outcome{
		Status: scriptrun.StatusSucceeded, Summary: "Updated main from " + g.first[:7] + " to " + two[:7] + ", 1 new commit.",
	}, got, "the branch moved on from where the pull found it")

	ok := 0
	got = v.Finish(t.Context(), scriptrun.Ending{ExitCode: &ok}, scriptrun.Outcome{Status: scriptrun.StatusSucceeded})
	assert.Equal(t, "Updated main from "+g.first[:7]+" to "+two[:7]+", 1 new commit.", got.Summary,
		"an end that was seen, as its Spec's Finish words it")

	got = v.Finish(t.Context(), scriptrun.Ending{Quit: true}, scriptrun.Outcome{Status: scriptrun.StatusStopped, Summary: "x"})
	assert.Equal(t, scriptrun.StatusStopped, got.Status)
	assert.True(t, strings.HasPrefix(got.Summary, "x. The pull was cut off partway"), "a pull stopped says it was cut off")

	unread := NewVerifier(g.branch).Verify(scriptrun.Record{
		ID: 8, Kind: scriptrun.KindPull, Dir: g.dir, Result: scriptrun.Result{Intent: "nonsense"},
	})
	require.NotNil(t, unread.Progress, "a pull whose intent can't be read still holds Stop")
	assert.Equal(t, scriptrun.Progress{StopRefused: stopPullRefused}, unread.Progress())
	assert.Nil(t, unread.Finish)

	other := NewVerifier(g.branch).Verify(scriptrun.Record{Kind: scriptrun.KindScript})
	assert.Nil(t, other.Progress)
	assert.Nil(t, other.Finish)
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
