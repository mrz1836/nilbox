package autopilot_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/uierr"
)

func TestANewRoutineStartsOffInTheDefaultModeAndBelongsToTheAccountSignedIn(t *testing.T) {
	t.Parallel()

	r := newRig(t, autopilotprefs.ModeDryRun)

	got := r.save(t, autopilot.Draft{
		Name: "  Tell me about failing checks ", Description: "Mine only.", Spec: notifySpec(),
		TemplateKey: "tell-me-about-failing-checks",
	})

	assert.Positive(t, got.ID)
	assert.Equal(t, "Tell me about failing checks", got.Name, "the name is trimmed")
	assert.Equal(t, "Mine only.", got.Description)
	assert.False(t, got.Enabled, "a new routine starts off")
	assert.Nil(t, got.EnabledAt)
	assert.Equal(t, viewer, got.Account)
	assert.False(t, got.OtherAccount)
	assert.Equal(t, "tell-me-about-failing-checks", got.TemplateKey)
	assert.Empty(t, got.BrokenReason)
	assert.True(t, startedAt().Equal(got.CreatedAt), "stamped from the clock")
	assert.Equal(t, spec.Normalize(notifySpec()), got.Spec)
	assert.NotEmpty(t, got.Sentence)
	assert.Empty(t, got.DryRunOnly, "Notify me runs in this build")
	assert.Equal(t, autopilotprefs.ModeDryRun, got.Mode, "the mode new routines start in")

	assert.Equal(t, []autopilot.Routine{got}, r.list(t))
}

func TestARoutineWithAStepThisBuildCantRunYetIsSavedInDryRun(t *testing.T) {
	t.Parallel()

	r := newRig(t, autopilotprefs.ModeAsk)

	// Every kind of step is listed before it runs, so a routine with one can only
	// dry-run; once the step runs, the mode stands as asked (TestTheModeANewRoutineStartsIn).
	got := r.save(t, draft("Fix failing checks", agentSpec()))
	assert.Equal(t, autopilotprefs.ModeDryRun, got.Mode, "a step this build can't run keeps it in Dry run")
	assert.Equal(t, []catalog.StepKind{catalog.StepAgent}, got.DryRunOnly)

	d := draft("Fix them all", agentSpec())
	d.Mode = autopilotprefs.ModeAuto
	assert.Equal(t, autopilotprefs.ModeDryRun, r.save(t, d).Mode, "whatever the draft asks")
}

func TestSaveRefusesWhatItCantStoreAndStoresNothing(t *testing.T) {
	t.Parallel()

	r := newRig(t, autopilotprefs.ModeAsk)
	r.save(t, draft("Taken", notifySpec()))

	invalid := notifySpec()
	invalid.Watch.Sources = nil
	invalid.Steps = append(invalid.Steps, spec.Step{Pin: &spec.Empty{}, Dismiss: &spec.Empty{}})

	for _, c := range []struct {
		d    autopilot.Draft
		err  error
		want string
	}{
		{draft("", notifySpec()), autopilot.ErrInvalidName, "Give the routine a name."},
		{draft("two\nlines", notifySpec()), autopilot.ErrInvalidName, "Keep the name to one line of plain text."},
		{draft(string(make([]rune, 81)), notifySpec()), autopilot.ErrInvalidName, ""},
		{
			draft("TAKEN", notifySpec()), autopilot.ErrNameTaken,
			"Another routine is named “TAKEN”. Give this one a name of its own.",
		},
		{
			autopilot.Draft{Name: "Template", Spec: notifySpec(), TemplateKey: "Not a key"},
			autopilot.ErrInvalidTemplate,
			"That isn't one of the templates.",
		},
		{
			autopilot.Draft{Name: "Mode", Spec: notifySpec(), Mode: "always"},
			autopilotprefs.ErrUnknownMode,
			"That isn't one of the modes: choose Dry run, Ask first, or Autopilot.",
		},
		{draft("Invalid", invalid), spec.ErrInvalid, ""},
		{autopilot.Draft{ID: 999, Name: "Gone", Spec: notifySpec()}, autopilot.ErrNotFound, "The routine was deleted."},
	} {
		_, err := r.svc.Save(t.Context(), c.d)
		require.ErrorIs(t, err, c.err, c.d.Name)

		if c.want != "" {
			assert.Equal(t, c.want, uierr.Message(err), c.d.Name)
		}
	}

	assert.Len(t, r.list(t), 1, "a refused save stores nothing")
}

