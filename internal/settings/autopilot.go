package settings

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// Each setter of Autopilot's settings reads them and writes the change in the
// transaction that reads the settings back, so two quick changes never lose either, and
// emits an event.AutopilotChanged once it is stored; a change that is refused stores
// nothing and emits nothing. Only the keys that change are written, so a setting the user
// never touched keeps following its default.

// SetAutopilotEnabled turns Autopilot (beta) on or off, and returns the settings with it.
// Off, the window shows none of Autopilot and its engine doesn't run; the routines and
// their history stay as they are, and come back as they were once it is on again.
func (s *Service) SetAutopilotEnabled(ctx context.Context, on bool) (Settings, error) {
	return s.saveAutopilot(ctx, func(c *autopilotprefs.Config) { c.Enabled = on })
}

// SetAutopilotPaused turns Pause Autopilot on or off, and returns the settings with it.
// Paused, no routine starts anything new; agents already running carry on.
func (s *Service) SetAutopilotPaused(ctx context.Context, on bool) (Settings, error) {
	return s.saveAutopilot(ctx, func(c *autopilotprefs.Config) { c.Paused = on })
}

// SetAutopilotDefaultMode stores the mode new routines start in (Dry run, Ask first, or
// Autopilot), and returns the settings with it. A routine already made keeps its own.
func (s *Service) SetAutopilotDefaultMode(ctx context.Context, mode autopilotprefs.Mode) (Settings, error) {
	return s.saveAutopilot(ctx, func(c *autopilotprefs.Config) { c.DefaultMode = mode })
}

// SetAutopilotAgentsAtOnce stores how many agents Autopilot runs at once,
// autopilotprefs.MinAgentsAtOnce to MaxAgentsAtOnce, and returns the settings with it.
func (s *Service) SetAutopilotAgentsAtOnce(ctx context.Context, n int) (Settings, error) {
	return s.saveAutopilot(ctx, func(c *autopilotprefs.Config) { c.AgentsAtOnce = n })
}

// SetAutopilotRunsPerDay stores how many agent runs Autopilot starts a day, across every
// routine, autopilotprefs.MinRunsPerDay to MaxRunsPerDay, and returns the settings with it.
func (s *Service) SetAutopilotRunsPerDay(ctx context.Context, n int) (Settings, error) {
	return s.saveAutopilot(ctx, func(c *autopilotprefs.Config) { c.RunsPerDay = n })
}

// SetAutopilotActionsPerDay stores how many GitHub actions Autopilot sends a day, across
// every routine, autopilotprefs.MinActionsPerDay to MaxActionsPerDay, and returns the
// settings with it.
func (s *Service) SetAutopilotActionsPerDay(ctx context.Context, n int) (Settings, error) {
	return s.saveAutopilot(ctx, func(c *autopilotprefs.Config) { c.ActionsPerDay = n })
}

// SetAutopilotActionsPerHour stores how many GitHub actions Autopilot sends an hour, across
// every routine, autopilotprefs.MinActionsPerHour to MaxActionsPerHour, and returns the
// settings with it.
func (s *Service) SetAutopilotActionsPerHour(ctx context.Context, n int) (Settings, error) {
	return s.saveAutopilot(ctx, func(c *autopilotprefs.Config) { c.ActionsPerHour = n })
}

// SetAutopilotDailyCap stores the most Autopilot's agents may spend a day, and returns the
// settings with it: an amount of 0 for no cap, or whole dollars (autopilotprefs.MinCapUSD
// to MaxCapUSD) or tokens (MinCapTokens to MaxCapTokens), as its unit says.
func (s *Service) SetAutopilotDailyCap(ctx context.Context, dailyCap autopilotprefs.DailyCap) (Settings, error) {
	return s.saveAutopilot(ctx, func(c *autopilotprefs.Config) { c.DailyCap = dailyCap })
}

// SetAutopilotKeepAwake stores whether the Mac is kept awake while agents work, and
// returns the settings with it.
func (s *Service) SetAutopilotKeepAwake(ctx context.Context, on bool) (Settings, error) {
	return s.saveAutopilot(ctx, func(c *autopilotprefs.Config) { c.KeepAwake = on })
}

// SetAutopilotHoldOnOutage stores whether agent starts wait while the agent's provider
// reports an outage, and returns the settings with it.
func (s *Service) SetAutopilotHoldOnOutage(ctx context.Context, on bool) (Settings, error) {
	return s.saveAutopilot(ctx, func(c *autopilotprefs.Config) { c.HoldOnOutage = on })
}

