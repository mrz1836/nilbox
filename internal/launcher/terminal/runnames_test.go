package terminal

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/scriptrun"
)

// TestScriptrunNamesArePruned ties the names scriptrun gives the scripts it has the
// terminal open, a run's and a terminal opened in a clone's, to the prefixes the launch
// folder's pruning knows: each is written, and pruned once it is old.
func TestScriptrunNamesArePruned(t *testing.T) {
	t.Parallel()

	folder := Script{
		Purpose: "to open a terminal in a run's folder", Dir: "/work/projects/sdk", Binary: "/bin/zsh",
		Args: []string{"-l"}, NoPrompt: true,
	}

	for name, s := range map[string]Script{scriptrun.FolderScript: folder, scriptrun.ScriptName(42): followedRun()} {
		dir := t.TempDir()

		old, err := WriteScript(dir, name, s, writeNow().Add(-scriptMaxAge-time.Second), fixedRandom(), nil)
		require.NoError(t, err, name)

		_, err = WriteScript(dir, name, s, writeNow(), nil, nil)
		require.NoError(t, err, name)
		assert.NoFileExists(t, old, "%s: pruned once it is old", name)
	}
}
