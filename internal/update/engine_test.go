package update_test

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/uierr"
	"github.com/mrz1836/nilbox/internal/update"
	"github.com/mrz1836/nilbox/internal/updateprefs"
)

func TestASourceBuildIsOffWhateverElseIsSet(t *testing.T) {
	t.Parallel()

	for _, flavor := range []update.Flavor{update.FlavorSource, "", "nightly"} {
		r := newRig(t, func(c *update.Config) { c.Flavor = flavor })
		r.release(t, next)

		s := r.status(t)
		assert.Equal(t, update.FlavorSource, s.Flavor, flavor)
		assert.False(t, s.Enabled)
		assert.Equal(t, update.StateOff, s.State)
		assert.Equal(t, "Updates are off in builds made from source.", s.Off)
		assert.Empty(t, s.ReleaseURL)

		_, err := r.service.CheckNow(t.Context())
		require.ErrorIs(t, err, update.ErrOff)
		assert.Equal(t, s.Off, uierr.Message(err))

		_, err = r.service.Restart(t.Context(), true)
		require.ErrorIs(t, err, update.ErrOff)
		require.NoError(t, r.engine.Tick(t.Context()))
		assert.Zero(t, r.feed.manifests.Load(), "a source build reads nothing")
	}
}

func TestAReleaseTrustsThePinnedKeyAloneAndIsOffWithoutOne(t *testing.T) {
	t.Parallel()

	r := newRig(t, func(c *update.Config) { c.Flavor = update.FlavorRelease })

	if update.ReleaseKey != "" {
		t.Skip("a key is pinned: TestThePinnedReleaseKeyParses covers it")
	}

	s := r.status(t)
	assert.Equal(t, update.FlavorRelease, s.Flavor)
	assert.False(t, s.Enabled, "the rehearsal key the rig hands it is ignored")
	assert.Equal(t, "Updates are off: this build has no release key pinned to check them with.", s.Off)
}

func TestARehearsalNeedsARawKeyAFeedAndAReleaseVersion(t *testing.T) {
	t.Parallel()

	public, private := testKey(7)
	encoded, err := update.EncodePrivateKey(private)
	require.NoError(t, err)

	for name, change := range map[string]func(*update.Config){
		"a private key's PEM block": func(c *update.Config) { c.Key = string(encoded) },
		"a public key's PEM block": func(c *update.Config) {
			c.Key = "-----BEGIN PUBLIC KEY-----\n" + base64.StdEncoding.EncodeToString(public) + "\n-----END PUBLIC KEY-----\n"
		},
		"no key":      func(c *update.Config) { c.Key = "" },
		"a short key": func(c *update.Config) { c.Key = base64.StdEncoding.EncodeToString(public[:16]) },
		"the identity point": func(c *update.Config) {
			c.Key = base64.StdEncoding.EncodeToString(append([]byte{1}, make([]byte, ed25519.PublicKeySize-1)...))
		},
		"no feed":                func(c *update.Config) { c.Feed = nil },
		"no cache":               func(c *update.Config) { c.Cache = "" },
		"a version from source":  func(c *update.Config) { c.Current = "dev" },
		"a version with no core": func(c *update.Config) { c.Current = "0.2" },
	} {
		r := newRig(t, change)
		assert.False(t, r.engine.Enabled(), name)
		assert.NotEmpty(t, r.status(t).Off, name)
	}

	assert.True(t, newRig(t).engine.Enabled(), "the rig's own rehearsal updates")
}

func TestUpToDate(t *testing.T) {
	t.Parallel()

	for name, publish := range map[string]func(*rig){
		"no release":         func(*rig) {},
		"the same version":   func(r *rig) { r.release(t, running) },
		"an older version":   func(r *rig) { r.release(t, "0.1.9") },
		"an earlier release": func(r *rig) { r.release(t, "0.1.0") },
	} {
		r := newRig(t)
		publish(r)

		s, err := r.service.CheckNow(t.Context())
		require.NoError(t, err, name)
		assert.Equal(t, update.StateIdle, s.State, name)
		assert.Empty(t, s.Latest, name)
		assert.Empty(t, s.LastError, name)
		assert.NotEmpty(t, s.LastChecked, name)
		assert.Equal(t, update.ReleaseURL(running), s.ReleaseURL, name)
		assert.Zero(t, r.feed.downloads.Load(), name)
	}
}

