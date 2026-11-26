package terminal

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/mrz1836/nilbox/internal/procs"
)

const (
	// appWarp names Warp in error messages.
	appWarp = "Warp"

	// warpBundleID is Warp Stable's bundle identifier. The driver opens Warp by it, never
	// by name, so neither Warp Preview nor another app called Warp can answer.
	warpBundleID = "dev.warp.Warp-Stable"

	// warpFocusEnv is the environment variable Warp (v0.2026.05.27 and newer) exports in
	// every shell it starts: the link that brings that shell's tab forward. A launch's
	// agent inherits it from the tab's shell.
	warpFocusEnv = "WARP_FOCUS_URL"
)

// ErrNoFocusLink is returned when a tab has no Warp focus link to open: a followed run's
// script recorded none, and its process carries none either, as for a Warp older than
// v0.2026.05.27, a session or a run not started in Warp, a run whose script was written
// before scripts recorded the link, or a process whose environment can't be read, as a
// shell's, which macOS hides.
var ErrNoFocusLink = errors.New("the tab carries no Warp focus link")

// warpFocusPattern is what a Warp focus link is, exactly: a session link, its pane's ID in
// 32 lowercase hex digits, and nothing else. Any other warp:// link, such as an action
// that opens a tab, is refused.
var warpFocusPattern = regexp.MustCompile(`^warp://session/[0-9a-f]{32}$`)

// warpFocusLink returns value, read from a process's WARP_FOCUS_URL, when it is a Warp
// focus link, or ErrNoFocusLink. The error never repeats a value that isn't one, since
// another program could have set the variable to anything. It is pure: opening the link
// is the darwin driver's job.
func warpFocusLink(value string) (string, error) {
	if !warpFocusPattern.MatchString(value) {
		return "", fmt.Errorf("%w: %s is not a warp://session/ link", ErrNoFocusLink, warpFocusEnv)
	}

	return value, nil
}

// warpTabLink returns the focus link that brings tab forward in Warp: the link its run's
// script recorded (Tab.Link), when that is exactly a focus link, or else the one env reads
// from its process's environment, WARP_FOCUS_URL and nothing else, as a session's agent
// inherits it from its tab. A link that isn't one, recorded or read, is never repeated in
// the error. It is pure but for env: opening the link is the darwin driver's job.
func warpTabLink(tab Tab, env func(pid int, name string) (string, bool)) (string, error) {
	if link, err := warpFocusLink(tab.Link); err == nil {
		return link, nil
	}

	if !procs.ValidPID(tab.PID) {
		return "", fmt.Errorf("%w: no process %d to read it from", ErrNoFocusLink, tab.PID)
	}

	value, ok := env(tab.PID, warpFocusEnv)
	if !ok {
		return "", fmt.Errorf("%w: process %d has no %s", ErrNoFocusLink, tab.PID, warpFocusEnv)
	}

	return warpFocusLink(value)
}

// warpOpenArgs returns the `open` arguments that run a launch script in Warp. Warp runs
// an executable .command file it is asked to open in a new tab of its frontmost window,
// or a new window when none is open, typing the script's path into a shell of its own;
// it needs no Automation permission. Before Warp's first-run welcome is finished, Warp
// drops the request while open still succeeds. It is pure: running it is the darwin
// driver's job.
func warpOpenArgs(scriptPath string) []string {
	return []string{"-b", warpBundleID, scriptPath}
}

// warpFocusArgs returns the `open` arguments that open a focus link in Warp: Warp brings
// forward the window of the link's tab and selects its pane, and needs no Automation
// permission. A link whose tab was closed only brings Warp forward, and open still
// succeeds: neither Warp nor the driver can tell. It is pure: running it is the darwin
// driver's job.
func warpFocusArgs(link string) []string {
	return []string{"-b", warpBundleID, link}
}
