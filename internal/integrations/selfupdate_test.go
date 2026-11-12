package integrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/toolinstall"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// errNoSlot stands for a runner that can't start anything.
var errNoSlot = errors.New("no slot")

// play stands for a tool's own updater: what it does to the copy, what it prints, and how
// it ends.
type play func(spec scriptrun.Spec) (output string, end scriptrun.Ending)

// fakeRuns stands for the runner: it starts a run as the engine does, without a process.
// Start runs the spec's Before, records the run, and plays the updater, then calls its
// Finish, Recorded, and Released. With gate set, the run waits for it to close, on a
// goroutine of its own, and done closes once the run has ended.
type fakeRuns struct {
	play play

	// refuse fails Start before Before, as a busy folder does; beforeHook runs just before
	// Before, as something that changes while the run waits for its lock; noLaunch records
	// the run, then fails it as one that couldn't start.
	refuse     error
	beforeHook func()
	noLaunch   bool

	gate chan struct{}
	done chan struct{}

	// orphan is mage-x's own update the runner follows from before the app restarted, and
	// orphans what it says first, one answer each time it is asked, before orphan.
	orphan  int64
	orphans []int64

	mu       sync.Mutex
	specs    []scriptrun.Spec
	outcomes []scriptrun.Outcome
}

func (f *fakeRuns) Updating(tool string) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()

	if tool != string(integrationprefs.MageX) {
		return 0
	}

	if len(f.orphans) > 0 {
		next := f.orphans[0]
		f.orphans = f.orphans[1:]

		return next
	}

	return f.orphan
}

func (f *fakeRuns) Start(ctx context.Context, spec scriptrun.Spec) (int64, error) {
	if f.refuse != nil {
		return 0, f.refuse
	}

	if f.beforeHook != nil {
		f.beforeHook()
	}

	argv, err := spec.Before(ctx)
	if err != nil {
		return 0, err
	}

	spec.Argv = argv

	f.mu.Lock()
	f.specs = append(f.specs, spec)
	id := int64(len(f.specs))
	f.mu.Unlock()

	if f.noLaunch {
		f.end(ctx, spec, id, scriptrun.Outcome{Status: scriptrun.StatusFailed, Summary: "It couldn't start: no such file."})

		return id, uierr.New("The run couldn't start.", errNoProgram)
	}

	if f.gate == nil {
		f.run(ctx, spec, id)

		return id, nil
	}

	go func() {
		defer close(f.done)

		<-f.gate
		f.run(context.WithoutCancel(ctx), spec, id)
	}()

	return id, nil
}

// run plays the updater and ends the run.
func (f *fakeRuns) run(ctx context.Context, spec scriptrun.Spec, id int64) {
	output, end := f.play(spec)
	_, _ = spec.Output.Write([]byte(output))

	f.end(ctx, spec, id, spec.Finish(ctx, end, engineOutcome(end)))
}

// end records the run's outcome, then lets go of it.
func (f *fakeRuns) end(ctx context.Context, spec scriptrun.Spec, id int64, out scriptrun.Outcome) {
	f.mu.Lock()
	f.outcomes = append(f.outcomes, out)
	f.mu.Unlock()

	spec.Recorded(ctx, id, out)
	spec.Released(id)
}

// started returns the specs started, and the outcomes recorded.
func (f *fakeRuns) started() ([]scriptrun.Spec, []scriptrun.Outcome) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]scriptrun.Spec{}, f.specs...), append([]scriptrun.Outcome{}, f.outcomes...)
}

// engineOutcome is the outcome the engine works out from how a run ended.
func engineOutcome(end scriptrun.Ending) scriptrun.Outcome {
	switch {
	case end.Stopped:
		return scriptrun.Outcome{Status: scriptrun.StatusStopped, Summary: "Stopped."}
	case end.ExitCode == nil:
		return scriptrun.Outcome{Status: scriptrun.StatusInterrupted, Summary: "Its exit status is unknown."}
	case *end.ExitCode == 0:
		return scriptrun.Outcome{Status: scriptrun.StatusSucceeded, Summary: "Finished."}
	default:
		return scriptrun.Outcome{Status: scriptrun.StatusFailed, Summary: fmt.Sprintf("It exited with status %d.", *end.ExitCode)}
	}
}

// exited is an updater that exited on its own with code.
func exited(code int) scriptrun.Ending {
	return scriptrun.Ending{ExitCode: &code}
}

// swap replaces the copy at path with one that prints out, as go-selfupdate does: a new
// file beside it, renamed over it.
func swap(path, out string) error {
	if err := os.WriteFile(path+".new", []byte(out), 0o700); err != nil { //nolint:gosec // a fake tool must be executable
		return err
	}

	return os.Rename(path+".new", path)
}

// updatesToNewer plays an updater that swaps in a copy at the newer version, says so, and
// exits 0.
func updatesToNewer(spec scriptrun.Spec) (string, scriptrun.Ending) {
	if err := swap(spec.Argv[0], magexSays(newerMagex).stdout); err != nil {
		return "Error: " + err.Error() + "\n", exited(1)
	}

	return "\n=== Installing Update ===\n\nUpdating from 1.29.5 to " + newerMagex + "\n✅ Updated magex to " + newerMagex + "\n",
		exited(0)
}

// says plays an updater that changes nothing, prints output, and ends with end.
func says(output string, end scriptrun.Ending) play {
	return func(scriptrun.Spec) (string, scriptrun.Ending) { return output, end }
}

// updateHarness is an install harness whose copies can update themselves.
type updateHarness struct {
	*installHarness

	runs *fakeRuns
}

