package update_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/update"
	"github.com/mrz1836/nilbox/internal/updateprefs"
)

// relaunch is the app opened again on first's data: the same cache, memory, logs,
// temporary folder, bundle, and releases.
func relaunch(t *testing.T, first *rig) *rig {
	t.Helper()

	again := newRig(t, func(c *update.Config) {
		c.Cache, c.Key, c.Feed, c.Memory = first.cfg.Cache, first.cfg.Key, first.feed, first.memory
		c.Logs, c.TempDir, c.Bundle, c.Applications = first.cfg.Logs, first.cfg.TempDir, first.cfg.Bundle, first.cfg.Applications
	})
	again.memory, again.feed = first.memory, first.feed

	return again
}

// failedInstall readies next in a run, has a restart into it fail as log tells, and
// returns the run that opened again after it, settled.
func failedInstall(t *testing.T, log ...string) *rig {
	t.Helper()

	first, _ := ready(t)
	again := relaunch(t, first)

	record(t, again, running, next, update.HelperWindow+time.Minute)
	helperLogIn(t, again, append([]string{"2026-11-15T12:00:00Z: helper start"}, log...)...)

	require.Zero(t, again.engine.Settle(t.Context()))

	return again
}

func TestAnUpdateThatFailsForGoodIsNeverOfferedAgain(t *testing.T) {
	t.Parallel()

	for _, ending := range []string{
		"installed application failed validation: sealed — restoring backup",
		"staged update is not installable: empty — aborting swap",
		"launch new failed: exit 1 — restoring backup",
	} {
		t.Run(ending, func(t *testing.T) {
			t.Parallel()

			r := failedInstall(t, "2026-11-15T12:00:01Z: "+ending)

			s := r.status(t)
			assert.Equal(t, next, s.InstallFailed.Version)
			assert.False(t, s.InstallFailed.Retryable, "the update fails the same way every time")
			assert.Equal(t, next, s.Latest, "still the version found, so the window can say it didn't install")
			assert.Equal(t, update.StateAvailable, s.State, "but never ready")
			assert.Empty(t, r.cached(t), "its download is dropped")
			assert.Contains(t, r.memory.get(update.KeyUninstallable), sha512Of(zipOf(next)), "and remembered")

			// A check finds it, and neither downloads nor tells of it.
			require.NoError(t, r.engine.Tick(t.Context()))
			assert.Equal(t, update.StateAvailable, r.status(t).State)
			assert.Equal(t, int32(1), r.feed.downloads.Load(), "only the first run's download")
			assert.Empty(t, r.notifier.all())

			_, err := r.service.CheckNow(t.Context())
			require.NoError(t, err)
			assert.Equal(t, update.StateAvailable, r.status(t).State, "a check the user asks for only finds it too")

			_, err = r.service.Download(t.Context())
			require.ErrorIs(t, err, update.ErrUninstallable)
		})
	}
}

func TestAFailureOfTheMomentKeepsTheUpdateOffered(t *testing.T) {
	t.Parallel()

	for _, ending := range []string{
		"parent did not exit within timeout: timeout — aborting swap",
		"all swap attempts exhausted — relaunching intact original",
		"backup failed: no space",
		"something no one has seen",
	} {
		t.Run(ending, func(t *testing.T) {
			t.Parallel()

			r := failedInstall(t, "2026-11-15T12:00:01Z: "+ending)

			s := r.status(t)
			assert.Equal(t, next, s.InstallFailed.Version)
			assert.True(t, s.InstallFailed.Retryable)
			assert.Equal(t, update.StateReady, s.State, "the download is kept, to try again")
			assert.Empty(t, r.memory.get(update.KeyUninstallable))

			exited, err := r.restart(t, false)
			require.NoError(t, err)
			assert.True(t, exited, "trying again restarts")
		})
	}
}

func TestALaterRunStillSaysAnUpdateFailedForGood(t *testing.T) {
	t.Parallel()

	failed := failedInstall(t, "2026-11-15T12:00:01Z: installed application failed validation: sealed — restoring backup")
	later := relaunch(t, failed)
	require.Zero(t, later.engine.Settle(t.Context()))

	assert.Empty(t, later.status(t).InstallFailed.Version, "before a check, nothing is found")

	_, err := later.service.CheckNow(t.Context())
	require.NoError(t, err)

	s := later.status(t)
	assert.Equal(t, update.StateAvailable, s.State)
	assert.Equal(t, update.InstallFailure{
		Version: next, Reason: "The new version failed its signature check, so the old one was put back.", Log: true,
	}, s.InstallFailed, "the release found is the one that failed, so the window can say why")
	require.NoError(t, later.service.ShowInstallLog(t.Context()), "and show its log")
}