func TestATimedCheckDownloadsVerifiesAndTellsOnce(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	m := r.release(t, next)

	require.NoError(t, r.engine.Tick(t.Context()))

	s := r.status(t)
	assert.Equal(t, update.StateReady, s.State)
	assert.Equal(t, next, s.Latest)
	assert.Equal(t, update.ReleaseURL(next), s.ReleaseURL)
	assert.Equal(t, brand.Repository+"/releases/tag/v"+next, s.ReleaseURL)
	assert.Equal(t, []string{next}, r.cached(t))
	assert.Equal(t, []update.Notice{{Version: next, Ready: true}}, r.notifier.all())

	zip := filepath.Join(r.cfg.Cache, next, m.File)
	body, err := os.ReadFile(zip) //nolint:gosec // the test's own cache
	require.NoError(t, err)
	assert.Equal(t, zipOf(next), body)

	manifest, err := os.ReadFile(filepath.Join(r.cfg.Cache, next, update.ManifestName))
	require.NoError(t, err)
	assert.Equal(t, encode(t, m), manifest, "the manifest is kept beside the zip")

	// The next tick, before CheckEvery, reads nothing; a manual check finds the same
	// version and tells nothing again.
	require.NoError(t, r.engine.Tick(t.Context()))
	assert.Equal(t, int32(1), r.feed.manifests.Load())

	_, err = r.service.CheckNow(t.Context())
	require.NoError(t, err)
	assert.Equal(t, int32(1), r.feed.downloads.Load(), "a version downloaded isn't downloaded again")
	assert.Len(t, r.notifier.all(), 1, "each version is told once")
}

func TestJustTellMeStopsAtAvailableAndUpdateDownloads(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.mode(updateprefs.ModeNotify)
	r.release(t, next)

	require.NoError(t, r.engine.Tick(t.Context()))

	s := r.status(t)
	assert.Equal(t, update.StateAvailable, s.State)
	assert.Equal(t, next, s.Latest)
	assert.Zero(t, r.feed.downloads.Load(), "nothing downloads until the user asks")
	assert.Empty(t, r.cached(t))
	assert.Equal(t, []update.Notice{{Version: next, Ready: false}}, r.notifier.all(), "told it is available")

	s, err := r.service.Download(t.Context())
	require.NoError(t, err)
	assert.Equal(t, update.StateReady, s.State)
	assert.Equal(t, []string{next}, r.cached(t))
	assert.Len(t, r.notifier.all(), 1, "the download the user asked for tells nothing")
}

func TestDontCheckMakesNoTimedChecksButCheckNowWorksAndOnlyTells(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.mode(updateprefs.ModeOff)
	r.release(t, next)

	require.NoError(t, r.engine.Tick(t.Context()))
	assert.Zero(t, r.feed.manifests.Load(), "no timed check")

	s, err := r.service.CheckNow(t.Context())
	require.NoError(t, err)
	assert.Equal(t, update.StateAvailable, s.State, "a manual check shows what it found, as Just tell me does")
	assert.Zero(t, r.feed.downloads.Load())
	assert.Empty(t, r.notifier.all(), "the user is looking at the result already")
	assert.Equal(t, next, r.memory.get(update.KeyNotified), "and the version counts as told")

	require.NoError(t, r.engine.Tick(t.Context()))
	assert.Zero(t, r.feed.downloads.Load(), "Don't check downloads nothing on its own")
}

func TestAFailedCheckKeepsTheStateAndSaysWhy(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.release(t, next)
	require.NoError(t, r.engine.Tick(t.Context()))
	require.Equal(t, update.StateReady, r.status(t).State)

	checked := r.memory.get(update.KeyLastCheck)

	r.feed.set(nil, errFakeNetwork)

	s, err := r.service.CheckNow(t.Context())
	require.NoError(t, err, "a failed check is told by the status")
	assert.Equal(t, update.StateReady, s.State, "the ready update stays ready")
	assert.Contains(t, s.LastError, "Couldn't reach the releases")
	assert.Equal(t, checked, s.LastChecked, "only a check that reached the releases counts")

	// The next check that works clears it.
	r.release(t, next)

	s, err = r.service.CheckNow(t.Context())
	require.NoError(t, err)
	assert.Empty(t, s.LastError)
}

