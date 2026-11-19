package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/backup"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/prompts"
)

// exportAll writes c's whole setup to a backup in a fresh folder, and returns its path.
func exportAll(t *testing.T, c *Core) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "backup.json")
	_, err := c.Backup.Export(t.Context(), path, backup.Sections())
	require.NoError(t, err)

	return path
}

// rewriteBackup edits the backup at path's sections, as another copy of the app, or a
// hand, might have written them.
func rewriteBackup(t *testing.T, path string, edit func(sections map[string]json.RawMessage)) {
	t.Helper()

	data, err := os.ReadFile(path) //nolint:gosec // a file the test wrote
	require.NoError(t, err)

	var sections map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &sections))

	edit(sections)

	data, err = json.MarshalIndent(sections, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o600))
}

// sectionPreview is the preview of section in p.
func sectionPreview(t *testing.T, p backup.Preview, section backup.Section) backup.SectionPreview {
	t.Helper()

	i := slices.IndexFunc(p.Sections, func(s backup.SectionPreview) bool { return s.Section == section })
	require.GreaterOrEqual(t, i, 0, "the preview shows %s", section)

	return p.Sections[i]
}

func TestABackupsSectionThatCantBeReadSaysSoInThePreviewAlone(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, backupOptions(t))
	path := exportAll(t, c)

	// Each section is an object, as a backup's must be, holding a field of the wrong type.
	rewriteBackup(t, path, func(sections map[string]json.RawMessage) {
		sections[string(backup.SectionSettings)] = json.RawMessage(`{"defaultAgent": 42}`)
		sections[string(backup.SectionRepositories)] = json.RawMessage(`{"cloneRoot": 42}`)
		sections[string(backup.SectionScripts)] = json.RawMessage(`{"scripts": 42}`)

		for _, section := range []backup.Section{backup.SectionPrompts, backup.SectionLaunchDefaults, backup.SectionAutopilot} {
			sections[string(section)] = json.RawMessage(`{"format": 42}`)
		}
	})

	p, err := c.Backup.Preview(t.Context(), path)
	require.NoError(t, err, "a file whose sections can't be read still previews")

	for _, section := range backup.Sections() {
		got := sectionPreview(t, p, section)
		assert.True(t, got.Present, section)
		assert.NotEmpty(t, got.Error, "%s says it can't be imported", section)
	}

	assert.Contains(t, sectionPreview(t, p, backup.SectionSettings).Error, "settings can't be read")
	assert.Contains(t, sectionPreview(t, p, backup.SectionRepositories).Error, "repositories can't be read")
	assert.Contains(t, sectionPreview(t, p, backup.SectionScripts).Error, "scripts can't be read")
}

func TestABackupOfADatabaseThatCantBeReadWritesNothingAndPlansNothing(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, backupOptions(t))
	path := exportAll(t, c)
	require.NoError(t, c.Close())

	for _, section := range backup.Sections() {
		out := filepath.Join(t.TempDir(), "backup.json")
		_, err := c.Backup.Export(t.Context(), out, []backup.Section{section})
		require.Error(t, err, "%s can't be exported", section)
		assert.NoFileExists(t, out, "and no file is left for %s", section)
	}

	p, err := c.Backup.Preview(t.Context(), path)
	require.NoError(t, err)

	for _, section := range backup.Sections() {
		assert.NotEmpty(t, sectionPreview(t, p, section).Error, "%s can't be planned over a database that can't be read",
			section)
	}
}

