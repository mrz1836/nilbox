package prompts

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// FuzzDecodeBundle checks that any prompt file either fails as not a prompt file, one
// from a newer build, or one too large, or reads into a bundle of this format and a
// version this build reads, whose lists are never nil, and which the app writes and
// reads back as the same bundle.
func FuzzDecodeBundle(f *testing.F) {
	header := `{"format": "` + BundleFormat + `", "version": `

	for _, seed := range []string{
		"",
		"null",
		`{"format": "other", "version": 1}`,
		header + `99}`,
		header + `0}`,
		header + `3, "prompts": null, "startersAdded": null}`,
		header + `1, "prompts": [{"name": "Review", "model": "opus", "extraArgs": ["--verbose", ""]}]}`,
		header + `2, "prompts": [{"models": {"claude": "", "codex": "gpt"}, "extraArgs": {"codex": [], "claude": ["-x"]}}]}`,
		header + `3, "defaults": {"pr": "Review", "replies": {}}, "prompts": [null, {"planMode": true, "worktree": null}]}`,
		header + `3, "prompts": [{"extraArgs": "not arguments"}]}`,
		header + `3, "app": "v0.4.0", "exportedAt": "2026-11-08T17:00:00Z", "unknown": [1, 2], "FORMAT": "x"}`,
	} {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		b, err := DecodeBundle(data)
		if err != nil {
			assert.True(t, errors.Is(err, ErrNotBundle) || errors.Is(err, ErrNewerBundle) || errors.Is(err, ErrBundleTooLarge),
				"%v", err)
			assert.Equal(t, Bundle{}, b)

			return
		}

		assert.Equal(t, BundleFormat, b.Format)
		assert.GreaterOrEqual(t, b.Version, 1)
		assert.LessOrEqual(t, b.Version, BundleVersion)
		assert.LessOrEqual(t, len(b.Prompts), maxBundlePrompts)
		assert.NotNil(t, b.StartersAdded)
		assert.NotNil(t, b.Prompts)

		written, err := EncodeBundle(b)
		require.NoError(t, err)

		again, err := DecodeBundle(written)
		require.NoError(t, err)

		// An empty map of default replies is left out of the file, so it reads back as none.
		if len(b.Defaults.Replies) == 0 {
			b.Defaults.Replies = nil
		}

		assert.Equal(t, b, again)
	})
}