func TestADownloadThatDoesntMatchIsThrownAway(t *testing.T) {
	t.Parallel()

	for name, serve := range map[string][]byte{
		"another file of the same size": []byte(strings.Repeat("x", len(zipOf(next)))),
		"a shorter file":                zipOf(next)[:5],
		"a longer file":                 append(zipOf(next), "and more"...),
	} {
		r := newRig(t)
		m := sign(t, next, zipOf(next), r.private)
		r.feed.publish(t, m, serve)

		require.Error(t, r.engine.Tick(t.Context()), name)

		s := r.status(t)
		assert.Equal(t, update.StateAvailable, s.State, name)
		assert.Contains(t, s.LastError, "Couldn't download "+next, name)
		assert.Contains(t, s.LastError, "didn't match the release's manifest", name)
		assert.Empty(t, r.cached(t), "nothing is kept: %s", name)
		assert.Empty(t, r.notifier.all(), name)
	}
}

func TestAFetchErrorKeepsNothing(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.release(t, next)
	r.feed.downloadErr = errFakeNetwork

	require.Error(t, r.engine.Tick(t.Context()))

	s := r.status(t)
	assert.Equal(t, update.StateAvailable, s.State)
	assert.Contains(t, s.LastError, "the network is down")
	assert.Empty(t, r.cached(t))

	// The next look tries again.
	r.feed.downloadErr = nil

	require.NoError(t, r.engine.Tick(t.Context()))
	assert.Equal(t, update.StateReady, r.status(t).State)
	assert.Empty(t, r.status(t).LastError)
}

func TestTheManifestRules(t *testing.T) {
	t.Parallel()

	_, stranger := testKey(9)

	// forged signs m's message as a release would, without Sign's checks of its form.
	forged := func(r *rig, m update.Manifest) update.Manifest {
		m.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(r.private, update.SignedMessage(m)))

		return m
	}

	tests := map[string]struct {
		manifest func(t *testing.T, r *rig) []byte
		words    string
	}{
		"a prerelease": {
			manifest: func(t *testing.T, r *rig) []byte { return encode(t, sign(t, "0.3.0-rc.1", zipOf(next), r.private)) },
			words:    "didn't check out",
		},
		"another platform": {
			manifest: func(t *testing.T, r *rig) []byte {
				m := sign(t, next, zipOf(next), r.private)
				m.Platform, m.File = "linux-amd64", update.FileName("linux-amd64", next)

				return encode(t, forged(r, m))
			},
			words: "didn't check out",
		},
		"a wrong file name": {
			manifest: func(t *testing.T, r *rig) []byte {
				m := sign(t, next, zipOf(next), r.private)
				m.File = "other.zip"

				return encode(t, forged(r, m))
			},
			words: "didn't check out",
		},
		"an oversized size field": {
			manifest: func(t *testing.T, r *rig) []byte {
				m := sign(t, next, zipOf(next), r.private)
				m.Size = update.MaxFileBytes + 1

				return encode(t, forged(r, m))
			},
			words: "didn't check out",
		},
		"another key's signature": {
			manifest: func(t *testing.T, _ *rig) []byte { return encode(t, sign(t, next, zipOf(next), stranger)) },
			words:    "didn't check out",
		},
		"a relabeled version": {
			manifest: func(t *testing.T, r *rig) []byte {
				m := sign(t, next, zipOf(next), r.private)
				m.Version, m.File = later, update.FileName(update.Platform, later)

				return encode(t, m)
			},
			words: "didn't check out",
		},
		"an oversized manifest": {
			manifest: func(t *testing.T, r *rig) []byte {
				m := encode(t, sign(t, next, zipOf(next), r.private))

				return append(m[:len(m)-2], []byte(`, "pad": "`+strings.Repeat("x", update.MaxManifestBytes)+`"}`)...)
			},
			words: "can't be read",
		},
		"not JSON": {
			manifest: func(*testing.T, *rig) []byte { return []byte("<html>not found</html>") },
			words:    "can't be read",
		},
	}

	for name, tt := range tests {
		r := newRig(t)
		r.feed.set(tt.manifest(t, r), nil)

		s, err := r.service.CheckNow(t.Context())
		require.NoError(t, err, name)
		assert.Equal(t, update.StateIdle, s.State, name)
		assert.Contains(t, s.LastError, tt.words, name)
		assert.Empty(t, s.LastChecked, "a manifest refused isn't a check that worked: %s", name)
		assert.Zero(t, r.feed.downloads.Load(), name)
	}
}

