package toolinstall

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

// TestMain fails the run when a test leaves a goroutine behind.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

const (
	testSlug    = "testapp"
	testBinary  = "magex"
	testVersion = "1.30.2"
	archiveName = "mage-x_1.30.2_darwin_arm64.tar.gz"
	sumsName    = "mage-x_1.30.2_checksums.txt"

	// goodProgram is the program the release holds: the smoke test passes on it.
	goodProgram = "program " + testVersion
)

// errOffHost stands for a download refused for its host, and errBroken for a program that
// won't run.
var (
	errOffHost  = errors.New("not a GitHub release download")
	errBroken   = errors.New("it won't run")
	errTooLarge = errors.New("larger than allowed")
)

// entry is one file in a test archive.
type entry struct {
	name, body, link string
	typ              byte
	size             int64
}

// makeArchive builds a .tar.gz of entries.
func makeArchive(t *testing.T, entries ...entry) []byte {
	t.Helper()

	var buf bytes.Buffer

	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for _, e := range entries {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}

		size := int64(len(e.body))
		if e.size > 0 {
			size = e.size
		}

		if typ != tar.TypeReg {
			size = 0
		}

		require.NoError(t, tw.WriteHeader(&tar.Header{Name: e.name, Typeflag: typ, Size: size, Mode: 0o755, Linkname: e.link}))

		if typ == tar.TypeReg {
			_, err := io.Copy(tw, io.LimitReader(io.MultiReader(strings.NewReader(e.body), zeros{}), size))
			require.NoError(t, err)
		}
	}

	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())

	return buf.Bytes()
}

// zeros reads as endless zero bytes.
type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)

	return len(p), nil
}

// sha returns data's SHA-256 in hex.
func sha(data []byte) string {
	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:])
}

// release is a test release: its files by URL, and the assets that list them.
type release struct {
	files  map[string][]byte
	assets []Asset
}

// newRelease is a release whose archive is archive, listed in its checksums file.
func newRelease(archive []byte) *release {
	sums := []byte(sha(archive) + "  " + archiveName + "\n" + strings.Repeat("0", 64) + "  other.tar.gz\n")

	r := &release{files: map[string][]byte{"https://github.com/a": archive, "https://github.com/s": sums}}
	r.assets = []Asset{
		{Name: archiveName, URL: "https://github.com/a", Size: int64(len(archive)), Digest: "sha256:" + sha(archive)},
		{Name: sumsName, URL: "https://github.com/s", Size: int64(len(sums)), Digest: "sha256:" + sha(sums)},
	}

	return r
}

// downloader serves a release, as DownloadAsset does.
type downloader struct {
	r *release

	// short serves a file one byte short, and hang waits for ctx to end; err fails.
	short bool
	hang  bool
	err   error
}

func (d *downloader) DownloadAsset(ctx context.Context, rawURL string, w io.Writer, maxBytes int64) (int64, error) {
	switch {
	case d.err != nil:
		return 0, d.err
	case d.hang:
		_, _ = w.Write([]byte("a start"))

		<-ctx.Done()

		return 7, ctx.Err()
	}

	data, ok := d.r.files[rawURL]
	if !ok {
		return 0, errOffHost
	}

	if d.short && len(data) > 0 {
		data = data[:len(data)-1]
	}

	if int64(len(data)) > maxBytes {
		return 0, fmt.Errorf("%w: %d bytes", errTooLarge, len(data))
	}

	n, err := w.Write(data)

	return int64(n), err
}

// smoke passes a program that reports the test version, and fails any other; failPlaced
// fails it once placed in dir.
func smoke(failPlaced string) func(context.Context, string) error {
	return func(_ context.Context, path string) error {
		data, err := os.ReadFile(path) //nolint:gosec // the test's own file
		if err != nil {
			return err
		}

		if string(data) != goodProgram || (failPlaced != "" && filepath.Dir(path) == resolve(failPlaced)) {
			return errBroken
		}

		return nil
	}
}

// resolve returns path with its links resolved, or path when it can't be.
func resolve(path string) string {
	if r, err := filepath.EvalSymlinks(path); err == nil {
		return r
	}

	return path
}

// setSums replaces the release's checksums file with content.
func (r *release) setSums(content string) {
	r.files["https://github.com/s"] = []byte(content)
	r.assets[1].Size, r.assets[1].Digest = int64(len(content)), ""
}

// setup is an installer over a release, with a target folder.
type setup struct {
	in        *Installer
	d         *downloader
	downloads string
	dir       string
}

