//go:build mage

package main

import (
	"archive/zip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
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

	// unsignedRoot holds each release's unsigned bundle, alone in a folder named for its
	// version, which app:buildRelease builds and app:signRelease signs.
	unsignedRoot = "bin/unsigned"

	// bundleName is the app bundle's folder name.
	bundleName = brand.Name + ".app"

	// dmgFormat is the disk image's format: compressed with LZMA, which macOS reads since
	// 10.15, and about a third smaller than the zlib UDZO.
	dmgFormat = "ULMO"

	// maxSigningKeyBytes bounds the read of the private key's file, which holds one short
	// PEM block.
	maxSigningKeyBytes = 64 << 10
)

// pinnedReleaseKey is the release key app:package and app:verify check against:
// update.ReleaseKey, which tests replace with a throwaway key of their own, so the targets'
// wiring stays tested once a key is pinned.
//
//nolint:gochecknoglobals // tests swap in a throwaway key
var pinnedReleaseKey = update.ReleaseKey

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

	// errMissingEnv is returned when app:package or app:signRelease lacks a required
	// environment variable.
	errMissingEnv = errors.New("signing a release needs these environment variables")

	// errBundlePath is returned when app:signRelease isn't given the absolute path of an
	// unsigned bundle outside the release's own folder.
	errBundlePath = errors.New("bundle= must be the absolute path of the unsigned " + bundleName +
		" app:buildRelease built, outside " + releaseRoot)

	// errUnsignedBundle is returned for a bundle to sign that isn't an unsigned bundle as
	// app:buildRelease builds it.
	errUnsignedBundle = errors.New("not an unsigned bundle as app:buildRelease builds it")

	// errBadVersion is returned for a RELEASE_VERSION that isn't a release's version.
	errBadVersion = errors.New(envReleaseVersion + " is not a release version such as 0.3.0 or 0.3.0-rc.1")

	// errAdHoc is returned for an identity that would sign ad hoc.
	errAdHoc = errors.New(envCodesignIdentity + " must name a code-signing identity: a release is never signed ad hoc")

	// errNoReleaseTag is returned when no tag names the version to release.
	errNoReleaseTag = errors.New("no tag names " + envReleaseVersion + "; a release is built from the commit its tag names")

	// errTagMismatch is returned when the version's tag names another commit than HEAD.
	errTagMismatch = errors.New("the tag of " + envReleaseVersion + " names another commit than HEAD")

	// errDirtyTree is returned when the working tree holds changes its tag doesn't.
	errDirtyTree = errors.New("the working tree has changes the tag doesn't hold; commit or stash them, so the " +
		"release is built from its tag alone")

	// errKeychainPath is returned for a CODESIGN_KEYCHAIN that isn't an absolute path to a
	// file: security reads a relative one under ~/Library/Keychains, codesign from the
	// working folder, so the two could disagree.
	errKeychainPath = errors.New(envCodesignKeychain + " must be the absolute path of a keychain file")

	// errKeyMismatch is returned when the private key doesn't match the key the app pins.
	errKeyMismatch = errors.New("the signing key does not match the release key the app pins")

	// errZipLayout is returned for a zip that holds anything but the app bundle.
	errZipLayout = errors.New("the zip must hold exactly the app bundle")

	// errNoReleaseDir is returned when app:verify is given no folder.
	errNoReleaseDir = errors.New("name the folder that holds the release's files: magex app:verify dir=<folder>")

	// errOtherPlatform is returned for a manifest of another platform than releases ship.
	errOtherPlatform = errors.New("the manifest is for another platform")

	// errOtherVersion is returned for a manifest of another version than app:verify was
	// asked to check.
	errOtherVersion = errors.New("the manifest is for another version")
)

