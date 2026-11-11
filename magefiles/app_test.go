//go:build mage

package main

import (
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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/brandgen"
)

// fakeWailsVersion is the wails version the fake go reports for the module go.mod pins.
const fakeWailsVersion = "v3.0.0-test.1"

// bindingsGenerator is a fake go body that writes one binding into the directory after
// -d, with the trailing space the real generator leaves on blank doc-comment lines.
const bindingsGenerator = `while [ $# -gt 0 ]; do
  if [ "$1" = -d ]; then out="$2"; fi
  shift
done
/bin/mkdir -p "$out/models"
printf '/**\n * Doc. \n */\nexport const a = 1;\n' > "$out/models/a.ts"`

// fakeGo puts a go on an isolated PATH that logs each call's arguments, answers
// `go list` with fakeWailsVersion, and runs body for every other command. It returns
// the log's path, which body can append to through $log.
func fakeGo(t *testing.T, body string) string {
	t.Helper()

	return fakeLoggedTool(t, isolatedPath(t), "go", `if [ "$1" = list ]; then echo "`+fakeWailsVersion+`"; exit 0; fi
`+body)
}

// fakeLint puts a golangci-lint at version 2.15.0 on an isolated PATH that logs each
// run's arguments, pins that version for the target, and returns the log's path.
func fakeLint(t *testing.T) string {
	t.Helper()

	log := filepath.Join(t.TempDir(), "lint.log")
	t.Setenv("LINT_LOG", log)
	t.Setenv(lintVersionKey, "v2.15.0")

	fakeTool(t, isolatedPath(t), "golangci-lint", `if [ "$1" = version ]; then
  echo "golangci-lint has version 2.15.0 built with go1.26.0"
  exit 0
fi
printf '%s\n' "$*" >> "$LINT_LOG"`)

	return log
}

// fakeOpen puts an open on an isolated PATH that logs its arguments and runs body, and
// returns the log's path.
func fakeOpen(t *testing.T, body string) string {
	t.Helper()

	return fakeLoggedTool(t, isolatedPath(t), "open", body)
}

// appTools puts a fake of every tool the app targets run on an isolated PATH, each
// logging its name and arguments to one shared log: pnpm; a go that answers `go list`
// with fakeWailsVersion, writes a binary after -o, and writes one binding after -d;
// golangci-lint at the pinned version; plutil; codesign; open; and a pgrep that finds
// nothing running. It returns the PATH directory, so a test can replace a tool, and the
// log's path.
func appTools(t *testing.T) (string, string) {
	t.Helper()

	log := filepath.Join(t.TempDir(), "tools.log")
	t.Setenv("TOOLS_LOG", log)
	t.Setenv(lintVersionKey, "v2.15.0")

	dir := isolatedPath(t)

	for _, name := range []string{"pnpm", "plutil", "codesign", "open"} {
		fakeTool(t, dir, name, `printf '`+name+` %s\n' "$*" >> "$TOOLS_LOG"`)
	}

	fakeTool(t, dir, "pgrep", `printf 'pgrep %s\n' "$*" >> "$TOOLS_LOG"; exit 1`)
	fakeTool(t, dir, "golangci-lint", `if [ "$1" = version ]; then
  echo "golangci-lint has version 2.15.0 built with go1.26.0"
  exit 0
fi
printf 'golangci-lint %s\n' "$*" >> "$TOOLS_LOG"`)
	fakeTool(t, dir, "go", `printf 'go %s\n' "$*" >> "$TOOLS_LOG"
if [ "$1" = list ]; then echo "`+fakeWailsVersion+`"; exit 0; fi
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out="$2" ;;
    -d) bindings="$2" ;;
  esac
  shift
done
if [ -n "$out" ]; then printf 'binary' > "$out"; fi
if [ -n "$bindings" ]; then
  /bin/mkdir -p "$bindings/models"
  printf 'export const a = 1;\n' > "$bindings/models/a.ts"
fi`)

	return dir, log
}

// checkout writes into dir what a checkout holds for the app targets: the generated
// brand files, current app icons, and the frontend directory.
func checkout(t *testing.T, dir string) {
	t.Helper()

	require.NoError(t, brandgen.Write(dir))

	for _, icon := range []string{brandgen.IconSetPath, brandgen.DevIconPath, brandgen.TrayIconPath} {
		writeFile(t, filepath.Join(dir, filepath.FromSlash(icon)), "png")
	}

	require.NoError(t, brandgen.WriteIconStamp(dir))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, frontendDir), 0o750))
}

// startProcess starts cmd, a process of the test's own, and returns its ID and a channel
// that is closed once it has exited. The test kills and reaps it when it ends.
func startProcess(t *testing.T, cmd *exec.Cmd) (int, <-chan struct{}) {
	t.Helper()

	require.NoError(t, cmd.Start())

	done := make(chan struct{})

	go func() {
		_ = cmd.Wait()

		close(done)
	}()

	t.Cleanup(func() {
		_ = cmd.Process.Kill()

		<-done
	})

	return cmd.Process.Pid, done
}

