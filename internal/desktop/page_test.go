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
		got := desktop.PageURL(size, false)

		parsed, err := url.Parse(got)
		require.NoError(t, err, got)
		assert.Equal(t, "/", parsed.Path, "the page is the app's root")
		assert.Equal(t, string(size), parsed.Query().Get(desktop.TextSizeParam), got)
		assert.False(t, parsed.Query().Has(desktop.SetupParam), "no setup, no parameter: %s", got)
	}

	assert.Equal(t, "/?textSize=larger", desktop.PageURL(settings.TextSizeLarger, false))
}

func TestPageURLSaysTheSetupShows(t *testing.T) {
	t.Parallel()

	got := desktop.PageURL(settings.TextSizeLarge, true)

	parsed, err := url.Parse(got)
	require.NoError(t, err, got)
	assert.Equal(t, desktop.SetupValue, parsed.Query().Get(desktop.SetupParam))
	assert.Equal(t, string(settings.TextSizeLarge), parsed.Query().Get(desktop.TextSizeParam))
	assert.Equal(t, "/?setup=1&textSize=large", got)
}

func TestThePageReadsTheTextSizeParameter(t *testing.T) {
	t.Parallel()

	assert.Contains(t, pageTextSizeSource(t), "'"+desktop.TextSizeParam+"'",
		"the page must read the text size from the parameter the shell sets")
}

func TestThePageReadsTheSetupParameter(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile(filepath.Join("..", "..", "frontend", "src", "lib", "setup.ts"))
	require.NoError(t, err)
	assert.Contains(t, string(source), "'"+desktop.SetupParam+"'",
		"the page must read whether the setup shows from the parameter the shell sets")
	assert.Contains(t, string(source), "'"+desktop.SetupValue+"'")
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
