package hooks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// hookInClone is the pre-commit hook's path relative to a clone, where a fake
// go-pre-commit writes it, in whichever clone its run starts in.
const hookInClone = ".git/hooks/" + hookPreCommit

// writesHook is a fake install that writes go-pre-commit's current hook as the clone's
// pre-commit hook, with mode, and prints what install-fresh printed.
func writesHook(t *testing.T, mode os.FileMode) testutil.FakeRun {
	t.Helper()

	return loadRecording(t, "install-fresh").fakeRun(testutil.FakeFile{Path: hookInClone, Content: currentHook, Mode: mode})
}

// act runs action in the clone at dir, waits for it to end, and returns the run and its
// result.
func (r *rig) act(action Action, dir string, req ActionRequest) (scriptrun.Run, ActionResult) {
	r.t.Helper()

	req.Dir = r.shown(dir)

	start := map[Action]func(ActionRequest) (int64, error){
		ActionInstall:   func(q ActionRequest) (int64, error) { return r.svc.Install(r.t.Context(), q) },
		ActionUpdate:    func(q ActionRequest) (int64, error) { return r.svc.Update(r.t.Context(), q) },
		ActionReinstall: func(q ActionRequest) (int64, error) { return r.svc.Reinstall(r.t.Context(), q) },
		ActionReplace:   func(q ActionRequest) (int64, error) { return r.svc.Replace(r.t.Context(), q) },
		ActionUninstall: func(q ActionRequest) (int64, error) { return r.svc.Uninstall(r.t.Context(), q) },
	}[action]

	id, err := start(req)
	require.NoError(r.t, err)

	run := r.ended(id)

	res, err := r.svc.ActionResult(r.t.Context(), id)
	require.NoError(r.t, err)

	return run, res
}

func TestInstallIntoACloneWithNoHook(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("api", cloneOpts{config: coreEnv, origin: "acme-org/api"})
	path := r.tool("1.13.1", writesHook(t, 0o755))

	assert.Equal(t, StateMissing, row(t, r.page(false), dir).State)

	run, res := r.act(ActionInstall, dir, ActionRequest{})

	assert.Equal(t, scriptrun.StatusSucceeded, run.Status)
	assert.Equal(t, "Install the hook", run.Title)
	assert.Equal(t, []string{path, "install", noColor}, run.Argv)
	assert.Equal(t, "Installed the pre-commit hook in acme-org/api.", run.Summary)
	assert.Equal(t, scriptrun.KindHooks, run.Kind)
	assert.Equal(t, string(integrationprefs.GoPreCommit), run.Result.Tool)
	assert.Empty(t, run.Result.Unrecognized)

	assert.Equal(t, ActionResult{
		Action: ActionInstall, Outcome: OutcomeInstalled, Message: "Installed the pre-commit hook in acme-org/api.",
		Repo: "acme-org/api", Name: "acme-org/api", Dir: "~/projects/api", HooksDir: "~/projects/api/.git/hooks",
		Removed: []string{},
	}, res)

	// The clone is scanned again, and the bytes it wrote are known to be current.
	assert.Equal(t, StateReady, row(t, r.page(false), dir).State)
	assert.Empty(t, r.status.runs(), "no status was needed")
}

func TestNothingInstalledWhenAnotherToolsHookIsInTheWayOnExitZero(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("sdk", cloneOpts{config: coreEnv, hook: huskyHook, origin: "acme-org/sdk"})
	rec := loadRecording(t, "install-over-other")
	require.Zero(t, rec.Exit, "go-pre-commit 1.13.1 exits 0 when another tool's hook stops it")
	r.tool("1.13.1", testutil.FakeRun{Stdout: rec.Stdout, Stderr: rec.Stderr})

	run, res := r.act(ActionInstall, dir, ActionRequest{})

	require.NotNil(t, run.ExitCode)
	assert.Zero(t, *run.ExitCode)
	assert.Equal(t, scriptrun.StatusFailed, run.Status, "exit 0 isn't success: the hook file says nothing was installed")
	assert.Equal(t, OutcomeBlocked, res.Outcome)
	assert.Equal(t, toolHusky, res.OtherTool)
	assert.Equal(t, "Nothing installed: husky's hook is in the way.", res.Message)
	assert.False(t, res.Shared, "the clone's own hooks folder")
	assert.Zero(t, res.SharedWith)
	assert.Equal(t, huskyHook, readHookText(t, dir))
}