func newSetup(t *testing.T, archive []byte) *setup {
	t.Helper()

	root := t.TempDir()
	s := &setup{
		d: &downloader{r: newRelease(archive)}, downloads: filepath.Join(root, "downloads"), dir: filepath.Join(root, "bin"),
	}
	s.in = &Installer{Downloads: s.downloads, Download: s.d, Slug: testSlug}

	return s
}

// request installs the setup's release into its folder.
func (s *setup) request(failPlaced string) Request {
	return Request{
		Tool: "mage-x", Binary: testBinary, Version: testVersion, Archive: archiveName, Checksums: sumsName,
		Assets: s.d.r.assets, Dir: s.dir, Smoke: smoke(failPlaced),
	}
}

// leftovers lists what is in the downloads folder and the install's files beside the
// target.
func (s *setup) leftovers(t *testing.T) []string {
	t.Helper()

	var found []string

	for _, dir := range []string{s.downloads, s.dir} {
		entries, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}

		require.NoError(t, err)

		for _, e := range entries {
			if dir == s.downloads || strings.Contains(e.Name(), "."+testSlug+"-") {
				found = append(found, e.Name())
			}
		}
	}

	return found
}

// goodArchive is a release archive as GoReleaser makes one.
func goodArchive(t *testing.T) []byte {
	t.Helper()

	return makeArchive(t, entry{name: "LICENSE", body: "MIT"}, entry{name: "README.md", body: "# mage-x"},
		entry{name: testBinary, body: goodProgram})
}

func TestInstallPlacesTheProgram(t *testing.T) {
	t.Parallel()

	s := newSetup(t, goodArchive(t))

	var steps []Step

	req := s.request("")
	req.Progress = func(p Progress) {
		if len(steps) == 0 || steps[len(steps)-1] != p.Step {
			steps = append(steps, p.Step)
		}
	}

	placed, err := s.in.Install(t.Context(), req)
	require.NoError(t, err)

	assert.Equal(t, Placed{
		Path: filepath.Join(resolve(s.dir), testBinary), SHA256: sha([]byte(goodProgram)), Version: testVersion,
		Archive: archiveName,
	}, placed)

	data, err := os.ReadFile(placed.Path)
	require.NoError(t, err)
	assert.Equal(t, goodProgram, string(data))

	info, err := os.Stat(placed.Path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm())

	assert.Equal(t, []Step{StepDownloading, StepVerifying, StepTrying, StepInstalling}, steps)
	assert.Empty(t, s.leftovers(t), "the download folder is gone, and nothing is left beside the target")
}

func TestInstallReplacesACopy(t *testing.T) {
	t.Parallel()

	s := newSetup(t, goodArchive(t))
	target := filepath.Join(s.dir, testBinary)
	require.NoError(t, os.MkdirAll(s.dir, 0o750))
	require.NoError(t, os.WriteFile(target, []byte("program 1.29.5"), 0o755)) //nolint:gosec // a program

	before, err := os.Stat(target)
	require.NoError(t, err)

	placed, err := s.in.Install(t.Context(), s.request(""))
	require.NoError(t, err)
	assert.True(t, placed.Replaced)

	after, err := os.Stat(target)
	require.NoError(t, err)
	assert.False(t, os.SameFile(before, after), "a new file, so a running copy keeps the old one")
	assert.Empty(t, s.leftovers(t))
}

func TestTargetNeverMissingDuringReplace(t *testing.T) {
	t.Parallel()

	s := newSetup(t, goodArchive(t))
	target := filepath.Join(s.dir, testBinary)

	_, err := s.in.Install(t.Context(), s.request(""))
	require.NoError(t, err)

	var (
		misses, stats atomic.Int64
		stop          atomic.Bool
		wg            sync.WaitGroup
	)

	wg.Go(func() {
		for !stop.Load() {
			if _, statErr := os.Stat(target); statErr != nil {
				misses.Add(1)
			}

			stats.Add(1)
		}
	})

	for range 40 {
		if _, err = s.in.Install(t.Context(), s.request("")); err != nil {
			break
		}
	}

	stop.Store(true)
	wg.Wait()

	require.NoError(t, err)
	assert.Positive(t, stats.Load())
	assert.Zero(t, misses.Load(), "the target was there at every look, through 40 replaces")
}