func TestSaveNamesEveryProblemForTheWindow(t *testing.T) {
	t.Parallel()

	r := newRig(t, autopilotprefs.ModeAsk)

	invalid := notifySpec()
	invalid.Watch.Sources = nil
	invalid.Steps = []spec.Step{{Pin: &spec.Empty{}, Dismiss: &spec.Empty{}}}

	_, err := r.svc.Save(t.Context(), draft("Invalid", invalid))
	require.Error(t, err)

	problems, ok := errors.AsType[*autopilot.ProblemsError](err)
	require.True(t, ok, "the error carries the problems")

	detail, err2 := json.Marshal(problems.Detail())
	require.NoError(t, err2)
	assert.Contains(t, string(detail), `{"problems":[{"part":"watch"`, "the window gets every problem where it is")
	assert.GreaterOrEqual(t, strings.Count(string(detail), `"part"`), 2)
	assert.Contains(t, uierr.Message(err), "The routine isn't valid yet:")
}

func TestSaveOverARoutineKeepsItsSwitchPlaceAndTemplate(t *testing.T) {
	t.Parallel()

	r := newRig(t, autopilotprefs.ModeAsk)
	first := r.save(t, autopilot.Draft{Name: firstName, Spec: notifySpec(), TemplateKey: "first"})
	r.save(t, draft("Second", notifySpec()))

	on, err := r.svc.SetEnabled(t.Context(), first.ID, true, autopilot.CurrentRun)
	require.NoError(t, err)

	changed := notifySpec()
	changed.Steps = []spec.Step{{Notify: &spec.NotifyStep{Note: "Look now"}}}

	got := r.save(t, autopilot.Draft{ID: first.ID, Name: "First, renamed", Spec: changed, TemplateKey: "other"})
	assert.Equal(t, "First, renamed", got.Name)
	assert.True(t, got.Enabled, "saving keeps the switch")
	assert.Equal(t, on.EnabledAt, got.EnabledAt)
	assert.Equal(t, "first", got.TemplateKey, "and the template it was made from")
	assert.Equal(t, "Look now", got.Spec.Steps[0].Notify.Note)

	list := r.list(t)
	names := make([]string, 0, len(list))

	for _, rt := range list {
		names = append(names, rt.Name)
	}

	assert.Equal(t, []string{"First, renamed", "Second"}, names, "and its place")
}

func TestDeleteIsSoftAndFreesTheName(t *testing.T) {
	t.Parallel()

	r := newRig(t, autopilotprefs.ModeAsk)
	gone := r.save(t, draft("Old", notifySpec()))
	kept := r.save(t, draft("Kept", notifySpec()))

	_, err := r.svc.SetEnabled(t.Context(), gone.ID, true, autopilot.CurrentRun)
	require.NoError(t, err)
	require.NoError(t, r.svc.Delete(t.Context(), gone.ID))

	assert.Equal(t, []int64{kept.ID}, ids(r.list(t)), "a deleted routine leaves the list")

	_, err = r.svc.Get(t.Context(), gone.ID)
	require.ErrorIs(t, err, autopilot.ErrNotFound)
	require.ErrorIs(t, r.svc.Delete(t.Context(), gone.ID), autopilot.ErrNotFound, "a routine is deleted once")

	var (
		name    string
		enabled bool
		deleted bool
	)

	require.NoError(t, r.db.QueryRowContext(t.Context(),
		`SELECT name, enabled, deleted_at IS NOT NULL FROM autopilot_routines WHERE id = ?`, gone.ID).
		Scan(&name, &enabled, &deleted))
	assert.Equal(t, "Old", name, "its record stays, for the history that names it")
	assert.False(t, enabled, "switched off")
	assert.True(t, deleted)

	again := r.save(t, draft("old", notifySpec()))
	assert.NotEqual(t, gone.ID, again.ID, "a deleted routine's name is free again")
}