func TestUpdateAnOutdatedHook(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("cli", cloneOpts{config: coreEnv, hook: oldHook})
	r.tool("1.13.1", writesHook(t, 0))

	run, res := r.act(ActionUpdate, dir, ActionRequest{})

	assert.Equal(t, scriptrun.StatusSucceeded, run.Status)
	assert.Equal(t, "Update the hook", run.Title)
	assert.Equal(t, OutcomeUpdated, res.Outcome)
	assert.Equal(t, "Updated the pre-commit hook in cli.", res.Message)
}

func TestReinstallACurrentHookChecksTheBytesItLeft(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	web := r.clone("web", cloneOpts{config: coreEnv, hook: currentHook})
	cli := r.clone("cli", cloneOpts{config: coreEnv, hook: oldHook})
	r.tool("1.13.1", loadRecording(t, "install-over-own").fakeRun())

	run, res := r.act(ActionReinstall, web, ActionRequest{})
	assert.Equal(t, scriptrun.StatusSucceeded, run.Status)
	assert.Equal(t, OutcomeCurrent, res.Outcome)
	assert.Equal(t, "The pre-commit hook in web was already current.", res.Message)
	assert.Len(t, r.status.runs(), 1, "status judged the bytes the install left as they were")

	// An install that said it succeeded and left an outdated hook didn't.
	run, res = r.act(ActionUpdate, cli, ActionRequest{})
	assert.Equal(t, scriptrun.StatusFailed, run.Status)
	assert.Equal(t, OutcomeFailed, res.Outcome)
	assert.Equal(t, "go-pre-commit couldn't update the hook in cli.", res.Message)
}

func TestReinstallWhoseBytesCantBeJudged(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("web", cloneOpts{config: coreEnv, hook: currentHook})
	r.tool("1.13.1", loadRecording(t, "install-over-own").fakeRun())
	r.status.sayInstead("status-no-config")

	run, res := r.act(ActionReinstall, dir, ActionRequest{})
	assert.Equal(t, scriptrun.StatusFailed, run.Status)
	assert.Equal(t, OutcomeUnverified, res.Outcome)
	assert.Equal(t, "Couldn't read what go-pre-commit did to the hooks in web.", res.Message)
}

func TestReplaceAnotherToolsHook(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("sdk", cloneOpts{config: coreEnv, hook: huskyHook, origin: "acme-org/sdk"})
	backup := filepath.Join(".git", "hooks", "pre-commit.backup.64138")
	path := r.tool("1.13.1", loadRecording(t, "install-force-over-other").fakeRun(
		testutil.FakeFile{Path: backup, From: hookInClone},
		testutil.FakeFile{Path: hookInClone, Content: currentHook, Mode: 0o755},
	))

	assert.Equal(t, ActionReplace, row(t, r.page(false), dir).Action)

	run, res := r.act(ActionReplace, dir, ActionRequest{})

	assert.Equal(t, []string{path, "install", "--force", noColor}, run.Argv)
	assert.Equal(t, scriptrun.StatusSucceeded, run.Status)
	assert.Equal(t, OutcomeReplaced, res.Outcome)
	assert.Equal(t, toolHusky, res.OtherTool)
	assert.Equal(t, "pre-commit.backup.64138", res.Backup)
	assert.Equal(t, "Replaced husky's pre-commit hook in acme-org/sdk. go-pre-commit kept it as "+
		"pre-commit.backup.64138, and puts it back if you uninstall.", res.Message)

	got := row(t, r.page(false), dir)
	assert.Equal(t, StateReady, got.State)
	assert.Equal(t, []string{"pre-commit.backup.64138"}, got.Backups)
}