func newUpdateHarness(t *testing.T, p play) *updateHarness {
	t.Helper()

	h := &updateHarness{installHarness: newInstallHarness(t), runs: &fakeRuns{play: p}}
	h.svc.runs = h.runs

	return h
}

// magex returns mage-x as the tools list it now.
func (h *updateHarness) magex(t *testing.T) Tool {
	t.Helper()

	_, tools := h.list(t)

	return tools[integrationprefs.MageX]
}

// onPath has the shell's PATH hold dirs, and go install's folders follow the shell's
// environment, as dirsFrom works them out; nothing else of the machine's is searched.
func (h *updateHarness) onPath(dirs ...string) {
	h.svc.dirsFor = func(env []string, home string) searchDirs {
		got := h.m.dirs()
		got.path, got.goBins = dirs, dirsFrom(env, home).goBins

		return got
	}
}

// putGo puts a go in pathA, first on the shell's PATH, that answers a.
func (h *updateHarness) putGo(a answer) string {
	path := filepath.Join(h.m.pathA, "go")
	require.NoError(h.m.t, os.WriteFile(path, nil, 0o700)) //nolint:gosec // a fake tool must be executable

	h.m.mu.Lock()
	h.m.answers[path] = a
	h.m.mu.Unlock()

	return path
}

func TestACopyThatCanUpdateItselfRunsItsOwnUpdater(t *testing.T) {
	t.Parallel()

	h := newUpdateHarness(t, updatesToNewer)
	old := h.m.put(h.m.pathB, magexSays(recordedMagex))

	plan := h.ready(t)["mage-x"].Install
	assert.Equal(t, InstallPlan{
		Action: ActionUpdate, Version: newerMagex, Archive: "mage-x_1.30.2_darwin_arm64.tar.gz",
		TargetDir: "~/.local/bin", Target: "~/.local/bin/magex", Replaces: recordedMagex,
		ReplacesPath: "~/.local/bin/magex", Updater: "~/.local/bin/magex update:install",
	}, plan, "a copy the app didn't install updates itself, without asking to replace it")

	got, err := h.svc.Update(t.Context(), integrationprefs.MageX)
	require.NoError(t, err)
	assert.Equal(t, int64(1), got.Run, "the window opens the run")
	assert.Empty(t, got.Message, "the run says what it did as it ends")

	specs, outcomes := h.runs.started()
	require.Len(t, specs, 1)

	spec := specs[0]
	assert.Equal(t, scriptrun.KindUpdate, spec.Kind)
	assert.Equal(t, "Update magex", spec.Title)
	assert.Equal(t, "mage-x", spec.Source)
	assert.Equal(t, "mage-x", spec.Tool)
	assert.Equal(t, h.m.pathB, spec.Dir, "it runs in the copy's folder, never a clone")
	assert.Equal(t, scriptrun.RunsInApp, spec.RunsIn)
	assert.Equal(t, []string{old, "update:install"}, spec.Argv)
	assert.Equal(t, []string{
		"HOME=/Users/example", "GIT_TERMINAL_PROMPT=0", "MAGEX_NO_UPDATE_CHECK=1", "NO_COLOR=1",
	}, spec.Env, "the shell's environment, with the tool's quiet variables")
	assert.Equal(t, updateIntent{Tool: integrationprefs.MageX, Path: old, From: recordedMagex}, spec.Intent)

	require.Len(t, outcomes, 1)
	assert.Equal(t, scriptrun.StatusSucceeded, outcomes[0].Status)
	assert.Equal(t, "Updated magex to "+newerMagex+".", outcomes[0].Summary)
	assert.Equal(t, updateResult{
		Tool: integrationprefs.MageX, From: recordedMagex, To: newerMagex, Updated: true,
		Message: "Updated magex to " + newerMagex,
	}, outcomes[0].Detail)

	magex := h.magex(t)
	assert.Equal(t, StateFound, magex.State, "the tools were looked for again")
	assert.Equal(t, newerMagex, magex.Copy.Version.Raw)
	assert.Equal(t, SourceRelease, magex.Copy.Source, "a release's copy stays a release's")
	assert.Equal(t, SelfUpdate{Run: 1, Updated: true, Message: "Updated magex to " + newerMagex}, magex.SelfUpdate)
	assert.Empty(t, magex.Progress.Step)
	assert.NoDirExists(t, filepath.Join(h.m.root, "downloads"), "the app downloaded nothing itself")
}

func TestACopyTheAppInstalledStaysTheAppsOnceItUpdatesItself(t *testing.T) {
	t.Parallel()

	h := newUpdateHarness(t, updatesToNewer)
	h.withRecords(t)

	path := filepath.Join(h.m.pathB, "magex")
	program := magexSays(recordedMagex).stdout
	require.NoError(t, os.WriteFile(path, []byte(program), 0o700)) //nolint:gosec // a fake tool must be executable

	archive := MageX().Asset(recordedMagex, "darwin", "arm64")
	h.svc.record(t.Context(), MageX(), toolinstall.Placed{
		Path: paths.Resolved(path), SHA256: hexSHA([]byte(program)), Version: recordedMagex, Archive: archive,
	}, "")

	tools := h.ready(t)
	require.Equal(t, SourceApp, tools["mage-x"].Copy.Source)
	assert.NotEmpty(t, tools["mage-x"].Install.Updater, "the app's own copy updates itself too")

	_, err := h.svc.Update(t.Context(), integrationprefs.MageX)
	require.NoError(t, err)

	specs, _ := h.runs.started()
	require.Len(t, specs, 1)
	assert.Equal(t, updateIntent{Tool: integrationprefs.MageX, Path: path, From: recordedMagex, Installed: true}, specs[0].Intent)

	magex := h.magex(t)
	assert.Equal(t, newerMagex, magex.Copy.Version.Raw)
	assert.Equal(t, SourceApp, magex.Copy.Source, "the same path is still the app's")
	assert.Equal(t, "mage-x_1.30.2_darwin_arm64.tar.gz", magex.Copy.Archive)

	row, err := h.svc.queries.LatestInstallAt(t.Context(), paths.Resolved(path))
	require.NoError(t, err)
	assert.Equal(t, newerMagex, row.Version)
	assert.Equal(t, recordedMagex, row.Replaced)
	assert.Equal(t, hexSHA([]byte(magexSays(newerMagex).stdout)), row.Sha256)
}