func TestAFailedSmokeTestRollsBack(t *testing.T) {
	t.Parallel()

	s := newSetup(t, goodArchive(t))
	target := filepath.Join(s.dir, testBinary)
	require.NoError(t, os.MkdirAll(s.dir, 0o750))
	require.NoError(t, os.WriteFile(target, []byte("program 1.29.5"), 0o755)) //nolint:gosec // a program

	_, err := s.in.Install(t.Context(), s.request(s.dir))
	require.ErrorIs(t, err, ErrRolledBack)

	data, err := os.ReadFile(target) //nolint:gosec // the test's own file
	require.NoError(t, err)
	assert.Equal(t, "program 1.29.5", string(data), "the old copy is back")
	assert.Empty(t, s.leftovers(t))

	// With no copy before, the new one is taken away.
	fresh := newSetup(t, goodArchive(t))

	_, err = fresh.in.Install(t.Context(), fresh.request(fresh.dir))
	require.ErrorIs(t, err, ErrRolledBack)
	assert.NoFileExists(t, filepath.Join(fresh.dir, testBinary))
	assert.Empty(t, fresh.leftovers(t))
}

func TestAFailedRollBackSaysSo(t *testing.T) {
	t.Parallel()

	s := newSetup(t, goodArchive(t))
	target := filepath.Join(s.dir, testBinary)
	require.NoError(t, os.MkdirAll(s.dir, 0o750))
	require.NoError(t, os.WriteFile(target, []byte("program 1.29.5"), 0o755)) //nolint:gosec // a program

	req := s.request("")
	smokeExtracted := req.Smoke
	req.Smoke = func(ctx context.Context, path string) error {
		if filepath.Dir(path) != resolve(s.dir) {
			return smokeExtracted(ctx, path)
		}

		// Something removed the old copy kept aside, so it can't be put back.
		_ = os.Remove(path + "." + testSlug + "-old")

		return errBroken
	}

	_, err := s.in.Install(t.Context(), req)
	require.ErrorIs(t, err, ErrRollbackFailed)
	require.NotErrorIs(t, err, ErrRolledBack, "never said to be put back when it wasn't")
}

func TestEveryRefusalLeavesNothingBehind(t *testing.T) {
	t.Parallel()

	// The bomb holds the program and unpacks to more than twice the cap its case sets, so
	// only the cap refuses it. Its subtests share the one archive.
	bombArchive := makeArchive(t, entry{name: testBinary, body: goodProgram}, entry{name: "padding", size: 128 << 10})
	bomb := func(*testing.T) []byte { return bombArchive }

	tests := []struct {
		name    string
		archive func(t *testing.T) []byte
		change  func(s *setup, req *Request)
		want    error
	}{
		{name: "no checksums file", change: func(_ *setup, req *Request) { req.Checksums = "missing.txt" }, want: ErrNoChecksums},
		{name: "no build for this machine", change: func(_ *setup, req *Request) { req.Archive = "mage-x_1.30.2_plan9_386.tar.gz" }, want: ErrNoBuild},
		{
			name:   "a checksum mismatch",
			change: func(s *setup, _ *Request) { s.d.r.setSums(strings.Repeat("a", 64) + "  " + archiveName + "\n") },
			want:   ErrChecksum,
		},
		{
			name: "the archive listed twice",
			change: func(s *setup, _ *Request) {
				line := sha(s.d.r.files["https://github.com/a"]) + "  " + archiveName + "\n"
				s.d.r.setSums(line + line)
			},
			want: ErrChecksum,
		},
		{name: "GitHub's digest mismatch", change: func(s *setup, _ *Request) { s.d.r.assets[0].Digest = "sha256:" + strings.Repeat("b", 64) }, want: ErrChecksum},
		{name: "a short download", change: func(s *setup, _ *Request) { s.d.short = true }, want: ErrShortDownload},
		{name: "an off-host URL or redirect", change: func(s *setup, _ *Request) { s.d.err = errOffHost }, want: errOffHost},
		{name: "path traversal", archive: func(t *testing.T) []byte {
			t.Helper()

			return makeArchive(t, entry{name: "../" + testBinary, body: goodProgram})
		}, want: ErrArchive},
		{name: "an absolute path", archive: func(t *testing.T) []byte {
			t.Helper()

			return makeArchive(t, entry{name: "/usr/local/bin/" + testBinary, body: goodProgram})
		}, want: ErrArchive},
		{name: "a link entry", archive: func(t *testing.T) []byte {
			t.Helper()

			return makeArchive(t, entry{name: testBinary, typ: tar.TypeSymlink, link: "/bin/sh"})
		}, want: ErrArchive},
		{name: "a hardlink entry", archive: func(t *testing.T) []byte {
			t.Helper()

			return makeArchive(t, entry{name: "LICENSE", body: "MIT"}, entry{name: testBinary, typ: tar.TypeLink, link: "LICENSE"})
		}, want: ErrArchive},
		{name: "no program", archive: func(t *testing.T) []byte {
			t.Helper()

			return makeArchive(t, entry{name: "bin/" + testBinary, body: goodProgram})
		}, want: ErrArchive},
		{name: "the program twice", archive: func(t *testing.T) []byte {
			t.Helper()

			return makeArchive(t, entry{name: testBinary, body: goodProgram}, entry{name: "./" + testBinary, body: goodProgram})
		}, want: ErrArchive},
		{name: "an oversized entry", change: func(s *setup, _ *Request) { s.in.maxProgram = 4 }, want: ErrArchive},
		{name: "a gzip bomb", archive: bomb, change: func(s *setup, _ *Request) { s.in.maxUnpacked = 64 << 10 }, want: ErrArchive},
		{name: "a download that won't run", archive: func(t *testing.T) []byte {
			t.Helper()

			return makeArchive(t, entry{name: testBinary, body: "program 0.0.1"})
		}, want: ErrSmoke},
		{name: "a symlinked target", change: func(s *setup, _ *Request) {
			if err := os.MkdirAll(s.dir, 0o750); err == nil {
				_ = os.Symlink("/bin/sh", filepath.Join(s.dir, testBinary))
			}
		}, want: ErrLinkTarget},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			refuse(t, tt.archive, tt.change, tt.want, false)
		})

		if errors.Is(tt.want, ErrLinkTarget) {
			continue // its target is the link it puts there
		}

		t.Run(tt.name+", over a copy already there", func(t *testing.T) {
			t.Parallel()

			refuse(t, tt.archive, tt.change, tt.want, true)
		})
	}
}

