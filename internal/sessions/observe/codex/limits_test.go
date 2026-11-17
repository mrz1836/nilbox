package codex

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// limitsLine is a rollout's token count with its rate limits, as S6's rollout recorded them
// (codex-cli 0.163.0): the plan's weekly window, and which limit was reached, or null.
func limitsLine(used, reached string) string {
	return `{"timestamp":"2026-11-16T22:28:09.326Z","type":"event_msg","payload":{"type":"token_count","info":null,` +
		`"rate_limits":{"limit_id":"codex","primary":{"used_percent":` + used + `,"window_minutes":10080,` +
		`"resets_at":1794984399},"secondary":{"used_percent":12.0,"window_minutes":300,"resets_at":1794870000},` +
		`"credits":{"has_credits":true,"unlimited":false,"balance":"0.0"},"rate_limit_reached_type":` + reached + `}}}`
}

func TestLimitsReadsTheLastRateLimitsOfTheThread(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	writeRollout(t, filepath.Join(home, ".codex"), limitsLine("4.0", "null"), limitsLine("100.0", `"rate_limit_reached"`))

	got, ok := Limits{Home: home}.Limit("", wordsThread)
	assert.True(t, ok)
	assert.Equal(t, Limit{Reached: true, Window: "seven_day", ResetsAt: 1794984399}, got,
		"the last token count: the weekly window, reached, and when it resets")

	work := t.TempDir()
	writeRollout(t, work, limitsLine("4.0", "null"))

	got, ok = Limits{Home: home}.Limit(work, wordsThread)
	assert.True(t, ok, "a profile's own home")
	assert.False(t, got.Reached)

	_, ok = Limits{Home: home}.Limit("", "01a1e66a-0000-7000-8000-000000000000")
	assert.False(t, ok, "a thread with no rollout")
}

func TestRateLimitsReadsTheReachedWindow(t *testing.T) {
	t.Parallel()

	line := `{"type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":40,` +
		`"window_minutes":10080,"resets_at":2},"secondary":{"used_percent":100,"window_minutes":300,"resets_at":1},` +
		`"rate_limit_reached_type":"rate_limit_reached"}}}`

	got, ok := rateLimits([]byte(line))
	assert.True(t, ok)
	assert.Equal(t, Limit{Reached: true, Window: "five_hour", ResetsAt: 1}, got, "the window that is full")

	for _, bad := range []string{`{"type":"event_msg","payload":{"type":"agent_message","rate_limits":{}}}`, "rate_limits", ""} {
		_, ok = rateLimits([]byte(bad))
		assert.False(t, ok, bad)
	}

	assert.Empty(t, windowName(60))
}
