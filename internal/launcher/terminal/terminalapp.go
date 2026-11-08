package terminal

// appTerminal names Terminal.app, both for `open -a` and in error messages.
const appTerminal = "Terminal"

// terminalAppArgs returns the `open` arguments that run a launch script in a new
// Terminal.app window. Terminal.app runs .command files it is asked to open, so this
// needs no Automation permission. It is pure: running it is the darwin driver's job.
func terminalAppArgs(scriptPath string) []string {
	return []string{"-a", appTerminal, scriptPath}
}
