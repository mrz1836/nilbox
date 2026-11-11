package gitread

import (
	"context"
	"errors"
	"slices"
	"strings"
)

// ErrCloneConfig is returned, inside a CloneConfigError, for a clone whose own git config
// sets a setting a command that changes the clone isn't run with (cloneSettingAllowed).
var ErrCloneConfig = errors.New("this clone's own git config sets settings a command that changes it isn't run with")

// CloneConfigError is a command that changes a clone refused before anything ran, as the
// clone's own git config sets settings cloneSettingAllowed doesn't allow.
type CloneConfigError struct {
	// Keys are the settings, as git lists them (the section and the name lowercased), each
	// once, in the order git reads them, and Origins where each was first set, as git names
	// it (file:.git/config).
	Keys    []string
	Origins []string
}

// Error returns the refusal's text, with the settings and where each is set.
func (e *CloneConfigError) Error() string {
	named := make([]string, len(e.Keys))
	for i, key := range e.Keys {
		named[i] = key + " (" + e.Origins[i] + ")"
	}

	return ErrCloneConfig.Error() + ": " + strings.Join(named, ", ")
}

// Unwrap returns ErrCloneConfig.
func (e *CloneConfigError) Unwrap() error { return ErrCloneConfig }

// anyPart stands, in a key pattern, for any subsection, which must be there, or for any
// name.
const anyPart = "*"

// The settings a clone's own config may hold fall in three classes, each listed here and
// nowhere else, as patterns of keys as git lists them (section.name, or
// section.subsection.name, anyPart standing for any subsection or name):
//
//   - inertKeys: what git, Git LFS, and the editors and git clients write into a clone for
//     themselves, which none of the commands a Setup is for (a fast-forward pull, a fetch,
//     an ls-remote, `git worktree add`) reads as a program to run, a place to write outside
//     the clone, or a way to reach a host;
//   - neutralizedKeys: settings that could name a program or change how a remote is
//     reached, which every Setup sets again for the command or sets aside for the user's
//     own (remoteGuard), so the clone's own value is never used, or which the remote guard
//     refuses where they would matter (cloneRoutes);
//   - everything else is refused, refusedKeys among them though a pattern above would
//     allow it: a hook (hook.*), a filter but Git LFS's own (lfsSettings), core.worktree,
//     include and includeIf (what they include could change with the folder or the branch,
//     as in a worktree about to be made), core.alternateRefsCommand, Git LFS's transfer
//     agents and URLs, gc's settings but gc.auto (gc.recentObjectsHook runs a program),
//     a merge driver or strategy, and any key a later git adds.
//
// Only the clone's own scopes (local, worktree, and whatever they include) are weighed:
// the user's system and global config and their environment's settings are their own.

// inertKeys are the settings of the first class (see above), each read as data by the
// commands a Setup is for:
//
//   - core: what git init and git clone write, and sparse checkout's switches, which only
//     choose which of the clone's files are written (index.sparse with them);
//   - remote.<name>: what git clone and git remote write (url is weighed by the remote
//     guard for each remote the command reaches, and fetch is never used: every fetch
//     names its refspec with --refmap=); and gh-resolved and gtserviceaccountidentifier,
//     which the GitHub CLI and a desktop git client write for themselves;
//   - branch.<name>: anything but mergeoptions (refusedKeys), which could name a merge
//     strategy, a program; editors keep their notes there, as VS Code's vscode-merge-base;
//   - extensions: the repository's format, partial clone, and worktree config;
//   - user, commit, tag, gpg: who commits, and whether and how a commit or tag is signed
//     (gpg.format, not gpg.program); nothing here commits, tags, or checks a signature;
//   - pull, fetch: the pull's own words beat rebase and ff, and prune stays in the clone;
//   - submodule: a submodule's URL and whether it is active, which no command here fetches
//     or checks out (no submodule recursion);
//   - lfs: the format Git LFS writes, and what it writes of a server by itself: how it
//     authenticates there (access) and whether it checks locks (locksverify);
//   - rerere: whether git records conflict resolutions; a fast-forward has no conflict,
//     and rerere runs no program anyway;
//   - log, color, gui: what git log decorates, how git colors its output, and git gui's
//     and gitk's own settings, which only they read;
//   - mergetool, difftool, merge.tool, diff.tool, and their gui ones: only `git mergetool`
//     and `git difftool` read them, which no command here runs (a merge driver,
//     merge.<driver>.driver, and a diff driver, diff.<driver>.*, are refused).
func inertKeys() []string {
	return []string{
		"core.repositoryformatversion", "core.filemode", "core.bare", "core.logallrefupdates", "core.ignorecase",
		"core.precomposeunicode", "core.symlinks", "core.autocrlf", "core.eol", "core.sparsecheckout",
		"core.sparsecheckoutcone", "index.sparse",
		"remote.*.url", "remote.*.fetch", "remote.*.pushurl", "remote.*.tagopt", "remote.*.prune", "remote.*.promisor",
		"remote.*.partialclonefilter", "remote.*.gh-resolved", "remote.*.gtserviceaccountidentifier",
		"branch.*.*",
		"extensions.partialclone", "extensions.worktreeconfig", "extensions.objectformat", "extensions.refstorage",
		"user.name", "user.email", "user.signingkey", "commit.gpgsign", "tag.gpgsign", "gpg.format",
		"pull.rebase", "pull.ff", "fetch.prune",
		"submodule.*.url", "submodule.*.active", "submodule.active",
		"lfs.repositoryformatversion", "lfs.*.access", "lfs.*.locksverify",
		"rerere.enabled", "rerere.autoupdate",
		"log.excludedecoration", "color.*", "color.*.*", "gui.*", "gui.*.*",
		"mergetool.*", "mergetool.*.*", "difftool.*", "difftool.*.*", "merge.tool", "merge.guitool", "diff.tool",
		"diff.guitool",
	}
}

