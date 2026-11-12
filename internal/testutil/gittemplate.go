package testutil

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// templateDate is when every commit and tag a template's build makes was authored and
// committed, 2026-01-02 03:04:05 UTC in git's own form, so a template's commit IDs are
// the same in every run.
const templateDate = "1767323045 +0000"

// templateBuildLimit bounds how long a template's build may run.
const templateBuildLimit = 2 * time.Minute

// errTemplateEntry is a template holding something other than folders and regular files.
var errTemplateEntry = errors.New("a git template holds only folders and regular files")

// GitTemplate is a folder of git repositories, such as an origin and its clone, built once
// per test binary, the first time a test asks for a copy, and copied whole into each test
// that does. A suite whose rigs start from the same repositories pays for git's commands
// once, and each test still gets repositories of its own to commit to, push to, or break.
// Its value is what the build returned, such as the commit IDs it made, which every copy
// shares: the copies hold the same commits.
type GitTemplate[V any] struct {
	tree func() (*gitTree[V], error)
}

// NewGitTemplate returns the template build makes. build runs at most once, outside any
// test: a step of it that fails (see GitBuild) fails every test that asks for a copy.
func NewGitTemplate[V any](build func(b *GitBuild) V) *GitTemplate[V] {
	return &GitTemplate[V]{tree: sync.OnceValues(func() (*gitTree[V], error) { return buildTree(build) })}
}

// Copy copies the template into dir, which may already exist but must hold none of the
// template's files, and returns what its build returned. Every file that named the folder
// the template was built in, such as a clone's config naming its origin, names dir in its
// place, and each work tree's index is refreshed, as the copy gave every file new times.
// It skips the test without git.
func (g *GitTemplate[V]) Copy(t testing.TB, dir string) V {
	t.Helper()

	RequireGit(t)

	tree, err := g.tree()
	require.NoError(t, err, "build the git template")
	require.NoError(t, tree.copyTo(dir), "copy the git template")

	for _, work := range tree.workTrees {
		RunGit(t, filepath.Join(dir, work), "update-index", "-q", "--refresh")
	}

	return tree.value
}

// GitTemplates is a GitTemplate for each key a suite asks for, such as each name its
// clones take, each built the first time a test asks for a copy of it.
type GitTemplates[K comparable, V any] struct {
	build func(b *GitBuild, key K) V

	mu    sync.Mutex
	built map[K]*GitTemplate[V]
}

// NewGitTemplates returns the templates build makes, one for each key.
func NewGitTemplates[K comparable, V any](build func(b *GitBuild, key K) V) *GitTemplates[K, V] {
	return &GitTemplates[K, V]{build: build, built: map[K]*GitTemplate[V]{}}
}

// Copy copies the template for key into dir, as GitTemplate.Copy does, and returns what
// its build returned.
func (s *GitTemplates[K, V]) Copy(t testing.TB, key K, dir string) V {
	t.Helper()

	return s.template(key).Copy(t, dir)
}

// template returns the template for key, made on the first ask and built on its first
// copy.
func (s *GitTemplates[K, V]) template(key K) *GitTemplate[V] {
	s.mu.Lock()
	defer s.mu.Unlock()

	tpl, ok := s.built[key]
	if !ok {
		tpl = NewGitTemplate(func(b *GitBuild) V { return s.build(b, key) })
		s.built[key] = tpl
	}

	return tpl
}

// GitBuild is where a template is built: Root, a fresh folder with its symbolic links
// resolved, which each copy's folder takes the place of, and a git isolated as GitEnv
// describes, which dates every commit and tag alike, at 2026-01-02 03:04:05 UTC. A build
// is its steps in order: once one fails, the rest do nothing, and the build fails with
// that first failure.
type GitBuild struct {
	// Root is the template: everything under it is copied.
	Root string

	env []string
	err error
}

// Git runs git with args in dir, relative to Root, and returns what it wrote to standard
// output, with its surrounding space trimmed, as a commit ID is read. After a failed step
// it runs nothing and returns "".
func (b *GitBuild) Git(dir string, args ...string) string {
	if b.err != nil {
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), templateBuildLimit)
	defer cancel()

	var stdout, stderr bytes.Buffer

	cmd := exec.CommandContext(ctx, "git", args...) //nolint:gosec // test fixture setup; argv only, no shell
	cmd.Dir = filepath.Join(b.Root, dir)
	cmd.Env = b.env
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		b.err = fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, stderr.String())

		return ""
	}

	return strings.TrimSpace(stdout.String())
}

