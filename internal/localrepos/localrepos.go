// Package localrepos finds the user's local clones of GitHub repositories.
//
// Clones live one level under a clone root (by default ~/projects). Scan lists every git
// repository there and reads its remotes with a read-only `git config --get-regexp`;
// Resolve picks the clone of a given owner/name, preferring one whose origin is that
// repository. Git always runs as an argv through the injected Runner, never a shell.
package localrepos

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/sync/errgroup"
)

const (
	// scanConcurrency bounds how many git processes Scan runs at once.
	scanConcurrency = 8

	// originRemote is git's conventional name for the remote a clone came from.
	originRemote = "origin"

	// remoteURLPattern selects every remote's fetch URL (not pushurl) from git config.
	remoteURLPattern = `^remote\..*\.url$`
)

var (
	// ErrCloneNotFound is returned when no clone under the root has the repository as a remote.
	ErrCloneNotFound = errors.New("no local clone")

	// ErrNotARepo is returned when a directory is not a git working tree.
	ErrNotARepo = errors.New("not a git repository")

	// ErrEmptyRoot is returned when no clone root is configured.
	ErrEmptyRoot = errors.New("clone root is empty")

	// ErrNoHome is returned when a root starts with ~ but the home directory is unknown.
	ErrNoHome = errors.New("home directory is unknown")

	// ErrRelativeRoot is returned when a clone root is neither absolute nor ~-relative.
	ErrRelativeRoot = errors.New("clone root must be an absolute path or start with ~/")
)

// githubHosts are the hosts whose remotes count as GitHub repositories.
func githubHosts() []string {
	return []string{"github.com", "www.github.com", "ssh.github.com"}
}

// Runner runs a program with arguments and returns what it wrote. Implementations run the
// argv directly, never through a shell.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error)
}

// LocalRepo is a git working tree on disk and its GitHub remotes.
type LocalRepo struct {
	// Path is the working tree's directory.
	Path string

	// Remotes maps each remote's name to the lower-case "owner/name" of the GitHub
	// repository it points at. Remotes that are not on GitHub are left out.
	Remotes map[string]string
}

// RemoteFor returns the name of the remote that points at owner/name, preferring origin
// and otherwise the alphabetically first. The comparison ignores case.
func (r LocalRepo) RemoteFor(owner, name string) (string, bool) {
	want := slug(owner, name)

	if r.Remotes[originRemote] == want {
		return originRemote, true
	}

	names := make([]string, 0, len(r.Remotes))
	for remote, repo := range r.Remotes {
		if repo == want {
			names = append(names, remote)
		}
	}

	if len(names) == 0 {
		return "", false
	}

	sort.Strings(names)

	return names[0], true
}

// Match is the clone Resolve chose and the remote that matched.
type Match struct {
	Repo          LocalRepo
	MatchedRemote string
}

// OriginMatched reports whether the matching remote is origin.
func (m Match) OriginMatched() bool {
	return m.MatchedRemote == originRemote
}

// Resolver finds the local clone of a repository under a clone root.
type Resolver struct {
	// Root is the clone root as configured, e.g. "~/projects".
	Root string

	// Home is the user's home directory, used to expand and display a ~ root.
	Home string

	// Git runs git.
	Git Runner
}

// Resolve scans the clone root and returns the clone of owner/name. A clone whose origin
// is the repository wins over one that only has it as another remote; ties go to the
// first path in sorted order. A miss returns ErrCloneNotFound naming the repository and
// the root.
func (r Resolver) Resolve(ctx context.Context, owner, name string) (Match, error) {
	root, err := ExpandRoot(r.Root, r.Home)
	if err != nil {
		return Match{}, err
	}

	repos, err := Scan(ctx, root, r.Git)
	if err != nil {
		return Match{}, err
	}

	var fallback *Match

	for _, repo := range repos {
		remote, ok := repo.RemoteFor(owner, name)
		if !ok {
			continue
		}

		if remote == originRemote {
			return Match{Repo: repo, MatchedRemote: remote}, nil
		}

		if fallback == nil {
			fallback = &Match{Repo: repo, MatchedRemote: remote}
		}
	}

	if fallback != nil {
		return *fallback, nil
	}

	return Match{}, fmt.Errorf("%w of %s/%s under %s", ErrCloneNotFound, owner, name, TildePath(root, r.Home))
}

