// Package inbox keeps the local inbox in step with GitHub and serves it to the UI.
//
// A refresh borrows the token, fetches the batched inbox query and the REST rate limits
// concurrently, checks the token's scopes, and writes everything in one transaction:
// the account, the repositories, the items with their reason flags and trust fields, and
// rate-limit snapshots. A snapshot reads it back as three plain lists plus a rate gauge.
//
// Inbox status that has no column of its own (each list's total match count, the time of
// the last successful refresh, the last error, and a scope warning) is kept in the prefs
// table under internal inbox.* keys, so it survives a restart.
package inbox

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox/model"
)

// snapshotRetention is how long rate-limit snapshots are kept.
const snapshotRetention = 7 * 24 * time.Hour

// Rate-limit resources stored in rate_snapshots.resource.
const (
	resourceGraphQL = "graphql"
	resourceCore    = "core"
	resourceSearch  = "search"
)

// Internal prefs keys for inbox status.
const (
	prefViewer               = "inbox.viewer"
	prefTotalReviewRequested = "inbox.total.review_requested"
	prefTotalAuthored        = "inbox.total.authored"
	prefTotalAssigned        = "inbox.total.assigned"
	prefLastRefreshAt        = "inbox.last_refresh_at"
	prefLastError            = "inbox.last_error"
	prefScopeWarning         = "inbox.scope_warning"
)

// ErrIncompleteFetch is returned when GitHub's answer lacks the signed-in account.
var ErrIncompleteFetch = errors.New("GitHub did not report the signed-in account")

// TokenSource borrows a GitHub token.
type TokenSource interface {
	Token(ctx context.Context) (github.Token, error)
}

// Client fetches the inbox and the rate limits from GitHub.
type Client interface {
	FetchInbox(ctx context.Context, tok github.Token) (*github.Inbox, error)
	FetchRateLimits(ctx context.Context, tok github.Token) (*github.RateLimits, error)
}

// Clock tells the time.
type Clock interface {
	Now() time.Time
}

// Notifier is told when the stored inbox or its status changed.
type Notifier interface {
	InboxUpdated()
}

// Config holds a Service's dependencies. Clock and Notifier are optional.
type Config struct {
	DB       *gorm.DB
	Tokens   TokenSource
	Client   Client
	Clock    Clock
	Notifier Notifier
}

// Service refreshes the inbox from GitHub and serves snapshots of it.
type Service struct {
	db       *gorm.DB
	tokens   TokenSource
	client   Client
	clock    Clock
	notifier Notifier

	// mu serializes refreshes, so a manual refresh and a scheduled one never interleave.
	mu sync.Mutex
}

// NewService returns a Service. A nil Clock uses the system clock.
func NewService(cfg Config) *Service {
	clock := cfg.Clock
	if clock == nil {
		clock = systemClock{}
	}

	return &Service{db: cfg.DB, tokens: cfg.Tokens, client: cfg.Client, clock: clock, notifier: cfg.Notifier}
}

// Refresh fetches the inbox from GitHub, stores it, and returns the new snapshot. On
// failure the error is recorded as the snapshot's last error (unless ctx was canceled)
// and the stored inbox is left as it was; the returned snapshot then shows that error.
func (s *Service) Refresh(ctx context.Context) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.refresh(ctx); err != nil {
		if ctx.Err() != nil {
			return Snapshot{}, fmt.Errorf("refresh the inbox: %w", err)
		}

		recordErr := s.recordFailure(ctx, err)

		s.notify()

		snap, _ := s.Snapshot(ctx) // best effort; the refresh error is what matters

		return snap, errors.Join(err, recordErr)
	}

	s.notify()

	return s.Snapshot(ctx)
}

// Snapshot returns the stored inbox: the three lists, the latest rate readings, and the
// refresh status.
func (s *Service) Snapshot(ctx context.Context) (Snapshot, error) {
	db := s.db.WithContext(ctx)

	prefs, err := readPrefs(db)
	if err != nil {
		return Snapshot{}, err
	}

	snap := newSnapshot(prefs)

	accountID, err := lookupAccount(db, prefs[prefViewer])
	if err != nil || accountID == 0 {
		return snap, err
	}

	rows, err := loadItems(db, accountID)
	if err != nil {
		return Snapshot{}, err
	}

	snap.addItems(rows)
	snap.applyTotals(prefs)

	if snap.Rate, err = loadRates(db, accountID); err != nil {
		return Snapshot{}, err
	}

	return snap, nil
}

