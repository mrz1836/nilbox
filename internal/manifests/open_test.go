package manifests

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// fakeOpener is the user's terminal and Finder: it records what each was asked to open,
// and fails with err when set.
type fakeOpener struct {
	mu      sync.Mutex
	err     error
	folders []string
	shown   []string
}

func (f *fakeOpener) OpenFolder(_ context.Context, dir string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.folders = append(f.folders, dir)

	return f.err
}

func (f *fakeOpener) Reveal(_ context.Context, path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.shown = append(f.shown, path)

	return f.err
}

// openRig returns a rig holding the clone api, with web/package.json in its checkout, and
// a Service over its clones that opens the terminal and Finder through opener, or
// through nothing when opener is nil.
func openRig(t *testing.T, opener *fakeOpener) (*rig, *Service) {
	t.Helper()

	r := newRig(t)
	api := r.clone("api", moduleRepo(commitID(1), daysAgo(10), daysAgo(5)))
	testutil.WriteFile(t, filepath.Join(api, "web", "package.json"), "{}\n")

	cfg := Config{Clones: r.clones, Git: r.git, Home: r.home}
	if opener != nil {
		cfg.Terminal, cfg.Reveal = opener, opener
	}

	return r, NewService(cfg)
}

func TestOpenTerminalOpensTheClone(t *testing.T) {
	t.Parallel()

	opener := &fakeOpener{}
	r, svc := openRig(t, opener)

	require.NoError(t, svc.OpenTerminal(t.Context(), "~/projects/api"))
	assert.Equal(t, []string{filepath.Join(r.root, "api")}, opener.folders)
}

func TestOpenTerminalRefusesWhatItCant(t *testing.T) {
	t.Parallel()

	_, svc := openRig(t, &fakeOpener{})

	err := svc.OpenTerminal(t.Context(), "~/projects/nope")
	require.ErrorIs(t, err, ErrNoClone)
	assert.Equal(t, "~/projects/nope isn't a clone in ~/projects.", uierr.Message(err))

	_, svc = openRig(t, nil)

	err = svc.OpenTerminal(t.Context(), "~/projects/api")
	require.ErrorIs(t, err, ErrNoTerminal)
	assert.Equal(t, "Your terminal can't be opened here.", uierr.Message(err))

	_, svc = openRig(t, &fakeOpener{err: errFake})

	err = svc.OpenTerminal(t.Context(), "~/projects/api")
	require.ErrorIs(t, err, errFake)
}

func TestRevealShowsAManifestInFinder(t *testing.T) {
	t.Parallel()

	opener := &fakeOpener{}
	r, svc := openRig(t, opener)

	require.NoError(t, svc.Reveal(t.Context(), "~/projects/api", webPackage))
	assert.Equal(t, []string{filepath.Join(r.root, "api", "web", "package.json")}, opener.shown)
}

func TestRevealRefusesAPathThatIsntAManifestBeforeReadingAnything(t *testing.T) {
	t.Parallel()

	opener := &fakeOpener{}
	r, svc := openRig(t, opener)
	reads := r.clones.Reads()

	for _, path := range []string{
		"", "README.md", "../api/go.mod", "/etc/go.mod", "web/../go.mod", "./go.mod", "-x/go.mod", "web//package.json",
		"web/package.json/", "a\nb/go.mod", ":(glob)go.mod", "go.mod.bak",
	} {
		err := svc.Reveal(t.Context(), "~/projects/api", path)
		require.ErrorIs(t, err, ErrInvalidRequest, "%q", path)
		assert.Equal(t, "That isn't a go.mod, a package.json, or a lockfile in this clone.", uierr.Message(err))
	}

	assert.Empty(t, opener.shown)
	assert.Equal(t, reads, r.clones.Reads(), "the locations aren't read for a path that can't be one")
}

func TestRevealRefusesAFileNotInTheCheckoutOrOutsideTheClone(t *testing.T) {
	t.Parallel()

	opener := &fakeOpener{}
	r, svc := openRig(t, opener)

	err := svc.Reveal(t.Context(), "~/projects/api", toolsGoMod)
	require.ErrorIs(t, err, ErrInvalidRequest)
	assert.Equal(t, "tools/go.mod isn't in this clone's checkout: the branch it's on may not have it.", uierr.Message(err))

	outside := t.TempDir()
	testutil.WriteFile(t, filepath.Join(outside, "package.json"), "{}\n")
	require.NoError(t, os.Symlink(outside, filepath.Join(r.root, "api", "linked")))

	err = svc.Reveal(t.Context(), "~/projects/api", "linked/package.json")
	require.ErrorIs(t, err, ErrInvalidRequest)
	assert.Equal(t, "linked/package.json leads outside this clone.", uierr.Message(err))

	err = svc.Reveal(t.Context(), "~/projects/nope", webPackage)
	require.ErrorIs(t, err, ErrNoClone)

	assert.Empty(t, opener.shown)
}

func TestRevealWithoutFinder(t *testing.T) {
	t.Parallel()

	_, svc := openRig(t, nil)

	err := svc.Reveal(t.Context(), "~/projects/api", webPackage)
	require.ErrorIs(t, err, ErrNoFinder)
	assert.Equal(t, brand.Name+" can't open Finder here.", uierr.Message(err))

	_, svc = openRig(t, &fakeOpener{err: errFake})

	err = svc.Reveal(t.Context(), "~/projects/api", webPackage)
	require.ErrorIs(t, err, errFake)
}
