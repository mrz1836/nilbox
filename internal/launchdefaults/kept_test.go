package launchdefaults_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// Values the copy and restore tests set.
const (
	opusModel   = "opus"
	exampleRepo = "example-repo"
)

// keptPath returns where a test's copy of the launch defaults goes.
func keptPath(t *testing.T) string {
	t.Helper()

	return filepath.Join(t.TempDir(), "launch-defaults.json")
}

// keeping returns a Service over the harness's stores that keeps its copy at path.
func (h *harness) keeping(path string, logs *testutil.Logs) *launchdefaults.Service {
	cfg := launchdefaults.Config{
		Store: h.store, Profiles: h.known, Prompts: h.library, File: launchdefaults.NewFile(path, "v1.2.3", nil),
	}

	if logs != nil {
		cfg.Logger = logs.Logger()
	}

	return launchdefaults.NewService(cfg)
}

// load reads the copy at path.
func load(t *testing.T, path string) []launchdefaults.Kept {
	t.Helper()

	kept, err := launchdefaults.NewFile(path, "", nil).Load()
	require.NoError(t, err)

	return kept
}

// nicknames finds the starters by nickname.
func (h *harness) promptID(nickname string) (int64, bool) {
	switch nickname {
	case "Review":
		return h.review, true
	case "Work on it":
		return h.work, true
	default:
		return 0, false
	}
}

func TestEveryChangeRewritesTheCopy(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	path := keptPath(t)
	svc := h.keeping(path, nil)
	on := true

	_, err := svc.Save(t.Context(), launchdefaults.Input{
		Owner: acme, Profile: "work", PRPromptID: &h.review, IssuePromptID: &h.work, Model: opusModel, Effort: "xhigh",
		Worktree: &on, ExtraFlags: "--verbose",
	})
	require.NoError(t, err)

	assert.Equal(t, []launchdefaults.Kept{{
		Owner: acme, Profile: "work", PRPrompt: "Review", IssuePrompt: "Work on it", Model: opusModel, Effort: "xhigh",
		Worktree: &on, ExtraArgs: []string{"--verbose"},
	}}, load(t, path), "prompts are named by nickname")

	_, err = svc.Save(t.Context(), launchdefaults.Input{Owner: acme, Name: exampleRepo, Model: "sonnet"})
	require.NoError(t, err)
	assert.Len(t, load(t, path), 2)

	_, err = svc.Save(t.Context(), launchdefaults.Input{Owner: acme, Name: exampleRepo})
	require.NoError(t, err)
	assert.Len(t, load(t, path), 1, "saving nothing removes the repository's defaults")

	require.NoError(t, svc.Remove(t.Context(), acme, ""))
	assert.Empty(t, load(t, path))
}

func TestACopyThatCannotBeWrittenIsLoggedNotFailed(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	blocked := filepath.Join(t.TempDir(), "launch-defaults.json")
	require.NoError(t, os.Mkdir(blocked, appdata.PrivateDirPerm))
	require.NoError(t, os.WriteFile(filepath.Join(blocked, "inside"), nil, appdata.PrivateFilePerm))

	var logs testutil.Logs

	_, err := h.keeping(blocked, &logs).Save(t.Context(), launchdefaults.Input{Owner: acme, Model: opusModel})
	require.NoError(t, err, "the defaults are stored even when their copy cannot be written")
	assert.Contains(t, logs.String(), "could not save the copy of the launch defaults")
}

func TestARefusedChangeLeavesTheCopy(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	path := keptPath(t)

	_, err := h.keeping(path, nil).Save(t.Context(), launchdefaults.Input{Owner: acme, Model: "-x"})
	require.ErrorIs(t, err, launchdefaults.ErrInvalidDefaults)
	assert.NoFileExists(t, path)

	require.ErrorIs(t, h.keeping(path, nil).Remove(t.Context(), acme, ""), launchdefaults.ErrNotFound)
	assert.NoFileExists(t, path)
}

func TestLoadRefusesWhatIsNotACopy(t *testing.T) {
	t.Parallel()

	path := keptPath(t)
	file := launchdefaults.NewFile(path, "", nil)

	_, err := file.Load()
	require.ErrorIs(t, err, os.ErrNotExist)

	for text, want := range map[string]error{
		"{ not json":                                                           launchdefaults.ErrNotKept,
		`{"format": "other", "version": 1}`:                                    launchdefaults.ErrNotKept,
		`{"format": "` + launchdefaults.KeptFormat + `"}`:                      launchdefaults.ErrNotKept,
		`{"format": "` + launchdefaults.KeptFormat + `", "version": 2}`:        launchdefaults.ErrNewerKept,
		`{"format": "` + launchdefaults.KeptFormat + `", "version": 1, "x":1}`: nil,
	} {
		testutil.WriteFile(t, path, text)

		got, loadErr := file.Load()
		if want == nil {
			require.NoError(t, loadErr, text)
			assert.NotNil(t, got, "the list is never nil")

			continue
		}

		require.ErrorIs(t, loadErr, want, text)
	}
}

