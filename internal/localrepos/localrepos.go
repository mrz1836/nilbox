// Package localrepos finds the user's local clones of GitHub repositories.
//
// Clones live one level inside the clone root (by default ~/projects), the primary, which
// new clones go in, and in the other repository locations the user added: a folder looked
// into one level deep as the clone root is, or a single clone. Roots holds them, and its
// Clones lists the clones in all of them, reading no git; Scan reads each one's remotes
// with a read-only `git config --get-regexp`; Resolve picks the clone of a given
// repository, preferring one whose origin is that repository; ByFolder finds the clone a
// page's request names by its folder; Survey and Check say what a location is, for the
// settings that add one.
// Git always runs as an argv through the injected execpath.Runner, never a shell.
package localrepos

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/repo"
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
	// ErrCloneNotFound is returned when no clone in the locations has the repository as a
	// remote.
	ErrCloneNotFound = errors.New("no local clone")

	// ErrNotARepo is returned when a directory is not a git working tree.
	ErrNotARepo = errors.New("not a git repository")
)

// githubHosts are the hosts whose remotes count as GitHub repositories.
func githubHosts() []string {
	return []string{"github.com", "www.github.com", "ssh.github.com"}
}

// LocalRepo is a git working tree on disk and its GitHub remotes.
type LocalRepo struct {
	// Path is the working tree's directory.
	Path string

	// Remotes maps each remote's name to the GitHub repository it points at, as its URL
	// writes it. Remotes that are not on GitHub are left out.
	Remotes map[string]repo.Name
}

// RemoteFor returns the name of the remote that points at name, preferring origin and
// otherwise the alphabetically first. The comparison ignores case.
func (r LocalRepo) RemoteFor(name repo.Name) (string, bool) {
	want := name.Key()

	if origin, ok := r.Remotes[originRemote]; ok && origin.Key() == want {
		return originRemote, true
	}

	names := make([]string, 0, len(r.Remotes))
	for remote, target := range r.Remotes {
		if target.Key() == want {
			names = append(names, remote)
		}
	}

	if len(names) == 0 {
		return "", false
	}

	slices.Sort(names)

	return names[0], true
}

// Match is the clone Resolve chose and the remote that matched.
type Match struct {
	Repo          LocalRepo
	MatchedRemote string
}

// Resolver finds the local clone of a repository in the user's repository locations.
type Resolver struct {
	// Roots are the clone root and the other locations.
	Roots Roots

	// Home is the user's home directory, which a miss shows as ~.
	Home string

	// Git runs git.
	Git execpath.Runner
}

// Resolve lists the clones in every location and returns the clone of name. A clone whose
// origin is the repository wins over one that only has it as another remote; ties go to
// the first in Found order, the clone root's first. A miss returns ErrCloneNotFound naming
// the repository, where it looked, and any location it couldn't read.
func (r Resolver) Resolve(ctx context.Context, name repo.Name) (Match, error) {
	found, err := r.Roots.Clones(ctx)
	if err != nil {
		return Match{}, err
	}

	repos, err := Scan(ctx, found.Dirs, r.Git)
	if err != nil {
		return Match{}, err
	}

	if match, ok := pick(repos, name); ok {
		return match, nil
	}

	where := "under " + r.Roots.Where(r.Home)
	if r.Roots.Several() {
		where = "in " + r.Roots.Where(r.Home)
	}

	for _, u := range found.Skipped {
		where += "; " + u.Words(r.Home)
	}

	return Match{}, fmt.Errorf("%w of %s %s", ErrCloneNotFound, name, where)
}

// pick returns the first of repos whose origin is name, else the first with another
// remote that is.
func pick(repos []LocalRepo, name repo.Name) (Match, bool) {
	var fallback *Match

	for _, clone := range repos {
		remote, ok := clone.RemoteFor(name)

		switch {
		case !ok:
			continue
		case remote == originRemote:
			return Match{Repo: clone, MatchedRemote: remote}, true
		case fallback == nil:
			fallback = &Match{Repo: clone, MatchedRemote: remote}
		}
	}

	if fallback == nil {
		return Match{}, false
	}

	return *fallback, true
}

// Scan reads the GitHub remotes of each clone in dirs, as Roots.Clones lists them, eight
// git processes at a time, and returns them in the order of dirs.
func Scan(ctx context.Context, dirs []string, git execpath.Runner) ([]LocalRepo, error) {
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

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return repos, nil
}

// Inspect reads the GitHub remotes of the git working tree at dir. It returns ErrNotARepo
// when dir has no .git entry.
func Inspect(ctx context.Context, dir string, git execpath.Runner) (LocalRepo, error) {
	if !isRepo(dir) {
		return LocalRepo{}, fmt.Errorf("%w: %s", ErrNotARepo, dir)
	}

	remotes, err := readRemotes(ctx, dir, git)
	if err != nil {
		return LocalRepo{}, err
	}

	return LocalRepo{Path: dir, Remotes: remotes}, nil
}

// parseRemoteURL returns the GitHub repository a git remote URL points at, as the URL
// writes it. It accepts https, http, git, and ssh URLs and the scp-like
// [user@]host:owner/name form, with or without a .git suffix or trailing slash.
func parseRemoteURL(raw string) (repo.Name, bool) {
	host, path, ok := splitRemote(strings.TrimSpace(raw))
	if !ok || !isGitHubHost(host) {
		return repo.Name{}, false
	}

	path = strings.Trim(path, "/")
	if len(path) >= len(".git") && strings.EqualFold(path[len(path)-len(".git"):], ".git") {
		path = path[:len(path)-len(".git")]
	}

	name, err := repo.Parse(path)
	if err != nil {
		return repo.Name{}, false
	}

	return name, true
}

// isRepo reports whether dir is a directory (following symlinks) with a .git entry.
func isRepo(dir string) bool {
	info, err := os.Stat(dir)

	return err == nil && info.IsDir() && hasGit(dir)
}

// readRemotes lists dir's remote URLs with git config and keeps the GitHub ones. A git
// config that exits with a status (1 when there are no remotes, or another status for an
// unreadable config) yields no remotes, so one broken clone never hides the rest; failing
// to run git at all is an error.
func readRemotes(ctx context.Context, dir string, git execpath.Runner) (map[string]repo.Name, error) {
	stdout, _, err := git.Run(ctx, "git", "-C", dir, "config", "--get-regexp", remoteURLPattern)
	if err != nil {
		if ctx.Err() == nil && execpath.ExitCode(err) > 0 {
			return map[string]repo.Name{}, nil
		}

		return nil, fmt.Errorf("read remotes of %s: %w", dir, err)
	}

	return parseRemotes(string(stdout)), nil
}

// parseRemotes turns `git config --get-regexp` lines ("remote.<name>.url <url>") into a
// remote-to-repository map. The first parseable URL of a remote wins.
func parseRemotes(out string) map[string]repo.Name {
	remotes := map[string]repo.Name{}

	for line := range strings.SplitSeq(out, "\n") {
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

		if name, ok := parseRemoteURL(value); ok {
			remotes[remote] = name
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
