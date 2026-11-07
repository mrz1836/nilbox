package store_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/store"
)

func TestEncodeJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   any
		want string
	}{
		{name: "a list", in: []string{"b", "a"}, want: `["b","a"]`},
		{name: "an empty list", in: []string{}, want: `[]`},
		{name: "a nil list is an empty one", in: []string(nil), want: `[]`},
		{name: "an object", in: map[string]string{"k": "v"}, want: `{"k":"v"}`},
		{name: "a nil object is an empty one", in: map[string]int(nil), want: `{}`},
		{name: "a struct", in: struct {
			N int `json:"n"`
		}{N: 2}, want: `{"n":2}`},
		{name: "a string", in: "text", want: `"text"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := store.EncodeJSON(tt.in)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEncodeJSONReportsAValueItCannotEncode(t *testing.T) {
	t.Parallel()

	_, err := store.EncodeJSON(make(chan int))
	require.ErrorContains(t, err, "encode chan int as JSON")
}

func TestDecodeJSON(t *testing.T) {
	t.Parallel()

	list, err := store.DecodeJSON[[]string](`["a","b"]`)
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, list)

	object, err := store.DecodeJSON[map[string]int](`{"a":1}`)
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"a": 1}, object)

	for _, text := range []string{"", "not json", `{"a":1}`, `["a",1]`} {
		got, err := store.DecodeJSON[[]string](text)
		require.ErrorContains(t, err, "decode []string from JSON", text)
		assert.Nil(t, got, "a value that cannot be read decodes as the zero value: %q", text)
	}
}

func TestJSONRoundTrips(t *testing.T) {
	t.Parallel()

	for _, list := range [][]string{nil, {}, {"one"}, {"a", "b", "c"}} {
		text, err := store.EncodeJSON(list)
		require.NoError(t, err)

		got, err := store.DecodeJSON[[]string](text)
		require.NoError(t, err)
		assert.Equal(t, append([]string{}, list...), got, "a nil list comes back empty")
	}
}
