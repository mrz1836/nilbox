package hooks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/gitread"
)

const (
	// maxHashed caps how many dirty files a snapshot hashes; a clone with more has them
	// all told apart by their size and modification time.
	maxHashed = 2000

	// maxHashSize caps the size of a file a snapshot hashes; a larger one is told apart by
	// its size and modification time.
	maxHashSize = 16 << 20

	// untrackedCode is the status of an untracked file.
	untrackedCode = "??"
)

// ChangedFile is a file a checks run changed: rewritten, staged, or both.
type ChangedFile struct {
	// Path is the file, relative to the clone's folder.
	Path string `json:"path"`

	// Staged reports that the run changed what the index holds for it, as whitespace's
	// fixes do, and WasDirty that it had uncommitted changes before the run, which the run
	// may have mixed its own into.
	Staged   bool `json:"staged"`
	WasDirty bool `json:"wasDirty"`

	// By are the checks go-pre-commit said changed it (run --json only), or [].
	By []Check `json:"by"`
}

// snapshot is a clone's uncommitted state at one moment: git's status of every changed,
// staged, and untracked file, with what the index holds for each, and a sum of each such
// file, so a dirty file rewritten shows even when its status doesn't change.
type snapshot struct {
	// err says why the status couldn't be read; the rest is then empty.
	err error

	entries map[string]gitread.Entry
	sums    map[string]string

	// hashed reports sums of the files' contents, rather than of their sizes and times,
	// as the snapshot after a run must take them too.
	hashed bool
}

// take reads the clone at dir's snapshot before a run: its files' contents hashed, unless
// it has more than maxHashed dirty files.
func (s *Service) take(ctx context.Context, dir string) snapshot {
	return s.read(ctx, dir, func(n int) bool { return n <= maxHashed })
}

// takeAfter reads the clone at dir's snapshot after a run, its files told apart as
// before's were.
func (s *Service) takeAfter(ctx context.Context, dir string, before snapshot) snapshot {
	return s.read(ctx, dir, func(int) bool { return before.hashed })
}

// read reads a snapshot, hashing the files' contents when hash says so for their count.
func (s *Service) read(ctx context.Context, dir string, hash func(n int) bool) snapshot {
	entries, err := s.git.Entries(ctx, dir)
	if err != nil {
		return snapshot{err: err}
	}

	snap := snapshot{
		entries: make(map[string]gitread.Entry, len(entries)), sums: make(map[string]string, len(entries)),
		hashed: hash(len(entries)),
	}

	for _, e := range entries {
		snap.entries[e.Path] = e
		snap.sums[e.Path] = fileSum(dir, e.Path, snap.hashed)
	}

	return snap
}

// changedFiles compares two snapshots and returns, sorted, every file the run between
// them changed: one newly dirty, one clean again, or one whose status, staged object, or
// contents moved.
func changedFiles(before, after snapshot) []ChangedFile {
	all := slices.Collect(maps.Keys(before.entries))
	for p := range after.entries {
		if _, ok := before.entries[p]; !ok {
			all = append(all, p)
		}
	}

	slices.Sort(all)

	out := []ChangedFile{}

	for _, p := range all {
		b, wasDirty := before.entries[p]
		a, isDirty := after.entries[p]

		var changed, staged bool

		switch {
		case !isDirty:
			// Clean again: the run brought the file back to its last commit, and the index
			// with it when it had been staged.
			changed, staged = true, indexChanged(b)
		case !wasDirty:
			changed, staged = true, indexChanged(a)
		default:
			staged = b.Index != a.Index || b.Code[0] != a.Code[0]
			changed = staged || b.Code != a.Code || before.sums[p] != after.sums[p]
		}

		if changed {
			out = append(out, ChangedFile{Path: p, Staged: staged, WasDirty: wasDirty, By: []Check{}})
		}
	}

	return out
}

// indexChanged reports an entry whose index side differs from the last commit: staged.
func indexChanged(e gitread.Entry) bool {
	return e.Code != untrackedCode && e.Code != "" && e.Code[0] != '.'
}

// fileSum tells the file at rel in the clone at dir apart from any other version of it:
// its contents' SHA-256 when hash is set and it is small enough, else its size and time;
// a link's target, a folder, or a file that is gone.
func fileSum(dir, rel string, hash bool) string {
	if !filepath.IsLocal(filepath.FromSlash(strings.TrimSuffix(rel, "/"))) {
		return "outside"
	}

	path := filepath.Join(dir, filepath.FromSlash(rel))

	info, err := os.Lstat(path)

	switch {
	case err != nil:
		return "gone"
	case info.Mode()&os.ModeSymlink != 0:
		target, _ := os.Readlink(path)

		return "link:" + target
	case info.IsDir():
		return "folder"
	case !info.Mode().IsRegular():
		return "special"
	case !hash || info.Size() > maxHashSize:
		return fmt.Sprintf("size:%d time:%d", info.Size(), info.ModTime().UnixNano())
	}

	f, err := os.Open(path) //nolint:gosec // a file in the clone a checks run ran in, only read
	if err != nil {
		return "unreadable"
	}

	defer func() { _ = f.Close() }()

	sum := sha256.New()
	if _, err = io.Copy(sum, f); err != nil {
		return "unreadable"
	}

	return hex.EncodeToString(sum.Sum(nil))
}
