package gitread

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

const (
	// The git commands gitread runs, and the words they share.
	statusCmd   = "status"
	revParse    = "rev-parse"
	lsRemote    = "ls-remote"
	configCmd   = "config"
	verifyFlag  = "--verify"
	quietFlag   = "--quiet"
	gitPathFlag = "--git-path"
	getFlag     = "--get"
	tagsFlag    = "--tags"

	// remoteName is the only remote gitread asks about: the one magex releases to.
	remoteName = "origin"

	// hooksName is the git path of the hooks folder, which core.hooksPath moves.
	hooksName = "hooks"

	// headsPrefix, trackingPrefix, and tagsPrefix start a local branch, a branch of
	// origin's as the clone last learned it, and a tag.
	headsPrefix    = "refs/heads/"
	trackingPrefix = "refs/remotes/" + remoteName + "/"
	tagsPrefix     = "refs/tags/"

	// originHeadRef is origin's HEAD as the clone last learned it, a clone's own ref that
	// names origin's default branch: git clone and git remote set-head write it.
	originHeadRef = trackingPrefix + "HEAD"

	// peelSuffix names the commit an annotated tag points at, as ls-remote lists it, and
	// commitSuffix makes rev-parse answer only for a commit the clone has.
	peelSuffix   = "^{}"
	commitSuffix = "^{commit}"

	// branchKeyPrefix starts a branch's own config keys, and remoteKeySuffix and
	// upstreamKeySuffix end the two that name its upstream.
	branchKeyPrefix   = "branch."
	remoteKeySuffix   = ".remote"
	upstreamKeySuffix = ".merge"

	// The config keys gitread reads.
	keyTagSign    = "tag.gpgSign"
	keyForceSign  = "tag.forceSignAnnotated"
	keyGPGFormat  = "gpg.format"
	keySigningKey = "user.signingkey"

	// versionTagPrefix starts every version tag, and versionTagsPattern lists them all.
	versionTagPrefix   = "v"
	versionTagsPattern = versionTagPrefix + "*"

	// maxNameLen caps a branch or tag name gitread passes to git.
	maxNameLen = 255

	// ignoreSubmodulesDirty keeps a status from running a status inside each submodule,
	// whose own config and attributes could name a filter driver, which would then run: a
	// submodule moved to another commit still shows, and edits inside one don't.
	ignoreSubmodulesDirty = "--ignore-submodules=dirty"

	// porcelainV2 is the status format that names each changed file's staged object, so a
	// file staged again shows even when its status letters stay the same, and untrackedAll
	// lists every untracked file rather than its folder.
	porcelainV2  = "--porcelain=v2"
	untrackedAll = "--untracked-files=all"

	// filterKeysPattern matches the config keys that make a filter driver run a program
	// when git reads a file or writes one out: its clean and smudge commands and its
	// long-running process.
	filterKeysPattern = `^filter\..*\.(clean|smudge|process)$`

	// remoteKeysPattern matches the config keys an ls-remote's guard weighs: the ssh command,
	// the program that asks for a password, and the credential helpers (the URL-scoped ones
	// too), which run programs; the transports' allowances, for the user's own policy;
	// each remote's remote helper and proxy, of which the guard weighs its remote's; a
	// partial clone's promisor remotes and their upload-pack, which a lazy fetch reaches;
	// the cookie settings, which write a file; and the http settings that change how https
	// reaches a remote (routeKeys), for every URL or one.
	remoteKeysPattern = `^(core\.(sshcommand|askpass)|credential\.(.*\.)?helper|protocol\.(.*\.)?allow|` +
		`remote\..+\.(vcs|proxy|promisor|uploadpack)|extensions\.partialclone|` +
		`http\.(.*\.)?(savecookies|cookiefile|sslverify|sslcainfo|sslcapath|proxy|curloptresolve))$`

	// uploadPackFlag names the upload-pack an ls-remote asks for: git's own, which a
	// remote.origin.uploadpack in the clone's config can't replace, as the flag wins over
	// the config and a -c setting doesn't (git takes the first uploadpack it reads).
	uploadPackFlag = "--upload-pack=git-upload-pack"
)

// objectIDPattern is a full object ID, SHA-1 or SHA-256.
var objectIDPattern = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

// versionGlobPattern is a pattern git may list version tags by: v*, or narrowed to a version's
// start, as in v1.* or v1.4*.
var versionGlobPattern = regexp.MustCompile(`^v(?:[0-9]+\.)*[0-9]*\*$`)

// slot is the kind of value one word of an allowlisted command takes.
type slot int

// The kinds of value a command's word can take. slotFixed is a word that must be exactly
// as written; every other slot is checked by its validator.
const (
	slotFixed slot = iota
	slotGitPath
	slotLocalRef
	slotTrackingRef
	slotCommitPeel
	slotCommitID
	slotConfigKey
	slotBoolKey
	slotUnpushedRange
	slotVersionPattern
	slotTagRef
	slotPeeledTagRef
	slotTagCommit
	slotCommitRange
	slotRemoteName
)

