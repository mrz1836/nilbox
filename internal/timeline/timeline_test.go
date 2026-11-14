package timeline

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
)

// at is the time every converted item in these tests carries.
var at = time.Date(2026, 11, 9, 17, 0, 0, 0, time.UTC) //nolint:gochecknoglobals // a fixed time, read only

func TestEveryTimelineTypeTheSheetsRead(t *testing.T) {
	t.Parallel()

	for typ, want := range map[string]Kind{
		github.TimelineComment: KindComment, github.TimelineCrossReferenced: KindMentioned,
		github.TimelineConnected: KindLinked, github.TimelineDisconnected: KindUnlinked,
		github.TimelineClosed: KindClosed, github.TimelineReopened: KindReopened,
		github.TimelineAssigned: KindAssigned, github.TimelineUnassigned: KindUnassigned,
		github.TimelineLabeled: KindLabeled, github.TimelineUnlabeled: KindUnlabeled,
		github.TimelineRenamed: KindRenamed, github.TimelineMarkedAsDuplicate: KindDuplicate,
		github.TimelineReview: KindReviewed, github.TimelineMerged: KindMerged,
		github.TimelineForcePushed: KindForcePushed, github.TimelineReviewRequested: KindReviewRequested,
		github.TimelineReadyForReview: KindReadyForReview, github.TimelineConvertedToDraft: KindConvertedToDraft,
	} {
		got, ok := KindOf(typ)
		assert.True(t, ok, typ)
		assert.Equal(t, want, got, typ)
	}

	_, ok := KindOf("PinnedEvent")
	assert.False(t, ok)
}

func TestEntriesConvertsEachKind(t *testing.T) {
	t.Parallel()

	pr14 := &github.ItemRef{IsPR: true, Repo: "acme-org/example-repo", Number: 14, State: github.StateMerged, IsDraft: true}

	got := Entries([]github.TimelineItem{
		{
			Type: github.TimelineComment, ID: "IC_1", CreatedAt: at, Actor: "Octo-User", Body: "Hi.", URL: "u",
			Association: "MEMBER", EditedAt: at, Minimized: true, MinimizedReason: "OUTDATED",
		},
		{Type: github.TimelineCrossReferenced, ID: "CRE_1", CreatedAt: at, Actor: "octo-bot", ActorType: "Bot", Ref: pr14, WillClose: true},
		{Type: github.TimelineLabeled, ID: "LE_1", CreatedAt: at, Label: &github.Label{Name: "bug", Color: "#d73a4a"}},
		{Type: github.TimelineRenamed, ID: "RTE_1", CreatedAt: at, PreviousTitle: "Old", CurrentTitle: "New"},
		{
			Type: github.TimelineReview, ID: "PRR_1", CreatedAt: at, Actor: "octo-other", Body: "Looks off.",
			ReviewState: github.ReviewStateChangesRequested, Outdated: true, LineCommentCount: 3,
			LineComments: []github.ReviewLineComment{{
				ID: "PRRC_1", URL: "c", CreatedAt: at, Body: "Here.", Path: "a.go", Line: 12, StartLine: 10,
				Outdated: true, DiffHunk: "@@ -1,2 +1,3 @@\n a\n-b\n+c\n+d\n e",
			}},
		},
		{Type: github.TimelineMerged, ID: "ME_1", CreatedAt: at, Commit: "4444444", Branch: "main", URL: "m"},
		{Type: github.TimelineForcePushed, ID: "HRFPE_1", CreatedAt: at, BeforeCommit: "2222222", AfterCommit: "1111111"},
		{Type: github.TimelineReviewRequested, ID: "RRE_1", CreatedAt: at, Reviewer: "acme-org/maintainers"},
		{Type: "PinnedEvent", ID: "PE_1", CreatedAt: at},
	}, "octo-user")

	require.Len(t, got, 8, "a type the sheets don't read is left out")

	stamp := "2026-11-09T17:00:00Z"
	assert.Equal(t, Entry{
		Kind: KindComment, ID: "IC_1", At: stamp, Actor: "Octo-User", Mine: true, Body: "Hi.", URL: "u",
		Association: "MEMBER", EditedAt: stamp, Minimized: true, MinimizedReason: "OUTDATED", LineComments: []LineComment{},
	}, got[0], "the viewer's own, by their login in any case")
	assert.True(t, got[1].ActorIsBot)
	assert.True(t, got[1].WillClose)
	assert.Equal(t, &Ref{
		Kind: item.KindPR, Repo: "acme-org/example-repo", Number: 14, State: StateMerged, Draft: true,
	}, got[1].Ref)
	assert.Equal(t, &Label{Name: "bug", Color: "#d73a4a"}, got[2].Label)
	assert.Equal(t, "Old", got[3].From)
	assert.Equal(t, "New", got[3].To)

	assert.Equal(t, Entry{
		Kind: KindReviewed, ID: "PRR_1", At: stamp, Actor: "octo-other", Body: "Looks off.",
		ReviewState: ReviewChangesRequested, Outdated: true, MoreLineComments: 2,
		LineComments: []LineComment{{
			ID: "PRRC_1", URL: "c", At: stamp, Path: "a.go", Line: 12, StartLine: 10, Outdated: true, Body: "Here.",
			Hunk: []string{"-b", "+c", "+d", " e"},
		}},
	}, got[4])

	assert.Equal(t, "4444444", got[5].Commit)
	assert.Equal(t, "main", got[5].Branch)
	assert.Equal(t, "2222222", got[6].From, "a force push's commits, before")
	assert.Equal(t, "1111111", got[6].To, "and after")
	assert.Equal(t, "acme-org/maintainers", got[7].Reviewer)

	assert.NotNil(t, Entries(nil, "octo-user"))
}

