package hooks

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// opened is one folder the fake terminal or Finder was asked to open.
type opened struct {
	name string
	path string
}

// fakeOpener is the user's terminal and Finder: it records what each was asked to open,
// and fails with err when set.
type fakeOpener struct {
	mu     sync.Mutex
	err    error
	folder []opened
	shown  []string
}

func (f *fakeOpener) OpenFolder(_ context.Context, name, dir string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.folder = append(f.folder, opened{name: name, path: dir})

	return f.err
}

func (f *fakeOpener) Reveal(_ context.Context, path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.shown = append(f.shown, path)

	return f.err
}

// openService is the rig's clones, under a Service that opens the terminal and Finder
// through opener, or through nothing when opener is nil.
func openService(r *rig, opener *fakeOpener) *Service {
	cfg := Config{Clones: r.clones, Git: r.git, Home: r.home}

	if opener != nil {
		cfg.Terminal, cfg.Reveal = opener, opener
	}

	return NewService(cfg)
}

func TestOpenTerminalOpensTheCloneAndRunsNothing(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("web", cloneOpts{config: coreEnv, origin: "acme-org/web"})
	opener := &fakeOpener{}

	require.NoError(t, openService(r, opener).OpenTerminal(t.Context(), "~/projects/web"))
	assert.Equal(t, []opened{{name: scriptrun.FolderScript, path: dir}}, opener.folder)
	assert.Empty(t, opener.shown)
}

func TestOpenTerminalOpensACloneWithoutAConfigOrAGitHubOrigin(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("plain", cloneOpts{})
	opener := &fakeOpener{}

	require.NoError(t, openService(r, opener).OpenTerminal(t.Context(), r.shown(dir)))
	assert.Equal(t, []opened{{name: scriptrun.FolderScript, path: dir}}, opener.folder)
}

func TestOpenTerminalRefusesAFolderThatIsntAClone(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.clone("web", cloneOpts{config: coreEnv})

	opener := &fakeOpener{}
	err := openService(r, opener).OpenTerminal(t.Context(), "~/elsewhere")

	require.ErrorIs(t, err, ErrNoClone)
	assert.Equal(t, "~/elsewhere isn't a clone in ~/projects.", uierr.Message(err))
	assert.Empty(t, opener.folder)
}

func TestOpenTerminalSaysWhenItCant(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.clone("web", cloneOpts{config: coreEnv})

	err := openService(r, nil).OpenTerminal(t.Context(), "~/projects/web")

	require.ErrorIs(t, err, scriptrun.ErrNoFolderTerminal)
	assert.Equal(t, "Your terminal can't be opened here.", uierr.Message(err))
}

func TestOpenTerminalPassesTheTerminalsFailureOn(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.clone("web", cloneOpts{config: coreEnv})

	err := openService(r, &fakeOpener{err: errFake}).OpenTerminal(t.Context(), "~/projects/web")

	require.ErrorIs(t, err, errFake)
	assert.Contains(t, err.Error(), "open the terminal in the clone")
}

func TestOpenTerminalSaysWhenTheCloneRootCantBeRead(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.clones.rootErr = errFake

	require.ErrorIs(t, openService(r, &fakeOpener{}).OpenTerminal(t.Context(), "~/projects/web"), errFake)
}

func TestShowConfigShowsTheModularFolder(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("web", cloneOpts{config: coreEnv})
	opener := &fakeOpener{}

	require.NoError(t, openService(r, opener).ShowConfig(t.Context(), "~/projects/web"))
	assert.Equal(t, []string{filepath.Join(dir, ".github", "env")}, opener.shown)
	assert.Empty(t, opener.folder)
}

func TestShowConfigShowsTheLegacyFile(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("old", cloneOpts{legacy: coreEnv})
	opener := &fakeOpener{}

	require.NoError(t, openService(r, opener).ShowConfig(t.Context(), "~/projects/old"))
	assert.Equal(t, []string{filepath.Join(dir, ".github", ".env.base")}, opener.shown)
}

func TestShowConfigPrefersTheModularFolderToTheLegacyFile(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("both", cloneOpts{config: coreEnv, legacy: coreEnv})
	opener := &fakeOpener{}

	require.NoError(t, openService(r, opener).ShowConfig(t.Context(), "~/projects/both"))
	assert.Equal(t, []string{filepath.Join(dir, ".github", "env")}, opener.shown)
}

func TestShowConfigRefusesACloneWithoutAConfig(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.clone("plain", cloneOpts{})

	opener := &fakeOpener{}
	err := openService(r, opener).ShowConfig(t.Context(), "~/projects/plain")

	require.ErrorIs(t, err, ErrNotSetUp)
	assert.Equal(t, "~/projects/plain has no go-pre-commit config (.github/env/ or .github/.env.base).",
		uierr.Message(err))
	assert.Empty(t, opener.shown)
}

func TestShowConfigRefusesAFolderThatIsntAClone(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	opener := &fakeOpener{}

	require.ErrorIs(t, openService(r, opener).ShowConfig(t.Context(), "~/elsewhere"), ErrNoClone)
	assert.Empty(t, opener.shown)
}

func TestShowConfigSaysWhenFinderCantShowIt(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.clone("web", cloneOpts{config: coreEnv})

	err := openService(r, nil).ShowConfig(t.Context(), "~/projects/web")

	require.ErrorIs(t, err, ErrNoFinder)
	assert.Equal(t, brand.Name+" can't open Finder here.", uierr.Message(err))
}

func TestShowConfigPassesFindersFailureOn(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.clone("web", cloneOpts{config: coreEnv})

	err := openService(r, &fakeOpener{err: errFake}).ShowConfig(t.Context(), "~/projects/web")

	require.ErrorIs(t, err, errFake)
	assert.Contains(t, err.Error(), "show the config")
}
