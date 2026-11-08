package core

import (
	"bytes"
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/appinfo"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// olderBuild is a database as a build whose migrations were later combined into fewer
// files left it: a history this build disagrees with, so it is set aside, holding the
// prompt library the user built in it.
func olderBuild() []string {
	return []string{
		`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, checksum TEXT NOT NULL, applied_at TEXT NOT NULL)`,
		`INSERT INTO schema_migrations VALUES (1, 'init', 'e0a22b503faef27899852e69e1833fbfa9de8094da4da0e0bf55bda9933ce60b', '2026-11-05T18:04:23Z')`,
		`INSERT INTO schema_migrations VALUES (4, 'prompts', '7b9017a1fed8c512701f6e7591d0d9b3e6f0b003b5ce3b87db752f92e7acecbd', '2026-11-05T22:14:47Z')`,
		`CREATE TABLE prefs (key TEXT NOT NULL PRIMARY KEY, value TEXT NOT NULL, updated_at DATETIME NOT NULL)`,
		`CREATE TABLE prompts (
		id INTEGER PRIMARY KEY, name TEXT NOT NULL, nickname TEXT NOT NULL COLLATE NOCASE UNIQUE,
		applies_to TEXT NOT NULL, body TEXT NOT NULL, model TEXT NOT NULL DEFAULT '', effort TEXT NOT NULL DEFAULT '',
		plan_mode INTEGER, worktree INTEGER, extra_args TEXT NOT NULL DEFAULT '[]', starter_key TEXT NOT NULL DEFAULT '',
		created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)`,
		// The review starter, edited; the quick-look starter, never touched; the user's own
		// prompt; the fix-checks starter was deleted.
		`INSERT INTO prompts VALUES (1, 'Review a pull request', 'Review', 'pr',
		'Review #{{.Number}} in {{.Repo}} my way: {{.URL}}', '', '', NULL, NULL, '[]', 'review-pr',
		'2026-11-05 22:14:47.975532+00:00', '2026-11-06 17:21:24.828823+00:00')`,
		`INSERT INTO prompts VALUES (3, 'Take a quick look for blockers', 'Quick look', 'pr',
		'An older quick look at {{.URL}}', 'sonnet', 'low', NULL, 0, '[]', 'quick-look',
		'2026-11-05 22:14:47.975714+00:00', '2026-11-05 22:14:47.975714+00:00')`,
		`INSERT INTO prompts VALUES (7, 'Security pass', 'Security', 'any',
		'Look for security problems in {{.URL}}', 'opus', 'high', 1, NULL, '["--verbose"]', '',
		'2026-11-07 09:00:00+00:00', '2026-11-07 09:30:00+00:00')`,
		`INSERT INTO prefs VALUES ('prompts.seeded', '["review-pr","work-on-issue","quick-look","fix-checks"]', '2026-11-05 22:14:47+00:00')`,
		`INSERT INTO prefs VALUES ('prompts.default.pr', '7', '2026-11-07 09:31:00+00:00')`,
	}
}

// writeOlderBuild writes the olderBuild database at path.
func writeOlderBuild(t *testing.T, path string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), appdata.PrivateDirPerm))

	raw, err := sql.Open("sqlite", path)
	require.NoError(t, err)

	for _, stmt := range olderBuild() {
		_, err = raw.ExecContext(t.Context(), stmt)
		require.NoError(t, err, stmt)
	}

	require.NoError(t, raw.Close())
}

// libraryByNickname returns the library's prompts by nickname.
func libraryByNickname(t *testing.T, c *Core) map[string]prompts.Prompt {
	t.Helper()

	all, err := c.Prompts.List(t.Context())
	require.NoError(t, err)

	out := map[string]prompts.Prompt{}
	for _, p := range all {
		out[p.Nickname] = p
	}

	return out
}

func TestAnUpdateThatSetsTheDatabaseAsideKeepsThePrompts(t *testing.T) {
	t.Parallel()

	home := testutil.ClaudeHome(t, ".claude")
	paths := appdata.NewPaths(filepath.Join(home, "data"))
	writeOlderBuild(t, paths.DB)

	c := bootstrap(t, Options{Paths: paths, Home: home, Runner: noPrograms()})
	info := c.AppInfo.Info()

	require.NotNil(t, info.DataReset, "the database is set aside")
	assert.Equal(t, appinfo.ResetHistory, info.DataReset.Reason)

	require.NotNil(t, info.PromptsRestored, "the window is told the prompts came back")
	assert.Equal(t, 3, info.PromptsRestored.Count)
	assert.Equal(t, info.DataReset.Path, info.PromptsRestored.From, "they came from the database set aside")
	assert.Regexp(t, `^~/data/`+regexp.QuoteMeta(filepath.Base(paths.DB))+`\.broken-`, info.PromptsRestored.From)

	lib := libraryByNickname(t, c)
	assert.Len(t, lib, 3, "the three prompts, and no starter the user deleted")

	assert.Equal(t, "Review #{{.Number}} in {{.Repo}} my way: {{.URL}}", lib["Review"].Body, "the edit is kept")
	assert.Equal(t, prompts.StarterReviewPR, lib["Review"].StarterKey, "it can still be reset to the original")

	security := lib["Security"]
	assert.Equal(t, "Look for security problems in {{.URL}}", security.Body)
	assert.Equal(t, "opus", security.Model)
	assert.Equal(t, "high", security.Effort)
	require.NotNil(t, security.PlanMode)
	assert.True(t, *security.PlanMode)
	assert.Nil(t, security.Worktree)
	assert.Equal(t, []string{"--verbose"}, security.ExtraArgs)

	assert.NotContains(t, lib, "Fix checks", "a starter the user deleted stays deleted")
	assert.NotContains(t, lib, "Work on it", "a starter the user deleted stays deleted")

	defaults, err := c.Prompts.Defaults(t.Context())
	require.NoError(t, err)
	assert.Equal(t, security.ID, defaults.PR, "the default for pull requests is the one the user chose")

	saved, err := prompts.NewLibraryFile(paths.Prompts, "", nil).Load()
	require.NoError(t, err, "the restored library is saved beside the database")
	assert.Len(t, saved.Prompts, 3)
}