// Scan lists the git working trees directly under root (a directory, or a symlink to one,
// holding a .git directory or file) with their GitHub remotes, sorted by path. Entries
// whose names start with a dot are skipped, and nested repositories are not searched.
func Scan(ctx context.Context, root string, git Runner) ([]LocalRepo, error) {
	dirs, err := candidates(root)
	if err != nil {
		return nil, err
	}

	repos := make([]LocalRepo, len(dirs))

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(scanConcurrency)

	for i, dir := range dirs {
		g.Go(func() error {
			remotes, readErr := readRemotes(gctx, dir, git)
			if readErr != nil {
				return readErr
			}

			repos[i] = LocalRepo{Path: dir, Remotes: remotes}

			return nil
		})
	}

	if err = g.Wait(); err != nil {
		return nil, err
	}

	return repos, nil
}

// Inspect reads the GitHub remotes of the git working tree at dir. It returns ErrNotARepo
// when dir has no .git entry.
func Inspect(ctx context.Context, dir string, git Runner) (LocalRepo, error) {
	if !isRepo(dir) {
		return LocalRepo{}, fmt.Errorf("%w: %s", ErrNotARepo, dir)
	}

	remotes, err := readRemotes(ctx, dir, git)
	if err != nil {
		return LocalRepo{}, err
	}

	return LocalRepo{Path: dir, Remotes: remotes}, nil
}

// ParseRemoteURL returns the lower-case owner and name of the GitHub repository a git
// remote URL points at. It accepts https, http, git, and ssh URLs and the scp-like
// [user@]host:owner/name form, with or without a .git suffix or trailing slash.
func ParseRemoteURL(raw string) (owner, name string, ok bool) {
	host, path, ok := splitRemote(strings.TrimSpace(raw))
	if !ok || !isGitHubHost(host) {
		return "", "", false
	}

	path = strings.Trim(path, "/")
	if len(path) >= len(".git") && strings.EqualFold(path[len(path)-len(".git"):], ".git") {
		path = path[:len(path)-len(".git")]
	}

	owner, name, found := strings.Cut(path, "/")
	if !found || !validSegment(owner) || !validSegment(name) {
		return "", "", false
	}

	return strings.ToLower(owner), strings.ToLower(name), true
}

// ExpandRoot turns a configured clone root into an absolute, cleaned path, expanding a
// leading ~ to home.
func ExpandRoot(root, home string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", ErrEmptyRoot
	}

	if root == "~" || strings.HasPrefix(root, "~/") {
		if home == "" {
			return "", ErrNoHome
		}

		root = filepath.Join(home, root[1:])
	}

	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("%w: %s", ErrRelativeRoot, root)
	}

	return filepath.Clean(root), nil
}

// TildePath shows path relative to home as ~/..., or unchanged when it is outside home.
func TildePath(path, home string) string {
	if home == "" {
		return path
	}

	rel, err := filepath.Rel(filepath.Clean(home), filepath.Clean(path))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}

	if rel == "." {
		return "~"
	}

	return "~/" + filepath.ToSlash(rel)
}

// candidates returns the directories directly under root that are git working trees.
func candidates(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read clone root: %w", err)
	}

	var dirs []string

	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		dir := filepath.Join(root, entry.Name())
		if isRepo(dir) {
			dirs = append(dirs, dir)
		}
	}

	return dirs, nil
}