// word is one word of an allowlisted command: text itself, or a value its slot accepts.
type word struct {
	text string
	slot slot
}

// command is one allowlisted git command: the words after `git -C <dir> -c ... -c ...`.
type command struct {
	// name says what the command is for, in messages and tests.
	name string

	words []word

	// together, when set, checks the words as a whole once each passes on its own.
	together func(args []string) bool
}

// matches reports whether args is exactly this command, every value accepted.
func (c command) matches(args []string) bool {
	if len(args) != len(c.words) {
		return false
	}

	checks := validators()

	for i, w := range c.words {
		if w.slot == slotFixed && args[i] != w.text {
			return false
		}

		if w.slot != slotFixed && !checks[w.slot](args[i]) {
			return false
		}
	}

	return c.together == nil || c.together(args)
}

// allowlist returns every command gitread runs. Each reads; none changes a clone's files,
// index, refs, or config, nor runs a hook. The ls-remote ones ask origin and are the only
// ones that reach the network.
func allowlist() []command {
	return []command{
		{name: "a state file's path", words: append(fixed(revParse, gitPathFlag), value(slotGitPath))},
		{name: "the top folder and the hooks folder", words: fixed(revParse, "--show-toplevel", gitPathFlag, hooksName)},
		{name: "the common git folder", words: fixed(revParse, "--git-common-dir")},
		{name: "a local branch", words: append(fixed(revParse, verifyFlag, quietFlag), value(slotLocalRef))},
		{name: "a branch on origin", words: append(fixed(revParse, verifyFlag, quietFlag), value(slotTrackingRef))},
		{name: "a commit in the clone", words: append(fixed(revParse, verifyFlag, quietFlag), value(slotCommitPeel))},
		{name: "a version tag's commit", words: append(fixed(revParse, verifyFlag, quietFlag), value(slotTagCommit))},
		{name: "uncommitted changes", words: fixed(statusCmd, "--porcelain", "-z", ignoreSubmodulesDirty)},
		{
			name:  "every changed and untracked file, with its staged object",
			words: fixed(statusCmd, porcelainV2, "-z", untrackedAll, ignoreSubmodulesDirty),
		},
		{name: "the filter drivers", words: fixed(configCmd, "--show-scope", "-z", "--get-regexp", filterKeysPattern)},
		{name: "every setting, with where it is set", words: fixed(configCmd, "--show-scope", "--show-origin", "-z", "--list")},
		{name: "the git folder and the top folder", words: fixed(revParse, "--absolute-git-dir", "--show-toplevel")},
		{name: "what an ls-remote's guard weighs", words: fixed(configCmd, "--show-scope", "-z", "--get-regexp", remoteKeysPattern)},
		{name: "a remote's URL, asking nothing of it", words: append(fixed(lsRemote, "--get-url"), value(slotRemoteName))},
		{name: "the current branch", words: fixed("symbolic-ref", quietFlag, "--short", "HEAD")},
		{
			name:  "origin's default branch, as the clone last learned it",
			words: fixed("symbolic-ref", quietFlag, "--short", originHeadRef),
		},
		{name: "the worktrees", words: fixed("worktree", "list", "--porcelain", "-z")},
		{name: "a setting", words: append(fixed(configCmd, getFlag), value(slotConfigKey))},
		{name: "a switch", words: append(fixed(configCmd, "--type=bool", getFlag), value(slotBoolKey))},
		{name: "unpushed commits", words: append(fixed("rev-list", "--count"), value(slotUnpushedRange), word{text: "--"})},
		{name: "the commits between two", words: append(fixed("rev-list", "--count"), value(slotCommitRange), word{text: "--"})},
		{name: "an ancestor", words: append(fixed("merge-base", "--is-ancestor"), value(slotCommitID), value(slotCommitID))},
		{name: "the local version tags", words: append(fixed("tag", "--list"), value(slotVersionPattern))},
		{name: "origin's branches and tags", words: fixed(lsRemote, uploadPackFlag, "--heads", tagsFlag, remoteName)},
		{
			name:  "origin's version tags",
			words: append(fixed(lsRemote, uploadPackFlag, tagsFlag, "--refs", remoteName), value(slotVersionPattern)),
		},
		{
			name:     "a tag on origin and its commit",
			words:    append(fixed(lsRemote, uploadPackFlag, remoteName), value(slotTagRef), value(slotPeeledTagRef)),
			together: samePeeledTag,
		},
	}
}

// fixed returns texts as fixed words.
func fixed(texts ...string) []word {
	words := make([]word, 0, len(texts))
	for _, text := range texts {
		words = append(words, word{text: text})
	}

	return words
}

