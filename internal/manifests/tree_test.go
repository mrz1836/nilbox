package manifests

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/gitread"
)

// entry is a regular file at file in a fake tree, size bytes long, its object named for
// its path.
func entry(file string, size int64) gitread.TreeEntry {
	return gitread.TreeEntry{Path: file, Mode: "100644", Type: "blob", Object: objectFor(file), Size: size}
}

// objectFor is a fake object ID for the file at file, the same in every run.
func objectFor(file string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(file)))
}

// paths returns each entry's path, in order.
func entryPaths(entries []gitread.TreeEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Path)
	}

	return out
}

func TestPickerKeepsWhatDepsReads(t *testing.T) {
	t.Parallel()

	p := newPicker()

	for _, file := range []string{
		goModFile, toolsGoMod, "web/package.json", goSumFile, "web/pnpm-lock.yaml", "package-lock.json", "yarn.lock",
		"bun.lock", "a/bun.lockb", goWorkFile, "web/pnpm-workspace.yaml", ".mage.yaml", ".github/env/10-mage-x.env",
		"magefiles/magefile.go", "magefiles/go.mod", "my app/package.json", "vendored/go.mod", "x/testdatas/go.mod",
	} {
		assert.True(t, p.wanted(file), "%q is read", file)
	}

	for _, file := range []string{
		"README.md", "vendor/github.com/acme-org/lib/go.mod", "web/node_modules/left-pad/package.json",
		"internal/testdata/go.mod", "testdata/pnpm-lock.yaml", "a/vendor/b/c/go.sum", "vendor", "go.mod.bak",
		"magefilesx/magefile.go", "tools/magefiles/magefile.go", ".github/env/00-core.env", "npm-shrinkwrap.json",
	} {
		assert.False(t, p.wanted(file), "%q isn't read", file)
	}
}

func TestPickerSortsATreesFiles(t *testing.T) {
	t.Parallel()

	link := entry("linked/package.json", 9)
	link.Mode = "120000"

	submodule := gitread.TreeEntry{Path: "third_party/lib", Mode: "160000", Type: "commit", Object: objectFor("lib"), Size: -1}

	files := newPicker().files([]gitread.TreeEntry{
		entry("web/packages/ui/package.json", 10), entry(toolsGoMod, 10), entry(goModFile, 10), entry(goSumFile, 10),
		entry("web/package.json", 10), entry("web/pnpm-lock.yaml", 10), entry("web/pnpm-workspace.yaml", 10),
		entry(goWorkFile, 10), entry("magefiles/magefile.go", 10), entry("-odd/go.mod", 10), entry("-odd/go.sum", 10),
		link, submodule, entry("vendor/x/go.mod", 10),
	})

	assert.Equal(t, []string{goModFile, toolsGoMod, "web/package.json", "web/packages/ui/package.json"},
		entryPaths(files.manifests), "the shallowest first; no link, submodule, vendored module, or path git isn't given")
	assert.Equal(t, map[string]bool{goSumFile: true, "web/pnpm-lock.yaml": true}, files.lockfiles)
	assert.Equal(t, []string{goWorkFile, "web/pnpm-workspace.yaml"}, files.workspaces)
	assert.True(t, files.magex)
	assert.True(t, files.pnpmWorkspace())
	assert.False(t, files.truncated)

	empty := newPicker().files(nil)
	assert.NotNil(t, empty.manifests)
	assert.NotNil(t, empty.workspaces)
	assert.False(t, empty.magex)
	assert.False(t, empty.pnpmWorkspace())
}

func TestModulesPastTheCapAreTruncated(t *testing.T) {
	t.Parallel()

	entries := make([]gitread.TreeEntry, 0, maxModules+6)
	entries = append(entries, entry("z/deep/er/go.mod", 1))

	for i := range maxModules + 4 {
		entries = append(entries, entry(fmt.Sprintf("m%03d/go.mod", i), 1))
	}

	entries = append(entries, entry(goModFile, 1))

	files := newPicker().files(entries)
	require.Len(t, files.manifests, maxModules)
	assert.True(t, files.truncated)
	assert.Equal(t, goModFile, files.manifests[0].Path, "the top module is read first")
	assert.Equal(t, "m198/go.mod", files.manifests[maxModules-1].Path)
	assert.NotContains(t, entryPaths(files.manifests), "z/deep/er/go.mod", "the deepest are left out")
}

