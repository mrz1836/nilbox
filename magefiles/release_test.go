//go:build mage

package main

import (
	"archive/zip"
	"bytes"
	"cmp"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/brandgen"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/update"
)

const (
	// testVersion is the version the packaging tests release: a prerelease, so the bundle's
	// version is its numeric core while main.release keeps it whole.
	testVersion = "1.2.3-rc.1"

	// testIdentity is the code-signing identity the packaging tests sign with, by SHA-1.
	testIdentity = "0123456789ABCDEF0123456789ABCDEF01234567"

	// testKeychain is the keychain the search list tests name, with a space, as a path may
	// hold. The packaging tests make a real file of the same name (keychainFile).
	testKeychain = "/keychains/CI Signing.keychain-db"

	// originalKeychains is the keychain search list before a test, as security prints it.
	originalKeychains = "    \"/Library/Keychains/My Keys.keychain-db\"\n    \"/Library/Keychains/System.keychain\"\n"
)

// keychainFile makes an empty file where CODESIGN_KEYCHAIN would name a keychain, named
// as testKeychain is, and returns its path.
func keychainFile(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), filepath.Base(testKeychain))
	testutil.WriteFile(t, path, "")

	return path
}

// releaseKeyPair returns the throwaway key pair seeded by seed, the same on every run.
func releaseKeyPair(seed byte) releaseKeys {
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, ed25519.SeedSize))

	return releaseKeys{private: private, public: update.PublicKeyOf(private)}
}

// writeSigningKey writes keys' private half where UPDATE_SIGNING_KEY would name it, and
// returns the path.
func writeSigningKey(t *testing.T, keys releaseKeys) string {
	t.Helper()

	body, err := update.EncodePrivateKey(keys.private)
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), signingKeyFile)
	testutil.WriteFileMode(t, path, string(body), 0o600)

	return path
}

// zipEntry is one entry of a zip a test builds: a file with its body, a folder (a name
// ending in /), or a link to target.
type zipEntry struct {
	name   string
	body   string
	target string
}

// writeZip writes a zip of entries to path.
func writeZip(t *testing.T, path string, entries ...zipEntry) {
	t.Helper()

	var buf bytes.Buffer

	w := zip.NewWriter(&buf)

	for _, e := range entries {
		header := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		body := e.body

		switch {
		case e.target != "":
			body = e.target

			header.SetMode(fs.ModeSymlink | 0o755)
		case strings.HasSuffix(e.name, "/"):
			header.SetMode(fs.ModeDir | 0o755)
		default:
			header.SetMode(0o644)
		}

		f, err := w.CreateHeader(header)
		require.NoError(t, err)

		_, err = f.Write([]byte(body))
		require.NoError(t, err)
	}

	require.NoError(t, w.Close())
	testutil.WriteFile(t, path, buf.String())
}

// bundleEntries are a zipped bundle's entries, as ditto writes them, folders included.
func bundleEntries() []zipEntry {
	return []zipEntry{
		{name: bundleName + "/"},
		{name: bundleName + "/Contents/"},
		{name: bundleName + "/Contents/Info.plist", body: "<plist/>"},
		{name: bundleName + "/Contents/MacOS/"},
		{name: bundleName + "/Contents/MacOS/" + brand.Slug, body: "binary"},
		{name: bundleName + "/Contents/Resources/icons.icns", body: "icns"},
	}
}

// releaseFake is what the fake tools of a release say of the checkout and what they make.
type releaseFake struct {
	tag    string     // the tag that names HEAD's commit, or "" for none
	at     string     // the commit the tag names, when not HEAD's
	dirty  string     // what git status prints
	archs  string     // what lipo reads in a binary, when not both release architectures
	zipped []zipEntry // what ditto zips, when not the bundle
}

// releaseState is what the fake tools of a release keep: the keychain search list, and
// the mode of the folder the disk image was made from.
type releaseState struct {
	keychains *keychainList

	mu      sync.Mutex
	dmgRoot os.FileMode
}

// dmgRootMode returns the mode of the folder hdiutil made the disk image from.
func (s *releaseState) dmgRootMode() os.FileMode {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.dmgRoot
}

// codesignAnswer is a fake codesign's answer, which fails a call fails reports, and
// otherwise seals a bundle it signs in Contents/_CodeSignature, as the real one does.
func codesignAnswer(t *testing.T, fails func(args []string) bool) answer {
	t.Helper()

	return func(args []string) testutil.FakeRun {
		if fails != nil && fails(args) {
			return testutil.FakeRun{Exit: 1}
		}

		last := lastArg(args)
		if args[0] != "--force" || !strings.HasSuffix(last, ".app") {
			return testutil.FakeRun{}
		}

		seal := filepath.Join(last, "Contents", "_CodeSignature")
		mkdir(t, seal)

		return writes(filepath.Join(seal, "CodeResources"), "seal")
	}
}

// signs reports whether codesign's arguments sign rather than check a signature, and,
// with suffix, sign a path that ends with it.
func signs(suffix string) func(args []string) bool {
	return func(args []string) bool { return args[0] == "--force" && strings.HasSuffix(lastArg(args), suffix) }
}

// releaseTools makes a fake of every tool app:package runs on the rig, and returns what
// they keep: a git whose tag (when not "") names HEAD's commit, or f.at's, and whose
// status prints f.dirty; a go that writes a binary after -o and reports its GOARCH; a lipo
// that writes the universal binary and reads x86_64 and arm64 in it (or f.archs); a ditto
// that zips by writing a zip of a bundle (or of f.zipped) and copies a folder by making
// it; an hdiutil that writes the disk image, noting its folder's mode; a security that
// keeps the keychain search list; a codesign that seals a bundle it signs; and pnpm and
// plutil.
func releaseTools(g *rig, f releaseFake) *releaseState {
	state := &releaseState{keychains: newKeychainList()}

	for _, name := range []string{"pnpm", "plutil"} {
		g.fixed(name, testutil.FakeRun{})
	}

	g.tool("codesign", codesignAnswer(g.t, nil))
	g.tool("security", state.keychains.answer)
	g.tool("git", f.git)
	g.tool("go", releaseGo)
	g.tool("lipo", f.lipo)
	g.tool("ditto", f.ditto(g.t))
	g.tool("hdiutil", state.hdiutil)

	return state
}

// git is the answer of a release's fake git: HEAD's commit, the tag's, HEAD's tags, and
// the working tree's changes, as f says they are.
func (f releaseFake) git(args []string) testutil.FakeRun {
	switch strings.Join(args, " ") {
	case "rev-parse --verify --quiet HEAD^{commit}":
		return testutil.FakeRun{Stdout: "headcommit\n"}
	case "tag --points-at HEAD":
		if f.at != "" {
			return testutil.FakeRun{}
		}

		return testutil.FakeRun{Stdout: f.tag + "\n"}
	case "status --porcelain --untracked-files=normal --ignore-submodules=none":
		return testutil.FakeRun{Stdout: f.dirty}
	}

	tagged, ok := strings.CutPrefix(strings.Join(args, " "), "rev-parse --verify --quiet refs/tags/")

	switch {
	case !ok:
		return testutil.FakeRun{Exit: 128}
	case f.tag != "" && tagged == f.tag+"^{commit}":
		return testutil.FakeRun{Stdout: cmp.Or(f.at, "headcommit") + "\n"}
	default:
		return testutil.FakeRun{Exit: 1}
	}
}

