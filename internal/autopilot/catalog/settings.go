package catalog

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/mrz1836/nilbox/internal/textutil"
)

// The agent step's ranges. A dollar budget is held in cents, so the builder's whole
// numbers meet the spec's dollars exactly.
const (
	MinRunCents, MaxRunCents, DefaultRunCents                   = 50, 5000, 300
	MinRunTokens, MaxRunTokens                                  = 10_000, 50_000_000
	MinTimeoutMinutes, MaxTimeoutMinutes, DefaultTimeoutMinutes = 5, 180, 30

	// MaxFallbacks caps the profiles an agent step falls back to when one is out of
	// usage.
	MaxFallbacks = 3

	// DefaultPreset and DefaultWhere are the agent step's tools and place.
	DefaultPreset = PresetEditTestPush
	DefaultWhere  = WhereBackground

	// Inherit is the agent step's agent when it takes the one a click would.
	Inherit = "inherit"
)

// The other steps' ranges.
const (
	MinSnoozeDays, MaxSnoozeDays, DefaultSnoozeDays    = 1, 365, 3
	MinGiveUpHours, MaxGiveUpHours, DefaultGiveUpHours = 1, 24, 2
)

// The limits' ranges.
const (
	MinTries, MaxTries, DefaultTries                   = 1, 10, 2
	MinRunsPerDay, MaxRunsPerDay, DefaultRunsPerDay    = 1, 100, 10
	MinWaitMinutes, MaxWaitMinutes, DefaultWaitMinutes = 0, 24 * 60, 30
	MinDayCents, MaxDayCents                           = 100, 50_000
	MinDayTokens, MaxDayTokens                         = 100_000, 50_000_000
)

// Setting describes one of a step's or the limits' settings for the builder.
type Setting struct {
	// ID is the spec's key for it.
	ID string `json:"id"`

	Label string `json:"label"`
	Help  string `json:"help"`

	// Default says the default in words.
	Default string `json:"default"`

	// Range is a whole-number setting's bounds, nil for any other.
	Range *Range `json:"range"`

	// Choices are a setting's choices, empty for one that has none.
	Choices []Choice `json:"choices"`
}

// AgentSettings returns the agent step's settings, in the builder's order.
func AgentSettings() []Setting {
	return []Setting{
		{ID: "prompt", Label: "Prompt", Default: "the template's", Help: "A library prompt for the item's kind."},
		{
			ID: "where", Label: "Where", Default: "In the background",
			Help: "A terminal run is exactly a click's launch, and defaults its routine to Ask first.",
			Choices: []Choice{
				{Value: string(WhereBackground), Label: "In the background"},
				{Value: string(WhereTerminal), Label: "In my terminal"},
			},
		},
		{ID: "agent", Label: "Agent", Default: "Inherit", Help: "The launch defaults' agent, as a click."},
		{ID: "profile", Label: "Profile", Default: "Inherit", Help: "The launch defaults' profile, as a click."},
		{
			ID: "fallbacks", Label: "Fallbacks", Default: "None",
			Help:  "Profiles to use when one is out of usage, another agent's included.",
			Range: &Range{Min: 0, Max: MaxFallbacks, Unit: "profiles"},
		},
		{ID: "model", Label: "Model", Default: "Inherit"},
		{ID: "effort", Label: "Effort", Default: "Inherit"},
		{ID: "planMode", Label: "Plan mode", Default: "Inherit", Help: "For terminal runs; in the background, Tools decides."},
		{ID: "worktree", Label: "Worktree", Default: "Inherit", Help: "For terminal runs; in the background, always on."},
		{
			ID: "launchFor", Label: "Launch settings per org or repository", Default: "None",
			Help: "Rows that override the agent, profile, model, and effort for one org or repository.",
		},
		{
			ID: "budget", Label: "Budget per run", Default: "$3",
			Help:  "Dollars, for an agent that enforces one, or tokens, for any agent whose stream reports usage.",
			Range: &Range{Min: MinRunCents, Max: MaxRunCents, Default: DefaultRunCents, Unit: "cents"},
		},
		{
			ID: "timeoutMinutes", Label: "Time limit", Default: "30 min",
			Range: &Range{Min: MinTimeoutMinutes, Max: MaxTimeoutMinutes, Default: DefaultTimeoutMinutes, Unit: "minutes"},
		},
		{ID: "tools", Label: "Tools", Default: "Edit, test, and push", Choices: presetChoices()},
		{ID: "allow", Label: "Extra commands to allow", Default: "None", Help: "Never loosens the fence's hard lines."},
		{ID: "deny", Label: "Extra commands to deny", Default: "None"},
	}
}

