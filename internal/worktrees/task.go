package worktrees

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// A task's worktree (EnsureRequest.Task) is made new every time, never resumed, adopted,
// or attached: its name is the task's base, as deps-20261121, or, when that is taken, the
// first free one of <base>-2, <base>-3, and on. A name is taken while anything is at its
// managed path, while git has a worktree at that path or on its branch (worktree-<name>),
// its folder there or gone, while the clone has that local branch, and while it has a
// remote-tracking branch of that name on any remote: one left from an earlier task, pushed
// and since merged, would relate the new worktree, not pushed yet, to the old pull request,
// as branchAsk links a worktree through the remote-tracking branch of its branch's name.
// So a second task the same day takes <base>-2, and one after a worktree removed with its
// branch kept takes the next name too.

// findTask works out a task's new worktree, changing nothing and asking no remote: under
// the first free name (freeTaskName), on a new branch of its own, from the repository's
// default branch, the request's when it is a branch's name, else the one the clone last
// learned from its remote's HEAD, if any. The clone must be the repository's own folder,
// the main worktree git lists first, not a linked worktree: a task's worktree goes beside
// the clone's own, where a listing and a sweep find it as the app's.
func (m *Maker) findTask(ctx context.Context, g git, req EnsureRequest, entries []Entry) (found, error) {
	shown := m.s.display(req.Clone)

	if !slices.ContainsFunc(entries, func(e Entry) bool { return e.Main && !e.Bare && paths.Same(e.Path, req.Clone) }) {
		return found{}, uierr.New(shown+" is a linked worktree, not a clone of its own, so no worktree is made from it.",
			fmt.Errorf("%w: %s", ErrNotMainWorktree, shown))
	}

	refs, err := g.readRefs(ctx, req.Clone)
	if err != nil {
		return found{}, fmt.Errorf("read the branches of %s: %w", shown, err)
	}

	name, ok := freeTaskName(req, entries, refs)
	if !ok {
		return found{}, uierr.New(fmt.Sprintf("Every name for today's worktree of %s, %s to %s, is taken. Remove some of "+
			"them on the Worktrees tab, then launch again.", shown, req.Task, taskName(req.Task, maxTaskNames)),
			fmt.Errorf("%w: %s in %s", ErrNoTaskName, req.Task, shown))
	}

	known := refs.defaults[req.remote()]
	if gitread.ValidBranch(req.DefaultBranch) {
		known = req.DefaultBranch
	}

	return found{
		action: ActionCreate, path: ManagedPath(req.Clone, name), branch: ownBranchPrefix + name,
		base: req.remote() + "/" + defaultName(known), task: name, defaultBranch: known,
	}, nil
}

// freeTaskName returns the first name of req's task no worktree, branch, or file has: its
// base, then <base>-2 up to <base>-<maxTaskNames>, as entries, the clone's worktrees, and
// refs, its branches, say, and as its managed folder holds. It reports false when every
// one is taken.
func freeTaskName(req EnsureRequest, entries []Entry, refs cloneRefs) (string, bool) {
	for n := 1; n <= maxTaskNames; n++ {
		name := taskName(req.Task, n)
		if !taskNameTaken(ManagedPath(req.Clone, name), ownBranchPrefix+name, entries, refs) {
			return name, true
		}
	}

	return "", false
}

// taskName is the nth name a task of base may take: base itself first, then <base>-<n>.
func taskName(base string, n int) string {
	if n <= 1 {
		return base
	}

	return base + "-" + strconv.Itoa(n)
}

// taskNameTaken reports a task's worktree at path, on branch, that can't be made new there:
// something is at path, or may be, as when it can't be looked at; git has a worktree at
// path or on branch, its folder there or gone; or the clone has branch, locally or as a
// remote-tracking branch of any remote.
func taskNameTaken(path, branch string, entries []Entry, refs cloneRefs) bool {
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		return true
	}

	if slices.ContainsFunc(entries, func(e Entry) bool { return e.Branch == branch || paths.Same(e.Path, path) }) {
		return true
	}

	if _, ok := refs.local(branch); ok {
		return true
	}

	for ref := range refs.tracking {
		if strings.HasSuffix(ref, "/"+branch) {
			return true
		}
	}

	return false
}