// releaseGo is the answer of a release's fake go: it writes a binary after -o, and
// reports the GOARCH it builds for.
func releaseGo(args []string) testutil.FakeRun {
	run := testutil.FakeRun{ReportEnv: []string{"GOARCH"}}
	if out := argAfter(args, "-o"); out != "" {
		run.Files = []testutil.FakeFile{{Path: out, Content: "binary"}}
	}

	return run
}

// lipo is the answer of a release's fake lipo: it writes the universal binary, and reads
// x86_64 and arm64 in a binary, or f.archs.
func (f releaseFake) lipo(args []string) testutil.FakeRun {
	switch args[0] {
	case "-create":
		return writes(args[2], "universal")
	case "-archs":
		return testutil.FakeRun{Stdout: cmp.Or(f.archs, "x86_64 arm64") + "\n"}
	default:
		return testutil.FakeRun{}
	}
}

// ditto returns the answer of a release's fake ditto: it zips by writing a zip of a
// bundle, or of f.zipped, and copies a folder by making it.
func (f releaseFake) ditto(t *testing.T) answer {
	t.Helper()

	zipped := f.zipped
	if zipped == nil {
		zipped = bundleEntries()
	}

	return func(args []string) testutil.FakeRun {
		if args[0] != "-c" {
			mkdir(t, lastArg(args))

			return testutil.FakeRun{}
		}

		made := filepath.Join(t.TempDir(), "bundle.zip")
		writeZip(t, made, zipped...)

		return testutil.FakeRun{Files: []testutil.FakeFile{{Path: lastArg(args), From: made}}}
	}
}

// hdiutil is the answer of a release's fake hdiutil: it writes the disk image, and notes
// the mode of the folder it is made from.
func (s *releaseState) hdiutil(args []string) testutil.FakeRun {
	if info, err := os.Stat(argAfter(args, "-srcfolder")); err == nil {
		s.mu.Lock()
		s.dmgRoot = info.Mode().Perm()
		s.mu.Unlock()
	}

	return writes(lastArg(args), "disk image")
}

// toolLines returns the lines of the rig's tool calls, but git's, which read the version.
func toolLines(g *rig) []string {
	return slices.DeleteFunc(g.lines(), func(line string) bool { return strings.HasPrefix(line, "git ") })
}

// requireLines requires lines to match patterns, one regular expression each, in order.
func requireLines(t *testing.T, patterns, lines []string) {
	t.Helper()

	require.Len(t, lines, len(patterns), strings.Join(lines, "\n"))

	for i, pattern := range patterns {
		assert.Regexp(t, "^"+pattern+"$", lines[i], "line %d", i+1)
	}
}

// testConfig is the packaging config the tests release with: testVersion, signed as
// testIdentity found in keychain, with keys' private half, verified against its public
// half.
func testConfig(t *testing.T, keys releaseKeys, keychain string) packageConfig {
	t.Helper()

	return packageConfig{
		version:    testVersion,
		identity:   testIdentity,
		keychain:   keychain,
		signingKey: writeSigningKey(t, keys),
		releaseKey: update.EncodeKey(keys.public),
	}
}

// unsignedBundle is where app:buildRelease builds the unsigned bundle of testVersion in
// the checkout at dir.
func unsignedBundle(dir string) string {
	return filepath.Join(dir, unsignedRoot, testVersion, bundleName)
}

// releaseBuildLines are the tool lines building testVersion's unsigned bundle in the
// checkout at dir logs, as patterns: the frontend, both slices, lipo, and the Info.plist,
// into bin/unsigned. No security and no codesign line is among them.
func releaseBuildLines(dir string) []string {
	q := regexp.QuoteMeta
	exe := bundleExecutable(unsignedBundle(dir))
	plist := filepath.Join(unsignedBundle(dir), "Contents", "Info.plist")

	goBuild := func(arch string) string {
		return q("go build -tags desktop,production -trimpath -buildvcs=false -ldflags -s -w -X main.version=") +
			`\S+` + q(" -X main.commit=") + `\S+` + q(" -X main.buildDate=") + `\S+` +
			q(" -X main.release="+testVersion+" -X main.buildNumber=") + `\S+` + q(" -X main.flavor=release -o ") +
			q(filepath.Join(dir, unsignedRoot, testVersion)+"/.slices-") + `\S+` + q("/"+brand.Slug+"-"+arch+" ./cmd/"+brand.Slug)
	}

	return []string{
		q("pnpm install --frozen-lockfile"),
		q("pnpm build"),
		goBuild("arm64"),
		goBuild("amd64"),
		q("lipo -create -output "+exe+" ") + `\S+` + q("/"+brand.Slug+"-arm64 ") + `\S+` + q("/"+brand.Slug+"-amd64"),
		q("lipo -archs " + exe),
		// The versions alone: a release keeps the generated Info.plist's name and bundle ID,
		// brand.Name and brand.BundleID, never a build from source's (devStamps).
		q("plutil -replace CFBundleShortVersionString -string 1.2.3 " + plist),
		q("plutil -replace CFBundleVersion -string " + brandgen.PlaceholderVersion + " " + plist),
	}
}

// releaseSignLines are the tool lines signing the unsigned bundle at unsigned and
// packaging testVersion in the checkout at dir log, as patterns: the bundle's
// architectures read, then, with keychain first on the search list (none when keychain is
// ""), the copy in bin/release signed and verified, the zip, the disk image, and its
// signature, and the list put back.
func releaseSignLines(dir, unsigned, keychain string) []string {
	q := regexp.QuoteMeta
	out := filepath.Join(dir, releaseRoot, testVersion)
	bundle := filepath.Join(out, bundleName)
	names := update.AssetNames(testVersion)
	keychains := q("/Library/Keychains/My Keys.keychain-db /Library/Keychains/System.keychain")

	sign := "codesign --force --sign " + testIdentity + " "
	if keychain != "" {
		sign += "--keychain " + keychain + " "
	}

	sign += "--timestamp=none "

	lines := []string{q("lipo -archs " + bundleExecutable(unsigned))}

	if keychain != "" {
		lines = append(lines,
			q("security list-keychains -d user"),
			q("security list-keychains -d user -s "+keychain+" ")+keychains)
	}

	lines = append(lines,
		q(sign+bundle),
		q("codesign --verify --deep --strict "+bundle),
		q("ditto -c -k --norsrc --keepParent "+bundle+" "+filepath.Join(out, names.Zip)),
		q("ditto "+bundle+" "+out+"/.dmg-")+`\S+`+q("/"+bundleName),
		q("hdiutil create -volname "+brand.Name+" -srcfolder "+out+"/.dmg-")+`\S+`+
			q(" -ov -format ULMO "+filepath.Join(out, names.DMG)),
		q(sign+filepath.Join(out, names.DMG)),
		q("codesign --verify --strict "+filepath.Join(out, names.DMG)))

	if keychain != "" {
		lines = append(lines, q("security list-keychains -d user -s ")+keychains)
	}

	return lines
}