func TestHunkTailKeepsTheLastLinesAsTheDiffShowsThem(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{" b", "+c"}, HunkTail("@@ -1,2 +1,2 @@\n b\n+c\n"), "the header left out")
	assert.Equal(t, []string{"-b", "+c", "+d", " e"}, HunkTail("@@ -1 +1 @@\n a\n-b\n+c\n+d\n e"), "the last four")
	assert.Equal(t, []string{"+x"}, HunkTail("+x\r\n"), "a hunk with no header, its line ending trimmed")
	assert.Equal(t, []string{}, HunkTail("@@ -0,0 +1 @@"))

	long := HunkTail("+" + strings.Repeat("é", hunkLineBytes))
	require.Len(t, long, 1)
	assert.LessOrEqual(t, len(long[0]), hunkLineBytes, "a minified line is cut")
	assert.True(t, strings.HasPrefix(long[0], "+é"))
}

func TestCutCutsEveryTextAndMarksIt(t *testing.T) {
	t.Parallel()

	body, trimmed := strings.Repeat("d", 50), false
	entries := []Entry{
		{Kind: KindComment, Body: strings.Repeat("c", 50)},
		{Kind: KindReviewed, Body: "short", LineComments: []LineComment{{Body: strings.Repeat("l", 50)}, {Body: "ok"}}},
		{Kind: KindLabeled},
	}

	got := Cut(&body, &trimmed, entries, 10)

	assert.Len(t, body, 10)
	assert.True(t, trimmed)
	assert.Len(t, got[0].Body, 10)
	assert.True(t, got[0].Trimmed)
	assert.False(t, got[1].Trimmed, "a short text is kept whole")
	assert.Len(t, got[1].LineComments[0].Body, 10)
	assert.True(t, got[1].LineComments[0].Trimmed)
	assert.False(t, got[1].LineComments[1].Trimmed)
	assert.Equal(t, KindLabeled, got[2].Kind, "no entry is dropped")

	assert.Len(t, entries[0].Body, 50, "the entries given are left as they were")
	assert.Len(t, entries[1].LineComments[0].Body, 50)

	body, trimmed = "kept", false
	Cut(&body, &trimmed, nil, 10)
	assert.Equal(t, "kept", body)
	assert.False(t, trimmed)
}

func TestCutTextKeepsWholeRunes(t *testing.T) {
	t.Parallel()

	got, cut := cutText("héllo", 2)
	assert.True(t, cut)
	assert.Equal(t, "h", got, "é is two bytes: the cut backs off to its start")

	got, cut = cutText("hello", 5)
	assert.False(t, cut)
	assert.Equal(t, "hello", got)

	got, cut = cutText("hello", 0)
	assert.True(t, cut)
	assert.Empty(t, got)
}

func TestNonNilFillsTheListsAnOlderCacheLacks(t *testing.T) {
	t.Parallel()

	var older []Entry
	require.NoError(t, json.Unmarshal([]byte(`[{"kind":"comment","id":"IC_1"},{"kind":"reviewed","lineComments":[{"id":"c"}]}]`),
		&older))

	got := NonNil(older)
	assert.NotNil(t, got[0].LineComments)
	assert.NotNil(t, got[1].LineComments[0].Hunk)
	assert.Nil(t, older[0].LineComments, "the entries given are left as they were")
	assert.NotNil(t, NonNil(nil))
}

func TestTheWindowsWordsNeverChange(t *testing.T) {
	t.Parallel()

	// The issue sheet's cache, written before this package, holds these names: a rename
	// here would read every cached conversation as empty.
	encoded, err := json.Marshal(Entry{Kind: KindComment, Ref: &Ref{}, Label: &Label{}})
	require.NoError(t, err)

	for _, key := range []string{
		`"kind"`, `"id"`, `"at"`, `"actor"`, `"actorIsBot"`, `"mine"`, `"body"`, `"trimmed"`, `"url"`,
		`"association"`, `"editedAt"`, `"minimized"`, `"minimizedReason"`, `"ref"`, `"willClose"`, `"commit"`,
		`"duplicateOf"`, `"stateReason"`, `"label"`, `"assignee"`, `"from"`, `"to"`, `"repo"`, `"number"`,
		`"title"`, `"state"`, `"draft"`, `"name"`, `"color"`,
	} {
		assert.Contains(t, string(encoded), key+":")
	}
}
