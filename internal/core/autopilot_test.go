package core

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilot/watch"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/backup"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/item"
	prefsstore "github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// triageSpec is a routine whose agent step runs the user's own prompt, by its nickname.
func triageSpec(nickname string) spec.Spec {
	s := boundSpec()
	s.Steps = []spec.Step{{Agent: &spec.AgentStep{Prompt: spec.PromptRef{Nickname: nickname}, Where: catalog.WhereTerminal}}}

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

	_, err = c.Autopilot.SetEnabled(ctx, first.ID, true, autopilot.CurrentRun)
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

	_, err = c.Autopilot.SetEnabled(ctx, r.ID, true, autopilot.CurrentRun)
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

func TestTheInboxReadsTheRoutinesSearchesForTheAccountSignedIn(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, testOptions(t, ".claude"))
	ctx := t.Context()

	_, err := c.Settings.SetAutopilotEnabled(ctx, true)
	require.NoError(t, err)

	saved, err := c.Autopilot.Save(ctx, autopilot.Draft{Name: "Dependabot", Spec: myDependabotSpec()})
	require.NoError(t, err)

	_, err = c.Autopilot.SetEnabled(ctx, saved.ID, true, autopilot.CurrentRun)
	require.NoError(t, err)

	source := &autopilotWatches{searches: autopilot.NewSearches(c.Autopilot)}
	assert.Empty(t, source.Watches(ctx, ""), "no one is signed in, so your account names no one")

	require.NoError(t, prefsstore.New(c.DB, nil).Set(ctx, "inbox.viewer", "octo-user"))

	got := source.Watches(ctx, "octo-user")
	require.Len(t, got, 1, "the account signed in changed, so the searches are built again")
	assert.Contains(t, got[0].Query, "user:octo-user")
	assert.True(t, got[0].Follow, "Checks fail reads the checks")
	assert.NotNil(t, c.WatchPasses, "the desktop shell runs the watch passes")
}

func TestTheRoutinesSearchesAreBuiltAgainUnderTheRefreshsContext(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, testOptions(t, ".claude"))
	ctx := t.Context()

	_, err := c.Settings.SetAutopilotEnabled(ctx, true)
	require.NoError(t, err)

	saved, err := c.Autopilot.Save(ctx, autopilot.Draft{Name: "Dependabot", Spec: myDependabotSpec()})
	require.NoError(t, err)

	_, err = c.Autopilot.SetEnabled(ctx, saved.ID, true, autopilot.CurrentRun)
	require.NoError(t, err)

	// The snapshot was built while no one was signed in; now octo-user is.
	require.NoError(t, prefsstore.New(c.DB, nil).Set(ctx, "inbox.viewer", "octo-user"))

	searches := autopilot.NewSearches(c.Autopilot)
	source := &autopilotWatches{searches: searches}

	ended, cancel := context.WithCancel(ctx)
	cancel()

	assert.Empty(t, source.Watches(ended, "octo-user"), "a refresh whose ctx has ended builds nothing")
	assert.Empty(t, searches.Current().Login, "so the snapshot stays the one built for no one")

	got := source.Watches(ctx, "octo-user")
	require.Len(t, got, 1, "the refresh's live ctx builds the searches again")
	assert.Equal(t, "octo-user", searches.Current().Login)
}

// myDependabotSpec is a routine that tells the user when a pull request Dependabot opened
// on one of their repositories fails its checks: a search of the account signed in.
func myDependabotSpec() spec.Spec {
	search := watch.Search{Kind: item.KindPR, Authors: []string{"app/dependabot"}, Where: []string{watch.Me}}
	routine := boundSpec()
	routine.Watch.Sources = []spec.Source{{Search: &search}}

	return routine
}

// lockFree reports whether another copy of the app could take Autopilot's lock at path.
func lockFree(t *testing.T, path string) bool {
	t.Helper()

	other := autopilot.NewLock(path)

	held, err := other.TryLock()
	require.NoError(t, err)
	other.Unlock()

	return held
}

