package notify

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/notifyprefs"
	"github.com/mrz1836/nilbox/internal/snooze"
)

// row is a recorded notification of kind about pull request number in example-repo.
func row(id int64, kind notifyprefs.Kind, number int) Notification {
	return Notification{
		ID: id, Kind: kind, ItemID: id * 10, ItemKind: "pr", Repo: "acme-org/example-repo", Number: number,
		Title: "Fix the login redirect", URL: "https://github.com/acme-org/example-repo/pull/7",
	}
}

func TestABannerPerNotification(t *testing.T) {
	t.Parallel()

	woke := row(2, KindWoke, 8)
	woke.Detail = "approved, 2 new commits"

	got := Banners([]Notification{row(1, KindReviewRequested, 7), woke}, nil)

	require.Len(t, got, 2)
	assert.Equal(t, Banner{
		ID: brand.Slug + "-1", Title: "Review requested", Subtitle: "acme-org/example-repo #7",
		Body: "Fix the login redirect", Thread: string(KindReviewRequested), ItemID: 10,
		URL: "https://github.com/acme-org/example-repo/pull/7",
	}, got[0])
	assert.Equal(t, "Back from snooze", got[1].Title)
	assert.Equal(t, "Fix the login redirect\nApproved, 2 new commits", got[1].Body)
}

func TestABannerForAnItemNoLongerStored(t *testing.T) {
	t.Parallel()

	r := row(1, KindAssigned, 7)
	r.ItemID = 0

	got := Banners([]Notification{r}, nil)
	require.Len(t, got, 1)
	assert.Zero(t, got[0].ItemID)
	assert.Equal(t, "Assigned to you", got[0].Title)
}

func TestABannerForAnEntryAboutNoItem(t *testing.T) {
	t.Parallel()

	got := Banners([]Notification{{
		ID: 3, Kind: KindReleaseDone, Repo: "acme-org/sdk", Title: "v1.4.3 is on GitHub",
		URL: "https://github.com/acme-org/sdk/releases/tag/v1.4.3",
	}}, nil)

	require.Len(t, got, 1)
	assert.Equal(t, Banner{
		ID: brand.Slug + "-3", Title: "Released", Subtitle: "acme-org/sdk", Body: "v1.4.3 is on GitHub",
		Thread: string(KindReleaseDone), URL: "https://github.com/acme-org/sdk/releases/tag/v1.4.3",
	}, got[0], "its repository alone as the subtitle, and no item to show")

	sum := Banners([]Notification{
		row(1, KindReviewRequested, 1), row(2, KindAssigned, 2), row(3, KindWoke, 3),
		{ID: 4, Kind: KindReleaseDone},
		{ID: 5, Kind: KindReleaseDone},
	}, nil)
	require.Len(t, sum, 1)
	assert.Equal(t, "1 review request, 1 assignment, 1 back from snooze, 2 releases", sum[0].Body)
}

func TestBannersForTheReleaseWorkflow(t *testing.T) {
	t.Parallel()

	got := Banners([]Notification{
		{
			ID: 6, Kind: KindReleasePublished, Repo: "acme-org/sdk", Title: "acme-org/sdk v1.4.3 is published",
			URL: "https://github.com/acme-org/sdk/releases/tag/v1.4.3",
		},
		{
			ID: 7, Kind: KindReleaseFailed, Repo: "acme-org/sdk", Title: "The release workflow failed for acme-org/sdk v1.4.4",
			URL: "https://github.com/acme-org/sdk/actions/runs/7",
		},
	}, nil)

	require.Len(t, got, 2)
	assert.Equal(t, Banner{
		ID: brand.Slug + "-6", Title: "Published", Subtitle: "acme-org/sdk", Body: "acme-org/sdk v1.4.3 is published",
		Thread: string(KindReleasePublished), URL: "https://github.com/acme-org/sdk/releases/tag/v1.4.3",
	}, got[0], "it opens the release")
	assert.Equal(t, "Release workflow failed", got[1].Title)
	assert.Equal(t, "https://github.com/acme-org/sdk/actions/runs/7", got[1].URL, "it opens the run")

	off := Banners([]Notification{{ID: 8, Kind: KindReleasePublished}}, []notifyprefs.Kind{KindReleasePublished})
	assert.Empty(t, off, "a kind turned off shows none")

	sum := Banners([]Notification{
		row(1, KindReviewRequested, 1), row(2, KindAssigned, 2), row(3, KindWoke, 3),
		{ID: 4, Kind: KindReleasePublished},
		{ID: 5, Kind: KindReleasePublished},
		{ID: 6, Kind: KindReleaseFailed},
	}, nil)
	require.Len(t, sum, 1)
	assert.Equal(t,
		"1 review request, 1 assignment, 1 back from snooze, 2 releases published, 1 release workflow failed",
		sum[0].Body)
}