// neutralizedKeys are the settings of the second class (see above), allowed because the
// Setup never lets the clone's own value reach git:
//
//   - core.sshCommand, core.askPass, credential.* (its helpers), and http's cookie
//     settings: the remote guard sets them aside for the user's own (remoteGuard), for
//     every Setup, so a Git LFS the checkout runs asks for credentials the same way; a
//     command that fails without them says which it went without (SetAside);
//   - url.<base>.insteadOf and pushInsteadOf: the remote guard weighs each remote the
//     command reaches by the URL git will use, insteadOf applied, and refuses a remote
//     helper there;
//   - http.* and remote.<name>.proxy: the ones that change how a host is reached (a proxy,
//     the certificates trusted, the address resolved: cloneRoutes) refuse a command that
//     reaches an http or https remote, or that Git LFS may run in, which reaches its
//     server over https whatever origin's scheme; the rest (a buffer's size, a header for
//     the clone's own remote, the HTTP version) send nothing of the user's elsewhere;
//   - protocol.allow and protocol.<name>.allow, and protocol.version: GIT_ALLOW_PROTOCOL,
//     which every Setup sets, beats them;
//   - core.hooksPath, core.fsmonitor, and submodule.recurse: every Setup sets them again
//     (Hardening), so husky's hooks folder, say, doesn't refuse the command;
//   - maintenance.* and gc.auto: every Setup turns automatic maintenance off
//     (maintenance.auto=false, gc.auto=0), so what git maintenance register and scalar
//     write never starts.
func neutralizedKeys() []string {
	return []string{
		"core.sshcommand", "core.askpass", "credential.*", "credential.*.*",
		"url.*.insteadof", "url.*.pushinsteadof",
		"http.*", "http.*.*", "remote.*.proxy", "remote.*.proxyauthmethod",
		"protocol.allow", "protocol.*.allow", "protocol.version",
		"core.hookspath", "core.fsmonitor", "submodule.recurse",
		"maintenance.*", "maintenance.*.*", "gc.auto",
	}
}

// refusedKeys are settings a pattern of inertKeys or neutralizedKeys would allow that
// refuse the command all the same.
func refusedKeys() []string { return []string{"branch.*.mergeoptions"} }

// lfsSettings are the values Git LFS's filter driver's settings may take in a clone's own
// config, as `git lfs install --local` writes them, with --skip-smudge or without: each
// runs the user's own git-lfs.
func lfsSettings() map[string][]string {
	return map[string][]string{
		"filter.lfs.clean":    {"git-lfs clean -- %f"},
		"filter.lfs.smudge":   {"git-lfs smudge -- %f", "git-lfs smudge --skip -- %f"},
		"filter.lfs.process":  {"git-lfs filter-process", "git-lfs filter-process --skip"},
		"filter.lfs.required": {"true"},
	}
}

