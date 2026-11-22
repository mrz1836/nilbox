package settings

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/backup"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// autopilotOf returns Autopilot's default settings with change made to them.
func autopilotOf(change func(*autopilotprefs.Config)) autopilotprefs.Config {
	c := autopilotprefs.Defaults()
	change(&c)

	return c
}

func TestAutopilotStartsWithItsDefaults(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, autopilotprefs.Defaults(), got.Autopilot)
	assert.False(t, got.Autopilot.Enabled, "Autopilot (beta) is off")
	assert.False(t, got.Autopilot.Paused)
	assert.Equal(t, autopilotprefs.ModeAsk, got.Autopilot.DefaultMode, "new routines ask first")
	assert.True(t, got.Autopilot.DailyCap.Off(), "the daily cap is off")
}

func TestAutopilotSettersStoreEachSettingAndTellOnce(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	svc := serviceOver(t, testutil.DB(t), t.TempDir(), WithEvents(events))
	ctx := t.Context()
	tokens := autopilotprefs.DailyCap{Amount: 750_000, Unit: autopilotprefs.UnitTokens}

	for _, c := range []struct {
		name string
		set  func() (Settings, error)
		want func(*autopilotprefs.Config)
	}{
		{
			"enabled", func() (Settings, error) { return svc.SetAutopilotEnabled(ctx, true) },
			func(c *autopilotprefs.Config) { c.Enabled = true },
		},
		{
			"paused", func() (Settings, error) { return svc.SetAutopilotPaused(ctx, true) },
			func(c *autopilotprefs.Config) { c.Paused = true },
		},
		{
			"default mode", func() (Settings, error) { return svc.SetAutopilotDefaultMode(ctx, autopilotprefs.ModeAuto) },
			func(c *autopilotprefs.Config) { c.DefaultMode = autopilotprefs.ModeAuto },
		},
		{
			"agents", func() (Settings, error) { return svc.SetAutopilotAgentsAtOnce(ctx, 3) },
			func(c *autopilotprefs.Config) { c.AgentsAtOnce = 3 },
		},
		{
			"runs", func() (Settings, error) { return svc.SetAutopilotRunsPerDay(ctx, 200) },
			func(c *autopilotprefs.Config) { c.RunsPerDay = 200 },
		},
		{
			"actions", func() (Settings, error) { return svc.SetAutopilotActionsPerDay(ctx, 1) },
			func(c *autopilotprefs.Config) { c.ActionsPerDay = 1 },
		},
		{
			"cap", func() (Settings, error) { return svc.SetAutopilotDailyCap(ctx, tokens) },
			func(c *autopilotprefs.Config) { c.DailyCap = tokens },
		},
		{
			"awake", func() (Settings, error) { return svc.SetAutopilotKeepAwake(ctx, true) },
			func(c *autopilotprefs.Config) { c.KeepAwake = true },
		},
		{
			"outage", func() (Settings, error) { return svc.SetAutopilotHoldOnOutage(ctx, false) },
			func(c *autopilotprefs.Config) { c.HoldOnOutage = false },
		},
		{
			"catch up", func() (Settings, error) { return svc.SetAutopilotCatchUpHours(ctx, 168) },
			func(c *autopilotprefs.Config) { c.CatchUpHours = 168 },
		},
		{
			"history", func() (Settings, error) { return svc.SetAutopilotHistoryDays(ctx, 365) },
			func(c *autopilotprefs.Config) { c.HistoryDays = 365 },
		},
		{
			"logs", func() (Settings, error) { return svc.SetAutopilotLogDays(ctx, 90) },
			func(c *autopilotprefs.Config) { c.LogDays = 90 },
		},
		{
			"past the rules", func() (Settings, error) { return svc.SetAutopilotMergePastRules(ctx, true) },
			func(c *autopilotprefs.Config) { c.MergePastRules = true },
		},
	} {
		before, err := svc.Get(ctx)
		require.NoError(t, err)

		want := before.Autopilot
		c.want(&want)

		got, err := c.set()
		require.NoError(t, err, c.name)
		assert.Equal(t, want, got.Autopilot, "%s: the setter returns the settings with it", c.name)

		again, err := svc.Get(ctx)
		require.NoError(t, err)
		assert.Equal(t, want, again.Autopilot, "%s: it is stored", c.name)
	}

	assert.Len(t, events.All(), 13, "each setter tells once")

	for _, e := range events.All() {
		assert.Equal(t, event.AutopilotChanged{}, e)
	}
}

