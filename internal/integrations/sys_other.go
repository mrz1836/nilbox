//go:build !unix

package integrations

// writable reports whether the user may create files in dir; elsewhere, the write itself
// says so.
func writable(string) bool {
	return true
}

// killed reports a program that ended on SIGKILL; elsewhere there is none.
func killed(error) bool {
	return false
}