func TestAutopilotIsOffAndInertOnAFreshInstall(t *testing.T) {
	t.Parallel()

	opts := testOptions(t, ".claude")
	c := bootstrap(t, opts)
	ctx := t.Context()

	set, err := c.Settings.Get(ctx)
	require.NoError(t, err)
	assert.False(t, set.Autopilot.Enabled, "Autopilot (beta) starts off")

	// A routine switched on, for the account signed in, is stored and does nothing.
	require.NoError(t, prefsstore.New(c.DB, nil).Set(ctx, "inbox.viewer", "octo-user"))

	saved, err := c.Autopilot.Save(ctx, autopilot.Draft{Name: "Dependabot", Spec: myDependabotSpec()})
	require.NoError(t, err)

	_, err = c.Autopilot.SetEnabled(ctx, saved.ID, true, autopilot.CurrentRun)
	require.NoError(t, err)

	source := &autopilotWatches{searches: autopilot.NewSearches(c.Autopilot)}
	assert.False(t, c.AutopilotEngine.Enabled(), "the engine doesn't run")
	assert.True(t, lockFree(t, opts.Paths.AutopilotLock), "and holds no lock")
	assert.False(t, source.HoldsWatches(), "so the inbox sends no routine search")
	assert.Empty(t, source.Watches(ctx, "octo-user"), "and is handed none")

	// The menu bar shows no Autopilot group, and the window's close quits as before.
	shell, err := c.ShellAutopilot.State(ctx)
	require.NoError(t, err)
	assert.Zero(t, shell.Menu.Routines, "no Autopilot group")
	assert.False(t, shell.KeepsRunning, "a routine on keeps nothing running")

	// Turned on, the routine shows and keeps the app running, and its search is built.
	_, err = c.Settings.SetAutopilotEnabled(ctx, true)
	require.NoError(t, err)

	shell, err = c.ShellAutopilot.State(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, shell.Menu.Routines)
	assert.True(t, shell.KeepsRunning)
	assert.Len(t, source.searches.Rebuild(ctx).Searches, 1)

	// Off again: the routine is as it was, and hidden.
	_, err = c.Settings.SetAutopilotEnabled(ctx, false)
	require.NoError(t, err)

	shell, err = c.ShellAutopilot.State(ctx)
	require.NoError(t, err)
	assert.Zero(t, shell.Menu.Routines)
	assert.False(t, shell.KeepsRunning)
	assert.Empty(t, source.searches.Rebuild(ctx).Searches)

	routines, err := c.Autopilot.List(ctx)
	require.NoError(t, err)
	require.Len(t, routines, 1)
	assert.True(t, routines[0].Enabled, "the routine's own switch is left as it was")
}

func TestARoutineOfTriageStepsTakesAnyMode(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, testOptions(t, ".claude"))

	routine := boundSpec()
	routine.Steps = []spec.Step{{Pin: &spec.Empty{}}, {Snooze: &spec.SnoozeStep{}}, {Dismiss: &spec.Empty{}}, {Restore: &spec.Empty{}}}

	saved, err := c.Autopilot.Save(t.Context(), autopilot.Draft{Name: "Tidy", Spec: routine, Mode: autopilotprefs.ModeAuto})
	require.NoError(t, err)
	assert.Equal(t, autopilotprefs.ModeAuto, saved.Mode, "the inbox's triage runs them")
	assert.Empty(t, saved.DryRunOnly)
}

