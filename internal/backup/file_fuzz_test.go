package backup

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// FuzzDecodeBackup checks that any file either fails as not a backup or as one from a
// newer build, or reads into a backup of this format and a version this build reads, each
// section an object or absent, which the app writes and reads back as the same backup.
func FuzzDecodeBackup(f *testing.F) {
	header := `{"format": "` + Format + `", "version": `

	for _, seed := range []string{
		"",
		"null",
		"[]",
		`{"format": "other", "version": 1}`,
		header + `0}`,
		header + `2}`,
		header + `1}`,
		header + `1, "settings": null, "scripts": {"scripts": []}}`,
		header + `1, "settings": {"theme": "dark"}, "repositories": {"pinned": ["acme-org"]}}`,
		header + `1, "prompts": {"format": "x.prompts", "version": 3, "prompts": [{"body": "<b> & <"}]}}`,
		header + `1, "launchDefaults": [1, 2]}`,
		header + `1, "scripts": "a word", "exportedAt": "2026-11-14T17:00:00Z", "app": "v0.4.0", "unknown": {}}`,
	} {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		got, err := Decode(data)
		if err != nil {
			assert.True(t, errors.Is(err, ErrNotBackup) || errors.Is(err, ErrNewerBackup) || errors.Is(err, ErrTooLarge),
				"%v", err)
			assert.Equal(t, File{}, got)

			return
		}

		checkDecoded(t, got)
	})
}

// checkDecoded checks a backup Decode read: of this format and a version this build reads,
// each section an object or absent, and read back the same once the app writes it.
func checkDecoded(t *testing.T, got File) {
	t.Helper()

	assert.Equal(t, Format, got.Format)
	assert.True(t, got.Version >= 1 && got.Version <= Version, "version %d", got.Version)

	for _, s := range Sections() {
		if raw := got.Section(s); raw != nil {
			assert.Equal(t, byte('{'), bytes.TrimSpace(raw)[0], "%s is an object", s)
		}
	}

	encoded, err := Encode(got)
	require.NoError(t, err)

	again, err := Decode(encoded)
	require.NoError(t, err, "what the app writes, it reads")
	assert.Equal(t, got.ExportedAt, again.ExportedAt)
	assert.Equal(t, got.App, again.App)

	for _, s := range Sections() {
		assert.Equal(t, compact(t, got.Section(s)), compact(t, again.Section(s)), "%s reads back the same", s)
	}
}

// compact returns raw with no space between its tokens, or "" for no section.
func compact(t *testing.T, raw json.RawMessage) string {
	t.Helper()

	if raw == nil {
		return ""
	}

	var buf bytes.Buffer
	require.NoError(t, json.Compact(&buf, raw))

	return buf.String()
}
