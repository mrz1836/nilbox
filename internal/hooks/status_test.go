package hooks

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadStatusTextReadsEveryRecording(t *testing.T) {
	t.Parallel()

	want := map[string]statusReport{
		"status-ready":                  {hooks: map[string]string{hookPreCommit: statusReady}},
		"status-outdated":               {hooks: map[string]string{hookPreCommit: statusOutdated}},
		"status-not-executable":         {hooks: map[string]string{hookPreCommit: statusReady}},
		"status-other":                  {hooks: map[string]string{hookPreCommit: statusOther}},
		"status-not-installed":          {hooks: map[string]string{}},
		"status-disabled":               {hooks: map[string]string{hookPreCommit: statusReady}, disabled: true},
		"status-commit-msg":             {hooks: map[string]string{hookPreCommit: statusReady, hookCommitMsg: statusReady}},
		"global-hookspath-status-other": {hooks: map[string]string{hookPreCommit: statusReady}},
	}

	names := append(recordingNames(t, "status-"), "global-hookspath-status-other")

	for _, name := range names {
		rec := loadRecording(t, name)
		got, err := readStatusText([]byte(rec.Stdout + rec.Stderr))

		if name == "status-no-config" {
			require.ErrorIs(t, err, errStatusFailed, name)
			assert.Contains(t, err.Error(), "failed to find environment configuration")

			continue
		}

		require.NoError(t, err, name)

		expected, ok := want[name]
		require.True(t, ok, "%s has no expectation", name)
		assert.Equal(t, expected, got, name)
	}
}

func TestReadStatusTextRefusesOutputItDoesntKnow(t *testing.T) {
	t.Parallel()

	_, err := readStatusText([]byte("something else entirely\n"))
	require.ErrorIs(t, err, errStatusOutput)

	got, err := readStatusText([]byte("⚠   ⚠ pre-commit: Go pre-commit hook installed but outdated (warning: not executable)\n" +
		"✓   ✓ pre-push: a message it never printed\n"))
	require.NoError(t, err)
	assert.Equal(t, map[string]string{hookPreCommit: statusOutdated, "pre-push": ""}, got.hooks)
}

func TestReadStatusJSON(t *testing.T) {
	t.Parallel()

	got, err := readStatusJSON([]byte(`{
  "version": "1.14.1", "enabled": false,
  "config": {"mode": "modular", "path": "/work/repo/.github/env"},
  "repository_root": "/work/repo", "hooks_dir": "/work/repo/.git/hooks",
  "hooks": [
    {"name": "pre-commit", "state": "not_executable", "path": "/work/repo/.git/hooks/pre-commit", "executable": false, "outdated": false},
    {"name": "pre-push", "state": "not_executable", "outdated": true},
    {"name": "commit-msg", "state": "ready", "executable": true},
    {"name": "post-commit", "state": "missing"},
    {"name": "other", "state": "other"},
    {"name": "broken", "state": "error", "error": "permission denied"},
    {"name": "old", "state": "outdated"}
  ]
}`))
	require.NoError(t, err)
	assert.Equal(t, statusReport{disabled: true, hooks: map[string]string{
		hookPreCommit: statusReady, "pre-push": statusOutdated, hookCommitMsg: statusReady, "other": statusOther,
		"old": statusOutdated,
	}}, got)

	_, err = readStatusJSON([]byte(`{"version": "1.14.1", "error": {"code": "config_not_found", "message": "no config"}}`))
	require.ErrorIs(t, err, errStatusFailed)
	assert.Contains(t, err.Error(), "no config")

	_, err = readStatusJSON([]byte(`{"version": "1.14.1"}`))
	require.ErrorIs(t, err, errStatusOutput)

	_, err = readStatusJSON([]byte(`not json`))
	require.Error(t, err)
}