func TestANewerReleaseReplacesTheOneWaitingAndAPulledOneIsWithdrawn(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.release(t, next)
	require.NoError(t, r.engine.Tick(t.Context()))
	require.Equal(t, next, r.status(t).Latest)

	r.release(t, later)
	r.clock.sleep(update.CheckEvery)
	require.NoError(t, r.engine.Tick(t.Context()), "checks go on while an update is ready")

	s := r.status(t)
	assert.Equal(t, update.StateReady, s.State)
	assert.Equal(t, later, s.Latest, "the newer release replaces the one waiting")
	assert.Equal(t, []string{later}, r.cached(t), "and the old one is pruned")
	assert.Equal(t, []update.Notice{{Version: next, Ready: true}, {Version: later, Ready: true}}, r.notifier.all())

	// A release whose manifest is pulled reads as up to date, and its update is withdrawn.
	r.feed.set(nil, nil)

	s, err := r.service.CheckNow(t.Context())
	require.NoError(t, err)
	assert.Equal(t, update.StateIdle, s.State)
	assert.Empty(t, r.cached(t))
}

func TestRestartInstallsTheUpdateAfterABackupAndLetsTheQuitThrough(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	m := r.release(t, next)
	require.NoError(t, r.engine.Tick(t.Context()))

	var atRestart struct {
		quit    []event.Event
		pending string
		state   update.State
	}

	r.installer.onRestart = func() {
		atRestart.quit = r.quitEvents()
		atRestart.pending = r.memory.get(update.KeyPendingInstall)
		atRestart.state = r.status(t).State
	}

	_, err := r.service.Restart(t.Context(), false)
	require.NoError(t, err)

	installs, restarts := r.installer.calls()
	assert.Equal(t, []install{{
		zip: filepath.Join(r.cfg.Cache, next, m.File), version: next, size: m.Size, sha512: m.SHA512,
	}}, installs)
	assert.Equal(t, 1, restarts)
	assert.Equal(t, int32(1), r.backup.calls.Load(), "the data is backed up first")

	assert.Equal(t, []event.Event{event.QuitConfirmed{Restarting: true}}, atRestart.quit,
		"the quit is let through before the installer quits the app, with no run going too")
	assert.Equal(t, update.StateInstalling, atRestart.state)

	var pending map[string]any
	require.NoError(t, json.Unmarshal([]byte(atRestart.pending), &pending))
	assert.Equal(t, running, pending["from"])
	assert.Equal(t, next, pending["to"])
	assert.InDelta(t, 4242, pending["pid"], 0, "the app's process names the helper's log")
	assert.NotEmpty(t, pending["at"])
}

