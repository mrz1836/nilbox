package localrepos

import (
	"cmp"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/paths"
)

// candidateFolders are the folders under home where clones commonly live, which Candidates
// looks in: ~/projects, the clone root's default, first.
func candidateFolders() []string {
	return []string{
		"projects", "Projects", "code", "src", "dev", "Developer", "repos", "git", "GitHub", "workspace",
		filepath.Join("go", "src", "github.com"),
	}
}

// Candidates suggests folders under home that could be the clone root or a repository
// location, for a user whose clone root is missing or holds no clones: each folder where
// clones commonly live (~/projects, ~/code, ~/src, ~/Developer, ~/go/src/github.com, and a
// few more) that has clones directly inside it, or, for one with none, each folder one
// level down inside it that has some, as Survey looks (~/src/<owner>/<repo>). It returns
// them with ~ for home, the most clones first, each folder once however many of its names
// lead to it, leaving out the folders roots already holds. It reads with stat and ReadDir
// alone, within Survey's bounds, and runs no git.
func Candidates(home string, roots Roots) []Suggestion {
	out := []Suggestion{}
	if home == "" {
		return out
	}

	c := candidates{home: home, roots: roots, out: out}

	for _, rel := range candidateFolders() {
		c.look(filepath.Join(home, rel))
	}

	slices.SortStableFunc(c.out, func(a, b Suggestion) int {
		return cmp.Or(b.Clones-a.Clones, strings.Compare(a.Path, b.Path))
	})

	return c.out
}

// candidates is Candidates' list as it is found: what it holds, and their folders.
type candidates struct {
	home  string
	roots Roots
	dirs  []string
	out   []Suggestion
}

// look lists dir when it has clones directly inside it, or else each folder one level down
// inside it that has some, as Survey looks; a folder that isn't there, or can't be read,
// holds none.
func (c *candidates) look(dir string) {
	if !isFolder(dir) {
		return
	}

	clones, err := folderClones(dir)
	if err != nil {
		return
	}

	if len(clones) > 0 {
		c.add(dir, len(clones))

		return
	}

	found, _, _ := c.roots.inside(dir, c.home)
	for _, s := range found {
		if sub, expandErr := paths.ExpandHome(s.Path, c.home); expandErr == nil {
			c.add(sub, s.Clones)
		}
	}
}

// add lists dir, with clones clones directly inside it, unless it can't be a location,
// the locations hold it already, or it is listed already, under any name. The other
// locations are compared by their paths resolved once, never read again here, since one
// may be on a share that hangs; the clone root, which is always read directly, is
// compared as a file too.
func (c *candidates) add(dir string, clones int) {
	if CheckAddable(dir, c.home) != nil {
		return
	}

	resolved := paths.Resolved(dir)

	for _, p := range c.roots.places {
		if p.resolved == resolved || (p.Primary && sameFolder(p.Path, dir)) {
			return
		}
	}

	for _, listed := range c.dirs {
		if sameFolder(listed, dir) {
			return
		}
	}

	c.dirs = append(c.dirs, dir)
	c.out = append(c.out, Suggestion{Path: paths.Tilde(dir, c.home), Clones: clones})
}

// sameFolder reports whether a and b are one folder: the same path once resolved, or, on a
// disk that ignores case, the same file by any name.
func sameFolder(a, b string) bool {
	if paths.Same(a, b) {
		return true
	}

	ia, errA := os.Stat(a)
	ib, errB := os.Stat(b)

	return errA == nil && errB == nil && os.SameFile(ia, ib)
}
