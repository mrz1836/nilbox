//go:build mage

package main

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// fakeSQLCVersion is the sqlc version the db target tests pin.
const fakeSQLCVersion = "v1.2.3"

// sqlcRun is the argv the fake go logs for `sqlc command` at fakeSQLCVersion.
func sqlcRun(command string) string {
	return "run " + sqlcCommand + "@" + fakeSQLCVersion + " " + command
}

// sqlcTools makes a fresh temporary directory the working directory, writes env as its
// project env file, and puts on an isolated PATH a go that logs each call's arguments and
// runs body, where $3 is the sqlc command. It returns the log's path.
func sqlcTools(t *testing.T, env, body string) string {
	t.Helper()

	dir := inTempDir(t)
	t.Setenv(sqlcVersionKey, "")
	testutil.WriteFile(t, filepath.Join(dir, projectEnvFile), env)

	return fakeLoggedTool(t, isolatedPath(t), "go", body)
}

// pinnedEnv is a project env file that pins fakeSQLCVersion among other settings.
const pinnedEnv = "# Tool pins\nGO_PRIMARY_VERSION=1.26.x\n" + sqlcVersionKey + "=" + fakeSQLCVersion + "\n"

//nolint:paralleltest // changes the process's PATH
func TestDBGenerate(t *testing.T) {
	t.Run("runs sqlc generate at the pinned version", func(t *testing.T) {
		log := sqlcTools(t, pinnedEnv, "")

		require.NoError(t, DB{}.Generate())

		assert.Equal(t, []string{sqlcRun("generate")}, logLines(t, log))
	})

	t.Run("the environment's pin wins over the env file's", func(t *testing.T) {
		log := sqlcTools(t, pinnedEnv, "")
		t.Setenv(sqlcVersionKey, "v9.9.9")

		require.NoError(t, DB{}.Generate())

		assert.Equal(t, []string{"run " + sqlcCommand + "@v9.9.9 generate"}, logLines(t, log))
	})

	t.Run("sqlc fails", func(t *testing.T) {
		sqlcTools(t, pinnedEnv, "exit 3")

		err := DB{}.Generate()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "sqlc generate: ")
		assert.Contains(t, err.Error(), "exit code 3")
	})

	t.Run("no pin runs nothing", func(t *testing.T) {
		log := sqlcTools(t, "GO_PRIMARY_VERSION=1.26.x\n", "")

		err := DB{}.Generate()

		require.ErrorIs(t, err, errNotPinned)
		require.EqualError(t, err, "the tool version is not pinned: "+projectEnvFile+" has no "+sqlcVersionKey)
		assert.NoFileExists(t, log, "go must not run without a pinned version")
	})
}

//nolint:paralleltest // changes the process's working directory, PATH, and standard output
func TestDBCheck(t *testing.T) {
	t.Run("diffs, then vets, at the pinned version", func(t *testing.T) {
		log := sqlcTools(t, pinnedEnv, "")

		out := captureStdout(t, func() { require.NoError(t, DB{}.Check()) })

		assert.Equal(t, []string{sqlcRun("diff"), sqlcRun("vet")}, logLines(t, log))
		assert.Contains(t, out, "up to date")
	})

	t.Run("stale generated code stops before vet", func(t *testing.T) {
		log := sqlcTools(t, pinnedEnv, `if [ "$3" = diff ]; then exit 1; fi`)

		err := DB{}.Check()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "sqlc diff: ")
		assert.Contains(t, err.Error(), "run magex db:generate")
		assert.Equal(t, []string{sqlcRun("diff")}, logLines(t, log))
	})

	t.Run("a query that does not prepare", func(t *testing.T) {
		log := sqlcTools(t, pinnedEnv, `if [ "$3" = vet ]; then exit 1; fi`)

		err := DB{}.Check()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "sqlc vet: ")
		assert.NotContains(t, err.Error(), "db:generate", "regenerating cannot fix a query")
		assert.Equal(t, []string{sqlcRun("diff"), sqlcRun("vet")}, logLines(t, log))
	})

	t.Run("no env file runs nothing", func(t *testing.T) {
		inTempDir(t)
		t.Setenv(sqlcVersionKey, "")
		log := fakeLoggedTool(t, isolatedPath(t), "go", "")

		err := DB{}.Check()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "read "+projectEnvFile)
		assert.NoFileExists(t, log, "go must not run without a pinned version")
	})
}

func TestSQLCVersionReadsTheRepositoryPin(t *testing.T) {
	inRepoRoot(t)
	t.Setenv(sqlcVersionKey, "")

	got, err := pinnedVersion(projectEnvFile, sqlcVersionKey)

	require.NoError(t, err, "the project env file must keep pinning sqlc")
	assert.Regexp(t, `^v\d+\.\d+\.\d+$`, got)
}