// releaseRig returns a rig whose checkout holds what the app targets need, with the fake
// tools of a release (releaseTools), and what they keep.
func releaseRig(t *testing.T, f releaseFake) (*rig, *releaseState) {
	t.Helper()

	g := newRig(t)
	checkout(t, g.dir)

	return g, releaseTools(g, f)
}

func TestPackageRelease(t *testing.T) {
	t.Parallel()

	keys := releaseKeyPair(1)
	names := update.AssetNames(testVersion)

	t.Run("builds, signs, packages, and verifies a universal release", func(t *testing.T) {
		t.Parallel()

		g, state := releaseRig(t, releaseFake{tag: "v" + testVersion})
		keychain := keychainFile(t)
		out := g.path(releaseRoot + "/" + testVersion)
		r := g.runner()

		require.NoError(t, r.packageRelease(testConfig(t, keys, keychain)))

		// The keychain goes on the search list only once the bundle is built: no build tool
		// runs while it is there.
		requireLines(t, append(releaseBuildLines(g.dir), releaseSignLines(g.dir, unsignedBundle(g.dir), keychain)...), toolLines(g))

		assert.Equal(t, []string{
			"git rev-parse --verify --quiet refs/tags/v" + testVersion + "^{commit}",
			"git rev-parse --verify --quiet HEAD^{commit}",
			"git status --porcelain --untracked-files=normal --ignore-submodules=none",
		}, g.lines()[:3], "the tag and the working tree are checked before anything builds")
		assert.NotContains(t, strings.Join(g.lines(), "\n"), "main.updateFeed", "a release reads the feed GitHub serves")
		assert.NotContains(t, strings.Join(g.lines(), "\n"), "main.updateKey", "a release verifies with the key it pins")
		assert.Equal(t, originalKeychains, state.keychains.printed(), "the search list is back as it was")
		assert.Equal(t, os.FileMode(0o755), state.dmgRootMode(), "the disk image's root is world-readable")

		entries, err := os.ReadDir(out)
		require.NoError(t, err)

		files := make([]string, 0, len(entries))
		for _, e := range entries {
			files = append(files, e.Name())
		}

		assert.ElementsMatch(t, []string{bundleName, names.Zip, names.DMG, names.Sums, names.Manifest}, files,
			"the disk image's folder is gone")
		assert.Equal(t, "universal", readFile(t, bundleExecutable(filepath.Join(out, bundleName))))

		require.NoError(t, r.verifyRelease(out, testVersion, keys.public))

		stdout := g.out.String()
		assert.Contains(t, stdout, "GOARCH=arm64\nBuilt the arm64 slice (6 bytes", "each slice builds for its own processor")
		assert.Contains(t, stdout, "GOARCH=amd64\nBuilt the amd64 slice (6 bytes")
		assert.Contains(t, stdout, "✅ "+names.Zip+" matches its signed manifest ("+testVersion+", darwin-universal)")
		assert.Contains(t, stdout, "  "+names.DMG+" (10 bytes, 0.0 MB)")

		sums := readFile(t, filepath.Join(out, names.Sums))
		assert.Equal(t, sha256Hex(t, filepath.Join(out, names.DMG))+"  "+names.DMG+"\n"+
			sha256Hex(t, filepath.Join(out, names.Zip))+"  "+names.Zip+"\n", sums)
	})

	t.Run("signs from the search list when no keychain is named", func(t *testing.T) {
		t.Parallel()

		g, _ := releaseRig(t, releaseFake{tag: "v" + testVersion})

		require.NoError(t, g.runner().packageRelease(testConfig(t, keys, "")))

		assert.Empty(t, g.linesOf("security"), "the search list is left alone")
		assert.Contains(t, toolLines(g), "codesign --force --sign "+testIdentity+" --timestamp=none "+
			filepath.Join(g.path(releaseRoot), testVersion, bundleName))
	})

	t.Run("a step that fails still puts the search list back", func(t *testing.T) {
		t.Parallel()

		g, state := releaseRig(t, releaseFake{tag: "v" + testVersion})
		g.tool("hdiutil", exits(1))

		err := g.runner().packageRelease(testConfig(t, keys, keychainFile(t)))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "make the disk image")

		lines := toolLines(g)
		assert.Equal(t, "security list-keychains -d user -s /Library/Keychains/My Keys.keychain-db "+
			"/Library/Keychains/System.keychain", lines[len(lines)-1])
		assert.Equal(t, originalKeychains, state.keychains.printed())
	})

	t.Run("a binary without both architectures fails", func(t *testing.T) {
		t.Parallel()

		g, _ := releaseRig(t, releaseFake{tag: "v" + testVersion, archs: "arm64"})

		err := g.runner().packageRelease(testConfig(t, keys, ""))

		require.ErrorIs(t, err, errArches)
		assert.Contains(t, err.Error(), `lipo -archs reads "arm64", not "arm64 x86_64"`)
	})
}

func TestPackageReleaseStopsAtAFailingStep(t *testing.T) {
	t.Parallel()

	keys := releaseKeyPair(1)

	tests := []struct {
		name    string
		tool    string                    // the tool to replace, or ""
		answer  func(t *testing.T) answer // the replacement's answer
		zipped  []zipEntry                // what ditto zips, when not the bundle
		wantErr string                    // a part of the error, {bundle} standing for the release's bundle
	}{
		{name: "the first slice fails to build", tool: "go", answer: func(*testing.T) answer { return exits(1) }, wantErr: "build the desktop binary"},
		{name: "lipo can't combine the slices", tool: "lipo", answer: func(*testing.T) answer { return exits(1) }, wantErr: "combine the slices with lipo"},
		{name: "lipo can't read the binary", tool: "lipo", answer: func(*testing.T) answer {
			return func(args []string) testutil.FakeRun {
				if args[0] == "-archs" {
					return testutil.FakeRun{Exit: 1}
				}

				return writes(args[2], "universal")
			}
		}, wantErr: "read the binary's architectures"},
		{name: "signing fails", tool: "codesign", answer: func(t *testing.T) answer {
			return codesignAnswer(t, signs(""))
		}, wantErr: "sign the bundle as " + testIdentity},
		{name: "the signature doesn't verify", tool: "codesign", answer: func(t *testing.T) answer {
			return codesignAnswer(t, func(args []string) bool { return args[0] == "--verify" })
		}, wantErr: "verify the signature of {bundle}"},
		{name: "signing the disk image fails", tool: "codesign", answer: func(t *testing.T) answer {
			return codesignAnswer(t, signs(".dmg"))
		}, wantErr: "sign the disk image as " + testIdentity},
		{name: "zipping fails", tool: "ditto", answer: func(*testing.T) answer { return exits(1) }, wantErr: "zip the bundle"},
		{name: "the zip holds a stray file", zipped: append(bundleEntries(), zipEntry{name: "README", body: "stray"}), wantErr: "is beside"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g, _ := releaseRig(t, releaseFake{tag: "v" + testVersion, zipped: tt.zipped})

			if tt.tool != "" {
				g.tool(tt.tool, tt.answer(t))
			}

			err := g.runner().packageRelease(testConfig(t, keys, ""))

			require.Error(t, err)
			assert.Contains(t, err.Error(), strings.ReplaceAll(tt.wantErr, "{bundle}",
				filepath.Join(g.path(releaseRoot), testVersion, bundleName)))
		})
	}

	t.Run("copying the bundle for the disk image fails", func(t *testing.T) {
		t.Parallel()

		g, _ := releaseRig(t, releaseFake{tag: "v" + testVersion})
		zipper := g.answers["ditto"]
		g.tool("ditto", func(args []string) testutil.FakeRun {
			if args[0] == "-c" {
				return zipper(args)
			}

			return testutil.FakeRun{Exit: 1}
		})

		err := g.runner().packageRelease(testConfig(t, keys, ""))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "copy the bundle for the disk image")
	})
}

