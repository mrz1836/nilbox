package gitread

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/execpath"
)

// errOutput is returned when git answers in a shape gitread doesn't read.
var errOutput = errors.New("unexpected git output")

// Operation is what git is in the middle of in a worktree, if anything.
type Operation string

// The operations InProgress reports.
const (
	OperationNone          Operation = ""
	OperationRebasing      Operation = "rebasing"
	OperationMerging       Operation = "merging"
	OperationCherryPicking Operation = "cherry-picking"
	OperationReverting     Operation = "reverting"
)

// operationFile is a file or folder git keeps in a worktree's git folder while it is in
// the middle of an operation.
type operationFile struct {
	name string
	op   Operation
}

// operationFiles are the files InProgress looks for, in order: a rebase first, since one
// can stop on a commit it was picking. rebase-apply is also git am's.
func operationFiles() []operationFile {
	return []operationFile{
		{name: "rebase-merge", op: OperationRebasing},
		{name: "rebase-apply", op: OperationRebasing},
		{name: "MERGE_HEAD", op: OperationMerging},
		{name: "CHERRY_PICK_HEAD", op: OperationCherryPicking},
		{name: "REVERT_HEAD", op: OperationReverting},
	}
}

// Change is one changed or untracked file in a worktree.
type Change struct {
	// Code is git's two-letter status, as in " M", "A ", "R ", or "??".
	Code string

	// Path is the file, relative to the worktree's top folder; for a rename or a copy,
	// its new name. An untracked folder ends in "/".
	Path string
}

// Entry is one changed, staged, unmerged, or untracked file in a worktree, as `git status
// --porcelain=v2` lists it.
type Entry struct {
	// Code is git's two-letter status, "." standing for no change on that side, as in
	// ".M" (changed, not staged), "M." (staged), or "A."; "??" is an untracked file.
	Code string

	// Index names what the index holds for the file: its staged object, or, for an
	// unmerged file, its three stages' objects joined by ":"; "" for an untracked file. A
	// file staged again shows as a new Index even when Code stays the same.
	Index string

	// Path is the file, relative to the worktree's top folder; for a rename or a copy, its
	// new name, and From its old one.
	Path string
	From string
}

// Upstream is the branch a local branch is set to follow, from its config.
type Upstream struct {
	// Remote is branch.<b>.remote, as in "origin", or "" when unset.
	Remote string

	// Ref is branch.<b>.merge, as in "refs/heads/main", or "" when unset.
	Ref string
}

// RemoteTag is one of origin's tags.
type RemoteTag struct {
	// Object is what the tag's ref points at: an annotated tag's own object, or a
	// lightweight tag's commit.
	Object string

	// Commit is the commit the tag names: the peeled one for an annotated tag.
	Commit string
}

// RemoteRefs is what origin has, as ls-remote lists it.
type RemoteRefs struct {
	// Heads maps each branch, as in "main", to the commit it points at.
	Heads map[string]string

	// Tags maps each tag, as in "v1.4.2", to what it names.
	Tags map[string]RemoteTag
}

// Signing is the clone's tag signing config, as git reads it there.
type Signing struct {
	// SignTags is tag.gpgSign: every annotated tag is signed.
	SignTags bool

	// SignAnnotated is tag.forceSignAnnotated: an annotated tag made without asking for
	// a signature is signed.
	SignAnnotated bool

	// Format is gpg.format, as in "openpgp", "x509", or "ssh", or "" when unset, which
	// git reads as openpgp.
	Format string

	// Key is user.signingkey as written, or "" when unset: a key ID, a key file (which
	// may start with ~), or a key itself.
	Key string
}

// Signed reports whether git signs an annotated tag, such as the one a release makes.
func (s Signing) Signed() bool {
	return s.SignTags || s.SignAnnotated
}

