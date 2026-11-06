package model_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/launchlog/model"
)

func TestAllModelsTableNames(t *testing.T) {
	models := model.AllModels()
	require.Len(t, models, 1)

	launch, ok := models[0].(*model.WorktreeLaunch)
	require.True(t, ok)
	assert.Equal(t, "worktree_launches", launch.TableName())
}
