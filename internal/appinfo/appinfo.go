// Package appinfo tells the frontend who the app is and takes its first-paint report.
//
// The product name and tagline come from internal/brand, so the frontend never hard-codes
// them. When the window has painted its first frame, the frontend calls
// ReportFirstPaint, which records the startup time once.
package appinfo

import (
	"fmt"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/timing"
)

// Info is the app's identity and version. Every field is a plain value.
type Info struct {
	// Name is the product name.
	Name string `json:"name"`

	// Tagline is the one-line product promise.
	Tagline string `json:"tagline"`

	// Version is the build's version, "dev" for local builds.
	Version string `json:"version"`
}

// FirstPaintRecorder records the first paint once. *timing.Recorder implements it.
type FirstPaintRecorder interface {
	FirstPaint() (m timing.Measurement, first bool, err error)
}

// Config holds a Service's dependencies. Recorder and OnFirstPaint are optional.
type Config struct {
	// Version is the build's version.
	Version string

	// Recorder records the first paint.
	Recorder FirstPaintRecorder

	// OnFirstPaint is called once, with the measurement, after the first paint is
	// recorded. The startup measurement mode uses it to quit after one frame.
	OnFirstPaint func(timing.Measurement)
}

// Service serves the app's identity and records its first paint.
type Service struct {
	version      string
	recorder     FirstPaintRecorder
	onFirstPaint func(timing.Measurement)
}

// NewService returns a Service over cfg.
func NewService(cfg Config) *Service {
	return &Service{version: cfg.Version, recorder: cfg.Recorder, onFirstPaint: cfg.OnFirstPaint}
}

// Info returns the product name, tagline, and version.
func (s *Service) Info() Info {
	return Info{Name: brand.Name, Tagline: brand.Tagline, Version: s.version}
}

// ReportFirstPaint records that the window painted its first frame. Only the first report
// counts; later ones do nothing. An error means the timing log could not be written; the
// measurement was still logged.
func (s *Service) ReportFirstPaint() error {
	if s.recorder == nil {
		return nil
	}

	m, first, err := s.recorder.FirstPaint()
	if first && s.onFirstPaint != nil {
		s.onFirstPaint(m)
	}

	if err != nil {
		return fmt.Errorf("record the first paint: %w", err)
	}

	return nil
}
