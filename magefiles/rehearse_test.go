//go:build mage

package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/brandgen"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/update"
)

// rehearsalHome is the home folder the rehearsal tests name the data directory from.
const rehearsalHome = "/Users/example"

// rehearsalTools puts a fake of every tool app:rehearse runs on an isolated PATH, as
// releaseTools does, with a codesign whose check of a bundle fails once case=broken has
// changed its app icon, and returns the PATH directory and the tools' log.
func rehearsalTools(t *testing.T) (string, string) {
	t.Helper()

	tools, log := releaseTools(t, "")
	fakeTool(t, tools, "codesign", `printf 'codesign %s\n' "$*" >> "$TOOLS_LOG"
for a; do last="$a"; done
case "$1:$last" in
  --force:*.app) /bin/mkdir -p "$last/Contents/_CodeSignature"; printf 'seal' > "$last/Contents/_CodeSignature/CodeResources" ;;
  --verify:*.app) case "$(/bin/cat "$last/Contents/Resources/`+brandgen.IconFile+`.icns")" in *"case=broken"*) exit 1 ;; esac ;;
esac`)

	return tools, log
}

// rehearsalConfigIn is the rehearsal config the tests build kase from, in dir's
// bin/rehearsal, signed as testIdentity.
func rehearsalConfigIn(dir, kase string) rehearsalConfig {
	return rehearsalConfig{
		kase: kase, identity: testIdentity, root: filepath.Join(dir, "bin", "rehearsal"), home: rehearsalHome,
	}
}

// stampedKey is the key a rehearsal build's go build line stamps, read from the tools'
// log line.
func stampedKey(t *testing.T, line string) string {
	t.Helper()

	m := regexp.MustCompile(`-X main\.updateKey=(\S+)`).FindStringSubmatch(line)
	require.NotNil(t, m, line)

	return m[1]
}

// filesUnder lists every file under root but folders, by its slash-separated path from
// root, in the order filepath.WalkDir finds them.
func filesUnder(t *testing.T, root string) []string {
	t.Helper()

	var files []string

	require.NoError(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		require.NoError(t, err)

		if d.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		require.NoError(t, err)

		files = append(files, filepath.ToSlash(rel))

		return nil
	}))

	return files
}

