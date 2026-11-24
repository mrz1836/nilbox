package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/backup"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/scripts"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// fixChecksNick is the Fix checks starter's nickname, and fixCI the one the user gave it.
const (
	fixChecksNick = "Fix checks"
	fixCI         = "fix-ci"
)

// renamedTo saves the prompt p under nickname, as the editor saves it.
func renamedTo(t *testing.T, c *Core, p prompts.Prompt, nickname string) prompts.Prompt {
	t.Helper()

	got, err := c.Prompts.Update(t.Context(), p.ID, prompts.Input{
		Name: p.Name, Nickname: nickname, AppliesTo: p.AppliesTo, Body: p.Body, Models: p.Models, Effort: p.Effort,
		PlanMode: p.PlanMode, Worktree: p.Worktree, ExtraFlags: p.ExtraFlags,
	})
	require.NoError(t, err)

	return got
}

// assertRunsFixChecks checks that the routine id still runs the prompt fix, by its UUID,
// named as the library names it now, and that a run of it would launch that prompt.
func assertRunsFixChecks(t *testing.T, c *Core, id int64, fix prompts.Prompt) {
	t.Helper()

	got, err := c.Autopilot.Get(t.Context(), id)
	require.NoError(t, err)
	assert.Empty(t, got.Broken)

	ref := got.Spec.Steps[0].Agent.Prompt
	assert.Equal(t, spec.PromptRef{UUID: fix.UUID, Nickname: fixChecksNick}, ref,
		"the routine names the prompt by its UUID, and by its nickname now")
	assert.Contains(t, got.Sentence, `"`+fixChecksNick+`"`, "its sentence names it as it is called now")

	launched, err := promptIDOf(t.Context(), prompts.NewStore(c.DB, nil), ref)
	require.NoError(t, err)
	assert.Equal(t, fix.ID, launched, "a run launches that very prompt")
}

func TestAPromptResetKeepsTheRoutinesThatRunIt(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, testOptions(t, ".claude"))
	ctx := t.Context()

	// The user renamed their Fix checks, and mapped an imported routine's prompt to it.
	fix := renamedTo(t, c, promptNamed(t, c, fixChecksNick), fixCI)

	saved, err := c.Autopilot.Save(ctx, autopilot.Draft{Name: "Fix my checks", Spec: triageSpec(fixCI)})
	require.NoError(t, err)
	require.Equal(t, fix.UUID, saved.Spec.Steps[0].Agent.Prompt.UUID, "Save names it by its UUID")

	// Reset gives the prompt its starter's nickname back, without a word to the routines.
	reset, err := c.Prompts.Reset(ctx, fix.ID)
	require.NoError(t, err)
	require.Equal(t, fixChecksNick, reset.Nickname)

	assertRunsFixChecks(t, c, saved.ID, fix)

	usage := promptNamed(t, c, fixChecksNick).Usage
	assert.Equal(t, []string{"Fix my checks"}, usage.Routines, "the library still says the routine runs it")
}

func TestAnImportOverAStarterKeepsTheRoutinesThatRunIt(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, testOptions(t, ".claude"))
	ctx := t.Context()

	fix := renamedTo(t, c, promptNamed(t, c, fixChecksNick), fixCI)
	require.False(t, fix.Modified, "a starter's copy renamed and nothing more is still as the app added it")

	saved, err := c.Autopilot.Save(ctx, autopilot.Draft{Name: "Fix my checks", Spec: triageSpec(fixCI)})
	require.NoError(t, err)

	// A prompt file brings the user's own version of the starter, which the import saves
	// over the untouched copy, under the nickname the file gives it.
	var starter prompts.Starter

	for _, st := range prompts.Starters() {
		if st.Key == prompts.StarterFixChecks {
			starter = st
		}
	}

	require.NotEmpty(t, starter.Key)

	res, err := c.Prompts.Import(ctx, []prompts.BundlePrompt{{
		Name: starter.Name, Nickname: fixChecksNick, AppliesTo: starter.AppliesTo,
		Body: starter.Body + "\nAnd say what you changed.\n", Models: map[agent.ID]string{},
		ExtraArgs: map[agent.ID][]string{}, StarterKey: starter.Key,
	}}, prompts.BundleDefaults{})
	require.NoError(t, err)
	require.Equal(t, 1, res.Replaced, "saved over the starter's copy")

	assertRunsFixChecks(t, c, saved.ID, fix)
}

