package update_test

import (
	"crypto/ed25519"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/uierr"
	"github.com/mrz1836/nilbox/internal/update"
)

// bareRig is a bare update over fakes: a rehearsal build of running whose releases verify
// with the rig's key, in an Applications folder of its own, with no database anywhere.
type bareRig struct {
	feed      *fakeFeed
	installer *fakeInstaller
	private   ed25519.PrivateKey
	cfg       update.BareConfig
	bare      *update.Bare
}

// newBareRig returns a bare rig, its config changed by each change.
func newBareRig(t *testing.T, change ...func(*update.BareConfig)) *bareRig {
	t.Helper()

	public, private := testKey(7)
	root := t.TempDir()
	apps := filepath.Join(root, "Applications")
	bundle := filepath.Join(apps, brand.Name+".app")
	require.NoError(t, os.MkdirAll(filepath.Join(bundle, "Contents", "MacOS"), 0o700))

	r := &bareRig{feed: newFeed(), installer: &fakeInstaller{}, private: private}
	r.cfg = update.BareConfig{
		Flavor: update.FlavorRehearsal, Current: running, Feed: r.feed, Key: update.EncodeKey(public),
		Cache: filepath.Join(root, "updates"), Bundle: bundle, Applications: apps, Home: filepath.Join(root, "home"),
		Writable: func(string) bool { return true },
	}

	for _, c := range change {
		c(&r.cfg)
	}

	r.bare = update.NewBare(r.cfg)

	return r
}

// release publishes version as the latest release, signed with the rig's key, and returns
// its manifest.
func (r *bareRig) release(t *testing.T, version string) update.Manifest {
	t.Helper()

	m := sign(t, version, zipOf(version), r.private)
	r.feed.publish(t, m, zipOf(version))

	return m
}

// update runs the bare update of m on a goroutine of its own, and waits for it: exited
// reports that it never returned, as an update that reached the installer's Restart
// doesn't, the app exiting inside it; otherwise err is what it returned.
func (r *bareRig) update(t *testing.T, m update.Manifest) (exited bool, err error) {
	t.Helper()

	done := make(chan struct{})
	returned := false

	go func() {
		defer close(done)

		err = r.bare.Update(t.Context(), m, r.installer)
		returned = true
	}()

	<-done

	return !returned, err
}

// beside lists what is beside the bundle, the bundle included.
func (r *bareRig) beside(t *testing.T) []string {
	t.Helper()

	return folderNames(t, filepath.Dir(r.cfg.Bundle))
}

// cached lists the version folders in the cache.
func (r *bareRig) cached(t *testing.T) []string {
	t.Helper()

	return folderNames(t, r.cfg.Cache)
}

// folderNames lists what dir holds, none when it doesn't exist.
func folderNames(t *testing.T, dir string) []string {
	t.Helper()

	listed, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return []string{}
	}

	require.NoError(t, err)

	out := []string{}
	for _, item := range listed {
		out = append(out, item.Name())
	}

	return out
}

func TestTheBareUpdatesWayFollowsTheBuild(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(*update.BareConfig)
		want   update.Way
	}{
		{name: "a build that updates where it runs", change: func(*update.BareConfig) {}, want: update.WayUpdate},
		{
			name:   "a build from source",
			change: func(c *update.BareConfig) { c.Flavor = update.FlavorSource },
			want:   update.WaySource,
		},
		{
			name:   "an unknown flavor, which is a build from source",
			change: func(c *update.BareConfig) { c.Flavor = "" },
			want:   update.WaySource,
		},
		{
			name: "a release outside every Applications folder",
			change: func(c *update.BareConfig) {
				c.Bundle = filepath.Join(filepath.Dir(c.Applications), "Downloads", brand.Name+".app")
			},
			want: update.WayInstall,
		},
		{
			name:   "a release in a folder it can't write",
			change: func(c *update.BareConfig) { c.Writable = func(string) bool { return false } },
			want:   update.WayInstall,
		},
		{
			name:   "a release whose version isn't a release's",
			change: func(c *update.BareConfig) { c.Current = "dev" },
			want:   update.WayInstall,
		},
		{
			name:   "a release with nowhere to keep the download",
			change: func(c *update.BareConfig) { c.Cache = "" },
			want:   update.WayInstall,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newBareRig(t, tt.change)

			assert.Equal(t, tt.want, r.bare.Way())
		})
	}
}

