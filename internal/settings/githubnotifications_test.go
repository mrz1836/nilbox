package settings

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/threadrules"
	"github.com/mrz1836/nilbox/internal/uierr"
)

func TestGitHubNotificationsStartAtTheDefaults(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, threadrules.Rules{
		Read: true, Auto: false, Action: threadrules.ActionDone, TriggersOff: []threadrules.Trigger{},
		Watching: true, BotActivity: true, CleanupCardDismissed: false,
	}, got.GitHubNotifications, "the notifications are read, and nothing is cleared until the user says so")
}

func TestUnreadableGitHubNotificationRulesReadAsTheDefaults(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)

	for key, value := range map[string]string{
		threadrules.KeyRead: "nope", threadrules.KeyAuto: "yes", threadrules.KeyAction: "archive",
		threadrules.KeyTriggersOff: `["by_hand","later"`, threadrules.KeyWatching: "",
		threadrules.KeyBotActivity: "sometimes", threadrules.KeyCleanupCardDismissed: "2",
	} {
		setPref(t, db, key, value)
	}

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, threadrules.Defaults(), got.GitHubNotifications)
}

func TestSetGitHubNotificationSwitch(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	svc := serviceOver(t, testutil.DB(t), t.TempDir(), WithEvents(events))
	ctx := t.Context()

	for _, tc := range []struct {
		name  threadrules.Switch
		key   string
		field func(threadrules.Rules) bool
	}{
		{name: threadrules.SwitchRead, key: threadrules.KeyRead, field: func(r threadrules.Rules) bool { return r.Read }},
		{name: threadrules.SwitchAuto, key: threadrules.KeyAuto, field: func(r threadrules.Rules) bool { return r.Auto }},
		{
			name: threadrules.SwitchWatching, key: threadrules.KeyWatching,
			field: func(r threadrules.Rules) bool { return r.Watching },
		},
		{
			name: threadrules.SwitchBotActivity, key: threadrules.KeyBotActivity,
			field: func(r threadrules.Rules) bool { return r.BotActivity },
		},
	} {
		for _, on := range []bool{true, false, true} {
			got, err := svc.SetGitHubNotificationSwitch(ctx, tc.name, on)
			require.NoError(t, err, tc.name)
			assert.Equal(t, on, tc.field(got.GitHubNotifications), tc.name)

			stored, ok, err := svc.prefs.Get(ctx, tc.key)
			require.NoError(t, err)
			assert.True(t, ok, tc.key)
			assert.Equal(t, map[bool]string{true: "true", false: "false"}[on], stored, tc.key)

			reread, err := svc.Get(ctx)
			require.NoError(t, err)
			assert.Equal(t, reread, got, "%s returns the settings as stored", tc.name)
		}
	}

	assert.Equal(t, 12, events.Count(), "every change stored is told")
}

