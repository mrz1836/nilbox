package menubar

import (
	"time"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/update"
)

// NoteFor is how long the menu's status line shows how a check, or a restart, the menu
// itself asked for went.
const NoteFor = time.Minute

// UpdateAction is what a row of the update's part of the menu does when clicked.
type UpdateAction string

// The actions. A row with none is a line that can't be clicked.
const (
	// UpdateNone is a row that does nothing: a line that says how things stand.
	UpdateNone UpdateAction = ""

	// UpdateCheck checks for a new version now.
	UpdateCheck UpdateAction = "check"

	// UpdateRestart restarts into the version downloaded and checked.
	UpdateRestart UpdateAction = "restart"

	// UpdateDownload downloads the version found, then restarts into it, as Just tell me
	// leaves one.
	UpdateDownload UpdateAction = "download"

	// UpdateManual opens the release page of the version found, to download it by hand, as
	// an update that failed to install for good leaves the user to.
	UpdateManual UpdateAction = "manual"
)

// UpdateRow is one row of the update's part of the menu.
type UpdateRow struct {
	// Label is the row's text, and Tooltip what hovering it says, or "".
	Label   string
	Tooltip string

	// Action is what a click does; a row with none is shown disabled.
	Action UpdateAction
}

// Updates is the update's part of the menu, above Refresh Now: Check for Updates…, the
// version ready or found, and, for a minute after the menu asked for a check or a restart,
// a line saying how it went. A build that doesn't update shows none of it.
type Updates struct {
	// Rows are the rows, in order, or none.
	Rows []UpdateRow

	// Until is when the status line ends, and the menu is to be drawn again without it,
	// or the zero time when none shows.
	Until time.Time
}

// UpdateNote is what the menu itself last asked of the update, as the shell remembers it:
// a check, or a restart or download that was refused, and when it ended.
type UpdateNote struct {
	// At is when it ended; the zero time is nothing asked.
	At time.Time

	// Checked marks a check the menu asked for, and CheckError what the check said went
	// wrong, as its status's LastError, or "".
	Checked    bool
	CheckError string

	// Refused is the line a restart or a download the menu asked for shows when it was
	// refused or failed, and Why its words, or "".
	Refused string
	Why     string
}

// UpdateRows works out the update's part of the menu from its status at now, and what the
// menu itself last asked of it (note):
//
//   - Check for Updates…, or Checking for Updates… (disabled) while a check runs;
//   - Restart to Update (X) once X is ready, Update to X… once X is found and not yet
//     downloaded (Just tell me), or a disabled line while X downloads, the app restarts
//     into it, or it finishes a restart whose quit was held;
//   - when the last restart didn't install X, the version found: a disabled line saying
//     so, its reason the tooltip, then Try Updating to X Again when trying again may work,
//     or Download X Manually…, which opens its release page, when X fails the same way
//     every time;
//   - for NoteFor after the menu's own check: Up to date (current), Check failed, or, when
//     the check found a version whose download then failed, Download failed; after a
//     restart or download it refused, the refusal. Each says more in its tooltip.
//
// A build that doesn't update gets none.
func UpdateRows(st update.Status, note UpdateNote, now time.Time) Updates {
	if !st.Enabled {
		return Updates{}
	}

	rows := append([]UpdateRow{checkRow(st)}, versionRows(st)...)

	if note.At.IsZero() || now.Before(note.At) || now.Sub(note.At) >= NoteFor {
		return Updates{Rows: rows}
	}

	if row, ok := noteRow(st, note); ok {
		return Updates{Rows: append(rows, row), Until: note.At.Add(NoteFor)}
	}

	return Updates{Rows: rows}
}

// checkRow is Check for Updates…, disabled while a check runs or the app restarts.
func checkRow(st update.Status) UpdateRow {
	switch {
	case st.Checking:
		return UpdateRow{Label: "Checking for Updates…"}
	case st.State == update.StateInstalling || st.State == update.StateFinishing:
		return UpdateRow{Label: "Check for Updates…"}
	default:
		return UpdateRow{Label: "Check for Updates…", Action: UpdateCheck}
	}
}

// versionRows are the rows of the newer version, while there is one: its row, or, when the
// last restart didn't install it, a line saying so, then Try Updating to X Again or
// Download X Manually….
func versionRows(st update.Status) []UpdateRow {
	row, ok := installRow(st)
	if st.Latest == "" || st.InstallFailed.Version != st.Latest {
		if !ok {
			return nil
		}

		return []UpdateRow{row}
	}

	rows := []UpdateRow{{Label: st.Latest + " didn't install", Tooltip: st.InstallFailed.Reason}}

	switch {
	case !st.InstallFailed.Retryable && (st.State == update.StateAvailable || st.State == update.StateReady):
		rows = append(rows, UpdateRow{
			Label: "Download " + st.Latest + " Manually…", Tooltip: st.ReleaseURL, Action: UpdateManual,
		})
	case ok && row.Action != UpdateNone:
		row.Label = "Try Updating to " + st.Latest + " Again"
		rows = append(rows, row)
	case ok:
		rows = append(rows, row)
	}

	return rows
}

// installRow is the row of the newer version, while there is one.
func installRow(st update.Status) (UpdateRow, bool) {
	switch st.State {
	case update.StateReady:
		return UpdateRow{Label: "Restart to Update (" + st.Latest + ")", Action: UpdateRestart}, true
	case update.StateAvailable:
		return UpdateRow{Label: "Update to " + st.Latest + "…", Action: UpdateDownload}, true
	case update.StateDownloading:
		return UpdateRow{Label: "Downloading " + st.Latest + "…"}, true
	case update.StateInstalling:
		return UpdateRow{Label: "Restarting into " + st.Latest + "…"}, true
	case update.StateFinishing:
		return UpdateRow{
			Label:   "Finishing the update…",
			Tooltip: brand.Name + " is quitting to install " + st.Latest + ". If it doesn't, quit it and open it again.",
		}, true
	case update.StateOff, update.StateIdle, update.StateChecking:
		return UpdateRow{}, false
	default:
		return UpdateRow{}, false
	}
}

// noteRow is the status line note leaves, while it has something to say.
func noteRow(st update.Status, note UpdateNote) (UpdateRow, bool) {
	switch {
	case note.Refused != "":
		return UpdateRow{Label: note.Refused, Tooltip: note.Why}, true
	case !note.Checked || st.Checking:
		return UpdateRow{}, false
	case note.CheckError != "":
		return UpdateRow{Label: "Check failed", Tooltip: note.CheckError}, true
	case st.Latest == "":
		return UpdateRow{Label: "Up to date (" + st.Current + ")", Tooltip: "Checked just now."}, true
	case st.State == update.StateAvailable && st.LastError != "":
		return UpdateRow{Label: "Download failed", Tooltip: st.LastError}, true
	default:
		return UpdateRow{}, false
	}
}
