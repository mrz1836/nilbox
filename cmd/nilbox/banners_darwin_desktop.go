//go:build desktop && darwin

package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"github.com/mrz1836/nilbox/internal/desktop"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/notify"
)

// banners posts native notifications through the system's notification center. They need
// the app bundle, whose identifier they are posted under: a development run is a bare
// binary, so banners are unavailable there, and the notification log is kept without them.
type banners struct {
	center *notifications.NotificationService
}

// newBanners prepares the notification center, emitting an event.RevealItem into events
// for every click on a banner: the item it was for, or none for a banner that stands for
// many. It returns nil, and logs why, when this build cannot show banners. It must run
// before the app finishes launching, so a click on a banner that launched the app is
// still delivered.
func newBanners(logger *slog.Logger, events event.Sink) *banners {
	center := notifications.New()

	if err := center.ServiceStartup(context.Background(), application.ServiceOptions{}); err != nil {
		logger.Info("notifications are off for this run", "error", err)

		return nil
	}

	center.OnNotificationResponse(func(result notifications.NotificationResult) {
		if result.Error != nil {
			logger.Warn("could not read a notification click", "error", result.Error)

			return
		}

		if result.Response.ActionIdentifier != notifications.DefaultActionIdentifier {
			return
		}

		events.Emit(desktop.BannerClicked(result.Response.UserInfo))
	})

	return &banners{center: center}
}

// Post shows a banner, when the user has allowed them.
func (b *banners) Post(banner notify.Banner) error {
	allowed, err := b.center.CheckNotificationAuthorization()
	if err != nil {
		return fmt.Errorf("check notification permission: %w", err)
	}

	if !allowed {
		return notify.ErrBannersOff
	}

	err = b.center.SendNotification(notifications.NotificationOptions{
		ID:       banner.ID,
		Title:    banner.Title,
		Subtitle: banner.Subtitle,
		Body:     banner.Body,
		ThreadID: banner.Thread,
		Data:     desktop.BannerData(banner),
	})
	if err != nil {
		return fmt.Errorf("post the notification: %w", err)
	}

	return nil
}

// Status reports whether the user has allowed banners.
func (b *banners) Status() notify.BannerStatus {
	allowed, err := b.center.CheckNotificationAuthorization()
	if err != nil || !allowed {
		return notify.StatusOff
	}

	return notify.StatusOn
}

// Authorize asks the user to allow banners, the first time, and reports whether they are.
func (b *banners) Authorize() (bool, error) {
	allowed, err := b.center.RequestNotificationAuthorization()
	if err != nil {
		return false, fmt.Errorf("ask to show notifications: %w", err)
	}

	return allowed, nil
}