// startSleeper starts a process of the test's own that sleeps until it is stopped and
// returns its ID. The test reaps it when it ends.
func startSleeper(t *testing.T) int {
	t.Helper()

	pid, _ := startProcess(t, exec.CommandContext(t.Context(), requireTool(t, "sleep"), "30")) //nolint:gosec // the test's own process, fixed arguments

	return pid
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
		writeFile(t, filepath.Join(root, "a.ts"), "a")
		writeFile(t, filepath.Join(root, "nested", "b.ts"), "b")
		require.NoError(t, os.Symlink("a.ts", filepath.Join(root, "link.ts")))

		files, err := treeFiles(root)

		require.NoError(t, err)
		assert.Equal(t, map[string][]byte{"a.ts": []byte("a"), "nested/b.ts": []byte("b")}, files)
	})

	t.Run("unreadable file", func(t *testing.T) {
		t.Parallel()

		skipAsRoot(t)

		root := t.TempDir()
		writeFile(t, filepath.Join(root, "a.ts"), "a")
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
		writeFile(t, filepath.Join(want, "same.ts"), "x")
		writeFile(t, filepath.Join(got, "same.ts"), "x")
		writeFile(t, filepath.Join(want, "changed.ts"), "old")
		writeFile(t, filepath.Join(got, "changed.ts"), "new")
		writeFile(t, filepath.Join(got, "added", "new.ts"), "new")
		writeFile(t, filepath.Join(want, "gone.ts"), "old")

		drift, err := treeDiff(want, got)

		require.NoError(t, err)
		assert.Equal(t, []string{"changed: changed.ts", "missing: added/new.ts", "stale: gone.ts"}, drift)
	})

	t.Run("identical trees", func(t *testing.T) {
		t.Parallel()

		want, got := t.TempDir(), t.TempDir()
		writeFile(t, filepath.Join(want, "a.ts"), "a")
		writeFile(t, filepath.Join(got, "a.ts"), "a")

		drift, err := treeDiff(want, got)

		require.NoError(t, err)
		assert.Empty(t, drift)
	})

	t.Run("unreadable tree", func(t *testing.T) {
		t.Parallel()

		skipAsRoot(t)

		bad := t.TempDir()
		writeFile(t, filepath.Join(bad, "a.ts"), "a")
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

		writeFile(t, dirty, "/**\n * Doc. \n *\t\n */\nexport const a = 1;  \n")
		writeFile(t, enum, "export enum Kind {\n    KindPR = \"pr\",\n};\n")
		writeFile(t, clean, "export const b = 2;\n")

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
		writeFile(t, path, "a \n")
		require.NoError(t, os.Chmod(path, 0o400))

		err := tidyBindings(root)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "tidy a.ts")
	})

	t.Run("unreadable file", func(t *testing.T) {
		t.Parallel()

		skipAsRoot(t)

		root := t.TempDir()
		writeFile(t, filepath.Join(root, "a.ts"), "a \n")
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

//nolint:paralleltest // changes the process's working directory
func TestEnsureDist(t *testing.T) {
	t.Run("writes a placeholder when the frontend is not built", func(t *testing.T) {
		inTempDir(t)

		require.NoError(t, ensureDist())

		assert.Equal(t, placeholderPage, readFile(t, distIndex))
	})

	t.Run("keeps a built frontend", func(t *testing.T) {
		inTempDir(t)
		writeFile(t, distIndex, "<html>built</html>")

		require.NoError(t, ensureDist())

		assert.Equal(t, "<html>built</html>", readFile(t, distIndex))
	})

	t.Run("frontend is not a directory", func(t *testing.T) {
		inTempDir(t)
		writeFile(t, frontendDir, "")

		err := ensureDist()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "create frontend/dist")
	})

	t.Run("dist is read-only", func(t *testing.T) {
		skipAsRoot(t)

		dir := inTempDir(t)
		dist := filepath.Join(dir, filepath.Dir(distIndex))
		require.NoError(t, os.MkdirAll(dist, 0o750))
		require.NoError(t, os.Chmod(dist, 0o500)) //nolint:gosec // a read-only directory is the fixture

		err := ensureDist()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "write the placeholder frontend")
	})
}

func TestLintVersion(t *testing.T) {
	t.Run("from the environment", func(t *testing.T) {
		inTempDir(t)
		t.Setenv(lintVersionKey, "v9.9.9")

		got, err := lintVersion()

		require.NoError(t, err)
		assert.Equal(t, "v9.9.9", got)
	})

	t.Run("no env file", func(t *testing.T) {
		inTempDir(t)
		t.Setenv(lintVersionKey, "")

		_, err := lintVersion()

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
			dir := inTempDir(t)
			t.Setenv(lintVersionKey, "")
			writeFile(t, filepath.Join(dir, mageXEnvFile), tt.env)

			got, err := lintVersion()

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
	inRepoRoot(t)
	t.Setenv(lintVersionKey, "")

	got, err := lintVersion()

	require.NoError(t, err, "the synced MAGE-X env file must keep pinning golangci-lint")
	assert.Regexp(t, `^v\d+\.\d+\.\d+$`, got)
}

//nolint:paralleltest // changes the process's PATH
func TestLintOnPath(t *testing.T) {
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
			fakeLint(t)

			assert.Equal(t, tt.want, lintOnPath(tt.version))
		})
	}

	t.Run("not installed", func(t *testing.T) {
		isolatedPath(t)

		assert.False(t, lintOnPath("v2.15.0"))
	})
}

