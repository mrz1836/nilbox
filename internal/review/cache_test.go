package review

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/store"
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

	encoded, err := store.EncodeJSON(pc)
	require.NoError(t, err)
	assert.NotContains(t, encoded, "firstCommit", "a context written now keeps no commit message")
}
