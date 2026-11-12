package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// magefile is a magefiles package with every kind of declaration magex's discovery
// weighs: a namespace declared apart from its methods, functions with and without a
// target's signature, and functions magex leaves to its own built-ins.
const magefile = `//go:build mage

package main

import (
	"context"
	"time"

	"github.com/magefile/mage/mg"
)

// Deploy ships it.
// Twice, if need be.
func Deploy() error { return nil }

// Wait waits.
func Wait(ctx context.Context, d time.Duration) {}

// Lint is magex's own lint, which wins.
func Lint() {}

// BuildDefault wraps build:default.
func BuildDefault() {}

func Returns() (int, error) { return 0, nil }
func Variadic(args ...string) {}
func unexported() {}

// CI runs the pipeline.
func (Pipeline) CI(ctx context.Context) error { return nil }
func (*Pipeline) Nightly() {}
func (Other) Skipped() {}

type Other struct{}
`

// magefileTypes declares the namespace in another file, as magex reads them together.
const magefileTypes = "package main\n\nimport \"github.com/magefile/mage/mg\"\n\ntype Pipeline mg.Namespace\n"

// helpBuiltins are magex 1.30.2's built-ins, as its help:commands json=true printed them,
// kept one command to a line with the fields the app reads.
func helpBuiltins(t *testing.T) builtins {
	t.Helper()

	b, err := parseBuiltins([]byte(fixture(t, "magex-1.30.2-help-commands.txt")))
	require.NoError(t, err)

	return b
}

func TestParseBuiltins(t *testing.T) {
	t.Parallel()

	b := helpBuiltins(t)
	require.Len(t, b.list, 184)
	assert.Equal(t, "bench", b.list[0].Name, "in magex's order")
	assert.True(t, b.names["upgrade"], "an alias is a name")
	assert.True(t, b.namespaces["agentos"])
	assert.True(t, b.names["version:bump"])

	_, err := parseBuiltins([]byte("not json"))
	require.Error(t, err)

	_, err = parseBuiltins([]byte(`{"commands": []}`))
	require.Error(t, err, "a magex that lists nothing")

	got, err := parseBuiltins([]byte("banner text\n" + `{"commands": [{"name": "Test"}, {"name": "test"}]}`))
	require.NoError(t, err)
	assert.Len(t, got.list, 1, "read from the JSON on, each name once, lowercased")
}

func TestReadMagefiles(t *testing.T) {
	t.Parallel()

	dir := folder(t, map[string]string{
		"magefiles/main.go": magefile, "magefiles/types.go": magefileTypes, "magefiles/main_test.go": "package main\nfunc TestX() {}\n",
		"magefiles/broken.go": "package main\nfunc Broken( {", "magefiles/sub/nested.go": "package sub\nfunc Nested() {}\n",
		"magefiles/README.md": "notes",
	})

	mf := readMagefiles(dir)
	require.NoError(t, mf.readErr)
	require.NoError(t, mf.sumErr)
	require.True(t, mf.found)

	assert.Equal(t, []customTarget{
		{name: "builddefault", description: "BuildDefault wraps build:default."},
		{name: "deploy", description: "Deploy ships it. Twice, if need be."},
		{name: "lint", description: "Lint is magex's own lint, which wins."},
		{name: "pipeline:ci", description: "CI runs the pipeline."},
		{name: "pipeline:nightly"},
		{name: "wait", description: "Wait waits.", params: true},
	}, mf.targets, "only targets, named as magex runs them; a file that doesn't parse, tests, and subfolders left out")

	paths := make([]string, 0, len(mf.files))
	for _, f := range mf.files {
		paths = append(paths, f.Path)
		assert.Len(t, f.SHA256, 64, f.Path)
	}

	assert.Equal(t, []string{"README.md", "broken.go", "main.go", "main_test.go", "sub/nested.go", "types.go"}, paths,
		"every file under magefiles/ is summed, by path")

	scripts := byName(magexScripts(helpBuiltins(t), mf))
	assert.False(t, scripts["lint"].entry.Custom, "magex's own lint runs instead")
	assert.NotContains(t, scripts, "builddefault", "it wraps a built-in")

	deploy := scripts["deploy"]
	assert.True(t, deploy.entry.Custom)
	assert.True(t, deploy.entry.Runnable)
	assert.False(t, deploy.entry.Args)
	assert.Len(t, deploy.hash, 64, "the repository's own code needs trust")
	assert.Equal(t, "magex deploy", deploy.entry.Command)
	assert.Equal(t, "Takes arguments, which "+brand.Name+" can't pass yet.", scripts["wait"].entry.Why)

	test := scripts["test"]
	assert.Empty(t, test.hash, "a built-in needs no trust")
	assert.True(t, test.entry.Args)
	assert.True(t, scripts["version:bump"].entry.Release, "Release does this")
}

