package core

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// folderOpened records the folders a terminal was asked to open, each with its script's
// name.
type folderOpened struct {
	opened [][2]string
}

func (f *folderOpened) OpenFolder(_ context.Context, name, dir string) error {
	f.opened = append(f.opened, [2]string{name, dir})

	return nil
}

func TestAWorktreesTerminalOpensOnceThereIsOne(t *testing.T) {
	t.Parallel()

	term := &worktreeTerminal{}

	err := term.OpenFolder(t.Context(), "/srv/worktrees/pr-7")
	require.ErrorIs(t, err, scriptrun.ErrNoFolderTerminal, "before the terminal is built, nothing opens")
	assert.Equal(t, "Your terminal can't be opened here.", uierr.Message(err))

	runs := &folderOpened{}
	term.opener = runs

	require.NoError(t, term.OpenFolder(t.Context(), "/srv/worktrees/pr-7"))
	assert.Equal(t, [][2]string{{scriptrun.FolderScript, "/srv/worktrees/pr-7"}}, runs.opened,
		"once it is, the worktree opens in it under the script that runs nothing")
}
