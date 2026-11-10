package integrations

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// recorded reads a recorded run's stdout from testdata.
func recorded(t *testing.T, name string) []byte {
	t.Helper()

	return []byte(testutil.LoadFakeRun(t, filepath.Join("testdata", name)).Stdout)
}

func TestParseMagexVersionReadsTheRecordedOutput(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"magex-1.29.5/version.json", "magex-1.29.5/version-dash.json"} {
		got, err := ParseMagexVersion(recorded(t, name))
		require.NoError(t, err, name)
		assert.Equal(t, Version{Raw: recordedMagex, Commit: "9a61f25", Built: "2026-11-05T04:17:38Z"}, got, name)
	}
}

func TestParseGoPreCommitVersionReadsTheRecordedOutput(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"go-pre-commit-1.13.1/version.json", "go-pre-commit-1.13.1/version-short.json"} {
		got, err := ParseGoPreCommitVersion(recorded(t, name))
		require.NoError(t, err, name)
		assert.Equal(t, Version{
			Raw: "1.13.1", Commit: "cf3b488734cae11acac58840a3177e93d00b53c3", Built: "2026-10-18T04:07:07Z",
		}, got, name)
	}
}

func TestParseVersionVariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		parse func([]byte) (Version, error)
		out   string
		want  Version
	}{
		{
			"magex from go install", ParseMagexVersion, "Version Information\n  Version:      v1.29.5\n  Commit:       unknown\n",
			Version{Raw: recordedMagex, Commit: "unknown"},
		},
		{
			"magex in color", ParseMagexVersion, "\x1b[1mVersion Information\x1b[0m\n  \x1b[36mVersion:\x1b[0m      1.30.2\n",
			Version{Raw: "1.30.2"},
		},
		{
			"magex development build", ParseMagexVersion, "  Version:      1.29.6-0.20261105041738-9a61f25f00aa+dirty\n",
			Version{Raw: "1.29.6-0.20261105041738-9a61f25f00aa+dirty"},
		},
		{
			"go-pre-commit from go install", ParseGoPreCommitVersion,
			"go-pre-commit version v1.13.1 (commit: none, built: go-install)\n",
			Version{Raw: "1.13.1", Commit: "none", Built: "go-install"},
		},
		{
			"go-pre-commit without details", ParseGoPreCommitVersion, "go-pre-commit version dev\n", Version{Raw: "dev"},
		},
		{
			"go-pre-commit after an update banner", ParseGoPreCommitVersion,
			"A new version is available\ngo-pre-commit version 1.14.1 (commit: abc1234, built: 2026-11-10T00:00:00Z)\n",
			Version{Raw: "1.14.1", Commit: "abc1234", Built: "2026-11-10T00:00:00Z"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.parse([]byte(tt.out))
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	for _, out := range []string{"", "magex: command not found\n", "Usage: magex [target]\n", "  Version:   \n", "  Version:  1.2 beta\n"} {
		_, err := ParseMagexVersion([]byte(out))
		require.ErrorIs(t, err, ErrNoVersion, "%q", out)
	}

	for _, out := range []string{"", "go-pre-commit: unknown flag\n", "go-pre-commit version\n", "go-pre-commit version v"} {
		_, err := ParseGoPreCommitVersion([]byte(out))
		require.ErrorIs(t, err, ErrNoVersion, "%q", out)
	}
}

func TestCompareVersions(t *testing.T) {
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
	}

	for _, tt := range tests {
		got, ok := compareVersions(tt.a, tt.b)
		require.True(t, ok, "%s vs %s", tt.a, tt.b)
		assert.Equal(t, tt.want, got, "%s vs %s", tt.a, tt.b)
	}

	for _, bad := range []string{"", "dev", "1.2", "1.2.3.4", "01.2.3", "1.2.x", "1.2.3-", "-1.2.3"} {
		_, ok := compareVersions(bad, "1.0.0")
		assert.False(t, ok, "%q isn't a version", bad)
	}
}

func TestDevelopmentBuilds(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"dev", "(devel)", "1.29.6-0.20261105041738-9a61f25f00aa", "1.29.5+dirty", "1.29.5-dirty",
		"0.0.0-20261105041738-9a61f25f00aa",
	} {
		v := Version{Raw: raw}
		assert.True(t, v.Development(), raw)
		assert.False(t, v.AtLeast("0.0.1"), "%s is never at least anything", raw)
		assert.False(t, v.OlderThan("99.0.0"), "%s is never older", raw)
	}

	for _, raw := range []string{"1.29.5", "1.30.0-rc.1"} {
		assert.False(t, Version{Raw: raw}.Development(), raw)
	}

	assert.True(t, Version{Raw: recordedMagex}.AtLeast("1.29.5"))
	assert.False(t, Version{Raw: "1.11.0"}.AtLeast("1.12.0"))
	assert.True(t, Version{Raw: recordedMagex}.OlderThan("1.30.0"))
	assert.False(t, Version{Raw: "1.30.0"}.OlderThan("1.30.0"))
}

