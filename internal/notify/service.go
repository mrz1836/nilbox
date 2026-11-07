package notify

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/notifyprefs"
	"github.com/mrz1836/nilbox/internal/prefs"
)

var (
	// ErrBannersUnavailable is returned by SendTest when this build cannot show banners.
	ErrBannersUnavailable = errors.New("this build cannot show notifications; they need the packaged app")

	// ErrBannersOff is returned by SendTest when the user has not allowed banners.
	ErrBannersOff = errors.New("notifications are off for this app in System Settings")
)

// Notifier is told when the log changed, so the window reads it again.
type Notifier interface {
	NotificationsUpdated()
}

// Config holds the dependencies of a Dispatcher and a Service. Pool is required; Poster,
// Notifier, Clock, and Logger are optional: without a Poster no banner is shown, and the
// log is still kept.
type Config struct {
	// Pool is the database's connection pool. It holds the log, and the prefs the
	// Dispatcher reads which banners are on from.
	Pool *sql.DB

	Poster   Poster
	Notifier Notifier
	Clock    clock.Clock
	Logger   *slog.Logger
}

// base is what the Dispatcher and the Service share.
type base struct {
	store    *Store
	prefs    *prefs.Store
	poster   Poster
	notifier Notifier
	clock    clock.Clock
	logger   *slog.Logger
}

// newBase fills in the defaults.
func newBase(cfg Config) base {
	b := base{
		store: NewStore(cfg.Pool), prefs: prefs.New(cfg.Pool, cfg.Clock), poster: cfg.Poster, notifier: cfg.Notifier,
		clock: clock.Or(cfg.Clock), logger: cfg.Logger,
	}

	if b.logger == nil {
		b.logger = slog.Default()
	}

	return b
}

// notify tells the notifier the log changed.
func (b base) notify() {
	if b.notifier != nil {
		b.notifier.NotificationsUpdated()
	}
}

// bannersOff reads the kinds whose banners are off.
func (b base) bannersOff(ctx context.Context) ([]string, error) {
	stored, _, err := b.prefs.Get(ctx, notifyprefs.KeyBannersOff)
	if err != nil {
		return nil, fmt.Errorf("read the notification settings: %w", err)
	}

	return notifyprefs.DecodeBannersOff(stored), nil
}

// Dispatcher records the inbox's arrivals and shows their banners. It is the inbox's
// inbox.ArrivalSink, and is not bound to the window.
type Dispatcher struct {
	base
}

// NewDispatcher returns a Dispatcher.
func NewDispatcher(cfg Config) *Dispatcher {
	return &Dispatcher{base: newBase(cfg)}
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

	if d.poster == nil || d.poster.Status() != StatusOn {
		return
	}

	off, err := d.bannersOff(ctx)
	if err != nil {
		d.logger.WarnContext(ctx, "could not read which banners are on", "error", err)

		return
	}

	for _, b := range Banners(rows, off) {
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
func (s *Service) BannerStatus() string {
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
