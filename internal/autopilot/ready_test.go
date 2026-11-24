package autopilot_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
)

// needIDs names each need.
func needIDs(needs []autopilot.Need) []string {
	out := make([]string, 0, len(needs))
	for _, n := range needs {
		out = append(out, n.ID())
	}

	return out
}

func TestNeedsForEveryKindOfStep(t *testing.T) {
	t.Parallel()

	defaults := autopilot.LaunchDefaults{Agent: "claude", Profiles: map[string]string{"codex": "work"}}
	base := notifySpec()

	for _, c := range []struct {
		name  string
		steps []spec.Step
		want  []string
	}{
		{name: "a notice needs banners", steps: []spec.Step{{Notify: &spec.NotifyStep{}}}, want: []string{"github", "banners"}},
		{name: "Tell me too", steps: []spec.Step{{TellMe: &spec.TellStep{}}}, want: []string{"github", "banners"}},
		{name: "a triage step only GitHub", steps: []spec.Step{{Pin: &spec.Empty{}}}, want: []string{"github"}},
		{
			name:  "a GitHub step only GitHub",
			steps: []spec.Step{{GitHub: []spec.GitHubAction{{Rebase: &spec.Empty{}}}}}, want: []string{"github"},
		},
		{
			name:  "an agent in the terminal inherits the app's agent and its default profile",
			steps: []spec.Step{{Agent: &spec.AgentStep{Where: catalog.WhereTerminal}}},
			want: []string{
				"github", "place:terminal", "terminal", "agent:claude", "profile:claude/default", "tool:gh", "tool:git",
			},
		},
		{
			name: "an agent in the background, with its rows and fallbacks",
			steps: []spec.Step{{Agent: &spec.AgentStep{
				Agent: "claude", Profile: "work",
				LaunchFor: []spec.LaunchRow{{Target: "acme-labs", Agent: "codex"}, {Target: "acme-labs/api", Model: "opus"}},
				Fallbacks: []spec.ProfileRef{{Agent: "claude", Profile: "spare"}},
			}}},
			want: []string{
				"github", "place:background", "agent:claude", "profile:claude/work", "agent:codex", "profile:codex/work",
				"profile:claude/spare", "tool:gh", "tool:git",
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			sp := base
			sp.Steps = c.steps

			assert.Equal(t, c.want, needIDs(autopilot.Needs(sp, defaults)))
		})
	}

	withTools := base
	withTools.Requires = []string{"pnpm", "go"}
	assert.Equal(t, []string{"github", "banners", "tool:pnpm", "tool:go"}, needIDs(autopilot.Needs(withTools, defaults)),
		"what it Requires, after what it works out")
}

// checker is a readiness checker over fixed levels by need ID; any other is ok.
type checker struct {
	levels map[string]autopilot.ReadyLevel
	asked  int
	fresh  []bool
}

func (c *checker) Check(_ context.Context, needs []autopilot.Need, fresh bool) ([]autopilot.ReadyCheck, error) {
	c.asked++
	c.fresh = append(c.fresh, fresh)

	out := make([]autopilot.ReadyCheck, 0, len(needs))
	for _, n := range needs {
		level := autopilot.ReadyOK
		if l, ok := c.levels[n.ID()]; ok {
			level = l
		}

		out = append(out, autopilot.ReadyCheck{Need: n, Label: n.ID(), Level: level, Found: string(level)})
	}

	return out, nil
}

func (*checker) Defaults(context.Context) (autopilot.LaunchDefaults, error) {
	return autopilot.LaunchDefaults{Agent: "claude"}, nil
}

func TestAWarningLeavesARoutineReadyAndAProblemDoesnt(t *testing.T) {
	t.Parallel()

	ck := &checker{levels: map[string]autopilot.ReadyLevel{
		"terminal": autopilot.ReadyWarning, "profile:claude/default": autopilot.ReadyProblem,
	}}
	r := newRig(t, autopilotprefs.ModeAsk)
	r.svc = autopilot.NewService(autopilot.Config{Pool: r.db, Accounts: r.accounts, Clock: r.clock, Readiness: ck})

	agent := r.save(t, draft("Fix", agentSpec()))
	_, err := r.svc.SetEnabled(t.Context(), agent.ID, true, autopilot.CurrentRun)
	require.NoError(t, err)

	notify := r.save(t, draft("Tell", notifySpec()))
	_, err = r.svc.SetEnabled(t.Context(), notify.ID, true, autopilot.CurrentRun)
	require.NoError(t, err)

	got, err := r.svc.Readiness(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, ck.asked, "the first read checks, as none ran yet")
	assert.Equal(t, []bool{true}, ck.fresh)
	require.Len(t, got.Routines, 2)
	assert.Equal(t, 1, got.NotReady)

	byID := map[int64]autopilot.RoutineReady{}
	for _, rr := range got.Routines {
		byID[rr.RoutineID] = rr
	}

	assert.False(t, byID[agent.ID].Ready)
	require.Len(t, byID[agent.ID].Problems, 1)
	assert.Equal(t, "profile:claude/default", byID[agent.ID].Problems[0].ID)
	require.Len(t, byID[agent.ID].Warnings, 1)
	assert.Equal(t, "terminal", byID[agent.ID].Warnings[0].ID, "a warning only warns")
	assert.True(t, byID[notify.ID].Ready)

	again, err := r.svc.Readiness(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, ck.asked, "a later read serves the last checks")
	assert.Equal(t, got.Checks, again.Checks)

	_, err = r.svc.CheckReadiness(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 2, ck.asked, "Check again checks again")
}

