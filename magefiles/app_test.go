//go:build mage

package main

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/brandgen"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// fakeWailsVersion is the wails version the fake go reports for the module go.mod pins.
const fakeWailsVersion = "v3.0.0-test.1"

// bindingsGenerator is a fake go's answer that writes one binding into the folder after
// -d, with the trailing space the real generator leaves on blank doc-comment lines.
func bindingsGenerator(t *testing.T) answer {
	t.Helper()

	return func(args []string) testutil.FakeRun {
		out := argAfter(args, "-d")
		mkdir(t, filepath.Join(out, "models"))

		return writes(filepath.Join(out, "models", "a.ts"), "/**\n * Doc. \n */\nexport const a = 1;\n")
	}
}

// fakeGo makes go a fake tool on the rig that answers `go list` with fakeWailsVersion and
// every other command with other.
func fakeGo(g *rig, other answer) {
	g.tool("go", func(args []string) testutil.FakeRun {
		if len(args) > 0 && args[0] == "list" {
			return testutil.FakeRun{Stdout: fakeWailsVersion + "\n"}
		}

		return other(args)
	})
}

// fakeLint makes golangci-lint at version 2.15.0 a fake tool on the rig, and pins that
// version in the rig's environment.
func fakeLint(g *rig) {
	g.setenv(lintVersionKey, "v2.15.0")
	g.tool("golangci-lint", func(args []string) testutil.FakeRun {
		if slices.Equal(args, []string{"version"}) {
			return testutil.FakeRun{Stdout: "golangci-lint has version 2.15.0 built with go1.26.0\n"}
		}

		return testutil.FakeRun{}
	})
}

// appTools makes a fake of every tool the app targets run on the rig: pnpm; a go that
// answers `go list` with fakeWailsVersion, writes a binary after -o, and writes one binding
// after -d; golangci-lint at the pinned version; plutil; codesign; open; and a pgrep that
// finds nothing running. Neither git nor ld is among them.
func appTools(g *rig) {
	for _, name := range []string{"pnpm", "plutil", "codesign", "open"} {
		g.fixed(name, testutil.FakeRun{})
	}

	g.tool("pgrep", exits(1))
	fakeLint(g)
	fakeGo(g, func(args []string) testutil.FakeRun {
		if out := argAfter(args, "-o"); out != "" {
			return writes(out, "binary")
		}

		if out := argAfter(args, "-d"); out != "" {
			mkdir(g.t, filepath.Join(out, "models"))

			return writes(filepath.Join(out, "models", "a.ts"), "export const a = 1;\n")
		}

		return testutil.FakeRun{}
	})
}

// checkout writes into dir what a checkout holds for the app targets: the generated
// brand files, current app icons, and the frontend directory.
func checkout(t *testing.T, dir string) {
	t.Helper()

	require.NoError(t, brandgen.Write(dir))

	for _, icon := range []string{brandgen.IconSetPath, brandgen.DevIconPath, brandgen.TrayIconPath} {
		testutil.WriteFile(t, filepath.Join(dir, filepath.FromSlash(icon)), "png")
	}

	require.NoError(t, brandgen.WriteIconStamp(dir))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, frontendDir), 0o750))
}

// requireProcessList skips a test that needs pgrep to see real processes when pgrep
// cannot list any, as inside a sandbox that hides the process table. A pattern that
// matches every command line finds at least the go command running the tests.
func requireProcessList(t *testing.T) {
	t.Helper()

	if err := exec.CommandContext(t.Context(), requireTool(t, "pgrep"), "-f", ".").Run(); err != nil { //nolint:gosec // fixed arguments
		t.Skipf("pgrep cannot list processes here: %v", err)
	}
}

// packageVars returns the names of the package-level variables declared in the Go file
// at path.
func packageVars(t *testing.T, path string) []string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	require.NoError(t, err)

	var names []string

	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}

		for _, spec := range gen.Specs {
			for _, name := range spec.(*ast.ValueSpec).Names {
				names = append(names, name.Name)
			}
		}
	}

	return names
}

func TestTreeFiles(t *testing.T) {
	t.Parallel()

	t.Run("missing root is an empty tree", func(t *testing.T) {
		t.Parallel()

		files, err := treeFiles(filepath.Join(t.TempDir(), "missing"))

		require.NoError(t, err)
		assert.Empty(t, files)
	})

	t.Run("regular files keyed by slash-separated path", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		testutil.WriteFile(t, filepath.Join(root, "a.ts"), "a")
		testutil.WriteFile(t, filepath.Join(root, "nested", "b.ts"), "b")
		require.NoError(t, os.Symlink("a.ts", filepath.Join(root, "link.ts")))

		files, err := treeFiles(root)

		require.NoError(t, err)
		assert.Equal(t, map[string][]byte{"a.ts": []byte("a"), "nested/b.ts": []byte("b")}, files)
	})

	t.Run("unreadable file", func(t *testing.T) {
		t.Parallel()

		skipAsRoot(t)

		root := t.TempDir()
		testutil.WriteFile(t, filepath.Join(root, "a.ts"), "a")
		require.NoError(t, os.Chmod(filepath.Join(root, "a.ts"), 0o000))

		_, err := treeFiles(root)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "read "+root)
	})
}

func TestTreeDiff(t *testing.T) {
	t.Parallel()

	t.Run("reports each difference, sorted", func(t *testing.T) {
		t.Parallel()

		want, got := t.TempDir(), t.TempDir()
		testutil.WriteFile(t, filepath.Join(want, "same.ts"), "x")
		testutil.WriteFile(t, filepath.Join(got, "same.ts"), "x")
		testutil.WriteFile(t, filepath.Join(want, "changed.ts"), "old")
		testutil.WriteFile(t, filepath.Join(got, "changed.ts"), "new")
		testutil.WriteFile(t, filepath.Join(got, "added", "new.ts"), "new")
		testutil.WriteFile(t, filepath.Join(want, "gone.ts"), "old")

		drift, err := treeDiff(want, got)

		require.NoError(t, err)
		assert.Equal(t, []string{"changed: changed.ts", "missing: added/new.ts", "stale: gone.ts"}, drift)
	})

	t.Run("identical trees", func(t *testing.T) {
		t.Parallel()

		want, got := t.TempDir(), t.TempDir()
		testutil.WriteFile(t, filepath.Join(want, "a.ts"), "a")
		testutil.WriteFile(t, filepath.Join(got, "a.ts"), "a")

		drift, err := treeDiff(want, got)

		require.NoError(t, err)
		assert.Empty(t, drift)
	})

	t.Run("unreadable tree", func(t *testing.T) {
		t.Parallel()

		skipAsRoot(t)

		bad := t.TempDir()
		testutil.WriteFile(t, filepath.Join(bad, "a.ts"), "a")
		require.NoError(t, os.Chmod(filepath.Join(bad, "a.ts"), 0o000))

		_, err := treeDiff(bad, t.TempDir())
		require.Error(t, err)

		_, err = treeDiff(t.TempDir(), bad)
		require.Error(t, err)
	})
}

