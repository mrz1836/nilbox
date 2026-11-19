package autopilot

import (
	"context"
	"strconv"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// unbuilt is the sheets' services, whose preview of an action can't be built.
type unbuilt struct {
	fakeActions
}

func (*unbuilt) Preview(context.Context, ItemRef, spec.GitHubAction) (string, error) {
	return "", uierr.New("The reply is no longer in your library.", errBroken)
}

// labeling is a routine that labels each of the user's pull requests as it appears, then
// marks its GitHub notification done.
func labeling() spec.Spec {
	return spec.Spec{
		Version: spec.Version,
		Watch:   spec.Watch{Sources: []spec.Source{{Lists: []catalog.ListSource{catalog.SourceAuthored}}}},
		Trigger: spec.Trigger{Event: catalog.EventAppears},
		Steps: []spec.Step{
			{GitHub: []spec.GitHubAction{{Labels: &spec.LabelsAction{Add: []string{"triaged"}}}}},
			{MarkDone: &spec.Empty{}},
		},
	}
}

func TestTheGitHubDispatcherRefusesAnOutcomeItDoesntKnow(t *testing.T) {
	t.Parallel()

	job := Job{Step: labeling().Steps[0]}

	_, err := NewGitHub(&fakeActions{got: GitHubSent{Outcome: "teleported"}}).Dispatch(t.Context(), job)
	require.ErrorIs(t, err, errNoAction)
	assert.Contains(t, err.Error(), "teleported")

	_, err = NewGitHub(&fakeActions{err: errBroken}).Dispatch(t.Context(), Job{Step: spec.Step{MarkDone: &spec.Empty{}}})
	require.ErrorIs(t, err, errBroken, "marking done that fails fails the step")
}

func TestThePreviewOfAGitHubStepSaysWhatEachActionWouldSend(t *testing.T) {
	t.Parallel()

	stored := map[int64]facts.Facts{
		cmdPR: {Item: facts.Item{ID: cmdPR, Kind: "pr", Repo: "acme-org/example-repo", Number: 5}},
	}
	ctx := t.Context()

	svc := NewService(Config{Pool: testutil.DB(t), Items: explainItems{facts: stored}, GitHub: &fakeActions{}})
	got, err := svc.PreviewActions(ctx, Draft{Spec: labeling()}, cmdPR)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, catalog.GitHubLabels, got[0].Action)
	assert.Empty(t, got[0].Problem)
	assert.Equal(t, ActionPreview{
		Step: 1, Kind: catalog.StepMarkDone, Label: got[1].Label,
		Sends: "Marks its GitHub notifications done, as your rules clear them.",
	}, got[1])
	assert.NotEmpty(t, got[1].Label)

	// An action that can't be built for the item says why, beside the others.
	failing := NewService(Config{Pool: testutil.DB(t), Items: explainItems{facts: stored}, GitHub: &unbuilt{}})
	got, err = failing.PreviewActions(ctx, Draft{Spec: labeling()}, cmdPR)
	require.NoError(t, err)
	assert.Equal(t, "The reply is no longer in your library.", got[0].Problem)
	assert.Empty(t, got[0].Sends)

	for _, c := range []struct {
		name string
		svc  *Service
		d    Draft
		want string
	}{
		{
			"no GitHub step", svc,
			Draft{Spec: checksFailed()},
			"This routine sends nothing to GitHub, so there's nothing to preview.",
		},
		{
			"no way to preview", NewService(Config{Pool: testutil.DB(t)}),
			Draft{Spec: labeling()},
			"Preview isn't available here.",
		},
		{
			"the item can't be read",
			NewService(Config{Pool: testutil.DB(t), Items: explainItems{factsErr: errBroken}, GitHub: &fakeActions{}}),
			Draft{Spec: labeling()},
			"The item couldn't be read from the inbox.",
		},
		{
			"the item is gone", NewService(Config{Pool: testutil.DB(t), Items: explainItems{}, GitHub: &fakeActions{}}),
			Draft{Spec: labeling()},
			"That item isn't in the inbox any more.",
		},
	} {
		_, err = c.svc.PreviewActions(ctx, c.d, cmdPR)
		require.Error(t, err, c.name)
		assert.Equal(t, c.want, uierr.Message(err), c.name)
	}

	noSteps := labeling()
	noSteps.Steps = nil

	_, err = svc.PreviewActions(ctx, Draft{Spec: noSteps}, cmdPR)

	var problems *ProblemsError

	require.ErrorAs(t, err, &problems)
	assert.Contains(t, uierr.Message(err), "Fix the routine's problems first:")
}

func TestABacklogsCardActionsNeedARunningEngine(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	svc := NewService(Config{Pool: testutil.DB(t), Prefs: switchedOn(true)})

	_, err := svc.ApproveBacklog(ctx, 1)
	require.ErrorIs(t, err, ErrNoEngine)

	_, err = svc.SkipBacklog(ctx, 1, []int64{1})
	require.ErrorIs(t, err, ErrNoEngine)

	NewEngine(svc, EngineConfig{Items: &quiet{}})

	approved, err := svc.ApproveBacklog(ctx, 1)
	require.ErrorIs(t, err, ErrNotRunning)
	assert.Equal(t, []Match{}, approved)

	skipped, err := svc.SkipBacklog(ctx, 1, []int64{1})
	require.ErrorIs(t, err, ErrNotRunning)
	assert.Equal(t, []Match{}, skipped)
}

func TestSkippingABacklogSkipsNoneWhenOneCantBeSkipped(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newCmdRig(t, EngineConfig{})
		id := r.checks()
		first := r.match(id, StateWaiting, BlockBacklog, step(catalog.StepNotify, ""))
		second := r.match(id, StateWaiting, BlockBacklog, step(catalog.StepNotify, ""))
		r.start()

		testutil.Exec(t, r.db, `CREATE TRIGGER refused BEFORE UPDATE ON autopilot_matches WHEN OLD.id = `+
			strconv.FormatInt(second, 10)+` BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`)

		_, err := r.svc.SkipBacklog(t.Context(), id, []int64{first, second, 999})
		require.Error(t, err)
		assert.Equal(t, "The backlog couldn't be skipped.", uierr.Message(err))

		got, err := r.svc.matchByID(t.Context(), first)
		require.NoError(t, err)
		assert.Equal(t, StateWaiting, got.State, "one transaction: the first is left waiting too")

		testutil.Exec(t, r.db, `DROP TRIGGER refused`)
		testutil.Exec(t, r.db, `UPDATE autopilot_matches SET steps = '"not steps"' WHERE id = ?`, second)

		_, err = r.svc.SkipBacklog(t.Context(), id, []int64{first, second})
		assert.Equal(t, "The backlog couldn't be skipped.", uierr.Message(err), "a match that can't be read")
	})
}

func TestApprovingABacklogSaysWhenItsMatchesCantBeRead(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newCmdRig(t, EngineConfig{})
		id := r.checks()
		r.match(id, StateWaiting, BlockBacklog, step(catalog.StepNotify, ""))
		r.start()

		testutil.Exec(t, r.db, `UPDATE autopilot_matches SET steps = '"not steps"'`)

		_, err := r.svc.ApproveBacklog(t.Context(), id)
		require.Error(t, err)
		assert.Equal(t, "Autopilot's history couldn't be read.", uierr.Message(err))
	})
}
