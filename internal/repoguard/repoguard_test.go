package repoguard_test

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

func TestNoHardcodedProductName(t *testing.T) {
	root := moduleRoot(t)
	module := modulePath(t, root)
	brandDir := "internal/brand/"
	cmdDir := "cmd/" + brand.Slug

	var found []string

	for _, rel := range goFiles(t, root) {
		if strings.HasPrefix(rel, brandDir) {
			continue
		}

		text, ok := readText(t, root, rel)
		if !ok || generatedPattern.MatchString(text) {
			continue
		}

		// The module path is exempt everywhere, and the command directory's own name is
		// exempt both inside it and in references to it.
		text = strings.ReplaceAll(text, module, "")
		inCmd := strings.HasPrefix(rel, cmdDir+"/")
		text = strings.ReplaceAll(text, cmdDir, "")

		found = append(found, hits(rel, text, func(line string) bool {
			return strings.Contains(line, brand.Name) || (!inCmd && strings.Contains(line, brand.Slug))
		})...)
	}

	assert.Empty(t, found, "reference brand.Name and brand.Slug instead of writing the product name")
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
