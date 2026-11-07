package brandgen

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/brand"
)

// Icon paths, relative to the repository root and slash-separated.
//
// The artwork is generated like every other brand file: the app icon, IconSVGPath, and
// the menu bar icon, TraySVGPath. The raster icons are rendered from it by `magex
// brand:icons`, which needs macOS, and are committed: the bundle's icon set, IconSetPath;
// the PNG a development run shows in the Dock, which has no bundle, DevIconPath; and the
// menu bar icon both kinds of build show, TrayIconPath. IconStampPath records the artwork
// they were rendered from, so CheckIcons can tell when they are stale.
const (
	IconSVGPath   = "build/appicon.svg"
	IconSetPath   = "build/darwin/icons.icns"
	DevIconPath   = "cmd/nilbox/appicon.png"
	IconStampPath = "build/appicon.svg.sha256"
	TraySVGPath   = "build/trayicon.svg"
	TrayIconPath  = "cmd/nilbox/trayicon.png"
)

// TrayIconSize is the side, in pixels, the menu bar icon is rendered at: three times the
// menu bar's 22 points, so the system scales it down smoothly on any display.
const TrayIconSize = 66

// trayGrid is the side of the menu bar icon's canvas, in mark units: the mark's 512 units
// of height take 16 of the menu bar's 22 points, the size of the system's own icons.
const trayGrid = brand.MarkHeight * 22 / 16

// IconFile is the bundle's icon set name, without its extension: the Info.plist's
// CFBundleIconFile, and the name the bundle's Contents/Resources copy takes.
const IconFile = "icons"

// iconGrid is the side of Apple's macOS app icon canvas, in points.
const iconGrid = 1024

// ErrStaleIcons is returned by CheckIcons when the raster icons are missing or were
// rendered from different artwork.
var ErrStaleIcons = errors.New("the app icons are out of date")

// iconTile is the icon's body on the iconGrid: Apple's template shape, an 824-point square
// at (100, 100) with continuous corners of radius 185.4, which leaves room for the shadow.
const iconTile = "M383.4 100L640.6 100C722.2 100 763 100 799.8 112.1C854.9 130 894 169.1 911.9 224.2" +
	"C924 261 924 301.8 924 383.4L924 640.6C924 722.2 924 763 911.9 799.8" +
	"C894 854.9 854.9 894 799.8 911.9C763 924 722.2 924 640.6 924L383.4 924" +
	"C301.8 924 261 924 224.2 911.9C169.1 894 130 854.9 112.1 799.8C100 763 100 722.2 100 640.6" +
	"L100 383.4C100 301.8 100 261 112.1 224.2C130 169.1 169.1 130 224.2 112.1" +
	"C261 100 301.8 100 383.4 100Z"

