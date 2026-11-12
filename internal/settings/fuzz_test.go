package settings

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/prefs/prefval"
	"github.com/mrz1836/nilbox/internal/store"
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
		theme := themes().decode(stored)
		assert.True(t, themes().valid(theme), "theme %q", theme)
		assert.Equal(t, theme, themes().decode(string(theme)))

		if themes().valid(Theme(stored)) {
			assert.Equal(t, Theme(stored), theme)
		}

		on := prefval.Bool(stored, DefaultMenuBar)
		assert.Equal(t, on, prefval.Bool(prefval.EncodeBool(on), DefaultMenuBar))

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

	written := map[string]string{collapsedKey(list): prefval.EncodeBool(len(collapsed) > 0)}
	assert.Equal(t, collapsed, decodeCollapsed(written))
}

// FuzzDecodeCloneLocations checks that any stored text decodes to a list, never nil, of
// locations each with a known kind and a path that is ~, ~-relative, or absolute, which,
// written back the way the settings service writes it, decodes to itself.
func FuzzDecodeCloneLocations(f *testing.F) {
	for _, seed := range []string{
		"",
		"[]",
		"null",
		`[{"path":"~/work","kind":"folder"},{"path":"~/Desktop/exp","kind":"clone"}]`,
		`[{"path":"relative","kind":"folder"},{"path":"/srv/x/","kind":"clone"}]`,
		`[{"path":"~","kind":"folder"},{"path":"~user/x","kind":"folder"},{"path":"~/x","kind":"bucket"}]`,
		`{"path":"~/work","kind":"folder"}`,
		`[{"path":" ~/a/../b ","kind":"folder"}]`,
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, stored string) {
		list := decodeCloneLocations(stored)
		require.NotNil(t, list)

		for _, loc := range list {
			assert.True(t, loc.Kind.Valid(), "kind %q", loc.Kind)
			assert.True(t, loc.Path == "~" || strings.HasPrefix(loc.Path, "~/") || filepath.IsAbs(loc.Path),
				"path %q", loc.Path)
		}

		written, err := store.EncodeJSON(list)
		require.NoError(t, err)
		assert.Equal(t, list, decodeCloneLocations(written))
	})
}
