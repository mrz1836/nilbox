// Package svcstatus follows the status pages of the services the app depends on (GitHub,
// Claude, and OpenAI's Codex), so the window can say, without getting in the way, when a
// part the user watches has an incident: the inbox, reviews, and checks can lag or fail
// while GitHub has one, and the agents the app launches while Claude or Codex do.
//
// What to watch is the user's (internal/statusprefs): each provider can be turned off,
// and each one's parts ticked. A check is one small GET of a provider's summary through
// the GitHub client's FetchStatus, outside any API, so it sends no token and spends no
// budget; a page that gives an ETag is asked conditionally, and a 304 costs nothing. Each
// provider's last reading is kept raw, in memory alone, and judged against the choices as
// they are on every read of the Status, so a change to what is watched needs no request.
// Relevance is decided by component, never by a page's overall state (see judge).
//
// The Checker, a deferred desktop worker, checks every provider watched once after the
// first paint and then as often as Settings → Service status says (every 30 minutes by
// default, or off), the providers' timers kept together; a provider turned on is checked
// at once. A part watched going down, and coming back, is told as a banner through the
// Notifier (see alerts.go).
package svcstatus

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/flight"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/statusprefs"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// FreshFor is how long a provider's reading stands before CheckNow reads its page again,
// so a click on Check again moments after a check costs nothing.
const FreshFor = 10 * time.Second

// Status is where the providers the user watches stand.
type Status struct {
	// State is the worst of the watched providers' states; StateOff when the check is
	// off or nothing is watched, and StateUnknown before any reading.
	State State `json:"state"`

	// Providers are every provider in the catalog, in its order, each StateOff while not
	// watched.
	Providers []Provider `json:"providers"`

	// CheckedAt is when the oldest reading among the watched providers was taken, RFC
	// 3339, or '' with none.
	CheckedAt string `json:"checkedAt"`
}

// Provider is where one provider stands for what the user watches on it.
type Provider struct {
	ID   statusprefs.ProviderID `json:"id"`
	Name string                 `json:"name"`

	// URL is its status page.
	URL string `json:"url"`

	// State is the worst over Affected and Incidents; StateOff while it isn't watched or
	// the check is off, and StateUnknown with no reading.
	State State `json:"state"`

	// Affected are the parts watched that aren't operational, in catalog order.
	Affected []Part `json:"affected"`

	// Incidents are the incidents and the maintenance under way that name a part watched,
	// or that name no part and are major or critical.
	Incidents []Incident `json:"incidents"`

	// Others counts the incidents and maintenance on the page that don't touch what the
	// user watches.
	Others int `json:"others"`

	// Unlisted are the parts watched that the page no longer lists.
	Unlisted []statusprefs.PartID `json:"unlisted"`

	// CheckedAt is when the reading was taken, RFC 3339, or '' with none.
	CheckedAt string `json:"checkedAt"`

	// Error says why the last check failed, in the window's words, or '' when it worked.
	// A failed check keeps the reading before it.
	Error string `json:"error"`
}

// Part is a part the user watches that isn't operational.
type Part struct {
	ID    statusprefs.PartID `json:"id"`
	Name  string             `json:"name"`
	State State              `json:"state"`
}

// Incident is an incident not yet resolved, or maintenance under way, that touches what
// the user watches.
type Incident struct {
	// Name is its title, e.g. "Elevated errors on Claude API".
	Name string `json:"name"`

	// Stage is where it stands.
	Stage Stage `json:"stage"`

	// State is how bad it is.
	State State `json:"state"`

	// URL is its page on the provider's status page.
	URL string `json:"url"`

	// Parts are the parts watched it names, by name, in catalog order; none when Unscoped.
	Parts []string `json:"parts"`

	// Unscoped reports that it names no component, and counts because it is major or
	// critical.
	Unscoped bool `json:"unscoped"`

	// Maintenance reports that it is maintenance under way, not an incident.
	Maintenance bool `json:"maintenance"`
}

