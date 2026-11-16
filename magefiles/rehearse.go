//go:build mage

package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/brandgen"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/update"
)

const (
	// flavorRehearsal is the flavor stamped into a rehearsal build (main.flavor).
	flavorRehearsal = "rehearsal"

	// rehearsalRoot holds the rehearsal's builds: A's bundle, in A/, and B's release, in
	// feed/, which A reads as its releases.
	rehearsalRoot = "bin/rehearsal"

	// rehearsalFrom is A's version, the build that updates, and rehearsalTo B's, the version
	// it updates to.
	rehearsalFrom = "0.0.1"
	rehearsalTo   = "0.0.2"

	// brokenMark is what case=broken appends to a file B's signature seals.
	brokenMark = "\nchanged by magex app:rehearse case=broken after signing\n"
)

// The rehearsal's cases.
const (
	// caseNormal is an update that installs: B opens in A's place.
	caseNormal = "normal"

	// caseTampered changes one byte of B's zip after its manifest is signed: A must refuse
	// the download and stay as it is.
	caseTampered = "tampered"

	// caseBroken changes a file B's signature seals after B is signed: the update's helper
	// must find B failing its signature check once swapped in, and put A back.
	caseBroken = "broken"
)

// rehearsalCases are the cases app:rehearse builds, in the order its help names them.
func rehearsalCases() []string {
	return []string{caseNormal, caseTampered, caseBroken}
}

var (
	// errRehearsalCase is returned for a case app:rehearse doesn't build.
	errRehearsalCase = errors.New("name the case to rehearse: magex app:rehearse case=normal, case=tampered, or case=broken")

	// errNoIdentity is returned when app:rehearse has no identity to sign with.
	errNoIdentity = errors.New(envCodesignIdentity + " must name the code-signing identity to sign the rehearsal " +
		"with (or - to sign it ad hoc)")

	// errQuotedPath is returned for a repository whose path holds a quote, which the linker
	// flag that stamps the feed folder can't carry.
	errQuotedPath = errors.New("the repository's path holds a quote, which the rehearsal can't stamp into its builds")

	// errStillSigned is returned when case=broken leaves B passing its signature check, so
	// the rehearsal would test nothing.
	errStillSigned = errors.New("the broken bundle still passes its signature check")

	// errStillMatches is returned when case=tampered leaves B's zip matching its manifest,
	// so the rehearsal would test nothing.
	errStillMatches = errors.New("the tampered zip still matches its manifest")
)

// Rehearse builds a pair of rehearsal builds, to try an update end to end on this Mac: A
// (0.0.1) as the app bundle in bin/rehearsal/A/, and B (0.0.2) as a release's zip and its signed
// update manifest in bin/rehearsal/feed/, the folder A reads its releases from. Both are
// the rehearsal flavor, built for this Mac's processor, run on their own data directory
// beside yours, and trust a key pair made for this run alone, whose private half is never
// written anywhere. They are signed with CODESIGN_IDENTITY (- signs ad hoc) and, optionally,
// CODESIGN_KEYCHAIN, which goes on the keychain search list while the target runs, as
// app:package does. case=tampered changes one byte of B's zip after its manifest is
// signed, which A must refuse; case=broken changes a file B's signature seals after B is
// signed, so the update's helper must put A back. It prints the steps to rehearse.
//
//	CODESIGN_IDENTITY=<identity> magex app:rehearse case=normal
func (App) Rehearse() error {
	if runtime.GOOS != goosMacOS {
		return errNeedsMacOS
	}

	root, err := filepath.Abs(filepath.FromSlash(rehearsalRoot))
	if err != nil {
		return fmt.Errorf("find the rehearsal's folder: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("find the home folder: %w", err)
	}

	return rehearse(rehearsalConfig{
		kase:     mageArg("case"),
		identity: os.Getenv(envCodesignIdentity),
		keychain: cleanPath(os.Getenv(envCodesignKeychain)),
		root:     root,
		home:     home,
	})
}

// rehearsalConfig is what rehearse builds a rehearsal from.
type rehearsalConfig struct {
	kase     string // the case: caseNormal, caseTampered, or caseBroken
	identity string // the code-signing identity's name or SHA-1, or adHoc
	keychain string // the keychain that holds it, or "" for the search list
	root     string // the rehearsal's folder, absolute
	home     string // the home folder, for the data directory the steps name
}

// check refuses a rehearsal that can't be built: an unknown case, no identity, a keychain
// that isn't an absolute path to a file, or a folder the linker can't stamp.
func (c rehearsalConfig) check() error {
	switch {
	case !slices.Contains(rehearsalCases(), c.kase):
		return fmt.Errorf("%w (got %q)", errRehearsalCase, c.kase)
	case c.identity == "":
		return errNoIdentity
	case strings.ContainsAny(c.root, `'"`):
		return fmt.Errorf("%w: %s", errQuotedPath, c.root)
	}

	return checkKeychain(c.keychain)
}

// rehearsal's paths: A's bundle, B's while it is built, and the feed with its zip and its
// manifest.
type rehearsalPaths struct {
	a, stage, b, feed, zip, manifest string
}

// paths are the rehearsal's paths under its folder.
func (c rehearsalConfig) paths() rehearsalPaths {
	names := update.AssetNames(rehearsalTo)
	feed := filepath.Join(c.root, "feed")
	stage := filepath.Join(c.root, ".B")

	return rehearsalPaths{
		a:        filepath.Join(c.root, "A", brand.Name+".app"),
		stage:    stage,
		b:        filepath.Join(stage, brand.Name+".app"),
		feed:     feed,
		zip:      filepath.Join(feed, names.Zip),
		manifest: filepath.Join(feed, names.Manifest),
	}
}

// rehearse builds the rehearsal cfg describes, the steps App.Rehearse runs once it knows
// it is on macOS: it checks cfg before any tool runs, makes the key pair, builds the
// frontend once, and builds both versions with the identity's keychain on the search list.
func rehearse(cfg rehearsalConfig) error {
	if err := cfg.check(); err != nil {
		return err
	}

	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("make the rehearsal's key pair: %w", err)
	}

	if err = prepareBuild(); err != nil {
		return err
	}

	keys := releaseKeys{private: private, public: public}

	return withKeychain(cfg.keychain, func() error { return buildRehearsal(cfg, keys) })
}

