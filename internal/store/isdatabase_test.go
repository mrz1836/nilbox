package store_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestIsDatabaseKnowsADatabaseByItsHeader(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	db := filepath.Join(dir, "app.db")
	testutil.DBAt(t, db)

	linked := filepath.Join(dir, "linked.db")
	require.NoError(t, os.Symlink(db, linked))

	headed := filepath.Join(dir, "headed")
	testutil.WriteFile(t, headed, "SQLite format 3\x00and then anything")

	short := filepath.Join(dir, "short")
	testutil.WriteFile(t, short, "SQLite format 3")

	text := filepath.Join(dir, "prompts.json")
	testutil.WriteFile(t, text, `{"format":"x"}`)

	blank := filepath.Join(dir, "blank")
	testutil.WriteFile(t, blank, "")

	for path, want := range map[string]bool{
		db: true, linked: true, headed: true, short: false, text: false, blank: false,
	} {
		got, err := store.IsDatabase(path)
		require.NoError(t, err, filepath.Base(path))
		assert.Equal(t, want, got, filepath.Base(path))
	}
}

func TestIsDatabaseRefusesWhatIsNoRegularFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	_, err := store.IsDatabase(dir)
	require.ErrorIs(t, err, paths.ErrNotRegular, "a folder is never read")

	_, err = store.IsDatabase(filepath.Join(dir, "missing.db"))
	require.ErrorIs(t, err, fs.ErrNotExist)
}
