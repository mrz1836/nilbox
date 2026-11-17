package sim_test

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/sim"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/snooze"
)

// The local steps, through the inbox's own triage: what each did, marked with its routine;
// Undo; and the routine leaving alone what the user took back.

// snoozeBots is the Snooze passing bot PRs template, exactly as the template gallery holds it.
func snoozeBots(t *testing.T) spec.Spec {
	t.Helper()

	tmpl, ok := autopilot.TemplateByKey("snooze-passing-bot-prs")
	require.True(t, ok)
	require.NoError(t, spec.Validate(tmpl.Spec))

	return tmpl.Spec
}

// passedKey is Checks pass's fire key on commit n of the pull request number.
func passedKey(number, n int) string {
	return "checks_passed:commit:" + sim.Commit(number, n)
}

// botPR is a Dependabot patch update in Review requested whose checks passed: no member
// of the repository, as GitHub says of the bot.
func botPR(number int) sim.Item {
	return sim.Item{Number: number, ReviewRequested: true, Checks: passing, Passed: 4, Association: "NONE"}
}

// snoozing starts a simulation with the pull requests its, the routine Snooze passing bot
// PRs on in mode, after a refresh.
func snoozing(t *testing.T, mode autopilotprefs.Mode, its ...sim.Item) (*sim.Sim, autopilot.Routine) {
	t.Helper()

	s := sim.New(t)
	for _, it := range its {
		s.World.Add(it)
	}

	s.Begin()
	s.Refresh()

	r := s.Save("Snooze passing bot PRs", snoozeBots(t), mode)
	s.Enable(r.ID, autopilot.CurrentRun)
	s.Refresh()

	return s, r
}

// groups returns every grouped line of Activity's first page, newest day first.
func groups(t *testing.T, s *sim.Sim) []autopilot.ActivityGroup {
	t.Helper()

	page, err := s.Service.Activity(t.Context(), 0, 500)
	require.NoError(t, err)

	var out []autopilot.ActivityGroup
	for _, d := range page.Days {
		out = append(out, d.Groups...)
	}

	return out
}

// snoozedBy asserts that #number is snoozed until days from at, by the routine id (0 for
// the user).
func snoozedBy(t *testing.T, s *sim.Sim, number, days int, at time.Time, id int64) {
	t.Helper()

	it := s.InboxItem(number)
	assert.Equal(t, at.AddDate(0, 0, days).UTC().Truncate(time.Second).Format(time.RFC3339), it.SnoozedUntil, "#%d", number)
	assert.Equal(t, id, it.SetAsideBy, "#%d", number)
}

func TestSnoozePassingBotPRsSnoozesTheTrustedBotsPatchAndMinorUpdates(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		start := sim.Start()
		member := sim.Item{Number: 3, Assigned: true, Checks: passing, Passed: 4, Author: "octo-member"}
		failing := botPR(4)
		failing.Checks, failing.Failed, failing.Passed = "FAILURE", 1, 3
		major := botPR(2)
		major.Title = "Bump example-lib from 1.0.0 to 2.0.0"
		searched := sim.Item{Number: 5, Watched: true, Checks: passing, Passed: 4}

		s, r := snoozing(t, autopilotprefs.ModeAuto, botPR(1), major, member, failing, searched)

		want := []sim.Row{row(1, autopilot.StateDone, "", autopilot.ReasonRan, passedKey(1, 1))}
		assert.Equal(t, want, s.Ledger(), "only the bot's patch update, whose checks passed, in the lists it watches")
		snoozedBy(t, s, 1, 3, start, r.ID)

		for _, n := range []int{2, 3, 4} {
			assert.Empty(t, s.InboxItem(n).SnoozedUntil, "#%d stays in the inbox", n)
		}

		lines := groups(t, s)
		require.Len(t, lines, 1)
		assert.Equal(t, "Snoozed 1", lines[0].Words)
		assert.Equal(t, catalog.StepSnooze, lines[0].Action)
		assert.Equal(t, r.Name, lines[0].Routine)
		assert.Len(t, lines[0].Undo, 1, "it can be undone")
		assert.Equal(t, []int64{s.Matches()[0].ID}, lines[0].Matches)

		// Once per head commit: refreshes and a restart snooze nothing again.
		s.Refresh()
		s.Restart()
		s.Advance(10*time.Minute, true)
		assert.Equal(t, want, s.Ledger())
		assert.Len(t, s.Triage.Calls(), 1)

		// Dependabot rebases it: a bot's push doesn't wake the snooze, and the new commit
		// passing is a new occurrence, which snoozes it again for the routine's length.
		s.World.Push(1, sim.Commit(1, 2))
		s.World.Pass(1)
		s.Advance(5*time.Minute, true)

		got := s.Ledger()
		require.Len(t, got, 2)
		assert.Equal(t, row(1, autopilot.StateDone, "", autopilot.ReasonRan, passedKey(1, 2)), got[1])
		snoozedBy(t, s, 1, 3, s.Clock.Now(), r.ID)
		assert.Equal(t, "Snoozed 2", groups(t, s)[0].Words, "the day's line counts both")
	})
}