// HookPaths is where a worktree's hooks live, as git resolves them. A relative answer from
// git is made absolute against the folder asked about; an absolute one is kept as git
// gives it.
type HookPaths struct {
	// Toplevel is the worktree's top folder.
	Toplevel string

	// Hooks is the folder git runs hooks from: core.hooksPath when set, else the hooks
	// folder in the common git folder.
	Hooks string

	// CommonDir is the git folder the clone's worktrees share, as in <clone>/.git.
	CommonDir string
}

// CurrentBranch returns the branch checked out in the worktree at dir, as in "main", or
// "" when HEAD is detached.
func (g *Git) CurrentBranch(ctx context.Context, dir string) (string, error) {
	out, yes, err := g.ask(ctx, dir, currentBranchArgs())
	if err != nil || !yes {
		return "", err
	}

	return firstLine(out), nil
}

// OriginHead returns origin's default branch as the clone at dir last learned it, as in
// "main": the branch its refs/remotes/origin/HEAD points at, which git clone and git remote
// set-head write. It reads the clone's own refs and never asks origin. It is "" when the
// clone has no such ref, as one made with git init and git remote add, or when it doesn't
// name a branch of origin's.
func (g *Git) OriginHead(ctx context.Context, dir string) (string, error) {
	out, yes, err := g.ask(ctx, dir, originHeadArgs())
	if err != nil || !yes {
		return "", err
	}

	branch, ok := strings.CutPrefix(firstLine(out), remoteName+"/")
	if !ok || !isLocalRef(headsPrefix+branch) {
		return "", nil
	}

	return branch, nil
}

// InProgress returns what git is in the middle of in the worktree at dir: a rebase (or git
// am), a merge, a cherry-pick, or a revert, or OperationNone. It asks git where each one's
// file would be, which differs in a linked worktree, and looks for it.
func (g *Git) InProgress(ctx context.Context, dir string) (Operation, error) {
	for _, f := range operationFiles() {
		path, err := g.gitPath(ctx, dir, f.name)
		if err != nil {
			return OperationNone, err
		}

		_, err = os.Lstat(path)

		switch {
		case err == nil:
			return f.op, nil
		case !errors.Is(err, fs.ErrNotExist):
			return OperationNone, fmt.Errorf("look for %s: %w", f.name, err)
		}
	}

	return OperationNone, nil
}

// indexLock is the file git holds while it writes the index, left behind by a git cut off
// partway, which every later command that writes the index then refuses to run past.
const indexLock = "index.lock"

// IndexLocked reports whether the clone at dir's index is locked: whether git's
// index.lock, as a git that was writing the index, as a checkout does, leaves when it is
// cut off, is there. It asks git where the file would be, which differs in a linked
// worktree, and looks for it.
func (g *Git) IndexLocked(ctx context.Context, dir string) (bool, error) {
	path, err := g.gitPath(ctx, dir, indexLock)
	if err != nil {
		return false, err
	}

	switch _, err = os.Lstat(path); {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	default:
		return false, fmt.Errorf("look for %s: %w", indexLock, err)
	}
}

// Changes returns the worktree at dir's changed and untracked files, by `git status
// --porcelain`, which is the rule a release applies: any line means uncommitted changes.
func (g *Git) Changes(ctx context.Context, dir string) ([]Change, error) {
	out, err := g.read(ctx, dir, statusArgs())
	if err != nil {
		return nil, err
	}

	return parseStatus(out), nil
}

// Entries returns every changed, staged, unmerged, and untracked file in the worktree at
// dir, each untracked file listed on its own, by `git status --porcelain=v2
// --untracked-files=all`, which names what the index holds for each. Taken before and
// after a tool runs, they tell which files it changed or staged.
func (g *Git) Entries(ctx context.Context, dir string) ([]Entry, error) {
	out, err := g.read(ctx, dir, entriesArgs())
	if err != nil {
		return nil, err
	}

	return parseEntries(out), nil
}

