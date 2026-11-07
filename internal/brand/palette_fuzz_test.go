package brand

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// FuzzContrastRatio checks that ContrastRatio never panics, only fails with
// ErrInvalidHexColor, and always returns a symmetric ratio within [1, 21].
func FuzzContrastRatio(f *testing.F) {
	for _, seed := range [][2]string{
		{black, white},
		{Dark.Accent, Dark.Background},
		{"#fff", black},
		{"", ""},
		{"#ZZZZZZ", "#123456"},
	} {
		f.Add(seed[0], seed[1])
	}

	f.Fuzz(func(t *testing.T, a, b string) {
		got, err := ContrastRatio(a, b)
		if err != nil {
			requireInvalidHex(t, err)

			return
		}

		requireRatioInRange(t, got, a, b)
		requireSymmetric(t, got, a, b)
	})
}

func requireInvalidHex(t *testing.T, err error) {
	t.Helper()

	require.ErrorIs(t, err, ErrInvalidHexColor)
}

func requireRatioInRange(t *testing.T, got float64, a, b string) {
	t.Helper()

	require.GreaterOrEqual(t, got, 1.0, "ratio out of range for %q/%q", a, b)
	require.LessOrEqual(t, got, 21.0001, "ratio out of range for %q/%q", a, b)
}

func requireSymmetric(t *testing.T, got float64, a, b string) {
	t.Helper()

	swapped, err := ContrastRatio(b, a)
	require.NoError(t, err, "ratio not symmetric for %q/%q", a, b)
	require.InDelta(t, got, swapped, 0, "ratio not symmetric for %q/%q", a, b)
}
