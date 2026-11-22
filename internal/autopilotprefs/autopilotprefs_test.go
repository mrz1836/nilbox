package autopilotprefs_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilotprefs"
)

func TestDefaults(t *testing.T) {
	t.Parallel()

	d := autopilotprefs.Defaults()

	assert.Equal(t, autopilotprefs.Config{
		DefaultMode: autopilotprefs.ModeAsk, AgentsAtOnce: 2, RunsPerDay: 20, ActionsPerDay: 500, ActionsPerHour: 120,
		DailyCap: autopilotprefs.DailyCap{Unit: autopilotprefs.UnitUSD}, HoldOnOutage: true, CatchUpHours: 24,
		HistoryDays: 90, LogDays: 30,
	}, d)
	require.NoError(t, d.Check())
	assert.False(t, d.Enabled, "Autopilot (beta) is off on a fresh database")
	assert.True(t, d.DailyCap.Off(), "the daily cap is off on a fresh database")
	assert.Equal(t, d, autopilotprefs.Decode(nil), "nothing stored reads as the defaults")
}

func TestAutopilotIsOffUntilTurnedOn(t *testing.T) {
	t.Parallel()

	// An install from before the switch stored other settings, never it: Autopilot reads as
	// off there too.
	existing := map[string]string{autopilotprefs.KeyPaused: "true", autopilotprefs.KeyAgentsAtOnce: "3"}
	assert.False(t, autopilotprefs.Decode(existing).Enabled)

	for stored, want := range map[string]bool{"true": true, "false": false, "": false, "yes": false} {
		got := autopilotprefs.Decode(map[string]string{autopilotprefs.KeyEnabled: stored})
		assert.Equal(t, want, got.Enabled, "%q", stored)
	}

	on := autopilotprefs.Defaults()
	on.Enabled = true
	assert.Equal(t, map[string]string{autopilotprefs.KeyEnabled: "true"}, autopilotprefs.Writes(nil, on),
		"turning it on writes its key alone")
	assert.NotEqual(t, autopilotprefs.KeyPaused, autopilotprefs.KeyEnabled)
}

func TestMergingPastTheRulesIsOffUntilTurnedOn(t *testing.T) {
	t.Parallel()

	assert.False(t, autopilotprefs.Defaults().MergePastRules)
	assert.False(t, autopilotprefs.Decode(map[string]string{autopilotprefs.KeyEnabled: "true"}).MergePastRules)

	for stored, want := range map[string]bool{"true": true, "false": false, "": false, "yes": false} {
		got := autopilotprefs.Decode(map[string]string{autopilotprefs.KeyMergePastRules: stored})
		assert.Equal(t, want, got.MergePastRules, "%q", stored)
	}

	on := autopilotprefs.Defaults()
	on.MergePastRules = true
	assert.Equal(t, map[string]string{autopilotprefs.KeyMergePastRules: "true"}, autopilotprefs.Writes(nil, on),
		"turning it on writes its key alone")
}

func TestModes(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []autopilotprefs.Mode{autopilotprefs.ModeDryRun, autopilotprefs.ModeAsk, autopilotprefs.ModeAuto},
		autopilotprefs.Modes())
	assert.Equal(t, []string{"Dry run", "Ask first", "Autopilot"}, []string{
		autopilotprefs.ModeDryRun.Label(), autopilotprefs.ModeAsk.Label(), autopilotprefs.ModeAuto.Label(),
	})

	for _, m := range []autopilotprefs.Mode{"", "dryrun", "Ask", "autopilot"} {
		assert.False(t, m.Valid(), m)
	}
}

func TestEncodeReadsBackAsItself(t *testing.T) {
	t.Parallel()

	c := autopilotprefs.Config{
		Enabled: true, Paused: true, DefaultMode: autopilotprefs.ModeDryRun, AgentsAtOnce: 3, RunsPerDay: 200, ActionsPerDay: 1,
		ActionsPerHour: 300,
		DailyCap:       autopilotprefs.DailyCap{Amount: 2_000_000, Unit: autopilotprefs.UnitTokens}, KeepAwake: true,
		CatchUpHours: 168, HistoryDays: 7, LogDays: 7, MergePastRules: true,
	}
	require.NoError(t, c.Check())

	stored := c.Encode()
	assert.ElementsMatch(t, autopilotprefs.Keys(), keysOf(stored), "every key is written")
	assert.Equal(t, c, autopilotprefs.Decode(stored))
}