func TestSnoozePassingBotPRsWakesByTheUsersRules(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, r := snoozing(t, autopilotprefs.ModeAuto, botPR(1))
		require.NoError(t, prefs.New(s.DB, s.Clock).SetMany(t.Context(), map[string]string{snooze.PrefWakeComments: "1"}))
		snoozedBy(t, s, 1, 3, sim.Start(), r.ID)

		s.World.Change(1, func(it *sim.Item) { it.Comments++ })
		s.Advance(5*time.Minute, true)

		it := s.InboxItem(1)
		require.NotNil(t, it.WokeBy, "a comment woke it, as the user's rules say")
		assert.Equal(t, snooze.Reasons{Comments: 1}, *it.WokeBy)
		assert.Len(t, s.Ledger(), 1, "the same commit doesn't snooze it again")
	})
}

func TestSnoozePassingBotPRsRunsAtOnceInAskFirstAndRecordsInDryRun(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, r := snoozing(t, autopilotprefs.ModeAsk, botPR(1))

		assert.Equal(t, []sim.Row{row(1, autopilot.StateDone, "", autopilot.ReasonRan, passedKey(1, 1))}, s.Ledger(),
			"a local step needs no OK")
		snoozedBy(t, s, 1, 3, sim.Start(), r.ID)
	})

	synctest.Test(t, func(t *testing.T) {
		s, _ := snoozing(t, autopilotprefs.ModeDryRun, botPR(1))

		dry := sim.Row{Number: 1, State: autopilot.StateDone, Reason: autopilot.ReasonWouldRun, DryRun: true, Key: passedKey(1, 1)}
		assert.Equal(t, []sim.Row{dry}, s.Ledger())
		assert.Empty(t, s.InboxItem(1).SnoozedUntil, "Dry run records what would run, and changes nothing")
		assert.Empty(t, s.Triage.Calls())
		assert.Empty(t, groups(t, s))
	})
}

func TestALocalStepActsOnAnOutsideContributorsItem(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		stranger := sim.Item{Number: 1, Watched: true, Author: "a-stranger", Association: "NONE", CrossRepo: true}
		s, r := started(t, sim.Pins(), autopilotprefs.ModeAuto)
		s.World.Add(stranger)
		s.Refresh()

		assert.Equal(t, []sim.Row{row(1, autopilot.StateDone, "", autopilot.ReasonRan, "appears:item")}, s.Ledger())
		assert.Equal(t, r.ID, s.InboxItem(1).PinnedBy, "a pin puts no one's text in front of an agent")
	})
}