// CheckedOutElsewhere returns the folders of the clone's other worktrees, every one but
// the one at dir, that have branch checked out. dir must be a worktree's top folder.
func (g *Git) CheckedOutElsewhere(ctx context.Context, dir, branch string) ([]string, error) {
	out, err := g.read(ctx, dir, worktreesArgs())
	if err != nil {
		return nil, err
	}

	here, _ := os.Stat(dir)
	others := make([]string, 0)

	for _, wt := range parseWorktrees(out) {
		if wt.branch == branch && !sameFolder(here, wt.path) {
			others = append(others, wt.path)
		}
	}

	return others, nil
}

// LocalBranch returns the commit the clone's branch points at, or "" when it has no such
// branch.
func (g *Git) LocalBranch(ctx context.Context, dir, branch string) (string, error) {
	return g.resolve(ctx, dir, headsPrefix+branch)
}

// TrackingBranch returns the commit origin's branch pointed at when the clone last
// learned it (refs/remotes/origin/<branch>), or "" when the clone has no such ref.
func (g *Git) TrackingBranch(ctx context.Context, dir, branch string) (string, error) {
	return g.resolve(ctx, dir, trackingPrefix+branch)
}

// LocalTagCommit returns the commit the clone's version tag points at, peeled through an
// annotated tag, or "" when the clone has no such tag.
func (g *Git) LocalTagCommit(ctx context.Context, dir, tag string) (string, error) {
	return g.resolve(ctx, dir, tagsPrefix+tag+commitSuffix)
}

// HasCommit reports whether the clone has the commit oid.
func (g *Git) HasCommit(ctx context.Context, dir, oid string) (bool, error) {
	got, err := g.resolve(ctx, dir, oid+commitSuffix)

	return got != "", err
}

// Upstream returns the upstream config of the clone's branch, which a release's
// `pull --rebase origin` needs. A key that isn't set is "".
func (g *Git) Upstream(ctx context.Context, dir, branch string) (Upstream, error) {
	remote, err := g.config(ctx, dir, branchKeyPrefix+branch+remoteKeySuffix)
	if err != nil {
		return Upstream{}, err
	}

	ref, err := g.config(ctx, dir, branchKeyPrefix+branch+upstreamKeySuffix)
	if err != nil {
		return Upstream{}, err
	}

	return Upstream{Remote: remote, Ref: ref}, nil
}

// Unpushed returns how many commits the clone's branch has that origin's branch, as the
// clone last learned it, doesn't. It fails when either ref is missing (see LocalBranch and
// TrackingBranch).
func (g *Git) Unpushed(ctx context.Context, dir, branch string) (int, error) {
	return g.count(ctx, dir, unpushedArgs(branch))
}

// CommitsBetween returns how many commits the commit to has that the commit from doesn't,
// as `rev-list --count from..to` counts them: the commits a fast-forward from one to the
// other brought. It fails when the clone lacks either commit (see HasCommit).
func (g *Git) CommitsBetween(ctx context.Context, dir, from, to string) (int, error) {
	return g.count(ctx, dir, commitsBetweenArgs(from, to))
}

// count runs a rev-list --count in dir and reads its count.
func (g *Git) count(ctx context.Context, dir string, args []string) (int, error) {
	out, err := g.read(ctx, dir, args)
	if err != nil {
		return 0, err
	}

	count, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || count < 0 {
		return 0, fmt.Errorf("%w: a count of %q", errOutput, out)
	}

	return count, nil
}

// IsAncestor reports whether the commit oid is of or one of its ancestors. It fails when
// the clone lacks either commit (see HasCommit).
func (g *Git) IsAncestor(ctx context.Context, dir, oid, of string) (bool, error) {
	_, yes, err := g.ask(ctx, dir, ancestorArgs(oid, of))

	return yes, err
}

