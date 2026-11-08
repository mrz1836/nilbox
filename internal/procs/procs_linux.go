//go:build linux

package procs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/execpath"
)

// procRoot is where Linux describes its processes.
const procRoot = "/proc"

// processes reads each process's working directory and command from /proc. A process
// that is not the user's, or that ends while it is read, cannot be read and is left out.
func processes(ctx context.Context, _ execpath.Runner) ([]Process, error) {
	return readProc(ctx, procRoot)
}

// readProc reads the processes described under root.
func readProc(ctx context.Context, root string) ([]Process, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("list the running processes: %w", err)
	}

	var list []Process

	for _, e := range entries {
		if err = ctx.Err(); err != nil {
			return nil, err
		}

		pid, convErr := strconv.Atoi(e.Name())
		if convErr != nil || pid <= 0 {
			continue
		}

		dir, linkErr := os.Readlink(filepath.Join(root, e.Name(), "cwd"))
		if linkErr != nil || !filepath.IsAbs(dir) {
			continue
		}

		comm, _ := os.ReadFile(filepath.Join(root, e.Name(), "comm")) //nolint:gosec // the kernel's process table

		list = append(list, Process{
			PID: pid, PPID: parentOf(filepath.Join(root, e.Name(), "status")), Command: strings.TrimSpace(string(comm)),
			Dir: dir,
		})
	}

	return list, nil
}

// parentOf reads a process's parent from its status file's "PPid:" line, or 0.
func parentOf(status string) int {
	data, err := os.ReadFile(status) //nolint:gosec // the kernel's process table
	if err != nil {
		return 0
	}

	for line := range strings.Lines(string(data)) {
		if value, ok := strings.CutPrefix(line, "PPid:"); ok {
			ppid, _ := strconv.Atoi(strings.TrimSpace(value)) // an unreadable parent is unknown, 0

			return ppid
		}
	}

	return 0
}
