//go:build !unix

package autopilot

import "os"

// flock holds the lock on a system without flock: the app runs on macOS, and its plain Go
// builds and tests on Linux, both of which have it.
func flock(*os.File) error { return nil }

// funlock lets go of nothing.
func funlock(*os.File) error { return nil }
