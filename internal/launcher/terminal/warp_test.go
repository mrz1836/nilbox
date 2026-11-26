package terminal

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWarpOpenArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
	}{
		{name: "plain", path: "/tmp/launch/pr-7.command"},
		{name: "awkward", path: awkwardScriptPath},
		{name: "a folder with a space", path: "/tmp/Application Support/example/launch/pr-7.command"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := warpOpenArgs(tt.path)
			assert.Equal(t, []string{"-b", "dev.warp.Warp-Stable", tt.path}, got,
				"Warp is opened by its bundle ID, and the path is one argv element, never quoted")
		})
	}
}
