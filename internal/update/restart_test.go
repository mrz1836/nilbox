package update_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/uierr"
	"github.com/mrz1836/nilbox/internal/update"
	"github.com/mrz1836/nilbox/internal/updateprefs"
)

// errFakeRename stands in for an error of the runtime's updater that names a path.
var errFakeRename = errors.New("rename /srv/private-place/wails-update-1/X.app: operation not permitted")

// ready returns a rig with next downloaded and ready.
func ready(t *testing.T) (*rig, update.Manifest) {
	t.Helper()

	r := newRig(t)
	m := r.release(t, next)
	require.NoError(t, r.engine.Tick(t.Context()))
	require.Equal(t, update.StateReady, r.status(t).State)

	return r, m
}

func TestRestartInstallsTheUpdateAfterABackupAndLetsTheQuitThrough(t *testing.T) {
	t.Parallel()

	r, m := ready(t)

	var atRestart struct {
		quit    []event.Event
		pending string
		state   update.State
	}

	r.installer.onRestart = func() {
		atRestart.quit = r.quitEvents()
		atRestart.pending = r.memory.get(update.KeyPendingInstall)
		atRestart.state = r.status(t).State
	}

	exited, err := r.restart(t, false)
	require.NoError(t, err)
	assert.True(t, exited, "the app exits inside the installer's Restart")

	installs, restarts := r.installer.calls()
	assert.Equal(t, []install{{
		zip: filepath.Join(r.cfg.Cache, next, m.File), version: next, size: m.Size, sha512: m.SHA512,
	}}, installs)
	assert.Equal(t, 1, restarts)
	assert.Equal(t, int32(1), r.backup.calls.Load(), "the data is backed up first")

	assert.Equal(t, []event.Event{event.QuitConfirmed{Restarting: true}}, atRestart.quit,
		"the quit is let through before the installer quits the app, with no run going too")
	assert.Equal(t, update.StateInstalling, atRestart.state)

	var pending map[string]any
	require.NoError(t, json.Unmarshal([]byte(atRestart.pending), &pending))
	assert.Equal(t, running, pending["from"])
	assert.Equal(t, next, pending["to"])
	assert.InDelta(t, 4242, pending["pid"], 0, "the app's process names the helper's log")
	assert.NotEmpty(t, pending["at"])
}

func TestRestartRefusesWhileRunsGoUnlessConfirmed(t *testing.T) {
	t.Parallel()

	r, _ := ready(t)
	release := update.Run{ID: 7, Kind: "release", Title: "Release v1.2.0", Repo: "acme-org/example-repo"}

	r.runs.active.Store(true)
	r.runs.going = []update.Run{release}

	_, err := r.service.Restart(t.Context(), false)
	require.ErrorIs(t, err, update.ErrRunsGoing)
	assert.Equal(t, "Restarting stops Release v1.2.0 in acme-org/example-repo.", uierr.Message(err))

	goingErr, ok := err.(interface{ Detail() any })
	require.True(t, ok)

	detail, jsonErr := json.Marshal(goingErr.Detail())
	require.NoError(t, jsonErr)
	assert.JSONEq(t, `{"runsGoing":[{"id":7,"kind":"release","title":"Release v1.2.0",`+
		`"repo":"acme-org/example-repo","dir":"","stopRefused":""}]}`, string(detail))

	installs, restarts := r.installer.calls()
	assert.Empty(t, installs, "nothing is installed")
	assert.Zero(t, restarts)
	assert.Zero(t, r.backup.calls.Load(), "nothing is backed up")
	assert.Empty(t, r.quitEvents(), "the quit is left alone")
	assert.Equal(t, update.StateReady, r.status(t).State)

	exited, err := r.restart(t, true)
	require.NoError(t, err)
	assert.True(t, exited, "confirmed, it restarts, stopping the runs")
	assert.Equal(t, []event.Event{event.QuitConfirmed{Restarting: true}}, r.quitEvents())
}

