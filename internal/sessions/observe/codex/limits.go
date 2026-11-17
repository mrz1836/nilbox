package codex

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"slices"

	"github.com/mrz1836/nilbox/internal/paths"
)

// limitsTailBytes is how much of a rollout file's end is read for its last rate limits.
const limitsTailBytes = 4 << 20

// Limit is the provider's usage limit as a Codex thread's rollout last recorded it: Codex's
// --json stream says only that a turn was refused, while each token count in the rollout
// carries the plan's windows (how much of each is used, how long it is, and when it
// resets) and, once one is reached, rate_limit_reached_type.
type Limit struct {
	// Reached reports the rollout saying a limit was reached.
	Reached bool

	// Window is the reached window in the words the runner names limits by: "five_hour",
	// "seven_day", or "" for another length; ResetsAt is when it resets, in Unix seconds, 0
	// when not said.
	Window   string
	ResetsAt int64
}

// Limits reads a thread's usage limit from its rollout file.
type Limits struct {
	// Home is the user's home directory, whose .codex is the default Codex home.
	Home string
}

// Limit returns the usage limit the rollout of the thread threadID, in the Codex home
// configDir ("" for the default one), last recorded, and whether it holds one.
func (l Limits) Limit(configDir, threadID string) (Limit, bool) {
	home := configDir
	if home == "" {
		home = filepath.Join(l.Home, codexInfo().ConfigDir)
	}

	path := findRollout(filepath.Join(home, rolloutsDir), threadID)
	if path == "" {
		return Limit{}, false
	}

	data, cut, err := paths.ReadTail(path, limitsTailBytes)
	if err != nil {
		return Limit{}, false
	}

	lines := bytes.Split(data, []byte("\n"))
	if cut && len(lines) > 0 {
		lines = lines[1:]
	}

	for _, line := range slices.Backward(lines) {
		if got, ok := rateLimits(line); ok {
			return got, true
		}
	}

	return Limit{}, false
}

// rateLimitsLine is one line of a rollout file, as far as its rate limits need it.
type rateLimitsLine struct {
	Type    string `json:"type"`
	Payload struct {
		Type       string `json:"type"`
		RateLimits *struct {
			Primary   *rateWindow `json:"primary"`
			Secondary *rateWindow `json:"secondary"`
			Reached   *string     `json:"rate_limit_reached_type"`
		} `json:"rate_limits"`
	} `json:"payload"`
}

// rateWindow is one of the plan's windows.
type rateWindow struct {
	UsedPercent   float64 `json:"used_percent"`
	WindowMinutes int64   `json:"window_minutes"`
	ResetsAt      int64   `json:"resets_at"`
}

// rateLimits returns the limit a rollout line's token count carries, and whether it is one
// that carries rate limits.
func rateLimits(line []byte) (Limit, bool) {
	if !bytes.Contains(line, []byte(`"rate_limits"`)) {
		return Limit{}, false
	}

	var l rateLimitsLine
	if json.Unmarshal(bytes.TrimSpace(line), &l) != nil || l.Type != "event_msg" || l.Payload.Type != "token_count" ||
		l.Payload.RateLimits == nil {
		return Limit{}, false
	}

	rl := l.Payload.RateLimits
	out := Limit{Reached: rl.Reached != nil && *rl.Reached != ""}

	if window := reachedWindow(rl.Primary, rl.Secondary); window != nil {
		out.Window, out.ResetsAt = windowName(window.WindowMinutes), window.ResetsAt
	}

	return out, true
}

// reachedWindow returns the window a limit is in: the secondary when it alone is full, else
// the primary, or whichever there is.
func reachedWindow(primary, secondary *rateWindow) *rateWindow {
	if secondary != nil && (primary == nil || secondary.UsedPercent >= 100 && primary.UsedPercent < 100) {
		return secondary
	}

	return primary
}

// windowName names a window by its length, as the runner names a plan's limits.
func windowName(minutes int64) string {
	switch minutes {
	case 5 * 60:
		return "five_hour"
	case 7 * 24 * 60:
		return "seven_day"
	default:
		return ""
	}
}
