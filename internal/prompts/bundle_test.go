package prompts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestBundlePromptReadsEitherVersionsSettings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		data       string
		wantModels map[agent.ID]string
		wantArgs   map[agent.ID][]string
		wantErr    string
	}{
		{
			name:       "version 2's objects keyed by agent",
			data:       `{"models": {"claude": "fable", "codex": "gpt"}, "extraArgs": {"codex": ["--x"], "claude": []}}`,
			wantModels: map[agent.ID]string{agent.Claude: "fable", agent.Codex: "gpt"},
			wantArgs:   map[agent.ID][]string{agent.Codex: {"--x"}},
		},
		{
			name:       "version 1's model and list, which were Claude's",
			data:       `{"model": "fable", "extraArgs": ["--verbose"]}`,
			wantModels: map[agent.ID]string{agent.Claude: "fable"},
			wantArgs:   map[agent.ID][]string{agent.Claude: {"--verbose"}},
		},
		{
			name:       "a version 1 model joins the other agents' models",
			data:       `{"model": "fable", "models": {"codex": "gpt"}}`,
			wantModels: map[agent.ID]string{agent.Claude: "fable", agent.Codex: "gpt"},
			wantArgs:   map[agent.ID][]string{},
		},
		{
			name:       "a version 1 model never replaces Claude's own",
			data:       `{"model": "sonnet", "models": {"claude": "fable"}}`,
			wantModels: map[agent.ID]string{agent.Claude: "fable"},
			wantArgs:   map[agent.ID][]string{},
		},
		{
			name:       "blank models and empty lists are left out",
			data:       `{"models": {"claude": ""}, "extraArgs": {"claude": [], "codex": null}}`,
			wantModels: map[agent.ID]string{},
			wantArgs:   map[agent.ID][]string{},
		},
		{
			name:       "extraArgs padded with spaces",
			data:       `{"extraArgs":   ["--x"]  }`,
			wantModels: map[agent.ID]string{},
			wantArgs:   map[agent.ID][]string{agent.Claude: {"--x"}},
		},
		{
			name:       "a null prompt reads as an empty one",
			data:       `null`,
			wantModels: map[agent.ID]string{},
			wantArgs:   map[agent.ID][]string{},
		},
		{name: "a list of other than words", data: `{"extraArgs": [1, 2]}`, wantErr: "extraArgs"},
		{name: "an object of other than lists", data: `{"extraArgs": {"claude": "--x"}}`, wantErr: "extraArgs"},
		{name: "extraArgs that are neither", data: `{"extraArgs": "--x"}`, wantErr: "extraArgs"},
		{name: "a name that is not text", data: `{"name": 5}`, wantErr: "cannot unmarshal number"},
		{name: "not an object", data: `"Review"`, wantErr: "cannot unmarshal string"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var p BundlePrompt

			err := json.Unmarshal([]byte(tt.data), &p)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantModels, p.Models)
			assert.Equal(t, tt.wantArgs, p.ExtraArgs)
		})
	}
}

func TestBundlePromptKeepsItsOtherFields(t *testing.T) {
	t.Parallel()

	var p BundlePrompt

	require.NoError(t, json.Unmarshal([]byte(`{
		"name": "Review it", "nickname": "Review", "appliesTo": "pr", "body": "Look at {{.URL}}",
		"purpose": "reply", "action": "approve", "effort": "high", "planMode": true, "worktree": false,
		"starterKey": "review-pr", "createdAt": "2026-11-08T17:00:00Z", "updatedAt": "2026-11-09T17:00:00Z"
	}`), &p))

	yes, no := true, false
	assert.Equal(t, BundlePrompt{
		Name: "Review it", Nickname: "Review", AppliesTo: AppliesToPR, Body: "Look at {{.URL}}", Purpose: PurposeReply,
		Action: ReplyApprove, Models: map[agent.ID]string{}, Effort: "high", PlanMode: &yes, Worktree: &no,
		ExtraArgs: map[agent.ID][]string{}, StarterKey: "review-pr", CreatedAt: "2026-11-08T17:00:00Z",
		UpdatedAt: "2026-11-09T17:00:00Z",
	}, p)
}

func TestReadBundleFileRefusesWhatIsNoPromptFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	_, err := readBundleFile(dir)
	require.ErrorIs(t, err, ErrNotBundle, "a folder is not a prompt file")
	require.ErrorIs(t, err, appdata.ErrNotAFile)

	large := filepath.Join(dir, "large.json")
	testutil.WriteFile(t, large, "{}")
	require.NoError(t, os.Truncate(large, maxBundleBytes+1))

	_, err = readBundleFile(large)
	require.ErrorIs(t, err, ErrBundleTooLarge)

	_, err = readBundleFile(filepath.Join(dir, "missing.json"))
	require.ErrorIs(t, err, os.ErrNotExist)
}
