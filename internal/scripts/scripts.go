// Package scripts is Repo scripts' service: the commands a clone's own files name (its
// t3.json scripts, its .vscode/tasks.json tasks, and its magex targets), listed for the
// page, and run on the user's click through scriptrun.
//
// Listing runs nothing in a clone. t3.json and tasks.json are read as files, tasks.json
// through a JSONC stripper. magex's built-in targets come from `magex help:commands
// json=true`, run in a scratch folder of the app's own, never in a clone, whose
// .github/env files magex would load over its environment, PATH included; the answer is
// kept per copy of magex. The repository's own targets are read from its magefiles/ Go
// files with go/parser, by the rules magex itself discovers them by, and kept per clone
// until a file there changes.
//
// A command from the repository's files asks the user before its first run, and again
// whenever it changes (D13): its trust is the SHA-256 of the canonical JSON of its
// effective form, with ${workspaceFolder} and ${env:…} left as written, so moving the
// clone asks nothing. A custom magex target's form holds the SHA-256 of every file under
// magefiles/. magex's built-in targets need no trust. A run's Before, under the clone's
// lock, reads the source again and works the hash out afresh, from the files' bytes, and
// refuses a command whose hash isn't the one the user confirmed, or the one they trusted
// before: what runs is exactly what the user saw.
//
// The user's own scripts, saved in the app (Save, Delete, Duplicate, Move), show in the
// clones their scope includes: every clone, one with a go.mod at its root, or the clones
// of the repositories they chose. They need no trust, since the user wrote them, and run
// exactly as written, as the login shell's -c, in the clone, with the clone's repository,
// folder, and default branch in Variables, where the script says, or else where the
// setting sends runs.
//
// It owns the script_trust table, the commands the user trusted, and user_scripts, the
// user's own scripts, in the order the user put them in.
package scripts

import (
	"errors"
	"slices"

	"github.com/mrz1836/nilbox/internal/integrationprefs"
)

// Source is where a script comes from.
type Source string

// The sources, in the order the page lists them.
const (
	// SourceYours is the user's own scripts, saved in the app.
	SourceYours Source = "yours"

	// SourceT3 is the repository's t3.json.
	SourceT3 Source = "t3"

	// SourceTasks is the repository's .vscode/tasks.json.
	SourceTasks Source = "tasks"

	// SourceMagex is magex's targets: its built-ins and the repository's own.
	SourceMagex Source = "magex"
)

// Sources returns every source, in the order the page lists them.
func Sources() []Source {
	return []Source{SourceYours, SourceT3, SourceTasks, SourceMagex}
}

// Valid reports whether s is one of Sources.
func (s Source) Valid() bool {
	return slices.Contains(Sources(), s)
}

// The files the sources read, relative to the clone.
const (
	t3File       = "t3.json"
	tasksFile    = ".vscode/tasks.json"
	magefilesDir = "magefiles"
	magefileGo   = "magefile.go"
)

// Trust is what running a script asks of the user first.
type Trust string

// The trust states.
const (
	// TrustNone is a script that needs no trust: a built-in magex target, or the user's
	// own.
	TrustNone Trust = "none"

	// TrustTrusted is a repository command the user trusted as it is now.
	TrustTrusted Trust = "trusted"

	// TrustAsk is a repository command the user never trusted.
	TrustAsk Trust = "ask"

	// TrustChanged is a repository command that changed since the user trusted it.
	TrustChanged Trust = "changed"
)

// InputKind is the kind of a task's input.
type InputKind string

// The input kinds tasks.json can use.
const (
	// InputPick picks one of a list of values.
	InputPick InputKind = "pickString"

	// InputPrompt is a line of text the user types.
	InputPrompt InputKind = "promptString"
)

// Page is Repo scripts for one clone: its sources in the order the page lists them.
type Page struct {
	// Repo is the clone's repository on GitHub, "owner/name", or "" for a folder whose
	// origin isn't on GitHub, where trust is kept by repository, so a repository command
	// asks every time and the question offers no "Don't ask again"; Name is what the page
	// calls it, and Dir its folder, with ~, which every request names it by.
	Repo string `json:"repo"`
	Name string `json:"name"`
	Dir  string `json:"dir"`

	// Branch is the branch the clone is on, or "" when HEAD is detached or unread, and
	// FromBranch that branch when it isn't the repository's default, since a branch can
	// change what the clone's files say: a repository command's question names it.
	Branch     string `json:"branch"`
	FromBranch string `json:"fromBranch"`

	// Switches are the settings' sources, as set now.
	Switches integrationprefs.ScriptSources `json:"switches"`

	// Off reports the Scripts integration off, and Why says why; the page then has no
	// sections, and nothing runs.
	Off bool   `json:"off"`
	Why string `json:"why"`

	// Sections are the sources: the user's own scripts, t3.json, tasks.json, and magex.
	Sections []Section `json:"sections"`
}

