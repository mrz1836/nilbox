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

// webClone is the clone most open tests open.
const webClone = "web"

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
	cfg := Config{Clones: r.clones, Home: r.home}

	if opener != nil {
		cfg.Terminal, cfg.Reveal = opener, opener
	}

	return NewService(cfg)
}

// openCase is one row of the open tests: the clone the root holds, called clone (none
// when ""), made as opts says; whether the clone root can be read; the terminal and
// Finder (none with noOpener, failing with openErr otherwise); the folder asked for, with
// ~; and what comes of it.
type openCase struct {
	name     string
	clone    string
	opts     cloneOpts
	rootErr  error
	noOpener bool
	openErr  error
	dir      string

	// wantErr is the error the call fails with, or nil, wantText its words for the window
	// (uierr.Message) when set, and wantIn what its own text holds when set.
	wantErr  error
	wantText string
	wantIn   string

	// folder is the clone the terminal opened in, and shown the path, relative to the
	// clone, Finder showed: "" for none.
	folder string
	shown  string
}

// openRig is a light rig holding tt's clone, and the opener the call goes through, or nil.
func openRig(t *testing.T, tt openCase) (*rig, *fakeOpener) {
	t.Helper()

	r := newLightRig(t)
	r.clones.rootErr = tt.rootErr

	if tt.clone != "" {
		r.lightClone(tt.clone, tt.opts)
	}

	if tt.noOpener {
		return r, nil
	}

	return r, &fakeOpener{err: tt.openErr}
}

// checkOpen checks what came of tt's call: err, and what opener was asked to open.
func checkOpen(t *testing.T, r *rig, tt openCase, opener *fakeOpener, err error) {
	t.Helper()

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

	if tt.folder == "" {
		assert.Empty(t, opener.folder)
	} else {
		assert.Equal(t, []opened{{name: scriptrun.FolderScript, path: filepath.Join(r.root, tt.folder)}}, opener.folder)
	}

	if tt.shown == "" {
		assert.Empty(t, opener.shown)
	} else {
		assert.Equal(t, []string{filepath.Join(r.root, filepath.FromSlash(tt.shown))}, opener.shown)
	}
}

func TestOpenTerminal(t *testing.T) {
	t.Parallel()

	tests := []openCase{
		{
			name: "opens the clone and runs nothing", clone: webClone, opts: cloneOpts{config: coreEnv, origin: "acme-org/web"},
			dir: "~/projects/web", folder: webClone,
		},
		{name: "opens a clone without a config or a GitHub origin", clone: "plain", dir: "~/projects/plain", folder: "plain"},
		{
			name: "refuses a folder that isn't a clone", clone: webClone, opts: cloneOpts{config: coreEnv}, dir: "~/elsewhere",
			wantErr: ErrNoClone, wantText: "~/elsewhere isn't a clone in ~/projects.",
		},
		{
			name: "says when it can't", clone: webClone, opts: cloneOpts{config: coreEnv}, noOpener: true, dir: "~/projects/web",
			wantErr: scriptrun.ErrNoFolderTerminal, wantText: "Your terminal can't be opened here.",
		},
		{
			name: "passes the terminal's failure on", clone: webClone, opts: cloneOpts{config: coreEnv}, openErr: errFake,
			dir: "~/projects/web", wantErr: errFake, wantIn: "open the terminal in the clone", folder: webClone,
		},
		{name: "says when the clone root can't be read", rootErr: errFake, dir: "~/projects/web", wantErr: errFake},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r, opener := openRig(t, tt)
			err := openService(r, opener).OpenTerminal(t.Context(), tt.dir)

			checkOpen(t, r, tt, opener, err)
		})
	}
}

func TestShowConfig(t *testing.T) {
	t.Parallel()

	tests := []openCase{
		{
			name: "shows the modular folder", clone: webClone, opts: cloneOpts{config: coreEnv}, dir: "~/projects/web",
			shown: "web/.github/env",
		},
		{
			name: "shows the legacy file", clone: "old", opts: cloneOpts{legacy: coreEnv}, dir: "~/projects/old",
			shown: "old/.github/.env.base",
		},
		{
			name: "prefers the modular folder to the legacy file", clone: "both", opts: cloneOpts{config: coreEnv, legacy: coreEnv},
			dir: "~/projects/both", shown: "both/.github/env",
		},
		{
			name: "refuses a clone without a config", clone: "plain", dir: "~/projects/plain", wantErr: ErrNotSetUp,
			wantText: "~/projects/plain has no go-pre-commit config (.github/env/ or .github/.env.base).",
		},
		{name: "refuses a folder that isn't a clone", dir: "~/elsewhere", wantErr: ErrNoClone},
		{
			name: "says when Finder can't show it", clone: webClone, opts: cloneOpts{config: coreEnv}, noOpener: true,
			dir: "~/projects/web", wantErr: ErrNoFinder, wantText: brand.Name + " can't open Finder here.",
		},
		{
			name: "passes Finder's failure on", clone: webClone, opts: cloneOpts{config: coreEnv}, openErr: errFake,
			dir: "~/projects/web", wantErr: errFake, wantIn: "show the config", shown: "web/.github/env",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r, opener := openRig(t, tt)
			err := openService(r, opener).ShowConfig(t.Context(), tt.dir)

			checkOpen(t, r, tt, opener, err)
		})
	}
}
