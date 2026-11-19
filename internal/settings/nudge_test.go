package settings

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodeNudgeAfterHours(t *testing.T) {
	t.Parallel()

	for stored, want := range map[string]int{
		"0": 0, "1": 1, "12": 12, "24": 24, "168": 168, "720": 720,
		"": DefaultNudgeAfterHours, "-1": DefaultNudgeAfterHours, "721": DefaultNudgeAfterHours,
		"1.5": DefaultNudgeAfterHours, " 24": DefaultNudgeAfterHours, "soon": DefaultNudgeAfterHours,
	} {
		assert.Equal(t, want, decodeNudgeAfterHours(stored), "stored %q", stored)
	}
}

func TestNudgeSettingsStartAtTheirDefaults(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, DefaultNudgeAfterHours, got.NudgeAfterHours)
	assert.Equal(t, DefaultNudgeSnooze, got.NudgeSnooze)
}

func TestNudgeAfterHoursBoundsAreKept(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)

	for _, hours := range []int{0, 1, MaxNudgeAfterHours} {
		got, err := svc.SetNudgeAfterHours(t.Context(), hours)
		require.NoError(t, err)
		assert.Equal(t, hours, got.NudgeAfterHours)

		again, err := svc.Get(t.Context())
		require.NoError(t, err)
		assert.Equal(t, hours, again.NudgeAfterHours)
	}
}

func TestNudgeAfterHoursOutOfRangeIsRefused(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)

	_, err := svc.SetNudgeAfterHours(t.Context(), 48)
	require.NoError(t, err)

	for _, hours := range []int{-1, MaxNudgeAfterHours + 1} {
		_, err = svc.SetNudgeAfterHours(t.Context(), hours)
		require.ErrorIs(t, err, ErrInvalidNudgeAfterHours, "%d hours", hours)
	}

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 48, got.NudgeAfterHours, "a refused time stores nothing")
}

func TestSetNudgeSnooze(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)

	got, err := svc.SetNudgeSnooze(t.Context(), true)
	require.NoError(t, err)
	assert.True(t, got.NudgeSnooze)

	got, err = svc.SetNudgeSnooze(t.Context(), false)
	require.NoError(t, err)
	assert.False(t, got.NudgeSnooze)
}

func TestNudgeHoursWords(t *testing.T) {
	t.Parallel()

	for hours, want := range map[int]string{
		0: "off", 1: "1 hour", 12: "12 hours", 24: "1 day", 36: "36 hours", 48: "2 days", 168: "7 days",
	} {
		assert.Equal(t, want, nudgeHours(hours), "%d hours", hours)
	}
}
