//go:build !darwin && !linux

package procs

// DetailOf can't read a process's detail here, and reports so.
func DetailOf(_ int) (Detail, bool) {
	return Detail{}, false
}

// EnvOf can't read a process's environment here, and reports so.
func EnvOf(_ int, _ string) (string, bool) {
	return "", false
}
