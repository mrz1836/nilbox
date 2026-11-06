//go:build mage

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/brandgen"
)

const (
	// svgRenderer rasterizes the icon artwork; it ships with librsvg.
	svgRenderer = "rsvg-convert"

	// devIconSize is the side, in pixels, of the Dock icon a development run shows.
	devIconSize = 512
)

// errNoSVGRenderer is returned by Brand.Icons when rsvg-convert is not installed.
var errNoSVGRenderer = errors.New("rendering the app icon needs rsvg-convert: brew install librsvg")

// iconSetSizes are the point sizes of a macOS icon set; each is rendered at 1x and 2x.
//
//nolint:gochecknoglobals // a fixed table
var iconSetSizes = []int{16, 32, 128, 256, 512}

// Brand groups the targets that work with files generated from internal/brand.
type Brand mg.Namespace

// Generate writes the frontend theme tokens, the frontend brand constants, the macOS
// Info.plist, and the app icon artwork from internal/brand. It is idempotent: a second
// run changes nothing.
//
//	magex brand:generate
func (Brand) Generate() error {
	return brandgen.Write(".")
}

// Icons renders the app icon artwork, build/appicon.svg, into the bundle's icon set,
// build/darwin/icons.icns, and the Dock icon a development run shows,
// cmd/nilbox/appicon.png, then records which artwork they came from. It regenerates the
// brand files first. It needs macOS for iconutil and rsvg-convert (brew install librsvg);
// the outputs are committed, so only a change to the mark or the artwork needs it.
//
//	magex brand:icons
func (Brand) Icons() error {
	if runtime.GOOS != "darwin" {
		return errNeedsMacOS
	}

	if _, err := exec.LookPath(svgRenderer); err != nil {
		return errNoSVGRenderer
	}

	mg.Deps(Brand.Generate)

	tmp, err := os.MkdirTemp("", brand.Slug+"-icons-")
	if err != nil {
		return fmt.Errorf("create a temporary directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	iconset := filepath.Join(tmp, brandgen.IconFile+".iconset")
	if err = renderIconSet(iconset); err != nil {
		return err
	}

	err = sh.Run("iconutil", "--convert", "icns", "--output", filepath.FromSlash(brandgen.IconSetPath), iconset)
	if err != nil {
		return fmt.Errorf("build the icon set: %w", err)
	}

	if err = renderIcon(devIconSize, filepath.FromSlash(brandgen.DevIconPath)); err != nil {
		return err
	}

	if err = brandgen.WriteIconStamp("."); err != nil {
		return err
	}

	//nolint:forbidigo // mage target output
	fmt.Printf("✅ Rendered %s and %s\n", brandgen.IconSetPath, brandgen.DevIconPath)

	return nil
}

// renderIconSet renders every image of a macOS icon set into dir, an .iconset directory
// it creates, under the names iconutil expects.
func renderIconSet(dir string) error {
	if err := os.Mkdir(dir, 0o750); err != nil {
		return fmt.Errorf("create the icon set: %w", err)
	}

	for _, size := range iconSetSizes {
		name := "icon_" + strconv.Itoa(size) + "x" + strconv.Itoa(size)

		if err := renderIcon(size, filepath.Join(dir, name+".png")); err != nil {
			return err
		}

		if err := renderIcon(2*size, filepath.Join(dir, name+"@2x.png")); err != nil {
			return err
		}
	}

	return nil
}

// renderIcon rasterizes the icon artwork into a square PNG of the given side.
func renderIcon(size int, out string) error {
	side := strconv.Itoa(size)

	err := sh.Run(svgRenderer, "--width", side, "--height", side, "--output", out, filepath.FromSlash(brandgen.IconSVGPath))
	if err != nil {
		return fmt.Errorf("render the %s-pixel icon: %w", side, err)
	}

	return nil
}
