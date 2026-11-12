package repoguard_test

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
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

	root, err := findModuleRoot()
	require.NoError(t, err)

	return root
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

// readTextFile returns a file's contents, or ok=false for a binary or missing file, with
// the error a test would fail with.
func readTextFile(root, rel string) (string, bool, error) {
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel))) //nolint:gosec // paths come from this repo's file list
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil // deleted in the working tree but still in the index
	}

	if err != nil {
		return "", false, fmt.Errorf("read %s: %w", rel, err)
	}

	if bytes.IndexByte(body, 0) >= 0 {
		return "", false, nil
	}

	return string(body), true, nil
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

func TestDataLayerBuildsWithoutCGO(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	// Built from parts so this file does not match itself. The look-behind-style prefix
	// keeps URL path segments (such as example.com/home/page) from matching.
	pattern := regexp.MustCompile(`(^|[^A-Za-z0-9._~-])/(` + "Us" + "ers" + "|" + "ho" + "me" + `)/([^/\s"'` + "`" + `)]+)`)

	// Every match holds one of these, so a file without either is not matched line by
	// line.
	homes := []string{"/" + "Us" + "ers" + "/", "/" + "ho" + "me" + "/"}

	placeholders := map[string]bool{
		"me": true, "you": true, "user": true, "username": true, "runner": true, "example": true,
		"USER": true, "<user>": true, "<username>": true, "$USER": true, "${USER}": true,
	}

	realHome := func(line string) bool {
		for _, m := range pattern.FindAllStringSubmatch(line, -1) {
			if !placeholders[m[3]] {
				return true
			}
		}

		return false
	}

	var found []string

	for _, rel := range repoFiles(t) {
		if text, ok := readText(t, rel); ok && containsAny(text, homes) {
			found = append(found, hits(rel, text, realHome)...)
		}
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
	t.Parallel()

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
func productNameText(t *testing.T, module, rel string) (string, bool) {
	t.Helper()

	isGo := strings.HasSuffix(rel, ".go")
	if (!isGo && !frontendBrandFile(rel)) || strings.HasPrefix(rel, "internal/brand/") {
		return "", false
	}

	text, ok := readText(t, rel)
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
	t.Parallel()

	root := moduleRoot(t)
	module := modulePath(t, root)
	cmdDir := "cmd/" + brand.Slug + "/"

	var found []string

	for _, rel := range repoFiles(t) {
		text, ok := productNameText(t, module, rel)
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
	t.Parallel()

	// Built from parts so this file does not match itself.
	needle := "api." + "github" + ".com"

	var found []string

	for _, rel := range goFiles(t) {
		if !strings.HasSuffix(rel, "_test.go") {
			continue
		}

		if text, ok := readText(t, rel); ok {
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
	t.Parallel()

	needles := branchDeleteNeedles()

	var found []string

	for _, rel := range goFiles(t) {
		if strings.HasSuffix(rel, "_test.go") {
			continue
		}

		if text, ok := readText(t, rel); ok {
			found = append(found, hits(rel, text, func(line string) bool { return containsAny(line, needles) })...)
		}
	}

	assert.Empty(t, found, "a branch is deleted only with update-ref -d <ref> <commit>, which git refuses once the "+
		"branch has moved, so a commit made after the check is never lost; never with git branch -d or -D")
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
func productionGoHits(t *testing.T, pattern *regexp.Regexp, exempt ...string) []string {
	t.Helper()

	var found []string

	for _, rel := range goFiles(t) {
		if strings.HasSuffix(rel, "_test.go") || slices.Contains(exempt, rel) {
			continue
		}

		if text, ok := readText(t, rel); ok {
			found = append(found, hits(rel, text, pattern.MatchString)...)
		}
	}

	return found
}

func TestOnlyTheLauncherCreatesClones(t *testing.T) {
	t.Parallel()

	// Built from parts so this file does not match itself. "clone" is the argv word of
	// both `git clone` and `gh repo clone`.
	clone := argvLiteral("clo" + "ne")

	writer, ok := readText(t, cloneWriterFile)
	require.True(t, ok, "%s is gone; update this guard", cloneWriterFile)
	assert.NotEmpty(t, hits(cloneWriterFile, writer, clone.MatchString),
		"%s no longer runs the clone; update this guard", cloneWriterFile)

	assert.Empty(t, productionGoHits(t, clone, cloneWriterFile),
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

// worktreeMakerFile is the only production file allowed to fetch and to add a worktree:
// the worktrees package's Maker, which fetches a pull request's head or a default branch
// into the item's own refs and adds the item's worktree. Nothing changes a clone's own
// checkout.
const worktreeMakerFile = "internal/worktrees/create.go"

// clonePullerFile is the only production file allowed to pull: Repo scripts' pull of the
// branch a clone is on from the branch it follows on origin, fast-forward only, on the
// user's click. It runs no other git command that changes a clone.
const clonePullerFile = "internal/scripts/pull.go"

// TestNoGitCommandChangesAClone keeps every git command the app runs from changing a
// clone's checkout, with two exceptions, each held to one file: worktree creation
// (worktreeMakerFile) fetches and adds worktrees, and Repo scripts' pull (clonePullerFile)
// fast-forwards the branch a clone is on to origin's, on the user's click. Other work
// that changes one belongs to the integration's own tool, run on the user's click: a
// release's magex switches, pulls, tags, and pushes, and the app only starts it and reads,
// afterwards, where the tag and the clone are.
func TestNoGitCommandChangesAClone(t *testing.T) {
	t.Parallel()

	verbs := argvLiteral(cloneChangingVerbs()...)
	worktreeAdd := regexp.MustCompile(`"work` + `tree",\s*"add"`)

	// Built from parts so this file does not match itself.
	fetch := argvLiteral("fet" + "ch")
	changing := slices.DeleteFunc(cloneChangingVerbs(), func(v string) bool { return v == "fet"+"ch" })

	maker, ok := readText(t, worktreeMakerFile)
	require.True(t, ok, "%s is gone; update this guard", worktreeMakerFile)
	assert.NotEmpty(t, hits(worktreeMakerFile, maker, fetch.MatchString),
		"%s no longer fetches; update this guard", worktreeMakerFile)
	assert.NotEmpty(t, hits(worktreeMakerFile, maker, worktreeAdd.MatchString),
		"%s no longer adds worktrees; update this guard", worktreeMakerFile)
	assert.Empty(t, hits(worktreeMakerFile, maker, argvLiteral(changing...).MatchString),
		"%s only fetches and adds worktrees", worktreeMakerFile)

	assertMakerFetchesOnlyItsOwnRefs(t, maker)
	assertPullerOnlyFastForwards(t)

	found := productionGoHits(t, verbs, worktreeMakerFile, clonePullerFile)
	found = append(found, productionGoHits(t, worktreeAdd, worktreeMakerFile)...)

	assert.Empty(t, found, "nothing changes a clone's own checkout: no checkout, stash, branch switch, pull, reset, "+
		"merge, rebase, commit, push, restore, cherry-pick, or clean anywhere, only worktree creation ("+
		worktreeMakerFile+") fetches and adds worktrees, and only Repo scripts' pull ("+clonePullerFile+") pulls")
}

// TestNoGitCommandRunsAMergeOrDiffTool keeps the app from running git mergetool or git
// difftool anywhere: gitread lets a clone's own config name their programs
// (mergetool.<name>.cmd, difftool.<name>.cmd) as settings nothing the app runs reads, which
// holds only while no command the app runs is one of them.
func TestNoGitCommandRunsAMergeOrDiffTool(t *testing.T) {
	t.Parallel()

	// Built from parts so this file does not match itself.
	tools := argvLiteral("merge"+"tool", "diff"+"tool")

	assert.Empty(t, productionGoHits(t, tools),
		"no git command the app runs is mergetool or difftool, whose programs a clone's config may name")
}

// makerFetchWords are the only words worktree creation's fetch holds, besides the remote
// and the refspec: no tag, nothing fetched through the clone's own refspecs (--refmap=,
// which a forced map onto the clone's own branches would otherwise move), and git's own
// upload-pack; makerOptions are every option worktreeMakerFile may name, those and
// `worktree add`'s. Built from parts so this file does not match itself.
func makerFetchWords() []string {
	return []string{"fet" + "ch", "--no-tags", "--refmap=", "--upload-pack=git-upload-pack"}
}

func makerOptions() []string {
	return append(makerFetchWords()[1:], "--no-track", "-b")
}

// assertMakerFetchesOnlyItsOwnRefs checks worktreeMakerFile's text: it fetches with one
// argv literal whose words are exactly makerFetchWords, each a literal, and names no
// option but makerOptions anywhere, so no forced refspec, --update-head-ok, -c setting, or
// other upload-pack can be added beside.
func assertMakerFetchesOnlyItsOwnRefs(t *testing.T, text string) {
	t.Helper()

	words := makerFetchWords()
	argvs, options := verbLiterals(t, text, words[0])

	require.Len(t, argvs, 1, "%s fetches with one argv literal; update this guard", worktreeMakerFile)
	assert.ElementsMatch(t, words, argvs[0], "%s fetches with exactly these words, each a literal", worktreeMakerFile)

	for _, option := range options {
		assert.Contains(t, makerOptions(), option, "%s names an option it may not run git with", worktreeMakerFile)
	}
}

// pullWords are the only words Repo scripts' pull holds, besides origin's name and its
// refspec: the verb, then fast-forward only, never a rebase, nothing fetched through the
// clone's own refspecs and no tag, nothing set aside, no submodule, no signature program,
// no diff, and git's own upload-pack. Built from parts so this file does not match itself.
func pullWords() []string {
	return []string{
		"pu" + "ll", "--ff-only", "--no-re" + "base", "--refmap=", "--no-tags", "--no-auto" + "stash",
		"--no-recurse-submodules", "--no-verify-signatures", "--no-stat", "--upload-pack=git-upload-pack",
	}
}

// assertPullerOnlyFastForwards checks clonePullerFile: it still pulls, with one argv
// literal whose words are exactly pullWords, each a literal, so --ff-only, --refmap=, and
// --no-verify-signatures are there and nothing else is; every option it names anywhere is
// one of them, so no -c setting, short option, merge strategy, or --update-head-ok can be
// added beside; and it holds no other verb that changes a clone.
func assertPullerOnlyFastForwards(t *testing.T) {
	t.Helper()

	words := pullWords()
	pull := words[0]
	others := slices.DeleteFunc(cloneChangingVerbs(), func(v string) bool { return v == pull })

	text, ok := readText(t, clonePullerFile)
	require.True(t, ok, "%s is gone; update this guard", clonePullerFile)

	argvs, options := verbLiterals(t, text, pull)

	require.Len(t, argvs, 1, "%s pulls with one argv literal; update this guard", clonePullerFile)
	assert.ElementsMatch(t, words, argvs[0], "%s pulls with exactly these words, each a literal", clonePullerFile)

	for _, option := range options {
		assert.Contains(t, words, option, "%s names an option its pull may not take", clonePullerFile)
	}

	assert.Empty(t, hits(clonePullerFile, text, argvLiteral(others...).MatchString), "%s only pulls", clonePullerFile)
}

// verbLiterals parses the Go source text and returns the words of each []string literal
// whose first element is the string verb, a word that isn't a string literal kept as "?",
// and every string literal in it that starts with "-".
func verbLiterals(t *testing.T, text, verb string) (argvs [][]string, options []string) {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "src.go", text, parser.SkipObjectResolution)
	require.NoError(t, err)

	ast.Inspect(file, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CompositeLit:
			if words := literalWords(x.Elts); len(words) > 0 && words[0] == verb {
				argvs = append(argvs, words)
			}
		case *ast.BasicLit:
			if s, ok := stringLiteral(x); ok && strings.HasPrefix(s, "-") {
				options = append(options, s)
			}
		}

		return true
	})

	return argvs, options
}

// literalWords returns each expression's value as a string literal, or "?" for one that
// isn't.
func literalWords(exprs []ast.Expr) []string {
	words := make([]string, 0, len(exprs))

	for _, e := range exprs {
		s, ok := stringLiteral(e)
		if !ok {
			s = "?"
		}

		words = append(words, s)
	}

	return words
}

func TestVerbLiterals(t *testing.T) {
	t.Parallel()

	src := "package p\n\nfunc a(b string) []string {\n\treturn append([]string{\"pu" + "ll\", \"--ff-only\", x, " +
		"\"-r\"}, \"origin\", \"+\"+b)\n}\n\nvar c = []string{\"-X\", \"ours\"}\n"

	argvs, options := verbLiterals(t, src, "pu"+"ll")
	assert.Equal(t, [][]string{{"pu" + "ll", "--ff-only", "?", "-r"}}, argvs)
	assert.Equal(t, []string{"--ff-only", "-r", "-X"}, options)
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
	t.Parallel()

	var found []string

	for _, rel := range repoFiles(t) {
		text, ok := readText(t, rel)
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

// wailsBootstrapFiles are the only files allowed to import the desktop runtime: the
// desktop shell (the desktop main, the menu bar icon, and the notification banners, each
// in a *_desktop.go file) and its desktop-tagged tests (*_desktop_test.go), its per-OS
// window options, and the desktop-tagged frontend embed.
func wailsBootstrapFiles() []string {
	cmd := "cmd/" + brand.Slug + "/"

	return []string{cmd + "*_desktop.go", cmd + "*_desktop_test.go", cmd + "window_*.go", "frontend/assets_desktop.go"}
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
	t.Parallel()

	// Built from parts so the needle reads as data, not as an import path.
	runtimePrefix := "github.com/" + "wailsapp/"

	var found []string

	// Every Go file is parsed whatever its build tags, so a tagged file cannot hide an
	// import from this check.
	for _, rel := range goFiles(t) {
		if wailsAllowed(rel) {
			continue
		}

		for _, imp := range fileImports(t, rel) {
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
		cmd + "bound_desktop_test.go":       true,
		cmd + "window_darwin.go":            true,
		cmd + "window_other.go":             true,
		"frontend/assets_desktop.go":        true,
		cmd + "main.go":                     false,
		cmd + "sub/main_desktop.go":         false,
		cmd + "main_test.go":                false,
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
	t.Parallel()

	root := moduleRoot(t)
	testSupport := modulePath(t, root) + "/" + testSupportDir

	require.DirExists(t, filepath.Join(root, filepath.FromSlash(testSupportDir)),
		"%s is gone; update this guard", testSupportDir)

	var found []string

	// Every Go file is parsed whatever its build tags, so a tagged file cannot hide an
	// import from this check.
	for _, rel := range goFiles(t) {
		if strings.HasSuffix(rel, "_test.go") {
			continue
		}

		for _, imp := range fileImports(t, rel) {
			if imp == testSupport || strings.HasPrefix(imp, testSupport+"/") {
				found = append(found, rel+": "+imp)
			}
		}
	}

	assert.Empty(t, found, "only _test.go files may import the test-support package")
}