func TestDecodeFallsBackForAnythingASetterWouldRefuse(t *testing.T) {
	t.Parallel()

	got := autopilotprefs.Decode(map[string]string{
		autopilotprefs.KeyDefaultMode:   "always",
		autopilotprefs.KeyAgentsAtOnce:  "4",
		autopilotprefs.KeyRunsPerDay:    "0",
		autopilotprefs.KeyActionsPerDay: "lots",
		autopilotprefs.KeyDailyCap:      "600",
		autopilotprefs.KeyDailyCapUnit:  "euros",
		autopilotprefs.KeyCatchUpHours:  "169",
		autopilotprefs.KeyHistoryDays:   "6",
		autopilotprefs.KeyLogDays:       "91",
		autopilotprefs.KeyHoldOnOutage:  "maybe",
	})

	assert.Equal(t, autopilotprefs.Defaults(), got)
}

func TestDecodeKeepsTheLogsWithinTheHistory(t *testing.T) {
	t.Parallel()

	got := autopilotprefs.Decode(map[string]string{
		autopilotprefs.KeyHistoryDays: "10", autopilotprefs.KeyLogDays: "60",
	})

	assert.Equal(t, 10, got.HistoryDays)
	assert.Equal(t, 10, got.LogDays, "logs stored longer than the history read as long as it")
}

func TestDecodeReadsACapInItsUnit(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		amount, unit string
		want         autopilotprefs.DailyCap
	}{
		{"25", "usd", autopilotprefs.DailyCap{Amount: 25, Unit: autopilotprefs.UnitUSD}},
		{"0", "tokens", autopilotprefs.DailyCap{Unit: autopilotprefs.UnitTokens}},
		{"500000", "tokens", autopilotprefs.DailyCap{Amount: 500_000, Unit: autopilotprefs.UnitTokens}},
		{"500000", "usd", autopilotprefs.DailyCap{Unit: autopilotprefs.UnitUSD}},
		{"25", "tokens", autopilotprefs.DailyCap{Unit: autopilotprefs.UnitTokens}},
		{"25", "", autopilotprefs.DailyCap{Amount: 25, Unit: autopilotprefs.UnitUSD}},
	} {
		got := autopilotprefs.Decode(map[string]string{
			autopilotprefs.KeyDailyCap: c.amount, autopilotprefs.KeyDailyCapUnit: c.unit,
		})
		assert.Equal(t, c.want, got.DailyCap, "%s %s", c.amount, c.unit)
	}
}

func TestCheckRefusesWhatASetterRefuses(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		change func(*autopilotprefs.Config)
		want   error
	}{
		"a mode":             {func(c *autopilotprefs.Config) { c.DefaultMode = "always" }, autopilotprefs.ErrUnknownMode},
		"no agent":           {func(c *autopilotprefs.Config) { c.AgentsAtOnce = 0 }, autopilotprefs.ErrOutOfRange},
		"four agents":        {func(c *autopilotprefs.Config) { c.AgentsAtOnce = 4 }, autopilotprefs.ErrOutOfRange},
		"201 runs":           {func(c *autopilotprefs.Config) { c.RunsPerDay = 201 }, autopilotprefs.ErrOutOfRange},
		"no actions":         {func(c *autopilotprefs.Config) { c.ActionsPerDay = 0 }, autopilotprefs.ErrOutOfRange},
		"a week and an hour": {func(c *autopilotprefs.Config) { c.CatchUpHours = 169 }, autopilotprefs.ErrOutOfRange},
		"six days":           {func(c *autopilotprefs.Config) { c.HistoryDays = 6 }, autopilotprefs.ErrOutOfRange},
		"no logs":            {func(c *autopilotprefs.Config) { c.LogDays = 0 }, autopilotprefs.ErrOutOfRange},
		"$501":               {func(c *autopilotprefs.Config) { c.DailyCap.Amount = 501 }, autopilotprefs.ErrOutOfRange},
		"too few tokens": {func(c *autopilotprefs.Config) {
			c.DailyCap = autopilotprefs.DailyCap{Amount: 99_999, Unit: autopilotprefs.UnitTokens}
		}, autopilotprefs.ErrOutOfRange},
		"a unit": {func(c *autopilotprefs.Config) { c.DailyCap.Unit = "euros" }, autopilotprefs.ErrUnknownUnit},
		"logs past the history": {func(c *autopilotprefs.Config) {
			c.HistoryDays, c.LogDays = 30, 31
		}, autopilotprefs.ErrLogsOutlastHistory},
	} {
		cfg := autopilotprefs.Defaults()
		c.change(&cfg)
		require.ErrorIs(t, cfg.Check(), c.want, name)
	}
}

