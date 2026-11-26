//go:build darwin

package terminal

import (
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/procs"
)

// DefaultRegistry returns the terminal drivers for macOS: iTerm2 (the default), Warp, and
// Terminal.app. Each runs its programs through executor, and Warp's reads a session's
// focus link from its process with procs.EnvOf.
func DefaultRegistry(executor execpath.Runner) Registry {
	return NewRegistry(itermDriver{executor: executor}, warpDriver{executor: executor, env: procs.EnvOf},
		terminalAppDriver{executor: executor})
}