func TestARunThatStartsDuringTheRestartRefusesIt(t *testing.T) {
	t.Parallel()

	r, _ := ready(t)
	staging := filepath.Join(filepath.Dir(r.cfg.Bundle), "wails-update-5")
	r.runs.going = []update.Run{{Title: "Checks", Dir: "~/projects/tool"}}

	// A run starts while the update is staged.
	r.installer.onInstall = func() {
		require.NoError(t, os.MkdirAll(filepath.Join(staging, brand.Name+".app"), 0o700))
		r.runs.active.Store(true)
	}

	_, err := r.service.Restart(t.Context(), false)
	require.ErrorIs(t, err, update.ErrRunsGoing)
	assert.Equal(t, "Restarting stops Checks in ~/projects/tool.", uierr.Message(err))

	_, restarts := r.installer.calls()
	assert.Zero(t, restarts, "it never restarts")
	assert.Empty(t, r.quitEvents(), "nor lets the quit through")
	assert.Empty(t, r.memory.get(update.KeyPendingInstall))
	assert.NoDirExists(t, staging, "the staged update is taken back")

	s := r.status(t)
	assert.Equal(t, update.StateReady, s.State)
	assert.Empty(t, s.LastError, "a refusal to ask first is no failure")
}

func TestRunsGoingWordsTheRuns(t *testing.T) {
	t.Parallel()

	r, _ := ready(t)
	r.runs.active.Store(true)

	for want, going := range map[string][]update.Run{
		"Restarting stops the runs going in " + brand.Name + ".": nil,
		"Restarting stops Checks in ~/projects/tool.":            {{Title: "Checks", Dir: "~/projects/tool"}},
		"Restarting stops the 2 runs going in " + brand.Name + ": Release v1.2.0, Pull main.": {
			{Title: "Release v1.2.0"}, {Title: "Pull main"},
		},
	} {
		r.runs.going = going

		_, err := r.service.Restart(t.Context(), false)
		require.ErrorIs(t, err, update.ErrRunsGoing)
		assert.Equal(t, want, uierr.Message(err))
	}
}

func TestABackupThatFailsStopsTheRestart(t *testing.T) {
	t.Parallel()

	r, _ := ready(t)
	r.backup.err = errFakeDisk

	_, err := r.service.Restart(t.Context(), false)
	require.ErrorIs(t, err, errFakeDisk)
	assert.Equal(t, "Couldn't back up your data first, so nothing was installed.", uierr.Message(err))

	installs, restarts := r.installer.calls()
	assert.Empty(t, installs)
	assert.Zero(t, restarts)
	assert.Empty(t, r.memory.get(update.KeyPendingInstall))
	assert.Empty(t, r.quitEvents())

	s := r.status(t)
	assert.Equal(t, update.StateReady, s.State, "back to ready")
	assert.Equal(t, uierr.Message(err), s.LastError)
}

func TestARestartTriedAgainSoonReusesItsBackup(t *testing.T) {
	t.Parallel()

	r, _ := ready(t)
	r.installer.installErr = errFakeDisk

	_, err := r.service.Restart(t.Context(), false)
	require.Error(t, err)
	require.Equal(t, int32(1), r.backup.calls.Load())

	r.clock.sleep(update.BackupReuse - time.Minute)

	_, err = r.service.Restart(t.Context(), false)
	require.Error(t, err)
	assert.Equal(t, int32(1), r.backup.calls.Load(), "a retry soon after takes no backup of its own")

	r.clock.sleep(time.Minute)

	_, err = r.service.Restart(t.Context(), false)
	require.Error(t, err)
	assert.Equal(t, int32(2), r.backup.calls.Load(), "one later does")

	// A backup that is gone is never counted on.
	entries, err := filepath.Glob(filepath.Join(r.backup.dir, brand.Slug+".db.backup-*"))
	require.NoError(t, err)

	for _, path := range entries {
		require.NoError(t, os.Remove(path))
	}

	_, err = r.service.Restart(t.Context(), false)
	require.Error(t, err)
	assert.Equal(t, int32(3), r.backup.calls.Load())
}

func TestADamagedOrMissingDownloadIsDroppedBeforeAnything(t *testing.T) {
	t.Parallel()

	for name, damage := range map[string]func(t *testing.T, zip string){
		"damaged": func(t *testing.T, zip string) {
			t.Helper()

			require.NoError(t, os.WriteFile(zip, []byte("the zipped bundle of 0.3.9"), 0o600))
		},
		"missing": func(t *testing.T, zip string) {
			t.Helper()

			require.NoError(t, os.Remove(zip))
		},
	} {
		r, m := ready(t)
		damage(t, filepath.Join(r.cfg.Cache, next, m.File))

		_, err := r.service.Restart(t.Context(), false)
		require.ErrorIs(t, err, update.ErrDamaged, name)
		assert.Equal(t, "The downloaded "+next+" was damaged, so it was thrown away. It's downloading again.",
			uierr.Message(err), name)

		installs, _ := r.installer.calls()
		assert.Empty(t, installs, name)
		assert.Zero(t, r.backup.calls.Load(), "no backup for a restart that can't go on: %s", name)
		assert.Empty(t, r.cached(t), name)

		s := r.status(t)
		assert.Equal(t, update.StateAvailable, s.State, name)
		assert.Equal(t, uierr.Message(err), s.LastError, name)

		// With downloads on, it is downloaded again at the next look, not left behind.
		require.NoError(t, r.engine.Tick(t.Context()), name)
		assert.Equal(t, update.StateReady, r.status(t).State, name)
	}

	r, m := ready(t)
	r.mode(updateprefs.ModeNotify)
	require.NoError(t, os.Remove(filepath.Join(r.cfg.Cache, next, m.File)))

	_, err := r.service.Restart(t.Context(), false)
	require.ErrorIs(t, err, update.ErrDamaged)
	assert.Contains(t, uierr.Message(err), "Click Update to download it again.")
}