// Section is one source's scripts.
type Section struct {
	// Source is the source, and Title what the page heads it with.
	Source Source `json:"source"`
	Title  string `json:"title"`

	// Off reports a source turned off in Settings → Integrations, and Found one the
	// clone has: its file, or for magex a copy the app can run.
	Off   bool `json:"off"`
	Found bool `json:"found"`

	// Error says why the source couldn't be read, and Note what the page says under its
	// heading; both may be "".
	Error string `json:"error"`
	Note  string `json:"note"`

	// Entries are its scripts, in the file's order; magex's are the repository's own
	// targets, then the built-ins.
	Entries []Entry `json:"entries"`

	// Common are the refs of magex's common targets, for the page's chips, in their
	// order.
	Common []string `json:"common"`
}

// Entry is one script as the page lists it.
type Entry struct {
	// Ref names it within the clone, for a run: its source and its name.
	Ref    string `json:"ref"`
	Source Source `json:"source"`

	// Name is its name, Command what it runs, with ${…} as written and a task's
	// dependencies before it, Icon t3.json's icon name, and Detail a task's detail or a
	// target's description.
	Name    string `json:"name"`
	Command string `json:"command"`
	Icon    string `json:"icon"`
	Detail  string `json:"detail"`

	// Custom reports a magex target from the repository's own magefiles, and Args one
	// that takes key=value arguments.
	Custom bool `json:"custom"`
	Args   bool `json:"args"`

	// Runnable reports a script the app can run; Why says, when it can't, why it is
	// greyed.
	Runnable bool   `json:"runnable"`
	Why      string `json:"why"`

	// Release reports a command that bumps a version: Release does that with its
	// checks, and the page points there; running it anyway takes RunRequest.Anyway.
	Release bool `json:"release"`

	// Background reports a task that runs until stopped, and Note what the page says of
	// how it runs, as for dependencies VS Code runs side by side.
	Background bool   `json:"background"`
	Note       string `json:"note"`

	// Trust is what running it asks first; Trusted is the command trusted before, for a
	// changed one, and TrustNote what the question adds, as for a magex target, that it
	// runs the repository's own code. SHA256 is the hash of what the page shows, which a
	// run the user confirmed sends back.
	Trust     Trust  `json:"trust"`
	Trusted   string `json:"trusted"`
	TrustNote string `json:"trustNote"`
	SHA256    string `json:"sha256"`

	// Inputs are the values a task asks for, which a small form fills in before it runs.
	Inputs []Input `json:"inputs"`

	// Mine is the user's own script, as saved, for its menu's Edit and the question Ask
	// before running asks; nil for every other source's.
	Mine *UserScript `json:"mine"`
}

// Input is one value a task asks for: VS Code's pickString or promptString.
type Input struct {
	ID          string    `json:"id"`
	Kind        InputKind `json:"kind"`
	Description string    `json:"description"`
	Default     string    `json:"default"`

	// Options are a pickString's choices; empty for a promptString.
	Options []Option `json:"options"`
}

// Option is one choice of a pickString: its label, the value it fills in, and Shown, what
// the form shows for it, the label with the value beside it whenever they differ, since
// the value is what runs.
type Option struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Shown string `json:"shown"`
}

// optionOf is the choice label gives value.
func optionOf(label, value string) Option {
	shown := value
	if label != value {
		shown = label + " (" + value + ")"
	}

	return Option{Label: label, Value: value, Shown: shown}
}

// Clone is a clone under the clone root, as the page's picker lists it: its folder's
// name, and its folder, with ~.
type Clone struct {
	Name string `json:"name"`
	Dir  string `json:"dir"`
}

var (
	// ErrNotSetUp is returned by a run on a Service built without the runner.
	ErrNotSetUp = errors.New("scripts are not set up")

	// ErrNoClone is returned for a folder that isn't a clone under the clone root.
	ErrNoClone = errors.New("not a clone under the clone root")

	// ErrInvalidRequest is returned for a run whose script, inputs, arguments, or place
	// isn't one.
	ErrInvalidRequest = errors.New("invalid scripts request")

	// ErrNotFound is returned for a script its source no longer names.
	ErrNotFound = errors.New("the script is gone")

	// ErrCantRun is returned for a script greyed on the page.
	ErrCantRun = errors.New("the script can't run")

	// ErrRelease is returned for a version bump run without Anyway: Release does that.
	ErrRelease = errors.New("version bumps belong to Release")

	// ErrNeedsTrust is returned for a repository command the user hasn't confirmed as it
	// is now.
	ErrNeedsTrust = errors.New("the command needs the user's trust")

	// ErrChanged is returned for a repository command that changed after the user
	// confirmed it.
	ErrChanged = errors.New("the command changed since it was confirmed")
)
