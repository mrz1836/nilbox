//go:build mage

package main

import (
	"archive/zip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/magefile/mage/sh"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/update"
)

// The environment app:package reads. The names say what each holds and nothing of the
// product, so a CI workflow can pass them as they are.
const (
	envReleaseVersion   = "RELEASE_VERSION"    // the version to release, without its v
	envCodesignIdentity = "CODESIGN_IDENTITY"  // the code-signing identity's name or SHA-1
	envCodesignKeychain = "CODESIGN_KEYCHAIN"  // the keychain that holds it, when not the search list's
	envUpdateSigningKey = "UPDATE_SIGNING_KEY" // the path to the release's Ed25519 private key
)

const (
	// flavorRelease is the flavor stamped into a release build (main.flavor); a build from
	// source stamps none.
	flavorRelease = "release"

	// releaseRoot holds each release's files, in a folder named for its version.
	releaseRoot = "bin/release"

	// signingKeyFile is the name app:keygen gives the private key it writes.
	signingKeyFile = "release.key"

	// dmgFormat is the disk image's format: compressed with LZMA, which macOS reads since
	// 10.15, and about a third smaller than the zlib UDZO.
	dmgFormat = "ULMO"

	// maxSigningKeyBytes bounds the read of the private key's file, which holds one short
	// PEM block.
	maxSigningKeyBytes = 64 << 10
)

// releaseArches are the architectures a release's universal binary holds: Apple silicon
// and Intel.
func releaseArches() []string {
	return []string{"arm64", "amd64"}
}

var (
	// errNoReleaseKey is returned while the app pins no release key, so no release could
	// ship a manifest it would verify.
	errNoReleaseKey = errors.New("no release key pinned yet: make one with magex app:keygen and pin its public key " +
		"as update.ReleaseKey")

	// errMissingEnv is returned when app:package lacks a required environment variable.
	errMissingEnv = errors.New("app:package needs these environment variables")

	// errBadVersion is returned for a RELEASE_VERSION that isn't a release's version.
	errBadVersion = errors.New(envReleaseVersion + " is not a release version such as 0.3.0 or 0.3.0-rc.1")

	// errAdHoc is returned for an identity that would sign ad hoc.
	errAdHoc = errors.New(envCodesignIdentity + " must name a code-signing identity: a release is never signed ad hoc")

	// errNoExactTag is returned when HEAD carries no tag.
	errNoExactTag = errors.New("HEAD has no tag; a release is built from the commit its tag names")

	// errTagMismatch is returned when the tag on HEAD names another version.
	errTagMismatch = errors.New(envReleaseVersion + " does not match the tag on HEAD")

	// errKeyMismatch is returned when the private key doesn't match the key the app pins.
	errKeyMismatch = errors.New("the signing key does not match the release key the app pins")

	// errZipLayout is returned for a zip that holds anything but the app bundle.
	errZipLayout = errors.New("the zip must hold exactly the app bundle")

	// errInRepository is returned for a key folder inside the repository.
	errInRepository = errors.New("keep the private key outside the repository")

	// errNoKeyFolder is returned when app:keygen is given no folder.
	errNoKeyFolder = errors.New("name a folder outside the repository: magex app:keygen out=<folder>")

	// errNoReleaseDir is returned when app:verify is given no folder.
	errNoReleaseDir = errors.New("name the folder that holds the release's files: magex app:verify dir=<folder>")

	// errOtherPlatform is returned for a manifest of another platform than releases ship.
	errOtherPlatform = errors.New("the manifest is for another platform")
)

