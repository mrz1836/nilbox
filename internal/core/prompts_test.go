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

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/appinfo"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// olderBuild is a database as a build whose migrations were later combined into fewer
// files left it: a history this build disagrees with, so it is set aside, holding the
// prompt library and launch defaults the user built in it.
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
		// An org's defaults name the review and security prompts by their IDs here, a
		// repository's set only the model, and one names a prompt that is gone.
		`CREATE TABLE launch_defaults (
		id INTEGER PRIMARY KEY, owner TEXT NOT NULL COLLATE NOCASE, name TEXT NOT NULL DEFAULT '' COLLATE NOCASE,
		agent TEXT NOT NULL DEFAULT '', profile TEXT NOT NULL DEFAULT '', pr_prompt_id INTEGER, issue_prompt_id INTEGER,
		model TEXT NOT NULL DEFAULT '', effort TEXT NOT NULL DEFAULT '', plan_mode INTEGER, worktree INTEGER,
		extra_args TEXT NOT NULL DEFAULT '[]', created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL,
		UNIQUE (owner, name))`,
		`INSERT INTO launch_defaults VALUES (1, 'acme-org', '', '', 'work', 1, 7, 'opus', 'xhigh', 0, 1, '[]',
		'2026-11-06 10:00:00+00:00', '2026-11-06 10:00:00+00:00')`,
		`INSERT INTO launch_defaults VALUES (2, 'acme-org', 'example-repo', '', '', NULL, NULL, 'sonnet', '', NULL, NULL,
		'[]', '2026-11-06 11:00:00+00:00', '2026-11-06 11:00:00+00:00')`,
		`INSERT INTO launch_defaults VALUES (3, 'other-org', '', '', '', 99, NULL, '', '', NULL, NULL, '[]',
		'2026-11-06 12:00:00+00:00', '2026-11-06 12:00:00+00:00')`,
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

// launchDefaultsByTarget returns the stored launch defaults by target.
func launchDefaultsByTarget(t *testing.T, c *Core) map[string]launchdefaults.Defaults {
	t.Helper()

	all, err := c.LaunchDefaults.List(t.Context())
	require.NoError(t, err)

	out := map[string]launchdefaults.Defaults{}
	for _, d := range all {
		out[d.Target()] = d
	}

	return out
}

