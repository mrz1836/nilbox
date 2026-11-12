package localrepos

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// shareLocation is a folder location on a share that may hang, as stored.
const shareLocation = "~/share"

// stuck is a location whose reads, or the resolution of whose links, hang until release
// is closed, as on a network share that stops answering, and counts how many start.
type stuck struct {
	dir     string
	release chan struct{}
	reads   atomic.Int32
	resolve atomic.Int32
}

// newStuck returns the stuck location dir. It must be made inside the test's synctest
// bubble, whose clock moves only once every goroutine in it is blocked, so the reader's
// limit passes at once and exactly.
func newStuck(dir string) *stuck {
	return &stuck{dir: dir, release: make(chan struct{})}
}

// reader returns a Reader with DefaultReadLimit whose reads of the location hang, and its
// classification as a folder to add; with links, the resolution of its links does too.
func (s *stuck) reader(links bool) *Reader {
	r := NewReader(DefaultReadLimit)

	r.read = func(p Place) placeRead {
		if p.Path == s.dir {
			s.reads.Add(1)
			<-s.release
		}

		return readPlace(p)
	}

	r.resolve = func(dir string) string {
		if links && dir == s.dir {
			s.resolve.Add(1)
			<-s.release
		}

		return paths.Resolved(dir)
	}

	r.addable = func(dir, home string) (Kind, error) {
		if dir == s.dir {
			<-s.release
		}

		return classifyAddable(dir, home)
	}

	return r
}

// end lets every hung read end, and waits until they have.
func (s *stuck) end() {
	close(s.release)
	synctest.Wait()
}

// readerRoots builds the Roots of ~/projects in home and extra, read through reader.
func readerRoots(t *testing.T, home string, reader *Reader, extra ...Location) Roots {
	t.Helper()

	roots, err := NewRoots("~/projects", extra, home, WithReader(reader))
	require.NoError(t, err)

	return roots
}

func TestReaderSkipsALocationThatDoesNotAnswer(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		home := t.TempDir()
		api := fakeClone(t, filepath.Join(home, "projects", "api"))
		web := fakeClone(t, filepath.Join(home, "work", "web"))
		share := newStuck(filepath.Join(home, "share"))
		cli := fakeClone(t, filepath.Join(share.dir, "cli"))

		roots := readerRoots(t, home, share.reader(false),
			Location{Path: shareLocation, Kind: KindFolder}, Location{Path: workLocation, Kind: KindFolder})

		start := time.Now()
		found, err := roots.Clones(t.Context())
		require.NoError(t, err)
		assert.Equal(t, DefaultReadLimit, time.Since(start), "the location is skipped at the limit")
		assert.Equal(t, []string{api, web}, found.Dirs, "the other locations still list")
		assert.Equal(t, []Unread{{Dir: share.dir, State: StateNotAnswering}}, found.Skipped)
		assert.Equal(t, "~/share didn't answer in time", found.Skipped[0].Words(home))

		found, err = roots.Clones(t.Context())
		require.NoError(t, err)
		assert.Equal(t, []Unread{{Dir: share.dir, State: StateNotAnswering}}, found.Skipped)
		assert.Equal(t, int32(1), share.reads.Load(), "a read still hung is joined, never started again beside it")

		share.end()

		found, err = roots.Clones(t.Context())
		require.NoError(t, err)
		assert.Equal(t, Found{Dirs: []string{api, cli, web}, Skipped: []Unread{}}, found,
			"once the hung read ends, the next listing reads the location afresh")
		assert.Equal(t, int32(2), share.reads.Load())
	})
}

func TestReaderBoundsTheResolutionOfALocation(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		home := t.TempDir()
		api := fakeClone(t, filepath.Join(home, "projects", "api"))
		share := newStuck(filepath.Join(home, "share"))
		fakeClone(t, filepath.Join(share.dir, "cli"))

		reader := share.reader(true)

		start := time.Now()
		roots := readerRoots(t, home, reader, Location{Path: shareLocation, Kind: KindFolder})
		assert.Equal(t, DefaultReadLimit, time.Since(start), "resolving the location's links waits the limit at most")
		assert.Equal(t, []Place{
			{Path: filepath.Join(home, "projects"), Kind: KindFolder, Primary: true},
			{Path: share.dir, Kind: KindFolder},
		}, roots.Places(), "a location that doesn't answer is kept as it is written")

		found, err := roots.Clones(t.Context())
		require.NoError(t, err)
		assert.Equal(t, []string{api}, found.Dirs)
		assert.Equal(t, []Unread{{Dir: share.dir, State: StateNotAnswering}}, found.Skipped)
		assert.Equal(t, int32(0), share.reads.Load(), "a location whose links didn't resolve isn't read")

		readerRoots(t, home, reader, Location{Path: shareLocation, Kind: KindFolder})
		assert.Equal(t, int32(1), share.resolve.Load(), "a resolution still hung is joined")

		share.end()
	})
}

