//go:build mage

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/magefile/mage/sh"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/brandgen"
)

// buildBundle builds the production app bundle at bundle, the steps App.Build runs once
// it knows it is on macOS.
func buildBundle(bundle string) error {
	if err := checkBrandFiles(); err != nil {
		return err
	}

	if err := (Frontend{}).Build(); err != nil {
		return err
	}

	contents := filepath.Join(bundle, "Contents")
	exe := filepath.Join(contents, "MacOS", brand.Slug)

	if err := os.RemoveAll(bundle); err != nil {
		return fmt.Errorf("remove the previous bundle: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil { //nolint:gosec // an app bundle is world-readable
		return fmt.Errorf("create the bundle: %w", err)
	}

	meta := readBuildMeta()

	if err := buildDesktopBinary(exe, meta); err != nil {
		return err
	}

	if err := writeInfoPlist(filepath.Join(contents, "Info.plist"), meta); err != nil {
		return err
	}

	if err := copyIconSet(filepath.Join(contents, "Resources")); err != nil {
		return err
	}

	if err := sh.RunV("codesign", "--force", "--sign", "-", bundle); err != nil {
		return fmt.Errorf("sign the bundle ad hoc: %w", err)
	}

	return reportSize(bundle, exe)
}

// buildMeta is the version metadata stamped into a build.
type buildMeta struct {
	version       string // the build's version, e.g. v0.1.0-3-gabc1234
	commit        string // the short commit hash
	date          string // the build time, RFC 3339 UTC
	bundleVersion string // the release version macOS shows, e.g. 0.1.0 (CFBundleShortVersionString)
	bundleBuild   string // the build number, the commit count, e.g. 42 (CFBundleVersion)
}

// readBuildMeta derives the version metadata from git; outside a git checkout it falls
// back to development values.
//
// The build number is the commit count, so it grows with every commit, tagged or not.
// macOS keys its icon caches on the bundle version (CFBundleVersion), and a build number
// that never changed would leave Stage Manager and Finder showing a stale icon.
func readBuildMeta() buildMeta {
	meta := buildMeta{
		version:       "dev",
		commit:        "none",
		date:          time.Now().UTC().Format(time.RFC3339),
		bundleVersion: brandgen.PlaceholderVersion,
		bundleBuild:   brandgen.PlaceholderVersion,
	}

	if out := gitOutput("describe", "--tags", "--always", "--dirty"); out != "" {
		meta.version = out
	}

	if out := gitOutput("rev-parse", "--short", "HEAD"); out != "" {
		meta.commit = out
	}

	if out := gitOutput("describe", "--tags", "--abbrev=0"); out != "" {
		meta.bundleVersion = strings.TrimPrefix(out, "v")
	}

	if out := gitOutput("rev-list", "--count", "HEAD"); out != "" {
		meta.bundleBuild = out
	}

	return meta
}

// gitOutput returns git's trimmed output, or "" when git fails (for example, describe in
// a repository without tags); its error output is discarded.
func gitOutput(args ...string) string {
	out, err := exec.CommandContext(context.Background(), "git", args...).Output() //nolint:gosec // argv only, no shell
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(out))
}

// versionFlags returns the linker flags that stamp the build's version metadata into the
// desktop command, which reports it on the About page.
func versionFlags(meta buildMeta) string {
	return fmt.Sprintf("-X main.version=%s -X main.commit=%s -X main.buildDate=%s -X main.release=%s -X main.buildNumber=%s",
		meta.version, meta.commit, meta.date, meta.bundleVersion, meta.bundleBuild)
}

