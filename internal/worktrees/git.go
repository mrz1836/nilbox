package worktrees

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/gitread"
)

const (
	// gitBin is the program name used when no Finder resolves an absolute path.
	gitBin = "git"

	// worktreeCmd is git's worktree subcommand.
	worktreeCmd = "worktree"

	// ownBranchPrefix starts the name of the branch an item's worktree is made on:
	// "worktree-<name>". It is the only kind of branch a worktree is ever made on.
	ownBranchPrefix = "worktree-"
)

// ErrGitCommandNotAllowed is returned for a git command outside the allowlist; it is
// never run.
var ErrGitCommandNotAllowed = errors.New("git command not allowed")

// errParseCommitTime is returned when git log prints something other than a Unix time.
var errParseCommitTime = errors.New("unexpected commit time")

// errParseCount is returned when git rev-list prints something other than a count.
var errParseCount = errors.New("unexpected commit count")

// objectIDPattern is a full commit ID, SHA-1 or SHA-256.
var objectIDPattern = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

// allowedFixedCommands are the complete argument lists, after `-C <dir>`, that git may
// run with. `worktree remove`, `worktree unlock`, and `update-ref -d` are checked
// separately because they take a path or a branch.
func allowedFixedCommands() []string {
	return []string{
		joinArgs(listArgs()),
		joinArgs(pruneArgs()),
		joinArgs(statusArgs()),
		joinArgs(lastCommitArgs()),
		joinArgs(unpushedArgs()),
		joinArgs(branchListArgs()),
		joinArgs(remoteHeadsArgs()),
	}
}

// branchListArgs lists the clone's local branches: each one's name, the commit it points
// at, and the worktree that has it checked out, if any, separated by NULs.
func branchListArgs() []string {
	return []string{"for-each-ref", "--format=%(refname:lstrip=2)%00%(objectname)%00%(worktreepath)", headsPrefix}
}

// remoteHeadsArgs prints the branch each remote's HEAD points at, as the clone last
// learned it: the repository's default branch.
func remoteHeadsArgs() []string {
	return []string{"for-each-ref", "--format=%(symref:lstrip=3)", remotesRef + "*/HEAD"}
}

// branchUnpushedArgs counts the commits on oid that no remote-tracking branch has.
func branchUnpushedArgs(oid string) []string {
	return []string{"rev-list", "--count", oid, "--not", "--remotes"}
}

// ancestorArgs succeeds when oid is of or one of its ancestors.
func ancestorArgs(oid, of string) []string { return []string{"merge-base", "--is-ancestor", oid, of} }

func listArgs() []string { return []string{worktreeCmd, "list", "--porcelain", "-z"} }

func pruneArgs() []string { return []string{worktreeCmd, "prune"} }

func statusArgs() []string {
	return []string{"status", "--porcelain=v1", "-z", "--untracked-files=all"}
}

func lastCommitArgs() []string { return []string{"log", "-1", "--format=%ct"} }

// commitTimesCmd starts the argument list that prints the commit time of each commit
// named after it, each once, without walking their history.
func commitTimesCmd() []string { return []string{"log", "--no-walk=unsorted", "--format=%H %ct"} }

// commitTimesArgs reads the commit times of oids in one run.
func commitTimesArgs(oids []string) []string { return append(commitTimesCmd(), oids...) }

// unpushedArgs counts the commits on HEAD that no remote-tracking branch has.
func unpushedArgs() []string { return []string{"rev-list", "--count", "HEAD", "--not", "--remotes"} }

// revParseArgs checks that ref names a commit, printing it.
func revParseArgs(ref string) []string { return []string{"rev-parse", "--verify", "--quiet", ref} }

// symbolicRefArgs prints the branch the clone last learned remote's HEAD points at.
func symbolicRefArgs(remote string) []string {
	return []string{"symbolic-ref", "--quiet", remotesRef + remote + "/HEAD"}
}