func TestAutopilotSettersRefuseOutOfRangeAndStoreNothing(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	svc := serviceOver(t, testutil.DB(t), t.TempDir(), WithEvents(events))
	ctx := t.Context()

	for _, c := range []struct {
		set  func() (Settings, error)
		want string
	}{
		{
			func() (Settings, error) { return svc.SetAutopilotDefaultMode(ctx, "always") },
			"That isn't one of the modes: choose Dry run, Ask first, or Autopilot.",
		},
		{func() (Settings, error) { return svc.SetAutopilotAgentsAtOnce(ctx, 4) }, "Choose 1 to 3 agents at once."},
		{func() (Settings, error) { return svc.SetAutopilotAgentsAtOnce(ctx, 0) }, "Choose 1 to 3 agents at once."},
		{func() (Settings, error) { return svc.SetAutopilotRunsPerDay(ctx, 201) }, "Choose 1 to 200 agent runs a day."},
		{
			func() (Settings, error) { return svc.SetAutopilotActionsPerDay(ctx, 0) },
			"Choose 1 to 2000 GitHub actions a day.",
		},
		{
			func() (Settings, error) { return svc.SetAutopilotCatchUpHours(ctx, 0) },
			"Choose 1 to 168 hours before Autopilot asks to catch up.",
		},
		{
			func() (Settings, error) { return svc.SetAutopilotHistoryDays(ctx, 366) },
			"Keep 7 to 365 days of Autopilot's history.",
		},
		{func() (Settings, error) { return svc.SetAutopilotLogDays(ctx, 0) }, "Keep the run logs 1 to 90 days."},
		{func() (Settings, error) {
			return svc.SetAutopilotDailyCap(ctx, autopilotprefs.DailyCap{Amount: 501, Unit: autopilotprefs.UnitUSD})
		}, "Set the daily cap to 0 for none, or $1 to $500."},
		{func() (Settings, error) {
			return svc.SetAutopilotDailyCap(ctx, autopilotprefs.DailyCap{Amount: 99, Unit: autopilotprefs.UnitTokens})
		}, "Set the daily cap to 0 for none, or 100,000 to 50,000,000 tokens."},
		{func() (Settings, error) {
			return svc.SetAutopilotDailyCap(ctx, autopilotprefs.DailyCap{Amount: 5, Unit: "euros"})
		}, "The daily cap is set in dollars or in tokens."},
	} {
		_, err := c.set()
		require.Error(t, err, c.want)
		assert.Equal(t, c.want, uierr.Message(err))
	}

	got, err := svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, autopilotprefs.Defaults(), got.Autopilot, "a refused setting stores nothing")
	assert.Zero(t, events.Count(), "a refused setting tells nothing")

	stored, err := svc.prefs.GetMany(ctx, autopilotprefs.Keys()...)
	require.NoError(t, err)
	assert.Empty(t, stored, "no key is written")
}

func TestAutopilotRunLogsNeverOutlastTheHistory(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)
	ctx := t.Context()

	_, err := svc.SetAutopilotLogDays(ctx, 60)
	require.NoError(t, err)

	got, err := svc.SetAutopilotHistoryDays(ctx, 14)
	require.NoError(t, err)
	assert.Equal(t, 14, got.Autopilot.HistoryDays)
	assert.Equal(t, 14, got.Autopilot.LogDays, "a shorter history shortens the logs with it")

	_, err = svc.SetAutopilotLogDays(ctx, 15)
	require.ErrorIs(t, err, autopilotprefs.ErrLogsOutlastHistory)
	assert.Equal(t, "Keep the run logs no longer than the history: 14 days at most.", uierr.Message(err))

	got, err = svc.SetAutopilotHistoryDays(ctx, 120)
	require.NoError(t, err)
	assert.Equal(t, 14, got.Autopilot.LogDays, "a longer history leaves the logs as they were")
}

func TestAutopilotSettersWriteOnlyWhatChanges(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)
	ctx := t.Context()

	_, err := svc.SetAutopilotAgentsAtOnce(ctx, autopilotprefs.DefaultAgentsAtOnce)
	require.NoError(t, err)

	_, err = svc.SetAutopilotKeepAwake(ctx, true)
	require.NoError(t, err)

	stored, err := svc.prefs.GetMany(ctx, autopilotprefs.Keys()...)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{autopilotprefs.KeyKeepAwake: "true"}, stored,
		"a setting left at its default keeps following it")
}

func TestAStoredAutopilotSettingTheSetterWouldRefuseReadsAsItsDefault(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	setPref(t, db, autopilotprefs.KeyDefaultMode, "always")
	setPref(t, db, autopilotprefs.KeyAgentsAtOnce, "9")

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, autopilotprefs.Defaults(), got.Autopilot)
}

