package ghthreads

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/ghthreads/internal/queries"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
)

// Snapshot is what the window shows of the user's GitHub notifications. No URL GitHub gave
// is in it: the window builds a link to an item from its repository and number.
type Snapshot struct {
	// Status is how reading the notifications goes, and how many there are.
	Status Status `json:"status"`

	// ByItem has one entry per pull request or issue with threads, the most recently
	// updated first, for the cards' tags.
	ByItem []ItemThreads `json:"byItem"`

	// Unread are the newest MaxUnread unread threads, whatever their subject, the newest
	// first.
	Unread []Thread `json:"unread"`
}

// Status is how reading the user's GitHub notifications goes, for the account the last pass
// read, and how many there are.
type Status struct {
	// Reading is whether the app reads them (the Read rule). Off, the lists are empty and
	// the counts zero.
	Reading bool `json:"reading"`

	// Account is the login whose notifications the last pass read, "" before the first.
	Account string `json:"account"`

	// Access is whether GitHub lets the token read them.
	Access Access `json:"access"`

	// LastPassAt is when GitHub last answered a listing, and LastFullWalkAt when a listing
	// last read every page, as RFC 3339, "" for never.
	LastPassAt     string `json:"lastPassAt"`
	LastFullWalkAt string `json:"lastFullWalkAt"`

	// LastError is what went wrong with the last pass, in a sentence, "" when it went
	// through.
	LastError string `json:"lastError"`

	// AskAgainAt is when a listing GitHub refused is sent again, as RFC 3339, "" unless
	// Access is AccessRefused.
	AskAgainAt string `json:"askAgainAt"`

	// Count is how many notifications the user's GitHub inbox holds (read and unread, not
	// done), and Unread how many of them are unread.
	Count  int `json:"count"`
	Unread int `json:"unread"`

	// Truncated is whether GitHub lists more than the app reads: it holds the newest
	// MaxThreads only.
	Truncated bool `json:"truncated"`
}

// ItemThreads are the threads about one pull request or issue.
type ItemThreads struct {
	// Owner and Name are the item's repository, Kind and Number the item.
	Owner  string    `json:"owner"`
	Name   string    `json:"name"`
	Kind   item.Kind `json:"kind"`
	Number int       `json:"number"`

	// Unread is how many of its threads are unread, and Total how many there are.
	Unread int `json:"unread"`
	Total  int `json:"total"`

	// Reasons are GitHub's reasons for its threads (review_requested, mention, author,
	// ...), each once, that of the most recently updated thread first.
	Reasons []string `json:"reasons"`
}

// Thread is one notification.
type Thread struct {
	// ID is GitHub's ID of the thread.
	ID int64 `json:"id"`

	// Owner and Name are its repository, "" when GitHub named none.
	Owner string `json:"owner"`
	Name  string `json:"name"`

	// Kind and Number are the pull request or issue it is about, "" and 0 for any other
	// subject.
	Kind   item.Kind `json:"kind"`
	Number int       `json:"number"`

	// SubjectType is what it is about, in GitHub's words (PullRequest, Issue, Release,
	// CheckSuite, Discussion, ...), and Title the subject's title.
	SubjectType string `json:"subjectType"`
	Title       string `json:"title"`

	// Reason is why the user got it, in GitHub's words.
	Reason string `json:"reason"`

	// UpdatedAt is when it last changed on GitHub, as RFC 3339.
	UpdatedAt string `json:"updatedAt"`
}

// compared is s as a pass compares it with the last one told: the passes' times left out,
// since a pass that changed nothing else tells nothing.
func compared(s Snapshot) Snapshot {
	s.Status.LastPassAt, s.Status.LastFullWalkAt = "", ""

	return s
}

// snapshot builds the Snapshot of st's account, with reading on or off.
func (s *Service) snapshot(ctx context.Context, reading bool, st syncState) (Snapshot, error) {
	out := Snapshot{Status: st.status(reading), ByItem: []ItemThreads{}, Unread: []Thread{}}

	if !reading || st.Account == "" {
		return out, nil
	}

	rows, err := s.q.ListThreads(ctx, st.Account)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read the GitHub notifications: %w", err)
	}

	byItem := map[string]int{}

	for i := range rows {
		out.count(&rows[i])
		out.group(&rows[i], byItem)
	}

	return out, nil
}

// count counts r, the next row, the newest first, and lists it among the unread while
// they are fewer than MaxUnread.
func (s *Snapshot) count(r *queries.ListThreadsRow) {
	s.Status.Count++

	if !r.Unread {
		return
	}

	s.Status.Unread++

	if len(s.Unread) < MaxUnread {
		s.Unread = append(s.Unread, shownThread(r))
	}
}

// group adds r, the next row, the newest first, to the entry of the pull request or issue
// it is about, if any; byItem holds each entry's index by item key.
func (s *Snapshot) group(r *queries.ListThreadsRow, byItem map[string]int) {
	key, ok := itemKey(r)
	if !ok {
		return
	}

	at, found := byItem[key]
	if !found {
		at = len(s.ByItem)
		byItem[key] = at

		s.ByItem = append(s.ByItem, ItemThreads{
			Owner: r.RepoOwner, Name: r.RepoName, Kind: item.Kind(r.ItemKind), Number: int(r.Number),
			Reasons: []string{},
		})
	}

	entry := &s.ByItem[at]
	entry.Total++

	if r.Unread {
		entry.Unread++
	}

	if r.Reason != "" && !slices.Contains(entry.Reasons, r.Reason) {
		entry.Reasons = append(entry.Reasons, r.Reason)
	}
}

// status is the Status st records, with reading on or off.
func (st syncState) status(reading bool) Status {
	out := Status{
		Reading: reading, Account: st.Account, Access: st.access(), LastPassAt: stamp(st.LastPass),
		LastFullWalkAt: stamp(st.LastFullWalk), LastError: st.LastError, Truncated: st.Truncated,
	}

	if out.Access == AccessRefused {
		out.AskAgainAt = stamp(st.AskAgainAt)
	}

	return out
}

// itemKey keys the pull request or issue a row is about, as GitHub compares them; ok is
// false for a row about any other subject.
func itemKey(r *queries.ListThreadsRow) (string, bool) {
	if !item.Kind(r.ItemKind).Valid() || r.Number <= 0 {
		return "", false
	}

	return item.Ref{Repo: repo.Name{Owner: r.RepoOwner, Repo: r.RepoName}, Number: int(r.Number)}.Key(), true
}

// shownThread is a row as the window shows it.
func shownThread(r *queries.ListThreadsRow) Thread {
	return Thread{
		ID: r.ThreadID, Owner: r.RepoOwner, Name: r.RepoName, Kind: item.Kind(r.ItemKind), Number: int(r.Number),
		SubjectType: r.SubjectType, Title: r.SubjectTitle, Reason: r.Reason, UpdatedAt: stamp(r.UpdatedAt),
	}
}

// stamp writes t as RFC 3339, "" for the zero time.
func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	return clock.Format(t)
}
