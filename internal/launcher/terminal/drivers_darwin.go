//go:build darwin

package terminal

import "github.com/mrz1836/nilbox/internal/execpath"

// DefaultRegistry returns the terminal drivers for macOS: iTerm2 (the default) and
// Terminal.app. Both run their programs through executor.
func DefaultRegistry(executor execpath.Runner) Registry {
	return NewRegistry(itermDriver{executor: executor}, terminalAppDriver{executor: executor})
}
