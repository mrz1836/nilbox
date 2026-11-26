package terminal

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// warpTestLink is a focus link as Warp exports it: a session link and 32 lowercase hex
// digits.
const warpTestLink = "warp://session/0123456789abcdef0123456789abcdef"

func TestWarpFocusLink(t *testing.T) {
	t.Parallel()

	got, err := warpFocusLink(warpTestLink)
	require.NoError(t, err)
	assert.Equal(t, warpTestLink, got)

	hex := strings.TrimPrefix(warpTestLink, "warp://session/")

	for name, bad := range map[string]string{
		"empty":               "",
		"an action":           "warp://action/new_tab?path=/tmp/launch/pr-7.command",
		"a new window":        "warp://action/new_window?path=/tmp",
		"uppercase hex":       "warp://session/" + strings.ToUpper(hex),
		"short hex":           "warp://session/" + hex[:31],
		"long hex":            "warp://session/" + hex + "0",
		"not hex":             "warp://session/" + hex[:31] + "g",
		"an extra query":      warpTestLink + "?path=/tmp",
		"a fragment":          warpTestLink + "#x",
		"a trailing slash":    warpTestLink + "/",
		"a trailing newline":  warpTestLink + "\n",
		"a leading space":     " " + warpTestLink,
		"another scheme":      "warppreview://session/" + hex,
		"no scheme":           "session/" + hex,
		"a dashed UUID":       "warp://session/01234567-89ab-cdef-0123-456789abcdef",
		"another app's link":  "https://example.com/session/" + hex,
		"a second link after": warpTestLink + " " + warpTestLink,
	} {
		got, err = warpFocusLink(bad)
		require.ErrorIs(t, err, ErrNoFocusLink, name)
		assert.Empty(t, got, name)
		assert.NotContains(t, err.Error(), "path=", "%s: the error never repeats a value that isn't a link", name)
	}
}

func TestWarpFocusArgs(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"-b", "dev.warp.Warp-Stable", warpTestLink}, warpFocusArgs(warpTestLink),
		"Warp is opened by its bundle ID, and the link is one argv element, never quoted")
}

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