func TestTidyBindings(t *testing.T) {
	t.Parallel()

	t.Run("tidies changed files, leaving tidy files alone", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		dirty := filepath.Join(root, "models", "a.ts")
		enum := filepath.Join(root, "item", "models.ts")
		clean := filepath.Join(root, "b.ts")

		testutil.WriteFile(t, dirty, "/**\n * Doc. \n *\t\n */\nexport const a = 1;  \n")
		testutil.WriteFile(t, enum, "export enum Kind {\n    KindPR = \"pr\",\n};\n")
		testutil.WriteFile(t, clean, "export const b = 2;\n")

		past := time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC)
		require.NoError(t, os.Chtimes(clean, past, past))

		require.NoError(t, tidyBindings(root))

		assert.Equal(t, "/**\n * Doc.\n *\n */\nexport const a = 1;\n", readFile(t, dirty))
		assert.Contains(t, readFile(t, enum), `export type Kind = "pr";`)

		info, err := os.Stat(clean)
		require.NoError(t, err)
		assert.True(t, info.ModTime().Equal(past), "a tidy file is not rewritten")
	})

	t.Run("missing root", func(t *testing.T) {
		t.Parallel()

		require.NoError(t, tidyBindings(filepath.Join(t.TempDir(), "missing")))
	})

	t.Run("read-only file", func(t *testing.T) {
		t.Parallel()

		skipAsRoot(t)

		root := t.TempDir()
		path := filepath.Join(root, "a.ts")
		testutil.WriteFile(t, path, "a \n")
		require.NoError(t, os.Chmod(path, 0o400))

		err := tidyBindings(root)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "tidy a.ts")
	})

	t.Run("unreadable file", func(t *testing.T) {
		t.Parallel()

		skipAsRoot(t)

		root := t.TempDir()
		testutil.WriteFile(t, filepath.Join(root, "a.ts"), "a \n")
		require.NoError(t, os.Chmod(filepath.Join(root, "a.ts"), 0o000))

		require.Error(t, tidyBindings(root))
	})
}

func TestTrimTrailingSpace(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "/**\n * Doc.\n *\n */\nexport const a = 1;\n",
		string(trimTrailingSpace([]byte("/**\n * Doc. \n *\t\n */\nexport const a = 1;  \n"))))
}

// generatedEnum is an enum as the binding generator writes one for a Go string type with
// constants: a $zero member first, then each constant, with a blank line between
// declaration groups.
const generatedEnum = `// This file is automatically generated. DO NOT EDIT

/**
 * Theme is the palette the app shows.
 */
export enum Theme {
    /**
     * The Go zero value for the underlying type of the enum.
     */
    $zero = "",

    /**
     * Theme choices.
     */
    ThemeSystem = "system",
    ThemeLight = "light",

    /**
     * DefaultTheme is the theme of a fresh database.
     */
    DefaultTheme = "system",
};

export interface Settings {
    "theme": Theme;
}
`

func TestUnionEnums(t *testing.T) {
	t.Parallel()

	t.Run("rewrites an enum as a union and a const object", func(t *testing.T) {
		t.Parallel()

		want := `// This file is automatically generated. DO NOT EDIT

/**
 * Theme is the palette the app shows.
 */
export type Theme = "system" | "light";

/**
 * The values of Theme, by the names of their Go constants.
 */
export const Theme = {
    /**
     * Theme choices.
     */
    ThemeSystem: "system",
    ThemeLight: "light",

    /**
     * DefaultTheme is the theme of a fresh database.
     */
    DefaultTheme: "system",
} as const;

export interface Settings {
    "theme": Theme;
}
`

		assert.Equal(t, want, string(unionEnums([]byte(generatedEnum))))
	})

	t.Run("keeps a constant whose value is the zero value", func(t *testing.T) {
		t.Parallel()

		got := string(unionEnums([]byte("export enum Stage {\n    $zero = \"\",\n    StageNone = \"\",\n" +
			"    StageActive = \"active\",\n};\n")))

		assert.Equal(t, "export type Stage = \"\" | \"active\";\n\n/**\n"+
			" * The values of Stage, by the names of their Go constants.\n */\nexport const Stage = {\n"+
			"    StageNone: \"\",\n    StageActive: \"active\",\n} as const;\n", got)
	})

	t.Run("leaves a file without enums alone", func(t *testing.T) {
		t.Parallel()

		body := "export interface A {\n    \"a\": string;\n}\n"

		assert.Equal(t, body, string(unionEnums([]byte(body))))
	})

	t.Run("leaves an enum that never closes alone", func(t *testing.T) {
		t.Parallel()

		body := "export enum Kind {\n    KindPR = \"pr\",\n"

		assert.Equal(t, body, string(unionEnums([]byte(body))))
	})
}

func TestEnsureDist(t *testing.T) {
	t.Parallel()

	t.Run("writes a placeholder when the frontend is not built", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)

		require.NoError(t, g.runner().ensureDist())

		assert.Equal(t, placeholderPage, readFile(t, g.path(distIndex)))
	})

	t.Run("keeps a built frontend", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		testutil.WriteFile(t, g.path(distIndex), "<html>built</html>")

		require.NoError(t, g.runner().ensureDist())

		assert.Equal(t, "<html>built</html>", readFile(t, g.path(distIndex)))
	})

	t.Run("frontend is not a directory", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		testutil.WriteFile(t, g.path(frontendDir), "")

		err := g.runner().ensureDist()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "create frontend/dist")
	})

	t.Run("dist is read-only", func(t *testing.T) {
		t.Parallel()

		skipAsRoot(t)

		g := newRig(t)
		dist := g.path(filepath.Dir(distIndex))
		require.NoError(t, os.MkdirAll(dist, 0o750))
		require.NoError(t, os.Chmod(dist, 0o500)) //nolint:gosec // a read-only directory is the fixture

		err := g.runner().ensureDist()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "write the placeholder frontend")
	})
}

func TestLintVersion(t *testing.T) {
	t.Parallel()

	t.Run("from the environment", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		g.setenv(lintVersionKey, "v9.9.9")

		got, err := g.runner().lintVersion()

		require.NoError(t, err)
		assert.Equal(t, "v9.9.9", got)
	})

	t.Run("no env file", func(t *testing.T) {
		t.Parallel()

		_, err := newRig(t).runner().lintVersion()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "read "+mageXEnvFile)
	})

	tests := []struct {
		name    string
		env     string
		want    string
		wantErr string
	}{
		{
			name: "pinned in the env file",
			env:  "# Tool pins\n  " + lintVersionKey + "=v2.15.0  \n",
			want: "v2.15.0",
		},
		{
			name: "an empty pin is skipped",
			env:  lintVersionKey + "=\n" + lintVersionKey + "=v2.1.0\n",
			want: "v2.1.0",
		},
		{
			name:    "no pin",
			env:     "MAGE_X_GOFUMPT_VERSION=v0.11.0\n",
			wantErr: "the tool version is not pinned: " + mageXEnvFile + " has no " + lintVersionKey,
		},
		{
			name:    "unreadable line",
			env:     strings.Repeat("x", 70*1024) + "\n",
			wantErr: "read " + mageXEnvFile + ": bufio.Scanner: token too long",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := newRig(t)
			testutil.WriteFile(t, g.path(mageXEnvFile), tt.env)

			got, err := g.runner().lintVersion()

			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLintVersionReadsTheRepositoryPin(t *testing.T) {
	t.Parallel()

	r, _ := realRunner(t, repoRoot(t), nil)

	got, err := r.lintVersion()

	require.NoError(t, err, "the synced MAGE-X env file must keep pinning golangci-lint")
	assert.Regexp(t, `^v\d+\.\d+\.\d+$`, got)
}

func TestLintOnPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version string
		want    bool
	}{
		{name: "the pinned version", version: "v2.15.0", want: true},
		{name: "another version", version: "v2.14.0", want: false},
		{name: "a version prefix", version: "v2.15", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := newRig(t)
			fakeLint(g)

			assert.Equal(t, tt.want, g.runner().lintOnPath(tt.version))
		})
	}

	t.Run("not installed", func(t *testing.T) {
		t.Parallel()

		assert.False(t, newRig(t).runner().lintOnPath("v2.15.0"))
	})
}

