//go:build desktop

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/updater"

	"github.com/mrz1836/nilbox/internal/desktop"
)

// cacheProviderName names the updater's one provider, the app's own update cache.
const cacheProviderName = "cache"

// copyChunk is how much of the cached zip the provider copies at a time, telling the
// updater how far it has got after each.
const copyChunk = 256 << 10

var (
	// errNotOffered is returned when the runtime's updater found another release than the
	// one the install handed it, or none.
	errNotOffered = errors.New("the installer didn't take the update it was handed")

	// errOtherRelease is returned by the cache provider asked for a release it doesn't
	// hold.
	errOtherRelease = errors.New("the update cache holds another release")

	// errNoApp is returned by a call on the runtime before the app exists.
	errNoApp = errors.New("the desktop app isn't running yet")
)

// installer installs an update through the desktop runtime's updater (update.Installer),
// which runs headless here: the app checks, downloads, and verifies its updates itself, and
// the updater only stages the zip from the app's update cache beside the running bundle and
// swaps it in once the app has quit.
type installer struct {
	updater *updater.Updater
	cache   *cacheProvider
}

// newInstaller configures u, the runtime's updater, for a build of release: its one
// provider the update cache, no window, no timed checks, and no public key, since the app
// verified the manifest's signature over the digest the updater checks.
func newInstaller(u *updater.Updater, release string) (*installer, error) {
	cache := &cacheProvider{}

	err := u.Init(updater.Config{
		CurrentVersion: release,
		Providers:      []updater.Provider{cache},
		Window:         updater.WindowNone,
	})
	if err != nil {
		return nil, fmt.Errorf("set up the installer: %w", err)
	}

	return &installer{updater: u, cache: cache}, nil
}

// Install hands the runtime's updater the update in zip, which it copies beside the
// running bundle, checking its size and SHA-512 as it does, and extracts there, ready for
// Restart. What isn't a zip with a digest is never handed over (desktop.NewArtifact).
func (i *installer) Install(ctx context.Context, zip, version string, size int64, sha512 string) error {
	artifact, err := desktop.NewArtifact(zip, version, size, sha512)
	if err != nil {
		return err //nolint:wrapcheck // desktop words what it refuses
	}

	i.cache.offer(artifact)

	found, err := i.updater.Check(ctx)
	if err != nil {
		return fmt.Errorf("hand the update to the installer: %w", err)
	}

	if found == nil || found.Version != version {
		return errNotOffered
	}

	if err = i.updater.DownloadAndInstall(ctx); err != nil {
		return fmt.Errorf("stage the update beside the app: %w", err)
	}

	return nil
}

// Restart starts the runtime's helper, waits up to 30 seconds for it to stand by, and
// quits the app, which exits inside it. It is never called on the main thread, since that
// wait would freeze the menus and the window while it lasts: the menu bar runs it in a
// goroutine of its own, untracked, since the quit stops the shell, which waits for its
// tracked work, and the window's bound calls run on goroutines of the runtime's.
func (i *installer) Restart(ctx context.Context) error {
	if err := i.updater.Restart(ctx); err != nil {
		return fmt.Errorf("start the update's helper: %w", err)
	}

	return nil
}

// cacheProvider is the runtime updater's one provider: the update the app offered it from
// its own cache, already checked against its signed manifest. It reads nothing from the
// network.
type cacheProvider struct {
	mu      sync.Mutex
	offered *desktop.Artifact
}

// Name names the provider.
func (*cacheProvider) Name() string { return cacheProviderName }

// Check returns the release offered, with its zip's name, size, and SHA-512, which the
// updater checks the copy against; with none offered, the app is up to date.
func (p *cacheProvider) Check(_ context.Context, req updater.CheckRequest) (*updater.Release, error) {
	a := p.current()
	if a == nil {
		return nil, nil //nolint:nilnil // the updater's "up to date"
	}

	return &updater.Release{
		Version: a.Version,
		Artifact: updater.Artifact{
			Filename: a.Filename, Filetype: "zip", Size: a.Size, Platform: req.Platform, Arch: req.Arch,
		},
		Verification: &updater.Verification{DigestAlgo: desktop.DigestAlgo, Digest: a.Digest},
	}, nil
}

// Download copies the offered release's zip into dst, telling onProgress how far it has
// got after each write, until ctx ends.
func (p *cacheProvider) Download(ctx context.Context, rel *updater.Release, dst io.Writer, onProgress func(written, total int64)) error {
	a := p.current()
	if a == nil || rel == nil || rel.Version != a.Version {
		return errOtherRelease
	}

	src, err := os.Open(a.Path)
	if err != nil {
		return fmt.Errorf("read the update from the cache: %w", err)
	}

	defer func() { _ = src.Close() }()

	w := &progressWriter{w: dst, total: a.Size, told: onProgress}
	if _, err = io.CopyBuffer(w, contextReader{ctx: ctx, r: src}, make([]byte, copyChunk)); err != nil {
		return fmt.Errorf("copy the update from the cache: %w", err)
	}

	return nil
}

// offer makes a the release the provider holds.
func (p *cacheProvider) offer(a desktop.Artifact) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.offered = &a
}

// current is the release offered, or nil.
func (p *cacheProvider) current() *desktop.Artifact {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.offered
}

// progressWriter writes into w, telling told how much of total it has written after each
// write.
type progressWriter struct {
	w       io.Writer
	written int64
	total   int64
	told    func(written, total int64)
}

// Write writes b into w, then tells how far the copy has got.
func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	p.written += int64(n)
	p.told(p.written, p.total)

	return n, err //nolint:wrapcheck // the writer's own error, which io.Copy passes on
}

// contextReader stops a read once its context ends.
type contextReader struct {
	ctx context.Context //nolint:containedctx // the copy's own context, for one copy
	r   io.Reader
}

// Read reads from r, or returns ctx's error once it has ended.
func (c contextReader) Read(b []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err //nolint:wrapcheck // io.Copy passes it on, wrapped by Download
	}

	return c.r.Read(b) //nolint:wrapcheck // io.Reader's own errors, io.EOF among them
}
