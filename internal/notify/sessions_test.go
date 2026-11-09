package notify

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/notifyprefs"
	"github.com/mrz1836/nilbox/internal/sessions"
	"github.com/mrz1836/nilbox/internal/settings"
)

// waitingSession is a Claude session on acme-org/example-repo#12 that needs approval.
func waitingSession() sessions.Session {
	return sessions.Session{
		ID: "s1", Agent: agent.Claude, ItemID: 7, Repo: "acme-org/example-repo", Number: 12, Title: "Fix the widget",
		URL: "https://github.com/acme-org/example-repo/pull/12", State: sessions.StateNeedsApproval,
		LastMessage: "May I run\n  the tests?",
	}
}

func TestSessionBanner(t *testing.T) {
	t.Parallel()

	assert.Equal(t, Banner{
		ID: brand.Slug + "-session-s1", Title: "Claude needs approval", Subtitle: "example-repo #12",
		Body: "May I run the tests?", Thread: string(KindSessionNeedsYou), ItemID: 7,
		URL: "https://github.com/acme-org/example-repo/pull/12",
	}, SessionBanner(KindSessionNeedsYou, waitingSession()))

	s := waitingSession()
	s.Agent, s.State, s.LastMessage = agent.Codex, sessions.StateIdle, ""

	got := SessionBanner(KindSessionFinished, s)
	assert.Equal(t, "Codex finished its turn", got.Title)
	assert.Equal(t, "Fix the widget", got.Body, "the item's title when the agent said nothing")
}

func TestSessionBannersStartOff(t *testing.T) {
	t.Parallel()

	f := newFixture(t, 0)
	d := NewSessionBanners(f.config())

	d.NeedsYou(t.Context(), waitingSession())
	d.Finished(t.Context(), waitingSession())
	assert.Empty(t, f.poster.posted(), "both kinds start off")

	prefs := settings.NewService(f.pool, nil, "")
	_, err := prefs.SetNotificationBanner(t.Context(), notifyprefs.KindSessionNeedsYou, true)
	require.NoError(t, err)

	d.NeedsYou(t.Context(), waitingSession())
	d.Finished(t.Context(), waitingSession())

	posted := f.poster.posted()
	require.Len(t, posted, 1, "only the kind turned on")
	assert.Equal(t, "Claude needs approval", posted[0].Title)

	log, err := NewStore(f.pool).Log(t.Context())
	require.NoError(t, err)
	assert.Empty(t, log.Items, "the notification log keeps the inbox's notifications only")
	assert.Zero(t, f.events.Count())
}

func TestSessionBannersNeedAllowedBanners(t *testing.T) {
	t.Parallel()

	f := newFixture(t, 0)
	f.poster.status = StatusOff

	_, err := settings.NewService(f.pool, nil, "").SetNotificationBanner(t.Context(), notifyprefs.KindSessionFinished, true)
	require.NoError(t, err)

	NewSessionBanners(f.config()).Finished(t.Context(), waitingSession())
	assert.Empty(t, f.poster.posted())

	NewSessionBanners(Config{Pool: f.pool}).Finished(t.Context(), waitingSession())
}
