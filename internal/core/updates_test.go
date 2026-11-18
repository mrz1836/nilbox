package core

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/appinfo"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/notify"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/update"
	"github.com/mrz1836/nilbox/internal/updateprefs"
)

// releaseHost answers GitHub's release download links from files, by path, and records
// every request; any other path is a 404.
type releaseHost struct {
	files    map[string]string
	requests []*http.Request
}

func (h *releaseHost) RoundTrip(r *http.Request) (*http.Response, error) {
	h.requests = append(h.requests, r)

	body, ok := h.files[r.URL.Path]
	status := http.StatusOK

	if !ok {
		status = http.StatusNotFound
	}

	return &http.Response{
		StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: r,
	}, nil
}

// releaseClient is a GitHub client whose requests host answers.
func releaseClient(host *releaseHost) *github.Client {
	return &github.Client{HTTP: &http.Client{Transport: host}, UserAgent: "test"}
}

func TestAReleaseReadsGitHubAndTrustsThePinnedKeyAlone(t *testing.T) {
	t.Parallel()

	client := releaseClient(&releaseHost{})

	opts := Options{Flavor: update.FlavorRelease, UpdateFeed: "/srv/feed", UpdateKey: "a rehearsal's key"}
	flavor, feed, key := updateSource(opts, client)
	assert.Equal(t, update.FlavorRelease, flavor)
	assert.Equal(t, githubFeed{client: client}, feed, "the folder a rehearsal reads is ignored")
	assert.Empty(t, key, "the engine trusts update.ReleaseKey alone")

	opts.Flavor = update.FlavorRehearsal
	flavor, feed, key = updateSource(opts, client)
	assert.Equal(t, update.FlavorRehearsal, flavor)
	assert.Equal(t, update.FolderFeed{Dir: "/srv/feed"}, feed)
	assert.Equal(t, "a rehearsal's key", key)

	opts.UpdateFeed = ""
	_, feed, _ = updateSource(opts, client)
	assert.Nil(t, feed, "a rehearsal with no folder reads nothing")

	for _, other := range []update.Flavor{update.FlavorSource, "", "nightly"} {
		opts.Flavor = other
		flavor, feed, key = updateSource(opts, client)
		assert.Equal(t, update.FlavorSource, flavor, other)
		assert.Nil(t, feed, other)
		assert.Empty(t, key, other)
	}
}

func TestTheGitHubFeedReadsTheLatestManifestAndThePinnedZip(t *testing.T) {
	t.Parallel()

	repoPath := strings.TrimPrefix(brand.Repository, "https://github.com")
	file := update.FileName(update.Platform, "0.3.0")
	host := &releaseHost{files: map[string]string{
		repoPath + "/releases/latest/download/" + update.ManifestName: `{"version":"0.3.0"}`,
		repoPath + "/releases/download/v0.3.0/" + file:                "the zip",
	}}
	feed := githubFeed{client: releaseClient(host)}

	manifest, err := feed.Manifest(t.Context())
	require.NoError(t, err)
	assert.JSONEq(t, `{"version":"0.3.0"}`, string(manifest))

	var zip bytes.Buffer
	require.NoError(t, feed.Download(t.Context(), "0.3.0", file, &zip))
	assert.Equal(t, "the zip", zip.String())

	for _, r := range host.requests {
		assert.Equal(t, "github.com", r.URL.Host)
		assert.Empty(t, r.Header.Get("Authorization"), "no token goes with a release download")
	}

	// A release that hasn't attached its manifest reads as no release at all.
	delete(host.files, repoPath+"/releases/latest/download/"+update.ManifestName)

	_, err = feed.Manifest(t.Context())
	require.ErrorIs(t, err, update.ErrNoRelease)

	require.Error(t, feed.Download(t.Context(), "0.3.1", update.FileName(update.Platform, "0.3.1"), &zip))
}

func TestTheUpdateNotificationSaysReadyOrAvailable(t *testing.T) {
	t.Parallel()

	assert.Equal(t, notify.Entry{
		Kind: notify.KindUpdateReady, Title: brand.Name + " 0.3.0 is ready", Body: "Restart to update when you're ready.",
	}, updateEntry(update.Notice{Version: "0.3.0", Ready: true}))

	assert.Equal(t, notify.Entry{
		Kind: notify.KindUpdateReady, Title: brand.Name + " 0.3.0 is available", Body: "Update when you're ready.",
	}, updateEntry(update.Notice{Version: "0.3.0"}), "an entry with no URL brings the window forward")
}

func TestUpdatesChangedWakesTheUpdateCheckAndStaysInGo(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	check := &countWakes{}

	(&followSwitch{sink: events, updates: check}).Emit(event.UpdatesChanged{})
	assert.Equal(t, int32(1), check.n.Load(), "the check follows the new choice at once")
	assert.Equal(t, []event.Event{event.UpdatesUpdated{}}, events.All(), "the window reads the update again")

	(&followSwitch{sink: events}).Emit(event.UpdatesChanged{})
	assert.Len(t, events.All(), 2, "with no check to wake, the window still hears it")
}