func TestTheBareUpdateFindsANewerRelease(t *testing.T) {
	t.Parallel()

	r := newBareRig(t)
	want := r.release(t, next)

	m, found, err := r.bare.Latest(t.Context())
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, want, m)
	assert.EqualValues(t, 1, r.feed.manifests.Load(), "one read of the manifest")
	assert.Zero(t, r.feed.downloads.Load(), "a check downloads nothing")
}

func TestTheBareUpdateFindsNothingNewer(t *testing.T) {
	t.Parallel()

	t.Run("the latest release is this build", func(t *testing.T) {
		t.Parallel()

		r := newBareRig(t)
		r.release(t, running)

		_, found, err := r.bare.Latest(t.Context())
		require.NoError(t, err)
		assert.False(t, found)
	})

	t.Run("the latest release is older", func(t *testing.T) {
		t.Parallel()

		r := newBareRig(t)
		r.release(t, "0.1.9")

		_, found, err := r.bare.Latest(t.Context())
		require.NoError(t, err)
		assert.False(t, found)
	})

	t.Run("no release has a manifest", func(t *testing.T) {
		t.Parallel()

		r := newBareRig(t)

		_, found, err := r.bare.Latest(t.Context())
		require.NoError(t, err)
		assert.False(t, found)
	})
}

func TestTheBareUpdateRefusesAManifestThatDoesntCheckOut(t *testing.T) {
	t.Parallel()

	r := newBareRig(t)
	_, stranger := testKey(9)
	m := sign(t, next, zipOf(next), stranger)
	r.feed.publish(t, m, zipOf(next))

	_, found, err := r.bare.Latest(t.Context())
	require.ErrorIs(t, err, update.ErrSignature)
	assert.False(t, found)
	assert.Equal(t, "The latest release's update didn't check out, so it wasn't used.", uierr.Message(err))

	exited, err := r.update(t, m)
	assert.False(t, exited)
	require.Error(t, err)
	assert.Zero(t, r.feed.downloads.Load(), "a manifest the key doesn't verify downloads nothing")
}

func TestTheBareUpdateWordsAReleaseItCouldntReach(t *testing.T) {
	t.Parallel()

	r := newBareRig(t)
	r.feed.set(nil, errFakeNetwork)

	_, found, err := r.bare.Latest(t.Context())
	require.ErrorIs(t, err, errFakeNetwork)
	assert.False(t, found)
	assert.Equal(t, "Couldn't reach the releases: check the connection, then try again.", uierr.Message(err))
}

func TestTheBareUpdateInstallsTheReleaseAndRestarts(t *testing.T) {
	t.Parallel()

	r := newBareRig(t)
	m := r.release(t, next)

	exited, err := r.update(t, m)
	require.NoError(t, err)
	assert.True(t, exited, "the app exits inside the installer's Restart")

	installs, restarts := r.installer.calls()
	require.Len(t, installs, 1)
	assert.Equal(t, filepath.Join(r.cfg.Cache, next, m.File), installs[0].zip)
	assert.Equal(t, next, installs[0].version)
	assert.Equal(t, m.Size, installs[0].size)
	assert.Equal(t, m.SHA512, installs[0].sha512)
	assert.Equal(t, 1, restarts)
	assert.Equal(t, []string{next}, r.cached(t))
	assert.EqualValues(t, 1, r.feed.downloads.Load())
}