func TestGitHubNotificationSettersEmitRulesChanged(t *testing.T) {
	t.Parallel()

	for name, set := range map[string]func(context.Context, *Service) error{
		"switch": func(ctx context.Context, s *Service) error {
			return testutil.ErrOf(s.SetGitHubNotificationSwitch(ctx, threadrules.SwitchAuto, true))
		},
		"trigger": func(ctx context.Context, s *Service) error {
			return testutil.ErrOf(s.SetGitHubNotificationTrigger(ctx, threadrules.TriggerDismissed, false))
		},
		"action": func(ctx context.Context, s *Service) error {
			return testutil.ErrOf(s.SetGitHubNotificationAction(ctx, threadrules.ActionRead))
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			events := &testutil.Events{}
			svc := serviceOver(t, testutil.DB(t), t.TempDir(), WithEvents(events))

			require.NoError(t, set(t.Context(), svc))
			assert.Equal(t, []event.Event{event.ThreadRulesChanged{}}, events.All(), "told once it is stored")
		})
	}

	t.Run("cleanup card", func(t *testing.T) {
		t.Parallel()

		events := &testutil.Events{}
		svc := serviceOver(t, testutil.DB(t), t.TempDir(), WithEvents(events))

		got, err := svc.SetGitHubCleanupCardDismissed(t.Context(), true)
		require.NoError(t, err)
		assert.True(t, got.GitHubNotifications.CleanupCardDismissed, "stored")
		assert.Zero(t, events.Count(), "the window's own flag changes no rule: nothing is told")
	})

	for name, set := range map[string]func(context.Context, *Service) error{
		"unknown switch": func(ctx context.Context, s *Service) error {
			return testutil.ErrOf(s.SetGitHubNotificationSwitch(ctx, "sometimes", true))
		},
		"by hand": func(ctx context.Context, s *Service) error {
			return testutil.ErrOf(s.SetGitHubNotificationTrigger(ctx, threadrules.TriggerByHand, false))
		},
		"unknown action": func(ctx context.Context, s *Service) error {
			return testutil.ErrOf(s.SetGitHubNotificationAction(ctx, "archive"))
		},
		"failed write": func(ctx context.Context, s *Service) error {
			if _, err := s.pool.ExecContext(ctx, refusePrefs); err != nil {
				return err
			}

			return testutil.ErrOf(s.SetGitHubNotificationSwitch(ctx, threadrules.SwitchAuto, true))
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			events := &testutil.Events{}
			svc := serviceOver(t, testutil.DB(t), t.TempDir(), WithEvents(events))

			require.Error(t, set(t.Context(), svc))
			assert.Zero(t, events.Count(), "a change refused, or not stored, tells nothing")
		})
	}
}

func TestSetGitHubNotificationSwitchRefusesAnUnknownOne(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)

	for _, name := range []threadrules.Switch{"", "action", "cleanupCardDismissed", "Auto"} {
		got, err := svc.SetGitHubNotificationSwitch(t.Context(), name, true)
		require.ErrorIs(t, err, threadrules.ErrUnknownSwitch, name)
		assert.Equal(t, "That isn't a setting of GitHub notifications.", uierr.Message(err))
		assert.Equal(t, Settings{}, got)
	}

	reread, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, threadrules.Defaults(), reread.GitHubNotifications, "a refused switch stored nothing")
}

func TestSetGitHubNotificationTrigger(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	svc := serviceOver(t, testutil.DB(t), t.TempDir(), WithEvents(events))
	ctx := t.Context()

	got, err := svc.SetGitHubNotificationTrigger(ctx, threadrules.TriggerUnassigned, false)
	require.NoError(t, err)
	assert.Equal(t, []threadrules.Trigger{threadrules.TriggerUnassigned}, got.GitHubNotifications.TriggersOff)

	got, err = svc.SetGitHubNotificationTrigger(ctx, threadrules.TriggerMerged, false)
	require.NoError(t, err)
	assert.Equal(t, []threadrules.Trigger{threadrules.TriggerMerged, threadrules.TriggerUnassigned},
		got.GitHubNotifications.TriggersOff, "the other trigger stays off, in the order of the triggers")

	stored, ok, err := svc.prefs.Get(ctx, threadrules.KeyTriggersOff)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.JSONEq(t, `["merged","unassigned"]`, stored, "only the triggers that are off are stored")

	got, err = svc.SetGitHubNotificationTrigger(ctx, threadrules.TriggerUnassigned, true)
	require.NoError(t, err)
	assert.Equal(t, []threadrules.Trigger{threadrules.TriggerMerged}, got.GitHubNotifications.TriggersOff)
	assert.False(t, got.GitHubNotifications.On(threadrules.TriggerMerged))
	assert.True(t, got.GitHubNotifications.On(threadrules.TriggerUnassigned))

	reread, err := svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, reread, got, "it returns the settings as stored")
	assert.Equal(t, 3, events.Count(), "every change stored is told")
}