func TestABugReportWordsTheUpdates(t *testing.T) {
	t.Parallel()

	report := joined([]appinfo.StateSection{updatesSection(update.Status{
		Flavor: update.FlavorRelease, Enabled: true, Mode: updateprefs.ModeNotify, State: update.StateReady,
		Latest: "0.3.0", InstallFailed: update.InstallFailure{Version: "0.2.9", Reason: "Rename /srv/private-user/x"},
		Location: update.Location{OK: true, Kind: update.LocationApplications, Target: "/srv/private-user/Applications"},
	})})

	assert.Contains(t, report, "Updates: just tell me · release build · ready, 0.3.0 · last update: 0.2.9 didn't install\n")
	assert.Contains(t, report, "Location: applications · can update there: yes\n")
	assert.NotContains(t, report, "private-user", "no path is written out")

	source := joined([]appinfo.StateSection{updatesSection(update.Status{
		Flavor: update.FlavorSource, Mode: updateprefs.ModeDownload, State: update.StateOff,
		Location: update.Location{Kind: update.LocationElsewhere},
	})})
	assert.Contains(t, source, "Updates: download it and tell me · source build · off\n")
}

// stagedInstall records the install a restart asks for, and refuses to restart.
type stagedInstall struct {
	version string
}

func (s *stagedInstall) Install(_ context.Context, _, version string, _ int64, _ string) error {
	s.version = version

	return nil
}

func (*stagedInstall) Restart(context.Context) error { return errStaged }

// errStaged is the restart the test's installer refuses.
var errStaged = errors.New("fake: the helper never stood by")

func TestARehearsalCoreUpdatesFromItsFolderAfterABackup(t *testing.T) {
	t.Parallel()

	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))
	feed := t.TempDir()
	zip := []byte("the zipped bundle")
	sum := sha512.Sum512(zip)

	m, err := update.Sign(update.Manifest{
		Version: "0.3.0", Platform: update.Platform, File: update.FileName(update.Platform, "0.3.0"),
		Size: int64(len(zip)), SHA512: hex.EncodeToString(sum[:]),
	}, private)
	require.NoError(t, err)

	body, err := update.Encode(m)
	require.NoError(t, err)
	testutil.WriteFile(t, filepath.Join(feed, update.ManifestName), string(body))
	testutil.WriteFile(t, filepath.Join(feed, m.File), string(zip))

	opts := testOptions(t)
	opts.Flavor, opts.Release, opts.UpdateFeed = update.FlavorRehearsal, "0.2.0", feed
	opts.UpdateKey = update.EncodeKey(update.PublicKeyOf(private))

	// Only a copy in an Applications folder updates itself.
	opts.Bundle = filepath.Join(opts.Home, "Applications", brand.Name+".app")
	require.NoError(t, os.MkdirAll(opts.Bundle, 0o700))

	c := bootstrap(t, opts)
	installer := &stagedInstall{}
	c.UpdateEngine.Attach(installer)

	s, err := c.Updates.CheckNow(t.Context())
	require.NoError(t, err)
	require.Equal(t, update.StateAvailable, s.State, s.LastError)

	s, err = c.Updates.Download(t.Context())
	require.NoError(t, err)
	require.Equal(t, update.StateReady, s.State)
	assert.FileExists(t, filepath.Join(opts.Paths.Updates, "0.3.0", m.File), "kept in the data folder's updates")

	_, err = c.Updates.Restart(t.Context(), false)
	require.ErrorIs(t, err, errStaged)
	assert.Equal(t, "0.3.0", installer.version)

	snapshots, err := store.Snapshots(opts.Paths.DB)
	require.NoError(t, err)
	require.Len(t, snapshots, 1, "the database was backed up before the install")
	assert.Equal(t, store.SnapshotBackup, snapshots[0].Kind)
}

// A rehearsal updates itself from the folder magex app:rehearse builds it into, beside its
// feed; a release with the same bundle and options never does.
func TestOnlyARehearsalUpdatesItselfBesideItsFeed(t *testing.T) {
	t.Parallel()

	for flavor, want := range map[update.Flavor]bool{update.FlavorRehearsal: true, update.FlavorRelease: false} {
		t.Run(string(flavor), func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			opts := testOptions(t)
			opts.Flavor, opts.Release = flavor, "0.2.0"
			opts.UpdateFeed = filepath.Join(root, "feed")
			opts.UpdateKey = update.EncodeKey(update.PublicKeyOf(ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))))
			opts.Bundle = filepath.Join(root, "A", brand.Name+".app")
			require.NoError(t, os.MkdirAll(opts.Bundle, 0o700))

			s, err := bootstrap(t, opts).Updates.Status(t.Context())
			require.NoError(t, err)

			assert.Equal(t, want, s.Location.OK)
			assert.Equal(t, update.LocationElsewhere, s.Location.Kind)
		})
	}
}

