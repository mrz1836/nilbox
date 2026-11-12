package core

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"strings"

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
// opens it in the terminal chosen in Settings → Launching (scriptrun.TerminalOpener).
type terminalRuns struct {
	launch string
	opener *launcher.CommandOpener
	clock  clock.Clock
	logger *slog.Logger

	// shell is the user's login shell, which a terminal opened in a folder starts.
	shell func() string
}

// OpenRun writes run's script and opens it.
func (t terminalRuns) OpenRun(ctx context.Context, run scriptrun.TerminalRun) error {
	if len(run.Argv) == 0 {
		return errNoCommand
	}

	path, err := terminal.WriteScript(t.launch, run.Name, terminal.Script{
		Dir: run.Dir, Binary: run.Argv[0], Args: run.Argv[1:], Env: envMap(run.Env), NoPrompt: true,
		Record: &terminal.Recording{Log: run.Log, PIDFile: run.PIDFile, ExitFile: run.ExitFile},
	}, clock.Or(t.clock).Now(), nil, t.logger)
	if err != nil {
		return fmt.Errorf("write the run's script: %w", err)
	}

	return t.opener.Open(ctx, path)
}

// OpenFolder opens the terminal in dir, for Open in terminal: a script that changes into
// dir and starts the user's login shell there, and runs nothing else.
func (t terminalRuns) OpenFolder(ctx context.Context, name, dir string) error {
	path, err := terminal.WriteScript(t.launch, name, terminal.Script{
		Purpose: "to open a terminal in a run's folder", Dir: dir, Binary: t.shell(), Args: []string{"-l"},
		NoPrompt: true,
	}, clock.Or(t.clock).Now(), nil, t.logger)
	if err != nil {
		return fmt.Errorf("write the terminal's script: %w", err)
	}

	return t.opener.Open(ctx, path)
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
