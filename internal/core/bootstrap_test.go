package core

import (
	"database/sql"
	"os"
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
const refuseNewProfiles = `CREATE TRIGGER refuse_new_profiles BEFORE INSERT ON agent_profiles
BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`

// execOn runs a statement on db, failing the test when it fails.
func execOn(t *testing.T, db *sql.DB, query string) {
	t.Helper()

	_, err := db.ExecContext(t.Context(), query)
	require.NoError(t, err)
}

func TestBootstrapStopsWhenAStartupStepFails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string

		// damage breaks the database a first start made, or the home it ran over, so the
		// next start fails at one step.
		damage func(t *testing.T, db *sql.DB, home string)

		want string
	}{
		{
			name: "the starter prompts cannot be added",
			damage: func(t *testing.T, db *sql.DB, _ string) {
				t.Helper()

				execOn(t, db, "UPDATE prefs SET value = 'not json' WHERE key = 'prompts.seeded'")
			},
			want: "add the starter prompts: read the starter prompts added",
		},
		{
			name: "a new profile cannot be stored",
			damage: func(t *testing.T, db *sql.DB, home string) {
				t.Helper()

				execOn(t, db, refuseNewProfiles)
				testutil.MakeClaudeDirs(t, home, ".claude_work")
			},
			want: "sync profiles",
		},
		{
			name: "the profiles cannot be pruned",
			damage: func(t *testing.T, db *sql.DB, _ string) {
				t.Helper()

				execOn(t, db, "DROP TABLE agent_profiles")
			},
			want: "prune profiles",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			home := testutil.ClaudeHome(t, ".claude")
			opts := Options{Paths: appdata.NewPaths(filepath.Join(t.TempDir(), "data")), Home: home, Runner: noPrograms()}

			first := bootstrap(t, opts)
			tt.damage(t, first.DB, home)
			require.NoError(t, first.Close())

			_, err := Bootstrap(t.Context(), opts)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestBootstrapRefusesWhatItCannotStartFrom(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts func(t *testing.T) Options

		// wantIs, when set, is in the error's chain; wantMsg, when set, is in its text.
		wantIs  error
		wantMsg string
	}{
		{
			name:   "no data directory",
			opts:   func(t *testing.T) Options { return Options{Home: t.TempDir()} },
			wantIs: ErrNoDataDir,
		},
		{
			name:   "no home directory",
			opts:   func(t *testing.T) Options { return Options{Paths: appdata.NewPaths(t.TempDir())} },
			wantIs: ErrNoHomeDir,
		},
		{
			name: "a data directory that cannot be created",
			opts: func(t *testing.T) Options {
				file := filepath.Join(t.TempDir(), "file")
				require.NoError(t, os.WriteFile(file, nil, 0o600))

				return Options{Paths: appdata.NewPaths(filepath.Join(file, "data")), Home: t.TempDir()}
			},
			wantMsg: "create data directory",
		},
		{
			name: "a database that cannot be opened",
			opts: func(t *testing.T) Options {
				paths := appdata.NewPaths(t.TempDir())
				paths.DB = filepath.Join(paths.Root, "bad?name.db")

				return Options{Paths: paths, Home: t.TempDir()}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := Bootstrap(t.Context(), tt.opts(t))
			require.Error(t, err)

			if tt.wantIs != nil {
				require.ErrorIs(t, err, tt.wantIs)
			}

			if tt.wantMsg != "" {
				require.ErrorContains(t, err, tt.wantMsg)
			}
		})
	}
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
