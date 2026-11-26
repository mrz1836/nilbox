package terminal

const (
	// appWarp names Warp in error messages.
	appWarp = "Warp"

	// warpBundleID is Warp Stable's bundle identifier. The driver opens Warp by it, never
	// by name, so neither Warp Preview nor another app called Warp can answer.
	warpBundleID = "dev.warp.Warp-Stable"
)

// warpOpenArgs returns the `open` arguments that run a launch script in Warp. Warp runs
// an executable .command file it is asked to open in a new tab of its frontmost window,
// or a new window when none is open, typing the script's path into a shell of its own;
// it needs no Automation permission. Before Warp's first-run welcome is finished, Warp
// drops the request while open still succeeds. It is pure: running it is the darwin
// driver's job.
func warpOpenArgs(scriptPath string) []string {
	return []string{"-b", warpBundleID, scriptPath}
}
