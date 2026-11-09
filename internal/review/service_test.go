package review

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/bump"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/sensitive"
)

func TestOpenBeforeAnyReadShowsTheCard(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	r, err := h.svc.Open(t.Context(), pullID)
	require.NoError(t, err)

	assert.True(t, r.Stale, "never read from GitHub")
	assert.Empty(t, r.FetchedAt)
	assert.Equal(t, "acme-org/example-repo", r.Repo)
	assert.Equal(t, 7, r.Number)
	assert.Equal(t, headA, r.Head)
	assert.Equal(t, []File{}, r.Files)
	assert.Equal(t, []Check{}, r.Checks)
	assert.Equal(t, []string{"enhancement"}, r.Labels)
	assert.Equal(t, ReadyUnknown, r.Readiness.State)
	assert.Equal(t, ActionApprove, r.Primary, "nothing merges before GitHub says it can")
	assert.Zero(t, h.github.called("review"), "Open asks GitHub nothing")
}

func TestRefreshReadsThePullRequestAndItsFilesOnce(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	r := h.refreshed(t)

	assert.Equal(t, 1, h.github.called("review"))
	assert.Equal(t, 1, h.github.called("diff"), "the first read lists the files")
	assert.False(t, r.Stale)
	assert.Equal(t, "2026-11-09T17:00:00Z", r.FetchedAt)
	assert.Equal(t, "main", r.BaseRef)
	assert.Contains(t, r.Body, "Adds a cache.")
	assert.Equal(t, ReadyMerge, r.Readiness.State)
	assert.Equal(t, ActionApproveMerge, r.Primary)
	assert.Equal(t, MethodSquash, r.DefaultMethod)
	assert.Equal(t, []MergeMethod{MethodMerge, MethodSquash}, r.MergeMethods)

	require.Len(t, r.Files, 3)
	assert.Equal(t, File{
		Path: "cache.go", Status: "modified", Additions: 2, Deletions: 1, Patch: "@@ -1,2 +1,3 @@\n line\n-old\n+new\n+added",
	}, r.Files[0])
	assert.Equal(t, "Workflows", r.Files[1].Category)
	assert.Equal(t, sensitive.LevelDanger, r.Files[1].Level)
	assert.Equal(t, PatchNone, r.Files[2].PatchState, "GitHub shows no diff for an image")
	assert.Equal(t, Change{Kind: ChangeCode, Label: "Code", Areas: []string{".", ".github"}}, r.Change)

	merge, ok := Decide(factsOf(storedPull(), mustCache(t, h), r), PrefRepo).offer(ActionMerge)
	require.True(t, ok)
	assert.Equal(t, []string{"It changes sensitive files: Workflows."}, merge.Warnings)

	require.Len(t, r.Reviews, 1)
	assert.True(t, r.Reviews[0].Outdated, "left on another head")
	assert.Equal(t, "2026-11-09T17:00:00Z", r.Reviews[0].SubmittedAt)

	require.Len(t, h.inbox.read, 1)
	files := h.inbox.read[0].Files
	require.NotNil(t, files, "the files listed serve the inbox's sensitive check")
	assert.Equal(t, headA, files.Head)
	assert.Len(t, files.Files, 3)
}

// mustCache reads the harness's cache of #7.
func mustCache(t *testing.T, h *harness) *cached {
	t.Helper()

	c, err := h.svc.cache.load(t.Context(), pullID, openPatchBytes)
	require.NoError(t, err)
	require.NotNil(t, c)

	return c
}

func TestRefreshCostsWhatItMust(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.refreshed(t)

	// Read again at once: nothing is asked.
	h.refreshed(t)
	assert.Equal(t, 1, h.github.called("review"))

	// Later, with nothing pushed: one GraphQL read, and the cached files serve the check.
	h.clock.Advance(time.Minute)
	r := h.refreshed(t)
	assert.Equal(t, 2, h.github.called("review"))
	assert.Equal(t, 1, h.github.called("diff"), "the files at that head are cached")
	require.Len(t, h.inbox.read, 2)
	require.NotNil(t, h.inbox.read[1].Files)
	assert.Equal(t, headA, h.inbox.read[1].Files.Head)
	assert.Len(t, r.Files, 3)

	// Pushed since: the files are listed again.
	h.clock.Advance(time.Minute)
	h.github.review, h.github.diff = pullReview(headB, github.MergeStateClean), pullDiff(headB)
	r = h.refreshed(t)
	assert.Equal(t, 3, h.github.called("review"))
	assert.Equal(t, 2, h.github.called("diff"))
	assert.Equal(t, headB, r.Head)
	assert.True(t, r.Stale, "the inbox still knows the old head until its card is stored")
}

