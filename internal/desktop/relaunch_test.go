package desktop_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/desktop"
)

func TestRelaunchArgsOpenTheBundleAgain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		exe     string
		dataDir string
		want    []string
	}{
		{
			name: "the user's own data",
			exe:  "/Applications/Example.app/Contents/MacOS/example",
			want: []string{"/usr/bin/open", "-n", "/Applications/Example.app"},
		},
		{
			name:    "a sandbox",
			exe:     "/opt/example/bin/Example.app/Contents/MacOS/example",
			dataDir: "/tmp/example-fresh-1",
			want: []string{
				"/usr/bin/open", "-n", "/opt/example/bin/Example.app", "--args",
				"--data-dir=/tmp/example-fresh-1",
			},
		},
		{
			name: "a bundle whose name has spaces",
			exe:  "/Applications/My Example.app/Contents/MacOS/example",
			want: []string{"/usr/bin/open", "-n", "/Applications/My Example.app"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := desktop.RelaunchArgs(tt.exe, tt.dataDir)
			assert.True(t, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRelaunchArgsOpenNothingOutsideABundle(t *testing.T) {
	t.Parallel()

	for _, exe := range []string{
		"",
		"example",
		"/tmp/go-build123/b001/exe/example",
		"/Applications/Example/Contents/MacOS/example",
		"/Applications/Example.app/MacOS/example",
		"/.app/Contents/MacOS/example",
	} {
		got, ok := desktop.RelaunchArgs(exe, "")
		assert.False(t, ok, exe)
		assert.Nil(t, got, exe)
	}
}
