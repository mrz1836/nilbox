//go:build !unix

package worktrees

// processLiveness cannot probe processes portably here, so every lock's session state is
// reported as unknown and no stale-lock hint is shown.
func processLiveness(_ int) liveness {
	return livenessUnknown
}
