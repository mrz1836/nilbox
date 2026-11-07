package repoguard_test

import (
	"bytes"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
)

// skipDirs are never scanned when walking the tree without git.
var skipDirs = map[string]bool{ //nolint:gochecknoglobals // read-only lookup table
	".git": true, "node_modules": true, "bin": true, ".mage-cache": true,
	"dist": true, "coverage": true, ".idea": true,
}

// generatedPattern matches Go's standard generated-file header.
var generatedPattern = regexp.MustCompile(`(?m)^// Code generated .* DO NOT EDIT\.$`)

// moduleRoot walks up from the test's directory to the directory holding go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	require.NoError(t, err)

	for {
		if _, err = os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "go.mod not found above the test directory")

		dir = parent
	}
}

// modulePath reads the module path from go.mod.
func modulePath(t *testing.T, root string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(root, "go.mod")) //nolint:gosec // reading this repo's own go.mod
	require.NoError(t, err)

	m := regexp.MustCompile(`(?m)^module\s+(\S+)`).FindSubmatch(body)
	require.NotNil(t, m, "go.mod has no module line")

	return string(m[1])
}

// repoFiles lists the repository's files (tracked, plus untracked files that are not
// ignored) as slash-separated paths relative to root. Without git it walks the tree.
func repoFiles(t *testing.T, root string) []string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", "-C", root, "ls-files", "-z", //nolint:gosec // argv only, no shell
		"--cached", "--others", "--exclude-standard")

	out, err := cmd.Output()
	if err != nil {
		t.Logf("git ls-files unavailable (%v); walking the tree instead", err)

		return walkFiles(t, root)
	}

	var files []string

	for _, f := range strings.Split(string(out), "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}

	return files
}

// walkFiles lists the files under root, skipping build output and dependency trees.
func walkFiles(t *testing.T, root string) []string {
	t.Helper()

	var files []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		switch {
		case walkErr != nil:
			return walkErr
		case d.IsDir() && path != root && skipDirs[d.Name()]:
			return filepath.SkipDir
		case d.IsDir():
			return nil
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}

		files = append(files, filepath.ToSlash(rel))

		return nil
	})
	require.NoError(t, err)

	return files
}

// readText returns a file's contents, or ok=false for a binary or missing file.
func readText(t *testing.T, root, rel string) (string, bool) {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel))) //nolint:gosec // paths come from this repo's file list
	if errors.Is(err, fs.ErrNotExist) {
		return "", false // deleted in the working tree but still in the index
	}

	require.NoError(t, err)

	if bytes.IndexByte(body, 0) >= 0 {
		return "", false
	}

	return string(body), true
}

// goFiles returns the repository's Go files.
func goFiles(t *testing.T, root string) []string {
	t.Helper()

	var files []string

	for _, f := range repoFiles(t, root) {
		if strings.HasSuffix(f, ".go") {
			files = append(files, f)
		}
	}

	require.NotEmpty(t, files)

	return files
}

// hits returns "file:line: text" for every line of text that matches.
func hits(rel, text string, match func(line string) bool) []string {
	var found []string

	for i, line := range strings.Split(text, "\n") {
		if match(line) {
			found = append(found, fmt.Sprintf("%s:%d: %s", rel, i+1, strings.TrimSpace(line)))
		}
	}

	return found
}

func TestNoAutoMigrateInProductionCode(t *testing.T) {
	root := moduleRoot(t)
	needle := "Auto" + "Migrate("

	var found []string

	for _, rel := range goFiles(t, root) {
		if strings.HasSuffix(rel, "_test.go") {
			continue
		}

		if text, ok := readText(t, root, rel); ok {
			found = append(found, hits(rel, text, func(line string) bool { return strings.Contains(line, needle) })...)
		}
	}

	assert.Empty(t, found, "the schema comes from SQL migrations only; never create it from models")
}

func TestDataLayerBuildsWithoutCGO(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("the go command is not available")
	}

	root := moduleRoot(t)

	build := exec.CommandContext(t.Context(), "go", "build", "./internal/...")
	build.Dir = root

	build.Env = append(os.Environ(), "CGO_ENABLED=0")

	out, err := build.CombinedOutput()
	require.NoError(t, err, "CGO_ENABLED=0 go build ./internal/... failed:\n%s", out)

	// A cgo package can still build with CGO_ENABLED=0 by compiling a stub that fails at
	// run time, so also require that no dependency has cgo files when cgo is available.
	list := exec.CommandContext(t.Context(), "go", "list", "-deps",
		"-f", "{{if and (not .Standard) .CgoFiles}}{{.ImportPath}}{{end}}", "./internal/...")
	list.Dir = root

	list.Env = append(os.Environ(), "CGO_ENABLED=1")

	out, err = list.Output()
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(string(out)), "these dependencies of internal/ use cgo")
}

