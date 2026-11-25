//go:build !darwin && !linux

package procs

// DetailOf can't read a process's detail here, and reports so.
func DetailOf(_ int) (Detail, bool) {
	return Detail{}, false
}
