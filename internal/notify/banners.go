package notify

import (
	"slices"
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/notifyprefs"
	"github.com/mrz1836/nilbox/internal/snooze"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// MaxBanners is how many banners one refresh shows one by one; more are summed up in one.
const MaxBanners = 3

// BannerStatus says whether banners are shown.
type BannerStatus string

// Banner states, as Poster.Status reports them.
const (
	// StatusOn means banners are shown.
	StatusOn BannerStatus = "on"

	// StatusOff means the user has not allowed the app's banners, or turned them off in
	// the system's settings.
	StatusOff BannerStatus = "off"

	// StatusUnavailable means this build cannot show banners: a development run, which has
	// no app bundle to own them, or an OS the shell does not post to yet.
	StatusUnavailable BannerStatus = "unavailable"
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
	// GitHub, for when it is no longer in the inbox, or the page of an entry about no item.
	ItemID int64
	URL    string

	// RunID is the run a click on the banner opens in Scripts → Runs, or 0 for none, and
	// RunStarted when it started, which the window checks first.
	RunID      int64
	RunStarted string
}

// Poster shows banners. The desktop shell implements it over the system's notification
// center.
type Poster interface {
	// Post shows a banner. It never waits on the user: a banner the user has not allowed
	// is not shown, and Post reports why.
	Post(b Banner) error

	// Status reports StatusOn, StatusOff, or StatusUnavailable.
	Status() BannerStatus

	// Authorize asks the user to allow banners, if they have not been asked, and reports
	// whether banners are allowed. It may wait for the user's answer.
	Authorize() (bool, error)
}

// kindTitles are each kind's banner title.
//
//nolint:gochecknoglobals // a fixed table
var kindTitles = map[notifyprefs.Kind]string{
	KindReviewRequested:  "Review requested",
	KindAssigned:         "Assigned to you",
	KindWoke:             "Back from snooze",
	KindReleaseDone:      "Released",
	KindReleasePublished: "Published",
	KindReleaseFailed:    "Release workflow failed",
	KindRunFailed:        "Run failed",
}

// summaryNouns name each kind in a summary banner, singular and plural.
//
//nolint:gochecknoglobals // a fixed table
var summaryNouns = map[notifyprefs.Kind][2]string{
	KindReviewRequested:  {"review request", "review requests"},
	KindAssigned:         {"assignment", "assignments"},
	KindWoke:             {"back from snooze", "back from snooze"},
	KindReleaseDone:      {"release", "releases"},
	KindReleasePublished: {"release published", "releases published"},
	KindReleaseFailed:    {"release workflow failed", "release workflows failed"},
	KindRunFailed:        {"failed run", "failed runs"},
}

// maxTitleRunes bounds an item title in a banner body; the system wraps and cuts the rest.
const maxTitleRunes = 160

// banner is the banner for one recorded notification: its kind as the title, and its item,
// or for an entry about no item its repository, as the subtitle. A click shows the item,
// opens the entry's page, or opens the run it is about.
func banner(n Notification) Banner {
	body := textutil.Truncate(n.Title, maxTitleRunes)
	if n.Detail != "" {
		body += "\n" + textutil.Capitalize(n.Detail)
	}

	subtitle := n.Repo + " #" + strconv.Itoa(n.Number)
	if n.ItemKind == "" {
		subtitle = n.Repo
	}

	return Banner{
		ID:         brand.Slug + "-" + strconv.FormatInt(n.ID, 10),
		Title:      kindTitles[n.Kind],
		Subtitle:   subtitle,
		Body:       body,
		Thread:     string(n.Kind),
		ItemID:     n.ItemID,
		URL:        n.URL,
		RunID:      n.RunID,
		RunStarted: n.RunStarted,
	}
}

// summary is the one banner that stands for many: "5 new in your inbox", with how many of
// each kind, or "5 new notifications" when none is about an item, as a burst of failed runs.
func summary(rows []Notification) Banner {
	counts := make(map[notifyprefs.Kind]int)
	title := " new notifications"

	for _, r := range rows {
		counts[r.Kind]++

		if r.ItemKind != "" {
			title = " new in your inbox"
		}
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
		Title:  strconv.Itoa(len(rows)) + title,
		Body:   strings.Join(parts, ", "),
		Thread: "summary",
	}
}

// Banners returns what to show for recorded notifications: one banner each, or a single
// summary when there are more than MaxBanners. It shows none of the kinds in off.
func Banners(rows []Notification, off []notifyprefs.Kind) []Banner {
	shown := slices.DeleteFunc(slices.Clone(rows), func(r Notification) bool {
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
	case snooze.ReviewNone:
	}

	switch {
	case r.Commits > 0:
		parts = append(parts, textutil.Counted(r.Commits, "new commit", "new commits"))
	case r.ForcePushed:
		parts = append(parts, "force-pushed")
	}

	if r.Comments > 0 {
		parts = append(parts, textutil.Counted(r.Comments, "new comment", "new comments"))
	}

	return strings.Join(parts, ", ")
}
