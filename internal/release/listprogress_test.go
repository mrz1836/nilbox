package release

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/testutil"
)

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

func TestARefreshTellsHowFarItHasGot(t *testing.T) {
	t.Parallel()

	r := newRig()
	r.github.batch = 2
	r.svc.progressEvery = 0

	_, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	asking := func(done int) event.ReleasesProgress {
		return event.ReleasesProgress{Phase: event.ReleasesAskingGitHub, Done: done, Total: 7}
	}
	comparing := func(done int) event.ReleasesProgress {
		return event.ReleasesProgress{Phase: event.ReleasesComparing, Done: done, Total: 2}
	}
	checking := func(done int) event.ReleasesProgress {
		return event.ReleasesProgress{Phase: event.ReleasesCheckingClones, Done: done, Total: 2}
	}

	assert.Equal(t, []event.ReleasesProgress{
		{Phase: event.ReleasesFindingClones},
		asking(0), asking(2), asking(4), asking(6), asking(7),
		comparing(0), comparing(2),
		checking(0), checking(1), checking(2),
	}, told(r.events), "each round counts its repositories off by batch, then the unreleased clones one by one")

	all := r.events.All()
	assert.Equal(t, event.ReleasesUpdated{}, all[len(all)-1], "the list lands after its last count")
}

func TestARefreshWithNothingToCompareOrCheckSaysSo(t *testing.T) {
	t.Parallel()

	r := newRig()
	r.svc.progressEvery = 0
	r.github.compares = map[string]Compare{}
	delete(r.github.statuses, name("sdk").Key())
	delete(r.github.statuses, name("web").Key())

	_, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	assert.Equal(t, []event.ReleasesProgress{
		{Phase: event.ReleasesFindingClones},
		{Phase: event.ReleasesAskingGitHub, Total: 7},
		{Phase: event.ReleasesAskingGitHub, Done: 7, Total: 7},
	}, told(r.events), "no round without repositories to count")
}

func TestProgressIsToldAFewTimesASecond(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		events := &testutil.Events{}
		p := newListProgress(events, progressEvery)

		p.start(event.ReleasesAskingGitHub, 151)
		assert.Len(t, told(events), 1, "the first is told at once")

		// A batch answered every 50 ms for two seconds.
		for range 40 {
			time.Sleep(50 * time.Millisecond)
			p.add(3)
		}

		time.Sleep(progressEvery)
		synctest.Wait()

		got := told(events)
		assert.LessOrEqual(t, len(got), 1+int(2*time.Second/progressEvery)+1, "at most four times a second")
		assert.Equal(t, event.ReleasesProgress{Phase: event.ReleasesAskingGitHub, Done: 120, Total: 151}, got[len(got)-1],
			"the latest counts are told once the wait is over")

		p.add(1000)
		synctest.Wait()

		last := told(events)
		if len(last) > len(got) {
			assert.Equal(t, 151, last[len(last)-1].Done, "never past the total")
		}
	})
}

func TestProgressEndsWithItsRefresh(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		events := &testutil.Events{}
		p := newListProgress(events, progressEvery)

		p.start(event.ReleasesCheckingClones, 3)
		p.add(1)
		p.end()

		time.Sleep(2 * progressEvery)
		synctest.Wait()
		assert.Equal(t, []event.ReleasesProgress{{Phase: event.ReleasesCheckingClones, Total: 3}}, told(events),
			"a count held back is dropped once the refresh ends")

		p.add(1)
		p.start(event.ReleasesAskingGitHub, 2)
		assert.Len(t, told(events), 1, "and nothing more is told")
	})
}