// refresh does one fetch-and-store cycle.
func (s *Service) refresh(ctx context.Context) error {
	tok, err := s.tokens.Token(ctx)
	if err != nil {
		return err
	}

	inbox, limits, err := s.fetch(ctx, tok)
	if err != nil {
		return github.Redact(err, tok)
	}

	warning, err := github.CheckScopes(inbox.OAuthScopes)
	if err != nil {
		return err
	}

	return github.Redact(s.save(ctx, inbox, limits, warning), tok)
}

// fetch gets the inbox and the rate limits concurrently.
func (s *Service) fetch(ctx context.Context, tok github.Token) (*github.Inbox, *github.RateLimits, error) {
	var (
		inbox  *github.Inbox
		limits *github.RateLimits
	)

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		var err error

		limits, err = s.client.FetchRateLimits(gctx, tok)

		return err
	})

	g.Go(func() error {
		var err error

		inbox, err = s.client.FetchInbox(gctx, tok)

		return err
	})

	if err := g.Wait(); err != nil {
		return nil, nil, err
	}

	if inbox == nil || limits == nil || inbox.Viewer == "" {
		return nil, nil, ErrIncompleteFetch
	}

	return inbox, limits, nil
}

// save stores a fetched inbox in one transaction.
func (s *Service) save(ctx context.Context, inbox *github.Inbox, limits *github.RateLimits, warning github.ScopeWarning) error {
	now := s.now()

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		w := &writer{tx: tx, now: now, repos: make(map[string]int64)}

		if err := w.saveAccount(inbox.Viewer); err != nil {
			return err
		}

		if err := w.saveItems(inbox); err != nil {
			return err
		}

		if err := w.saveRates(inbox.RateLimit, limits); err != nil {
			return err
		}

		return setPrefs(tx, now, []pref{
			{prefViewer, inbox.Viewer},
			{prefTotalReviewRequested, strconv.Itoa(inbox.ReviewRequested.IssueCount)},
			{prefTotalAuthored, strconv.Itoa(inbox.Authored.IssueCount)},
			{prefTotalAssigned, strconv.Itoa(inbox.Assigned.IssueCount)},
			{prefLastRefreshAt, now.Format(time.RFC3339)},
			{prefLastError, ""},
			{prefScopeWarning, string(warning)},
		})
	})
	if err != nil {
		return fmt.Errorf("save the inbox: %w", err)
	}

	return nil
}

// recordFailure stores a failed refresh's message as the last error.
func (s *Service) recordFailure(ctx context.Context, err error) error {
	if recErr := setPrefs(s.db.WithContext(ctx), s.now(), []pref{{prefLastError, err.Error()}}); recErr != nil {
		return fmt.Errorf("record the refresh error: %w", recErr)
	}

	return nil
}

// notify tells the notifier, if any, that the inbox changed.
func (s *Service) notify() {
	if s.notifier != nil {
		s.notifier.InboxUpdated()
	}
}

// now returns the current time in UTC, which keeps stored timestamps sortable.
func (s *Service) now() time.Time {
	return s.clock.Now().UTC()
}

// systemClock is the real clock.
type systemClock struct{}

// Now returns the current time.
func (systemClock) Now() time.Time { return time.Now() }

// entry is one fetched item with the lists that returned it.
type entry struct {
	item            github.Item
	reviewRequested bool
	authored        bool
	assigned        bool
}

// mergeLists combines the three lists, one entry per item, in first-seen order.
func mergeLists(inbox *github.Inbox) []*entry {
	var order []*entry

	byKey := make(map[string]*entry)

	add := func(items []github.Item, mark func(*entry)) {
		for _, item := range items {
			key := itemKey(item)

			e, ok := byKey[key]
			if !ok {
				e = &entry{item: item}
				byKey[key] = e
				order = append(order, e)
			}

			mark(e)
		}
	}

	add(inbox.ReviewRequested.Items, func(e *entry) { e.reviewRequested = true })
	add(inbox.Authored.Items, func(e *entry) { e.authored = true })
	add(inbox.Assigned.Items, func(e *entry) { e.assigned = true })

	return order
}

// itemKey identifies an item across lists; owner and repository names are case-insensitive.
func itemKey(item github.Item) string {
	return repoKey(item.RepoOwner, item.RepoName) + "#" + kindOf(item) + strconv.Itoa(item.Number)
}

// repoKey identifies a repository case-insensitively.
func repoKey(owner, name string) string {
	return strings.ToLower(owner + "/" + name)
}

// kindOf maps an item to the stored kind.
func kindOf(item github.Item) string {
	if item.IsPR {
		return model.KindPR
	}

	return model.KindIssue
}
