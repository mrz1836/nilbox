package inbox

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/trust"
)

// Head commits the file tests move between.
const (
	headA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	headB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// from returns a fetched pull request in example-repo opened by someone else.
func from(number int, login, assoc string, crossRepo bool) github.Item {
	it := pr(number, assoc, crossRepo)
	it.AuthorLogin = login

	return it
}

// refKey names a pull request in example-repo the way the fake client keys it.
func refKey(number int) string {
	return fmt.Sprintf("acme-org/example-repo#%d", number)
}

// changed returns a pull request's files at head: a workflow, agent settings, and a
// plain source file.
func changed(head string) *github.ChangedFiles {
	return &github.ChangedFiles{Head: head, Total: 3, Complete: true, Files: []github.ChangedFile{
		{Path: ".github/workflows/ci.yml", Status: "modified"},
		{Path: ".claude/settings.json", Status: "added"},
		{Path: "main.go", Status: "modified"},
	}}
}

// filesHarness lists pull requests with details at headA, and every one's files.
func filesHarness(t *testing.T, items ...github.Item) *harness {
	t.Helper()

	h := newHarness(t)
	h.client.inbox = fetched(items, nil, nil)
	h.client.details = map[string]github.ItemDetails{}
	h.client.files = map[string]*github.ChangedFiles{}

	for _, it := range items {
		d := prDetails()
		d.HeadOID = headA
		h.client.details[it.NodeID] = d
		h.client.files[fmt.Sprintf("acme-org/%s#%d", it.RepoName, it.Number)] = changed(headA)
	}

	return h
}

func TestRefreshChecksAnOutsideContributorsFiles(t *testing.T) {
	t.Parallel()

	h := filesHarness(t, from(1, "outsider", "NONE", false), from(2, "forker", "CONTRIBUTOR", true))

	snap := h.refresh(t)

	assert.ElementsMatch(t, []string{refKey(1), refKey(2)}, h.client.fileReads)

	for _, n := range []int{1, 2} {
		it := itemByNumber(t, snap.ReviewRequested, n)
		require.NotNil(t, it.Files, n)
		assert.Equal(t, sensitive.StatusFlagged, it.Files.Status)
		assert.Equal(t, headA, it.Files.Head)
		assert.Equal(t, 3, it.Files.Checked)
		assert.True(t, it.Files.Complete)
		assert.Equal(t, "2026-01-10T12:00:00Z", it.Files.CheckedAt)
		require.Len(t, it.Files.Groups, 2)
		assert.Equal(t, sensitive.CategoryAgent, it.Files.Groups[0].Category)
		assert.Equal(t, ".claude/settings.json", it.Files.Groups[0].Files[0].Path)
		assert.Equal(t, sensitive.CategoryWorkflows, it.Files.Groups[1].Category)
		assert.True(t, strings.HasPrefix(it.Files.Groups[1].Files[0].URL, it.URL+"/files#diff-"))
	}

	// Nothing moved: the next refresh lists nothing again.
	h.client.fileReads = nil
	h.refresh(t)
	assert.Empty(t, h.client.fileReads)
}

func TestRefreshChecksAgainAfterAPush(t *testing.T) {
	t.Parallel()

	h := filesHarness(t, from(1, "outsider", "NONE", false))
	h.refresh(t)

	// GitHub reports the push before the files are checked again: pending until then.
	d := h.client.details[nodeID(1)]
	d.HeadOID = headB
	h.client.details[nodeID(1)] = d
	h.client.files[refKey(1)] = &github.ChangedFiles{
		Head: headB, Total: 1, Complete: true, Files: []github.ChangedFile{{Path: "README.md", Status: "modified"}},
	}
	h.client.fileReads = nil

	snap := h.refresh(t)

	assert.Equal(t, []string{refKey(1)}, h.client.fileReads)

	it := itemByNumber(t, snap.ReviewRequested, 1)
	require.NotNil(t, it.Files)
	assert.Equal(t, sensitive.StatusClear, it.Files.Status)
	assert.Equal(t, headB, it.Files.Head)
	assert.Empty(t, it.Files.Groups)
}

func TestRefreshOnlyChecksPullRequestsFromOutside(t *testing.T) {
	t.Parallel()

	bot := from(4, "dependabot", "NONE", false)
	bot.AuthorType = trust.TypeBot

	h := filesHarness(t,
		from(1, "teammate", "MEMBER", false),
		from(2, "owner", "OWNER", false),
		from(3, octoUser, "NONE", true), // the signed-in user's own
		bot,
		from(5, "helper", "COLLABORATOR", false),
	)
	h.client.inbox.Assigned = github.SearchResult{IssueCount: 1, Items: []github.Item{issue("example-repo", 6, "NONE")}}

	snap := h.refresh(t)

	assert.Equal(t, []string{refKey(5)}, h.client.fileReads, "collaborators are checked by default")

	for _, n := range []int{1, 2, 3, 4} {
		assert.Nil(t, itemByNumber(t, snap.ReviewRequested, n).Files, n)
	}

	assert.Nil(t, itemByNumber(t, snap.Assigned, 6).Files, "issues have no files")
	assert.NotNil(t, itemByNumber(t, snap.ReviewRequested, 5).Files)

	// Narrowed to outside contributors, a collaborator's pull request is no longer shown
	// as checked, at once.
	h.setPrefs(t, map[string]string{sensitive.PrefScope: string(sensitive.ScopeOutside)})

	snap, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)
	assert.Nil(t, itemByNumber(t, snap.ReviewRequested, 5).Files)
}