func TestRefreshReadsAgainWhileGitHubWorksOutTheMerge(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.github.review = pullReview(headA, github.MergeStateUnknown)
	r := h.refreshed(t)
	assert.Equal(t, ReadyUnknown, r.Readiness.State)

	// At once: GitHub had no answer, so the sheet asks again.
	h.github.review = pullReview(headA, github.MergeStateClean)
	r = h.refreshed(t)
	assert.Equal(t, 2, h.github.called("review"))
	assert.Equal(t, ReadyMerge, r.Readiness.State)

	// With the answer read, a refresh at once asks nothing.
	h.refreshed(t)
	assert.Equal(t, 2, h.github.called("review"))
}

func TestRefreshReadsAgainWhenPushedMidway(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.github.reviews = []*github.PullReview{pullReview(headA, github.MergeStateClean), pullReview(headB, github.MergeStateBlocked)}
	h.github.diff = pullDiff(headB)

	r := h.refreshed(t)
	assert.Equal(t, 2, h.github.called("review"), "the context is read again for the head the files belong to")
	assert.Equal(t, headB, r.Head)
}

func TestRefreshOfAPullRequestNoLongerOpenDropsItsCache(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.refreshed(t)

	h.clock.Advance(time.Minute)

	merged := pullReview(headA, github.MergeStateClean)
	merged.Update.Item.State = github.StateMerged
	h.github.review = merged

	r := h.refreshed(t)
	assert.True(t, r.Stale)
	assert.Empty(t, r.Files, "the cache of a merged pull request is dropped")
	assert.Equal(t, 1, h.github.called("diff"))
}

func TestRefreshFailures(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.github.review = nil

	_, err := h.svc.Refresh(t.Context(), pullID)
	require.ErrorIs(t, err, errNotCalled)

	h.github.review, h.github.diff = pullReview(headA, github.MergeStateClean), nil

	_, err = h.svc.Refresh(t.Context(), pullID)
	require.ErrorIs(t, err, errNotCalled, "the files could not be listed")

	h.inbox.pull.Kind = item.KindIssue

	_, err = h.svc.Refresh(t.Context(), pullID)
	require.ErrorIs(t, err, ErrNotPull)

	h.inbox.pulled = errFakeRead

	_, err = h.svc.Open(t.Context(), pullID)
	require.ErrorIs(t, err, errFakeRead)
}

func TestOpenFollowsTheSettings(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.refreshed(t)

	off := sensitive.DefaultConfig()
	off.Off = []string{sensitive.CategoryWorkflows}

	r, err := h.service(fakeSettings{pref: PrefMerge, cfg: off}, fakeReplies{}).Open(t.Context(), pullID)
	require.NoError(t, err)
	assert.Empty(t, r.Files[1].Category, "a category turned off")
	assert.Equal(t, MethodMerge, r.DefaultMethod)

	_, err = h.service(fakeSettings{err: errFakeRead}, fakeReplies{}).Open(t.Context(), pullID)
	require.ErrorIs(t, err, errFakeRead)
}

func TestABotsDependencyUpdate(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.inbox.pull.AuthorLogin, h.inbox.pull.AuthorType = "dependabot", "Bot"
	h.inbox.pull.Title = "build(deps): bump golang.org/x/net from 0.30.0 to 0.31.0"

	r, err := h.svc.Open(t.Context(), pullID)
	require.NoError(t, err)

	require.NotNil(t, r.Bump)
	assert.Equal(t, bump.Update{
		Name: "golang.org/x/net", From: "0.30.0", To: "0.31.0", Count: 1, Level: bump.LevelMinor,
	}, *r.Bump)
	assert.True(t, r.IsBot)
	assert.Equal(t, ChangeDependencies, r.Change.Kind)
}

func TestPatchesReadsWhatOpenLeftOut(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	big := strings.Repeat("+x\n", openPatchBytes/6)
	h.github.diff.Files[0].Patch = "@@ -0,0 +1 @@\n" + big
	h.github.diff.Files[1].Patch = "@@ -1 +1 @@\n" + big
	h.refreshed(t)

	r, err := h.svc.Open(t.Context(), pullID)
	require.NoError(t, err)
	assert.Equal(t, PatchShown, r.Files[0].PatchState)
	assert.Equal(t, PatchOmitted, r.Files[1].PatchState, "past what Open returns at once")
	assert.Empty(t, r.Files[1].Patch)

	got, err := h.svc.Patches(t.Context(), pullID, headA, []string{".github/workflows/ci.yml", "logo.png", "missing.go"})
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, h.github.diff.Files[1].Patch, got[0].Patch)
	assert.Equal(t, FilePatch{Path: "logo.png", PatchState: PatchNone}, got[1])

	_, err = h.svc.Patches(t.Context(), pullID, headB, []string{"cache.go"})
	require.ErrorIs(t, err, github.ErrPushedSince, "a head the cache no longer holds")
}