// iconArtwork is the app icon: an ink tile lit from within by an ember glow, with the mark
// in enameled rufous on top. The glyph's filter lifts it with a drop shadow, catches light
// on its upper edges, and shades its lower ones; a faint grain dithers the tile's dark
// gradients so they do not band. The {{…}} fields are filled from internal/brand.
const iconArtwork = `<svg xmlns="http://www.w3.org/2000/svg" width="1024" height="1024" viewBox="0 0 1024 1024">
  <title>{{name}}</title>
  <defs>
    <linearGradient id="tile" x1="0" y1="100" x2="0" y2="924" gradientUnits="userSpaceOnUse">
      <stop offset="0" stop-color="#262B37"/>
      <stop offset="0.5" stop-color="#151922"/>
      <stop offset="1" stop-color="#0B0D12"/>
    </linearGradient>
    <radialGradient id="ember" cx="512" cy="560" r="400" gradientUnits="userSpaceOnUse">
      <stop offset="0" stop-color="{{accent}}" stop-opacity="0.34"/>
      <stop offset="0.55" stop-color="{{accent}}" stop-opacity="0.09"/>
      <stop offset="1" stop-color="{{accent}}" stop-opacity="0"/>
    </radialGradient>
    <radialGradient id="sheen" cx="512" cy="100" r="560" gradientUnits="userSpaceOnUse">
      <stop offset="0" stop-color="#FFFFFF" stop-opacity="0.1"/>
      <stop offset="1" stop-color="#FFFFFF" stop-opacity="0"/>
    </radialGradient>
    <linearGradient id="rim" x1="0" y1="100" x2="0" y2="924" gradientUnits="userSpaceOnUse">
      <stop offset="0" stop-color="#FFFFFF" stop-opacity="0.34"/>
      <stop offset="0.18" stop-color="#FFFFFF" stop-opacity="0.08"/>
      <stop offset="0.85" stop-color="#FFFFFF" stop-opacity="0.03"/>
      <stop offset="1" stop-color="#FFFFFF" stop-opacity="0.12"/>
    </linearGradient>
    <linearGradient id="glyph" x1="0" y1="0" x2="0" y2="{{markHeight}}" gradientUnits="userSpaceOnUse">
      <stop offset="0" stop-color="{{highlight}}"/>
      <stop offset="0.45" stop-color="#F5803F"/>
      <stop offset="1" stop-color="#D24F1B"/>
    </linearGradient>
    <filter id="drop" x="-10%" y="-10%" width="120%" height="125%" color-interpolation-filters="sRGB">
      <feGaussianBlur in="SourceAlpha" stdDeviation="10"/>
      <feOffset dy="10"/>
      <feComponentTransfer><feFuncA type="linear" slope="0.3"/></feComponentTransfer>
      <feMerge><feMergeNode/><feMergeNode in="SourceGraphic"/></feMerge>
    </filter>
    <filter id="bloom" x="-50%" y="-50%" width="200%" height="200%">
      <feGaussianBlur stdDeviation="34"/>
    </filter>
    <filter id="grain" x="0" y="0" width="100%" height="100%" color-interpolation-filters="sRGB">
      <feTurbulence type="fractalNoise" baseFrequency="0.9" numOctaves="2" seed="7" stitchTiles="stitch"/>
      <feColorMatrix type="matrix" values="0 0 0 0 1  0 0 0 0 1  0 0 0 0 1  0 0 0 0.03 0"/>
    </filter>
    <filter id="enamel" x="-30%" y="-30%" width="160%" height="160%" color-interpolation-filters="sRGB">
      <feGaussianBlur in="SourceAlpha" stdDeviation="18" result="blur"/>
      <feOffset in="blur" dy="20" result="offset"/>
      <feComponentTransfer in="offset" result="shadow"><feFuncA type="linear" slope="0.55"/></feComponentTransfer>
      <feOffset in="SourceAlpha" dy="7" result="down"/>
      <feComposite in="SourceAlpha" in2="down" operator="out" result="top"/>
      <feGaussianBlur in="top" stdDeviation="2.5" result="topSoft"/>
      <feFlood flood-color="#FFF4EA" flood-opacity="0.85"/>
      <feComposite in2="topSoft" operator="in" result="highlight"/>
      <feOffset in="SourceAlpha" dy="-8" result="up"/>
      <feComposite in="SourceAlpha" in2="up" operator="out" result="bottom"/>
      <feGaussianBlur in="bottom" stdDeviation="4" result="bottomSoft"/>
      <feFlood flood-color="#7A2306" flood-opacity="0.55"/>
      <feComposite in2="bottomSoft" operator="in" result="shade"/>
      <feMerge>
        <feMergeNode in="shadow"/>
        <feMergeNode in="SourceGraphic"/>
        <feMergeNode in="shade"/>
        <feMergeNode in="highlight"/>
      </feMerge>
    </filter>
    <clipPath id="body"><path d="{{tile}}"/></clipPath>
    <path id="mark" d="{{mark}}"/>
  </defs>
  <path d="{{tile}}" fill="url(#tile)" filter="url(#drop)"/>
  <g clip-path="url(#body)">
    <rect x="100" y="100" width="824" height="824" fill="url(#ember)"/>
    <rect x="100" y="100" width="824" height="824" fill="url(#sheen)"/>
    <rect x="100" y="100" width="824" height="824" filter="url(#grain)"/>
    <use href="#mark" x="{{markX}}" y="{{markY}}" fill="#F07A3E" opacity="0.55" filter="url(#bloom)"/>
  </g>
  <path d="{{tile}}" fill="none" stroke="url(#rim)" stroke-width="5" clip-path="url(#body)"/>
  <g transform="translate({{markX}} {{markY}})" filter="url(#enamel)">
    <use href="#mark" fill="url(#glyph)"/>
  </g>
</svg>
`