func TestPortableAutopilotRoundTrip(t *testing.T) {
	t.Parallel()

	src, _, _ := newTestService(t)
	ctx := t.Context()

	for _, set := range []func(context.Context) (Settings, error){
		func(ctx context.Context) (Settings, error) { return src.SetAutopilotEnabled(ctx, true) },
		func(ctx context.Context) (Settings, error) { return src.SetAutopilotPaused(ctx, true) },
		func(ctx context.Context) (Settings, error) {
			return src.SetAutopilotDefaultMode(ctx, autopilotprefs.ModeDryRun)
		},
		func(ctx context.Context) (Settings, error) { return src.SetAutopilotAgentsAtOnce(ctx, 1) },
		func(ctx context.Context) (Settings, error) {
			return src.SetAutopilotDailyCap(ctx, autopilotprefs.DailyCap{Amount: 25, Unit: autopilotprefs.UnitUSD})
		},
		func(ctx context.Context) (Settings, error) { return src.SetAutopilotHistoryDays(ctx, 20) },
		func(ctx context.Context) (Settings, error) { return src.SetAutopilotLogDays(ctx, 10) },
	} {
		_, err := set(ctx)
		require.NoError(t, err)
	}

	p, err := NewBackupPart(src).Portable(ctx)
	require.NoError(t, err)
	require.NotNil(t, p.Autopilot)

	dst, dstDB, _ := newTestService(t)
	_, err = dst.SetAutopilotLogDays(ctx, 60)
	require.NoError(t, err)

	part := NewBackupPart(dst)

	plan, err := part.PlanPortable(ctx, Portable{Autopilot: p.Autopilot})
	require.NoError(t, err)
	assert.Empty(t, plan.Notes)
	require.NotNil(t, p.Autopilot.Enabled)
	assert.True(t, *p.Autopilot.Enabled, "the backup holds Autopilot (beta) on")
	assert.Contains(t, plan.Items, backup.Item{
		Label: "Autopilot (beta)", Change: backup.ChangeUpdate, Detail: "on (now off)",
	})
	assert.Contains(t, plan.Items, backup.Item{
		Label: "Autopilot's daily cap", Change: backup.ChangeUpdate, Detail: "$25 (now off)",
	})
	assert.Contains(t, plan.Items, backup.Item{
		Label: "Autopilot's history and run logs", Change: backup.ChangeUpdate, Detail: "20 days, run logs 10 (now 90 days, run logs 60)",
	})

	applyPlan(t, part, dstDB, plan)

	want, err := src.Get(ctx)
	require.NoError(t, err)

	got, err := dst.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, want.Autopilot, got.Autopilot, "every Autopilot setting comes back")
}

func TestABackupNeverCarriesMergingPastTheRules(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	src, _, _ := newTestService(t)
	_, err := src.SetAutopilotMergePastRules(ctx, true)
	require.NoError(t, err)

	p, err := NewBackupPart(src).Portable(ctx)
	require.NoError(t, err)

	raw, err := json.Marshal(p)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "astRules", "an export never holds the switch")

	t.Run("an import leaves it off", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()

		dst, dstDB, _ := newTestService(t)
		part := NewBackupPart(dst)

		plan, err := part.PlanPortable(ctx, Portable{Autopilot: p.Autopilot})
		require.NoError(t, err)
		applyPlan(t, part, dstDB, plan)

		got, err := dst.Get(ctx)
		require.NoError(t, err)
		assert.False(t, got.Autopilot.MergePastRules)
	})

	t.Run("an import leaves it on", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()

		dst, dstDB, _ := newTestService(t)
		_, err := dst.SetAutopilotMergePastRules(ctx, true)
		require.NoError(t, err)

		part := NewBackupPart(dst)

		plan, err := part.PlanPortable(ctx, Portable{Autopilot: p.Autopilot})
		require.NoError(t, err)
		applyPlan(t, part, dstDB, plan)

		got, err := dst.Get(ctx)
		require.NoError(t, err)
		assert.True(t, got.Autopilot.MergePastRules)
	})
}

func TestPlanPortableAutopilotLeavesOutWhatItRefuses(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	ctx := t.Context()

	agents, logs, mode := 7, 120, autopilotprefs.Mode("always")
	plan, err := NewBackupPart(svc).PlanPortable(ctx, Portable{Autopilot: &PortableAutopilot{
		AgentsAtOnce: &agents, LogDays: &logs, DefaultMode: &mode,
	}})
	require.NoError(t, err)
	assert.Equal(t, []string{
		"New routines start in was left as it is. That isn't one of the modes: choose Dry run, Ask first, or Autopilot.",
		"Agents at once was left as it is. Choose 1 to 3 agents at once.",
		"Autopilot's history and run logs was left as it is. Keep the run logs 1 to 90 days.",
	}, plan.Notes)
	assert.Empty(t, changed(plan.Plan))

	// A history alone shortens the logs with it, as its setter does.
	history := 20
	plan, err = NewBackupPart(svc).PlanPortable(ctx, Portable{Autopilot: &PortableAutopilot{HistoryDays: &history}})
	require.NoError(t, err)
	assert.Empty(t, plan.Notes)
	applyPlan(t, NewBackupPart(svc), db, plan)

	got, err := svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, 20, got.Autopilot.HistoryDays)
	assert.Equal(t, 20, got.Autopilot.LogDays)

	// Logs alone that fit their range but outlast this Mac's history are refused.
	logs = 45
	plan, err = NewBackupPart(svc).PlanPortable(ctx, Portable{Autopilot: &PortableAutopilot{LogDays: &logs}})
	require.NoError(t, err)
	assert.Equal(t, []string{
		"Autopilot's history and run logs was left as it is. " +
			"Keep the run logs no longer than the history: 20 days at most.",
	}, plan.Notes)
}