// value returns a word that takes a value of kind s.
func value(s slot) word {
	return word{slot: s}
}

// validators returns what accepts a value of each slot.
func validators() map[slot]func(string) bool {
	return map[slot]func(string) bool{
		slotGitPath:        isGitPathName,
		slotLocalRef:       isLocalRef,
		slotTrackingRef:    isTrackingRef,
		slotCommitPeel:     isCommitPeel,
		slotCommitID:       isCommitID,
		slotConfigKey:      isConfigKey,
		slotBoolKey:        isBoolKey,
		slotUnpushedRange:  isUnpushedRange,
		slotVersionPattern: versionGlobPattern.MatchString,
		slotTagRef:         isTagRef,
		slotPeeledTagRef:   isPeeledTagRef,
		slotTagCommit:      isTagCommit,
		slotCommitRange:    isCommitRange,
		slotRemoteName:     isRemoteName,
	}
}

// allowed reports whether args is one of the allowlisted commands.
func allowed(args []string) bool {
	return slices.ContainsFunc(allowlist(), func(c command) bool { return c.matches(args) })
}

// isDir reports a folder git may be pointed at with -C: absolute and clean, so it holds
// no "..", and free of control characters.
func isDir(dir string) bool {
	return filepath.IsAbs(dir) && filepath.Clean(dir) == dir && !strings.ContainsFunc(dir, unicode.IsControl)
}

// gitPathNames are the git paths gitread resolves: the files and folders git keeps while
// it is in the middle of something, and the hooks folder.
func gitPathNames() []string {
	names := make([]string, 0, len(operationFiles())+1)
	for _, f := range operationFiles() {
		names = append(names, f.name)
	}

	return append(names, hooksName)
}

// isGitPathName reports one of gitPathNames.
func isGitPathName(name string) bool {
	return slices.Contains(gitPathNames(), name)
}

// isLocalRef reports refs/heads/<branch> with a well-formed branch.
func isLocalRef(ref string) bool {
	name, ok := strings.CutPrefix(ref, headsPrefix)

	return ok && validBranch(name)
}

// isTrackingRef reports refs/remotes/origin/<branch> with a well-formed branch.
func isTrackingRef(ref string) bool {
	name, ok := strings.CutPrefix(ref, trackingPrefix)

	return ok && validBranch(name)
}

// isCommitPeel reports <commit ID>^{commit}.
func isCommitPeel(arg string) bool {
	oid, ok := strings.CutSuffix(arg, commitSuffix)

	return ok && isCommitID(oid)
}

// isCommitID reports a full commit ID that can name a commit: not the null ID.
func isCommitID(oid string) bool {
	return objectIDPattern.MatchString(oid) && strings.Trim(oid, "0") != ""
}

// plainKeys are the config keys gitread reads as text.
func plainKeys() []string {
	return []string{keyGPGFormat, keySigningKey}
}

// boolKeys are the config keys gitread reads as git booleans.
func boolKeys() []string {
	return []string{keyTagSign, keyForceSign}
}

// isConfigKey reports one of plainKeys, or branch.<branch>.remote or .merge with a
// well-formed branch.
func isConfigKey(key string) bool {
	if slices.Contains(plainKeys(), key) {
		return true
	}

	rest, ok := strings.CutPrefix(key, branchKeyPrefix)
	if !ok {
		return false
	}

	for _, suffix := range []string{remoteKeySuffix, upstreamKeySuffix} {
		if name, found := strings.CutSuffix(rest, suffix); found && validBranch(name) {
			return true
		}
	}

	return false
}

// isBoolKey reports one of boolKeys.
func isBoolKey(key string) bool {
	return slices.Contains(boolKeys(), key)
}

// isUnpushedRange reports refs/remotes/origin/<branch>..refs/heads/<branch>, one
// well-formed branch on both sides. A branch never holds "..", so the first one splits it.
func isUnpushedRange(arg string) bool {
	from, to, ok := strings.Cut(arg, "..")
	if !ok || !isTrackingRef(from) || !isLocalRef(to) {
		return false
	}

	return strings.TrimPrefix(from, trackingPrefix) == strings.TrimPrefix(to, headsPrefix)
}

// isCommitRange reports <commit ID>..<commit ID>, two full commit IDs.
func isCommitRange(arg string) bool {
	from, to, ok := strings.Cut(arg, "..")

	return ok && isCommitID(from) && isCommitID(to)
}

// remoteNamePattern is what a remote's name must look like to be named in a command or a
// config key: a letter or a digit first, then letters, digits, '.', '_', or '-'.
var remoteNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// isRemoteName reports a remote's name gitread may pass to git, which can't be read as an
// option, a path, or a URL: remoteNamePattern, with no "..", and at most maxNameLen bytes.
func isRemoteName(name string) bool {
	return len(name) <= maxNameLen && remoteNamePattern.MatchString(name) && !strings.Contains(name, "..")
}

