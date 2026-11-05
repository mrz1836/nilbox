package model_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/profiles/model"
)

func TestAllModelsTableNames(t *testing.T) {
	models := model.AllModels()
	require.Len(t, models, 1)

	profile, ok := models[0].(*model.ClaudeProfile)
	require.True(t, ok)
	assert.Equal(t, "claude_profiles", profile.TableName())
}

func TestStringListValue(t *testing.T) {
	tests := []struct {
		name string
		list model.StringList
		want string
	}{
		{name: "nil is an empty array", list: nil, want: "[]"},
		{name: "empty", list: model.StringList{}, want: "[]"},
		{name: "values keep order", list: model.StringList{"--verbose", "a b", `quote"d`}, want: `["--verbose","a b","quote\"d"]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.list.Value()
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestStringMapValue(t *testing.T) {
	got, err := model.StringMap(nil).Value()
	require.NoError(t, err)
	assert.Equal(t, "{}", got)

	got, err = model.StringMap{"B": "2", "A": "1"}.Value()
	require.NoError(t, err)
	assert.JSONEq(t, `{"A":"1","B":"2"}`, got.(string))
}

func TestStringListScan(t *testing.T) {
	tests := []struct {
		name    string
		src     any
		want    model.StringList
		wantErr string
	}{
		{name: "text", src: `["one","two"]`, want: model.StringList{"one", "two"}},
		{name: "bytes", src: []byte(`["one"]`), want: model.StringList{"one"}},
		{name: "empty array", src: `[]`, want: model.StringList{}},
		{name: "malformed", src: `[1,`, wantErr: "decode JSON column"},
		{name: "wrong shape", src: `{"a":"b"}`, wantErr: "decode JSON column"},
		{name: "unsupported type", src: int64(3), wantErr: "unsupported column value"},
		{name: "null", src: nil, wantErr: "unsupported column value"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got model.StringList

			err := got.Scan(tt.src)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestStringMapScan(t *testing.T) {
	var got model.StringMap

	require.NoError(t, got.Scan(`{"KEY":"value"}`))
	assert.Equal(t, model.StringMap{"KEY": "value"}, got)

	err := got.Scan(`["not","an","object"]`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode JSON column")

	require.ErrorIs(t, got.Scan(3.5), model.ErrUnsupportedScanType)
}