// Package builds a release of the macOS app into bin/release/<version>/: a universal
// bundle for Apple silicon and Intel, signed with a code-signing identity; its zip, which
// updates install; a signed disk image for first installs; SHA256SUMS; and the update
// manifest, signed with the release's private key and verified against the key the app
// pins. It reads RELEASE_VERSION, the version without its v, whose tag must name HEAD in a
// working tree with no changes; CODESIGN_IDENTITY, a name or SHA-1, never - (ad hoc);
// CODESIGN_KEYCHAIN, optionally, the absolute path of a keychain that holds the identity,
// which goes on the keychain search list while the bundle is signed; and
// UPDATE_SIGNING_KEY, the path to the private key app:keygen wrote. It is
// app:buildRelease, then app:signRelease of the bundle that built, in one target.
//
//	RELEASE_VERSION=0.4.0 CODESIGN_IDENTITY=<identity> UPDATE_SIGNING_KEY=<key file> magex app:package
func (App) Package() error {
	if runtime.GOOS != goosMacOS {
		return errNeedsMacOS
	}

	cfg, err := packageConfigFromEnv(pinnedReleaseKey)
	if err != nil {
		return err
	}

	return packageRelease(cfg)
}

// BuildRelease builds the first half of app:package, which needs no secret: the
// release's universal bundle for Apple silicon and Intel, unsigned, alone in
// bin/unsigned/<version>/. It reads RELEASE_VERSION, whose tag must name HEAD in a working
// tree with no changes, and refuses while the app pins no release key, as app:package
// does. app:signRelease signs what it builds.
//
//	RELEASE_VERSION=0.4.0 magex app:buildRelease
func (App) BuildRelease() error {
	if runtime.GOOS != goosMacOS {
		return errNeedsMacOS
	}

	_, err := buildReleaseBundle(os.Getenv(envReleaseVersion), pinnedReleaseKey)

	return err
}

// SignRelease builds the second half of app:package from the unsigned bundle
// app:buildRelease built: given its absolute path as bundle=, it checks the bundle (named
// as the app's, alone in its folder, folders and regular files only, unsigned, and
// universal), copies it into bin/release/<version>/, signs it, and builds the release's
// other files there, exactly as app:package does. It reads the environment app:package
// reads, and checks the tag and the working tree as it does.
//
//	RELEASE_VERSION=0.4.0 CODESIGN_IDENTITY=<identity> UPDATE_SIGNING_KEY=<key file> \
//	  magex app:signRelease bundle=<absolute path of bin/unsigned/0.4.0/<Name>.app>
func (App) SignRelease() error {
	if runtime.GOOS != goosMacOS {
		return errNeedsMacOS
	}

	cfg, err := packageConfigFromEnv(pinnedReleaseKey)
	if err != nil {
		return err
	}

	return signReleaseBundle(cfg, mageArg("bundle"))
}

