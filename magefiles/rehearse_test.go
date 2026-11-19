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
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/update"
)

// rehearsalHome is the home folder the rehearsal tests name the data directory from.
const rehearsalHome = "/Users/example"

// brokenCheck reports whether codesign's arguments check the signature of a bundle whose
// app icon case=broken changed, a check the real codesign fails.
func brokenCheck(args []string) bool {
	if args[0] != "--verify" || !strings.HasSuffix(lastArg(args), ".app") {
		return false
	}

	icon := filepath.Join(lastArg(args), "Contents", "Resources", brandgen.IconFile+".icns")
	body, err := os.ReadFile(icon) //nolint:gosec // the bundle the rehearsal built

	return err == nil && strings.Contains(string(body), "case=broken")
}

// rehearsalRig returns a rig whose checkout holds what the app targets need, with the fake
// tools of a release, as releaseRig makes them, and a codesign whose check of a bundle
// fails once case=broken has changed its app icon.
func rehearsalRig(t *testing.T) (*rig, *releaseState) {
	t.Helper()

	g, state := releaseRig(t, releaseFake{})
	g.tool("codesign", codesignAnswer(t, brokenCheck))

	return g, state
}

// rehearsalConfigIn is the rehearsal config the tests build kase from, in dir's
// bin/rehearsal, signed as testIdentity.
func rehearsalConfigIn(dir, kase string) rehearsalConfig {
	return rehearsalConfig{
		kase: kase, identity: testIdentity, root: filepath.Join(dir, "bin", "rehearsal"), home: rehearsalHome,
	}
}

// stampedKey is the key a rehearsal build's go build line stamps, read from the line.
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