//nolint:paralleltest // sets the process's umask, which every file the process makes is under
func TestReleaseModesIgnoreTheUmask(t *testing.T) {
	keys := releaseKeyPair(1)

	original := syscall.Umask(0o077)

	t.Cleanup(func() { syscall.Umask(original) })

	t.Run("the packaged bundle", func(t *testing.T) {
		g, _ := releaseRig(t, releaseFake{tag: "v" + testVersion})
		bundle := filepath.Join(g.path(releaseRoot), testVersion, bundleName)

		require.NoError(t, g.runner().packageRelease(testConfig(t, keys, "")))

		modes := map[string]os.FileMode{}

		require.NoError(t, filepath.WalkDir(bundle, func(path string, d fs.DirEntry, err error) error {
			require.NoError(t, err)

			info, err := d.Info()
			require.NoError(t, err)

			rel, err := filepath.Rel(bundle, path)
			require.NoError(t, err)

			modes[rel] = info.Mode().Perm()

			return nil
		}))

		assert.Equal(t, map[string]os.FileMode{
			".":                                     0o755,
			"Contents":                              0o755,
			"Contents/Info.plist":                   0o644,
			"Contents/_CodeSignature":               0o755,
			"Contents/_CodeSignature/CodeResources": 0o644,
			"Contents/MacOS":                        0o755,
			"Contents/MacOS/" + brand.Slug:          0o755,
			"Contents/Resources":                    0o755,
			"Contents/Resources/" + brandgen.IconFile + ".icns": 0o644,
		}, modes, "a umask of 077 would leave 0700 and 0600 in the zip and the disk image")
	})

	t.Run("the unsigned bundle", func(t *testing.T) {
		g, _ := releaseRig(t, releaseFake{tag: "v" + testVersion})

		bundle, err := g.runner().buildReleaseBundle(testVersion, update.EncodeKey(keys.public))
		require.NoError(t, err)

		tree := treeOf(t, bundle)
		assert.Equal(t, os.FileMode(0o755), tree[filepath.Join("Contents", "MacOS", brand.Slug)].Perm())
		assert.Equal(t, os.FileMode(0o644), tree[filepath.Join("Contents", "Info.plist")].Perm(),
			"the modes don't depend on the umask")
	})
}

// sha256Hex returns the SHA-256 of the file at path, as hex.
func sha256Hex(t *testing.T, path string) string {
	t.Helper()

	sum := sha256.Sum256([]byte(readFile(t, path)))

	return hex.EncodeToString(sum[:])
}

func TestPackageReleaseRefusesBeforeAnyToolRuns(t *testing.T) {
	t.Parallel()

	keys := releaseKeyPair(1)
	other := releaseKeyPair(2)

	tests := []struct {
		name    string
		tag     string
		at      string // the commit the tag names, when not HEAD's
		dirty   string // what git status prints
		change  func(t *testing.T, cfg *packageConfig)
		wantErr error  // the error it fails with, or nil to match wantMsg alone
		wantMsg string // a part of the error's text
	}{
		{name: "no release key pinned", tag: "v" + testVersion, change: func(_ *testing.T, cfg *packageConfig) {
			cfg.releaseKey = ""
		}, wantErr: errNoReleaseKey},
		{name: "a version with its v", tag: "v" + testVersion, change: func(_ *testing.T, cfg *packageConfig) {
			cfg.version = "v" + testVersion
		}, wantErr: errBadVersion},
		{name: "ad hoc", tag: "v" + testVersion, change: func(_ *testing.T, cfg *packageConfig) {
			cfg.identity = "-"
		}, wantErr: errAdHoc},
		{name: "no identity", tag: "v" + testVersion, change: func(_ *testing.T, cfg *packageConfig) {
			cfg.identity = ""
		}, wantErr: errAdHoc},
		{name: "a missing key file", tag: "v" + testVersion, change: func(t *testing.T, cfg *packageConfig) {
			cfg.signingKey = filepath.Join(t.TempDir(), "missing.key")
		}, wantErr: fs.ErrNotExist},
		{name: "a key file that holds no key", tag: "v" + testVersion, change: func(t *testing.T, cfg *packageConfig) {
			cfg.signingKey = filepath.Join(t.TempDir(), signingKeyFile)
			testutil.WriteFile(t, cfg.signingKey, "not a key")
		}, wantErr: update.ErrKey},
		{name: "a malformed pinned key", tag: "v" + testVersion, change: func(_ *testing.T, cfg *packageConfig) {
			cfg.releaseKey = "not a key"
		}, wantErr: update.ErrKey},
		{name: "a signing key the app doesn't pin", tag: "v" + testVersion, change: func(t *testing.T, cfg *packageConfig) {
			cfg.signingKey = writeSigningKey(t, other)
		}, wantErr: errKeyMismatch},
		{name: "a relative keychain", tag: "v" + testVersion, change: func(_ *testing.T, cfg *packageConfig) {
			cfg.keychain = "build.keychain-db"
		}, wantErr: errKeychainPath},
		{name: "a missing keychain", tag: "v" + testVersion, change: func(t *testing.T, cfg *packageConfig) {
			cfg.keychain = filepath.Join(t.TempDir(), "missing.keychain-db")
		}, wantErr: fs.ErrNotExist},
		{name: "a keychain that is a folder", tag: "v" + testVersion, change: func(t *testing.T, cfg *packageConfig) {
			cfg.keychain = t.TempDir()
		}, wantErr: errKeychainPath, wantMsg: "is not a file"},
		{name: "no tag on HEAD", wantErr: errNoReleaseTag, wantMsg: "HEAD's tags: none"},
		{name: "the tag of another version", tag: "v1.2.4", wantErr: errNoReleaseTag, wantMsg: "HEAD's tags: v1.2.4"},
		{
			name: "the version's tag on another commit", tag: "v" + testVersion, at: "othercommit", wantErr: errTagMismatch,
			wantMsg: "check out v" + testVersion,
		},
		{
			name: "changes in the working tree", tag: "v" + testVersion, dirty: " M main.go\n?? notes.go\n", wantErr: errDirtyTree,
			wantMsg: "M main.go; ?? notes.go",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g, _ := releaseRig(t, releaseFake{tag: tt.tag, at: tt.at, dirty: tt.dirty})

			cfg := testConfig(t, keys, keychainFile(t))
			if tt.change != nil {
				tt.change(t, &cfg)
			}

			err := g.runner().packageRelease(cfg)

			require.ErrorIs(t, err, tt.wantErr)
			assert.Contains(t, err.Error(), tt.wantMsg)
			assert.Empty(t, toolLines(g), "nothing builds, and the search list is left alone")
			assert.NoDirExists(t, g.path("bin/release"))
		})
	}
}

