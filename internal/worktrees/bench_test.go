package worktrees

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/procs"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// Shape of the benchmark clone root: a few clones, each with a few agent worktrees, one
// of them with uncommitted changes, beside many more clones with no worktrees, each with
// an origin, as most of a user's clones are.
const (
	benchClones    = 3
	benchWorktrees = 4
	benchPlain     = 30
)

// BenchmarkList lists the worktrees of every clone under the clone root with real git,
// as the Worktrees tab and every sweep do, with the running processes matched to every
// worktree: none, a few, and as many as a busy machine's user runs. The listing runs
// whole, then the matching alone, on the worktrees the listing found, without git's
// time around it.
func BenchmarkList(b *testing.B) {
	e := newEnv(b)
	worktrees, elsewhere := benchCloneRoot(b, e)
	counts := []int{0, 50, 200, 500}

	b.Run("listing", func(b *testing.B) {
		for _, n := range counts {
			b.Run(fmt.Sprintf("processes=%d", n), func(b *testing.B) {
				benchListing(b, e, benchRunning(n, worktrees, elsewhere))
			})
		}
	})

	snap, err := e.service(e.git).List(b.Context())
	require.NoError(b, err)

	b.Run("sessions", func(b *testing.B) {
		for _, n := range counts {
			b.Run(fmt.Sprintf("processes=%d", n), func(b *testing.B) {
				running := benchRunning(n, worktrees, elsewhere)

				b.ReportAllocs()

				for b.Loop() {
					markSessions(snap.Repos, running, nil)
				}
			})
		}
	})
}

// benchListing lists the clone root over and over with running as the processes.
func benchListing(b *testing.B, e *env, running []procs.Process) {
	b.Helper()

	svc := e.linked(linkDeps{running: fakeRunning{list: running}})

	snap, err := svc.List(b.Context())
	require.NoError(b, err)
	require.Len(b, snap.Repos, benchClones)

	b.ReportAllocs()

	for b.Loop() {
		if _, err = svc.List(b.Context()); err != nil {
			b.Fatal(err)
		}
	}
}

// benchCloneRoot makes the benchmark's clones and their worktrees, and returns the
// worktrees and the other folders a process can work in: the file system's root, the
// home folder, the clone root, and each clone and its git folder.
func benchCloneRoot(b *testing.B, e *env) (worktrees, elsewhere []string) {
	b.Helper()

	worktrees = make([]string, 0, benchClones*benchWorktrees)
	elsewhere = append(make([]string, 0, 3+2*benchClones), "/", e.home, e.root)

	for c := range benchClones {
		repo := e.newRepo(b, fmt.Sprintf("clone-%d", c))
		elsewhere = append(elsewhere, repo, filepath.Join(repo, ".git"))

		for w := range benchWorktrees {
			name := fmt.Sprintf("wt-%d", w)
			path := addWorktree(b, repo, filepath.Join(repo, ".claude", "worktrees", name), ownBranchPrefix+name)
			worktrees = append(worktrees, path)

			if w == 0 {
				testutil.WriteFile(b, filepath.Join(path, "notes.txt"), "draft\n")
			}
		}
	}

	for p := range benchPlain {
		name := fmt.Sprintf("plain-%02d", p)
		testutil.RunGit(b, e.newRepo(b, name), "remote", "add", "origin", "https://github.com/acme-org/"+name+".git")
	}

	return worktrees, elsewhere
}

// benchRunning returns n processes, one in ten working in a worktree and the rest in the
// folders elsewhere, by PID. Their PIDs are past any this OS hands out, so none is the
// benchmark's own.
func benchRunning(n int, worktrees, elsewhere []string) []procs.Process {
	const firstPID = 10_000_000

	list := make([]procs.Process, 0, n)

	for i := range n {
		dir := elsewhere[i%len(elsewhere)]
		if i%10 == 0 {
			dir = worktrees[(i/10)%len(worktrees)]
		}

		list = append(list, procs.Process{PID: firstPID + i, PPID: firstPID - 1, Command: "node", Dir: dir})
	}

	return list
}