func TestUndoPutsBackOnlyWhatIsStillAsTheRoutineLeftIt(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, r := snoozing(t, autopilotprefs.ModeAuto, botPR(1), botPR(2), botPR(3))
		ctx := t.Context()

		lines := groups(t, s)
		require.Len(t, lines, 1)
		require.Equal(t, "Snoozed 3", lines[0].Words)
		require.Len(t, lines[0].Undo, 3)

		// The user snoozes #2 again themselves, which takes the routine's snooze back, and
		// dismisses #3, which goes further than it.
		_, err := s.Inbox.Snooze(ctx, s.ItemID(2), 7)
		require.NoError(t, err)
		_, err = s.Inbox.Dismiss(ctx, s.ItemID(3))
		require.NoError(t, err)
		s.Settle()

		line := groups(t, s)[0]
		assert.Equal(t, 1, line.Reversed, "the user took one back")
		assert.Len(t, line.Undo, 1, "only #1 is still as the routine left it")

		effects := map[int]int64{}

		for _, m := range s.Matches() {
			require.Len(t, m.Effects, 1)
			effects[m.Item.Number] = m.Effects[0].ID
		}

		undone, err := s.Service.Undo(ctx, []int64{effects[1], effects[2], effects[3]})
		require.NoError(t, err)
		assert.Equal(t, []int64{effects[1]}, undone.PutBack)
		assert.Equal(t, []int64{effects[3]}, undone.Left, "changed since: left as the user left it")
		assert.Equal(t, []int64{effects[2]}, undone.Skipped, "the user took it back already")
		assert.Equal(t, "Put back 1 item. 1 changed since, so it's left as it is.", undone.Words)

		assert.Empty(t, s.InboxItem(1).SnoozedUntil, "#1 is back in the inbox")
		snoozedBy(t, s, 2, 7, sim.Start(), 0)
		assert.NotEmpty(t, s.InboxItem(3).DismissedAt, "#3 stays dismissed")

		again, err := s.Service.Undo(ctx, []int64{effects[1]})
		require.NoError(t, err)
		assert.Equal(t, []int64{effects[1]}, again.Skipped, "a second Undo changes nothing")

		line = groups(t, s)[0]
		assert.Equal(t, 1, line.Undone)
		assert.Empty(t, line.Undo)

		// Undo and the user's own snooze left the routine hands off #1 and #2; #3, which the
		// user only went further with, isn't.
		page, err := s.Service.Activity(ctx, 0, 0)
		require.NoError(t, err)
		require.Len(t, page.LeftAlone, 2)

		why := map[int]string{}

		for _, l := range page.LeftAlone {
			assert.Equal(t, r.ID, l.RoutineID)
			why[l.Item.Number] = l.Why
		}

		assert.Equal(t, map[int]string{1: "You undid what it did.", 2: "You snoozed it again yourself."}, why)
	})
}

func TestARoutineLeavesAloneWhatYouTookBackUntilYouLetItActAgain(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, r := snoozing(t, autopilotprefs.ModeAuto, botPR(1), botPR(2))
		ctx := t.Context()
		one := s.ItemID(1)

		_, err := s.Inbox.Restore(ctx, one)
		require.NoError(t, err)
		s.Settle()

		explained, err := s.Service.Explain(ctx, r.ID, one)
		require.NoError(t, err)
		assert.True(t, explained.LeftAlone)
		assert.Contains(t, explained.Ledger, "Left alone: you took back what it did here. Let it act again to change that.")

		// Activity says so too: the routine leaves #1 alone, since the user brought back
		// what it snoozed, and #2, which the user didn't touch, isn't listed.
		page, err := s.Service.Activity(ctx, 0, 0)
		require.NoError(t, err)
		require.Len(t, page.LeftAlone, 1)
		assert.Equal(t, r.ID, page.LeftAlone[0].RoutineID)
		assert.Equal(t, 1, page.LeftAlone[0].Item.Number)
		assert.Equal(t, "You brought back what it snoozed.", page.LeftAlone[0].Why)

		tested, err := s.Service.Test(ctx, autopilot.Draft{ID: r.ID, Name: r.Name, Spec: r.Spec})
		require.NoError(t, err)

		for _, it := range tested.Items {
			assert.Equal(t, it.Number == 1, it.LeftAlone, "#%d", it.Number)
		}

		// Both are rebased and pass again: the routine snoozes #2, and leaves #1 alone.
		for _, n := range []int{1, 2} {
			s.World.Push(n, sim.Commit(n, 2))
			s.World.Pass(n)
		}

		s.Advance(10*time.Minute, true)

		keys := map[string]bool{}
		for _, m := range s.Ledger() {
			keys[m.Key] = true
		}

		assert.True(t, keys[passedKey(2, 2)])
		assert.False(t, keys[passedKey(1, 2)], "left alone: no match is recorded")
		assert.Empty(t, s.InboxItem(1).SnoozedUntil, "as the user left it")

		require.NoError(t, s.Service.LetActAgain(ctx, r.ID, one))
		s.Settle()

		got := s.Ledger()
		assert.Equal(t, row(1, autopilot.StateDone, "", autopilot.ReasonRan, passedKey(1, 2)), got[len(got)-1],
			"let act again, it snoozes #1 for the commit that holds now")
		snoozedBy(t, s, 1, 3, s.Clock.Now(), r.ID)
	})
}