func TestABannerForAFailedRun(t *testing.T) {
	t.Parallel()

	got := Banners([]Notification{{
		ID: 6, Kind: KindRunFailed, Title: "A script failed in ~/projects/notes: lint", Detail: "It exited with status 1.",
		RunID: 42,
	}}, nil)

	require.Len(t, got, 1)
	assert.Equal(t, Banner{
		ID: brand.Slug + "-6", Title: "Run failed", Body: "A script failed in ~/projects/notes: lint\nIt exited with status 1.",
		Thread: string(KindRunFailed), RunID: 42,
	}, got[0], "no repository for a folder that isn't one, which the title names; a click opens the run")

	assert.Empty(t, Banners([]Notification{{ID: 6, Kind: KindRunFailed}}, []notifyprefs.Kind{KindRunFailed}),
		"none while its kind is off")

	sum := Banners([]Notification{
		row(1, KindReviewRequested, 1),
		{ID: 2, Kind: KindRunFailed},
		{ID: 3, Kind: KindRunFailed},
		{ID: 4, Kind: KindRunFailed},
	}, nil)
	require.Len(t, sum, 1)
	assert.Equal(t, "1 review request, 3 failed runs", sum[0].Body)
}

func TestManyBannersAreSummedUp(t *testing.T) {
	t.Parallel()

	rows := []Notification{
		row(4, KindReviewRequested, 1), row(5, KindReviewRequested, 2), row(6, KindAssigned, 3),
		row(7, KindWoke, 4), row(8, KindReviewRequested, 5),
	}

	got := Banners(rows, nil)

	require.Len(t, got, 1)
	assert.Equal(t, Banner{
		ID: brand.Slug + "-summary-4", Title: "5 new in your inbox",
		Body: "3 review requests, 1 assignment, 1 back from snooze", Thread: "summary",
	}, got[0])

	// Up to MaxBanners are shown one by one.
	assert.Len(t, Banners(rows[:MaxBanners], nil), MaxBanners)

	// Plural nouns, and kinds that are off are not counted.
	got = Banners(append(rows, row(9, KindAssigned, 6)), []notifyprefs.Kind{KindReviewRequested})
	require.Len(t, got, 3, "three are left once review requests are off")

	got = Banners(append(rows, row(9, KindAssigned, 6), row(10, KindWoke, 7)), []notifyprefs.Kind{KindReviewRequested})
	require.Len(t, got, 1)
	assert.Equal(t, "4 new in your inbox", got[0].Title)
	assert.Equal(t, "2 assignments, 2 back from snooze", got[0].Body)
}

func TestNoBannersWhenEveryKindIsOff(t *testing.T) {
	t.Parallel()

	assert.Empty(t, Banners([]Notification{row(1, KindReviewRequested, 1)}, notifyprefs.Kinds()))
	assert.Empty(t, Banners(nil, nil))
}

func TestALongTitleIsCut(t *testing.T) {
	t.Parallel()

	r := row(1, KindReviewRequested, 7)
	r.Title = strings.Repeat("word ", 60)

	body := Banners([]Notification{r}, nil)[0].Body

	assert.Len(t, []rune(body), maxTitleRunes)
	assert.True(t, strings.HasSuffix(body, "word…"), "trailing space is trimmed before the ellipsis: %q", body)
}

func TestTheTestBannerNamesTheApp(t *testing.T) {
	t.Parallel()

	b := TestBanner()
	assert.Equal(t, brand.Slug+"-test", b.ID)
	assert.Contains(t, b.Body, brand.Name)
}

func TestWakeDetail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		reasons snooze.Reasons
		want    string
	}{
		{snooze.Reasons{}, ""},
		{snooze.Reasons{Review: snooze.ReviewChangesRequested}, "changes requested"},
		{snooze.Reasons{Review: snooze.ReviewApproved, Commits: 1}, "approved, 1 new commit"},
		{snooze.Reasons{Review: snooze.ReviewNew, Comments: 3}, "new review, 3 new comments"},
		{snooze.Reasons{ForcePushed: true, Comments: 1}, "force-pushed, 1 new comment"},
		{snooze.Reasons{Commits: 2, ForcePushed: true}, "2 new commits"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, WakeDetail(tt.reasons))
		})
	}
}
