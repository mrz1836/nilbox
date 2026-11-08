package settings

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// FuzzDecodeWindowSize checks that a stored window size either is refused, as the zero
// size, or decodes to a size within the bounds that encodes to a value decoding to
// itself.
func FuzzDecodeWindowSize(f *testing.F) {
	for _, seed := range []string{
		"",
		"1280x800",
		"800x520",
		"7680x4320",
		"799x520",
		"+1280x0800",
		"1280x800x2",
		"-1x-1",
		"99999999999999999999x800",
		" 1280 x 800 ",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, stored string) {
		size, ok := decodeWindowSize(stored)
		if !ok {
			assert.Equal(t, WindowSize{}, size)

			return
		}

		assert.True(t, size.valid(), "decoded an out-of-bounds size %+v from %q", size, stored)

		again, ok := decodeWindowSize(size.encode())
		require.True(t, ok, "%q does not decode", size.encode())
		assert.Equal(t, size, again)
	})
}

// FuzzDecodeUIPrefs checks the decoders of the stored theme, menu bar, and collapsed
// sections: any stored value gives a theme that is one of the choices (the stored one when
// it is), and each decoded value, written back the way the settings service writes it,
// decodes to itself. A section is collapsed exactly when its stored value reads as true.
func FuzzDecodeUIPrefs(f *testing.F) {
	for _, seed := range []string{"", "system", "dark", "Dark", "true", "false", "1", "T", "yes", "\x00"} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, stored string) {
		theme := decodeTheme(stored)
		assert.True(t, validTheme(theme), "theme %q", theme)
		assert.Equal(t, theme, decodeTheme(string(theme)))

		if validTheme(Theme(stored)) {
			assert.Equal(t, Theme(stored), theme)
		}

		on := decodeMenuBar(stored)
		assert.Equal(t, on, decodeMenuBar(strconv.FormatBool(on)))

		for _, list := range listKeys() {
			checkDecodedSection(t, list, stored)
		}
	})
}

// checkDecodedSection checks decodeCollapsed when only list's section has a stored value.
func checkDecodedSection(t *testing.T, list ListKey, stored string) {
	t.Helper()

	collapsed := decodeCollapsed(map[string]string{collapsedKey(list): stored})
	require.NotNil(t, collapsed)

	if want, err := strconv.ParseBool(stored); err == nil && want {
		assert.Equal(t, []ListKey{list}, collapsed)
	} else {
		assert.Empty(t, collapsed)
	}

	written := map[string]string{collapsedKey(list): strconv.FormatBool(len(collapsed) > 0)}
	assert.Equal(t, collapsed, decodeCollapsed(written))
}
