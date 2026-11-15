//go:build mage

package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/magefile/mage/sh"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/brandgen"
)

// errArches is returned when a universal binary lacks an architecture it was built for,
// or holds one it wasn't.
var errArches = errors.New("the universal binary holds other architectures than it was built for")

// adHoc is the codesign identity that signs ad hoc: no certificate, enough to run the
// bundle on the Mac that built it.
const adHoc = "-"

// bundleSpec is the app bundle assembleBundle builds.
type bundleSpec struct {
	bundle string    // where the bundle goes
	meta   buildMeta // the version metadata stamped into it
	arches []string  // the GOARCH values to build and combine with lipo, or none for this Mac's own
	signer signer    // what signs it
}

// buildBundle builds the production app bundle at bundle, the steps App.Build runs once
// it knows it is on macOS: a build from source, for this Mac's processor, signed ad hoc.
func buildBundle(bundle string) error {
	if err := prepareBuild(); err != nil {
		return err
	}

	return assembleBundle(bundleSpec{bundle: bundle, meta: readBuildMeta(), signer: signer{identity: adHoc}})
}

// prepareBuild checks the generated brand files and app icons, then builds the frontend
// the desktop binary embeds.
func prepareBuild() error {
	if err := checkBrandFiles(); err != nil {
		return err
	}

	return (Frontend{}).Build()
}

// assembleBundle builds the bundle spec describes from a built frontend: it compiles the
// desktop binary, adds the Info.plist with the build's versions and the app icon, sets
// every mode as an app's (whatever the umask), signs the bundle, sets the modes again for
// the seal codesign wrote, and reports the binary's size. A previous bundle at the same path is removed first.
func assembleBundle(spec bundleSpec) error {
	contents := filepath.Join(spec.bundle, "Contents")
	exe := filepath.Join(contents, "MacOS", brand.Slug)

	if err := os.RemoveAll(spec.bundle); err != nil {
		return fmt.Errorf("remove the previous bundle: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil { //nolint:gosec // an app bundle is world-readable
		return fmt.Errorf("create the bundle: %w", err)
	}

	if err := buildExecutable(exe, spec.meta, spec.arches, filepath.Dir(spec.bundle)); err != nil {
		return err
	}

	if err := writeInfoPlist(filepath.Join(contents, "Info.plist"), spec.meta); err != nil {
		return err
	}

	if err := copyIconSet(filepath.Join(contents, "Resources")); err != nil {
		return err
	}

	if err := normalizeModes(spec.bundle, exe); err != nil {
		return err
	}

	if err := spec.signer.sign(spec.bundle); err != nil {
		return fmt.Errorf("sign the bundle %s: %w", spec.signer, err)
	}

	// codesign writes the seal (_CodeSignature) under the umask too; a mode is no part of
	// the signature, so setting it again leaves the signature valid.
	if err := normalizeModes(spec.bundle, exe); err != nil {
		return err
	}

	return reportSize(spec.bundle, exe)
}

// buildExecutable compiles the desktop binary into exe: for this Mac's processor when
// arches is empty, else one slice for each GOARCH in arches, combined into one universal
// binary. The slices are built in a folder of their own inside scratch, which is removed
// once they are combined; a run cut off before that leaves it there, beside the bundle,
// for the next run to clear.
func buildExecutable(exe string, meta buildMeta, arches []string, scratch string) error {
	if len(arches) == 0 {
		return buildDesktopBinary(exe, meta, "")
	}

	dir, err := os.MkdirTemp(scratch, ".slices-")
	if err != nil {
		return fmt.Errorf("create a folder for the slices: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	parts := make([]string, 0, len(arches))

	for _, arch := range arches {
		part := filepath.Join(dir, brand.Slug+"-"+arch)
		if err = buildDesktopBinary(part, meta, arch); err != nil {
			return err
		}

		if err = reportSlice(arch, part); err != nil {
			return err
		}

		parts = append(parts, part)
	}

	if err = sh.RunV("lipo", append([]string{"-create", "-output", exe}, parts...)...); err != nil {
		return fmt.Errorf("combine the slices with lipo: %w", err)
	}

	return checkArches(exe, arches)
}

// normalizeModes gives every folder in bundle and every executable (exe, and any file with
// an execute bit) mode 0755, and every other file 0644, so the bundle the zip and the disk
// image hold reads the same whatever umask built it. Links are left alone. It works inside
// an os.Root of the bundle, so nothing it changes can lie outside it.
func normalizeModes(bundle, exe string) error {
	root, err := os.OpenRoot(bundle)
	if err != nil {
		return fmt.Errorf("set the bundle's modes: %w", err)
	}
	defer func() { _ = root.Close() }()

	exeRel, err := filepath.Rel(bundle, exe)
	if err != nil {
		return fmt.Errorf("set the bundle's modes: %w", err)
	}

	err = fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		mode, ok, modeErr := appMode(d, name == filepath.ToSlash(exeRel))
		if modeErr != nil || !ok {
			return modeErr
		}

		return root.Chmod(name, mode)
	})
	if err != nil {
		return fmt.Errorf("set the bundle's modes: %w", err)
	}

	return nil
}

// appMode is the mode an app bundle's entry d gets: 0755 for a folder or an executable
// (exe, or a file with an execute bit), 0644 for any other file, and none (ok false) for
// a link.
func appMode(d fs.DirEntry, exe bool) (os.FileMode, bool, error) {
	info, err := d.Info()
	if err != nil {
		return 0, false, err
	}

	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		return 0, false, nil
	case d.IsDir() || exe || info.Mode()&0o111 != 0:
		return 0o755, true, nil
	default:
		return 0o644, true, nil
	}
}