// Verify checks a release's files as the app will: the update manifest in dir, against
// the release key the app pins, and the zip it names, by its size and SHA-512. With
// version (with or without its v) it also refuses a manifest of any other version, so a
// stale pair can't pass for this release.
//
//	magex app:verify dir=bin/release/<version>
//	magex app:verify dir=<folder> version=0.3.0
func (App) Verify() error {
	return verifyPinned(mageArg("dir"), mageArg("version"), pinnedReleaseKey)
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
		keychain:   cleanPath(os.Getenv(envCodesignKeychain)),
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

// cleanPath returns path cleaned, or "" for "".
func cleanPath(path string) string {
	if path == "" {
		return ""
	}

	return filepath.Clean(path)
}

// packageRelease builds the release cfg describes, the steps App.Package runs once it
// knows it is on macOS: app:buildRelease's, then app:signRelease's. Everything it can
// check without building, the version, the identity, the keychain, the keys, the tag, and
// the working tree, it checks before any tool but git runs.
func packageRelease(cfg packageConfig) error {
	keys, err := cfg.check()
	if err != nil {
		return err
	}

	if err = checkReleaseTag(cfg.version); err != nil {
		return err
	}

	if err = checkCleanTree(); err != nil {
		return err
	}

	bundle, err := buildUnsignedRelease(cfg.version)
	if err != nil {
		return err
	}

	return signRelease(cfg, keys, bundle)
}

// buildReleaseBundle builds the unsigned bundle of the release of version, the steps
// App.BuildRelease runs once it knows it is on macOS, and returns its path. It refuses
// while no release key is pinned (releaseKey, as app:package does, so a release that could
// never ship isn't built), a version out of form, a tag that doesn't name HEAD, and a
// working tree with changes, before any tool but git runs.
func buildReleaseBundle(version, releaseKey string) (string, error) {
	switch {
	case releaseKey == "":
		return "", errNoReleaseKey
	case !update.ValidVersion(version):
		return "", fmt.Errorf("%w: %q", errBadVersion, version)
	}

	if err := checkReleaseTag(version); err != nil {
		return "", err
	}

	if err := checkCleanTree(); err != nil {
		return "", err
	}

	return buildUnsignedRelease(version)
}

// signReleaseBundle signs the unsigned bundle at bundle and packages the release cfg
// describes, the steps App.SignRelease runs once it knows it is on macOS. Before the
// keychain search list is touched, it checks what packageRelease checks, and that bundle
// is an absolute path outside the release's own folder, which packaging empties first.
func signReleaseBundle(cfg packageConfig, bundle string) error {
	keys, err := cfg.check()
	if err != nil {
		return err
	}

	if err = checkBundlePath(bundle, cfg.version); err != nil {
		return err
	}

	if err = checkReleaseTag(cfg.version); err != nil {
		return err
	}

	if err = checkCleanTree(); err != nil {
		return err
	}

	return signRelease(cfg, keys, filepath.Clean(bundle))
}

// checkBundlePath fails unless bundle is an absolute path outside the release folder of
// version, which packaging the release empties before it copies the bundle in.
func checkBundlePath(bundle, version string) error {
	if bundle == "" || !filepath.IsAbs(bundle) {
		return fmt.Errorf("%w: %q", errBundlePath, bundle)
	}

	out, err := filepath.Abs(filepath.Join(filepath.FromSlash(releaseRoot), version))
	if err != nil {
		return fmt.Errorf("find the release's folder: %w", err)
	}

	rel, err := filepath.Rel(out, filepath.Clean(bundle))
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%w: %s is inside it", errBundlePath, bundle)
	}

	return nil
}

// buildUnsignedRelease checks the brand files, builds the frontend, and builds the release
// of version's universal bundle, unsigned, into a fresh bin/unsigned/<version>/, alone in
// it. It returns the bundle's path.
func buildUnsignedRelease(version string) (string, error) {
	if err := prepareBuild(); err != nil {
		return "", err
	}

	dir := filepath.Join(filepath.FromSlash(unsignedRoot), version)
	bundle := filepath.Join(dir, bundleName)

	if err := freshFolder(dir); err != nil {
		return "", err
	}

	spec := bundleSpec{bundle: bundle, meta: releaseMeta(readBuildMeta(), version), arches: releaseArches()}
	if _, err := buildUnsignedBundle(spec); err != nil {
		return "", err
	}

	say("✅ Built the unsigned bundle " + bundle)

	return bundle, nil
}

// signRelease checks the unsigned bundle at unsigned, then, with cfg's keychain first on
// the search list, signs a copy of it and builds the release's other files with it, as
// buildRelease does.
func signRelease(cfg packageConfig, keys releaseKeys, unsigned string) error {
	if err := checkUnsignedBundle(unsigned); err != nil {
		return err
	}

	return withKeychain(cfg.keychain, func() error { return buildRelease(cfg, keys, unsigned) })
}

// checkUnsignedBundle fails unless bundle is an unsigned bundle as app:buildRelease builds
// it: a folder (not a link) named as the app's, alone in its folder, holding only folders
// and regular files (no link, which is the one way out of it), its Info.plist and binary
// among them, no code signature yet, and a binary lipo reads both release architectures
// in. A workflow's build artifact is checked so before it is signed.
func checkUnsignedBundle(bundle string) error {
	info, err := os.Lstat(bundle)

	switch {
	case err != nil:
		return fmt.Errorf("%w: %w", errUnsignedBundle, err)
	case !info.IsDir():
		return fmt.Errorf("%w: %s is not a folder", errUnsignedBundle, bundle)
	case filepath.Base(bundle) != bundleName:
		return fmt.Errorf("%w: %s isn't named %s", errUnsignedBundle, bundle, bundleName)
	}

	if err = checkAlone(bundle); err != nil {
		return err
	}

	if err = checkBundleEntries(bundle); err != nil {
		return err
	}

	return checkArches(bundleExecutable(bundle), releaseArches())
}

