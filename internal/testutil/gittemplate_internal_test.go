package testutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
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

func TestATemplateNamesItsFolderOnlyInText(t *testing.T) {
	t.Parallel()

	for name, prefix := range map[string]string{
		"a NUL byte":        "\x00",
		"bytes not UTF-8":   "\xff\xfe",
		"an index's header": "DIRC\x00\x00\x00\x02",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := buildTree(func(b *GitBuild) struct{} {
				b.WriteFile("blob", prefix+b.Root+"\n")

				return struct{}{}
			})

			require.ErrorIs(t, err, errTemplateBinary, "a rewrite of the folder's path would break the file")
			assert.Contains(t, err.Error(), "blob", "the build names the file")
		})
	}

	t.Run("a binary file that doesn't name it is kept as it is", func(t *testing.T) {
		t.Parallel()

		tree, err := buildTree(func(b *GitBuild) struct{} {
			b.WriteFile("blob", "\x00\xff")

			return struct{}{}
		})

		require.NoError(t, err)
		require.Len(t, tree.entries, 1)
		assert.Equal(t, []byte("\x00\xff"), tree.entries[0].data)
	})
}

func TestATemplateRefreshesOnlyTheIndexesItCanPlace(t *testing.T) {
	t.Parallel()

	t.Run("a clone's own index", func(t *testing.T) {
		t.Parallel()

		tree, err := buildTree(func(b *GitBuild) struct{} {
			b.WriteFile(filepath.Join("clone", ".git", "index"), "DIRC")
			b.WriteFile(filepath.Join("clone", "docs", "index"), "a page, not an index\n")

			return struct{}{}
		})

		require.NoError(t, err)
		assert.Equal(t, []string{"clone"}, tree.workTrees)
	})

	t.Run("a linked worktree outside the template", func(t *testing.T) {
		t.Parallel()

		_, err := buildTree(func(b *GitBuild) struct{} {
			admin := filepath.Join("clone", ".git", "worktrees", "away")
			b.WriteFile(filepath.Join(admin, "gitdir"), filepath.Join(filepath.Dir(b.Root), "away", ".git")+"\n")
			b.WriteFile(filepath.Join(admin, "index"), "DIRC")

			return struct{}{}
		})

		require.ErrorIs(t, err, errTemplateWorkTree, "its copy would hold no work tree to refresh")
	})

	t.Run("a submodule's index", func(t *testing.T) {
		t.Parallel()

		_, err := buildTree(func(b *GitBuild) struct{} {
			b.WriteFile(filepath.Join("clone", ".git", "modules", "lib", "index"), "DIRC")

			return struct{}{}
		})

		require.ErrorIs(t, err, errTemplateWorkTree, "a copy wouldn't know which work tree it indexes")
	})
}
