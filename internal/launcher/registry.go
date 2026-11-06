package launcher

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
)

// maxErrDetail caps how much of a terminal program's error output an error message carries.
const maxErrDetail = 200

// automationDeniedCode is the AppleScript error macOS reports when the user has not
// allowed the app to control another app (errAEEventNotPermitted).
const automationDeniedCode = "-1743"

var (
	// ErrUnsupportedOS is returned when no terminal driver exists for this OS.
	ErrUnsupportedOS = errors.New("launching a terminal is not supported on this OS yet")

	// ErrUnknownTerminal is returned for a terminal ID no driver is registered under.
	ErrUnknownTerminal = errors.New("unknown terminal")

	// ErrInvalidScriptPath is returned for a launch script path a terminal cannot be
	// handed safely: relative, or containing control characters.
	ErrInvalidScriptPath = errors.New("invalid launch script path")

	// ErrAutomationDenied is returned when macOS refused to let the app control the
	// terminal app. The user allows it once in System Settings.
	ErrAutomationDenied = errors.New("macOS did not allow controlling the terminal app; " +
		"allow it in System Settings → Privacy & Security → Automation, then launch again")

	// ErrTerminalOpen is returned when the terminal app could not be told to run the script.
	ErrTerminalOpen = errors.New("could not open the terminal")
)

// Executor runs a program as an argv, never through a shell, and returns what it wrote.
// execpath.Runner is the implementation used at run time.
type Executor interface {
	Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error)
}

// Driver opens a launch script in one terminal app.
type Driver interface {
	// ID is the terminal's settings value, such as "iterm2".
	ID() string

	// Open runs the script at scriptPath in a new tab or window of the terminal.
	Open(ctx context.Context, scriptPath string) error
}

// Registry holds the terminal drivers available on this OS, by ID. A port to another OS
// registers its own drivers; callers only ever ask the registry.
type Registry struct {
	drivers map[string]Driver
}

// NewRegistry returns a registry of drivers. A later driver with the same ID replaces an
// earlier one.
func NewRegistry(drivers ...Driver) Registry {
	r := Registry{drivers: make(map[string]Driver, len(drivers))}
	for _, d := range drivers {
		r.drivers[d.ID()] = d
	}

	return r
}

// Get returns the driver registered under id. An empty registry (an OS with no terminal
// support yet) returns ErrUnsupportedOS; an unknown ID returns ErrUnknownTerminal.
func (r Registry) Get(id string) (Driver, error) {
	if len(r.drivers) == 0 {
		return nil, ErrUnsupportedOS
	}

	d, ok := r.drivers[id]
	if !ok {
		return nil, fmt.Errorf("%w %q: choose one of %s", ErrUnknownTerminal, id, strings.Join(r.IDs(), ", "))
	}

	return d, nil
}

// IDs returns the registered terminal IDs in sorted order.
func (r Registry) IDs() []string {
	return slices.Sorted(maps.Keys(r.drivers))
}

// validateScriptPath checks that a script path is absolute and holds no control
// characters, so typing or passing it to a terminal can never run anything else.
func validateScriptPath(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%w: %q is not absolute", ErrInvalidScriptPath, path)
	}

	if strings.IndexFunc(path, unicode.IsControl) >= 0 {
		return fmt.Errorf("%w: %q contains a control character", ErrInvalidScriptPath, path)
	}

	return nil
}

// openError explains a failed attempt to open a terminal app. macOS's Automation refusal
// becomes ErrAutomationDenied; anything else is ErrTerminalOpen with the first line of
// the program's error output.
func openError(app string, stderr []byte, err error) error {
	detail := firstLine(string(stderr))
	if strings.Contains(detail, automationDeniedCode) {
		return fmt.Errorf("%w (%s)", ErrAutomationDenied, app)
	}

	if detail == "" {
		return fmt.Errorf("%w %s: %w", ErrTerminalOpen, app, err)
	}

	return fmt.Errorf("%w %s: %s: %w", ErrTerminalOpen, app, detail, err)
}

// firstLine returns the first non-blank line of s, trimmed and capped at maxErrDetail runes.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return truncateRunes(line, maxErrDetail)
		}
	}

	return ""
}

// truncateRunes caps s at limit runes.
func truncateRunes(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}

	return strings.TrimSpace(string(runes[:limit]))
}