func TestPackageConfigFromEnv(t *testing.T) {
	t.Parallel()

	setAll := func(g *rig) {
		g.setenv(envReleaseVersion, "0.3.0")
		g.setenv(envCodesignIdentity, "Release Signing")
		g.setenv(envCodesignKeychain, "/keychains/ci.keychain-db")
		g.setenv(envUpdateSigningKey, "/keys/release.key")
	}

	t.Run("reads every variable", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		setAll(g)

		cfg, err := g.runner().packageConfigFromEnv("pinned")

		require.NoError(t, err)
		assert.Equal(t, packageConfig{
			version: "0.3.0", identity: "Release Signing", keychain: "/keychains/ci.keychain-db",
			signingKey: "/keys/release.key", releaseKey: "pinned",
		}, cfg)
	})

	t.Run("the keychain is optional", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		setAll(g)
		g.setenv(envCodesignKeychain, "")

		cfg, err := g.runner().packageConfigFromEnv("pinned")

		require.NoError(t, err)
		assert.Empty(t, cfg.keychain)
	})

	t.Run("names every missing variable", func(t *testing.T) {
		t.Parallel()

		_, err := newRig(t).runner().packageConfigFromEnv("pinned")

		require.ErrorIs(t, err, errMissingEnv)
		assert.Contains(t, err.Error(), "RELEASE_VERSION, CODESIGN_IDENTITY, UPDATE_SIGNING_KEY")
	})

	t.Run("refuses while no release key is pinned", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		setAll(g)

		_, err := g.runner().packageConfigFromEnv("")

		require.ErrorIs(t, err, errNoReleaseKey)
	})
}

func TestCheckReleaseTag(t *testing.T) {
	t.Parallel()

	// repo makes a repository of one empty commit, and returns a runner of the real git in
	// it, the repository, and git's environment.
	repo := func(t *testing.T) (runner, string, []string) {
		t.Helper()

		dir, env := gitRepo(t)
		r, _ := realRunner(t, dir, env)

		return r, dir, env
	}

	t.Run("the version's tag on HEAD", func(t *testing.T) {
		t.Parallel()

		r, dir, env := repo(t)
		gitIn(t, dir, env, "tag", "v0.3.0")

		require.NoError(t, r.checkReleaseTag("0.3.0"))
		require.ErrorIs(t, r.checkReleaseTag("0.3.1"), errNoReleaseTag)
	})

	t.Run("HEAD carries the release candidate's tag and the release's", func(t *testing.T) {
		t.Parallel()

		r, dir, env := repo(t)
		gitIn(t, dir, env, "tag", "v0.2.0-rc.1")
		gitIn(t, dir, env, "tag", "-a", "-m", "release", "v0.2.0")

		require.NoError(t, r.checkReleaseTag("0.2.0-rc.1"), "the candidate's run again finds its tag")
		require.NoError(t, r.checkReleaseTag("0.2.0"), "an annotated tag names its commit")

		err := r.checkReleaseTag("0.2.1")
		require.ErrorIs(t, err, errNoReleaseTag)
		assert.Contains(t, err.Error(), "HEAD's tags: v0.2.0, v0.2.0-rc.1")
	})

	t.Run("a tag on an earlier commit", func(t *testing.T) {
		t.Parallel()

		r, dir, env := repo(t)
		gitIn(t, dir, env, "tag", "v0.3.0")
		gitIn(t, dir, env, "commit", "-q", "--allow-empty", "-m", "second")

		err := r.checkReleaseTag("0.3.0")
		require.ErrorIs(t, err, errTagMismatch)
		assert.Contains(t, err.Error(), "HEAD's tags: none")
	})

	t.Run("a branch named as the tag is no tag", func(t *testing.T) {
		t.Parallel()

		r, dir, env := repo(t)
		gitIn(t, dir, env, "branch", "v0.3.0")

		require.ErrorIs(t, r.checkReleaseTag("0.3.0"), errNoReleaseTag)
	})
}

func TestCheckCleanTree(t *testing.T) {
	t.Parallel()

	t.Run("a clean tree", func(t *testing.T) {
		t.Parallel()

		dir, env := t.TempDir(), testutil.Git(t)
		gitIn(t, dir, env, "init", "-q")
		testutil.WriteFile(t, filepath.Join(dir, "main.go"), "package main\n")
		gitIn(t, dir, env, "add", "main.go")
		gitIn(t, dir, env, "commit", "-q", "-m", "first")
		testutil.WriteFile(t, filepath.Join(dir, ".gitignore"), "bin/\n")
		gitIn(t, dir, env, "add", ".gitignore")
		gitIn(t, dir, env, "commit", "-q", "-m", "ignore")
		testutil.WriteFile(t, filepath.Join(dir, "bin", "built"), "ignored")

		r, _ := realRunner(t, dir, env)

		require.NoError(t, r.checkCleanTree(), "an ignored file is no change")

		testutil.WriteFile(t, filepath.Join(dir, "main.go"), "package main // changed\n")

		err := r.checkCleanTree()
		require.ErrorIs(t, err, errDirtyTree)
		assert.Contains(t, err.Error(), "M main.go")

		gitIn(t, dir, env, "checkout", "--", "main.go")
		testutil.WriteFile(t, filepath.Join(dir, "extra.go"), "package main\n")
		require.ErrorIs(t, r.checkCleanTree(), errDirtyTree, "an untracked file would be built in")
	})

	t.Run("a git config that hides untracked files hides none from the check", func(t *testing.T) {
		t.Parallel()

		global := filepath.Join(t.TempDir(), "gitconfig")
		testutil.WriteFile(t, global, "[status]\n\tshowUntrackedFiles = no\n[diff]\n\tignoreSubmodules = all\n")

		dir, env := t.TempDir(), append(testutil.Git(t), "GIT_CONFIG_GLOBAL="+global)
		gitIn(t, dir, env, "init", "-q")
		gitIn(t, dir, env, "commit", "-q", "--allow-empty", "-m", "first")
		testutil.WriteFile(t, filepath.Join(dir, "extra.go"), "package main\n")

		require.Empty(t, gitIn(t, dir, env, "status", "--porcelain"), "the user's config hides the untracked file from a plain status")

		r, _ := realRunner(t, dir, env)

		err := r.checkCleanTree()
		require.ErrorIs(t, err, errDirtyTree)
		assert.Contains(t, err.Error(), "?? extra.go")

		gitIn(t, dir, env, "config", "status.showUntrackedFiles", "no")
		require.ErrorIs(t, r.checkCleanTree(), errDirtyTree, "the clone's own config hides nothing either")
	})

	t.Run("many changes are counted", func(t *testing.T) {
		t.Parallel()

		dir, env := gitRepo(t)

		for i := range 8 {
			testutil.WriteFile(t, filepath.Join(dir, "f"+strconv.Itoa(i)), "x")
		}

		r, _ := realRunner(t, dir, env)

		err := r.checkCleanTree()
		require.ErrorIs(t, err, errDirtyTree)
		assert.Contains(t, err.Error(), ", and 3 more")
	})

	t.Run("outside a repository", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		r, _ := realRunner(t, dir, append(testutil.Git(t), "GIT_CEILING_DIRECTORIES="+filepath.Dir(dir)))

		err := r.checkCleanTree()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "read the working tree's changes")
	})
}