func TestAnInstallOrRestartErrorGoesBackToReadyWordedWithNoPath(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		installErr, restartErr error
		words                  string
		quit                   []event.Event
	}{
		"install": {installErr: errFakeRename, words: "Couldn't get " + next + " ready to install.", quit: []event.Event{}},
		"restart": {
			restartErr: errFakeRename, words: "Couldn't restart into " + next + ".",
			quit: []event.Event{event.QuitConfirmed{Restarting: true}, event.UpdateRestartFailed{}},
		},
	} {
		r, _ := ready(t)

		staging := filepath.Join(filepath.Dir(r.cfg.Bundle), "wails-update-123")
		require.NoError(t, os.MkdirAll(filepath.Join(staging, brand.Name+".app"), 0o700))

		r.installer.installErr, r.installer.restartErr = tt.installErr, tt.restartErr

		_, err := r.service.Restart(t.Context(), false)
		require.ErrorIs(t, err, errFakeRename, name)
		assert.Equal(t, tt.words, uierr.Message(err), name)

		s := r.status(t)
		assert.Equal(t, update.StateReady, s.State, name)
		assert.Equal(t, tt.words, s.LastError, name)
		assert.NotContains(t, s.LastError, "/srv", "no path reaches the window: %s", name)
		assert.Empty(t, r.memory.get(update.KeyPendingInstall), "the record is cleared: %s", name)
		assert.NoDirExists(t, staging, "the staging folder is removed: %s", name)
		assert.Equal(t, tt.quit, r.quitEvents(), "the shell holds a quit again once one was let through: %s", name)
	}
}

func TestARestartThatReturnsWithTheAppRunningIsNoLongerInstalling(t *testing.T) {
	t.Parallel()

	r, _ := ready(t)
	r.installer.returns = true

	exited, err := r.restart(t, false)
	require.False(t, exited)
	require.Error(t, err)
	assert.Equal(t, "Couldn't restart into "+next+", because "+brand.Name+" didn't quit. Quit "+brand.Name+
		" and open it again to finish updating.", uierr.Message(err))

	s := r.status(t)
	assert.Equal(t, update.StateReady, s.State, "no longer installing")
	assert.Equal(t, uierr.Message(err), s.LastError)
	assert.Empty(t, r.memory.get(update.KeyPendingInstall), "the record is cleared")
	assert.Equal(t, []event.Event{event.QuitConfirmed{Restarting: true}, event.UpdateRestartFailed{}}, r.quitEvents(),
		"the shell holds a quit while runs go again")
}

func TestRestartNeedsTheAppInApplicationsAnUpdateReadyAndAnInstaller(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	_, err := r.service.Restart(t.Context(), false)
	require.ErrorIs(t, err, update.ErrNotReady)

	r.mode(updateprefs.ModeNotify)
	r.release(t, next)
	require.NoError(t, r.engine.Tick(t.Context()))

	_, err = r.service.Restart(t.Context(), true)
	require.ErrorIs(t, err, update.ErrNotReady, "an update found but not downloaded isn't ready")

	bare := newRig(t)
	bare.engine.Attach(nil)

	_, err = bare.service.Restart(t.Context(), false)
	require.ErrorIs(t, err, update.ErrNoInstaller)
	assert.Equal(t, "This build can't install updates.", uierr.Message(err))

	away := newRig(t, func(c *update.Config) { c.Applications = "" })

	_, err = away.service.Restart(t.Context(), true)
	require.ErrorIs(t, err, update.ErrLocation)
	assert.Equal(t, "Move "+brand.Name+" to Applications to update it.", uierr.Message(err))
}
