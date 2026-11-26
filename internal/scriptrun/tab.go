package scriptrun

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// psBin is ps's fixed path on macOS and Linux; it names the terminal a process runs on.
const psBin = "/bin/ps"

// lostTab is what Show in terminal says of a run whose shell is gone, or runs on no
// terminal.
const lostTab = "Couldn't find the run's terminal tab. The run may have just ended."

// showable returns why Show in terminal can't bring rec's tab forward, worded for the
// window, or nil: a run in the app (ErrNotInTerminal), one that has ended
// (ErrNotRunning), nothing to show a tab with (ErrNoTerminal), and a run whose shell isn't
// recorded yet or is gone (ErrNoTab), told by the shell's recorded start time, so a PID
// since taken by another process never counts.
func (e *Engine) showable(rec Record) error {
	// A run from before a restart that had ended by then is only being settled. One still
	// recorded as going that the engine doesn't follow (yet, or any more) is looked for all
	// the same: its shell, when it is gone, says the run has just ended.
	l := e.goingRun(rec.ID)

	switch {
	case rec.RunsIn != RunsInTerminal:
		return uierr.New("This run goes in "+brand.Name+", not in your terminal.", ErrNotInTerminal)
	case !rec.Status.Live() || (l != nil && l.settling):
		return uierr.New("The run has ended.", ErrNotRunning)
	case e.tabs == nil || e.ps == nil:
		return uierr.New("Showing a run's terminal tab isn't supported here.", ErrNoTerminal)
	case rec.PID <= 0:
		return uierr.New("The run's terminal tab isn't known yet. Try again once the run has started.", ErrNoTab)
	case !e.processAlive(rec.PID, rec.ProcStartedAt):
		return uierr.New(lostTab, fmt.Errorf("%w: its shell, process %d, is gone", ErrNoTab, rec.PID))
	default:
		return nil
	}
}

// shellTTY returns the terminal device the process pid runs on, such as /dev/ttys004,
// asking ps through run, as the sessions' Show in terminal does for an agent: the shell
// that runs a run's script in the terminal runs on its tab's device. A process with no
// terminal, or none at all, is ErrNoTab.
func shellTTY(ctx context.Context, run execpath.Runner, pid int) (string, error) {
	out, _, err := run.Run(ctx, psBin, "-o", "tty=", "-p", strconv.Itoa(pid))
	if err != nil {
		return "", fmt.Errorf("%w: ps: %w", ErrNoTab, err)
	}

	name := strings.TrimSpace(execpath.FirstLine(out))
	if name == "" || strings.Trim(name, "?") == "" || strings.ContainsAny(name, " \t") {
		return "", fmt.Errorf("%w: process %d runs on no terminal", ErrNoTab, pid)
	}

	return "/dev/" + strings.TrimPrefix(name, "/dev/"), nil
}