func TestANewerReleaseOrARebuildIsOfferedAfterAFailureForGood(t *testing.T) {
	t.Parallel()

	t.Run("a newer release", func(t *testing.T) {
		t.Parallel()

		r := failedInstall(t, "2026-11-15T12:00:01Z: launch new failed: exit 1 — restoring backup")
		r.release(t, later)

		_, err := r.service.CheckNow(t.Context())
		require.NoError(t, err)
		require.NoError(t, r.engine.Tick(t.Context()), "the Worker's look downloads it")

		s := r.status(t)
		assert.Equal(t, update.StateReady, s.State)
		assert.Equal(t, later, s.Latest)
		assert.NotEqual(t, s.Latest, s.InstallFailed.Version, "the failure was another version's, which the menu leaves out")
	})

	t.Run("the same version rebuilt", func(t *testing.T) {
		t.Parallel()

		r := failedInstall(t, "2026-11-15T12:00:01Z: launch new failed: exit 1 — restoring backup")
		body := []byte("the zipped bundle of " + next + ", rebuilt")
		m := sign(t, next, body, r.private)
		r.feed.publish(t, m, body)

		_, err := r.service.CheckNow(t.Context())
		require.NoError(t, err)
		require.NoError(t, r.engine.Tick(t.Context()), "the Worker's look downloads it")

		s := r.status(t)
		assert.Equal(t, update.StateReady, s.State, "another zip of the same version installs")
		assert.Equal(t, next, s.InstallFailed.Version, "this run still says the last restart failed")
		assert.True(t, s.InstallFailed.Retryable, "but the rebuilt zip may be tried")
	})
}

func TestAFailureForGoodRecordedWithoutItsZipStaysOffered(t *testing.T) {
	t.Parallel()

	first, _ := ready(t)
	again := relaunch(t, first)

	r := again
	r.memory.put(update.KeyPendingInstall, `{"from":"`+running+`","to":"`+next+`","at":"2026-01-01T00:00:00Z","pid":31337}`)
	helperLogIn(t, r, "2026-11-15T12:00:00Z: helper start",
		"2026-11-15T12:00:01Z: launch new failed: exit 1 — restoring backup")

	require.Zero(t, r.engine.Settle(t.Context()))

	s := r.status(t)
	assert.True(t, s.InstallFailed.Retryable, "it can't be told apart from a rebuild, so it stays offered")
	assert.Equal(t, update.StateReady, s.State)
	assert.Empty(t, r.memory.get(update.KeyUninstallable))
}

func TestTheUpdatesThatFailedForGoodAreFewAndTheNewestKept(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.mode(updateprefs.ModeNotify)

	for i := range 7 {
		version := "0.3." + string(rune('0'+i))
		body := zipOf(version)

		r.memory.put(update.KeyPendingInstall, `{"from":"`+running+`","to":"`+version+`","at":"2026-01-01T00:00:00Z",`+
			`"pid":31337,"sha512":"`+sha512Of(body)+`"}`)
		helperLogIn(t, r, "2026-11-15T12:00:00Z: launch new failed: exit 1 — restoring backup")

		again := relaunch(t, r)
		require.Zero(t, again.engine.Settle(t.Context()))
	}

	stored := r.memory.get(update.KeyUninstallable)
	assert.NotContains(t, stored, `"0.3.0"`, "the oldest are dropped")
	assert.NotContains(t, stored, `"0.3.1"`)
	assert.Contains(t, stored, `"0.3.6"`)
}

func TestTheStartSettlesAtOnceOnceARestoreHasPutTheOldAppBack(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	record(t, r, running, next, 10*time.Second)
	helperLogIn(t, r, "2026-11-15T12:00:00Z: helper start",
		"2026-11-15T12:00:09Z: installed application failed validation: sealed — restoring backup")

	_, err := os.Stat(r.cfg.Bundle + ".bak")
	require.ErrorIs(t, err, os.ErrNotExist, "the restore renamed the copy into place, and opened this app")

	assert.Zero(t, r.engine.Settle(t.Context()), "no need to wait out the window")
	assert.Equal(t, next, r.status(t).InstallFailed.Version)
	assert.NoFileExists(t, filepath.Join(r.cfg.TempDir, "wails-update-31337.log"))
}