// LocalVersionTags returns the clone's version tags (v*), in git's order.
func (g *Git) LocalVersionTags(ctx context.Context, dir string) ([]string, error) {
	out, err := g.read(ctx, dir, localTagsArgs())
	if err != nil {
		return nil, err
	}

	tags := make([]string, 0)

	for line := range strings.Lines(string(out)) {
		if name := strings.TrimSuffix(line, "\n"); strings.HasPrefix(name, versionTagPrefix) {
			tags = append(tags, name)
		}
	}

	return tags, nil
}

// RemoteRefs asks origin for its branches and tags (`ls-remote --heads --tags origin`),
// which also says whether origin can be reached. It stops after RemoteTimeout.
func (g *Git) RemoteRefs(ctx context.Context, dir string) (RemoteRefs, error) {
	out, err := g.read(ctx, dir, remoteRefsArgs())
	if err != nil {
		return RemoteRefs{}, err
	}

	return parseRemoteRefs(out), nil
}

// RemoteBranch asks origin for one branch's commit (`ls-remote --heads origin
// refs/heads/<branch>`), and returns it, or "" when origin has no such branch, under every
// guard an ls-remote runs under: origin answers with that one branch, never every branch
// and tag it has. A branch ValidBranch refuses is refused (ErrGitCommandNotAllowed). It
// stops after RemoteTimeout.
func (g *Git) RemoteBranch(ctx context.Context, dir, branch string) (string, error) {
	out, err := g.read(ctx, dir, remoteBranchArgs(branch))
	if err != nil {
		return "", err
	}

	return parseRemoteRefs(out).Heads[branch], nil
}

// RemoteVersionTags asks origin for its version tags (v*) and returns their names, sorted.
// It stops after RemoteTimeout.
func (g *Git) RemoteVersionTags(ctx context.Context, dir string) ([]string, error) {
	out, err := g.read(ctx, dir, remoteTagsArgs())
	if err != nil {
		return nil, err
	}

	return versionTagNames(parseRemoteRefs(out).Tags), nil
}

// RemoteTagCommit asks origin for the commit its version tag names, peeled, or "" when
// origin has no such tag. It stops after RemoteTimeout.
func (g *Git) RemoteTagCommit(ctx context.Context, dir, tag string) (string, error) {
	out, err := g.read(ctx, dir, remoteTagArgs(tag))
	if err != nil {
		return "", err
	}

	return parseRemoteRefs(out).Tags[tag].Commit, nil
}

// Signing reads the clone's tag signing config.
func (g *Git) Signing(ctx context.Context, dir string) (Signing, error) {
	var (
		s   Signing
		err error
	)

	if s.SignTags, err = g.flag(ctx, dir, keyTagSign); err != nil {
		return Signing{}, err
	}

	if s.SignAnnotated, err = g.flag(ctx, dir, keyForceSign); err != nil {
		return Signing{}, err
	}

	if s.Format, err = g.config(ctx, dir, keyGPGFormat); err != nil {
		return Signing{}, err
	}

	if s.Key, err = g.config(ctx, dir, keySigningKey); err != nil {
		return Signing{}, err
	}

	return s, nil
}

// HooksDir returns the folder git runs the worktree at dir's hooks from: core.hooksPath
// when set, else the hooks folder in the common git folder.
func (g *Git) HooksDir(ctx context.Context, dir string) (string, error) {
	return g.gitPath(ctx, dir, hooksName)
}

// HookPaths returns the worktree at dir's top folder, hooks folder, and common git folder,
// which together tell a hooks folder only this clone uses from one shared with others.
func (g *Git) HookPaths(ctx context.Context, dir string) (HookPaths, error) {
	out, err := g.read(ctx, dir, hookPathsArgs())
	if err != nil {
		return HookPaths{}, err
	}

	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != 2 || lines[0] == "" || lines[1] == "" {
		return HookPaths{}, fmt.Errorf("%w: the top and hooks folders as %q", errOutput, out)
	}

	common, err := g.read(ctx, dir, commonDirArgs())
	if err != nil {
		return HookPaths{}, err
	}

	if firstLine(common) == "" {
		return HookPaths{}, fmt.Errorf("%w: the common git folder as %q", errOutput, common)
	}

	return HookPaths{
		Toplevel: absolute(dir, lines[0]), Hooks: absolute(dir, lines[1]), CommonDir: absolute(dir, firstLine(common)),
	}, nil
}