func TestAnUpdateShowsWhileItsUpdaterRuns(t *testing.T) {
	t.Parallel()

	h := newUpdateHarness(t, updatesToNewer)
	h.runs.gate, h.runs.done = make(chan struct{}), make(chan struct{})
	h.m.put(h.m.pathB, magexSays(recordedMagex))
	h.ready(t)

	got, err := h.svc.Update(t.Context(), integrationprefs.MageX)
	require.NoError(t, err)

	var shown Tool

	for _, tl := range got.Tools.Tools {
		if tl.ID == integrationprefs.MageX {
			shown = tl
		}
	}

	assert.Equal(t, InstallProgress{Step: StepUpdating}, shown.Progress, "the card shows it updating")
	assert.Equal(t, SelfUpdate{Run: got.Run, Running: true}, shown.SelfUpdate)

	for _, again := range []func(context.Context, integrationprefs.ID) (InstallResult, error){
		h.svc.Update, h.svc.Install, h.svc.Reinstall,
	} {
		_, err = again(t.Context(), integrationprefs.MageX)
		require.ErrorIs(t, err, ErrInstallRefused)
		assert.Equal(t, "magex is updating itself. Try again once it ends.", uierr.Message(err))
	}

	// The installer waits for it too.
	h.svc.runs = nil
	_, err = h.svc.Update(t.Context(), integrationprefs.MageX)
	require.ErrorIs(t, err, ErrInstallRefused)
	assert.Equal(t, "magex is updating itself. Try again once it ends.", uierr.Message(err))

	h.svc.runs = h.runs

	before := h.events.Count()

	close(h.runs.gate)
	<-h.runs.done

	magex := h.magex(t)
	assert.Empty(t, magex.Progress.Step)
	assert.Equal(t, SelfUpdate{Run: got.Run, Updated: true, Message: "Updated magex to " + newerMagex}, magex.SelfUpdate)

	heard := h.events.All()[before:]
	require.NotEmpty(t, heard, "the window heard it end")
	assert.Equal(t, event.ToolsUpdated{}.Name(), heard[len(heard)-1].Name())
}

func TestAnUpdaterThatDidntUpdateSaysWhy(t *testing.T) {
	t.Parallel()

	stopped := exited(-1)
	stopped.Stopped = true

	tests := []struct {
		name     string
		play     play
		status   scriptrun.Status
		message  string
		updated  bool
		warnings []string
	}{
		{
			name:    "it failed",
			play:    says("\n=== Installing Update ===\n\n❌ Error: update failed: checksum mismatch\n", exited(1)),
			status:  scriptrun.StatusFailed,
			message: "magex couldn't update itself: update failed: checksum mismatch.",
		},
		{
			name:    "it failed without a word",
			play:    says("", exited(2)),
			status:  scriptrun.StatusFailed,
			message: "magex couldn't update itself: it exited with status 2.",
		},
		{
			name:    "it was up to date",
			play:    says("magex is already up to date (1.29.5)\n", exited(0)),
			status:  scriptrun.StatusSucceeded,
			message: "magex is already up to date (1.29.5).",
		},
		{
			name: "it wouldn't touch the copy",
			play: says("12:00:01 [WARN] magex was installed by another tool and cannot self-update in place.\n"+
				"12:00:01 [INFO] To update, run:\n", exited(0)),
			status:   scriptrun.StatusFailed,
			message:  "magex is still 1.29.5: magex was installed by another tool and cannot self-update in place.",
			warnings: []string{"magex was installed by another tool and cannot self-update in place."},
		},
		{
			name:     "it warned, marked",
			play:     says("\u26a0\ufe0f  careful\n", exited(0)),
			status:   scriptrun.StatusFailed,
			message:  "magex is still 1.29.5: careful.",
			warnings: []string{"careful"},
		},
		{
			name:    "it said nothing",
			play:    says("", exited(0)),
			status:  scriptrun.StatusFailed,
			message: "magex is still 1.29.5.",
		},
		{
			name:    "it was stopped",
			play:    says("Updating from 1.29.5 to 1.30.2\n", stopped),
			status:  scriptrun.StatusStopped,
			message: "Stopped. magex is still 1.29.5.",
		},
		{
			name: "the new copy won't run",
			play: func(spec scriptrun.Spec) (string, scriptrun.Ending) {
				_ = swap(spec.Argv[0], "garbage")

				return "Updated magex to 1.30.2\n", exited(0)
			},
			status:  scriptrun.StatusFailed,
			message: "magex doesn't run since its update: it printed no version.",
		},
		{
			name: "it updated, then failed",
			play: func(spec scriptrun.Spec) (string, scriptrun.Ending) {
				_, _ = updatesToNewer(spec)

				return "12:00:02 [WARN] failed to clear update cache: denied\n", exited(1)
			},
			status:   scriptrun.StatusFailed,
			message:  "Updated magex to 1.30.2. It exited with status 1.",
			updated:  true,
			warnings: []string{"failed to clear update cache: denied"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newUpdateHarness(t, tt.play)
			h.m.put(h.m.pathB, magexSays(recordedMagex))
			h.ready(t)

			_, err := h.svc.Update(t.Context(), integrationprefs.MageX)
			require.NoError(t, err, "the run started; how it ended is its own")

			_, outcomes := h.runs.started()
			require.Len(t, outcomes, 1)
			assert.Equal(t, tt.status, outcomes[0].Status)
			assert.Equal(t, tt.message, outcomes[0].Summary)
			assert.Equal(t, append([]string{}, tt.warnings...), outcomes[0].Warnings)

			magex := h.magex(t)
			assert.Equal(t, tt.updated, magex.SelfUpdate.Updated)
			assert.Equal(t, tt.status != scriptrun.StatusSucceeded, magex.SelfUpdate.Failed)
			assert.Equal(t, strings.TrimSuffix(tt.message, "."), strings.TrimSuffix(magex.SelfUpdate.Message, "."))
		})
	}
}

