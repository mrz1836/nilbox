package core

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/backup"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// triageSpec is a routine whose agent step runs the user's own prompt, by its nickname.
func triageSpec(nickname string) spec.Spec {
	s := boundSpec()
	s.Steps = []spec.Step{{Agent: &spec.AgentStep{Prompt: spec.PromptRef{Nickname: nickname}}}}

	return s
}

// routinesCore starts a Core over opts whose user made a prompt of their own and two
// routines, the first of which runs that prompt and is on, and closes it. It returns the
// prompt's nickname.
func routinesCore(t *testing.T, opts Options) string {
	t.Helper()

	c := bootstrap(t, opts)
	ctx := t.Context()

	mine, err := c.Prompts.Create(ctx, prompts.Input{
		Name: "Triage the checks", Nickname: "Triage", AppliesTo: prompts.AppliesToAny, Body: "Triage {{.URL}}",
	})
	require.NoError(t, err)

	first, err := c.Autopilot.Save(ctx, autopilot.Draft{Name: "Triage failing checks", Spec: triageSpec(mine.Nickname)})
	require.NoError(t, err)

	_, err = c.Autopilot.SetEnabled(ctx, first.ID, true)
	require.NoError(t, err)

	_, err = c.Autopilot.Save(ctx, autopilot.Draft{Name: "Tell me", Spec: boundSpec()})
	require.NoError(t, err)

	require.NoError(t, c.Close())

	return mine.Nickname
}

// assertRoutinesBack checks that c holds the two routines routinesCore made, off, the
// first naming the restored prompt nickname.
func assertRoutinesBack(t *testing.T, c *Core, nickname string) {
	t.Helper()

	routines, err := c.Autopilot.List(t.Context())
	require.NoError(t, err)
	require.Len(t, routines, 2)
	assert.Equal(t, "Triage failing checks", routines[0].Name)
	assert.Equal(t, "Tell me", routines[1].Name)

	for _, r := range routines {
		assert.False(t, r.Enabled, "%s arrives off", r.Name)
		assert.Empty(t, r.BrokenReason)
	}

	named := routines[0].Spec.Steps[0].Agent.Prompt.Nickname
	assert.Equal(t, nickname, named)
	assert.Contains(t, libraryByNickname(t, c), named, "the prompt it names came back first")
}

func TestBootstrapBringsTheRoutinesBackFromTheDatabaseSetAside(t *testing.T) {
	t.Parallel()

	opts := testOptions(t, ".claude")
	nickname := routinesCore(t, opts)

	require.NoError(t, appdata.RequestReset(opts.Paths.ResetRequest, appdata.ResetBySettings, askedAt()))

	c := bootstrap(t, opts)
	info := c.AppInfo.Info()

	require.NotNil(t, info.Restored)
	assert.Equal(t, 2, info.Restored.Routines, "the window is told the routines came back")
	assertRoutinesBack(t, c, nickname)
}

func TestBootstrapBringsTheRoutinesBackFromTheCopyAlone(t *testing.T) {
	t.Parallel()

	opts := testOptions(t, ".claude")
	nickname := routinesCore(t, opts)
	require.FileExists(t, opts.Paths.Autopilot, "every change writes the copy beside the database")

	// The database is lost: deleted, or never copied to a new Mac.
	for _, suffix := range []string{"", "-wal", "-shm"} {
		require.NoError(t, os.RemoveAll(opts.Paths.DB+suffix))
	}

	c := bootstrap(t, opts)
	info := c.AppInfo.Info()

	require.NotNil(t, info.Restored)
	assert.Equal(t, 2, info.Restored.Routines)
	assertRoutinesBack(t, c, nickname)

	again, err := autopilot.NewFile(opts.Paths.Autopilot, "", nil).Load()
	require.NoError(t, err)
	assert.Len(t, again.Routines, 2, "the copy is written again from what came back")
	assert.False(t, again.Routines[0].Enabled)
}

func TestBootstrapLeavesTheRoutinesCopyAsTheLastChangeWroteIt(t *testing.T) {
	t.Parallel()

	opts := testOptions(t, ".claude")
	routinesCore(t, opts)

	written, err := os.ReadFile(opts.Paths.Autopilot)
	require.NoError(t, err)

	// A routine disappears from the database behind the app's back, as a faulty update
	// might lose it. The next start must not copy the loss over the file.
	db := testutil.DBAt(t, opts.Paths.DB)
	testutil.Exec(t, db, `DELETE FROM autopilot_routines WHERE name = 'Tell me'`)
	require.NoError(t, db.Close())

	c := bootstrap(t, opts)
	assert.Nil(t, c.AppInfo.Info().Restored, "a table that isn't new brings nothing back")

	after, err := os.ReadFile(opts.Paths.Autopilot)
	require.NoError(t, err)
	assert.Equal(t, string(written), string(after), "the copy still holds both routines")
}

func TestTheFirstStartWritesAnEmptyCopyOfTheRoutines(t *testing.T) {
	t.Parallel()

	opts := testOptions(t, ".claude")
	bootstrap(t, opts)

	b, err := autopilot.NewFile(opts.Paths.Autopilot, "", nil).Load()
	require.NoError(t, err)
	assert.Empty(t, b.Routines)

	info, err := os.Stat(opts.Paths.Autopilot)
	require.NoError(t, err)
	assert.Equal(t, appdata.PrivateFilePerm, info.Mode().Perm(), "readable by the user alone")
}

func TestABackupBringsTheRoutinesBackOff(t *testing.T) {
	t.Parallel()

	opts := testOptions(t, ".claude")
	c := bootstrap(t, opts)
	ctx := t.Context()

	r, err := c.Autopilot.Save(ctx, autopilot.Draft{Name: "Tell me", Spec: boundSpec()})
	require.NoError(t, err)

	_, err = c.Autopilot.SetEnabled(ctx, r.ID, true)
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "backup.json")
	exported, err := c.Backup.Export(ctx, path, []backup.Section{backup.SectionAutopilot})
	require.NoError(t, err)
	assert.Equal(t, []backup.SectionCount{{Section: backup.SectionAutopilot, Items: 1}}, exported.Sections)

	// Another Mac, which has a routine of its own.
	thereOpts := testOptions(t, ".claude")
	there := bootstrap(t, thereOpts)
	_, err = there.Autopilot.Save(ctx, autopilot.Draft{Name: "Only there", Spec: boundSpec()})
	require.NoError(t, err)

	preview, err := there.Backup.Preview(ctx, path)
	require.NoError(t, err)

	var section backup.SectionPreview

	for _, s := range preview.Sections {
		if s.Section == backup.SectionAutopilot {
			section = s
		}
	}

	require.True(t, section.Present)
	assert.Equal(t, backup.Count{Add: 1}, section.Merge.Count)
	assert.Equal(t, backup.Count{Add: 1, Remove: 1}, section.Replace.Count)

	_, err = there.Backup.Import(ctx, backup.Request{
		Path: path, Hash: preview.Hash, Sections: []backup.Section{backup.SectionAutopilot}, Mode: backup.ModeReplace,
	})
	require.NoError(t, err)

	got, err := there.Autopilot.List(ctx)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "Tell me", got[0].Name)
	assert.False(t, got[0].Enabled, "an imported routine arrives off")

	copied, err := autopilot.NewFile(thereOpts.Paths.Autopilot, "", nil).Load()
	require.NoError(t, err)
	require.Len(t, copied.Routines, 1, "the import writes the copy again")
	assert.Equal(t, "Tell me", copied.Routines[0].Name)
}