func TestReaderEndsWithTheContext(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		home := t.TempDir()
		fakeClone(t, filepath.Join(home, "projects", "api"))
		share := newStuck(filepath.Join(home, "share"))

		roots := readerRoots(t, home, share.reader(false), Location{Path: shareLocation, Kind: KindFolder})

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		_, err := roots.Clones(ctx)
		require.ErrorIs(t, err, context.Canceled)

		read := share.reader(false).within(ctx, Place{Path: share.dir, Kind: KindFolder})
		require.ErrorIs(t, read.err, context.Canceled)
		assert.Equal(t, StateNotAnswering, read.state)

		share.end()
	})
}

func TestCheckMarksALocationThatDoesNotAnswer(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		home := t.TempDir()
		fakeClone(t, filepath.Join(home, "projects", "api"))
		fakeClone(t, filepath.Join(home, "work", "web"))
		share := newStuck(filepath.Join(home, "share"))
		links := newStuck(filepath.Join(home, "links"))

		reader := share.reader(false)
		resolve := reader.resolve
		reader.resolve = func(dir string) string {
			if dir == links.dir {
				<-links.release
			}

			return resolve(dir)
		}

		got := Check(t.Context(), "~/projects", []Location{
			{Path: shareLocation, Kind: KindFolder},
			{Path: workLocation, Kind: KindFolder},
			{Path: "~/links", Kind: KindClone},
		}, home, nil, reader)

		assert.Equal(t, []LocationState{
			{Path: "~/projects", Kind: KindFolder, Primary: true, State: StateOK, Clones: 1},
			{Path: shareLocation, Kind: KindFolder, State: StateNotAnswering},
			{Path: workLocation, Kind: KindFolder, State: StateOK, Clones: 1},
			{Path: "~/links", Kind: KindClone, State: StateNotAnswering},
		}, got, "a read or a resolution that doesn't answer reads as not answering; the rest as they are")

		share.end()
		links.end()
	})
}

func TestNilReaderReadsDirectly(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	api := fakeClone(t, filepath.Join(home, "work", "api"))

	var reader *Reader

	read := reader.within(t.Context(), Place{Path: filepath.Join(home, "work"), Kind: KindFolder})
	assert.Equal(t, placeRead{dirs: []string{api}, state: StateOK}, read)

	resolved, answered := reader.Resolved(filepath.Join(home, "work"))
	assert.True(t, answered)
	assert.Equal(t, paths.Resolved(filepath.Join(home, "work")), resolved)

	assert.Equal(t, DefaultReadLimit, NewReader(0).limit, "a limit that isn't positive is the default")
	assert.Equal(t, time.Second, NewReader(time.Second).limit)
}

func TestReaderRefusesToAddAFolderThatDoesNotAnswer(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		home := t.TempDir()
		work := mkdir(t, filepath.Join(home, "work"))
		share := newStuck(mkdir(t, filepath.Join(home, "share")))
		reader := share.reader(false)

		kind, err := reader.Addable(t.Context(), work, home)
		require.NoError(t, err)
		assert.Equal(t, KindFolder, kind)

		_, err = reader.Addable(t.Context(), filepath.Join(home, ".hidden"), home)
		require.Error(t, err, "Classify's refusals come through")

		start := time.Now()
		_, err = reader.Addable(t.Context(), share.dir, home)
		require.ErrorIs(t, err, ErrNotAnswering)
		assert.Equal(t, DefaultReadLimit, time.Since(start))
		assert.Equal(t, "~/share didn't answer in time.", uierr.Message(err))

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		_, err = reader.Addable(ctx, share.dir, home)
		require.ErrorIs(t, err, context.Canceled)

		share.end()

		kind, err = reader.Addable(t.Context(), share.dir, home)
		require.NoError(t, err, "once the share answers, it can be added")
		assert.Equal(t, KindFolder, kind)

		var direct *Reader

		kind, err = direct.Addable(t.Context(), work, home)
		require.NoError(t, err)
		assert.Equal(t, KindFolder, kind, "a nil Reader classifies directly")
	})
}
