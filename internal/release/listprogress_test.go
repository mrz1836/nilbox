package release

import (
	"testing"

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