func TestReleaseMeta(t *testing.T) {
	t.Parallel()

	meta := releaseMeta(buildMeta{version: "v0.2.0-rc.1", commit: "abc1234", bundleBuild: "42"}, "0.2.0-rc.1")

	assert.Equal(t, "v0.2.0-rc.1", meta.version, "git's metadata is kept")
	assert.Equal(t, "42", meta.bundleBuild)

	assert.Equal(t, "0.2.0-rc.1", meta.release)
	assert.Equal(t, "0.2.0", meta.bundleVersion)
	assert.Equal(t, flavorRelease, meta.flavor)
}

func TestNumericCore(t *testing.T) {
	t.Parallel()

	for version, want := range map[string]string{
		"0.3.0":         "0.3.0",
		"0.2.0-rc.1":    "0.2.0",
		"1.0.0+build.7": "1.0.0",
		"1.0.0-a+b":     "1.0.0",
	} {
		assert.Equal(t, want, numericCore(version), version)
	}
}

func TestCheckZipLayout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		entries []zipEntry
		wantErr string // "" when the zip passes
	}{
		{name: "one top-level bundle", entries: bundleEntries()},
		{name: "one bundle without folder entries", entries: []zipEntry{
			{name: bundleName + "/Contents/Info.plist"}, {name: bundleName + "/Contents/MacOS/" + brand.Slug},
		}},
		{name: "resource forks beside it", entries: append(bundleEntries(),
			zipEntry{name: "__MACOSX/"}, zipEntry{name: "__MACOSX/" + bundleName + "/._Contents", body: "fork"}), wantErr: "__MACOSX"},
		{name: "two bundles", entries: append(bundleEntries(), zipEntry{name: "Other.app/"}), wantErr: "Other.app"},
		{name: "a stray file", entries: append(bundleEntries(), zipEntry{name: "README", body: "stray"}), wantErr: "README"},
		{name: "a name that climbs out", entries: append(bundleEntries(),
			zipEntry{name: bundleName + "/../evil", body: "x"}), wantErr: "not a clean name"},
		{
			name: "an absolute name", entries: append(bundleEntries(), zipEntry{name: "/" + bundleName + "/x", body: "x"}),
			wantErr: "not a clean name",
		},
		{
			name: "a backslash", entries: append(bundleEntries(), zipEntry{name: bundleName + `\..\x`, body: "x"}),
			wantErr: "not a clean name",
		},
		{name: "a name starting with ./", entries: append(bundleEntries(),
			zipEntry{name: "./" + bundleName + "/Contents/Extra", body: "x"}), wantErr: "not a clean name"},
		{name: "a name with //", entries: append(bundleEntries(),
			zipEntry{name: bundleName + "//Contents/Extra", body: "x"}), wantErr: "not a clean name"},
		{name: "an entry twice", entries: append(bundleEntries(),
			zipEntry{name: bundleName + "/Contents/Info.plist", body: "<plist>other</plist>"}), wantErr: "appears twice"},
		{name: "a folder twice, once without its slash", entries: append(bundleEntries(),
			zipEntry{name: bundleName + "/Contents", body: "file"}), wantErr: "appears twice"},
		{name: "a link that stays inside", entries: append(bundleEntries(),
			zipEntry{name: bundleName + "/Contents/Current", target: "MacOS"}), wantErr: "is a link"},
		{name: "a chain of links that climbs out", entries: append(bundleEntries(),
			zipEntry{name: bundleName + "/Contents/MacOS/up", target: "../.."},
			zipEntry{name: bundleName + "/Contents/MacOS/esc", target: "up/../outside"}), wantErr: "is a link"},
		{name: "a link to an absolute path", entries: append(bundleEntries(),
			zipEntry{name: bundleName + "/Contents/out", target: "/etc"}), wantErr: "is a link"},
		{name: "no executable", entries: bundleEntries()[:4], wantErr: "lacks " + bundleName + "/Contents/MacOS/" + brand.Slug},
		{name: "empty", wantErr: "lacks"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "bundle.zip")
			writeZip(t, path, tt.entries...)

			err := checkZipLayout(path, bundleName)

			if tt.wantErr == "" {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, errZipLayout)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}

	t.Run("not a zip", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "bundle.zip")
		testutil.WriteFile(t, path, "not a zip")

		err := checkZipLayout(path, bundleName)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "read the zip")
	})
}

func TestWriteSums(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	zipPath := filepath.Join(dir, update.AssetNames("0.3.0").Zip)
	dmgPath := filepath.Join(dir, update.AssetNames("0.3.0").DMG)
	sums := filepath.Join(dir, update.SumsName)

	testutil.WriteFile(t, zipPath, "zip")
	testutil.WriteFile(t, dmgPath, "dmg")

	require.NoError(t, writeSums(sums, zipPath, dmgPath))

	zipSum := sha256.Sum256([]byte("zip"))
	dmgSum := sha256.Sum256([]byte("dmg"))

	assert.Equal(t, hex.EncodeToString(dmgSum[:])+"  "+brand.Name+".dmg\n"+
		hex.EncodeToString(zipSum[:])+"  "+brand.Name+"_0.3.0_darwin_universal.zip\n", readFile(t, sums),
		"one line each, sorted by name, as shasum -a 256 -c reads them")

	err := writeSums(sums, filepath.Join(dir, "missing"))
	require.ErrorIs(t, err, fs.ErrNotExist)

	err = writeSums(filepath.Join(dir, "missing", update.SumsName), zipPath)
	require.ErrorIs(t, err, fs.ErrNotExist)
}

// releaseFolder writes the release of 0.3.0 into a fresh folder, its zip "the zip" and its
// manifest signed with keys, and returns the folder.
func releaseFolder(t *testing.T, keys releaseKeys) string {
	t.Helper()

	dir := t.TempDir()
	zipPath := filepath.Join(dir, update.AssetNames("0.3.0").Zip)
	testutil.WriteFile(t, zipPath, "the zip")

	require.NoError(t, writeManifest(filepath.Join(dir, update.ManifestName), "0.3.0", zipPath, keys))

	return dir
}

