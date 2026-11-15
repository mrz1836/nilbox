package menubar_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/menubar"
	"github.com/mrz1836/nilbox/internal/update"
	"github.com/mrz1836/nilbox/internal/updateprefs"
)

// on is the status of a build that updates, running 0.2.0, as st changes it.
func on(change func(st *update.Status)) update.Status {
	st := update.Status{
		Flavor: update.FlavorRelease, Enabled: true, Current: "0.2.0", Mode: updateprefs.ModeDownload,
		State: update.StateIdle,
	}

	if change != nil {
		change(&st)
	}

	return st
}

const (
	// newer is the version found, newer than the 0.2.0 running.
	newer = "0.3.0"

	// releasePage is the release page a status names.
	releasePage = "https://example.com/releases/tag/v0.3.0"
)

// ready is 0.3.0, downloaded and waiting for a restart.
func ready(st *update.Status) { st.State, st.Latest = update.StateReady, newer }

// check is Check for Updates…, enabled.
func check() menubar.UpdateRow {
	return menubar.UpdateRow{Label: "Check for Updates…", Action: menubar.UpdateCheck}
}

func TestUpdateRows(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 15, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		st   update.Status
		want []menubar.UpdateRow
	}{
		{name: "a build that doesn't update shows nothing", st: update.Status{Flavor: update.FlavorSource, State: update.StateOff}},
		{name: "up to date", st: on(nil), want: []menubar.UpdateRow{check()}},
		{
			name: "a check running",
			st:   on(func(st *update.Status) { st.Checking, st.State = true, update.StateChecking }),
			want: []menubar.UpdateRow{{Label: "Checking for Updates…"}},
		},
		{
			name: "a version ready",
			st:   on(func(st *update.Status) { st.State, st.Latest = update.StateReady, newer }),
			want: []menubar.UpdateRow{check(), {Label: "Restart to Update (0.3.0)", Action: menubar.UpdateRestart}},
		},
		{
			name: "a check running while a version is ready",
			st:   on(func(st *update.Status) { st.State, st.Latest, st.Checking = update.StateReady, newer, true }),
			want: []menubar.UpdateRow{
				{Label: "Checking for Updates…"}, {Label: "Restart to Update (0.3.0)", Action: menubar.UpdateRestart},
			},
		},
		{
			name: "a version found with Just tell me",
			st: on(func(st *update.Status) {
				st.State, st.Latest, st.Mode = update.StateAvailable, newer, updateprefs.ModeNotify
			}),
			want: []menubar.UpdateRow{check(), {Label: "Update to 0.3.0…", Action: menubar.UpdateDownload}},
		},
		{
			name: "a version downloading",
			st:   on(func(st *update.Status) { st.State, st.Latest, st.Progress = update.StateDownloading, newer, 42 }),
			want: []menubar.UpdateRow{check(), {Label: "Downloading 0.3.0…"}},
		},
		{
			name: "restarting into a version",
			st:   on(func(st *update.Status) { st.State, st.Latest = update.StateInstalling, newer }),
			want: []menubar.UpdateRow{{Label: "Check for Updates…"}, {Label: "Restarting into 0.3.0…"}},
		},
		{
			name: "finishing a restart whose quit was held",
			st:   on(func(st *update.Status) { st.State, st.Latest = update.StateFinishing, newer }),
			want: []menubar.UpdateRow{{Label: "Check for Updates…"}, {
				Label:   "Finishing the update…",
				Tooltip: brand.Name + " is quitting to install 0.3.0. If it doesn't, quit it and open it again.",
			}},
		},
		{
			name: "a version ready whose last install may work if tried again",
			st: on(func(st *update.Status) {
				ready(st)
				st.InstallFailed = update.InstallFailure{Version: newer, Reason: "It didn't quit in time.", Retryable: true}
			}),
			want: []menubar.UpdateRow{
				check(),
				{Label: "0.3.0 didn't install", Tooltip: "It didn't quit in time."},
				{Label: "Try Updating to 0.3.0 Again", Action: menubar.UpdateRestart},
			},
		},
		{
			name: "a version found whose last install may work if tried again",
			st: on(func(st *update.Status) {
				st.State, st.Latest = update.StateAvailable, newer
				st.InstallFailed = update.InstallFailure{Version: newer, Reason: "It didn't quit in time.", Retryable: true}
			}),
			want: []menubar.UpdateRow{
				check(),
				{Label: "0.3.0 didn't install", Tooltip: "It didn't quit in time."},
				{Label: "Try Updating to 0.3.0 Again", Action: menubar.UpdateDownload},
			},
		},
		{
			name: "a version found that failed to install for good",
			st: on(func(st *update.Status) {
				st.State, st.Latest, st.ReleaseURL = update.StateAvailable, newer, releasePage
				st.InstallFailed = update.InstallFailure{Version: newer, Reason: "It failed its signature check."}
			}),
			want: []menubar.UpdateRow{
				check(),
				{Label: "0.3.0 didn't install", Tooltip: "It failed its signature check."},
				{Label: "Download 0.3.0 Manually…", Tooltip: releasePage, Action: menubar.UpdateManual},
			},
		},
		{
			name: "no version found after a restart that may work if tried again",
			st: on(func(st *update.Status) {
				st.ReleaseURL = update.ReleaseURL("0.2.0")
				st.InstallFailed = update.InstallFailure{Version: newer, Reason: "It didn't quit in time.", Retryable: true}
			}),
			want: []menubar.UpdateRow{check(), {Label: "0.3.0 didn't install", Tooltip: "It didn't quit in time."}},
		},
		{
			name: "no version found after a restart that failed for good",
			st: on(func(st *update.Status) {
				st.ReleaseURL = update.ReleaseURL("0.2.0")
				st.InstallFailed = update.InstallFailure{Version: newer, Reason: "It failed its signature check."}
			}),
			want: []menubar.UpdateRow{
				check(),
				{Label: "0.3.0 didn't install", Tooltip: "It failed its signature check."},
				{Label: "Download 0.3.0 Manually…", Tooltip: update.ReleaseURL(newer), Action: menubar.UpdateManual},
			},
		},
		{
			name: "a failure of another version than the one found",
			st: on(func(st *update.Status) {
				st.State, st.Latest = update.StateReady, "0.3.1"
				st.InstallFailed = update.InstallFailure{Version: newer, Reason: "It failed its signature check."}
			}),
			want: []menubar.UpdateRow{check(), {Label: "Restart to Update (0.3.1)", Action: menubar.UpdateRestart}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := menubar.UpdateRows(tt.st, menubar.UpdateNote{}, now)

			assert.Equal(t, tt.want, got.Rows)
			assert.True(t, got.Until.IsZero(), "no status line, so nothing to redraw it for")
		})
	}
}