func TestRehearse(t *testing.T) {
	t.Parallel()

	q := regexp.QuoteMeta

	goBuild := func(version, feed, exe string) string {
		return q("go build -tags desktop,production -trimpath -buildvcs=false -ldflags -s -w -X main.version=") +
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
		t.Parallel()

		g, _ := rehearsalRig(t)
		cfg := rehearsalConfigIn(g.dir, caseNormal)
		p := cfg.paths()
		r := g.runner()

		require.NoError(t, r.rehearse(cfg))

		lines := toolLines(g)
		requireLines(t, append(built(cfg), zipped(cfg)), lines)

		key := stampedKey(t, lines[2])
		assert.Equal(t, key, stampedKey(t, lines[7]), "A and B trust the same key")

		public, err := update.ParseKey([]byte(key))
		require.NoError(t, err)
		require.NoError(t, r.verifyRelease(p.feed, rehearsalTo, public), "B's release verifies with the key A trusts")

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

		data, err := appdata.RehearsalDir(runtime.GOOS, rehearsalHome, g.getenv)
		require.NoError(t, err)

		installed := "/Applications/" + brand.Name + ".app"
		aside := "~/" + brand.Name + "-before-rehearsal.app"
		stdout := g.out.String()

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
		t.Parallel()

		g, _ := rehearsalRig(t)
		cfg := rehearsalConfigIn(g.dir, caseNormal)
		r := g.runner()

		require.NoError(t, r.rehearse(cfg))
		first := stampedKey(t, toolLines(g)[2])

		require.NoError(t, r.rehearse(cfg))

		lines := toolLines(g)
		assert.NotEqual(t, first, stampedKey(t, lines[len(lines)-11]))
	})

	t.Run("case=tampered leaves a zip that doesn't match its manifest", func(t *testing.T) {
		t.Parallel()

		g, _ := rehearsalRig(t)
		cfg := rehearsalConfigIn(g.dir, caseTampered)
		p := cfg.paths()
		r := g.runner()

		require.NoError(t, r.rehearse(cfg))
		requireLines(t, append(built(cfg), zipped(cfg)), toolLines(g))

		public, err := update.ParseKey([]byte(stampedKey(t, toolLines(g)[2])))
		require.NoError(t, err)
		require.Error(t, r.verifyRelease(p.feed, rehearsalTo, public), "the app refuses the zip")
		assert.Contains(t, g.out.String(), "Changed one byte of "+filepath.Base(p.zip))
		assert.Contains(t, g.out.String(), "What should happen: the menu says Download failed, nothing restarts, and 0.0.1 stays.")
	})

	t.Run("case=broken leaves B failing its signature check", func(t *testing.T) {
		t.Parallel()

		g, _ := rehearsalRig(t)
		cfg := rehearsalConfigIn(g.dir, caseBroken)
		p := cfg.paths()
		r := g.runner()

		require.NoError(t, r.rehearse(cfg))
		requireLines(t, append(built(cfg), q("codesign --verify --deep --strict "+p.b), zipped(cfg)), toolLines(g))

		public, err := update.ParseKey([]byte(stampedKey(t, toolLines(g)[2])))
		require.NoError(t, err)
		require.NoError(t, r.verifyRelease(p.feed, rehearsalTo, public), "the app takes the zip; the helper refuses B")
		assert.Contains(t, g.out.String(), "Broke "+p.b+" after signing it")
		assert.Contains(t, g.out.String(), "opens again in a few seconds")
	})

	t.Run("a break the signature check misses fails", func(t *testing.T) {
		t.Parallel()

		g, _ := rehearsalRig(t)
		g.tool("codesign", codesignAnswer(t, nil))

		require.ErrorIs(t, g.runner().rehearse(rehearsalConfigIn(g.dir, caseBroken)), errStillSigned)
	})

	t.Run("signs ad hoc, and with a keychain put on the search list and back", func(t *testing.T) {
		t.Parallel()

		g, _ := rehearsalRig(t)
		cfg := rehearsalConfigIn(g.dir, caseNormal)
		cfg.identity = adHoc

		require.NoError(t, g.runner().rehearse(cfg))
		assert.Contains(t, toolLines(g), "codesign --force --sign - "+cfg.paths().a)

		g, state := rehearsalRig(t)
		cfg = rehearsalConfigIn(g.dir, caseNormal)
		cfg.keychain = keychainFile(t)

		require.NoError(t, g.runner().rehearse(cfg))

		lines := toolLines(g)
		assert.Equal(t, "security list-keychains -d user -s "+cfg.keychain+
			" /Library/Keychains/My Keys.keychain-db /Library/Keychains/System.keychain", lines[3])
		assert.Contains(t, lines, "codesign --force --sign "+testIdentity+" --keychain "+cfg.keychain+
			" --timestamp=none "+cfg.paths().a)
		assert.Equal(t, originalKeychains, state.keychains.printed(), "the search list is back as it was")
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
			t.Parallel()

			g, _ := rehearsalRig(t)
			cfg := rehearsalConfigIn(g.dir, caseNormal)
			tt.change(&cfg)

			require.ErrorIs(t, g.runner().rehearse(cfg), tt.wantErr)
			assert.Empty(t, g.lines(), "no tool ran")
			assert.NoDirExists(t, g.path("bin"))
		})
	}
}

