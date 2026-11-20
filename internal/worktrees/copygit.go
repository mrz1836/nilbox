package worktrees

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/gitread"
)

// The git reads that compare a worktree's commits with the pushed branches (see
// copies.go). Each is a read (readOnly), run with gitread's read environment, and the
// allowlist holds each to its exact words: commit IDs, a remote-tracking branch, and a
// whole number of seconds, nothing else.

// copyUnpushedCmd starts the log of the commits on one or more HEADs that aren't on GitHub:
// each one's commit ID, its parents' IDs separated by spaces, its author time, its author
// email, and its subject, separated by NULs, one line each, at most copyBatchMax, with no
// signature check and no mailmap.
func copyUnpushedCmd() []string {
	return []string{
		"log", noSignatures, noMailmap, "--max-count=" + strconv.Itoa(copyBatchMax), "--format=%H%x00%P%x00%at%x00%ae%x00%s",
	}
}

// copyUnpushedArgs lists the commits on heads that no remote-tracking branch has, nor
// fetched, the commit the worktrees were made at from GitHub, when it is one: those
// unpushedArgs counts for each, together. A fetched commit the clone no longer has is left
// out (--ignore-missing), as unpushedPastArgs leaves it out.
func copyUnpushedArgs(heads []string, fetched string) []string {
	args := copyUnpushedCmd()
	if fetched != "" {
		args = append(args, "--ignore-missing")
	}

	args = append(append(args, heads...), "--not", "--remotes")
	if fetched != "" {
		args = append(args, fetched)
	}

	return args
}

// copyCandidatesCmd starts the log of the commits on the remote-tracking branches that may
// be copies: each one's commit ID, the branch it was found on, its author time, author
// email, and subject, separated by NULs, one line each, at most copyCandidatesMax, no
// merge, no signature check, and no mailmap.
func copyCandidatesCmd() []string {
	return []string{
		"log", noSignatures, noMailmap, "--no-merges", "--source", "--max-count=" + strconv.Itoa(copyCandidatesMax),
		"--format=%H%x00%S%x00%at%x00%ae%x00%s",
	}
}

// sincePrefix starts the argument that keeps a log to the commits made at or after a Unix
// time, which the @ has git read as one, never as a date.
const sincePrefix = "--since=@"

// copyCandidatesArgs lists the commits on the remote-tracking branches made at or after
// since, in Unix seconds, past base, the default branch's remote-tracking branch, when it
// is not "".
func copyCandidatesArgs(since int64, base string) []string {
	args := append(copyCandidatesCmd(), sincePrefix+strconv.FormatInt(since, 10), "--remotes")
	if base != "" {
		args = append(args, "--not", base)
	}

	return args
}

// copyCherryArgs marks each commit on head that tip doesn't have, past their merge base:
// = when tip has one with an identical change (git's patch ID), + when it doesn't. No merge
// is marked.
func copyCherryArgs(tip, head string) []string {
	return []string{"rev-list", "--cherry-mark", "--right-only", "--no-merges", tip + "..." + head}
}

// allowedCopyRead reports one of the comparison's reads, with the arguments it may take.
func allowedCopyRead(args []string) bool {
	return allowedCopyUnpushed(args) || allowedCopyCandidates(args) || allowedCopyCherry(args) || allowedCopyPatches(args)
}

// allowedCopyUnpushed reports a log of the commits not on GitHub of one or more full commit
// IDs: past the remote-tracking branches, or, with --ignore-missing, past them and one more
// full commit ID.
func allowedCopyUnpushed(args []string) bool {
	head := copyUnpushedCmd()
	if len(args) <= len(head) || joinArgs(args[:len(head)]) != joinArgs(head) {
		return false
	}

	rest := args[len(head):]
	fetched := ""

	if rest[0] == "--ignore-missing" {
		if len(rest) < 2 || !gitread.IsCommitID(rest[len(rest)-1]) {
			return false
		}

		fetched, rest = rest[len(rest)-1], rest[1:len(rest)-1]
	}

	n := len(rest) - 2
	if n < 1 || !allCommitIDs(rest[:n]) {
		return false
	}

	return joinArgs(args) == joinArgs(copyUnpushedArgs(rest[:n], fetched))
}

// allowedCopyCandidates reports a log of the commits on the remote-tracking branches made at
// or after a whole number of seconds, past one remote-tracking branch or none.
func allowedCopyCandidates(args []string) bool {
	head := copyCandidatesCmd()
	if len(args) <= len(head) || joinArgs(args[:len(head)]) != joinArgs(head) {
		return false
	}

	since, ok := sinceArg(args[len(head)])
	if !ok {
		return false
	}

	switch len(args) - len(head) {
	case 2:
		return joinArgs(args) == joinArgs(copyCandidatesArgs(since, ""))
	case 4:
		base := args[len(args)-1]

		return isTrackingRef(base) && joinArgs(args) == joinArgs(copyCandidatesArgs(since, base))
	default:
		return false
	}
}

// sinceArg reads a --since=@<seconds> argument, a whole number of seconds written as
// strconv writes it: no sign, no leading zero, no space.
func sinceArg(arg string) (int64, bool) {
	digits, ok := strings.CutPrefix(arg, sincePrefix)
	if !ok {
		return 0, false
	}

	since, err := strconv.ParseInt(digits, 10, 64)

	return since, err == nil && since >= 0 && strconv.FormatInt(since, 10) == digits
}

// allowedCopyCherry reports a comparison of a full commit ID's commits with another's.
func allowedCopyCherry(args []string) bool {
	if len(args) != len(copyCherryArgs("", "")) {
		return false
	}

	tip, head, ok := strings.Cut(args[len(args)-1], "...")

	return ok && gitread.IsCommitID(tip) && gitread.IsCommitID(head) && joinArgs(args) == joinArgs(copyCherryArgs(tip, head))
}