func TestNoAbsoluteHomePaths(t *testing.T) {
	root := moduleRoot(t)

	// Built from parts so this file does not match itself. The look-behind-style prefix
	// keeps URL path segments (such as example.com/home/page) from matching.
	pattern := regexp.MustCompile(`(^|[^A-Za-z0-9._~-])/(` + "Us" + "ers" + "|" + "ho" + "me" + `)/([^/\s"'` + "`" + `)]+)`)

	placeholders := map[string]bool{
		"me": true, "you": true, "user": true, "username": true, "runner": true, "example": true,
		"USER": true, "<user>": true, "<username>": true, "$USER": true, "${USER}": true,
	}

	var found []string

	for _, rel := range repoFiles(t, root) {
		text, ok := readText(t, root, rel)
		if !ok {
			continue
		}

		found = append(found, hits(rel, text, func(line string) bool {
			for _, m := range pattern.FindAllStringSubmatch(line, -1) {
				if !placeholders[m[3]] {
					return true
				}
			}

			return false
		})...)
	}

	assert.Empty(t, found, "use ~/projects or a placeholder home directory, never a real one")
}

// frontendBrandFile reports a frontend file the product-name guard scans: the page shell
// and every hand-written source under frontend/src. The generated brand files are exempt.
func frontendBrandFile(rel string) bool {
	if rel == "frontend/index.html" {
		return true
	}

	if !strings.HasPrefix(rel, "frontend/src/") || strings.HasPrefix(rel, "frontend/src/generated/") {
		return false
	}

	switch path.Ext(rel) {
	case ".ts", ".tsx", ".css":
		return true
	default:
		return false
	}
}

func TestFrontendBrandFile(t *testing.T) {
	cases := map[string]bool{
		"frontend/index.html":                   true,
		"frontend/src/App.tsx":                  true,
		"frontend/src/lib/theme.ts":             true,
		"frontend/src/index.css":                true,
		"frontend/src/components/App.test.tsx":  true,
		"frontend/src/generated/brand.ts":       false,
		"frontend/src/generated/theme.css":      false,
		"frontend/src/assets/logo.svg":          false,
		"frontend/package.json":                 false,
		"frontend/bindings/github.com/x/y/a.ts": false,
		"frontend/vite.config.ts":               false,
		"docs/frontend/src/notes.ts":            false,
	}

	for rel, want := range cases {
		assert.Equal(t, want, frontendBrandFile(rel), rel)
	}
}

// productNameText returns the text of a file the product-name guard scans, with the
// exempt module path and command directory removed, or ok=false for a file it skips:
// anything that is neither Go nor a frontend source, the brand package itself, and
// generated Go files.
func productNameText(t *testing.T, root, module, rel string) (string, bool) {
	t.Helper()

	isGo := strings.HasSuffix(rel, ".go")
	if (!isGo && !frontendBrandFile(rel)) || strings.HasPrefix(rel, "internal/brand/") {
		return "", false
	}

	text, ok := readText(t, root, rel)
	if !ok || (isGo && generatedPattern.MatchString(text)) {
		return "", false
	}

	// The module path is exempt everywhere (the frontend imports the bindings under it),
	// and the command directory's own name is exempt both inside it and in references
	// to it.
	text = strings.ReplaceAll(text, module, "")

	return strings.ReplaceAll(text, "cmd/"+brand.Slug, ""), true
}

func TestNoHardcodedProductName(t *testing.T) {
	root := moduleRoot(t)
	module := modulePath(t, root)
	cmdDir := "cmd/" + brand.Slug + "/"

	var found []string

	for _, rel := range repoFiles(t, root) {
		text, ok := productNameText(t, root, module, rel)
		if !ok {
			continue
		}

		inCmd := strings.HasPrefix(rel, cmdDir)

		found = append(found, hits(rel, text, func(line string) bool {
			return strings.Contains(line, brand.Name) || (!inCmd && strings.Contains(line, brand.Slug))
		})...)
	}

	assert.Empty(t, found, "reference the brand package (or the generated frontend brand file) instead of writing the product name")
}