func TestFeaturesFollowTheVersion(t *testing.T) {
	t.Parallel()

	magex := MageX()

	assert.False(t, magex.Has(FeatureExpect, Version{Raw: recordedMagex}))
	assert.True(t, magex.Has(FeatureExpect, Version{Raw: "1.30.0"}))
	assert.False(t, magex.Has(FeatureBumpJSON, Version{Raw: "1.30.0"}))
	assert.True(t, magex.Has(FeatureBumpJSON, Version{Raw: "1.30.1"}))
	assert.False(t, magex.Has(FeatureFastForwardPull, Version{Raw: "1.30.1"}))
	assert.True(t, magex.Has(FeatureFastForwardPull, Version{Raw: "1.31.0"}))
	assert.False(t, magex.Has(FeatureExpect, Version{Raw: "dev"}), "a development build can't say")
	assert.False(t, magex.Has(FeatureStatusJSON, Version{Raw: "9.9.9"}), "another tool's feature")

	gopc := GoPreCommit()

	assert.False(t, gopc.Has(FeatureVerifiedTools, Version{Raw: "1.13.1"}))
	assert.True(t, gopc.Has(FeatureVerifiedTools, Version{Raw: "1.14.0"}))
	assert.False(t, gopc.Has(FeatureRunJSON, Version{Raw: "1.14.0"}))
	assert.True(t, gopc.Has(FeatureRunJSON, Version{Raw: "1.14.1"}))
}

func TestDefinitionsNameTheirReleaseFiles(t *testing.T) {
	t.Parallel()

	magex, gopc := MageX(), GoPreCommit()

	assert.Equal(t, "mage-x_1.29.5_darwin_arm64.tar.gz", magex.Asset("1.29.5", "darwin", "arm64"))
	assert.Equal(t, "mage-x_1.29.5_checksums.txt", magex.Checksums("1.29.5"))
	assert.Equal(t, "go-pre-commit_1.13.1_linux_amd64.tar.gz", gopc.Asset("1.13.1", "linux", "amd64"))
	assert.Equal(t, "go-pre-commit_1.13.1_checksums.txt", gopc.Checksums("1.13.1"))
	assert.Equal(t, map[string]string{"NO_COLOR": "1", "MAGEX_NO_UPDATE_CHECK": "1"}, magex.QuietEnv)
	assert.Equal(t, map[string]string{"NO_COLOR": "1", "GO_PRE_COMMIT_NO_UPDATE_CHECK": "1"}, gopc.QuietEnv)
	assert.Len(t, Definitions(), 2)
}

func FuzzParseVersions(f *testing.F) {
	f.Add([]byte("  Version:      1.29.5\n  Commit:       9a61f25\n"))
	f.Add([]byte("go-pre-commit version 1.13.1 (commit: cf3b488, built: 2026-10-18T04:07:07Z)\n"))
	f.Add([]byte("\x1b[36mVersion:\x1b[0m v1.2.3"))
	f.Add([]byte("go-pre-commit version v1.0.0-rc.1+dirty"))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, out []byte) {
		for _, parse := range []func([]byte) (Version, error){ParseMagexVersion, ParseGoPreCommitVersion} {
			v, err := parse(out)
			if err != nil {
				continue
			}

			if v.Raw == "" {
				t.Fatalf("parsed an empty version from %q", out)
			}

			// Whatever was read compares without panicking.
			_ = v.Development()
			_ = v.AtLeast("1.0.0")
			_ = v.OlderThan("1.0.0")
		}
	})
}