func TestCopiesThatCantUpdateThemselvesAreInstalledOver(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(t *testing.T, h *updateHarness)
	}{
		{
			name:  "go install's folder",
			setup: func(_ *testing.T, h *updateHarness) { h.m.put(h.m.goBin, magexSays(recordedMagex)) },
		},
		{
			// A $GOBIN set too, which go install would put a tool in first, doesn't make
			// $GOPATH's folders any less go install's: the updater checks them all.
			name: "a GOPATH the shell sets",
			setup: func(_ *testing.T, h *updateHarness) {
				gopath := filepath.Join(h.m.root, "gopath")
				h.env.env = append(h.env.env, "GOBIN="+h.m.goBin,
					"GOPATH="+filepath.Join(h.m.root, "elsewhere")+string(filepath.ListSeparator)+gopath)
				h.onPath(filepath.Join(gopath, "bin"))
				h.m.put(filepath.Join(gopath, "bin"), magexSays(recordedMagex))
			},
		},
		{
			// go env -w, or GOPATH's default, which only go itself says.
			name: "a GOPATH go env says",
			setup: func(_ *testing.T, h *updateHarness) {
				gopath := filepath.Join(h.m.root, "gopath")
				h.putGo(answer{stdout: "\n" + gopath + "\n"})
				h.onPath(h.m.pathA, filepath.Join(gopath, "bin"))
				h.m.put(filepath.Join(gopath, "bin"), magexSays(recordedMagex))
			},
		},
		{
			name: "a go whose settings can't be read",
			setup: func(_ *testing.T, h *updateHarness) {
				h.putGo(answer{stderr: "go: cannot find GOROOT\n", err: errExit})
				h.m.put(h.m.pathB, magexSays(recordedMagex))
			},
		},
		{
			name:  "too old to have an updater",
			setup: func(_ *testing.T, h *updateHarness) { h.m.put(h.m.pathB, magexSays("1.25.0")) },
		},
		{
			name: "a copy its owner can't write",
			setup: func(t *testing.T, h *updateHarness) {
				t.Helper()
				require.NoError(t, os.Chmod(h.m.put(h.m.pathB, magexSays(recordedMagex)), 0o500)) //nolint:gosec // a program its owner can read and run, not write
			},
		},
		{
			name: "no runner",
			setup: func(_ *testing.T, h *updateHarness) {
				h.m.put(h.m.pathB, magexSays(recordedMagex))
				h.svc.runs = nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newUpdateHarness(t, updatesToNewer)
			tt.setup(t, h)

			plan := h.ready(t)["mage-x"].Install
			assert.Empty(t, plan.Updater, "the app installs the release itself")

			got, err := h.svc.Update(t.Context(), integrationprefs.MageX)
			require.NoError(t, err)
			assert.Zero(t, got.Run)
			assert.NotEmpty(t, got.Message)

			specs, _ := h.runs.started()
			assert.Empty(t, specs, "no updater ran")
		})
	}
}

func TestATooOldCopyWithAnUpdaterUpdatesItself(t *testing.T) {
	t.Parallel()

	h := newUpdateHarness(t, updatesToNewer)
	h.m.put(h.m.pathB, magexSays("1.27.0"))
	h.releases.err = ErrSignedOut

	require.NoError(t, h.svc.detect(t.Context()))
	require.ErrorIs(t, h.svc.checkUpdates(t.Context(), 0), ErrSignedOut)

	magex := h.magex(t)
	require.Equal(t, StateTooOld, magex.State)
	assert.Empty(t, magex.Install.Blocked, "it finds its own release, signed in or not")
	assert.Equal(t, "~/.local/bin/magex update:install", magex.Install.Updater)

	got, err := h.svc.Update(t.Context(), integrationprefs.MageX)
	require.NoError(t, err)
	assert.Positive(t, got.Run)
	assert.Equal(t, newerMagex, h.magex(t).Copy.Version.Raw)
}

func TestAnInstallWaitsForThisRunsFirstLookForTheTools(t *testing.T) {
	t.Parallel()

	h := newUpdateHarness(t, updatesToNewer)
	h.m.put(h.m.pathB, magexSays(recordedMagex))

	for _, call := range []func(context.Context, integrationprefs.ID) (InstallResult, error){
		h.svc.Install, h.svc.Update, h.svc.Reinstall,
	} {
		_, err := call(t.Context(), integrationprefs.MageX)
		require.ErrorIs(t, err, ErrInstallRefused)
		assert.Equal(t, brand.Name+" is still looking for the tools. Try again in a moment.", uierr.Message(err))
	}

	specs, _ := h.runs.started()
	assert.Empty(t, specs, "what the last run kept isn't acted on")
	assert.NoFileExists(t, filepath.Join(h.m.root, "downloads"))
}

