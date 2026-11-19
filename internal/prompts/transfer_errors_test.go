package prompts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// beneathAFile returns a path whose folder is a regular file, so nothing can be found,
// listed, or written there.
func beneathAFile(t *testing.T, name string) string {
	t.Helper()

	file := filepath.Join(t.TempDir(), "a-file")
	testutil.WriteFile(t, file, "not a folder")

	return filepath.Join(file, name)
}

// seededWith returns a Service over a seeded database that keeps its library file at file
// and finds its backups beside database, either "" for none.
func seededWith(t *testing.T, file, database string) *Service {
	t.Helper()

	s, db := seeded(t, nil)

	cfg := Config{Store: s.store, Usage: launchDefaults{db: db}, Database: database}
	if file != "" {
		cfg.File = NewLibraryFile(file, "", nil)
	}

	return NewService(cfg)
}

func TestExportReportsALibraryItCannotRead(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "x.json")

	_, err := closedService(t).Export(t.Context(), path, nil)
	require.ErrorContains(t, err, "read setting "+prefDefaultPR)

	s, _ := seeded(t, nil)
	require.NoError(t, s.store.prefs.Set(t.Context(), prefSeeded, "not a list"))

	_, err = s.Export(t.Context(), path, nil)
	require.ErrorContains(t, err, "read setting "+prefSeeded)
	assert.NoFileExists(t, path, "nothing is written")
}

func TestBackupsWithNoCopiesListsNone(t *testing.T) {
	t.Parallel()

	got, err := seededWith(t, "", "").Backups(t.Context())
	require.NoError(t, err)
	assert.Empty(t, got, "a library that keeps no file and no database lists nothing")

	folder := filepath.Join(t.TempDir(), "prompts.json")
	require.NoError(t, os.Mkdir(folder, appdata.PrivateDirPerm))

	got, err = seededWith(t, folder, filepath.Join(t.TempDir(), "app.db")).Backups(t.Context())
	require.NoError(t, err)
	assert.Empty(t, got, "a folder in the library file's place is no copy of the library")
}

func TestBackupsReportsCopiesItCannotList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		file, database string
		want           string
	}{
		{name: "the library file", file: beneathAFile(t, "prompts.json"), want: "find the copy of the prompt library"},
		{name: "the databases", database: beneathAFile(t, "app.db"), want: "list the database's backups"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := seededWith(t, tt.file, tt.database)

			_, err := s.Backups(t.Context())
			require.ErrorContains(t, err, tt.want)

			_, err = s.PreviewImport(t.Context(), ImportSource{Kind: SourceBackup, Path: "/backups/app.db.broken"})
			require.ErrorContains(t, err, tt.want, "a backup is looked for among those listed")
		})
	}
}

func TestBackupsReportsALibraryItCannotCompareWith(t *testing.T) {
	t.Parallel()

	_, err := closedService(t).Backups(t.Context())
	require.ErrorContains(t, err, "read the prompts")

	s, db := seeded(t, nil)
	testutil.Exec(t, db, "DROP TABLE launch_defaults")

	_, err = s.Backups(t.Context())
	require.ErrorContains(t, err, "read where the prompts are used")
}

func TestPreviewImportAndImportReportALibraryTheyCannotRead(t *testing.T) {
	t.Parallel()

	source := ImportSource{Kind: SourceFile, Path: writeBundle(t, bundleOf(mine("Fresh")))}

	_, err := closedService(t).PreviewImport(t.Context(), source)
	require.ErrorContains(t, err, "read setting "+prefDefaultPR)

	s, db := seeded(t, nil)
	testutil.Exec(t, db, "DROP TABLE launch_defaults")

	_, err = s.PreviewImport(t.Context(), source)
	require.ErrorContains(t, err, "read where the prompts are used")

	_, err = s.Import(t.Context(), []BundlePrompt{mine("Fresh")}, BundleDefaults{})
	require.ErrorContains(t, err, "read where the prompts are used")

	all, err := s.store.All(t.Context())
	require.NoError(t, err)
	assert.Len(t, all, len(Starters()), "nothing is imported")
}

func TestPreviewImportRefusesADatabaseWithoutPrompts(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "other.db")
	other := testutil.DBAt(t, path)
	testutil.Exec(t, other, "DROP TABLE prompts")
	require.NoError(t, other.Close())

	s, _ := seeded(t, nil)

	_, err := s.PreviewImport(t.Context(), ImportSource{Kind: SourceFile, Path: path})
	require.ErrorIs(t, err, store.ErrNoTable)
}

func TestRestoreReportsALibraryItCannotCheck(t *testing.T) {
	t.Parallel()

	s, db := empty(t, nil)
	testutil.Exec(t, db, "DROP TABLE prompts")

	restored, err := Restore(t.Context(), s.store, "", NewLibraryFile(writeBundle(t, bundleOf(mine("Fresh"))), "", nil))
	require.ErrorContains(t, err, "read the prompts", "whether the library exists is not known")
	assert.Equal(t, Restored{}, restored)
}

func TestPreviewImportReportsAFileItCannotRead(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)

	for name, path := range map[string]string{
		"a file that is not there": filepath.Join(t.TempDir(), "missing.json"),
		"a folder":                 t.TempDir(),
	} {
		_, err := s.PreviewImport(t.Context(), ImportSource{Kind: SourceFile, Path: path})
		require.ErrorContains(t, err, "read the file", name)
	}
}
