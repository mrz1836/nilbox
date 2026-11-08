package store_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/store"
)

func TestRowReadsEachValueAsItsType(t *testing.T) {
	t.Parallel()

	on, off := true, false
	at := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)

	row := store.Row{
		"text": "x", "blob": []byte("y"), "int": int64(7), "real": 3.5, "time": at, "null": nil,
		"on": int64(1), "off": int64(0), "bool": true, "list": `["a","b"]`, "bad list": "not json",
		"null list": "null", "rfc": "2026-01-02T03:04:05Z", "stored": "2026-01-02 03:04:05.5+00:00",
		"plain": "2026-01-02 03:04:05", "not a time": "yesterday",
	}

	assert.Equal(t, "x", row.Text("text"))
	assert.Equal(t, "y", row.Text("blob"))
	assert.Equal(t, "7", row.Text("int"))
	assert.Empty(t, row.Text("real"))
	assert.Equal(t, "2026-01-02T03:04:05Z", row.Text("time"))
	assert.Empty(t, row.Text("missing"))

	n, ok := row.Int("int")
	assert.True(t, ok)
	assert.Equal(t, int64(7), n)

	_, ok = row.Int("text")
	assert.False(t, ok)

	assert.Equal(t, &on, row.Bool("on"))
	assert.Equal(t, &off, row.Bool("off"))
	assert.Equal(t, &on, row.Bool("bool"))
	assert.Nil(t, row.Bool("null"))
	assert.Nil(t, row.Bool("text"))

	assert.Equal(t, []string{"a", "b"}, row.Strings("list"))
	assert.Equal(t, []string{}, row.Strings("bad list"))
	assert.Equal(t, []string{}, row.Strings("null list"))
	assert.Equal(t, []string{}, row.Strings("missing"))

	assert.Equal(t, "2026-01-02T03:04:05Z", row.Time("time"))
	assert.Equal(t, "2026-01-02T03:04:05Z", row.Time("rfc"))
	assert.Equal(t, "2026-01-02T03:04:05Z", row.Time("stored"))
	assert.Equal(t, "2026-01-02T03:04:05Z", row.Time("plain"))
	assert.Empty(t, row.Time("not a time"))
	assert.Empty(t, row.Time("int"))
}

func TestRowReadsJSONObjects(t *testing.T) {
	t.Parallel()

	row := store.Row{
		"names": `{"claude":"work","codex":"alt"}`, "lists": `{"claude":["--a","b"]}`, "array": `["a"]`,
		"bad": "not json", "null": nil, "json null": "null",
	}

	assert.Equal(t, map[string]string{"claude": "work", "codex": "alt"}, row.StringMap("names"))
	assert.Equal(t, map[string][]string{"claude": {"--a", "b"}}, row.StringLists("lists"))

	for _, column := range []string{"array", "bad", "null", "json null", "missing"} {
		assert.Equal(t, map[string]string{}, row.StringMap(column), column)
		assert.Equal(t, map[string][]string{}, row.StringLists(column), column)
	}

	assert.True(t, row.Has("null"), "a NULL column is still there")
	assert.False(t, row.Has("missing"))
}
