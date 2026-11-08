//go:build darwin

package terminal

import (
	"context"
	"time"

	"github.com/mrz1836/nilbox/internal/execpath"
)

const (
	// osascriptBin runs AppleScript; it drives iTerm2.
	osascriptBin = "osascript"

	// openBin is macOS's open command; it hands scripts to Terminal.app.
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

// runTerminal runs one terminal program with openTimeout and explains a failure.
func runTerminal(ctx context.Context, executor execpath.Runner, app, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, openTimeout)
	defer cancel()

	if _, stderr, err := executor.Run(ctx, name, args...); err != nil {
		return openError(app, stderr, err)
	}

	return nil
}
