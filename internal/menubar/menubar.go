// Package menubar works out what the app's menu bar icon shows for the stored inbox and
// the agent sessions: how many items need the user, beside the icon, and in its menu the
// sessions waiting for the user first, then a short list of the items, by list. The
// desktop shell draws the result with native menu items; this package decides what they
// say, so it builds and is tested on any OS.
package menubar

import (
	"strconv"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/sessions"
	"github.com/mrz1836/nilbox/internal/textutil"
)

const (
	// PerSection is how many items each list shows in the menu before "N more".
	PerSection = 5

	// MaxCount is the highest count the label spells out; more show as "99+".
	MaxCount = 99

	// maxTitleRunes bounds an item title in the menu, which is as wide as its widest row.
	maxTitleRunes = 48
)

// Summary is what the menu bar shows for an inbox at a moment.
type Summary struct {
	// Count is how many items need the user: the Inbox tab's count.
	Count int

	// Label is the text beside the icon: the count, "99+" past MaxCount, or "" for none.
	Label string

	// Waiting are the agent sessions waiting for the user, the longest waiting first,
	// which the menu lists before the items under WaitingTitle.
	Waiting []Waiting

	// Sections are the lists with items that need the user, in the inbox's order.
	Sections []Section

	// Nothing is the menu's line in place of the lists when no item and no session needs
	// the user.
	Nothing string

	// Status says when the inbox was last refreshed, or that the last refresh failed.
	Status string

	// Failed reports that the last refresh failed, and Error is its message.
	Failed bool
	Error  string

	// NextChange is when the soonest snooze ends, and Count goes up without anything being
	// stored, or the zero time when no snooze is running.
	NextChange time.Time
}

// Section is one list in the menu.
type Section struct {
	// Title is the list's name, as the inbox shows it.
	Title string

	// Entries are its first items that need the user, in the list's order.
	Entries []Entry

	// More is how many more of its items need the user.
	More int
}

// Entry is one item in the menu.
type Entry struct {
	// ItemID is the item a click shows in the window.
	ItemID int64

	// Label is the row: "example-repo #123 · Fix the login redirect", the title cut short.
	Label string

	// Tooltip is the item in full: "acme-org/example-repo #123: Fix the login redirect".
	Tooltip string
}

// WaitingTitle heads the sessions waiting for the user.
const WaitingTitle = "Waiting for you"

// Waiting is one agent session waiting for the user, in the menu.
type Waiting struct {
	// SessionID is the session a click shows the terminal of; ItemID and URL are its item,
	// which a click shows instead when the terminal cannot be.
	SessionID string
	ItemID    int64
	URL       string

	// Label is the row: "example-repo #123 · Claude needs approval".
	Label string

	// Tooltip is the item in full, and what the session waits for: "acme-org/example-repo
	// #123: Fix the login redirect · Claude needs approval".
	Tooltip string
}

// sectionTitles are the lists' names, as the inbox shows them.
//
//nolint:gochecknoglobals // a fixed table
var sectionTitles = [...]string{"Review requested", "Authored", "Assigned"}

// Summarize works out what the menu bar shows for snap and the sessions waiting for the
// user at now, with times shown in now's location. An item in more than one list is
// listed once, under the first, as the count counts it.
func Summarize(snap inbox.Snapshot, waiting []sessions.Session, now time.Time) Summary {
	out := Summary{
		Count:      snap.NeedsYou(now),
		Status:     status(snap, now),
		Failed:     snap.LastError != "",
		Error:      snap.LastError,
		NextChange: snap.NextSnoozeEnd(now),
	}

	out.Label = label(out.Count)
	out.Nothing = "Nothing needs you"

	if snap.LastRefreshedAt == "" {
		out.Nothing = "The inbox has not loaded yet"
	}

	out.Sections = sections(&snap, now)
	out.Waiting = waitingEntries(waiting)

	return out
}

// waitingEntries are the rows of the sessions waiting for the user.
func waitingEntries(waiting []sessions.Session) []Waiting {
	out := make([]Waiting, 0, len(waiting))

	for _, s := range waiting {
		name := s.Repo
		if i := strings.LastIndexByte(name, '/'); i >= 0 {
			name = name[i+1:]
		}

		number := "#" + strconv.Itoa(s.Number)
		what := agentName(s.Agent) + " " + s.State.Phrase()

		tooltip := s.Repo + " " + number
		if title := strings.Join(strings.Fields(s.Title), " "); title != "" {
			tooltip += ": " + title
		}

		out = append(out, Waiting{
			SessionID: s.ID, ItemID: s.ItemID, URL: s.URL,
			Label: name + " " + number + " · " + what, Tooltip: tooltip + " · " + what,
		})
	}

	return out
}

// agentName is what the menu calls an agent.
func agentName(id agent.ID) string {
	if a, err := agent.Lookup(id); err == nil {
		return a.Name
	}

	return string(id)
}

// sections are the lists with items that need the user at now, each item under the first
// list that holds it.
func sections(snap *inbox.Snapshot, now time.Time) []Section {
	var out []Section

	listed := make(map[int64]bool)

	for i, list := range snap.Lists() {
		section := Section{Title: sectionTitles[i]}

		for _, it := range list.Waiting(now) {
			if !listed[it.ID] {
				listed[it.ID] = true
				section.add(it)
			}
		}

		if len(section.Entries) > 0 {
			out = append(out, section)
		}
	}

	return out
}

// add lists an item, or counts it as one more once the section is full.
func (s *Section) add(it inbox.Item) {
	if len(s.Entries) == PerSection {
		s.More++

		return
	}

	s.Entries = append(s.Entries, entry(it))
}

// label is the text beside the icon for count items: "" for none, the count, or "99+".
func label(count int) string {
	switch {
	case count <= 0:
		return ""
	case count > MaxCount:
		return strconv.Itoa(MaxCount) + "+"
	default:
		return strconv.Itoa(count)
	}
}

// MoreLabel is the row that stands for a list's items left out: "3 more…".
func MoreLabel(more int) string {
	return strconv.Itoa(more) + " more…"
}

// entry is an item's row.
func entry(it inbox.Item) Entry {
	name := it.Repo
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}

	number := "#" + strconv.Itoa(it.Number)
	title := strings.Join(strings.Fields(it.Title), " ")

	return Entry{
		ItemID:  it.ID,
		Label:   name + " " + number + " · " + textutil.Truncate(title, maxTitleRunes),
		Tooltip: it.Repo + " " + number + ": " + title,
	}
}

// status says when the inbox was last refreshed, as of now and in now's location:
// "Updated 3:42 PM" today, "Updated Mar 4, 3:42 PM" before, "Last refresh failed", or
// "Not refreshed yet".
func status(snap inbox.Snapshot, now time.Time) string {
	if snap.LastError != "" {
		return "Last refresh failed"
	}

	at, err := time.Parse(time.RFC3339, snap.LastRefreshedAt)
	if err != nil {
		return "Not refreshed yet"
	}

	at = at.In(now.Location())

	if y, m, d := at.Date(); y == now.Year() && m == now.Month() && d == now.Day() {
		return "Updated " + at.Format("3:04 PM")
	}

	return "Updated " + at.Format("Jan 2, 3:04 PM")
}