// Fetcher reads a status page. *github.Client implements it.
type Fetcher interface {
	FetchStatus(ctx context.Context, req github.StatusRequest) (github.StatusAnswer, error)
}

// Rules are the settings the check follows, read together.
type Rules struct {
	// Every says how often to check; zero or less is off.
	Every time.Duration

	// Watch is what the user watches.
	Watch statusprefs.Config
}

// Config holds a Service's dependencies. Fetcher and Rules are required; the rest are
// optional.
type Config struct {
	// Fetcher reads the pages.
	Fetcher Fetcher

	// Rules reads the settings as they are now, once per call.
	Rules func(ctx context.Context) Rules

	// Notifier hears the alerts; nil hears none.
	Notifier Notifier

	// Clock stamps each reading and times the checks; nil is the system clock.
	Clock clock.Clock

	// Events hears an event.ServiceStatusUpdated after each pass and each change of the
	// rules.
	Events event.Sink

	// Logger hears the checks that fail; nil discards them.
	Logger *slog.Logger
}

// Service serves where the providers the user watches stand to the window, and checks
// their pages again when asked.
type Service struct {
	fetch    Fetcher
	rules    func(ctx context.Context) Rules
	notifier Notifier
	clock    clock.Clock
	events   event.Sink
	logger   *slog.Logger
	catalog  []statusprefs.Provider
	checks   flight.Group[bool]

	mu       sync.Mutex
	kept     map[statusprefs.ProviderID]*kept
	unlisted map[statusprefs.PartID]bool // the unlisted parts already logged
}

// kept is what the Service keeps of one provider.
type kept struct {
	read    *reading  // the last reading, nil before the first
	etag    string    // the last reading's ETag, or ""
	readAt  time.Time // when the last reading was taken, or confirmed by a 304
	triedAt time.Time // when the last check ended, worked or not; zero before the first
	err     string    // why the last check failed, in the window's words, or ""
	alert   alertState
	gen     int // moves on each time what is kept is dropped, so a check started before is let go
}

// NewService returns a Service over cfg, with no reading yet.
func NewService(cfg Config) *Service {
	return &Service{
		fetch: cfg.Fetcher, rules: cfg.Rules, notifier: cfg.Notifier, clock: clock.Or(cfg.Clock),
		events: event.Or(cfg.Events), logger: logging.Or(cfg.Logger), catalog: statusprefs.Catalog(),
		kept: map[statusprefs.ProviderID]*kept{}, unlisted: map[statusprefs.PartID]bool{},
	}
}

// Status returns where the providers the user watches stand, judged from their last
// readings against the choices as they are now. It reads nothing from the network.
func (s *Service) Status(ctx context.Context) Status {
	return s.status(s.rules(ctx))
}

// CheckNow reads again, in parallel, the page of each provider watched whose last check
// ended FreshFor ago or more, joining a check already running, and returns the Status. A
// check that fails keeps the reading before it and says why in its provider's Error. With
// the check off it reads nothing.
func (s *Service) CheckNow(ctx context.Context) Status {
	rules := s.rules(ctx)
	if rules.Every <= 0 {
		return s.status(rules)
	}

	now := s.clock.Now()

	s.mu.Lock()
	due := s.watched(rules, func(k *kept) bool { return k.triedAt.IsZero() || now.Sub(k.triedAt) >= FreshFor })
	s.mu.Unlock()

	s.checkAll(ctx, due, rules)

	return s.status(rules)
}

// checkAll checks providers in parallel, then tells the window once if any check
// changed what is kept.
func (s *Service) checkAll(ctx context.Context, providers []statusprefs.Provider, rules Rules) {
	if len(providers) == 0 {
		return
	}

	var (
		wg      sync.WaitGroup
		changed atomic.Bool
	)

	for _, p := range providers {
		wg.Go(func() {
			if s.check(ctx, p, rules) {
				changed.Store(true)
			}
		})
	}

	wg.Wait()

	if changed.Load() {
		s.events.Emit(event.ServiceStatusUpdated{})
	}
}

