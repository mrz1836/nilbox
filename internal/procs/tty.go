package procs

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/execpath"
)

// psBin is ps's fixed path on macOS and Linux; it names the terminal a process runs on.
const psBin = "/bin/ps"

// ErrNoTTY is returned for a process that runs on no terminal, or that ps can't find.
var ErrNoTTY = errors.New("the process runs on no terminal")

// TTY returns the terminal device the process pid runs on, such as /dev/ttys004 or
// /dev/pts/3, asking ps through run. A terminal's tab runs on such a device, so a terminal
// that finds tabs by it (iTerm2, Terminal.app) can bring forward the tab a session or a
// run's script runs in. A process with no terminal, or none at all, is ErrNoTTY.
func TTY(ctx context.Context, run execpath.Runner, pid int) (string, error) {
	out, _, err := run.Run(ctx, psBin, "-o", "tty=", "-p", strconv.Itoa(pid))
	if err != nil {
		return "", fmt.Errorf("%w: ps: %w", ErrNoTTY, err)
	}

	name := strings.TrimSpace(execpath.FirstLine(out))
	if name == "" || strings.Trim(name, "?") == "" || strings.ContainsAny(name, " \t") {
		return "", fmt.Errorf("%w: process %d", ErrNoTTY, pid)
	}

	return "/dev/" + strings.TrimPrefix(name, "/dev/"), nil
}
