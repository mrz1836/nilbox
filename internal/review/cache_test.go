package review

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/timeline"
)

func TestACachedContextFromAnOlderBuildStillReads(t *testing.T) {
	t.Parallel()

	// An older build kept the first commit's message, which nothing reads any more.
	older := `{"head":"abc","state":"OPEN","mergeState":"CLEAN","reviews":[],` +
		`"firstCommit":"Bump x from 1 to 2\n\nupdate-type: version-update:semver-patch","checks":[]}`

	pc, err := store.DecodeJSON[pullContext](older)
	require.NoError(t, err)
	assert.Equal(t, "abc", pc.Head)
	assert.Equal(t, "OPEN", pc.State)
	assert.Equal(t, "CLEAN", pc.MergeState)

	assert.Empty(t, pc.UpdatedAt, "an older build kept no time of the read")
	assert.Empty(t, pc.EditedAt)
	assert.Nil(t, pc.Reactions)
	assert.Nil(t, pc.Timeline, "nor the conversation, until the sheet reads it again")
	assert.False(t, pc.BodyTrimmed)

	r := build(storedPull(), &cached{head: "abc", context: pc}, nil, PrefRepo)
	assert.Equal(t, []timeline.Entry{}, r.Timeline.Items, "the sheet shows no conversation, never null")
	assert.Equal(t, []timeline.Reaction{}, r.Reactions)
	assert.False(t, recent(storedPull(), &cached{head: "abc", context: pc, contextAt: testNow}, testNow),
		"a context that says nothing of when is read again")

	// A context an older build wrote with a reviewer's text keeps reading too.
	withText, err := store.DecodeJSON[pullContext](`{"head":"abc","reviews":[{"author":"octo-other","body":"Nice."}]}`)
	require.NoError(t, err)
	assert.Equal(t, "octo-other", withText.Reviews[0].Author)

	encoded, err := store.EncodeJSON(pc)
	require.NoError(t, err)
	assert.NotContains(t, encoded, "firstCommit", "a context written now keeps no commit message")
}
