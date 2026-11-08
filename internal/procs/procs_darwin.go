//go:build darwin

package procs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"

	"github.com/mrz1836/nilbox/internal/execpath"
)

// lsofBin is lsof's fixed path on macOS.
const lsofBin = "/usr/sbin/lsof"

// processes asks lsof for the working directory of each of the user's processes. lsof
// exits non-zero when a process ends while it reads, or it cannot read one; what it
// printed for the others still counts.
func processes(ctx context.Context, run execpath.Runner) ([]Process, error) {
	out, _, err := run.Run(ctx, lsofBin, lsofArgs(os.Getuid())...)

	if _, exited := errors.AsType[*exec.ExitError](err); err != nil && (!exited || len(out) == 0 || ctx.Err() != nil) {
		return nil, fmt.Errorf("list the running processes: %w", err)
	}

	return ParseLsof(string(out)), nil
}

// lsofArgs asks lsof for the parent, command, and working directory of each process of
// the user with uid, numerically, without resolving names.
func lsofArgs(uid int) []string {
	return []string{"-nP", "-a", "-u", strconv.Itoa(uid), "-d", "cwd", "-FpcRn"}
}