// iconSVG renders the app icon artwork: the mark, centered on the icon grid, in the
// iconArtwork treatment.
func iconSVG() []byte {
	centered := func(size int) string {
		return strconv.FormatFloat(float64(iconGrid-size)/2, 'f', -1, 64)
	}

	fields := strings.NewReplacer(
		"{{name}}", xmlText(brand.Name),
		"{{accent}}", brand.Dark.Accent,
		"{{highlight}}", brand.MarkHighlight,
		"{{tile}}", iconTile,
		"{{mark}}", brand.MarkPath,
		"{{markX}}", centered(brand.MarkWidth),
		"{{markY}}", centered(brand.MarkHeight),
		"{{markHeight}}", strconv.Itoa(brand.MarkHeight),
	)

	return []byte("<!-- " + Header + " -->\n" + fields.Replace(iconArtwork))
}

// trayArtwork is the menu bar icon: the mark in black on a transparent canvas, a template
// image the system tints to suit the menu bar, light or dark, and highlights when its menu
// is open. The {{…}} fields are filled from internal/brand.
const trayArtwork = `<svg xmlns="http://www.w3.org/2000/svg" width="{{side}}" height="{{side}}" viewBox="0 0 {{side}} {{side}}">
  <title>{{name}}</title>
  <path transform="translate({{markX}} {{markY}})" d="{{mark}}" fill="#000000"/>
</svg>
`

// traySVG renders the menu bar icon artwork: the mark, centered on the trayGrid.
func traySVG() []byte {
	centered := func(size int) string {
		return strconv.FormatFloat(float64(trayGrid-size)/2, 'f', -1, 64)
	}

	fields := strings.NewReplacer(
		"{{side}}", strconv.Itoa(trayGrid),
		"{{name}}", xmlText(brand.Name),
		"{{mark}}", brand.MarkPath,
		"{{markX}}", centered(brand.MarkWidth),
		"{{markY}}", centered(brand.MarkHeight),
	)

	return []byte("<!-- " + Header + " -->\n" + fields.Replace(trayArtwork))
}

// IconStamp returns the contents of IconStampPath for the current artwork, one line per
// artwork file, in the format `shasum -a 256 -c` reads from the build directory.
func IconStamp() []byte {
	var b strings.Builder

	for _, art := range []struct {
		path    string
		content []byte
	}{{IconSVGPath, iconSVG()}, {TraySVGPath, traySVG()}} {
		sum := sha256.Sum256(art.content)
		b.WriteString(hex.EncodeToString(sum[:]) + "  " + path.Base(art.path) + "\n")
	}

	return []byte(b.String())
}

// WriteIconStamp records under root that the raster icons were rendered from the current
// artwork. `magex brand:icons` calls it after rendering them.
func WriteIconStamp(root string) error {
	stamp := filepath.Join(root, filepath.FromSlash(IconStampPath))

	if err := os.WriteFile(stamp, IconStamp(), filePerm); err != nil {
		return fmt.Errorf("write %s: %w", IconStampPath, err)
	}

	return nil
}

// CheckIcons reports whether the raster icons under root exist and were rendered from the
// current artwork. It returns an error wrapping ErrStaleIcons when they need rendering.
func CheckIcons(root string) error {
	var stale []string

	for _, p := range []string{IconSetPath, DevIconPath, TrayIconPath} {
		_, err := os.Stat(filepath.Join(root, filepath.FromSlash(p)))

		switch {
		case errors.Is(err, fs.ErrNotExist):
			stale = append(stale, p+" (missing)")
		case err != nil:
			return fmt.Errorf("read %s: %w", p, err)
		}
	}

	stampPath := filepath.Join(root, filepath.FromSlash(IconStampPath))

	stamp, err := os.ReadFile(stampPath) //nolint:gosec // a constant path under root

	switch {
	case errors.Is(err, fs.ErrNotExist):
		stale = append(stale, IconStampPath+" (missing)")
	case err != nil:
		return fmt.Errorf("read %s: %w", IconStampPath, err)
	case !bytes.Equal(stamp, IconStamp()):
		stale = append(stale, "rendered from older artwork than "+IconSVGPath+" and "+TraySVGPath)
	}

	if len(stale) > 0 {
		return fmt.Errorf("%w: %s; run magex brand:icons", ErrStaleIcons, strings.Join(stale, ", "))
	}

	return nil
}