func TestRestoreMatchesPromptsByNickname(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	off := false

	n, err := launchdefaults.Restore(t.Context(), h.store, []launchdefaults.Kept{
		{Owner: acme, Profile: "gone", PRPrompt: "Review", IssuePrompt: "Renamed away", PlanMode: &off},
		{Owner: acme, Name: exampleRepo, Model: "sonnet", Effort: "huge", Agent: "codex", ExtraArgs: []string{"--print"}},
		{Owner: "other-org", IssuePrompt: "Renamed away"},
		{Owner: "not an org!", Model: opusModel},
		{Owner: acme, Name: "../escape", Model: opusModel},
	}, h.promptID)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	all, err := h.svc.List(t.Context())
	require.NoError(t, err)
	require.Len(t, all, 2)

	org := all[0]
	assert.Equal(t, acme, org.Owner)
	require.NotNil(t, org.PRPromptID)
	assert.Equal(t, h.review, *org.PRPromptID)
	assert.Nil(t, org.IssuePromptID, "a prompt no longer there inherits")
	assert.Equal(t, "gone", org.Profile, "a profile is kept by name, for when it is back")
	require.NotNil(t, org.PlanMode)
	assert.False(t, *org.PlanMode)

	repo := all[1]
	assert.Equal(t, "sonnet", repo.Model)
	assert.Empty(t, repo.Effort, "a setting this build refuses is left out")
	assert.Empty(t, repo.Agent)
	assert.Empty(t, repo.ExtraArgs)
}

func TestRestoreLeavesLaunchDefaultsThatExist(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	_, err := h.svc.Save(t.Context(), launchdefaults.Input{Owner: acme, Model: opusModel})
	require.NoError(t, err)

	n, err := launchdefaults.Restore(t.Context(), h.store, []launchdefaults.Kept{{Owner: "other-org", Model: "sonnet"}}, h.promptID)
	require.NoError(t, err)
	assert.Zero(t, n)

	all, err := h.svc.List(t.Context())
	require.NoError(t, err)
	assert.Len(t, all, 1)
}

func TestRestoreIsAllOrNothing(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	_, err := h.db.ExecContext(t.Context(), `CREATE TRIGGER refuse_second BEFORE INSERT ON launch_defaults
WHEN NEW.owner = 'second-org' BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`)
	require.NoError(t, err)

	_, err = launchdefaults.Restore(t.Context(), h.store, []launchdefaults.Kept{
		{Owner: acme, Model: opusModel}, {Owner: "second-org", Model: opusModel},
	}, h.promptID)
	require.ErrorContains(t, err, "restore the launch defaults")

	all, err := h.svc.List(t.Context())
	require.NoError(t, err)
	assert.Empty(t, all)
}

func TestReadSnapshotReadsAnOlderSchema(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "older.db")

	raw, err := sql.Open("sqlite", path)
	require.NoError(t, err)

	for _, stmt := range []string{
		// Launch defaults before the coding tool could be chosen, and with no extra flags.
		`CREATE TABLE launch_defaults (owner TEXT, name TEXT, profile TEXT, pr_prompt_id INTEGER, issue_prompt_id INTEGER,
			model TEXT, effort TEXT, plan_mode INTEGER, worktree INTEGER)`,
		`INSERT INTO launch_defaults VALUES ('acme-org', '', 'work', 4, NULL, 'opus', 'xhigh', NULL, 1)`,
	} {
		_, err = raw.ExecContext(t.Context(), stmt)
		require.NoError(t, err, stmt)
	}

	require.NoError(t, raw.Close())

	got, err := launchdefaults.ReadSnapshot(t.Context(), path, func(id int64) string {
		if id == 4 {
			return "Review"
		}

		return ""
	})
	require.NoError(t, err)

	on := true
	assert.Equal(t, []launchdefaults.Kept{{
		Owner: acme, Profile: "work", PRPrompt: "Review", Model: opusModel, Effort: "xhigh", Worktree: &on,
		ExtraArgs: []string{},
	}}, got)

	_, err = launchdefaults.ReadSnapshot(t.Context(), filepath.Join(t.TempDir(), "missing.db"), nil)
	require.ErrorIs(t, err, os.ErrNotExist)
}
