package settings

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/reporules"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestSetRepoRuleStoresTheRuleAndTellsOfIt(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	db := testutil.DB(t)
	svc := serviceOver(t, db, t.TempDir(), WithEvents(events))
	ctx := t.Context()

	fresh, err := svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, reporules.Rules{Pinned: []string{}, Muted: []string{}}, fresh.RepoRules)

	_, err = svc.SetRepoRule(ctx, "acme-org", reporules.RuleMuted)
	require.NoError(t, err)

	got, err := svc.SetRepoRule(ctx, " acme-org/example-repo ", reporules.RulePinned)
	require.NoError(t, err)
	assert.Equal(t, reporules.Rules{Pinned: []string{"acme-org/example-repo"}, Muted: []string{"acme-org"}}, got.RepoRules)

	again, err := svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, got.RepoRules, again.RepoRules, "stored")

	got, err = svc.SetRepoRule(ctx, "ACME-ORG", reporules.RuleNone)
	require.NoError(t, err)
	assert.Equal(t, reporules.Rules{Pinned: []string{"acme-org/example-repo"}, Muted: []string{}}, got.RepoRules)

	assert.Equal(t, []event.Event{event.RepoRulesChanged{}, event.RepoRulesChanged{}, event.RepoRulesChanged{}},
		events.All())
}

func TestSetRepoRuleRefusesWhatItCannotStore(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	db := testutil.DB(t)
	svc := serviceOver(t, db, t.TempDir(), WithEvents(events))
	ctx := t.Context()

	_, err := svc.SetRepoRule(ctx, "not a repo", reporules.RuleMuted)
	require.ErrorIs(t, err, reporules.ErrInvalidTarget)

	_, err = svc.SetRepoRule(ctx, "acme-org", "hidden")
	require.ErrorIs(t, err, reporules.ErrInvalidRule)

	setPref(t, db, reporules.PrefMuted, "{broken")

	got, err := svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, reporules.Rules{Pinned: []string{}, Muted: []string{}}, got.RepoRules,
		"a list that can't be read is empty")

	got, err = svc.SetRepoRule(ctx, "acme-org", reporules.RuleMuted)
	require.NoError(t, err, "and is written again whole")
	assert.Equal(t, []string{"acme-org"}, got.RepoRules.Muted)

	assert.Equal(t, []event.Event{event.RepoRulesChanged{}}, events.All(), "only a stored rule is told")
}