func TestCheckTakesEveryEndOfEachRange(t *testing.T) {
	t.Parallel()

	low := autopilotprefs.Config{
		DefaultMode: autopilotprefs.ModeAuto, AgentsAtOnce: autopilotprefs.MinAgentsAtOnce,
		RunsPerDay: autopilotprefs.MinRunsPerDay, ActionsPerDay: autopilotprefs.MinActionsPerDay,
		ActionsPerHour: autopilotprefs.MinActionsPerHour,
		DailyCap:       autopilotprefs.DailyCap{Amount: autopilotprefs.MinCapUSD, Unit: autopilotprefs.UnitUSD},
		CatchUpHours:   autopilotprefs.MinCatchUpHours, HistoryDays: autopilotprefs.MinHistoryDays,
		LogDays: autopilotprefs.MinLogDays,
	}
	high := autopilotprefs.Config{
		DefaultMode: autopilotprefs.ModeAsk, AgentsAtOnce: autopilotprefs.MaxAgentsAtOnce,
		RunsPerDay: autopilotprefs.MaxRunsPerDay, ActionsPerDay: autopilotprefs.MaxActionsPerDay,
		ActionsPerHour: autopilotprefs.MaxActionsPerHour,
		DailyCap:       autopilotprefs.DailyCap{Amount: autopilotprefs.MaxCapTokens, Unit: autopilotprefs.UnitTokens},
		CatchUpHours:   autopilotprefs.MaxCatchUpHours, HistoryDays: autopilotprefs.MaxHistoryDays,
		LogDays: autopilotprefs.MaxLogDays,
	}

	require.NoError(t, low.Check())
	require.NoError(t, high.Check())
	assert.Equal(t, low, autopilotprefs.Decode(low.Encode()))
	assert.Equal(t, high, autopilotprefs.Decode(high.Encode()))
}

func TestWritesStoreOnlyWhatDiffers(t *testing.T) {
	t.Parallel()

	c := autopilotprefs.Defaults()
	c.HistoryDays, c.LogDays = 14, 14

	assert.Equal(t, map[string]string{
		autopilotprefs.KeyHistoryDays: "14", autopilotprefs.KeyLogDays: "14",
	}, autopilotprefs.Writes(nil, c), "a default never stored stays unstored")
	assert.Empty(t, autopilotprefs.Writes(nil, autopilotprefs.Defaults()))

	// Logs read as the history's 10 days, though the key holds nothing: raising the
	// history keeps them at the 10 days shown.
	stored := map[string]string{autopilotprefs.KeyHistoryDays: "10"}
	shown := autopilotprefs.Decode(stored)
	require.Equal(t, 10, shown.LogDays)

	shown.HistoryDays = 90
	assert.Equal(t, map[string]string{
		autopilotprefs.KeyHistoryDays: "90", autopilotprefs.KeyLogDays: "10",
	}, autopilotprefs.Writes(stored, shown))

	// A value stored as its default is written again when it changes, and left when not.
	stored = map[string]string{autopilotprefs.KeyAgentsAtOnce: "2"}
	again := autopilotprefs.Decode(stored)
	assert.Empty(t, autopilotprefs.Writes(stored, again))

	again.AgentsAtOnce = 1
	assert.Equal(t, map[string]string{autopilotprefs.KeyAgentsAtOnce: "1"}, autopilotprefs.Writes(stored, again))
}

// keysOf returns m's keys.
func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	return out
}