func TestAppLint(t *testing.T) {
	t.Parallel()

	lintArgs := "run --build-tags desktop --config .golangci.json --timeout 10m ./cmd/" + brand.Slug + "/... ./frontend/"

	t.Run("lints the desktop code with the pinned version on PATH", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		fakeLint(g)

		require.NoError(t, g.runner().lintDesktop())

		assert.Equal(t, []string{"version", lintArgs}, g.linesOf("golangci-lint"), "the version is checked first")
		assert.FileExists(t, g.path(distIndex), "the desktop embed needs a frontend to match")
	})

	t.Run("runs the pinned version when PATH has another", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		fakeLint(g)
		g.setenv(lintVersionKey, "v2.14.0")
		g.fixed("go", testutil.FakeRun{})

		require.NoError(t, g.runner().lintDesktop())

		assert.Equal(t, []string{"version"}, g.linesOf("golangci-lint"))
		assert.Equal(t, []string{"run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 " + lintArgs}, g.linesOf("go"))
	})

	t.Run("no pinned version", func(t *testing.T) {
		t.Parallel()

		err := newRig(t).runner().lintDesktop()

		require.ErrorIs(t, err, os.ErrNotExist)
		assert.Contains(t, err.Error(), "read "+mageXEnvFile)
	})

	t.Run("frontend is not a directory", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		testutil.WriteFile(t, g.path(frontendDir), "")

		require.Error(t, g.runner().lintDesktop())
	})
}

func TestAppBindings(t *testing.T) {
	t.Parallel()

	t.Run("generates trimmed bindings with the pinned wails CLI", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		fakeGo(g, bindingsGenerator(t))

		require.NoError(t, g.runner().generateBindings(bindingsDir))

		assert.Equal(t, "/**\n * Doc.\n */\nexport const a = 1;\n", readFile(t, g.path(bindingsDir+"/models/a.ts")))
		assert.Equal(t, []string{
			"list -m -f {{.Version}} " + wailsModule,
			"run " + wailsModule + "/cmd/wails3@" + fakeWailsVersion +
				" generate bindings -ts -i -f -tags desktop -d " + g.path(bindingsDir) + " ./cmd/" + brand.Slug,
		}, g.linesOf("go"))
	})

	t.Run("the generator fails", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		fakeGo(g, exits(1))

		err := g.runner().generateBindings(bindingsDir)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "generate bindings")
	})

	t.Run("go missing", func(t *testing.T) {
		t.Parallel()

		err := newRig(t).runner().generateBindings(bindingsDir)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "read the pinned "+wailsModule+" version")
	})

	t.Run("frontend is not a directory", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		testutil.WriteFile(t, g.path(frontendDir), "")

		require.Error(t, g.runner().generateBindings(bindingsDir))
	})
}

func TestAppBindingsCheck(t *testing.T) {
	t.Parallel()

	t.Run("passes when the committed bindings match", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		fakeGo(g, bindingsGenerator(t))
		require.NoError(t, g.runner().generateBindings(bindingsDir))

		require.NoError(t, g.runner().checkBindings())

		assert.Contains(t, g.out.String(), "frontend/bindings is up to date")
	})

	t.Run("lists every drifted file", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		fakeGo(g, bindingsGenerator(t))
		testutil.WriteFile(t, g.path(bindingsDir+"/models/a.ts"), "export const a = 0;\n")
		testutil.WriteFile(t, g.path(bindingsDir+"/old.ts"), "")

		err := g.runner().checkBindings()

		require.ErrorIs(t, err, errBindingsDrift)
		assert.Contains(t, err.Error(), "changed: models/a.ts")
		assert.Contains(t, err.Error(), "stale: old.ts")
	})

	t.Run("the generator fails", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		fakeGo(g, exits(1))

		require.Error(t, g.runner().checkBindings())
	})

	t.Run("committed bindings unreadable", func(t *testing.T) {
		t.Parallel()

		skipAsRoot(t)

		g := newRig(t)
		fakeGo(g, bindingsGenerator(t))
		testutil.WriteFile(t, g.path(bindingsDir+"/a.ts"), "")
		require.NoError(t, os.Chmod(g.path(bindingsDir+"/a.ts"), 0o000))

		err := g.runner().checkBindings()

		require.Error(t, err)
		assert.NotErrorIs(t, err, errBindingsDrift)
	})
}

func TestWailsCLIUsesTheVersionGoModPins(t *testing.T) {
	t.Parallel()

	requireTool(t, "go")

	r, _ := realRunner(t, repoRoot(t), nil)

	cli, err := r.wailsCLI()

	require.NoError(t, err)
	assert.Regexp(t, "^"+regexp.QuoteMeta(wailsModule+"/cmd/wails3@")+`v3\.`, cli)
}

func TestAppDevStopsTheDevServerWhenTheAppFails(t *testing.T) {
	t.Parallel()

	g := newRig(t)
	require.NoError(t, os.Mkdir(g.path(frontendDir), 0o750))
	g.tool("pnpm", func(args []string) testutil.FakeRun {
		if slices.Equal(args, []string{"dev"}) {
			return testutil.FakeRun{Sleep: time.Minute} // the dev server runs until it is stopped
		}

		return testutil.FakeRun{}
	})

	r := g.runner()

	appArgs, err := r.prepareDev(false, nil)
	require.NoError(t, err)
	assert.Empty(t, appArgs, "the user's own data")

	start := time.Now()
	err = r.runDev(t.Context(), appArgs) // go is not on PATH, so the app cannot start

	require.Error(t, err)
	assert.Contains(t, err.Error(), "run the desktop app")
	assert.Less(t, time.Since(start), devServerStopTimeout, "the dev server is stopped, not waited out")
	assert.Equal(t, []string{"install --frozen-lockfile", "dev"}, g.linesOf("pnpm"))
	assert.FileExists(t, g.path(distIndex))
}

func TestAppDevNeedsPNPM(t *testing.T) {
	t.Parallel()

	_, err := newRig(t).runner().prepareDev(false, nil)

	require.ErrorIs(t, err, errPNPMMissing)
}

func TestMacOSTargetsRefuseOtherSystems(t *testing.T) {
	t.Parallel()

	skipOnMacOS(t)

	targets := []struct {
		name string
		run  func() error
	}{
		{name: "app:build", run: App{}.Build},
		{name: "app:package", run: App{}.Package},
		{name: "app:rehearse", run: App{}.Rehearse},
		{name: "app:buildRelease", run: App{}.BuildRelease},
		{name: "app:signRelease", run: App{}.SignRelease},
		{name: "app:open", run: App{}.Open},
		{name: "app:run", run: App{}.Run},
		{name: "app:reset", run: App{}.Reset},
		{name: "app:fresh", run: App{}.Fresh},
		{name: "brand:icons", run: Brand{}.Icons},
		{name: "measure:startup", run: Measure{}.Startup},
		{name: "measure:refresh", run: Measure{}.Refresh},
		{name: "measure:launch", run: Measure{}.Launch},
		{name: "measure:size", run: Measure{}.Size},
		{name: "measure:all", run: Measure{}.All},
	}

	for _, target := range targets {
		t.Run(target.name, func(t *testing.T) {
			t.Parallel()

			require.ErrorIs(t, target.run(), errNeedsMacOS)
		})
	}
}

