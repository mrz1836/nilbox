package launchdefaults_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestTheOutsideContributorsChoiceIsALaunchDefaultOfItsOwn(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ctx := t.Context()

	// Set alone, it is a row: Save keeps it, though every other setting inherits.
	on, err := h.svc.Save(ctx, launchdefaults.Input{Owner: acme, AutopilotOutside: launchdefaults.OutsideOn})
	require.NoError(t, err)
	assert.Equal(t, launchdefaults.OutsideOn, on.AutopilotOutside)

	all, err := h.svc.List(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1, "a row whose input is the choice alone isn't empty")

	assert.False(t, launchdefaults.Input{AutopilotOutside: launchdefaults.OutsideOff}.Empty())
	assert.True(t, launchdefaults.Input{}.Empty())

	// The save's upsert replaces it with the rest.
	off, err := h.svc.Save(ctx, launchdefaults.Input{
		Owner: acme, AutopilotOutside: launchdefaults.OutsideOff, Effort: "high",
	})
	require.NoError(t, err)
	assert.Equal(t, launchdefaults.OutsideOff, off.AutopilotOutside)

	// Back to inherit, with nothing else set, the row goes.
	_, err = h.svc.Save(ctx, launchdefaults.Input{Owner: acme})
	require.NoError(t, err)

	all, err = h.svc.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, all)

	_, err = h.svc.Save(ctx, launchdefaults.Input{Owner: acme, AutopilotOutside: "always"})

	var invalid *agent.ValidationError
	require.ErrorAs(t, err, &invalid)
	assert.Equal(t, launchdefaults.FieldOutside, invalid.Fields[0].Field)
}

func TestARepositorysOutsideContributorsChoiceWinsOverItsOrgs(t *testing.T) {
	t.Parallel()

	api, web := repo.Name{Owner: acme, Repo: "api"}, repo.Name{Owner: acme, Repo: "site"}

	idx := launchdefaults.NewIndex([]launchdefaults.Defaults{
		{Owner: acme, AutopilotOutside: launchdefaults.OutsideOn},
		{Owner: acme, Name: "api", AutopilotOutside: launchdefaults.OutsideOff},
		{Owner: acme, Name: "docs"},
	})

	allowed, from := idx.OutsideAllowed(api)
	assert.False(t, allowed)
	assert.Equal(t, "repo", from)

	allowed, from = idx.OutsideAllowed(web)
	assert.True(t, allowed, "a repository with no row of its own takes its org's")
	assert.Equal(t, "org", from)

	allowed, from = idx.OutsideAllowed(repo.Name{Owner: acme, Repo: "docs"})
	assert.True(t, allowed, "a row that inherits takes its org's")
	assert.Equal(t, "org", from)

	allowed, from = launchdefaults.NewIndex(nil).OutsideAllowed(api)
	assert.False(t, allowed, "nothing chosen is off: there is no app-wide switch")
	assert.Empty(t, from)
}

func TestTheOutsideContributorsChoiceOutlivesTheDatabase(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ctx := t.Context()

	_, err := h.svc.Save(ctx, launchdefaults.Input{Owner: acme, Name: "api", AutopilotOutside: launchdefaults.OutsideOn})
	require.NoError(t, err)

	file := launchdefaults.NewFile(filepath.Join(t.TempDir(), "launch-defaults.json"), "v0.0.0-test", nil)
	require.NoError(t, file.Save(ctx, h.store, h.library))

	kept, err := file.Load()
	require.NoError(t, err)
	require.Len(t, kept, 1)
	assert.Equal(t, launchdefaults.OutsideOn, kept[0].AutopilotOutside)

	fresh := launchdefaults.NewStore(testutil.DB(t), nil)
	n, err := launchdefaults.Restore(ctx, fresh, kept, func(string) (int64, bool) { return 0, false })
	require.NoError(t, err)
	assert.Equal(t, 1, n, "the choice alone is a row worth restoring")

	back, err := fresh.All(ctx)
	require.NoError(t, err)
	require.Len(t, back, 1)
	assert.Equal(t, launchdefaults.OutsideOn, back[0].AutopilotOutside)
}
