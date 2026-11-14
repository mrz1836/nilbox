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
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/threadrules"
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

	// Queued are the clears waiting to be sent, the one due first first, each of which Keep
	// takes back.
	Queued []QueuedClear `json:"queued"`

	// Backlog is how many clears the last pass held back, more than clear on their own, for
	// the user to review (Preview, then Clear); 0 when none waits.
	Backlog int `json:"backlog"`

	// Recent are the latest MaxRecent clears that settled, the latest first.
	Recent []SettledClear `json:"recent"`
}

// QueuedClear is a clear waiting to be sent.
type QueuedClear struct {
	// ID is the clear's, which Keep takes, and ThreadID GitHub's ID of the thread.
	ID       int64 `json:"id"`
	ThreadID int64 `json:"threadId"`

	// Owner and Name are the thread's repository, Kind and Number its item, and Title the
	// item's title.
	Owner  string    `json:"owner"`
	Name   string    `json:"name"`
	Kind   item.Kind `json:"kind"`
	Number int       `json:"number"`
	Title  string    `json:"title"`

	// SendAfter is when it may be sent, as RFC 3339: the Undo hold's end, or when it was
	// queued.
	SendAfter string `json:"sendAfter"`

	// Trigger is the moment that chose it, and Action what it does.
	Trigger threadrules.Trigger `json:"trigger"`
	Action  threadrules.Action  `json:"action"`
}

// SettledClear is a clear that left the queue.
type SettledClear struct {
	// ID is the clear's, and ThreadID GitHub's ID of the thread.
	ID       int64 `json:"id"`
	ThreadID int64 `json:"threadId"`

	// Owner and Name are the thread's repository, Kind and Number its item, and Title the
	// item's title.
	Owner  string    `json:"owner"`
	Name   string    `json:"name"`
	Kind   item.Kind `json:"kind"`
	Number int       `json:"number"`
	Title  string    `json:"title"`

	// Trigger is the moment that chose it, Action what it did or was to do, and State how
	// it settled.
	Trigger threadrules.Trigger `json:"trigger"`
	Action  threadrules.Action  `json:"action"`
	State   ClearState          `json:"state"`

	// At is when it settled, as RFC 3339.
	At string `json:"at"`

	// Error is the last failure, in a sentence, for a failed clear, and Reason why a
	// skipped one was.
	Error  string     `json:"error"`
	Reason SkipReason `json:"reason"`
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
	out := Snapshot{
		Status: st.status(reading), ByItem: []ItemThreads{}, Unread: []Thread{}, Queued: []QueuedClear{},
		Recent: []SettledClear{},
	}

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

	if err = s.ledger(ctx, &out, st); err != nil {
		return Snapshot{}, err
	}

	return out, nil
}

// ledger adds the clears of st's account to out: those queued, the backlog, and the latest
// that settled.
func (s *Service) ledger(ctx context.Context, out *Snapshot, st syncState) error {
	queued, err := s.q.ListQueuedClears(ctx, st.Account)
	if err != nil {
		return fmt.Errorf("read the queued clears of GitHub notifications: %w", err)
	}

	for _, c := range queued {
		out.Queued = append(out.Queued, QueuedClear{
			ID: c.ID, ThreadID: c.ThreadID, Owner: c.RepoOwner, Name: c.RepoName, Kind: item.Kind(c.ItemKind),
			Number: int(c.Number), Title: c.Title, SendAfter: stamp(c.SendAfter), Trigger: threadrules.Trigger(c.Trigger),
			Action: threadrules.Action(c.Action),
		})
	}

	recent, err := s.q.ListRecentClears(ctx, queries.ListRecentClearsParams{AccountLogin: st.Account, RowLimit: MaxRecent})
	if err != nil {
		return fmt.Errorf("read the cleared GitHub notifications: %w", err)
	}

	for _, c := range recent {
		out.Recent = append(out.Recent, SettledClear{
			ID: c.ID, ThreadID: c.ThreadID, Owner: c.RepoOwner, Name: c.RepoName, Kind: item.Kind(c.ItemKind),
			Number: int(c.Number), Title: c.Title, Trigger: threadrules.Trigger(c.Trigger),
			Action: threadrules.Action(c.Action), State: ClearState(c.State), At: stamp(store.Deref(c.SettledAt)),
			Error: c.Error, Reason: SkipReason(c.SkipReason),
		})
	}

	out.Backlog = st.Backlog

	return nil
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