func TestUpdateRowsStatusLine(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.November, 15, 12, 0, 0, 0, time.UTC)
	checked := menubar.UpdateNote{At: at, Checked: true}

	tests := []struct {
		name string
		st   update.Status
		note menubar.UpdateNote
		want menubar.UpdateRow // the status line, or none
	}{
		{name: "up to date", st: on(nil), note: checked, want: menubar.UpdateRow{
			Label: "Up to date (0.2.0)", Tooltip: "Checked just now.",
		}},
		{
			name: "a check that failed",
			st:   on(func(st *update.Status) { st.LastError = "Couldn't reach the releases." }),
			note: menubar.UpdateNote{At: at, Checked: true, CheckError: "Couldn't reach the releases."},
			want: menubar.UpdateRow{Label: "Check failed", Tooltip: "Couldn't reach the releases."},
		},
		{
			name: "a check that found a version says nothing more: its row shows it",
			st:   on(func(st *update.Status) { st.State, st.Latest = update.StateReady, newer }),
			note: checked,
		},
		{
			name: "a check that found a version whose download failed",
			st: on(func(st *update.Status) {
				st.State, st.Latest, st.LastError = update.StateAvailable, newer, "Couldn't download 0.3.0."
			}),
			note: checked,
			want: menubar.UpdateRow{Label: "Download failed", Tooltip: "Couldn't download 0.3.0."},
		},
		{
			name: "a check running again shows its row alone",
			st:   on(func(st *update.Status) { st.Checking = true }),
			note: checked,
		},
		{
			name: "a restart refused",
			st:   on(func(st *update.Status) { st.State, st.Latest = update.StateReady, newer }),
			note: menubar.UpdateNote{At: at, Refused: "Not restarted: a run is going", Why: "Restarting stops a run."},
			want: menubar.UpdateRow{Label: "Not restarted: a run is going", Tooltip: "Restarting stops a run."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := menubar.UpdateRows(tt.st, tt.note, at.Add(30*time.Second))
			without := menubar.UpdateRows(tt.st, menubar.UpdateNote{}, at.Add(30*time.Second))

			if tt.want == (menubar.UpdateRow{}) {
				assert.Equal(t, without, got, "no status line")

				return
			}

			assert.Equal(t, append(without.Rows, tt.want), got.Rows, "the status line comes last")
			assert.Equal(t, at.Add(menubar.NoteFor), got.Until, "drawn again once the line ends")

			after := menubar.UpdateRows(tt.st, tt.note, at.Add(menubar.NoteFor))
			assert.NotContains(t, after.Rows, tt.want, "a minute on, the line is gone")
			assert.True(t, after.Until.IsZero())
		})
	}
}