func TestTheBareUpdateUsesTheDownloadTheCacheHolds(t *testing.T) {
	t.Parallel()

	r := newBareRig(t)
	m := r.release(t, next)

	r.installer.installErr = errFakeRename
	_, err := r.update(t, m)
	require.Error(t, err)
	require.EqualValues(t, 1, r.feed.downloads.Load())

	r.installer.installErr = nil
	exited, err := r.update(t, m)
	require.NoError(t, err)
	assert.True(t, exited)
	assert.EqualValues(t, 1, r.feed.downloads.Load(), "a try again installs the zip downloaded already")
}

func TestABareDownloadThatFailsLeavesTheAppAsItWas(t *testing.T) {
	t.Parallel()

	t.Run("the download fails", func(t *testing.T) {
		t.Parallel()

		r := newBareRig(t)
		m := r.release(t, next)
		r.feed.mu.Lock()
		r.feed.downloadErr = errFakeNetwork
		r.feed.mu.Unlock()

		before := r.beside(t)

		exited, err := r.update(t, m)
		assert.False(t, exited)
		require.ErrorIs(t, err, errFakeNetwork)
		assert.Equal(t, "Couldn't download "+next+": check the connection, then try again.", uierr.Message(err))

		installs, restarts := r.installer.calls()
		assert.Empty(t, installs, "nothing is installed")
		assert.Zero(t, restarts)
		assert.Equal(t, before, r.beside(t), "nothing changes beside the app")
		assert.Empty(t, r.cached(t), "nothing of the download is kept")
	})

	t.Run("the zip doesn't match its manifest", func(t *testing.T) {
		t.Parallel()

		r := newBareRig(t)
		m := r.release(t, next)
		r.feed.publish(t, m, []byte("the zipped bundle of someone else"))
		before := r.beside(t)

		exited, err := r.update(t, m)
		assert.False(t, exited)
		require.ErrorIs(t, err, update.ErrMismatch)
		assert.Equal(t, "Couldn't download "+next+": it didn't match the release's manifest, so it was thrown away.",
			uierr.Message(err))

		installs, _ := r.installer.calls()
		assert.Empty(t, installs)
		assert.Equal(t, before, r.beside(t))
		assert.Empty(t, r.cached(t))
	})
}

func TestABareInstallThatFailsRemovesItsStaging(t *testing.T) {
	t.Parallel()

	t.Run("the staging fails", func(t *testing.T) {
		t.Parallel()

		r := newBareRig(t)
		m := r.release(t, next)
		before := r.beside(t)

		staging := filepath.Join(filepath.Dir(r.cfg.Bundle), "wails-update-5")
		r.installer.onInstall = func() {
			require.NoError(t, os.MkdirAll(filepath.Join(staging, brand.Name+".app"), 0o700))
		}
		r.installer.installErr = errFakeRename

		exited, err := r.update(t, m)
		assert.False(t, exited)
		require.ErrorIs(t, err, errFakeRename)
		assert.Equal(t, "Couldn't get "+next+" ready to install.", uierr.Message(err), "no path in the words")
		assert.NoDirExists(t, staging)
		assert.Equal(t, before, r.beside(t))
	})

	t.Run("the restart fails", func(t *testing.T) {
		t.Parallel()

		r := newBareRig(t)
		m := r.release(t, next)
		before := r.beside(t)

		staging := filepath.Join(filepath.Dir(r.cfg.Bundle), "wails-update-6")
		r.installer.onInstall = func() {
			require.NoError(t, os.MkdirAll(filepath.Join(staging, brand.Name+".app"), 0o700))
		}
		r.installer.restartErr = errFakeRename

		exited, err := r.update(t, m)
		assert.False(t, exited)
		require.ErrorIs(t, err, errFakeRename)
		assert.Equal(t, "Couldn't restart into "+next+".", uierr.Message(err))
		assert.NoDirExists(t, staging)
		assert.Equal(t, before, r.beside(t))
		assert.Equal(t, []string{next}, r.cached(t), "the download is kept for another try")
	})
}

