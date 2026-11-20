package core

import (
	"log/slog"

	"github.com/mrz1836/nilbox/internal/describeit"
	"github.com/mrz1836/nilbox/internal/event"
)

// newDescribeIt builds Describe it in the data folder's describe folder, with the app's
// own executable as the tool mode it runs, and its background worker.
func newDescribeIt(opts Options, logger *slog.Logger) (*describeit.Service, *describeit.Worker) {
	svc := describeit.New(describeit.Config{
		Dir: opts.Paths.Describe, Executable: ownExecutable, Clock: opts.Clock, Sink: event.Or(opts.Events),
		Logger: logger,
	})

	return svc, describeit.NewWorker(svc)
}