func TestVerifyRelease(t *testing.T) {
	t.Parallel()

	keys := releaseKeyPair(1)
	other := releaseKeyPair(2)

	t.Run("a release the key signed", func(t *testing.T) {
		t.Parallel()

		dir := releaseFolder(t, keys)
		g := newRig(t)

		require.NoError(t, g.runner().verifyRelease(dir, "", keys.public))

		assert.Equal(t, "✅ "+update.AssetNames("0.3.0").Zip+" matches its signed manifest (0.3.0, darwin-universal)\n",
			g.out.String())
	})

	t.Run("another key", func(t *testing.T) {
		t.Parallel()

		dir := releaseFolder(t, keys)

		require.ErrorIs(t, newRig(t).runner().verifyRelease(dir, "", other.public), update.ErrSignature)
	})

	t.Run("a zip changed after signing", func(t *testing.T) {
		t.Parallel()

		dir := releaseFolder(t, keys)
		testutil.WriteFile(t, filepath.Join(dir, update.AssetNames("0.3.0").Zip), "the zap")

		require.ErrorIs(t, newRig(t).runner().verifyRelease(dir, "", keys.public), update.ErrMismatch)
	})

	t.Run("no zip", func(t *testing.T) {
		t.Parallel()

		dir := releaseFolder(t, keys)
		require.NoError(t, os.Remove(filepath.Join(dir, update.AssetNames("0.3.0").Zip)))

		require.ErrorIs(t, newRig(t).runner().verifyRelease(dir, "", keys.public), fs.ErrNotExist)
	})

	t.Run("no manifest", func(t *testing.T) {
		t.Parallel()

		require.ErrorIs(t, newRig(t).runner().verifyRelease(t.TempDir(), "", keys.public), fs.ErrNotExist)
	})

	t.Run("a manifest over the limit", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		testutil.WriteFile(t, filepath.Join(dir, update.ManifestName), strings.Repeat(" ", update.MaxManifestBytes+1))

		require.ErrorIs(t, newRig(t).runner().verifyRelease(dir, "", keys.public), update.ErrInvalid)
	})

	t.Run("another platform's manifest", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		m, err := update.Sign(update.Manifest{
			Version: "0.3.0", Platform: "darwin-arm64", File: update.FileName("darwin-arm64", "0.3.0"),
			Size: 1, SHA512: strings.Repeat("0", 128),
		}, keys.private)
		require.NoError(t, err)

		body, err := update.Encode(m)
		require.NoError(t, err)
		testutil.WriteFile(t, filepath.Join(dir, update.ManifestName), string(body))

		require.ErrorIs(t, newRig(t).runner().verifyRelease(dir, "", keys.public), errOtherPlatform)
	})
}

func TestWriteManifest(t *testing.T) {
	t.Parallel()

	keys := releaseKeyPair(1)

	t.Run("a private key that isn't the public key's half", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		zipPath := filepath.Join(dir, update.AssetNames("0.3.0").Zip)
		testutil.WriteFile(t, zipPath, "the zip")

		mixed := releaseKeys{private: keys.private, public: releaseKeyPair(2).public}

		require.ErrorIs(t, writeManifest(filepath.Join(dir, update.ManifestName), "0.3.0", zipPath, mixed), errKeyMismatch)
		assert.NoFileExists(t, filepath.Join(dir, update.ManifestName), "nothing is written")
	})

	t.Run("no zip", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()

		require.ErrorIs(t, writeManifest(filepath.Join(dir, update.ManifestName), "0.3.0",
			filepath.Join(dir, "missing.zip"), keys), fs.ErrNotExist)
	})

	t.Run("an empty zip", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		zipPath := filepath.Join(dir, update.AssetNames("0.3.0").Zip)
		testutil.WriteFile(t, zipPath, "")

		require.ErrorIs(t, writeManifest(filepath.Join(dir, update.ManifestName), "0.3.0", zipPath, keys), update.ErrInvalid)
	})

	t.Run("the manifest can't be written", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		zipPath := filepath.Join(dir, update.AssetNames("0.3.0").Zip)
		testutil.WriteFile(t, zipPath, "the zip")

		require.ErrorIs(t, writeManifest(filepath.Join(dir, "missing", update.ManifestName), "0.3.0", zipPath, keys),
			fs.ErrNotExist)
	})
}

func TestVerifyPinned(t *testing.T) {
	t.Parallel()

	keys := releaseKeyPair(1)
	pinned := update.EncodeKey(keys.public)
	dir := releaseFolder(t, keys)
	r := newRig(t).runner()

	require.NoError(t, r.verifyPinned(dir, "", pinned))
	require.NoError(t, r.verifyPinned(dir, "0.3.0", pinned), "the version it was asked for")
	require.NoError(t, r.verifyPinned(dir, "v0.3.0", pinned), "a tag's v is dropped")

	err := r.verifyPinned(dir, "0.3.1", pinned)
	require.ErrorIs(t, err, errOtherVersion, "a stale pair can't pass for another release")
	assert.Contains(t, err.Error(), "0.3.0, not 0.3.1")

	require.ErrorIs(t, r.verifyPinned(dir, "latest", pinned), errOtherVersion)
	require.ErrorIs(t, r.verifyPinned("", "", pinned), errNoReleaseDir)
	require.ErrorIs(t, r.verifyPinned(dir, "", ""), errNoReleaseKey)
	require.ErrorIs(t, r.verifyPinned(dir, "", "not a key"), update.ErrKey)
}

func TestAppVerify(t *testing.T) {
	t.Parallel()

	keys := releaseKeyPair(1)
	dir := releaseFolder(t, keys)
	pinned := update.EncodeKey(keys.public)

	t.Run("checks the folder against the pinned key", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		g.setenv(mageArgsJSON, `["dir=`+dir+`","version=0.3.0"]`)

		require.NoError(t, g.runner().verifyFromArgs(pinned))
		assert.Contains(t, g.out.String(), "matches its signed manifest (0.3.0")

		g.setenv(mageArgsJSON, `["dir=`+dir+`","version=0.4.0"]`)
		require.ErrorIs(t, g.runner().verifyFromArgs(pinned), errOtherVersion)
	})

	t.Run("another key", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		g.setenv(mageArgsJSON, `["dir=`+dir+`"]`)

		require.ErrorIs(t, g.runner().verifyFromArgs(update.EncodeKey(releaseKeyPair(2).public)), update.ErrSignature)
	})

	t.Run("no key pinned", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		g.setenv(mageArgsJSON, `["dir=`+dir+`"]`)

		require.ErrorIs(t, g.runner().verifyFromArgs(""), errNoReleaseKey)
	})

	t.Run("no folder", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		g.setenv(mageArgsJSON, `[]`)

		require.ErrorIs(t, g.runner().verifyFromArgs(pinned), errNoReleaseDir)
	})
}

func TestAppPackage(t *testing.T) {
	t.Parallel()

	keys := releaseKeyPair(1)

	// setEnv sets app:package's environment in the rig: testVersion, signed as testIdentity
	// in a keychain of the test's own, with keys' private half.
	setEnv := func(t *testing.T, g *rig) {
		t.Helper()

		g.setenv(envReleaseVersion, testVersion)
		g.setenv(envCodesignIdentity, testIdentity)
		g.setenv(envCodesignKeychain, keychainFile(t))
		g.setenv(envUpdateSigningKey, writeSigningKey(t, keys))
	}

	t.Run("packages from the environment, against the pinned key", func(t *testing.T) {
		t.Parallel()

		g, _ := releaseRig(t, releaseFake{tag: "v" + testVersion})
		setEnv(t, g)

		require.NoError(t, g.runner().packageFromEnv(update.EncodeKey(keys.public)))

		require.NoError(t, g.runner().verifyRelease(filepath.Join(g.path(releaseRoot), testVersion), testVersion, keys.public))
	})

	t.Run("refuses while no key is pinned", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		setEnv(t, g)

		require.ErrorIs(t, g.runner().packageFromEnv(""), errNoReleaseKey)
	})

	t.Run("refuses a signing key that isn't the pinned key's half", func(t *testing.T) {
		t.Parallel()

		g, _ := releaseRig(t, releaseFake{tag: "v" + testVersion})
		setEnv(t, g)

		require.ErrorIs(t, g.runner().packageFromEnv(update.EncodeKey(releaseKeyPair(2).public)), errKeyMismatch)
		assert.Empty(t, toolLines(g))
	})
}