func TestUnpinningWhatARoutinePinnedLeavesItHandsOff(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, r := started(t, sim.Pins(), autopilotprefs.ModeAuto)
		ctx := t.Context()
		one := s.ItemID(1)

		require.Equal(t, r.ID, s.InboxItem(1).PinnedBy, "the pin is marked with the routine")

		_, err := s.Inbox.Unpin(ctx, one)
		require.NoError(t, err)
		s.Settle()

		assert.Zero(t, s.InboxItem(1).PinnedBy)

		page, err := s.Service.Activity(ctx, 0, 0)
		require.NoError(t, err)
		require.Len(t, page.LeftAlone, 1)
		assert.Equal(t, "You unpinned what it pinned.", page.LeftAlone[0].Why)
		assert.Equal(t, 1, page.LeftAlone[0].Item.Number)

		effect := s.Effects()[0]
		assert.NotNil(t, effect.ReversedAt, "the effect is taken back")
		assert.False(t, effect.CanUndo)

		require.NoError(t, s.Service.LetActAgain(ctx, r.ID, one))

		page, err = s.Service.Activity(ctx, 0, 0)
		require.NoError(t, err)
		assert.Empty(t, page.LeftAlone)
	})
}

// onSearch is a routine over the scenarios' search that runs step when trigger fires;
// Checks fail fires with no wait for the checks to settle.
func onSearch(trigger catalog.Event, step spec.Step) spec.Spec {
	sp := spec.Spec{
		Version: spec.Version,
		Watch:   spec.Watch{Sources: []spec.Source{sim.Search()}},
		Trigger: spec.Trigger{Event: trigger},
		Steps:   []spec.Step{step},
	}

	if trigger == catalog.EventChecksFailed {
		sp.Trigger.WaitForChecks = new(false)
	}

	return sp
}

func TestAStepOnItsWayLosesToTheUsersOwnReversal(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, onSearch(catalog.EventChecksPassed, spec.Step{Dismiss: &spec.Empty{}}), autopilotprefs.ModeAuto)
		ctx := t.Context()
		one := s.ItemID(1)

		require.NotEmpty(t, s.InboxItem(1).DismissedAt, "the routine dismissed #1's first commit")

		// Dependabot rebases it, and the routine decides to dismiss the new commit; while
		// that step is on its way, the user brings #1 back.
		s.Triage.Hold()
		s.World.Push(1, sim.Commit(1, 2))
		s.World.Pass(1)
		s.Advance(5*time.Minute, true)
		require.Len(t, s.Triage.Calls(), 2, "the second dismissal is on its way")

		_, err := s.Inbox.Restore(ctx, one)
		require.NoError(t, err)
		s.Triage.Release()
		s.Settle()

		assert.Empty(t, s.InboxItem(1).DismissedAt, "the user's restore wins over the step on its way")

		got := s.Matches()
		require.Len(t, got, 2)
		require.Len(t, got[1].Steps, 1)
		assert.Equal(t, "Not done: the item changed since this matched.", got[1].Steps[0].Note,
			"the step was a compare-and-set on the dismissal it decided on")
		assert.Empty(t, got[1].Effects, "the step changed nothing, so it records nothing")
		require.Len(t, got[0].Effects, 1)
		assert.NotNil(t, got[0].Effects[0].ReversedAt, "the first dismissal is what the user took back")
	})
}

