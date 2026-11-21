package prompts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/uierr"
)

func TestPreviewImportOfAFolderSaysItCantBeRead(t *testing.T) {
	t.Parallel()

	s, _, _ := keeping(t)

	folder := filepath.Join(t.TempDir(), "prompts.json")
	require.NoError(t, os.Mkdir(folder, 0o700))

	_, err := s.PreviewImport(t.Context(), ImportSource{Kind: SourceFile, Path: folder})
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrNotBundle, "read as a file, not judged as a prompt file")

	assert.Equal(t, "read the file: read "+folder+": is a directory", err.Error())
	assert.Equal(t, "Read the file: read "+folder+": is a directory.", uierr.Message(err),
		"what the window shows")
}