func TestUninstallEveryHookTypeWithTheMarker(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("msg", cloneOpts{
		config: coreEnv, hook: currentHook, hooks: map[string]string{hookCommitMsg: currentHook, "pre-push": "#!/bin/sh\n"},
	})
	path := r.tool("1.13.1", loadRecording(t, "uninstall-both-types").fakeRun(
		testutil.FakeFile{Path: hookInClone, Remove: true},
		testutil.FakeFile{Path: filepath.Join(".git", "hooks", hookCommitMsg), Remove: true},
	))

	run, res := r.act(ActionUninstall, dir, ActionRequest{})

	assert.Equal(t, []string{path, "uninstall", noColor, "--hook-type", "pre-commit", "--hook-type", hookCommitMsg},
		run.Argv)
	assert.Equal(t, scriptrun.StatusSucceeded, run.Status)
	assert.Equal(t, "Uninstall the hook", run.Title)
	assert.Equal(t, OutcomeUninstalled, res.Outcome)
	assert.Equal(t, []string{"pre-commit", hookCommitMsg}, res.Removed)
	assert.Equal(t, "Uninstalled the pre-commit and commit-msg hooks from msg.", res.Message)
}

func TestUninstallPutsTheReplacedHookBack(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("sdk", cloneOpts{
		config: coreEnv, hook: currentHook, hooks: map[string]string{"pre-commit.backup.64150": huskyHook},
	})
	r.tool("1.13.1", loadRecording(t, "uninstall-restores-backup").fakeRun(
		testutil.FakeFile{Path: hookInClone, Remove: true},
		testutil.FakeFile{Path: hookInClone, From: filepath.Join(".git", "hooks", "pre-commit.backup.64150")},
	))

	run, res := r.act(ActionUninstall, dir, ActionRequest{})

	assert.Equal(t, scriptrun.StatusSucceeded, run.Status)
	assert.Equal(t, OutcomeUninstalled, res.Outcome)
	assert.True(t, res.Restored)
	assert.Equal(t, toolHusky, res.OtherTool)
	assert.Equal(t, "Uninstalled the pre-commit hook from sdk. Husky's pre-commit hook is back.", res.Message)
	assert.Equal(t, huskyHook, readHookText(t, dir))
}

func TestUninstallWithNothingToRemove(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("sdk", cloneOpts{config: coreEnv, hook: huskyHook})
	path := r.tool("1.13.1", loadRecording(t, "uninstall-not-installed").fakeRun())

	run, res := r.act(ActionUninstall, dir, ActionRequest{})

	assert.Equal(t, []string{path, "uninstall", noColor, "--hook-type", "pre-commit"}, run.Argv)
	assert.Equal(t, scriptrun.StatusSucceeded, run.Status)
	assert.Equal(t, OutcomeNoneToRemove, res.Outcome)
	assert.Equal(t, "There's no hook of go-pre-commit's to uninstall in sdk.", res.Message)
}

func TestUninstallThatLeftAHook(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("web", cloneOpts{config: coreEnv, hook: currentHook})
	r.tool("1.13.1", testutil.FakeRun{Stderr: "Error: failed to remove hook: permission denied\n", Exit: 1})

	run, res := r.act(ActionUninstall, dir, ActionRequest{})

	assert.Equal(t, scriptrun.StatusFailed, run.Status)
	assert.Equal(t, OutcomeFailed, res.Outcome)
	assert.Equal(t, "go-pre-commit couldn't uninstall the hook in web: failed to remove hook: permission denied.", res.Message)
}

