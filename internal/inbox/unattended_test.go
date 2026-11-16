package inbox

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/trust"
)

func TestAnUnattendedStartChecksEveryPullRequestTheUserDidntOpen(t *testing.T) {
	t.Parallel()

	bot := from(4, "dependabot", "NONE", false)
	bot.AuthorType = trust.TypeBot

	h := filesHarness(t, from(1, "teammate", "MEMBER", false), from(3, octoUser, "NONE", false), bot)
	snap := h.refresh(t)
	faces := NewRoutines(h.svc)

	botID := itemByNumber(t, snap.ReviewRequested, 4).ID
	_, err := h.svc.CheckFiles(t.Context(), botID)
	require.ErrorIs(t, err, sensitive.ErrNotChecked, "a click checks no trusted bot's files")

	check, err := faces.CheckFilesUnattended(t.Context(), botID)
	require.NoError(t, err, "a routine's start checks a trusted bot's pull request")

	_, agentFiles := check.Category(sensitive.CategoryAgent)
	assert.True(t, agentFiles, "so one that changes .claude/settings.json is found, and waits for the user")

	member, err := faces.CheckFilesUnattended(t.Context(), itemByNumber(t, snap.ReviewRequested, 1).ID)
	require.NoError(t, err, "and a member's")
	assert.Equal(t, headA, member.Head)

	_, err = faces.CheckFilesUnattended(t.Context(), itemByNumber(t, snap.ReviewRequested, 3).ID)
	require.ErrorIs(t, err, sensitive.ErrNotChecked, "but never one the user opened")
}
