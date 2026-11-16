// Package autopilotprefs declares Autopilot's settings: Autopilot (beta), the switch that
// turns all of it on, off on every install until the user turns it on; Pause Autopilot,
// the mode new routines start in, how many agents run at once, the daily limits and the daily cap,
// keeping the Mac awake, holding agent starts during an outage, when to ask before
// catching up, and how long the history and the run logs are kept. The settings package
// writes them, under the keys declared here, and Autopilot reads them. It also declares a
// routine's modes, which the settings' default and each routine's own share.
//
// It is a leaf: it imports only prefval, never the store or a service, so settings writes
// the keys without depending on Autopilot, nor Autopilot on settings. Autopilot's engine
// keeps its own state under a key of its own, which is no setting.
package autopilotprefs

import (
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/mrz1836/nilbox/internal/prefs/prefval"
)

// The preferences the settings are stored under. Settings writes them.
const (
	// KeyEnabled is Autopilot (beta): off, the default, the window shows none of Autopilot
	// and its engine doesn't run, so no routine sends a search or starts anything.
	KeyEnabled = "autopilot.enabled"

	// KeyPaused is Pause Autopilot: on, no routine starts anything new.
	KeyPaused = "autopilot.paused"

	// KeyDefaultMode is the mode a new routine starts in.
	KeyDefaultMode = "autopilot.defaultMode"

	// KeyAgentsAtOnce is how many agents Autopilot runs at once.
	KeyAgentsAtOnce = "autopilot.agentsAtOnce"

	// KeyRunsPerDay is how many agent runs Autopilot starts a day, across its routines.
	KeyRunsPerDay = "autopilot.runsPerDay"

	// KeyActionsPerDay is how many GitHub actions Autopilot sends a day.
	KeyActionsPerDay = "autopilot.actionsPerDay"

	// KeyDailyCap is the daily cap's amount, 0 for none, in KeyDailyCapUnit's unit.
	KeyDailyCap = "autopilot.dailyCap"

	// KeyDailyCapUnit is the daily cap's unit: dollars or tokens.
	KeyDailyCapUnit = "autopilot.dailyCapUnit"

	// KeyKeepAwake keeps the Mac awake while agents work.
	KeyKeepAwake = "autopilot.keepAwake"

	// KeyHoldOnOutage holds agent starts while the agent's provider reports an outage.
	KeyHoldOnOutage = "autopilot.holdOnOutage"

	// KeyCatchUpHours is how long a gap makes Autopilot ask before it catches up.
	KeyCatchUpHours = "autopilot.catchUpHours"

	// KeyHistoryDays is how many days of history Autopilot keeps.
	KeyHistoryDays = "autopilot.historyDays"

	// KeyLogDays is how many days the agent runs' logs are kept, never more than the
	// history.
	KeyLogDays = "autopilot.logDays"
)

var (
	// ErrUnknownMode is returned for a mode that is not one of the modes.
	ErrUnknownMode = errors.New("unknown Autopilot mode")

	// ErrUnknownUnit is returned for a daily cap's unit that is not one of the units.
	ErrUnknownUnit = errors.New("unknown unit for the daily cap")

	// ErrOutOfRange is returned for a number outside its setting's range.
	ErrOutOfRange = errors.New("out of range")

	// ErrLogsOutlastHistory is returned for run logs kept longer than the history.
	ErrLogsOutlastHistory = errors.New("the run logs can't be kept longer than the history")
)

// Mode is how a routine acts on a match.
type Mode string

// The modes, from the most careful.
const (
	// ModeDryRun records what the routine would have done, and does nothing else.
	ModeDryRun Mode = "dryRun"

	// ModeAsk runs the local steps at once and waits for the user's OK before the first
	// agent, GitHub, or script step: Ask first.
	ModeAsk Mode = "ask"

	// ModeAuto runs every step at once, within the limits: Autopilot.
	ModeAuto Mode = "auto"
)

// DefaultMode is the mode new routines start in on a fresh database: Ask first.
const DefaultMode = ModeAsk

