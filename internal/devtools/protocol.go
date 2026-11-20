package devtools

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// SocketName is the inspector's socket's name in the app's data directory.
const SocketName = "devtools.sock"

// The types of the lines of an answer.
const (
	// LineLog holds a LogEntry, LineEvent an EventEntry, LineConsole a ConsoleEntry,
	// LineTrail a TrailEntry, and LineNote a Note.
	LineLog     = "log"
	LineEvent   = "event"
	LineConsole = "console"
	LineTrail   = "trail"
	LineNote    = "note"

	// LineResult holds any JSON value: what a script gave back, or what a command found.
	LineResult = "result"

	// LineTable holds a query's store.QueryResult, LineText a string to print as it is,
	// and LineShot a Shot.
	LineTable = "table"
	LineText  = "text"
	LineShot  = "shot"

	// LineError ends an answer that failed, holding why as a string; LineEnd ends one
	// that did not.
	LineError = "error"
	LineEnd   = "end"
)

// ErrMissingArg is returned for a command asked without an argument it needs.
var ErrMissingArg = errors.New("missing argument")

// ErrUnknownCommand is returned for a command the inspector doesn't have.
var ErrUnknownCommand = errors.New("no such command; ask for help")

// ErrBadNoteList is returned for done= that names no notes: it takes note numbers, as 2,3,
// or all.
var ErrBadNoteList = errors.New("done= takes note numbers, as 2,3, or all")

// Request is what a client asks the inspector: the first line a connection carries.
type Request struct {
	Command string            `json:"cmd"`
	Args    map[string]string `json:"args,omitempty"`
}

// Line is one line of the inspector's answer: Type says what Value holds.
type Line struct {
	Type  string          `json:"t"`
	Value json.RawMessage `json:"v,omitempty"`
}

// Shot is a picture of the window as the web view draws it.
type Shot struct {
	PNG []byte `json:"png"`
}

// WindowInfo is the app's window as the inspector describes it: whether it has opened,
// whether it shows and has focus, and where it is on the screen, in points.
type WindowInfo struct {
	Open    bool `json:"open"`
	Visible bool `json:"visible"`
	Focused bool `json:"focused"`
	X       int  `json:"x"`
	Y       int  `json:"y"`
	Width   int  `json:"width"`
	Height  int  `json:"height"`
}

// About describes the running app: its build, the process, where its data is, and its
// window.
type About struct {
	Version  string     `json:"version"`
	Commit   string     `json:"commit"`
	Flavor   string     `json:"flavor"`
	PID      int        `json:"pid"`
	Started  time.Time  `json:"started"`
	Data     string     `json:"data"`
	Database string     `json:"database"`
	Logs     string     `json:"logs"`
	Socket   string     `json:"socket"`
	Window   WindowInfo `json:"window"`
}

// Status is what the status command answers: the app as About describes it, how long it
// has run, its Go runtime's numbers, the page, and how many entries each feed keeps.
type Status struct {
	About

	Uptime     string         `json:"uptime"`
	Go         string         `json:"go"`
	Goroutines int            `json:"goroutines"`
	HeapMB     float64        `json:"heapMB"`
	SysMB      float64        `json:"sysMB"`
	GCs        uint32         `json:"gcs"`
	Page       PageInfo       `json:"page"`
	Kept       map[string]int `json:"kept"`
}

// CommandInfo describes one command, as help lists it.
type CommandInfo struct {
	Command string `json:"cmd"`
	Args    string `json:"args,omitempty"`
	About   string `json:"about"`
}

// SocketPath returns where the inspector of the app whose data directory is dataDir
// listens.
func SocketPath(dataDir string) string {
	return filepath.Join(dataDir, SocketName)
}

// args are a request's arguments, each a string as the magex targets pass them.
type args map[string]string

// text returns the argument key, or "".
func (a args) text(key string) string {
	return strings.TrimSpace(a[key])
}

// need returns the argument key, or ErrMissingArg when it is empty.
func (a args) need(key string) (string, error) {
	value := a.text(key)
	if value == "" {
		return "", fmt.Errorf("%w: %s", ErrMissingArg, key)
	}

	return value, nil
}

// number returns the argument key as a whole number, or fallback when it is empty or
// not a number.
func (a args) number(key string, fallback int) int {
	n, err := strconv.Atoi(a.text(key))
	if err != nil {
		return fallback
	}

	return n
}

// flag reports whether the argument key is a true value: true, 1, yes, or on.
func (a args) flag(key string) bool {
	switch strings.ToLower(a.text(key)) {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
	}
}

// when stamps an entry of a feed, so a stream of several feeds keeps their entries in
// time order.
func (e LogEntry) when() time.Time { return e.Time }

func (e EventEntry) when() time.Time { return e.Time }

func (e ConsoleEntry) when() time.Time { return e.Time }

func (e TrailEntry) when() time.Time { return e.Time }

func (n Note) when() time.Time { return n.Time }
