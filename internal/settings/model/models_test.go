package model_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/settings/model"
)

func TestAllModelsTableNames(t *testing.T) {
	models := model.AllModels()
	require.Len(t, models, 1)

	pref, ok := models[0].(*model.Pref)
	require.True(t, ok)
	assert.Equal(t, "prefs", pref.TableName())
}
