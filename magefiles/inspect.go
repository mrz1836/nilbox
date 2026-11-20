//go:build mage

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/magefile/mage/mg"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/devtools"
)

// shotsDir is where inspect:shot saves a picture unless out= names a file.
const shotsDir = "bin/inspect"

// Inspect talks to the inspector a running build from source carries (internal/devtools):
// the app's log and events, its live database, and its window's page, from the terminal,
// while the app runs. A target finds the app by its data directory: the user's own, or the
// one data= names (a sandbox's, as app:fresh and app:dev fresh=true make). json=true prints
// the answer's lines as JSON, as a script would read them.
type Inspect mg.Namespace

// Help lists what the inspector does.
//
//	magex inspect:help
func (Inspect) Help() error {
	return hostRunner().inspect("help", appdata.Resolve)
}

// Status says what runs: the build, the process and its Go runtime, where its data is,
// the window, the page, and how much each feed keeps.
//
//	magex inspect:status
func (Inspect) Status() error {
	return hostRunner().inspect("status", appdata.Resolve)
}

// Logs prints the app's log, every record from Debug up whatever the log file keeps: the
// last n (100), at level and up (debug, info, warn, error), holding grep; follow=true
// keeps printing each new one until interrupted.
//
//	magex inspect:logs n=50 level=warn grep=refresh follow=true
func (Inspect) Logs() error {
	return hostRunner().inspect("logs", appdata.Resolve, "n", "level", "grep", "follow")
}

// Events prints the events the services emitted, with what each carried, as logs prints
// the log.
//
//	magex inspect:events grep=inbox follow=true
func (Inspect) Events() error {
	return hostRunner().inspect("events", appdata.Resolve, "n", "grep", "follow")
}

// Console prints the page's console and every error nothing caught, as logs prints the
// log.
//
//	magex inspect:console level=error
func (Inspect) Console() error {
	return hostRunner().inspect("console", appdata.Resolve, "n", "level", "grep", "follow")
}

// Trail prints what the user did in the window, most recent last: each click, key,
// field typed in (never what was typed), and choice changed, and where, as logs prints
// the log.
//
//	magex inspect:trail n=30
func (Inspect) Trail() error {
	return hostRunner().inspect("trail", appdata.Resolve, "n", "grep", "follow")
}

// Notes prints the open notes left on the window's elements: ⌥⌘P in the window, its menu
// bar's Point at an Element, or inspect:point lets the user point at an element, click
// it, and say what is wrong with it. Each note says what the element is and where, its
// styles, and, under app:dev, the components that render it and its source file; its
// picture of the window, the element boxed, is saved as bin/inspect/note-<n>.png. The app
// keeps the notes across restarts. done=2,3 (or done=all) marks notes dealt with, and
// all=true shows those too; follow=true keeps printing each new note until interrupted;
// wait=true waits for the next one, prints it, and returns; clear=true empties the list.
//
//	magex inspect:notes
//	magex inspect:notes follow=true
//	magex inspect:notes done=3
func (Inspect) Notes() error {
	return hostRunner().inspect("notes", appdata.Resolve, "n", "all", "follow", "wait", "done", "clear")
}

// Point turns pointing on in the window, as ⌥⌘P does there: the user points at an
// element, clicks it, types a note, and presses Enter, which inspect:notes then reads.
// on=false turns it off. It refuses while the user has pointing turned off in Settings.
//
//	magex inspect:point
func (Inspect) Point() error {
	return hostRunner().inspect("point", appdata.Resolve, "on")
}

// Watch follows the log, the events, the console, and the trail together, as they
// happen, until interrupted: n= of each first, level= and grep= as logs reads them.
//
//	magex inspect:watch level=info
func (Inspect) Watch() error {
	return hostRunner().inspect("watch", appdata.Resolve, "n", "level", "grep")
}

// SQL runs one statement that reads against the live database, read-only, and prints at
// most max= rows (200).
//
//	magex inspect:sql q="SELECT key, value FROM prefs ORDER BY key"
func (Inspect) SQL() error {
	return hostRunner().inspect("sql", appdata.Resolve, "q", "max")
}

// Tables lists the database's tables, with their row counts.
//
//	magex inspect:tables
func (Inspect) Tables() error {
	return hostRunner().inspect("tables", appdata.Resolve)
}

// JS runs JavaScript in the window's page and prints what it gives back, once a promise
// it returns settles: code= is an expression, or statements that return; file= reads
// them from a file. nb holds the agent's helpers: nb.$ and nb.$$, nb.find, nb.describe,
// nb.dom, nb.text, nb.cards, nb.click, nb.type, nb.press, nb.scroll, nb.wait, and
// nb.waitFor. timeout= is in seconds (15).
//
//	magex inspect:js code="document.title"
//	magex inspect:js code="await nb.click('Snoozed'); return nb.cards()"
func (Inspect) JS() error {
	return hostRunner().inspect("js", appdata.Resolve, "code", "timeout")
}

// DOM describes the elements a selector matches as the web view lays them out: what each
// is, where, whether it is on screen and drawn, and, with styles=true and html=true, its
// computed styles and its HTML; at most limit= (50).
//
//	magex inspect:dom sel="li[data-item-id]" styles=true
func (Inspect) DOM() error {
	return hostRunner().inspect("dom", appdata.Resolve, "sel", "styles", "html", "limit")
}

// Cards describes the inbox's cards: each one's list, rule, and place, and whether the
// web view draws it and what is inside it.
//
//	magex inspect:cards
func (Inspect) Cards() error {
	return hostRunner().inspect("cards", appdata.Resolve)
}

