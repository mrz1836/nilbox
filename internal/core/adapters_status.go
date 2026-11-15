package core

import (
	"context"
	"log/slog"

	"github.com/mrz1836/nilbox/internal/notify"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/statusprefs"
	"github.com/mrz1836/nilbox/internal/svcstatus"
)

// statusRules reads, in one read of the settings, how often the service status check runs
// and what it watches (svcstatus.Config.Rules). Settings that can't be read count as the
// defaults, logged, so a passing problem never stops the check.
func statusRules(prefs *settings.Service, logger *slog.Logger) func(context.Context) svcstatus.Rules {
	return func(ctx context.Context) svcstatus.Rules {
		s, err := prefs.Get(ctx)
		if err != nil {
			if ctx.Err() == nil {
				logger.WarnContext(ctx, "could not read what the service status check watches; using the defaults",
					"error", err)
			}

			return svcstatus.Rules{
				Every: settings.DefaultRefreshIntervals().Every(settings.RefreshServiceStatus),
				Watch: statusprefs.Defaults(),
			}
		}

		return svcstatus.Rules{Every: s.Refresh.Every(settings.RefreshServiceStatus), Watch: s.ServiceStatus}
	}
}

// statusAlerts tells the user that a service they watch went down or came back, with a
// notification about no item (service_status), and its banner when that kind is on
// (svcstatus.Notifier). Each opens the incident, or the status page.
type statusAlerts struct {
	dispatcher *notify.Dispatcher
}

// Alert records alert as a notification, and shows its banner.
func (a statusAlerts) Alert(ctx context.Context, alert svcstatus.Alert) {
	a.dispatcher.Notify(ctx, notify.Entry{
		Kind: notify.KindServiceStatus, Title: alert.Title, Body: alert.Body, URL: alert.URL,
	})
}