func TestDuplicateMakesAnOffCopyRightAfterIt(t *testing.T) {
	t.Parallel()

	r := newRig(t, autopilotprefs.ModeAsk)
	first := r.save(t, autopilot.Draft{Name: firstName, Description: "The first.", Spec: notifySpec(), TemplateKey: "first"})
	last := r.save(t, draft("Last", notifySpec()))

	_, err := r.svc.SetEnabled(t.Context(), first.ID, true, autopilot.CurrentRun)
	require.NoError(t, err)

	r.accounts.signIn("other-user")

	copied, err := r.svc.Duplicate(t.Context(), first.ID)
	require.NoError(t, err)
	assert.Equal(t, "First copy", copied.Name)
	assert.False(t, copied.Enabled, "a copy starts off")
	assert.Equal(t, "The first.", copied.Description)
	assert.Equal(t, first.Spec, copied.Spec)
	assert.Equal(t, "first", copied.TemplateKey)
	assert.Equal(t, "other-user", copied.Account, "the copy is the account signed in's")

	again, err := r.svc.Duplicate(t.Context(), first.ID)
	require.NoError(t, err)
	assert.Equal(t, "First copy 2", again.Name, "a copy's name is its own")

	assert.Equal(t, []int64{first.ID, again.ID, copied.ID, last.ID}, ids(r.list(t)), "each copy right after it")

	_, err = r.svc.Duplicate(t.Context(), 999)
	require.ErrorIs(t, err, autopilot.ErrNotFound)
}

func TestSetEnabledAndSetMode(t *testing.T) {
	t.Parallel()

	r := newRig(t, autopilotprefs.ModeAsk)
	rt := r.save(t, draft("Fix", agentSpec()))

	on, err := r.svc.SetEnabled(t.Context(), rt.ID, true, autopilot.CurrentRun)
	require.NoError(t, err)
	assert.True(t, on.Enabled)
	require.NotNil(t, on.EnabledAt)

	still, err := r.svc.SetEnabled(t.Context(), rt.ID, true, autopilot.CurrentRun)
	require.NoError(t, err)
	assert.Equal(t, on.EnabledAt, still.EnabledAt, "switching on what is on keeps when it was")

	off, err := r.svc.SetEnabled(t.Context(), rt.ID, false, autopilot.CurrentRun)
	require.NoError(t, err)
	assert.False(t, off.Enabled)
	assert.Nil(t, off.EnabledAt)

	dry, err := r.svc.SetMode(t.Context(), rt.ID, autopilotprefs.ModeDryRun, autopilot.CurrentRun)
	require.NoError(t, err)
	assert.Equal(t, autopilotprefs.ModeDryRun, dry.Mode)

	for _, mode := range []autopilotprefs.Mode{autopilotprefs.ModeAsk, autopilotprefs.ModeAuto} {
		_, err = r.svc.SetMode(t.Context(), rt.ID, mode, autopilot.CurrentRun)
		require.ErrorIs(t, err, autopilot.ErrDryRunOnly, mode)
		assert.Equal(t, "This routine runs in Dry run for now: this version of "+brand.Name+
			" can't yet run “Run a coding agent”.", uierr.Message(err))
	}

	notify := r.save(t, draft("Notify", notifySpec()))

	for _, mode := range []autopilotprefs.Mode{autopilotprefs.ModeAsk, autopilotprefs.ModeAuto} {
		got, setErr := r.svc.SetMode(t.Context(), notify.ID, mode, autopilot.CurrentRun)
		require.NoError(t, setErr, "Notify me runs in this build: %s", mode)
		assert.Equal(t, mode, got.Mode)
	}

	_, err = r.svc.SetMode(t.Context(), rt.ID, "", autopilot.CurrentRun)
	require.ErrorIs(t, err, autopilotprefs.ErrUnknownMode)

	_, err = r.svc.SetEnabled(t.Context(), 999, true, autopilot.CurrentRun)
	require.ErrorIs(t, err, autopilot.ErrNotFound)
}