// SetAutopilotCatchUpHours stores how long a gap makes Autopilot ask before it catches up,
// autopilotprefs.MinCatchUpHours to MaxCatchUpHours, and returns the settings with it.
func (s *Service) SetAutopilotCatchUpHours(ctx context.Context, hours int) (Settings, error) {
	return s.saveAutopilot(ctx, func(c *autopilotprefs.Config) { c.CatchUpHours = hours })
}

// SetAutopilotHistoryDays stores how many days of Autopilot's history are kept,
// autopilotprefs.MinHistoryDays to MaxHistoryDays, and returns the settings with it. Run
// logs kept longer than the new history are kept as long as it instead.
func (s *Service) SetAutopilotHistoryDays(ctx context.Context, days int) (Settings, error) {
	return s.saveAutopilot(ctx, func(c *autopilotprefs.Config) { c.HistoryDays, c.LogDays = days, min(c.LogDays, days) })
}

// SetAutopilotLogDays stores how many days the agent runs' logs are kept,
// autopilotprefs.MinLogDays to MaxLogDays and never more than the history, and returns the
// settings with it.
func (s *Service) SetAutopilotLogDays(ctx context.Context, days int) (Settings, error) {
	return s.saveAutopilot(ctx, func(c *autopilotprefs.Config) { c.LogDays = days })
}

// SetAutopilotMergePastRules turns Let routines merge your own pull requests past the rules
// on or off, and returns the settings with it. On, a routine whose Merge it allows it, and
// that the user accepted so, may merge their own signed pull request that lacks only an
// approval from someone else; off, none does, whatever its step allows. A backup never
// carries it.
func (s *Service) SetAutopilotMergePastRules(ctx context.Context, on bool) (Settings, error) {
	return s.saveAutopilot(ctx, func(c *autopilotprefs.Config) { c.MergePastRules = on })
}

// saveAutopilot changes Autopilot's settings with change, inside the transaction that
// reads the settings back: it reads them, refuses what their check refuses, worded for the
// window, writes only the keys that change, and emits an event.AutopilotChanged once it is
// stored.
func (s *Service) saveAutopilot(ctx context.Context, change func(*autopilotprefs.Config)) (Settings, error) {
	saved, err := s.save(ctx, func(ctx context.Context, p *prefs.Store) error {
		stored, err := p.GetMany(ctx, autopilotprefs.Keys()...)
		if err != nil {
			return fmt.Errorf("read Autopilot's settings: %w", err)
		}

		c := autopilotprefs.Decode(stored)
		change(&c)

		writes, err := checkAutopilot(stored, c)
		if err != nil {
			return err
		}

		if err = p.SetMany(ctx, writes); err != nil {
			return fmt.Errorf("save Autopilot's settings: %w", err)
		}

		return nil
	})
	if err != nil {
		return saved, err
	}

	s.events.Emit(event.AutopilotChanged{})

	return saved, nil
}

// checkAutopilot returns the writes that store c over Autopilot's settings as stored,
// refusing, worded for the window, settings their check refuses.
func checkAutopilot(stored map[string]string, c autopilotprefs.Config) (map[string]string, error) {
	if err := c.Check(); err != nil {
		return nil, autopilotRefusal(c, err)
	}

	return autopilotprefs.Writes(stored, c), nil
}

// autopilotRefusal words a refusal of c, the settings Check refused with err.
func autopilotRefusal(c autopilotprefs.Config, err error) error {
	var msg string

	switch {
	case errors.Is(err, autopilotprefs.ErrUnknownMode):
		msg = "That isn't one of the modes: choose Dry run, Ask first, or Autopilot."
	case errors.Is(err, autopilotprefs.ErrUnknownUnit):
		msg = "The daily cap is set in dollars or in tokens."
	case errors.Is(err, autopilotprefs.ErrLogsOutlastHistory):
		msg = fmt.Sprintf("Keep the run logs no longer than the history: %d days at most.", c.HistoryDays)
	default:
		msg = rangeRefusal(c)
	}

	return uierr.New(msg, err)
}

