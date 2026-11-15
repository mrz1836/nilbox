package update_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/update"
	"github.com/mrz1836/nilbox/internal/updateprefs"
)

// The versions the tests use: the running build, and the releases after it.
const (
	running = "0.2.0"
	next    = "0.3.0"
	later   = "0.3.1"
)

var (
	errFakeNetwork = errors.New("fake: the network is down")
	errFakeDisk    = errors.New("fake: the disk is full")
	errFakeHelper  = errors.New("fake: the helper never stood by")
)

// zipOf is the zip a test release of version holds.
func zipOf(version string) []byte {
	return []byte("the zipped bundle of " + version)
}

// sign signs a manifest for body at version with private, as a release's would be.
func sign(t *testing.T, version string, body []byte, private ed25519.PrivateKey) update.Manifest {
	t.Helper()

	sum := sha512.Sum512(body)
	m, err := update.Sign(update.Manifest{
		Version: version, Platform: update.Platform, File: update.FileName(update.Platform, version),
		Size: int64(len(body)), SHA512: hex.EncodeToString(sum[:]),
	}, private)
	require.NoError(t, err)

	return m
}

// encode writes m as a release attaches it.
func encode(t *testing.T, m update.Manifest) []byte {
	t.Helper()

	body, err := update.Encode(m)
	require.NoError(t, err)

	return body
}

// fakeFeed serves one release, or none, counting the reads and how many overlap.
type fakeFeed struct {
	mu          sync.Mutex
	manifest    []byte
	manifestErr error
	zips        map[string][]byte
	downloadErr error

	// gate, when set, holds every manifest read until it is closed.
	gate chan struct{}

	manifests, downloads  atomic.Int32
	inFlight, maxInFlight atomic.Int32
}

func newFeed() *fakeFeed {
	return &fakeFeed{zips: map[string][]byte{}}
}

func (f *fakeFeed) Manifest(ctx context.Context) ([]byte, error) {
	f.manifests.Add(1)

	n := f.inFlight.Add(1)
	defer f.inFlight.Add(-1)

	for {
		most := f.maxInFlight.Load()
		if n <= most || f.maxInFlight.CompareAndSwap(most, n) {
			break
		}
	}

	f.mu.Lock()
	gate, raw, err := f.gate, f.manifest, f.manifestErr
	f.mu.Unlock()

	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	switch {
	case err != nil:
		return nil, err
	case raw == nil:
		return nil, update.ErrNoRelease
	default:
		return bytes.Clone(raw), nil
	}
}

func (f *fakeFeed) Download(_ context.Context, _, file string, w io.Writer) error {
	f.downloads.Add(1)

	f.mu.Lock()
	body, err := f.zips[file], f.downloadErr
	f.mu.Unlock()

	if err != nil {
		return err
	}

	_, writeErr := w.Write(body)

	return writeErr
}

// publish makes m, with body as its zip, the latest release.
func (f *fakeFeed) publish(t *testing.T, m update.Manifest, body []byte) {
	t.Helper()

	f.mu.Lock()
	defer f.mu.Unlock()

	f.manifest, f.manifestErr = encode(t, m), nil
	f.zips[m.File] = body
}

// set serves raw as the manifest, or err.
func (f *fakeFeed) set(raw []byte, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.manifest, f.manifestErr = raw, err
}

// fakeMemory is the update.* preferences, in memory.
type fakeMemory struct {
	mu      sync.Mutex
	values  map[string]string
	failSet error
}

func newMemory() *fakeMemory {
	return &fakeMemory{values: map[string]string{}}
}

func (m *fakeMemory) GetMany(_ context.Context, keys ...string) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := map[string]string{}

	for _, k := range keys {
		if v, ok := m.values[k]; ok {
			out[k] = v
		}
	}

	return out, nil
}

func (m *fakeMemory) SetMany(_ context.Context, values map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.failSet != nil {
		return m.failSet
	}

	maps.Copy(m.values, values)

	return nil
}

func (m *fakeMemory) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.values, key)

	return nil
}

// get reads one value.
func (m *fakeMemory) get(key string) string {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.values[key]
}

// put stores one value.
func (m *fakeMemory) put(key, value string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.values[key] = value
}

// install is one call to the installer's Install.
type install struct {
	zip, version string
	size         int64
	sha512       string
}

// fakeInstaller records what it is asked to install, and what the app had told the shell
// and remembered by the time it was asked to restart.
type fakeInstaller struct {
	mu         sync.Mutex
	installs   []install
	restarts   int
	installErr error
	restartErr error

	// onRestart sees the moment Restart is called.
	onRestart func()
}

func (f *fakeInstaller) Install(_ context.Context, zip, version string, size int64, sha512 string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.installs = append(f.installs, install{zip: zip, version: version, size: size, sha512: sha512})

	return f.installErr
}

func (f *fakeInstaller) Restart(context.Context) error {
	f.mu.Lock()
	f.restarts++
	onRestart, err := f.onRestart, f.restartErr
	f.mu.Unlock()

	if onRestart != nil {
		onRestart()
	}

	return err
}

func (f *fakeInstaller) calls() ([]install, int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]install(nil), f.installs...), f.restarts
}