//nolint:paralleltest // changes the process's working directory, environment, PATH, and standard output
func TestRehearse(t *testing.T) {
	q := regexp.QuoteMeta

	goBuild := func(version, feed, exe string) string {
		return q("go GOARCH= build -tags desktop,production -trimpath -buildvcs=false -ldflags -s -w -X main.version=") +
			`\S+` + q(" -X main.commit=") + `\S+` + q(" -X main.buildDate=") + `\S+` +
			q(" -X main.release="+version+" -X main.buildNumber=") + `\S+` +
			q(" -X main.flavor=rehearsal -X 'main.updateFeed="+feed+"' -X main.updateKey=") + `\S+` +
			q(" -o "+exe+" ./cmd/"+brand.Slug)
	}

	built := func(cfg rehearsalConfig) []string {
		p := cfg.paths()
		signed := q("codesign --force --sign " + testIdentity + " --timestamp=none ")
		plist := func(bundle string) string { return filepath.Join(bundle, "Contents", "Info.plist") }
		exe := func(bundle string) string { return filepath.Join(bundle, "Contents", "MacOS", brand.Slug) }

		// Each bundle's Info.plist gets the versions alone: a rehearsal keeps the release's
		// name and bundle ID, as the update it rehearses does, never a build from source's
		// (devStamps).
		return []string{
			q("pnpm install --frozen-lockfile"),
			q("pnpm build"),
			goBuild(rehearsalFrom, p.feed, exe(p.a)),
			q("plutil -replace CFBundleShortVersionString -string " + rehearsalFrom + " " + plist(p.a)),
			q("plutil -replace CFBundleVersion -string ") + `\S+ ` + q(plist(p.a)),
			signed + q(p.a),
			q("codesign --verify --deep --strict " + p.a),
			goBuild(rehearsalTo, p.feed, exe(p.b)),
			q("plutil -replace CFBundleShortVersionString -string " + rehearsalTo + " " + plist(p.b)),
			q("plutil -replace CFBundleVersion -string ") + `\S+ ` + q(plist(p.b)),
			signed + q(p.b),
			q("codesign --verify --deep --strict " + p.b),
		}
	}

	zipped := func(cfg rehearsalConfig) string {
		p := cfg.paths()

		return q("ditto -c -k --norsrc --keepParent " + p.b + " " + p.zip)
	}

	t.Run("builds A, and B's release in the feed A reads", func(t *testing.T) {
		dir := inTempDir(t)
		checkout(t, dir)
		_, log := rehearsalTools(t)
		cfg := rehearsalConfigIn(dir, caseNormal)
		p := cfg.paths()

		var err error

		stdout := captureStdout(t, func() { err = rehearse(cfg) })

		require.NoError(t, err)

		lines := toolLines(t, log)
		requireLines(t, append(built(cfg), zipped(cfg)), lines)

		key := stampedKey(t, lines[2])
		assert.Equal(t, key, stampedKey(t, lines[7]), "A and B trust the same key")

		public, err := update.ParseKey([]byte(key))
		require.NoError(t, err)
		require.NoError(t, verifyRelease(p.feed, rehearsalTo, public), "B's release verifies with the key A trusts")

		assert.DirExists(t, p.a)
		assert.NoDirExists(t, p.stage, "B's bundle is gone once zipped")

		app := "A/" + brand.Name + ".app/Contents/"
		assert.Equal(t, []string{
			app + "Info.plist",
			app + "MacOS/" + brand.Slug,
			app + "Resources/" + brandgen.IconFile + ".icns",
			app + "_CodeSignature/CodeResources",
			"feed/" + update.AssetNames(rehearsalTo).Zip,
			"feed/" + update.ManifestName,
		}, filesUnder(t, cfg.root), "the rehearsal holds A and B's release alone: no key, no scratch file")

		data, err := appdata.RehearsalDir(runtime.GOOS, rehearsalHome, os.Getenv)
		require.NoError(t, err)

		installed := "/Applications/" + brand.Name + ".app"
		aside := "~/" + brand.Name + "-before-rehearsal.app"

		assert.Contains(t, stdout, strings.Join([]string{
			"✅ The rehearsal is ready (case=normal): A is 0.0.1, B is 0.0.2.",
			"  A:    " + p.a,
			"  feed: " + p.feed + " (B's zip and its signed manifest, which A reads as its releases)",
			"  data: " + paths.Tilde(data, rehearsalHome) + " (both builds run on it, never on your own data)",
			"",
			"To rehearse in place (quit " + brand.Name + " first; a rehearsal build updates itself in " + cfg.root + "):",
			"  1. open " + p.a,
			"  2. In the menu bar menu, choose Check for Updates…, then Restart to Update (0.0.2).",
			"",
			"What should happen: " + brand.Name + " quits, and 0.0.2 opens in its place in a few seconds, its data " +
				"backed up first beside the database.",
			"",
		}, "\n"), "the in-place route comes first")

		assert.Contains(t, stdout, strings.Join([]string{
			"  1. [ ! -e " + installed + " ] || mv " + installed + " " + aside,
			"  2. ditto " + p.a + " " + installed,
			"  3. open " + installed,
			"  4. Do step 2 above.",
			"Then quit it, and put your copy back:",
			"  rm -rf " + installed,
			"  [ ! -e " + aside + " ] || mv " + aside + " " + installed,
		}, "\n"), "the Applications route moves the user's copy aside, and says how to put it back")
	})

	t.Run("each run trusts a key of its own", func(t *testing.T) {
		dir := inTempDir(t)
		checkout(t, dir)
		_, log := rehearsalTools(t)
		cfg := rehearsalConfigIn(dir, caseNormal)

		captureStdout(t, func() { require.NoError(t, rehearse(cfg)) })
		first := stampedKey(t, toolLines(t, log)[2])

		captureStdout(t, func() { require.NoError(t, rehearse(cfg)) })
		lines := toolLines(t, log)
		assert.NotEqual(t, first, stampedKey(t, lines[len(lines)-11]))
	})

	t.Run("case=tampered leaves a zip that doesn't match its manifest", func(t *testing.T) {
		dir := inTempDir(t)
		checkout(t, dir)
		_, log := rehearsalTools(t)
		cfg := rehearsalConfigIn(dir, caseTampered)
		p := cfg.paths()

		var err error

		stdout := captureStdout(t, func() { err = rehearse(cfg) })

		require.NoError(t, err)
		requireLines(t, append(built(cfg), zipped(cfg)), toolLines(t, log))

		public, err := update.ParseKey([]byte(stampedKey(t, toolLines(t, log)[2])))
		require.NoError(t, err)
		require.Error(t, verifyRelease(p.feed, rehearsalTo, public), "the app refuses the zip")
		assert.Contains(t, stdout, "Changed one byte of "+filepath.Base(p.zip))
		assert.Contains(t, stdout, "What should happen: the menu says Download failed, nothing restarts, and 0.0.1 stays.")
	})

	t.Run("case=broken leaves B failing its signature check", func(t *testing.T) {
		dir := inTempDir(t)
		checkout(t, dir)
		_, log := rehearsalTools(t)
		cfg := rehearsalConfigIn(dir, caseBroken)
		p := cfg.paths()

		var err error

		stdout := captureStdout(t, func() { err = rehearse(cfg) })

		require.NoError(t, err)
		requireLines(t, append(built(cfg), q("codesign --verify --deep --strict "+p.b), zipped(cfg)), toolLines(t, log))

		public, err := update.ParseKey([]byte(stampedKey(t, toolLines(t, log)[2])))
		require.NoError(t, err)
		require.NoError(t, verifyRelease(p.feed, rehearsalTo, public), "the app takes the zip; the helper refuses B")
		assert.Contains(t, stdout, "Broke "+p.b+" after signing it")
		assert.Contains(t, stdout, "opens again in a few seconds")
	})

	t.Run("a break the signature check misses fails", func(t *testing.T) {
		dir := inTempDir(t)
		checkout(t, dir)
		tools, _ := rehearsalTools(t)
		fakeTool(t, tools, "codesign", `for a; do last="$a"; done
case "$1:$last" in --force:*.app) /bin/mkdir -p "$last/Contents/_CodeSignature" ;; esac`)

		var err error

		captureStdout(t, func() { err = rehearse(rehearsalConfigIn(dir, caseBroken)) })

		require.ErrorIs(t, err, errStillSigned)
	})

	t.Run("signs ad hoc, and with a keychain put on the search list and back", func(t *testing.T) {
		dir := inTempDir(t)
		checkout(t, dir)
		_, log := rehearsalTools(t)
		cfg := rehearsalConfigIn(dir, caseNormal)
		cfg.identity = adHoc

		captureStdout(t, func() { require.NoError(t, rehearse(cfg)) })
		assert.Contains(t, toolLines(t, log), "codesign --force --sign - "+cfg.paths().a)

		dir = inTempDir(t)
		checkout(t, dir)
		_, log = rehearsalTools(t)
		cfg = rehearsalConfigIn(dir, caseNormal)
		cfg.keychain = keychainFile(t)

		captureStdout(t, func() { require.NoError(t, rehearse(cfg)) })

		lines := toolLines(t, log)
		assert.Equal(t, "security list-keychains -d user -s "+cfg.keychain+
			" /Library/Keychains/My Keys.keychain-db /Library/Keychains/System.keychain", lines[3])
		assert.Contains(t, lines, "codesign --force --sign "+testIdentity+" --keychain "+cfg.keychain+
			" --timestamp=none "+cfg.paths().a)
		assert.Equal(t, originalKeychains, readFile(t, os.Getenv("KEYCHAIN_LIST")), "the search list is back as it was")
	})

	refusals := []struct {
		name    string
		change  func(cfg *rehearsalConfig)
		wantErr error
	}{
		{name: "no case", change: func(cfg *rehearsalConfig) { cfg.kase = "" }, wantErr: errRehearsalCase},
		{name: "an unknown case", change: func(cfg *rehearsalConfig) { cfg.kase = "slow" }, wantErr: errRehearsalCase},
		{name: "no identity", change: func(cfg *rehearsalConfig) { cfg.identity = "" }, wantErr: errNoIdentity},
		{name: "a relative keychain", change: func(cfg *rehearsalConfig) { cfg.keychain = "ci.keychain-db" }, wantErr: errKeychainPath},
		{name: "a quote in the path", change: func(cfg *rehearsalConfig) { cfg.root = "/repos/it's/bin/rehearsal" }, wantErr: errQuotedPath},
	}

	for _, tt := range refusals {
		t.Run("refuses "+tt.name+" before any tool runs", func(t *testing.T) {
			dir := inTempDir(t)
			checkout(t, dir)
			_, log := rehearsalTools(t)
			cfg := rehearsalConfigIn(dir, caseNormal)
			tt.change(&cfg)

			require.ErrorIs(t, rehearse(cfg), tt.wantErr)
			assert.NoFileExists(t, log, "no tool ran")
			assert.NoDirExists(t, filepath.Join(dir, "bin"))
		})
	}
}

func TestRehearsalMeta(t *testing.T) {
	t.Parallel()

	keys := releaseKeyPair(3)
	meta := rehearsalMeta(buildMeta{version: "v1.0.0-2-gabc1234", release: "1.0.0", bundleVersion: "1.0.0"},
		"0.0.2", "/repo/bin/rehearsal/feed", keys.public)

	assert.Equal(t, buildMeta{
		version: "v1.0.0-2-gabc1234", release: "0.0.2", bundleVersion: "0.0.2", flavor: flavorRehearsal,
		updateFeed: "/repo/bin/rehearsal/feed", updateKey: update.EncodeKey(keys.public),
	}, meta)
	assert.True(t, strings.HasSuffix(versionFlags(meta), " -X main.flavor=rehearsal -X 'main.updateFeed=/repo/bin/rehearsal/feed'"+
		" -X main.updateKey="+update.EncodeKey(keys.public)))
}
