//go:build darwin

package launcher

import (
	"context"
	"time"

	"github.com/mrz1836/nilbox/internal/settings"
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
	executor Executor
}

// ID returns the iTerm2 terminal setting.
func (itermDriver) ID() string { return settings.TerminalITerm2 }

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
	executor Executor
}

// ID returns the Terminal.app terminal setting.
func (terminalAppDriver) ID() string { return settings.TerminalApp }

// Open runs the script in a new Terminal.app window.
func (d terminalAppDriver) Open(ctx context.Context, scriptPath string) error {
	if err := validateScriptPath(scriptPath); err != nil {
		return err
	}

	return runTerminal(ctx, d.executor, appTerminal, openBin, terminalAppArgs(scriptPath)...)
}

// runTerminal runs one terminal program with openTimeout and explains a failure.
func runTerminal(ctx context.Context, executor Executor, app, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, openTimeout)
	defer cancel()

	if _, stderr, err := executor.Run(ctx, name, args...); err != nil {
		return openError(app, stderr, err)
	}

	return nil
}
