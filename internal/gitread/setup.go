package gitread

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	// noLazyFetchVar keeps a partial clone's missing objects from being fetched; a Setup for
	// a partial clone leaves it out, so its checkout can fetch them, under the remote guard.
	noLazyFetchVar = "GIT_NO_LAZY_FETCH"

	// gitDirVar and workTreeVar name the repository and the work tree a command runs on,
	// over anything its config says, core.worktree included.
	gitDirVar   = "GIT_DIR"
	workTreeVar = "GIT_WORK_TREE"

	// configCountVar, configKeyVar, and configValueVar pass settings to git through the
	// environment, as -c does on the command line: git reads them after every config file,
	// as the command line's.
	configCountVar = "GIT_CONFIG_COUNT"
	configKeyVar   = "GIT_CONFIG_KEY_"
	configValueVar = "GIT_CONFIG_VALUE_"
)

// ErrWorkTree is returned for a folder git doesn't take as its clone's top folder, as a
// subfolder of one, so no command that changes it runs there.
var ErrWorkTree = errors.New("git doesn't take this folder as its clone's top folder")

// remoteKeys and filterKeys are remoteKeysPattern and filterKeysPattern, which a Setup
// picks the guards' settings out of the clone's whole config with.
var (
	remoteKeys = regexp.MustCompile(remoteKeysPattern)
	filterKeys = regexp.MustCompile(filterKeysPattern)
)

// Setup is what a git command that changes a clone runs with, Repo scripts' pull and
// worktree creation's fetches, ls-remote, and checkouts: the clone's own config allowed
// setting by setting first, then every guard gitread's own ls-remote runs under, and those
// a status runs under, so a command that changes a clone is held tighter than the reads.
type Setup struct {
	// Git is git's absolute path: the git every gitread command runs.
	Git string

	// Env is the command's whole environment, its settings in it, so its argv holds
	// nothing but git's own words:
	//
	//   - for one that reaches a remote, the ls-remote's (remoteGuard), with
	//     GIT_TERMINAL_PROMPT=0, the transports GIT_ALLOW_PROTOCOL allows, and the ssh
	//     command the user's own config or environment chose, else a batch-mode ssh; for one
	//     that reaches none, a read's, which allows no transport. GIT_NO_LAZY_FETCH=1 is in
	//     it, but for a partial clone, whose promisor remotes the guard has weighed too;
	//   - GIT_DIR and GIT_WORK_TREE, the clone's git folder and the folder itself, which
	//     beat anything its config says;
	//   - Settings, through GIT_CONFIG_COUNT, after any the environment passes already.
	Env []string

	// Settings are the key=value settings Env passes, read only to say what they are:
	// Hardening's; fetchHardening's; the ls-remote's, which keep the user's own credential
	// helpers and save no cookie; and every filter driver the clone's own config defines
	// but Git LFS's turned off, as a status turns them off.
	Settings []string

	// Partial reports a partial clone, whose checkout may fetch a missing file's contents
	// from its promisor remote.
	Partial bool
}

// RemoteSetup works out what a git command in the clone at dir that reaches origin and
// writes the clone's files runs with (Setup), reading the clone's config, its folders, and
// origin's URL (`ls-remote --get-url`, which asks origin nothing): no command here reaches
// the network. It refuses, before anything runs, a clone whose own config sets a setting
// cloneSettingAllowed doesn't allow (a CloneConfigError naming them), a folder that isn't
// its clone's top folder (ErrWorkTree), what an ls-remote's guard refuses (an origin
// reached through a remote helper, a RefusedError naming it, for a partial clone its
// promisor remotes too), and a setting it can't pass safely (ErrGitCommandNotAllowed).
func (g *Git) RemoteSetup(ctx context.Context, dir string) (Setup, error) {
	return g.RemoteSetupFor(ctx, dir, remoteName)
}