func TestAnInstallThatLeavesTheHookNotExecutable(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("api", cloneOpts{config: coreEnv})
	r.tool("1.13.1", writesHook(t, 0o644))

	run, res := r.act(ActionInstall, dir, ActionRequest{})

	assert.Equal(t, scriptrun.StatusFailed, run.Status)
	assert.True(t, res.NotExecutable)
	assert.Equal(t, OutcomeFailed, res.Outcome)
	assert.Contains(t, res.Message, "isn't executable, so git won't run it")
}

func TestAnInstallThatFailed(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("api", cloneOpts{config: coreEnv})
	r.tool("1.13.1", loadRecording(t, "install-no-config").fakeRun())

	run, res := r.act(ActionInstall, dir, ActionRequest{})

	assert.Equal(t, scriptrun.StatusFailed, run.Status)
	assert.Equal(t, OutcomeFailed, res.Outcome)
	assert.Equal(t, "go-pre-commit couldn't install the hook in api: failed to load configuration: failed to load "+
		"configuration: failed to find environment configuration (.github/env/ directory or .github/.env.base).", res.Message)
}

func TestTheExitCodeCountsOnceGoPreCommitSaysSo(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("api", cloneOpts{config: coreEnv})
	failing := writesHook(t, 0o755)
	failing.Exit = 1

	r.tool("1.13.1", failing)

	_, res := r.act(ActionInstall, dir, ActionRequest{})
	assert.Equal(t, OutcomeInstalled, res.Outcome, "1.13 exits as it likes; the hook file decides")

	require.NoError(t, os.Remove(filepath.Join(dir, hookInClone)))
	r.tool("1.14.0", failing)

	_, res = r.act(ActionInstall, dir, ActionRequest{})
	assert.Equal(t, OutcomeFailed, res.Outcome, "from 1.14.0 a non-zero exit fails it too")
}

func TestASharedHooksFolderBlocksUntilConfirmed(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	shared := filepath.Join(r.home, ".githooks")
	require.NoError(t, os.MkdirAll(shared, 0o700)) // go-pre-commit makes it; its fake doesn't

	ops := r.clone("ops", cloneOpts{config: coreEnv, hooksPath: shared, origin: "acme-org/ops"})
	r.clone("web", cloneOpts{config: coreEnv, hooksPath: shared})
	r.tool("1.13.1", loadRecording(t, "global-hookspath-install").fakeRun(
		testutil.FakeFile{Path: filepath.Join(shared, hookPreCommit), Content: currentHook, Mode: 0o755},
	))

	p := r.page(false)
	assert.Equal(t, 2, row(t, p, ops).SharedWith)

	for _, action := range []Action{ActionInstall, ActionReplace, ActionUninstall} {
		_, err := r.svc.act(t.Context(), action, ActionRequest{Dir: r.shown(ops)})
		require.ErrorIs(t, err, ErrSharedFolder, action)
		assert.NoFileExists(t, filepath.Join(shared, hookPreCommit), action)
	}

	_, err := r.svc.Install(t.Context(), ActionRequest{Dir: r.shown(ops)})
	assert.Equal(t, "~/.githooks is shared by 2 of your repositories, and by any other repository that uses it. "+
		"Installing here installs for all of them.", uierr.Message(err))

	_, err = r.svc.Install(t.Context(), ActionRequest{Dir: r.shown(ops), Confirm: "~/projects"})
	require.ErrorIs(t, err, ErrSharedFolder, "a confirmation of another folder isn't one")

	run, res := r.act(ActionInstall, ops, ActionRequest{Confirm: row(t, p, ops).HooksDir})
	assert.Equal(t, scriptrun.StatusSucceeded, run.Status)
	assert.Equal(t, OutcomeInstalled, res.Outcome)
	assert.Equal(t, "~/.githooks", res.HooksDir)
	assert.True(t, res.Shared, "the result says the folder is shared, so the window asks before acting there again")
	assert.Equal(t, 2, res.SharedWith)
	assert.FileExists(t, filepath.Join(shared, hookPreCommit))
}

