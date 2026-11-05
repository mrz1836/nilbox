package brand

import (
	"errors"
	"testing"
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

	if !errors.Is(err, ErrInvalidHexColor) {
		t.Fatalf("unexpected error type: %v", err)
	}
}

func requireRatioInRange(t *testing.T, got float64, a, b string) {
	t.Helper()

	if got < 1 || got > 21.0001 {
		t.Fatalf("ratio %f out of range for %q/%q", got, a, b)
	}
}

func requireSymmetric(t *testing.T, got float64, a, b string) {
	t.Helper()

	swapped, err := ContrastRatio(b, a)
	if err != nil || swapped != got {
		t.Fatalf("ratio not symmetric for %q/%q: %f vs %f (%v)", a, b, got, swapped, err)
	}
}