// refuse runs an install over the archive (a good one when nil) after change, and checks
// it fails with want and leaves nothing behind. With existing, a copy is in place first,
// and the refusal leaves it as it was: its bytes, its mode, and its modification time.
func refuse(t *testing.T, archive func(t *testing.T) []byte, change func(s *setup, req *Request), want error,
	existing bool,
) {
	t.Helper()

	data := goodArchive(t)
	if archive != nil {
		data = archive(t)
	}

	s := newSetup(t, data)
	req := s.request("")
	target := filepath.Join(s.dir, testBinary)

	var before os.FileInfo

	if existing {
		require.NoError(t, os.MkdirAll(s.dir, 0o750))
		require.NoError(t, os.WriteFile(target, []byte("the copy already here"), 0o755)) //nolint:gosec // a program

		var err error

		before, err = os.Stat(target)
		require.NoError(t, err)
	}

	if change != nil {
		change(s, &req)
		req.Assets = s.d.r.assets
	}

	_, err := s.in.Install(t.Context(), req)
	require.ErrorIs(t, err, want)
	assert.Empty(t, s.leftovers(t), "nothing is left behind")

	if existing {
		got, readErr := os.ReadFile(target) //nolint:gosec // the test's own temporary folder
		require.NoError(t, readErr)
		assert.Equal(t, "the copy already here", string(got), "the copy in place is unchanged")

		after, statErr := os.Stat(target)
		require.NoError(t, statErr)
		assert.Equal(t, before.Mode(), after.Mode())
		assert.True(t, before.ModTime().Equal(after.ModTime()), "and untouched")

		return
	}

	if info, statErr := os.Lstat(filepath.Join(s.dir, testBinary)); statErr == nil {
		assert.NotZero(t, info.Mode()&os.ModeSymlink, "only the link that was there is there")
	}
}

func TestACancelLeavesNothingBehind(t *testing.T) {
	t.Parallel()

	s := newSetup(t, goodArchive(t))
	s.d.hang = true

	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(20*time.Millisecond, cancel)

	_, err := s.in.Install(ctx, s.request(""))
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, s.leftovers(t))
	assert.NoFileExists(t, filepath.Join(s.dir, testBinary))
}

