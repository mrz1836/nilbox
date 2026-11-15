package semver_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/semver"
)

func TestCompare(t *testing.T) {
	t.Parallel()

	tests := []struct {
		a, b string
		want int
	}{
		{"1.29.5", "1.29.5", 0},
		{"v1.29.5", "1.29.5", 0},
		{"1.29.5", "1.30.0", -1},
		{"1.30.0", "1.29.5", 1},
		{"1.10.0", "1.9.9", 1},
		{"2.0.0", "1.99.99", 1},
		{"1.30.0-rc.1", "1.30.0", -1},
		{"1.30.0-rc.2", "1.30.0-rc.10", -1},
		{"1.30.0-alpha", "1.30.0-alpha.1", -1},
		{"1.30.0-1", "1.30.0-alpha", -1},
		{"1.30.0+build.5", "1.30.0", 0},
		{"1.29.6-0.20261105041738-9a61f25f00aa", "1.29.5", 1},
		{"0.0.0-20261105041738-9a61f25f00aa", "0.0.1", -1},
		{"1.0.0-0", "1.0.0-x-y", -1},
		{"1.0.0-rc.10", "1.0.0-rc.9", 1},
		{"1.29.5+dirty", "1.29.5", 0},
	}

	for _, tt := range tests {
		got, ok := semver.Compare(tt.a, tt.b)
		require.True(t, ok, "%s vs %s", tt.a, tt.b)
		assert.Equal(t, tt.want, got, "%s vs %s", tt.a, tt.b)
	}

	for _, bad := range []string{
		"", "dev", "1.2", "1.2.3.4", "01.2.3", "1.2.x", "1.2.3-", "-1.2.3", "1.2.3-rc.01", "1.2.3-rc..1",
		"1.2.3-rc.1.", "1.2.3+", "1.2.3+meta..5", "1.2.3-r c", "1.2.3-é", "+1.2.3", "1.-2.3",
	} {
		_, ok := semver.Compare(bad, "1.0.0")
		assert.False(t, ok, "%q isn't a version", bad)

		_, ok = semver.Compare("1.0.0", bad)
		assert.False(t, ok, "%q isn't a version", bad)
	}
}

func TestPrerelease(t *testing.T) {
	t.Parallel()

	for raw, want := range map[string]bool{
		"0.3.0": false, "v0.3.0": false, "0.3.0+build.5": false, "0.3.0-rc.1": true, "1.0.0-alpha": true,
	} {
		v, ok := semver.Parse(raw)
		require.True(t, ok, raw)
		assert.Equal(t, want, v.Prerelease(), raw)
	}
}

func FuzzCompare(f *testing.F) {
	for _, seed := range [][2]string{
		{"1.29.5", "1.30.0"}, {"1.30.0-rc.2", "1.30.0-rc.10"}, {"v2.0.0+meta", "2.0.0"}, {"", "1.0.0"},
	} {
		f.Add(seed[0], seed[1])
	}

	f.Fuzz(func(t *testing.T, a, b string) {
		ab, okAB := semver.Compare(a, b)
		ba, okBA := semver.Compare(b, a)

		assert.Equal(t, okAB, okBA, "whether both are versions doesn't depend on the order")

		if okAB {
			assert.Equal(t, -ab, ba, "%q and %q compare the same both ways", a, b)
		}
	})
}