// rehearsalMeta is the version metadata of a rehearsal build of version: meta, git's,
// with version as the release and the bundle's version, the rehearsal flavor, and the
// feed and the public key it trusts.
func rehearsalMeta(meta buildMeta, version, feed string, key ed25519.PublicKey) buildMeta {
	meta.release = version
	meta.bundleVersion = version
	meta.flavor = flavorRehearsal
	meta.updateFeed = feed
	meta.updateKey = update.EncodeKey(key)

	return meta
}

// buildRehearsal builds A and B into cfg's folder, signed by cfg's identity, B's zip and
// manifest signed with keys, breaks what cfg's case breaks, checks each step, and prints
// the steps to rehearse.
func buildRehearsal(cfg rehearsalConfig, keys releaseKeys) error {
	p := cfg.paths()
	sign := signer{identity: cfg.identity, keychain: cfg.keychain}
	base := readBuildMeta()

	steps := []func() error{
		func() error { return freshFolder(cfg.root) },
		func() error {
			return assembleBundle(bundleSpec{
				bundle: p.a, meta: rehearsalMeta(base, rehearsalFrom, p.feed, keys.public), signer: sign,
			})
		},
		func() error { return verifySignature(p.a, "--deep") },
		func() error {
			return assembleBundle(bundleSpec{
				bundle: p.b, meta: rehearsalMeta(base, rehearsalTo, p.feed, keys.public), signer: sign,
			})
		},
		func() error { return verifySignature(p.b, "--deep") },
		func() error { return breakBundle(cfg.kase, p.b) },
		func() error { return os.MkdirAll(p.feed, 0o755) }, //nolint:gosec // build output, world-readable
		func() error { return zipBundle(p.b, p.zip) },
		func() error { return writeManifest(p.manifest, rehearsalTo, p.zip, keys) },
		func() error { return verifyRelease(p.feed, rehearsalTo, keys.public) },
		func() error { return tamperZip(cfg.kase, p, keys.public) },
		func() error { return os.RemoveAll(p.stage) },
		func() error { return printRehearsal(cfg, p) },
	}

	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}

	return nil
}

// breakBundle, for case=broken, appends brokenMark to the app icon in bundle, a file its
// signature seals, and checks the signature now fails, as the update's helper will find
// once it has swapped the bundle in. Any other case leaves the bundle as it is.
func breakBundle(kase, bundle string) error {
	if kase != caseBroken {
		return nil
	}

	icon := filepath.Join(bundle, "Contents", "Resources", brandgen.IconFile+".icns")

	f, err := os.OpenFile(icon, os.O_WRONLY|os.O_APPEND, 0) //nolint:gosec // the bundle this target built
	if err != nil {
		return fmt.Errorf("break the bundle: %w", err)
	}

	_, err = f.WriteString(brokenMark)
	if err = errors.Join(err, f.Close()); err != nil {
		return fmt.Errorf("break the bundle: %w", err)
	}

	if verifySignature(bundle, "--deep") == nil {
		return errStillSigned
	}

	say("Broke " + bundle + " after signing it, as case=broken means: its signature check fails, as it should.")

	return nil
}

