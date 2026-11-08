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
)

const (
	// gitBin is the program name used when no Finder resolves an absolute path.
	gitBin = "git"

	// worktreeCmd is git's worktree subcommand.
	worktreeCmd = "worktree"

	// ownBranchPrefix starts the name of the branch an item's worktree is made on:
	// "worktree-<name>". It is the only kind of branch ever deleted, and the only kind a
	// worktree is ever made on.
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
	}
}

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

// lsRemoteArgs asks remote which branch its HEAD points at.
func lsRemoteArgs(remote string) []string { return []string{"ls-remote", "--symref", remote, "HEAD"} }

// git runs allowlisted git commands as `git -C <dir> <args>`.
type git struct {
	run execpath.Runner
	bin string
}

// do runs git in dir with args after checking them against the allowlist.
func (g git) do(ctx context.Context, dir string, args ...string) (stdout, stderr []byte, err error) {
	if !allowed(args) {
		return nil, nil, fmt.Errorf("%w: git %s", ErrGitCommandNotAllowed, strings.Join(args, " "))
	}

	argv := make([]string, 0, len(args)+2)
	argv = append(argv, "-C", dir)
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

// deleteBranch deletes the local branch only while it still points at oid, so a commit
// made after the check keeps it. Only the agent's own worktree-<name> branches and a real
// commit ID pass the allowlist.
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

	return allowedCommitTimes(args) || allowedLookup(args) || allowedCreate(args) || allowedChange(args)
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
		return len(args) == 4 && isOwnBranchRef(args[2]) && isCommitID(args[3])
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
	case len(args) == 4 && joinArgs(args) == joinArgs(lsRemoteArgs(args[2])):
		return remotePattern.MatchString(args[2])
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