// Modes returns every mode, from the most careful.
func Modes() []Mode {
	return []Mode{ModeDryRun, ModeAsk, ModeAuto}
}

// Valid reports whether m is one of the modes.
func (m Mode) Valid() bool {
	return slices.Contains(Modes(), m)
}

// Label is the name Settings and the routine list give m: "Dry run", "Ask first", or
// "Autopilot".
func (m Mode) Label() string {
	switch m {
	case ModeDryRun:
		return "Dry run"
	case ModeAsk:
		return "Ask first"
	case ModeAuto:
		return "Autopilot"
	default:
		return string(m)
	}
}

// Unit is what the daily cap counts.
type Unit string

// The units.
const (
	// UnitUSD counts dollars, as the agents report what a run cost.
	UnitUSD Unit = "usd"

	// UnitTokens counts tokens, for agents that report no cost.
	UnitTokens Unit = "tokens"
)

// Units returns every unit.
func Units() []Unit {
	return []Unit{UnitUSD, UnitTokens}
}

// Valid reports whether u is one of the units.
func (u Unit) Valid() bool {
	return slices.Contains(Units(), u)
}

// The settings' ranges, both ends included. A cap of 0, in either unit, is no cap.
const (
	// MaxAgentsAtOnce keeps one of the run engine's four slots free for the user's own
	// scripts.
	MinAgentsAtOnce, MaxAgentsAtOnce   = 1, 3
	MinRunsPerDay, MaxRunsPerDay       = 1, 200
	MinActionsPerDay, MaxActionsPerDay = 1, 200
	MinCatchUpHours, MaxCatchUpHours   = 1, 168
	MinHistoryDays, MaxHistoryDays     = 7, 365
	MinLogDays, MaxLogDays             = 1, 90

	// A dollar cap is whole dollars, and a token cap tokens.
	MinCapUSD, MaxCapUSD       = 1, 500
	MinCapTokens, MaxCapTokens = 100_000, 50_000_000
)

// bounds is a whole-number setting's range, both ends included.
type bounds struct {
	low, high int
}

// holds reports whether n is within b.
func (b bounds) holds(n int) bool {
	return n >= b.low && n <= b.high
}

// The defaults of a fresh database.
const (
	DefaultAgentsAtOnce  = 2
	DefaultRunsPerDay    = 20
	DefaultActionsPerDay = 50
	DefaultCatchUpHours  = 24
	DefaultHistoryDays   = 90
	DefaultLogDays       = 30
	DefaultHoldOnOutage  = true
	DefaultDailyCapUnit  = UnitUSD
)

// DailyCap is the most Autopilot's agents may spend a day, across its routines: an amount
// in a unit, or no cap when the amount is 0.
type DailyCap struct {
	// Amount is whole dollars or tokens, 0 for no cap.
	Amount int `json:"amount"`

	// Unit is what Amount counts.
	Unit Unit `json:"unit"`
}

// Off reports a cap of none.
func (c DailyCap) Off() bool {
	return c.Amount == 0
}

// capBounds returns the range of a cap's amount in unit, 0 aside.
func capBounds(unit Unit) bounds {
	if unit == UnitTokens {
		return bounds{MinCapTokens, MaxCapTokens}
	}

	return bounds{MinCapUSD, MaxCapUSD}
}

// Check refuses a unit that is not one of the units, and an amount outside its unit's
// range but 0.
func (c DailyCap) Check() error {
	if !c.Unit.Valid() {
		return fmt.Errorf("%w %q", ErrUnknownUnit, c.Unit)
	}

	if b := capBounds(c.Unit); c.Amount != 0 && !b.holds(c.Amount) {
		return fmt.Errorf("the daily cap is %w: %d %s; choose 0 or %d to %d", ErrOutOfRange, c.Amount, c.Unit,
			b.low, b.high)
	}

	return nil
}

