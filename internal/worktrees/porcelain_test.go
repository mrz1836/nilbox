package worktrees

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	sha       = "0320a26099f14d4a4b68fe33a2ffaf350dd48dd7"
	mainPath  = "/srv/projects/example-repo"
	claudeArg = "claude session pr-7 (pid 4242 start Tue Nov 4 10:00:00 2026)"
)

// porcelain builds `git worktree list --porcelain -z` output: each entry's attributes
// end with a NUL, and an empty attribute ends the entry.
func porcelain(entries ...[]string) []byte {
	var b strings.Builder

	for _, attrs := range entries {
		for _, a := range attrs {
			b.WriteString(a)
			b.WriteByte(0)
		}

		b.WriteByte(0)
	}

	return []byte(b.String())
}

func mainAttrs() []string {
	return []string{"worktree " + mainPath, "HEAD " + sha, "branch refs/heads/main"}
}

func TestParsePorcelain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data []byte
		want []Entry
	}{
		{
			name: "main only",
			data: porcelain(mainAttrs()),
			want: []Entry{{Path: mainPath, HEAD: sha, Branch: "main", Main: true}},
		},
		{
			name: "main and linked",
			data: porcelain(mainAttrs(), []string{"worktree /srv/wt/feature", "HEAD " + sha, "branch refs/heads/feature/x"}),
			want: []Entry{
				{Path: mainPath, HEAD: sha, Branch: "main", Main: true},
				{Path: "/srv/wt/feature", HEAD: sha, Branch: "feature/x"},
			},
		},
		{
			name: "detached",
			data: porcelain(mainAttrs(), []string{"worktree /srv/wt/detached", "HEAD " + sha, "detached"}),
			want: []Entry{
				{Path: mainPath, HEAD: sha, Branch: "main", Main: true},
				{Path: "/srv/wt/detached", HEAD: sha, Detached: true},
			},
		},
		{
			name: "locked with a reason",
			data: porcelain(mainAttrs(), []string{"worktree /srv/wt/pr-7", "HEAD " + sha, "branch refs/heads/worktree-pr-7", "locked " + claudeArg}),
			want: []Entry{
				{Path: mainPath, HEAD: sha, Branch: "main", Main: true},
				{Path: "/srv/wt/pr-7", HEAD: sha, Branch: "worktree-pr-7", Locked: true, LockReason: claudeArg},
			},
		},
		{
			name: "locked without a reason",
			data: porcelain(mainAttrs(), []string{"worktree /srv/wt/held", "HEAD " + sha, "branch refs/heads/held", "locked"}),
			want: []Entry{
				{Path: mainPath, HEAD: sha, Branch: "main", Main: true},
				{Path: "/srv/wt/held", HEAD: sha, Branch: "held", Locked: true},
			},
		},
		{
			name: "prunable",
			data: porcelain(mainAttrs(), []string{
				"worktree /srv/wt/gone", "HEAD " + sha, "branch refs/heads/gone",
				"prunable gitdir file points to non-existent location",
			}),
			want: []Entry{
				{Path: mainPath, HEAD: sha, Branch: "main", Main: true},
				{
					Path: "/srv/wt/gone", HEAD: sha, Branch: "gone",
					Prunable: true, PrunableReason: "gitdir file points to non-existent location",
				},
			},
		},
		{
			name: "prunable without a reason",
			data: porcelain(mainAttrs(), []string{"worktree /srv/wt/gone", "HEAD " + sha, "detached", "prunable"}),
			want: []Entry{
				{Path: mainPath, HEAD: sha, Branch: "main", Main: true},
				{Path: "/srv/wt/gone", HEAD: sha, Detached: true, Prunable: true},
			},
		},
		{
			name: "bare main with a linked worktree",
			data: porcelain([]string{"worktree /srv/example-repo.git", "bare"}, []string{"worktree /srv/wt/a", "HEAD " + sha, "branch refs/heads/a"}),
			want: []Entry{
				{Path: "/srv/example-repo.git", Bare: true, Main: true},
				{Path: "/srv/wt/a", HEAD: sha, Branch: "a"},
			},
		},
		{
			name: "multiple entries, a path with spaces, a non-branch ref, and an unknown attribute",
			data: porcelain(
				mainAttrs(),
				[]string{"worktree /srv/wt/with space", "HEAD " + sha, "branch refs/remotes/origin/x", "future-attribute yes"},
				[]string{"worktree /srv/wt/b", "HEAD " + sha, "branch refs/heads/b"},
			),
			want: []Entry{
				{Path: mainPath, HEAD: sha, Branch: "main", Main: true},
				{Path: "/srv/wt/with space", HEAD: sha, Branch: "refs/remotes/origin/x"},
				{Path: "/srv/wt/b", HEAD: sha, Branch: "b"},
			},
		},
		{
			name: "last entry without its terminating empty attribute",
			data: []byte("worktree " + mainPath + "\x00HEAD " + sha + "\x00"),
			want: []Entry{{Path: mainPath, HEAD: sha, Main: true}},
		},
		{
			name: "empty output",
			data: nil,
			want: []Entry{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParsePorcelain(tt.data)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParsePorcelainMalformed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data []byte
	}{
		{name: "not NUL-terminated", data: []byte("worktree " + mainPath)},
		{name: "attribute before any worktree", data: []byte("HEAD " + sha + "\x00\x00")},
		{name: "worktree without a path", data: []byte("worktree\x00\x00")},
		{name: "two worktrees in one entry", data: []byte("worktree /a\x00worktree /b\x00\x00")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParsePorcelain(tt.data)
			require.ErrorIs(t, err, ErrMalformedPorcelain)
			assert.Nil(t, got)
		})
	}
}