func TestALaunchDefaultImportedWithoutItsPromptsNamesTheLibrarysPrompt(t *testing.T) {
	t.Parallel()

	from := bootstrap(t, backupOptions(t))
	ctx := t.Context()

	review := promptNamed(t, from, "Review")
	_, err := from.LaunchDefaults.Save(ctx, launchdefaults.Input{Owner: boundOwner, PRPromptID: &review.ID})
	require.NoError(t, err)

	path := exportAll(t, from)

	// Another Mac's library has the prompt under its own ID, and a prompt of its own first.
	to := bootstrap(t, backupOptions(t))
	_, err = to.Prompts.Create(ctx, prompts.Input{
		Name: "Mine first", Nickname: "Mine", AppliesTo: prompts.AppliesToAny, Body: "Look at {{.URL}}",
	})
	require.NoError(t, err)

	importBackup(t, to, path, backup.ModeMerge, backup.SectionLaunchDefaults)

	defaults, err := to.LaunchDefaults.List(ctx)
	require.NoError(t, err)
	require.Len(t, defaults, 1)
	require.NotNil(t, defaults[0].PRPromptID)
	assert.Equal(t, promptNamed(t, to, "Review").ID, *defaults[0].PRPromptID,
		"the default names this library's prompt of that nickname")
}

func TestAReplaceOfLaunchDefaultsInheritsAPromptTheFileDoesntBring(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, backupOptions(t))
	ctx := t.Context()

	mine, err := c.Prompts.Create(ctx, prompts.Input{
		Name: "My own review", Nickname: "Mine", AppliesTo: prompts.AppliesToAny, Body: "Review {{.URL}}",
	})
	require.NoError(t, err)

	_, err = c.LaunchDefaults.Save(ctx, launchdefaults.Input{Owner: boundOwner, PRPromptID: &mine.ID, Worktree: new(false)})
	require.NoError(t, err)

	other := "other-org"
	_, err = c.LaunchDefaults.Save(ctx, launchdefaults.Input{Owner: other, Worktree: new(true)})
	require.NoError(t, err)

	path := exportAll(t, c)

	// The file's library lacks the prompt its launch defaults name, and holds only the first
	// org's defaults.
	rewriteBackup(t, path, func(sections map[string]json.RawMessage) {
		var bundle map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(sections[string(backup.SectionPrompts)], &bundle))

		var list []map[string]any
		require.NoError(t, json.Unmarshal(bundle["prompts"], &list))

		list = slices.DeleteFunc(list, func(p map[string]any) bool { return p["nickname"] == "Mine" })

		raw, encodeErr := json.Marshal(list)
		require.NoError(t, encodeErr)

		bundle["prompts"] = raw
		sections[string(backup.SectionPrompts)], encodeErr = json.Marshal(bundle)
		require.NoError(t, encodeErr)

		var kept map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(sections[string(backup.SectionLaunchDefaults)], &kept))

		var rows []map[string]any
		require.NoError(t, json.Unmarshal(kept["defaults"], &rows))

		rows = slices.DeleteFunc(rows, func(r map[string]any) bool { return r["owner"] == other })

		raw, encodeErr = json.Marshal(rows)
		require.NoError(t, encodeErr)

		kept["defaults"] = raw
		sections[string(backup.SectionLaunchDefaults)], encodeErr = json.Marshal(kept)
		require.NoError(t, encodeErr)
	})

	p, err := c.Backup.Preview(ctx, path)
	require.NoError(t, err)

	defaults := sectionPreview(t, p, backup.SectionLaunchDefaults)
	require.Empty(t, defaults.Error)
	assert.Empty(t, defaults.Merge.Notes, "a merge keeps the library, which has the prompt")
	require.Len(t, defaults.Replace.Notes, 1, "a replace leaves the library only the file's prompts")
	assert.Contains(t, defaults.Replace.Notes[0], "“Mine”, isn't here, so it inherits.")

	importBackup(t, c, path, backup.ModeReplace, backup.SectionPrompts, backup.SectionLaunchDefaults)

	got, err := c.LaunchDefaults.List(ctx)
	require.NoError(t, err)
	require.Len(t, got, 1, "Replace clears the defaults the file doesn't hold first")
	assert.Equal(t, boundOwner, got[0].Owner)
	assert.Nil(t, got[0].PRPromptID, "its prompt inherits")
	require.NotNil(t, got[0].Worktree)
	assert.False(t, *got[0].Worktree, "and what else it sets comes back")
	assert.Zero(t, promptNamed(t, c, "Mine").ID, "the prompt the file doesn't bring is gone")
}