func TestAnUpdateWaitsWhileARunUsesTheTool(t *testing.T) {
	t.Parallel()

	h := newUpdateHarness(t, updatesToNewer)
	h.m.put(h.m.pathB, magexSays(recordedMagex))
	h.busy = true

	plan := h.ready(t)["mage-x"].Install
	assert.Equal(t, "A run is using magex. Update it when the run ends.", plan.Blocked)

	_, err := h.svc.Update(t.Context(), integrationprefs.MageX)
	require.ErrorIs(t, err, ErrInstallRefused)
	assert.Equal(t, plan.Blocked, uierr.Message(err))

	specs, _ := h.runs.started()
	assert.Empty(t, specs)
}

func TestTheUpdatersBeforeChecksTheCopyAgain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(h *updateHarness, path string)
		want   string
	}{
		{
			name:   "a run started using it",
			change: func(h *updateHarness, _ string) { h.busy = true },
			want:   "A run is using magex. Update it when the run ends.",
		},
		{
			name: "it became a link",
			change: func(h *updateHarness, path string) {
				target := h.m.put(filepath.Join(h.m.home, "projects", "mage-x", "bin"), magexSays(recordedMagex))
				_ = os.Remove(path)
				_ = os.Symlink(target, path)
			},
			want: "~/.local/bin/magex changed since " + brand.Name + " looked for it. Check again, then update it.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newUpdateHarness(t, updatesToNewer)
			path := h.m.put(h.m.pathB, magexSays(recordedMagex))
			h.ready(t)
			h.runs.beforeHook = func() { tt.change(h, path) }

			_, err := h.svc.Update(t.Context(), integrationprefs.MageX)
			require.ErrorIs(t, err, ErrInstallRefused)
			assert.Equal(t, tt.want, uierr.Message(err))
			assert.Equal(t, SelfUpdate{}, h.magex(t).SelfUpdate, "an update that never started is forgotten")
		})
	}
}

func TestAnUpdateThatCantStartSaysWhy(t *testing.T) {
	t.Parallel()

	busy := &clonelock.BusyError{Holder: clonelock.Holder{Kind: clonelock.KindUpdate, Repo: "~/.local/bin"}}

	tests := []struct {
		name   string
		runs   func(r *fakeRuns)
		want   string
		record string
	}{
		{
			name: "its folder is busy",
			runs: func(r *fakeRuns) { r.refuse = uierr.New(busy.Error(), busy) },
			want: "A tool update is using ~/.local/bin right now.",
		},
		{
			name: "the runner refused",
			runs: func(r *fakeRuns) { r.refuse = errNoSlot },
			want: "magex couldn't start updating itself.",
		},
		{
			name:   "it couldn't launch",
			runs:   func(r *fakeRuns) { r.noLaunch = true },
			want:   "The run couldn't start.",
			record: "It couldn't start: no such file.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newUpdateHarness(t, updatesToNewer)
			tt.runs(h.runs)
			h.m.put(h.m.pathB, magexSays(recordedMagex))
			h.ready(t)

			_, err := h.svc.Update(t.Context(), integrationprefs.MageX)
			require.Error(t, err)
			assert.Equal(t, tt.want, uierr.Message(err))

			self := h.magex(t).SelfUpdate
			assert.False(t, self.Running)
			assert.Equal(t, tt.record, self.Message, "only a run the runner recorded keeps what it says")
		})
	}
}

func TestAnUpdateWhoseToolsCantBeReadAgainSaysSo(t *testing.T) {
	t.Parallel()

	h := newUpdateHarness(t, nil)
	h.runs.play = func(spec scriptrun.Spec) (string, scriptrun.Ending) {
		// The database goes away as the run ends: the tools can't be kept or listed again.
		h.svc.prefs = prefs.New(testutil.ClosedDB(t), h.clock)

		return updatesToNewer(spec)
	}
	h.m.put(h.m.pathB, magexSays(recordedMagex))
	h.ready(t)

	_, err := h.svc.Update(t.Context(), integrationprefs.MageX)
	require.Error(t, err, "the run started, but the tools can't be listed")

	_, outcomes := h.runs.started()
	require.Len(t, outcomes, 1)
	assert.Equal(t, scriptrun.StatusSucceeded, outcomes[0].Status, "the copy's own version says it updated")
	assert.Contains(t, h.logs.String(), "a tool updated itself, but couldn't look for the tools again")

	_, err = h.svc.Update(t.Context(), integrationprefs.MageX)
	require.Error(t, err, "nor can a plan be made")
}

func TestAnUpdateOfTheAppsCopyThatCantBeReadIsntRecorded(t *testing.T) {
	t.Parallel()

	h := newUpdateHarness(t, nil)
	h.withRecords(t)
	close(h.env.ready)

	// An empty copy answers as the machine says, but has nothing to hash.
	path := h.m.put(h.m.pathB, magexSays(newerMagex))

	got := NewVerifier(h.svc).Verify(scriptrun.Record{ID: 3, Result: scriptrun.Result{
		Intent: map[string]any{"tool": "mage-x", "path": path, "from": recordedMagex, "installed": true},
	}})

	out := got.Finish(t.Context(), exited(0), engineOutcome(exited(0)))
	assert.Equal(t, scriptrun.StatusSucceeded, out.Status)
	assert.Contains(t, h.logs.String(), "couldn't read a tool that updated itself")

	_, err := h.svc.queries.LatestInstallAt(t.Context(), paths.Resolved(path))
	require.ErrorIs(t, err, sql.ErrNoRows, "nothing is recorded for it")
}

func TestAnUpdateWhoseEndWasntRecordedSaysSo(t *testing.T) {
	t.Parallel()

	h := newUpdateHarness(t, nil)
	r := &selfRun{def: MageX(), running: true}

	h.svc.releasedUpdate(r)

	assert.False(t, r.running)
	assert.Equal(t, "magex's update ended, but "+brand.Name+" couldn't record how.", r.result.Message)
}