// quietLinkerFlags returns linker flags that quiet Apple's linker about duplicate
// libraries, or "" where there is nothing to quiet. Two Wails packages hold Objective-C,
// and Go asks for libobjc once for each; the linker Xcode 15 brought (ld-prime) links it
// once and warns "ignoring duplicate libraries: '-lobjc'". Only that linker knows
// -no_warn_duplicate_libraries, so the flag is added only when ld -v names it (ld-, where
// the older ld64 says ld64-); an older Apple linker, or any other, gets nothing.
func quietLinkerFlags() string {
	// ld -v reports on standard error; whatever its exit status, the text decides.
	out, _ := exec.CommandContext(context.Background(), "ld", "-v").CombinedOutput()
	if !strings.Contains(string(out), "PROJECT:ld-") {
		return ""
	}

	return " -extldflags=-Wl,-no_warn_duplicate_libraries"
}

// buildDesktopBinary compiles the production desktop binary into exe.
func buildDesktopBinary(exe string, meta buildMeta) error {
	minVersion := "-mmacosx-version-min=" + macOSMinVersion
	env := map[string]string{
		"CGO_ENABLED":              "1",
		"CGO_CFLAGS":               minVersion,
		"CGO_LDFLAGS":              minVersion,
		"MACOSX_DEPLOYMENT_TARGET": macOSMinVersion,
	}

	err := sh.RunWithV(env, "go", "build",
		"-tags", desktopTag+",production", "-trimpath", "-buildvcs=false",
		"-ldflags", "-s -w "+versionFlags(meta)+quietLinkerFlags(), "-o", exe, "./cmd/"+brand.Slug)
	if err != nil {
		return fmt.Errorf("build the desktop binary: %w", err)
	}

	return nil
}

// writeInfoPlist copies the generated Info.plist into the bundle and stamps the build's
// release version and build number into the copy; the committed file keeps its
// placeholder version.
func writeInfoPlist(dst string, meta buildMeta) error {
	body, err := os.ReadFile(filepath.Join("build", "darwin", "Info.plist"))
	if err != nil {
		return fmt.Errorf("read the Info.plist: %w", err)
	}

	if err = os.WriteFile(dst, body, 0o644); err != nil { //nolint:gosec // an app bundle is world-readable
		return fmt.Errorf("write the bundle Info.plist: %w", err)
	}

	stamps := []struct{ key, value string }{
		{"CFBundleShortVersionString", meta.bundleVersion},
		{"CFBundleVersion", meta.bundleBuild},
	}

	for _, s := range stamps {
		if err = sh.Run("plutil", "-replace", s.key, "-string", s.value, dst); err != nil {
			return fmt.Errorf("stamp %s: %w", s.key, err)
		}
	}

	return nil
}

// checkBrandFiles fails when a file generated from internal/brand, or an app icon
// rendered from its artwork, is out of date.
func checkBrandFiles() error {
	if err := brandgen.Check("."); err != nil {
		return fmt.Errorf("generated brand files: %w", err)
	}

	if err := brandgen.CheckIcons("."); err != nil {
		return fmt.Errorf("app icons: %w", err)
	}

	return nil
}

// copyIconSet copies the committed app icon set into the bundle's resources directory,
// under the name the Info.plist gives it.
func copyIconSet(resources string) error {
	body, err := os.ReadFile(filepath.FromSlash(brandgen.IconSetPath))
	if err != nil {
		return fmt.Errorf("read the app icon set: %w", err)
	}

	if err = os.MkdirAll(resources, 0o755); err != nil { //nolint:gosec // an app bundle is world-readable
		return fmt.Errorf("create the bundle resources: %w", err)
	}

	dst := filepath.Join(resources, brandgen.IconFile+".icns")
	if err = os.WriteFile(dst, body, 0o644); err != nil { //nolint:gosec // an app bundle is world-readable
		return fmt.Errorf("write the bundle app icon: %w", err)
	}

	return nil
}

// reportSize prints the built bundle's path and its binary's size.
func reportSize(bundle, exe string) error {
	info, err := os.Stat(exe)
	if err != nil {
		return fmt.Errorf("read the binary size: %w", err)
	}

	//nolint:forbidigo // mage target output
	fmt.Printf("✅ Built %s (binary %d bytes, %.1f MB)\n", bundle, info.Size(), float64(info.Size())/1e6)

	return nil
}