func TestBuildReleaseBundleFailures(t *testing.T) {
	t.Parallel()

	pinned := update.EncodeKey(releaseKeyPair(1).public)

	t.Run("stale brand files stop it once the tag and the tree are checked", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		releaseTools(g, releaseFake{tag: "v" + testVersion})

		_, err := g.runner().buildReleaseBundle(testVersion, pinned)

		require.ErrorIs(t, err, brandgen.ErrStale)
		assert.Empty(t, toolLines(g))
		assert.NoDirExists(t, g.path(unsignedRoot))
	})

	t.Run("a folder for the bundle that can't be made", func(t *testing.T) {
		t.Parallel()

		skipAsRoot(t)

		g, _ := releaseRig(t, releaseFake{tag: "v" + testVersion})
		mkdir(t, g.path(unsignedRoot))
		require.NoError(t, os.Chmod(g.path(unsignedRoot), 0o500))       //nolint:gosec // a folder nothing can be made in is the fixture
		t.Cleanup(func() { _ = os.Chmod(g.path(unsignedRoot), 0o700) }) //nolint:gosec // lets the test's cleanup remove it

		_, err := g.runner().buildReleaseBundle(testVersion, pinned)

		require.ErrorIs(t, err, fs.ErrPermission)
		assert.Contains(t, err.Error(), "create "+filepath.Join(g.path(unsignedRoot), testVersion))
		assert.Empty(t, g.linesOf("go"), "nothing is compiled")
	})
}

func TestCheckUnsignedBundleUnreadable(t *testing.T) {
	t.Parallel()

	skipAsRoot(t)

	tests := []struct {
		name string
		lock func(bundle string) string // the part of the bundle made unreadable
	}{
		{name: "a folder that can't be listed", lock: filepath.Dir},
		{name: "a bundle that can't be opened", lock: func(bundle string) string { return bundle }},
		{name: "a folder inside it that can't be read", lock: func(bundle string) string {
			return filepath.Join(bundle, "Contents", "Resources")
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			bundle := writeUnsignedBundle(t, t.TempDir())
			locked := tt.lock(bundle)
			require.NoError(t, os.Chmod(locked, 0o300))       //nolint:gosec // a folder that can't be read is the fixture
			t.Cleanup(func() { _ = os.Chmod(locked, 0o700) }) //nolint:gosec // lets the test's cleanup remove it

			err := newRig(t).runner().checkUnsignedBundle(bundle)

			require.ErrorIs(t, err, errUnsignedBundle)
			require.ErrorIs(t, err, fs.ErrPermission)
		})
	}
}

func TestCopyBundle(t *testing.T) {
	t.Parallel()

	src := writeUnsignedBundle(t, t.TempDir())
	dst := filepath.Join(t.TempDir(), bundleName)

	require.NoError(t, copyBundle(src, dst))
	assert.Equal(t, "universal", readFile(t, bundleExecutable(dst)))
	assert.Equal(t, "<plist/>", readFile(t, filepath.Join(dst, "Contents", "Info.plist")))

	err := copyBundle(src, dst)
	require.ErrorIs(t, err, fs.ErrExist, "a copy never overwrites a bundle")
	assert.Contains(t, err.Error(), "copy the unsigned bundle")

	err = copyBundle(filepath.Join(t.TempDir(), "missing.app"), filepath.Join(t.TempDir(), bundleName))
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestReleaseFileFailures(t *testing.T) {
	t.Parallel()

	t.Run("a signing key that is a folder", func(t *testing.T) {
		t.Parallel()

		_, err := readSigningKey(t.TempDir())

		require.Error(t, err)
		assert.Contains(t, err.Error(), "read the signing key")
	})

	t.Run("an earlier release that can't be removed", func(t *testing.T) {
		t.Parallel()

		skipAsRoot(t)

		dir := filepath.Join(t.TempDir(), testVersion)
		testutil.WriteFile(t, filepath.Join(dir, "old.zip"), "old")
		require.NoError(t, os.Chmod(dir, 0o500))       //nolint:gosec // a folder nothing can be removed from is the fixture
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:gosec // lets the test's cleanup remove it

		err := freshFolder(dir)

		require.ErrorIs(t, err, fs.ErrPermission)
		assert.Contains(t, err.Error(), "remove the earlier files in "+dir)
	})

	t.Run("a file to sum that is a folder", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()

		require.Error(t, writeSums(filepath.Join(dir, update.SumsName), t.TempDir()))
		assert.NoFileExists(t, filepath.Join(dir, update.SumsName), "no sums for a file that can't be read")
	})

	t.Run("a manifest that is a folder", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		mkdir(t, filepath.Join(dir, update.ManifestName))

		err := newRig(t).runner().verifyRelease(dir, "", releaseKeyPair(1).public)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "read the update manifest")
	})

	t.Run("a release missing a file to report", func(t *testing.T) {
		t.Parallel()

		names := update.AssetNames(testVersion)
		dir := t.TempDir()
		testutil.WriteFile(t, filepath.Join(dir, names.Zip), "zip")

		g := newRig(t)
		err := g.runner().reportRelease(dir, names)

		require.ErrorIs(t, err, fs.ErrNotExist)
		assert.Contains(t, err.Error(), "read the size of "+names.DMG)
		assert.Contains(t, g.out.String(), "  "+names.Zip+" (3 bytes, 0.0 MB)", "the files before it are reported")
	})
}

func TestMakeDMGFailures(t *testing.T) {
	t.Parallel()

	sign := signer{identity: testIdentity}

	t.Run("no folder for the disk image", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		releaseTools(g, releaseFake{})

		err := g.runner().makeDMG(t.TempDir(), filepath.Join(t.TempDir(), "missing", "app.dmg"), sign)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "create the disk image's folder")
		assert.Empty(t, g.lines())
	})

	t.Run("Applications can't be linked beside the bundle", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		releaseTools(g, releaseFake{})
		g.tool("ditto", func(args []string) testutil.FakeRun {
			mkdir(t, lastArg(args))
			mkdir(t, filepath.Join(filepath.Dir(lastArg(args)), "Applications")) // in the way of the link

			return testutil.FakeRun{}
		})

		dmg := filepath.Join(t.TempDir(), "app.dmg")
		err := g.runner().makeDMG(filepath.Join(t.TempDir(), bundleName), dmg, sign)

		require.ErrorIs(t, err, fs.ErrExist)
		assert.Contains(t, err.Error(), "link Applications for the disk image")
		assert.Empty(t, g.linesOf("hdiutil"), "no disk image is made")
		assert.NoFileExists(t, dmg)
	})
}