func TestActionsRefusedBeforeTheyStart(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("api", cloneOpts{config: coreEnv})
	plain := r.clone("plain", cloneOpts{})
	ctx := t.Context()

	_, err := r.svc.Install(ctx, ActionRequest{Dir: r.shown(plain)})
	require.ErrorIs(t, err, ErrNotSetUp)
	assert.Equal(t, "~/projects/plain has no go-pre-commit config (.github/env/ or .github/.env.base).", uierr.Message(err))

	_, err = r.svc.Install(ctx, ActionRequest{Dir: "~/elsewhere"})
	require.ErrorIs(t, err, ErrNoClone)
	assert.Equal(t, "~/elsewhere isn't a clone in ~/projects.", uierr.Message(err))

	_, err = r.svc.Install(ctx, ActionRequest{Dir: "relative/dir"})
	require.ErrorIs(t, err, ErrInvalidRequest)

	_, err = r.svc.Install(ctx, ActionRequest{Dir: r.shown(dir), RunsIn: "nowhere"})
	require.ErrorIs(t, err, ErrInvalidRequest)

	_, err = r.svc.act(ctx, "explode", ActionRequest{Dir: r.shown(dir)})
	require.ErrorIs(t, err, ErrInvalidRequest)

	release, _, ok := r.locks.TryLock(dir, clonelock.Holder{Kind: clonelock.KindChecks})
	require.True(t, ok)

	_, err = r.svc.Install(ctx, ActionRequest{Dir: r.shown(dir)})
	require.ErrorIs(t, err, clonelock.ErrBusy)
	release()

	for _, facts := range []integrations.CopyFacts{
		{State: integrations.StateMissing},
		{Path: "/opt/go-pre-commit", State: integrations.StateFound, Off: true},
		{Path: "/opt/go-pre-commit", State: integrations.StateTooOld},
		{Path: "/opt/go-pre-commit", State: integrations.StateWontRun},
	} {
		r.tools.set(ToolFacts{CopyFacts: facts}, "")

		_, err = r.svc.Install(ctx, ActionRequest{Dir: r.shown(dir)})
		require.ErrorIs(t, err, ErrNoTool)
		assert.Contains(t, uierr.Message(err), "Settings → Integrations")
	}

	bare := NewService(Config{Clones: r.clones, Git: r.git})
	_, err = bare.Install(ctx, ActionRequest{Dir: r.shown(dir)})
	require.ErrorIs(t, err, ErrNotSetUp)

	_, err = bare.InstallAll(ctx, []string{r.shown(dir)})
	require.ErrorIs(t, err, ErrNotSetUp)

	_, err = bare.ActionResult(ctx, 1)
	require.ErrorIs(t, err, ErrNotSetUp)
}

func TestARunsPlaceComesFromTheSetting(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("api", cloneOpts{config: coreEnv})
	r.svc.places = fakePlaces{where: integrationprefs.RunsInTerminal}

	_, err := r.svc.Install(t.Context(), ActionRequest{Dir: r.shown(dir)})
	require.ErrorIs(t, err, scriptrun.ErrNoTerminal, "the rig's runner has no terminal")

	r.svc.places = fakePlaces{err: errFake}
	_, err = r.svc.Install(t.Context(), ActionRequest{Dir: r.shown(dir)})
	require.ErrorIs(t, err, errFake)
}

