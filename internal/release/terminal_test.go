package release

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/uierr"
)

// errTerminalBusy is a terminal that refused to open.
var errTerminalBusy = errors.New("terminal refused")

// fakeFolders records the folders it was asked to open a terminal in.
type fakeFolders struct {
	mu     sync.Mutex
	opened []string
	err    error
}

func (f *fakeFolders) OpenFolder(_ context.Context, name, dir string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.opened = append(f.opened, name+" "+dir)

	return f.err
}

func TestOpenTerminalOpensOnlyAClonesFolder(t *testing.T) {
	t.Parallel()

	folders := &fakeFolders{}
	clones := &fakeClones{list: []Clone{
		{Dir: "/Users/example/projects/sdk", Origin: name("sdk"), OnHub: true},
		{Dir: "/Users/example/projects/notes"},
	}}
	svc := NewService(Config{GitHub: &fakeGitHub{}, Clones: clones, Home: "/Users/example", Terminal: folders})

	require.NoError(t, svc.OpenTerminal(t.Context(), "acme-org/sdk", "~/projects/sdk"))
	assert.Equal(t, []string{terminalScript + " /Users/example/projects/sdk"}, folders.opened, "nothing is run there")

	err := svc.OpenTerminal(t.Context(), "acme-org/web", "")
	require.ErrorIs(t, err, ErrNoClone, "only a clone of the repository opens")

	err = svc.OpenTerminal(t.Context(), "acme-org/sdk", "/etc")
	require.ErrorIs(t, err, ErrNoClone, "a folder that isn't the clone is refused")

	err = svc.OpenTerminal(t.Context(), "not a repo", "")
	require.ErrorIs(t, err, ErrInvalidRequest)

	assert.Len(t, folders.opened, 1)

	folders.err = errTerminalBusy
	require.ErrorIs(t, svc.OpenTerminal(t.Context(), "acme-org/sdk", ""), errTerminalBusy)

	bare := NewService(Config{GitHub: &fakeGitHub{}, Clones: clones, Home: "/Users/example"})
	err = bare.OpenTerminal(t.Context(), "acme-org/sdk", "")
	require.ErrorIs(t, err, ErrNoTerminal)
	assert.Equal(t, "Your terminal can't be opened here.", uierr.Message(err))
}