// allCommitIDs reports a list of full commit IDs.
func allCommitIDs(oids []string) bool {
	return !slices.ContainsFunc(oids, func(oid string) bool { return !gitread.IsCommitID(oid) })
}

// copyGitError is a comparison's read that git refused, or that warned of something, as a
// patch ID it couldn't work out: stderr says why.
type copyGitError struct {
	err    error
	stderr []byte
}

// Error returns the error's text.
func (e *copyGitError) Error() string { return e.err.Error() }

// Unwrap returns the error.
func (e *copyGitError) Unwrap() error { return e.err }

// errGitWarned is a comparison's read that git ran, but warned of something on the way, so
// its answer may be wrong: a patch ID it couldn't work out marks a commit as having no copy.
var errGitWarned = errors.New("git warned while comparing")

// copyRead runs one of the comparison's reads in the clone at dir: an error when git refused
// it, or warned of anything.
func (g git) copyRead(ctx context.Context, dir string, args []string) ([]byte, error) {
	stdout, stderr, err := g.do(ctx, dir, args...)

	switch {
	case err != nil:
		return nil, &copyGitError{err: err, stderr: stderr}
	case len(strings.TrimSpace(string(stderr))) > 0:
		return nil, &copyGitError{err: errGitWarned, stderr: stderr}
	default:
		return stdout, nil
	}
}

// copyUnpushed reads the commits on heads, in the clone at dir, that no remote-tracking
// branch has, nor fetched, when it is one, by commit ID (see copyUnpushedArgs). A log that
// reached copyBatchMax is an error: it may have left some out.
func (g git) copyUnpushed(ctx context.Context, dir string, heads []string, fetched string) (map[string]copyCommit, error) {
	if !gitread.IsCommitID(fetched) {
		fetched = ""
	}

	stdout, err := g.copyRead(ctx, dir, copyUnpushedArgs(heads, fetched))
	if err != nil {
		return nil, err
	}

	commits, err := parseCopyCommits(stdout, false)
	if err != nil {
		return nil, err
	}

	if len(commits) >= copyBatchMax {
		return nil, errTooManyCommits
	}

	out := make(map[string]copyCommit, len(commits))
	for _, c := range commits {
		out[c.oid] = c
	}

	return out, nil
}

// copyCandidates reads the commits on the remote-tracking branches, in the clone at dir,
// made at or after since, past base when it is not "", each with the branch it was found
// on (see copyCandidatesArgs). A log that reached copyCandidatesMax is an error: it may have
// left some out.
func (g git) copyCandidates(ctx context.Context, dir string, since int64, base string) ([]copyCommit, error) {
	stdout, err := g.copyRead(ctx, dir, copyCandidatesArgs(since, base))
	if err != nil {
		return nil, err
	}

	commits, err := parseCopyCommits(stdout, true)
	if err != nil {
		return nil, err
	}

	if len(commits) >= copyCandidatesMax {
		return nil, errTooManyCandidates
	}

	return commits, nil
}

// cherryMarks compares the commits on head that tip doesn't have with tip's, in the clone at
// dir (see copyCherryArgs), and reports for each by commit ID whether tip has an identical
// change.
func (g git) cherryMarks(ctx context.Context, dir, tip, head string) (map[string]bool, error) {
	stdout, err := g.copyRead(ctx, dir, copyCherryArgs(tip, head))
	if err != nil {
		return nil, err
	}

	marks := map[string]bool{}

	for line := range strings.Lines(string(stdout)) {
		line = strings.TrimSuffix(line, "\n")

		mark, oid := line[:min(len(line), 1)], line[min(len(line), 1):]
		if (mark != "=" && mark != "+") || !gitread.IsCommitID(oid) {
			return nil, fmt.Errorf("%w %q", errParseCommits, line)
		}

		marks[oid] = mark == "="
	}

	return marks, nil
}

var (
	// errTooManyCommits is returned for worktrees with more commits not on GitHub than one
	// log compares.
	errTooManyCommits = errors.New("too many commits that aren't on GitHub to compare")

	// errTooManyCandidates is returned when the pushed branches had more commits lately than
	// one log compares with.
	errTooManyCandidates = errors.New("too many pushed commits to compare with")
)

// parseCopyCommits reads the lines copyUnpushedCmd prints, or, with source, those
// copyCandidatesCmd prints: five fields separated by NULs, the second a commit's parents or
// the branch it was found on, which --remotes names without refs/remotes/.
func parseCopyCommits(stdout []byte, source bool) ([]copyCommit, error) {
	var out []copyCommit

	for line := range strings.Lines(string(stdout)) {
		if line = strings.TrimSuffix(line, "\n"); line == "" {
			continue
		}

		c, err := parseCopyCommit(line, source)
		if err != nil {
			return nil, err
		}

		out = append(out, c)
	}

	return out, nil
}

// parseCopyCommit reads one line parseCopyCommits reads.
func parseCopyCommit(line string, source bool) (copyCommit, error) {
	fields := strings.SplitN(line, "\x00", 5)
	if len(fields) != 5 || !gitread.IsCommitID(fields[0]) {
		return copyCommit{}, fmt.Errorf("%w %q", errParseCommits, line)
	}

	at, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil {
		return copyCommit{}, fmt.Errorf("%w %q: %w", errParseCommits, line, err)
	}

	c := copyCommit{oid: fields[0], at: at, email: fields[3], subject: fields[4]}

	switch {
	case !source:
		c.parents = strings.Fields(fields[1])
	case strings.HasPrefix(fields[1], "refs/"):
		c.ref = fields[1]
	default:
		c.ref = remotesRef + fields[1]
	}

	return c, nil
}