func TestAppBundle(t *testing.T) {
	t.Parallel()

	g := newRig(t)
	r := g.runner()

	_, _, err := r.appBundle()
	require.ErrorIs(t, err, errNoBundle)

	testutil.WriteFile(t, bundleExecutable(g.path(legacyBundle)), "")

	_, _, err = r.appBundle()
	require.ErrorIs(t, err, errNoBundle, "a bundle under the release's name is an earlier build's, never this one")

	testutil.WriteFile(t, bundleExecutable(g.path(devBundle)), "")

	bundle, exe, err := r.appBundle()

	require.NoError(t, err)
	assert.Equal(t, g.path(devBundle), bundle)
	assert.True(t, filepath.IsAbs(exe), "the executable path is absolute, so pgrep can match it")
	assert.True(t, strings.HasSuffix(exe, filepath.Join(brand.DevName+".app", "Contents", "MacOS", brand.Slug)))
}

func TestAppProcesses(t *testing.T) {
	t.Parallel()

	const exe = "/Applications/Example.app/Contents/MacOS/example"

	tests := []struct {
		name    string
		pgrep   testutil.FakeRun
		want    []int
		wantErr string
	}{
		{name: "running copies", pgrep: testutil.FakeRun{Stdout: "12\n345\n"}, want: []int{12, 345}},
		{name: "none running", pgrep: testutil.FakeRun{Exit: 1}, want: nil},
		{name: "pgrep fails", pgrep: testutil.FakeRun{Exit: 2}, wantErr: "find the running app"},
		{name: "unexpected output", pgrep: testutil.FakeRun{Stdout: "twelve\n"}, wantErr: `parse the pgrep output "twelve"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := newRig(t)
			g.fixed("pgrep", tt.pgrep)

			got, err := g.runner().appProcesses(exe)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	t.Run("matches the whole executable path at the start of the command line", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		g.tool("pgrep", exits(1))

		_, err := g.runner().appProcesses(exe)

		require.NoError(t, err)
		assert.Equal(t, []string{"-f ^" + regexp.QuoteMeta(exe) + "( |$)"}, g.linesOf("pgrep"))
	})
}

func TestAppProcessesFindsARealProcess(t *testing.T) {
	t.Parallel()

	requireProcessList(t)

	self, err := os.Executable()
	require.NoError(t, err)

	exe := filepath.Join(t.TempDir(), brand.DevName+".app", "Contents", "MacOS", brand.Slug) // a space, as the build from source's path holds

	// The app's path links to this test binary, which plays a fake app that sleeps until it
	// is stopped (testutil.FakeTool) under the app's path as its command name. A system
	// tool would not do: a multi-call build of sleep (as on Ubuntu) picks what it does by
	// the name it runs under, and exits at once.
	_, entry := testutil.FakeTool(t, brand.Slug, testutil.FakeRun{Sleep: time.Minute})
	require.NoError(t, os.MkdirAll(filepath.Dir(exe), 0o750))
	require.NoError(t, os.Symlink(self, exe))

	helper := exec.CommandContext(t.Context(), exe) //nolint:gosec // the test's own binary, no arguments

	helper.Env = append(os.Environ(), entry)
	pid, done := startProcess(t, helper)

	r, out := realRunner(t, t.TempDir(), nil)

	assert.Eventually(t, func() bool {
		pids, listErr := r.appProcesses(exe)

		return listErr == nil && slices.Equal(pids, []int{pid})
	}, 5*time.Second, 20*time.Millisecond)

	quit, err := r.quitRunningApp(exe, 5*time.Second)

	require.NoError(t, err)
	assert.True(t, quit)
	assert.Equal(t, "Quitting the running "+brand.DevName+"...\n", out.String())

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		assert.Fail(t, "the app did not quit")
	}
}

func TestQuitRunningApp(t *testing.T) {
	t.Parallel()

	const exe = "/Applications/Example.app/Contents/MacOS/example"

	t.Run("nothing running", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		g.tool("pgrep", exits(1))

		quit, err := g.runner().quitRunningApp(exe, time.Second)

		require.NoError(t, err)
		assert.False(t, quit)
		assert.Empty(t, g.out.String())
	})

	t.Run("waits until the running copy has quit", func(t *testing.T) {
		t.Parallel()

		pid := startSleeper(t)

		synctest.Test(t, func(t *testing.T) {
			g := newRig(t)
			calls := 0

			g.tool("pgrep", func([]string) testutil.FakeRun {
				calls++
				if calls > 1 {
					return testutil.FakeRun{Exit: 1} // gone
				}

				return testutil.FakeRun{Stdout: strconv.Itoa(pid) + "\n"}
			})

			quit, err := g.runner().quitRunningApp(exe, 5*time.Second)

			require.NoError(t, err)
			assert.True(t, quit)
			assert.Equal(t, 2, calls, "it looks again once it has asked the copy to quit")
			assert.Equal(t, "Quitting the running "+brand.DevName+"...\n", g.out.String())
		})

		requireExited(t, pid)
	})

	t.Run("gives up when it does not quit", func(t *testing.T) {
		t.Parallel()

		pid := startSleeper(t)

		synctest.Test(t, func(t *testing.T) {
			g := newRig(t)
			g.fixed("pgrep", testutil.FakeRun{Stdout: strconv.Itoa(pid) + "\n"})

			start := time.Now()
			quit, err := g.runner().quitRunningApp(exe, 250*time.Millisecond)

			require.ErrorIs(t, err, errStillRunning)
			assert.True(t, quit)
			assert.GreaterOrEqual(t, time.Since(start), 250*time.Millisecond, "it waits out its timeout")
			assert.Len(t, g.lines(), 4, "the first look, then one each poll")
		})
	})

	t.Run("pgrep fails", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		g.tool("pgrep", exits(2))

		quit, err := g.runner().quitRunningApp(exe, time.Second)

		require.Error(t, err)
		assert.False(t, quit)
	})
}

// requireExited fails the test unless the process pid has exited, which a process of the
// test's own reaped by startProcess shows by no longer taking a signal.
func requireExited(t *testing.T, pid int) {
	t.Helper()

	require.Eventually(t, func() bool {
		return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
	}, 5*time.Second, 10*time.Millisecond, "the running copy was asked to quit")
}

func TestOpenAfterQuit(t *testing.T) {
	t.Parallel()

	const bundle = "bin/Example.app"

	t.Run("opens the bundle", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		g.fixed("open", testutil.FakeRun{})

		require.NoError(t, g.runner().openAfterQuit(bundle, time.Second))

		assert.Equal(t, []string{bundle}, g.linesOf("open"))
	})

	t.Run("tries again while the old copy is still listed", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			g := newRig(t)
			tried := false

			g.tool("open", func([]string) testutil.FakeRun {
				if tried {
					return testutil.FakeRun{}
				}

				tried = true

				return testutil.FakeRun{Stdout: "procNotFound (-600)\n", Exit: 1}
			})

			require.NoError(t, g.runner().openAfterQuit(bundle, 5*time.Second))

			assert.Equal(t, []string{bundle, bundle}, g.linesOf("open"))
		})
	})

	t.Run("reports the last failure", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		g.fixed("open", testutil.FakeRun{Stdout: "procNotFound (-600)\n", Exit: 1})

		err := g.runner().openAfterQuit(bundle, 0)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "open "+bundle)
		assert.Contains(t, err.Error(), "procNotFound (-600)")
	})
}

func TestStopProcessGroup(t *testing.T) {
	t.Parallel()

	t.Run("not started", func(t *testing.T) {
		t.Parallel()

		assert.NotPanics(t, func() { stopProcessGroup(&exec.Cmd{}) })
	})

	t.Run("stops the group and waits for it", func(t *testing.T) {
		t.Parallel()

		path, entry := testutil.FakeTool(t, "server", testutil.FakeRun{Sleep: time.Minute})

		cmd := exec.CommandContext(t.Context(), path) //nolint:gosec // the test's own fake

		cmd.Env = append(os.Environ(), entry)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		require.NoError(t, cmd.Start())

		stopProcessGroup(cmd)

		require.NotNil(t, cmd.ProcessState)
		assert.False(t, cmd.ProcessState.Success())
	})
}

func TestVersionFlags(t *testing.T) {
	t.Parallel()

	meta := buildMeta{
		version:       "v1.2.3-rc.1-4-gabc1234",
		commit:        "abc1234",
		date:          "2026-11-07T00:00:00Z",
		release:       "1.2.3-rc.1",
		bundleVersion: "1.2.3",
		bundleBuild:   "42",
	}

	t.Run("a build from source stamps no flavor", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t,
			"-X main.version=v1.2.3-rc.1-4-gabc1234 -X main.commit=abc1234 -X main.buildDate=2026-11-07T00:00:00Z "+
				"-X main.release=1.2.3-rc.1 -X main.buildNumber=42",
			versionFlags(meta), "main.release keeps the whole version, the prerelease's suffix included")
	})

	t.Run("a release stamps its flavor", func(t *testing.T) {
		t.Parallel()

		release := meta
		release.flavor = flavorRelease

		assert.Equal(t, versionFlags(meta)+" -X main.flavor=release", versionFlags(release))
	})

	t.Run("a rehearsal stamps its feed, quoted, and its key", func(t *testing.T) {
		t.Parallel()

		rehearsal := meta
		rehearsal.flavor = flavorRehearsal
		rehearsal.updateFeed = "/projects/a repo/bin/rehearsal/feed"
		rehearsal.updateKey = "c29tZSBrZXk="

		assert.Equal(t, versionFlags(meta)+" -X main.flavor=rehearsal -X 'main.updateFeed=/projects/a repo/bin/rehearsal/feed'"+
			" -X main.updateKey=c29tZSBrZXk=", versionFlags(rehearsal))
	})
}

func TestVersionFlagsStampDeclaredVariables(t *testing.T) {
	t.Parallel()

	declared := packageVars(t, filepath.Join(repoRoot(t), "cmd", brand.Slug, "main_desktop.go"))
	rehearsal := buildMeta{flavor: flavorRehearsal, updateFeed: "/feed", updateKey: "key"}
	stamped := regexp.MustCompile(`-X '?main\.(\w+)=`).FindAllStringSubmatch(versionFlags(rehearsal), -1)

	require.NotEmpty(t, stamped)

	names := make([]string, 0, len(stamped))
	for _, match := range stamped {
		names = append(names, match[1])
		assert.Contains(t, declared, match[1], "the linker ignores -X for an undeclared variable")
	}

	assert.Subset(t, names, []string{"flavor", "updateFeed", "updateKey"})
}

func TestMacOSMinimumMatchesTheInfoPlist(t *testing.T) {
	t.Parallel()

	assert.Equal(t, brandgen.MinimumMacOS, macOSMinVersion, "the binary and the Info.plist name the same oldest macOS")
}

// gitRepo makes a repository of one empty commit in a fresh folder, and returns the
// folder and the environment that isolates git there from the user's configuration.
func gitRepo(t *testing.T) (string, []string) {
	t.Helper()

	dir := t.TempDir()
	env := testutil.Git(t)

	gitIn(t, dir, env, "init", "-q")
	gitIn(t, dir, env, "commit", "-q", "--allow-empty", "-m", "first")

	return dir, env
}

func TestReadBuildMeta(t *testing.T) {
	t.Parallel()

	t.Run("outside a git checkout", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		r, _ := realRunner(t, dir, append(testutil.Git(t), "GIT_CEILING_DIRECTORIES="+filepath.Dir(dir)))

		meta := r.readBuildMeta(t.Context())

		assert.Equal(t, "dev", meta.version)
		assert.Equal(t, "none", meta.commit)
		assert.Equal(t, brandgen.PlaceholderVersion, meta.release)
		assert.Equal(t, brandgen.PlaceholderVersion, meta.bundleVersion)
		assert.Equal(t, brandgen.PlaceholderVersion, meta.bundleBuild)
		assert.Empty(t, meta.flavor, "a build from source stamps no flavor")

		_, err := time.Parse(time.RFC3339, meta.date)
		require.NoError(t, err)
	})

	t.Run("before the first tag", func(t *testing.T) {
		t.Parallel()

		dir, env := gitRepo(t)
		head := gitIn(t, dir, env, "rev-parse", "--short", "HEAD")
		r, _ := realRunner(t, dir, env)

		meta := r.readBuildMeta(t.Context())

		assert.Equal(t, head, meta.version)
		assert.Equal(t, head, meta.commit)
		assert.Equal(t, brandgen.PlaceholderVersion, meta.bundleVersion)
		assert.Equal(t, "1", meta.bundleBuild)
	})

	t.Run("after a tag", func(t *testing.T) {
		t.Parallel()

		dir, env := gitRepo(t)
		gitIn(t, dir, env, "tag", "v1.2.3")
		gitIn(t, dir, env, "commit", "-q", "--allow-empty", "-m", "second")
		head := gitIn(t, dir, env, "rev-parse", "--short", "HEAD")
		r, _ := realRunner(t, dir, env)

		meta := r.readBuildMeta(t.Context())

		assert.Equal(t, "v1.2.3-1-g"+head, meta.version)
		assert.Equal(t, head, meta.commit)
		assert.Equal(t, "1.2.3", meta.release)
		assert.Equal(t, "1.2.3", meta.bundleVersion)
		assert.Equal(t, "2", meta.bundleBuild)
		assert.Empty(t, meta.flavor)
	})

	t.Run("after a prerelease's tag", func(t *testing.T) {
		t.Parallel()

		dir, env := gitRepo(t)
		gitIn(t, dir, env, "tag", "v0.2.0-rc.1")
		r, _ := realRunner(t, dir, env)

		meta := r.readBuildMeta(t.Context())

		assert.Equal(t, "v0.2.0-rc.1", meta.version)
		assert.Equal(t, "0.2.0-rc.1", meta.release, "main.release keeps the whole version")
		assert.Equal(t, "0.2.0", meta.bundleVersion, "macOS gets the numeric core")
	})

	t.Run("uncommitted changes", func(t *testing.T) {
		t.Parallel()

		dir, env := gitRepo(t)
		testutil.WriteFile(t, filepath.Join(dir, "a.txt"), "a")
		gitIn(t, dir, env, "add", "a.txt")
		gitIn(t, dir, env, "commit", "-q", "-m", "second")
		gitIn(t, dir, env, "tag", "v0.1.0")
		testutil.WriteFile(t, filepath.Join(dir, "a.txt"), "b")
		r, _ := realRunner(t, dir, env)

		assert.Equal(t, "v0.1.0-dirty", r.readBuildMeta(t.Context()).version)
	})
}

func TestBuildDesktopBinary(t *testing.T) {
	t.Parallel()

	meta := buildMeta{
		version:       "v1.0.0",
		commit:        "abc1234",
		date:          "2026-11-07T00:00:00Z",
		bundleVersion: "1.0.0",
		bundleBuild:   "7",
	}

	build := "build -tags desktop,production -trimpath -buildvcs=false -ldflags -s -w " + versionFlags(meta)

	t.Run("builds the tagged production binary for the oldest supported macOS", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		g.fixed("go", testutil.FakeRun{ReportEnv: []string{
			"CGO_ENABLED", "CGO_CFLAGS", "CGO_LDFLAGS", "MACOSX_DEPLOYMENT_TARGET", "GOARCH",
		}})

		minVersion := "-mmacosx-version-min=" + macOSMinVersion

		require.NoError(t, g.runner().buildDesktopBinary("bin/app", meta, ""))

		assert.Equal(t, []string{build + " -o bin/app ./cmd/" + brand.Slug}, g.linesOf("go"))
		assert.Equal(t, "CGO_ENABLED=1\nCGO_CFLAGS="+minVersion+"\nCGO_LDFLAGS="+minVersion+"\nMACOSX_DEPLOYMENT_TARGET="+
			macOSMinVersion+"\nGOARCH=\n", g.out.String(), "this Mac's own processor: no GOARCH")
	})

	t.Run("builds for another architecture", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		g.fixed("go", testutil.FakeRun{ReportEnv: []string{"GOARCH", "CGO_ENABLED"}})

		require.NoError(t, g.runner().buildDesktopBinary("bin/app-amd64", meta, "amd64"))

		assert.Equal(t, "GOARCH=amd64\nCGO_ENABLED=1\n", g.out.String())
	})

	t.Run("quiets Apple's newer linker about duplicate libraries", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		g.fixed("ld", testutil.FakeRun{Stderr: "@(#)PROGRAM:ld PROJECT:ld-1167.5\n"})
		g.fixed("go", testutil.FakeRun{})

		require.NoError(t, g.runner().buildDesktopBinary("bin/app", meta, ""))

		assert.Equal(t, []string{
			build + " -extldflags=-Wl,-no_warn_duplicate_libraries -o bin/app ./cmd/" + brand.Slug,
		}, g.linesOf("go"))
	})

	t.Run("the build fails", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		g.tool("go", exits(1))

		err := g.runner().buildDesktopBinary("bin/app", meta, "")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "build the desktop binary")
	})
}

