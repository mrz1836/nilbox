package menubar_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

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
			st:   on(func(st *update.Status) { st.State, st.Latest = update.StateReady, "0.3.0" }),
			want: []menubar.UpdateRow{check(), {Label: "Restart to Update (0.3.0)", Action: menubar.UpdateRestart}},
		},
		{
			name: "a check running while a version is ready",
			st:   on(func(st *update.Status) { st.State, st.Latest, st.Checking = update.StateReady, "0.3.0", true }),
			want: []menubar.UpdateRow{
				{Label: "Checking for Updates…"}, {Label: "Restart to Update (0.3.0)", Action: menubar.UpdateRestart},
			},
		},
		{
			name: "a version found with Just tell me",
			st: on(func(st *update.Status) {
				st.State, st.Latest, st.Mode = update.StateAvailable, "0.3.0", updateprefs.ModeNotify
			}),
			want: []menubar.UpdateRow{check(), {Label: "Update to 0.3.0…", Action: menubar.UpdateDownload}},
		},
		{
			name: "a version downloading",
			st:   on(func(st *update.Status) { st.State, st.Latest, st.Progress = update.StateDownloading, "0.3.0", 42 }),
			want: []menubar.UpdateRow{check(), {Label: "Downloading 0.3.0…"}},
		},
		{
			name: "restarting into a version",
			st:   on(func(st *update.Status) { st.State, st.Latest = update.StateInstalling, "0.3.0" }),
			want: []menubar.UpdateRow{{Label: "Check for Updates…"}, {Label: "Restarting into 0.3.0…"}},
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
			st:   on(func(st *update.Status) { st.State, st.Latest = update.StateReady, "0.3.0" }),
			note: checked,
		},
		{
			name: "a check that found a version whose download failed",
			st: on(func(st *update.Status) {
				st.State, st.Latest, st.LastError = update.StateAvailable, "0.3.0", "Couldn't download 0.3.0."
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
			st:   on(func(st *update.Status) { st.State, st.Latest = update.StateReady, "0.3.0" }),
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
