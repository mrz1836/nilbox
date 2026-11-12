package release

import (
	"time"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/progress"
)

// progressEvery is how often, at most, a refresh of the list tells the window how far it
// has got.
const progressEvery = progress.Every

// listProgress tells the window how far one refresh of the list has got, as
// event.ReleasesProgress: the phase under way and its counts (see progress.Tracker).
type listProgress = progress.Tracker[event.ReleasesPhase]

// newListProgress returns the progress of a refresh that tells events at most once per
// every.
func newListProgress(events event.Sink, every time.Duration) *listProgress {
	return progress.New(events, every, func(phase event.ReleasesPhase, done, total int) event.Event {
		return event.ReleasesProgress{Phase: phase, Done: done, Total: total}
	})
}