// uploadPack asks a remote for git's own upload-pack, as gitread's ls-remote does, so a
// remote.<name>.uploadpack in the clone's config runs nothing.
const uploadPack = "--upload-pack=git-upload-pack"

// lsRemoteArgs asks remote which branch its HEAD points at.
func lsRemoteArgs(remote string) []string {
	return []string{"ls-remote", uploadPack, "--symref", remote, "HEAD"}
}

// git runs allowlisted git commands as `git -C <dir> <args>`, and, with a guard, those that
// reach a remote or write a worktree's files under gitread's guards.
type git struct {
	run   execpath.Runner
	bin   string
	guard Guard
}

// reach runs args, a command that reaches the clone's remote, as gitread's RemoteSetupFor
// works it out for that remote (guarded), and returns the clone's own settings it set
// aside, which may be why it failed. Without a guard it runs as do does. A remote the guard
// refuses, a remote helper or a rerouted one, is refused before anything runs.
func (g git) reach(ctx context.Context, dir, remote string, args ...string) (stdout, stderr []byte, aside []string,
	err error,
) {
	if g.guard == nil {
		stdout, stderr, err = g.do(ctx, dir, args...)

		return stdout, stderr, nil, err
	}

	setup, err := g.guard.RemoteSetupFor(ctx, dir, remote)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w: %w", errGuard, err)
	}

	stdout, stderr, err = g.guarded(ctx, dir, setup, args)

	return stdout, stderr, setup.SetAside, err
}

// write runs args, a command that writes a worktree's files and reaches no remote, as
// gitread's CheckoutSetup works it out (guarded). Without a guard it runs as do does.
func (g git) write(ctx context.Context, dir string, args ...string) (stdout, stderr []byte, err error) {
	if g.guard == nil {
		return g.do(ctx, dir, args...)
	}

	setup, err := g.guard.CheckoutSetup(ctx, dir)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", errGuard, err)
	}

	return g.guarded(ctx, dir, setup, args)
}

// guarded runs args, which must pass the allowlist, with setup's git, its environment, and
// its settings (gitread.Hardening first) ahead of them.
func (g git) guarded(ctx context.Context, dir string, setup gitread.Setup, args []string) (stdout, stderr []byte,
	err error,
) {
	if !allowed(args) {
		return nil, nil, fmt.Errorf("%w: git %s", ErrGitCommandNotAllowed, strings.Join(args, " "))
	}

	argv := make([]string, 0, len(setup.Settings)+len(args)+2)
	argv = append(argv, "-C", dir)
	argv = append(argv, setup.Settings...)
	argv = append(argv, args...)

	return execpath.Exec{Env: setup.Env}.Run(ctx, setup.Git, argv...)
}

// hardening are the settings every git command here runs with, gitread.Hardening: the
// ones every git command that changes a clone runs with. Git runs hooks, and the file
// system monitor, with the worktree as their working directory, and a worktree holds a
// pull request's own files, which may come from anyone: a hook that runs a tool from the
// checkout (node_modules/.bin, say) would run the pull request's code before any agent
// starts, outside every limit a launch sets. None of these commands needs either, nor to
// recurse into submodules, whose URLs the pull request names.
func hardening() []string {
	return gitread.Hardening()
}

// do runs git in dir with args after checking them against the allowlist, with hooks,
// the file system monitor, and submodule recursion off (see hardening).
func (g git) do(ctx context.Context, dir string, args ...string) (stdout, stderr []byte, err error) {
	if !allowed(args) {
		return nil, nil, fmt.Errorf("%w: git %s", ErrGitCommandNotAllowed, strings.Join(args, " "))
	}

	safe := hardening()
	argv := make([]string, 0, len(safe)+len(args)+2)
	argv = append(argv, "-C", dir)
	argv = append(argv, safe...)
	argv = append(argv, args...)

	return g.run.Run(ctx, g.bin, argv...)
}

// list returns the worktrees of the repository at dir.
func (g git) list(ctx context.Context, dir string) ([]Entry, []byte, error) {
	stdout, stderr, err := g.do(ctx, dir, listArgs()...)
	if err != nil {
		return nil, stderr, err
	}

	entries, err := ParsePorcelain(stdout)

	return entries, nil, err
}

