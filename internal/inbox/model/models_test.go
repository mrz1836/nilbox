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

	assert.Equal(t, []string{"accounts", "repos", "items", "item_details", "item_local_state", "rate_snapshots"}, names)
}

func TestLabelsRoundTrip(t *testing.T) {
	value, err := model.Labels(nil).Value()
	require.NoError(t, err)
	assert.Equal(t, "[]", value, "no labels are stored as an empty array")

	labels := model.Labels{{Name: "bug", Color: "#d73a4a"}, {Name: "odd"}}
	value, err = labels.Value()
	require.NoError(t, err)
	assert.JSONEq(t, `[{"name":"bug","color":"#d73a4a"},{"name":"odd","color":""}]`, value.(string))

	for _, src := range []any{value, []byte(value.(string))} {
		var got model.Labels
		require.NoError(t, got.Scan(src))
		assert.Equal(t, labels, got)
	}

	var got model.Labels
	require.ErrorIs(t, got.Scan(42), model.ErrUnsupportedScanType)
	require.ErrorContains(t, got.Scan("{not json"), "decode the labels")
}

func TestItemKinds(t *testing.T) {
	assert.Equal(t, "pr", model.KindPR)
	assert.Equal(t, "issue", model.KindIssue)
}
