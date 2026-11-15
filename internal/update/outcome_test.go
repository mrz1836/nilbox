package update_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
	"github.com/mrz1836/nilbox/internal/update"
)

// oldPID is the process of the app that restarted to update, in the outcome tests.
const oldPID = 31337

// pending stores the record of an install from running to to, begun by oldPID.
func pending(t *testing.T, r *rig, to string) {
	t.Helper()

	body, err := json.Marshal(map[string]any{"from": "0.1.0", "to": to, "at": time.Now().UTC(), "pid": oldPID})
	require.NoError(t, err)

	r.memory.put(update.KeyPendingInstall, string(body))
}

// helperLogIn writes the helper's log of oldPID into the rig's temporary folder.
func helperLogIn(t *testing.T, r *rig, lines ...string) {
	t.Helper()

	path := filepath.Join(r.cfg.TempDir, "wails-update-"+strconv.Itoa(oldPID)+".log")
	testutil.WriteFile(t, path, strings.Join(lines, "\n")+"\n")
}

func TestTheOutcomeOfTheLastRestart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		before  func(t *testing.T, r *rig)
		updated string
		failed  string
	}{
		{
			name: "the update installed",
			before: func(t *testing.T, r *rig) {
				pending(t, r, running)
				helperLogIn(t, r, "2026-11-15T12:00:00Z: helper start", "2026-11-15T12:00:04Z: helper done")
			},
			updated: running,
		},
		{
			name: "the update didn't install",
			before: func(t *testing.T, r *rig) {
				pending(t, r, next)
				helperLogIn(t, r, "2026-11-15T12:00:00Z: helper start",
					"2026-11-15T12:00:30Z: parent did not exit within timeout: timed out — aborting swap",
					"2026-11-15T12:00:30Z: relaunched original after parent-timeout abort")
			},
			failed: next,
		},
		{
			name:    "a newer build installed by hand",
			before:  func(_ *testing.T, r *rig) { r.memory.put(update.KeyLastRunRelease, "0.1.0") },
			updated: running,
		},
		{name: "the first run ever", before: func(*testing.T, *rig) {}},
		{name: "the same version", before: func(_ *testing.T, r *rig) { r.memory.put(update.KeyLastRunRelease, running) }},
		{name: "an older build", before: func(_ *testing.T, r *rig) { r.memory.put(update.KeyLastRunRelease, later) }},
	}

	for _, tt := range tests {
		r := newRig(t)
		tt.before(t, r)

		r.engine.Settle(t.Context())

		s := r.status(t)
		assert.Equal(t, tt.updated, s.JustUpdated, tt.name)
		assert.Equal(t, tt.failed, s.InstallFailed.Version, tt.name)
		assert.Equal(t, running, r.memory.get(update.KeyLastRunRelease), "this build ran: %s", tt.name)
		assert.Empty(t, r.memory.get(update.KeyPendingInstall), "the record is cleared: %s", tt.name)
		assert.NoFileExists(t, filepath.Join(r.cfg.TempDir, "wails-update-"+strconv.Itoa(oldPID)+".log"),
			"the helper's log is cleaned up: %s", tt.name)
	}
}

func TestAFailedInstallKeepsTheHelpersLog(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	pending(t, r, next)

	log := []string{
		"2026-11-15T12:00:00Z: helper start: target=/Applications/X.app new=/Applications/wails-update-1/X.app pid=1",
		"2026-11-15T12:00:01Z: installed application failed validation: codesign — restoring backup",
	}
	helperLogIn(t, r, log...)

	r.engine.Settle(t.Context())

	failed := r.status(t).InstallFailed
	assert.Equal(t, update.InstallFailure{
		Version: next, Reason: "The new version failed its signature check, so the old one was put back.", Log: true,
	}, failed)
	assert.NotContains(t, failed.Reason, "/Applications", "no path from the log is repeated")

	kept, err := os.ReadFile(filepath.Join(r.cfg.Logs, "update-"+next+".log"))
	require.NoError(t, err)
	assert.Equal(t, strings.Join(log, "\n")+"\n", string(kept))

	require.NoError(t, r.service.ShowInstallLog(t.Context()))
	assert.Equal(t, []string{filepath.Join(r.cfg.Logs, "update-"+next+".log")}, r.reveal.shown)
}

func TestAFailedInstallWithNoLogSaysSo(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	pending(t, r, next)

	r.engine.Settle(t.Context())

	failed := r.status(t).InstallFailed
	assert.Equal(t, next, failed.Version)
	assert.Equal(t, "The update didn't install, and left no log saying why.", failed.Reason)
	assert.False(t, failed.Log)

	err := r.service.ShowInstallLog(t.Context())
	require.ErrorIs(t, err, update.ErrNoLog)
	assert.Equal(t, "No log of that update was kept.", uierr.Message(err))
}

func TestARecordThatNamesNoReleaseIsDropped(t *testing.T) {
	t.Parallel()

	for _, stored := range []string{"{", `{"to":"../../x"}`, `{"to":""}`, "null"} {
		r := newRig(t)
		r.memory.put(update.KeyPendingInstall, stored)

		r.engine.Settle(t.Context())

		s := r.status(t)
		assert.Empty(t, s.InstallFailed.Version, stored)
		assert.Empty(t, s.JustUpdated, stored)
		assert.Empty(t, r.memory.get(update.KeyPendingInstall), stored)
	}
}

func TestHelperReasonWordsEachEnding(t *testing.T) {
	t.Parallel()

	fix := " Rename " + brand.Name + ".app.bak to " + brand.Name + ".app in its folder, or install " + brand.Name + " again."

	for want, lines := range map[string][]string{
		brand.Name + " didn't quit in time, so nothing was changed.": {
			"parent did not exit within timeout: timeout — aborting swap", "relaunched original after parent-timeout abort",
		},
		"The downloaded update couldn't be installed, so nothing was changed.": {
			"staged update is not installable: empty — aborting swap",
		},
		brand.Name + " couldn't be copied aside first, so nothing was changed.": {"backup failed: no space"},
		"macOS didn't let " + brand.Name + " be replaced, so the old version was kept.": {
			"replace (attempt 20): operation not permitted", "all swap attempts exhausted — relaunching intact original",
		},
		"macOS didn't let " + brand.Name + " be replaced, and the old version couldn't be put back." + fix: {
			"all swap attempts exhausted — restoring backup", "restore failed: rename: no such file",
		},
		"The new version wouldn't open, so the old one was put back.": {"launch new failed: exit 1 — restoring backup"},
		"The new version wouldn't open, and the old one couldn't be put back." + fix: {
			"launch new failed: exit 1 — restoring backup", "restore failed: busy",
		},
		"The new version failed its signature check, and the old one couldn't be put back." + fix: {
			"installed application failed validation: sealed — restoring backup", "restore failed: busy",
		},
		"The update's files were missing, so nothing was changed.": {"stat target failed: no such file"},
		"The update didn't install, and its log doesn't say why.":  {"something else entirely"},
		"The update installed, but this isn't the copy of " + brand.Name + " it replaced.": {
			"swap succeeded on attempt 1", "helper done",
		},
	} {
		log := []byte{}
		for _, line := range lines {
			log = append(log, "2026-11-15T12:00:00Z: "+line+"\n"...)
		}

		assert.Equal(t, want, update.HelperReason(log), lines)
	}
}