func TestNoLiveGitHubCallsInTests(t *testing.T) {
	root := moduleRoot(t)

	// Built from parts so this file does not match itself.
	needle := "api." + "github" + ".com"

	var found []string

	for _, rel := range goFiles(t, root) {
		if !strings.HasSuffix(rel, "_test.go") {
			continue
		}

		if text, ok := readText(t, root, rel); ok {
			found = append(found, hits(rel, text, func(line string) bool { return strings.Contains(line, needle) })...)
		}
	}

	assert.Empty(t, found, "tests use local httptest servers or fakes, never the live GitHub API")
}

// branchDeleteNeedles are the ways Go code could spell a branch deletion: an argv
// ("branch", "-d") or a command string (branch -D), with each delete flag. They are built
// from parts so this file does not match itself.
func branchDeleteNeedles() []string {
	verb := "bran" + "ch"
	flags := []string{"-" + "d", "-" + "D", "--" + "delete"}
	needles := make([]string, 0, len(flags)*5)

	for _, flag := range flags {
		needles = append(needles,
			`"`+verb+`", "`+flag+`"`,
			`"`+verb+`","`+flag+`"`,
			verb+" "+flag+" ",
			verb+" "+flag+`"`,
			verb+" "+flag+"`",
		)
	}

	return needles
}

// containsAny reports whether line contains any of needles.
func containsAny(line string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(line, needle) {
			return true
		}
	}

	return false
}

func TestNoBranchDeletion(t *testing.T) {
	root := moduleRoot(t)
	needles := branchDeleteNeedles()

	var found []string

	for _, rel := range goFiles(t, root) {
		if strings.HasSuffix(rel, "_test.go") {
			continue
		}

		if text, ok := readText(t, root, rel); ok {
			found = append(found, hits(rel, text, func(line string) bool { return containsAny(line, needles) })...)
		}
	}

	assert.Empty(t, found, "removing worktrees must never delete a branch")
}

// cloneWriterFile is the only production file allowed to create a clone: the launcher's
// clone of a repository with no local clone, run after the user confirms it.
const cloneWriterFile = "internal/launcher/clone.go"

// argvLiteral matches any of words as a Go string literal passed as an argv element: a
// quoted word at the start of a line, or after whitespace, a comma, an opening
// parenthesis, or a brace, but not a struct tag such as json:"word".
func argvLiteral(words ...string) *regexp.Regexp {
	quoted := make([]string, 0, len(words))
	for _, w := range words {
		quoted = append(quoted, regexp.QuoteMeta(w))
	}

	return regexp.MustCompile(`(^|[\s,({])"(` + strings.Join(quoted, "|") + `)"`)
}

// productionGoHits returns "file:line: text" for every line of a non-test Go file that
// pattern matches, skipping the files in exempt.
func productionGoHits(t *testing.T, root string, pattern *regexp.Regexp, exempt ...string) []string {
	t.Helper()

	var found []string

	for _, rel := range goFiles(t, root) {
		if strings.HasSuffix(rel, "_test.go") || slices.Contains(exempt, rel) {
			continue
		}

		if text, ok := readText(t, root, rel); ok {
			found = append(found, hits(rel, text, pattern.MatchString)...)
		}
	}

	return found
}

func TestOnlyTheLauncherCreatesClones(t *testing.T) {
	root := moduleRoot(t)

	// Built from parts so this file does not match itself. "clone" is the argv word of
	// both `git clone` and `gh repo clone`.
	clone := argvLiteral("clo" + "ne")

	writer, ok := readText(t, root, cloneWriterFile)
	require.True(t, ok, "%s is gone; update this guard", cloneWriterFile)
	assert.NotEmpty(t, hits(cloneWriterFile, writer, clone.MatchString),
		"%s no longer runs the clone; update this guard", cloneWriterFile)

	assert.Empty(t, productionGoHits(t, root, clone, cloneWriterFile),
		"only the launcher's confirmed clone of a missing repository may create a clone")
}

// cloneChangingVerbs are the git subcommands that change an existing clone's checkout,
// index, history, or remotes' refs. Built from parts so this file does not match itself.
func cloneChangingVerbs() []string {
	return []string{
		"check" + "out", "sta" + "sh", "fet" + "ch", "swi" + "tch", "pu" + "ll", "re" + "set", "mer" + "ge",
		"re" + "base", "com" + "mit", "pu" + "sh", "res" + "tore", "cherry" + "-pick", "cle" + "an",
	}
}