func TestActionResultReadsOnlyAnEndedHooksAction(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("api", cloneOpts{config: coreEnv})
	r.tool("1.13.1", testutil.FakeRun{Sleep: runWait})

	id, err := r.svc.Install(t.Context(), ActionRequest{Dir: r.shown(dir)})
	require.NoError(t, err)

	_, err = r.svc.ActionResult(t.Context(), id)
	require.ErrorIs(t, err, ErrNoResult, "it is still going")

	require.NoError(t, r.runs.Stop(t.Context(), id))
	run := r.ended(id)
	assert.Equal(t, scriptrun.StatusStopped, run.Status)

	res, err := r.svc.ActionResult(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, OutcomeFailed, res.Outcome, "stopped before it wrote anything")

	_, err = r.svc.ChecksResult(t.Context(), id)
	require.ErrorIs(t, err, ErrNoResult, "a hooks action has no checks result")

	_, err = r.svc.ActionResult(t.Context(), id+100)
	require.ErrorIs(t, err, scriptrun.ErrNotFound)
}

func TestRunAgainDoesTheSameAction(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("sdk", cloneOpts{config: coreEnv, hook: huskyHook})
	r.tool("1.13.1", loadRecording(t, "install-over-other").fakeRun())

	first, _ := r.act(ActionInstall, dir, ActionRequest{})
	require.True(t, first.CanRunAgain)

	id, err := r.runs.RunAgain(t.Context(), first.ID)
	require.NoError(t, err)

	again := r.ended(id)
	assert.Equal(t, first.Argv, again.Argv)
	assert.Equal(t, first.Title, again.Title)

	res, err := r.svc.ActionResult(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, OutcomeBlocked, res.Outcome)
}

func TestInstallAll(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	shared := filepath.Join(r.home, ".githooks")
	api := r.clone("api", cloneOpts{config: coreEnv, origin: "acme-org/api"})
	cli := r.clone("cli", cloneOpts{config: coreEnv, hook: oldHook})
	sdk := r.clone("sdk", cloneOpts{config: coreEnv, hook: huskyHook})
	ops := r.clone("ops", cloneOpts{config: coreEnv, hooksPath: shared})
	web := r.clone("web", cloneOpts{config: coreEnv, hook: currentHook})
	off := r.clone("off", cloneOpts{config: "ENABLE_GO_PRE_COMMIT=false\n"})
	busy := r.clone("busy", cloneOpts{config: coreEnv})
	plain := r.clone("plain", cloneOpts{})
	require.NoError(t, os.Symlink(api, filepath.Join(r.root, "zz-api-link")))

	r.tool("1.13.1", writesHook(t, 0o755))

	release, _, ok := r.locks.TryLock(busy, clonelock.Holder{Kind: clonelock.KindRelease, Repo: "busy"})
	require.True(t, ok)

	defer release()

	dirs := []string{api, cli, sdk, ops, web, off, busy, plain, filepath.Join(r.root, "zz-api-link"), "~/elsewhere"}
	shown := make([]string, 0, len(dirs)+1)

	for _, d := range dirs {
		if filepath.IsAbs(d) {
			d = r.shown(d)
		}

		shown = append(shown, d)
	}

	got, err := r.svc.InstallAll(t.Context(), append(shown, r.shown(api)))
	require.NoError(t, err)
	require.Len(t, got.Lines, len(dirs), "a clone asked twice is done once")

	outcomes := map[string]ActionOutcome{}
	for _, l := range got.Lines {
		outcomes[l.Dir] = l.Outcome
	}

	assert.Equal(t, map[string]ActionOutcome{
		"~/projects/api": OutcomeInstalled, "~/projects/cli": OutcomeUpdated, "~/projects/sdk": OutcomeSkipped,
		"~/projects/ops": OutcomeSkipped, "~/projects/web": OutcomeSkipped, "~/projects/off": OutcomeSkipped,
		"~/projects/busy": OutcomeSkipped, "~/projects/plain": OutcomeSkipped, "~/projects/zz-api-link": OutcomeSkipped,
		"~/elsewhere": OutcomeSkipped,
	}, outcomes)

	byDir := func(d string) InstallLine {
		for _, l := range got.Lines {
			if l.Dir == d {
				return l
			}
		}

		return InstallLine{}
	}

	assert.Equal(t, ActionInstall, byDir("~/projects/api").Action)
	assert.Positive(t, byDir("~/projects/api").RunID)
	assert.Equal(t, "acme-org/api", byDir("~/projects/api").Name)
	assert.Equal(t, ActionUpdate, byDir("~/projects/cli").Action)
	assert.Equal(t, "Husky's hook is there, so it's left as it is.", byDir("~/projects/sdk").Message)
	assert.Equal(t, "Its hooks folder, ~/.githooks, is shared, so it's left as it is.", byDir("~/projects/ops").Message)
	assert.Equal(t, "Its hook is current already.", byDir("~/projects/web").Message)
	assert.Equal(t, "go-pre-commit is turned off in this repository.", byDir("~/projects/off").Message)
	assert.Equal(t, "A release is using busy right now.", byDir("~/projects/busy").Message)
	assert.Equal(t, "It has no go-pre-commit config.", byDir("~/projects/plain").Message)
	assert.Equal(t, "It shares its hooks folder with acme-org/api, done just now.", byDir("~/projects/zz-api-link").Message)
	assert.Equal(t, "~/elsewhere isn't a clone in ~/projects.", byDir("~/elsewhere").Message)
	assert.Equal(t, "1 installed, 1 updated, 8 skipped.", got.Message)

	assert.Equal(t, currentHook, readHookText(t, cli))
	assert.Equal(t, huskyHook, readHookText(t, sdk))
	assert.NoFileExists(t, filepath.Join(shared, hookPreCommit))
}

