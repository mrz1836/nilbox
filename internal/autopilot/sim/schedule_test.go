package sim_test

import (
	"strings"
	"testing"
	"testing/synctest"
	"time"
	_ "time/tzdata" // the daylight-saving and time-zone scenarios need real zones on any machine

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/sim"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
)

// Schedules: a routine On a schedule fires on the engine's 1-minute tick once its time of
// day has come, by the Mac's own clock, once a day it is due: a time missed while asleep
// runs on waking the same day, never for a day that is gone; and a clock that changes
// (daylight saving, the user setting it, a new time zone) fires it at the same time of
// day, never twice in one day.

// scheduled is a routine On a schedule over the scenarios' search, at its time of day,
// with steps, told of each match on its own or, with style, once with the list.
func scheduled(every catalog.Every, at string, style catalog.ScheduleStyle, steps ...spec.Step) spec.Spec {
	return spec.Spec{
		Version: spec.Version,
		Watch:   spec.Watch{Sources: []spec.Source{sim.Search()}},
		Trigger: spec.Trigger{Event: catalog.EventSchedule, Schedule: &spec.Schedule{Every: every, At: at, Style: style}},
		Steps:   steps,
	}
}

// notifyStep is Notify me.
func notifyStep() spec.Step {
	return spec.Step{Notify: &spec.NotifyStep{}}
}

// scheduleRig is a simulation watching #1 to #n, their checks passing, with sp switched on
// in Autopilot at the clock's time (Start, a Monday at 09:00, unless at is set), and
// refreshed once.
func scheduleRig(t *testing.T, at time.Time, n int, sp spec.Spec) (*sim.Sim, autopilot.Routine) {
	t.Helper()

	s := sim.New(t)
	if !at.IsZero() {
		s.Clock.Set(at)
	}

	for i := 1; i <= n; i++ {
		s.World.Add(sim.Item{Number: i, Watched: true, Checks: passing, Passed: 4})
	}

	s.Begin()

	r := s.Save("On a schedule", sp, autopilotprefs.ModeAuto)
	s.Enable(r.ID, autopilot.CurrentRun)
	s.Refresh()

	return s, r
}

// keys returns each match's fire key, oldest first.
func keys(s *sim.Sim) []string {
	ledger := s.Ledger()
	out := make([]string, 0, len(ledger))

	for _, r := range ledger {
		out = append(out, r.Key)
	}

	return out
}

// until advances the simulation to at on its own clock, its ticks running, refreshing as
// the scheduler does when refresh says so.
func until(s *sim.Sim, at time.Time, refresh bool) {
	if d := at.Sub(s.Clock.Now()); d > 0 {
		s.Advance(d, refresh)
	}
}

func TestAScheduleFiresOnTheTickOnceItsTimeComes(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := scheduleRig(t, time.Time{}, 2, scheduled(catalog.EveryDay, "10:00", "", notifyStep()))
		assert.Empty(t, s.Ledger(), "at 09:00 it isn't due")

		monday := sim.Start()
		s.Reach(monday.Add(59*time.Minute), true)
		assert.Empty(t, s.Ledger(), "at 09:59 it still isn't")

		// The tick after 10:00 fires it, with no refresh between: the clock alone decides.
		until(s, monday.Add(62*time.Minute), false)
		assert.Equal(t, []string{"schedule:2026-11-16", "schedule:2026-11-16"}, keys(s))
		assert.Len(t, s.Notices.Told(), 2, "each match is told of on its own")

		// The rest of the day, every tick and refresh finds its occurrence recorded: the
		// half hour after it fired, and the evening, woken from a sleep.
		s.Advance(30*time.Minute, true)
		s.Reach(monday.Add(14*time.Hour), true)
		assert.Len(t, s.Ledger(), 2, "once a day")

		// The next day at its time, it fires again, on the new day's key.
		s.Reach(monday.Add(25*time.Hour+2*time.Minute), true)
		assert.Equal(t, []string{
			"schedule:2026-11-16", "schedule:2026-11-16", "schedule:2026-11-17", "schedule:2026-11-17",
		}, keys(s))
	})
}

