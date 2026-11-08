package clock_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/clock"
)

// fixed is a Clock stopped at one time.
type fixed time.Time

func (f fixed) Now() time.Time { return time.Time(f) }

func TestSystemTellsTheTime(t *testing.T) {
	t.Parallel()

	before := time.Now()
	got := clock.System{}.Now()

	assert.False(t, got.Before(before))
	assert.WithinDuration(t, time.Now(), got, time.Minute)
}

func TestOr(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 3, 14, 15, 9, 26, 0, time.UTC)

	assert.Equal(t, clock.System{}, clock.Or(nil))
	assert.Equal(t, at, clock.Or(fixed(at)).Now())
}

func TestFormatAndStamp(t *testing.T) {
	t.Parallel()

	east := time.FixedZone("UTC+2", 2*60*60)
	at := time.Date(2026, 3, 14, 17, 9, 26, 500, east)

	assert.Equal(t, "2026-03-14T15:09:26Z", clock.Format(at), "always UTC, to the second")
	assert.Equal(t, "20260314T150926Z", clock.Stamp(at))

	parsed, err := time.Parse(clock.StampLayout, clock.Stamp(at))
	require.NoError(t, err)
	assert.True(t, parsed.Equal(at.Truncate(time.Second)))
}