func TestAFallbackThisMacLacksOnlyWarns(t *testing.T) {
	t.Parallel()

	ck := &checker{levels: map[string]autopilot.ReadyLevel{
		"agent:codex": autopilot.ReadyProblem, "profile:codex/default": autopilot.ReadyProblem,
	}}
	r := newRig(t, autopilotprefs.ModeAsk)
	r.svc = autopilot.NewService(autopilot.Config{Pool: r.db, Accounts: r.accounts, Clock: r.clock, Readiness: ck})

	sp := backgroundSpec()
	sp.Steps[0].Agent.Fallbacks = []spec.ProfileRef{{Agent: "codex", Profile: "default"}}

	needs := autopilot.Needs(sp, autopilot.LaunchDefaults{Agent: "claude"})
	for _, n := range needs {
		assert.Equal(t, n.Agent == "codex", n.Fallback, "%s is the fallback's alone", n.ID())
	}

	fix := r.save(t, draft("Fix", sp))
	_, err := r.svc.SetEnabled(t.Context(), fix.ID, true, autopilot.CurrentRun)
	require.NoError(t, err)

	got, err := r.svc.Readiness(t.Context())
	require.NoError(t, err)
	require.Len(t, got.Routines, 1)

	rr := got.Routines[0]
	assert.True(t, rr.Ready, "the step runs on its own profile")
	assert.Empty(t, rr.Problems)
	assert.Len(t, rr.Warnings, 2, "Codex, missing here, only warns")

	// A need its own profile brings too is never only a fallback's.
	sp.Steps[0].Agent.Fallbacks = []spec.ProfileRef{{Agent: "claude", Profile: "default"}}
	for _, n := range autopilot.Needs(sp, autopilot.LaunchDefaults{Agent: "claude"}) {
		assert.False(t, n.Fallback, n.ID())
	}
}

func TestPreviewRunPlansTheDraftsAgentStepAsItWouldStart(t *testing.T) {
	t.Parallel()

	previews := &previewer{}
	r := newRig(t, autopilotprefs.ModeAsk)
	r.svc = autopilot.NewService(autopilot.Config{Pool: r.db, Accounts: r.accounts, Clock: r.clock, Previews: previews})

	d := draft("Fix", agentSpec())
	d.ID = 7

	got, err := r.svc.PreviewRun(t.Context(), d, 42)
	require.NoError(t, err)
	assert.Equal(t, "claude --model opus", got.Command)
	assert.NotNil(t, got.Env)
	assert.NotNil(t, got.Warnings)
	assert.NotNil(t, got.Sources)

	require.Len(t, previews.asked, 1)
	asked := previews.asked[0]
	assert.Equal(t, int64(42), asked.ItemID)
	assert.Equal(t, int64(7), asked.RoutineID)
	assert.Equal(t, "Fix", asked.Routine)
	assert.Equal(t, catalog.EventChecksFailed, asked.Trigger)
	assert.Equal(t, 1, asked.Attempt)
	assert.Equal(t, "fix-checks", asked.Step.Prompt.Starter)
	assert.Empty(t, asked.Confirmed, "a preview confirms nothing")

	_, err = r.svc.PreviewRun(t.Context(), draft("Tell", notifySpec()), 42)
	require.ErrorIs(t, err, autopilot.ErrNoAgentStep)

	older := autopilot.NewService(autopilot.Config{
		Pool: r.db, Accounts: r.accounts, Clock: r.clock, Previews: previews, Offers: terminalOnly(),
	})
	_, err = older.PreviewRun(t.Context(), draft("Fix", backgroundSpec()), 42)
	require.ErrorIs(t, err, spec.ErrInvalid, "a draft with a problem previews nothing")
}

// previewer records what it was asked to preview.
type previewer struct {
	asked []autopilot.RunRequest
}

func (p *previewer) PreviewRun(_ context.Context, req autopilot.RunRequest) (autopilot.RunPreview, error) {
	p.asked = append(p.asked, req)

	return autopilot.RunPreview{Command: "claude --model opus"}, nil
}

func TestAPromptRenamedIsFollowedByTheRoutinesThatNameIt(t *testing.T) {
	t.Parallel()

	r := newRig(t, autopilotprefs.ModeAsk)

	mine := agentSpec()
	mine.Steps[0].Agent.Prompt = spec.PromptRef{Nickname: "Triage"}
	byName := r.save(t, draft("By name", mine))
	byStarter := r.save(t, draft("By starter", agentSpec()))

	follower := autopilot.NewFollower(r.svc)

	uses, err := follower.Uses(t.Context())
	require.NoError(t, err)
	assert.ElementsMatch(t, []autopilot.PromptUse{
		{Routine: "By name", Nickname: "Triage"}, {Routine: "By starter", Starter: "fix-checks"},
	}, uses)

	n, err := follower.Follow(t.Context(), []autopilot.PromptRename{{From: "TRIAGE", To: "Sort the checks"}})
	require.NoError(t, err)
	assert.Equal(t, 1, n, "a nickname matches ignoring case; a starter's key follows its prompt on its own")

	got, err := r.svc.Get(t.Context(), byName.ID)
	require.NoError(t, err)
	assert.Equal(t, "Sort the checks", got.Spec.Steps[0].Agent.Prompt.Nickname)

	kept, err := r.svc.Get(t.Context(), byStarter.ID)
	require.NoError(t, err)
	assert.Equal(t, "fix-checks", kept.Spec.Steps[0].Agent.Prompt.Starter)

	n, err = follower.Follow(t.Context(), []autopilot.PromptRename{{From: "Nothing named so", To: "Anything"}})
	require.NoError(t, err)
	assert.Zero(t, n)
}

func TestNeedsYouListsWhatWaitsAndNothingElse(t *testing.T) {
	t.Parallel()

	r := newRig(t, autopilotprefs.ModeAsk)

	got, err := r.svc.NeedsYou(t.Context())
	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}
