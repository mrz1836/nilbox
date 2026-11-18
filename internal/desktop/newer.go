package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
	"github.com/mrz1836/nilbox/internal/update"
)

// errNoUpdate is why a dialog offered an update with nothing to run it.
var errNoUpdate = errors.New("this build has no update to run")

// Choice is what a button of a startup dialog does.
type Choice string

// The choices.
const (
	// ChoiceQuit quits the app.
	ChoiceQuit Choice = "quit"

	// ChoicePutBack puts the backup the dialog offers back in place of the newer data
	// (Failure.Restore), and opens the app again.
	ChoicePutBack Choice = "putBack"

	// ChoiceUpdate checks for the latest release, installs it in place of this copy, and
	// opens it: Update and Reopen, and Try Again after an update that failed.
	ChoiceUpdate Choice = "update"

	// ChoiceCopy puts the button's command (Button.Copies) on the clipboard.
	ChoiceCopy Choice = "copy"
)

// Button is a button of a startup dialog.
type Button struct {
	// Label is its text, and Choice what it does.
	Label  string
	Choice Choice

	// Copies is the command a ChoiceCopy button puts on the clipboard.
	Copies string

	// Default marks the button that takes Return, and Cancel the one that takes Escape: the
	// runtime's macOS alert gives a button one or the other, never both.
	Default bool
	Cancel  bool
}

// Buttons are the dialog's buttons, left to right: the backup's, when one is offered, then
// the actions, then Quit, rightmost. The first action takes Return, and Quit takes Escape;
// with no action, Quit takes Return unless a backup is offered, so no key ever puts a
// backup back by accident. While an update runs (Waiting), Quit is Stop and Quit, which
// takes Escape alone: a Return pressed again after Update and Reopen never stops it.
func (f Failure) Buttons() []Button {
	out := []Button{}

	if f.Restore != nil {
		out = append(out, Button{Label: f.Restore.Button, Choice: ChoicePutBack})
	}

	for i, a := range f.Actions {
		a.Default, a.Cancel = i == 0, false
		out = append(out, a)
	}

	quit := Button{Label: "Quit", Choice: ChoiceQuit}

	switch {
	case f.Waiting:
		quit.Label, quit.Cancel = "Stop and Quit", true
	case len(out) == 0:
		quit.Default = true
	default:
		quit.Cancel = true
	}

	return append(out, quit)
}

// Newer is what this build can do about data a newer build saved: its way forward
// (update.Bare's Way) and the commands it offers to copy.
type Newer struct {
	Way      update.Way
	Commands Commands
}

// Commands are the commands the dialogs about data a newer build saved offer to copy.
type Commands struct {
	// Install is the README's install command, which installs the latest release in the
	// Applications folder this copy runs from, or would move to.
	Install string

	// Build pulls the latest master and builds the app from source, in the clone a build
	// from source runs from when it can be told.
	Build string
}

// buildCommand pulls the latest and builds the app from source (README, Build from
// source).
const buildCommand = "git pull && magex app:run"

// NewerCommands works out the commands for a build that runs from bundle ("" outside one),
// at loc: the install command for the Applications folder loc names (the one in home when
// it runs there, or would move there), and the build command, run in the clone a build
// from source in its clone's bin folder runs from.
func NewerCommands(bundle string, loc update.Location) Commands {
	user := loc.Kind == update.LocationUserApplications ||
		(loc.Kind != update.LocationApplications && strings.HasPrefix(loc.Target, "~/"))

	build := buildCommand
	if clone, ok := cloneOf(bundle); ok {
		build = "cd " + textutil.ShellWord(clone) + " && " + buildCommand
	}

	return Commands{Install: InstallCommand(user), Build: build}
}

// cloneOf is the clone a build from source at bundle runs from: magex app:build builds it
// into the clone's bin folder. ok is false when bundle isn't in a folder named bin beside a
// .git.
func cloneOf(bundle string) (string, bool) {
	if bundle == "" {
		return "", false
	}

	bin := filepath.Dir(filepath.Clean(bundle))
	if filepath.Base(bin) != "bin" {
		return "", false
	}

	clone := filepath.Dir(bin)
	if _, err := os.Lstat(filepath.Join(clone, ".git")); err != nil {
		return "", false
	}

	return clone, true
}

// InstallCommand is the README's install command: it downloads the latest release's disk
// image and puts the app in /Applications in place of the copy there, or, for user, in
// ~/Applications, and opens it. The README's Install section holds both, word for word.
func InstallCommand(user bool) string {
	apps, first := "/Applications", ""
	if user {
		apps, first = "~/Applications", "mkdir -p ~/Applications\n"
	}

	dmg := update.AssetNames("").DMG
	app := apps + "/" + brand.Name + ".app"

	return first + `(cd "$(mktemp -d)" &&
  curl -fsSLO ` + brand.Repository + `/releases/latest/download/` + dmg + ` &&
  hdiutil attach -nobrowse -quiet -mountpoint "$PWD/mnt" ` + dmg + ` &&
  rm -rf ` + app + ` &&
  ditto mnt/` + brand.Name + `.app ` + app + ` &&
  hdiutil detach -quiet mnt &&
  open ` + app + `)`
}

