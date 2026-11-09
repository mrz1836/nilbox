package desktop_test

import (
	"fmt"
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

	assert.Contains(t, pageTextSizeSource(t), "'"+desktop.TextSizeParam+"'",
		"the page must read the text size from the parameter the shell sets")
}

func TestThePageScalesTextAsTheShellSizesTheWindow(t *testing.T) {
	t.Parallel()

	source := pageTextSizeSource(t)

	// The shell sizes the window's minimum by each text size's scale, so the page must
	// scale its text by the same.
	for _, size := range []settings.TextSize{settings.TextSizeDefault, settings.TextSizeLarge, settings.TextSizeLarger} {
		want := fmt.Sprintf("{ value: '%s', percent: %d }", size, size.Percent())
		assert.Contains(t, source, want, "frontend/src/lib/textSize.ts must scale %s as settings.TextSize.Percent does", size)
	}
}

// pageTextSizeSource returns the frontend's text size module.
func pageTextSizeSource(t *testing.T) string {
	t.Helper()

	source, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "lib", "textSize.ts"))
	require.NoError(t, err)

	return string(source)
}
