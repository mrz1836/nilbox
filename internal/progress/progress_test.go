package progress_test

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/progress"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// newTracker returns a Tracker that tells events as event.ReleasesProgress, every
// progress.Every at most.
func newTracker(events *testutil.Events) *progress.Tracker[event.ReleasesPhase] {
	return progress.New(events, progress.Every, func(phase event.ReleasesPhase, done, total int) event.Event {
		return event.ReleasesProgress{Phase: phase, Done: done, Total: total}
	})
}

// told returns the progress among events, in order.
func told(events *testutil.Events) []event.ReleasesProgress {
	var out []event.ReleasesProgress

	for _, e := range events.All() {
		if p, ok := e.(event.ReleasesProgress); ok {
			out = append(out, p)
		}
	}

	return out
}

func TestProgressIsToldAFewTimesASecond(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		events := &testutil.Events{}
		p := newTracker(events)

		p.Start(event.ReleasesAskingGitHub, 151)
		assert.Len(t, told(events), 1, "the first is told at once")

		// A batch answered every 50 ms for two seconds.
		for range 40 {
			time.Sleep(50 * time.Millisecond)
			p.Add(3)
		}

		time.Sleep(progress.Every)
		synctest.Wait()

		got := told(events)
		assert.LessOrEqual(t, len(got), 1+int(2*time.Second/progress.Every)+1, "at most four times a second")
		assert.Equal(t, event.ReleasesProgress{Phase: event.ReleasesAskingGitHub, Done: 120, Total: 151}, got[len(got)-1],
			"the latest counts are told once the wait is over")

		p.Add(1000)
		synctest.Wait()

		last := told(events)
		if len(last) > len(got) {
			assert.Equal(t, 151, last[len(last)-1].Done, "never past the total")
		}
	})
}

func TestProgressEndsWithItsRead(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		events := &testutil.Events{}
		p := newTracker(events)

		p.Start(event.ReleasesCheckingClones, 3)
		p.Add(1)
		p.End()

		time.Sleep(2 * progress.Every)
		synctest.Wait()
		assert.Equal(t, []event.ReleasesProgress{{Phase: event.ReleasesCheckingClones, Total: 3}}, told(events),
			"a count held back is dropped once the read ends")

		p.Add(1)
		p.Start(event.ReleasesAskingGitHub, 2)
		assert.Len(t, told(events), 1, "and nothing more is told")
	})
}

func TestANilTrackerTellsNothing(t *testing.T) {
	t.Parallel()

	var p *progress.Tracker[event.ReleasesPhase]

	assert.NotPanics(t, func() {
		p.Start(event.ReleasesAskingGitHub, 2)
		p.Add(1)
		p.End()
	})
}

func TestATrackerWithoutASinkTellsNothing(t *testing.T) {
	t.Parallel()

	p := progress.New(nil, 0, func(phase event.ReleasesPhase, done, total int) event.Event {
		return event.ReleasesProgress{Phase: phase, Done: done, Total: total}
	})

	assert.NotPanics(t, func() {
		p.Start(event.ReleasesAskingGitHub, 2)
		p.Add(1)
		p.End()
	})
}
