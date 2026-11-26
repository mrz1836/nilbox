//go:build darwin

package terminal

import "github.com/mrz1836/nilbox/internal/execpath"

// DefaultRegistry returns the terminal drivers for macOS: iTerm2 (the default), Warp, and
// Terminal.app. Each runs its programs through executor.
func DefaultRegistry(executor execpath.Runner) Registry {
	return NewRegistry(itermDriver{executor: executor}, warpDriver{executor: executor},
		terminalAppDriver{executor: executor})
}
