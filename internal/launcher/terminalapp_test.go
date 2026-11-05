package launcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTerminalAppArgs(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "plain", path: "/tmp/launch/pr-7.command"},
		{name: "awkward", path: awkwardScriptPath},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := terminalAppArgs(tt.path)
			assert.Equal(t, []string{"-a", "Terminal", tt.path}, got, "the path is one argv element, never quoted")
		})
	}
}