func TestTheEngineHearsWhichRoutinesTriageTheUserTookBack(t *testing.T) {
	t.Parallel()

	ref := item.Ref{Repo: repo.Name{Owner: boundOwner, Repo: boundRepo}, Number: 7, Kind: item.KindPR}
	at := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)

	got := toEngineOutcomes([]inbox.Outcome{
		{Kind: inbox.OutcomeUnpinned, Ref: ref, At: at, Reversed: inbox.Reversal{Routine: 3, Item: 9, Did: inbox.TriagePin}},
		{Kind: inbox.OutcomeMerged, Ref: ref, At: at},
	})

	assert.Equal(t, []autopilot.Outcome{
		{Kind: "unpinned", Ref: ref, At: at, Reversed: autopilot.Reversal{RoutineID: 3, ItemID: 9, Action: catalog.StepPin}},
		{Kind: "merged", Ref: ref, At: at},
	}, got)

	for _, kind := range []inbox.OutcomeKind{inbox.OutcomeSnoozed, inbox.OutcomeUnpinned} {
		_, mirrored := outcomeKinds()[kind]
		assert.False(t, mirrored, "%s finishes nothing, so the GitHub notifications' mirror never hears it", kind)
	}
}

// toldInOrder records which of the fan-out's sinks heard outcomes, in order.
type toldInOrder struct {
	mu    sync.Mutex
	heard []string
}

func (o *toldInOrder) add(who string) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.heard = append(o.heard, who)
}

// mirrorSink is the mirror of the GitHub notifications, as the fan-out tells it.
type mirrorSink struct{ order *toldInOrder }

func (m mirrorSink) Settle(context.Context, []inbox.Outcome) { m.order.add("mirror") }

// engineSink is Autopilot's engine, as the fan-out tells it.
type engineSink struct{ order *toldInOrder }

func (e engineSink) Outcomes(context.Context, []autopilot.Outcome) { e.order.add("engine") }

// sessionsSink is the agent sessions, as the fan-out tells them.
type sessionsSink struct{ order *toldInOrder }

func (s sessionsSink) Settle(context.Context, []inbox.Outcome) { s.order.add("sessions") }

func TestTheEngineHearsWhatTheUserTookBackBeforeTheMirror(t *testing.T) {
	t.Parallel()

	order := &toldInOrder{}
	fan := outcomesFanOut{
		threads: mirrorSink{order: order}, engine: engineSink{order: order}, sessions: sessionsSink{order: order},
	}

	fan.Settle(t.Context(), []inbox.Outcome{{Kind: inbox.OutcomeRestored}})
	assert.Equal(t, []string{"engine", "mirror", "sessions"}, order.heard,
		"the engine records hands off at once, never behind a clear the mirror sends to GitHub")

	(&engineWaker{}).Outcomes(t.Context(), []autopilot.Outcome{{Kind: "restored"}})
	outcomesFanOut{}.Settle(t.Context(), []inbox.Outcome{{Kind: inbox.OutcomeRestored}})
}

func TestTheTriageStepsAreTheInboxsOwnWords(t *testing.T) {
	t.Parallel()

	for _, k := range []catalog.StepKind{catalog.StepPin, catalog.StepSnooze, catalog.StepDismiss, catalog.StepRestore} {
		assert.True(t, inbox.TriageAction(k).Valid(), "%s is one of the inbox's triage steps", k)
	}

	assert.False(t, inbox.TriageAction(catalog.StepMarkDone).Valid(), "marking a notification done isn't triage")

	for _, k := range []inbox.TriageKept{inbox.KeptPinned, inbox.KeptDismissed, inbox.KeptYours, inbox.KeptNotSetAside} {
		assert.Equal(t, string(k), string(triageChangeOf(inbox.TriageChange{Kept: k}).Kept))
	}

	assert.Equal(t, []autopilot.TriageKept{
		autopilot.KeptPinned, autopilot.KeptDismissed, autopilot.KeptYours, autopilot.KeptNotSetAside,
	}, []autopilot.TriageKept{
		autopilot.TriageKept(inbox.KeptPinned), autopilot.TriageKept(inbox.KeptDismissed),
		autopilot.TriageKept(inbox.KeptYours), autopilot.TriageKept(inbox.KeptNotSetAside),
	}, "the same words on both sides")
}
