package prompts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/uierr"
)

func TestPreviewImportOfAFolderSaysItCantBeRead(t *testing.T) {
	t.Parallel()

	s, _, _ := keeping(t)

	folder := filepath.Join(t.TempDir(), "prompts.json")
	require.NoError(t, os.Mkdir(folder, 0o700))

	_, err := s.PreviewImport(t.Context(), ImportSource{Kind: SourceFile, Path: folder})
	require.ErrorIs(t, err, ErrNotBundle, "a folder is no prompt file")
	require.ErrorIs(t, err, paths.ErrNotRegular, "refused without being read")

	assert.Equal(t, "the file isn't a "+brand.Name+" prompts file: read the file: not a regular file", err.Error())
	assert.Equal(t, "The file isn't a "+brand.Name+" prompts file: read the file: not a regular file.", uierr.Message(err),
		"what the window shows")
}