func TestNoGitCommandChangesAClone(t *testing.T) {
	root := moduleRoot(t)
	verbs := argvLiteral(cloneChangingVerbs()...)
	worktreeAdd := regexp.MustCompile(`"work` + `tree",\s*"add"`)

	found := productionGoHits(t, root, verbs)
	found = append(found, productionGoHits(t, root, worktreeAdd)...)

	assert.Empty(t, found, "the app never changes an existing clone: no checkout, stash, fetch, branch "+
		"switch, pull, reset, merge, rebase, commit, push, restore, cherry-pick, clean, or worktree creation")
}

func TestArgvLiteral(t *testing.T) {
	t.Parallel()

	pattern := argvLiteral("fet"+"ch", "cherry"+"-pick")

	for line, want := range map[string]bool{
		`run(ctx, "git", "-C", dir, "fet` + `ch")`: true,
		`args := []string{"fet` + `ch", "origin"}`: true,
		`	"fet` + `ch",`:     true,
		`"cherry` + `-pick"`: true,
		`Reset int64 ` + "`" + `json:"fet` + `ch"` + "`": false,
		`msg := "fet` + `ch failed"`:                     false,
		`"prefetch"`:                                     false,
		`"cherry"`:                                       false,
	} {
		assert.Equal(t, want, pattern.MatchString(line), line)
	}

	worktreeAdd := regexp.MustCompile(`"work` + `tree",\s*"add"`)
	assert.True(t, worktreeAdd.MatchString(`"-C", dir, "work`+`tree", "add", path`))
	assert.False(t, worktreeAdd.MatchString(`"remote", "add", "origin"`))
}

// runsOnPattern matches a workflow `runs-on:` key and captures its inline value.
var runsOnPattern = regexp.MustCompile(`^(\s*)(?:-\s+)?runs-on:\s*(.*)$`)

// runnerEnvPattern matches a runner assignment in the CI env files, such as
// PRIMARY_RUNNER=ubuntu-26.04.
var runnerEnvPattern = regexp.MustCompile(`^\s*[A-Z0-9_]*_RUNNER=`)

// runnerValues returns "file:line: value" for every runner a workflow selects: the
// inline value of each runs-on key and the items of a runs-on list.
func runnerValues(rel, text string) []string {
	var (
		values     []string
		listIndent = -1
	)

	for i, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))

		if listIndent >= 0 && strings.HasPrefix(trimmed, "- ") && indent > listIndent {
			values = append(values, fmt.Sprintf("%s:%d: %s", rel, i+1, strings.TrimPrefix(trimmed, "- ")))

			continue
		}

		listIndent = -1

		m := runsOnPattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		value := strings.TrimSpace(m[2])
		if value == "" || strings.HasPrefix(value, "#") {
			listIndent = len(m[1])

			continue
		}

		values = append(values, fmt.Sprintf("%s:%d: %s", rel, i+1, value))
	}

	return values
}

// fileRunners returns the runner selections in one repository file: runs-on values in
// workflows and *_RUNNER assignments in the CI env files.
func fileRunners(rel, text string) []string {
	switch {
	case strings.HasPrefix(rel, ".github/workflows/") && (strings.HasSuffix(rel, ".yml") || strings.HasSuffix(rel, ".yaml")):
		return runnerValues(rel, text)
	case strings.HasPrefix(rel, ".github/env/") && strings.HasSuffix(rel, ".env"):
		return hits(rel, text, func(line string) bool { return runnerEnvPattern.MatchString(line) })
	default:
		return nil
	}
}

func TestNoMacOSRunners(t *testing.T) {
	root := moduleRoot(t)

	var found []string

	for _, rel := range repoFiles(t, root) {
		text, ok := readText(t, root, rel)
		if !ok {
			continue
		}

		for _, v := range fileRunners(rel, text) {
			if strings.Contains(strings.ToLower(v), "macos") {
				found = append(found, v)
			}
		}
	}

	assert.Empty(t, found, "CI runs on ubuntu runners only; macOS runners are not used")
}

