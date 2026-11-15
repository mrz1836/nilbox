// Package ghstatus follows GitHub's own status page, www.githubstatus.com, so the window
// can say, without getting in the way, when GitHub has an incident: the inbox, reviews,
// and checks can lag or fail while it does.
//
// A check is one small GET of the page's summary through the GitHub client's FetchStatus.
// The page isn't the API, so the check sends no token, spends no point of any budget, and
// carries nothing of the user's. The Checker, a deferred desktop worker, checks once after
// the first paint and then as often as Settings → Refreshing → GitHub status says (every
// 30 minutes by default, or off), woken on event.IntervalsChanged so a change applies at
// once. The reading is kept in memory alone: a start shows nothing until its first check
// lands, and with the check off the Service says so and reads nothing.
package ghstatus

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/flight"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/periodic"
	"github.com/mrz1836/nilbox/internal/uierr"
)

const (
	// PageURL is GitHub's status page, which the window opens for the details.
	PageURL = "https://" + github.DefaultStatusHost + "/"

	// FreshFor is how long a reading stands before CheckNow reads the page again, so a
	// click on Check again moments after a check costs nothing.
	FreshFor = 10 * time.Second

	// unreachable is what the window is told when a check fails.
	unreachable = "Couldn't reach GitHub's status page."

	// checkKey names the one check that runs at a time.
	checkKey = "check"

	// jobName labels the Checker's runs in the log.
	jobName = "GitHub status check"
)

// State is where GitHub stands, as its status page says, or why there is no reading.
type State string

// Where GitHub stands.
const (
	// StateOff is the check turned off in Settings → Refreshing.
	StateOff State = "off"

	// StateUnknown is no reading yet: before the first check lands, or while every
	// check has failed.
	StateUnknown State = "unknown"

	// StateOperational is every system operational.
	StateOperational State = "operational"

	// StateMinor is a minor incident, such as degraded performance.
	StateMinor State = "minor"

	// StateMajor is a major incident, such as a partial outage.
	StateMajor State = "major"

	// StateCritical is a critical incident, such as a major outage.
	StateCritical State = "critical"

	// StateMaintenance is maintenance under way.
	StateMaintenance State = "maintenance"
)

// Incident is an incident on GitHub not yet resolved, or maintenance under way.
type Incident struct {
	// Name is its title, e.g. "Incident with Actions".
	Name string `json:"name"`

	// Status is where it stands, as the page words it: investigating, identified,
	// monitoring, in_progress, ...
	Status string `json:"status"`

	// Impact is how bad it is: none, minor, major, critical, or maintenance.
	Impact string `json:"impact"`

	// URL is its page on GitHub's status page.
	URL string `json:"url"`
}

// Component is a part of GitHub that isn't operational.
type Component struct {
	// Name is its name, e.g. "Actions".
	Name string `json:"name"`

	// Status is its state, as the page words it: degraded_performance, partial_outage,
	// major_outage, or under_maintenance.
	Status string `json:"status"`
}

// Status is the last reading of GitHub's status page.
type Status struct {
	// State is where GitHub stands, or why there is no reading.
	State State `json:"state"`

	// Description is the page's own words for it, e.g. "Partial System Outage", or ''
	// with no reading.
	Description string `json:"description"`

	// Incidents are the incidents not yet resolved and the maintenance under way.
	Incidents []Incident `json:"incidents"`

	// Components are the parts of GitHub that aren't operational.
	Components []Component `json:"components"`

	// CheckedAt is when the reading was taken, RFC 3339, or '' with none.
	CheckedAt string `json:"checkedAt"`

	// Error says why the last check failed, in the window's words, or '' when it worked.
	// A failed check keeps the reading before it.
	Error string `json:"error"`

	// URL is GitHub's status page.
	URL string `json:"url"`
}

// Fetcher reads GitHub's status page. *github.Client implements it.
type Fetcher interface {
	FetchStatus(ctx context.Context) (*github.ServiceStatus, error)
}

// Config holds a Service's dependencies. Every is required; the rest are optional.
type Config struct {
	// Fetcher reads the page.
	Fetcher Fetcher

	// Every says how often to check, as set now; zero or less is off.
	Every periodic.Every

	// Clock stamps each reading; nil is the system clock.
	Clock clock.Clock

	// Events hears an event.GitHubStatusUpdated after each check.
	Events event.Sink

	// Logger hears the checks that fail; nil discards them.
	Logger *slog.Logger
}

// Service serves the last reading of GitHub's status page to the window, and checks it
// again when asked.
type Service struct {
	fetch  Fetcher
	every  periodic.Every
	clock  clock.Clock
	events event.Sink
	logger *slog.Logger
	checks flight.Group[struct{}]

	mu     sync.Mutex
	last   Status
	readAt time.Time // when the last check ended, worked or not; zero before the first
}

