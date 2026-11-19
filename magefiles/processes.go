//go:build mage

package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mrz1836/nilbox/internal/brand"
)

// quitRunningApp asks every process running exe, the build from source's, to quit, with
// SIGTERM, which the app handles as a normal quit, and waits up to timeout until they have
// exited. It reports whether any was running.
func (r runner) quitRunningApp(exe string, timeout time.Duration) (bool, error) {
	pids, err := r.appProcesses(exe)
	if err != nil || len(pids) == 0 {
		return false, err
	}

	r.say("Quitting the running " + brand.DevName + "...")

	for _, pid := range pids {
		if err = syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
			return true, fmt.Errorf("quit the running app (pid %d): %w", pid, err)
		}
	}

	for deadline := time.Now().Add(timeout); time.Now().Before(deadline); time.Sleep(appQuitPoll) {
		if pids, err = r.appProcesses(exe); err != nil || len(pids) == 0 {
			return true, err
		}
	}

	return true, errStillRunning
}

// openAfterQuit opens bundle just after a running copy of the app quit. LaunchServices
// can still list the old copy for a moment after its process exits, and an open in that
// window fails with error -600 (procNotFound), so it tries again until timeout, quietly,
// and reports the last failure only if none succeeds.
func (r runner) openAfterQuit(bundle string, timeout time.Duration) error {
	return r.openRetrying(timeout, bundle)
}

// openRetrying runs open with args as openAfterQuit does: again until timeout while it
// fails, reporting the last failure only if none succeeds.
func (r runner) openRetrying(timeout time.Duration, args ...string) error {
	deadline := time.Now().Add(timeout)

	for {
		out, err := r.cmd(context.Background(), nil, "open", args...).CombinedOutput()
		if err == nil {
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("open %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}

		time.Sleep(appReopenPoll)
	}
}

// appProcesses returns the IDs of the processes whose command line starts with exe.
func (r runner) appProcesses(exe string) ([]int, error) {
	pattern := "^" + regexp.QuoteMeta(exe) + "( |$)"

	out, err := r.cmd(context.Background(), nil, "pgrep", "-f", pattern).Output()
	if err != nil {
		if exit, ok := errors.AsType[*exec.ExitError](err); ok && exit.ExitCode() == 1 { // pgrep exits 1 when nothing matches
			return nil, nil
		}

		return nil, fmt.Errorf("find the running app: %w", err)
	}

	var pids []int

	for field := range strings.FieldsSeq(string(out)) {
		pid, convErr := strconv.Atoi(field)
		if convErr != nil {
			return nil, fmt.Errorf("parse the pgrep output %q: %w", field, convErr)
		}

		pids = append(pids, pid)
	}

	return pids, nil
}

// stopProcessGroup stops a process started in its own group, and everything it started.
func stopProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}

	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)

	done := make(chan struct{})

	go func() {
		_ = cmd.Wait()

		close(done)
	}()

	select {
	case <-done:
	case <-time.After(devServerStopTimeout):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
