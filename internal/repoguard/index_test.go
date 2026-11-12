package repoguard_test

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

// repoIndex is what the guards read of the repository, gathered once per test binary and
// shared by every test: the file list, each listed file's text, and each Go file's syntax
// and imports, parsed the first time a test asks for them. Tests only read what it hands
// out, its syntax trees included, and never change it.
type repoIndex struct {
	root string

	// files lists the repository's files (tracked, plus untracked files that are not
	// ignored) as slash-separated paths relative to root, and goFiles the Go files among
	// them, in the same order.
	files   []string
	goFiles []string

	// walked is why git could not list the files, when the tree was walked instead.
	walked error

	// listed holds every listed file, and texts the text of each one that is text: a
	// binary file, or one deleted in the working tree but still in the index, has none.
	listed map[string]bool
	texts  map[string]string

	// fset positions every syntax tree syntax parses.
	fset *token.FileSet

	// syntax parses a listed Go file in full, with object resolution on (the logging and
	// request guards tell a package from a local variable that shadows it by it), and
	// imports parses only as far as its imports, whatever its build tags; each runs once.
	syntax  map[string]func() (*ast.File, error)
	imports map[string]func() ([]string, error)
}

// sharedIndex builds the repository's index once per test binary.
var sharedIndex = sync.OnceValues(buildIndex) //nolint:gochecknoglobals // one read-only index per test binary

// repo returns the shared index, failing t when it could not be built.
func repo(t *testing.T) *repoIndex {
	t.Helper()

	idx, err := sharedIndex()
	require.NoError(t, err)

	return idx
}

// buildIndex lists the repository's files, reads them side by side, and sets up each Go
// file's parses.
func buildIndex() (*repoIndex, error) {
	root, err := findModuleRoot()
	if err != nil {
		return nil, err
	}

	idx := &repoIndex{root: root, fset: token.NewFileSet()}

	idx.files, idx.walked = gitFiles(root)
	if idx.walked != nil {
		if idx.files, err = walkFiles(root); err != nil {
			return nil, err
		}
	}

	if err = idx.readTexts(); err != nil {
		return nil, err
	}

	idx.syntax = make(map[string]func() (*ast.File, error), len(idx.goFiles))
	idx.imports = make(map[string]func() ([]string, error), len(idx.goFiles))

	for _, rel := range idx.goFiles {
		idx.syntax[rel] = sync.OnceValues(func() (*ast.File, error) { return idx.parse(rel) })
		idx.imports[rel] = sync.OnceValues(func() ([]string, error) { return idx.parseImports(rel) })
	}

	return idx, nil
}

// errNoModule is what findModuleRoot fails with when no directory above the test's holds
// go.mod.
var errNoModule = errors.New("go.mod not found above the test directory")

// findModuleRoot walks up from the test's directory to the directory holding go.mod.
func findModuleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("find the working directory: %w", err)
	}

	for {
		if _, err = os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errNoModule
		}

		dir = parent
	}
}

// gitFiles lists the repository's files with git, or returns why it could not.
func gitFiles(root string) ([]string, error) {
	out, err := exec.CommandContext(context.Background(), "git", "-C", root, "ls-files", "-z", //nolint:gosec // argv only, no shell
		"--cached", "--others", "--exclude-standard").Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}

	var files []string

	for f := range strings.SplitSeq(string(out), "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}

	return files, nil
}

// walkFiles lists the files under root, skipping build output and dependency trees.
func walkFiles(root string) ([]string, error) {
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
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", root, err)
	}

	return files, nil
}

// readTexts reads every listed file side by side, keeping the text of each that is text,
// and picks out the Go files.
func (idx *repoIndex) readTexts() error {
	texts := make([]string, len(idx.files))
	isText := make([]bool, len(idx.files))

	var g errgroup.Group

	g.SetLimit(runtime.GOMAXPROCS(0))

	for i, rel := range idx.files {
		g.Go(func() error {
			var err error

			texts[i], isText[i], err = readTextFile(idx.root, rel)

			return err
		})
	}

	if err := g.Wait(); err != nil {
		return err
	}

	idx.listed = make(map[string]bool, len(idx.files))
	idx.texts = make(map[string]string, len(idx.files))

	for i, rel := range idx.files {
		idx.listed[rel] = true

		if isText[i] {
			idx.texts[rel] = texts[i]
		}

		if strings.HasSuffix(rel, ".go") {
			idx.goFiles = append(idx.goFiles, rel)
		}
	}

	return nil
}

// parse parses the Go file rel in full.
func (idx *repoIndex) parse(rel string) (*ast.File, error) {
	file, err := parser.ParseFile(idx.fset, rel, idx.texts[rel], 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", rel, err)
	}

	return file, nil
}

// parseImports returns the import paths of the Go file rel, or none for a file that is
// not text.
func (idx *repoIndex) parseImports(rel string) ([]string, error) {
	src, ok := idx.texts[rel]
	if !ok {
		return nil, nil
	}

	file, err := parser.ParseFile(token.NewFileSet(), rel, src, parser.ImportsOnly)
	if err != nil {
		return nil, fmt.Errorf("parse the imports of %s: %w", rel, err)
	}

	paths := make([]string, 0, len(file.Imports))
	for _, imp := range file.Imports {
		paths = append(paths, strings.Trim(imp.Path.Value, `"`))
	}

	return paths, nil
}

// repoFiles lists the repository's files (tracked, plus untracked files that are not
// ignored) as slash-separated paths relative to the module root. Without git it walks the
// tree.
func repoFiles(t *testing.T) []string {
	t.Helper()

	idx := repo(t)
	if idx.walked != nil {
		t.Logf("git ls-files unavailable (%v); walking the tree instead", idx.walked)
	}

	return idx.files
}

// goFiles returns the repository's Go files.
func goFiles(t *testing.T) []string {
	t.Helper()

	files := repo(t).goFiles
	require.NotEmpty(t, files)

	return files
}

// readText returns a repository file's contents, or ok=false for a binary or missing file.
func readText(t *testing.T, rel string) (string, bool) {
	t.Helper()

	idx := repo(t)

	if idx.listed[rel] {
		text, ok := idx.texts[rel]

		return text, ok
	}

	text, ok, err := readTextFile(idx.root, rel)
	require.NoError(t, err)

	return text, ok
}

// goSyntax returns the syntax of the listed Go file rel, positioned in the index's file
// set, or ok=false for a file that is not text.
func goSyntax(t *testing.T, rel string) (*token.FileSet, *ast.File, bool) {
	t.Helper()

	idx := repo(t)

	parse, listed := idx.syntax[rel]
	require.True(t, listed, "%s is not a listed Go file", rel)

	if _, ok := idx.texts[rel]; !ok {
		return nil, nil, false
	}

	file, err := parse()
	require.NoError(t, err)

	return idx.fset, file, true
}

// fileImports returns the import paths of the listed Go file rel, whatever its build tags,
// or none for a file that cannot be read as text.
func fileImports(t *testing.T, rel string) []string {
	t.Helper()

	parse, listed := repo(t).imports[rel]
	require.True(t, listed, "%s is not a listed Go file", rel)

	paths, err := parse()
	require.NoError(t, err, rel)

	return paths
}