// WriteFile writes content to the file at name, relative to Root, with mode 0600, making
// the folders it needs. After a failed step it writes nothing.
func (b *GitBuild) WriteFile(name, content string) {
	if b.err != nil {
		return
	}

	path := filepath.Join(b.Root, name)
	if b.err = os.MkdirAll(filepath.Dir(path), 0o700); b.err == nil {
		b.err = os.WriteFile(path, []byte(content), 0o600)
	}
}

// Commit writes content to the file name in the repository at dir, relative to Root,
// adds it, and commits it with name as the message, as Commit does in a test, and returns
// the new commit, or "" after a failed step.
func (b *GitBuild) Commit(dir, name, content string) string {
	b.WriteFile(filepath.Join(dir, name), content)
	b.Git(dir, "add", name)
	b.Git(dir, "commit", "-q", "-m", name)

	return b.Git(dir, "rev-parse", "HEAD")
}

// gitTree is a built template, held in memory: its folders and files in the order a walk
// meets them, so each folder comes before what it holds, the folder it was built in, the
// work trees whose index a copy refreshes, and what its build returned.
type gitTree[V any] struct {
	entries   []treeEntry
	root      string
	workTrees []string
	value     V
}

// treeEntry is one folder or file of a template, its path relative to the template.
// named marks a file that names the folder the template was built in.
type treeEntry struct {
	rel   string
	mode  fs.FileMode
	data  []byte
	named bool
}

// buildTree builds a template in a fresh temporary folder, beside a home of its own, reads
// it into memory, and removes the folder.
func buildTree[V any](build func(b *GitBuild) V) (tree *gitTree[V], err error) {
	tmp, err := os.MkdirTemp("", "git-template-")
	if err != nil {
		return nil, err
	}

	defer func() { err = errors.Join(err, os.RemoveAll(tmp)) }()

	if tmp, err = filepath.EvalSymlinks(tmp); err != nil {
		return nil, err
	}

	root, home := filepath.Join(tmp, "template"), filepath.Join(tmp, "home")
	for _, dir := range []string{root, home} {
		if err = os.Mkdir(dir, 0o700); err != nil {
			return nil, err
		}
	}

	b := &GitBuild{Root: root, env: append(GitEnv(home), "GIT_AUTHOR_DATE="+templateDate, "GIT_COMMITTER_DATE="+templateDate)}

	value := build(b)
	if b.err != nil {
		return nil, fmt.Errorf("build the git template: %w", b.err)
	}

	tree, err = readTree[V](root)
	if err != nil {
		return nil, err
	}

	tree.value = value

	return tree, nil
}

// readTree reads the template at root into memory.
func readTree[V any](root string) (*gitTree[V], error) {
	tree := &gitTree[V]{root: root}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == root {
			return err
		}

		return tree.read(path, d)
	})

	return tree, err
}

// read adds the folder or file at path, under the template's folder, to the tree.
func (tree *gitTree[V]) read(path string, d fs.DirEntry) error {
	rel, err := filepath.Rel(tree.root, path)
	if err != nil {
		return err
	}

	info, err := d.Info()
	if err != nil {
		return err
	}

	entry := treeEntry{rel: rel, mode: info.Mode()}

	switch {
	case d.IsDir():
	case info.Mode().IsRegular():
		if entry.data, err = os.ReadFile(path); err != nil { //nolint:gosec // the template this binary just built
			return err
		}

		entry.named = bytes.Contains(entry.data, []byte(tree.root))

		if d.Name() == "index" && filepath.Base(filepath.Dir(rel)) == ".git" {
			tree.workTrees = append(tree.workTrees, filepath.Dir(filepath.Dir(rel)))
		}
	default:
		return fmt.Errorf("%w: %s", errTemplateEntry, rel)
	}

	tree.entries = append(tree.entries, entry)

	return nil
}

// copyTo writes the template into dir, each file with its own mode and never over a file
// already there, naming dir wherever a file named the folder the template was built in.
func (tree *gitTree[V]) copyTo(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	for _, e := range tree.entries {
		if err := tree.write(dir, e); err != nil {
			return err
		}
	}

	return nil
}

// write writes the folder or file e under dir.
func (tree *gitTree[V]) write(dir string, e treeEntry) error {
	path := filepath.Join(dir, e.rel)

	if e.mode.IsDir() {
		if err := os.Mkdir(path, e.mode.Perm()); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}

		return nil
	}

	data := e.data
	if e.named {
		data = bytes.ReplaceAll(data, []byte(tree.root), []byte(dir))
	}

	return writeNew(path, data, e.mode.Perm())
}

// writeNew writes data to a new file at path with perm, failing when one is there.
func writeNew(path string, data []byte, perm fs.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm) //nolint:gosec // a test's own folder
	if err != nil {
		return err
	}

	_, err = f.Write(data)

	return errors.Join(err, f.Close())
}
