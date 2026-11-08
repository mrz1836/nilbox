//go:build desktop && !darwin

package main

import (
	"log/slog"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/notify"
)

// banners has no notification center to post to off macOS yet; a port posts this OS's
// notifications here. The notification log is kept without them.
type banners struct{}

// newBanners returns nil: this OS shows no banners yet.
func newBanners(*slog.Logger, event.Sink) *banners {
	return nil
}

// Post shows nothing.
func (*banners) Post(notify.Banner) error { return notify.ErrBannersUnavailable }

// Status reports that banners are unavailable.
func (*banners) Status() notify.BannerStatus { return notify.StatusUnavailable }

// Authorize reports that banners are not allowed.
func (*banners) Authorize() (bool, error) { return false, notify.ErrBannersUnavailable }
