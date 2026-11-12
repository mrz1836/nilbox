package testutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestATemplateHoldsOnlyFoldersAndFiles(t *testing.T) {
	t.Parallel()

	_, err := buildTree(func(b *GitBuild) struct{} {
		require.NoError(t, os.Symlink("elsewhere", filepath.Join(b.Root, "link")))

		return struct{}{}
	})

	require.ErrorIs(t, err, errTemplateEntry)
}