//nolint:paralleltest // changes the process's working directory and PATH
func TestAppLint(t *testing.T) {
	t.Run("lints the desktop code with the pinned version on PATH", func(t *testing.T) {
		inTempDir(t)
		log := fakeLint(t)

		require.NoError(t, App{}.Lint())

		assert.Equal(t, []string{
			"run --build-tags desktop --config .golangci.json --timeout 10m ./cmd/" + brand.Slug + "/... ./frontend/",
		}, logLines(t, log))
		assert.FileExists(t, distIndex, "the desktop embed needs a frontend to match")
	})

	t.Run("runs the pinned version when PATH has another", func(t *testing.T) {
		inTempDir(t)
		fakeLint(t)
		t.Setenv(lintVersionKey, "v2.14.0")

		err := App{}.Lint()

		require.Error(t, err, "go is not on PATH")
		assert.Contains(t, err.Error(), "golangci-lint@v2.14.0")
	})

	t.Run("no pinned version", func(t *testing.T) {
		inTempDir(t)
		isolatedPath(t)
		t.Setenv(lintVersionKey, "")

		require.Error(t, App{}.Lint())
	})

	t.Run("frontend is not a directory", func(t *testing.T) {
		inTempDir(t)
		writeFile(t, frontendDir, "")

		require.Error(t, App{}.Lint())
	})
}

//nolint:paralleltest // changes the process's working directory and PATH
func TestAppBindings(t *testing.T) {
	t.Run("generates trimmed bindings with the pinned wails CLI", func(t *testing.T) {
		inTempDir(t)
		log := fakeGo(t, bindingsGenerator)

		require.NoError(t, App{}.Bindings())

		assert.Equal(t, "/**\n * Doc.\n */\nexport const a = 1;\n", readFile(t, filepath.Join(bindingsDir, "models", "a.ts")))
		assert.Equal(t, []string{
			"list -m -f {{.Version}} " + wailsModule,
			"run " + wailsModule + "/cmd/wails3@" + fakeWailsVersion +
				" generate bindings -ts -i -f -tags desktop -d " + bindingsDir + " ./cmd/" + brand.Slug,
		}, logLines(t, log))
	})

	t.Run("the generator fails", func(t *testing.T) {
		inTempDir(t)
		fakeGo(t, "exit 1")

		err := App{}.Bindings()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "generate bindings")
	})

	t.Run("go missing", func(t *testing.T) {
		inTempDir(t)
		isolatedPath(t)

		err := App{}.Bindings()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "read the pinned "+wailsModule+" version")
	})

	t.Run("frontend is not a directory", func(t *testing.T) {
		inTempDir(t)
		writeFile(t, frontendDir, "")

		require.Error(t, App{}.Bindings())
	})
}

//nolint:paralleltest // changes the process's working directory, PATH, and standard output
func TestAppBindingsCheck(t *testing.T) {
	t.Run("passes when the committed bindings match", func(t *testing.T) {
		inTempDir(t)
		fakeGo(t, bindingsGenerator)
		require.NoError(t, App{}.Bindings())

		var err error

		out := captureStdout(t, func() { err = App{}.BindingsCheck() })

		require.NoError(t, err)
		assert.Contains(t, out, "frontend/bindings is up to date")
	})

	t.Run("lists every drifted file", func(t *testing.T) {
		inTempDir(t)
		fakeGo(t, bindingsGenerator)
		writeFile(t, filepath.Join(bindingsDir, "models", "a.ts"), "export const a = 0;\n")
		writeFile(t, filepath.Join(bindingsDir, "old.ts"), "")

		err := App{}.BindingsCheck()

		require.ErrorIs(t, err, errBindingsDrift)
		assert.Contains(t, err.Error(), "changed: models/a.ts")
		assert.Contains(t, err.Error(), "stale: old.ts")
	})

	t.Run("the generator fails", func(t *testing.T) {
		inTempDir(t)
		fakeGo(t, "exit 1")

		require.Error(t, App{}.BindingsCheck())
	})

	t.Run("committed bindings unreadable", func(t *testing.T) {
		skipAsRoot(t)

		inTempDir(t)
		fakeGo(t, bindingsGenerator)
		writeFile(t, filepath.Join(bindingsDir, "a.ts"), "")
		require.NoError(t, os.Chmod(filepath.Join(bindingsDir, "a.ts"), 0o000))

		err := App{}.BindingsCheck()

		require.Error(t, err)
		assert.NotErrorIs(t, err, errBindingsDrift)
	})
}

func TestWailsCLIUsesTheVersionGoModPins(t *testing.T) {
	t.Parallel()

	requireTool(t, "go")

	cli, err := wailsCLI()

	require.NoError(t, err)
	assert.Regexp(t, "^"+regexp.QuoteMeta(wailsModule+"/cmd/wails3@")+`v3\.`, cli)
}

//nolint:paralleltest // changes the process's working directory and PATH
func TestAppDevStopsTheDevServerWhenTheAppFails(t *testing.T) {
	dir := inTempDir(t)
	require.NoError(t, os.Mkdir(filepath.Join(dir, frontendDir), 0o750))

	sleep := requireTool(t, "sleep")
	log := fakePNPM(t, `if [ "$1" = dev ]; then exec `+sleep+` 30; fi`)

	err := App{}.Dev() // go is not on PATH, so the app cannot start

	require.Error(t, err)
	assert.Contains(t, err.Error(), "run the desktop app")
	assert.Equal(t, "install --frozen-lockfile", logLines(t, log)[0])
	assert.FileExists(t, distIndex)
}