func TestQuietLinkerFlags(t *testing.T) {
	t.Parallel()

	const quiet = " -extldflags=-Wl,-no_warn_duplicate_libraries"

	tests := []struct {
		name string
		ld   *testutil.FakeRun // the fake ld, or nil for no ld on PATH
		want string
	}{
		{name: "Apple's linker since Xcode 15", ld: &testutil.FakeRun{Stderr: "@(#)PROGRAM:ld PROJECT:ld-1167.5\n"}, want: quiet},
		{
			name: "whatever ld -v exits with", ld: &testutil.FakeRun{Stderr: "@(#)PROGRAM:ld PROJECT:ld-1115.7.3\n", Exit: 1},
			want: quiet,
		},
		{name: "Apple's older ld64, which does not know the flag", ld: &testutil.FakeRun{Stderr: "@(#)PROGRAM:ld PROJECT:ld64-820.1\n"}},
		{name: "a GNU linker", ld: &testutil.FakeRun{Stdout: "GNU ld (GNU Binutils) 2.42\n"}},
		{name: "no linker on PATH"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := newRig(t)
			if tt.ld != nil {
				g.fixed("ld", *tt.ld)
			}

			assert.Equal(t, tt.want, g.runner().quietLinkerFlags(t.Context()))
		})
	}
}