// NewService returns a Service over cfg, with no reading yet.
func NewService(cfg Config) *Service {
	return &Service{
		fetch: cfg.Fetcher, every: cfg.Every, clock: clock.Or(cfg.Clock), events: event.Or(cfg.Events),
		logger: cfg.Logger, last: unknown(),
	}
}

// Status returns the last reading of GitHub's status page, or StateOff while the check is
// off. It reads nothing from the network.
func (s *Service) Status(ctx context.Context) Status {
	if s.every(ctx) <= 0 {
		return off()
	}

	return s.reading()
}

// CheckNow reads GitHub's status page now, unless a check ended within FreshFor, and
// returns the reading; a check already running is joined. A check that fails keeps the
// reading before it and says why in its Error. With the check off it reads nothing and
// returns StateOff.
func (s *Service) CheckNow(ctx context.Context) Status {
	if s.every(ctx) <= 0 {
		return off()
	}

	s.mu.Lock()
	fresh := !s.readAt.IsZero() && s.clock.Now().Sub(s.readAt) < FreshFor
	s.mu.Unlock()

	if !fresh {
		s.check(ctx)
	}

	return s.reading()
}

// run is the Checker's job: one check. A failure is logged by check and kept in the
// reading, so the job itself never fails.
func (s *Service) run(ctx context.Context) error {
	s.check(ctx)

	return nil
}

// check reads the page once, or joins a check already running, keeps the reading, and
// tells the window. A failure keeps the reading before it, with why in its Error, and is
// logged. A check whose context ends changes nothing.
func (s *Service) check(ctx context.Context) {
	_, _ = s.checks.Do(ctx, checkKey, func(ctx context.Context) (struct{}, error) {
		read, err := s.fetch.FetchStatus(ctx)
		if ctx.Err() != nil {
			return struct{}{}, ctx.Err()
		}

		if err != nil {
			logging.Or(s.logger).WarnContext(ctx, "could not read GitHub's status page", "error", err)
		}

		s.mu.Lock()

		if err != nil {
			s.last.Error = uierr.Message(uierr.New(unreachable, err))
		} else {
			s.last = fromPage(read, s.clock.Now())
		}

		s.readAt = s.clock.Now()
		s.mu.Unlock()

		s.events.Emit(event.GitHubStatusUpdated{})

		return struct{}{}, nil
	})
}

// reading returns a copy of the last reading.
func (s *Service) reading() Status {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.last.clone()
}

// fromPage turns what the page said into a Status taken at.
func fromPage(read *github.ServiceStatus, at time.Time) Status {
	status := Status{
		State: stateOf(read.Indicator), Description: read.Description,
		Incidents: make([]Incident, 0, len(read.Incidents)), Components: make([]Component, 0, len(read.Components)),
		CheckedAt: clock.Format(at), URL: PageURL,
	}

	for _, i := range read.Incidents {
		status.Incidents = append(status.Incidents, Incident{Name: i.Name, Status: i.Status, Impact: i.Impact, URL: i.URL})
	}

	for _, c := range read.Components {
		status.Components = append(status.Components, Component{Name: c.Name, Status: c.Status})
	}

	return status
}

// stateOf turns the page's overall indicator into a State. One this build doesn't know
// counts as minor, so the window still shows the page's own words for it.
func stateOf(indicator string) State {
	switch indicator {
	case github.StatusNone:
		return StateOperational
	case github.StatusMinor:
		return StateMinor
	case github.StatusMajor:
		return StateMajor
	case github.StatusCritical:
		return StateCritical
	case github.StatusMaintenance:
		return StateMaintenance
	default:
		return StateMinor
	}
}

// unknown is the Status before the first reading.
func unknown() Status {
	return Status{State: StateUnknown, Incidents: []Incident{}, Components: []Component{}, URL: PageURL}
}

// off is the Status while the check is off.
func off() Status {
	return Status{State: StateOff, Incidents: []Incident{}, Components: []Component{}, URL: PageURL}
}

// clone returns a copy of s that shares no list with it.
func (s Status) clone() Status {
	s.Incidents = slices.Clone(s.Incidents)
	s.Components = slices.Clone(s.Components)

	return s
}

// Checker checks GitHub's status page as often as the settings say. It is a desktop
// worker, never bound.
type Checker struct {
	s    *Service
	wake chan struct{}
}

// NewChecker returns a Checker over s.
func NewChecker(s *Service) *Checker {
	return &Checker{s: s, wake: make(chan struct{}, 1)}
}

// Run checks now and then as often as the settings say until ctx is canceled, then waits
// for a check in flight to return. While the check is off it reads nothing.
func (c *Checker) Run(ctx context.Context) {
	periodic.RunEvery(ctx, c.s.every, c.wake, jobName, c.s.run, c.s.logger)
}

// Wake has the Checker read how often to check again, as when the user changed it.
func (c *Checker) Wake() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}