// Package builds a release of the macOS app into bin/release/<version>/: a universal
// bundle for Apple silicon and Intel, signed with a code-signing identity; its zip, which
// updates install; a signed disk image for first installs; SHA256SUMS; and the update
// manifest, signed with the release's private key and verified against the key the app
// pins. It reads RELEASE_VERSION, which must be the exact tag on HEAD without its v;
// CODESIGN_IDENTITY, a name or SHA-1, never - (ad hoc); CODESIGN_KEYCHAIN, optionally, a
// keychain that holds the identity, which goes on the keychain search list while the
// target runs; and UPDATE_SIGNING_KEY, the path to the private key app:keygen wrote.
//
//	RELEASE_VERSION=0.3.0 CODESIGN_IDENTITY=<identity> UPDATE_SIGNING_KEY=<key file> magex app:package
func (App) Package() error {
	if runtime.GOOS != "darwin" {
		return errNeedsMacOS
	}

	cfg, err := packageConfigFromEnv(update.ReleaseKey)
	if err != nil {
		return err
	}

	return packageRelease(cfg)
}

// Keygen makes the release key, the Ed25519 key pair whose private half signs every
// release's update manifest: it writes the private key to <out>/release.key (PKCS#8 PEM,
// readable by you alone) and prints the public key, base64, to pin as update.ReleaseKey.
// It refuses a folder inside the repository and never overwrites a key.
//
//	magex app:keygen out=<a folder outside the repository>
func (App) Keygen() error {
	repo, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("find the repository: %w", err)
	}

	return keygen(mageArg("out"), repo)
}

// Verify checks a release's files as the app will: the update manifest in dir, against
// the release key the app pins, and the zip it names, by its size and SHA-512.
//
//	magex app:verify dir=bin/release/<version>
func (App) Verify() error {
	return verifyPinned(mageArg("dir"), update.ReleaseKey)
}

// packageConfig is what packageRelease builds a release from.
type packageConfig struct {
	version    string // the version to release, without its v
	identity   string // the code-signing identity's name or SHA-1
	keychain   string // the keychain that holds the identity, or "" for the search list
	signingKey string // the path to the release's Ed25519 private key, PKCS#8 PEM
	releaseKey string // the public key, base64, the manifest must verify with: the one the app pins
}

// releaseKeys are the release key's halves: the private one that signs the manifest, and
// the public one the app pins.
type releaseKeys struct {
	private ed25519.PrivateKey
	public  ed25519.PublicKey
}

// packageConfigFromEnv reads app:package's environment, with releaseKey as the key the
// manifest must verify with. It refuses while no key is pinned, and names every required
// variable that is missing.
func packageConfigFromEnv(releaseKey string) (packageConfig, error) {
	if releaseKey == "" {
		return packageConfig{}, errNoReleaseKey
	}

	cfg := packageConfig{
		version:    os.Getenv(envReleaseVersion),
		identity:   os.Getenv(envCodesignIdentity),
		keychain:   os.Getenv(envCodesignKeychain),
		signingKey: os.Getenv(envUpdateSigningKey),
		releaseKey: releaseKey,
	}

	var missing []string

	for _, v := range []struct{ name, value string }{
		{envReleaseVersion, cfg.version}, {envCodesignIdentity, cfg.identity}, {envUpdateSigningKey, cfg.signingKey},
	} {
		if v.value == "" {
			missing = append(missing, v.name)
		}
	}

	if len(missing) > 0 {
		return packageConfig{}, fmt.Errorf("%w: %s", errMissingEnv, strings.Join(missing, ", "))
	}

	return cfg, nil
}

// packageRelease builds the release cfg describes, the steps App.Package runs once it
// knows it is on macOS. Everything it can check without building, the version, the
// identity, the keys, and the tag, it checks before any tool runs.
func packageRelease(cfg packageConfig) error {
	keys, err := cfg.check()
	if err != nil {
		return err
	}

	if err = checkReleaseTag(cfg.version); err != nil {
		return err
	}

	if err = prepareBuild(); err != nil {
		return err
	}

	return withKeychain(cfg.keychain, func() error { return buildRelease(cfg, keys) })
}