func TestReorder(t *testing.T) {
	t.Parallel()

	r := newRig(t, autopilotprefs.ModeAsk)
	a := r.save(t, draft("A", notifySpec()))
	b := r.save(t, draft("B", notifySpec()))
	c := r.save(t, draft("C", notifySpec()))

	got, err := r.svc.Reorder(t.Context(), []int64{c.ID, a.ID, c.ID})
	require.NoError(t, err)
	assert.Equal(t, []int64{c.ID, a.ID, b.ID}, ids(got), "those named first, in order, and the rest as they were")
	assert.Equal(t, ids(got), ids(r.list(t)))

	_, err = r.svc.Reorder(t.Context(), []int64{b.ID, 999})
	require.ErrorIs(t, err, autopilot.ErrNotFound)
	assert.Equal(t, []int64{c.ID, a.ID, b.ID}, ids(r.list(t)), "nothing moves")
}

func TestARoutineOfAnotherAccountIsMarked(t *testing.T) {
	t.Parallel()

	r := newRig(t, autopilotprefs.ModeAsk)
	rt := r.save(t, draft("Mine", notifySpec()))

	r.accounts.signIn("other-user")

	got := r.list(t)
	require.Len(t, got, 1)
	assert.True(t, got[0].OtherAccount, "it belongs to %s", rt.Account)
	assert.Equal(t, viewer, got[0].Account)

	r.accounts.signIn("OCTO-USER")
	assert.False(t, r.list(t)[0].OtherAccount, "logins ignore case")

	r.accounts.signIn("")
	assert.False(t, r.list(t)[0].OtherAccount, "with no one signed in, no routine is another's")
}

func TestARoutineOfNoAccountBecomesTheEditorsWhenSaved(t *testing.T) {
	t.Parallel()

	r := newRig(t, autopilotprefs.ModeAsk)
	r.accounts.signIn("")

	rt := r.save(t, draft("Before signing in", notifySpec()))
	assert.Empty(t, rt.Account)

	r.accounts.signIn(viewer)
	assert.False(t, r.list(t)[0].OtherAccount, "a routine of no account is the account signed in's")

	saved := r.save(t, autopilot.Draft{ID: rt.ID, Name: rt.Name, Spec: notifySpec()})
	assert.Equal(t, viewer, saved.Account)
}

