package worktrees

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// guardOf is the guard core gives the worktrees, over the env's isolated git.
func (e *env) guardOf() *gitread.Git {
	return gitread.New(gitread.Config{Env: func() []string { return testutil.GitEnv(e.home) }})
}

// plantPrograms sets, in the clone at repo's own config, a clean filter every file is read
// through and hooks for the events a status that writes the index, a removal, and a
// branch's deletion fire, each a command that writes a file in ran, and changes the stat
// data of every file in each of worktrees, so a status reads them all again.
func plantPrograms(t *testing.T, repo, ran string, worktrees ...string) {
	t.Helper()

	info := filepath.Join(repo, ".git", "info")
	require.NoError(t, os.MkdirAll(info, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(info, "attributes"), []byte("* filter=evil\n"), 0o600))
	testutil.RunGit(t, repo, "config", "filter.evil.clean", "touch '"+filepath.Join(ran, "clean")+"'; cat")

	for _, event := range []string{"post-index-change", "reference-transaction", "post-checkout"} {
		testutil.RunGit(t, repo, "config", "hook.evil-"+event+".command", "touch '"+filepath.Join(ran, event)+"'")
		testutil.RunGit(t, repo, "config", "hook.evil-"+event+".event", event)
	}

	later := time.Now().Add(time.Hour)

	for _, wt := range append([]string{repo}, worktrees...) {
		entries, err := os.ReadDir(wt)
		require.NoError(t, err)

		for _, entry := range entries {
			if !entry.IsDir() {
				require.NoError(t, os.Chtimes(filepath.Join(wt, entry.Name()), later, later))
			}
		}
	}
}

// ranAny returns the files the planted programs wrote in ran.
func ranAny(t *testing.T, ran string) []string {
	t.Helper()

	entries, err := os.ReadDir(ran)
	require.NoError(t, err)

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}

	return names
}

// TestTheSweepRunsNoProgramTheClonesOwnConfigNames sweeps a clone whose own config names a
// clean filter for every file and hooks for the events a status, a removal, and a
// branch's deletion fire, with every worktree's files' stat data changed, as the timer's
// sweep would with no click: under gitread's guards, nothing of the clone's runs, nothing
// is removed, and each worktree says why where the window shows it. Without the guards, the
// sweep's own status runs the filter.
func TestTheSweepRunsNoProgramTheClonesOwnConfigNames(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newSweepFixture(t, e)
	ran := t.TempDir()

	plantPrograms(t, f.repo, ran, f.launchedPR, f.commitLinked, f.nameOnly, f.unlaunchedIssue, f.launchedIssue,
		f.changed, f.open)

	svc := e.linked(linkDeps{gh: f.gh, launches: f.launches, auto: true, guard: e.guardOf()})

	report := svc.Sweep(t.Context())
	assert.Empty(t, report.Cleaned, "nothing is removed from a clone whose status can't be read")
	assert.Empty(t, ranAny(t, ran), "the sweep ran none of the clone's programs")

	snap, err := svc.List(t.Context())
	require.NoError(t, err)

	var seen int

	for _, r := range snap.Repos {
		for _, wt := range r.Worktrees {
			if !strings.HasPrefix(wt.Path, resolvedPath(f.repo)) && !strings.HasPrefix(wt.Path, f.repo) {
				continue
			}

			seen++

			assert.Contains(t, wt.Error, "This clone's own git config sets filter.evil.clean,", wt.Path)
		}
	}

	assert.Positive(t, seen)

	_, err = svc.Remove(t.Context(), f.repo, f.open, nil)
	require.Error(t, err)
	assert.Contains(t, uierr.Message(err), "This clone's own git config sets filter.evil.clean,")
	assert.DirExists(t, f.open, "a removal is refused too")
	assert.Empty(t, ranAny(t, ran))

	unguarded := e.linked(linkDeps{gh: f.gh, launches: f.launches})
	_, err = unguarded.List(t.Context())
	require.NoError(t, err)
	assert.Contains(t, ranAny(t, ran), "clean", "the clone is set up so that a status runs its filter")

	testutil.RunGit(t, f.open, "status", "--porcelain")
	assert.Contains(t, ranAny(t, ran), "post-index-change", "and so that a plain status runs its hook")
}

// TestTheSweepReadsAPlainClonesStatusWithItsOwnLFSOff sweeps a clone whose own config holds
// only what git and Git LFS write: it is read and swept as before.
func TestTheSweepReadsAPlainClonesStatusWithItsOwnLFSOff(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newSweepFixture(t, e)

	testutil.RunGit(t, f.repo, "config", "filter.lfs.process", "git-lfs filter-process")
	testutil.RunGit(t, f.repo, "config", "core.hooksPath", ".husky/_")

	svc := e.linked(linkDeps{gh: f.gh, launches: f.launches, auto: true, guard: e.guardOf()})

	report := svc.Sweep(t.Context())
	assert.Empty(t, report.Error)
	assert.Len(t, report.Cleaned, 3, "the same three as without the guards")
}

// TestEnsureChecksOutTheCommitItChecked plants, in the clone, a replace ref that has git
// read another commit, with other files, in place of a pull request's head: the launch
// checks the head's ID, which a replace ref leaves as it is, and its worktree holds the
// head's own files. A plain `git worktree add` of the same commit writes the other's.
func TestEnsureChecksOutTheCommitItChecked(t *testing.T) {
	t.Parallel()

	r := newGuardRig(t)

	work := filepath.Join(r.e.home, "evil-work")
	testutil.RunGit(t, r.e.home, "clone", "-q", r.f.bare, work)
	require.NoError(t, os.WriteFile(filepath.Join(work, "evil.txt"), []byte("evil\n"), 0o600))
	testutil.RunGit(t, work, "add", "evil.txt")
	testutil.RunGit(t, work, "commit", "-q", "-m", "evil")
	evil := strings.TrimSpace(testutil.RunGit(t, work, "rev-parse", "HEAD"))

	testutil.RunGit(t, r.f.clone, "fetch", "-q", work, "HEAD:refs/heads/evil-side")
	testutil.RunGit(t, r.f.clone, "replace", r.f.prHead, evil)

	req := r.f.request(item.KindPR, 7)
	req.Head = r.f.prHead

	pr, err := r.maker().Ensure(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, r.f.prHead, pr.Head)
	assert.FileExists(t, filepath.Join(pr.Path, "fix.txt"), "the head's own files")
	assert.NoFileExists(t, filepath.Join(pr.Path, "evil.txt"))

	plain := filepath.Join(r.e.home, "plain-wt")
	testutil.RunGit(t, r.f.clone, "worktree", "add", "-q", "--detach", plain, r.f.prHead)
	assert.FileExists(t, filepath.Join(plain, "evil.txt"), "a plain checkout of the same commit writes the replacement's")
}

func TestNoWorktreeCommandIsAMergeOrDiffTool(t *testing.T) {
	t.Parallel()

	for _, verb := range []string{"mergetool", "difftool"} {
		assert.False(t, allowed([]string{verb}), verb)
		assert.False(t, allowed([]string{verb, "--tool=x"}), verb)
	}
}