// checkAlone fails unless bundle is the only entry of its folder, as it is in
// app:buildRelease's folder and in the build artifact a workflow hands app:signRelease.
func checkAlone(bundle string) error {
	entries, err := os.ReadDir(filepath.Dir(bundle))
	if err != nil {
		return fmt.Errorf("%w: %w", errUnsignedBundle, err)
	}

	others := make([]string, 0, len(entries))

	for _, e := range entries {
		if e.Name() != filepath.Base(bundle) {
			others = append(others, e.Name())
		}
	}

	if len(others) > 0 {
		return fmt.Errorf("%w: it must be alone in its folder, which also holds %s", errUnsignedBundle,
			strings.Join(others, ", "))
	}

	return nil
}

// checkBundleEntries walks bundle inside an os.Root, so nothing it reads lies outside it,
// and fails on an entry that is neither a folder nor a regular file, on a code signature,
// and when the Info.plist or the binary is missing.
func checkBundleEntries(bundle string) error {
	root, err := os.OpenRoot(bundle)
	if err != nil {
		return fmt.Errorf("%w: %w", errUnsignedBundle, err)
	}
	defer func() { _ = root.Close() }()

	files := map[string]bool{}

	err = fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, walkErr error) error {
		switch {
		case walkErr != nil:
			return walkErr
		case !d.IsDir() && !d.Type().IsRegular():
			return fmt.Errorf("%w: %s is a link or a special file", errUnsignedBundle, name)
		case name == "Contents/_CodeSignature":
			return fmt.Errorf("%w: it is signed already", errUnsignedBundle)
		}

		files[name] = d.Type().IsRegular()

		return nil
	})
	if err != nil {
		if errors.Is(err, errUnsignedBundle) {
			return err
		}

		return fmt.Errorf("%w: %w", errUnsignedBundle, err)
	}

	for _, want := range []string{"Contents/Info.plist", "Contents/MacOS/" + brand.Slug} {
		if !files[want] {
			return fmt.Errorf("%w: it lacks %s", errUnsignedBundle, want)
		}
	}

	return nil
}

// copyBundle copies the bundle at src to dst, which must not exist yet, read through an
// os.Root of src; checkUnsignedBundle has refused a link or a special file inside it,
// which the copy refuses too. Modes are set again before the copy is signed.
func copyBundle(src, dst string) error {
	root, err := os.OpenRoot(src)
	if err != nil {
		return fmt.Errorf("copy the unsigned bundle: %w", err)
	}
	defer func() { _ = root.Close() }()

	if err = os.CopyFS(dst, root.FS()); err != nil {
		return fmt.Errorf("copy the unsigned bundle: %w", err)
	}

	return nil
}

