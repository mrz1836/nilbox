package core

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/notify"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/statusprefs"
	"github.com/mrz1836/nilbox/internal/svcstatus"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// countStatusRules counts the times the status check is told the rules changed.
type countStatusRules struct {
	n atomic.Int32
}

func (r *countStatusRules) RulesChanged() { r.n.Add(1) }

func TestAStatusRuleChangeReachesTheCheckAndStaysInGo(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	check := &countStatusRules{}
	relay := &followSwitch{sink: events, statusRules: check}

	relay.Emit(event.StatusRulesChanged{})

	assert.Equal(t, int32(1), check.n.Load(), "the check follows the change")
	assert.Empty(t, events.All(), "the window has the settings from the setter, and hears the check")

	(&followSwitch{sink: events}).Emit(event.StatusRulesChanged{})
	assert.Empty(t, events.All(), "with no check, nothing")
}

func TestStatusRulesReadTheSettingsAsTheyAreNow(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, testOptions(t))
	rules := statusRules(c.Settings, nil)

	assert.Equal(t, svcstatus.Rules{Every: 30 * time.Minute, Watch: statusprefs.Defaults()}, rules(t.Context()))

	_, err := c.Settings.SetRefreshInterval(t.Context(), settings.RefreshServiceStatus, 300)
	require.NoError(t, err)
	_, err = c.Settings.SetStatusProvider(t.Context(), statusprefs.ProviderOpenAI, false)
	require.NoError(t, err)

	got := rules(t.Context())
	assert.Equal(t, 5*time.Minute, got.Every)

	openai, _ := got.Watch.For(statusprefs.ProviderOpenAI)
	assert.False(t, openai.On)
}

func TestStatusRulesFallBackToTheDefaultsLogged(t *testing.T) {
	t.Parallel()

	logs := &testutil.Logs{}
	db := testutil.ClosedDB(t)
	rules := statusRules(settings.NewService(db, profiles.NewStore(db, nil), t.TempDir()), logs.Logger())

	assert.Equal(t, svcstatus.Rules{Every: 30 * time.Minute, Watch: statusprefs.Defaults()}, rules(t.Context()))
	assert.Contains(t, logs.String(), "using the defaults")
}

func TestStatusAlertsAreNotificationsAboutNoItem(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	opts := testOptions(t)
	opts.Events = events
	c := bootstrap(t, opts)

	statusAlerts{dispatcher: notify.NewDispatcher(notify.Config{Pool: c.DB, Events: events})}.Alert(t.Context(),
		svcstatus.Alert{
			Provider: statusprefs.ProviderClaude, State: svcstatus.StateMajor, Title: "Claude: Partial outage",
			Body: "Claude API · Elevated errors on Claude API", URL: "https://status.claude.com/incidents/x1",
		})

	log, err := c.Notifications.List(t.Context())
	require.NoError(t, err)
	require.Len(t, log.Items, 1)

	got := log.Items[0]
	assert.Equal(t, notify.KindServiceStatus, got.Kind)
	assert.Equal(t, "Claude: Partial outage", got.Title)
	assert.Equal(t, "Claude API · Elevated errors on Claude API", got.Detail)
	assert.Equal(t, "https://status.claude.com/incidents/x1", got.URL)
	assert.Empty(t, got.ItemKind, "about no item")
	assert.Empty(t, got.Repo)
	assert.Contains(t, events.All(), event.Event(event.NotificationsUpdated{}))
}
