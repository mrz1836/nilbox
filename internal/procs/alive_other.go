//go:build !unix

package procs

// Alive cannot tell here, and reports every process with a pid as running, so nothing
// is taken for ended on a guess.
func Alive(pid int) bool {
	return pid > 0
}