// check reads p's page once, or joins a check of it already running, and reports whether
// what is kept changed. A panic in the check is logged and kept as the check's failure,
// so the provider waits for its next turn rather than being tried again at once.
func (s *Service) check(ctx context.Context, p statusprefs.Provider, rules Rules) (changed bool) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.ErrorContext(ctx, "the status check panicked", "provider", p.ID, "panic", r)
			s.mu.Lock()
			k := s.keptOf(p.ID)
			k.triedAt, k.err = s.clock.Now(), cantRead(p)
			s.mu.Unlock()

			changed = true
		}
	}()

	got, err := s.checks.Do(ctx, string(p.ID), func(ctx context.Context) (bool, error) {
		return s.read(ctx, p, rules)
	})

	return err == nil && got
}

// read reads p's page, keeps what it said, and tells the Notifier of a change worth a
// banner. A failure keeps the reading before it, with why in its error, and is logged. A
// check whose context ends, or whose provider was turned off meanwhile, changes nothing.
func (s *Service) read(ctx context.Context, p statusprefs.Provider, rules Rules) (bool, error) {
	s.mu.Lock()
	k := s.keptOf(p.ID)
	gen, etag := k.gen, k.etag
	s.mu.Unlock()

	answer, err := s.fetch.FetchStatus(ctx, github.StatusRequest{Host: p.Host, Path: p.Path, ETag: etag})
	if ctx.Err() != nil {
		return false, ctx.Err()
	}

	var (
		parsed reading
		words  = unreachable(p)
	)

	if err == nil && !answer.NotModified {
		if parsed, err = parse(p, answer.Body); err != nil {
			words = cantRead(p)
		}
	}

	if err != nil {
		s.logger.WarnContext(ctx, "could not read a status page", "provider", p.ID, "error", err)
		err = uierr.New(words, err)
	}

	s.mu.Lock()

	k = s.keptOf(p.ID)
	if k.gen != gen {
		s.mu.Unlock()

		return false, nil
	}

	k.keep(answer, parsed, err, s.clock.Now())
	alert := s.judgeNew(ctx, p, k, rules, err == nil)

	s.mu.Unlock()

	if alert != nil && s.notifier != nil {
		s.notifier.Alert(ctx, *alert)
	}

	return true, nil
}

// keep keeps the outcome of a check that ended at now: the reading parsed from answer, or
// its 304's confirmation of the reading kept, or err, the check's failure, in the window's
// words, which keeps the reading before it.
func (k *kept) keep(answer github.StatusAnswer, parsed reading, err error, now time.Time) {
	k.triedAt = now

	switch {
	case err != nil:
		k.err = uierr.Message(err)
	case answer.NotModified && k.read == nil:
		// A 304 for a reading no longer kept says nothing; the next check asks afresh.
		k.err, k.etag = "", ""
	default:
		if !answer.NotModified {
			k.read = &parsed
		}

		k.err, k.etag, k.readAt = "", answer.ETag, now
	}
}

// judgeNew judges the reading a check that worked kept of p against rules, logs the parts
// watched it no longer lists, and returns the alert the change is worth, or nil. s.mu is
// held.
func (s *Service) judgeNew(ctx context.Context, p statusprefs.Provider, k *kept, rules Rules, worked bool) *Alert {
	if !worked || k.read == nil {
		return nil
	}

	w, ok := rules.Watch.For(p.ID)
	if !ok || !w.Watched() {
		return nil
	}

	judged := judge(p, *k.read, w.Parts)
	s.logUnlisted(ctx, p, judged.Unlisted)

	return k.alert.next(judged)
}

// logUnlisted logs, once each, the parts watched that p's page no longer lists: the
// catalog needs an edit. s.mu is held.
func (s *Service) logUnlisted(ctx context.Context, p statusprefs.Provider, unlisted []statusprefs.PartID) {
	for _, id := range unlisted {
		if !s.unlisted[id] {
			s.unlisted[id] = true
			s.logger.WarnContext(ctx, "a status page no longer lists a part the user watches", "provider", p.ID,
				"part", id)
		}
	}
}