// remove removes the worktree at path, passing a single --force when force is set.
func (g git) remove(ctx context.Context, dir, path string, force bool) ([]byte, error) {
	args := []string{worktreeCmd, "remove"}
	if force {
		args = append(args, "--force")
	}

	_, stderr, err := g.do(ctx, dir, append(args, path)...)

	return stderr, err
}

// unlock releases the lock on the worktree at path.
func (g git) unlock(ctx context.Context, dir, path string) ([]byte, error) {
	_, stderr, err := g.do(ctx, dir, worktreeCmd, "unlock", path)

	return stderr, err
}

// localBranch is one of a clone's local branches.
type localBranch struct {
	// name is the branch, as in feature-x, and head the commit it points at.
	name string
	head string

	// worktree is the worktree that has it checked out, or "".
	worktree string
}

// branches lists the local branches of the clone at dir.
func (g git) branches(ctx context.Context, dir string) ([]localBranch, []byte, error) {
	stdout, stderr, err := g.do(ctx, dir, branchListArgs()...)
	if err != nil {
		return nil, stderr, err
	}

	var out []localBranch

	for line := range strings.Lines(string(stdout)) {
		fields := strings.Split(strings.TrimRight(line, "\n"), "\x00")
		if len(fields) != 3 || fields[0] == "" || !isCommitID(fields[1]) {
			continue
		}

		out = append(out, localBranch{name: fields[0], head: fields[1], worktree: fields[2]})
	}

	return out, nil, nil
}

// remoteHeads returns the branches the clone's remotes' HEADs point at, as last fetched.
// A clone that cannot say has none.
func (g git) remoteHeads(ctx context.Context, dir string) []string {
	stdout, _, err := g.do(ctx, dir, remoteHeadsArgs()...)
	if err != nil {
		return nil
	}

	var out []string

	for line := range strings.Lines(string(stdout)) {
		if name := strings.TrimSpace(line); name != "" && !slices.Contains(out, name) {
			out = append(out, name)
		}
	}

	return out
}

// unpushedOf returns how many commits on oid no remote-tracking branch has, read in the
// repository at dir.
func (g git) unpushedOf(ctx context.Context, dir, oid string) (int, error) {
	stdout, _, err := g.do(ctx, dir, branchUnpushedArgs(oid)...)
	if err != nil {
		return 0, err
	}

	count, err := strconv.Atoi(strings.TrimSpace(string(stdout)))
	if err != nil || count < 0 {
		return 0, fmt.Errorf("%w %q", errParseCount, stdout)
	}

	return count, nil
}

// isAncestor reports oid as of or one of its ancestors, in the repository at dir. A commit
// the clone does not have, or a failure, is no.
func (g git) isAncestor(ctx context.Context, dir, oid, of string) bool {
	_, _, err := g.do(ctx, dir, ancestorArgs(oid, of)...)

	return err == nil
}

// deleteBranch deletes the local branch only while it still points at oid, so a commit
// made after the check keeps it. Only a well-formed branch and a real commit ID pass the
// allowlist; the callers decide which branches may go (see deletable).
func (g git) deleteBranch(ctx context.Context, dir, branch, oid string) ([]byte, error) {
	_, stderr, err := g.do(ctx, dir, "update-ref", "-d", headsPrefix+branch, oid)

	return stderr, err
}

// unpushed returns how many commits on HEAD in the worktree at dir no remote-tracking
// branch has.
func (g git) unpushed(ctx context.Context, dir string) (int, []byte, error) {
	stdout, stderr, err := g.do(ctx, dir, unpushedArgs()...)
	if err != nil {
		return 0, stderr, err
	}

	count, err := strconv.Atoi(strings.TrimSpace(string(stdout)))
	if err != nil || count < 0 {
		return 0, nil, fmt.Errorf("%w %q", errParseCount, stdout)
	}

	return count, nil, nil
}