// The buttons the dialogs about data a newer build saved offer.
const (
	updateLabel  = "Update and Reopen"
	installLabel = "Copy Install Command"
)

// safe is what every dialog about data a newer build saved says of the data.
const safe = "Your data is safe and untouched"

// newerData is the startup dialog for data a newer build saved, which this build can't
// open, with newer's way forward: Update and Reopen where it updates itself, the build
// command in a build from source, which never downloads, and else the install command.
func newerData(newer Newer) Failure {
	title := brand.Name + " needs an update"
	saved := "The data was saved by a newer version of " + brand.Name + " than this one."

	switch newer.Way {
	case update.WayUpdate:
		return Failure{
			Title: title,
			Message: saved + " " + updateLabel + " downloads the latest release from GitHub, checks it, installs it " +
				"in place of this copy, and opens it.\n\n" + safe + ": the newer version opens it as it is.",
			Actions: []Button{{Label: updateLabel, Choice: ChoiceUpdate}},
		}
	case update.WaySource:
		return Failure{
			Title: title,
			Message: "The data was saved by a newer build of " + brand.Name + " than this one. A build from source " +
				"never downloads an update: pull the latest master and build it again, in its clone:\n\n" +
				command(newer.Commands.Build) + "\n\n" + safe + ": the newer build opens it as it is.",
			Actions: []Button{{Label: "Copy Command", Choice: ChoiceCopy, Copies: newer.Commands.Build}},
		}
	case update.WayInstall:
	}

	return Failure{
		Title: title,
		Message: saved + " This copy can't update itself where it runs, so install the latest release: " +
			installLabel + ", quit, then paste it into Terminal. It downloads the latest release from GitHub, " +
			"puts it in Applications, and opens it.\n\n" + safe + ": the newer version opens it as it is.",
		Actions: []Button{installButton(newer.Commands)},
	}
}

// Updating is the dialog shown while the update runs, over the one that started it: its
// one button stops the update and quits.
func Updating() Failure {
	return Failure{
		Title: "Updating " + brand.Name + "…",
		Message: "Checking for the latest release, then downloading it from GitHub, checking it against the key " +
			"built into " + brand.Name + ", and installing it in place of this copy. " + brand.Name + " opens " +
			"again on its own when it's done, usually within a few minutes.\n\n" + safe + ".",
		Waiting: true,
	}
}

// NoNewerRelease is the dialog shown when the latest release is no newer than this build,
// so no published release can open the data: it offers the build from source, and the
// install command for once a newer release is out.
func NoNewerRelease(cmds Commands) Failure {
	return Failure{
		Title: "No release can open this data yet",
		Message: "No published release can open this data yet; it was saved by a newer build, likely one built " +
			"from source. " + safe + ".\n\nTo open it now, build " + brand.Name + " from source on the latest " +
			"master, in your clone of it:\n\n" + command(cmds.Build) + "\n\nOnce a newer release is out, " +
			updateLabel + " installs it, and so does the README's install command.",
		Actions: []Button{
			{Label: "Copy Build Command", Choice: ChoiceCopy, Copies: cmds.Build},
			installButton(cmds),
		},
	}
}

// UpdateFailed words err, an update that failed before the app quit, which changed
// nothing, with Try Again and the install command.
func UpdateFailed(err error, cmds Commands) Failure {
	return Failure{
		Title: brand.Name + " couldn't update",
		Message: uierr.Message(err) + "\n\nNothing was installed, and your data is safe and untouched. Try again, " +
			"or install the latest release by hand: " + installLabel + ", quit, then paste it into Terminal.",
		Actions: []Button{{Label: "Try Again", Choice: ChoiceUpdate}, installButton(cmds)},
	}
}

// Copied returns f said again once its button b put b's command on the clipboard, or, when
// copied is false, failed to.
func (f Failure) Copied(b Button, copied bool) Failure {
	if copied {
		f.Message += "\n\nCopied. Quit " + brand.Name + ", then paste it into Terminal."

		return f
	}

	f.Message += "\n\nThe command couldn't be copied. The README holds it: " + brand.Repository + "#install"

	return f
}

// installButton copies the install command.
func installButton(cmds Commands) Button {
	return Button{Label: installLabel, Choice: ChoiceCopy, Copies: cmds.Install}
}

// command sets a command apart in a dialog's text.
func command(cmd string) string {
	return "    " + strings.ReplaceAll(cmd, "\n", "\n    ")
}