func TestRunnerValues(t *testing.T) {
	t.Parallel()

	workflow := strings.Join([]string{
		"jobs:",
		"  a:",
		"    runs-on: ubuntu-26.04",
		"  b:",
		"    runs-on:",
		"      - self-hosted",
		"      - macos-15",
		"    steps:",
		"      - run: echo macos is mentioned here",
		"  c:",
		"    runs-on: ${{ matrix.os }} # comment",
	}, "\n")

	assert.Equal(t, []string{
		"w.yml:3: ubuntu-26.04",
		"w.yml:6: self-hosted",
		"w.yml:7: macos-15",
		"w.yml:11: ${{ matrix.os }} # comment",
	}, runnerValues("w.yml", workflow))
}

// fileImports returns the import paths of the Go file rel, whatever its build tags, or
// none for a file that cannot be read as text.
func fileImports(t *testing.T, root, rel string) []string {
	t.Helper()

	src, ok := readText(t, root, rel)
	if !ok {
		return nil
	}

	file, err := parser.ParseFile(token.NewFileSet(), rel, src, parser.ImportsOnly)
	require.NoError(t, err, rel)

	paths := make([]string, 0, len(file.Imports))
	for _, imp := range file.Imports {
		paths = append(paths, strings.Trim(imp.Path.Value, `"`))
	}

	return paths
}

// wailsBootstrapFiles are the only files allowed to import the desktop runtime: the
// desktop shell (the desktop main, the menu bar icon, and the notification banners, each
// in a *_desktop.go file), its per-OS window options, and the desktop-tagged frontend
// embed.
func wailsBootstrapFiles() []string {
	cmd := "cmd/" + brand.Slug + "/"

	return []string{cmd + "*_desktop.go", cmd + "window_*.go", "frontend/assets_desktop.go"}
}

// wailsAllowed reports whether the repository file rel may import the desktop runtime.
func wailsAllowed(rel string) bool {
	for _, pattern := range wailsBootstrapFiles() {
		if ok, _ := path.Match(pattern, rel); ok {
			return true
		}
	}

	return false
}

func TestOnlyDesktopBootstrapImportsWails(t *testing.T) {
	root := moduleRoot(t)

	// Built from parts so the needle reads as data, not as an import path.
	runtimePrefix := "github.com/" + "wailsapp/"

	var found []string

	// Every Go file is parsed whatever its build tags, so a tagged file cannot hide an
	// import from this check.
	for _, rel := range goFiles(t, root) {
		if wailsAllowed(rel) {
			continue
		}

		for _, imp := range fileImports(t, root, rel) {
			if strings.HasPrefix(imp, runtimePrefix) {
				found = append(found, rel+": "+imp)
			}
		}
	}

	assert.Empty(t, found, "only the desktop bootstrap may import the desktop runtime; keep services in plain Go")
}

func TestWailsAllowed(t *testing.T) {
	t.Parallel()

	cmd := "cmd/" + brand.Slug + "/"

	for rel, want := range map[string]bool{
		cmd + "main_desktop.go":             true,
		cmd + "window_darwin.go":            true,
		cmd + "window_other.go":             true,
		"frontend/assets_desktop.go":        true,
		cmd + "main.go":                     false,
		cmd + "sub/main_desktop.go":         false,
		"internal/core/core.go":             false,
		"internal/launcher/main_desktop.go": false,
		"frontend/other_desktop.go":         false,
	} {
		assert.Equal(t, want, wailsAllowed(rel), rel)
	}
}

// testSupportDir holds the test-support package: helpers and fakes the test suites share,
// which must never reach the app.
const testSupportDir = "internal/testutil"

func TestOnlyTestsImportTestSupport(t *testing.T) {
	root := moduleRoot(t)
	testSupport := modulePath(t, root) + "/" + testSupportDir

	require.DirExists(t, filepath.Join(root, filepath.FromSlash(testSupportDir)),
		"%s is gone; update this guard", testSupportDir)

	var found []string

	// Every Go file is parsed whatever its build tags, so a tagged file cannot hide an
	// import from this check.
	for _, rel := range goFiles(t, root) {
		if strings.HasSuffix(rel, "_test.go") {
			continue
		}

		for _, imp := range fileImports(t, root, rel) {
			if imp == testSupport || strings.HasPrefix(imp, testSupport+"/") {
				found = append(found, rel+": "+imp)
			}
		}
	}

	assert.Empty(t, found, "only _test.go files may import the test-support package")
}