// rangeRefusal names the first of c's numbers outside its range, and the range.
func rangeRefusal(c autopilotprefs.Config) string {
	for _, n := range []struct {
		value, low, high int
		msg              string
	}{
		{c.AgentsAtOnce, autopilotprefs.MinAgentsAtOnce, autopilotprefs.MaxAgentsAtOnce, "Choose %d to %d agents at once."},
		{c.RunsPerDay, autopilotprefs.MinRunsPerDay, autopilotprefs.MaxRunsPerDay, "Choose %d to %d agent runs a day."},
		{
			c.ActionsPerDay, autopilotprefs.MinActionsPerDay, autopilotprefs.MaxActionsPerDay,
			"Choose %d to %d GitHub actions a day.",
		},
		{
			c.ActionsPerHour, autopilotprefs.MinActionsPerHour, autopilotprefs.MaxActionsPerHour,
			"Choose %d to %d GitHub actions an hour.",
		},
		{
			c.CatchUpHours, autopilotprefs.MinCatchUpHours, autopilotprefs.MaxCatchUpHours,
			"Choose %d to %d hours before Autopilot asks to catch up.",
		},
		{
			c.HistoryDays, autopilotprefs.MinHistoryDays, autopilotprefs.MaxHistoryDays,
			"Keep %d to %d days of Autopilot's history.",
		},
		{c.LogDays, autopilotprefs.MinLogDays, autopilotprefs.MaxLogDays, "Keep the run logs %d to %d days."},
	} {
		if n.value < n.low || n.value > n.high {
			return fmt.Sprintf(n.msg, n.low, n.high)
		}
	}

	if c.DailyCap.Unit == autopilotprefs.UnitTokens {
		return fmt.Sprintf("Set the daily cap to 0 for none, or %s to %s tokens.",
			grouped(autopilotprefs.MinCapTokens), grouped(autopilotprefs.MaxCapTokens))
	}

	return fmt.Sprintf("Set the daily cap to 0 for none, or $%d to $%d.", autopilotprefs.MinCapUSD,
		autopilotprefs.MaxCapUSD)
}

// grouped writes n with its thousands set apart by commas: "100,000".
func grouped(n int) string {
	digits := strconv.Itoa(n)
	out := make([]byte, 0, len(digits)+len(digits)/3)

	for i := range len(digits) {
		if i > 0 && (len(digits)-i)%3 == 0 {
			out = append(out, ',')
		}

		out = append(out, digits[i])
	}

	return string(out)
}

// autopilotFields are Autopilot's settings a backup can hold. Each is checked as its
// setter checks it, against the other settings as they are stored: the history and the
// run logs together, so the logs never outlast the history.
func (s *Service) autopilotFields() []portableField {
	field, number, toggle := s.autopilotField, s.autopilotNumber, s.autopilotToggle

	return []portableField{
		toggle("Autopilot (beta)", func(a PortableAutopilot) *bool { return a.Enabled },
			func(c *autopilotprefs.Config, on bool) { c.Enabled = on }),
		toggle("Pause Autopilot", func(a PortableAutopilot) *bool { return a.Paused },
			func(c *autopilotprefs.Config, on bool) { c.Paused = on }),
		field("New routines start in", func(a PortableAutopilot) bool { return a.DefaultMode != nil },
			func(a PortableAutopilot, c *autopilotprefs.Config) { c.DefaultMode = *a.DefaultMode },
			func(a PortableAutopilot) string { return a.DefaultMode.Label() }),
		number("Agents at once", func(a PortableAutopilot) *int { return a.AgentsAtOnce },
			func(c *autopilotprefs.Config, n int) { c.AgentsAtOnce = n }, ""),
		number("Agent runs a day", func(a PortableAutopilot) *int { return a.RunsPerDay },
			func(c *autopilotprefs.Config, n int) { c.RunsPerDay = n }, ""),
		number("GitHub actions a day", func(a PortableAutopilot) *int { return a.ActionsPerDay },
			func(c *autopilotprefs.Config, n int) { c.ActionsPerDay = n }, ""),
		number("GitHub actions an hour", func(a PortableAutopilot) *int { return a.ActionsPerHour },
			func(c *autopilotprefs.Config, n int) { c.ActionsPerHour = n }, ""),
		field("Autopilot's daily cap", func(a PortableAutopilot) bool { return a.DailyCap != nil },
			func(a PortableAutopilot, c *autopilotprefs.Config) { c.DailyCap = *a.DailyCap },
			func(a PortableAutopilot) string { return capWords(*a.DailyCap) }),
		toggle("Keep the Mac awake while agents work", func(a PortableAutopilot) *bool { return a.KeepAwake },
			func(c *autopilotprefs.Config, on bool) { c.KeepAwake = on }),
		toggle("Hold agent starts during an outage", func(a PortableAutopilot) *bool { return a.HoldOnOutage },
			func(c *autopilotprefs.Config, on bool) { c.HoldOnOutage = on }),
		number("Ask before catching up after", func(a PortableAutopilot) *int { return a.CatchUpHours },
			func(c *autopilotprefs.Config, n int) { c.CatchUpHours = n }, " hours"),
		field("Autopilot's history and run logs",
			func(a PortableAutopilot) bool { return a.HistoryDays != nil || a.LogDays != nil }, setHistory,
			historyWords),
	}
}