func TestSnapshotAppliesTheCategoriesAsTheyAreNow(t *testing.T) {
	t.Parallel()

	h := filesHarness(t, from(1, "outsider", "NONE", false))
	h.refresh(t)

	h.setPrefs(t, map[string]string{
		sensitive.PrefOff:      `["workflows"]`,
		sensitive.PrefPatterns: `["main.go"]`,
	})

	snap, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)

	files := itemByNumber(t, snap.ReviewRequested, 1).Files
	require.NotNil(t, files)
	require.Len(t, files.Groups, 2)
	assert.Equal(t, sensitive.CategoryAgent, files.Groups[0].Category)
	assert.Equal(t, sensitive.CategoryCustom, files.Groups[1].Category)
	assert.Equal(t, "main.go", files.Groups[1].Files[0].Path)
}

func TestAFailedCheckShowsAndIsTriedAgain(t *testing.T) {
	t.Parallel()

	h := filesHarness(t, from(1, "outsider", "NONE", false))
	h.client.filesErr = &github.StatusError{StatusCode: 502, Message: "Bad Gateway"}

	snap := h.refresh(t)

	files := itemByNumber(t, snap.ReviewRequested, 1).Files
	require.NotNil(t, files)
	assert.Equal(t, sensitive.StatusFailed, files.Status)
	assert.Contains(t, files.Error, "502")
	assert.Empty(t, snap.LastError, "a failed check does not fail the refresh")

	h.client.filesErr = nil
	h.client.fileReads = nil
	snap = h.refresh(t)

	assert.Equal(t, []string{refKey(1)}, h.client.fileReads)
	assert.Equal(t, sensitive.StatusFlagged, itemByNumber(t, snap.ReviewRequested, 1).Files.Status)
}

func TestAPullRequestNotCheckedYetIsPending(t *testing.T) {
	t.Parallel()

	var items []github.Item
	for n := 1; n <= maxFileChecks+3; n++ {
		items = append(items, from(n, "outsider", "NONE", false))
	}

	h := filesHarness(t, items...)

	snap := h.refresh(t)
	assert.Len(t, h.client.fileReads, maxFileChecks, "one refresh checks a bounded number")

	pending := 0

	for _, it := range snap.ReviewRequested.Items {
		require.NotNil(t, it.Files)

		if it.Files.Status == sensitive.StatusPending {
			pending++
		}
	}

	assert.Equal(t, 3, pending)

	h.client.fileReads = nil
	h.refresh(t)
	assert.Len(t, h.client.fileReads, 3, "the next refresh checks the rest")
}

func TestChecksLeaveWithTheirItems(t *testing.T) {
	t.Parallel()

	h := filesHarness(t, from(1, "outsider", "NONE", false))
	h.refresh(t)
	assert.Equal(t, int64(1), h.count(t, "item_files"))

	h.seed(t, nil, nil, nil)
	assert.Equal(t, int64(0), h.count(t, "item_files"))
}

func TestCheckFilesReusesAListForTheSameHead(t *testing.T) {
	t.Parallel()

	h := filesHarness(t, from(1, "outsider", "NONE", false))
	id := itemByNumber(t, h.refresh(t).ReviewRequested, 1).ID
	h.client.fileReads, h.client.headReads = nil, nil
	calls := h.events.Count()

	check, err := h.svc.CheckFiles(t.Context(), id)
	require.NoError(t, err)
	require.NotNil(t, check)

	assert.Equal(t, []string{refKey(1)}, h.client.headReads)
	assert.Empty(t, h.client.fileReads, "the head did not move, so the files are not listed again")
	assert.Equal(t, sensitive.StatusFlagged, check.Status)
	assert.Equal(t, headA, check.Head)
	assert.Equal(t, calls+1, h.events.Count())
}

