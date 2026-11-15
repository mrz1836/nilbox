package update

import (
	"crypto/ed25519"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/mrz1836/nilbox/internal/appdata"
)

// errNotNewer is why a cached update is dropped once the running build is as new as it:
// installed, or older than what runs.
var errNotNewer = errors.New("the update is not newer than the running build")

// Entry is one update in the cache: its manifest, verified with the key, and its zip, which
// matched the manifest's size and SHA-512 when it was written or loaded.
type Entry struct {
	Manifest Manifest
	Zip      string
}

// Cache keeps the downloaded updates, each in a folder named for its version holding its
// zip and its manifest (<dir>/<version>/<file> and ManifestName). Nothing in it is trusted
// as it lies: Load checks every update in it again, and drops what doesn't check out.
type Cache struct {
	dir string
	key ed25519.PublicKey
}

// NewCache returns the cache in dir, whose updates must verify with key.
func NewCache(dir string, key ed25519.PublicKey) *Cache {
	return &Cache{dir: dir, key: key}
}

// Put downloads the update m names into the cache with fetch, which writes the zip into
// the writer it is given, and returns its entry. The zip goes into a temporary file first,
// counted (progress hears how many bytes have arrived, as they do) and hashed as it
// arrives, and stops at m.Size; only a zip whose size and SHA-512 match m is renamed into
// place, with m written beside it after. Anything else leaves nothing of the version in the
// cache. m must be verified already.
func (c *Cache) Put(m Manifest, fetch func(w io.Writer) error, progress func(written int64)) (Entry, error) {
	dir := filepath.Join(c.dir, m.Version)

	entry, err := put(dir, m, fetch, progress)
	if err != nil {
		return Entry{}, errors.Join(err, removeAll(dir))
	}

	return entry, nil
}

// put is Put, into dir, leaving what it wrote for Put to remove on a failure.
func put(dir string, m Manifest, fetch func(w io.Writer) error, progress func(int64)) (Entry, error) {
	if !ValidVersion(m.Version) || m.File != FileName(m.Platform, m.Version) {
		return Entry{}, fmt.Errorf("%w: the manifest names no release's file", ErrInvalid)
	}

	if err := removeAll(dir); err != nil {
		return Entry{}, err
	}

	if err := os.MkdirAll(dir, appdata.PrivateDirPerm); err != nil {
		return Entry{}, fmt.Errorf("make the update's folder: %w", err)
	}

	tmp, err := fetchInto(dir, m, fetch, progress)
	if err != nil {
		return Entry{}, err
	}

	zip := filepath.Join(dir, m.File)
	if err = os.Rename(tmp, zip); err != nil {
		return Entry{}, fmt.Errorf("save the update: %w", err)
	}

	body, err := Encode(m)
	if err != nil {
		return Entry{}, err
	}

	if err = appdata.WriteFile(filepath.Join(dir, ManifestName), body); err != nil {
		return Entry{}, fmt.Errorf("save the update's manifest: %w", err)
	}

	return Entry{Manifest: m, Zip: zip}, nil
}

// fetchInto downloads m's zip with fetch into a temporary file in dir, flushed to disk, and
// returns its path once its size and SHA-512 match m.
func fetchInto(dir string, m Manifest, fetch func(w io.Writer) error, progress func(int64)) (string, error) {
	tmp, err := os.CreateTemp(dir, "."+m.File+".*.part")
	if err != nil {
		return "", fmt.Errorf("save the update: %w", err)
	}

	w := &capped{w: tmp, h: sha512.New(), limit: m.Size, progress: progress}

	err = fetch(w)
	if err == nil {
		err = w.check(m)
	}

	if err == nil {
		if syncErr := tmp.Sync(); syncErr != nil {
			err = fmt.Errorf("save the update: %w", syncErr)
		}
	}

	if closeErr := tmp.Close(); err == nil && closeErr != nil {
		err = fmt.Errorf("save the update: %w", closeErr)
	}

	if err != nil {
		return "", err
	}

	return tmp.Name(), nil
}