func TestASpecFromANewerBuildLoadsAsBrokenAndIsNeverRewritten(t *testing.T) {
	t.Parallel()

	r := newRig(t, autopilotprefs.ModeAsk)
	fine := r.save(t, draft("Fine", notifySpec()))

	const newer = `{"version":2,"watch":{"sources":[]},"somethingNew":{"x":1}}`

	id := r.insertRaw(t, "From the future", "neverHeardOf", newer)

	got := r.list(t)
	require.Len(t, got, 2, "the list carries on with the rest")
	assert.Equal(t, fine.ID, got[0].ID)

	broken := got[1]
	assert.Equal(t, spec.ReasonNewer, broken.BrokenReason)
	assert.Equal(t, "Made by a newer "+brand.Name+": update to edit it.", broken.Broken)
	assert.Empty(t, broken.Sentence)
	assert.Equal(t, autopilotprefs.ModeDryRun, broken.Mode, "a mode this build doesn't know reads as Dry run")
	assert.NotNil(t, broken.Problems, "its lists are empty, never null")
	assert.NotNil(t, broken.DryRunOnly)
	assert.NotNil(t, broken.Spec.Watch.Sources)
	assert.NotNil(t, broken.Spec.Conditions.Of)
	assert.NotNil(t, broken.Spec.Steps)

	_, err := r.svc.SetEnabled(t.Context(), id, true, autopilot.CurrentRun)
	require.ErrorIs(t, err, autopilot.ErrBroken)

	_, err = r.svc.SetMode(t.Context(), id, autopilotprefs.ModeDryRun, autopilot.CurrentRun)
	require.ErrorIs(t, err, autopilot.ErrBroken)

	_, err = r.svc.Save(t.Context(), autopilot.Draft{ID: id, Name: "From the future", Spec: notifySpec()})
	require.ErrorIs(t, err, autopilot.ErrBroken, "a newer build's routine is never saved over")

	_, err = r.svc.SetEnabled(t.Context(), id, false, autopilot.CurrentRun)
	require.NoError(t, err, "it can always be switched off")

	_, err = r.svc.Reorder(t.Context(), []int64{id, fine.ID})
	require.NoError(t, err)

	_, err = r.svc.Get(t.Context(), id)
	require.NoError(t, err)

	//nolint:testifylint // byte for byte, not as JSON: no read or change rewrites the spec
	assert.Equal(t, newer, r.storedSpec(t, id), "no read or change rewrites its spec")

	var mode string

	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT mode FROM autopilot_routines WHERE id = ?`, id).
		Scan(&mode))
	assert.Equal(t, "neverHeardOf", mode, "nor its mode")
}

func TestASpecThatNoLongerValidatesIsBrokenWithItsProblems(t *testing.T) {
	t.Parallel()

	r := newRig(t, autopilotprefs.ModeAsk)
	id := r.insertRaw(t, "Stale", "ask",
		`{"version":1,"watch":{"sources":[]},"trigger":{"event":"appears"},"conditions":{"match":"all","of":[]},`+
			`"steps":[],"limits":{}}`)

	got, err := r.svc.Get(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, spec.ReasonInvalid, got.BrokenReason)
	assert.NotEmpty(t, got.Problems)
	assert.Contains(t, got.Broken, "It isn't valid:")

	fixed := r.save(t, autopilot.Draft{ID: id, Name: "Stale", Spec: notifySpec()})
	assert.Empty(t, fixed.BrokenReason, "saving a valid spec over it mends it")
	assert.Equal(t, autopilotprefs.ModeAsk, fixed.Mode, "and keeps its mode, as Notify me runs")

	later := r.save(t, autopilot.Draft{ID: id, Name: "Stale", Spec: agentSpec()})
	assert.Equal(t, autopilotprefs.ModeDryRun, later.Mode, "a step this build can't run keeps it in Dry run")
}

func TestAnUnreadableSpecIsBroken(t *testing.T) {
	t.Parallel()

	r := newRig(t, autopilotprefs.ModeAsk)
	id := r.insertRaw(t, "Garbled", "ask", `{"version":1,"watch":`)

	got, err := r.svc.Get(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, spec.ReasonUnreadable, got.BrokenReason)
	assert.Equal(t, "Its definition can't be read.", got.Broken)
}

func TestEveryChangeRewritesTheCopy(t *testing.T) {
	t.Parallel()

	r := newRig(t, autopilotprefs.ModeAsk)

	rt := r.save(t, draft(firstName, notifySpec()))
	assert.Equal(t, []string{firstName}, copyNames(t, r.file))

	copied, err := r.svc.Duplicate(t.Context(), rt.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{firstName, "First copy"}, copyNames(t, r.file))

	_, err = r.svc.Reorder(t.Context(), []int64{copied.ID})
	require.NoError(t, err)
	assert.Equal(t, []string{"First copy", firstName}, copyNames(t, r.file))

	_, err = r.svc.SetEnabled(t.Context(), rt.ID, true, autopilot.CurrentRun)
	require.NoError(t, err)

	b, err := r.file.Load()
	require.NoError(t, err)
	assert.True(t, b.Routines[1].Enabled)

	require.NoError(t, r.svc.Delete(t.Context(), copied.ID))
	assert.Equal(t, []string{firstName}, copyNames(t, r.file))
}

func TestARoutineWithoutADatabaseIsRefused(t *testing.T) {
	t.Parallel()

	svc := autopilot.NewService(autopilot.Config{})

	got, err := svc.List(t.Context())
	require.NoError(t, err)
	assert.Empty(t, got)

	_, err = svc.Save(t.Context(), draft("No database", notifySpec()))
	require.ErrorIs(t, err, autopilot.ErrNotSetUp)

	assert.NotEmpty(t, svc.Catalog().Steps, "the catalog needs no database")
}

// ids returns each routine's ID, in order.
func ids(routines []autopilot.Routine) []int64 {
	out := make([]int64, 0, len(routines))
	for _, rt := range routines {
		out = append(out, rt.ID)
	}

	return out
}

// copyNames returns the names the copy holds, in order.
func copyNames(t *testing.T, f *autopilot.File) []string {
	t.Helper()

	b, err := f.Load()
	require.NoError(t, err)

	out := make([]string, 0, len(b.Routines))
	for _, rt := range b.Routines {
		out = append(out, rt.Name)
	}

	return out
}
