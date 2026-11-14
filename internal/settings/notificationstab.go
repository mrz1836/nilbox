package settings

import (
	"context"
	"errors"
)

// KeyNotificationsTab is the Notifications page's tab the user last chose, stored as one of
// the NotificationsTab choices: the window's memory, which a backup never holds.
const KeyNotificationsTab = "ui.notificationsTab"

// NotificationsTab is a tab of the Notifications page: the app's own notifications, or the
// user's GitHub notifications.
type NotificationsTab string

// The Notifications page's tabs.
const (
	NotificationsTabApp    NotificationsTab = "app"
	NotificationsTabGitHub NotificationsTab = "github"
)

// DefaultNotificationsTab is the tab a fresh database opens the Notifications page on.
const DefaultNotificationsTab = NotificationsTabApp

// ErrInvalidNotificationsTab is returned for a Notifications page tab that is not one of the
// choices.
var ErrInvalidNotificationsTab = errors.New("unknown notifications tab")

// notificationsTabs is the Notifications page's tab choice, falling back to
// DefaultNotificationsTab for a stored tab that is unset or no longer a choice.
func notificationsTabs() choice[NotificationsTab] {
	return choice[NotificationsTab]{
		values:   []NotificationsTab{NotificationsTabApp, NotificationsTabGitHub},
		fallback: DefaultNotificationsTab, invalid: ErrInvalidNotificationsTab,
	}
}

// SetNotificationsTab stores the Notifications page's tab the user chose,
// NotificationsTabApp or NotificationsTabGitHub, which the page opens on from then on, and
// returns the settings with it.
func (s *Service) SetNotificationsTab(ctx context.Context, tab NotificationsTab) (Settings, error) {
	if err := notificationsTabs().check(tab); err != nil {
		return Settings{}, err
	}

	return s.save(ctx, setting(KeyNotificationsTab, string(tab)))
}
