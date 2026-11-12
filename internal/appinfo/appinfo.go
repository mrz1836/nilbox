// Package appinfo tells the frontend who the app is and takes its first-paint report.
//
// The product name and tagline come from internal/brand, so the frontend never hard-codes
// them. Info also carries what a bug report needs, the build's versions and the system it
// runs on; AgentVersions reads the installed coding agents' versions, ToolVersions the
// other tools' (the GitHub CLI), and BugReportURL opens a new bug report with all of them
// filled in, and with the app's state as its StateReader words it: the settings and the
// integrations, with nothing personal in them. The user reads the report on GitHub before
// sending it, and the app sends nothing itself. When the
// database could not be read at startup and was set aside, or the prompt library and
// launch defaults were brought back into a new one, Info says so, so the window can tell the user once it
// loads: the window does not exist yet when that happens, so an event would be lost. When the window has painted its first frame, the frontend calls
// ReportFirstPaint, which records the startup time once.
package appinfo

import (
	"context"
	"fmt"
	"net/url"
	"runtime"
	"strings"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/execpath"
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

// Info is the app's identity, its versions, the system it runs on, and whether its data
// was reset at startup.
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

	// DataReset is set when the database could not be read at startup, so it was set
	// aside and the app started with an empty one; it is nil otherwise.
	DataReset *DataReset `json:"dataReset,omitempty"`

	// Restored is set when the prompt library and the launch defaults were lost at
	// startup, with the database they were in, and were brought back; it is nil
	// otherwise.
	Restored *Restored `json:"restored,omitempty"`
}

// Restored says what of the user's own was brought back at startup, and from where.
type Restored struct {
	// Prompts and LaunchDefaults count the prompts and the org and repository launch
	// defaults that came back.
	Prompts        int `json:"prompts"`
	LaunchDefaults int `json:"launchDefaults"`

	// From is the file they came from, with ~ for the home directory: the database set
	// aside, or the copy of the prompt library kept beside the database.
	From string `json:"from"`
}

// DataReset says where a database that could not be read was set aside, and why.
type DataReset struct {
	// Path is where the database was moved, with ~ for the home directory.
	Path string `json:"path"`

	// Reason says why it could not be read.
	Reason ResetReason `json:"reason"`
}

// ResetReason says why the database could not be read. The values are the store
// package's reasons for setting a database aside.
type ResetReason string

// Why the database could not be read.
const (
	// ResetCorrupt is a file that is not a database, or is damaged.
	ResetCorrupt ResetReason = "corrupt"

	// ResetUnmanaged is a database with tables the app did not create.
	ResetUnmanaged ResetReason = "unmanaged"

	// ResetHistory is a migration history this build disagrees with.
	ResetHistory ResetReason = "history"
)

// StateReader words what a bug report says of the app's state, for whoever reads the
// report: the settings and the integrations, as sections of Markdown lines, each with
// nothing personal in it, so no account, repository, organization, profile name, path,
// or pattern of the user's own. Core adapts the settings and integrations services.
type StateReader interface {
	BugReportState(ctx context.Context) []StateSection
}

// StateSection is one part of a bug report's state, such as the settings: its title and
// its lines, each a Markdown list item's text.
type StateSection struct {
	Title string
	Lines []string
}

// FirstPaintRecorder records the first paint once. *timing.Recorder implements it.
type FirstPaintRecorder interface {
	FirstPaint() (m timing.Measurement, first bool, err error)
}

// Config holds a Service's dependencies. Everything but Version, Release, and Build is
// optional; without Finder and Runner, no coding agent counts as installed.
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

	// Events hears an event.FirstPaint, once, after the first paint is recorded. The
	// startup measurement mode uses it to quit after one frame.
	Events event.Sink

	// DataReset is set when the database was set aside at startup.
	DataReset *DataReset

	// Restored is set when the prompt library and launch defaults were brought back at
	// startup.
	Restored *Restored

	// Finder finds the coding agents' and the tools' commands, and Runner runs their
	// --version.
	Finder execpath.Finder
	Runner execpath.Runner

	// Home is the user's home directory, shown as ~ in a command's path.
	Home string

	// State words the app's state for a bug report; without it, a report holds only the
	// versions.
	State StateReader
}

