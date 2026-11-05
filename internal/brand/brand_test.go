package brand

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	black = "#000000"
	white = "#FFFFFF"
)

func TestIdentity(t *testing.T) {
	t.Parallel()

	assert.NotEmpty(t, Name)
	assert.NotEmpty(t, Tagline)
	assert.Equal(t, Slug, strings.ToLower(Name), "slug must be the lowercase name")
	assert.Regexp(t, `^[a-z][a-z0-9-]*$`, Slug)
	assert.True(t, strings.HasSuffix(BundleID, "."+Slug), "bundle ID must end with the slug")
	assert.Regexp(t, `^[a-z0-9]+(\.[a-z0-9-]+)+$`, BundleID)
	assert.Equal(t, Name, DataDirName)
}

func TestPalettesAreCompleteAndValid(t *testing.T) {
	t.Parallel()

	hexColor := regexp.MustCompile(`^#[0-9A-F]{6}$`)

	for name, p := range map[string]Palette{"Dark": Dark, "Light": Light} {
		v := reflect.ValueOf(p)
		for i := range v.NumField() {
			field := v.Type().Field(i).Name
			value := v.Field(i).String()
			assert.Regexpf(t, hexColor, value, "%s.%s must be uppercase #RRGGBB", name, field)
		}
	}
}

// TestPaletteTextContrast enforces WCAG AA (4.5:1) for every text-bearing token against
// both background layers it can sit on, in both themes. A palette tweak that hurts
// readability fails here instead of shipping.
func TestPaletteTextContrast(t *testing.T) {
	t.Parallel()

	for name, p := range map[string]Palette{"Dark": Dark, "Light": Light} {
		for token, fg := range p.TextTokens() {
			for layer, bg := range map[string]string{"Background": p.Background, "Surface": p.Surface} {
				ratio, err := ContrastRatio(fg, bg)
				require.NoError(t, err)
				assert.GreaterOrEqualf(t, ratio, MinTextContrast,
					"%s.%s %s on %s %s is %.2f:1", name, token, fg, layer, bg, ratio)
			}
		}
	}
}

func TestContrastRatio(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		a, b    string
		want    float64
		wantErr bool
	}{
		{name: "black on white is 21", a: black, b: white, want: 21},
		{name: "order does not matter", a: white, b: black, want: 21},
		{name: "same color is 1", a: "#777777", b: "#777777", want: 1},
		{name: "lowercase hex accepted", a: "#ffffff", b: black, want: 21},
		{name: "missing hash", a: "FFFFFF", b: black, wantErr: true},
		{name: "short form rejected", a: "#FFF", b: black, wantErr: true},
		{name: "non-hex digits", a: "#GGGGGG", b: black, wantErr: true},
		{name: "invalid second color", a: black, b: "#12345", wantErr: true},
		{name: "empty", a: "", b: black, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ContrastRatio(tt.a, tt.b)
			if tt.wantErr {
				require.ErrorIs(t, err, ErrInvalidHexColor)
				return
			}

			require.NoError(t, err)
			assert.InDelta(t, tt.want, got, 0.001)
		})
	}
}

func TestTextTokensCoversEveryTextField(t *testing.T) {
	t.Parallel()

	tokens := Dark.TextTokens()
	assert.Len(t, tokens, 7)

	for _, k := range []string{"Text", "Muted", "Accent", "Info", "Success", "Warning", "Danger"} {
		assert.Containsf(t, tokens, k, "missing text token %s", k)
	}
}
