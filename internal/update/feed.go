package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// ErrNoRelease is returned by a Feed whose latest release has no manifest: none is out yet,
// or one was pulled. It reads as up to date.
var ErrNoRelease = errors.New("the latest release has no update manifest")

// Feed is where the releases are read from: GitHub's latest release for a release build
// (core adapts the GitHub client's release downloads), a local folder for a rehearsal.
type Feed interface {
	// Manifest returns the latest release's manifest, as its bytes, at most
	// MaxManifestBytes, or ErrNoRelease when it has none.
	Manifest(ctx context.Context) ([]byte, error)

	// Download writes the zip file of the release of version into w. It may write more
	// than the manifest's size; the caller stops it there.
	Download(ctx context.Context, version, file string, w io.Writer) error
}

// ManifestURL is where a release build reads the latest release's manifest: GitHub's link
// to the latest release's asset, which skips drafts and prereleases, and answers 404 while
// that release has no manifest.
func ManifestURL() string {
	return brand.Repository + "/releases/latest/download/" + ManifestName
}

// ZipURL is where a release build downloads the zip of the release of version: pinned to
// its tag, so a release published between a check and its download can't move it.
func ZipURL(version, file string) string {
	return brand.Repository + "/releases/download/v" + version + "/" + file
}

// folderUnread is why a rehearsal's folder failed, worded with no path.
const folderUnread = "The rehearsal's folder can't be read."

// FolderFeed reads the releases from a folder holding one release's manifest
// (ManifestName) and its zip, as magex app:rehearse writes them: a rehearsal's feed, read
// from disk with no network at all.
type FolderFeed struct {
	// Dir is the folder.
	Dir string
}

// Manifest reads the folder's manifest, or fails with ErrNoRelease when there is none.
func (f FolderFeed) Manifest(_ context.Context) ([]byte, error) {
	file, err := os.Open(filepath.Join(f.Dir, ManifestName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNoRelease
	}

	if err != nil {
		return nil, uierr.New(folderUnread, fmt.Errorf("read the update manifest: %w", err))
	}

	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(io.LimitReader(file, MaxManifestBytes+1))
	if err != nil {
		return nil, uierr.New(folderUnread, fmt.Errorf("read the update manifest: %w", err))
	}

	return data, nil
}

// Download copies the folder's file into w. The file must be a plain name, in the folder.
func (f FolderFeed) Download(ctx context.Context, _, file string, w io.Writer) error {
	if file == "" || filepath.Base(file) != file || file == "." || file == ".." {
		return fmt.Errorf("%w: the file %q is not a plain name", ErrInvalid, file)
	}

	src, err := os.Open(filepath.Join(f.Dir, file)) //nolint:gosec // a plain name, in the rehearsal's own folder
	if errors.Is(err, fs.ErrNotExist) {
		return uierr.New("The rehearsal's folder doesn't hold the update's zip.", fmt.Errorf("read the update: %w", err))
	}

	if err != nil {
		return uierr.New(folderUnread, fmt.Errorf("read the update: %w", err))
	}

	defer func() { _ = src.Close() }()

	if _, err = io.Copy(w, contextReader{ctx: ctx, r: io.LimitReader(src, MaxFileBytes+1)}); err != nil {
		return fmt.Errorf("read the update: %w", err)
	}

	return nil
}

// contextReader stops a read once its context ends.
type contextReader struct {
	ctx context.Context //nolint:containedctx // the read's own context, for one copy
	r   io.Reader
}

// Read reads from r, or returns ctx's error once it has ended.
func (c contextReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}

	return c.r.Read(p) //nolint:wrapcheck // io.Reader's own errors, io.EOF among them
}