func TestARestoreStepWaitsForItsActiveHours(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		sp := onSearch(catalog.EventChecksFailed, spec.Step{Restore: &spec.Empty{}})
		sp.Limits.ActiveHours = &spec.ActiveHours{Days: catalog.Weekdays(), From: "10:00", To: "11:00"}

		s, _ := started(t, sp, autopilotprefs.ModeAuto)
		_, err := s.Inbox.Dismiss(t.Context(), s.ItemID(1))
		require.NoError(t, err)

		s.World.Fail(1)
		s.Refresh()

		assert.Equal(t, autopilot.BlockActiveHours, only(t, s).BlockedBy, "at 09:00, outside its hours")
		assert.NotEmpty(t, s.InboxItem(1).DismissedAt)
		assert.Empty(t, s.Triage.Calls())

		s.Advance(time.Hour, true)

		assert.Equal(t, autopilot.StateDone, only(t, s).State, "it runs once the hours begin")
		assert.Empty(t, s.InboxItem(1).DismissedAt, "and brings back what the user dismissed")
	})
}

func TestARestoreStepRunsAtOnceInACatchUp(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, onSearch(catalog.EventChecksFailed, spec.Step{Restore: &spec.Empty{}}), autopilotprefs.ModeAuto)
		_, err := s.Inbox.Dismiss(t.Context(), s.ItemID(1))
		require.NoError(t, err)

		// The app is closed for three days; the pull request fails meanwhile.
		s.Crash()
		s.Sleep(3 * 24 * time.Hour)
		s.World.Fail(1)
		s.Begin()
		s.Refresh()
		s.Refresh()

		engine, err := s.Service.Engine(t.Context())
		require.NoError(t, err)
		require.NotNil(t, engine.LastCycle)

		assert.Equal(t, autopilot.StateDone, only(t, s).State, "a local step never waits for a catch-up's question")
		assert.Empty(t, s.InboxItem(1).DismissedAt)
	})
}

// figuresOf returns the routine id's figures as List gives them.
func figuresOf(t *testing.T, s *sim.Sim, id int64) autopilot.RoutineFigures {
	t.Helper()

	list, err := s.Service.List(t.Context())
	require.NoError(t, err)

	for _, r := range list {
		if r.ID == id {
			return r.Figures
		}
	}

	require.Failf(t, "not listed", "routine %d", id)

	return autopilot.RoutineFigures{}
}

func TestEachRoutineIsListedWithItsOwnFigures(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, r := snoozing(t, autopilotprefs.ModeAuto, botPR(1), botPR(2))

		got := figuresOf(t, s, r.ID)
		assert.Equal(t, 2, got.Today)
		assert.Zero(t, got.WouldRunToday)
		require.NotNil(t, got.LastAt)
		assert.False(t, got.LastDryRun)

		// The next day nothing is today's, and the newest is still when it last matched.
		s.Sleep(24 * time.Hour)
		got = figuresOf(t, s, r.ID)
		assert.Zero(t, got.Today)
		require.NotNil(t, got.LastAt)
	})

	synctest.Test(t, func(t *testing.T) {
		s, r := snoozing(t, autopilotprefs.ModeDryRun, botPR(1))

		got := figuresOf(t, s, r.ID)
		assert.Zero(t, got.Today)
		assert.Equal(t, 1, got.WouldRunToday)
		assert.True(t, got.LastDryRun, "its newest match is a Dry run's")
	})

	synctest.Test(t, func(t *testing.T) {
		// Turned on from now on, over what already matched: its baseline counts for nothing.
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Watched: true})
		s.Begin()
		s.Refresh()

		r := s.Save("Pin bumps", sim.Pins(), autopilotprefs.ModeAuto)
		s.Enable(r.ID, autopilot.CurrentFromNow)
		s.Refresh()
		require.Equal(t, []sim.Row{row(1, autopilot.StateBaseline, "", autopilot.ReasonFromNow, "appears:item")}, s.Ledger())

		got := figuresOf(t, s, r.ID)
		assert.Zero(t, got.Today)
		assert.Nil(t, got.LastAt, "a baseline is nothing the routine did")
	})
}

