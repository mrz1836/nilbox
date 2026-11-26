package core

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"strings"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/launcher"
	"github.com/mrz1836/nilbox/internal/launcher/terminal"
	"github.com/mrz1836/nilbox/internal/notify"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// errNoCommand is returned for a run in the terminal with no command to run.
var errNoCommand = errors.New("the run has no command")

// terminalRuns opens the runs that go in the user's terminal: it writes each one's script
// into the launch folder, as a launch writes its own, recorded so the app follows it, and
// opens it in the terminal chosen in Settings → Launching (scriptrun.TerminalOpener),
// reporting which. It opens that terminal in a folder too, for every page's Open in
// terminal, and brings a run's tab forward again, for its Show in terminal
// (scriptrun.TabFocuser).
type terminalRuns struct {
	launch string
	opener *launcher.CommandOpener
	clock  clock.Clock
	logger *slog.Logger

	// shell is the user's login shell, which the script that opens a terminal in a folder
	// starts, for a terminal that can't open a folder itself.
	shell func() string
}

// OpenRun writes run's script and opens it, and returns the settings ID of the terminal it
// opened in, which the run records.
func (t terminalRuns) OpenRun(ctx context.Context, run scriptrun.TerminalRun) (string, error) {
	if len(run.Argv) == 0 {
		return "", errNoCommand
	}

	path, err := terminal.WriteScript(t.launch, run.Name, terminal.Script{
		Dir: run.Dir, Binary: run.Argv[0], Args: run.Argv[1:], Env: envMap(run.Env), NoPrompt: true,
		Record: &terminal.Recording{
			Log: run.Log, PIDFile: run.PIDFile, ExitFile: run.ExitFile, LinkFile: run.LinkFile,
		},
	}, clock.Or(t.clock).Now(), nil, t.logger)
	if err != nil {
		return "", fmt.Errorf("write the run's script: %w", err)
	}

	id, err := t.opener.Open(ctx, path)

	return string(id), err
}

// FocusRun brings forward the tab a run in the terminal goes in, in the terminal it
// recorded it opened in, or the one chosen now for a run that recorded none, and words
// what the terminal says for the window: no tab on the run's tty, no Warp focus link to
// it (a run started before its script recorded the link, or in a Warp older than
// v0.2026.05.27), and a terminal that can't show a tab.
func (t terminalRuns) FocusRun(ctx context.Context, tab scriptrun.TerminalTab) error {
	err := t.opener.Focus(ctx, terminal.ID(tab.Terminal), terminal.Tab{TTY: tab.TTY, PID: tab.PID, Link: tab.Link})

	switch {
	case errors.Is(err, terminal.ErrNoSuchTab):
		return uierr.New("Couldn't find the run's terminal tab. It may have been closed, or moved to another app.", err)
	case errors.Is(err, terminal.ErrNoFocusLink):
		return uierr.New("Couldn't tell which Warp tab runs this. Runs started from this version of "+brand.Name+
			" on, in Warp v0.2026.05.27 or newer, can be shown.", err)
	case errors.Is(err, terminal.ErrNoFocus), errors.Is(err, terminal.ErrUnknownTerminal),
		errors.Is(err, terminal.ErrUnsupportedOS):
		return uierr.New("This terminal can't show a run's tab.", err)
	default:
		return err
	}
}

// OpenFolder opens the terminal in dir, for Open in terminal, and runs nothing there. A
// terminal that opens a folder itself, as Warp does, opens a tab whose own shell starts in
// dir, so the tab is the terminal's own (Warp's, with its blocks), and no script is
// written. Any other gets a script named name that changes into dir and starts the user's
// login shell there, and runs nothing else.
func (t terminalRuns) OpenFolder(ctx context.Context, name, dir string) error {
	if opened, err := t.opener.OpenFolder(ctx, dir); err != nil || opened {
		return err
	}

	path, err := terminal.WriteScript(t.launch, name, terminal.Script{
		Purpose: "to open a terminal in a run's folder", Dir: dir, Binary: t.shell(), Args: []string{"-l"},
		NoPrompt: true,
	}, clock.Or(t.clock).Now(), nil, t.logger)
	if err != nil {
		return fmt.Errorf("write the terminal's script: %w", err)
	}

	_, err = t.opener.Open(ctx, path)

	return err
}

// finderReveal shows a file in Finder with open -R (scriptrun.Revealer). Core wires it
// only on macOS.
type finderReveal struct {
	runner execpath.Runner
}

// Reveal shows path in Finder.
func (f finderReveal) Reveal(ctx context.Context, path string) error {
	if _, stderr, err := f.runner.Run(ctx, "/usr/bin/open", "-R", path); err != nil {
		reason := cmp.Or(execpath.FirstLine(stderr), err.Error())

		return uierr.New("Finder couldn't show the file: "+reason, err)
	}

	return nil
}

// revealer is finderReveal over runner on macOS, and nothing elsewhere, where there is
// no Finder: Show the log file then says so.
func revealer(runner execpath.Runner) scriptrun.Revealer {
	if runtime.GOOS != "darwin" {
		return nil
	}

	return finderReveal{runner: runner}
}

// envMap turns NAME=value entries into a map; an entry without "=" sets the name empty.
func envMap(env []string) map[string]string {
	out := make(map[string]string, len(env))

	for _, kv := range env {
		name, value, _ := strings.Cut(kv, "=")
		out[name] = value
	}

	return out
}

// runFailures tells the user of a run that failed with a notification about no item,
// run_failed, which names the run a click on it opens and when it started, and its banner
// when that kind is on, gathered with those of the failures that follow it closely, so a
// burst shows as one (scriptrun's Config.Failed).
type runFailures struct {
	dispatcher *notify.Dispatcher
}

// Failed records the notice at once and gathers its banner.
func (n runFailures) Failed(ctx context.Context, f scriptrun.Failure) {
	n.dispatcher.Gather(ctx, notify.Entry{
		Kind: notify.KindRunFailed, Title: f.Title, Body: f.Body, RepoOwner: f.Repo.Owner, RepoName: f.Repo.Repo,
		RunID: f.ID, RunStarted: clock.Format(f.Started),
	})
}
