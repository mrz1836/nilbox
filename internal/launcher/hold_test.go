package launcher

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/sensitive"
)

var errFakeFiles = errors.New("files unreadable")

// Head commits the hold tests use.
const (
	holdHeadA = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	holdHeadB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// fakeFiles answers file checks with canned results by item ID, recording which kind of
// check was asked for.
type fakeFiles struct {
	checks map[int64]*sensitive.Check
	err    error
	fresh  []int64
	stored []int64
}

func (f *fakeFiles) CheckFiles(_ context.Context, id int64) (*sensitive.Check, error) {
	f.fresh = append(f.fresh, id)

	return f.answer(id)
}

func (f *fakeFiles) StoredFiles(_ context.Context, id int64) (*sensitive.Check, error) {
	f.stored = append(f.stored, id)

	return f.answer(id)
}

// answer is the canned check of an item, or ErrNotChecked for one with none.
func (f *fakeFiles) answer(id int64) (*sensitive.Check, error) {
	if f.err != nil {
		return nil, f.err
	}

	check, ok := f.checks[id]
	if !ok {
		return nil, sensitive.ErrNotChecked
	}

	return check, nil
}

// checkOf evaluates files at head the way the inbox does.
func checkOf(head string, complete bool, paths ...string) *sensitive.Check {
	files := make([]sensitive.File, 0, len(paths))
	for _, p := range paths {
		files = append(files, sensitive.File{Path: p, Change: "modified"})
	}

	r := &sensitive.Result{Head: head, Total: len(files), Files: files, Complete: complete}
	if !complete {
		r.Total += 10
	}

	matcher := sensitive.NewMatcher(sensitive.DefaultConfig())

	return new(matcher.Evaluate(r, head, "https://github.com/acme-org/example-repo/pull/8"))
}

// withFiles gives the harness a file checker answering checks for the outside PR.
func (h *launchHarness) withFiles(check *sensitive.Check) *fakeFiles {
	files := &fakeFiles{checks: map[int64]*sensitive.Check{h.outsidePR: check}}
	h.cfg.Files = files

	return files
}

// trusted reports whether the default profile's agent config marks any clone trusted.
func (h *launchHarness) trusted(t *testing.T) bool {
	t.Helper()

	_, err := os.Stat(filepath.Join(h.home, ".claude.json"))
	if errors.Is(err, os.ErrNotExist) {
		return false
	}

	require.NoError(t, err)

	return true
}

func TestLaunchHoldsBackAPullRequestThatChangesAgentFiles(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	files := h.withFiles(checkOf(holdHeadA, true, ".claude/settings.json", ".github/workflows/ci.yml", "main.go"))

	res, err := h.service().Launch(t.Context(), LaunchRequest{ItemID: h.outsidePR})
	require.NoError(t, err)
	require.NotNil(t, res.Hold)

	assert.Equal(t, []int64{h.outsidePR}, files.fresh, "a launch checks against GitHub as of now")
	assert.Equal(t, HoldAgentFiles, res.Hold.Reason)
	assert.Equal(t, "agentFiles:"+"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", res.Hold.Key)
	assert.Equal(t, holdHeadA, res.Hold.Head)
	require.Len(t, res.Hold.Files, 1, "only agent files hold a launch back")
	assert.Equal(t, ".claude/settings.json", res.Hold.Files[0].Path)
	assert.True(t, res.Hold.AtHead, "the worktree starts at the pull request's head")
	assert.Equal(t, "pr-8", res.Hold.Worktree)
	assert.Equal(t, "~/projects/."+brand.Slug+"/worktrees/example-repo/pr-8", res.Hold.Dir)
	assert.Empty(t, res.ScriptPath)

	assert.Empty(t, h.iterm.opened, "nothing is started")
	assert.Empty(t, h.launchFiles(t), "nothing is written")
	assert.False(t, h.trusted(t), "the clone is not marked trusted")
	assert.Empty(t, h.maker.ensures(), "no worktree is made, so the pull request's files are not checked out")

	// Confirming that hold launches, with the files named in the prompt.
	res, err = h.service().Launch(t.Context(), LaunchRequest{ItemID: h.outsidePR, Confirmed: res.Hold.Key})
	require.NoError(t, err)
	require.Nil(t, res.Hold)
	require.Len(t, h.iterm.opened, 1)
	assert.True(t, h.trusted(t), "a confirmed launch marks the clone trusted, as any launch does")

	prompt := promptFor(t, res.ScriptPath)
	assert.Contains(t, prompt, "This pull request changes sensitive files")
	assert.Contains(t, prompt, ".claude/settings.json (Agent config), .github/workflows/ci.yml (Workflows)")
}

func TestAConfirmationDoesNotCarryOverToANewPush(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	h.withFiles(checkOf(holdHeadA, true, ".mcp.json"))

	res, err := h.service().Launch(t.Context(), LaunchRequest{ItemID: h.outsidePR})
	require.NoError(t, err)
	require.NotNil(t, res.Hold)

	confirmed := res.Hold.Key

	h.withFiles(checkOf(holdHeadB, true, ".mcp.json"))

	res, err = h.service().Launch(t.Context(), LaunchRequest{ItemID: h.outsidePR, Confirmed: confirmed})
	require.NoError(t, err)
	require.NotNil(t, res.Hold, "pushed since the user confirmed: asked again")
	assert.Equal(t, holdHeadB, res.Hold.Head)
	assert.Empty(t, h.iterm.opened)
}

func TestLaunchHoldsBackAPullRequestWhoseFilesWereNotAllChecked(t *testing.T) {
	t.Parallel()

	failed := &sensitive.Check{Status: sensitive.StatusFailed, Error: "GitHub answered 502", Groups: []sensitive.Group{}}

	for name, check := range map[string]*sensitive.Check{
		"failed":              failed,
		"partial":             checkOf(holdHeadA, false, "main.go"),
		"flagged but partial": checkOf(holdHeadA, false, ".github/workflows/ci.yml"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			h := newLaunchHarness(t)
			h.withFiles(check)

			res, err := h.service().Launch(t.Context(), LaunchRequest{ItemID: h.outsidePR})
			require.NoError(t, err)
			require.NotNil(t, res.Hold)

			assert.Equal(t, HoldUnchecked, res.Hold.Reason)
			assert.Empty(t, res.Hold.Files)
			assert.Equal(t, check.Error, res.Hold.Error)
			assert.Empty(t, h.iterm.opened)

			res, err = h.service().Launch(t.Context(), LaunchRequest{ItemID: h.outsidePR, Confirmed: res.Hold.Key})
			require.NoError(t, err)
			assert.Nil(t, res.Hold)
			assert.Len(t, h.iterm.opened, 1)
		})
	}
}

