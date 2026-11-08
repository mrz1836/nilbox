package brandgen

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
)

// fileContent returns the rendered content of the generated file at path.
func fileContent(t *testing.T, path string) string {
	t.Helper()

	for _, f := range Files() {
		if f.Path == path {
			return string(f.Content)
		}
	}

	require.Failf(t, "generated file not found", "%s", path)

	return ""
}

// cssRule returns the body of the first CSS rule that starts with header.
func cssRule(t *testing.T, css, header string) string {
	t.Helper()

	start := strings.Index(css, header+" {\n")
	require.GreaterOrEqualf(t, start, 0, "rule %q not found", header)

	rest := css[start+len(header)+3:]
	end := strings.Index(rest, "}")
	require.GreaterOrEqual(t, end, 0)

	return rest[:end]
}

func TestGeneratedFilesAreCurrent(t *testing.T) {
	t.Parallel()

	require.NoError(t, Check(filepath.Join("..", "..")), "run magex brand:generate")
}

// themeTokens parses the generated @theme block into each token's light and dark value,
// keyed by the token's CSS name, failing on any line that is not a light-dark() token.
func themeTokens(t *testing.T, css string) map[string][2]string {
	t.Helper()

	body := cssRule(t, css, "@theme static")
	tokens := map[string][2]string{}

	for line := range strings.SplitSeq(strings.TrimSpace(body), "\n") {
		name, value, ok := strings.Cut(strings.TrimSpace(line), ": ")
		require.Truef(t, ok, "malformed theme line %q", line)
		require.Truef(t, strings.HasPrefix(name, themeVarPrefix), "%s is not a theme color", name)

		inner, ok := strings.CutPrefix(value, "light-dark(")
		require.Truef(t, ok, "%s is not light-dark(): %s", name, value)

		inner, ok = strings.CutSuffix(inner, ");")
		require.Truef(t, ok, "%s is not light-dark(): %s", name, value)

		light, dark, ok := strings.Cut(inner, ", ")
		require.Truef(t, ok, "%s has no dark value: %s", name, value)

		key := strings.TrimPrefix(name, themeVarPrefix)
		require.NotContainsf(t, tokens, key, "%s is declared twice", name)

		tokens[key] = [2]string{light, dark}
	}

	return tokens
}

func TestThemeCSSPairsBothPalettes(t *testing.T) {
	t.Parallel()

	css := fileContent(t, ThemeCSSPath)
	tokens := themeTokens(t, css)

	light := reflect.ValueOf(brand.Light)
	dark := reflect.ValueOf(brand.Dark)

	require.Len(t, tokens, light.NumField(), "one theme color per palette field")

	for i := range light.NumField() {
		name := kebab(light.Type().Field(i).Name)

		assert.Equalf(t, [2]string{light.Field(i).String(), dark.Field(i).String()}, tokens[name],
			"%s%s must be light-dark(Light, Dark)", themeVarPrefix, name)
	}

	// The root follows the appearance, and data-theme pins a theme. The pins are
	// stylesheet rules because the bundle's stand-in for light-dark() switches on them.
	assert.Equal(t, "  color-scheme: light dark;\n", cssRule(t, css, ":root"))
	assert.Equal(t, "  color-scheme: light;\n", cssRule(t, css, ":root[data-theme='light']"))
	assert.Equal(t, "  color-scheme: dark;\n", cssRule(t, css, ":root[data-theme='dark']"))
	assert.NotContains(t, css, "@media", "light-dark() replaces the per-appearance blocks")
}

// TestThemeCSSTextContrast enforces WCAG AA on the generated theme itself: every text
// token, in each theme, against both background layers it can sit on.
func TestThemeCSSTextContrast(t *testing.T) {
	t.Parallel()

	tokens := themeTokens(t, fileContent(t, ThemeCSSPath))
	themes := []struct {
		name string
		side int
	}{{"light", 0}, {"dark", 1}}

	for _, theme := range themes {
		for field := range brand.Light.TextTokens() {
			fg := tokens[kebab(field)][theme.side]

			for _, layer := range []string{"Background", "Surface"} {
				bg := tokens[kebab(layer)][theme.side]

				ratio, err := brand.ContrastRatio(fg, bg)
				require.NoErrorf(t, err, "%s %s on %s", theme.name, field, layer)
				assert.GreaterOrEqualf(t, ratio, brand.MinTextContrast,
					"%s %s %s on %s %s is %.2f:1", theme.name, field, fg, layer, bg, ratio)
			}
		}
	}
}

func TestInfoPlistHasAutomationUsage(t *testing.T) {
	t.Parallel()

	plist := fileContent(t, InfoPlistPath)
	values := plistStrings(t, plist)

	assert.Equal(t, brand.Name, values["CFBundleName"])
	assert.Equal(t, brand.Name, values["CFBundleDisplayName"])
	assert.Equal(t, brand.BundleID, values["CFBundleIdentifier"])
	assert.Equal(t, brand.Slug, values["CFBundleExecutable"])
	assert.Equal(t, IconFile, values["CFBundleIconFile"])
	assert.Equal(t, IconFile+".icns", path.Base(IconSetPath), "the bundle's icon set is the one the plist names")
	assert.Equal(t, "APPL", values["CFBundlePackageType"])
	assert.Equal(t, MinimumMacOS, values["LSMinimumSystemVersion"])
	assert.Equal(t, PlaceholderVersion, values["CFBundleShortVersionString"])
	assert.Equal(t, PlaceholderVersion, values["CFBundleVersion"])

	usage := values["NSAppleEventsUsageDescription"]
	assert.True(t, strings.HasPrefix(usage, brand.Name+" "), "the automation prompt names the app")
	assert.Contains(t, usage, "iTerm2")
	assert.Contains(t, plist, "<key>NSHighResolutionCapable</key>\n\t<true/>")
	assert.Contains(t, plist, "<!-- "+Header+" -->")
}

