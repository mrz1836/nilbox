package update_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
	"github.com/mrz1836/nilbox/internal/update"
	"github.com/mrz1836/nilbox/internal/updateprefs"
)

// loggedRig is a rig whose engine logs into the logs it returns, its config changed by each
// change after.
func loggedRig(t *testing.T, change ...func(*update.Config)) (*rig, *testutil.Logs) {
	t.Helper()

	logs := &testutil.Logs{}
	withLogs := func(c *update.Config) { c.Logger = logs.Logger() }

	return newRig(t, append([]func(*update.Config){withLogs}, change...)...), logs
}

func TestAStatusThatCantReadTheMemoryFails(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.memory.failReads(errFakeDisk, update.KeyBannerHidden)

	_, err := r.service.Status(t.Context())
	require.ErrorIs(t, err, errFakeDisk)

	_, err = r.service.HideBanner(t.Context())
	require.ErrorIs(t, err, errFakeDisk, "Later can't hide a banner it can't read")
	require.ErrorIs(t, r.service.ShowInstallLog(t.Context()), errFakeDisk)
}

func TestABuildThatDoesntUpdateSaysTheDefaultWhenItCantReadTheChoice(t *testing.T) {
	t.Parallel()

	for name, change := range map[string]func(*update.Config){
		"no memory":                   func(c *update.Config) { c.Flavor, c.Memory = update.FlavorSource, nil },
		"a memory that can't be read": func(c *update.Config) { c.Flavor = update.FlavorSource },
	} {
		r := newRig(t, change)
		r.mode(updateprefs.ModeNotify)
		r.memory.failReads(errFakeDisk, updateprefs.KeyMode)

		s, err := r.service.Status(t.Context())
		require.NoError(t, err, name)
		assert.False(t, s.Enabled, name)
		assert.Equal(t, updateprefs.DefaultMode, s.Mode, name)
	}
}

func TestShowBannerInABuildThatDoesntUpdateChangesNothing(t *testing.T) {
	t.Parallel()

	r := newRig(t, func(c *update.Config) { c.Flavor = update.FlavorSource })
	r.memory.put(update.KeyBannerHidden, `{"version":"0.3.0","until":"2026-11-16T12:00:00Z"}`)

	s, err := r.service.ShowBanner(t.Context())
	require.NoError(t, err)
	assert.Equal(t, update.StateOff, s.State)
	assert.NotEmpty(t, r.memory.get(update.KeyBannerHidden), "a build that doesn't update touches none of its memory")
}

func TestATimedCheckThatCantReadItsSettingsReadsNothing(t *testing.T) {
	t.Parallel()

	r, logs := loggedRig(t)
	r.release(t, next)
	r.memory.failReads(errFakeDisk, updateprefs.KeyMode, update.KeyLastCheck)

	require.ErrorIs(t, r.engine.Tick(t.Context()), errFakeDisk)
	assert.Zero(t, r.feed.manifests.Load(), "a check that can't tell whether it is due doesn't run")
	assert.Contains(t, logs.String(), "could not read what to do with a new version; using the default")
}

func TestAnUpdateTheAppCantRememberTellingIsNeverTold(t *testing.T) {
	t.Parallel()

	r, logs := loggedRig(t)
	r.mode(updateprefs.ModeNotify)
	r.release(t, next)
	r.memory.failWrites(errFakeDisk, nil)

	require.NoError(t, r.engine.Tick(t.Context()))
	assert.Equal(t, update.StateAvailable, r.status(t).State, "the version is still found")
	assert.Empty(t, r.notifier.all(), "a notice it can't remember sending would be sent at every check")
	assert.Contains(t, logs.String(), "could not remember when the update check ran")
	assert.Contains(t, logs.String(), "could not remember the update told; telling nothing")
}

func TestAnUpdateWhoseToldVersionCantBeReadIsNotTold(t *testing.T) {
	t.Parallel()

	r, logs := loggedRig(t)
	r.mode(updateprefs.ModeNotify)
	r.release(t, next)
	r.memory.failReads(errFakeDisk, update.KeyNotified)

	require.NoError(t, r.engine.Tick(t.Context()))
	assert.Equal(t, update.StateAvailable, r.status(t).State)
	assert.Empty(t, r.notifier.all())
	assert.Empty(t, r.memory.get(update.KeyNotified))
	assert.Contains(t, logs.String(), "could not read which update was told; telling nothing")
}

func TestAListOfFailedUpdatesThatCantBeReadBlocksNothing(t *testing.T) {
	t.Parallel()

	r, logs := loggedRig(t)
	r.release(t, next)
	r.memory.failReads(errFakeDisk, update.KeyUninstallable)

	require.NoError(t, r.engine.Tick(t.Context()))
	assert.Equal(t, []string{next}, r.cached(t), "the update downloads")
	assert.Equal(t, []update.Notice{{Version: next, Ready: true}}, r.notifier.all())
	assert.Contains(t, logs.String(), "could not read the updates that failed to install")
}