func TestSetGitHubNotificationTriggerDropsWhatIsNoLongerKnown(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	setPref(t, db, threadrules.KeyTriggersOff, `["later","by_hand","reviewed"]`)

	got, err := svc.SetGitHubNotificationTrigger(t.Context(), threadrules.TriggerDismissed, false)
	require.NoError(t, err)
	assert.Equal(t, []threadrules.Trigger{threadrules.TriggerReviewed, threadrules.TriggerDismissed},
		got.GitHubNotifications.TriggersOff)

	stored, _, err := svc.prefs.Get(t.Context(), threadrules.KeyTriggersOff)
	require.NoError(t, err)
	assert.JSONEq(t, `["reviewed","dismissed"]`, stored)
}

func TestSetGitHubNotificationTriggerRefusesOneWithoutASwitch(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)

	got, err := svc.SetGitHubNotificationTrigger(t.Context(), threadrules.TriggerByHand, false)
	require.ErrorIs(t, err, threadrules.ErrNoSwitch)
	assert.Equal(t, "Clearing a notification by hand has no switch: it is always on.", uierr.Message(err))
	assert.Equal(t, Settings{}, got)

	for _, trigger := range []threadrules.Trigger{"", "commented", "Merged"} {
		_, err = svc.SetGitHubNotificationTrigger(t.Context(), trigger, true)
		require.ErrorIs(t, err, threadrules.ErrUnknownTrigger, trigger)
		assert.Equal(t, "That isn't a moment that clears a GitHub notification.", uierr.Message(err))
	}

	_, ok, err := svc.prefs.Get(t.Context(), threadrules.KeyTriggersOff)
	require.NoError(t, err)
	assert.False(t, ok, "a refused trigger stored nothing")
}

func TestSetGitHubNotificationTriggerStoresNothingWhenTheWriteFails(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)

	_, err := db.ExecContext(t.Context(), refusePrefs)
	require.NoError(t, err)

	_, err = svc.SetGitHubNotificationTrigger(t.Context(), threadrules.TriggerMerged, false)
	require.ErrorContains(t, err, "save the GitHub notification triggers")

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Empty(t, got.GitHubNotifications.TriggersOff)
}

func TestSetGitHubNotificationAction(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)
	ctx := t.Context()

	got, err := svc.SetGitHubNotificationAction(ctx, threadrules.ActionRead)
	require.NoError(t, err)
	assert.Equal(t, threadrules.ActionRead, got.GitHubNotifications.Action)

	stored, ok, err := svc.prefs.Get(ctx, threadrules.KeyAction)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "read", stored)

	for _, bad := range []threadrules.Action{"", "archive", "READ"} {
		refused, setErr := svc.SetGitHubNotificationAction(ctx, bad)
		require.ErrorIs(t, setErr, threadrules.ErrUnknownAction, bad)
		assert.Equal(t, "Choose to mark a cleared notification done or read.", uierr.Message(setErr))
		assert.Equal(t, Settings{}, refused)
	}

	reread, err := svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, threadrules.ActionRead, reread.GitHubNotifications.Action, "a refused action stored nothing")

	got, err = svc.SetGitHubNotificationAction(ctx, threadrules.ActionDone)
	require.NoError(t, err)
	assert.Equal(t, threadrules.ActionDone, got.GitHubNotifications.Action)
	assert.Equal(t, got, mustGet(t, svc))
}

func TestSetGitHubCleanupCardDismissed(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)
	ctx := t.Context()

	got, err := svc.SetGitHubCleanupCardDismissed(ctx, true)
	require.NoError(t, err)
	assert.True(t, got.GitHubNotifications.CleanupCardDismissed)

	stored, ok, err := svc.prefs.Get(ctx, threadrules.KeyCleanupCardDismissed)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "true", stored)
	assert.Equal(t, got, mustGet(t, svc), "it returns the settings as stored")

	got, err = svc.SetGitHubCleanupCardDismissed(ctx, false)
	require.NoError(t, err)
	assert.False(t, got.GitHubNotifications.CleanupCardDismissed)
}

// mustGet reads the settings back.
func mustGet(t *testing.T, svc *Service) Settings {
	t.Helper()

	got, err := svc.Get(t.Context())
	require.NoError(t, err)

	return got
}
