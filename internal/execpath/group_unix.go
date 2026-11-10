//go:build unix

package execpath

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// Start starts cmd in a new process group, with stdin from /dev/null and stdout and
// stderr both written straight into cmd.Output. When ctx ends, the whole group is killed.
// When the program exits, the rest of its group gets SIGHUP, as closing a terminal sends
// it: what the program left running in the background ends, unless it was started to
// survive a hang-up (nohup) or left the group (setsid).
func Start(ctx context.Context, cmd Cmd) (*Proc, error) {
	if !filepath.IsAbs(cmd.Path) {
		return nil, fmt.Errorf("start %s: %w", cmd.Path, errNotAbsolute)
	}

	out := cmd.Output
	if out == nil {
		devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", os.DevNull, err)
		}

		defer func() { _ = devNull.Close() }()

		out = devNull
	}

	c := exec.CommandContext(ctx, cmd.Path, cmd.Args...) //nolint:gosec // argv only, no shell
	c.Dir = cmd.Dir
	c.Env = cmd.Env
	c.Stdout = out
	c.Stderr = out
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error { return killGroup(c.Process.Pid) }
	c.WaitDelay = groupWaitDelay

	if err := c.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", filepath.Base(cmd.Path), err)
	}

	p := &Proc{cmd: c, ctx: ctx, pgid: c.Process.Pid, done: make(chan struct{})}

	go p.reap()

	return p, nil
}

// reap waits for the program, hangs up the rest of its group, and records the outcome.
func (p *Proc) reap() {
	err := p.cmd.Wait()

	_ = signalGroup(p.pgid, syscall.SIGHUP)

	p.code = exitStatus(p.cmd.ProcessState)
	p.err = p.waitError(err)

	close(p.done)
}

// killGroup kills the group pgid, Start's context having ended. A group already gone
// reports os.ErrProcessDone, so a program that exited just as its context ended still
// counts as having succeeded.
func killGroup(pgid int) error {
	err := syscall.Kill(-pgid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}

	return err
}

// signalGroup sends sig to every process in the group pgid.
func signalGroup(pgid int, sig syscall.Signal) error {
	if pgid <= 0 {
		return fmt.Errorf("signal process group %d: %w", pgid, syscall.ESRCH)
	}

	if err := syscall.Kill(-pgid, sig); err != nil {
		return fmt.Errorf("signal process group %d: %w", pgid, err)
	}

	return nil
}