func TestBootstrapBringsThePromptsBackFromTheLibraryFile(t *testing.T) {
	t.Parallel()

	home := testutil.ClaudeHome(t, ".claude")
	paths := appdata.NewPaths(filepath.Join(home, "data"))
	opts := Options{Paths: paths, Home: home, Runner: noPrograms()}

	c := bootstrap(t, opts)
	mine, err := c.Prompts.Create(t.Context(), prompts.Input{
		Name: "Triage the issue", Nickname: "Triage", AppliesTo: prompts.AppliesToIssue, Body: "Triage {{.URL}}",
	})
	require.NoError(t, err)
	require.NoError(t, c.Prompts.SetDefault(t.Context(), "issue", mine.ID))
	require.NoError(t, c.Close())

	// The database is lost: deleted, or never copied to a new Mac.
	for _, suffix := range []string{"", "-wal", "-shm"} {
		require.NoError(t, os.RemoveAll(paths.DB+suffix))
	}

	again := bootstrap(t, opts)
	info := again.AppInfo.Info()

	assert.Nil(t, info.DataReset, "nothing was set aside")
	require.NotNil(t, info.PromptsRestored)
	assert.Equal(t, "~/data/prompts.json", info.PromptsRestored.From)
	assert.Equal(t, len(prompts.Starters())+1, info.PromptsRestored.Count)

	lib := libraryByNickname(t, again)
	require.Contains(t, lib, "Triage")
	assert.Len(t, lib, len(prompts.Starters())+1, "the starters are not added a second time")

	defaults, err := again.Prompts.Defaults(t.Context())
	require.NoError(t, err)
	assert.Equal(t, lib["Triage"].ID, defaults.Issue)
}

func TestBootstrapLeavesTheLibraryFileAsTheLastChangeWroteIt(t *testing.T) {
	t.Parallel()

	home := testutil.ClaudeHome(t, ".claude")
	paths := appdata.NewPaths(filepath.Join(home, "data"))
	opts := Options{Paths: paths, Home: home, Runner: noPrograms()}

	c := bootstrap(t, opts)
	require.FileExists(t, paths.Prompts, "the first start writes the file")

	_, err := c.Prompts.Create(t.Context(), prompts.Input{
		Name: "Mine", Nickname: "Mine", AppliesTo: prompts.AppliesToAny, Body: "Look at {{.URL}}",
	})
	require.NoError(t, err)

	written, err := os.ReadFile(paths.Prompts)
	require.NoError(t, err)
	assert.Contains(t, string(written), `"nickname": "Mine"`, "a change rewrites the file")

	// The prompt disappears from the database behind the app's back, as a faulty update
	// might lose it. The next start must not copy the loss over the file.
	_, err = c.DB.ExecContext(t.Context(), `DELETE FROM prompts WHERE nickname = 'Mine'`)
	require.NoError(t, err)
	require.NoError(t, c.Close())

	bootstrap(t, opts)

	after, err := os.ReadFile(paths.Prompts)
	require.NoError(t, err)
	assert.Equal(t, string(written), string(after), "the file still holds the prompt")
}

func TestBootstrapStartsWhenNoPromptsCanBeBroughtBack(t *testing.T) {
	t.Parallel()

	home := testutil.ClaudeHome(t, ".claude")
	paths := appdata.NewPaths(filepath.Join(home, "data"))
	testutil.WriteFile(t, paths.DB, "this is not a database file, just text long enough to have a header")
	testutil.WriteFile(t, paths.Prompts, "{ not json")

	var logs bytes.Buffer

	c := bootstrap(t, Options{
		Paths: paths, Home: home, Runner: noPrograms(), Logger: slog.New(slog.NewTextHandler(&logs, nil)),
	})

	info := c.AppInfo.Info()
	require.NotNil(t, info.DataReset)
	assert.Nil(t, info.PromptsRestored)
	assert.Contains(t, logs.String(), "could not bring back all of the prompt library")

	assert.Len(t, libraryByNickname(t, c), len(prompts.Starters()), "the app starts with the starters")

	written, err := os.ReadFile(paths.Prompts)
	require.NoError(t, err)
	assert.Equal(t, "{ not json", string(written), "an unreadable copy is left for the user, never overwritten")
}