// RemoteSetupFor is RemoteSetup for the clone's remote called remote, as worktree creation
// fetches from a fork's upstream: that remote's URL and remote helper are weighed in
// origin's place. A name that isn't one is refused (ErrGitCommandNotAllowed).
func (g *Git) RemoteSetupFor(ctx context.Context, dir, remote string) (Setup, error) {
	if !isRemoteName(remote) {
		return Setup{}, fmt.Errorf("%w: a remote named %q", ErrGitCommandNotAllowed, remote)
	}

	return g.setupFor(ctx, dir, []string{remote})
}

// CheckoutSetup works out what a git command in the clone at dir that writes its files but
// reaches no remote runs with, as `git worktree add` does: gitread's own git, the
// environment every gitread read runs with (no transport allowed, no lazy fetch, no
// prompt), and RemoteSetup's checks and settings otherwise. A partial clone's checkout
// fetches a missing file's contents from its promisor remotes: for one, those are weighed
// and refused as RemoteSetup weighs its remote, and the environment is a fetch's.
func (g *Git) CheckoutSetup(ctx context.Context, dir string) (Setup, error) {
	return g.setupFor(ctx, dir, nil)
}

// setupFor works out the Setup of a command in dir that reaches remotes, none for a
// checkout, and a partial clone's promisor remotes. Its config is read once, whole, and
// checked setting by setting (cloneConfig) before anything else.
func (g *Git) setupFor(ctx context.Context, dir string, remotes []string) (Setup, error) {
	if !isDir(dir) {
		return Setup{}, fmt.Errorf("%w: the folder %q", ErrGitCommandNotAllowed, dir)
	}

	base := g.env()
	env := environmentOf(base)

	git, err := gitSearch(env).Find(gitBin)
	if err != nil {
		return Setup{}, fmt.Errorf("find git: %w", err)
	}

	listed, stderr, err := g.listConfig(ctx, dir, env)
	if err != nil {
		return Setup{}, failure(configListArgs(), stderr, err)
	}

	if err = cloneConfig(listed); err != nil {
		return Setup{}, err
	}

	folders, err := g.folders(ctx, dir, env)
	if err != nil {
		return Setup{}, err
	}

	lazies, err := promisors(listed)
	if err != nil {
		return Setup{}, err
	}

	for _, p := range lazies {
		if !slices.Contains(remotes, p) {
			remotes = append(slices.Clone(remotes), p)
		}
	}

	return g.setupWith(ctx, dir, setupFacts{
		git: git, env: env, userAllow: lookupVar(base, allowProtocolVar), listed: listed, remotes: remotes,
		folders: folders, partial: len(lazies) > 0,
	})
}

// setupFacts are what setupFor read before working out a Setup: git, the environment, the
// user's own GIT_ALLOW_PROTOCOL, the clone's whole config, the remotes the command reaches,
// the clone's folders as GIT_DIR and GIT_WORK_TREE give them, and whether it is partial.
type setupFacts struct {
	git       string
	env       []string
	userAllow *string
	listed    []scopedSetting
	remotes   []string
	folders   []string
	partial   bool
}

// setupWith works out a Setup from what setupFor read: the remotes weighed, the
// environment and settings an ls-remote runs with when the command reaches one, the
// clone's own filter drivers but Git LFS's off, and the settings passed through the
// environment.
func (g *Git) setupWith(ctx context.Context, dir string, f setupFacts) (Setup, error) {
	env, extra := f.env, []string(nil)

	if len(f.remotes) > 0 {
		guarded, stderr, err := g.guardRemotes(ctx, dir, f.remotes, matching(f.listed, remoteKeys), f.env, f.userAllow)
		if err != nil {
			return Setup{}, failure([]string{lsRemote}, stderr, err)
		}

		env, extra = guarded.env, guarded.extra
	}

	off, err := cloneFiltersOff(matching(f.listed, filterKeys))
	if err != nil {
		return Setup{}, err
	}

	if f.partial {
		env = slices.DeleteFunc(slices.Clone(env), func(kv string) bool { return strings.HasPrefix(kv, noLazyFetchVar+"=") })
	}

	settings := unflagged(slices.Concat(Hardening(), fetchHardening(), extra, off))

	env, err = withSettings(append(slices.Clone(env), f.folders...), settings)
	if err != nil {
		return Setup{}, err
	}

	return Setup{Git: f.git, Env: env, Settings: settings, Partial: f.partial}, nil
}