func TestARoutinesLastRunIsItsNewestMatchThatRan(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, r := snoozing(t, autopilotprefs.ModeAuto, botPR(1))

		ran := figuresOf(t, s, r.ID).LastAt
		require.NotNil(t, ran)

		// An hour on, Pause holds its next match: the newest, which hasn't run.
		s.Sleep(time.Hour)
		s.Change(func(c *autopilotprefs.Config) { c.Paused = true })
		s.World.Add(botPR(2))
		s.Refresh()

		states := map[int]autopilot.State{}
		for _, m := range s.Matches() {
			states[m.Item.Number] = m.State
		}

		require.Equal(t, map[int]autopilot.State{1: autopilot.StateDone, 2: autopilot.StateQueued}, states)

		got := figuresOf(t, s, r.ID)
		require.NotNil(t, got.LastAt)
		assert.True(t, got.LastAt.Equal(*ran), "it last ran on #1, not when #2 was queued: got %v, want %v", got.LastAt, ran)
		assert.Equal(t, 2, got.Today, "both count as today's")
	})
}

func TestActivityFromReadsAgainWhatThePagesHoldUpToTheNewest(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := snoozing(t, autopilotprefs.ModeAuto, botPR(1), botPR(2), botPR(3), botPR(4))
		ctx := t.Context()

		ids := func(p autopilot.ActivityPage) []int64 {
			var out []int64

			for _, d := range p.Days {
				for _, m := range d.Matches {
					out = append(out, m.ID)
				}
			}

			return out
		}

		first, err := s.Service.Activity(ctx, 0, 2)
		require.NoError(t, err)
		require.True(t, first.More)

		older, err := s.Service.Activity(ctx, first.Next, 2)
		require.NoError(t, err)

		// A newer match arrives: read again from the first page's last match on, the first
		// page grows by it and meets the next page with nothing between.
		s.World.Add(botPR(5))
		s.Refresh()

		again, err := s.Service.ActivityFrom(ctx, first.Next, 0, 0)
		require.NoError(t, err)
		assert.False(t, again.More)
		assert.NotNil(t, again.LeftAlone)

		all, err := s.Service.Activity(ctx, 0, 500)
		require.NoError(t, err)
		assert.Equal(t, ids(all), append(ids(again), ids(older)...))
		assert.Len(t, ids(again), 3)

		// Past its limit it says so, and where the rest starts.
		capped, err := s.Service.ActivityFrom(ctx, 0, 0, 2)
		require.NoError(t, err)
		assert.True(t, capped.More)
		assert.Equal(t, ids(all)[1], capped.Next)
		assert.Equal(t, ids(all)[:2], ids(capped))

		// Bounded above, it reads the stretch alone, the older page again here, and the rest
		// of a stretch past its limit by its own before, so slices meet with nothing between.
		stretch, err := s.Service.ActivityFrom(ctx, 0, first.Next, 0)
		require.NoError(t, err)
		assert.Equal(t, ids(older), ids(stretch))
		assert.False(t, stretch.More)
		assert.Empty(t, stretch.LeftAlone, "only a stretch up to the newest says what the routines leave alone")

		rest, err := s.Service.ActivityFrom(ctx, 0, capped.Next, 2)
		require.NoError(t, err)
		assert.Equal(t, ids(all)[2:4], ids(rest))
	})
}