func TestWriteInfoPlist(t *testing.T) {
	t.Parallel()

	meta := buildMeta{bundleVersion: "1.2.3", bundleBuild: "42"}

	// plistRig is a rig whose checkout holds a generated Info.plist, with a plutil that
	// answers each call with run.
	plistRig := func(t *testing.T, run testutil.FakeRun) *rig {
		t.Helper()

		g := newRig(t)
		testutil.WriteFile(t, g.path(brandgen.InfoPlistPath), "<plist/>")
		g.fixed("plutil", run)

		return g
	}

	t.Run("copies the plist and stamps the versions, keeping the release's name", func(t *testing.T) {
		t.Parallel()

		g := plistRig(t, testutil.FakeRun{})
		dst := filepath.Join(t.TempDir(), "Info.plist")

		require.NoError(t, g.runner().writeInfoPlist(dst, meta, false))

		assert.Equal(t, "<plist/>", readFile(t, dst), "the committed plist keeps its placeholder version")
		assert.Equal(t, []string{
			"-replace CFBundleShortVersionString -string 1.2.3 " + dst,
			"-replace CFBundleVersion -string 42 " + dst,
		}, g.linesOf("plutil"))
	})

	t.Run("names a build from source apart from the release", func(t *testing.T) {
		t.Parallel()

		g := plistRig(t, testutil.FakeRun{})
		dst := filepath.Join(t.TempDir(), "Info.plist")

		require.NoError(t, g.runner().writeInfoPlist(dst, meta, true))

		assert.Equal(t, "<plist/>", readFile(t, dst), "the committed plist keeps the release's name")
		assert.Equal(t, []string{
			"-replace CFBundleShortVersionString -string 1.2.3 " + dst,
			"-replace CFBundleVersion -string 42 " + dst,
			"-replace CFBundleName -string " + brand.DevName + " " + dst,
			"-replace CFBundleDisplayName -string " + brand.DevName + " " + dst,
			"-replace CFBundleIdentifier -string " + brand.DevBundleID + " " + dst,
			"-replace NSAppleEventsUsageDescription -string " + brandgen.AppleEventsUsage(brand.DevName) + " " + dst,
		}, g.linesOf("plutil"))
	})

	t.Run("plist missing", func(t *testing.T) {
		t.Parallel()

		err := newRig(t).runner().writeInfoPlist(filepath.Join(t.TempDir(), "Info.plist"), meta, false)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "read the Info.plist")
	})

	t.Run("bundle missing", func(t *testing.T) {
		t.Parallel()

		g := plistRig(t, testutil.FakeRun{})

		err := g.runner().writeInfoPlist(filepath.Join(t.TempDir(), "missing", "Info.plist"), meta, false)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "write the bundle Info.plist")
	})

	t.Run("stamping fails", func(t *testing.T) {
		t.Parallel()

		g := plistRig(t, testutil.FakeRun{Exit: 1})

		err := g.runner().writeInfoPlist(filepath.Join(t.TempDir(), "Info.plist"), meta, true)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "stamp CFBundleShortVersionString")
	})
}

