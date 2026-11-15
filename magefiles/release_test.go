//go:build mage

package main

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

	// testKeychain is the keychain the packaging tests name, with a space, as a path may hold.
	testKeychain = "/keychains/CI Signing.keychain-db"

	// originalKeychains is the keychain search list before a test, as security prints it.
	originalKeychains = "    \"/Library/Keychains/My Keys.keychain-db\"\n    \"/Library/Keychains/System.keychain\"\n"
)

// bundleName is the app bundle's folder name.
const bundleName = brand.Name + ".app"

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

// releaseTools puts a fake of every tool app:package runs on an isolated PATH, each
// logging its name and arguments to one shared log, and returns the PATH directory, so a
// test can replace a tool, and the log's path: a git whose describe --exact-match names
// tag; a go that logs GOARCH and writes a binary after -o; a lipo that writes the
// universal binary and reads x86_64 and arm64 in it; a ditto that zips by copying a zip of
// a bundle and copies a folder by making it; an hdiutil that writes the disk image; a
// security that keeps the keychain search list in a file, starting at originalKeychains;
// and pnpm, plutil, and codesign. The search list's file is in $KEYCHAIN_LIST.
func releaseTools(t *testing.T, tag string) (string, string) {
	t.Helper()

	tmp := t.TempDir()
	log := filepath.Join(tmp, "tools.log")
	list := filepath.Join(tmp, "keychains")
	fixture := filepath.Join(tmp, "bundle.zip")

	writeZip(t, fixture, bundleEntries()...)
	testutil.WriteFile(t, list, originalKeychains)

	t.Setenv("TOOLS_LOG", log)
	t.Setenv("KEYCHAIN_LIST", list)
	t.Setenv("ZIP_FIXTURE", fixture)
	t.Setenv("FAKE_TAG", tag)
	unsetenv(t, "GOARCH")

	dir := isolatedPath(t)

	for _, name := range []string{"pnpm", "plutil", "codesign"} {
		fakeTool(t, dir, name, `printf '`+name+` %s\n' "$*" >> "$TOOLS_LOG"`)
	}

	fakeTool(t, dir, "git", `printf 'git %s\n' "$*" >> "$TOOLS_LOG"
if [ "$*" = "describe --tags --exact-match" ] && [ -n "$FAKE_TAG" ]; then echo "$FAKE_TAG"; exit 0; fi
exit 128`)
	fakeTool(t, dir, "go", `printf 'go GOARCH=%s %s\n' "$GOARCH" "$*" >> "$TOOLS_LOG"
while [ $# -gt 0 ]; do
  if [ "$1" = -o ]; then out="$2"; fi
  shift
done
printf 'binary' > "$out"`)
	fakeTool(t, dir, "lipo", `printf 'lipo %s\n' "$*" >> "$TOOLS_LOG"
if [ "$1" = -create ]; then printf 'universal' > "$3"; fi
if [ "$1" = -archs ]; then echo "${LIPO_ARCHS-x86_64 arm64}"; fi`)
	fakeTool(t, dir, "ditto", `printf 'ditto %s\n' "$*" >> "$TOOLS_LOG"
for a; do last="$a"; done
if [ "$1" = -c ]; then /bin/cp "$ZIP_FIXTURE" "$last"; else /bin/mkdir -p "$last"; fi`)
	fakeTool(t, dir, "hdiutil", `printf 'hdiutil %s\n' "$*" >> "$TOOLS_LOG"
for a; do
  if [ "$prev" = -srcfolder ]; then /bin/ls -ld "$a" | /usr/bin/cut -c1-10 > "$TOOLS_LOG.srcfolder"; fi
  prev="$a"; last="$a"
done
printf 'disk image' > "$last"`)
	fakeTool(t, dir, "security", `printf 'security %s\n' "$*" >> "$TOOLS_LOG"
if [ "$1" = list-keychains ] && [ "$#" -eq 3 ]; then /bin/cat "$KEYCHAIN_LIST"; exit 0; fi
if [ "$1" = list-keychains ] && [ "$4" = -s ]; then
  shift 4
  : > "$KEYCHAIN_LIST"
  for k in "$@"; do printf '    "%s"\n' "$k" >> "$KEYCHAIN_LIST"; done
  exit 0
fi
exit 1`)

	return dir, log
}

