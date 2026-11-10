package release

import (
	"context"
	"fmt"
	"slices"

	"github.com/mrz1836/nilbox/internal/gitread"
)

// GitChecker reads a clone's state for the Release page with gitread's read-only commands
// (Checker). It asks nothing of origin: what the clone lacks is judged from GitHub's
// answer, so a row costs no network.
type GitChecker struct {
	Git *gitread.Git
}

// CloneState reads the clone at dir: the branch it's on, its uncommitted files, how its
// branch stands with branch on GitHub (whose newest commit is remoteHead), and the
// version tags only it has. A version tag counts as only on this Mac when GitHub's newest
// tags lack it and it is above their highest version, as magex would then skip it and
// leave a gap; GitHub's list holds only the newest tags, so an older one isn't judged.
// What it can't read, it marks Unreadable, and it never fails.
func (c GitChecker) CloneState(ctx context.Context, dir, branch, remoteHead string, tags []Tag) CloneState {
	out := CloneState{LocalOnly: []string{}}

	current, err := c.Git.CurrentBranch(ctx, dir)
	if err != nil {
		out.Unreadable = true

		return out
	}

	out.Branch, out.Detached = current, current == ""

	changes, err := c.Git.Changes(ctx, dir)
	if err != nil {
		out.Unreadable = true

		return out
	}

	out.Dirty = len(changes)

	if !c.readBranch(ctx, dir, branch, remoteHead, &out) {
		out.Unreadable = true

		return out
	}

	local, err := c.Git.LocalVersionTags(ctx, dir)
	if err != nil {
		out.Unreadable = true

		return out
	}

	out.LocalOnly = localOnly(local, tags)

	return out
}

// readBranch reads how the clone's branch stands with GitHub's: its commits not pushed,
// and whether it lacks GitHub's newest commit. A branch the clone doesn't have is neither.
// It reports whether git answered.
func (c GitChecker) readBranch(ctx context.Context, dir, branch, remoteHead string, out *CloneState) bool {
	if branch == "" {
		return true
	}

	head, err := c.Git.LocalBranch(ctx, dir, branch)
	if err != nil {
		return false
	}

	if head == "" {
		return true
	}

	if out.NotPushed, err = c.notPushed(ctx, dir, branch); err != nil {
		return false
	}

	if out.Behind, err = c.behind(ctx, dir, head, remoteHead); err != nil {
		return false
	}

	return true
}

// notPushed counts the branch's commits its remote-tracking branch lacks, or 0 when it has
// none.
func (c GitChecker) notPushed(ctx context.Context, dir, branch string) (int, error) {
	tracking, err := c.Git.TrackingBranch(ctx, dir, branch)
	if err != nil || tracking == "" {
		return 0, err
	}

	n, err := c.Git.Unpushed(ctx, dir, branch)
	if err != nil {
		return 0, fmt.Errorf("count the unpushed commits: %w", err)
	}

	return n, nil
}

// behind reports whether the clone's branch at head lacks GitHub's newest commit: the
// clone hasn't fetched it, or head is an ancestor of it. A branch that diverged isn't.
func (c GitChecker) behind(ctx context.Context, dir, head, remoteHead string) (bool, error) {
	if remoteHead == "" || head == remoteHead {
		return false, nil
	}

	present, err := c.Git.HasCommit(ctx, dir, remoteHead)
	if err != nil || !present {
		return !present && err == nil, err
	}

	ancestor, err := c.Git.IsAncestor(ctx, dir, head, remoteHead)
	if err != nil {
		return false, fmt.Errorf("compare with GitHub's newest commit: %w", err)
	}

	return ancestor, nil
}

// localOnly returns the version tags in local that GitHub's newest tags lack and that are
// above their highest version, in the order local lists them.
func localOnly(local []string, onGitHub []Tag) []string {
	names := make([]string, 0, len(onGitHub))
	for _, t := range onGitHub {
		names = append(names, t.Name)
	}

	highest, found := Highest(names)
	out := []string{}

	for _, tag := range local {
		v, ok := ParseVersion(tag)
		if !ok || slices.Contains(names, tag) || (found && !highest.Less(v)) {
			continue
		}

		out = append(out, tag)
	}

	return out
}