// lipoArch is the name lipo gives the architecture of a GOARCH.
func lipoArch(goarch string) string {
	if goarch == "amd64" {
		return "x86_64"
	}

	return goarch
}

// checkArches fails unless lipo reads exactly the architectures of arches in exe.
func checkArches(exe string, arches []string) error {
	out, err := sh.Output("lipo", "-archs", exe)
	if err != nil {
		return fmt.Errorf("read the binary's architectures: %w", err)
	}

	got := strings.Fields(out)
	want := make([]string, 0, len(arches))

	for _, arch := range arches {
		want = append(want, lipoArch(arch))
	}

	slices.Sort(got)
	slices.Sort(want)

	if !slices.Equal(got, want) {
		return fmt.Errorf("%w: lipo -archs reads %q, not %q", errArches, strings.Join(got, " "), strings.Join(want, " "))
	}

	return nil
}

// signer signs code with codesign: ad hoc, or with a code-signing identity.
type signer struct {
	identity string // the identity's name or SHA-1, or adHoc
	keychain string // the keychain that holds the identity, or "" for the search list
}

// String says how the signer signs, for an error.
func (s signer) String() string {
	if s.identity == adHoc {
		return "ad hoc"
	}

	return "as " + s.identity
}

// args are codesign's arguments to sign path. Ad hoc they are the ones app:build has
// always used; with an identity they name its keychain when there is one, and ask for no
// timestamp, which would send the signature to Apple's server.
func (s signer) args(path string) []string {
	if s.identity == adHoc {
		return []string{"--force", "--sign", adHoc, path}
	}

	args := []string{"--force", "--sign", s.identity}
	if s.keychain != "" {
		args = append(args, "--keychain", s.keychain)
	}

	return append(args, "--timestamp=none", path)
}

// sign signs path, a bundle or a disk image.
func (s signer) sign(path string) error {
	return sh.RunV("codesign", s.args(path)...)
}

// buildMeta is the version metadata stamped into a build.
type buildMeta struct {
	version       string // the build's version, e.g. v0.1.0-3-gabc1234
	commit        string // the short commit hash
	date          string // the build time, RFC 3339 UTC
	release       string // the release version, e.g. 0.2.0-rc.1 (main.release)
	bundleVersion string // the release version macOS shows, its numeric core, e.g. 0.2.0 (CFBundleShortVersionString)
	bundleBuild   string // the build number, the commit count, e.g. 42 (CFBundleVersion)
	flavor        string // the kind of build, flavorRelease for a release or "" for a build from source (main.flavor)
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
		release:       brandgen.PlaceholderVersion,
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
		meta.release = strings.TrimPrefix(out, "v")
		meta.bundleVersion = numericCore(meta.release)
	}

	if out := gitOutput("rev-list", "--count", "HEAD"); out != "" {
		meta.bundleBuild = out
	}

	return meta
}

// numericCore returns the MAJOR.MINOR.PATCH of version, which is all macOS expects of a
// bundle's version: 0.2.0 for 0.2.0-rc.1.
func numericCore(version string) string {
	core, _, _ := strings.Cut(version, "+")
	core, _, _ = strings.Cut(core, "-")

	return core
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
// desktop command, which reports it on the About page, and its flavor, which a build from
// source leaves empty.
func versionFlags(meta buildMeta) string {
	flags := fmt.Sprintf("-X main.version=%s -X main.commit=%s -X main.buildDate=%s -X main.release=%s -X main.buildNumber=%s",
		meta.version, meta.commit, meta.date, meta.release, meta.bundleBuild)

	if meta.flavor != "" {
		flags += " -X main.flavor=" + meta.flavor
	}

	return flags
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

// buildDesktopBinary compiles the production desktop binary into exe, for goarch, or for
// this Mac's processor when goarch is "".
func buildDesktopBinary(exe string, meta buildMeta, goarch string) error {
	minVersion := "-mmacosx-version-min=" + macOSMinVersion
	env := map[string]string{
		"CGO_ENABLED":              "1",
		"CGO_CFLAGS":               minVersion,
		"CGO_LDFLAGS":              minVersion,
		"MACOSX_DEPLOYMENT_TARGET": macOSMinVersion,
	}

	if goarch != "" {
		env["GOARCH"] = goarch
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

// reportSlice prints the size of one architecture's slice of a universal binary, which
// the per-architecture size target is held to.
func reportSlice(arch, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("read the %s slice's size: %w", arch, err)
	}

	//nolint:forbidigo // mage target output
	fmt.Printf("Built the %s slice (%d bytes, %.1f MB)\n", arch, info.Size(), float64(info.Size())/1e6)

	return nil
}
