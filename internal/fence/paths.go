package fence

import (
	"cmp"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/sensitive"
)

// DeniedCategories are the sensitive categories whose files no fenced agent edits: what
// agents load or run, what runs on CI with the repository's secrets, and what runs on the
// user's machine when they build or open the project. A run that changed one anyway, in a
// way the fence couldn't see, says so through Changed.
func DeniedCategories() []string {
	return []string{sensitive.CategoryAgent, sensitive.CategoryWorkflows, sensitive.CategoryTooling}
}

// deniedMatcher matches the denied categories' patterns, whatever the user turned off for
// the file check: turning a category off there never loosens a fence.
//
//nolint:gochecknoglobals // a fixed table, compiled once from internal/sensitive's own patterns
var deniedMatcher = sync.OnceValue(func() *sensitive.Matcher {
	cfg := sensitive.DefaultConfig()

	for _, c := range sensitive.Categories() {
		if !c.Locked && !slices.Contains(DeniedCategories(), c.ID) {
			cfg.Off = append(cfg.Off, c.ID)
		}
	}

	return sensitive.NewMatcher(cfg)
})

// DeniedPatterns returns the denied categories' patterns, in internal/sensitive's syntax,
// for an agent's own deny rules.
func DeniedPatterns() []string {
	var out []string

	for _, c := range sensitive.Categories() {
		if slices.Contains(DeniedCategories(), c.ID) {
			out = append(out, c.Patterns...)
		}
	}

	return out
}

// rootDenied returns the denied patterns that name one file or folder without a glob, as
// a path from the repository's root, for the sandbox's own refusals: a name at any depth
// is refused at the root, where the agent and the tools look for it.
func rootDenied() []string {
	var out []string

	for _, p := range DeniedPatterns() {
		if strings.ContainsAny(p, "*?[") {
			continue
		}

		if rel := strings.Trim(p, "/"); rel != "" && !slices.Contains(out, rel) {
			out = append(out, rel)
		}
	}

	return out
}

// deniedFile returns the label of the denied category a repository path is in, if any.
func deniedFile(rel string) (string, bool) {
	c, _, ok := deniedMatcher().Match(filepath.ToSlash(rel))
	if !ok {
		return "", false
	}

	return c.Label, true
}

// FS is what the policy asks of the files on disk: whether a path is there, its links
// resolved, and a small file's head. The hook uses the real files; nil uses none.
type FS interface {
	Exists(path string) bool
	Resolve(path string) string
	ReadHead(path string, limit int64) ([]byte, bool)
}

// OSFS is the real file system.
type OSFS struct{}

// Exists reports a path that is there, a link or not.
func (OSFS) Exists(path string) bool {
	_, err := os.Lstat(path)

	return err == nil
}

// Resolve returns path with its links resolved, as far as it exists.
func (OSFS) Resolve(path string) string {
	return paths.Resolved(path)
}

// ReadHead reads the head of a regular file, never waiting on a pipe.
func (OSFS) ReadHead(path string, limit int64) ([]byte, bool) {
	data, err := paths.ReadHead(path, limit)

	return data, err == nil
}

// noFS is no file system: nothing is there, and every path is as written.
type noFS struct{}

func (noFS) Exists(string) bool                    { return false }
func (noFS) Resolve(path string) string            { return filepath.Clean(path) }
func (noFS) ReadHead(string, int64) ([]byte, bool) { return nil, false }

// absPath turns a path a tool call or a command names into an absolute one: ~ and $HOME
// are home, and a relative path is from dir.
func (p Policy) absPath(path, dir string) string {
	switch {
	case path == "~" || path == "$HOME" || path == "${HOME}":
		return p.Home
	case strings.HasPrefix(path, "~/"):
		path = filepath.Join(p.Home, path[2:])
	case strings.HasPrefix(path, "$HOME/"):
		path = filepath.Join(p.Home, path[len("$HOME/"):])
	case strings.HasPrefix(path, "${HOME}/"):
		path = filepath.Join(p.Home, path[len("${HOME}/"):])
	}

	if !filepath.IsAbs(path) {
		path = filepath.Join(cmp.Or(dir, p.Worktree), path)
	}

	return filepath.Clean(path)
}

// writeDenied says why no write may reach path, the path as written and as its links
// resolve: outside the worktree, inside a git folder, or a denied category's file. "" is
// a path a write may reach.
func (p Policy) writeDenied(path string, fs FS, inWorktreeOnly bool) string {
	resolved := fs.Resolve(path)

	if p.inGitDirs(path, resolved) {
		return reasonGitFolder
	}

	rels := p.worktreeRels(path, resolved)

	for _, rel := range rels {
		if why := repoPathDenied(rel); why != "" {
			return why
		}
	}

	if len(rels) == 0 && inWorktreeOnly {
		return "is outside the worktree, and a fenced agent writes only inside it"
	}

	return ""
}

// reasonGitFolder says why a write inside git's own folder is refused.
const reasonGitFolder = "is inside git's own folder, which an agent never writes"

// inGitDirs reports a path, as written or resolved, inside one of the git folders.
func (p Policy) inGitDirs(path, resolved string) bool {
	return slices.ContainsFunc(p.GitDirs, func(dir string) bool { return within(dir, path) || within(dir, resolved) })
}

// worktreeRels returns the paths, relative to a worktree path, of each of paths inside one.
func (p Policy) worktreeRels(paths ...string) []string {
	var out []string

	for _, wt := range p.Worktrees() {
		for _, path := range paths {
			if rel, ok := relInside(wt, path); ok {
				out = append(out, rel)
			}
		}
	}

	return out
}

// repoPathDenied says why no write may reach a repository path: in a .git, or a denied
// category's file.
func repoPathDenied(rel string) string {
	if gitSegment(rel) {
		return reasonGitFolder
	}

	if label, denied := deniedFile(rel); denied {
		return "is one of the repository's " + strings.ToLower(label) + " files, which a fenced agent never edits"
	}

	return ""
}

// readDeniedPath says why no tool may read path, or is "".
func (p Policy) readDeniedPath(path string, fs FS) string {
	resolved := fs.Resolve(path)

	for _, denied := range p.ReadDeny {
		if within(denied, path) || within(denied, resolved) || within(fs.Resolve(denied), resolved) {
			return "is private to you (credentials, the app's data, or the agent's own settings), which a fenced agent never reads"
		}
	}

	return ""
}

// relInside returns path relative to dir, when path is inside it.
func relInside(dir, path string) (string, bool) {
	rel, err := filepath.Rel(dir, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}

	return rel, true
}

// gitSegment reports a repository path inside a .git folder, ignoring case, as macOS's
// file system does.
func gitSegment(rel string) bool {
	return slices.ContainsFunc(strings.Split(filepath.ToSlash(rel), "/"), func(name string) bool {
		return strings.EqualFold(name, ".git")
	})
}
