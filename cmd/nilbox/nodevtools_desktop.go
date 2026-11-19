//go:build desktop && !devtools

package main

import (
	"log/slog"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/mrz1836/nilbox/internal/event"
)

// devKit is the inspector's place in a build without it: a release, or a rehearsal of
// one, which magex never builds with the devtools tag. Every method leaves the app as it
// is; devtools_desktop.go has the inspector a build from source carries.
type devKit struct{}

// newDevKit returns the inspector that isn't there.
func newDevKit() *devKit {
	return &devKit{}
}

// logger returns logger as it is.
func (*devKit) logger(logger *slog.Logger) *slog.Logger {
	return logger
}

// sink returns next as it is.
func (*devKit) sink(next event.Sink) event.Sink {
	return next
}

// configure leaves the app's options as they are.
func (*devKit) configure(*application.Options) {}

// start does nothing.
func (*devKit) start(*application.App, launch) {}

// stop does nothing.
func (*devKit) stop() {}