func TestWriteInfoPlistWithPlutil(t *testing.T) {
	t.Parallel()

	onlyOnMacOS(t)

	plutil := requireTool(t, "plutil")
	r, _ := realRunner(t, repoRoot(t), nil)

	read := func(t *testing.T, plist, key string) string {
		t.Helper()

		out, err := exec.CommandContext(t.Context(), plutil, "-extract", key, "raw", "-o", "-", plist).Output() //nolint:gosec // the test's own plist, fixed arguments
		require.NoError(t, err, key)

		return strings.TrimSpace(string(out))
	}

	tests := []struct {
		name     string
		dev      bool
		wantName string
		wantID   string
	}{
		{name: "a release or a rehearsal keeps the release's name", dev: false, wantName: brand.Name, wantID: brand.BundleID},
		{name: "a build from source is named apart", dev: true, wantName: brand.DevName, wantID: brand.DevBundleID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			plist := filepath.Join(t.TempDir(), "Info.plist")

			require.NoError(t, r.writeInfoPlist(plist, buildMeta{bundleVersion: "1.2.3", bundleBuild: "42"}, tt.dev))

			assert.Equal(t, tt.wantName, read(t, plist, "CFBundleName"))
			assert.Equal(t, tt.wantName, read(t, plist, "CFBundleDisplayName"))
			assert.Equal(t, tt.wantID, read(t, plist, "CFBundleIdentifier"))
			assert.Equal(t, brandgen.AppleEventsUsage(tt.wantName), read(t, plist, "NSAppleEventsUsageDescription"))
			assert.Equal(t, brand.Slug, read(t, plist, "CFBundleExecutable"), "the executable keeps its name")
			assert.Equal(t, "1.2.3", read(t, plist, "CFBundleShortVersionString"))
			assert.Equal(t, "42", read(t, plist, "CFBundleVersion"))
		})
	}
}

func TestCheckBrandFiles(t *testing.T) {
	t.Parallel()

	t.Run("the committed files are current", func(t *testing.T) {
		t.Parallel()

		r, _ := realRunner(t, repoRoot(t), nil)

		require.NoError(t, r.checkBrandFiles())
	})

	t.Run("generated files missing", func(t *testing.T) {
		t.Parallel()

		err := newRig(t).runner().checkBrandFiles()

		require.ErrorIs(t, err, brandgen.ErrStale)
		assert.Contains(t, err.Error(), "generated brand files")
	})

	t.Run("icons missing", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		require.NoError(t, brandgen.Write(g.dir))

		err := g.runner().checkBrandFiles()

		require.ErrorIs(t, err, brandgen.ErrStaleIcons)
		assert.Contains(t, err.Error(), "app icons")
	})
}

func TestCopyIconSet(t *testing.T) {
	t.Parallel()

	t.Run("copies the icon set under the name the Info.plist gives it", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		testutil.WriteFile(t, g.path(brandgen.IconSetPath), "icns")

		resources := filepath.Join(t.TempDir(), "Contents", "Resources")

		require.NoError(t, g.runner().copyIconSet(resources))

		assert.Equal(t, "icns", readFile(t, filepath.Join(resources, brandgen.IconFile+".icns")))
	})

	t.Run("icon set missing", func(t *testing.T) {
		t.Parallel()

		err := newRig(t).runner().copyIconSet(filepath.Join(t.TempDir(), "Resources"))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "read the app icon set")
	})

	t.Run("resources is a file", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		testutil.WriteFile(t, g.path(brandgen.IconSetPath), "icns")

		resources := filepath.Join(t.TempDir(), "Resources")
		testutil.WriteFile(t, resources, "")

		err := g.runner().copyIconSet(resources)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "create the bundle resources")
	})

	t.Run("icon path is a directory", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		testutil.WriteFile(t, g.path(brandgen.IconSetPath), "icns")

		resources := filepath.Join(t.TempDir(), "Resources")
		require.NoError(t, os.MkdirAll(filepath.Join(resources, brandgen.IconFile+".icns"), 0o750))

		err := g.runner().copyIconSet(resources)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "write the bundle app icon")
	})
}

func TestReportSize(t *testing.T) {
	t.Parallel()

	t.Run("prints the bundle and its binary's size", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		exe := filepath.Join(t.TempDir(), "app")
		testutil.WriteFile(t, exe, strings.Repeat("x", 1500))

		require.NoError(t, g.runner().reportSize("bin/Example.app", exe))

		assert.Equal(t, "✅ Built bin/Example.app (binary 1500 bytes, 0.0 MB)\n", g.out.String())
	})

	t.Run("binary missing", func(t *testing.T) {
		t.Parallel()

		err := newRig(t).runner().reportSize("bin/Example.app", filepath.Join(t.TempDir(), "missing"))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "read the binary size")
	})
}

func TestNormalizeModes(t *testing.T) {
	t.Parallel()

	bundle := filepath.Join(t.TempDir(), "Example.app")
	exe := filepath.Join(bundle, "Contents", "MacOS", brand.Slug)
	helper := filepath.Join(bundle, "Contents", "Resources", "helper")
	plist := filepath.Join(bundle, "Contents", "Info.plist")

	testutil.WriteFileMode(t, exe, "binary", 0o600)
	testutil.WriteFileMode(t, helper, "script", 0o700)
	testutil.WriteFileMode(t, plist, "<plist/>", 0o664)
	require.NoError(t, os.Chmod(filepath.Join(bundle, "Contents"), 0o700)) //nolint:gosec // as a umask of 077 leaves a folder
	require.NoError(t, os.Symlink("Info.plist", filepath.Join(bundle, "Contents", "Link")))

	require.NoError(t, normalizeModes(bundle, exe))

	for path, want := range map[string]os.FileMode{
		bundle:                            0o755,
		filepath.Join(bundle, "Contents"): 0o755,
		exe:                               0o755,
		helper:                            0o755,
		plist:                             0o644,
	} {
		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, want, info.Mode().Perm(), path)
	}

	require.Error(t, normalizeModes(filepath.Join(t.TempDir(), "missing.app"), exe))
}

func TestBuildBundle(t *testing.T) {
	t.Parallel()

	t.Run("assembles, stamps, and signs the bundle, named apart from the release", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		checkout(t, g.dir)
		appTools(g)

		bundle := g.path(devBundle) // a space in its name, as a build from source's
		exe := bundleExecutable(bundle)
		testutil.WriteFile(t, filepath.Join(bundle, "stale.txt"), "from the previous build")

		require.NoError(t, g.runner().buildBundle(bundle))

		assert.Equal(t, "binary", readFile(t, exe))
		assert.Equal(t, readFile(t, g.path(brandgen.InfoPlistPath)), readFile(t, filepath.Join(bundle, "Contents", "Info.plist")))
		assert.Equal(t, "png", readFile(t, filepath.Join(bundle, "Contents", "Resources", brandgen.IconFile+".icns")))
		assert.NoFileExists(t, filepath.Join(bundle, "stale.txt"), "the previous bundle is removed first")
		assert.Contains(t, g.out.String(), "✅ Built "+bundle+" (binary 6 bytes")

		lines := g.lines()
		require.Len(t, lines, 10)
		assert.Equal(t, []string{"pnpm install --frozen-lockfile", "pnpm build"}, lines[:2])
		assert.True(t, strings.HasPrefix(lines[2], "go build -tags desktop,production -trimpath -buildvcs=false -ldflags -s -w -X main.version="), lines[2])
		assert.True(t, strings.HasSuffix(lines[2], " -o "+exe+" ./cmd/"+brand.Slug), lines[2])
		assert.NotContains(t, lines[2], "main.flavor", "a build from source stamps no flavor")

		plist := filepath.Join(bundle, "Contents", "Info.plist")
		assert.Equal(t, []string{
			"plutil -replace CFBundleShortVersionString -string " + brandgen.PlaceholderVersion + " " + plist,
			"plutil -replace CFBundleVersion -string " + brandgen.PlaceholderVersion + " " + plist,
			"plutil -replace CFBundleName -string " + brand.DevName + " " + plist,
			"plutil -replace CFBundleDisplayName -string " + brand.DevName + " " + plist,
			"plutil -replace CFBundleIdentifier -string " + brand.DevBundleID + " " + plist,
			"plutil -replace NSAppleEventsUsageDescription -string " + brandgen.AppleEventsUsage(brand.DevName) + " " + plist,
			"codesign --force --sign - " + bundle,
		}, lines[3:], "named before it is signed, so the signature seals the name")
	})

	t.Run("stale brand files stop the build before any tool runs", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		appTools(g)

		require.ErrorIs(t, g.runner().buildBundle(g.path(devBundle)), brandgen.ErrStale)
		assert.Empty(t, g.lines())
	})

	tests := []struct {
		name    string
		tool    string
		exit    int
		wantErr string
	}{
		{name: "the frontend build fails", tool: "pnpm", exit: 1, wantErr: "pnpm [install --frozen-lockfile]"},
		{name: "the compile fails", tool: "go", exit: 1, wantErr: "build the desktop binary"},
		{name: "stamping fails", tool: "plutil", exit: 1, wantErr: "stamp CFBundleShortVersionString"},
		{name: "signing fails", tool: "codesign", exit: 1, wantErr: "sign the bundle ad hoc"},
		{name: "no binary to report", tool: "go", exit: 0, wantErr: "read the binary size"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := newRig(t)
			checkout(t, g.dir)
			appTools(g)
			g.tool(tt.tool, exits(tt.exit))

			err := g.runner().buildBundle(g.path(devBundle))

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}

	t.Run("the bundle directory cannot be created", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		checkout(t, g.dir)
		appTools(g)
		testutil.WriteFile(t, g.path("bin"), "")

		require.Error(t, g.runner().buildBundle(g.path(devBundle)))
	})
}

