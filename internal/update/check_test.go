package update_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
	"github.com/mrz1836/nilbox/internal/update"
)

// heldFeed is a feed whose downloads wait until held is closed, or their context ends.
type heldFeed struct {
	*fakeFeed

	held chan struct{}
}

func (f heldFeed) Download(ctx context.Context, version, file string, w io.Writer) error {
	select {
	case <-f.held:
	case <-ctx.Done():
		return ctx.Err()
	}

	return f.fakeFeed.Download(ctx, version, file, w)
}

// cancelingFeed is a feed whose manifest read ends the check's own context, as the app
// quitting midway does, and fails with it.
type cancelingFeed struct {
	update.Feed

	cancel context.CancelFunc
}

func (f cancelingFeed) Manifest(ctx context.Context) ([]byte, error) {
	f.cancel()
	<-ctx.Done()

	return nil, ctx.Err()
}

// uninstallableEntry is the KeyUninstallable list naming m as failed for good.
func uninstallableEntry(m update.Manifest) string {
	return `[{"version":"` + m.Version + `","sha512":"` + m.SHA512 + `","reason":"The downloaded update couldn't be installed."}]`
}

func TestADueCheckThatFindsTheUpdateReadyTellsNothingAgain(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.release(t, next)
	require.NoError(t, r.engine.Tick(t.Context()))
	require.Equal(t, update.StateReady, r.status(t).State)

	r.clock.sleep(update.CheckEvery)
	require.NoError(t, r.engine.Tick(t.Context()))

	assert.Equal(t, int32(2), r.feed.manifests.Load(), "the due check ran")
	assert.Equal(t, int32(1), r.feed.downloads.Load(), "the update ready isn't downloaded again")
	assert.Equal(t, []update.Notice{{Version: next, Ready: true}}, r.notifier.all(), "nor told of again")
	assert.Equal(t, update.StateReady, r.status(t).State)
}

func TestATimedCheckNeverDownloadsAnUpdateThatFailedForGood(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		change func(*update.Config)
		log    bool
	}{
		"with its log kept":   {change: func(*update.Config) {}, log: true},
		"with no logs folder": {change: func(c *update.Config) { c.Logs = "" }},
	} {
		r := newRig(t, tt.change)
		m := r.release(t, next)
		r.memory.put(update.KeyUninstallable, uninstallableEntry(m))

		if r.cfg.Logs != "" {
			testutil.WriteFile(t, filepath.Join(r.cfg.Logs, "update-"+next+".log"), "helper start\n")
		}

		require.NoError(t, r.engine.Tick(t.Context()), name)

		s := r.status(t)
		assert.Equal(t, update.StateAvailable, s.State, name)
		assert.Equal(t, next, s.Latest, "found, so the window can say it didn't install: %s", name)
		assert.Equal(t, update.InstallFailure{
			Version: next, Reason: "The downloaded update couldn't be installed.", Log: tt.log,
		}, s.InstallFailed, name)
		assert.Zero(t, r.feed.downloads.Load(), name)
		assert.Empty(t, r.notifier.all(), name)
	}
}

func TestAnUpdateFoundWithNoNotifierIsStillMarkedTold(t *testing.T) {
	t.Parallel()

	r := newRig(t, func(c *update.Config) { c.Notifier = nil })
	r.release(t, next)

	require.NoError(t, r.engine.Tick(t.Context()))
	assert.Equal(t, update.StateReady, r.status(t).State)
	assert.Equal(t, next, r.memory.get(update.KeyNotified))
}

func TestDownloadWithNothingFoundSaysCheckFirst(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	_, err := r.service.Download(t.Context())
	require.ErrorIs(t, err, update.ErrNothingToDownload)
	assert.Equal(t, "There's no new version to download. Check for updates first.", uierr.Message(err))
	assert.Zero(t, r.feed.downloads.Load())
}

func TestEveryStepWaitingForTheOperationGivesUpOnceItsContextEnds(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		r.release(t, next)
		require.Zero(t, r.engine.Settle(t.Context()))

		gate := make(chan struct{})
		r.feed.gate = gate
		done := make(chan error, 1)

		go func() {
			_, err := r.service.CheckNow(t.Context())
			done <- err
		}()

		// The check holds the operation, waiting on the releases.
		synctest.Wait()

		canceled, cancel := context.WithCancel(t.Context())
		cancel()

		for name, step := range map[string]func() error{
			"a timed check": func() error { return r.engine.Tick(canceled) },
			"a check": func() error {
				_, err := r.service.CheckNow(canceled)

				return err
			},
			"a download": func() error {
				_, err := r.service.Download(canceled)

				return err
			},
			"a restart": func() error {
				_, err := r.service.Restart(canceled, true)

				return err
			},
		} {
			require.ErrorIs(t, step(), context.Canceled, name)
		}

		assert.Equal(t, int32(1), r.feed.manifests.Load(), "none of them read the releases")

		close(gate)
		require.NoError(t, <-done)
		assert.Equal(t, next, r.status(t).Latest, "the check that held the operation finished")
	})
}

func TestNothingChecksOrRestartsWhileTheUpdateDownloads(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		held := make(chan struct{})
		r := newRig(t, func(c *update.Config) {
			feed, ok := c.Feed.(*fakeFeed)
			require.True(t, ok)

			c.Feed = heldFeed{fakeFeed: feed, held: held}
		})
		r.release(t, next)
		require.Zero(t, r.engine.Settle(t.Context()))

		done := make(chan error, 1)

		go func() { done <- r.engine.Tick(t.Context()) }()

		synctest.Wait()
		require.Equal(t, update.StateDownloading, r.status(t).State)

		s, err := r.service.CheckNow(t.Context())
		require.NoError(t, err)
		assert.Equal(t, update.StateDownloading, s.State, "a check while the update downloads returns the status as it is")
		assert.Equal(t, int32(1), r.feed.manifests.Load(), "and reads nothing")

		_, err = r.service.Restart(t.Context(), true)
		require.ErrorIs(t, err, update.ErrBusy)
		assert.Equal(t, "Wait for the download to finish, then restart.", uierr.Message(err))

		close(held)
		require.NoError(t, <-done)
		assert.Equal(t, update.StateReady, r.status(t).State)
	})
}

func TestACheckTheAppStopsMidwayLeavesNoErrorToShow(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	r := newRig(t, func(c *update.Config) { c.Feed = cancelingFeed{Feed: c.Feed, cancel: cancel} })

	require.ErrorIs(t, r.engine.Tick(ctx), context.Canceled)

	s := r.status(t)
	assert.Empty(t, s.LastError, "a check the app stopped didn't fail")
	assert.Empty(t, s.LastChecked, "nor did it reach the releases")
}

func TestACacheThatCantBeReadIsLoggedAndChangesNothingElse(t *testing.T) {
	t.Parallel()

	r, logs := loggedRig(t, func(c *update.Config) { c.Cache = filepath.Join(c.TempDir, "not-a-folder") })
	require.NoError(t, os.WriteFile(r.cfg.Cache, []byte("a file"), 0o600))

	s, err := r.service.CheckNow(t.Context())
	require.NoError(t, err)
	assert.Equal(t, update.StateIdle, s.State)
	assert.Empty(t, s.LastError)
	assert.Contains(t, logs.String(), "could not tidy the updates kept")
	assert.Contains(t, logs.String(), "could not prune the updates kept")
}