func TestAFailureForGoodTheAppCantRememberStaysOffered(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		brk  func(m *fakeMemory)
		logs []string
	}{
		"the list can't be read": {
			brk:  func(m *fakeMemory) { m.failReads(errFakeDisk, update.KeyUninstallable) },
			logs: []string{"could not read the updates that failed to install"},
		},
		"nothing can be written": {
			brk: func(m *fakeMemory) { m.failWrites(errFakeDisk, nil) },
			logs: []string{
				"could not remember the update that failed to install", "could not remember the version that ran",
			},
		},
	} {
		r, logs := loggedRig(t)
		r.release(t, next)
		require.NoError(t, r.engine.Tick(t.Context()), name)

		record(t, r, running, next, update.HelperWindow+time.Minute)
		helperLogIn(t, r, "2026-11-15T12:00:00Z: helper start",
			"2026-11-15T12:00:01Z: installed application failed validation: sealed — restoring backup")
		tt.brk(r.memory)

		require.Zero(t, r.engine.Settle(t.Context()), name)

		for _, line := range tt.logs {
			assert.Contains(t, logs.String(), line, name)
		}

		r.memory.failReads(nil, update.KeyUninstallable)
		r.memory.failWrites(nil, nil)

		s := r.status(t)
		assert.Empty(t, r.memory.get(update.KeyUninstallable), "nothing is remembered: %s", name)
		assert.Equal(t, next, s.InstallFailed.Version, name)
		assert.True(t, s.InstallFailed.Retryable, "an update not remembered as failing for good may be tried: %s", name)
		assert.Equal(t, update.StateReady, s.State, "its download is kept: %s", name)
	}
}

func TestAStartThatCantReadHowTheLastUpdateWentStillSettles(t *testing.T) {
	t.Parallel()

	r, logs := loggedRig(t)
	pending(t, r, next)
	r.memory.failReads(errFakeDisk, update.KeyPendingInstall)

	assert.Zero(t, r.engine.Settle(t.Context()))
	assert.Contains(t, logs.String(), "could not read how the last update went")
	assert.Equal(t, running, r.memory.get(update.KeyLastRunRelease), "the run is still remembered")
	assert.NotEmpty(t, r.memory.get(update.KeyPendingInstall), "a record it couldn't read is left for the next start")
	assert.Empty(t, r.status(t).InstallFailed.Version, "nothing is told of an install it couldn't read")
}

func TestARecordTheStartCantClearIsLogged(t *testing.T) {
	t.Parallel()

	r, logs := loggedRig(t)
	pending(t, r, running)
	r.memory.failWrites(nil, errFakeDisk)

	assert.Zero(t, r.engine.Settle(t.Context()))
	assert.Equal(t, running, r.status(t).JustUpdated, "the outcome is still told")
	assert.Contains(t, logs.String(), "could not clear the install's record")
}

func TestARestartThatCantClearItsRecordStillGoesBackToReady(t *testing.T) {
	t.Parallel()

	r, logs := loggedRig(t)
	r.release(t, next)
	require.NoError(t, r.engine.Tick(t.Context()))

	r.installer.installErr = errFakeRename
	r.memory.failWrites(nil, errFakeDisk)

	_, err := r.service.Restart(t.Context(), false)
	require.ErrorIs(t, err, errFakeRename)
	assert.Equal(t, update.StateReady, r.status(t).State)
	assert.Contains(t, logs.String(), "could not clear the install's record")
}

func TestARestartThatCantRecordTheInstallStopsBeforeTheQuit(t *testing.T) {
	t.Parallel()

	r, _ := ready(t)
	r.memory.failWrites(errFakeDisk, nil)

	_, err := r.service.Restart(t.Context(), false)
	require.ErrorIs(t, err, errFakeDisk)
	assert.Equal(t, "Couldn't get ready to install "+next+".", uierr.Message(err))

	installs, restarts := r.installer.calls()
	assert.Len(t, installs, 1, "the update was staged")
	assert.Zero(t, restarts, "but the app never quits for an install it can't record")
	assert.Empty(t, r.quitEvents())

	s := r.status(t)
	assert.Equal(t, update.StateReady, s.State)
	assert.Equal(t, uierr.Message(err), s.LastError)
}

func TestLaterAndShowAgainThatCantBeSavedChangeNothing(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.release(t, next)

	_, err := r.service.CheckNow(t.Context())
	require.NoError(t, err)

	s, err := r.service.HideBanner(t.Context())
	require.NoError(t, err)
	require.True(t, s.BannerHidden)

	r.memory.failWrites(nil, errFakeDisk)

	_, err = r.service.ShowBanner(t.Context())
	require.ErrorIs(t, err, errFakeDisk)
	assert.True(t, r.status(t).BannerHidden, "the banner stays hidden")

	r.memory.failWrites(nil, nil)

	s, err = r.service.ShowBanner(t.Context())
	require.NoError(t, err)
	require.False(t, s.BannerHidden)

	r.memory.failWrites(errFakeDisk, nil)

	_, err = r.service.HideBanner(t.Context())
	require.ErrorIs(t, err, errFakeDisk)
	assert.False(t, r.status(t).BannerHidden, "the banner stays shown")
}
