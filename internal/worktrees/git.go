package worktrees

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	// gitBin is the program name used when no Finder resolves an absolute path.
	gitBin = "git"

	// worktreeCmd is git's worktree subcommand.
	worktreeCmd = "worktree"
)

// ErrGitCommandNotAllowed is returned for a git command outside the allowlist; it is
// never run.
var ErrGitCommandNotAllowed = errors.New("git command not allowed")

// errParseCommitTime is returned when git log prints something other than a Unix time.
var errParseCommitTime = errors.New("unexpected commit time")

// Runner runs a program with arguments and returns what it wrote. Implementations run the
// argv directly, never through a shell; execpath.Runner is the app's.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error)
}

// allowedFixedCommands are the complete argument lists, after `-C <dir>`, that git may
// run with. `worktree remove` is checked separately because it takes a path.
func allowedFixedCommands() []string {
	return []string{
		joinArgs(listArgs()),
		joinArgs(pruneArgs()),
		joinArgs(statusArgs()),
		joinArgs(lastCommitArgs()),
	}
}

func listArgs() []string { return []string{worktreeCmd, "list", "--porcelain", "-z"} }

func pruneArgs() []string { return []string{worktreeCmd, "prune"} }

func statusArgs() []string {
	return []string{"status", "--porcelain=v1", "-z", "--untracked-files=all"}
}

func lastCommitArgs() []string { return []string{"log", "-1", "--format=%ct"} }

// git runs allowlisted git commands as `git -C <dir> <args>`.
type git struct {
	run Runner
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

// allowed reports whether args is one of the allowlisted git commands. The removal path
// must not look like an option.
func allowed(args []string) bool {
	joined := joinArgs(args)
	for _, cmd := range allowedFixedCommands() {
		if joined == cmd {
			return true
		}
	}

	if len(args) < 3 || args[0] != worktreeCmd || args[1] != "remove" {
		return false
	}

	rest := args[2:]
	if len(rest) == 2 && rest[0] == "--force" {
		rest = rest[1:]
	}

	return len(rest) == 1 && rest[0] != "" && !strings.HasPrefix(rest[0], "-")
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

	for _, field := range bytes.Split(out, []byte{0}) {
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

// exitCode reports whether err is a process exit with a non-zero status (git ran and
// refused), as opposed to a failure to run git at all or a canceled context.
func exitCode(ctx context.Context, err error) bool {
	var exit interface{ ExitCode() int }

	return ctx.Err() == nil && errors.As(err, &exit) && exit.ExitCode() > 0
}

// firstLine returns git's first non-empty stderr line without its "fatal: " or
// "error: " prefix.
func firstLine(stderr []byte) string {
	for _, line := range strings.Split(string(stderr), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		for _, prefix := range []string{"fatal: ", "error: "} {
			line = strings.TrimPrefix(line, prefix)
		}

		return line
	}

	return ""
}
