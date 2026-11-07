package core

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// refuseNewProfiles makes every insert into the profile table fail, the way a full disk
// would.
const refuseNewProfiles = `CREATE TRIGGER refuse_new_profiles BEFORE INSERT ON claude_profiles
BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`

func TestBootstrapStopsWhenTheStarterPromptsCannotBeAdded(t *testing.T) {
	t.Parallel()

	opts := Options{
		Paths:  appdata.NewPaths(filepath.Join(t.TempDir(), "data")),
		Home:   testutil.ClaudeHome(t, ".claude"),
		Runner: noPrograms(),
	}

	first := bootstrap(t, opts)
	_, err := first.DB.ExecContext(t.Context(), "UPDATE prefs SET value = 'not json' WHERE key = 'prompts.seeded'")
	require.NoError(t, err)
	require.NoError(t, first.Close())

	_, err = Bootstrap(t.Context(), opts)
	require.ErrorContains(t, err, "add the starter prompts: read the starter prompts added")
}

func TestBootstrapStopsWhenProfilesCannotBeSynced(t *testing.T) {
	t.Parallel()

	home := testutil.ClaudeHome(t, ".claude")
	opts := Options{Paths: appdata.NewPaths(filepath.Join(t.TempDir(), "data")), Home: home, Runner: noPrograms()}

	first := bootstrap(t, opts)
	_, err := first.DB.ExecContext(t.Context(), refuseNewProfiles)
	require.NoError(t, err)
	require.NoError(t, first.Close())

	testutil.MakeClaudeDirs(t, home, ".claude_work")

	_, err = Bootstrap(t.Context(), opts)
	require.ErrorContains(t, err, "sync profiles")
}

func TestBootstrapWithoutARunnerStillBuildsEveryService(t *testing.T) {
	t.Parallel()

	// Starting runs no program, so the app's own runner is safe to build here.
	c := bootstrap(t, Options{
		Paths: appdata.NewPaths(filepath.Join(t.TempDir(), "data")),
		Home:  testutil.ClaudeHome(t, ".claude"),
	})

	for _, s := range c.Services() {
		assert.NotNil(t, s)
	}

	all, err := c.Prompts.List(t.Context())
	require.NoError(t, err)
	assert.Len(t, all, len(prompts.Starters()))
}

func TestItemStatesAsksWithTheBorrowedToken(t *testing.T) {
	t.Parallel()

	runner := &testutil.Runner{Stdout: "test-token-not-real\n"}
	states := itemStates{
		tokens: github.NewTokenSource(runner, testutil.Finder{Path: "/opt/fake/bin/gh"}),
		client: github.NewClient("test"),
	}

	// With no items to ask about, no request is sent once the token is borrowed.
	got, err := states.ItemStates(t.Context(), nil)
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Equal(t, [][]string{{"/opt/fake/bin/gh", "auth", "token", "--hostname", "github.com"}}, runner.Calls())
}