func TestLaunchGoesAheadWithoutAgentFiles(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	h.withFiles(checkOf(holdHeadA, true, ".github/workflows/ci.yml", "go.mod"))

	res, err := h.service().Launch(t.Context(), LaunchRequest{ItemID: h.outsidePR})
	require.NoError(t, err)
	require.Nil(t, res.Hold, "workflows and dependencies are pointed out, not held back")
	require.Len(t, h.iterm.opened, 1)
	assert.Contains(t, promptFor(t, res.ScriptPath),
		".github/workflows/ci.yml (Workflows), go.mod (Dependencies). Review those changes first and most closely.")

	// A clean pull request, and an item whose files are not checked, launch as before.
	h.withFiles(checkOf(holdHeadA, true, "main.go"))

	res, err = h.service().Launch(t.Context(), LaunchRequest{ItemID: h.outsidePR})
	require.NoError(t, err)
	assert.Nil(t, res.Hold)
	assert.NotContains(t, promptFor(t, res.ScriptPath), "sensitive files")

	res, err = h.service().Launch(t.Context(), LaunchRequest{ItemID: h.trustedPR})
	require.NoError(t, err)
	assert.Nil(t, res.Hold)
}

func TestAHeldLaunchOutsideAWorktreeSaysSo(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	h.withFiles(checkOf(holdHeadA, true, "CLAUDE.md"))

	res, err := h.service().Launch(t.Context(), LaunchRequest{ItemID: h.outsidePR, Worktree: new(false)})
	require.NoError(t, err)
	require.NotNil(t, res.Hold)
	assert.False(t, res.Hold.AtHead)
	assert.Empty(t, res.Hold.Worktree)
}

func TestPreviewUsesTheLastCheck(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	files := h.withFiles(checkOf(holdHeadA, true, ".claude/settings.json"))

	preview, err := h.service().Preview(t.Context(), LaunchRequest{ItemID: h.outsidePR})
	require.NoError(t, err)

	assert.Empty(t, files.fresh, "a preview asks GitHub nothing")
	assert.Equal(t, []int64{h.outsidePR}, files.stored)
	assert.Contains(t, preview.Prompt, ".claude/settings.json (Agent config)")

	data, err := NewPromptSource(h.service()).PreviewData(t.Context(), h.outsidePR)
	require.NoError(t, err)
	assert.Equal(t, ".claude/settings.json (Agent config)", data.SensitiveFiles)
}

func TestLaunchFailsWhenTheFilesCannotBeRead(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	files := h.withFiles(checkOf(holdHeadA, true))
	files.err = errFakeFiles

	_, err := h.service().Launch(t.Context(), LaunchRequest{ItemID: h.outsidePR})
	require.ErrorIs(t, err, errFakeFiles)
	assert.Empty(t, h.iterm.opened)

	_, err = h.service().Preview(t.Context(), LaunchRequest{ItemID: h.outsidePR})
	require.ErrorIs(t, err, errFakeFiles)

	_, err = NewPromptSource(h.service()).PreviewData(t.Context(), h.outsidePR)
	require.ErrorIs(t, err, errFakeFiles)
}

func TestALaunchThatCannotBePlannedAsksGitHubNothing(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	files := h.withFiles(checkOf(holdHeadA, true, ".claude/settings.json"))

	_, err := h.service().Launch(t.Context(), LaunchRequest{ItemID: h.orphanPR})
	require.Error(t, err)
	assert.Empty(t, files.fresh)
}
