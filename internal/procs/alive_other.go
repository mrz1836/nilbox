//go:build !unix

package procs

// Probe cannot tell here: every process is Unknown.
func Probe(_ int) Liveness {
	return Unknown
}

// Alive cannot tell here, and reports every process with a pid as running, so nothing
// is taken for ended on a guess.
func Alive(pid int) bool {
	return ValidPID(pid)
}