func TestAScheduleMissedWhileAsleepRunsOnceOnWakingTheSameDay(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := scheduleRig(t, time.Time{}, 1, scheduled(catalog.EveryDay, "10:00", "", notifyStep()))

		// Asleep from 09:00 to 13:00: no tick at 10:00.
		s.Sleep(4 * time.Hour)
		assert.Equal(t, []string{"schedule:2026-11-16"}, keys(s), "waking runs what it missed, today")

		s.Advance(10*time.Minute, true)
		assert.Len(t, s.Ledger(), 1, "and only once")
	})
}

func TestAScheduleMissedOnADayGoneWaitsForItsNextTime(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := scheduleRig(t, time.Time{}, 1, scheduled(catalog.EveryDay, "10:00", "", notifyStep()))

		// Asleep from Monday 09:00 to Tuesday 09:30: Monday's 10:00 went by in the sleep.
		s.Sleep(24*time.Hour + 30*time.Minute)
		s.Refresh()
		assert.Empty(t, s.Ledger(), "Monday's time is gone, and Tuesday's hasn't come")

		s.Reach(sim.Start().Add(25*time.Hour+2*time.Minute), true)
		assert.Equal(t, []string{"schedule:2026-11-17"}, keys(s), "Tuesday's, once")
	})
}

func TestAWeekdayScheduleSkipsTheWeekend(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		// Friday 20 November 2026, 09:00.
		friday := time.Date(2026, time.November, 20, 9, 0, 0, 0, time.UTC)
		s, _ := scheduleRig(t, friday, 1, scheduled(catalog.EveryWeekday, "09:30", "", notifyStep()))

		s.Reach(friday.Add(32*time.Minute), true)
		require.Equal(t, []string{"schedule:2026-11-20"}, keys(s))

		// Saturday and Sunday around 09:30 the Mac is awake, and nothing fires.
		s.Sleep(24*time.Hour - 10*time.Minute)
		s.Advance(15*time.Minute, true)
		s.Sleep(24*time.Hour - 15*time.Minute)
		s.Advance(15*time.Minute, true)
		assert.Len(t, s.Ledger(), 1, "a weekday schedule doesn't fire on the weekend")

		// Asleep from Sunday until Monday afternoon: Monday's runs on waking.
		s.Sleep(28 * time.Hour)
		s.Refresh()
		assert.Equal(t, []string{"schedule:2026-11-20", "schedule:2026-11-23"}, keys(s))
	})
}

func TestAScheduleKeepsItsTimeOfDayAcrossTheChangeOfClocks(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ny, err := time.LoadLocation("America/New_York")
		require.NoError(t, err)

		// Saturday 31 October 2026, 09:00 EDT; the clocks go back at 02:00 on 1 November.
		saturday := time.Date(2026, time.October, 31, 9, 0, 0, 0, ny)
		s, _ := scheduleRig(t, saturday, 1, scheduled(catalog.EveryDay, "10:00", "", notifyStep()))
		// A day between two refreshes asks before catching up; this timeline is about clocks.
		s.Change(func(c *autopilotprefs.Config) { c.CatchUpHours = autopilotprefs.MaxCatchUpHours })

		s.Reach(time.Date(2026, time.October, 31, 10, 2, 0, 0, ny), false)
		require.Equal(t, []string{"schedule:2026-10-31"}, keys(s))

		// Sunday at 09:30 EST is 14:30 UTC, past Saturday's 10:00 EDT by a day in UTC: a
		// schedule kept in UTC would fire now. It waits for 10:00 on the clock.
		s.Sleep(time.Date(2026, time.November, 1, 9, 30, 0, 0, ny).Sub(s.Clock.Now()))
		s.Advance(25*time.Minute, false)
		assert.Len(t, s.Ledger(), 1, "09:55 EST: not yet")

		until(s, time.Date(2026, time.November, 1, 10, 2, 0, 0, ny), false)
		assert.Equal(t, []string{"schedule:2026-10-31", "schedule:2026-11-01"}, keys(s))
		assert.Equal(t, 10, s.Matches()[1].CreatedAt.In(ny).Hour(), "at 10:00 on the clock, after the change")
	})
}

