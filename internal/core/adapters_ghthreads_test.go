package core

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/ghthreads"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/item"
	prefsstore "github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/threadrules"
)

func TestTheNotificationsMirrorReadsTheRulesAsSettingsStoreThem(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, testOptions(t))
	ctx := t.Context()
	rules := ghThreadRules{prefs: prefsstore.New(c.DB, nil)}

	got, err := rules.ThreadRules(ctx)
	require.NoError(t, err)
	assert.Equal(t, threadrules.Defaults(), got, "read by default")

	require.NoError(t, testutil.ErrOf(c.Settings.SetGitHubNotificationSwitch(ctx, threadrules.SwitchRead, false)))

	got, err = rules.ThreadRules(ctx)
	require.NoError(t, err)
	assert.False(t, got.Read, "turned off in Settings")

	_, err = ghThreadRules{prefs: prefsstore.New(testutil.ClosedDB(t), nil)}.ThreadRules(ctx)
	require.Error(t, err)
}

func TestTheNotificationsMirrorHoldsClearsAsLongAsToastsStay(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, testOptions(t))
	ctx := t.Context()
	holds := ghThreadHolds{settings: c.Settings}

	got, err := holds.ToastSeconds(ctx)
	require.NoError(t, err)
	assert.Equal(t, settings.DefaultToastSeconds, got)
	assert.Equal(t, ghthreads.MinHold, ghthreads.Hold(got), "the default toast holds the least")

	require.NoError(t, testutil.ErrOf(c.Settings.SetToastSeconds(ctx, 40)))

	got, err = holds.ToastSeconds(ctx)
	require.NoError(t, err)
	assert.Equal(t, 40, got)
}

func TestAnInboxOutcomeReachesTheNotificationsMirror(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, testOptions(t))
	ctx := t.Context()
	now := time.Now()

	ids, err := inbox.StoreFixture(ctx, c.DB, inbox.Fixture{
		Viewer: "octo-user",
		Items: []inbox.FixtureItem{{
			Item: github.Item{
				IsPR: true, NodeID: "NODE_7", Number: 7, Title: "Fix the login redirect",
				URL: "https://github.com/acme-org/example-repo/pull/7", CreatedAt: now, UpdatedAt: now,
				AuthorLogin: "octo-other", RepoOwner: boundOwner, RepoName: boundRepo,
			},
			ReviewRequested: true,
		}},
	})
	require.NoError(t, err)

	_, err = c.Inbox.Dismiss(ctx, ids[0])
	require.NoError(t, err)

	var (
		login, kind, trigger string
		number               int
	)

	require.NoError(t, c.DB.QueryRowContext(ctx, `SELECT account_login, item_kind, number, trigger
		FROM gh_thread_moments`).Scan(&login, &kind, &number, &trigger))
	assert.Equal(t, "octo-user", login)
	assert.Equal(t, string(item.KindPR), kind)
	assert.Equal(t, 7, number)
	assert.Equal(t, string(threadrules.TriggerDismissed), trigger, "the dismissal is a moment that clears")

	_, err = c.Inbox.Restore(ctx, ids[0])
	require.NoError(t, err)

	var left int

	require.NoError(t, c.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM gh_thread_moments`).Scan(&left))
	assert.Zero(t, left, "a restore takes it back")
}

func TestEveryInboxOutcomeKindReachesTheMirror(t *testing.T) {
	t.Parallel()

	for _, kind := range []inbox.OutcomeKind{
		inbox.OutcomeMerged, inbox.OutcomeClosed, inbox.OutcomeReviewed, inbox.OutcomeDismissed,
		inbox.OutcomeRestored, inbox.OutcomeUnassigned, inbox.OutcomeUnrequested, inbox.OutcomeSeenClosed,
		inbox.OutcomeUnlisted,
	} {
		got, ok := outcomeKinds()[kind]
		assert.True(t, ok, kind)
		assert.Equal(t, string(kind), string(got), "the same words on both sides")
	}
}

// countThreadRules counts the times the mirror is told the rules changed.
type countThreadRules struct {
	n atomic.Int32
}

func (r *countThreadRules) RulesChanged() { r.n.Add(1) }

func TestAThreadRuleChangeReachesTheMirrorAndStaysInGo(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	mirror := &countThreadRules{}
	relay := &followSwitch{sink: events, threadRules: mirror}

	relay.Emit(event.ThreadRulesChanged{})

	assert.Equal(t, int32(1), mirror.n.Load(), "the mirror decides again")
	assert.Empty(t, events.All(), "the window has the settings from the setter already")

	(&followSwitch{sink: events}).Emit(event.ThreadRulesChanged{})
	assert.Empty(t, events.All(), "with no mirror, nothing")
}