// Service serves the app's identity and the coding agents' and tools' versions, and
// records its first paint.
type Service struct {
	info     Info
	recorder FirstPaintRecorder
	events   event.Sink
	versions *versions
	state    StateReader
}

// NewService returns a Service over cfg.
func NewService(cfg Config) *Service {
	system := cfg.System
	if system == "" {
		system = detectSystem()
	}

	info := Info{
		Name:      brand.Name,
		Tagline:   brand.Tagline,
		Version:   cfg.Version,
		Release:   cfg.Release,
		Build:     cfg.Build,
		System:    system,
		Arch:      runtime.GOARCH,
		DataReset: cfg.DataReset,
		Restored:  cfg.Restored,
	}

	return &Service{
		info: info, recorder: cfg.Recorder, events: event.Or(cfg.Events),
		versions: &versions{finder: cfg.Finder, run: cfg.Runner, home: cfg.Home}, state: cfg.State,
	}
}

// Info returns the product's identity, its versions, the system it runs on, and whether
// its data was reset at startup.
func (s *Service) Info() Info {
	return s.info
}

// AgentVersions returns each coding agent's installed version, in the agents' order.
// Each is read with `<command> --version` the first time it is asked for, and again only
// once its command changes, as when the agent updates itself, so asking again runs
// nothing. An agent whose version cannot be read says why.
func (s *Service) AgentVersions(ctx context.Context) []AgentVersion {
	return s.versions.all(ctx)
}

// ToolVersions returns each tool's installed version, other than the coding agents', in
// the tools' order: the GitHub CLI, whose sign-in the app borrows. Each is read and kept
// as AgentVersions reads and keeps the agents'.
func (s *Service) ToolVersions(ctx context.Context) []ToolVersion {
	return s.versions.tools(ctx)
}

// BugReportURL returns the link that opens a new bug report on GitHub with the app's
// versions, the system, each coding agent's and tool's version, and the app's settings
// and integrations filled in.
func (s *Service) BugReportURL(ctx context.Context) string {
	var state []StateSection
	if s.state != nil {
		state = s.state.BugReportState(ctx)
	}

	return bugReportURL(s.info, s.versions.all(ctx), s.versions.tools(ctx), state)
}

// environment returns the details a bug report needs about the build, the system, the
// coding agents, and the tools, as a Markdown list, and then the app's state, folded
// away under a summary so the report stays short to read.
func environment(info Info, agents []AgentVersion, tools []ToolVersion, state []StateSection) string {
	var b strings.Builder

	fmt.Fprintf(&b, "- Version: %s (%s)\n- Commit: %s\n- System: %s, %s\n",
		info.Release, info.Build, info.Version, info.System, info.Arch)

	for _, a := range agents {
		fmt.Fprintf(&b, "- %s: %s\n", a.Name, versionText(a.Installed, a.Version))
	}

	for _, t := range tools {
		fmt.Fprintf(&b, "- %s: %s\n", t.Name, versionText(t.Installed, t.Version))
	}

	if len(state) == 0 {
		return b.String()
	}

	b.WriteString("\n<details><summary>Settings and integrations</summary>\n")

	for _, section := range state {
		fmt.Fprintf(&b, "\n**%s**\n\n", section.Title)

		for _, line := range section.Lines {
			fmt.Fprintf(&b, "- %s\n", line)
		}
	}

	b.WriteString("\n</details>\n")

	return b.String()
}

// bugReportURL returns the link that opens a new bug report with the environment field
// filled in from info and the agents' and tools' versions.
func bugReportURL(info Info, agents []AgentVersion, tools []ToolVersion, state []StateSection) string {
	q := url.Values{}
	q.Set("template", bugReportTemplate)
	q.Set(environmentField, environment(info, agents, tools, state))

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
	if first {
		s.events.Emit(event.FirstPaint{Measurement: m})
	}

	if err != nil {
		return fmt.Errorf("record the first paint: %w", err)
	}

	return nil
}