func TestRemoveLegacyBundle(t *testing.T) {
	t.Parallel()

	earlier := func(t *testing.T) string {
		t.Helper()

		legacy := filepath.Join(t.TempDir(), "bin", brand.Name+".app")
		testutil.WriteFile(t, bundleExecutable(legacy), "binary")

		return legacy
	}

	t.Run("removes an earlier build from source once nothing runs from it", func(t *testing.T) {
		t.Parallel()

		legacy := earlier(t)
		g := newRig(t)
		g.tool("pgrep", exits(1))

		require.NoError(t, g.runner().removeLegacyBundle(legacy))

		assert.NoDirExists(t, legacy)
		assert.DirExists(t, filepath.Dir(legacy), "only the bundle goes")
		assert.Equal(t, []string{"-f ^" + regexp.QuoteMeta(bundleExecutable(legacy)) + "( |$)"}, g.linesOf("pgrep"),
			"it looks for a copy running from it first")
		assert.Equal(t, "Removed "+legacy+", an earlier build from source under "+brand.Name+"'s own name and bundle ID.\n",
			g.out.String())
	})

	t.Run("leaves one that still runs, and says so", func(t *testing.T) {
		t.Parallel()

		legacy := earlier(t)
		g := newRig(t)
		g.fixed("pgrep", testutil.FakeRun{Stdout: "4242\n"})

		require.NoError(t, g.runner().removeLegacyBundle(legacy))

		assert.FileExists(t, bundleExecutable(legacy))
		assert.Contains(t, g.out.String(), legacy+", an earlier build from source under "+brand.Name+"'s own name, still runs")
	})

	t.Run("fails, removing nothing, when it can't tell whether one runs", func(t *testing.T) {
		t.Parallel()

		legacy := earlier(t)
		g := newRig(t)
		g.tool("pgrep", exits(2))

		require.ErrorContains(t, g.runner().removeLegacyBundle(legacy), "find the running app")
		assert.FileExists(t, bundleExecutable(legacy))
	})

	t.Run("nothing there, or a link, is nothing to remove", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		g.tool("pgrep", exits(1))
		r := g.runner()

		require.NoError(t, r.removeLegacyBundle(filepath.Join(t.TempDir(), brand.Name+".app")))

		target := earlier(t)
		link := filepath.Join(t.TempDir(), brand.Name+".app")
		require.NoError(t, os.Symlink(target, link))

		require.NoError(t, r.removeLegacyBundle(link))

		_, err := os.Lstat(link)
		require.NoError(t, err, "the link stays")
		assert.FileExists(t, bundleExecutable(target), "and what it links to")
		assert.Empty(t, g.lines(), "no bundle, so nothing to look for running")
	})
}

func TestAppTargets(t *testing.T) {
	t.Parallel()

	t.Run("build makes the bundle from source and removes the earlier one, named as the release", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		checkout(t, g.dir)
		appTools(g)

		bundle := g.path(devBundle)
		legacy := g.path(legacyBundle)
		testutil.WriteFile(t, bundleExecutable(legacy), "an earlier build")

		require.NoError(t, g.runner().buildApp())

		assert.Equal(t, "binary", readFile(t, bundleExecutable(bundle)))
		assert.NoDirExists(t, legacy)
		assert.Contains(t, g.out.String(), "Removed "+legacy)

		lines := g.lines()
		assert.Contains(t, lines, "plutil -replace CFBundleIdentifier -string "+brand.DevBundleID+" "+
			filepath.Join(bundle, "Contents", "Info.plist"))
		assert.True(t, strings.HasPrefix(lines[len(lines)-1], "pgrep -f ^"), "it looks for a copy running from the earlier one")
		assert.Equal(t, "codesign --force --sign - "+bundle, lines[len(lines)-2], "once this build is made")
	})

	t.Run("open builds a missing bundle, then opens it", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		checkout(t, g.dir)
		appTools(g)
		bundle := g.path(devBundle)

		require.NoError(t, g.runner().openApp())

		lines := g.lines()
		assert.Contains(t, lines, "codesign --force --sign - "+bundle)
		assert.Equal(t, "open "+bundle, lines[len(lines)-1])
	})

	t.Run("open opens an existing bundle as it is", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		appTools(g)
		testutil.WriteFile(t, bundleExecutable(g.path(devBundle)), "")

		require.NoError(t, g.runner().openApp())

		assert.Equal(t, []string{"open " + g.path(devBundle)}, g.lines())
	})

	t.Run("run builds, then opens the fresh bundle", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		checkout(t, g.dir)
		appTools(g)

		require.NoError(t, g.runner().runApp())

		lines := g.lines()
		assert.True(t, strings.HasPrefix(lines[len(lines)-2], "pgrep -f ^"), "it looks for a running copy first")
		assert.Equal(t, "open "+g.path(devBundle), lines[len(lines)-1])
	})

	t.Run("check runs the drift check, the desktop lint, the desktop tests, and the build", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		checkout(t, g.dir)
		appTools(g)
		require.NoError(t, g.runner().generateBindings(bindingsDir))

		require.NoError(t, g.runner().checkDesktop(goosMacOS))

		lines := g.lines()
		lint := slices.Index(lines, "golangci-lint run --build-tags desktop --config .golangci.json --timeout 10m ./cmd/"+
			brand.Slug+"/... ./frontend/")
		tests := slices.Index(lines, "go test -tags desktop ./cmd/"+brand.Slug+"/")

		require.NotEqual(t, -1, lint, "the desktop lint")
		require.NotEqual(t, -1, tests, "the desktop tests")
		assert.Less(t, lint, tests, "the tests run after the lint")
		assert.Equal(t, "codesign --force --sign - "+g.path(devBundle), lines[len(lines)-1])
	})

	t.Run("test runs the desktop-tagged tests, with a frontend to embed", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		appTools(g)

		require.NoError(t, g.runner().testDesktop())

		assert.Equal(t, []string{"go test -tags desktop ./cmd/" + brand.Slug + "/"}, g.lines())
		assert.FileExists(t, g.path(distIndex), "the embed needs a page, so a placeholder stands in")
	})
}
