//go:build darwin

package terminal

import (
	"context"
	"fmt"
	"time"

	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/procs"
)

const (
	// osascriptBin runs AppleScript; it drives iTerm2.
	osascriptBin = "osascript"

	// openBin is macOS's open command; it hands scripts to Terminal.app and Warp.
	openBin = "open"

	// openTimeout bounds how long a terminal app may take to accept a launch, so a stuck
	// permission dialog or an unresponsive app cannot hang the launch.
	openTimeout = 20 * time.Second
)

// itermDriver opens launch scripts in iTerm2 by running AppleScript with osascript. The
// first launch makes macOS ask the user to allow the app to control iTerm2.
type itermDriver struct {
	executor execpath.Runner
}

// ID returns the iTerm2 terminal setting.
func (itermDriver) ID() ID { return ITerm2 }

// Open runs the script in a new iTerm2 tab, or a new window when none is open.
func (d itermDriver) Open(ctx context.Context, scriptPath string) error {
	if err := validateScriptPath(scriptPath); err != nil {
		return err
	}

	return runTerminal(ctx, d.executor, appITerm2, osascriptBin, "-e", itermAppleScript(scriptPath))
}

// terminalAppDriver opens launch scripts in a new Terminal.app window with
// `open -a Terminal`, which needs no Automation permission.
type terminalAppDriver struct {
	executor execpath.Runner
}

// ID returns the Terminal.app terminal setting.
func (terminalAppDriver) ID() ID { return TerminalApp }

// Open runs the script in a new Terminal.app window.
func (d terminalAppDriver) Open(ctx context.Context, scriptPath string) error {
	if err := validateScriptPath(scriptPath); err != nil {
		return err
	}

	return runTerminal(ctx, d.executor, appTerminal, openBin, terminalAppArgs(scriptPath)...)
}

// warpDriver opens launch scripts in Warp with `open -b <Warp's bundle ID>`, which needs
// no Automation permission, and brings a session's tab forward by the focus link its
// agent's process inherited.
type warpDriver struct {
	executor execpath.Runner

	// env reads one environment variable of a running process, as procs.EnvOf does.
	env func(pid int, name string) (string, bool)
}

// ID returns the Warp terminal setting.
func (warpDriver) ID() ID { return Warp }

// Open runs the script in a new tab of Warp's frontmost window, or a new window when none
// is open.
func (d warpDriver) Open(ctx context.Context, scriptPath string) error {
	if err := validateScriptPath(scriptPath); err != nil {
		return err
	}

	return runTerminal(ctx, d.executor, appWarp, openBin, warpOpenArgs(scriptPath)...)
}

// runTerminal runs one terminal program with openTimeout and explains a failure.
func runTerminal(ctx context.Context, executor execpath.Runner, app, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, openTimeout)
	defer cancel()

	if _, stderr, err := executor.Run(ctx, name, args...); err != nil {
		return openError(app, stderr, err)
	}

	return nil
}

// Focus brings forward the iTerm2 session that runs on the tab's tty, with its tab and
// window.
func (d itermDriver) Focus(ctx context.Context, tab Tab) error {
	return focus(ctx, d.executor, appITerm2, tab.TTY, itermFocusScript)
}

// Focus brings forward the Terminal.app tab that runs on the tab's tty, with its window.
// The first time, macOS asks the user to allow the app to control Terminal.app.
func (d terminalAppDriver) Focus(ctx context.Context, tab Tab) error {
	return focus(ctx, d.executor, appTerminal, tab.TTY, terminalAppFocusScript)
}

// Focus brings forward the Warp tab the session's agent runs in, by the focus link Warp
// exported in that tab's shell: it reads WARP_FOCUS_URL, that one variable alone, from
// the agent's process, checks it is a focus link, and opens it in Warp. Warp has no tty
// lookup and asks for no permission. A process without a link, or whose environment can't
// be read, is ErrNoFocusLink; a link whose tab was closed only brings Warp forward.
func (d warpDriver) Focus(ctx context.Context, tab Tab) error {
	if !procs.ValidPID(tab.PID) {
		return fmt.Errorf("%w: no process %d to read it from", ErrNoFocusLink, tab.PID)
	}

	value, ok := d.env(tab.PID, warpFocusEnv)
	if !ok {
		return fmt.Errorf("%w: process %d has no %s", ErrNoFocusLink, tab.PID, warpFocusEnv)
	}

	link, err := warpFocusLink(value)
	if err != nil {
		return err
	}

	return runTerminal(ctx, d.executor, appWarp, openBin, warpFocusArgs(link)...)
}

// focus runs a terminal's focus script for tty with openTimeout, and explains a failure.
func focus(ctx context.Context, executor execpath.Runner, app, tty string, script func(string) string) error {
	if err := validateTTY(tty); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, openTimeout)
	defer cancel()

	stdout, stderr, err := executor.Run(ctx, osascriptBin, "-e", script(tty))
	if err != nil {
		return openError(app, stderr, err)
	}

	return focusResult(app, stdout)
}
