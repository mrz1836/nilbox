package periodic

import (
	"context"
	"log/slog"
	"time"
)

// Job is a job RunEvery runs as a worker: how often (its Every), the wake that has it ask
// again, its name in log entries, the run itself, and the logger its failures go to. The
// inbox's scheduler and checks follower, the Release page's checks follower, and the
// worktree sweeper each hold one.
type Job struct {
	every  Every
	wake   chan struct{}
	name   string
	fn     func(context.Context) error
	logger *slog.Logger
}

// NewJob returns a job named name that runs fn as often as every says, logging its
// failures and panics to logger; a nil logger discards them. every must not be nil.
func NewJob(every Every, name string, fn func(context.Context) error, logger *slog.Logger) *Job {
	return &Job{every: every, wake: make(chan struct{}, 1), name: name, fn: fn, logger: logger}
}

// Run runs the job through RunEvery until ctx is canceled, and returns once the run in
// flight has returned.
func (j *Job) Run(ctx context.Context) {
	RunEvery(ctx, j.every, j.wake, j.name, j.fn, j.logger)
}

// Wake has the job ask its Every again at once, as when the user changed the setting. It
// never blocks: a wake while another still waits to be read is dropped, since that one
// already has Run ask again. A nil job, as a worker's zero value holds, ignores it.
func (j *Job) Wake() {
	if j == nil {
		return
	}

	select {
	case j.wake <- struct{}{}:
	default:
	}
}

// Interval reports how long the job waits after a run, as its Every says now.
func (j *Job) Interval(ctx context.Context) time.Duration {
	return j.every(ctx)
}
