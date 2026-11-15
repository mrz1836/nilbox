//go:build integration

package desktop_test

import (
	"bytes"
	"crypto/ed25519"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/desktop"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/uierr"
	"github.com/mrz1836/nilbox/internal/update"
)

// quarantine is the extended attribute macOS marks a download with, which a move leaves out.
const quarantine = "com.apple.quarantine"

// signedBundle makes an app bundle at bundle as a build has one, the app's own Info.plist
// beside a program (a copy of /usr/bin/true), signs it ad hoc with the real codesign, and
// marks every file with a download's quarantine, as a bundle opened from Downloads has.
func signedBundle(t *testing.T, bundle string) {
	t.Helper()

	plist, err := os.ReadFile(filepath.Join("..", "..", "build", "darwin", "Info.plist"))
	require.NoError(t, err)

	program, err := os.ReadFile("/usr/bin/true")
	require.NoError(t, err)

	require.NoError(t, os.MkdirAll(filepath.Join(bundle, "Contents", "MacOS"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(bundle, "Contents", "Info.plist"), plist, 0o600))          //nolint:gosec // the test's own bundle
	require.NoError(t, os.WriteFile(filepath.Join(bundle, "Contents", "MacOS", brand.Slug), program, 0o700)) //nolint:gosec // a program

	_, stderr, err := (execpath.Exec{}).Run(t.Context(), desktop.CodesignBin, "--force", "--sign", "-", bundle)
	require.NoError(t, err, string(stderr))

	mark := []byte("0081;6560a000;Safari;")
	for _, path := range []string{
		bundle, filepath.Join(bundle, "Contents", "Info.plist"),
		filepath.Join(bundle, "Contents", "MacOS", brand.Slug),
	} {
		require.NoError(t, unix.Setxattr(path, quarantine, mark, 0))
	}
}

// moveRig is the real update engine over a bundle in a scratch Downloads folder, with a
// scratch Applications folder and home beside it, and the real Mover attached: ditto and
// codesign run through execpath, and the shell quits a fake app.
type moveRig struct {
	r       *rig
	shell   *desktop.Shell
	service *update.Service
	bundle  string
	apps    string
	home    string
}

func newMoveRig(t *testing.T, root string) moveRig {
	t.Helper()

	m := moveRig{
		r: newRig(), bundle: filepath.Join(root, "Downloads", brand.Name+".app"),
		apps: filepath.Join(root, "Applications"), home: filepath.Join(root, "home"),
	}
	require.NoError(t, os.MkdirAll(m.apps, 0o700))
	require.NoError(t, os.MkdirAll(m.home, 0o700))
	signedBundle(t, m.bundle)

	_, private, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	engine := update.NewEngine(update.Config{
		Flavor: update.FlavorRehearsal, Current: "0.0.1", Feed: update.FolderFeed{Dir: filepath.Join(root, "feed")},
		Key: update.EncodeKey(update.PublicKeyOf(private)), Memory: &memory{values: map[string]string{}},
		Cache: filepath.Join(root, "updates"), Logs: filepath.Join(root, "logs"), TempDir: root,
		Bundle: m.bundle, Home: m.home, Applications: m.apps,
	})
	m.shell = m.r.shell()
	engine.AttachMover(desktop.NewMover(execpath.Exec{}, m.shell))
	m.service = update.NewService(engine)

	return m
}

// TestMoveToApplicationsIntegration moves a bundle signed ad hoc into a scratch
// Applications folder with the real ditto and codesign, under the temporary folder alone:
// the copy is whole, checks out, carries no quarantine, and is queued to open once the app
// quits, while the bundle that ran is left as it was. A copy that fails its check is
// removed, and a read-only Applications folder sends the copy home. Nothing is opened, and
// no real Applications folder is touched.
func TestMoveToApplicationsIntegration(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "darwin" {
		t.Skip("ditto and codesign are macOS's")
	}

	t.Run("a move that goes ahead", func(t *testing.T) {
		t.Parallel()

		m := newMoveRig(t, t.TempDir())
		target := filepath.Join(m.apps, brand.Name+".app")

		status, err := m.service.Status(t.Context())
		require.NoError(t, err)
		require.True(t, status.Location.Movable)
		require.Equal(t, target, status.Location.Target)

		start := time.Now()
		got, err := m.service.MoveToApplications(t.Context())
		require.NoError(t, err)
		t.Logf("moved in %v: %+v", time.Since(start).Round(time.Millisecond), got)

		assert.Equal(t, update.MoveResult{Outcome: update.MoveMoved, Target: target}, got)

		_, stderr, err := (execpath.Exec{}).Run(t.Context(), desktop.VerifyArgs(target)[0], desktop.VerifyArgs(target)[1:]...)
		require.NoError(t, err, "the copy checks out: %s", stderr)

		want, err := os.ReadFile(filepath.Join(m.bundle, "Contents", "MacOS", brand.Slug))
		require.NoError(t, err)
		copied, err := os.ReadFile(filepath.Join(target, "Contents", "MacOS", brand.Slug)) //nolint:gosec // the test's own copy
		require.NoError(t, err)
		assert.True(t, bytes.Equal(want, copied), "the program is copied whole")

		for _, path := range []string{
			target, filepath.Join(target, "Contents", "Info.plist"),
			filepath.Join(target, "Contents", "MacOS", brand.Slug),
		} {
			_, err = unix.Getxattr(path, quarantine, nil)
			require.ErrorIs(t, err, unix.ENOATTR, "the copy carries no quarantine: %s", path)
		}

		_, err = unix.Getxattr(filepath.Join(m.bundle, "Contents", "MacOS", brand.Slug), quarantine, nil)
		require.NoError(t, err, "the bundle that ran is left as it was")

		require.Eventually(t, func() bool { return m.r.app.quitCount() == 1 }, 5*time.Second, 10*time.Millisecond,
			"the app quits the normal way")

		argv, ok := m.shell.RelaunchCommand(filepath.Join(m.bundle, "Contents", "MacOS", brand.Slug), "")
		require.True(t, ok)
		assert.Equal(t, []string{desktop.OpenBin, "-n", target}, argv, "and the copy opens once it has")

		// Moving again finds the copy, and Open it checks it with the real codesign.
		got, err = m.service.MoveToApplications(t.Context())
		require.NoError(t, err)
		assert.Equal(t, update.MoveExists, got.Outcome)

		got, err = m.service.OpenInApplications(t.Context())
		require.NoError(t, err)
		assert.Equal(t, update.MoveOpening, got.Outcome)

		require.NoError(t, m.shell.Stop())
	})

	t.Run("a copy that fails its check is removed", func(t *testing.T) {
		t.Parallel()

		m := newMoveRig(t, t.TempDir())
		// A file added after signing breaks the bundle's seal, so its copy fails the check.
		require.NoError(t, os.MkdirAll(filepath.Join(m.bundle, "Contents", "Resources"), 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(m.bundle, "Contents", "Resources", "added.txt"), []byte("x"), 0o600))

		_, err := m.service.MoveToApplications(t.Context())
		require.ErrorIs(t, err, update.ErrMoveFailed)
		t.Logf("refused: %s", uierr.Message(err))
		assert.Contains(t, uierr.Message(err), "didn't pass its signature check, so nothing was moved")

		listed, err := os.ReadDir(m.apps)
		require.NoError(t, err)
		assert.Empty(t, listed, "the copy is removed, and nothing else was there")
		assert.Zero(t, m.r.app.quitCount(), "nothing quits")
		assert.FileExists(t, filepath.Join(m.bundle, "Contents", "Resources", "added.txt"), "the bundle that ran stays")

		require.NoError(t, m.shell.Stop())
	})

	t.Run("a real build", func(t *testing.T) {
		t.Parallel()

		// INTEGRATION_APP_BUNDLE names a build of the app to move, such as one magex
		// app:rehearse made; it is copied into the scratch folder first and never touched.
		build := os.Getenv("INTEGRATION_APP_BUNDLE")
		if build == "" {
			t.Skip("INTEGRATION_APP_BUNDLE names no build to move")
		}

		m := newMoveRig(t, t.TempDir())
		require.NoError(t, os.RemoveAll(m.bundle))

		_, stderr, err := (execpath.Exec{}).Run(t.Context(), desktop.DittoBin, build, m.bundle)
		require.NoError(t, err, string(stderr))
		require.NoError(t, unix.Setxattr(filepath.Join(m.bundle, "Contents", "MacOS", brand.Slug), quarantine,
			[]byte("0081;6560a000;Safari;"), 0))

		start := time.Now()
		got, err := m.service.MoveToApplications(t.Context())
		require.NoError(t, err)
		t.Logf("moved %s in %v", build, time.Since(start).Round(time.Millisecond))
		assert.Equal(t, update.MoveMoved, got.Outcome)

		_, err = unix.Getxattr(filepath.Join(got.Target, "Contents", "MacOS", brand.Slug), quarantine, nil)
		require.ErrorIs(t, err, unix.ENOATTR, "the copy carries no quarantine")

		require.NoError(t, m.shell.Stop())
	})

	t.Run("a read-only Applications folder sends the copy home", func(t *testing.T) {
		t.Parallel()

		if os.Geteuid() == 0 {
			t.Skip("root can write anywhere")
		}

		m := newMoveRig(t, t.TempDir())
		require.NoError(t, os.Chmod(m.apps, 0o500)) //nolint:gosec // the test's own folder, made read-only

		t.Cleanup(func() { _ = os.Chmod(m.apps, 0o700) }) //nolint:gosec // put back for t.TempDir's removal

		got, err := m.service.MoveToApplications(t.Context())
		require.NoError(t, err)
		assert.Equal(t, update.MoveResult{Outcome: update.MoveMoved, Target: "~/Applications/" + brand.Name + ".app"}, got)
		assert.DirExists(t, filepath.Join(m.home, "Applications", brand.Name+".app", "Contents", "MacOS"))

		require.NoError(t, m.shell.Stop())
	})
}