// agentPrompts counts the agent prompts in lib, leaving out the review replies.
func agentPrompts(lib map[string]prompts.Prompt) int {
	n := 0

	for _, p := range lib {
		if !p.IsReply() {
			n++
		}
	}

	return n
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

	require.NotNil(t, info.Restored, "the window is told what came back")
	assert.Equal(t, 3, info.Restored.Prompts)
	assert.Equal(t, 2, info.Restored.LaunchDefaults, "the org's and the repository's; the third set nothing left")
	assert.Equal(t, info.DataReset.Path, info.Restored.From, "they came from the database set aside")
	assert.Regexp(t, `^~/data/`+regexp.QuoteMeta(filepath.Base(paths.DB))+`\.broken-`, info.Restored.From)

	lib := libraryByNickname(t, c)
	assert.Equal(t, 3, agentPrompts(lib), "the three prompts, and no starter the user deleted")
	assert.Contains(t, lib, "LGTM", "the starter replies, new in this build, are added")

	assert.Equal(t, "Review #{{.Number}} in {{.Repo}} my way: {{.URL}}", lib["Review"].Body, "the edit is kept")
	assert.Equal(t, prompts.StarterReviewPR, lib["Review"].StarterKey, "it can still be reset to the original")

	security := lib["Security"]
	assert.Equal(t, "Look for security problems in {{.URL}}", security.Body)
	assert.Equal(t, map[agent.ID]string{agent.Claude: "opus"}, security.Models, "an older build's model was Claude's")
	assert.Equal(t, "high", security.Effort)
	require.NotNil(t, security.PlanMode)
	assert.True(t, *security.PlanMode)
	assert.Nil(t, security.Worktree)
	assert.Equal(t, map[agent.ID][]string{agent.Claude: {"--verbose"}}, security.ExtraArgs)

	assert.NotContains(t, lib, "Fix checks", "a starter the user deleted stays deleted")
	assert.NotContains(t, lib, "Work on it", "a starter the user deleted stays deleted")

	defaults, err := c.Prompts.Defaults(t.Context())
	require.NoError(t, err)
	assert.Equal(t, security.ID, defaults.PR, "the default for pull requests is the one the user chose")

	saved, err := prompts.NewLibraryFile(paths.Prompts, "", nil).Load()
	require.NoError(t, err, "the restored library is saved beside the database")
	assert.Len(t, saved.Prompts, len(lib))

	// The launch defaults come back pointing at the restored prompts, which have new IDs.
	byTarget := launchDefaultsByTarget(t, c)
	require.Len(t, byTarget, 2)

	org := byTarget["acme-org"]
	require.NotNil(t, org.PRPromptID)
	require.NotNil(t, org.IssuePromptID)
	assert.Equal(t, lib["Review"].ID, *org.PRPromptID)
	assert.Equal(t, security.ID, *org.IssuePromptID)
	assert.Equal(t, map[agent.ID]string{agent.Claude: "work"}, org.Profiles,
		"a profile is kept by name, even one this Mac doesn't have")
	assert.Equal(t, map[agent.ID]string{agent.Claude: "opus"}, org.Models)
	assert.Equal(t, "xhigh", org.Effort)
	require.NotNil(t, org.Worktree)
	assert.True(t, *org.Worktree)
	require.NotNil(t, org.PlanMode)
	assert.False(t, *org.PlanMode)

	assert.Equal(t, map[agent.ID]string{agent.Claude: "sonnet"}, byTarget["acme-org/example-repo"].Models)
	assert.NotContains(t, byTarget, "other-org", "a row whose only setting named a missing prompt sets nothing")

	keptDefaults, err := launchdefaults.NewFile(paths.LaunchDefaults, "", nil).Load()
	require.NoError(t, err, "the restored launch defaults are saved beside the database")
	assert.Len(t, keptDefaults, 2)
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

	_, err = c.LaunchDefaults.Save(t.Context(), launchdefaults.Input{Owner: "acme-org", IssuePromptID: &mine.ID, Effort: "high"})
	require.NoError(t, err)

	// Renaming the prompt rewrites the launch defaults' copy, which names it by nickname.
	in := prompts.Input{Name: mine.Name, Nickname: "Triage it", AppliesTo: mine.AppliesTo, Body: mine.Body}
	_, err = c.Prompts.Update(t.Context(), mine.ID, in)
	require.NoError(t, err)
	require.NoError(t, c.Close())

	// The database is lost: deleted, or never copied to a new Mac.
	for _, suffix := range []string{"", "-wal", "-shm"} {
		require.NoError(t, os.RemoveAll(paths.DB+suffix))
	}

	again := bootstrap(t, opts)
	info := again.AppInfo.Info()

	assert.Nil(t, info.DataReset, "nothing was set aside")
	require.NotNil(t, info.Restored)
	assert.Equal(t, "~/data/prompts.json", info.Restored.From)
	assert.Equal(t, len(prompts.Starters())+1, info.Restored.Prompts)
	assert.Equal(t, 1, info.Restored.LaunchDefaults)

	lib := libraryByNickname(t, again)
	require.Contains(t, lib, "Triage it")
	assert.Len(t, lib, len(prompts.Starters())+1, "the starters are not added a second time")

	defaults, err := again.Prompts.Defaults(t.Context())
	require.NoError(t, err)
	assert.Equal(t, lib["Triage it"].ID, defaults.Issue)

	org := launchDefaultsByTarget(t, again)["acme-org"]
	require.NotNil(t, org.IssuePromptID, "the renamed prompt is still the org's default")
	assert.Equal(t, lib["Triage it"].ID, *org.IssuePromptID)
	assert.Equal(t, "high", org.Effort)
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
	assert.Nil(t, info.Restored)
	assert.Contains(t, logs.String(), "could not bring back all of the prompt library")

	assert.Len(t, libraryByNickname(t, c), len(prompts.Starters()), "the app starts with the starters")

	written, err := os.ReadFile(paths.Prompts)
	require.NoError(t, err)
	assert.Equal(t, "{ not json", string(written), "an unreadable copy is left for the user, never overwritten")
}