func TestAResetRestoreKeepsAScriptStepOnItsScriptUnderItsNewID(t *testing.T) {
	t.Parallel()

	opts := testOptions(t, ".claude")
	ctx := t.Context()

	c, err := Bootstrap(ctx, opts)
	require.NoError(t, err)

	first, err := c.Scripts.Save(ctx, scripts.UserScript{
		Name: "First", Command: "make first", Icon: scripts.IconPlay,
		Scope: scripts.ScopeAll,
	})
	require.NoError(t, err)

	lint, err := c.Scripts.Save(ctx, scripts.UserScript{
		Name: "Lint", Command: "magex lint", Icon: scripts.IconLint,
		Scope: scripts.ScopeAll,
	})
	require.NoError(t, err)
	require.NoError(t, c.Scripts.Delete(ctx, first.ID))

	routine := boundSpec()
	routine.Trigger = spec.Trigger{Event: catalog.EventAppears}
	routine.Steps = append(routine.Steps, spec.Step{Script: &spec.ScriptStep{ID: lint.ID, Name: "Lint"}})

	saved, err := c.Autopilot.Save(ctx, autopilot.Draft{Name: "Lint new PRs", Spec: routine})
	require.NoError(t, err)
	require.Equal(t, lint.UUID, saved.Spec.Steps[1].Script.UUID)
	require.NoError(t, c.Close())

	// The user starts over: the reset brings the scripts back, in their order, under new IDs.
	require.NoError(t, appdata.RequestReset(opts.Paths.ResetRequest, appdata.ResetBySettings, askedAt()))

	again := bootstrap(t, opts)

	mine, err := scripts.NewRoutines(again.Scripts).Mine(ctx)
	require.NoError(t, err)
	require.Len(t, mine, 1)
	require.Equal(t, lint.UUID, mine[0].UUID, "the script came back under its UUID")
	require.NotEqual(t, lint.ID, mine[0].ID, "and under another ID")

	routines, err := again.Autopilot.List(ctx)
	require.NoError(t, err)
	require.Len(t, routines, 1)
	assert.Equal(t, spec.ScriptStep{UUID: lint.UUID, ID: mine[0].ID, Name: "Lint"}, *routines[0].Spec.Steps[1].Script,
		"the routine finds its script by UUID, and names it by its new ID")

	sc, err := scriptSteps{mine: myScripts{routines: scripts.NewRoutines(again.Scripts)}}.script(ctx,
		*routines[0].Spec.Steps[1].Script)
	require.NoError(t, err)
	assert.Equal(t, mine[0].ID, sc.ID, "the script it runs is that one")
}

func TestAStartStampsARoutineStoredBeforeUUIDs(t *testing.T) {
	t.Parallel()

	opts := testOptions(t, ".claude")
	ctx := t.Context()

	c, err := Bootstrap(ctx, opts)
	require.NoError(t, err)

	review := promptNamed(t, c, "Review")

	saved, err := c.Autopilot.Save(ctx, autopilot.Draft{Name: "Review it", Spec: triageSpec("Review")})
	require.NoError(t, err)

	// As a build from before UUIDs stored it: version 1, named by its nickname alone, and
	// with no UUID of its own.
	data, err := spec.Encode(spec.Normalize(triageSpec("Review")))
	require.NoError(t, err)

	old := strings.Replace(string(data), `"version":2`, `"version":1`, 1)
	_, err = c.DB.ExecContext(ctx, `UPDATE autopilot_routines SET spec = ?, spec_version = 1, uuid = '' WHERE id = ?`,
		old, saved.ID)
	require.NoError(t, err)
	require.NoError(t, c.Close())

	again := bootstrap(t, opts)

	got, err := again.Autopilot.Get(ctx, saved.ID)
	require.NoError(t, err)
	assert.Equal(t, spec.PromptRef{UUID: review.UUID, Nickname: "Review"}, got.Spec.Steps[0].Agent.Prompt)
	assert.Equal(t, spec.Version, got.Spec.Version)
	assert.Regexp(t, `^[0-9a-f-]{36}$`, got.UUID, "the routine has its own UUID")

	var version int

	require.NoError(t, again.DB.QueryRowContext(ctx, `SELECT spec_version FROM autopilot_routines WHERE id = ?`,
		saved.ID).Scan(&version))
	assert.Equal(t, spec.Version, version, "stored in this build's version of the format")
}

