//go:build !darwin

package execpath

// wellKnownDirs returns no extra directories: only macOS is supported at runtime so far,
// and other systems' GUI sessions keep the user's PATH.
func wellKnownDirs(_ string) []string {
	return nil
}
