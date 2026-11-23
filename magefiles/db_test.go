//go:build mage

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// fakeSQLCVersion is the sqlc version the db target tests pin.
const fakeSQLCVersion = "v1.2.3"

// sqlcRun is the argv the fake go is called with for `sqlc command` at fakeSQLCVersion.
func sqlcRun(command string) string {
	return "run " + sqlcCommand + "@" + fakeSQLCVersion + " " + command
}

// sqlcRig returns a rig whose checkout holds env as its project env file, with a go that
// answers each call with a; the sqlc command is its third argument.
func sqlcRig(t *testing.T, env string, a answer) *rig {
	t.Helper()

	g := newRig(t)
	testutil.WriteFile(t, g.path(projectEnvFile), env)
	g.tool("go", a)

	return g
}

// failsOn is a fake go's answer that fails the sqlc command command, and runs every other.
func failsOn(command string) answer {
	return func(args []string) testutil.FakeRun {
		if len(args) > 2 && args[2] == command {
			return testutil.FakeRun{Exit: 1}
		}

		return testutil.FakeRun{}
	}
}

// pinnedEnv is a project env file that pins fakeSQLCVersion among other settings.
const pinnedEnv = "# Tool pins\nGO_PRIMARY_VERSION=1.27.x\n" + sqlcVersionKey + "=" + fakeSQLCVersion + "\n"

func TestDBGenerate(t *testing.T) {
	t.Parallel()

	t.Run("runs sqlc generate at the pinned version", func(t *testing.T) {
		t.Parallel()

		g := sqlcRig(t, pinnedEnv, exits(0))

		require.NoError(t, g.runner().runSQLC("generate"))

		assert.Equal(t, []string{sqlcRun("generate")}, g.linesOf("go"))
	})

	t.Run("the environment's pin wins over the env file's", func(t *testing.T) {
		t.Parallel()

		g := sqlcRig(t, pinnedEnv, exits(0))
		g.setenv(sqlcVersionKey, "v9.9.9")

		require.NoError(t, g.runner().runSQLC("generate"))

		assert.Equal(t, []string{"run " + sqlcCommand + "@v9.9.9 generate"}, g.linesOf("go"))
	})

	t.Run("sqlc fails", func(t *testing.T) {
		t.Parallel()

		g := sqlcRig(t, pinnedEnv, exits(3))

		err := g.runner().runSQLC("generate")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "sqlc generate: ")
		assert.Contains(t, err.Error(), "exit code 3")
	})

	t.Run("no pin runs nothing", func(t *testing.T) {
		t.Parallel()

		g := sqlcRig(t, "GO_PRIMARY_VERSION=1.27.x\n", exits(0))

		err := g.runner().runSQLC("generate")

		require.ErrorIs(t, err, errNotPinned)
		require.EqualError(t, err, "the tool version is not pinned: "+projectEnvFile+" has no "+sqlcVersionKey)
		assert.Empty(t, g.lines(), "go must not run without a pinned version")
	})
}

func TestDBCheck(t *testing.T) {
	t.Parallel()

	t.Run("diffs, then vets, at the pinned version", func(t *testing.T) {
		t.Parallel()

		g := sqlcRig(t, pinnedEnv, exits(0))

		require.NoError(t, g.runner().checkQueries())

		assert.Equal(t, []string{sqlcRun("diff"), sqlcRun("vet")}, g.linesOf("go"))
		assert.Contains(t, g.out.String(), "up to date")
	})

	t.Run("stale generated code stops before vet", func(t *testing.T) {
		t.Parallel()

		g := sqlcRig(t, pinnedEnv, failsOn("diff"))

		err := g.runner().checkQueries()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "sqlc diff: ")
		assert.Contains(t, err.Error(), "run magex db:generate")
		assert.Equal(t, []string{sqlcRun("diff")}, g.linesOf("go"))
	})

	t.Run("a query that does not prepare", func(t *testing.T) {
		t.Parallel()

		g := sqlcRig(t, pinnedEnv, failsOn("vet"))

		err := g.runner().checkQueries()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "sqlc vet: ")
		assert.NotContains(t, err.Error(), "db:generate", "regenerating cannot fix a query")
		assert.Equal(t, []string{sqlcRun("diff"), sqlcRun("vet")}, g.linesOf("go"))
	})

	t.Run("no env file runs nothing", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		g.tool("go", exits(0))

		err := g.runner().checkQueries()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "read "+projectEnvFile)
		assert.Empty(t, g.lines(), "go must not run without a pinned version")
	})
}

func TestSQLCVersionReadsTheRepositoryPin(t *testing.T) {
	t.Parallel()

	r, _ := realRunner(t, repoRoot(t), nil)

	got, err := r.pinnedVersion(projectEnvFile, sqlcVersionKey)

	require.NoError(t, err, "the project env file must keep pinning sqlc")
	assert.Regexp(t, `^v\d+\.\d+\.\d+$`, got)
}