func TestInstallAllWithoutAUsableGoPreCommit(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.tools.set(ToolFacts{CopyFacts: integrations.CopyFacts{State: integrations.StateMissing}}, "")

	_, err := r.svc.InstallAll(t.Context(), nil)
	require.ErrorIs(t, err, ErrNoTool)

	r.tool("1.13.1", testutil.FakeRun{})

	got, err := r.svc.InstallAll(t.Context(), nil)
	require.NoError(t, err)
	assert.Equal(t, InstallAllResult{Lines: []InstallLine{}, Message: "Nothing to install or update."}, got)
}

func TestActionArgvAndTitles(t *testing.T) {
	t.Parallel()

	app, term := integrationprefs.RunsInApp, integrationprefs.RunsInTerminal

	assert.Equal(t, []string{"/gpc", "install", noColor}, actionArgv(ActionUpdate, "/gpc", nil, app))
	assert.Equal(t, []string{"/gpc", "install", noColor}, actionArgv(ActionReinstall, "/gpc", nil, app))

	// A run in the terminal keeps go-pre-commit's colors.
	assert.Equal(t, []string{"/gpc", "install"}, actionArgv(ActionInstall, "/gpc", nil, term))
	assert.Equal(t, []string{"/gpc", "install", "--force"}, actionArgv(ActionReplace, "/gpc", nil, term))
	assert.Equal(t, []string{"/gpc", "uninstall", "--hook-type", "pre-commit", "--hook-type", hookCommitMsg},
		actionArgv(ActionUninstall, "/gpc", []string{"pre-commit", hookCommitMsg}, term))

	tool := factsAt("/gpc", "1.14.1")
	assert.Equal(t, []string{"/gpc", "run", "--all-files", "--json"}, checksArgv(tool, ChecksIntent{Scope: ScopeAll}, term))
	assert.Equal(t, []string{"/gpc", "run", "lint"}, checksArgv(factsAt("/gpc", "1.13.1"),
		ChecksIntent{Scope: ScopeOne, Check: CheckLint}, term)[:3])
	assert.Equal(t, []string{"/gpc", "run", noColor}, checksArgv(factsAt("/gpc", "1.13.1"), ChecksIntent{Scope: ScopeStaged}, app))

	for _, a := range Actions() {
		assert.NotEmpty(t, actionTitle(a))
	}

	assert.False(t, Action("explode").Valid())
	assert.Equal(t, "Reinstall the hook", actionTitle(ActionReinstall))
	assert.Equal(t, "Replace the hook", actionTitle(ActionReplace))
	assert.Equal(t, "Install the hook", actionTitle(ActionInstall))
}

