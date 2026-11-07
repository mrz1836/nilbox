package notify

import (
	"slices"
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/notify/model"
	"github.com/mrz1836/nilbox/internal/notifyprefs"
	"github.com/mrz1836/nilbox/internal/snooze"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// MaxBanners is how many banners one refresh shows one by one; more are summed up in one.
const MaxBanners = 3

// Banner states, as Poster.Status reports them.
const (
	// StatusOn means banners are shown.
	StatusOn = "on"

	// StatusOff means the user has not allowed the app's banners, or turned them off in
	// the system's settings.
	StatusOff = "off"

	// StatusUnavailable means this build cannot show banners: a development run, which has
	// no app bundle to own them, or an OS the shell does not post to yet.
	StatusUnavailable = "unavailable"
)

// Banner is one native notification.
type Banner struct {
	// ID identifies the banner to the system; posting the same ID again replaces it.
	ID string

	Title    string
	Subtitle string
	Body     string

	// Thread groups the banners of one kind in the system's notification list.
	Thread string

	// ItemID is the item a click on the banner shows, or 0 for none. URL is the item on
	// GitHub, for when it is no longer in the inbox.
	ItemID int64
	URL    string
}

// Poster shows banners. The desktop shell implements it over the system's notification
// center.
type Poster interface {
	// Post shows a banner. It never waits on the user: a banner the user has not allowed
	// is not shown, and Post reports why.
	Post(b Banner) error

	// Status reports StatusOn, StatusOff, or StatusUnavailable.
	Status() string

	// Authorize asks the user to allow banners, if they have not been asked, and reports
	// whether banners are allowed. It may wait for the user's answer.
	Authorize() (bool, error)
}

// kindTitles are each kind's banner title.
//
//nolint:gochecknoglobals // a fixed table
var kindTitles = map[string]string{
	KindReviewRequested: "Review requested",
	KindAssigned:        "Assigned to you",
	KindWoke:            "Back from snooze",
}

// summaryNouns name each kind in a summary banner, singular and plural.
//
//nolint:gochecknoglobals // a fixed table
var summaryNouns = map[string][2]string{
	KindReviewRequested: {"review request", "review requests"},
	KindAssigned:        {"assignment", "assignments"},
	KindWoke:            {"back from snooze", "back from snooze"},
}

// maxTitleRunes bounds an item title in a banner body; the system wraps and cuts the rest.
const maxTitleRunes = 160

// banner is the banner for one recorded notification.
func banner(n model.Notification) Banner {
	body := textutil.Truncate(n.Title, maxTitleRunes)
	if n.Detail != "" {
		body += "\n" + textutil.Capitalize(n.Detail)
	}

	itemID := int64(0)
	if n.ItemID != nil {
		itemID = *n.ItemID
	}

	return Banner{
		ID:       brand.Slug + "-" + strconv.FormatInt(n.ID, 10),
		Title:    kindTitles[n.Kind],
		Subtitle: repoName(n.RepoOwner, n.RepoName) + " #" + strconv.Itoa(n.Number),
		Body:     body,
		Thread:   n.Kind,
		ItemID:   itemID,
		URL:      n.URL,
	}
}

// summary is the one banner that stands for many: "5 new in your inbox", with how many of
// each kind.
func summary(rows []model.Notification) Banner {
	counts := make(map[string]int)
	for _, r := range rows {
		counts[r.Kind]++
	}

	var parts []string

	for _, kind := range notifyprefs.Kinds() {
		if n := counts[kind]; n > 0 {
			noun := summaryNouns[kind]
			if n == 1 {
				parts = append(parts, "1 "+noun[0])
			} else {
				parts = append(parts, strconv.Itoa(n)+" "+noun[1])
			}
		}
	}

	return Banner{
		ID:     brand.Slug + "-summary-" + strconv.FormatInt(rows[0].ID, 10),
		Title:  strconv.Itoa(len(rows)) + " new in your inbox",
		Body:   strings.Join(parts, ", "),
		Thread: "summary",
	}
}

// Banners returns what to show for recorded notifications: one banner each, or a single
// summary when there are more than MaxBanners. It shows none of the kinds in off.
func Banners(rows []model.Notification, off []string) []Banner {
	shown := slices.DeleteFunc(slices.Clone(rows), func(r model.Notification) bool {
		return slices.Contains(off, r.Kind)
	})

	switch {
	case len(shown) == 0:
		return nil
	case len(shown) > MaxBanners:
		return []Banner{summary(shown)}
	}

	out := make([]Banner, 0, len(shown))
	for _, r := range shown {
		out = append(out, banner(r))
	}

	return out
}

// TestBanner is the banner Service.SendTest shows.
func TestBanner() Banner {
	return Banner{
		ID:     brand.Slug + "-test",
		Title:  "Notifications are on",
		Body:   brand.Name + " will tell you here when something new needs you.",
		Thread: "test",
	}
}

// WakeDetail says what ended a snooze early, most decisive first, as the inbox card does:
// "changes requested, 2 new commits, 3 new comments". It is "" when nothing did.
func WakeDetail(r snooze.Reasons) string {
	var parts []string

	switch r.Review {
	case snooze.ReviewChangesRequested:
		parts = append(parts, "changes requested")
	case snooze.ReviewApproved:
		parts = append(parts, "approved")
	case snooze.ReviewNew:
		parts = append(parts, "new review")
	}

	switch {
	case r.Commits > 0:
		parts = append(parts, counted(r.Commits, "new commit", "new commits"))
	case r.ForcePushed:
		parts = append(parts, "force-pushed")
	}

	if r.Comments > 0 {
		parts = append(parts, counted(r.Comments, "new comment", "new comments"))
	}

	return strings.Join(parts, ", ")
}

// counted is "1 thing" or "N things".
func counted(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}

	return strconv.Itoa(n) + " " + many
}