func TestRehearseFromEnv(t *testing.T) {
	t.Parallel()

	home := func() (string, error) { return rehearsalHome, nil }

	t.Run("builds the case magex names, signed as the environment says, in bin/rehearsal", func(t *testing.T) {
		t.Parallel()

		g, state := rehearsalRig(t)
		keychain := keychainFile(t)

		g.setenv(mageArgsJSON, `["case=tampered"]`)
		g.setenv(envCodesignIdentity, testIdentity)
		g.setenv(envCodesignKeychain, keychain+"/") // cleaned, as app:package cleans it

		require.NoError(t, g.runner().rehearseFromEnv(home))

		cfg := rehearsalConfigIn(g.dir, caseTampered)
		assert.DirExists(t, cfg.paths().a)
		assert.Contains(t, toolLines(g), "codesign --force --sign "+testIdentity+" --keychain "+keychain+
			" --timestamp=none "+cfg.paths().a)
		assert.Contains(t, g.out.String(), "✅ The rehearsal is ready (case=tampered)")
		assert.Equal(t, originalKeychains, state.keychains.printed())
	})

	t.Run("refuses a rehearsal without an identity before anything builds", func(t *testing.T) {
		t.Parallel()

		g, _ := rehearsalRig(t)
		g.setenv(mageArgsJSON, `["case=normal"]`)

		require.ErrorIs(t, g.runner().rehearseFromEnv(home), errNoIdentity)
		assert.Empty(t, g.lines())
	})

	t.Run("needs a home folder for the steps it prints", func(t *testing.T) {
		t.Parallel()

		g, _ := rehearsalRig(t)

		err := g.runner().rehearseFromEnv(func() (string, error) { return "", appdata.ErrNoHomeDir })

		require.ErrorIs(t, err, appdata.ErrNoHomeDir)
		assert.Contains(t, err.Error(), "find the home folder")
		assert.Empty(t, g.lines())
	})
}

func TestRehearseFailures(t *testing.T) {
	t.Parallel()

	t.Run("stale brand files stop it before anything builds", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		releaseTools(g, releaseFake{})

		require.ErrorIs(t, g.runner().rehearse(rehearsalConfigIn(g.dir, caseNormal)), brandgen.ErrStale)
		assert.Empty(t, g.lines())
		assert.NoDirExists(t, g.path("bin"))
	})

	t.Run("a bundle with no icon to break", func(t *testing.T) {
		t.Parallel()

		err := newRig(t).runner().breakBundle(caseBroken, filepath.Join(t.TempDir(), bundleName))

		require.ErrorIs(t, err, fs.ErrNotExist)
		assert.Contains(t, err.Error(), "break the bundle")
	})

	t.Run("no zip to tamper with", func(t *testing.T) {
		t.Parallel()

		p := rehearsalConfigIn(t.TempDir(), caseTampered).paths()

		err := newRig(t).runner().tamperZip(caseTampered, p, releaseKeyPair(1).public)

		require.ErrorIs(t, err, fs.ErrNotExist)
		assert.Contains(t, err.Error(), "tamper with the zip")
	})

	t.Run("an empty zip has no byte to change", func(t *testing.T) {
		t.Parallel()

		p := rehearsalConfigIn(t.TempDir(), caseTampered).paths()
		testutil.WriteFile(t, p.zip, "")

		require.ErrorIs(t, newRig(t).runner().tamperZip(caseTampered, p, releaseKeyPair(1).public), errStillMatches)
	})

	t.Run("a zip it can't write", func(t *testing.T) {
		t.Parallel()

		skipAsRoot(t)

		p := rehearsalConfigIn(t.TempDir(), caseTampered).paths()
		testutil.WriteFileMode(t, p.zip, "the zip", 0o400)

		err := newRig(t).runner().tamperZip(caseTampered, p, releaseKeyPair(1).public)

		require.ErrorIs(t, err, fs.ErrPermission)
		assert.Equal(t, "the zip", readFile(t, p.zip))
	})

	t.Run("no data directory to name", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		cfg := rehearsalConfigIn(t.TempDir(), caseNormal)
		cfg.home = ""

		err := g.runner().printRehearsal(cfg, cfg.paths())

		require.ErrorIs(t, err, appdata.ErrNoHomeDir)
		assert.Contains(t, err.Error(), "find the rehearsal's data directory")
		assert.Empty(t, g.out.String())
	})

	t.Run("a step that fails stops the rest", func(t *testing.T) {
		t.Parallel()

		g, _ := rehearsalRig(t)
		g.tool("ditto", exits(1))

		err := g.runner().rehearse(rehearsalConfigIn(g.dir, caseNormal))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "zip the bundle")
		assert.NotContains(t, g.out.String(), "The rehearsal is ready")
	})
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