//nolint:paralleltest // changes the process's working directory and PATH
func TestAppDevNeedsPNPM(t *testing.T) {
	inTempDir(t)
	isolatedPath(t)

	require.ErrorIs(t, App{}.Dev(), errPNPMMissing)
}

//nolint:paralleltest // changes the process's working directory
func TestMacOSTargetsRefuseOtherSystems(t *testing.T) {
	skipOnMacOS(t)
	inTempDir(t)

	targets := []struct {
		name string
		run  func() error
	}{
		{name: "app:build", run: App{}.Build},
		{name: "app:open", run: App{}.Open},
		{name: "app:run", run: App{}.Run},
		{name: "brand:icons", run: Brand{}.Icons},
		{name: "measure:startup", run: Measure{}.Startup},
		{name: "measure:refresh", run: Measure{}.Refresh},
		{name: "measure:launch", run: Measure{}.Launch},
		{name: "measure:size", run: Measure{}.Size},
		{name: "measure:all", run: Measure{}.All},
	}

	for _, target := range targets {
		t.Run(target.name, func(t *testing.T) {
			require.ErrorIs(t, target.run(), errNeedsMacOS)
		})
	}
}

//nolint:paralleltest // changes the process's working directory
func TestAppBundleOnMacOS(t *testing.T) {
	onlyOnMacOS(t)

	dir := inTempDir(t)

	_, _, err := appBundle()
	require.ErrorIs(t, err, errNoBundle)

	writeFile(t, filepath.Join(dir, "bin", brand.Name+".app", "Contents", "MacOS", brand.Slug), "")

	bundle, exe, err := appBundle()

	require.NoError(t, err)
	assert.Equal(t, filepath.Join("bin", brand.Name+".app"), bundle)
	assert.True(t, filepath.IsAbs(exe), "the executable path is absolute, so pgrep can match it")
	assert.True(t, strings.HasSuffix(exe, filepath.Join(bundle, "Contents", "MacOS", brand.Slug)))
}