func TestRestartRefusesWhileRunsGoUnlessConfirmed(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.release(t, next)
	require.NoError(t, r.engine.Tick(t.Context()))

	release := update.Run{ID: 7, Kind: "release", Title: "Release v1.2.0", Repo: "acme-org/example-repo"}

	r.runs.active.Store(true)
	r.runs.going = []update.Run{release}

	_, err := r.service.Restart(t.Context(), false)
	require.ErrorIs(t, err, update.ErrRunsGoing)
	assert.Equal(t, "Restarting stops Release v1.2.0 in acme-org/example-repo.", uierr.Message(err))

	goingErr, ok := err.(interface{ Detail() any })
	require.True(t, ok)

	detail, jsonErr := json.Marshal(goingErr.Detail())
	require.NoError(t, jsonErr)
	assert.JSONEq(t, `{"runsGoing":[{"id":7,"kind":"release","title":"Release v1.2.0",`+
		`"repo":"acme-org/example-repo","dir":"","stopRefused":""}]}`, string(detail))

	installs, restarts := r.installer.calls()
	assert.Empty(t, installs, "nothing is installed")
	assert.Zero(t, restarts)
	assert.Zero(t, r.backup.calls.Load(), "nothing is backed up")
	assert.Empty(t, r.quitEvents(), "the quit is left alone")
	assert.Equal(t, update.StateReady, r.status(t).State)

	_, err = r.service.Restart(t.Context(), true)
	require.NoError(t, err)
	assert.Equal(t, []event.Event{event.QuitConfirmed{Restarting: true}}, r.quitEvents())

	_, restarts = r.installer.calls()
	assert.Equal(t, 1, restarts, "confirmed, it restarts, stopping the runs")
}

func TestRunsGoingWordsTheRuns(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.release(t, next)
	require.NoError(t, r.engine.Tick(t.Context()))
	r.runs.active.Store(true)

	for want, going := range map[string][]update.Run{
		"Restarting stops the runs going in " + brand.Name + ".": nil,
		"Restarting stops Checks in ~/projects/tool.":            {{Title: "Checks", Dir: "~/projects/tool"}},
		"Restarting stops the 2 runs going in " + brand.Name + ": Release v1.2.0, Pull main.": {
			{Title: "Release v1.2.0"}, {Title: "Pull main"},
		},
	} {
		r.runs.going = going

		_, err := r.service.Restart(t.Context(), false)
		require.ErrorIs(t, err, update.ErrRunsGoing)
		assert.Equal(t, want, uierr.Message(err))
	}
}

func TestABackupThatFailsStopsTheRestart(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.release(t, next)
	require.NoError(t, r.engine.Tick(t.Context()))
	r.backup.err = errFakeDisk

	_, err := r.service.Restart(t.Context(), false)
	require.ErrorIs(t, err, errFakeDisk)
	assert.Equal(t, "Couldn't back up your data first: "+uierr.Message(errFakeDisk), uierr.Message(err))

	installs, restarts := r.installer.calls()
	assert.Empty(t, installs)
	assert.Zero(t, restarts)
	assert.Empty(t, r.memory.get(update.KeyPendingInstall))
	assert.Empty(t, r.quitEvents())

	s := r.status(t)
	assert.Equal(t, update.StateReady, s.State, "back to ready")
	assert.Equal(t, uierr.Message(err), s.LastError)
}

func TestAnInstallOrRestartErrorGoesBackToReady(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		installErr, restartErr error
		quit                   []event.Event
	}{
		"install": {installErr: errFakeDisk, quit: []event.Event{}},
		"restart": {
			restartErr: errFakeHelper,
			quit:       []event.Event{event.QuitConfirmed{Restarting: true}, event.UpdateRestartFailed{}},
		},
	} {
		r := newRig(t)
		r.release(t, next)
		require.NoError(t, r.engine.Tick(t.Context()))

		staging := filepath.Join(filepath.Dir(r.cfg.Bundle), "wails-update-123")
		require.NoError(t, os.MkdirAll(filepath.Join(staging, brand.Name+".app"), 0o700))

		r.installer.installErr, r.installer.restartErr = tt.installErr, tt.restartErr

		_, err := r.service.Restart(t.Context(), false)
		require.Error(t, err, name)
		assert.Contains(t, uierr.Message(err), next, name)

		s := r.status(t)
		assert.Equal(t, update.StateReady, s.State, name)
		assert.Equal(t, uierr.Message(err), s.LastError, name)
		assert.Empty(t, r.memory.get(update.KeyPendingInstall), "the record is cleared: %s", name)
		assert.NoDirExists(t, staging, "the staging folder is removed: %s", name)
		assert.Equal(t, tt.quit, r.quitEvents(), "the shell holds a quit again once one was let through: %s", name)
	}
}