// Load returns the newest update in the cache that checks out and is newer than current,
// and drops every other version folder: one older than current or as new (installed), one
// whose manifest doesn't verify with the key or names another version or platform, and one
// whose zip doesn't match its manifest, as a damaged or tampered one wouldn't. ok is false
// when none is left. Anything in the cache that isn't a version folder is left alone.
func (c *Cache) Load(current string) (entry Entry, ok bool, err error) {
	listed, err := os.ReadDir(c.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return Entry{}, false, nil
	}

	if err != nil {
		return Entry{}, false, fmt.Errorf("read the updates kept: %w", err)
	}

	var errs []error

	for _, item := range listed {
		if !item.IsDir() || !ValidVersion(item.Name()) {
			continue
		}

		found, checkErr := c.check(item.Name(), current)

		switch {
		case checkErr != nil:
			errs = append(errs, removeAll(filepath.Join(c.dir, item.Name())))
		case !ok:
			entry, ok = found, true
		case newer(found.Manifest.Version, entry.Manifest.Version):
			errs = append(errs, removeAll(filepath.Dir(entry.Zip)))
			entry = found
		default:
			errs = append(errs, removeAll(filepath.Join(c.dir, item.Name())))
		}
	}

	return entry, ok, errors.Join(errs...)
}

// Prune drops every version folder but keep's; "" drops them all.
func (c *Cache) Prune(keep string) error {
	listed, err := os.ReadDir(c.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("read the updates kept: %w", err)
	}

	var errs []error

	for _, item := range listed {
		if item.IsDir() && ValidVersion(item.Name()) && item.Name() != keep {
			errs = append(errs, removeAll(filepath.Join(c.dir, item.Name())))
		}
	}

	return errors.Join(errs...)
}

// check reads the update in the folder named version and checks it as Load does.
func (c *Cache) check(version, current string) (Entry, error) {
	dir := filepath.Join(c.dir, version)

	data, err := appdata.ReadFile(filepath.Join(dir, ManifestName), MaxManifestBytes)
	if err != nil {
		return Entry{}, fmt.Errorf("read the kept manifest: %w", err)
	}

	m, err := Parse(data)
	if err != nil {
		return Entry{}, err
	}

	if err = accept(m, c.key); err != nil {
		return Entry{}, err
	}

	if m.Version != version {
		return Entry{}, fmt.Errorf("%w: the folder %s holds the manifest of %s", ErrInvalid, version, m.Version)
	}

	if !newer(m.Version, current) {
		return Entry{}, errNotNewer
	}

	zip := filepath.Join(dir, m.File)

	f, err := os.Open(zip) //nolint:gosec // the cache's own file, named by a verified manifest
	if err != nil {
		return Entry{}, fmt.Errorf("read the kept update: %w", err)
	}

	defer func() { _ = f.Close() }()

	if err = CheckFile(m, f); err != nil {
		return Entry{}, err
	}

	return Entry{Manifest: m, Zip: zip}, nil
}

// removeAll removes path and what it holds, a missing one included.
func removeAll(path string) error {
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove %s from the updates kept: %w", filepath.Base(path), err)
	}

	return nil
}

// capped writes a download into w, hashing it as it goes, and refuses a byte past limit.
type capped struct {
	w        io.Writer
	h        hash.Hash
	limit    int64
	written  int64
	progress func(int64)
}

// Write writes p, or fails with ErrMismatch once the download runs past limit.
func (c *capped) Write(p []byte) (int, error) {
	if c.written+int64(len(p)) > c.limit {
		return 0, fmt.Errorf("%w: the download is larger than its manifest says", ErrMismatch)
	}

	n, err := c.w.Write(p)
	c.h.Write(p[:n])
	c.written += int64(n)

	if c.progress != nil && n > 0 {
		c.progress(c.written)
	}

	if err != nil {
		return n, fmt.Errorf("save the update: %w", err)
	}

	return n, nil
}

// check fails with ErrMismatch unless what was written is m's size and SHA-512.
func (c *capped) check(m Manifest) error {
	switch {
	case c.written != m.Size:
		return fmt.Errorf("%w: the download holds %d bytes, not the manifest's %d", ErrMismatch, c.written, m.Size)
	case hex.EncodeToString(c.h.Sum(nil)) != m.SHA512:
		return fmt.Errorf("%w: the download's SHA-512 differs from the manifest's", ErrMismatch)
	default:
		return nil
	}
}