func TestABareRestartWhoseQuitWasHeldReturns(t *testing.T) {
	t.Parallel()

	r := newBareRig(t)
	m := r.release(t, next)
	r.installer.returns = true

	exited, err := r.update(t, m)
	assert.False(t, exited)
	require.NoError(t, err, "the helper waits for the process to exit")
}

func TestTheBareUpdateRefusesWhatItCantInstall(t *testing.T) {
	t.Parallel()

	t.Run("a release no newer than this build", func(t *testing.T) {
		t.Parallel()

		r := newBareRig(t)
		m := r.release(t, running)

		_, err := r.update(t, m)
		require.ErrorIs(t, err, update.ErrNothingToDownload)
		assert.Zero(t, r.feed.downloads.Load())
	})

	t.Run("no installer", func(t *testing.T) {
		t.Parallel()

		r := newBareRig(t)
		m := r.release(t, next)

		err := r.bare.Update(t.Context(), m, nil)
		require.ErrorIs(t, err, update.ErrNoInstaller)
		assert.Zero(t, r.feed.downloads.Load())
	})
}

func TestABareBuildFromSourceReadsNothing(t *testing.T) {
	t.Parallel()

	r := newBareRig(t, func(c *update.BareConfig) { c.Flavor = update.FlavorSource })
	m := r.release(t, next)

	_, found, err := r.bare.Latest(t.Context())
	require.ErrorIs(t, err, update.ErrOff)
	assert.False(t, found)
	assert.Equal(t, "Updates are off in builds made from source.", uierr.Message(err))

	_, err = r.update(t, m)
	require.ErrorIs(t, err, update.ErrOff)

	assert.Zero(t, r.feed.manifests.Load(), "a build from source never checks")
	assert.Zero(t, r.feed.downloads.Load(), "and never downloads")

	installs, restarts := r.installer.calls()
	assert.Empty(t, installs)
	assert.Zero(t, restarts)
}

func TestTheBareUpdateSaysWhereTheAppRuns(t *testing.T) {
	t.Parallel()

	assert.True(t, newBareRig(t).bare.Location().OK, "in Applications")

	away := newBareRig(t, func(c *update.BareConfig) {
		c.Bundle = filepath.Join(filepath.Dir(c.Applications), "Downloads", brand.Name+".app")
	})
	assert.False(t, away.bare.Location().OK, "in Downloads")
}

func TestABareBuildWithNowhereToKeepTheDownloadReadsNothing(t *testing.T) {
	t.Parallel()

	r := newBareRig(t, func(c *update.BareConfig) { c.Cache = "" })
	m := r.release(t, next)

	_, found, err := r.bare.Latest(t.Context())
	require.ErrorIs(t, err, update.ErrOff)
	assert.False(t, found)
	assert.Equal(t, "Updates are off: this build has nowhere to read them from.", uierr.Message(err))

	_, err = r.update(t, m)
	require.ErrorIs(t, err, update.ErrOff)
	assert.Zero(t, r.feed.manifests.Load())
	assert.Zero(t, r.feed.downloads.Load())
}

func TestABareReleaseOutsideApplicationsReadsNothing(t *testing.T) {
	t.Parallel()

	r := newBareRig(t, func(c *update.BareConfig) {
		c.Bundle = filepath.Join(filepath.Dir(c.Applications), "Downloads", brand.Name+".app")
	})
	m := r.release(t, next)

	_, _, err := r.bare.Latest(t.Context())
	require.ErrorIs(t, err, update.ErrLocation)

	_, err = r.update(t, m)
	require.ErrorIs(t, err, update.ErrLocation)
	assert.Zero(t, r.feed.manifests.Load())
	assert.Zero(t, r.feed.downloads.Load())
}
