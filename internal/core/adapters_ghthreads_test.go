package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	prefsstore "github.com/mrz1836/nilbox/internal/prefs"
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
