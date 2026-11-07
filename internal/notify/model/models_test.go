package model_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/notify/model"
)

func TestAllModelsTableNames(t *testing.T) {
	models := model.AllModels()
	require.Len(t, models, 1)

	notification, ok := models[0].(*model.Notification)
	require.True(t, ok)
	assert.Equal(t, "notifications", notification.TableName())
}