func TestABuiltCoreKeepsASourceBuildsUpdatesOff(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, testOptions(t))

	s, err := c.Updates.Status(t.Context())
	require.NoError(t, err)
	assert.Equal(t, update.FlavorSource, s.Flavor)
	assert.False(t, s.Enabled)

	runs := updateRuns{engine: c.Runs, history: c.ScriptRuns}
	assert.False(t, runs.Active())

	going, err := runs.Going(t.Context())
	require.NoError(t, err)
	assert.Empty(t, going)
	assert.NotNil(t, going)
}

func TestAutopilotChangedWakesTheEngineAndTellsTheShell(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	engine := &countWakes{}

	(&followSwitch{sink: events, autopilot: engine}).Emit(event.AutopilotChanged{})
	assert.Equal(t, int32(1), engine.n.Load(), "the engine runs a cycle under the new settings")
	assert.Equal(t, []event.Event{event.AutopilotUpdated{}}, events.All(),
		"the menu bar's Pause row and the window read Autopilot again, the engine's cycle or not")

	(&followSwitch{sink: events}).Emit(event.AutopilotChanged{})
	assert.Len(t, events.All(), 2, "with no engine to wake, as in a copy that doesn't hold it, they still hear it")
}

// dataFiles reads every file under root but the updates folder: each one's path, relative
// to root, and its contents' SHA-256.
func dataFiles(t *testing.T, root, updates string) map[string]string {
	t.Helper()

	out := map[string]string{}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case path == updates:
			return filepath.SkipDir
		case d.IsDir():
			return nil
		}

		body, readErr := os.ReadFile(path) //nolint:gosec // the test's own data folder
		if readErr != nil {
			return readErr
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}

		sum := sha256.Sum256(body)
		out[rel] = hex.EncodeToString(sum[:])

		return nil
	})
	require.NoError(t, err)

	return out
}

// The update the startup dialog runs, when a newer build saved the data, finds, downloads,
// checks, and installs the release without opening the database: every file of the data
// folder but the updates' is as it was, the database's own -wal and -shm included, and no
// backup is taken.
func TestTheBareUpdateLeavesTheNewerDataUntouched(t *testing.T) {
	t.Parallel()

	opts := testOptions(t)

	c, err := Bootstrap(t.Context(), opts)
	require.NoError(t, err)

	_, err = c.DB.ExecContext(t.Context(), `INSERT INTO schema_migrations (version, name, checksum, applied_at)
VALUES (9999, 'from_the_future', 'abc', '2026-01-01T00:00:00Z')`)
	require.NoError(t, err)
	require.NoError(t, c.Close())

	_, err = Bootstrap(t.Context(), opts)
	require.ErrorIs(t, err, store.ErrNewerSchema)

	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))
	feed := t.TempDir()
	zip := []byte("the zipped bundle")
	sum := sha512.Sum512(zip)

	m, err := update.Sign(update.Manifest{
		Version: "0.3.0", Platform: update.Platform, File: update.FileName(update.Platform, "0.3.0"),
		Size: int64(len(zip)), SHA512: hex.EncodeToString(sum[:]),
	}, private)
	require.NoError(t, err)

	body, err := update.Encode(m)
	require.NoError(t, err)
	testutil.WriteFile(t, filepath.Join(feed, update.ManifestName), string(body))
	testutil.WriteFile(t, filepath.Join(feed, m.File), string(zip))

	opts.Flavor, opts.Release, opts.UpdateFeed = update.FlavorRehearsal, "0.2.0", feed
	opts.UpdateKey = update.EncodeKey(update.PublicKeyOf(private))
	opts.Bundle = filepath.Join(opts.Home, "Applications", brand.Name+".app")
	require.NoError(t, os.MkdirAll(opts.Bundle, 0o700))

	before := dataFiles(t, opts.Paths.Root, opts.Paths.Updates)
	require.Contains(t, before, filepath.Base(opts.Paths.DB))

	backups, err := store.Snapshots(opts.Paths.DB)
	require.NoError(t, err)

	bare := BareUpdate(opts)
	require.Equal(t, update.WayUpdate, bare.Way())

	latest, found, err := bare.Latest(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, m, latest)

	installer := &stagedInstall{}
	require.ErrorIs(t, bare.Update(t.Context(), latest, installer), errStaged)
	assert.Equal(t, "0.3.0", installer.version, "the release was downloaded, checked, and staged")
	assert.FileExists(t, filepath.Join(opts.Paths.Updates, "0.3.0", m.File))

	assert.Equal(t, before, dataFiles(t, opts.Paths.Root, opts.Paths.Updates), "the data is as it was")

	after, err := store.Snapshots(opts.Paths.DB)
	require.NoError(t, err)
	assert.Equal(t, backups, after, "no backup, and nothing set aside")
}

// A build from source never reads the releases, even when it can't open the data: its
// bare update offers the way of a build from source.
func TestABuildFromSourcesBareUpdateReadsNothing(t *testing.T) {
	t.Parallel()

	opts := testOptions(t)
	opts.Release = "0.2.0"

	bare := BareUpdate(opts)
	assert.Equal(t, update.WaySource, bare.Way())

	_, _, err := bare.Latest(t.Context())
	require.ErrorIs(t, err, update.ErrOff, "refused before anything is read")
}