// Click clicks a control, by selector or by the name it shows.
//
//	magex inspect:click target="Refresh"
func (Inspect) Click() error {
	return hostRunner().inspect("click", appdata.Resolve, "target")
}

// Shot saves a picture of the window as its web view draws it, as PNG: to out=, or to
// bin/inspect/shot-<time>.png.
//
//	magex inspect:shot
func (Inspect) Shot() error {
	return hostRunner().inspect("shot", appdata.Resolve)
}

// Stacks prints every goroutine's stack: grouped, or each in full with debug=2.
//
//	magex inspect:stacks
func (Inspect) Stacks() error {
	return hostRunner().inspect("stacks", appdata.Resolve, "debug")
}

// Reload reloads the window's page.
//
//	magex inspect:reload
func (Inspect) Reload() error {
	return hostRunner().inspect("reload", appdata.Resolve)
}

// inspect asks the inspector of the app whose data resolve finds (or data= names) for
// command, with the target's arguments keys, prints the answer, and saves a picture it
// gets. An interrupt ends a command that follows.
func (r runner) inspect(command string, resolve func() (appdata.Paths, error), keys ...string) error {
	socket, err := r.inspectSocket(resolve)
	if err != nil {
		return err
	}

	request, err := r.inspectRequest(command, keys)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	raw := r.mageArg("json") == "true"

	if err = devtools.Ask(ctx, socket, request, func(line devtools.Line) error { return r.printLine(line, raw) }); err != nil {
		return fmt.Errorf("inspect: %w", err)
	}

	return nil
}

// inspectSocket is where the inspector listens: in the data directory data= names, or
// else in the user's own, which resolve finds.
func (r runner) inspectSocket(resolve func() (appdata.Paths, error)) (string, error) {
	if dir := r.mageArg("data"); dir != "" {
		return devtools.SocketPath(dir), nil
	}

	paths, err := resolve()
	if err != nil {
		return "", fmt.Errorf("find the app's data: %w", err)
	}

	return devtools.SocketPath(paths.Root), nil
}

// inspectRequest is command, with each of keys the target was given. js reads its code
// from the file file= names when code= is not given.
func (r runner) inspectRequest(command string, keys []string) (devtools.Request, error) {
	request := devtools.Request{Command: command, Args: map[string]string{}}

	for _, key := range keys {
		if value := r.mageArg(key); value != "" {
			request.Args[key] = value
		}
	}

	if file := r.mageArg("file"); command == "js" && request.Args["code"] == "" && file != "" {
		code, err := os.ReadFile(r.path(file))
		if err != nil {
			return devtools.Request{}, fmt.Errorf("read the script: %w", err)
		}

		request.Args["code"] = string(code)
	}

	return request, nil
}

// printLine prints one line of the answer: as Render shows it, or as JSON with raw. A
// picture is saved instead, and a note's picture beside the note.
func (r runner) printLine(line devtools.Line, raw bool) error {
	switch line.Type {
	case devtools.LineShot:
		return r.saveShot(line)
	case devtools.LineNote:
		return r.printNote(line, raw)
	}

	return r.printValue(line, raw)
}

// printValue prints line as Render shows it, or as JSON with raw.
func (r runner) printValue(line devtools.Line, raw bool) error {
	if !raw {
		return devtools.Render(r.stdout, line)
	}

	data, err := json.Marshal(line)
	if err != nil {
		return fmt.Errorf("print the answer: %w", err)
	}

	_, err = fmt.Fprintln(r.stdout, string(data))

	return err
}

// saveShot saves the picture line holds where out= says, or in shotsDir, and says where.
func (r runner) saveShot(line devtools.Line) error {
	var shot devtools.Shot
	if err := json.Unmarshal(line.Value, &shot); err != nil {
		return fmt.Errorf("read the picture: %w", err)
	}

	path := r.mageArg("out")
	if path == "" {
		path = filepath.Join(shotsDir, "shot-"+time.Now().Format("20060102-150405.000")+".png")
	}

	abs, err := r.savePicture(path, shot.PNG)
	if err != nil {
		return err
	}

	r.say("Saved a picture of the window: " + abs)

	return nil
}

// printNote prints the note line holds, as printLine prints a line, without its picture,
// which it saves in shotsDir as note-<n>.png, and says where. The app boxed the note's
// element on the picture as it kept it.
func (r runner) printNote(line devtools.Line, raw bool) error {
	var note devtools.Note
	if err := json.Unmarshal(line.Value, &note); err != nil {
		return fmt.Errorf("read the note: %w", err)
	}

	picture := note.Shot
	note.Shot = nil

	value, err := json.Marshal(note)
	if err != nil {
		return fmt.Errorf("print the note: %w", err)
	}

	if err = r.printValue(devtools.Line{Type: devtools.LineNote, Value: value}, raw); err != nil {
		return err
	}

	if len(picture) == 0 {
		return nil
	}

	abs, err := r.savePicture(filepath.Join(shotsDir, "note-"+strconv.Itoa(note.N)+".png"), picture)
	if err != nil {
		return err
	}

	r.say("  picture    " + abs)

	return nil
}

// savePicture writes the PNG picture to path, under the checkout unless it is absolute,
// and returns where, absolute.
func (r runner) savePicture(path string, picture []byte) (string, error) {
	path = r.path(path)

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", fmt.Errorf("make the picture's folder: %w", err)
	}

	if err := os.WriteFile(path, picture, 0o600); err != nil {
		return "", fmt.Errorf("save the picture: %w", err)
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}

	return abs, nil
}
