// Package ghthreads mirrors the user's GitHub notifications, which GitHub's REST API calls
// threads, as the web inbox lists them (read and unread, not done), in the gh_threads table
// it owns, one row per account and thread, so the window counts them and tags the cards
// they are about without asking GitHub, and clears them by the user's rules (clear.go):
// it marks a thread done, or read, once the work behind it is finished. Clearing is the
// only write it makes, through the GitHub client's chore lane, and it makes none until
// the user turns clearing on, or clears one by hand.
//
// A pass reads them, never two at once. With reading turned off (threadrules' Read), a pass
// sends nothing and borrows no token. Otherwise it learns the token's account (one GraphQL
// point per new token) and lists the notifications: page 1 is sent with the ETag its last
// answer carried, and GitHub's 304 Not Modified costs nothing and ends the listing; a 200
// reads on, page by page, until a page whose oldest thread is no newer than the newest the
// last pass saw (the high-water mark). A 304 says nothing of the later pages, so a thread
// read or marked done there shows only on a walk of every page: at the first pass after the
// app starts, every FullWalkEvery, after the account changes, and once reading comes back
// on. A walk reads at most github.MaxThreadPages pages, the newest MaxThreads threads. A
// 200 deletes the account's rows it didn't list within the span it read, done on GitHub:
// every one for a walk, and those updated after the oldest thread on its last page for a
// pass that stopped at the mark. A pass stores what it read in one transaction at its end,
// so one that fails partway stores nothing, and never an ETag or a high-water mark the
// pages it meant to read don't back.
//
// GitHub refusing the listing (github.NotificationsRefused: 403 or 404, as for a
// fine-grained token) is remembered, and
// no listing is sent again for RefusedFor, unless CheckNow asks. Then the pass checks the
// state of the pull requests and issues its threads are about, in one batched request: those
// never checked, those whose thread changed since, and those not known finished that were
// checked StateRecheck ago. Releases, discussions, check suites, and the other subjects are
// mirrored and counted, never checked.
//
// The Syncer runs a pass as often as the inbox is read, never sooner than GitHub's
// X-Poll-Interval asks, and the window reads the mirror through Snapshot alone, which asks
// GitHub nothing. Each pass that changes what Snapshot shows emits event.GHThreadsUpdated.
// A pass then decides which threads clear, queues them in the gh_thread_clears ledger, and
// sends the ones due; the Syncer also runs a pass when the inbox tells of something the user
// finished (Settler), when the rules change, and when a queued clear comes due.
package ghthreads

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/ghthreads/internal/queries"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/threadrules"
)

const (
	// FullWalkEvery is how long after a walk of every page another is due.
	FullWalkEvery = 6 * time.Hour

	// RefusedFor is how long after GitHub refused the listing it is sent again, unless the
	// user asks first (CheckNow).
	RefusedFor = 6 * time.Hour

	// StateRecheck is how long a thread's item, not known to be merged or closed, goes
	// before its state is checked again, though its thread didn't change: how a merge on a
	// thread that didn't move is caught.
	StateRecheck = 30 * time.Minute

	// OtherAccountsKept is how long the threads of an account other than the one read last
	// are kept, unseen, before a walk of every page drops them.
	OtherAccountsKept = 30 * 24 * time.Hour

	// MaxUnread is how many unread threads Snapshot lists, the newest first.
	MaxUnread = 20

	// MaxThreads is the most threads the mirror holds: every page a walk reads, full.
	MaxThreads = github.MaxThreadPages * github.ThreadsPerPage
)

// Client is what a pass asks GitHub: the token's account, a page of the notifications, the
// state of pull requests and issues, and who wrote a thread's latest comment; and what it
// sends: a thread marked done or read, through the chore lane. ParseSubject reads an API
// URL GitHub gave and sends nothing. *github.Client implements it.
type Client interface {
	FetchViewer(ctx context.Context, tok github.Token) (string, error)
	ListThreads(ctx context.Context, tok github.Token, q github.ThreadQuery) (*github.ThreadPage, error)
	FetchItemStates(ctx context.Context, tok github.Token, refs []item.Ref) (map[string]github.ItemState, error)
	FetchLatestComment(ctx context.Context, tok github.Token, rawURL string) (*github.CommentAuthor, error)
	ParseSubject(raw string) (item.Ref, bool)
	MarkThreadDone(ctx context.Context, tok github.Token, id int64) error
	MarkThreadRead(ctx context.Context, tok github.Token, id int64) error
}

// RuleSource reads the user's rules for their GitHub notifications, as stored now; core
// adapts the preferences settings writes.
type RuleSource interface {
	ThreadRules(ctx context.Context) (threadrules.Rules, error)
}

// HoldSource reads how long, in seconds, the window's toasts stay (the toast setting),
// which the Undo hold follows (Hold); core adapts the settings.
type HoldSource interface {
	ToastSeconds(ctx context.Context) (int, error)
}

// Config holds a Service's dependencies. Pool, Tokens, Client, and Rules are required.
type Config struct {
	// Pool is the database, which holds the threads, the ledger of clears, and the state of
	// the passes.
	Pool *sql.DB

	// Tokens lends the token a pass reads and clears with; a token GitHub refuses is
	// borrowed again once (github.WithToken).
	Tokens github.Tokens
	Client Client

	// Rules says whether the user's notifications are read at all, and when they clear.
	Rules RuleSource

	// Holds says how long the window's toasts stay, which the Undo hold follows; nil, or
	// one that can't be read, holds MinHold.
	Holds HoldSource

	// Clock stamps the passes and the rows; nil is the system clock. The Syncer's waits
	// run on the real clock.
	Clock clock.Clock

	// Events hears event.GHThreadsUpdated; nil drops it.
	Events event.Sink

	// Logger receives the passes' failures; nil discards them.
	Logger *slog.Logger
}

