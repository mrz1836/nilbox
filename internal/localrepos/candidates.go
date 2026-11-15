package localrepos

import (
	"cmp"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/paths"
)

// candidateEntries is the most entries Candidates reads in all, across every folder it
// looks in, so a home full of large folders can't hold the setup up.
const candidateEntries = 20000

// dataVolume is where macOS mounts the volume home is on. A mount table may name a folder
// in home by its path there, /System/Volumes/Data/Users/<user>/..., for /Users/<user>/...
const dataVolume = "/System/Volumes/Data"

// commonFolders are the names, lower case, that clones are commonly kept under directly
// in home. Candidates looks in these first, so a large folder elsewhere never spends its
// bounds before them.
func commonFolders() []string {
	return []string{"projects", "code", "src", "dev", "developer", "repos", "git", "github", "workspace", "work"}
}

// systemFolders are the folders macOS keeps directly in home, lower case, which Candidates
// never looks in: none holds clones, and macOS asks the user before an app reads Desktop,
// Documents, Downloads, or the Photos library in Pictures.
func systemFolders() []string {
	return []string{
		"applications", "desktop", "documents", "downloads", "library", "movies", "music", "pictures", "public",
	}
}

// layouts are the folders deeper in home where tools keep clones by owner, which
// Candidates looks in after the folders directly in home: GOPATH's
// (~/go/src/github.com/<owner>/<repo>) and ghq's (~/ghq/github.com/<owner>/<repo>).
func layouts() []string {
	return []string{filepath.Join("go", "src", "github.com"), filepath.Join("ghq", "github.com")}
}

// Candidates suggests folders under home that could be the clone root or a repository
// location, whatever they are named. It looks at each folder directly in home: one that
// has clones directly inside it, or, for one with none, each folder one level down inside
// it that has some, as Survey looks (~/src/<owner>/<repo>). It looks the same way in
// GOPATH's and ghq's layouts. It returns the folders with ~ for home, the most clones
// first, each folder once, leaving out the folders roots already holds.
//
// It never looks in a hidden folder, a link directly in home, macOS's own folders
// (Desktop, Documents, Library, ...), or a folder another disk is mounted on, such as a
// share, a cloud drive, or a virtual machine's files. So it never waits on a network or
// makes macOS ask the user. It reads with stat and ReadDir alone, at most
// candidateEntries entries in all, the common names first, and runs no git.
func Candidates(home string, roots Roots) []Suggestion {
	return candidatesIn(home, roots, mountPoints(), candidateEntries)
}

// candidatesIn is Candidates, never looking in a folder one of mounts names, where a disk
// is mounted, and reading at most budget entries.
func candidatesIn(home string, roots Roots, mounts []string, budget int) []Suggestion {
	c := candidates{home: home, roots: roots, mounts: mountSet(mounts), budget: budget, out: []Suggestion{}}
	if home == "" {
		return c.out
	}

	for _, dir := range c.homeFolders() {
		c.look(dir)
	}

	for _, rel := range layouts() {
		c.look(filepath.Join(home, rel))
	}

	slices.SortStableFunc(c.out, func(a, b Suggestion) int {
		return cmp.Or(b.Clones-a.Clones, strings.Compare(a.Path, b.Path))
	})

	return c.out
}

// mountSet is the folders mounts names, where disks are mounted. Each one under macOS's
// data volume is there by the path the user sees too.
func mountSet(mounts []string) map[string]bool {
	set := make(map[string]bool, len(mounts))

	for _, m := range mounts {
		m = filepath.Clean(m)
		set[m] = true

		if rest, ok := strings.CutPrefix(m, dataVolume+"/"); ok {
			set["/"+rest] = true
		}
	}

	return set
}

// candidates is Candidates' list as it is found: what it holds and their folders, the
// folders disks are mounted on, and how many entries it may still read.
type candidates struct {
	home   string
	roots  Roots
	mounts map[string]bool
	budget int
	dirs   []string
	out    []Suggestion
}

// homeFolders lists the folders directly in home that Candidates looks in, the common
// names first, then the rest by name. It skips a hidden folder, a link, one of macOS's
// own folders, and one a disk is mounted on. It tells them apart from home's own
// listing, so it never reads any of them.
func (c *candidates) homeFolders() []string {
	entries, err := os.ReadDir(c.home)
	if err != nil {
		return nil
	}

	system := systemFolders()
	out := make([]string, 0, len(entries))

	for _, entry := range entries {
		name := entry.Name()
		dir := filepath.Join(c.home, name)

		if strings.HasPrefix(name, ".") || !entry.IsDir() || slices.Contains(system, strings.ToLower(name)) ||
			c.mounted(dir) {
			continue
		}

		out = append(out, dir)
	}

	common := commonFolders()
	rank := func(dir string) int {
		if i := slices.Index(common, strings.ToLower(filepath.Base(dir))); i >= 0 {
			return i
		}

		return len(common)
	}

	slices.SortStableFunc(out, func(a, b string) int { return cmp.Compare(rank(a), rank(b)) })

	return out
}

// mounted reports whether a disk is mounted on dir.
func (c *candidates) mounted(dir string) bool {
	return c.mounts[filepath.Clean(dir)]
}

// look lists dir when it has clones directly inside it. Otherwise it lists each folder
// one level down inside it that has some, as Survey looks, never reading one a disk is
// mounted on. It reads only what is left of the budget. A folder that isn't there, or
// can't be read, holds none.
func (c *candidates) look(dir string) {
	if c.budget <= 0 || c.mounted(dir) || !isFolder(dir) {
		return
	}

	count, read, _ := countClones(dir, min(c.budget, insideEntries))
	c.budget -= max(read, 1)

	if count > 0 {
		c.add(dir, count)

		return
	}

	if c.budget <= 0 {
		return
	}

	look := insideLook{roots: c.roots, home: c.home, skip: c.mounted, budget: min(c.budget, insideEntries)}
	start := look.budget
	look.in(dir)
	c.budget -= start - look.budget

	found, _, _ := look.kept()
	for _, s := range found {
		if sub, err := paths.ExpandHome(s.Path, c.home); err == nil {
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