// check refuses a release that could never ship: a version out of form, an ad hoc
// identity, or a private key that isn't the release key's other half. It returns both
// halves.
func (c packageConfig) check() (releaseKeys, error) {
	switch {
	case c.releaseKey == "":
		return releaseKeys{}, errNoReleaseKey
	case !update.ValidVersion(c.version):
		return releaseKeys{}, fmt.Errorf("%w: %q", errBadVersion, c.version)
	case c.identity == "" || c.identity == adHoc:
		return releaseKeys{}, errAdHoc
	}

	public, err := update.ParseKey([]byte(c.releaseKey))
	if err != nil {
		return releaseKeys{}, fmt.Errorf("read the release key the app pins: %w", err)
	}

	private, err := readSigningKey(c.signingKey)
	if err != nil {
		return releaseKeys{}, err
	}

	if !update.PublicKeyOf(private).Equal(public) {
		return releaseKeys{}, errKeyMismatch
	}

	return releaseKeys{private: private, public: public}, nil
}

// readSigningKey reads the release's private key from the file at path.
func readSigningKey(path string) (ed25519.PrivateKey, error) {
	f, err := os.Open(path) //nolint:gosec // the key file the person packaging named
	if err != nil {
		return nil, fmt.Errorf("read the signing key: %w", err)
	}
	defer func() { _ = f.Close() }()

	body, err := io.ReadAll(io.LimitReader(f, maxSigningKeyBytes))
	if err != nil {
		return nil, fmt.Errorf("read the signing key: %w", err)
	}

	key, err := update.ParsePrivateKey(body)
	if err != nil {
		return nil, fmt.Errorf("read the signing key %s: %w", path, err)
	}

	return key, nil
}

// checkReleaseTag fails unless HEAD's exact tag is version's, v and all.
func checkReleaseTag(version string) error {
	tag := gitOutput("describe", "--tags", "--exact-match")

	switch {
	case tag == "":
		return errNoExactTag
	case tag != "v"+version:
		return fmt.Errorf("%w: %s is %s, and HEAD's tag is %s", errTagMismatch, envReleaseVersion, version, tag)
	}

	return nil
}

// releaseMeta is the version metadata of a release of version: git's, with version as
// the release and its numeric core as the bundle's version, and the release flavor.
func releaseMeta(version string) buildMeta {
	meta := readBuildMeta()
	meta.release = version
	meta.bundleVersion = numericCore(version)
	meta.flavor = flavorRelease

	return meta
}

// buildRelease builds every file of the release cfg describes into its folder, signing
// with keys, then checks them as the app and app:verify will, and reports their sizes.
func buildRelease(cfg packageConfig, keys releaseKeys) error {
	out := filepath.Join(filepath.FromSlash(releaseRoot), cfg.version)
	names := update.AssetNames(cfg.version)
	bundle := filepath.Join(out, brand.Name+".app")
	zipPath := filepath.Join(out, names.Zip)
	dmgPath := filepath.Join(out, names.DMG)
	sign := signer{identity: cfg.identity, keychain: cfg.keychain}

	steps := []func() error{
		func() error { return freshFolder(out) },
		func() error {
			return assembleBundle(bundleSpec{bundle: bundle, meta: releaseMeta(cfg.version), arches: releaseArches(), signer: sign})
		},
		func() error { return verifySignature(bundle, "--deep") },
		func() error { return zipBundle(bundle, zipPath) },
		func() error { return makeDMG(bundle, dmgPath, sign) },
		func() error { return writeSums(filepath.Join(out, names.Sums), zipPath, dmgPath) },
		func() error { return writeManifest(filepath.Join(out, names.Manifest), cfg.version, zipPath, keys) },
		func() error { return verifyRelease(out, keys.public) },
		func() error { return reportRelease(out, names) },
	}

	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}

	return nil
}

// freshFolder empties dir of an earlier run's files, creating it when it is missing.
func freshFolder(dir string) error {
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove the earlier files in %s: %w", dir, err)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // build output, world-readable
		return fmt.Errorf("create %s: %w", dir, err)
	}

	return nil
}

