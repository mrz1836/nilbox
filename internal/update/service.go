package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/uierr"
)

var (
	// ErrNoLog is returned by ShowInstallLog when no log of a failed install was kept.
	ErrNoLog = errors.New("no log of the failed update was kept")

	// ErrSettling is returned by Download and Restart while the start isn't settled: the
	// last install, under HelperWindow old, has a helper whose log hasn't ended, which may
	// still be replacing the app.
	ErrSettling = errors.New("the last update may still be finishing")
)

// Service is the app's update as the window sees it: its status, Check for Updates, the
// download Just tell me's Update asks for, the restart, Later, and the toast after an
// update. The window binds it; the Engine behind it is never bound.
type Service struct {
	e *Engine
}

// NewService returns the window's face of e.
func NewService(e *Engine) *Service {
	return &Service{e: e}
}

// Status returns the update as the window shows it. It reads no network.
func (s *Service) Status(ctx context.Context) (Status, error) {
	return s.e.Status(ctx)
}

// CheckNow checks for a new version now, in every mode, Don't check included, and returns
// the status, whose LastError tells a check that failed. A version it finds is shown
// without a notification; with Download it and tell me, the Worker downloads it at once,
// a download that failed before included, but only once the start is settled. While the
// update downloads, it returns the status as it is.
func (s *Service) CheckNow(ctx context.Context) (Status, error) {
	if _, err := s.on(ctx); err != nil {
		return Status{}, err
	}

	if err := s.e.checkNow(ctx); err != nil {
		return Status{}, err
	}

	return s.e.Status(ctx)
}

// Download downloads the newer version found and checks it, and returns the status, ready:
// what Update asks for with Just tell me, before it restarts. A version downloaded already
// is ready as it is. While the last install's helper may still be at work, it refuses with
// ErrSettling.
func (s *Service) Download(ctx context.Context) (Status, error) {
	if err := s.changes(ctx); err != nil {
		return Status{}, err
	}

	if err := s.e.downloadNow(ctx); err != nil {
		return Status{}, err
	}

	return s.e.Status(ctx)
}

// Restart installs the update that is ready and restarts into it. While runs go in the app,
// it refuses with a RunsGoingError naming them unless confirmed, so the window can ask
// first; confirmed, it stops them. It backs up the database first and refuses to go on
// without the backup. The app quits inside it once the update is staged, so it returns only
// when something stopped the restart: the update is then ready again, and the error says
// why. While the last install's helper may still be at work, it refuses with ErrSettling.
func (s *Service) Restart(ctx context.Context, confirmed bool) (Status, error) {
	if err := s.changes(ctx); err != nil {
		return Status{}, err
	}

	if err := s.e.restart(ctx, confirmed); err != nil {
		return Status{}, err
	}

	return s.e.Status(ctx)
}

// HideBanner hides the banner of the newer version for HideFor, as Later asks, and returns
// the status. A newer version than the one hidden shows it again.
func (s *Service) HideBanner(ctx context.Context) (Status, error) {
	status, err := s.e.Status(ctx)
	if err != nil || status.Latest == "" {
		return status, err
	}

	body, err := json.Marshal(bannerHidden{Version: status.Latest, Until: s.e.clock.Now().Add(HideFor).UTC()})
	if err != nil {
		return Status{}, fmt.Errorf("hide the update's banner: %w", err)
	}

	if err = s.e.memory.SetMany(ctx, map[string]string{KeyBannerHidden: string(body)}); err != nil {
		return Status{}, fmt.Errorf("hide the update's banner: %w", err)
	}

	s.e.events.Emit(event.UpdatesUpdated{})

	return s.e.Status(ctx)
}

// ShowBanner shows the banner again, lifting Later, as a click on the update's
// notification asks, and returns the status.
func (s *Service) ShowBanner(ctx context.Context) (Status, error) {
	if !s.e.Enabled() {
		return s.e.Status(ctx)
	}

	if err := s.e.memory.Delete(ctx, KeyBannerHidden); err != nil {
		return Status{}, fmt.Errorf("show the update's banner: %w", err)
	}

	s.e.events.Emit(event.UpdatesUpdated{})

	return s.e.Status(ctx)
}

// AckUpdated forgets which version this launch updated to, once the window has said so,
// and returns the status.
func (s *Service) AckUpdated(ctx context.Context) (Status, error) {
	s.e.change(func(st *state) { st.justUpdated = "" })

	return s.e.Status(ctx)
}

// ShowInstallLog shows the log of the update the last restart didn't install in Finder,
// the copy kept in the logs folder.
func (s *Service) ShowInstallLog(ctx context.Context) error {
	status, err := s.e.Status(ctx)
	if err != nil {
		return err
	}

	failed := status.InstallFailed
	if !failed.Log || !ValidVersion(failed.Version) {
		return uierr.New("No log of that update was kept.", ErrNoLog)
	}

	path := s.e.installLog(failed.Version)
	if _, err = os.Stat(path); err != nil {
		return uierr.New("The update's log is gone from the logs folder.", fmt.Errorf("%w: %w", ErrNoLog, err))
	}

	if s.e.reveal == nil {
		return uierr.New("This build can't show files in Finder; the log is in the logs folder.", ErrNoLog)
	}

	return s.e.reveal.Reveal(ctx, path) //nolint:wrapcheck // the revealer words its own failure
}

// on refuses an action in a build that doesn't update, and settles the start first. It
// reports whether the start is settled: while the last install's helper may still be at
// work (HelperWindow), it isn't, and nothing may download or install.
func (s *Service) on(ctx context.Context) (bool, error) {
	if !s.e.Enabled() {
		return false, uierr.New(s.e.off, ErrOff)
	}

	return s.e.settle(ctx) == 0, nil
}

// changes is on for an action that downloads or installs: it also refuses while the start
// isn't settled, with ErrSettling, so nothing overwrites the last install's record or
// restarts while its helper may still be replacing the app. The refusal carries
// RefusalSettling, so the window waits it out quietly rather than call it a failure.
func (s *Service) changes(ctx context.Context) error {
	settled, err := s.on(ctx)
	if err != nil {
		return err
	}

	if !settled {
		return newRefusal(RefusalSettling,
			uierr.New("The last update is still finishing. Try again in a moment.", ErrSettling))
	}

	return nil
}
