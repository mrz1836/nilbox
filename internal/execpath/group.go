package execpath

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// groupWaitDelay bounds how long a canceled group's leader may take to exit after it is
// killed before Wait gives up on it.
const groupWaitDelay = 2 * time.Second

// signalExitBase is added to a signal's number to make the exit status of a program a
// signal ended, as shells report it: 130 for SIGINT, 143 for SIGTERM, 137 for SIGKILL.
const signalExitBase = 128

// ErrUnsupported is returned by Start where process groups don't exist (Windows).
var ErrUnsupported = errors.New("process groups are not supported on this OS")

// errNotAbsolute is returned by Start for a program given without its absolute path.
var errNotAbsolute = errors.New("the program's path is not absolute")

// Cmd is a long-lived program to start in its own process group: a run the user started,
// or the login shell read. Its output goes straight into Output, never through a pipe the
// app reads, so a crash of the app can't break the program's output midway and a
// descendant holding that output can't hang Wait.
type Cmd struct {
	// Path is the program's absolute path.
	Path string

	// Args are the arguments after the program.
	Args []string

	// Dir is the working directory; "" is the app's.
	Dir string

	// Env is the whole environment, as in exec.Cmd.Env; nil is the app's.
	Env []string

	// Output gets stdout and stderr both, in one ordered stream; nil discards them.
	Output *os.File
}

// Step is one rung of a Ladder: a signal for the whole group, then how long to wait for
// the program to end before the next rung.
type Step struct {
	Signal syscall.Signal
	Wait   time.Duration
}

// Ladder is how Stop ends a group: each step's signal in turn, until the program ends.
// It should end with SIGKILL.
type Ladder []Step

// StopLadder is the ladder for a run the user stops: SIGINT, then SIGTERM after five
// seconds, then SIGKILL after ten more.
func StopLadder() Ladder {
	const toTerm, toKill = 5 * time.Second, 10 * time.Second

	return Ladder{{syscall.SIGINT, toTerm}, {syscall.SIGTERM, toKill}, {syscall.SIGKILL, 0}}
}

// Proc is a program Start started, the leader of its own process group. A descendant
// that calls setsid (an ssh or gpg agent, a daemon) leaves the group, and is not stopped
// with it.
type Proc struct {
	cmd  *exec.Cmd
	ctx  context.Context //nolint:containedctx // Start's context, read once the program exits
	pgid int

	done chan struct{}
	code int
	err  error

	stopOnce sync.Once
}

// Pid returns the program's process ID.
func (p *Proc) Pid() int { return p.cmd.Process.Pid }

// Pgid returns its process group's ID, which is its PID.
func (p *Proc) Pgid() int { return p.pgid }

// Done is closed once the program has exited and Wait's answer is ready.
func (p *Proc) Done() <-chan struct{} { return p.done }

// Wait waits for the program to exit and returns its exit status: its exit code, or
// 128 plus the signal's number when a signal ended it. err is the *exec.ExitError of a
// non-zero status, the context's error when Start's context ended it, or why it couldn't
// be waited for. Every call returns the same answer.
func (p *Proc) Wait() (exitCode int, err error) {
	<-p.done

	return p.code, p.err
}

// Signal sends sig to the program's whole process group.
func (p *Proc) Signal(sig syscall.Signal) error {
	return signalGroup(p.pgid, sig)
}

// Stop climbs ladder until the program exits: it sends the first step's signal to the
// group now, and each later one once the step before has waited in vain. It returns at
// once; the waiting happens in the background and ends with the program. Only the first
// call counts.
func (p *Proc) Stop(ladder Ladder) {
	p.stopOnce.Do(func() {
		go p.climb(ladder)
	})
}

// climb sends ladder's signals in turn until the program exits.
func (p *Proc) climb(ladder Ladder) {
	ladder.Climb(p.done, nil, func(sig syscall.Signal) error { return signalGroup(p.pgid, sig) })
}

// Climb sends each step's signal in turn through signal, waiting the step's time between
// them, until done closes or alive, when given, reports the target gone. Proc.Stop climbs
// with it; a caller that holds no Proc, such as one stopping a program from before the
// app restarted, passes its own alive and signal, which check the group is still its own.
func (l Ladder) Climb(done <-chan struct{}, alive func() bool, signal func(syscall.Signal) error) {
	for _, step := range l {
		select {
		case <-done:
			return
		default:
		}

		if alive != nil && !alive() {
			return
		}

		_ = signal(step.Signal)

		if step.Wait <= 0 {
			continue
		}

		timer := time.NewTimer(step.Wait)

		select {
		case <-done:
			timer.Stop()

			return
		case <-timer.C:
		}
	}
}

// waitError joins why Start's context ended, when it did, to the program's own error, so
// a caller can tell a timeout or a cancel from a program that failed by itself.
func (p *Proc) waitError(err error) error {
	if ctxErr := p.ctx.Err(); ctxErr != nil && err != nil && !errors.Is(err, ctxErr) {
		return fmt.Errorf("%w: %w", ctxErr, err)
	}

	return err
}

// exitStatus reads a finished program's exit status from its state.
func exitStatus(state *os.ProcessState) int {
	if state == nil {
		return -1
	}

	if ws, ok := state.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return signalExitBase + int(ws.Signal())
	}

	return state.ExitCode()
}