// check refuses a release that could never ship: a version out of form, an ad hoc
// identity, a keychain that isn't an absolute path to a file, or a private key that isn't
// the release key's other half. It returns both halves.
func (c packageConfig) check() (releaseKeys, error) {
	switch {
	case c.releaseKey == "":
		return releaseKeys{}, errNoReleaseKey
	case !update.ValidVersion(c.version):
		return releaseKeys{}, fmt.Errorf("%w: %q", errBadVersion, c.version)
	case c.identity == "" || c.identity == adHoc:
		return releaseKeys{}, errAdHoc
	}

	if err := checkKeychain(c.keychain); err != nil {
		return releaseKeys{}, err
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

// checkKeychain fails unless keychain is "" (the search list's) or the absolute path of a
// file, the one value both security and codesign are given.
func checkKeychain(keychain string) error {
	if keychain == "" {
		return nil
	}

	if !filepath.IsAbs(keychain) {
		return fmt.Errorf("%w: %s", errKeychainPath, keychain)
	}

	info, err := os.Stat(keychain)
	if err != nil {
		return fmt.Errorf("%w: %w", errKeychainPath, err)
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: %s is not a file", errKeychainPath, keychain)
	}

	return nil
}

// checkReleaseTag fails unless the tag v<version> names HEAD's commit. HEAD may carry
// other tags beside it, as a release does that follows its own release candidate.
func checkReleaseTag(version string) error {
	tag := "v" + version
	tagged := gitOutput("rev-parse", "--verify", "--quiet", "refs/tags/"+tag+"^{commit}")
	head := gitOutput("rev-parse", "--verify", "--quiet", "HEAD^{commit}")

	switch {
	case tagged == "":
		return fmt.Errorf("%w: there is no tag %s (HEAD's tags: %s)", errNoReleaseTag, tag, headTags())
	case tagged != head:
		return fmt.Errorf("%w: check out %s, or tag HEAD (HEAD's tags: %s)", errTagMismatch, tag, headTags())
	}

	return nil
}

// headTags names the tags on HEAD, for an error, or says there are none.
func headTags() string {
	tags := strings.Fields(gitOutput("tag", "--points-at", "HEAD"))
	if len(tags) == 0 {
		return "none"
	}

	return strings.Join(tags, ", ")
}

// maxDirtyNamed is how many changed files errDirtyTree names before it counts the rest.
const maxDirtyNamed = 5

// checkCleanTree fails unless git status finds the working tree exactly as HEAD holds it,
// with no untracked file either (one in a package would be built into the release). Its
// flags beat the user's config (status.showUntrackedFiles, a submodule's ignore setting),
// which could otherwise hide a change. A status git can't read fails too.
func checkCleanTree() error {
	out, err := exec.CommandContext(context.Background(), "git", "status", "--porcelain",
		"--untracked-files=normal", "--ignore-submodules=none").Output()
	if err != nil {
		return fmt.Errorf("read the working tree's changes with git status: %w", err)
	}

	changed := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(changed) == 1 && changed[0] == "" {
		return nil
	}

	named := changed[:min(len(changed), maxDirtyNamed)]
	for i, line := range named {
		named[i] = strings.TrimSpace(line)
	}

	more := ""
	if extra := len(changed) - len(named); extra > 0 {
		more = fmt.Sprintf(", and %d more", extra)
	}

	return fmt.Errorf("%w: %s%s", errDirtyTree, strings.Join(named, "; "), more)
}

// releaseMeta is the version metadata of a release of version: meta, git's, with version
// as the release and its numeric core as the bundle's version, and the release flavor.
func releaseMeta(meta buildMeta, version string) buildMeta {
	meta.release = version
	meta.bundleVersion = numericCore(version)
	meta.flavor = flavorRelease

	return meta
}

// buildRelease builds every file of the release cfg describes into its own fresh folder
// from the unsigned bundle at unsigned, which it leaves as it is: a copy of the bundle,
// signed, then its zip, the disk image, SHA256SUMS, and the manifest, signed with keys.
// Last, it checks them as the app and app:verify will, and reports their sizes.
func buildRelease(cfg packageConfig, keys releaseKeys, unsigned string) error {
	out := filepath.Join(filepath.FromSlash(releaseRoot), cfg.version)
	names := update.AssetNames(cfg.version)
	bundle := filepath.Join(out, bundleName)
	zipPath := filepath.Join(out, names.Zip)
	dmgPath := filepath.Join(out, names.DMG)
	sign := signer{identity: cfg.identity, keychain: cfg.keychain}

	steps := []func() error{
		func() error { return freshFolder(out) },
		func() error { return copyBundle(unsigned, bundle) },
		func() error { return signBundle(bundle, bundleExecutable(bundle), sign) },
		func() error { return verifySignature(bundle, "--deep") },
		func() error { return zipBundle(bundle, zipPath) },
		func() error { return makeDMG(bundle, dmgPath, sign) },
		func() error { return writeSums(filepath.Join(out, names.Sums), zipPath, dmgPath) },
		func() error { return writeManifest(filepath.Join(out, names.Manifest), cfg.version, zipPath, keys) },
		func() error { return verifyRelease(out, cfg.version, keys.public) },
		func() error { return reportRelease(out, names) },
	}

	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}

	return nil
}

// freshFolder empties dir of an earlier run's files, creating it when it is missing. dir is
// always bin/release/<version> or bin/unsigned/<version>, for a version its caller has
// held to update.ValidVersion, so it names no folder outside them.
func freshFolder(dir string) error {
	if err := os.RemoveAll(dir); err != nil { //nolint:gosec // a release folder under bin/, named for a checked version
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
// Info.plist inside it, and nothing beside it (a __MACOSX folder of resource forks, a
// second bundle, a stray file). Every name must be clean (no ./, no .., no //, no
// backslash, not absolute) and appear once, and no entry may be a link: the bundle holds
// none, and a link is the one way out of it a name alone can't show.
func checkZipLayout(zipPath, top string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("read the zip: %w", err)
	}
	defer func() { _ = r.Close() }()

	names, err := zipNames(r.File, top)
	if err != nil {
		return err
	}

	for _, name := range []string{top + "/Contents/Info.plist", top + "/Contents/MacOS/" + brand.Slug} {
		if !names[name] {
			return fmt.Errorf("%w: it lacks %s", errZipLayout, name)
		}
	}

	return nil
}

// zipNames checks every entry as checkZipEntry does, refuses a name that appears twice,
// and returns the names, without a folder's trailing slash.
func zipNames(files []*zip.File, top string) (map[string]bool, error) {
	names := make(map[string]bool, len(files))

	for _, f := range files {
		name, err := checkZipEntry(f, top)
		if err != nil {
			return nil, err
		}

		if names[name] {
			return nil, fmt.Errorf("%w: %q appears twice", errZipLayout, f.Name)
		}

		names[name] = true
	}

	return names, nil
}

// checkZipEntry fails unless f is no link and its name is clean and is top or inside it.
// It returns the name without a folder's trailing slash.
func checkZipEntry(f *zip.File, top string) (string, error) {
	name := strings.TrimSuffix(f.Name, "/")

	switch {
	case f.Mode()&fs.ModeSymlink != 0:
		return "", fmt.Errorf("%w: %q is a link", errZipLayout, f.Name)
	case name == "" || strings.Contains(name, `\`) || path.IsAbs(name) || path.Clean(name) != name:
		return "", fmt.Errorf("%w: %q is not a clean name", errZipLayout, f.Name)
	case name != top && !strings.HasPrefix(name, top+"/"):
		return "", fmt.Errorf("%w: %q is beside %s", errZipLayout, f.Name, top)
	}

	return name, nil
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
// key the app pins, and, when version isn't "", holds it to that version (with or without
// its v).
func verifyPinned(dir, version, releaseKey string) error {
	version = strings.TrimPrefix(version, "v")

	switch {
	case dir == "":
		return errNoReleaseDir
	case releaseKey == "":
		return errNoReleaseKey
	case version != "" && !update.ValidVersion(version):
		return fmt.Errorf("%w: version=%s is not a release version", errOtherVersion, version)
	}

	key, err := update.ParseKey([]byte(releaseKey))
	if err != nil {
		return fmt.Errorf("read the release key the app pins: %w", err)
	}

	return verifyRelease(dir, version, key)
}

// verifyRelease checks the release in dir as the app will: its update manifest, at most
// update.MaxManifestBytes, in form, for the platform releases ship (and for version, when
// it isn't ""), and signed by key's private half; then the zip it names, by its size and
// SHA-512.
func verifyRelease(dir, version string, key ed25519.PublicKey) error {
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

	if version != "" && m.Version != version {
		return fmt.Errorf("%w: %s, not %s", errOtherVersion, m.Version, version)
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