// setHistory sets how long a backup keeps Autopilot's history and run logs, as far as it
// holds them: a history alone shortens the logs with it, as its setter does.
func setHistory(a PortableAutopilot, c *autopilotprefs.Config) {
	if a.HistoryDays != nil {
		c.HistoryDays, c.LogDays = *a.HistoryDays, min(c.LogDays, *a.HistoryDays)
	}

	if a.LogDays != nil {
		c.LogDays = *a.LogDays
	}
}

// portableAutopilotOf returns p's Autopilot settings, none set when it holds none.
func portableAutopilotOf(p Portable) PortableAutopilot {
	if p.Autopilot == nil {
		return PortableAutopilot{}
	}

	return *p.Autopilot
}

// autopilotField is one of Autopilot's settings a backup can hold: has reports whether a
// backup holds it, change sets it, and show words it. It is checked as its setter checks
// it, set over the settings as they are stored.
func (s *Service) autopilotField(label string, has func(PortableAutopilot) bool,
	change func(PortableAutopilot, *autopilotprefs.Config), show func(PortableAutopilot) string,
) portableField {
	return portableField{
		label: label, tells: tellsAutopilot,
		has: func(p Portable) bool { return has(portableAutopilotOf(p)) },
		check: func(ctx context.Context, p Portable) (map[string]string, error) {
			stored, err := s.prefs.GetMany(ctx, autopilotprefs.Keys()...)
			if err != nil {
				return nil, fmt.Errorf("read Autopilot's settings: %w", err)
			}

			c := autopilotprefs.Decode(stored)
			change(portableAutopilotOf(p), &c)

			return checkAutopilot(stored, c)
		},
		show: func(p Portable) string { return show(portableAutopilotOf(p)) },
	}
}

// autopilotNumber is one of Autopilot's whole-number settings a backup can hold, worded
// with unit.
func (s *Service) autopilotNumber(label string, get func(PortableAutopilot) *int,
	set func(*autopilotprefs.Config, int), unit string,
) portableField {
	return s.autopilotField(label, func(a PortableAutopilot) bool { return get(a) != nil },
		func(a PortableAutopilot, c *autopilotprefs.Config) { set(c, *get(a)) },
		func(a PortableAutopilot) string { return strconv.Itoa(*get(a)) + unit })
}

// autopilotToggle is one of Autopilot's switches a backup can hold.
func (s *Service) autopilotToggle(label string, get func(PortableAutopilot) *bool,
	set func(*autopilotprefs.Config, bool),
) portableField {
	return s.autopilotField(label, func(a PortableAutopilot) bool { return get(a) != nil },
		func(a PortableAutopilot, c *autopilotprefs.Config) { set(c, *get(a)) },
		func(a PortableAutopilot) string { return onOffWords(*get(a)) })
}

// historyWords words how long a backup keeps Autopilot's history and run logs, as far as
// it holds them.
func historyWords(a PortableAutopilot) string {
	switch {
	case a.HistoryDays != nil && a.LogDays != nil:
		return fmt.Sprintf("%d days, run logs %d", *a.HistoryDays, *a.LogDays)
	case a.HistoryDays != nil:
		return fmt.Sprintf("%d days", *a.HistoryDays)
	case a.LogDays != nil:
		return fmt.Sprintf("run logs %d days", *a.LogDays)
	default:
		return ""
	}
}

// onOffWords words a switch.
func onOffWords(on bool) string {
	if on {
		return "on"
	}

	return "off"
}

// capWords words a daily cap, as Settings shows it: "off", "$25", or "2,000,000 tokens".
func capWords(c autopilotprefs.DailyCap) string {
	switch {
	case c.Off():
		return "off"
	case c.Unit == autopilotprefs.UnitTokens:
		return grouped(c.Amount) + " tokens"
	default:
		return "$" + strconv.Itoa(c.Amount)
	}
}
