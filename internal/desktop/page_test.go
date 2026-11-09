package desktop_test

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/desktop"
	"github.com/mrz1836/nilbox/internal/settings"
)

func TestPageURLCarriesTheTextSize(t *testing.T) {
	t.Parallel()

	for _, size := range []settings.TextSize{settings.TextSizeDefault, settings.TextSizeLarge, settings.TextSizeLarger} {
		got := desktop.PageURL(size)

		parsed, err := url.Parse(got)
		require.NoError(t, err, got)
		assert.Equal(t, "/", parsed.Path, "the page is the app's root")
		assert.Equal(t, string(size), parsed.Query().Get(desktop.TextSizeParam), got)
	}

	assert.Equal(t, "/?textSize=larger", desktop.PageURL(settings.TextSizeLarger))
}

func TestThePageReadsTheTextSizeParameter(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "lib", "textSize.ts"))
	require.NoError(t, err)

	assert.Contains(t, string(source), "'"+desktop.TextSizeParam+"'",
		"the page must read the text size from the parameter the shell sets")
}