// verifySignature checks the code signature of path, a bundle or a disk image, strictly,
// with extra options before the path (--deep for a bundle).
func verifySignature(path string, extra ...string) error {
	args := append(append([]string{"--verify"}, extra...), "--strict", path)

	if err := sh.RunV("codesign", args...); err != nil {
		return fmt.Errorf("verify the signature of %s: %w", path, err)
	}

	return nil
}

// zipBundle zips bundle into zipPath, the bundle's folder as the zip's one entry, without
// resource forks, and checks the zip holds exactly the bundle.
func zipBundle(bundle, zipPath string) error {
	if err := sh.RunV("ditto", "-c", "-k", "--norsrc", "--keepParent", bundle, zipPath); err != nil {
		return fmt.Errorf("zip the bundle: %w", err)
	}

	return checkZipLayout(zipPath, filepath.Base(bundle))
}

// checkZipLayout fails unless the zip at zipPath holds exactly the bundle top, as the
// app's installer requires: one top-level entry, top, with the bundle's executable and
// Info.plist inside it, no other entry beside it (a __MACOSX folder of resource forks, a
// second bundle, a stray file), and no name or link that leads out of it.
func checkZipLayout(zipPath, top string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("read the zip: %w", err)
	}
	defer func() { _ = r.Close() }()

	required := map[string]bool{
		top + "/Contents/Info.plist":          false,
		top + "/Contents/MacOS/" + brand.Slug: false,
	}

	for _, f := range r.File {
		if err = checkZipEntry(f, top); err != nil {
			return err
		}

		if _, ok := required[f.Name]; ok {
			required[f.Name] = true
		}
	}

	for name, found := range required {
		if !found {
			return fmt.Errorf("%w: it lacks %s", errZipLayout, name)
		}
	}

	return nil
}

// checkZipEntry fails unless f is the bundle top or inside it, and, when it is a link,
// points inside it too.
func checkZipEntry(f *zip.File, top string) error {
	name := f.Name
	if !inside(name, top) {
		return fmt.Errorf("%w: %q is beside %s", errZipLayout, name, top)
	}

	if f.Mode()&fs.ModeSymlink == 0 {
		return nil
	}

	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("read the link %s in the zip: %w", name, err)
	}
	defer func() { _ = rc.Close() }()

	target, err := io.ReadAll(io.LimitReader(rc, 4096))
	if err != nil {
		return fmt.Errorf("read the link %s in the zip: %w", name, err)
	}

	if t := string(target); path.IsAbs(t) || !inside(path.Join(path.Dir(name), t), top) {
		return fmt.Errorf("%w: the link %q points out of %s", errZipLayout, name, top)
	}

	return nil
}