func TestCheckFilesListsAgainAfterAPush(t *testing.T) {
	t.Parallel()

	h := filesHarness(t, from(1, "outsider", "NONE", false))
	id := itemByNumber(t, h.refresh(t).ReviewRequested, 1).ID
	h.client.fileReads = nil
	h.client.files[refKey(1)] = &github.ChangedFiles{
		Head: headB, Total: 1, Complete: true, Files: []github.ChangedFile{{Path: "go.mod", Status: "modified"}},
	}

	check, err := h.svc.CheckFiles(t.Context(), id)
	require.NoError(t, err)

	assert.Equal(t, []string{refKey(1)}, h.client.fileReads)
	assert.Equal(t, headB, check.Head)
	require.Len(t, check.Groups, 1)
	assert.Equal(t, sensitive.CategoryDependencies, check.Groups[0].Category)

	// The new check is stored: the card shows it, and it is not pending.
	stored, err := h.svc.StoredFiles(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, headB, stored.Head)
}

func TestCheckFilesReportsAFailureAsAStatus(t *testing.T) {
	t.Parallel()

	h := filesHarness(t, from(1, "outsider", "NONE", false))
	id := itemByNumber(t, h.refresh(t).ReviewRequested, 1).ID
	h.client.filesErr = &github.StatusError{StatusCode: 500, Message: "Server Error " + fakeToken}

	check, err := h.svc.CheckFiles(t.Context(), id)
	require.NoError(t, err)

	assert.Equal(t, sensitive.StatusFailed, check.Status)
	assert.Contains(t, check.Error, "500")
	assert.NotContains(t, check.Error, fakeToken)

	snap, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)
	assert.Equal(t, sensitive.StatusFailed, itemByNumber(t, snap.ReviewRequested, 1).Files.Status)
}

func TestCheckFilesWithoutAToken(t *testing.T) {
	t.Parallel()

	h := filesHarness(t, from(1, "outsider", "NONE", false))
	id := itemByNumber(t, h.refresh(t).ReviewRequested, 1).ID
	h.tokens.err = errFakeNetwork

	check, err := h.svc.CheckFiles(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, sensitive.StatusFailed, check.Status)
}

func TestCheckFilesIgnoresItemsThatAreNotChecked(t *testing.T) {
	t.Parallel()

	h := filesHarness(t, from(1, "teammate", "MEMBER", false))
	id := itemByNumber(t, h.refresh(t).ReviewRequested, 1).ID

	_, err := h.svc.CheckFiles(t.Context(), id)
	require.ErrorIs(t, err, sensitive.ErrNotChecked)
	assert.Empty(t, h.client.headReads)

	_, err = h.svc.StoredFiles(t.Context(), id)
	require.ErrorIs(t, err, sensitive.ErrNotChecked)

	_, err = h.svc.CheckFiles(t.Context(), 999)
	require.ErrorIs(t, err, item.ErrNotFound)

	_, err = h.svc.StoredFiles(t.Context(), 999)
	require.ErrorIs(t, err, item.ErrNotFound)
}

func TestCheckFilesHonorsCancellation(t *testing.T) {
	t.Parallel()

	h := filesHarness(t, from(1, "outsider", "NONE", false))
	id := itemByNumber(t, h.refresh(t).ReviewRequested, 1).ID

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := h.svc.CheckFiles(ctx, id)
	require.Error(t, err)
}

func TestRefreshItemChecksTheFilesOfAPushedPullRequest(t *testing.T) {
	t.Parallel()

	outside := from(1, "outsider", "NONE", false)
	h := filesHarness(t, outside)
	id := itemByNumber(t, h.refresh(t).ReviewRequested, 1).ID
	h.client.fileReads = nil

	details := prDetails()
	details.HeadOID = headA
	h.client.items = map[string]*github.ItemUpdate{nodeID(1): {Found: true, Item: outside, Details: details}}

	_, err := h.svc.RefreshItem(t.Context(), id)
	require.NoError(t, err)
	assert.Empty(t, h.client.fileReads, "not pushed: nothing to check")

	details.HeadOID = headB
	h.client.items[nodeID(1)].Details = details
	h.client.files[refKey(1)] = &github.ChangedFiles{Head: headB, Total: 0, Complete: true}

	snap, err := h.svc.RefreshItem(t.Context(), id)
	require.NoError(t, err)

	assert.Equal(t, []string{refKey(1)}, h.client.fileReads)
	assert.Equal(t, sensitive.StatusClear, itemByNumber(t, snap.ReviewRequested, 1).Files.Status)
}

func TestFileErrorMessage(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "GitHub did not list the files", fileErrorMessage(nil))
	assert.Equal(t, "GitHub took too long to list the files", fileErrorMessage(context.DeadlineExceeded))
	assert.Equal(t, "a b", fileErrorMessage(fmt.Errorf("a \n b"))) //nolint:err113 // a one-off message

	long := fileErrorMessage(fmt.Errorf("%s", strings.Repeat("x", 400))) //nolint:err113 // a one-off message
	assert.Len(t, []rune(long), maxFileErrorRunes+1)
}