func TestRestartNeedsAnUpdateReadyAndAnInstaller(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	_, err := r.service.Restart(t.Context(), false)
	require.ErrorIs(t, err, update.ErrNotReady)

	r.mode(updateprefs.ModeNotify)
	r.release(t, next)
	require.NoError(t, r.engine.Tick(t.Context()))

	_, err = r.service.Restart(t.Context(), true)
	require.ErrorIs(t, err, update.ErrNotReady, "an update found but not downloaded isn't ready")

	bare := newRig(t)
	bare.engine.Attach(nil)

	_, err = bare.service.Restart(t.Context(), false)
	require.ErrorIs(t, err, update.ErrNoInstaller)
	assert.Equal(t, "This build can't install updates.", uierr.Message(err))
}

func TestLaterHidesTheBannerForADayAndANewerVersionShowsIt(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	s, err := r.service.HideBanner(t.Context())
	require.NoError(t, err)
	assert.False(t, s.BannerHidden, "nothing to hide yet")
	assert.Empty(t, r.memory.get(update.KeyBannerHidden))

	r.release(t, next)
	require.NoError(t, r.engine.Tick(t.Context()))

	s, err = r.service.HideBanner(t.Context())
	require.NoError(t, err)
	assert.True(t, s.BannerHidden)

	r.clock.sleep(update.HideFor - time.Minute)
	assert.True(t, r.status(t).BannerHidden)

	r.clock.sleep(time.Minute)
	assert.False(t, r.status(t).BannerHidden, "a day later it shows again")

	_, err = r.service.HideBanner(t.Context())
	require.NoError(t, err)

	r.release(t, later)
	r.clock.sleep(update.CheckEvery)
	require.NoError(t, r.engine.Tick(t.Context()))
	assert.False(t, r.status(t).BannerHidden, "a newer version shows it again")

	_, err = r.service.HideBanner(t.Context())
	require.NoError(t, err)

	s, err = r.service.ShowBanner(t.Context())
	require.NoError(t, err)
	assert.False(t, s.BannerHidden, "the notification's click lifts Later")
}

func TestStatusJSONIsTheFrontendsContract(t *testing.T) {
	t.Parallel()

	data, err := json.Marshal(update.Status{})
	require.NoError(t, err)

	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &fields))

	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}

	assert.ElementsMatch(t, []string{
		"flavor", "enabled", "off", "current", "mode", "state", "checking", "progress", "latest", "releaseUrl",
		"lastChecked", "lastError", "bannerHidden", "justUpdated", "installFailed", "location",
	}, names)
	assert.JSONEq(t, `{"version":"","reason":"","log":false}`, string(fields["installFailed"]))
	assert.JSONEq(t, `{"ok":false,"kind":"","target":""}`, string(fields["location"]))
	assert.NotContains(t, string(data), "null", "no null list or map reaches the window")
}

func TestStatusOfAFreshRehearsal(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	// The rig's Applications folder is a temporary one, so it counts as elsewhere.
	assert.Equal(t, update.Status{
		Flavor: update.FlavorRehearsal, Enabled: true, Current: running, Mode: updateprefs.ModeDownload,
		State: update.StateIdle, ReleaseURL: update.ReleaseURL(running),
		Location: update.Location{Kind: update.LocationElsewhere, Target: "/Applications/" + brand.Name + ".app"},
	}, r.status(t))
}

func TestAckUpdatedForgetsTheToast(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.memory.put(update.KeyLastRunRelease, "0.1.0")
	r.engine.Settle(t.Context())
	require.Equal(t, running, r.status(t).JustUpdated)

	s, err := r.service.AckUpdated(t.Context())
	require.NoError(t, err)
	assert.Empty(t, s.JustUpdated)
}

func TestEveryChangeTellsTheWindow(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.release(t, next)
	require.NoError(t, r.engine.Tick(t.Context()))

	assert.Contains(t, r.events.All(), event.Event(event.UpdatesUpdated{}))

	for _, e := range r.events.All() {
		_, ok := e.(event.UpdatesUpdated)
		assert.True(t, ok, "a check and a download tell only that the update changed: %T", e)
	}
}