// Config is Autopilot's settings, as Settings shows them.
type Config struct {
	// Enabled is Autopilot (beta): off, Autopilot is hidden and its engine doesn't run.
	Enabled bool `json:"enabled"`

	// Paused is Pause Autopilot: no routine starts anything new; running agents carry on.
	Paused bool `json:"paused"`

	// DefaultMode is the mode new routines start in.
	DefaultMode Mode `json:"defaultMode"`

	// AgentsAtOnce is how many agents Autopilot runs at once.
	AgentsAtOnce int `json:"agentsAtOnce"`

	// RunsPerDay is how many agent runs it starts a day, and ActionsPerDay how many
	// GitHub actions it sends a day, across every routine.
	RunsPerDay    int `json:"runsPerDay"`
	ActionsPerDay int `json:"actionsPerDay"`

	// DailyCap is the most its agents may spend a day.
	DailyCap DailyCap `json:"dailyCap"`

	// KeepAwake keeps the Mac awake while agents work.
	KeepAwake bool `json:"keepAwake"`

	// HoldOnOutage holds agent starts while the agent's provider reports an outage.
	HoldOnOutage bool `json:"holdOnOutage"`

	// CatchUpHours is how long a gap makes Autopilot ask before it catches up.
	CatchUpHours int `json:"catchUpHours"`

	// HistoryDays is how long the history is kept, and LogDays the agent runs' logs, never
	// longer than the history.
	HistoryDays int `json:"historyDays"`
	LogDays     int `json:"logDays"`
}

// Defaults returns the settings of a fresh database.
func Defaults() Config {
	return Config{
		DefaultMode:   DefaultMode,
		AgentsAtOnce:  DefaultAgentsAtOnce,
		RunsPerDay:    DefaultRunsPerDay,
		ActionsPerDay: DefaultActionsPerDay,
		DailyCap:      DailyCap{Unit: DefaultDailyCapUnit},
		HoldOnOutage:  DefaultHoldOnOutage,
		CatchUpHours:  DefaultCatchUpHours,
		HistoryDays:   DefaultHistoryDays,
		LogDays:       DefaultLogDays,
	}
}

// Keys returns every key the settings are stored under.
func Keys() []string {
	return []string{
		KeyEnabled, KeyPaused, KeyDefaultMode, KeyAgentsAtOnce, KeyRunsPerDay, KeyActionsPerDay, KeyDailyCap, KeyDailyCapUnit,
		KeyKeepAwake, KeyHoldOnOutage, KeyCatchUpHours, KeyHistoryDays, KeyLogDays,
	}
}

// Decode reads the stored settings. Each one missing, or holding a value its setter would
// refuse, reads as its default; a cap whose amount doesn't fit its unit reads as none, and
// run logs kept longer than the history as kept as long as it.
func Decode(stored map[string]string) Config {
	d := Defaults()

	out := Config{
		Enabled:       prefval.Bool(stored[KeyEnabled], false),
		Paused:        prefval.Bool(stored[KeyPaused], false),
		DefaultMode:   d.DefaultMode,
		AgentsAtOnce:  number(stored[KeyAgentsAtOnce], bounds{MinAgentsAtOnce, MaxAgentsAtOnce}, d.AgentsAtOnce),
		RunsPerDay:    number(stored[KeyRunsPerDay], bounds{MinRunsPerDay, MaxRunsPerDay}, d.RunsPerDay),
		ActionsPerDay: number(stored[KeyActionsPerDay], bounds{MinActionsPerDay, MaxActionsPerDay}, d.ActionsPerDay),
		DailyCap:      decodeCap(stored[KeyDailyCap], stored[KeyDailyCapUnit]),
		KeepAwake:     prefval.Bool(stored[KeyKeepAwake], false),
		HoldOnOutage:  prefval.Bool(stored[KeyHoldOnOutage], d.HoldOnOutage),
		CatchUpHours:  number(stored[KeyCatchUpHours], bounds{MinCatchUpHours, MaxCatchUpHours}, d.CatchUpHours),
		HistoryDays:   number(stored[KeyHistoryDays], bounds{MinHistoryDays, MaxHistoryDays}, d.HistoryDays),
		LogDays:       number(stored[KeyLogDays], bounds{MinLogDays, MaxLogDays}, d.LogDays),
	}

	if m := Mode(stored[KeyDefaultMode]); m.Valid() {
		out.DefaultMode = m
	}

	out.LogDays = min(out.LogDays, out.HistoryDays)

	return out
}