func TestReadMagefilesFallsBackToMagefileGo(t *testing.T) {
	t.Parallel()

	mf := readMagefiles(folder(t, map[string]string{"magefile.go": magefile + "\ntype Pipeline mg.Namespace\n"}))
	require.NoError(t, mf.readErr)
	assert.Equal(t, []fileSum{{Path: "magefile.go", Kind: "file", SHA256: mf.files[0].SHA256}}, mf.files)
	assert.Len(t, mf.targets, 6)

	assert.False(t, readMagefiles(t.TempDir()).found, "no magefiles")
	assert.Empty(t, fingerprint(t.TempDir()))
}

func TestMagefilesHashFollowsEveryFile(t *testing.T) {
	t.Parallel()

	dir := folder(t, map[string]string{"magefiles/main.go": magefile, "magefiles/types.go": magefileTypes})
	hash := func() string {
		sc := byName(magexScripts(helpBuiltins(t), readMagefiles(dir)))["deploy"]
		require.True(t, sc.entry.Runnable)

		return sc.hash
	}

	first := hash()
	assert.Equal(t, first, hash(), "the same files hash the same")

	testutil.WriteFile(t, filepath.Join(dir, "magefiles", "data", "config.yaml"), "a file the build reads")

	second := hash()
	assert.NotEqual(t, first, second, "a new file anywhere under magefiles/")

	testutil.WriteFile(t, filepath.Join(dir, "magefiles", "types.go"), magefileTypes+"// changed\n")
	assert.NotEqual(t, second, hash(), "a changed file that holds no target")

	require.NoError(t, os.Symlink(filepath.Join(dir, "magefiles", "types.go"), filepath.Join(dir, "magefiles", "link.go")))
	mf := readMagefiles(dir)
	require.NoError(t, mf.sumErr)
	assert.Contains(t, mf.files, fileSum{
		Path: "link.go", Kind: "link", Link: filepath.Join(dir, "magefiles", "types.go"),
		SHA256: mf.files[len(mf.files)-1].SHA256,
	}, "a link by where it points and what it holds")
}

func TestMagefilesTooLarge(t *testing.T) {
	t.Parallel()

	dir := folder(t, map[string]string{"magefiles/main.go": magefile, "magefiles/types.go": magefileTypes})

	f, err := os.Create(filepath.Join(dir, "magefiles", "big.bin")) //nolint:gosec // the test's own folder
	require.NoError(t, err)
	require.NoError(t, f.Truncate(maxMagefileBytes+1))
	require.NoError(t, f.Close())

	deploy := byName(magexScripts(helpBuiltins(t), readMagefiles(dir)))["deploy"]
	assert.False(t, deploy.entry.Runnable)
	assert.Equal(t, "Its magefiles are too large to check: more than 64 MB.", deploy.entry.Why)
}

func TestFingerprintSeesAChange(t *testing.T) {
	t.Parallel()

	dir := folder(t, map[string]string{"magefiles/main.go": magefile})
	before := fingerprint(dir)
	assert.Equal(t, before, fingerprint(dir))

	testutil.WriteFile(t, filepath.Join(dir, "magefiles", "main.go"), magefile+"// longer\n")
	assert.NotEqual(t, before, fingerprint(dir))
}

func TestToolFacts(t *testing.T) {
	t.Parallel()

	found := ToolFactsOf(integrations.Tool{
		State: integrations.StateFound, Copy: integrations.Copy{Path: "/opt/bin/magex", Version: integrations.Version{Raw: "1.30.2"}},
	})
	assert.True(t, found.Usable())
	assert.Equal(t, "/opt/bin/magex@1.30.2", found.Key())

	for want, facts := range map[string]integrations.CopyFacts{
		"mage-x is turned off in Settings → Integrations.":                                    {Path: "/x", State: integrations.StateFound, Off: true},
		"magex isn't installed. Install it in Settings → Integrations.":                       {State: integrations.StateMissing},
		"This magex is too old for " + brand.Name + ". Update it in Settings → Integrations.": {Path: "/x", State: integrations.StateTooOld},
		"magex won't run. Check it in Settings → Integrations.":                               {Path: "/x", State: integrations.StateWontRun},
	} {
		tool := ToolFacts{CopyFacts: facts}
		assert.False(t, tool.Usable(), want)
		assert.Equal(t, want, tool.why())
	}

	off := ToolFactsOf(integrations.Tool{Switch: integrationprefs.SwitchOff})
	assert.True(t, off.Off)
}

func TestShadows(t *testing.T) {
	t.Parallel()

	b := helpBuiltins(t)

	for name, want := range map[string]bool{
		"lint": true, "upgrade": true, "agentos": true, "builddefault": true, "testrace": true,
		"deploy": false, "pipeline:ci": false, "buildx": false,
	} {
		assert.Equal(t, want, b.shadows(name), name)
	}

	assert.True(t, strings.HasPrefix(commonTargets()[0], "format"))
}