// toolLines returns the lines of the tools' log, but git's, which reads the version.
func toolLines(t *testing.T, log string) []string {
	t.Helper()

	if _, err := os.Stat(log); errors.Is(err, fs.ErrNotExist) {
		return nil // no tool ran
	}

	var lines []string

	for _, line := range logLines(t, log) {
		if !strings.HasPrefix(line, "git ") {
			lines = append(lines, line)
		}
	}

	return lines
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

//nolint:paralleltest // changes the process's working directory, environment, PATH, and standard output
func TestPackageRelease(t *testing.T) {
	keys := releaseKeyPair(1)
	out := filepath.Join("bin", "release", testVersion)
	bundle := filepath.Join(out, bundleName)
	exe := filepath.Join(bundle, "Contents", "MacOS", brand.Slug)
	plist := filepath.Join(bundle, "Contents", "Info.plist")
	names := update.AssetNames(testVersion)
	q := regexp.QuoteMeta
	keychains := q("/Library/Keychains/My Keys.keychain-db /Library/Keychains/System.keychain")
	signed := q("codesign --force --sign "+testIdentity+" --keychain "+testKeychain+" --timestamp=none ") + "%s"

	goBuild := func(arch string) string {
		return q("go GOARCH="+arch+" build -tags desktop,production -trimpath -buildvcs=false -ldflags -s -w -X main.version=") +
			`\S+` + q(" -X main.commit=") + `\S+` + q(" -X main.buildDate=") + `\S+` +
			q(" -X main.release="+testVersion+" -X main.buildNumber=") + `\S+` + q(" -X main.flavor=release -o ") +
			`\S+` + q("/"+brand.Slug+"-"+arch+" ./cmd/"+brand.Slug)
	}

	t.Run("builds, signs, packages, and verifies a universal release", func(t *testing.T) {
		dir := inTempDir(t)
		checkout(t, dir)
		_, log := releaseTools(t, "v"+testVersion)

		var err error

		stdout := captureStdout(t, func() { err = packageRelease(testConfig(t, keys, testKeychain)) })

		require.NoError(t, err)

		requireLines(t, []string{
			q("pnpm install --frozen-lockfile"),
			q("pnpm build"),
			q("security list-keychains -d user"),
			q("security list-keychains -d user -s "+testKeychain+" ") + keychains,
			goBuild("arm64"),
			goBuild("amd64"),
			q("lipo -create -output "+exe+" ") + `\S+` + q("/"+brand.Slug+"-arm64 ") + `\S+` + q("/"+brand.Slug+"-amd64"),
			q("lipo -archs " + exe),
			q("plutil -replace CFBundleShortVersionString -string 1.2.3 " + plist),
			q("plutil -replace CFBundleVersion -string " + brandgen.PlaceholderVersion + " " + plist),
			fmtPattern(signed, q(bundle)),
			q("codesign --verify --deep --strict " + bundle),
			q("ditto -c -k --norsrc --keepParent " + bundle + " " + filepath.Join(out, names.Zip)),
			q("ditto "+bundle+" "+out+"/.dmg-") + `\S+` + q("/"+bundleName),
			q("hdiutil create -volname "+brand.Name+" -srcfolder "+out+"/.dmg-") + `\S+` +
				q(" -ov -format ULMO "+filepath.Join(out, names.DMG)),
			fmtPattern(signed, q(filepath.Join(out, names.DMG))),
			q("codesign --verify --strict " + filepath.Join(out, names.DMG)),
			q("security list-keychains -d user -s ") + keychains,
		}, toolLines(t, log))

		assert.Equal(t, "git describe --tags --exact-match", logLines(t, log)[0], "the tag is checked before anything builds")
		assert.NotContains(t, readFile(t, log), "main.updateFeed", "a release reads the feed GitHub serves")
		assert.NotContains(t, readFile(t, log), "main.updateKey", "a release verifies with the key it pins")
		assert.Equal(t, originalKeychains, readFile(t, os.Getenv("KEYCHAIN_LIST")), "the search list is back as it was")
		assert.Equal(t, "drwxr-xr-x\n", readFile(t, log+".srcfolder"), "the disk image's root is world-readable")

		entries, err := os.ReadDir(out)
		require.NoError(t, err)

		files := make([]string, 0, len(entries))
		for _, e := range entries {
			files = append(files, e.Name())
		}

		assert.ElementsMatch(t, []string{bundleName, names.Zip, names.DMG, names.Sums, names.Manifest}, files,
			"the disk image's folder is gone")
		assert.Equal(t, "universal", readFile(t, exe))

		require.NoError(t, verifyRelease(out, keys.public))
		assert.Contains(t, stdout, "Built the arm64 slice (6 bytes")
		assert.Contains(t, stdout, "Built the amd64 slice (6 bytes")
		assert.Contains(t, stdout, "✅ "+names.Zip+" matches its signed manifest ("+testVersion+", darwin-universal)")
		assert.Contains(t, stdout, "  "+names.DMG+" (10 bytes, 0.0 MB)")

		sums := readFile(t, filepath.Join(out, names.Sums))
		assert.Equal(t, sha256Hex(t, filepath.Join(out, names.DMG))+"  "+names.DMG+"\n"+
			sha256Hex(t, filepath.Join(out, names.Zip))+"  "+names.Zip+"\n", sums)
	})

	t.Run("signs from the search list when no keychain is named", func(t *testing.T) {
		dir := inTempDir(t)
		checkout(t, dir)
		_, log := releaseTools(t, "v"+testVersion)

		captureStdout(t, func() { require.NoError(t, packageRelease(testConfig(t, keys, ""))) })

		lines := toolLines(t, log)
		assert.NotContains(t, readFile(t, log), "security", "the search list is left alone")
		assert.Contains(t, lines, "codesign --force --sign "+testIdentity+" --timestamp=none "+bundle)
	})

	t.Run("a step that fails still puts the search list back", func(t *testing.T) {
		dir := inTempDir(t)
		checkout(t, dir)
		tools, log := releaseTools(t, "v"+testVersion)
		fakeTool(t, tools, "hdiutil", `printf 'hdiutil %s\n' "$*" >> "$TOOLS_LOG"; exit 1`)

		var err error

		captureStdout(t, func() { err = packageRelease(testConfig(t, keys, testKeychain)) })

		require.Error(t, err)
		assert.Contains(t, err.Error(), "make the disk image")

		lines := toolLines(t, log)
		assert.Equal(t, "security list-keychains -d user -s /Library/Keychains/My Keys.keychain-db "+
			"/Library/Keychains/System.keychain", lines[len(lines)-1])
		assert.Equal(t, originalKeychains, readFile(t, os.Getenv("KEYCHAIN_LIST")))
	})

	t.Run("a binary without both architectures fails", func(t *testing.T) {
		dir := inTempDir(t)
		checkout(t, dir)
		releaseTools(t, "v"+testVersion)
		t.Setenv("LIPO_ARCHS", "arm64")

		var err error

		captureStdout(t, func() { err = packageRelease(testConfig(t, keys, "")) })

		require.ErrorIs(t, err, errArches)
		assert.Contains(t, err.Error(), `lipo -archs reads "arm64", not "arm64 x86_64"`)
	})

	tests := []struct {
		name    string
		tool    string     // the tool to replace, or ""
		body    string     // the replacement's body
		zipped  []zipEntry // what ditto zips, when not the bundle
		wantErr string
	}{
		{name: "the first slice fails to build", tool: "go", body: "exit 1", wantErr: "build the desktop binary"},
		{name: "lipo can't combine the slices", tool: "lipo", body: "exit 1", wantErr: "combine the slices with lipo"},
		{name: "lipo can't read the binary", tool: "lipo", body: `if [ "$1" = -archs ]; then exit 1; fi`, wantErr: "read the binary's architectures"},
		{name: "signing fails", tool: "codesign", body: `case "$*" in --force*) exit 1 ;; esac`, wantErr: "sign the bundle as " + testIdentity},
		{name: "the signature doesn't verify", tool: "codesign", body: `case "$*" in --verify*) exit 1 ;; esac`, wantErr: "verify the signature of " + bundle},
		{name: "signing the disk image fails", tool: "codesign", body: `case "$*" in --force*.dmg) exit 1 ;; esac`, wantErr: "sign the disk image as " + testIdentity},
		{name: "zipping fails", tool: "ditto", body: `if [ "$1" = -c ]; then exit 1; fi`, wantErr: "zip the bundle"},
		{name: "copying the bundle for the disk image fails", tool: "ditto", body: `if [ "$1" = -c ]; then /bin/cp "$ZIP_FIXTURE" "$6"; exit 0; fi; exit 1`, wantErr: "copy the bundle for the disk image"},
		{name: "the zip holds a stray file", zipped: append(bundleEntries(), zipEntry{name: "README", body: "stray"}), wantErr: "is beside"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := inTempDir(t)
			checkout(t, dir)
			tools, _ := releaseTools(t, "v"+testVersion)

			if tt.tool != "" {
				fakeTool(t, tools, tt.tool, tt.body)
			}

			if tt.zipped != nil {
				writeZip(t, os.Getenv("ZIP_FIXTURE"), tt.zipped...)
			}

			var err error

			captureStdout(t, func() { err = packageRelease(testConfig(t, keys, "")) })

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// fmtPattern fills the one %s in pattern with arg.
func fmtPattern(pattern, arg string) string {
	return strings.Replace(pattern, "%s", arg, 1)
}

// sha256Hex returns the SHA-256 of the file at path, as hex.
func sha256Hex(t *testing.T, path string) string {
	t.Helper()

	sum := sha256.Sum256([]byte(readFile(t, path)))

	return hex.EncodeToString(sum[:])
}

//nolint:paralleltest // changes the process's working directory, environment, and PATH
func TestPackageReleaseRefusesBeforeAnyToolRuns(t *testing.T) {
	keys := releaseKeyPair(1)
	other := releaseKeyPair(2)

	tests := []struct {
		name    string
		tag     string
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
		{name: "no tag on HEAD", wantErr: errNoExactTag},
		{name: "the tag of another version", tag: "v1.2.4", wantErr: errTagMismatch, wantMsg: "HEAD's tag is v1.2.4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := inTempDir(t)
			checkout(t, dir)
			_, log := releaseTools(t, tt.tag)

			cfg := testConfig(t, keys, testKeychain)
			if tt.change != nil {
				tt.change(t, &cfg)
			}

			err := packageRelease(cfg)

			require.ErrorIs(t, err, tt.wantErr)
			assert.Contains(t, err.Error(), tt.wantMsg)
			assert.Empty(t, toolLines(t, log), "nothing builds, and the search list is left alone")
			assert.NoDirExists(t, filepath.Join("bin", "release"))
		})
	}
}

//nolint:paralleltest // changes the process's environment
func TestPackageConfigFromEnv(t *testing.T) {
	setAll := func(t *testing.T) {
		t.Helper()

		t.Setenv(envReleaseVersion, "0.3.0")
		t.Setenv(envCodesignIdentity, "Release Signing")
		t.Setenv(envCodesignKeychain, "/keychains/ci.keychain-db")
		t.Setenv(envUpdateSigningKey, "/keys/release.key")
	}

	t.Run("reads every variable", func(t *testing.T) {
		setAll(t)

		cfg, err := packageConfigFromEnv("pinned")

		require.NoError(t, err)
		assert.Equal(t, packageConfig{
			version: "0.3.0", identity: "Release Signing", keychain: "/keychains/ci.keychain-db",
			signingKey: "/keys/release.key", releaseKey: "pinned",
		}, cfg)
	})

	t.Run("the keychain is optional", func(t *testing.T) {
		setAll(t)
		unsetenv(t, envCodesignKeychain)

		cfg, err := packageConfigFromEnv("pinned")

		require.NoError(t, err)
		assert.Empty(t, cfg.keychain)
	})

	t.Run("names every missing variable", func(t *testing.T) {
		for _, key := range []string{envReleaseVersion, envCodesignIdentity, envCodesignKeychain, envUpdateSigningKey} {
			unsetenv(t, key)
		}

		_, err := packageConfigFromEnv("pinned")

		require.ErrorIs(t, err, errMissingEnv)
		assert.Contains(t, err.Error(), "RELEASE_VERSION, CODESIGN_IDENTITY, UPDATE_SIGNING_KEY")
	})

	t.Run("refuses while no release key is pinned", func(t *testing.T) {
		setAll(t)

		_, err := packageConfigFromEnv("")

		require.ErrorIs(t, err, errNoReleaseKey)
	})
}

//nolint:paralleltest // changes the process's working directory and environment
func TestCheckReleaseTag(t *testing.T) {
	t.Run("the version's tag on HEAD", func(t *testing.T) {
		isolateGit(t)
		inTempDir(t)
		git(t, "init", "-q")
		git(t, "commit", "-q", "--allow-empty", "-m", "first")
		git(t, "tag", "v0.3.0")

		require.NoError(t, checkReleaseTag("0.3.0"))
		require.ErrorIs(t, checkReleaseTag("0.3.1"), errTagMismatch)
	})

	t.Run("a tag on an earlier commit", func(t *testing.T) {
		isolateGit(t)
		inTempDir(t)
		git(t, "init", "-q")
		git(t, "commit", "-q", "--allow-empty", "-m", "first")
		git(t, "tag", "v0.3.0")
		git(t, "commit", "-q", "--allow-empty", "-m", "second")

		require.ErrorIs(t, checkReleaseTag("0.3.0"), errNoExactTag)
	})

	t.Run("no tag at all", func(t *testing.T) {
		isolateGit(t)
		inTempDir(t)
		git(t, "init", "-q")
		git(t, "commit", "-q", "--allow-empty", "-m", "first")

		require.ErrorIs(t, checkReleaseTag("0.3.0"), errNoExactTag)
	})
}

func TestReleaseMeta(t *testing.T) {
	t.Parallel()

	meta := releaseMeta("0.2.0-rc.1")

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
		{name: "a link that stays inside", entries: append(bundleEntries(),
			zipEntry{name: bundleName + "/Contents/Current", target: "MacOS"})},
		{name: "resource forks beside it", entries: append(bundleEntries(),
			zipEntry{name: "__MACOSX/"}, zipEntry{name: "__MACOSX/" + bundleName + "/._Contents", body: "fork"}), wantErr: "__MACOSX"},
		{name: "two bundles", entries: append(bundleEntries(), zipEntry{name: "Other.app/"}), wantErr: "Other.app"},
		{name: "a stray file", entries: append(bundleEntries(), zipEntry{name: "README", body: "stray"}), wantErr: "README"},
		{name: "a name that climbs out", entries: append(bundleEntries(),
			zipEntry{name: bundleName + "/../evil", body: "x"}), wantErr: "evil"},
		{name: "an absolute name", entries: append(bundleEntries(), zipEntry{name: "/" + bundleName + "/x", body: "x"}), wantErr: "beside"},
		{name: "a backslash", entries: append(bundleEntries(), zipEntry{name: bundleName + `\..\x`, body: "x"}), wantErr: "beside"},
		{name: "a link out of the bundle", entries: append(bundleEntries(),
			zipEntry{name: bundleName + "/Contents/out", target: "../../.."}), wantErr: "points out"},
		{name: "a link to an absolute path", entries: append(bundleEntries(),
			zipEntry{name: bundleName + "/Contents/out", target: "/etc"}), wantErr: "points out"},
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

//nolint:paralleltest // changes the process's standard output
func TestVerifyRelease(t *testing.T) {
	keys := releaseKeyPair(1)
	other := releaseKeyPair(2)

	t.Run("a release the key signed", func(t *testing.T) {
		dir := releaseFolder(t, keys)

		var err error

		out := captureStdout(t, func() { err = verifyRelease(dir, keys.public) })

		require.NoError(t, err)
		assert.Equal(t, "✅ "+update.AssetNames("0.3.0").Zip+" matches its signed manifest (0.3.0, darwin-universal)\n", out)
	})

	t.Run("another key", func(t *testing.T) {
		dir := releaseFolder(t, keys)

		require.ErrorIs(t, verifyRelease(dir, other.public), update.ErrSignature)
	})

	t.Run("a zip changed after signing", func(t *testing.T) {
		dir := releaseFolder(t, keys)
		testutil.WriteFile(t, filepath.Join(dir, update.AssetNames("0.3.0").Zip), "the zap")

		require.ErrorIs(t, verifyRelease(dir, keys.public), update.ErrMismatch)
	})

	t.Run("no zip", func(t *testing.T) {
		dir := releaseFolder(t, keys)
		require.NoError(t, os.Remove(filepath.Join(dir, update.AssetNames("0.3.0").Zip)))

		require.ErrorIs(t, verifyRelease(dir, keys.public), fs.ErrNotExist)
	})

	t.Run("no manifest", func(t *testing.T) {
		require.ErrorIs(t, verifyRelease(t.TempDir(), keys.public), fs.ErrNotExist)
	})

	t.Run("a manifest over the limit", func(t *testing.T) {
		dir := t.TempDir()
		testutil.WriteFile(t, filepath.Join(dir, update.ManifestName), strings.Repeat(" ", update.MaxManifestBytes+1))

		require.ErrorIs(t, verifyRelease(dir, keys.public), update.ErrInvalid)
	})

	t.Run("another platform's manifest", func(t *testing.T) {
		dir := t.TempDir()
		m, err := update.Sign(update.Manifest{
			Version: "0.3.0", Platform: "darwin-arm64", File: update.FileName("darwin-arm64", "0.3.0"),
			Size: 1, SHA512: strings.Repeat("0", 128),
		}, keys.private)
		require.NoError(t, err)

		body, err := update.Encode(m)
		require.NoError(t, err)
		testutil.WriteFile(t, filepath.Join(dir, update.ManifestName), string(body))

		require.ErrorIs(t, verifyRelease(dir, keys.public), errOtherPlatform)
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

//nolint:paralleltest // changes the process's standard output
func TestVerifyPinned(t *testing.T) {
	keys := releaseKeyPair(1)
	dir := releaseFolder(t, keys)

	captureStdout(t, func() { require.NoError(t, verifyPinned(dir, update.EncodeKey(keys.public))) })

	require.ErrorIs(t, verifyPinned("", update.EncodeKey(keys.public)), errNoReleaseDir)
	require.ErrorIs(t, verifyPinned(dir, ""), errNoReleaseKey)
	require.ErrorIs(t, verifyPinned(dir, "not a key"), update.ErrKey)
}

func TestAppVerify(t *testing.T) {
	if update.ReleaseKey != "" {
		t.Skip("covers the refusal before a release key is pinned")
	}

	t.Setenv(mageArgsJSON, `["dir=`+t.TempDir()+`"]`)
	require.ErrorIs(t, App{}.Verify(), errNoReleaseKey)

	t.Setenv(mageArgsJSON, `[]`)
	require.ErrorIs(t, App{}.Verify(), errNoReleaseDir)
}

func TestAppPackageOnMacOS(t *testing.T) {
	onlyOnMacOS(t)

	if update.ReleaseKey != "" {
		t.Skip("covers the refusal before a release key is pinned")
	}

	t.Setenv(envReleaseVersion, testVersion)
	t.Setenv(envCodesignIdentity, testIdentity)
	t.Setenv(envUpdateSigningKey, "/keys/release.key")

	require.ErrorIs(t, App{}.Package(), errNoReleaseKey)
}

//nolint:paralleltest // changes the process's standard output
func TestKeygen(t *testing.T) {
	t.Run("writes the private key and prints the public key", func(t *testing.T) {
		repo := t.TempDir()
		out := filepath.Join(t.TempDir(), "keys")

		var err error

		stdout := captureStdout(t, func() { err = keygen(out, repo) })

		require.NoError(t, err)

		path := filepath.Join(out, signingKeyFile)
		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "readable by its owner alone")

		dirInfo, err := os.Stat(out)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm())

		private, err := update.ParsePrivateKey([]byte(readFile(t, path)))
		require.NoError(t, err)

		lines := strings.Split(strings.TrimSpace(stdout), "\n")
		assert.Equal(t, update.EncodeKey(update.PublicKeyOf(private)), lines[len(lines)-1], "the public key, last, alone")
		assert.NotContains(t, stdout, "PRIVATE KEY")
		assert.NotContains(t, stdout, strings.Split(readFile(t, path), "\n")[1], "nothing of the private key")
	})

	t.Run("never overwrites a key", func(t *testing.T) {
		repo := t.TempDir()
		out := t.TempDir()
		path := filepath.Join(out, signingKeyFile)
		testutil.WriteFile(t, path, "the key kept")

		err := keygen(out, repo)

		require.ErrorIs(t, err, fs.ErrExist)
		assert.Equal(t, "the key kept", readFile(t, path))
	})

	t.Run("refuses a folder inside the repository", func(t *testing.T) {
		repo := t.TempDir()
		outside := t.TempDir()
		link := filepath.Join(outside, "into-repo")
		require.NoError(t, os.Symlink(repo, link))

		for _, out := range []string{
			repo,
			filepath.Join(repo, "keys"),
			filepath.Join(repo, "not", "made", "yet"),
			filepath.Join(link, "keys"),
		} {
			require.ErrorIs(t, keygen(out, repo), errInRepository, out)
		}

		entries, err := os.ReadDir(repo)
		require.NoError(t, err)
		assert.Empty(t, entries, "nothing is written")
	})

	t.Run("refuses no folder", func(t *testing.T) {
		require.ErrorIs(t, keygen("", t.TempDir()), errNoKeyFolder)
	})

	t.Run("a folder that can't be made", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "file")
		testutil.WriteFile(t, file, "")

		require.ErrorIs(t, keygen(filepath.Join(file, "keys"), t.TempDir()), syscall.ENOTDIR)
	})
}

func TestAppKeygen(t *testing.T) {
	repo := inTempDir(t)
	out := filepath.Join(t.TempDir(), "keys")

	t.Setenv(mageArgsJSON, `["out=`+filepath.Join(repo, "keys")+`"]`)
	require.ErrorIs(t, App{}.Keygen(), errInRepository, "the working directory is the repository")

	t.Setenv(mageArgsJSON, `["out=`+out+`"]`)
	captureStdout(t, func() { require.NoError(t, App{}.Keygen()) })
	assert.FileExists(t, filepath.Join(out, signingKeyFile))
}

func TestWithin(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	for path, want := range map[string]bool{
		root:                                  true,
		filepath.Join(root, "a"):              true,
		filepath.Join(root, "a", "b"):         true,
		filepath.Join(root, "..", "sibling"):  false,
		root + "-other":                       false,
		filepath.Join(root, "a", "..", ".."):  false,
		filepath.Join(root, "..a-not-parent"): true,
	} {
		got, err := within(path, root)

		require.NoError(t, err)
		assert.Equal(t, want, got, path)
	}
}