func TestDescribeItReadsARoutineWithoutTheUUIDsTheAppKeeps(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, testOptions(t, ".claude"))

	saved, err := c.Autopilot.Save(t.Context(), autopilot.Draft{Name: "Review it", Spec: triageSpec("Review")})
	require.NoError(t, err)
	require.NotEmpty(t, saved.Spec.Steps[0].Agent.Prompt.UUID)

	got := kitRoutine(saved)
	assert.Equal(t, spec.PromptRef{Nickname: "Review"}, got.Spec.Steps[0].Agent.Prompt,
		"the snapshot and an attachment name the prompt by its nickname alone")
	assert.NotEmpty(t, saved.Spec.Steps[0].Agent.Prompt.UUID, "the routine it read keeps its UUID")

	words, err := describeAttachments{routines: c.Autopilot}.Routine(t.Context(), saved.ID)
	require.NoError(t, err)
	assert.NotContains(t, words.Words, saved.Spec.Steps[0].Agent.Prompt.UUID)
	assert.NotContains(t, words.Words, `"uuid"`)
}

func TestALaunchDefaultsCopyFindsItsPromptByUUIDWhateverItsNicknameSays(t *testing.T) {
	t.Parallel()

	opts := testOptions(t, ".claude")
	ctx := t.Context()

	c, err := Bootstrap(ctx, opts)
	require.NoError(t, err)

	quick := promptNamed(t, c, "Quick look")
	_, err = c.LaunchDefaults.Save(ctx, launchdefaults.Input{Owner: boundOwner, PRPromptID: &quick.ID})
	require.NoError(t, err)
	require.NoError(t, c.Close())

	// The copy names the prompt by a nickname the library gives another prompt by the time it
	// comes back, as an edit by hand or a clash on the way back may leave it.
	data, err := os.ReadFile(opts.Paths.LaunchDefaults)
	require.NoError(t, err)
	require.Contains(t, string(data), quick.UUID, "the copy names the prompt by its UUID too")

	edited := strings.Replace(string(data), `"prPrompt": "Quick look"`, `"prPrompt": "Review"`, 1)
	require.NotEqual(t, string(data), edited)
	testutil.WriteFile(t, opts.Paths.LaunchDefaults, edited)

	// The database is lost: the prompts come back from their copy, then the launch defaults.
	for _, suffix := range []string{"", "-wal", "-shm"} {
		require.NoError(t, os.RemoveAll(opts.Paths.DB+suffix))
	}

	again := bootstrap(t, opts)

	org := launchDefaultsByTarget(t, again)[boundOwner]
	require.NotNil(t, org.PRPromptID)
	assert.Equal(t, promptNamed(t, again, "Quick look").ID, *org.PRPromptID,
		"the UUID finds the prompt, never the one with the nickname the copy says")
}

func TestABackupsLaunchDefaultFollowsAPromptRenamedSince(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, backupOptions(t))
	ctx := t.Context()

	quick := promptNamed(t, c, "Quick look")
	_, err := c.LaunchDefaults.Save(ctx, launchdefaults.Input{Owner: boundOwner, PRPromptID: &quick.ID})
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "backup.json")
	_, err = c.Backup.Export(ctx, path, backup.Sections())
	require.NoError(t, err)

	// The user renamed the prompt since, and cleared the row.
	renamedTo(t, c, quick, "look-first")
	require.NoError(t, c.LaunchDefaults.Remove(ctx, boundOwner, ""))

	importBackup(t, c, path, backup.ModeMerge, backup.SectionLaunchDefaults)

	org := launchDefaultsByTarget(t, c)[boundOwner]
	require.NotNil(t, org.PRPromptID, "found by its UUID, it doesn't inherit")
	assert.Equal(t, quick.ID, *org.PRPromptID)
}
