package desktop_test

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/inbox"
)

// snoozedFor returns an inbox, refreshed now, with one item that needs the user and one
// snoozed for d.
func snoozedFor(d time.Duration) inbox.Snapshot {
	now := time.Now()

	return inbox.Snapshot{
		LastRefreshedAt: clock.Format(now),
		ReviewRequested: inbox.List{Shown: 2, Total: 2, Items: []inbox.Item{
			{ID: 1, Kind: "pr", Number: 12, Title: "Add the widget cache", Repo: "acme-org/example-repo", URL: exampleURL},
			{
				ID: 2, Kind: "pr", Number: 13, Title: "Fix the login redirect", Repo: "acme-org/example-repo",
				SnoozedUntil: clock.Format(now.Add(d)),
			},
		}},
	}
}

// labels returns the label of every drawing of the icon so far.
func labels(r *rig) []string {
	draws := r.tray.drawn()
	out := make([]string, 0, len(draws))

	for _, d := range draws {
		out = append(out, d.Label)
	}

	return out
}

func TestRedrawsFoldIntoOne(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		gate := make(chan struct{})
		r.inbox.hold(gate)
		s := r.shell()

		s.Start(t.Context())
		s.Launched()
		synctest.Wait()

		require.Equal(t, 1, r.inbox.readCount(), "the first redraw is under way")

		for range 10 {
			s.Emit(event.InboxUpdated{})
		}

		synctest.Wait()
		close(gate)
		synctest.Wait()

		assert.Len(t, r.page.events(), 10, "the page hears every change")
		assert.Equal(t, 2, r.inbox.readCount(), "ten changes during a redraw make one more")
		assert.Len(t, r.tray.drawn(), 2)

		require.NoError(t, s.Stop())
	})
}

func TestASnoozeEndingRedrawsTheIcon(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		r.inbox.set(snoozedFor(time.Hour), nil)
		s := r.shell()

		s.Start(t.Context())
		s.Launched()
		synctest.Wait()

		assert.Equal(t, []string{"1"}, labels(r), "the snoozed item is not counted")

		time.Sleep(time.Hour)
		synctest.Wait()
		assert.Equal(t, []string{"1"}, labels(r), "nothing is redrawn the moment the snooze ends")

		time.Sleep(time.Second)
		synctest.Wait()
		assert.Equal(t, []string{"1", "2"}, labels(r), "a second later the icon counts it")

		time.Sleep(24 * time.Hour)
		synctest.Wait()
		assert.Len(t, labels(r), 2, "with no snooze running, no timer is set")

		require.NoError(t, s.Stop())
	})
}

func TestEachRedrawReplacesTheSnoozeTimer(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		r.inbox.set(snoozedFor(time.Hour), nil)
		s := r.shell()

		s.Start(t.Context())
		s.Launched()
		synctest.Wait()

		// The snooze was undone: the next redraw finds nothing snoozed.
		r.inbox.set(snoozedFor(0), nil)
		s.Emit(event.InboxUpdated{})
		synctest.Wait()

		time.Sleep(2 * time.Hour)
		synctest.Wait()

		assert.Equal(t, []string{"1", "2"}, labels(r), "the old snooze's timer did not fire")

		require.NoError(t, s.Stop())
	})
}

func TestHidingTheIconStopsItsRedraws(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		r.inbox.set(snoozedFor(time.Hour), nil)
		s := r.shell()

		s.Start(t.Context())
		s.Launched()
		synctest.Wait()

		s.Emit(event.MenuBarChanged{On: false})
		assert.False(t, s.HidesOnClose(), "without the icon, closing the window quits")

		s.Emit(event.InboxUpdated{})
		time.Sleep(2 * time.Hour)
		synctest.Wait()

		assert.Equal(t, []string{`tray shown`, `tray drawn: "1"`, `tray hidden`}, r.lines("tray"))
		assert.Equal(t, 1, r.inbox.readCount(), "a hidden icon reads nothing")

		s.Emit(event.MenuBarChanged{On: true})
		synctest.Wait()

		assert.Equal(t, []string{`tray shown`, `tray drawn: "1"`, `tray hidden`, `tray shown`, `tray drawn: "2"`}, r.lines("tray"))
		assert.True(t, s.HidesOnClose())

		require.NoError(t, s.Stop())
	})
}

func TestAnInboxThatCannotBeReadIsNoted(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		r.inbox.set(inbox.Snapshot{}, errFakeInbox)
		s := r.shell()

		s.Start(t.Context())
		s.Launched()
		synctest.Wait()

		assert.Empty(t, r.tray.drawn())
		assert.Contains(t, r.logs.String(),
			`level=WARN msg="could not read the inbox for the menu bar" error="`+errFakeInbox.Error()+`"`)

		require.NoError(t, s.Stop())
	})
}

func TestARedrawCutShortByStopIsNotNoted(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		r.inbox.hold(make(chan struct{}))
		s := r.shell()

		s.Start(t.Context())
		s.Launched()
		synctest.Wait()

		require.NoError(t, s.Stop())

		assert.Empty(t, r.tray.drawn())
		assert.Empty(t, r.logs.String(), "a read the shutdown canceled is not a failure")
	})
}
