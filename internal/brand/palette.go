package brand

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ErrInvalidHexColor is returned when a color is not in #RRGGBB form.
var ErrInvalidHexColor = errors.New("invalid hex color: want #RRGGBB")

// MinTextContrast is the WCAG 2.x AA minimum contrast ratio for normal-size text.
const MinTextContrast = 4.5

// Palette holds the semantic color tokens for one theme. Every value is an sRGB hex
// color in #RRGGBB form.
//
// Accent is both the brand color and the semantic "needs you" signal. Text, Muted,
// Accent, Info, Success, Warning, and Danger are text-bearing tokens and must meet
// MinTextContrast against Background and Surface.
type Palette struct {
	Background string // app window background
	Surface    string // cards, rows, panels
	Raised     string // popovers, menus, hovered rows
	Border     string // hairlines and dividers
	Text       string // primary text
	Muted      string // secondary text and metadata
	Accent     string // brand + "needs you"
	Info       string // neutral emphasis
	Success    string // passing checks, merged
	Warning    string // pending, attention soon
	Danger     string // failing checks, critical alerts
}

// TextTokens returns the text-bearing tokens keyed by name, for contrast validation and
// for generating theme files.
func (p Palette) TextTokens() map[string]string {
	return map[string]string{
		"Text":    p.Text,
		"Muted":   p.Muted,
		"Accent":  p.Accent,
		"Info":    p.Info,
		"Success": p.Success,
		"Warning": p.Warning,
		"Danger":  p.Danger,
	}
}

// Dark is the default theme: slate ink with a rufous accent.
//
//nolint:gochecknoglobals // immutable design tokens, read-only by convention
var Dark = Palette{
	Background: "#0F1218",
	Surface:    "#161A22",
	Raised:     "#1D222C",
	Border:     "#2A303C",
	Text:       "#E6E8EC",
	Muted:      "#9AA3B2",
	Accent:     "#E8743B",
	Info:       "#8FA4C4",
	Success:    "#3DBE8B",
	Warning:    "#E9B44C",
	Danger:     "#F05D5E",
}

// Light is the "field paper" theme: warm paper with a deeper rufous accent.
//
//nolint:gochecknoglobals // immutable design tokens, read-only by convention
var Light = Palette{
	Background: "#F7F5F2",
	Surface:    "#FFFFFF",
	Raised:     "#EFECE7",
	Border:     "#DDD8D0",
	Text:       "#1A1E26",
	Muted:      "#5B6472",
	Accent:     "#B04A1E",
	Info:       "#4A6285",
	Success:    "#17784F",
	Warning:    "#9A6200",
	Danger:     "#C42B2B",
}

// ContrastRatio returns the WCAG 2.x contrast ratio between two #RRGGBB colors.
// The result is in the range [1, 21] and does not depend on argument order.
func ContrastRatio(a, b string) (float64, error) {
	la, err := relativeLuminance(a)
	if err != nil {
		return 0, err
	}

	lb, err := relativeLuminance(b)
	if err != nil {
		return 0, err
	}

	hi, lo := math.Max(la, lb), math.Min(la, lb)

	return (hi + 0.05) / (lo + 0.05), nil
}

// relativeLuminance returns the WCAG relative luminance of a #RRGGBB color.
func relativeLuminance(hex string) (float64, error) {
	if len(hex) != 7 || !strings.HasPrefix(hex, "#") {
		return 0, fmt.Errorf("%w: %q", ErrInvalidHexColor, hex)
	}

	v, err := strconv.ParseUint(hex[1:], 16, 32)
	if err != nil {
		return 0, fmt.Errorf("%w: %q", ErrInvalidHexColor, hex)
	}

	channel := func(shift uint) float64 {
		c := float64((v>>shift)&0xFF) / 255
		if c <= 0.03928 {
			return c / 12.92
		}

		return math.Pow((c+0.055)/1.055, 2.4)
	}

	return 0.2126*channel(16) + 0.7152*channel(8) + 0.0722*channel(0), nil
}
