//go:build desktop

package main

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/brand"
)

// stamp sets the package's flavor, as the linker would, until the test ends.
func stamp(t *testing.T, f string) {
	t.Helper()

	original := flavor
	flavor = f

	t.Cleanup(func() { flavor = original })
}

//nolint:paralleltest // sets the package's flavor, which the linker stamps
func TestBuildFlavor(t *testing.T) {
	tests := []struct {
		flavor string
		want   string
	}{
		{flavor: "", want: "source"},
		{flavor: "release", want: "release"},
		{flavor: "rehearsal", want: "rehearsal"},
	}

	for _, tt := range tests {
		stamp(t, tt.flavor)

		assert.Equal(t, tt.want, buildFlavor(), "flavor %q", tt.flavor)
	}
}

// A rehearsal build, which the update it rehearses opens with no arguments, finds its own
// data beside the user's, and never the user's, whatever its arguments say. It sets HOME,
// so it runs alone, as it must, since it sets the package's flavor too.
func TestARehearsalNeverRunsOnTheUsersData(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")

	users, err := appdata.Resolve()
	require.NoError(t, err)

	sandbox := filepath.Join(t.TempDir(), "sandbox")

	stamp(t, "rehearsal")

	for _, args := range [][]string{
		nil,
		{appdata.DataDirFlag + "=" + users.Root},
		{appdata.DataDirFlag + "=" + sandbox},
		{appdata.DataDirFlag + "=relative"},
	} {
		got, pathsErr := dataPaths(args)
		require.NoError(t, pathsErr, "args %q", args)

		assert.Equal(t, filepath.Join(filepath.Dir(users.Root), brand.RehearsalDataDirName), got.Root, "args %q", args)
		assert.NotEqual(t, users.Root, got.Root, "args %q", args)
		assert.NotEqual(t, users.DB, got.DB, "args %q", args)
		assert.Empty(t, sandboxDir(args), "it opens again with no arguments: args %q", args)
		assert.True(t, rehearsal())
	}

	stamp(t, "release")

	got, err := dataPaths(nil)
	require.NoError(t, err)
	assert.Equal(t, users, got, "a release runs on the user's own data")

	got, err = dataPaths([]string{appdata.DataDirFlag + "=" + sandbox})
	require.NoError(t, err)
	assert.Equal(t, sandbox, got.Root, "and on a sandbox's when named")
	assert.Equal(t, sandbox, sandboxDir([]string{appdata.DataDirFlag + "=" + sandbox}))
	assert.False(t, rehearsal())
}
