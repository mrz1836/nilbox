// Package appinfo tells the frontend who the app is and takes its first-paint report.
//
// The product name and tagline come from internal/brand, so the frontend never hard-codes
// them. Info also carries what a bug report needs, the build's versions and the system it
// runs on, and a link that opens a new bug report with those details filled in. When the
// window has painted its first frame, the frontend calls ReportFirstPaint, which records
// the startup time once.
package appinfo

import (
	"fmt"
	"net/url"
	"runtime"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/timing"
)

const (
	// bugReportTemplate is the repository's bug report issue form, under
	// .github/ISSUE_TEMPLATE.
	bugReportTemplate = "bug_report.yml"

	// environmentField is the id of the bug report form's environment field, which the
	// bug report link fills in.
	environmentField = "environment"
)

// Info is the app's identity, its versions, and the system it runs on. Every field is a
// plain value.
type Info struct {
	// Name is the product name.
	Name string `json:"name"`

	// Tagline is the one-line product promise.
	Tagline string `json:"tagline"`

	// Version is the build's git version: the commit, described from the latest tag,
	// with -dirty when the working tree had uncommitted changes, e.g. v0.1.0-3-gabc1234,
	// or "dev" for a run without build metadata.
	Version string `json:"version"`

	// Release is the release version, the latest tag without its v, e.g. 0.1.0; it is
	// 0.0.0 before the first release.
	Release string `json:"release"`

	// Build is the build number, the commit count, e.g. 42.
	Build string `json:"build"`

	// System is the operating system and its version, e.g. macOS 15.1 (24B83).
	System string `json:"system"`

	// Arch is the processor architecture the app was built for, e.g. arm64.
	Arch string `json:"arch"`

	// BugReportURL opens a new bug report on GitHub with the versions and system filled
	// in.
	BugReportURL string `json:"bugReportUrl"`
}

// FirstPaintRecorder records the first paint once. *timing.Recorder implements it.
type FirstPaintRecorder interface {
	FirstPaint() (m timing.Measurement, first bool, err error)
}

// Config holds a Service's dependencies. Everything but Version, Release, and Build is
// optional.
type Config struct {
	// Version is the build's git version.
	Version string

	// Release is the release version.
	Release string

	// Build is the build number.
	Build string

	// System is the operating system and its version. Empty means the one the app runs
	// on.
	System string

	// Recorder records the first paint.
	Recorder FirstPaintRecorder

	// OnFirstPaint is called once, with the measurement, after the first paint is
	// recorded. The startup measurement mode uses it to quit after one frame.
	OnFirstPaint func(timing.Measurement)
}

// Service serves the app's identity and records its first paint.
type Service struct {
	info         Info
	recorder     FirstPaintRecorder
	onFirstPaint func(timing.Measurement)
}

// NewService returns a Service over cfg.
func NewService(cfg Config) *Service {
	system := cfg.System
	if system == "" {
		system = detectSystem()
	}

	info := Info{
		Name:    brand.Name,
		Tagline: brand.Tagline,
		Version: cfg.Version,
		Release: cfg.Release,
		Build:   cfg.Build,
		System:  system,
		Arch:    runtime.GOARCH,
	}
	info.BugReportURL = bugReportURL(info)

	return &Service{info: info, recorder: cfg.Recorder, onFirstPaint: cfg.OnFirstPaint}
}

// Info returns the product's identity, its versions, and the system it runs on.
func (s *Service) Info() Info {
	return s.info
}

// environment returns the details a bug report needs about the build and the system, as
// a Markdown list.
func environment(info Info) string {
	return fmt.Sprintf("- Version: %s (%s)\n- Commit: %s\n- System: %s, %s\n",
		info.Release, info.Build, info.Version, info.System, info.Arch)
}

// bugReportURL returns the link that opens a new bug report with the environment field
// filled in from info.
func bugReportURL(info Info) string {
	q := url.Values{}
	q.Set("template", bugReportTemplate)
	q.Set(environmentField, environment(info))

	return brand.Repository + "/issues/new?" + q.Encode()
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