// plistStrings decodes a plist's top-level key/string pairs, failing on malformed XML.
func plistStrings(t *testing.T, plist string) map[string]string {
	t.Helper()

	dec := xml.NewDecoder(strings.NewReader(plist))
	values := map[string]string{}

	var element, key string

	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return values
		}

		require.NoError(t, err, "the Info.plist must be well-formed XML")

		switch v := tok.(type) {
		case xml.StartElement:
			element = v.Name.Local
		case xml.CharData:
			switch element {
			case "key":
				key = string(v)
			case "string":
				values[key] = string(v)
			}
		case xml.EndElement:
			element = ""
		}
	}
}

func TestBrandTSAndJSON(t *testing.T) {
	t.Parallel()

	ts := fileContent(t, BrandTSPath)
	assert.True(t, strings.HasPrefix(ts, "// "+Header+"\n"))
	assert.Contains(t, ts, "export const brand = {")
	assert.Contains(t, ts, `  name: "`+brand.Name+`",`)
	assert.Contains(t, ts, `  slug: "`+brand.Slug+`",`)
	assert.Contains(t, ts, `  tagline: "`+brand.Tagline+`",`)
	assert.Contains(t, ts, "  mark: {\n    width: "+strconv.Itoa(brand.MarkWidth)+",\n    height: "+
		strconv.Itoa(brand.MarkHeight)+",\n    path: \""+brand.MarkPath+"\",\n    highlight: \""+
		brand.MarkHighlight+"\",\n  },")
	assert.Contains(t, ts, `    accent: "`+brand.Light.Accent+`",`)
	assert.Contains(t, ts, `    accent: "`+brand.Dark.Accent+`",`)

	var identity map[string]string

	require.NoError(t, json.Unmarshal([]byte(fileContent(t, BrandJSONPath)), &identity))
	assert.Equal(t, map[string]string{"name": brand.Name, "tagline": brand.Tagline}, identity)
}

func TestOutputIsCleanText(t *testing.T) {
	t.Parallel()

	for _, f := range Files() {
		text := string(f.Content)

		assert.Truef(t, strings.HasSuffix(text, "\n") && !strings.HasSuffix(text, "\n\n"),
			"%s must end with exactly one newline", f.Path)

		for i, line := range strings.Split(text, "\n") {
			assert.Equalf(t, strings.TrimRight(line, " \t"), line, "%s:%d has trailing whitespace", f.Path, i+1)
		}
	}
}

func TestFilesAreByteStable(t *testing.T) {
	t.Parallel()

	first, second := Files(), Files()
	assert.Equal(t, first, second)
}

func TestWriteThenCheck(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	err := Check(root)
	require.ErrorIs(t, err, ErrStale)
	assert.Contains(t, err.Error(), ThemeCSSPath+" (missing)")
	assert.Contains(t, err.Error(), "run magex brand:generate")

	require.NoError(t, Write(root))
	require.NoError(t, Check(root))

	plist := filepath.Join(root, filepath.FromSlash(InfoPlistPath))
	before, err := os.Stat(plist)
	require.NoError(t, err)

	require.NoError(t, Write(root), "a second write is a no-op")

	after, err := os.Stat(plist)
	require.NoError(t, err)
	assert.Equal(t, before.ModTime(), after.ModTime(), "unchanged files are not rewritten")

	css := filepath.Join(root, filepath.FromSlash(ThemeCSSPath))
	require.NoError(t, os.WriteFile(css, []byte("edited\n"), 0o600))

	err = Check(root)
	require.ErrorIs(t, err, ErrStale)
	assert.Contains(t, err.Error(), ThemeCSSPath)
	assert.NotContains(t, err.Error(), BrandTSPath)

	require.NoError(t, Write(root))
	require.NoError(t, Check(root))
}

func TestWriteAndCheckErrors(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	// A regular file where a directory belongs blocks both writing and reading.
	require.NoError(t, os.WriteFile(filepath.Join(root, "frontend"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "build"), []byte("x"), 0o600))

	require.Error(t, Write(root))

	err := Check(root)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrStale)
}

func TestNameHelpers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in, kebab, camel string
	}{
		{in: "Background", kebab: "background", camel: "background"},
		{in: "SurfaceRaised", kebab: "surface-raised", camel: "surfaceRaised"},
		{in: "", kebab: "", camel: ""},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.kebab, kebab(tt.in))
		assert.Equal(t, tt.camel, lowerFirst(tt.in))
	}
}

func TestEscaping(t *testing.T) {
	t.Parallel()

	assert.Equal(t, `"plain"`, jsString("plain"))
	assert.Equal(t, `"a \"q\" \\ b"`, jsString(`a "q" \ b`))
	assert.Equal(t, "\"line\\u000abreak\\u2028\"", jsString("line\nbreak\u2028"))
	assert.Equal(t, "Tom &amp; &lt;Jerry&gt;", xmlText("Tom & <Jerry>"))
}
