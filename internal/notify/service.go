package notify

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/notifyprefs"
	"github.com/mrz1836/nilbox/internal/prefs"
)

var (
	// ErrBannersUnavailable is returned by SendTest when this build cannot show banners.
	ErrBannersUnavailable = errors.New("this build cannot show notifications; they need the packaged app")

	// ErrBannersOff is returned by SendTest when the user has not allowed banners.
	ErrBannersOff = errors.New("notifications are off for this app in System Settings")
)

// Config holds the dependencies of a Dispatcher and a Service. Pool is required; Poster,
// Events, Clock, and Logger are optional: without a Poster no banner is shown, and the
// log is still kept.
type Config struct {
	// Pool is the database's connection pool. It holds the log, and the prefs the
	// Dispatcher reads which banners are on from.
	Pool *sql.DB

	Poster Poster

	// Events hears an event.NotificationsUpdated whenever the log changed, so the window
	// reads it again.
	Events event.Sink

	Clock  clock.Clock
	Logger *slog.Logger
}

// base is what the Dispatcher and the Service share.
type base struct {
	store  *Store
	prefs  *prefs.Store
	poster Poster
	events event.Sink
	clock  clock.Clock
	logger *slog.Logger
}

// newBase fills in the defaults.
func newBase(cfg Config) base {
	b := base{
		store: NewStore(cfg.Pool), prefs: prefs.New(cfg.Pool, cfg.Clock), poster: cfg.Poster, events: event.Or(cfg.Events),
		clock: clock.Or(cfg.Clock), logger: logging.Or(cfg.Logger),
	}

	return b
}

// notify says the log changed.
func (b base) notify() {
	b.events.Emit(event.NotificationsUpdated{})
}

// bannersOff reads the kinds whose banners are off.
func (b base) bannersOff(ctx context.Context) ([]notifyprefs.Kind, error) {
	stored, err := b.prefs.GetMany(ctx, notifyprefs.KeyBannersOff, notifyprefs.KeyBannersOn)
	if err != nil {
		return nil, fmt.Errorf("read the notification settings: %w", err)
	}

	return notifyprefs.DecodeBannersOff(stored[notifyprefs.KeyBannersOff], stored[notifyprefs.KeyBannersOn]), nil
}

// Dispatcher records the inbox's arrivals and shows their banners. It is the inbox's
// inbox.ArrivalSink. It also records the notifications about no item, such as a release
// that reached GitHub, and shows theirs (Notify), or a run that failed, whose banners it
// gathers (Gather). It is not bound to the window.
type Dispatcher struct {
	base

	// held holds the banners Gather was given.
	held *gathering
}

// NewDispatcher returns a Dispatcher.
func NewDispatcher(cfg Config) *Dispatcher {
	d := &Dispatcher{base: newBase(cfg)}
	d.held = newGathering(d.show)

	return d
}

// Arrived records a notification for each arrival, tells the window, and shows banners for
// the kinds that are on, if the user allows banners. A failure is logged: the refresh that
// found the arrivals has already been stored, and nothing is gained by failing it.
func (d *Dispatcher) Arrived(ctx context.Context, arrivals []inbox.Arrival) {
	rows, err := d.store.Record(ctx, d.clock.Now(), arrivals)
	if err != nil {
		d.logger.WarnContext(ctx, "could not record the notifications", "error", err)

		return
	}

	if len(rows) == 0 {
		return
	}

	d.notify()
	d.show(ctx, rows)
}

// Notify records entry, a notification about no item, tells the window, and shows its
// banner when its kind is on and the user allows banners. A failure is logged: what it
// tells of has happened anyway.
func (d *Dispatcher) Notify(ctx context.Context, entry Entry) {
	row, err := d.store.RecordEvent(ctx, d.clock.Now(), entry)
	if err != nil {
		d.logger.WarnContext(ctx, "could not record the notification", "kind", string(entry.Kind), "error", err)

		return
	}

	d.notify()
	d.show(ctx, []Notification{row})
}

// Gather records entry, a notification about no item, and tells the window at once, as
// Notify does, but holds its banner for GatherWindow with those of every entry gathered
// meanwhile, and then shows them as one batch: one banner each, or past MaxBanners one that
// sums them up, as one refresh's arrivals are. A failure to record is logged, and holds
// nothing.
func (d *Dispatcher) Gather(ctx context.Context, entry Entry) {
	row, err := d.store.RecordEvent(ctx, d.clock.Now(), entry)
	if err != nil {
		d.logger.WarnContext(ctx, "could not record the notification", "kind", string(entry.Kind), "error", err)

		return
	}

	d.notify()
	d.held.add(row)
}

// Close drops the banners Gather holds, and holds none after: the history keeps their
// entries, and what Gather records later shows no banner. A batch already showing is cut
// short, and Close returns once it has ended, so nothing reads the database after it: core
// calls it before it closes the database.
func (d *Dispatcher) Close() {
	d.held.stop()
}

// show posts the banners for recorded notifications, of the kinds that are on, when the
// user allows banners. Once ctx ends it posts no more, and says nothing of a read it cut
// short.
func (d *Dispatcher) show(ctx context.Context, rows []Notification) {
	if d.poster == nil || d.poster.Status() != StatusOn {
		return
	}

	off, err := d.bannersOff(ctx)
	if err != nil {
		if ctx.Err() == nil {
			d.logger.WarnContext(ctx, "could not read which banners are on", "error", err)
		}

		return
	}

	for _, b := range Banners(rows, off) {
		if ctx.Err() != nil {
			return
		}

		if err = d.poster.Post(b); err != nil {
			d.logger.WarnContext(ctx, "could not show a notification", "error", err)
		}
	}
}

// Service is the notification log as the window binds it.
type Service struct {
	base
}

// NewService returns a Service.
func NewService(cfg Config) *Service {
	return &Service{base: newBase(cfg)}
}

// List returns the notification history, newest first, and how many are unread.
func (s *Service) List(ctx context.Context) (Log, error) {
	return s.store.Log(ctx)
}

// MarkAllRead marks every notification read, as the history does once it is opened.
func (s *Service) MarkAllRead(ctx context.Context) error {
	if err := s.store.MarkAllRead(ctx, s.clock.Now()); err != nil {
		return err
	}

	s.notify()

	return nil
}

// Clear forgets every notification.
func (s *Service) Clear(ctx context.Context) error {
	if err := s.store.Clear(ctx); err != nil {
		return err
	}

	s.notify()

	return nil
}

// BannerStatus reports whether banners are shown: StatusOn, StatusOff when the user has
// not allowed them, or StatusUnavailable when this build cannot show them.
func (s *Service) BannerStatus() BannerStatus {
	if s.poster == nil {
		return StatusUnavailable
	}

	return s.poster.Status()
}

// SendTest shows a sample banner, asking the user to allow banners first if they have not
// been asked, so the user can see what banners look like and that they arrive.
func (s *Service) SendTest() error {
	if s.poster == nil || s.poster.Status() == StatusUnavailable {
		return ErrBannersUnavailable
	}

	allowed, err := s.poster.Authorize()
	if err != nil {
		return fmt.Errorf("ask to show notifications: %w", err)
	}

	if !allowed {
		return ErrBannersOff
	}

	if err = s.poster.Post(TestBanner()); err != nil {
		return fmt.Errorf("show a test notification: %w", err)
	}

	return nil
}
