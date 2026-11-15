//go:build !darwin

package localrepos

// mountPoints lists the folders disks are mounted on. Outside macOS, where the desktop
// app doesn't run yet, none is known.
func mountPoints() []string {
	return nil
}