// fakeRuns says runs go in the app, or not.
type fakeRuns struct {
	active  atomic.Bool
	going   []update.Run
	listErr error
}

func (f *fakeRuns) Active() bool { return f.active.Load() }

func (f *fakeRuns) Going(context.Context) ([]update.Run, error) { return f.going, f.listErr }

// fakeBackup backs up the database, or fails.
type fakeBackup struct {
	calls atomic.Int32
	err   error
}

func (f *fakeBackup) BackUp(context.Context) (string, error) {
	f.calls.Add(1)

	if f.err != nil {
		return "", f.err
	}

	return "/data/" + brand.Slug + ".db.backup-20261115T120000Z-v4", nil
}

// fakeNotifier records what the user is told.
type fakeNotifier struct {
	mu   sync.Mutex
	told []update.Notice
}

func (f *fakeNotifier) UpdateFound(_ context.Context, n update.Notice) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.told = append(f.told, n)
}

func (f *fakeNotifier) all() []update.Notice {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]update.Notice(nil), f.told...)
}

// fakeReveal records the files it shows.
type fakeReveal struct {
	mu    sync.Mutex
	shown []string
}

func (f *fakeReveal) Reveal(_ context.Context, path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.shown = append(f.shown, path)

	return nil
}

// wallClock is the real clock, moved on by jumps the timers don't see, as a Mac's clock
// moves on while it sleeps.
type wallClock struct {
	jump atomic.Int64
}

func (c *wallClock) Now() time.Time { return time.Now().Add(time.Duration(c.jump.Load())) }

// sleep moves the wall clock on by d, as a sleep does, with no timer moving.
func (c *wallClock) sleep(d time.Duration) { c.jump.Add(int64(d)) }

// rig is an engine over fakes, a rehearsal build of running whose releases verify with
// the rig's key, in an Applications folder of its own.
type rig struct {
	feed      *fakeFeed
	memory    *fakeMemory
	installer *fakeInstaller
	runs      *fakeRuns
	backup    *fakeBackup
	notifier  *fakeNotifier
	reveal    *fakeReveal
	events    *testutil.Events
	clock     *wallClock

	private ed25519.PrivateKey
	cfg     update.Config
	engine  *update.Engine
	service *update.Service
}

// newRig returns a rig, its config changed by each change.
func newRig(t *testing.T, change ...func(*update.Config)) *rig {
	t.Helper()

	public, private := testKey(7)
	root := t.TempDir()
	apps := filepath.Join(root, "Applications")
	bundle := filepath.Join(apps, brand.Name+".app")
	require.NoError(t, os.MkdirAll(filepath.Join(bundle, "Contents", "MacOS"), 0o700))

	r := &rig{
		feed: newFeed(), memory: newMemory(), installer: &fakeInstaller{}, runs: &fakeRuns{}, backup: &fakeBackup{},
		notifier: &fakeNotifier{}, reveal: &fakeReveal{}, events: &testutil.Events{}, clock: &wallClock{},
		private: private,
	}

	r.cfg = update.Config{
		Flavor: update.FlavorRehearsal, Current: running, Feed: r.feed, Key: update.EncodeKey(public),
		Memory: r.memory, Cache: filepath.Join(root, "updates"), Logs: filepath.Join(root, "logs"), Bundle: bundle,
		Home: filepath.Join(root, "home"), TempDir: filepath.Join(root, "tmp"), PID: 4242,
		Backup: r.backup, Runs: r.runs, Notifier: r.notifier, Reveal: r.reveal, Events: r.events, Clock: r.clock,
		Writable: func(string) bool { return true },
	}

	require.NoError(t, os.MkdirAll(r.cfg.TempDir, 0o700))
	require.NoError(t, os.MkdirAll(r.cfg.Logs, 0o700))

	for _, c := range change {
		c(&r.cfg)
	}

	r.engine = update.NewEngine(r.cfg)
	r.engine.Attach(r.installer)
	r.service = update.NewService(r.engine)

	return r
}

// release publishes version as the latest release, signed with the rig's key, and returns
// its manifest.
func (r *rig) release(t *testing.T, version string) update.Manifest {
	t.Helper()

	m := sign(t, version, zipOf(version), r.private)
	r.feed.publish(t, m, zipOf(version))

	return m
}

// mode sets what the app does when a new version is out.
func (r *rig) mode(m updateprefs.Mode) {
	r.memory.put(updateprefs.KeyMode, string(m))
}

// status reads the status, failing the test on an error.
func (r *rig) status(t *testing.T) update.Status {
	t.Helper()

	s, err := r.service.Status(t.Context())
	require.NoError(t, err)

	return s
}

// cached lists the version folders in the cache.
func (r *rig) cached(t *testing.T) []string {
	t.Helper()

	listed, err := os.ReadDir(r.cfg.Cache)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}
	}

	require.NoError(t, err)

	out := []string{}
	for _, item := range listed {
		out = append(out, item.Name())
	}

	return out
}

// quitEvents lists the events about the quit the engine told the shell.
func (r *rig) quitEvents() []event.Event {
	out := []event.Event{}

	for _, e := range r.events.All() {
		switch e.(type) {
		case event.QuitConfirmed, event.UpdateRestartFailed:
			out = append(out, e)
		}
	}

	return out
}
