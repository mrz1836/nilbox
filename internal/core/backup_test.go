package core

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/backup"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/reporules"
	"github.com/mrz1836/nilbox/internal/scripts"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/testutil"
)

var update = flag.Bool("update", false, "rewrite the golden files from the current output") //nolint:gochecknoglobals // a test flag

// backupOptions are testOptions with a fixed clock and version, so a backup reads the same
// on every run.
func backupOptions(t *testing.T) Options {
	t.Helper()

	opts := testOptions(t, ".claude")
	opts.Clock = testutil.NewClock(time.Date(2026, time.November, 14, 17, 0, 0, 0, time.UTC))
	opts.Version = "v1.2.3"

	return opts
}

// importBackup previews the backup at path and imports sections of it under mode.
func importBackup(t *testing.T, c *Core, path string, mode backup.Mode, sections ...backup.Section) backup.Imported {
	t.Helper()

	p, err := c.Backup.Preview(t.Context(), path)
	require.NoError(t, err)

	got, err := c.Backup.Import(t.Context(), backup.Request{Path: path, Hash: p.Hash, Sections: sections, Mode: mode})
	require.NoError(t, err)

	return got
}

// scriptNames lists the user's scripts by name, in their order.
func scriptNames(t *testing.T, c *Core) []string {
	t.Helper()

	all, err := scripts.NewBackupPart(c.Scripts).PortableScripts(t.Context())
	require.NoError(t, err)

	out := make([]string, 0, len(all.Scripts))
	for _, s := range all.Scripts {
		out = append(out, s.Name)
	}

	return out
}

func TestBackupExportMatchesTheGolden(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, backupOptions(t))
	path := filepath.Join(t.TempDir(), "backup.json")

	got, err := c.Backup.Export(t.Context(), path, backup.Sections())
	require.NoError(t, err)
	require.Len(t, got.Sections, len(backup.Sections()))

	data, err := os.ReadFile(path) //nolint:gosec // a file the test wrote
	require.NoError(t, err)

	golden := filepath.Join("testdata", "backup.golden")
	if *update {
		require.NoError(t, os.WriteFile(golden, data, 0o600)) //nolint:gosec // the test's own golden file
	}

	want, err := os.ReadFile(golden) //nolint:gosec // the test's own golden file
	require.NoError(t, err)
	assert.Equal(t, string(want), string(data), "the backup format changed; rerun with -update if that was meant")
	assert.Contains(t, string(data), `"format": "`+brand.Slug+`.backup"`)
}

func TestBackupBringsTheSetupBack(t *testing.T) {
	t.Parallel()

	opts := backupOptions(t)
	c := bootstrap(t, opts)
	ctx := t.Context()

	_, err := c.Settings.SetTheme(ctx, settings.ThemeDark)
	require.NoError(t, err)
	_, err = c.Settings.SetRepoRule(ctx, boundOwner+"/"+boundRepo, reporules.RulePinned)
	require.NoError(t, err)
	_, err = c.Scripts.Save(ctx, scripts.UserScript{
		Name: "Fix all", Command: "magex format:fix", Icon: scripts.IconPlay,
		Scope: scripts.ScopeAll,
	})
	require.NoError(t, err)

	review := promptNamed(t, c, "Review")
	_, err = c.LaunchDefaults.Save(ctx, launchdefaults.Input{Owner: boundOwner, PRPromptID: &review.ID})
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "backup.json")
	_, err = c.Backup.Export(ctx, path, backup.Sections())
	require.NoError(t, err)

	// The user changes their mind about everything.
	_, err = c.Settings.SetTheme(ctx, settings.ThemeLight)
	require.NoError(t, err)
	_, err = c.Settings.SetRepoRule(ctx, boundOwner+"/"+boundRepo, reporules.RuleNone)
	require.NoError(t, err)

	all, err := scripts.NewBackupPart(c.Scripts).PortableScripts(ctx)
	require.NoError(t, err)
	require.Len(t, all.Scripts, 1)

	require.NoError(t, c.Prompts.Delete(ctx, promptNamed(t, c, "Quick look").ID))
	_, err = c.Scripts.Save(ctx, scripts.UserScript{
		Name: "Extra", Command: "echo extra", Icon: scripts.IconPlay,
		Scope: scripts.ScopeAll,
	})
	require.NoError(t, err)

	merged := importBackup(t, c, path, backup.ModeMerge, backup.Sections()...)
	assert.True(t, strings.HasPrefix(merged.SavedTo, "/") || strings.HasPrefix(merged.SavedTo, "~"), merged.SavedTo)

	got, err := c.Settings.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, settings.ThemeDark, got.Theme, "a setting comes back")
	assert.Equal(t, []string{boundOwner + "/" + boundRepo}, got.RepoRules.Pinned, "a pinned repository comes back")
	assert.NotZero(t, promptNamed(t, c, "Quick look").ID, "a deleted prompt comes back")
	assert.ElementsMatch(t, []string{"Fix all", "Extra"}, scriptNames(t, c), "Merge deletes nothing")

	defaults, err := c.LaunchDefaults.List(ctx)
	require.NoError(t, err)
	require.Len(t, defaults, 1)
	assert.Equal(t, &review.ID, defaults[0].PRPromptID, "a launch default names its prompt again")

	clock, ok := opts.Clock.(*testutil.Clock)
	require.True(t, ok)
	clock.Advance(time.Minute)

	replaced := importBackup(t, c, path, backup.ModeReplace, backup.SectionScripts, backup.SectionPrompts)
	assert.Equal(t, []string{"Fix all"}, scriptNames(t, c), "Replace leaves exactly the file's scripts")
	assert.NotEqual(t, merged.SavedTo, replaced.SavedTo)

	saved, err := os.ReadDir(opts.Paths.Backups)
	require.NoError(t, err)
	assert.Len(t, saved, 2, "each import saved the setup first")

	library, err := os.ReadFile(opts.Paths.Prompts)
	require.NoError(t, err)
	assert.Contains(t, string(library), "Quick look", "the copy of the library is rewritten after an import")
}

// promptNamed finds c's prompt with nickname, or the zero Prompt.
func promptNamed(t *testing.T, c *Core, nickname string) prompts.Prompt {
	t.Helper()

	all, err := c.Prompts.List(t.Context())
	require.NoError(t, err)

	for _, p := range all {
		if p.Nickname == nickname {
			return p
		}
	}

	return prompts.Prompt{}
}
