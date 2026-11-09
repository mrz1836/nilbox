package notifyprefs_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/notifyprefs"
)

// FuzzDecodeBannersOff checks that any stored values decode to known kinds, each once and
// in the order of Kinds, that the list reads back unchanged once encoded, and that
// turning any kind on or off changes that kind alone and also reads back unchanged.
func FuzzDecodeBannersOff(f *testing.F) {
	for _, seed := range [][2]string{
		{"", ""},
		{"[]", "[]"},
		{`["woke","review_requested"]`, `["session_finished"]`},
		{`["assigned","assigned","nope",""]`, `["session_needs_you","session_needs_you"]`},
		{`{"assigned":true}`, `{"session_finished":true}`},
		{`["woke"`, `["session_finished"`},
		{"null", "null"},
		{`["été", 7, null]`, `["session_finished", 7]`},
		{`["session_finished"]`, `["woke"]`},
	} {
		f.Add(seed[0], seed[1])
	}

	f.Fuzz(func(t *testing.T, storedOff, storedOn string) {
		off := notifyprefs.DecodeBannersOff(storedOff, storedOn)
		checkBannersOff(t, off)

		for _, kind := range notifyprefs.Kinds() {
			checkSetBanner(t, off, kind, true)
			checkSetBanner(t, off, kind, false)
		}
	})
}

// checkBannersOff checks a decoded list: not nil, known kinds in the order of Kinds
// without repeats, and the same once encoded and decoded again.
func checkBannersOff(t *testing.T, off []notifyprefs.Kind) {
	t.Helper()

	require.NotNil(t, off)

	last := -1

	for _, kind := range off {
		assert.True(t, kind.Valid(), "unknown kind %q", kind)

		at := slices.Index(notifyprefs.Kinds(), kind)
		assert.Greater(t, at, last, "%q is out of order or repeated in %q", kind, off)

		last = at
	}

	assert.Equal(t, off, notifyprefs.DecodeBannersOff(notifyprefs.EncodeBanners(off)))
}

// checkSetBanner checks that turning kind's banners on or off in off changes only kind
// and leaves a list that reads back unchanged.
func checkSetBanner(t *testing.T, off []notifyprefs.Kind, kind notifyprefs.Kind, on bool) {
	t.Helper()

	got, err := notifyprefs.SetBanner(off, kind, on)
	require.NoError(t, err)
	checkBannersOff(t, got)

	for _, k := range notifyprefs.Kinds() {
		want := slices.Contains(off, k)
		if k == kind {
			want = !on
		}

		assert.Equal(t, want, slices.Contains(got, k), "kind %q after setting %q to %v", k, kind, on)
	}
}