// inside reports whether the zip entry name is top or within it, with no element that
// climbs out.
func inside(name, top string) bool {
	if strings.Contains(name, `\`) || path.IsAbs(name) || slices.Contains(strings.Split(name, "/"), "..") {
		return false
	}

	clean := path.Clean(name)

	return clean == top || strings.HasPrefix(clean, top+"/")
}

// makeDMG makes the disk image first installs open, at dmg: the bundle beside a link to
// /Applications to drag it onto, in a volume named for the app, compressed, then signed
// with sign and its signature checked.
func makeDMG(bundle, dmg string, sign signer) error {
	staging, err := os.MkdirTemp(filepath.Dir(dmg), ".dmg-")
	if err != nil {
		return fmt.Errorf("create the disk image's folder: %w", err)
	}
	defer func() { _ = os.RemoveAll(staging) }()

	// The folder becomes the volume's root, which everyone who mounts it reads.
	if err = os.Chmod(staging, 0o755); err != nil { //nolint:gosec // a disk image's root is world-readable
		return fmt.Errorf("open the disk image's folder to everyone: %w", err)
	}

	if err = sh.RunV("ditto", bundle, filepath.Join(staging, filepath.Base(bundle))); err != nil {
		return fmt.Errorf("copy the bundle for the disk image: %w", err)
	}

	if err = os.Symlink("/Applications", filepath.Join(staging, "Applications")); err != nil {
		return fmt.Errorf("link Applications for the disk image: %w", err)
	}

	err = sh.RunV("hdiutil", "create", "-volname", brand.Name, "-srcfolder", staging, "-ov", "-format", dmgFormat, dmg)
	if err != nil {
		return fmt.Errorf("make the disk image: %w", err)
	}

	if err = sign.sign(dmg); err != nil {
		return fmt.Errorf("sign the disk image %s: %w", sign, err)
	}

	return verifySignature(dmg)
}

// writeSums writes the SHA-256 of each file to path, one "<hex>  <name>" line each,
// sorted by name, as shasum -a 256 -c reads them.
func writeSums(path string, files ...string) error {
	lines := make([]string, 0, len(files))

	for _, file := range files {
		sum, err := sha256File(file)
		if err != nil {
			return err
		}

		lines = append(lines, sum+"  "+filepath.Base(file)+"\n")
	}

	slices.SortFunc(lines, func(a, b string) int { return strings.Compare(a[sha256.Size*2:], b[sha256.Size*2:]) })

	if err := os.WriteFile(path, []byte(strings.Join(lines, "")), 0o644); err != nil { //nolint:gosec // a release file, public
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}

	return nil
}

// sha256File returns the SHA-256 of the file at path, as hex.
func sha256File(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // a file this target made
	if err != nil {
		return "", fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// writeManifest writes the update manifest of the zip at zipPath, the release of version,
// to path: signed with the private key, then verified against the public one, so a key
// that doesn't match the app's fails the build here, never in a user's hands.
func writeManifest(path, version, zipPath string, keys releaseKeys) error {
	f, err := os.Open(zipPath) //nolint:gosec // the zip this target made
	if err != nil {
		return fmt.Errorf("read the zip: %w", err)
	}
	defer func() { _ = f.Close() }()

	size, sum, err := update.Digest(f)
	if err != nil {
		return err
	}

	m, err := update.Sign(update.Manifest{
		Version: version, Platform: update.Platform, File: filepath.Base(zipPath), Size: size, SHA512: sum,
	}, keys.private)
	if err != nil {
		return fmt.Errorf("sign the update manifest: %w", err)
	}

	if err = update.Verify(m, keys.public); err != nil {
		return fmt.Errorf("%w: %w", errKeyMismatch, err)
	}

	body, err := update.Encode(m)
	if err != nil {
		return err
	}

	if err = os.WriteFile(path, body, 0o644); err != nil { //nolint:gosec // a release file, public
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}

	return nil
}

// verifyPinned checks the release in dir as verifyRelease does, against releaseKey, the
// key the app pins.
func verifyPinned(dir, releaseKey string) error {
	switch {
	case dir == "":
		return errNoReleaseDir
	case releaseKey == "":
		return errNoReleaseKey
	}

	key, err := update.ParseKey([]byte(releaseKey))
	if err != nil {
		return fmt.Errorf("read the release key the app pins: %w", err)
	}

	return verifyRelease(dir, key)
}

// verifyRelease checks the release in dir as the app will: its update manifest, at most
// update.MaxManifestBytes, in form, for the platform releases ship, and signed by key's
// private half; then the zip it names, by its size and SHA-512.
func verifyRelease(dir string, key ed25519.PublicKey) error {
	m, err := readManifest(filepath.Join(dir, update.ManifestName))
	if err != nil {
		return err
	}

	if err = update.Verify(m, key); err != nil {
		return fmt.Errorf("verify %s: %w", update.ManifestName, err)
	}

	if m.Platform != update.Platform {
		return fmt.Errorf("%w: %s, not %s", errOtherPlatform, m.Platform, update.Platform)
	}

	// Verify held the file's name to its exact form, so it names a file in dir.
	f, err := os.Open(filepath.Join(dir, m.File)) //nolint:gosec // a name the signed manifest holds to its form
	if err != nil {
		return fmt.Errorf("read the zip the manifest names: %w", err)
	}
	defer func() { _ = f.Close() }()

	if err = update.CheckFile(m, f); err != nil {
		return err
	}

	say("✅ " + m.File + " matches its signed manifest (" + m.Version + ", " + m.Platform + ")")

	return nil
}

// readManifest reads the update manifest at path, reading no more than one byte past the
// largest manifest, so a larger one is refused without being read to its end.
func readManifest(path string) (update.Manifest, error) {
	f, err := os.Open(path) //nolint:gosec // the release folder the person verifying named
	if err != nil {
		return update.Manifest{}, fmt.Errorf("read the update manifest: %w", err)
	}
	defer func() { _ = f.Close() }()

	body, err := io.ReadAll(io.LimitReader(f, update.MaxManifestBytes+1))
	if err != nil {
		return update.Manifest{}, fmt.Errorf("read the update manifest: %w", err)
	}

	return update.Parse(body)
}

// reportRelease prints the release's files in out with their sizes.
func reportRelease(out string, names update.Assets) error {
	say("✅ Packaged the release in " + out + ":")

	for _, name := range []string{names.Zip, names.DMG, names.Sums, names.Manifest} {
		info, err := os.Stat(filepath.Join(out, name))
		if err != nil {
			return fmt.Errorf("read the size of %s: %w", name, err)
		}

		say(fmt.Sprintf("  %s (%d bytes, %.1f MB)", name, info.Size(), float64(info.Size())/1e6))
	}

	return nil
}

// keygen makes the release key in out, which must not be inside repo, as App.Keygen
// describes.
func keygen(out, repo string) error {
	if out == "" {
		return errNoKeyFolder
	}

	in, err := within(out, repo)
	if err != nil {
		return err
	}

	if in {
		return fmt.Errorf("%w: %s is inside %s", errInRepository, out, repo)
	}

	if err = os.MkdirAll(out, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", out, err)
	}

	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate the release key: %w", err)
	}

	body, err := update.EncodePrivateKey(private)
	if err != nil {
		return err
	}

	path := filepath.Join(out, signingKeyFile)
	if err = writeNewFile(path, body, 0o600); err != nil {
		return err
	}

	say("Wrote the release's private key to " + path + ". Keep it secret and back it up: without it, no install " +
		"can verify an update again.")
	say("The public key, base64, to pin as update.ReleaseKey:")
	say(update.EncodeKey(public))

	return nil
}

// writeNewFile writes body to a new file at path with mode perm, refusing a file that is
// already there.
func writeNewFile(path string, body []byte, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm) //nolint:gosec // a folder app:keygen was named
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}

	if _, err = f.Write(body); err != nil {
		_ = f.Close()

		return fmt.Errorf("write %s: %w", path, err)
	}

	if err = f.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	return nil
}

// within reports whether path is root or inside it, each made absolute with its links
// resolved, as far as it exists.
func within(path, root string) (bool, error) {
	p, err := resolvedPath(path)
	if err != nil {
		return false, err
	}

	r, err := resolvedPath(root)
	if err != nil {
		return false, err
	}

	rel, err := filepath.Rel(r, p)
	if err != nil {
		return false, fmt.Errorf("compare %s with %s: %w", path, root, err)
	}

	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))), nil
}

// resolvedPath returns path made absolute, with the links of its longest part that exists
// resolved and the rest, which doesn't exist yet, joined on as written.
func resolvedPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", path, err)
	}

	var rest []string

	for dir := abs; ; dir = filepath.Dir(dir) {
		resolved, evalErr := filepath.EvalSymlinks(dir)
		if evalErr == nil {
			return filepath.Join(append([]string{resolved}, rest...)...), nil
		}

		if !errors.Is(evalErr, fs.ErrNotExist) || filepath.Dir(dir) == dir {
			return "", fmt.Errorf("resolve %s: %w", path, evalErr)
		}

		rest = append([]string{filepath.Base(dir)}, rest...)
	}
}
