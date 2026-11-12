package gitread

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/shellenv"
)

const (
	// noLazyFetchVar keeps a partial clone's missing objects from being fetched; a Setup for
	// a partial clone leaves it out, so its checkout can fetch them, under the remote guard.
	noLazyFetchVar = "GIT_NO_LAZY_FETCH"

	// noReplaceVar has git ignore refs/replace: a clone's replace ref for a commit would
	// otherwise have a checkout of that commit write another commit's files, while the
	// commit's ID, which the launch checked, stays the same.
	noReplaceVar = "GIT_NO_REPLACE_OBJECTS"

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

// ErrNoGit is returned for a Setup when git can't be found where every gitread command
// looks for it (GitFinder).
var ErrNoGit = errors.New("git can't be found on the login shell's PATH or in the usual tool folders")

// remoteKeys and filterKeys are remoteKeysPattern and filterKeysPattern, which a Setup
// picks the guards' settings out of the clone's whole config with.
var (
	remoteKeys = regexp.MustCompile(remoteKeysPattern)
	filterKeys = regexp.MustCompile(filterKeysPattern)
)

// Setup is what a git command that changes a clone runs with, Repo scripts' pull and
// worktree creation's fetches, ls-remote, and checkouts: the clone's own config weighed
// setting by setting first (cloneconfig.go), then every guard gitread's own ls-remote runs
// under, and those a status runs under, so a command that changes a clone is held tighter
// than the reads.
type Setup struct {
	// Git is git's absolute path, as GitFinder finds it: the git the command must run.
	Git string

	// Env is the command's whole environment, its settings in it, so its argv holds
	// nothing but git's own words:
	//
	//   - for one that reaches a remote, the ls-remote's (remoteGuard), with
	//     GIT_TERMINAL_PROMPT=0, the transports GIT_ALLOW_PROTOCOL allows, and the ssh
	//     command the user's own config or environment chose, else a batch-mode ssh; for one
	//     that reaches none (Checkout), the same but that it allows no transport.
	//     GIT_NO_LAZY_FETCH=1 is in it, but for a partial clone, whose promisor remotes the
	//     guard has weighed too;
	//   - GIT_DIR and GIT_WORK_TREE, the clone's git folder and the folder itself, which
	//     beat anything its config says;
	//   - Settings, through GIT_CONFIG_COUNT, after any the environment passes already.
	Env []string

	// Settings are the key=value settings Env passes, read only to say what they are:
	// Hardening's; fetchHardening's; the remote guard's, which set aside the clone's own
	// password program and credential helpers for the user's own, and save no cookie; and
	// every filter driver the clone's own config defines but Git LFS's turned off.
	Settings []string

	// SetAside names the clone's own settings set aside, of SetAsideSSHCommand,
	// SetAsideAskPass, and SetAsideHelpers, which may be why a command that reached a
	// remote failed: SetAsideNote says so.
	SetAside []string

	// Partial reports a partial clone, whose checkout may fetch a missing file's contents
	// from its promisor remote.
	Partial bool
}

// Checkout returns s for a command that writes the clone's files and reaches no remote,
// as `git worktree add` does, from a Setup RemoteSetupFor worked out for the same clone: the
// same, but that git may use no transport, unless the clone is partial, whose checkout may
// fetch a missing file's contents from its promisor remotes, which the setup weighed.
func (s Setup) Checkout() Setup {
	if s.Partial {
		return s
	}

	out := s
	out.Env = shellenv.With(s.Env, allowProtocolVar, "")

	return out
}

// GitFinder returns what finds the git every Setup names, as every gitread command finds
// it: on the PATH of the environment Config.Env returns, the login shell's in the app,
// then in the well-known tool folders under its HOME. Core hands it to every other service
// that runs git on a clone, so one clone is never handled by two git builds.
func (g *Git) GitFinder() execpath.Finder {
	return gitFinder{env: g.env}
}

// gitFinder finds a program as gitSearch does, on the environment env returns now.
type gitFinder struct {
	env func() []string
}

// Find returns name's absolute path.
func (f gitFinder) Find(name string) (string, error) {
	return gitSearch(f.env()).Find(name)
}

// RemoteSetup works out what a git command in the clone at dir that reaches origin and
// writes the clone's files runs with (Setup), reading the clone's config, its folders, and
// origin's URL (`ls-remote --get-url`, which asks origin nothing): no command here reaches
// the network. It refuses, before anything runs, a clone whose own config holds a setting
// cloneSettingAllowed doesn't allow (a CloneConfigError naming them), a folder that isn't
// its clone's top folder (ErrWorkTree), what an ls-remote's guard refuses (an origin
// reached through a remote helper, a RefusedError naming it, for a partial clone its
// promisor remotes too), a clone whose own config reroutes a host where it would matter
// (ErrCloneRoutes), and a setting it can't pass safely (ErrGitCommandNotAllowed).
func (g *Git) RemoteSetup(ctx context.Context, dir string) (Setup, error) {
	return g.RemoteSetupFor(ctx, dir, remoteName)
}

// RemoteSetupFor is RemoteSetup for the clone's remote called remote, as worktree creation
// fetches from a fork's upstream: that remote's URL and remote helper are weighed in
// origin's place. A name that isn't one is refused (ErrGitCommandNotAllowed). Its Checkout
// serves the same clone's checkout.
func (g *Git) RemoteSetupFor(ctx context.Context, dir, remote string) (Setup, error) {
	if !ValidRemoteName(remote) {
		return Setup{}, fmt.Errorf("%w: a remote named %q", ErrGitCommandNotAllowed, remote)
	}

	return g.setupFor(ctx, dir, []string{remote})
}

// CheckoutSetup works out what a git command in the clone at dir that writes its files but
// reaches no remote runs with, as `git worktree add` does: RemoteSetup's, but that no
// remote is weighed and git may use no transport (Checkout), unless the clone is partial,
// whose promisor remotes are weighed as RemoteSetup weighs its remote.
func (g *Git) CheckoutSetup(ctx context.Context, dir string) (Setup, error) {
	setup, err := g.setupFor(ctx, dir, nil)
	if err != nil {
		return Setup{}, err
	}

	return setup.Checkout(), nil
}

// setupFor works out the Setup of a command in dir that reaches remotes, none for a
// checkout, and a partial clone's promisor remotes. Its config is read once, whole, and
// checked setting by setting (checkedConfig) before anything else.
func (g *Git) setupFor(ctx context.Context, dir string, remotes []string) (Setup, error) {
	base := g.env()
	env := environmentOf(base)

	listed, err := g.checkedConfig(ctx, dir, env)
	if err != nil {
		return Setup{}, err
	}

	git, err := g.GitFinder().Find(gitBin)
	if err != nil {
		return Setup{}, fmt.Errorf("%w: %w", ErrNoGit, err)
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
		git: git, env: env, userAllow: userAllowOf(base), listed: listed, remotes: remotes,
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

// setupWith works out a Setup from what setupFor read: the remotes weighed; the
// environment and settings an ls-remote runs with, the clone's own ssh command, password
// program, credential helpers, and cookie file set aside whether or not the command
// reaches a remote, as Git LFS may reach one during a checkout; the clone's own filter
// drivers but Git LFS's off; and the settings passed through the environment.
func (g *Git) setupWith(ctx context.Context, dir string, f setupFacts) (Setup, error) {
	guarded, stderr, err := g.guardRemotes(ctx, dir, f.remotes, matching(f.listed, remoteKeys), f.env, f.userAllow)
	if err != nil {
		return Setup{}, failure([]string{lsRemote}, stderr, err)
	}

	off, err := cloneFiltersOff(matching(f.listed, filterKeys), true)
	if err != nil {
		return Setup{}, err
	}

	env := shellenv.With(guarded.env, noReplaceVar, "1")
	if f.partial {
		env = shellenv.Without(env, noLazyFetchVar)
	}

	settings := unflagged(slices.Concat(Hardening(), fetchHardening(), credentialHardening(), guarded.extra, off))

	env, err = withSettings(append(env, f.folders...), settings)
	if err != nil {
		return Setup{}, err
	}

	return Setup{
		Git: f.git, Env: env, Settings: settings, SetAside: append([]string{}, guarded.setAside...), Partial: f.partial,
	}, nil
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
// GIT_WORK_TREE, refusing a dir git doesn't take as the top folder (ErrWorkTree). The two
// are compared as files, not names, as a case-insensitive file system takes a folder by
// any case, and git names it as the disk does.
func (g *Git) folders(ctx context.Context, dir string, env []string) ([]string, error) {
	out, stderr, err := g.exec(ctx, dir, env, nil, gitDirArgs())
	if err != nil {
		return nil, failure(gitDirArgs(), stderr, err)
	}

	gitDir, top, _ := strings.Cut(strings.TrimRight(string(out), "\n"), "\n")

	here, err := os.Stat(dir)
	if err != nil || !isDir(gitDir) || !isDir(top) || !sameFolder(here, top) {
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

// credentialHardening are the credential settings every Setup runs with, which the
// clone's own config can't set (cloneSettingAllowed) and a command-line setting couldn't
// beat for one URL, set again all the same: a credential is only ever handed over for the
// protocol it was stored for (protectProtocol, git's defense against CVE-2024-52006), and a
// prompt's text is cleaned of what could mislead (sanitizePrompt).
func credentialHardening() []string {
	return []string{"-c", "credential.protectProtocol=true", "-c", "credential.sanitizePrompt=true"}
}

// Local is what a git command in a clone, or in one of its worktrees, that reaches no
// remote and writes no file of the clone's runs with (LocalSetup), as the worktrees' own
// status, removal, and branch deletion do: Setup's git and settings, the environment a
// read runs with (no transport, no lazy fetch, no optional lock, no replace ref), and
// every filter driver the clone's own config defines turned off, Git LFS's too, as a read
// does.
type Local struct {
	Setup

	// Refused, when set, is why a command there that could run one of the clone's own
	// programs, as a status's clean filter or a hook, must not run: the clone's own config
	// holds a setting cloneSettingAllowed doesn't allow (a CloneConfigError naming them).
	Refused error
}

// ReadEnv returns the environment a read of a clone runs with, every gitread read's
// (environment) with replace refs ignored too: no transport (an empty GIT_ALLOW_PROTOCOL,
// which every git honors, so a partial clone's missing object is never fetched through a
// URL the clone names), no lazy fetch (GIT_NO_LAZY_FETCH, from git 2.44), no optional
// lock, no prompt, and no replace ref. The worktree service runs its own reads with it
// (git's --no-lazy-fetch, which only git 2.44 and later take, isn't passed: the app runs
// any git the user has).
func (g *Git) ReadEnv() []string {
	return shellenv.With(environmentOf(g.env()), noReplaceVar, "1")
}

// LocalSetup works out what a git command in dir, a clone or one of its worktrees, that
// reaches no remote runs with (Local), reading the config git reads there: no command
// here reaches the network. A config git can't read, a folder that isn't absolute and
// clean, and no git refuse it; a clone whose own config holds a setting a Setup would
// refuse is told by Local.Refused, so a read that runs none of its programs can go on.
func (g *Git) LocalSetup(ctx context.Context, dir string) (Local, error) {
	if !isDir(dir) {
		return Local{}, errNotAFolder(dir)
	}

	env := g.ReadEnv()

	git, err := g.GitFinder().Find(gitBin)
	if err != nil {
		return Local{}, fmt.Errorf("%w: %w", ErrNoGit, err)
	}

	listed, stderr, err := g.listConfig(ctx, dir, env)
	if err != nil {
		return Local{}, failure(configListArgs(), stderr, err)
	}

	off, err := cloneFiltersOff(matching(listed, filterKeys), false)
	if err != nil {
		return Local{}, err
	}

	settings := unflagged(slices.Concat(Hardening(), fetchHardening(), off))

	env, err = withSettings(env, settings)
	if err != nil {
		return Local{}, err
	}

	return Local{
		Setup:   Setup{Git: git, Env: env, Settings: settings, SetAside: []string{}},
		Refused: cloneConfig(listed),
	}, nil
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

	if count, ok := shellenv.LookupOK(env, configCountVar); ok {
		parsed, err := strconv.Atoi(count)
		if err != nil || parsed < 0 {
			return nil, fmt.Errorf("%w: %s=%q", ErrGitCommandNotAllowed, configCountVar, count)
		}

		n = parsed
	}

	out := env

	for i, setting := range settings {
		key, value, _ := strings.Cut(setting, "=")
		out = shellenv.With(out, configKeyVar+strconv.Itoa(n+i), key)
		out = shellenv.With(out, configValueVar+strconv.Itoa(n+i), value)
	}

	return shellenv.With(out, configCountVar, strconv.Itoa(n+len(settings))), nil
}

// SetAsideNote says which of the clone's own settings, of SetAsideSSHCommand,
// SetAsideAskPass, and SetAsideHelpers, a command reaching the clone's remote called
// remote went without, which may be why it failed, and where to set them so that they are
// used: the user's global config, for this folder alone with an includeIf, which gitread
// counts as the user's own. It is "" for none.
func SetAsideNote(remote string, settings []string) string {
	if len(settings) == 0 {
		return ""
	}

	return setAsideNote(remote, settings)
}

// errNotAFolder refuses dir, which isn't absolute and clean.
func errNotAFolder(dir string) error {
	return fmt.Errorf("%w: the folder %q", ErrGitCommandNotAllowed, dir)
}

// routesErr is ErrCloneRoutes for the setting key.
func routesErr(key string) error {
	return fmt.Errorf("%w (%s)", ErrCloneRoutes, key)
}