// prune clears the entries of worktrees whose directories are gone.
func (g git) prune(ctx context.Context, dir string) ([]byte, error) {
	_, stderr, err := g.do(ctx, dir, pruneArgs()...)

	return stderr, err
}

// dirtyCount returns the number of changed and untracked files in the worktree at dir.
func (g git) dirtyCount(ctx context.Context, dir string) (int, []byte, error) {
	stdout, stderr, err := g.do(ctx, dir, statusArgs()...)
	if err != nil {
		return 0, stderr, err
	}

	return countStatusEntries(stdout), nil, nil
}

// commitTimes returns the commit time of each of oids, in Unix seconds, by commit ID, read
// in the repository at dir with one git run. oids must be full commit IDs; a commit git
// cannot read fails the whole run.
func (g git) commitTimes(ctx context.Context, dir string, oids []string) (map[string]int64, error) {
	stdout, _, err := g.do(ctx, dir, commitTimesArgs(oids)...)
	if err != nil {
		return nil, err
	}

	times := make(map[string]int64, len(oids))

	for line := range strings.Lines(string(stdout)) {
		oid, unix, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			return nil, fmt.Errorf("%w %q", errParseCommitTime, line)
		}

		at, parseErr := strconv.ParseInt(unix, 10, 64)
		if parseErr != nil {
			return nil, fmt.Errorf("%w %q: %w", errParseCommitTime, line, parseErr)
		}

		times[oid] = at
	}

	return times, nil
}