func TestLockfileFor(t *testing.T) {
	t.Parallel()

	files := newPicker().files([]gitread.TreeEntry{
		entry(goSumFile, 1), entry("pnpm-lock.yaml", 1), entry("apps/site/yarn.lock", 1),
		entry("apps/site/package-lock.json", 1), entry("libs/bun.lockb", 1), entry("libs/bun.lock", 1),
		entry("vendor/x/yarn.lock", 1),
	})

	tests := []struct {
		name     string
		kind     Kind
		file     string
		manager  Tool
		wantLock string
		wantTool Tool
	}{
		{name: "a go.sum beside", kind: KindGo, file: goModFile, wantLock: goSumFile},
		{name: "no go.sum beside, none above counts", kind: KindGo, file: toolsGoMod},
		{name: "the top's lockfile", kind: KindJS, file: packageJSONFile, wantLock: "pnpm-lock.yaml", wantTool: ToolPNPM},
		{
			name: "the nearest, above", kind: KindJS, file: "packages/ui/package.json", wantLock: "pnpm-lock.yaml",
			wantTool: ToolPNPM,
		},
		{
			name: "two in a folder, npm's before Yarn's", kind: KindJS, file: "apps/site/package.json",
			wantLock: "apps/site/package-lock.json", wantTool: ToolNPM,
		},
		{
			name: "two in a folder, the manager's own first", kind: KindJS, file: "apps/site/package.json", manager: ToolYarn,
			wantLock: "apps/site/yarn.lock", wantTool: ToolYarn,
		},
		{
			name: "the nearest folder wins over the manager", kind: KindJS, file: "apps/site/package.json",
			manager: ToolPNPM, wantLock: "apps/site/package-lock.json", wantTool: ToolNPM,
		},
		{
			name: "bun's text lockfile before its binary one", kind: KindJS, file: "libs/a/package.json",
			wantLock: "libs/bun.lock", wantTool: ToolBun,
		},
		{
			name: "a vendored lockfile counts for nothing", kind: KindJS, file: "vendor/x/package.json",
			wantLock: "pnpm-lock.yaml", wantTool: ToolPNPM,
		},
	}

	for _, tt := range tests {
		lock, tool := files.lockfileFor(tt.kind, tt.file, tt.manager)
		assert.Equal(t, tt.wantLock, lock, tt.name)
		assert.Equal(t, tt.wantTool, tool, tt.name)
	}

	none := newPicker().files([]gitread.TreeEntry{entry("web/package.json", 1)})
	lock, tool := none.lockfileFor(KindJS, "web/package.json", ToolNone)
	assert.Empty(t, lock)
	assert.Equal(t, ToolNone, tool)
}

// TestEveryFileDepsReadsIsOneGitReadPasses keeps the names Deps picks out of a tree and
// the names gitread lets it pass to git the same.
func TestEveryFileDepsReadsIsOneGitReadPasses(t *testing.T) {
	t.Parallel()

	for _, name := range []string{goModFile, packageJSONFile} {
		assert.True(t, gitread.ValidManifestPath("a/"+name), name)
	}

	assert.True(t, gitread.ValidLockfilePath("a/"+goSumFile))

	for _, l := range jsLockfiles() {
		assert.True(t, gitread.ValidLockfilePath("a/"+l.name), l.name)
	}
}

func TestUncountedAt(t *testing.T) {
	t.Parallel()

	for file, want := range map[string]Uncounted{
		goModFile:         UncountedNone,
		toolsGoMod:        UncountedNone,
		"fixtures/go.mod": UncountedFixture,
		".github/ci-tester/fixtures/action/go.mod": UncountedFixture,
		"src/__fixtures__/app/package.json":        UncountedFixture,
		"examples/with-config/go.mod":              UncountedExample,
		"docs/example/package.json":                UncountedExample,
		"examples/fixtures/go.mod":                 UncountedFixture,
		"fixtures.go/go.mod":                       UncountedNone,
		"my-examples/package.json":                 UncountedNone,
		"example-app/package.json":                 UncountedNone,
		"web/Examples/package.json":                UncountedNone,
		"fixtures":                                 UncountedNone,
		"internal/fixturesuite/go.mod":             UncountedNone,
	} {
		assert.Equal(t, want, uncountedAt(file), "%q", file)
	}
}
