package backup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestExportReportsWhatItCannotWrite(t *testing.T) {
	t.Parallel()

	r := newRig(t, "here")
	path := filepath.Join(t.TempDir(), "backup.json")

	_, err := r.svc.Export(t.Context(), filepath.Join(t.TempDir(), "missing", "backup.json"), Sections())
	require.ErrorContains(t, err, "save the backup")

	r.parts[SectionScripts].exportErr = errBroken

	_, err = r.svc.Export(t.Context(), path, Sections())
	require.ErrorIs(t, err, errBroken)
	require.ErrorContains(t, err, "back up the scripts")
	assert.NoFileExists(t, path, "no backup is written without every category")
}

func TestPreviewRefusesWhatIsNoBackup(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	large := filepath.Join(dir, "large.json")
	testutil.WriteFile(t, large, "{}")
	require.NoError(t, os.Truncate(large, MaxBytes+1))

	notJSON := filepath.Join(dir, "notes.json")
	testutil.WriteFile(t, notJSON, "not a backup")

	tests := []struct {
		name string
		path string
		want error
	}{
		{name: "a relative path", path: "backup.json", want: ErrNoPath},
		{name: "a file that is not there", path: filepath.Join(dir, "missing.json"), want: os.ErrNotExist},
		{name: "a folder", path: dir, want: ErrNotBackup},
		{name: "a file too large", path: large, want: ErrTooLarge},
		{name: "a file of other text", path: notJSON, want: ErrNotBackup},
	}

	r := newRig(t, "here")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := r.svc.Preview(t.Context(), tt.path)
			require.ErrorIs(t, err, tt.want)
		})
	}

	_, err := r.svc.Preview(t.Context(), dir)
	require.ErrorIs(t, err, appdata.ErrNotAFile, "a folder is named as no file")
}

func TestImportRefusesAFileGoneSinceItsPreview(t *testing.T) {
	t.Parallel()

	r := newRig(t, "file")
	path := r.export(t, Sections()...)

	p, err := r.svc.Preview(t.Context(), path)
	require.NoError(t, err)
	require.NoError(t, os.Remove(path))

	_, err = r.svc.Import(t.Context(), Request{Path: path, Hash: p.Hash, Mode: ModeMerge, Sections: Sections()})
	require.ErrorIs(t, err, os.ErrNotExist)
	assert.Empty(t, r.applied())
}

func TestImportStopsWhenTheSetupCannotBeSavedFirst(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		spoil func(t *testing.T, dir string)
	}{
		{
			name: "a file where the backups folder goes",
			spoil: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(filepath.Dir(dir), appdata.PrivateDirPerm))
				testutil.WriteFile(t, dir, "not a folder")
			},
		},
		{
			name: "a backups folder that cannot be written",
			spoil: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(dir, appdata.PrivateDirPerm))
				require.NoError(t, os.Chmod(dir, 0o500)) //nolint:gosec // a folder the test made, which must stay searchable
				t.Cleanup(func() { _ = os.Chmod(dir, appdata.PrivateDirPerm) })
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newRig(t, "file")
			path := r.export(t, SectionSettings)

			p, err := r.svc.Preview(t.Context(), path)
			require.NoError(t, err)

			tt.spoil(t, r.dir)

			_, err = r.svc.Import(t.Context(), Request{
				Path: path, Hash: p.Hash, Mode: ModeMerge, Sections: []Section{SectionSettings},
			})
			require.ErrorContains(t, err, "save the setup as it is before the import")
			assert.Empty(t, r.applied(), "nothing is imported without the copy saved first")
		})
	}
}

func TestReplaceStopsWhenAClearFails(t *testing.T) {
	t.Parallel()

	r := newRig(t, "file")
	path := r.export(t, Sections()...)

	p, err := r.svc.Preview(t.Context(), path)
	require.NoError(t, err)

	require.NoError(t, prefs.New(r.db, nil).Set(t.Context(), "test.cleared", "x"))
	testutil.Exec(t, r.db, `CREATE TRIGGER refuse_clear BEFORE DELETE ON prefs
		BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`)

	_, err = r.svc.Import(t.Context(), Request{
		Path: path, Hash: p.Hash, Mode: ModeReplace, Sections: []Section{SectionPrompts, SectionLaunchDefaults},
	})
	require.ErrorContains(t, err, "import the backup: launchDefaults: ")
	require.ErrorContains(t, err, "refused by the test")
	assert.Equal(t, []string{"clear launchDefaults"}, r.applied(), "nothing is applied after a clear that failed")

	_, ok := r.stored(t, SectionPrompts)
	assert.False(t, ok)
}

func TestEncodeRefusesWhatIsNotJSON(t *testing.T) {
	t.Parallel()

	_, err := Encode(File{Format: Format, Version: Version, Prompts: json.RawMessage(`{"cut off`)})
	require.ErrorContains(t, err, "write the backup")

	_, err = EncodeSection(func() {})
	require.ErrorContains(t, err, "write the backup")
}
