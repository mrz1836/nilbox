package autopilot

import (
	"context"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/draft"
	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// A profile out of usage rests. When the provider refuses a background run for its
// profile's usage limit (its runner tells RunOutOfUsage, with the limit's type and when it
// resets), the engine records the agent's profile as resting until then, in its own state,
// so it outlives a restart; the run counts as no try, and its step goes back to the queue.
// Every start of an agent step hands its runner the profiles resting now (Job.Resting), and
// the runner works through the step's chain in order (its profile as a click resolves it,
// then each fallback, which may be another agent's) and starts the first that isn't
// resting and can run here. A fallback whose agent can't run in the background yet is
// passed over, as is one this Mac lacks; with nothing left, the start waits, with when the
// soonest rest ends (Result.RetryAt), and is tried again then, asking GitHub nothing in
// between.

// defaultRest is how long a profile rests when its limit doesn't say when it resets.
const defaultRest = time.Hour

// UsageLimit is the provider's usage limit that refused a run, as its runner read it.
type UsageLimit struct {
	// Type is which window it is ("five_hour", "seven_day", ...), "" when not said.
	Type string

	// ResetsAt is when it resets, zero when not said.
	ResetsAt time.Time
}

// Resting is one agent's profile out of usage, resting until its limit resets.
type Resting struct {
	Agent   string    `json:"agent"`
	Profile string    `json:"profile"`
	Until   time.Time `json:"until"`
}

// restKey names an agent's profile among those resting: "claude/work".
func restKey(agent, profile string) string {
	return strings.ToLower(agent) + "/" + strings.ToLower(profile)
}

// rest records an agent's profile as resting until its limit resets, or for defaultRest
// when the limit doesn't say, keeping a later rest already recorded.
func (e *Engine) rest(ctx context.Context, run queries.AutopilotRun, limit *UsageLimit, now time.Time) {
	if run.Agent == "" {
		return
	}

	until := now.Add(defaultRest)
	if limit != nil && limit.ResetsAt.After(now) {
		until = limit.ResetsAt
	}

	key := restKey(run.Agent, run.Profile)

	if e.state.Resting == nil {
		e.state.Resting = map[string]Resting{}
	}

	if have, ok := e.state.Resting[key]; ok && have.Until.After(until) {
		return
	}

	e.state.Resting[key] = Resting{Agent: run.Agent, Profile: run.Profile, Until: until.UTC()}
	e.saveState(ctx)

	e.logger.InfoContext(ctx, "Autopilot rests a profile out of usage", "agent", run.Agent, "until", until.UTC())
}

// restingNow returns the profiles resting at now, in a stable order, and forgets those
// whose rest has ended.
func (e *Engine) restingNow(now time.Time) []Resting {
	out := []Resting{}

	for _, key := range slices.Sorted(maps.Keys(e.state.Resting)) {
		r := e.state.Resting[key]
		if !r.Until.After(now) {
			delete(e.state.Resting, key)

			continue
		}

		out = append(out, r)
	}

	return out
}

// RestingWords says which profiles rest, and until when on this Mac's clock, for a start
// that waits on them: "the default profile is out of usage until Mon 15:04".
func RestingWords(resting []Resting) string {
	parts := make([]string, 0, len(resting))

	for _, r := range resting {
		name := r.Profile
		if name == "" {
			name = "default"
		}

		// As the runner words a limit's reset: on this Mac's clock.
		at := time.Unix(r.Until.Unix(), 0).Format("Mon 15:04")
		parts = append(parts, "the "+name+" profile is out of usage until "+at)
	}

	return draft.Series(parts)
}

// outOfUsageWords says a run was refused for its profile's usage limit, and that the
// routine tries its next profile, or waits for the rest to end.
func outOfUsageWords(end RunEnd) string {
	return textutil.Sentence(firstOf(end.Error, end.Summary, "Out of usage")) +
		" Its profile rests until its limit resets; the step starts again on the next profile it may use."
}