// applyRules follows a change of what the user watches: a provider no longer watched has
// what is kept of it dropped, its ETag and its alert's baseline included, so turned on
// again it is checked at once; every other provider's alert baseline is set to where it
// stands now, quietly, so a change of the choices never shows a banner. The window is
// told.
func (s *Service) applyRules(ctx context.Context) {
	rules := s.rules(ctx)

	s.mu.Lock()

	for _, p := range s.catalog {
		k := s.keptOf(p.ID)

		w, _ := rules.Watch.For(p.ID)
		if !w.Watched() {
			*k = kept{gen: k.gen + 1}

			continue
		}

		k.alert = alertState{}
		if k.read != nil {
			k.alert = alertState{based: true, level: judge(p, *k.read, w.Parts).State}
		}
	}

	s.mu.Unlock()

	s.events.Emit(event.ServiceStatusUpdated{})
}

// status judges every provider's last reading against rules.
func (s *Service) status(rules Rules) Status {
	out := Status{State: StateOff, Providers: []Provider{}}
	state, watched := StateUnknown, false

	s.mu.Lock()
	defer s.mu.Unlock()

	var oldest time.Time

	for _, p := range s.catalog {
		w, _ := rules.Watch.For(p.ID)
		if rules.Every <= 0 || !w.Watched() {
			out.Providers = append(out.Providers, offProvider(p))

			continue
		}

		pv, readAt := s.provider(p, w)
		oldest = earliest(oldest, readAt)
		state, watched = worst(state, pv.State), true
		out.Providers = append(out.Providers, pv)
	}

	if watched {
		out.State = state
	}

	if !oldest.IsZero() {
		out.CheckedAt = clock.Format(oldest)
	}

	return out
}

// earliest returns the earlier of a and b, a zero time standing for none.
func earliest(a, b time.Time) time.Time {
	if a.IsZero() || !b.IsZero() && b.Before(a) {
		return b
	}

	return a
}

// provider judges what is kept of p, which w watches, and returns when its reading was
// taken, zero with none. s.mu is held.
func (s *Service) provider(p statusprefs.Provider, w statusprefs.Watch) (Provider, time.Time) {
	pv := offProvider(p)
	pv.State = StateUnknown

	k := s.kept[p.ID]
	if k == nil {
		return pv, time.Time{}
	}

	var readAt time.Time

	if k.read != nil {
		pv = judge(p, *k.read, w.Parts)
		pv.CheckedAt, readAt = clock.Format(k.readAt), k.readAt
	}

	pv.Error = k.err

	return pv, readAt
}

// watched returns the providers rules watches whose kept state want says yes to, in
// catalog order. s.mu is held.
func (s *Service) watched(rules Rules, want func(*kept) bool) []statusprefs.Provider {
	out := []statusprefs.Provider{}

	for _, p := range s.catalog {
		if w, _ := rules.Watch.For(p.ID); w.Watched() && want(s.keptOf(p.ID)) {
			out = append(out, p)
		}
	}

	return out
}

// keptOf returns what is kept of provider id, making it when there is none. s.mu is held.
func (s *Service) keptOf(id statusprefs.ProviderID) *kept {
	k, ok := s.kept[id]
	if !ok {
		k = &kept{}
		s.kept[id] = k
	}

	return k
}

// offProvider is p while it isn't watched, or the check is off.
func offProvider(p statusprefs.Provider) Provider {
	return Provider{
		ID: p.ID, Name: p.Name, URL: p.PageURL, State: StateOff,
		Affected: []Part{}, Incidents: []Incident{}, Unlisted: []statusprefs.PartID{},
	}
}

// unreachable is what the window is told when p's page couldn't be read.
func unreachable(p statusprefs.Provider) string {
	return "Couldn't reach " + p.Name + "'s status page."
}

// cantRead is what the window is told when p's page answered something the app can't
// read.
func cantRead(p statusprefs.Provider) string {
	return p.Name + "'s status page answered with something the app can't read."
}