// Limits returns the routine's limits, in the builder's order.
func Limits() []Setting {
	return []Setting{
		{
			ID: "oncePer", Label: "Once per", Default: "The trigger's", Choices: []Choice{
				{Value: string(OncePerItem), Label: "item"},
				{Value: string(OncePerHeadCommit), Label: "head commit"},
				{Value: string(OncePerDay), Label: "day"},
			},
		},
		{
			ID: "triesPerItem", Label: "Tries per item", Default: "2",
			Range: &Range{Min: MinTries, Max: MaxTries, Default: DefaultTries, Unit: "tries"},
		},
		{
			ID: "runsPerDay", Label: "Runs a day", Default: "10",
			Range: &Range{Min: MinRunsPerDay, Max: MaxRunsPerDay, Default: DefaultRunsPerDay, Unit: "runs"},
		},
		{
			ID: "waitMinutes", Label: "Wait between tries on one item", Default: "30 min",
			Range: &Range{Min: MinWaitMinutes, Max: MaxWaitMinutes, Default: DefaultWaitMinutes, Unit: "minutes"},
		},
		{ID: "activeHours", Label: "Active hours", Default: "Always"},
		{
			ID: "budgetPerDay", Label: "Daily budget", Default: "Off",
			Help:  "In dollars or in tokens; Settings → Autopilot's daily cap still holds beside it.",
			Range: &Range{Min: MinDayCents, Max: MaxDayCents, Unit: "cents"},
		},
	}
}

// presetChoices are the tools presets as a setting's choices.
func presetChoices() []Choice {
	presets := Presets()
	out := make([]Choice, 0, len(presets))

	for _, p := range presets {
		out = append(out, Choice{Value: string(p.ID), Label: p.Label})
	}

	return out
}

// ErrInvalidLength is returned for a length that isn't a whole number of minutes,
// hours, or days.
var ErrInvalidLength = errors.New(`write a length as a number and m, h, or d, such as "30m", "2h", or "14d"`)

// maxLengthDigits caps a length's number, so it always fits.
const maxLengthDigits = 5

// ParseLength reads a duration condition's length: a whole number of minutes, hours, or
// days, such as "30m", "2h", or "14d", from 1 up.
func ParseLength(s string) (time.Duration, error) {
	if len(s) < 2 || len(s) > maxLengthDigits+1 {
		return 0, fmt.Errorf("%w: %q", ErrInvalidLength, s)
	}

	unit, ok := map[byte]time.Duration{'m': time.Minute, 'h': time.Hour, 'd': 24 * time.Hour}[s[len(s)-1]]
	if !ok {
		return 0, fmt.Errorf("%w: %q", ErrInvalidLength, s)
	}

	digits := s[:len(s)-1]

	n, err := strconv.Atoi(digits)
	if err != nil || n < 1 || digits[0] == '0' || digits[0] == '+' || digits[0] == '-' {
		return 0, fmt.Errorf("%w: %q", ErrInvalidLength, s)
	}

	return time.Duration(n) * unit, nil
}

// Catalog is everything the builder renders, in one value.
type Catalog struct {
	Triggers      []Trigger        `json:"triggers"`
	Groups        []GroupInfo      `json:"groups"`
	Fields        []Field          `json:"fields"`
	Ops           []OpInfo         `json:"ops"`
	Lists         []ListSourceInfo `json:"lists"`
	Steps         []Step           `json:"steps"`
	GitHubActions []GitHubAction   `json:"githubActions"`
	Outcomes      []OutcomeInfo    `json:"outcomes"`
	Presets       []PresetInfo     `json:"presets"`
	AgentSettings []Setting        `json:"agentSettings"`
	Limits        []Setting        `json:"limits"`
}

// All returns the whole catalog, with every list empty rather than nil, so the window
// gets an array for each.
func All() Catalog {
	c := Catalog{
		Triggers: Triggers(), Groups: Groups(), Fields: Fields(), Ops: Ops(), Lists: ListSources(), Steps: Steps(),
		GitHubActions: GitHubActions(), Outcomes: Outcomes(), Presets: Presets(), AgentSettings: AgentSettings(),
		Limits: Limits(),
	}

	for i := range c.Triggers {
		c.Triggers[i].Options = textutil.NonNil(c.Triggers[i].Options)
	}

	for i := range c.Fields {
		c.Fields[i].Choices = textutil.NonNil(c.Fields[i].Choices)
	}

	for _, settings := range [][]Setting{c.AgentSettings, c.Limits} {
		for i := range settings {
			settings[i].Choices = textutil.NonNil(settings[i].Choices)
		}
	}

	return c
}
