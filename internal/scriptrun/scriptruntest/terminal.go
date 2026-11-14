package scriptruntest

import (
	"context"
	"errors"
	"os"
	"slices"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// terminalStop is how a shell still going as its test ends is stopped.
func terminalStop() execpath.Ladder {
	return execpath.Ladder{
		{Signal: syscall.SIGINT, Wait: 200 * time.Millisecond},
		{Signal: syscall.SIGTERM, Wait: 200 * time.Millisecond},
		{Signal: syscall.SIGKILL},
	}
}

// Terminal stands in for the user's terminal app, for the runs a Harness's engine opens
// there (WithTerminal): it starts each run's command with /bin/sh running
// testutil.TerminalScript, with no window, and records each run it was asked to open.
// Close closes it as a user closes the terminal's tab mid-run. A shell still going as the
// test ends is stopped before the folders its run writes into are removed.
type Terminal struct {
	// Env is the environment the terminal's shell starts with, beside the test's own and
	// what each run exports: the user's environment as a login shell would read it, such
	// as a fake tool's entry. Set it before the first run opens.
	Env []string

	t      testing.TB
	mu     sync.Mutex
	opened []scriptrun.TerminalRun
	procs  []*execpath.Proc
}

// NewTerminal returns a Terminal for the test t.
func NewTerminal(t testing.TB) *Terminal {
	t.Helper()

	return &Terminal{t: t}
}

// OpenRun starts the run's command in a shell that outlives the call, as a terminal's
// does.
func (term *Terminal) OpenRun(ctx context.Context, run scriptrun.TerminalRun) error {
	term.mu.Lock()
	defer term.mu.Unlock()

	term.opened = append(term.opened, run)

	env := slices.Concat(os.Environ(), term.Env, run.Env,
		[]string{"PID_FILE=" + run.PIDFile, "LOG_FILE=" + run.Log, "EXIT_FILE=" + run.ExitFile})

	proc, err := execpath.Start(context.WithoutCancel(ctx), execpath.Cmd{
		Path: "/bin/sh", Args: append([]string{"-c", testutil.TerminalScript, "sh"}, run.Argv...), Dir: run.Dir, Env: env,
	})
	if err != nil {
		return err
	}

	term.procs = append(term.procs, proc)

	// Registered now, after the folders the run writes into, so it runs before they go.
	term.t.Cleanup(func() {
		proc.Stop(terminalStop())
		_, _ = proc.Wait()
	})

	return nil
}

// OpenFolder opens nothing: no test of a run asks for the terminal in its folder.
func (*Terminal) OpenFolder(context.Context, string, string) error {
	return nil
}

// Opened returns the runs it was asked to open, in order.
func (term *Terminal) Opened() []scriptrun.TerminalRun {
	term.mu.Lock()
	defer term.mu.Unlock()

	return slices.Clone(term.opened)
}

// Close closes the terminal as a user closes its tab while a run goes on: every shell's
// process group is hung up (SIGHUP), so the command is cut off where it is and the shell
// ends without writing the exit status. It returns once every shell has ended.
func (term *Terminal) Close() error {
	term.mu.Lock()
	procs := slices.Clone(term.procs)
	term.mu.Unlock()

	var errs []error

	for _, p := range procs {
		if err := p.Signal(syscall.SIGHUP); err != nil && !errors.Is(err, syscall.ESRCH) {
			errs = append(errs, err)
		}
	}

	for _, p := range procs {
		_, _ = p.Wait()
	}

	return errors.Join(errs...)
}