// isRepo reports whether dir is a directory (following symlinks) with a .git entry.
func isRepo(dir string) bool {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}

	_, err = os.Stat(filepath.Join(dir, ".git"))

	return err == nil
}

// readRemotes lists dir's remote URLs with git config and keeps the GitHub ones. A git
// config that exits with a status (1 when there are no remotes, or another status for an
// unreadable config) yields no remotes, so one broken clone never hides the rest; failing
// to run git at all is an error.
func readRemotes(ctx context.Context, dir string, git Runner) (map[string]string, error) {
	stdout, _, err := git.Run(ctx, "git", "-C", dir, "config", "--get-regexp", remoteURLPattern)
	if err != nil {
		var exit interface{ ExitCode() int }
		if ctx.Err() == nil && errors.As(err, &exit) && exit.ExitCode() > 0 {
			return map[string]string{}, nil
		}

		return nil, fmt.Errorf("read remotes of %s: %w", dir, err)
	}

	return parseRemotes(string(stdout)), nil
}

// parseRemotes turns `git config --get-regexp` lines ("remote.<name>.url <url>") into a
// remote-to-repository map. The first parseable URL of a remote wins.
func parseRemotes(out string) map[string]string {
	remotes := map[string]string{}

	for _, line := range strings.Split(out, "\n") {
		key, value, found := strings.Cut(strings.TrimRight(line, "\r"), " ")
		if !found {
			continue
		}

		remote, ok := remoteName(key)
		if !ok {
			continue
		}

		if _, seen := remotes[remote]; seen {
			continue
		}

		if owner, name, ok := ParseRemoteURL(value); ok {
			remotes[remote] = slug(owner, name)
		}
	}

	return remotes
}

// remoteName extracts <name> from a "remote.<name>.url" config key. Git lower-cases the
// section and variable but keeps the remote name's case.
func remoteName(key string) (string, bool) {
	const prefix, suffix = "remote.", ".url"

	if len(key) <= len(prefix)+len(suffix) ||
		!strings.EqualFold(key[:len(prefix)], prefix) ||
		!strings.EqualFold(key[len(key)-len(suffix):], suffix) {
		return "", false
	}

	return key[len(prefix) : len(key)-len(suffix)], true
}

// splitRemote returns the host and path of a URL-style or scp-like remote.
func splitRemote(raw string) (host, path string, ok bool) {
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return "", "", false
		}

		switch strings.ToLower(u.Scheme) {
		case "https", "http", "ssh", "git", "git+ssh", "ssh+git":
			return u.Hostname(), u.Path, true
		default:
			return "", "", false
		}
	}

	// scp-like: [user@]host:path, where the colon comes before any slash.
	colon := strings.Index(raw, ":")
	if colon <= 0 || strings.Contains(raw[:colon], "/") {
		return "", "", false
	}

	host = raw[:colon]
	if at := strings.LastIndex(host, "@"); at >= 0 {
		host = host[at+1:]
	}

	return host, raw[colon+1:], true
}

// isGitHubHost reports whether host is one of GitHub's hosts.
func isGitHubHost(host string) bool {
	for _, h := range githubHosts() {
		if strings.EqualFold(host, h) {
			return true
		}
	}

	return false
}

// ValidName reports whether s can be a GitHub owner or repository name: letters, digits,
// '-', '_', and '.', but never "." or "..", so it is always a single path element.
func ValidName(s string) bool {
	return validSegment(s)
}

// validSegment reports whether s can be a GitHub owner or repository name.
func validSegment(s string) bool {
	if s == "" || s == "." || s == ".." {
		return false
	}

	for _, r := range s {
		if !isNameRune(r) {
			return false
		}
	}

	return true
}

// isNameRune reports whether r may appear in a GitHub owner or repository name.
func isNameRune(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.'
}

// slug is the lower-case "owner/name" key used to compare repositories.
func slug(owner, name string) string {
	return strings.ToLower(owner) + "/" + strings.ToLower(name)
}