// lastCommit returns the commit time of HEAD in the worktree at dir, in Unix seconds.
func (g git) lastCommit(ctx context.Context, dir string) (int64, error) {
	stdout, _, err := g.do(ctx, dir, lastCommitArgs()...)
	if err != nil {
		return 0, err
	}

	unix, err := strconv.ParseInt(strings.TrimSpace(string(stdout)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w %q: %w", errParseCommitTime, stdout, err)
	}

	return unix, nil
}

// allowed reports whether args is one of the allowlisted git commands. A path must not
// look like an option.
func allowed(args []string) bool {
	if slices.Contains(allowedFixedCommands(), joinArgs(args)) {
		return true
	}

	return allowedCommitTimes(args) || allowedLookup(args) || allowedBranchLookup(args) || allowedCreate(args) ||
		allowedChange(args)
}

// allowedChange reports a removal command with arguments it may run with: removing or
// unlocking a worktree at a path, and deleting an item's own branch at a commit.
func allowedChange(args []string) bool {
	if len(args) < 3 {
		return false
	}

	switch args[0] + " " + args[1] {
	case worktreeCmd + " remove":
		return allowedRemove(args[2:])
	case worktreeCmd + " unlock":
		return len(args) == 3 && isPathArg(args[2])
	case "update-ref -d":
		return len(args) == 4 && isBranchRef(args[2]) && isCommitID(args[3])
	default:
		return false
	}
}

// allowedCommitTimes reports a commit-times run: the fixed words, then one or more full
// commit IDs and nothing else.
func allowedCommitTimes(args []string) bool {
	head := commitTimesCmd()
	if len(args) <= len(head) || joinArgs(args[:len(head)]) != joinArgs(head) {
		return false
	}

	for _, oid := range args[len(head):] {
		if !isCommitID(oid) {
			return false
		}
	}

	return true
}

// allowedLookup reports a read-only lookup Ensure makes: whether an item's branch or a
// remote-tracking branch exists, and which branch a remote's HEAD points at.
func allowedLookup(args []string) bool {
	switch {
	case len(args) == 4 && joinArgs(args[:3]) == joinArgs(revParseArgs("")[:3]):
		return isOwnBranchRef(args[3]) || isTrackingRef(args[3])
	case len(args) == 3 && joinArgs(args[:2]) == joinArgs(symbolicRefArgs("")[:2]):
		return isRemoteHead(args[2])
	case len(args) == 5 && joinArgs(args) == joinArgs(lsRemoteArgs(args[3])):
		return remotePattern.MatchString(args[3])
	default:
		return false
	}
}

// isRemoteHead reports refs/remotes/<remote>/HEAD with a plain remote.
func isRemoteHead(ref string) bool {
	rest, ok := strings.CutPrefix(ref, remotesRef)
	if !ok {
		return false
	}

	remote, ok := strings.CutSuffix(rest, "/HEAD")

	return ok && remotePattern.MatchString(remote)
}

// isTrackingRef reports refs/remotes/<remote>/<branch> with a plain remote and branch.
func isTrackingRef(ref string) bool {
	rest, ok := strings.CutPrefix(ref, remotesRef)
	if !ok {
		return false
	}

	remote, branch, ok := strings.Cut(rest, "/")

	return ok && remotePattern.MatchString(remote) && validBranch(branch)
}

// allowedRemove reports the arguments after `worktree remove` that may run: a path, with
// at most a single --force before it.
func allowedRemove(rest []string) bool {
	if len(rest) == 2 && rest[0] == "--force" {
		rest = rest[1:]
	}

	return len(rest) == 1 && isPathArg(rest[0])
}

// isCommitID reports a full commit ID that names a commit. The null ID is refused: given
// as the old value to `update-ref -d` it turns the comparison off, and git reports it as
// the HEAD of a worktree with no commits.
func isCommitID(oid string) bool {
	return objectIDPattern.MatchString(oid) && strings.Trim(oid, "0") != ""
}

// isPathArg reports a non-empty argument that cannot be read as an option.
func isPathArg(arg string) bool {
	return arg != "" && !strings.HasPrefix(arg, "-")
}

// allowedBranchLookup reports a read-only question about a commit a branch points at: how
// many of its commits no remote-tracking branch has, and whether it is an ancestor of
// another commit.
func allowedBranchLookup(args []string) bool {
	switch {
	case len(args) == 5 && args[0] == "rev-list":
		return isCommitID(args[2]) && joinArgs(args) == joinArgs(branchUnpushedArgs(args[2]))
	case len(args) == 4 && args[0] == "merge-base":
		return isCommitID(args[2]) && isCommitID(args[3]) && joinArgs(args) == joinArgs(ancestorArgs(args[2], args[3]))
	default:
		return false
	}
}

// isBranchRef reports refs/heads/<branch> with a well-formed branch name.
func isBranchRef(ref string) bool {
	name, ok := strings.CutPrefix(ref, headsPrefix)

	return ok && validBranch(name)
}

// isOwnBranchRef reports refs/heads/worktree-<name>, where name is a single plain path
// component, so no other branch and no other kind of ref can be deleted.
func isOwnBranchRef(ref string) bool {
	name, ok := strings.CutPrefix(ref, headsPrefix+ownBranchPrefix)

	return ok && name != "" && !strings.ContainsAny(name, "/\\ ~^:?*[") && !strings.Contains(name, "..") &&
		!strings.HasPrefix(name, ".") && !strings.HasSuffix(name, ".lock")
}

// joinArgs joins an argument list with NULs, which cannot appear in an argument.
func joinArgs(args []string) string {
	return strings.Join(args, "\x00")
}

// countStatusEntries counts the files in `git status --porcelain=v1 -z` output. A rename
// or copy entry is followed by its source path, which is not counted again.
func countStatusEntries(out []byte) int {
	count := 0
	skip := false

	for field := range bytes.SplitSeq(out, []byte{0}) {
		switch {
		case skip:
			skip = false
		case len(field) < len("XY "):
			continue
		default:
			count++
			skip = field[0] == 'R' || field[0] == 'C' || field[1] == 'R' || field[1] == 'C'
		}
	}

	return count
}

// gitRefused reports whether err is a process exit with a non-zero status (git ran and
// refused), as opposed to a failure to run git at all or a canceled context.
func gitRefused(ctx context.Context, err error) bool {
	return ctx.Err() == nil && execpath.ExitCode(err) > 0
}