func TestAnUpdateCutOffAsTheAppQuitsIsLeftForTheNextStart(t *testing.T) {
	t.Parallel()

	h := newUpdateHarness(t, nil)
	path := h.m.put(h.m.pathB, magexSays(recordedMagex))
	r := &selfRun{def: MageX(), intent: updateIntent{Tool: integrationprefs.MageX, Path: path, From: recordedMagex}, reader: &updateReader{}}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	got := h.svc.finishUpdate(ctx, r, exited(0), engineOutcome(exited(0)))
	assert.True(t, got.Unsettled)
}

func TestVerifierChecksAnUpdateFromBeforeARestart(t *testing.T) {
	t.Parallel()

	h := newUpdateHarness(t, nil)
	path := h.m.put(h.m.pathB, magexSays(recordedMagex))
	h.ready(t)
	close(h.env.ready)

	v := NewVerifier(h.svc)
	got := v.Verify(scriptrun.Record{ID: 9, Kind: scriptrun.KindUpdate, Result: scriptrun.Result{
		Intent: map[string]any{"tool": "mage-x", "path": path, "from": recordedMagex, "installed": false},
	}})
	require.NotNil(t, got.Output)
	require.NotNil(t, got.Finish)

	require.NoError(t, swap(path, magexSays(newerMagex).stdout))

	_, err := got.Output.Write([]byte("Updated magex to 1.30.2\n"))
	require.NoError(t, err)

	out := got.Finish(t.Context(), exited(0), engineOutcome(exited(0)))
	assert.Equal(t, scriptrun.StatusSucceeded, out.Status)
	assert.Equal(t, "Updated magex to 1.30.2.", out.Summary)
	assert.Equal(t, newerMagex, h.magex(t).Copy.Version.Raw, "the tools were looked for again")

	for _, intent := range []any{nil, map[string]any{"tool": "other", "path": path}, map[string]any{"tool": "mage-x", "path": "rel"}, "{"} {
		assert.Equal(t, scriptrun.Verification{}, v.Verify(scriptrun.Record{ID: 10, Result: scriptrun.Result{Intent: intent}}),
			"%v: the runner's own outcome stands", intent)
	}
}

func TestVerifierLeavesAnUpdateUnsettledUntilTheShellIsRead(t *testing.T) {
	t.Parallel()

	h := newUpdateHarness(t, nil)
	path := h.m.put(h.m.pathB, magexSays(recordedMagex))

	got := NewVerifier(h.svc).Verify(scriptrun.Record{ID: 9, Result: scriptrun.Result{
		Intent: map[string]any{"tool": "mage-x", "path": path, "from": recordedMagex},
	}})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	assert.True(t, got.Finish(ctx, exited(0), engineOutcome(exited(0))).Unsettled)
}

func TestGoEnvAddsTheFoldersGoItselfReads(t *testing.T) {
	t.Parallel()

	h := newUpdateHarness(t, nil)
	gobin, gopath := filepath.Join(h.m.root, "gobin"), filepath.Join(h.m.root, "gopath")
	goPath := h.putGo(answer{stdout: gobin + "\n" + gopath + "\n"})
	h.env.env = []string{"HOME=/Users/example", "GOTOOLCHAIN=auto"}

	require.NoError(t, h.svc.detect(t.Context()))

	h.m.mu.Lock()
	args, env := h.m.args[goPath], h.m.envs[goPath]
	h.m.mu.Unlock()

	assert.Equal(t, []string{"env", "GOBIN", "GOPATH"}, args)
	assert.Equal(t, []string{"HOME=/Users/example", "GOTOOLCHAIN=local"}, env, "the shell's, never fetching a toolchain")

	h.svc.mu.Lock()
	bins := h.svc.goBins
	h.svc.mu.Unlock()

	assert.Equal(t, []string{h.m.goBin, gobin, filepath.Join(gopath, "bin")}, bins.dirs,
		"the environment's, then go env's: ~/go/bin always counts")
	assert.False(t, bins.unsure)
}

func TestGoEnvThatCantBeReadUpdatesNothingItself(t *testing.T) {
	t.Parallel()

	for _, a := range []answer{{err: errExit}, {stdout: "only one line\n"}, {stdout: "\n\n"}} {
		h := newUpdateHarness(t, nil)
		h.putGo(a)
		h.m.put(h.m.pathB, magexSays(recordedMagex))

		plan := h.ready(t)["mage-x"].Install
		assert.Empty(t, plan.Updater, "%+v: go install's folders aren't known, so the installer updates it", a)
		assert.Contains(t, h.logs.String(), "couldn't read go's settings")
	}
}

func TestCopyAtFindsTheUpdatedCopyAmongThoseALookFound(t *testing.T) {
	t.Parallel()

	d := detection{has: true, selected: Copy{Path: "/opt/a/magex"}, others: []Copy{{Path: "/opt/b/magex", Runs: true}}}

	got, ok := copyAt(d, "/opt/b/magex")
	assert.True(t, ok)
	assert.True(t, got.Runs, "another copy, not only the one in use")

	_, ok = copyAt(d, "/opt/c/magex")
	assert.False(t, ok)

	_, ok = copyAt(detection{}, "/opt/a/magex")
	assert.False(t, ok, "a look that found nothing")
}

