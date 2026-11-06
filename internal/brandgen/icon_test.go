package brandgen

import (
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
)

func TestIconsAreCurrent(t *testing.T) {
	t.Parallel()

	require.NoError(t, CheckIcons(filepath.Join("..", "..")), "run magex brand:icons")
}

func TestIconSVG(t *testing.T) {
	t.Parallel()

	svg := fileContent(t, IconSVGPath)

	assert.True(t, strings.HasPrefix(svg, "<!-- "+Header+" -->\n<svg "))
	assert.Contains(t, svg, "<title>"+brand.Name+"</title>")
	assert.Contains(t, svg, `<path id="mark" d="`+brand.MarkPath+`"/>`)
	assert.Contains(t, svg, `stop-color="`+brand.Dark.Accent+`"`, "the ember glow is the brand accent")
	assert.Contains(t, svg, `stop-color="`+brand.MarkHighlight+`"`, "the glyph catches the mark's highlight")
	assert.Contains(t, svg, `translate(308 256)`, "the mark is centered on the icon grid")
	assert.NotContains(t, svg, "{{", "every field is filled")

	ids := svgIDs(t, svg)

	for _, ref := range regexp.MustCompile(`url\(#([^)]+)\)|href="#([^"]+)"`).FindAllStringSubmatch(svg, -1) {
		id := ref[1] + ref[2]
		assert.Truef(t, ids[id], "the artwork refers to #%s, which it does not define", id)
	}
}

// svgIDs decodes an SVG, failing on malformed XML, and returns the ids it defines.
func svgIDs(t *testing.T, svg string) map[string]bool {
	t.Helper()

	ids := map[string]bool{}
	dec := xml.NewDecoder(strings.NewReader(svg))

	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return ids
		}

		require.NoError(t, err, "the artwork must be well-formed XML")

		el, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}

		for _, attr := range el.Attr {
			if attr.Name.Local == "id" {
				ids[attr.Value] = true
			}
		}
	}
}

func TestIconStamp(t *testing.T) {
	t.Parallel()

	stamp := string(IconStamp())

	assert.Regexp(t, `^[0-9a-f]{64}  `+regexp.QuoteMeta(path.Base(IconSVGPath))+"\n$", stamp)
	assert.Equal(t, stamp, string(IconStamp()), "the stamp is stable")
}

func TestCheckIcons(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	err := CheckIcons(root)
	require.ErrorIs(t, err, ErrStaleIcons)

	for _, p := range []string{IconSetPath, DevIconPath, IconStampPath} {
		assert.Contains(t, err.Error(), p+" (missing)")
	}

	assert.Contains(t, err.Error(), "run magex brand:icons")

	for _, p := range []string{IconSetPath, DevIconPath} {
		file := filepath.Join(root, filepath.FromSlash(p))
		require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o750))
		require.NoError(t, os.WriteFile(file, []byte("raster"), 0o600))
	}

	require.NoError(t, WriteIconStamp(root))
	require.NoError(t, CheckIcons(root))

	stamp := filepath.Join(root, filepath.FromSlash(IconStampPath))
	require.NoError(t, os.WriteFile(stamp, []byte("0000  appicon.svg\n"), 0o600))

	err = CheckIcons(root)
	require.ErrorIs(t, err, ErrStaleIcons)
	assert.Contains(t, err.Error(), "older artwork")
}

func TestIconReadAndWriteErrors(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	// A regular file where a directory belongs makes every path unreadable.
	require.NoError(t, os.WriteFile(filepath.Join(root, "build"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "cmd"), []byte("x"), 0o600))

	err := CheckIcons(root)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrStaleIcons)

	require.Error(t, WriteIconStamp(root))
}
