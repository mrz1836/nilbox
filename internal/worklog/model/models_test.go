package model_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/worklog/model"
)

func TestAllModelsTableNames(t *testing.T) {
	t.Parallel()

	models := model.AllModels()
	require.Len(t, models, 2)

	launch, ok := models[0].(*model.WorktreeLaunch)
	require.True(t, ok)
	assert.Equal(t, "worktree_launches", launch.TableName())

	cleanup, ok := models[1].(*model.WorktreeCleanup)
	require.True(t, ok)
	assert.Equal(t, "worktree_cleanups", cleanup.TableName())
}
