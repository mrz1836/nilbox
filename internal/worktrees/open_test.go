package worktrees

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

var errFakeOpen = errors.New("fake: it won't open")

// fakeOpener is the user's terminal and Finder: it records what each was asked to open,
// and fails with err when set.
type fakeOpener struct {
	mu     sync.Mutex
	err    error
	folder []string
	shown  []string
}

func (f *fakeOpener) OpenFolder(_ context.Context, dir string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.folder = append(f.folder, dir)

	return f.err
}

func (f *fakeOpener) Reveal(_ context.Context, path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.shown = append(f.shown, path)

	return f.err
}

// openRig is a clone, alpha, with a linked worktree, feature, and one whose folder is gone;
// a clone outside the repository locations; and a folder that is no worktree.
type openRig struct {
	e         *env
	repo      string
	worktree  string
	gone      string
	outside   string
	elsewhere string
}

func newOpenRig(t *testing.T) *openRig {
	t.Helper()

	e := newEnv(t)
	r := &openRig{e: e, repo: e.newRepo(t, "alpha")}
	r.worktree = addWorktree(t, r.repo, filepath.Join(e.home, "wt", "feature"), "feature")
	r.gone = addWorktree(t, r.repo, filepath.Join(e.home, "wt", "gone"), "gone")
	require.NoError(t, os.RemoveAll(r.gone))

	r.outside = filepath.Join(e.home, "outside")
	testutil.InitRepo(t, r.outside)

	r.elsewhere = filepath.Join(e.home, "elsewhere")
	require.NoError(t, os.MkdirAll(r.elsewhere, 0o700))

	return r
}

// service is a Service over the rig that opens the terminal and Finder through opener, or
// through nothing when opener is nil.
func (r *openRig) service(opener *fakeOpener) *Service {
	cfg := Config{Settings: fakeSettings{root: cloneRoot}, Git: r.e.git, Finder: execpath.Search{}, Home: r.e.home}

	if opener != nil {
		cfg.Terminal, cfg.Reveal = opener, opener
	}

	return NewService(cfg)
}

// openCase is one row of the open tests: the clone and the worktree asked for, as the
// rig names them; the terminal and Finder (none with noOpener, failing with openErr
// otherwise); and what comes of it.
type openCase struct {
	name     string
	clone    func(r *openRig) string
	path     func(r *openRig) string
	noOpener bool
	openErr  error

	// wantErr is the error the call fails with, or nil, wantText its words for the window
	// (uierr.Message) when set, and wantIn what its own text holds when set.
	wantErr  error
	wantText string
	wantIn   string

	// opens reports that the call reached the terminal or Finder, with the worktree.
	opens bool
}

// refusals are the requests both calls refuse before anything opens.
func refusals() []openCase {
	return []openCase{
		{
			name: "refuses a clone outside the repository locations", clone: func(r *openRig) string { return r.outside },
			path: func(r *openRig) string { return r.outside }, wantErr: ErrOutsideCloneRoot,
		},
		{
			name: "refuses the main worktree", clone: func(r *openRig) string { return r.repo },
			path: func(r *openRig) string { return r.repo }, wantErr: ErrMainWorktree,
		},
		{
			name: "refuses a folder that is no worktree of the clone", clone: func(r *openRig) string { return r.repo },
			path: func(r *openRig) string { return r.elsewhere }, wantErr: ErrNotAWorktree,
		},
		{
			name: "refuses a worktree whose folder is gone", clone: func(r *openRig) string { return r.repo },
			path: func(r *openRig) string { return r.gone }, wantErr: ErrPrunable,
			wantText: "The worktree's directory is gone; prune the repository instead.",
		},
	}
}

// alphaClone and featureWorktree name the rig's clone and its worktree that is there.
func alphaClone(r *openRig) string { return r.repo }

func featureWorktree(r *openRig) string { return r.worktree }

// runOpen makes tt's call with call, on a rig of its own, and checks what came of it: its
// error, and what the opener was asked to open, as seen.
func runOpen(t *testing.T, tt openCase, call func(*Service, context.Context, string, string) error,
	seen func(*fakeOpener) []string,
) {
	t.Helper()

	r := newOpenRig(t)

	var opener *fakeOpener
	if !tt.noOpener {
		opener = &fakeOpener{err: tt.openErr}
	}

	err := call(r.service(opener), t.Context(), tt.clone(r), tt.path(r))

	if tt.wantErr == nil {
		require.NoError(t, err)
	} else {
		require.ErrorIs(t, err, tt.wantErr)
	}

	if tt.wantText != "" {
		assert.Equal(t, tt.wantText, uierr.Message(err))
	}

	if tt.wantIn != "" {
		assert.Contains(t, err.Error(), tt.wantIn)
	}

	if opener == nil {
		return
	}

	if tt.opens {
		assert.Equal(t, []string{resolvedPath(r.worktree)}, seen(opener))
	} else {
		assert.Empty(t, seen(opener))
	}
}

func TestOpenTerminal(t *testing.T) {
	t.Parallel()

	tests := append([]openCase{
		{name: "opens the worktree and runs nothing", clone: alphaClone, path: featureWorktree, opens: true},
		{
			name: "says when it can't", clone: alphaClone, path: featureWorktree, noOpener: true,
			wantErr: ErrNoTerminal, wantText: "Your terminal can't be opened here.",
		},
		{
			name: "passes the terminal's failure on", clone: alphaClone, path: featureWorktree, openErr: errFakeOpen,
			wantErr: errFakeOpen, wantIn: "open the terminal in the worktree: ", opens: true,
		},
	}, refusals()...)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			runOpen(t, tt, (*Service).OpenTerminal, func(f *fakeOpener) []string { return resolvedPaths(f.folder) })
		})
	}
}

// resolvedPaths is each of paths with its links resolved.
func resolvedPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		out = append(out, resolvedPath(path))
	}

	return out
}

func TestReveal(t *testing.T) {
	t.Parallel()

	tests := append([]openCase{
		{name: "shows the worktree in Finder", clone: alphaClone, path: featureWorktree, opens: true},
		{
			name: "says when Finder can't show it", clone: alphaClone, path: featureWorktree, noOpener: true,
			wantErr: ErrNoFinder, wantText: brand.Name + " can't open Finder here.",
		},
		{
			name: "passes Finder's failure on", clone: alphaClone, path: featureWorktree, openErr: errFakeOpen,
			wantErr: errFakeOpen, wantIn: "show the worktree in Finder", opens: true,
		},
	}, refusals()...)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			runOpen(t, tt, (*Service).Reveal, func(f *fakeOpener) []string { return resolvedPaths(f.shown) })
		})
	}
}