func TestAScheduleNeverFiresTwiceInADayWhenTheClockIsSetBack(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := scheduleRig(t, time.Time{}, 1, scheduled(catalog.EveryDay, "10:00", "", notifyStep()))

		s.Reach(sim.Start().Add(62*time.Minute), false)
		require.Len(t, s.Ledger(), 1)

		// The user sets the Mac's clock back an hour, and its ticks pass 10:00 again.
		s.Clock.Turn(-time.Hour)
		s.Reach(sim.Start().Add(65*time.Minute), true)
		assert.Len(t, s.Ledger(), 1, "the day has had its run")

		// Set forward past midnight, the new day's time fires it once.
		s.Clock.Turn(14 * time.Hour)
		s.Reach(sim.Start().Add(25*time.Hour+2*time.Minute), true)
		assert.Equal(t, []string{"schedule:2026-11-16", "schedule:2026-11-17"}, keys(s))
	})
}

func TestAScheduleFollowsTheTimeZoneTheMacIsIn(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ny, err := time.LoadLocation("America/New_York")
		require.NoError(t, err)

		la, err := time.LoadLocation("America/Los_Angeles")
		require.NoError(t, err)

		// Monday 16 November 2026, 09:00 in New York.
		s, _ := scheduleRig(t, time.Date(2026, time.November, 16, 9, 0, 0, 0, ny), 1,
			scheduled(catalog.EveryDay, "10:00", "", notifyStep()))

		s.Reach(time.Date(2026, time.November, 16, 10, 2, 0, 0, ny), true)
		require.Equal(t, []string{"schedule:2026-11-16"}, keys(s))

		// Flying west, it is 07:02 again in Los Angeles: the same day, already run.
		s.Clock.Travel(la)
		s.Reach(time.Date(2026, time.November, 16, 10, 5, 0, 0, la), true)
		assert.Len(t, s.Ledger(), 1, "a day runs once, wherever the Mac is")

		// Tuesday, it fires at 10:00 in Los Angeles, the zone the Mac is in now: by 09:55
		// there, three hours past 10:00 in New York, it hasn't.
		s.Reach(time.Date(2026, time.November, 17, 9, 55, 0, 0, la), true)
		assert.Len(t, s.Ledger(), 1)

		until(s, time.Date(2026, time.November, 17, 10, 2, 0, 0, la), true)
		assert.Equal(t, []string{"schedule:2026-11-16", "schedule:2026-11-17"}, keys(s))
	})
}

func TestADigestTellsOnceWithTheList(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		sp := scheduled(catalog.EveryDay, "10:00", catalog.ScheduleDigest,
			spec.Step{Notify: &spec.NotifyStep{Note: "Waiting on you"}}, spec.Step{Pin: &spec.Empty{}})
		s, r := scheduleRig(t, time.Time{}, 3, sp)
		s.World.Fail(1)
		s.World.Fail(2)
		s.Refresh()

		s.Reach(sim.Start().Add(62*time.Minute), false)

		told := s.Notices.Told()
		require.Len(t, told, 1, "one notice for the three")
		assert.Equal(t, autopilot.NoticeAlert, told[0].Kind)
		assert.Equal(t, r.ID, told[0].RoutineID)
		assert.Equal(t, "On a schedule", told[0].Routine)
		assert.Nil(t, told[0].Item, "about no one item")
		assert.Equal(t, "3 Dependabot pull requests need you", told[0].Title)
		assert.Equal(t, "2 failing, 1 waiting for review: acme-org/example-repo #2, acme-org/example-repo #1, "+
			"and acme-org/example-repo #3. Waiting on you.", told[0].Body, "the failing first, newest activity first")

		// Each match's Notify me is done, and the pins ran on each.
		assert.Equal(t, []string{"done:ran", "done:ran", "done:ran"}, tally(s))

		for _, m := range s.Matches() {
			require.Len(t, m.Steps, 2)
			assert.Equal(t, "Told you, in a digest of 3.", m.Steps[0].Note)
		}

		assert.Len(t, s.Triage.Calls(), 3, "its other steps run on each item")

		// One that joins the watch later the same day is told in a digest of its own.
		s.World.Add(sim.Item{Number: 4, Watched: true, Checks: passing, Passed: 4})
		s.Advance(10*time.Minute, true)

		told = s.Notices.Told()
		require.Len(t, told, 2)
		assert.Equal(t, "1 Dependabot pull request needs you", told[1].Title)
		assert.True(t, strings.HasPrefix(told[1].Body, "1 waiting for review: acme-org/example-repo #4."), told[1].Body)
	})
}
