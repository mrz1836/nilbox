//go:build !desktop

package main

import (
	"fmt"
	"io"
)

// exitStatus prints err, if any, to stderr and returns the status the headless build
// exits with: 0 without an error, 1 with one. The desktop build logs its error instead.
func exitStatus(err error, stderr io.Writer) int {
	if err == nil {
		return 0
	}

	_, _ = fmt.Fprintln(stderr, err)

	return 1
}
