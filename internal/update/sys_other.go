//go:build !unix

package update

// canWrite reports whether the user may create files in dir; elsewhere, the write itself
// says so.
func canWrite(string) bool {
	return true
}