// gitPath returns where git keeps name for the worktree at dir.
func (g *Git) gitPath(ctx context.Context, dir, name string) (string, error) {
	out, err := g.read(ctx, dir, gitPathArgs(name))
	if err != nil {
		return "", err
	}

	if firstLine(out) == "" {
		return "", fmt.Errorf("%w: the path of %s as %q", errOutput, name, out)
	}

	return absolute(dir, firstLine(out)), nil
}

// resolve returns the object ID ref names in the clone, or "" when it names none.
func (g *Git) resolve(ctx context.Context, dir, ref string) (string, error) {
	out, yes, err := g.ask(ctx, dir, verifyArgs(ref))
	if err != nil || !yes {
		return "", err
	}

	oid := firstLine(out)
	if !objectIDPattern.MatchString(oid) {
		return "", fmt.Errorf("%w: an object ID as %q", errOutput, out)
	}

	return oid, nil
}

// config returns the value of the config key as git reads it in the clone, or "" when it
// isn't set.
func (g *Git) config(ctx context.Context, dir, key string) (string, error) {
	out, yes, err := g.ask(ctx, dir, configArgs(key))
	if err != nil || !yes {
		return "", err
	}

	return firstLine(out), nil
}

// flag returns the boolean config key as git reads it in the clone; unset is false.
func (g *Git) flag(ctx context.Context, dir, key string) (bool, error) {
	out, yes, err := g.ask(ctx, dir, boolConfigArgs(key))
	if err != nil || !yes {
		return false, err
	}

	switch firstLine(out) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("%w: %s as %q", errOutput, key, out)
	}
}

// read runs args in dir and returns what git printed, or an error with what git said.
func (g *Git) read(ctx context.Context, dir string, args []string) ([]byte, error) {
	stdout, stderr, err := g.Run(ctx, dir, args...)
	if err != nil {
		return nil, failure(args, stderr, err)
	}

	return stdout, nil
}

// ask runs a yes-or-no command in dir: exit 0 is yes, with what git printed, and exit 1
// is no. Anything else is an error with what git said.
func (g *Git) ask(ctx context.Context, dir string, args []string) ([]byte, bool, error) {
	stdout, stderr, err := g.Run(ctx, dir, args...)

	switch {
	case err == nil:
		return stdout, true, nil
	case ctx.Err() == nil && execpath.ExitCode(err) == 1:
		return nil, false, nil
	default:
		return nil, false, failure(args, stderr, err)
	}
}

// failure wraps err from the command args with the line of stderr worth showing, as a
// *commandError. A refused command's error is returned as it is.
func failure(args []string, stderr []byte, err error) error {
	if errors.Is(err, ErrGitCommandNotAllowed) {
		return err
	}

	return &commandError{cmd: args[0], said: execpath.GitMessage(stderr), err: err}
}

// commandError is a git command's failure: cmd is the command's first word, said the
// line of standard error worth showing, "" when git said nothing worth it, and err what
// it failed with. Message reads what git said from it, however a caller wraps it.
type commandError struct {
	cmd  string
	said string
	err  error
}

// Error returns "git <cmd>: <said>: <err>", without said when git said nothing.
func (e *commandError) Error() string {
	if e.said == "" {
		return "git " + e.cmd + ": " + e.err.Error()
	}

	return "git " + e.cmd + ": " + e.said + ": " + e.err.Error()
}

// Unwrap returns what the command failed with.
func (e *commandError) Unwrap() error { return e.err }
