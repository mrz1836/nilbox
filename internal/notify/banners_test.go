package notify

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/notify/model"
	"github.com/mrz1836/nilbox/internal/snooze"
)

// row is a recorded notification of kind about pull request number in example-repo.
func row(id int64, kind string, number int) model.Notification {
	itemID := id * 10

	return model.Notification{
		ID: id, Kind: kind, ItemID: &itemID, ItemKind: "pr", RepoOwner: "acme-org", RepoName: "example-repo",
		Number: number, Title: "Fix the login redirect", URL: "https://github.com/acme-org/example-repo/pull/7",
	}
}

func TestABannerPerNotification(t *testing.T) {
	woke := row(2, KindWoke, 8)
	woke.Detail = "approved, 2 new commits"

	got := Banners([]model.Notification{row(1, KindReviewRequested, 7), woke}, nil)

	require.Len(t, got, 2)
	assert.Equal(t, Banner{
		ID: brand.Slug + "-1", Title: "Review requested", Subtitle: "acme-org/example-repo #7",
		Body: "Fix the login redirect", Thread: KindReviewRequested, ItemID: 10,
		URL: "https://github.com/acme-org/example-repo/pull/7",
	}, got[0])
	assert.Equal(t, "Back from snooze", got[1].Title)
	assert.Equal(t, "Fix the login redirect\nApproved, 2 new commits", got[1].Body)
}

func TestABannerForAnItemNoLongerStored(t *testing.T) {
	r := row(1, KindAssigned, 7)
	r.ItemID = nil

	got := Banners([]model.Notification{r}, nil)
	require.Len(t, got, 1)
	assert.Zero(t, got[0].ItemID)
	assert.Equal(t, "Assigned to you", got[0].Title)
}

func TestManyBannersAreSummedUp(t *testing.T) {
	rows := []model.Notification{
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
	got = Banners(append(rows, row(9, KindAssigned, 6)), []string{KindReviewRequested})
	require.Len(t, got, 3, "three are left once review requests are off")

	got = Banners(append(rows, row(9, KindAssigned, 6), row(10, KindWoke, 7)), []string{KindReviewRequested})
	require.Len(t, got, 1)
	assert.Equal(t, "4 new in your inbox", got[0].Title)
	assert.Equal(t, "2 assignments, 2 back from snooze", got[0].Body)
}

func TestNoBannersWhenEveryKindIsOff(t *testing.T) {
	assert.Empty(t, Banners([]model.Notification{row(1, KindReviewRequested, 1)}, Kinds()))
	assert.Empty(t, Banners(nil, nil))
}

func TestALongTitleIsCut(t *testing.T) {
	r := row(1, KindReviewRequested, 7)
	r.Title = strings.Repeat("word ", 60)

	body := Banners([]model.Notification{r}, nil)[0].Body

	assert.Len(t, []rune(body), maxTitleRunes)
	assert.True(t, strings.HasSuffix(body, "word…"), "trailing space is trimmed before the ellipsis: %q", body)
}

func TestTheTestBannerNamesTheApp(t *testing.T) {
	b := TestBanner()
	assert.Equal(t, brand.Slug+"-test", b.ID)
	assert.Contains(t, b.Body, brand.Name)
}

func TestWakeDetail(t *testing.T) {
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
			assert.Equal(t, tt.want, WakeDetail(tt.reasons))
		})
	}
}

func TestBannersOffRoundTrip(t *testing.T) {
	assert.Equal(t, []string{}, DecodeBannersOff(nil), "nothing stored: every kind is on")
	assert.Equal(t, []string{}, DecodeBannersOff(map[string]string{PrefBannersOff: "not json"}))
	assert.Equal(t, []string{KindReviewRequested, KindWoke},
		DecodeBannersOff(map[string]string{PrefBannersOff: `["woke","mentions","review_requested"]`}),
		"unknown kinds are dropped and the rest kept in order")

	off, err := SetBanner(nil, KindWoke, false)
	require.NoError(t, err)
	assert.Equal(t, []string{KindWoke}, off)

	off, err = SetBanner(off, KindReviewRequested, false)
	require.NoError(t, err)
	assert.Equal(t, []string{KindReviewRequested, KindWoke}, off)

	off, err = SetBanner(off, KindWoke, true)
	require.NoError(t, err)
	assert.Equal(t, []string{KindReviewRequested}, off)

	_, err = SetBanner(off, "mentions", true)
	require.ErrorIs(t, err, ErrUnknownKind)

	assert.JSONEq(t, `["review_requested"]`, EncodeBannersOff(off))
	assert.JSONEq(t, `[]`, EncodeBannersOff(nil))
	assert.Equal(t, off, DecodeBannersOff(map[string]string{PrefBannersOff: EncodeBannersOff(off)}))
}

func TestCapitalize(t *testing.T) {
	assert.Empty(t, capitalize(""))
	assert.Equal(t, "Éclair", capitalize("éclair"))
}