func TestRunEnvPutsTheQuietVariablesOverTheShells(t *testing.T) {
	t.Parallel()

	base := []string{"PATH=/bin", "NO_COLOR=", "MAGEX_NO_UPDATE_CHECK=0", "HOME=/Users/example"}

	assert.Equal(t, []string{
		"PATH=/bin", "HOME=/Users/example", "GIT_TERMINAL_PROMPT=0", "MAGEX_NO_UPDATE_CHECK=1", "NO_COLOR=1",
	}, MageX().RunEnv(base, integrationprefs.RunsInApp))
	assert.Equal(t, []string{"MAGEX_NO_UPDATE_CHECK=1"}, MageX().RunEnv(base, integrationprefs.RunsInTerminal),
		"a terminal gets the quiet settings alone, its colors kept")
	assert.Equal(t, []string{"GO_PRE_COMMIT_NO_UPDATE_CHECK=1"}, GoPreCommit().RunEnv(base, integrationprefs.RunsInTerminal))
	assert.Equal(t, []string{}, Definition{}.RunEnv(base, integrationprefs.RunsInTerminal))
}

func TestUpdateReaderZeroValueIsReady(t *testing.T) {
	t.Parallel()

	// Each method works as the first call on a zero value.
	for name, call := range map[string]func(r *updateReader){
		"Write":    func(r *updateReader) { _, _ = r.Write([]byte("partial")) },
		"Flush":    func(r *updateReader) { r.Flush() },
		"snapshot": func(r *updateReader) { _ = r.snapshot() },
	} {
		assert.NotPanics(t, func() { call(new(updateReader)) }, name)
	}
}

func TestUpdateReaderReadsTheUpdatersLines(t *testing.T) {
	t.Parallel()

	var r updateReader

	for _, part := range []string{
		"\n=== Installing ", "Update ===\n12:00:00 [WARN] first\r\n",
		"12:00:01 [ERROR] Cannot update magex in place\n12:00:01 [INFO] note\n",
		"warning: install dir not writable\n", "❌ Error: update failed: denied\n", "✅ Updated magex to 1.30.2\n",
		"Error: the last word",
	} {
		n, err := r.Write([]byte(part))
		require.NoError(t, err)
		assert.Equal(t, len(part), n)
	}

	assert.Equal(t, "Updated magex to 1.30.2", r.snapshot().last, "a line without its newline waits for Flush")

	r.Flush()

	got := r.snapshot()
	assert.Equal(t, []string{"first", "install dir not writable"}, got.warnings)
	assert.Equal(t, "install dir not writable", got.warning)
	assert.Equal(t, "the last word", got.err)
	assert.Equal(t, "the last word", got.last)

	for range maxWarnings + 5 {
		_, _ = r.Write([]byte("12:00:02 [WARN] again\n"))
	}

	assert.Len(t, r.snapshot().warnings, maxWarnings, "the warnings kept are capped")
	assert.Equal(t, "again", r.snapshot().warning)
}

func TestLineLevelReadsEachMark(t *testing.T) {
	t.Parallel()

	for text, want := range map[string]struct {
		level level
		msg   string
	}{
		"12:00:00 [DEBUG] d":          {levelInfo, "d"},
		"12:00:00 [FATAL] f":          {levelError, "f"},
		"❌ boom":                      {levelError, "boom"},
		"❌ Error: update failed: x":   {levelError, "update failed: x"},
		"✅ Updated magex to 1.30.2":   {levelInfo, "Updated magex to 1.30.2"},
		"⚠️  careful":                 {levelWarning, "careful"},
		"12:00:00 [INFO] ⚠️ heads up": {levelWarning, "heads up"},
		"12:00:00 [ERROR] ✓ odd":      {levelError, "odd"},
		"ℹ️ ✔ fine":                   {levelInfo, "fine"},
		"✗ no":                        {levelError, "no"},
		"WARNING: shout":              {levelWarning, "shout"},
		"  plain  ":                   {levelInfo, "plain"},
		"":                            {levelInfo, ""},
		"Updating from 1 to 2":        {levelInfo, "Updating from 1 to 2"},
		"12:00:00 [WARN]":             {levelWarning, ""},
		"go-pre-commit is ready.":     {levelInfo, "go-pre-commit is ready."},
		"Err":                         {levelInfo, "Err"},
	} {
		lvl, msg := lineLevel(text)
		assert.Equal(t, want.level, lvl, text)
		assert.Equal(t, want.msg, msg, text)
	}
}

func TestEachToolNamesItsOwnUpdater(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"update:install"}, MageX().UpdateArgs)
	assert.Equal(t, "1.26.0", MageX().Features[FeatureSelfUpdate])
	assert.Equal(t, []string{"update"}, GoPreCommit().UpdateArgs)
	assert.Equal(t, "1.10.0", GoPreCommit().Features[FeatureSelfUpdate])

	assert.True(t, GoPreCommit().Has(FeatureSelfUpdate, Version{Raw: "1.13.1"}))
	assert.False(t, GoPreCommit().Has(FeatureSelfUpdate, Version{Raw: "1.9.4"}), "upgrade was another command")
	assert.False(t, MageX().Has(FeatureSelfUpdate, Version{Raw: "1.25.0"}), "it put the new copy in $GOPATH/bin")
}