func TestRepliesComeFromTheLibrary(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	svc := h.service(fakeSettings{}, fakeReplies{
		list: ReplyList{Replies: []Reply{{ID: 5, Nickname: "LGTM", Action: ReplyApprove}}, Default: 5, DefaultBots: 5},
		fill: "Approved, @octo-other.",
	})

	list, err := svc.Replies(t.Context(), ReplyApprove)
	require.NoError(t, err)
	assert.Equal(t, int64(5), list.Default)
	assert.Len(t, list.Replies, 1)

	text, err := svc.FillReply(t.Context(), pullID, 5)
	require.NoError(t, err)
	assert.Equal(t, "Approved, @octo-other.", text)

	empty, err := h.svc.Replies(t.Context(), ReplyComment)
	require.NoError(t, err)
	assert.Equal(t, []Reply{}, empty.Replies, "never nil")

	_, err = h.service(fakeSettings{}, fakeReplies{err: errFakeRead}).Replies(t.Context(), ReplyComment)
	require.ErrorIs(t, err, errFakeRead)
}

func TestChangeKinds(t *testing.T) {
	t.Parallel()

	files := func(paths ...string) []File {
		out := make([]File, 0, len(paths))
		for _, p := range paths {
			out = append(out, File{Path: p, Additions: 1})
		}

		return out
	}

	assert.Equal(t, ChangeDocs, changeOf(files("README.md", "docs/guide.png", "LICENSE"), false).Kind)
	assert.Equal(t, ChangeTests, changeOf(files("cache_test.go", "web/a.test.ts", "testdata/x.json"), false).Kind)
	assert.Equal(t, ChangeCI, changeOf(files(".github/workflows/ci.yml", ".github/actions/x/action.yml"), false).Kind)
	assert.Equal(t, ChangeDependencies, changeOf(files("go.mod", "go.sum", "vendor/x/y.go"), false).Kind)
	assert.Equal(t, ChangeCode, changeOf(nil, false).Kind)
	assert.Equal(t, "CI and workflows", changeLabel(ChangeCI))
	assert.Equal(t, "Docs only", changeLabel(ChangeDocs))
	assert.Equal(t, "Tests only", changeLabel(ChangeTests))
	assert.Equal(t, "Code", changeLabel("other"))

	c := changeOf([]File{
		{Path: "a/x.go", Additions: 10},
		{Path: "b/y.go", Additions: 50},
		{Path: "c/z.go", Additions: 5},
		{Path: "d/w.go", Additions: 1},
		{Path: "top.go"},
	}, false)
	assert.Equal(t, []string{"b", "a", "c"}, c.Areas, "the three most changed folders")
}

func TestCacheStaysWithinItsCaps(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ctx := t.Context()
	seedMore(t, h, maxCachedPulls+1)

	small := &github.PullDiff{Head: headA, Total: 1, Complete: true, Files: []github.PullFile{
		{Path: "a.go", Patch: "@@ -1 +1 @@\n-a\n+b", HasPatch: true},
	}}

	for id := int64(2); id <= maxCachedPulls+2; id++ {
		h.clock.Advance(time.Second)
		require.NoError(t, h.svc.cache.save(ctx, id, pullContext{Head: headA}, small, h.clock.Now().UTC()))
	}

	assert.Equal(t, maxCachedPulls, countRows(t, h, "review_diffs"), "the least recently used past the cap is dropped")

	gone, err := h.svc.cache.load(ctx, 2, 0)
	require.NoError(t, err)
	assert.Nil(t, gone, "the oldest went first")

	// A cache unused for two weeks is dropped by the next save.
	h.clock.Advance(unusedFor + time.Hour)
	require.NoError(t, h.svc.cache.save(ctx, pullID, pullContext{Head: headA}, small, h.clock.Now().UTC()))
	assert.Equal(t, 1, countRows(t, h, "review_diffs"))
}

func TestCacheKeepsAPullRequestsDiffsWithinItsCap(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	big := strings.Repeat("x", maxPullPatchBytes/2+1)
	diff := &github.PullDiff{Head: headA, Total: 3, Complete: true, Files: []github.PullFile{
		{Path: "a.go", Patch: big, HasPatch: true},
		{Path: "b.go", Patch: big, HasPatch: true},
		{Path: "c.go", Patch: "@@ -1 +1 @@\n-a\n+b", HasPatch: true},
	}}

	require.NoError(t, h.svc.cache.save(t.Context(), pullID, pullContext{Head: headA}, diff, testNow))

	c := mustCache(t, h)
	assert.Empty(t, c.files[0].state)
	assert.Equal(t, storedCapped, c.files[1].state, "past the cap")
	assert.Empty(t, c.files[2].state, "a small one after it still fits")
}

// seedMore stores items 2 to n+1 beside #7.
func seedMore(t *testing.T, h *harness, n int) {
	t.Helper()

	for i := 2; i <= n+1; i++ {
		exec(t, h.db, `INSERT INTO items (id, account_id, repo_id, kind, number, node_id, item_created_at, item_updated_at,
			fetched_at) VALUES (?, 1, 1, 'pr', ?, '', ?, ?, ?)`, i, i, testNow, testNow, testNow)
	}
}

// countRows counts a table's rows.
func countRows(t *testing.T, h *harness, table string) int {
	t.Helper()

	var n int
	require.NoError(t, h.db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table).Scan(&n))

	return n
}
