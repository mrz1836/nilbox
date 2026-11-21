package ghthreads

import (
	"context"
	"fmt"
	"hash/fnv"
	"io"
	"strconv"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/ghthreads/internal/queries"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/threadrules"
)

// Snapshot is what the window shows of the user's GitHub notifications. No URL GitHub gave
// is in it: the window builds a link to an item from its repository and number.
type Snapshot struct {
	// Status is how reading the notifications goes, and how many there are.
	Status Status `json:"status"`

	// Unread are the newest MaxUnread unread threads, whatever their subject, the newest
	// first.
	Unread []Thread `json:"unread"`

	// Queued are the clears waiting to be sent, the one due first first, each of which Keep
	// takes back.
	Queued []QueuedClear `json:"queued"`

	// Backlog is how many clears the last pass held back, more than clear on their own; 0
	// when none waits.
	Backlog int `json:"backlog"`

	// Recent are the latest MaxRecent clears that settled, the latest first.
	Recent []SettledClear `json:"recent"`

	// listed is a fingerprint of every thread the mirror lists for the account (its id,
	// unread, state, title, reason, and update time), read and unread, past the newest
	// MaxUnread too: List shows them all, so a pass that changes any of them differs from
	// the Snapshot told before, and tells the window. Unexported, it never reaches the
	// window, which compares nothing with it.
	listed uint64
}

// ThreadList is every thread the mirror holds for the account the last pass read, read and
// unread, as the Notifications page lists them. No URL GitHub gave is in it either.
type ThreadList struct {
	// Status is the Snapshot's: how reading goes, and how many there are.
	Status Status `json:"status"`

	// Threads are every one, the most recently updated first.
	Threads []Thread `json:"threads"`
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

	// Unread is whether it is unread on GitHub.
	Unread bool `json:"unread"`

	// State is its pull request's or issue's state as the last state check read it,
	// StateUnknown until one did and for any other subject.
	State threadrules.State `json:"state"`
}

// compared is s as a pass compares it with the last one told: the passes' times left out,
// since a pass that changed nothing else tells nothing.
func compared(s Snapshot) Snapshot {
	s.Status.LastPassAt, s.Status.LastFullWalkAt = "", ""

	return s
}

// List returns every thread the mirror holds for the account the last pass read, read and
// unread, the most recently updated first, with the Snapshot's status, from the database
// alone: it never asks GitHub. With reading turned off, or before a pass read an account,
// its status says so and its list is empty.
func (s *Service) List(ctx context.Context) (ThreadList, error) {
	rules, err := s.rules.ThreadRules(ctx)
	if err != nil {
		return ThreadList{}, fmt.Errorf("read the rules for GitHub notifications: %w", err)
	}

	st, err := s.loadState(ctx)
	if err != nil {
		return ThreadList{}, err
	}

	status, rows, err := s.listed(ctx, rules.Read, st)
	if err != nil {
		return ThreadList{}, err
	}

	out := ThreadList{Status: status, Threads: make([]Thread, 0, len(rows))}
	for i := range rows {
		out.Threads = append(out.Threads, shownThread(&rows[i]))
	}

	return out, nil
}

// snapshot builds the Snapshot of st's account, with reading on or off.
func (s *Service) snapshot(ctx context.Context, reading bool, st syncState) (Snapshot, error) {
	status, rows, err := s.listed(ctx, reading, st)
	if err != nil {
		return Snapshot{}, err
	}

	out := Snapshot{Status: status, Unread: []Thread{}, Queued: []QueuedClear{}, Recent: []SettledClear{}}

	if !reading || st.Account == "" {
		return out, nil
	}

	h := fnv.New64a()

	for i := range rows {
		r := &rows[i]
		printThread(h, r)

		if r.Unread && len(out.Unread) < MaxUnread {
			out.Unread = append(out.Unread, shownThread(r))
		}
	}

	out.listed = h.Sum64()

	if err = s.ledger(ctx, &out, st); err != nil {
		return Snapshot{}, err
	}

	return out, nil
}

// listed reads every thread of st's account, the newest first, and the Status st records
// with their counts: none with reading off or before a pass read an account. Snapshot and
// List share it, so both count alike.
func (s *Service) listed(ctx context.Context, reading bool, st syncState) (Status, []queries.ListThreadsRow, error) {
	status := st.status(reading)

	if !reading || st.Account == "" {
		return status, nil, nil
	}

	rows, err := s.q.ListThreads(ctx, st.Account)
	if err != nil {
		return Status{}, nil, fmt.Errorf("read the GitHub notifications: %w", err)
	}

	for i := range rows {
		status.Count++

		if rows[i].Unread {
			status.Unread++
		}
	}

	return status, rows, nil
}

// printThread writes what List shows of r that a pass may change into w, for Snapshot's
// fingerprint: its id, unread, state, title, reason, and update time, each ended by a zero
// byte.
func printThread(w io.Writer, r *queries.ListThreadsRow) {
	for _, part := range []string{
		strconv.FormatInt(r.ThreadID, 10), strconv.FormatBool(r.Unread), r.SubjectState, r.SubjectTitle, r.Reason,
		stamp(r.UpdatedAt),
	} {
		_, _ = io.WriteString(w, part+"\x00")
	}
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

// shownThread is a row as the window shows it.
func shownThread(r *queries.ListThreadsRow) Thread {
	return Thread{
		ID: r.ThreadID, Owner: r.RepoOwner, Name: r.RepoName, Kind: item.Kind(r.ItemKind), Number: int(r.Number),
		SubjectType: r.SubjectType, Title: r.SubjectTitle, Reason: r.Reason, UpdatedAt: stamp(r.UpdatedAt),
		Unread: r.Unread, State: threadrules.State(r.SubjectState),
	}
}

// stamp writes t as RFC 3339, "" for the zero time.
func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	return clock.Format(t)
}