// Service mirrors the user's GitHub notifications and serves the window what it holds. It
// is safe for concurrent use.
type Service struct {
	db     *sql.DB
	q      *queries.Queries
	prefs  *prefs.Store
	tokens github.Tokens
	client Client
	rules  RuleSource
	holds  HoldSource
	clock  clock.Clock
	events event.Sink
	logger *slog.Logger

	// viewers remembers the account the last token belongs to.
	viewers github.ViewerCache

	// running holds the one pass at a time: a pass sends into it to start, and takes its
	// send back once it ends. What follows is touched only by the pass that holds it.
	running chan struct{}

	// walked is whether a pass walked every page since the Service was built: until one
	// does, every pass walks every page.
	walked bool

	// shown is what Snapshot showed at the end of the last pass, nil until one ended, or
	// when it couldn't be read: the next pass tells the window only what changes it.
	shown *Snapshot

	// sending holds a clear from its last check to its record in the ledger, so Keep, and a
	// restore Settle hears of, never mark kept a clear on its way to GitHub.
	sending sync.Mutex

	// nudges holds what the Syncer was asked to run soon.
	nudges nudges

	// mu guards what the Syncer reads between passes.
	mu sync.Mutex

	// ended is when the last pass that listed, or meant to, ended, on the real clock, zero
	// before the first; poll is how long GitHub last asked the app to wait between listings.
	ended time.Time
	poll  time.Duration

	// sendAt is when the next queued clear may be sent, on the Service's clock, zero when
	// none waits on a time: none is queued, or what holds them back ends only with a pass.
	// sendGen counts the clears the user queued (sendBy), so a run that ends never puts
	// one off.
	sendAt  time.Time
	sendGen uint64
}

// NewService returns a Service over cfg.
func NewService(cfg Config) *Service {
	return &Service{
		db: cfg.Pool, q: queries.New(cfg.Pool), prefs: prefs.New(cfg.Pool, cfg.Clock), tokens: cfg.Tokens,
		client: cfg.Client, rules: cfg.Rules, holds: cfg.Holds, clock: clock.Or(cfg.Clock),
		events: event.Or(cfg.Events), logger: logging.Or(cfg.Logger), running: make(chan struct{}, 1),
		nudges: nudges{wake: make(chan struct{}, 1)},
	}
}

// Snapshot returns what the window shows of the user's GitHub notifications, for the
// account the last pass read, from the database alone: it never asks GitHub. With reading
// turned off, its status says so and its lists are empty.
func (s *Service) Snapshot(ctx context.Context) (Snapshot, error) {
	rules, err := s.rules.ThreadRules(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read the rules for GitHub notifications: %w", err)
	}

	st, err := s.loadState(ctx)
	if err != nil {
		return Snapshot{}, err
	}

	return s.snapshot(ctx, rules.Read, st)
}

// CheckNow runs a pass now, once any pass already running has ended, then returns the
// Snapshot: the window's Check again, and its first look once reading is turned on. It
// sends the listing even while a refused one waits RefusedFor, and whatever GitHub's poll
// interval says; with reading turned off, it sends nothing. A pass that fails is told in
// the Snapshot's status, which the window shows, so only a call the window gave up on, or a
// Snapshot that can't be read, is an error.
func (s *Service) CheckNow(ctx context.Context) (Snapshot, error) {
	if err := s.runPass(ctx, passRequest{now: true}); err != nil {
		if ctx.Err() != nil {
			return Snapshot{}, fmt.Errorf("check the GitHub notifications: %w", err)
		}

		s.logger.WarnContext(ctx, "checking the GitHub notifications failed", slog.Any("error", err))
	}

	return s.Snapshot(ctx)
}

// pollInterval is how long GitHub last asked the app to wait between listings, 0 when it
// didn't say.
func (s *Service) pollInterval() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.poll
}

// lastEnded is when the last pass ended, on the real clock, zero before the first.
func (s *Service) lastEnded() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.ended
}

// passEnded records that a pass ended now.
func (s *Service) passEnded() {
	s.mu.Lock()
	s.ended = time.Now()
	s.mu.Unlock()
}

// keepPoll records st's poll interval for the Syncer.
func (s *Service) keepPoll(st syncState) {
	s.mu.Lock()
	s.poll = st.pollInterval()
	s.mu.Unlock()
}

// untilSend says how long until a queued clear may be sent, on the Service's clock, which
// the Syncer waits on the real one; ok is false when none waits on a time.
func (s *Service) untilSend() (wait time.Duration, ok bool) {
	s.mu.Lock()
	at := s.sendAt
	s.mu.Unlock()

	if at.IsZero() {
		return 0, false
	}

	return at.Sub(s.clock.Now()), true
}

// sendGeneration is how many times sendBy was called: a run that begins at one learns, as
// it ends, whether a clear was queued meanwhile.
func (s *Service) sendGeneration() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.sendGen
}

// sendBy has the next send go by at, unless one is due sooner, and moves the generation on,
// so a run that was going on keeps it (planSend).
func (s *Service) sendBy(at time.Time) {
	s.mu.Lock()
	s.sendGen++
	s.sendAt = earliest(s.sendAt, at)
	s.mu.Unlock()
}

// earliest returns the earlier of a and b, zero standing for none.
func earliest(a, b time.Time) time.Time {
	switch {
	case a.IsZero():
		return b
	case b.IsZero() || a.Before(b):
		return a
	default:
		return b
	}
}