// tamperZip, for case=tampered, changes one byte in the middle of B's zip, after its
// manifest was signed, and checks the zip no longer matches the manifest, as the app will
// find once it downloads it. Any other case leaves the zip as it is.
func tamperZip(kase string, p rehearsalPaths, key ed25519.PublicKey) error {
	if kase != caseTampered {
		return nil
	}

	body, err := os.ReadFile(p.zip)
	if err != nil {
		return fmt.Errorf("tamper with the zip: %w", err)
	}

	if len(body) == 0 {
		return fmt.Errorf("tamper with the zip: %w", errStillMatches)
	}

	body[len(body)/2] ^= 0xFF

	if err = os.WriteFile(p.zip, body, 0o644); err != nil { //nolint:gosec // build output, world-readable
		return fmt.Errorf("tamper with the zip: %w", err)
	}

	if verifyRelease(p.feed, rehearsalTo, key) == nil {
		return errStillMatches
	}

	say("Changed one byte of " + filepath.Base(p.zip) + " after signing its manifest, as case=tampered means: " +
		"it no longer matches, as it should.")

	return nil
}

// printRehearsal prints the rehearsal's builds and the steps to rehearse its case.
func printRehearsal(cfg rehearsalConfig, p rehearsalPaths) error {
	data, err := appdata.RehearsalDir(runtime.GOOS, cfg.home, os.Getenv)
	if err != nil {
		return fmt.Errorf("find the rehearsal's data directory: %w", err)
	}

	installed := "/Applications/" + brand.Name + ".app"
	aside := rehearsalAside()

	lines := []string{
		"",
		"✅ The rehearsal is ready (case=" + cfg.kase + "): A is " + rehearsalFrom + ", B is " + rehearsalTo + ".",
		"  A:    " + p.a,
		"  feed: " + p.feed + " (B's zip and its signed manifest, which A reads as its releases)",
		"  data: " + paths.Tilde(data, cfg.home) + " (both builds run on it, never on your own data)",
		"",
		"To rehearse in place (quit " + brand.Name + " first; a rehearsal build updates itself in " + cfg.root +
			"):",
		"  1. open " + p.a,
		"  2. In the menu bar menu, choose Check for Updates…, then " + rehearsalNext(cfg.kase) + ".",
		"",
		"What should happen: " + rehearsalExpect(cfg.kase),
		"",
		"Or rehearse from the Applications folder, as a release updates (quit " + brand.Name + " first). Step 1 " +
			"moves your copy aside, if there is one; ditto merges into a bundle that is there, so go on only once " +
			"it left none (if " + aside + " is there from an earlier rehearsal, put it back first):",
		"  1. [ ! -e " + installed + " ] || mv " + installed + " " + aside,
		"  2. ditto " + p.a + " " + installed,
		"  3. open " + installed,
		"  4. Do step 2 above.",
		"Then quit it, and put your copy back:",
		"  rm -rf " + installed,
		"  [ ! -e " + aside + " ] || mv " + aside + " " + installed,
	}

	for _, line := range lines {
		say(line)
	}

	return nil
}

// rehearsalAside is where the steps move the user's own copy from the Applications folder
// while the rehearsal runs there: in the home folder, which the next rehearsal's build
// never empties, as it does its own folder.
func rehearsalAside() string {
	return "~/" + brand.Name + "-before-rehearsal.app"
}

// rehearsalNext is what to choose after the check, for kase.
func rehearsalNext(kase string) string {
	if kase == caseTampered {
		return "wait for its status line"
	}

	return "Restart to Update (" + rehearsalTo + ")"
}

// rehearsalExpect is what kase should show.
func rehearsalExpect(kase string) string {
	switch kase {
	case caseTampered:
		return "the menu says Download failed, nothing restarts, and " + rehearsalFrom + " stays."
	case caseBroken:
		return brand.Name + " quits, " + rehearsalFrom + " opens again in a few seconds (the update's helper put it " +
			"back), and its logs folder holds update-" + rehearsalTo + ".log."
	default:
		return brand.Name + " quits, and " + rehearsalTo + " opens in its place in a few seconds, its data backed up " +
			"first beside the database."
	}
}
