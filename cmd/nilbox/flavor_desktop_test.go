//go:build desktop

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

//nolint:paralleltest // sets the package's flavor, which the linker stamps
func TestBuildFlavor(t *testing.T) {
	original := flavor

	t.Cleanup(func() { flavor = original })

	tests := []struct {
		flavor string
		want   string
	}{
		{flavor: "", want: "source"},
		{flavor: "release", want: "release"},
	}

	for _, tt := range tests {
		flavor = tt.flavor

		assert.Equal(t, tt.want, buildFlavor(), "flavor %q", tt.flavor)
	}
}
