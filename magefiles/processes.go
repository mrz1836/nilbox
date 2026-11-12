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

// quitRunningApp asks every process running exe to quit, with SIGTERM, which the app
// handles as a normal quit, and waits up to timeout until they have exited. It reports
// whether any was running.
func quitRunningApp(exe string, timeout time.Duration) (bool, error) {
	pids, err := appProcesses(exe)
	if err != nil || len(pids) == 0 {
		return false, err
	}

	fmt.Printf("Quitting the running %s...\n", brand.Name) //nolint:forbidigo // mage target output

	for _, pid := range pids {
		if err = syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
			return true, fmt.Errorf("quit the running app (pid %d): %w", pid, err)
		}
	}

	for deadline := time.Now().Add(timeout); time.Now().Before(deadline); time.Sleep(appQuitPoll) {
		if pids, err = appProcesses(exe); err != nil || len(pids) == 0 {
			return true, err
		}
	}

	return true, errStillRunning
}

// openAfterQuit opens bundle just after a running copy of the app quit. LaunchServices
// can still list the old copy for a moment after its process exits, and an open in that
// window fails with error -600 (procNotFound), so it tries again until timeout, quietly,
// and reports the last failure only if none succeeds.
func openAfterQuit(bundle string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for {
		out, err := exec.CommandContext(context.Background(), "open", bundle).CombinedOutput() //nolint:gosec // argv only, no shell
		if err == nil {
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("open %s: %w: %s", bundle, err, strings.TrimSpace(string(out)))
		}

		time.Sleep(appReopenPoll)
	}
}

// appProcesses returns the IDs of the processes whose command line starts with exe.
func appProcesses(exe string) ([]int, error) {
	pattern := "^" + regexp.QuoteMeta(exe) + "( |$)"

	out, err := exec.CommandContext(context.Background(), "pgrep", "-f", pattern).Output() //nolint:gosec // argv only, no shell
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
