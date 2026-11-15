package settings

import (
	"encoding/json"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/backup"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/statusprefs"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

func TestServiceStatusStartsAtTheDefaults(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, statusprefs.Defaults(), got.ServiceStatus)
	assert.Equal(t, statusprefs.Catalog(), got.StatusProviders, "the catalog, for the settings page")
	assert.Equal(t, 1800, got.Refresh.ServiceStatus)
}

func TestSetStatusProviderAndPartAreStoredAndTold(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	db := testutil.DB(t)
	svc := serviceOver(t, db, t.TempDir(), WithEvents(events))
	ctx := t.Context()

	got, err := svc.SetStatusProvider(ctx, statusprefs.ProviderOpenAI, false)
	require.NoError(t, err)

	openai, _ := got.ServiceStatus.For(statusprefs.ProviderOpenAI)
	assert.False(t, openai.On)

	_, err = svc.SetStatusPart(ctx, "claude.ai", true)
	require.NoError(t, err)
	got, err = svc.SetStatusPart(ctx, "github.actions", false)
	require.NoError(t, err)

	claude, _ := got.ServiceStatus.For(statusprefs.ProviderClaude)
	assert.Equal(t, []statusprefs.PartID{"claude.api", "claude.code", "claude.ai"}, claude.Parts)

	stored, err := svc.prefs.GetMany(ctx, statusprefs.Keys()...)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		statusprefs.KeyProvidersOff: `["openai"]`, statusprefs.KeyPartsOn: `["claude.ai"]`,
		statusprefs.KeyPartsOff: `["github.actions"]`,
	}, stored, "stored as changes from the defaults")

	reread, err := svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, got.ServiceStatus, reread.ServiceStatus)

	assert.Equal(t, []event.Event{
		event.StatusRulesChanged{}, event.StatusRulesChanged{}, event.StatusRulesChanged{},
	}, events.All(), "each change, once stored")
}

func TestStatusSettersRefuseAnUnknownID(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	svc := serviceOver(t, testutil.DB(t), t.TempDir(), WithEvents(events))

	_, err := svc.SetStatusProvider(t.Context(), "gitlab", false)
	require.ErrorIs(t, err, statusprefs.ErrUnknownProvider)
	assert.Equal(t, "That isn't a service whose status the app checks.", uierr.Message(err))

	_, err = svc.SetStatusPart(t.Context(), "github.nope", true)
	require.ErrorIs(t, err, statusprefs.ErrUnknownPart)
	assert.Equal(t, "That isn't a part of a service whose status the app checks.", uierr.Message(err))

	assert.Empty(t, events.All(), "a refusal stores nothing and tells nothing")

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, statusprefs.Defaults(), got.ServiceStatus)
}

func TestStatusSettersNeverLoseAQuickChange(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)

	parts := []statusprefs.PartID{
		"github.webhooks", "github.packages", "github.pages", "claude.ai", "claude.console", "openai.chat",
		"openai.login", "github.git", "claude.code", "openai.responses",
	}

	var wg sync.WaitGroup

	for _, id := range parts {
		wg.Go(func() {
			_, part, _ := statusprefs.PartOf(id)
			_, err := svc.SetStatusPart(t.Context(), id, !part.Default)
			assert.NoError(t, err)
		})
	}

	wg.Wait()

	got, err := svc.Get(t.Context())
	require.NoError(t, err)

	for _, id := range parts {
		_, part, _ := statusprefs.PartOf(id)
		assert.Equal(t, !part.Default, slices.Contains(got.ServiceStatus.Picked(), id), id)
	}
}

func TestAnOlderBackupsGitHubStatusIntervalIsTheServiceStatusOne(t *testing.T) {
	t.Parallel()

	var p Portable
	require.NoError(t, json.Unmarshal([]byte(`{"refresh": {"inbox": 600, "githubStatus": 300}}`), &p))
	assert.Equal(t, map[RefreshKind]int{RefreshInbox: 600, RefreshServiceStatus: 300}, p.Refresh)

	p = Portable{}
	require.NoError(t, json.Unmarshal([]byte(`{"refresh": {"githubStatus": 300, "serviceStatus": 0}}`), &p))
	assert.Equal(t, map[RefreshKind]int{RefreshServiceStatus: 0}, p.Refresh, "a backup with both keeps the new one")

	svc, db, _ := newTestService(t)
	part := NewBackupPart(svc)

	p = Portable{}
	require.NoError(t, json.Unmarshal([]byte(`{"refresh": {"githubStatus": 0}}`), &p))

	plan, err := part.PlanPortable(t.Context(), p)
	require.NoError(t, err)
	assert.Equal(t, []backup.Item{{
		Label: "Check service status", Change: backup.ChangeUpdate, Detail: "off (now 30 min)",
	}}, plan.Items)
	assert.Equal(t, 1, part.PortableItems(p))

	applyPlan(t, part, db, plan)

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Zero(t, got.Refresh.ServiceStatus)

	assert.Error(t, json.Unmarshal([]byte(`{"refresh": 3}`), &p), "a backup that can't be read is still refused")
}

func TestPlanPortableTakesTheStatusChoicesAndLeavesOutUnknownIDs(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	db := testutil.DB(t)
	svc := serviceOver(t, db, t.TempDir(), WithEvents(events))
	part := NewBackupPart(svc)

	var p Portable
	require.NoError(t, json.Unmarshal([]byte(`{"serviceStatus": {
		"off": ["claude", "gitlab"],
		"parts": ["github.pulls", "claude.ai", "openai.codex-cli", "openai.future"]
	}}`), &p))

	plan, err := part.PlanPortable(t.Context(), p)
	require.NoError(t, err)
	assert.Equal(t, []string{
		`Service status: "gitlab" isn't a status page or part this version checks, so it was left out.`,
		`Service status: "openai.future" isn't a status page or part this version checks, so it was left out.`,
	}, plan.Notes)
	assert.Equal(t, []string{"Status pages checked", "Parts of status pages watched"}, changed(plan.Plan))

	applyPlan(t, part, db, plan)
	part.Announce(t.Context(), plan)

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []statusprefs.ProviderID{"claude"}, got.ServiceStatus.Off())
	assert.Equal(t, []statusprefs.PartID{"github.pulls", "claude.ai", "openai.codex-cli"}, got.ServiceStatus.Picked(),
		"the parts are the file's, exactly")
	assert.Equal(t, []event.Event{event.StatusRulesChanged{}}, events.All(), "told once")

	again, err := part.PlanPortable(t.Context(), p)
	require.NoError(t, err)
	assert.Empty(t, changed(again.Plan), "importing the same choices again changes nothing")
}
