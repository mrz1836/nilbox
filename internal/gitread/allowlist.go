package gitread

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
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

	// headCommit is the commit checked out in a worktree, as rev-parse reads it.
	headCommit = "HEAD" + commitSuffix

	// diffCmd lists the files two commits differ in, or shows a worktree's changes since a
	// commit, always with --no-ext-diff and --no-textconv, so no textconv or external diff
	// the clone names runs; the second reads the worktree's files, so it runs with the
	// clone's filter drivers off, as a status does.
	diffCmd = "diff"

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

	// lsTreeCmd lists a commit's tree, logCmd reads its history, and catFileCmd reads a
	// blob's contents from the object store: the reads of a clone's manifests, which
	// never touch the worktree.
	lsTreeCmd  = "ls-tree"
	logCmd     = "log"
	catFileCmd = "cat-file"

	// lastChangeFormat prints a commit's committer time, in seconds since the epoch, and
	// its subject, split by a byte no time holds.
	lastChangeFormat = "--format=%ct%x01%s"

	// literalMagic starts a pathspec git takes as the path it is, with no wildcard or other
	// magic read in it: a path a repository names is passed to git only so.
	literalMagic = ":(literal)"

	// fetchHeadName is the file every fetch writes, whose modification time is when the
	// clone last fetched.
	fetchHeadName = "FETCH_HEAD"

	// maxPathLen caps a path in a commit's tree gitread passes to git.
	maxPathLen = 1024
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
	slotTrackingCommit
	slotBlobID
	slotLiteralManifest
	slotLiteralLockfile
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
		{
			name:  "the commit a branch on origin names",
			words: append(fixed(revParse, verifyFlag, quietFlag), value(slotTrackingCommit)),
		},
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
		{name: "the checked out commit", words: fixed(revParse, verifyFlag, headCommit)},
		{
			name: "the files the commits between two changed",
			words: append(fixed(diffCmd, "--name-only", "--no-renames", "--no-ext-diff", "--no-textconv", "-z"),
				value(slotCommitRange), word{text: "--"}),
		},
		{
			name: "the changes in the worktree since a commit, committed or not",
			words: append(fixed(diffCmd, "--no-ext-diff", "--no-textconv", "--no-color", "--no-renames",
				ignoreSubmodulesDirty), value(slotCommitID), word{text: "--"}),
		},
		{name: "whether the clone is shallow", words: fixed(revParse, "--is-shallow-repository")},
		{
			name:  "every file in a commit's tree, with its object and size",
			words: append(fixed(lsTreeCmd, "-r", "-z", "-l", "--full-tree"), value(slotCommitID)),
		},
		{
			name:  "the newest commit to change a manifest",
			words: append(fixed(lastChangeWords()...), value(slotCommitID), word{text: "--"}, value(slotLiteralManifest)),
		},
		{
			// The lockfile comes first, so this command without its last word is no other.
			name: "the newest commit to change a manifest or its lockfile",
			words: append(fixed(lastChangeWords()...), value(slotCommitID), word{text: "--"}, value(slotLiteralLockfile),
				value(slotLiteralManifest)),
		},
		{name: "a blob's contents", words: append(fixed(catFileCmd, "blob"), value(slotBlobID))},
		{name: "the local version tags", words: append(fixed("tag", "--list"), value(slotVersionPattern))},
		{name: "origin's branches and tags", words: fixed(lsRemote, uploadPackFlag, "--heads", tagsFlag, remoteName)},
		{
			name:  "one branch on origin",
			words: append(fixed(lsRemote, uploadPackFlag, "--heads", remoteName), value(slotLocalRef)),
		},
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
		slotGitPath:         isGitPathName,
		slotLocalRef:        isLocalRef,
		slotTrackingRef:     isTrackingRef,
		slotCommitPeel:      isCommitPeel,
		slotCommitID:        IsCommitID,
		slotConfigKey:       isConfigKey,
		slotBoolKey:         isBoolKey,
		slotUnpushedRange:   isUnpushedRange,
		slotVersionPattern:  versionGlobPattern.MatchString,
		slotTagRef:          isTagRef,
		slotPeeledTagRef:    isPeeledTagRef,
		slotTagCommit:       isTagCommit,
		slotCommitRange:     isCommitRange,
		slotRemoteName:      ValidRemoteName,
		slotTrackingCommit:  isTrackingCommit,
		slotBlobID:          isBlobID,
		slotLiteralManifest: isLiteralManifest,
		slotLiteralLockfile: isLiteralLockfile,
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
// it is in the middle of something, the hooks folder, the index's lock, and FETCH_HEAD,
// which every fetch writes.
func gitPathNames() []string {
	names := make([]string, 0, len(operationFiles())+3)
	for _, f := range operationFiles() {
		names = append(names, f.name)
	}

	return append(names, hooksName, indexLock, fetchHeadName)
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

// isTrackingCommit reports refs/remotes/origin/<branch>^{commit} with a well-formed branch.
func isTrackingCommit(arg string) bool {
	ref, ok := strings.CutSuffix(arg, commitSuffix)

	return ok && isTrackingRef(ref)
}

// isCommitPeel reports <commit ID>^{commit}.
func isCommitPeel(arg string) bool {
	oid, ok := strings.CutSuffix(arg, commitSuffix)

	return ok && IsCommitID(oid)
}

// IsCommitID reports a full commit ID, SHA-1 or SHA-256, that can name a commit. The null
// ID is refused: given as the old value to `update-ref -d` it turns the comparison off,
// and git reports it as the HEAD of a worktree with no commits. internal/worktrees holds
// its commit IDs to it too.
func IsCommitID(oid string) bool {
	return objectIDPattern.MatchString(oid) && strings.Trim(oid, "0") != ""
}

// isBlobID reports a full object ID that can name a blob, as a tree lists one: a blob's
// ID takes a commit's form, and the null ID names neither.
func isBlobID(oid string) bool {
	return IsCommitID(oid)
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

	return ok && IsCommitID(from) && IsCommitID(to)
}

// remoteNamePattern is what a remote's name must look like to be named in a command or a
// config key: a letter or a digit first, then letters, digits, '.', '_', or '-'.
var remoteNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ValidRemoteName reports a remote's name the app may pass to git, which can't be read as
// an option, a path, or a URL: remoteNamePattern, with no "..", and at most maxNameLen
// bytes. internal/worktrees holds the remotes it fetches from to it too.
func ValidRemoteName(name string) bool {
	return len(name) <= maxNameLen && remoteNamePattern.MatchString(name) && !strings.Contains(name, "..")
}

// manifestNames are the manifests whose history and contents Deps reads: a Go module's
// go.mod, and a JavaScript package's package.json.
func manifestNames() []string {
	return []string{"go.mod", "package.json"}
}

// lockfileNames are the lockfiles whose history Deps reads beside a manifest's: a Go
// module's go.sum, and a JavaScript package's pnpm, npm, Yarn, or Bun lockfile.
func lockfileNames() []string {
	return []string{"go.sum", "pnpm-lock.yaml", "package-lock.json", "yarn.lock", "bun.lock", "bun.lockb"}
}

// ValidManifestPath reports the path, in a commit's tree, of a manifest gitread passes to
// git: a go.mod or a package.json, as treePath has it. Deps holds what it reads to it.
func ValidManifestPath(path string) bool {
	return treePath(path, manifestNames())
}

// ValidLockfilePath reports the path, in a commit's tree, of a lockfile gitread passes to
// git: a go.sum, pnpm-lock.yaml, package-lock.json, yarn.lock, bun.lock, or bun.lockb, as
// treePath has it.
func ValidLockfilePath(path string) bool {
	return treePath(path, lockfileNames())
}

// treePath reports a path, as git names a file in a commit's tree from its top, that
// can't be read as an option, a pattern's magic, or a place outside the tree: relative
// and clean (filepath.IsLocal, and no part empty, "." or ".."), no part starting with "-"
// or ":", valid UTF-8 with no control character or backslash, at most maxPathLen bytes,
// and its file one of names.
func treePath(path string, names []string) bool {
	if path == "" || len(path) > maxPathLen || !utf8.ValidString(path) || !filepath.IsLocal(path) {
		return false
	}

	if strings.ContainsFunc(path, unicode.IsControl) || strings.Contains(path, `\`) {
		return false
	}

	parts := strings.Split(path, "/")

	return !slices.ContainsFunc(parts, badPathPart) && slices.Contains(names, parts[len(parts)-1])
}

// badPathPart reports a part of a path between slashes that treePath refuses.
func badPathPart(part string) bool {
	return part == "" || part == "." || part == ".." || strings.HasPrefix(part, "-") || strings.HasPrefix(part, ":")
}

// isLiteralManifest reports :(literal)<path> with a manifest's path ValidManifestPath takes.
func isLiteralManifest(arg string) bool {
	path, ok := strings.CutPrefix(arg, literalMagic)

	return ok && ValidManifestPath(path)
}

// isLiteralLockfile reports :(literal)<path> with a lockfile's path ValidLockfilePath takes.
func isLiteralLockfile(arg string) bool {
	path, ok := strings.CutPrefix(arg, literalMagic)

	return ok && ValidLockfilePath(path)
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
// ".", and at most maxNameLen bytes. internal/worktrees holds its branch names to it too,
// through ValidBranch.
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

func headArgs() []string { return []string{revParse, verifyFlag, headCommit} }

func changedFilesArgs(from, to string) []string {
	return []string{diffCmd, "--name-only", "--no-renames", "--no-ext-diff", "--no-textconv", "-z", from + ".." + to, "--"}
}

func worktreeDiffArgs(from string) []string {
	return []string{diffCmd, "--no-ext-diff", "--no-textconv", "--no-color", "--no-renames", ignoreSubmodulesDirty, from, "--"}
}

// readsFiles reports a command, allowlisted, that reads the worktree's files, which a
// filter driver the clone names would run on: a status, and a diff against the worktree.
func readsFiles(args []string) bool {
	return args[0] == statusCmd || (args[0] == diffCmd && !slices.Contains(args, "--name-only"))
}

func localTagsArgs() []string { return []string{"tag", "--list", versionTagsPattern} }

func remoteRefsArgs() []string {
	return []string{lsRemote, uploadPackFlag, "--heads", tagsFlag, remoteName}
}

func remoteBranchArgs(branch string) []string {
	return []string{lsRemote, uploadPackFlag, "--heads", remoteName, headsPrefix + branch}
}

func remoteTagsArgs() []string {
	return []string{lsRemote, uploadPackFlag, tagsFlag, "--refs", remoteName, versionTagsPattern}
}

func remoteTagArgs(tag string) []string {
	return []string{lsRemote, uploadPackFlag, remoteName, tagsPrefix + tag, tagsPrefix + tag + peelSuffix}
}

func shallowArgs() []string { return []string{revParse, "--is-shallow-repository"} }

func treeArgs(commit string) []string {
	return []string{lsTreeCmd, "-r", "-z", "-l", "--full-tree", commit}
}

// lastChangeWords are a log's words before the commit it starts from: one commit, merges
// left out, no signature checked (the program that checks one is the clone's to name), no
// mailmap, no external diff or textconv, and the commit's time and subject alone.
func lastChangeWords() []string {
	return []string{
		logCmd, "-1", "--no-merges", "--no-show-signature", "--no-mailmap", "--no-ext-diff", "--no-textconv",
		lastChangeFormat,
	}
}

func lastChangeArgs(from, manifest, lockfile string) []string {
	args := append(lastChangeWords(), from, "--")
	if lockfile != "" {
		args = append(args, literalMagic+lockfile)
	}

	return append(args, literalMagic+manifest)
}

func blobArgs(oid string) []string { return []string{catFileCmd, "blob", oid} }

// The most a commit's tree, a log of the newest commit to change a manifest, and a blob's
// contents may print, in bytes, before git is stopped and the read fails: each is the
// repository's to make as large as it likes.
const (
	// maxTreeListing holds the tree of a large monorepo, hundreds of thousands of files.
	maxTreeListing = 64 << 20

	// maxLastChange holds a commit's time and a long subject.
	maxLastChange = 64 << 10

	// maxBlob holds a manifest of up to a mebibyte, with room to spare.
	maxBlob = 1<<20 + 64<<10
)

// outputLimit returns the most the allowlisted command args may print, in bytes, before
// git is stopped and it fails, or 0 for no limit: a tree's listing, a log, and a blob's
// contents are capped.
func outputLimit(args []string) int {
	if len(args) == 0 {
		return 0
	}

	switch args[0] {
	case lsTreeCmd:
		return maxTreeListing
	case logCmd:
		return maxLastChange
	case catFileCmd:
		return maxBlob
	default:
		return 0
	}
}