func TestAnUpdaterlessToolNeverUpdatesItself(t *testing.T) {
	t.Parallel()

	h := newUpdateHarness(t, nil)
	def := MageX()
	def.UpdateArgs = nil

	path := h.m.put(h.m.pathB, magexSays(recordedMagex))
	c := Copy{Path: path, Runs: true, Version: Version{Raw: recordedMagex}, Source: SourceRelease}

	assert.True(t, h.svc.updatesItself(MageX(), c, goFolders{}))
	assert.False(t, h.svc.updatesItself(def, c, goFolders{}))
	assert.False(t, h.svc.updatesItself(MageX(), Copy{Path: path, Version: c.Version, Source: SourceRelease}, goFolders{}),
		"it won't run")
	assert.False(t, h.svc.updatesItself(MageX(), Copy{Path: path, Runs: true, Version: c.Version, Source: SourceHomebrew},
		goFolders{}))
	assert.False(t, h.svc.updatesItself(MageX(), c, goFolders{unsure: true}), "go's own folders aren't known")
	assert.False(t, h.svc.updatesItself(MageX(), c, goFolders{rel: []string{"../bin"}}), "../bin, from a bin")
	assert.False(t, replaceable(filepath.Join(h.m.pathB, "missing"), goFolders{}))

	testutil.WriteFile(t, filepath.Join(h.m.pathB, "notes"), "x")
	assert.False(t, replaceable(filepath.Join(h.m.pathB, "notes"), goFolders{}), "not a program")
}

func TestAnUpdateFromBeforeARestartHoldsTheToolBack(t *testing.T) {
	t.Parallel()

	h := newUpdateHarness(t, updatesToNewer)
	h.m.put(h.m.goBin, magexSays(recordedMagex)) // go install's: the installer updates it
	h.ready(t)
	h.runs.orphan = 12

	magex := h.magex(t)
	assert.Equal(t, InstallProgress{Step: StepUpdating}, magex.Progress, "the card shows it updating")
	assert.Equal(t, SelfUpdate{Run: 12, Running: true}, magex.SelfUpdate, "with the run to show")

	for _, call := range []func(context.Context, integrationprefs.ID) (InstallResult, error){
		h.svc.Update, h.svc.Reinstall,
	} {
		_, err := call(t.Context(), integrationprefs.MageX)
		require.ErrorIs(t, err, ErrInstallRefused)
		assert.Equal(t, "magex is updating itself. Try again once it ends.", uierr.Message(err))
	}

	// One that the runner picks up just as the installer would start holds it back too.
	h.runs.mu.Lock()
	h.runs.orphan, h.runs.orphans = 0, []int64{0, 12}
	h.runs.mu.Unlock()

	_, err := h.svc.Update(t.Context(), integrationprefs.MageX)
	require.ErrorIs(t, err, ErrInstallRefused)
	assert.Equal(t, "magex is updating itself. Try again once it ends.", uierr.Message(err))
	assert.NoFileExists(t, filepath.Join(h.m.pathB, "magex"), "nothing was placed")

	_, err = h.svc.Update(t.Context(), integrationprefs.MageX)
	require.NoError(t, err, "once it ends, the installer updates it")
}

func TestAnInstallHoldsTheFolderItPlacesTheProgramIn(t *testing.T) {
	t.Parallel()

	h := newUpdateHarness(t, nil)
	h.m.put(h.m.goBin, magexSays(recordedMagex))
	h.ready(t)

	// A tool's update from before the app restarted holds ~/.local/bin under a placeholder.
	release, _, ok := h.svc.locks.TryLock(h.m.pathB, clonelock.Holder{Kind: clonelock.KindOrphan, Repo: "~/.local/bin"})
	require.True(t, ok)

	_, err := h.svc.Update(t.Context(), integrationprefs.MageX)
	require.ErrorIs(t, err, ErrInstallRefused)
	require.ErrorIs(t, err, clonelock.ErrBusy)
	assert.Equal(t, "A run from before "+brand.Name+" restarted is using ~/.local/bin right now.", uierr.Message(err))
	assert.NoFileExists(t, filepath.Join(h.m.pathB, "magex"), "nothing was placed")

	release()

	got, err := h.svc.Update(t.Context(), integrationprefs.MageX)
	require.NoError(t, err)
	assert.Equal(t, "Installed mage-x "+newerMagex+" in ~/.local/bin", got.Message)

	again, _, ok := h.svc.locks.TryLock(h.m.pathB, clonelock.Holder{Kind: clonelock.KindUpdate})
	assert.True(t, ok, "the install let go of the folder")
	again()
}

func TestAToolThatFindsItsOwnReleaseNeedsNoBuildNamedHere(t *testing.T) {
	t.Parallel()

	h := newUpdateHarness(t, updatesToNewer)
	h.svc.goarch = "riscv64" // the release lists no archive by the app's name for this
	h.m.put(h.m.pathB, magexSays(recordedMagex))

	plan := h.ready(t)["mage-x"].Install
	assert.NotEmpty(t, plan.Updater)
	assert.Empty(t, plan.Blocked, "the updater picks its own archive")

	h.svc.runs = nil
	require.NoError(t, h.svc.detect(t.Context()))

	_, tools := h.list(t)
	assert.Equal(t, "mage-x 1.30.2 has no build for this Mac (darwin/riscv64).", tools["mage-x"].Install.Blocked,
		"while the installer would need it")
}

func TestWhetherACopyUpdatesItselfIsWorkedOutOncePerLook(t *testing.T) {
	t.Parallel()

	h := newUpdateHarness(t, updatesToNewer)
	path := h.m.put(h.m.pathB, magexSays(recordedMagex))
	require.NotEmpty(t, h.ready(t)["mage-x"].Install.Updater)

	require.NoError(t, os.Chmod(path, 0o500)) //nolint:gosec // a program its owner can read and run, not write

	assert.NotEmpty(t, h.magex(t).Install.Updater, "listing doesn't look at the files again")

	_, err := h.svc.Update(t.Context(), integrationprefs.MageX)
	require.ErrorIs(t, err, ErrInstallRefused, "the run checks the copy again as it starts")
	assert.Equal(t, "~/.local/bin/magex changed since "+brand.Name+" looked for it. Check again, then update it.",
		uierr.Message(err))

	require.NoError(t, h.svc.detect(t.Context()))
	assert.Empty(t, h.magex(t).Install.Updater, "the next look sees it")
}