// isVersionTag reports a tag name that starts with v and is otherwise a well-formed ref
// name, as validBranch has it.
func isVersionTag(name string) bool {
	return len(name) > len(versionTagPrefix) && strings.HasPrefix(name, versionTagPrefix) && validBranch(name)
}

// isTagRef reports refs/tags/<version tag>.
func isTagRef(ref string) bool {
	name, ok := strings.CutPrefix(ref, tagsPrefix)

	return ok && isVersionTag(name)
}

// isPeeledTagRef reports refs/tags/<version tag>^{}.
func isPeeledTagRef(ref string) bool {
	tag, ok := strings.CutSuffix(ref, peelSuffix)

	return ok && isTagRef(tag)
}

// isTagCommit reports refs/tags/<version tag>^{commit}.
func isTagCommit(arg string) bool {
	ref, ok := strings.CutSuffix(arg, commitSuffix)

	return ok && isTagRef(ref)
}

// samePeeledTag reports a tag ref followed by the same tag peeled, as the last two words.
func samePeeledTag(args []string) bool {
	n := len(args)

	return n >= 2 && args[n-1] == args[n-2]+peelSuffix
}

// validBranch reports a branch name git takes that cannot be read as an option or reach
// outside its ref folder: no leading "-", no "..", "@{", whitespace, control characters,
// or any of ~^:?*[\, no component that is empty, starts with ".", or ends in ".lock" or
// ".", and at most maxNameLen bytes. It is internal/worktrees' rule.
func validBranch(name string) bool {
	if name == "" || name == "@" || len(name) > maxNameLen || strings.HasPrefix(name, "-") {
		return false
	}

	if strings.Contains(name, "..") || strings.Contains(name, "@{") {
		return false
	}

	if strings.ContainsAny(name, "~^:?*[\\") || strings.ContainsFunc(name, unsafeRune) {
		return false
	}

	return !slices.ContainsFunc(strings.Split(name, "/"), badComponent)
}

// unsafeRune reports a rune a branch name may not hold: whitespace or a control character.
func unsafeRune(r rune) bool {
	return unicode.IsSpace(r) || unicode.IsControl(r)
}

// badComponent reports a part of a branch name between slashes that git refuses.
func badComponent(part string) bool {
	return part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") || strings.HasSuffix(part, ".")
}

// The argument lists gitread's helpers run. Each is on the allowlist.

func gitPathArgs(name string) []string { return []string{revParse, gitPathFlag, name} }

func hookPathsArgs() []string { return []string{revParse, "--show-toplevel", gitPathFlag, hooksName} }

func commonDirArgs() []string { return []string{revParse, "--git-common-dir"} }

func configListArgs() []string {
	return []string{configCmd, "--show-scope", "--show-origin", "-z", "--list"}
}

func gitDirArgs() []string { return []string{revParse, "--absolute-git-dir", "--show-toplevel"} }

func verifyArgs(ref string) []string { return []string{revParse, verifyFlag, quietFlag, ref} }

func statusArgs() []string { return []string{statusCmd, "--porcelain", "-z", ignoreSubmodulesDirty} }

func entriesArgs() []string {
	return []string{statusCmd, porcelainV2, "-z", untrackedAll, ignoreSubmodulesDirty}
}

func currentBranchArgs() []string { return []string{"symbolic-ref", quietFlag, "--short", "HEAD"} }

func originHeadArgs() []string { return []string{"symbolic-ref", quietFlag, "--short", originHeadRef} }

func worktreesArgs() []string { return []string{"worktree", "list", "--porcelain", "-z"} }

func configArgs(key string) []string { return []string{configCmd, getFlag, key} }

func boolConfigArgs(key string) []string { return []string{configCmd, "--type=bool", getFlag, key} }

func unpushedArgs(branch string) []string {
	return []string{"rev-list", "--count", trackingPrefix + branch + ".." + headsPrefix + branch, "--"}
}

func commitsBetweenArgs(from, to string) []string {
	return []string{"rev-list", "--count", from + ".." + to, "--"}
}

func ancestorArgs(oid, of string) []string { return []string{"merge-base", "--is-ancestor", oid, of} }

func localTagsArgs() []string { return []string{"tag", "--list", versionTagsPattern} }

func remoteRefsArgs() []string {
	return []string{lsRemote, uploadPackFlag, "--heads", tagsFlag, remoteName}
}

func remoteTagsArgs() []string {
	return []string{lsRemote, uploadPackFlag, tagsFlag, "--refs", remoteName, versionTagsPattern}
}

func remoteTagArgs(tag string) []string {
	return []string{lsRemote, uploadPackFlag, remoteName, tagsPrefix + tag, tagsPrefix + tag + peelSuffix}
}