// number reads a whole number within b, or fallback.
func number(stored string, b bounds, fallback int) int {
	if n, err := strconv.Atoi(stored); err == nil && b.holds(n) {
		return n
	}

	return fallback
}

// decodeCap reads the daily cap: its unit, or the default's, and its amount, or none.
func decodeCap(amount, unit string) DailyCap {
	out := DailyCap{Unit: Unit(unit)}
	if !out.Unit.Valid() {
		out.Unit = DefaultDailyCapUnit
	}

	if n, err := strconv.Atoi(amount); err == nil && (n == 0 || capBounds(out.Unit).holds(n)) {
		out.Amount = n
	}

	return out
}

// Check refuses settings a setter would refuse: a mode or a unit that isn't one of the
// choices, a number outside its range, and run logs kept longer than the history. It
// names the first problem.
func (c Config) Check() error {
	if !c.DefaultMode.Valid() {
		return fmt.Errorf("%w %q", ErrUnknownMode, c.DefaultMode)
	}

	for _, n := range []struct {
		what  string
		value int
		b     bounds
	}{
		{"agents at once", c.AgentsAtOnce, bounds{MinAgentsAtOnce, MaxAgentsAtOnce}},
		{"agent runs a day", c.RunsPerDay, bounds{MinRunsPerDay, MaxRunsPerDay}},
		{"GitHub actions a day", c.ActionsPerDay, bounds{MinActionsPerDay, MaxActionsPerDay}},
		{"hours before catching up", c.CatchUpHours, bounds{MinCatchUpHours, MaxCatchUpHours}},
		{"days of history", c.HistoryDays, bounds{MinHistoryDays, MaxHistoryDays}},
		{"days of run logs", c.LogDays, bounds{MinLogDays, MaxLogDays}},
	} {
		if !n.b.holds(n.value) {
			return fmt.Errorf("%s is %w: %d; choose %d to %d", n.what, ErrOutOfRange, n.value, n.b.low, n.b.high)
		}
	}

	if err := c.DailyCap.Check(); err != nil {
		return err
	}

	if c.LogDays > c.HistoryDays {
		return fmt.Errorf("%w: %d days of logs, %d of history", ErrLogsOutlastHistory, c.LogDays, c.HistoryDays)
	}

	return nil
}

// Encode returns every setting's stored text, by its key.
func (c Config) Encode() map[string]string {
	return map[string]string{
		KeyEnabled:       prefval.EncodeBool(c.Enabled),
		KeyPaused:        prefval.EncodeBool(c.Paused),
		KeyDefaultMode:   string(c.DefaultMode),
		KeyAgentsAtOnce:  strconv.Itoa(c.AgentsAtOnce),
		KeyRunsPerDay:    strconv.Itoa(c.RunsPerDay),
		KeyActionsPerDay: strconv.Itoa(c.ActionsPerDay),
		KeyDailyCap:      strconv.Itoa(c.DailyCap.Amount),
		KeyDailyCapUnit:  string(c.DailyCap.Unit),
		KeyKeepAwake:     prefval.EncodeBool(c.KeepAwake),
		KeyHoldOnOutage:  prefval.EncodeBool(c.HoldOnOutage),
		KeyCatchUpHours:  strconv.Itoa(c.CatchUpHours),
		KeyHistoryDays:   strconv.Itoa(c.HistoryDays),
		KeyLogDays:       strconv.Itoa(c.LogDays),
	}
}

// Writes returns what storing c over stored, the settings as they are stored, writes: each
// key whose text differs from what is stored, but a key stored nowhere whose value is its
// default, so a setting the user never changed keeps following its default.
func Writes(stored map[string]string, c Config) map[string]string {
	defaults := Defaults().Encode()
	out := map[string]string{}

	for key, value := range c.Encode() {
		was, ok := stored[key]

		switch {
		case ok && was != value:
			out[key] = value
		case !ok && value != defaults[key]:
			out[key] = value
		}
	}

	return out
}