// listConfig reads every setting git reads in dir, includes followed, with its scope and
// where it is set, and returns what git wrote to standard error when it fails.
func (g *Git) listConfig(ctx context.Context, dir string, env []string) ([]scopedSetting, []byte, error) {
	out, stderr, err := g.exec(ctx, dir, env, nil, configListArgs())
	if err != nil {
		return nil, stderr, err
	}

	settings, err := parseSettings(out, true)

	return settings, nil, err
}

// folders reads the clone's git folder and top folder, and returns them as GIT_DIR and
// GIT_WORK_TREE, refusing a dir git doesn't take as the top folder (ErrWorkTree).
func (g *Git) folders(ctx context.Context, dir string, env []string) ([]string, error) {
	out, stderr, err := g.exec(ctx, dir, env, nil, gitDirArgs())
	if err != nil {
		return nil, failure(gitDirArgs(), stderr, err)
	}

	gitDir, top, _ := strings.Cut(strings.TrimRight(string(out), "\n"), "\n")

	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", dir, err)
	}

	if !filepath.IsAbs(gitDir) || top != resolved {
		return nil, fmt.Errorf("%w: %s is in %q", ErrWorkTree, dir, top)
	}

	return []string{gitDirVar + "=" + gitDir, workTreeVar + "=" + top}, nil
}

// fetchHardening are the settings a command that changes a clone runs with beside
// Hardening, which the clone's own config can't set (cloneSettingAllowed), set again all
// the same: no alternate refs command, which a fetch from a clone with alternates would
// run to learn the objects it already has (true tells it of none; an empty one would have
// git try to run a program named "", and say so, on every fetch); and no automatic
// maintenance, which a fetch or a merge would start afterwards, whose gc runs what its
// settings name.
func fetchHardening() []string {
	return []string{
		"-c", "core.alternateRefsCommand=true", "-c", "core.alternateRefsPrefixes=",
		"-c", "maintenance.auto=false", "-c", "gc.auto=0",
	}
}

// matching returns the settings whose key pattern matches, in order.
func matching(settings []scopedSetting, pattern *regexp.Regexp) []scopedSetting {
	return slices.DeleteFunc(slices.Clone(settings), func(s scopedSetting) bool { return !pattern.MatchString(s.key) })
}

// unflagged returns -c settings, "-c" and "key=value" by turns, as their key=value words.
func unflagged(settings []string) []string {
	out := make([]string, 0, len(settings)/2)

	for i := 1; i < len(settings); i += 2 {
		out = append(out, settings[i])
	}

	return out
}

// withSettings returns env passing settings, key=value each, to git through
// GIT_CONFIG_COUNT, after any settings env passes already, so they come last of those and
// win: git reads them after every config file, as the command line's, though a setting in
// the user's own GIT_CONFIG_PARAMETERS, which git reads after them, still beats them. A
// count that isn't one is refused (ErrGitCommandNotAllowed).
func withSettings(env, settings []string) ([]string, error) {
	n := 0

	if count := lookupVar(env, configCountVar); count != nil {
		parsed, err := strconv.Atoi(*count)
		if err != nil || parsed < 0 {
			return nil, fmt.Errorf("%w: %s=%q", ErrGitCommandNotAllowed, configCountVar, *count)
		}

		n = parsed
	}

	out := slices.Clone(env)

	for i, setting := range settings {
		key, value, _ := strings.Cut(setting, "=")
		out = withVar(out, configKeyVar+strconv.Itoa(n+i), key)
		out = withVar(out, configValueVar+strconv.Itoa(n+i), value)
	}

	return withVar(out, configCountVar, strconv.Itoa(n+len(settings))), nil
}
