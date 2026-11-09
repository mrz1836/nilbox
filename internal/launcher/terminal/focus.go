package terminal

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// focusFound and focusMissing are what a focus script prints: whether it found the tab.
const (
	focusFound   = "found"
	focusMissing = "missing"
)

var (
	// ErrNoSuchTab is returned when no tab of the terminal runs on the tty asked for: the
	// session ended, or its tab was moved to another terminal.
	ErrNoSuchTab = errors.New("no tab of the terminal runs that session")

	// ErrInvalidTTY is returned for a terminal device that is not one.
	ErrInvalidTTY = errors.New("invalid terminal device")

	// ErrNoFocus is returned for a terminal whose driver cannot bring a tab forward.
	ErrNoFocus = errors.New("the terminal can't show a session's tab")
)

// ttyPattern is what a terminal device path looks like: /dev/ttys004, /dev/pts/3.
var ttyPattern = regexp.MustCompile(`^/dev/[A-Za-z0-9]+(/[0-9]+)?$`)

// Focuser is a Driver that can also bring forward the tab a session runs in.
type Focuser interface {
	Driver

	// Focus selects the tab whose terminal device is tty, such as /dev/ttys004, brings
	// its window forward, and activates the terminal. It returns ErrNoSuchTab when no tab
	// runs on tty.
	Focus(ctx context.Context, tty string) error
}

// Focus brings forward the tab that runs on tty in the terminal registered under id. It
// returns ErrNoFocus for a terminal whose driver cannot.
func (r Registry) Focus(ctx context.Context, id ID, tty string) error {
	d, err := r.Get(id)
	if err != nil {
		return err
	}

	f, ok := d.(Focuser)
	if !ok {
		return fmt.Errorf("%w: %s", ErrNoFocus, id)
	}

	return f.Focus(ctx, tty)
}

// validateTTY checks that tty is a terminal device path, so it can be written into a
// script safely.
func validateTTY(tty string) error {
	if !ttyPattern.MatchString(tty) {
		return fmt.Errorf("%w: %q", ErrInvalidTTY, tty)
	}

	return nil
}

// focusResult reads what a focus script printed.
func focusResult(app string, stdout []byte) error {
	if strings.TrimSpace(string(stdout)) == focusFound {
		return nil
	}

	return fmt.Errorf("%w (%s)", ErrNoSuchTab, app)
}

// itermFocusScript returns the AppleScript that brings forward the iTerm2 session whose
// tty is tty, with its tab and window, and prints whether it found one. It is pure:
// running it is the darwin driver's job.
func itermFocusScript(tty string) string {
	return `tell application "iTerm"
	repeat with w in windows
		repeat with t in tabs of w
			repeat with s in sessions of t
				if tty of s is "` + appleScriptString(tty) + `" then
					select w
					tell t to select
					tell s to select
					activate
					return "` + focusFound + `"
				end if
			end repeat
		end repeat
	end repeat
end tell
return "` + focusMissing + `"
`
}

// terminalAppFocusScript returns the AppleScript that brings forward the Terminal.app tab
// whose tty is tty, with its window, and prints whether it found one. Terminal.app asks
// the user, the first time, to allow the app to control it. It is pure: running it is the
// darwin driver's job.
func terminalAppFocusScript(tty string) string {
	return `tell application "Terminal"
	repeat with w in windows
		repeat with t in tabs of w
			if tty of t is "` + appleScriptString(tty) + `" then
				set selected of t to true
				set index of w to 1
				activate
				return "` + focusFound + `"
			end if
		end repeat
	end repeat
end tell
return "` + focusMissing + `"
`
}