// cloneSettingAllowed reports a setting the clone's own config may hold for a command that
// changes the clone to run: one of inertKeys or neutralizedKeys that isn't one of
// refusedKeys, or one of lfsSettings with one of its values.
func cloneSettingAllowed(key, value string) bool {
	if values, ok := lfsSettings()[key]; ok {
		return slices.Contains(values, value)
	}

	matches := func(pattern string) bool { return keyMatches(pattern, key) }

	return (slices.ContainsFunc(inertKeys(), matches) || slices.ContainsFunc(neutralizedKeys(), matches)) &&
		!slices.ContainsFunc(refusedKeys(), matches)
}

// keyMatches reports a key, as git lists it, that pattern names: the same section, a
// subsection when pattern has one (any, for anyPart) and none when it hasn't, and the same
// name (any, for anyPart).
func keyMatches(pattern, key string) bool {
	pSection, pSub, pName := keyParts(pattern)
	section, sub, name := keyParts(key)

	switch {
	case section != pSection || (sub == "") != (pSub == ""):
		return false
	case pSub != anyPart && sub != pSub:
		return false
	default:
		return pName == anyPart || name == pName
	}
}

// keyParts splits a key into its section, its subsection, "" for none, and its name: the
// subsection is everything between the first dot and the last, and may hold dots itself.
func keyParts(key string) (section, sub, name string) {
	section, rest, _ := strings.Cut(key, ".")

	cut := strings.LastIndexByte(rest, '.')
	if cut < 0 {
		return section, "", rest
	}

	return section, rest[:cut], rest[cut+1:]
}

// cloneConfig refuses, with a CloneConfigError, every setting of the clone's own config
// (any scope but trustedScopes, includes followed) that cloneSettingAllowed doesn't allow.
func cloneConfig(settings []scopedSetting) error {
	var refused *CloneConfigError

	for _, s := range settings {
		if s.trusted() || cloneSettingAllowed(s.key, s.value) {
			continue
		}

		if refused == nil {
			refused = &CloneConfigError{}
		}

		if !slices.Contains(refused.Keys, s.key) {
			refused.Keys = append(refused.Keys, s.key)
			refused.Origins = append(refused.Origins, s.origin)
		}
	}

	if refused == nil {
		return nil
	}

	return refused
}

// lfsMayRun reports whether Git LFS's filter is set for the clone, in its own config or
// the user's: a checkout or a pull then runs git-lfs, which reaches its server over https.
func lfsMayRun(settings []scopedSetting) bool {
	return slices.ContainsFunc(settings, func(s scopedSetting) bool {
		return strings.HasPrefix(s.key, "filter.lfs.") && (strings.HasSuffix(s.key, ".process") || strings.HasSuffix(s.key, ".smudge"))
	})
}

// cloneRoutes refuses, with ErrCloneRoutes in a RefusedError naming origin, a clone whose
// own config changes how a host is reached (any of routeKeys, for every URL or one, or a
// remote's proxy) when Git LFS may run (lfsMayRun), whatever the remote's scheme: git-lfs
// reads them for its own https requests, which carry the user's credentials for the LFS
// server. Without Git LFS, the remote guard weighs them for each remote the command
// reaches over http or https (weigh).
func cloneRoutes(settings []scopedSetting) error {
	if !lfsMayRun(settings) {
		return nil
	}

	for _, s := range settings {
		_, sub, name := keyParts(s.key)
		if !s.trusted() && (isRouteKey(s.key, "") || strings.HasPrefix(s.key, "remote.") && sub != "" && name == "proxy") {
			return &RefusedError{Remote: remoteName, Err: routesErr(s.key)}
		}
	}

	return nil
}

// CloneConfig reads the clone at dir's whole config and refuses it as every Setup does
// before anything runs, reading only the clone: a setting of its own outside
// cloneSettingAllowed (a CloneConfigError naming them), and settings that change how a
// host is reached where Git LFS may run (cloneRoutes). Repo scripts' branch status asks it
// before offering a pull that would be refused.
func (g *Git) CloneConfig(ctx context.Context, dir string) error {
	_, err := g.checkedConfig(ctx, dir, environmentOf(g.env()))

	return err
}

// checkedConfig reads the clone at dir's whole config with env and refuses it as
// CloneConfig says, returning the settings when it passes.
func (g *Git) checkedConfig(ctx context.Context, dir string, env []string) ([]scopedSetting, error) {
	if !isDir(dir) {
		return nil, errNotAFolder(dir)
	}

	listed, stderr, err := g.listConfig(ctx, dir, env)
	if err != nil {
		return nil, failure(configListArgs(), stderr, err)
	}

	if err := cloneConfig(listed); err != nil {
		return nil, err
	}

	if err := cloneRoutes(listed); err != nil {
		return nil, err
	}

	return listed, nil
}
