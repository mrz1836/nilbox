package worktrees

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// Shape of the benchmark clone root: a few clones, each with a few agent worktrees, one
// of them with uncommitted changes.
const (
	benchClones    = 3
	benchWorktrees = 4
)

// BenchmarkList lists the worktrees of every clone under the clone root with real git,
// as the Worktrees tab and every sweep do.
func BenchmarkList(b *testing.B) {
	e := newEnv(b)

	for c := range benchClones {
		repo := e.newRepo(b, fmt.Sprintf("clone-%d", c))

		for w := range benchWorktrees {
			name := fmt.Sprintf("wt-%d", w)
			path := addWorktree(b, repo, filepath.Join(repo, ".claude", "worktrees", name), ownBranchPrefix+name)

			if w == 0 {
				testutil.WriteFile(b, filepath.Join(path, "notes.txt"), "draft\n")
			}
		}
	}

	snap, err := e.svc.List(b.Context())
	require.NoError(b, err)
	require.Len(b, snap.Repos, benchClones)

	b.ReportAllocs()

	for b.Loop() {
		if _, err = e.svc.List(b.Context()); err != nil {
			b.Fatal(err)
		}
	}
}
