package gitread

import (
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

// anyPart stands, in one of cloneKeys, for any subsection, which must be there, or for
// any name.
const anyPart = "*"

// cloneKeys are the settings a clone's own config may hold for a command that changes the
// clone to run (Setup), each as git lists it (section.name, or section.subsection.name,
// anyPart standing for any subsection or name): what git init, git clone, git remote, git
// branch, git submodule, and Git LFS write there, and the notes editors and git clients
// keep there. git, or a tool the command runs, reads each as data: none names a program, a
// file outside the clone, a host to reach, or a way to reach one, apart from a remote's
// URL, which the remote guard weighs for each remote the command reaches. Every other
// setting refuses the command (refusedKeys too): a hook, a filter, an ssh command, a
// password program, a credential helper, a proxy or another of http's settings,
// core.worktree, an include or includeIf, the maintenance and gc settings, and Git LFS's
// transfer agents and URLs among them.
//
//   - core: what git init and git clone write; and hooksPath and fsmonitor, which every
//     such command sets again (Hardening), so the clone's own is never read (husky writes
//     core.hooksPath);
//   - remote.<name>: what git clone and git remote write; and gh-resolved and
//     gtserviceaccountidentifier, which the GitHub CLI and a desktop git client write for
//     themselves, and git never reads;
//   - branch.<name>: anything but mergeoptions (refusedKeys), which could name a merge
//     strategy, a program; editors keep their notes there, as VS Code's vscode-merge-base;
//   - extensions: the repository's format, partial clone, and worktree config;
//   - user, commit, tag, gpg: who commits, and whether and how a commit or tag is signed
//     (gpg.format, not gpg.program); nothing here commits or tags;
//   - pull, fetch: the pull's own words beat rebase and ff, and prune stays in the clone;
//   - submodule: a submodule's URL and whether it is active, which the commands never
//     fetch or check out, active as git clone --recurse-submodules writes it, and recurse,
//     which every such command sets again (Hardening);
//   - lfs: the format Git LFS writes, and how it authenticates to a URL (lfs.<url>.access);
//   - log: excludeDecoration, which only git log reads.
//
// Git LFS's filter driver, filter.lfs, is allowed only with its own commands (lfsSettings).
func cloneKeys() []string {
	return []string{
		"core.repositoryformatversion", "core.filemode", "core.bare", "core.logallrefupdates", "core.ignorecase",
		"core.precomposeunicode", "core.symlinks", "core.autocrlf", "core.eol", "core.hookspath", "core.fsmonitor",
		"remote.*.url", "remote.*.fetch", "remote.*.pushurl", "remote.*.tagopt", "remote.*.prune", "remote.*.promisor",
		"remote.*.partialclonefilter", "remote.*.gh-resolved", "remote.*.gtserviceaccountidentifier",
		"branch.*.*",
		"extensions.partialclone", "extensions.worktreeconfig", "extensions.objectformat", "extensions.refstorage",
		"user.name", "user.email", "user.signingkey", "commit.gpgsign", "tag.gpgsign", "gpg.format",
		"pull.rebase", "pull.ff", "fetch.prune",
		"submodule.*.url", "submodule.*.active", "submodule.active", "submodule.recurse",
		"lfs.repositoryformatversion", "lfs.*.access",
		"log.excludedecoration",
	}
}

// refusedKeys are settings cloneKeys would allow that refuse the command all the same.
func refusedKeys() []string { return []string{"branch.*.mergeoptions"} }

// lfsSettings are Git LFS's filter driver's settings, as `git lfs install --local` writes
// them: a clone's own config may hold them with exactly these values, which run the
// user's own git-lfs.
func lfsSettings() map[string]string {
	return map[string]string{
		"filter.lfs.clean":    "git-lfs clean -- %f",
		"filter.lfs.smudge":   "git-lfs smudge -- %f",
		"filter.lfs.process":  "git-lfs filter-process",
		"filter.lfs.required": "true",
	}
}

// cloneSettingAllowed reports a setting the clone's own config may hold for a command that
// changes the clone to run: one of cloneKeys that isn't one of refusedKeys, or one of
// lfsSettings with its value.
func cloneSettingAllowed(key, value string) bool {
	if want, ok := lfsSettings()[key]; ok {
		return value == want
	}

	matches := func(pattern string) bool { return keyMatches(pattern, key) }

	return slices.ContainsFunc(cloneKeys(), matches) && !slices.ContainsFunc(refusedKeys(), matches)
}

// keyMatches reports a key, as git lists it, that pattern, one of cloneKeys or
// refusedKeys, names: the same section, a subsection when pattern has one (any, for
// anyPart) and none when it hasn't, and the same name (any, for anyPart).
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
// An include or includeIf is one: what it includes could change with the folder or the
// branch, as in a worktree the command is about to make.
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