func TestActionMessages(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Replaced another tool's pre-commit hook in api.",
		actionMessage(ActionResult{Outcome: OutcomeReplaced, Name: "api"}))
	assert.Equal(t, "Nothing installed: another tool's hook is in the way.",
		actionMessage(ActionResult{Outcome: OutcomeBlocked, Name: "api"}))
	assert.Equal(t, "Uninstalled the pre-commit hook from api. Another tool's pre-commit hook is back.",
		actionMessage(ActionResult{Outcome: OutcomeUninstalled, Name: "api", Removed: []string{"pre-commit"}, Restored: true}))
	assert.Equal(t, "Couldn't read what go-pre-commit did to the hooks in api: permission denied.",
		actionMessage(ActionResult{Outcome: OutcomeUnverified, Name: "api", Error: "permission denied."}))
	assert.Equal(t, "go-pre-commit couldn't replace the hook in api.",
		actionMessage(ActionResult{Action: ActionReplace, Outcome: OutcomeFailed, Name: "api"}))
	assert.Equal(t, "1 already current, 1 blocked by another tool's hook, 1 failed, 1 unverified.",
		installAllMessage([]InstallLine{
			{Outcome: OutcomeBlocked}, {Outcome: OutcomeFailed}, {Outcome: OutcomeUnverified}, {Outcome: OutcomeCurrent},
		}))
}

func TestRunEnv(t *testing.T) {
	t.Parallel()

	base := []string{"PATH=/opt/bin", "NO_COLOR=0", "HOME=/srv/home"}

	assert.Equal(t, []string{
		"PATH=/opt/bin", "HOME=/srv/home", "GIT_TERMINAL_PROMPT=0", "GO_PRE_COMMIT_NO_UPDATE_CHECK=1", "NO_COLOR=1",
	}, runEnv(base, integrationprefs.RunsInApp))
	assert.Equal(t, []string{"GO_PRE_COMMIT_NO_UPDATE_CHECK=1"}, runEnv(base, integrationprefs.RunsInTerminal),
		"a terminal keeps its colors and its own shell's environment")
}

func TestInstallAllReadsTheCloneRootsOnce(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	api := r.clone("api", cloneOpts{config: coreEnv, origin: "acme-org/api"})
	cli := r.clone("cli", cloneOpts{config: coreEnv})
	sdk := r.clone("sdk", cloneOpts{config: coreEnv, hook: huskyHook})
	r.tool("1.13.1", writesHook(t, 0o755))
	r.page(false)

	listed := r.clones.listings()
	rooted, asked := r.clones.counts()

	got, err := r.svc.InstallAll(t.Context(), []string{r.shown(api), r.shown(cli), r.shown(sdk)})
	require.NoError(t, err)
	require.Len(t, got.Lines, 3)
	assert.Equal(t, "acme-org/api", got.Lines[0].Name, "the name the scan read")

	nowRooted, nowAsked := r.clones.counts()
	assert.Equal(t, 1, r.clones.listings()-listed, "the clones are listed once")
	assert.Equal(t, 1, nowRooted-rooted, "the clone root is read once")
	assert.Equal(t, asked, nowAsked, "the origins the scan read aren't read again")

	// Each line is told once its run let go of its clone and its end was recorded.
	for _, l := range got.Lines[:2] {
		require.Positive(t, l.RunID)

		run, runErr := r.runs.Run(t.Context(), l.RunID)
		require.NoError(t, runErr)
		assert.Equal(t, scriptrun.StatusSucceeded, run.Status)
	}

	r.clones.setDirsErr(errFake)

	_, err = r.svc.InstallAll(t.Context(), []string{r.shown(api)})
	require.ErrorIs(t, err, errFake)
}
