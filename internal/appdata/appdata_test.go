package appdata_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/brand"
)

// env returns a getenv func backed by a fixed map.
func env(vars map[string]string) func(string) string {
	return func(key string) string { return vars[key] }
}

func TestDirDarwin(t *testing.T) {
	t.Parallel()

	home := filepath.Join("/", "tmp", "home-darwin")

	got, err := appdata.Dir("darwin", home, env(nil))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "Library", "Application Support", brand.DataDirName), got)

	_, err = appdata.Dir("darwin", "", env(nil))
	require.ErrorIs(t, err, appdata.ErrNoHomeDir)
}

func TestDirLinuxXDG(t *testing.T) {
	t.Parallel()

	xdg := filepath.Join("/", "tmp", "xdg-data")
	home := filepath.Join("/", "tmp", "home-linux")

	got, err := appdata.Dir("linux", home, env(map[string]string{"XDG_DATA_HOME": xdg}))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(xdg, brand.Slug), got)

	// XDG_DATA_HOME alone is enough; no home directory is needed.
	got, err = appdata.Dir("linux", "", env(map[string]string{"XDG_DATA_HOME": xdg}))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(xdg, brand.Slug), got)
}

func TestDirLinuxDefault(t *testing.T) {
	t.Parallel()

	home := filepath.Join("/", "tmp", "home-linux")

	tests := []struct {
		name string
		vars map[string]string
	}{
		{name: "unset", vars: nil},
		{name: "empty", vars: map[string]string{"XDG_DATA_HOME": ""}},
		{name: "relative is ignored", vars: map[string]string{"XDG_DATA_HOME": "relative/data"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := appdata.Dir("linux", home, env(tt.vars))
			require.NoError(t, err)
			assert.Equal(t, filepath.Join(home, ".local", "share", brand.Slug), got)
		})
	}

	_, err := appdata.Dir("linux", "", env(nil))
	require.ErrorIs(t, err, appdata.ErrNoHomeDir)
}

func TestDirWindows(t *testing.T) {
	t.Parallel()

	appData := filepath.Join("C:", "Users", "example", "AppData", "Roaming")

	got, err := appdata.Dir("windows", "", env(map[string]string{"APPDATA": appData}))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(appData, brand.DataDirName), got)

	_, err = appdata.Dir("windows", filepath.Join("C:", "Users", "example"), env(nil))
	require.ErrorIs(t, err, appdata.ErrNoAppDataEnv)
}

func TestDirUnsupported(t *testing.T) {
	t.Parallel()

	for _, goos := range []string{"plan9", "js", ""} {
		t.Run("goos="+goos, func(t *testing.T) {
			t.Parallel()

			_, err := appdata.Dir(goos, filepath.Join("/", "tmp", "home"), env(nil))
			require.ErrorIs(t, err, appdata.ErrUnsupportedOS)
		})
	}
}

func TestNewPaths(t *testing.T) {
	t.Parallel()

	root := filepath.Join("/", "tmp", "data-root")
	p := appdata.NewPaths(root)

	assert.Equal(t, root, p.Root)
	assert.Equal(t, filepath.Join(root, brand.Slug+".db"), p.DB)
	assert.Equal(t, filepath.Join(root, "prompts.json"), p.Prompts)
	assert.Equal(t, filepath.Join(root, "launch"), p.Launch)
	assert.Equal(t, filepath.Join(root, "logs"), p.Logs)
}

func TestResolve(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")

	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", filepath.Join(home, "AppData"))
	}

	p, err := appdata.Resolve()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		require.ErrorIs(t, err, appdata.ErrUnsupportedOS)

		return
	}

	require.NoError(t, err)

	want, err := appdata.Dir(runtime.GOOS, home, os.Getenv)
	require.NoError(t, err)
	assert.Equal(t, appdata.NewPaths(want), p)
}

func TestResolveWithoutHome(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("only darwin and linux derive the data directory from HOME")
	}

	t.Setenv("HOME", "")
	t.Setenv("XDG_DATA_HOME", "")

	_, err := appdata.Resolve()
	require.ErrorIs(t, err, appdata.ErrNoHomeDir)
}

func TestEnsureCreatesDirectories(t *testing.T) {
	t.Parallel()

	p := appdata.NewPaths(filepath.Join(t.TempDir(), "nested", "data"))

	require.NoError(t, p.Ensure())

	for _, dir := range []string{p.Root, p.Launch, p.Logs} {
		info, err := os.Stat(dir)
		require.NoError(t, err)
		assert.True(t, info.IsDir(), dir)
		assert.Equal(t, os.FileMode(0o700), info.Mode().Perm(), dir)
	}

	// A second call is a no-op on existing directories.
	require.NoError(t, p.Ensure())

	// The database file itself is not created, only its directory.
	_, err := os.Stat(p.DB)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestEnsureReportsBlockedPath(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	blocked := filepath.Join(parent, "data")

	// A regular file where the root directory should be blocks creation.
	require.NoError(t, os.WriteFile(blocked, []byte("x"), 0o600))

	err := appdata.NewPaths(blocked).Ensure()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create data directory")
}