func TestARoutinesLastRunIsWhenItRanNotWhenItWasRecorded(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, r := snoozing(t, autopilotprefs.ModeAuto)

		// Under Pause, a match is recorded and queued; it runs five hours on, once Pause ends.
		s.Change(func(c *autopilotprefs.Config) { c.Paused = true })
		s.World.Add(botPR(1))
		s.Refresh()

		queued := s.Matches()
		require.Len(t, queued, 1)
		require.Equal(t, autopilot.StateQueued, queued[0].State)
		assert.Nil(t, figuresOf(t, s, r.ID).LastAt, "nothing ran yet")

		s.Sleep(5 * time.Hour)
		s.Change(func(c *autopilotprefs.Config) { c.Paused = false })
		s.Refresh()

		ran := s.Matches()
		require.Len(t, ran, 1)
		require.Equal(t, autopilot.StateDone, ran[0].State)
		require.NotNil(t, ran[0].ClosedAt)
		require.GreaterOrEqual(t, ran[0].ClosedAt.Sub(ran[0].CreatedAt), 5*time.Hour)

		got := figuresOf(t, s, r.ID)
		require.NotNil(t, got.LastAt)
		assert.True(t, got.LastAt.Equal(*ran[0].ClosedAt), "it ran when it closed, %v, not when it was recorded, %v: got %v",
			ran[0].ClosedAt, ran[0].CreatedAt, got.LastAt)
		assert.False(t, got.LastDryRun)
	})
}

func TestTheNamesOfDeletedRoutinesStayForWhatTheyDid(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, r := snoozing(t, autopilotprefs.ModeAuto, botPR(1))
		require.NoError(t, s.Service.Delete(t.Context(), r.ID))

		names, err := s.Service.RoutineNames(t.Context())
		require.NoError(t, err)
		assert.Contains(t, names, autopilot.RoutineName{ID: r.ID, Name: r.Name, Deleted: true})

		list, err := s.Service.List(t.Context())
		require.NoError(t, err)
		assert.Empty(t, list)
	})
}

func TestEachItemsLatestMatchIsWhatItsChipSays(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		member := sim.Item{Number: 3, Assigned: true, Checks: passing, Passed: 4, Author: "octo-member"}
		s, r := snoozing(t, autopilotprefs.ModeAuto, botPR(1), member)
		one, three := s.InboxItem(1).ID, s.InboxItem(3).ID

		got, err := s.Service.LatestMatches(t.Context(), []int64{one, three, 9999})
		require.NoError(t, err)
		require.Len(t, got, 1, "only the item a routine touched")
		assert.Equal(t, one, got[0].Item.ID)
		assert.Equal(t, r.ID, got[0].RoutineID)
		require.Len(t, got[0].Effects, 1)
		assert.Equal(t, catalog.StepSnooze, got[0].Effects[0].Action)
		assert.True(t, got[0].Effects[0].CanUndo)

		_, err = s.Service.Undo(t.Context(), []int64{got[0].Effects[0].ID})
		require.NoError(t, err)

		got, err = s.Service.LatestMatches(t.Context(), []int64{one})
		require.NoError(t, err)
		require.Len(t, got, 1, "an undone step still touched the item")
		assert.NotNil(t, got[0].Effects[0].UndoneAt)

		none, err := s.Service.LatestMatches(t.Context(), nil)
		require.NoError(t, err)
		assert.NotNil(t, none)
		assert.Empty(t, none)
	})

	synctest.Test(t, func(t *testing.T) {
		s, _ := snoozing(t, autopilotprefs.ModeDryRun, botPR(1))

		got, err := s.Service.LatestMatches(t.Context(), []int64{s.InboxItem(1).ID})
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.True(t, got[0].DryRun)
		assert.Equal(t, autopilot.ReasonWouldRun, got[0].Reason)
	})
}
