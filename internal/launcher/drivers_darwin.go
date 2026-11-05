//go:build darwin

package launcher

// DefaultRegistry returns the terminal drivers for macOS: iTerm2 (the default) and
// Terminal.app. Both run their programs through executor.
func DefaultRegistry(executor Executor) Registry {
	return NewRegistry(itermDriver{executor: executor}, terminalAppDriver{executor: executor})
}
