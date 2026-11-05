package model_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/inbox/model"
)

// tabler is implemented by every model.
type tabler interface{ TableName() string }

func TestAllModelsTableNames(t *testing.T) {
	models := model.AllModels()
	names := make([]string, 0, len(models))

	for _, m := range models {
		tm, ok := m.(tabler)
		require.True(t, ok, "%T has no TableName", m)

		names = append(names, tm.TableName())
	}

	assert.Equal(t, []string{"accounts", "repos", "items", "item_local_state", "rate_snapshots"}, names)
}

func TestItemKinds(t *testing.T) {
	assert.Equal(t, "pr", model.KindPR)
	assert.Equal(t, "issue", model.KindIssue)
}