func TestInstallPlacesIntoTheResolvedFolder(t *testing.T) {
	t.Parallel()

	s := newSetup(t, goodArchive(t))
	dotfiles := filepath.Join(t.TempDir(), "dotfiles", "bin")
	require.NoError(t, os.MkdirAll(dotfiles, 0o750))
	require.NoError(t, os.Symlink(dotfiles, s.dir))

	placed, err := s.in.Install(t.Context(), s.request(""))
	require.NoError(t, err)

	resolved, err := filepath.EvalSymlinks(dotfiles)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(resolved, testBinary), placed.Path, "in the folder the link names")

	info, err := os.Lstat(s.dir)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSymlink, "the link stays a link")
}

func TestChecksumsAcceptTheBinaryMarkerAndCase(t *testing.T) {
	t.Parallel()

	work, err := os.OpenRoot(t.TempDir())
	require.NoError(t, err)

	t.Cleanup(func() { _ = work.Close() })

	sum := sha([]byte("x"))

	require.NoError(t, work.WriteFile("sums", []byte(strings.ToUpper(sum)+" *"+archiveName+"\n"), 0o600))
	require.NoError(t, verify(work, "sums", archiveName, sum))

	require.NoError(t, work.WriteFile("sums", []byte("abc  "+archiveName+"\n"), 0o600))
	require.ErrorIs(t, verify(work, "sums", archiveName, sum), ErrChecksum, "a checksum that isn't 64 hex digits")
}

func TestTheDownloadFolderKeepsEveryFileInside(t *testing.T) {
	t.Parallel()

	s := newSetup(t, goodArchive(t))
	parent := t.TempDir()
	dir := filepath.Join(parent, "work")
	require.NoError(t, os.Mkdir(dir, 0o700))

	work, err := os.OpenRoot(dir)
	require.NoError(t, err)

	t.Cleanup(func() { _ = work.Close() })

	// A name pickAssets would refuse, handed past it: the root refuses it as well.
	escaping := s.d.r.assets[1]
	escaping.Name = "../" + sumsName

	_, err = s.in.fetch(t.Context(), work, escaping, maxChecksumsBytes, nil)
	require.ErrorContains(t, err, "make the download file: ")
	assert.NoFileExists(t, filepath.Join(parent, sumsName), "nothing is written outside the download folder")

	// Nor is anything outside it read, though it is there.
	archive := goodArchive(t)
	require.NoError(t, os.WriteFile(filepath.Join(parent, sumsName), []byte(sha(archive)+"  "+archiveName+"\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(parent, archiveName), archive, 0o600))

	require.ErrorContains(t, verify(work, "../"+sumsName, archiveName, sha(archive)), "read the checksums: ")

	_, _, err = s.in.extract(work, "../"+archiveName, testBinary)
	require.ErrorContains(t, err, "open the archive: ")
}

func TestCleanupRemovesOnlyItsOwnOldLeftovers(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	downloads, bin := filepath.Join(root, "downloads"), filepath.Join(root, "bin")
	in := &Installer{Downloads: downloads, Slug: testSlug}
	now := time.Now()
	old := now.Add(-48 * time.Hour)

	create := func(path string, dir bool, at time.Time) {
		if dir {
			require.NoError(t, os.MkdirAll(path, 0o750))
		} else {
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
			require.NoError(t, os.WriteFile(path, nil, 0o600))
		}

		require.NoError(t, os.Chtimes(path, at, at))
	}

	create(filepath.Join(downloads, testSlug+"-mage-x-1.30.2-abc"), true, old)
	create(filepath.Join(downloads, testSlug+"-mage-x-1.30.2-new"), true, now)
	create(filepath.Join(downloads, "someone-elses"), true, old)
	create(filepath.Join(bin, "magex."+testSlug+"-new"), false, old)
	create(filepath.Join(bin, "magex."+testSlug+"-old"), false, old)
	create(filepath.Join(bin, "go-pre-commit."+testSlug+"-old"), false, now)
	create(filepath.Join(bin, "magex"), false, old)
	create(filepath.Join(bin, "notes-new"), false, old)

	in.Cleanup([]string{bin, filepath.Join(root, "missing")}, now, 24*time.Hour)

	names := func(dir string) []string {
		entries, err := os.ReadDir(dir)
		require.NoError(t, err)

		out := make([]string, 0, len(entries))
		for _, e := range entries {
			out = append(out, e.Name())
		}

		return out
	}

	assert.ElementsMatch(t, []string{testSlug + "-mage-x-1.30.2-new", "someone-elses"}, names(downloads))
	assert.ElementsMatch(t, []string{"go-pre-commit." + testSlug + "-old", "magex", "notes-new"}, names(bin))
}