//nolint:paralleltest // changes the process's PATH
func TestAppProcesses(t *testing.T) {
	const exe = "/Applications/Example.app/Contents/MacOS/example"

	tests := []struct {
		name    string
		pgrep   string
		want    []int
		wantErr string
	}{
		{name: "running copies", pgrep: "echo 12; echo 345", want: []int{12, 345}},
		{name: "none running", pgrep: "exit 1", want: nil},
		{name: "pgrep fails", pgrep: "exit 2", wantErr: "find the running app"},
		{name: "unexpected output", pgrep: "echo twelve", wantErr: `parse the pgrep output "twelve"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeTool(t, isolatedPath(t), "pgrep", tt.pgrep)

			got, err := appProcesses(exe)

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
		log := fakeLoggedTool(t, isolatedPath(t), "pgrep", "exit 1")

		_, err := appProcesses(exe)

		require.NoError(t, err)
		assert.Equal(t, []string{"-f ^" + regexp.QuoteMeta(exe) + "( |$)"}, logLines(t, log))
	})
}

// helperSleepEnv, set to "1", makes TestHelperSleep sleep rather than skip.
const helperSleepEnv = "MAGEFILES_TEST_HELPER_SLEEP"

// TestHelperSleep is not a test of its own: run by TestAppProcessesFindsARealProcess with
// helperSleepEnv set, it sleeps until it is stopped, as a process of the test's own under
// a path of the test's choosing. Any other time it skips.
func TestHelperSleep(t *testing.T) {
	t.Parallel()

	if os.Getenv(helperSleepEnv) != "1" {
		t.Skip("a helper process for TestAppProcessesFindsARealProcess")
	}

	time.Sleep(time.Minute)
}

//nolint:paralleltest // changes the process's standard output
func TestAppProcessesFindsARealProcess(t *testing.T) {
	requireProcessList(t)

	self, err := os.Executable()
	require.NoError(t, err)

	exe := filepath.Join(t.TempDir(), brand.Name+".app", "Contents", "MacOS", brand.Slug)

	// The app's path links to this test binary, which runs TestHelperSleep under the app's
	// path as its command name. A system tool would not do: a multi-call build of sleep
	// (as on Ubuntu) picks what it does by the name it runs under, and exits at once.
	require.NoError(t, os.MkdirAll(filepath.Dir(exe), 0o750))
	require.NoError(t, os.Symlink(self, exe))

	helper := exec.CommandContext(t.Context(), exe, "-test.run=^TestHelperSleep$") //nolint:gosec // the test's own binary, fixed arguments

	helper.Env = append(os.Environ(), helperSleepEnv+"=1")
	pid, done := startProcess(t, helper)

	assert.Eventually(t, func() bool {
		pids, listErr := appProcesses(exe)

		return listErr == nil && slices.Equal(pids, []int{pid})
	}, 5*time.Second, 20*time.Millisecond)

	var quit bool

	captureStdout(t, func() { quit, err = quitRunningApp(exe, 5*time.Second) })

	require.NoError(t, err)
	assert.True(t, quit)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		assert.Fail(t, "the app did not quit")
	}
}

//nolint:paralleltest // changes the process's PATH, environment, and standard output
func TestQuitRunningApp(t *testing.T) {
	const exe = "/Applications/Example.app/Contents/MacOS/example"

	t.Run("nothing running", func(t *testing.T) {
		fakeTool(t, isolatedPath(t), "pgrep", "exit 1")

		quit, err := quitRunningApp(exe, time.Second)

		require.NoError(t, err)
		assert.False(t, quit)
	})

	t.Run("waits until the running copy has quit", func(t *testing.T) {
		t.Setenv("FAKE_PID", strconv.Itoa(startSleeper(t)))
		t.Setenv("PGREP_STATE", filepath.Join(t.TempDir(), "listed"))
		fakeTool(t, isolatedPath(t), "pgrep", `if [ -e "$PGREP_STATE" ]; then exit 1; fi
: > "$PGREP_STATE"
echo "$FAKE_PID"`)

		var (
			quit bool
			err  error
		)

		out := captureStdout(t, func() { quit, err = quitRunningApp(exe, 5*time.Second) })

		require.NoError(t, err)
		assert.True(t, quit)
		assert.Equal(t, "Quitting the running "+brand.Name+"...\n", out)
	})

	t.Run("gives up when it does not quit", func(t *testing.T) {
		t.Setenv("FAKE_PID", strconv.Itoa(startSleeper(t)))
		fakeTool(t, isolatedPath(t), "pgrep", `echo "$FAKE_PID"`)

		var (
			quit bool
			err  error
		)

		captureStdout(t, func() { quit, err = quitRunningApp(exe, 50*time.Millisecond) })

		require.ErrorIs(t, err, errStillRunning)
		assert.True(t, quit)
	})

	t.Run("pgrep fails", func(t *testing.T) {
		fakeTool(t, isolatedPath(t), "pgrep", "exit 2")

		quit, err := quitRunningApp(exe, time.Second)

		require.Error(t, err)
		assert.False(t, quit)
	})
}

//nolint:paralleltest // changes the process's environment
func TestOpenAfterQuit(t *testing.T) {
	const bundle = "bin/Example.app"

	t.Run("opens the bundle", func(t *testing.T) {
		log := fakeOpen(t, "exit 0")

		require.NoError(t, openAfterQuit(bundle, time.Second))

		assert.Equal(t, []string{bundle}, logLines(t, log))
	})

	t.Run("tries again while the old copy is still listed", func(t *testing.T) {
		t.Setenv("OPEN_STATE", filepath.Join(t.TempDir(), "tried"))
		log := fakeOpen(t, `if [ -e "$OPEN_STATE" ]; then exit 0; fi
: > "$OPEN_STATE"
echo "procNotFound (-600)"
exit 1`)

		require.NoError(t, openAfterQuit(bundle, 5*time.Second))

		assert.Equal(t, []string{bundle, bundle}, logLines(t, log))
	})

	t.Run("reports the last failure", func(t *testing.T) {
		fakeOpen(t, `echo "procNotFound (-600)"; exit 1`)

		err := openAfterQuit(bundle, 0)

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

		cmd := exec.CommandContext(t.Context(), requireTool(t, "sleep"), "30") //nolint:gosec // the test's own process, fixed arguments
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
		version:       "v1.2.3-4-gabc1234",
		commit:        "abc1234",
		date:          "2026-11-07T00:00:00Z",
		bundleVersion: "1.2.3",
		bundleBuild:   "42",
	}

	assert.Equal(t,
		"-X main.version=v1.2.3-4-gabc1234 -X main.commit=abc1234 -X main.buildDate=2026-11-07T00:00:00Z "+
			"-X main.release=1.2.3 -X main.buildNumber=42",
		versionFlags(meta))
}

//nolint:paralleltest // changes the process's working directory
func TestVersionFlagsStampDeclaredVariables(t *testing.T) {
	inRepoRoot(t)

	declared := packageVars(t, filepath.Join("cmd", brand.Slug, "main_desktop.go"))
	stamped := regexp.MustCompile(`-X main\.(\w+)=`).FindAllStringSubmatch(versionFlags(buildMeta{}), -1)

	require.NotEmpty(t, stamped)

	for _, match := range stamped {
		assert.Contains(t, declared, match[1], "the linker ignores -X for an undeclared variable")
	}
}

func TestMacOSMinimumMatchesTheInfoPlist(t *testing.T) {
	t.Parallel()

	assert.Equal(t, brandgen.MinimumMacOS, macOSMinVersion, "the binary and the Info.plist name the same oldest macOS")
}

//nolint:paralleltest // changes the process's working directory and environment
func TestReadBuildMeta(t *testing.T) {
	t.Run("outside a git checkout", func(t *testing.T) {
		isolateGit(t)
		dir := inTempDir(t)
		t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))

		meta := readBuildMeta()

		assert.Equal(t, "dev", meta.version)
		assert.Equal(t, "none", meta.commit)
		assert.Equal(t, brandgen.PlaceholderVersion, meta.bundleVersion)
		assert.Equal(t, brandgen.PlaceholderVersion, meta.bundleBuild)

		_, err := time.Parse(time.RFC3339, meta.date)
		require.NoError(t, err)
	})

	t.Run("before the first tag", func(t *testing.T) {
		isolateGit(t)
		inTempDir(t)
		git(t, "init", "-q")
		git(t, "commit", "-q", "--allow-empty", "-m", "first")
		head := git(t, "rev-parse", "--short", "HEAD")

		meta := readBuildMeta()

		assert.Equal(t, head, meta.version)
		assert.Equal(t, head, meta.commit)
		assert.Equal(t, brandgen.PlaceholderVersion, meta.bundleVersion)
		assert.Equal(t, "1", meta.bundleBuild)
	})

	t.Run("after a tag", func(t *testing.T) {
		isolateGit(t)
		inTempDir(t)
		git(t, "init", "-q")
		git(t, "commit", "-q", "--allow-empty", "-m", "first")
		git(t, "tag", "v1.2.3")
		git(t, "commit", "-q", "--allow-empty", "-m", "second")
		head := git(t, "rev-parse", "--short", "HEAD")

		meta := readBuildMeta()

		assert.Equal(t, "v1.2.3-1-g"+head, meta.version)
		assert.Equal(t, head, meta.commit)
		assert.Equal(t, "1.2.3", meta.bundleVersion)
		assert.Equal(t, "2", meta.bundleBuild)
	})

	t.Run("uncommitted changes", func(t *testing.T) {
		isolateGit(t)
		dir := inTempDir(t)
		git(t, "init", "-q")
		writeFile(t, filepath.Join(dir, "a.txt"), "a")
		git(t, "add", "a.txt")
		git(t, "commit", "-q", "-m", "first")
		git(t, "tag", "v0.1.0")
		writeFile(t, filepath.Join(dir, "a.txt"), "b")

		assert.Equal(t, "v0.1.0-dirty", readBuildMeta().version)
	})
}

//nolint:paralleltest // changes the process's PATH
func TestBuildDesktopBinary(t *testing.T) {
	meta := buildMeta{
		version:       "v1.0.0",
		commit:        "abc1234",
		date:          "2026-11-07T00:00:00Z",
		bundleVersion: "1.0.0",
		bundleBuild:   "7",
	}

	t.Run("builds the tagged production binary for the oldest supported macOS", func(t *testing.T) {
		log := fakeGo(t, `printf '%s|%s|%s|%s\n' "$CGO_ENABLED" "$CGO_CFLAGS" "$CGO_LDFLAGS" "$MACOSX_DEPLOYMENT_TARGET" >> "$log"`)
		minVersion := "-mmacosx-version-min=" + macOSMinVersion

		require.NoError(t, buildDesktopBinary("bin/app", meta))

		assert.Equal(t, []string{
			"build -tags desktop,production -trimpath -buildvcs=false -ldflags -s -w " + versionFlags(meta) +
				" -o bin/app ./cmd/" + brand.Slug,
			"1|" + minVersion + "|" + minVersion + "|" + macOSMinVersion,
		}, logLines(t, log))
	})

	t.Run("quiets Apple's newer linker about duplicate libraries", func(t *testing.T) {
		dir := isolatedPath(t)
		fakeTool(t, dir, "ld", `echo "@(#)PROGRAM:ld PROJECT:ld-1167.5" >&2`)
		log := fakeLoggedTool(t, dir, "go", "")

		require.NoError(t, buildDesktopBinary("bin/app", meta))

		assert.Equal(t, []string{
			"build -tags desktop,production -trimpath -buildvcs=false -ldflags -s -w " + versionFlags(meta) +
				" -extldflags=-Wl,-no_warn_duplicate_libraries -o bin/app ./cmd/" + brand.Slug,
		}, logLines(t, log))
	})

	t.Run("the build fails", func(t *testing.T) {
		fakeGo(t, "exit 1")

		err := buildDesktopBinary("bin/app", meta)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "build the desktop binary")
	})
}

//nolint:paralleltest // changes the process's PATH
func TestQuietLinkerFlags(t *testing.T) {
	const quiet = " -extldflags=-Wl,-no_warn_duplicate_libraries"

	tests := []struct {
		name string
		ld   string // the fake ld's body, or "" for no ld on PATH
		want string
	}{
		{name: "Apple's linker since Xcode 15", ld: `echo "@(#)PROGRAM:ld PROJECT:ld-1167.5" >&2`, want: quiet},
		{name: "whatever ld -v exits with", ld: `echo "@(#)PROGRAM:ld PROJECT:ld-1115.7.3" >&2; exit 1`, want: quiet},
		{name: "Apple's older ld64, which does not know the flag", ld: `echo "@(#)PROGRAM:ld PROJECT:ld64-820.1" >&2`},
		{name: "a GNU linker", ld: `echo "GNU ld (GNU Binutils) 2.42"`},
		{name: "no linker on PATH"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := isolatedPath(t)
			if tt.ld != "" {
				fakeTool(t, dir, "ld", tt.ld)
			}

			assert.Equal(t, tt.want, quietLinkerFlags())
		})
	}
}

//nolint:paralleltest // changes the process's working directory and PATH
func TestWriteInfoPlist(t *testing.T) {
	meta := buildMeta{bundleVersion: "1.2.3", bundleBuild: "42"}

	t.Run("copies the plist and stamps the versions", func(t *testing.T) {
		dir := inTempDir(t)
		writeFile(t, filepath.FromSlash(brandgen.InfoPlistPath), "<plist/>")

		log := fakeLoggedTool(t, isolatedPath(t), "plutil", "")

		dst := filepath.Join(dir, "Info.plist")

		require.NoError(t, writeInfoPlist(dst, meta))

		assert.Equal(t, "<plist/>", readFile(t, dst), "the committed plist keeps its placeholder version")
		assert.Equal(t, []string{
			"-replace CFBundleShortVersionString -string 1.2.3 " + dst,
			"-replace CFBundleVersion -string 42 " + dst,
		}, logLines(t, log))
	})

	t.Run("plist missing", func(t *testing.T) {
		dir := inTempDir(t)

		err := writeInfoPlist(filepath.Join(dir, "Info.plist"), meta)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "read the Info.plist")
	})

	t.Run("bundle missing", func(t *testing.T) {
		dir := inTempDir(t)
		writeFile(t, filepath.FromSlash(brandgen.InfoPlistPath), "<plist/>")

		err := writeInfoPlist(filepath.Join(dir, "missing", "Info.plist"), meta)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "write the bundle Info.plist")
	})

	t.Run("stamping fails", func(t *testing.T) {
		dir := inTempDir(t)
		writeFile(t, filepath.FromSlash(brandgen.InfoPlistPath), "<plist/>")
		fakeTool(t, isolatedPath(t), "plutil", "exit 1")

		err := writeInfoPlist(filepath.Join(dir, "Info.plist"), meta)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "stamp CFBundleShortVersionString")
	})
}

//nolint:paralleltest // changes the process's working directory
func TestCheckBrandFiles(t *testing.T) {
	t.Run("the committed files are current", func(t *testing.T) {
		inRepoRoot(t)

		require.NoError(t, checkBrandFiles())
	})

	t.Run("generated files missing", func(t *testing.T) {
		inTempDir(t)

		err := checkBrandFiles()

		require.ErrorIs(t, err, brandgen.ErrStale)
		assert.Contains(t, err.Error(), "generated brand files")
	})

	t.Run("icons missing", func(t *testing.T) {
		dir := inTempDir(t)
		require.NoError(t, brandgen.Write(dir))

		err := checkBrandFiles()

		require.ErrorIs(t, err, brandgen.ErrStaleIcons)
		assert.Contains(t, err.Error(), "app icons")
	})
}

//nolint:paralleltest // changes the process's working directory
func TestCopyIconSet(t *testing.T) {
	t.Run("copies the icon set under the name the Info.plist gives it", func(t *testing.T) {
		dir := inTempDir(t)
		writeFile(t, filepath.FromSlash(brandgen.IconSetPath), "icns")

		resources := filepath.Join(dir, "Contents", "Resources")

		require.NoError(t, copyIconSet(resources))

		assert.Equal(t, "icns", readFile(t, filepath.Join(resources, brandgen.IconFile+".icns")))
	})

	t.Run("icon set missing", func(t *testing.T) {
		dir := inTempDir(t)

		err := copyIconSet(filepath.Join(dir, "Resources"))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "read the app icon set")
	})

	t.Run("resources is a file", func(t *testing.T) {
		dir := inTempDir(t)
		writeFile(t, filepath.FromSlash(brandgen.IconSetPath), "icns")

		resources := filepath.Join(dir, "Resources")
		writeFile(t, resources, "")

		err := copyIconSet(resources)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "create the bundle resources")
	})

	t.Run("icon path is a directory", func(t *testing.T) {
		dir := inTempDir(t)
		writeFile(t, filepath.FromSlash(brandgen.IconSetPath), "icns")

		resources := filepath.Join(dir, "Resources")
		require.NoError(t, os.MkdirAll(filepath.Join(resources, brandgen.IconFile+".icns"), 0o750))

		err := copyIconSet(resources)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "write the bundle app icon")
	})
}

//nolint:paralleltest // changes the process's standard output
func TestReportSize(t *testing.T) {
	t.Run("prints the bundle and its binary's size", func(t *testing.T) {
		exe := filepath.Join(t.TempDir(), "app")
		writeFile(t, exe, strings.Repeat("x", 1500))

		var err error

		out := captureStdout(t, func() { err = reportSize("bin/Example.app", exe) })

		require.NoError(t, err)
		assert.Equal(t, "✅ Built bin/Example.app (binary 1500 bytes, 0.0 MB)\n", out)
	})

	t.Run("binary missing", func(t *testing.T) {
		err := reportSize("bin/Example.app", filepath.Join(t.TempDir(), "missing"))

		require.Error(t, err)
		assert.Contains(t, err.Error(), "read the binary size")
	})
}

//nolint:paralleltest // changes the process's working directory, PATH, and standard output
func TestBuildBundle(t *testing.T) {
	bundle := filepath.Join("bin", "Example.app")
	exe := filepath.Join(bundle, "Contents", "MacOS", brand.Slug)

	t.Run("assembles, stamps, and signs the bundle", func(t *testing.T) {
		dir := inTempDir(t)
		checkout(t, dir)
		_, log := appTools(t)
		writeFile(t, filepath.Join(bundle, "stale.txt"), "from the previous build")

		var err error

		out := captureStdout(t, func() { err = buildBundle(bundle) })

		require.NoError(t, err)
		assert.Equal(t, "binary", readFile(t, exe))
		assert.Equal(t, readFile(t, filepath.FromSlash(brandgen.InfoPlistPath)), readFile(t, filepath.Join(bundle, "Contents", "Info.plist")))
		assert.Equal(t, "png", readFile(t, filepath.Join(bundle, "Contents", "Resources", brandgen.IconFile+".icns")))
		assert.NoFileExists(t, filepath.Join(bundle, "stale.txt"), "the previous bundle is removed first")
		assert.Contains(t, out, "✅ Built "+bundle+" (binary 6 bytes")

		lines := logLines(t, log)
		require.Len(t, lines, 6)
		assert.Equal(t, []string{"pnpm install --frozen-lockfile", "pnpm build"}, lines[:2])
		assert.True(t, strings.HasPrefix(lines[2], "go build -tags desktop,production -trimpath -buildvcs=false -ldflags -s -w -X main.version="), lines[2])
		assert.True(t, strings.HasSuffix(lines[2], " -o "+exe+" ./cmd/"+brand.Slug), lines[2])

		plist := filepath.Join(bundle, "Contents", "Info.plist")
		assert.Equal(t, []string{
			"plutil -replace CFBundleShortVersionString -string " + brandgen.PlaceholderVersion + " " + plist,
			"plutil -replace CFBundleVersion -string " + brandgen.PlaceholderVersion + " " + plist,
			"codesign --force --sign - " + bundle,
		}, lines[3:])
	})

	t.Run("stale brand files stop the build before any tool runs", func(t *testing.T) {
		inTempDir(t)
		_, log := appTools(t)

		require.ErrorIs(t, buildBundle(bundle), brandgen.ErrStale)
		assert.NoFileExists(t, log)
	})

	tests := []struct {
		name    string
		tool    string
		body    string
		wantErr string
	}{
		{name: "the frontend build fails", tool: "pnpm", body: "exit 1", wantErr: "pnpm [install --frozen-lockfile]"},
		{name: "the compile fails", tool: "go", body: "exit 1", wantErr: "build the desktop binary"},
		{name: "stamping fails", tool: "plutil", body: "exit 1", wantErr: "stamp CFBundleShortVersionString"},
		{name: "signing fails", tool: "codesign", body: "exit 1", wantErr: "sign the bundle ad hoc"},
		{name: "no binary to report", tool: "go", body: "exit 0", wantErr: "read the binary size"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := inTempDir(t)
			checkout(t, dir)
			tools, _ := appTools(t)
			fakeTool(t, tools, tt.tool, tt.body)

			err := buildBundle(bundle)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}

	t.Run("the bundle directory cannot be created", func(t *testing.T) {
		dir := inTempDir(t)
		checkout(t, dir)
		appTools(t)
		writeFile(t, "bin", "")

		require.Error(t, buildBundle(bundle))
	})
}

//nolint:paralleltest // changes the process's working directory, PATH, and standard output
func TestAppTargetsOnMacOS(t *testing.T) {
	onlyOnMacOS(t)

	bundle := filepath.Join("bin", brand.Name+".app")
	exe := filepath.Join(bundle, "Contents", "MacOS", brand.Slug)

	t.Run("open builds a missing bundle, then opens it", func(t *testing.T) {
		dir := inTempDir(t)
		checkout(t, dir)
		_, log := appTools(t)

		captureStdout(t, func() { require.NoError(t, App{}.Open()) })

		lines := logLines(t, log)
		assert.Contains(t, lines, "codesign --force --sign - "+bundle)
		assert.Equal(t, "open "+bundle, lines[len(lines)-1])
	})

	t.Run("open opens an existing bundle as it is", func(t *testing.T) {
		inTempDir(t)
		_, log := appTools(t)
		writeFile(t, exe, "")

		require.NoError(t, App{}.Open())

		assert.Equal(t, []string{"open " + bundle}, logLines(t, log))
	})

	t.Run("run builds, then opens the fresh bundle", func(t *testing.T) {
		dir := inTempDir(t)
		checkout(t, dir)
		_, log := appTools(t)

		captureStdout(t, func() { require.NoError(t, App{}.Run()) })

		lines := logLines(t, log)
		assert.True(t, strings.HasPrefix(lines[len(lines)-2], "pgrep -f ^"), "it looks for a running copy first")
		assert.Equal(t, "open "+bundle, lines[len(lines)-1])
	})

	t.Run("check runs the drift check, the desktop lint, the desktop tests, and the build", func(t *testing.T) {
		dir := inTempDir(t)
		checkout(t, dir)
		_, log := appTools(t)
		require.NoError(t, App{}.Bindings())

		captureStdout(t, func() { require.NoError(t, App{}.Check()) })

		lines := logLines(t, log)
		lint := slices.Index(lines, "golangci-lint run --build-tags desktop --config .golangci.json --timeout 10m ./cmd/"+
			brand.Slug+"/... ./frontend/")
		tests := slices.Index(lines, "go test -tags desktop ./cmd/"+brand.Slug+"/")

		require.NotEqual(t, -1, lint, "the desktop lint")
		require.NotEqual(t, -1, tests, "the desktop tests")
		assert.Less(t, lint, tests, "the tests run after the lint")
		assert.Equal(t, "codesign --force --sign - "+bundle, lines[len(lines)-1])
	})

	t.Run("test runs the desktop-tagged tests, with a frontend to embed", func(t *testing.T) {
		inTempDir(t)
		_, log := appTools(t)

		require.NoError(t, App{}.Test())

		assert.Equal(t, []string{"go test -tags desktop ./cmd/" + brand.Slug + "/"}, logLines(t, log))
		assert.FileExists(t, distIndex, "the embed needs a page, so a placeholder stands in")
	})
}
