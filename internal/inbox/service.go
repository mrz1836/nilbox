// Package inbox keeps the local inbox in step with GitHub and serves it to the UI.
//
// A refresh borrows the token and fetches, concurrently, the batched inbox query, the REST
// rate limits, and the details of the items listed after the last refresh (labels,
// comments and, for pull requests, size, checks, and reviews) by node ID. It checks the
// token's scopes, reads the details of any item new to the lists, and writes everything
// in one transaction: the account, the repositories, the items with their reason flags,
// trust fields, and details, and rate-limit snapshots. A snapshot reads it back as three
// plain lists, the signed-in account, and a rate gauge. The details are read on every
// refresh, because check results change without the item's own update time moving.
//
// After a successful refresh, the account's avatar is downloaded once and cached as a
// data URL, so the UI shows it offline and never loads anything from the network itself.
// It is downloaded again only when GitHub reports a different avatar URL, which happens
// when the picture or the signed-in account changes.
//
// Triage is local: Dismiss hides an item until Restore, and Snooze hides it for a number
// of days, after which it is back on its own. Both record the state in item_local_state
// and nothing else, so GitHub never hears of them. A snooze also records the item's
// activity, and each refresh, in the same transaction that stores the new details, ends
// early any snooze whose new activity passes the user's wake rules (see the snooze
// package), keeping the reasons. A snapshot keeps triaged items in their lists, each with
// its state, and the UI sets them apart; a snooze that runs its course ends without
// anything having to be written. Pin keeps an item at the top of its lists, the most
// recently pinned first, until Unpin; it is stored in the same row, apart from a snooze or
// a dismissal, so it outlasts both and Restore leaves it as it is.
//
// Inbox status that has no column of its own (each list's total match count, the
// account's profile, the time of the last successful refresh, the last error, a scope
// warning, and the cached avatar) is kept in the prefs table under internal inbox.* keys,
// so it survives a restart.
package inbox

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox/model"
	"github.com/mrz1836/nilbox/internal/trust"
)

// snapshotRetention is how long rate-limit snapshots are kept.
const snapshotRetention = 7 * 24 * time.Hour

// avatarTimeout bounds the avatar download, so a slow avatar host never holds up the
// refresh that triggered it.
const avatarTimeout = 10 * time.Second

// Rate-limit resources stored in rate_snapshots.resource.
const (
	resourceGraphQL = "graphql"
	resourceCore    = "core"
	resourceSearch  = "search"
)

// Internal prefs keys for inbox status.
const (
	prefViewer               = "inbox.viewer"
	prefViewerName           = "inbox.viewer_name"
	prefViewerURL            = "inbox.viewer_url"
	prefViewerAvatarURL      = "inbox.viewer_avatar_url"
	prefAvatar               = "inbox.avatar"
	prefAvatarSource         = "inbox.avatar_source"
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

// Client fetches the inbox, its items' details, a single item, the rate limits, and the
// viewer's avatar from GitHub.
type Client interface {
	FetchInbox(ctx context.Context, tok github.Token) (*github.Inbox, error)
	FetchDetails(ctx context.Context, tok github.Token, ids []string) (*github.Details, error)
	FetchItem(ctx context.Context, tok github.Token, id string) (*github.ItemUpdate, error)
	FetchRateLimits(ctx context.Context, tok github.Token) (*github.RateLimits, error)
	FetchAvatar(ctx context.Context, url string) (*github.Avatar, error)
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

	raw, stored := prefs[trust.PrefBots]
	snap.addItems(rows, trust.NewPolicy(trust.DecodeBots(raw, stored)))
	snap.applyTotals(prefs)

	if snap.Rate, err = loadRates(db, accountID); err != nil {
		return Snapshot{}, err
	}

	return snap, nil
}

// refresh does one fetch-and-store cycle. The details of the items already listed are
// read alongside the inbox query, which takes as long; only items new since the last
// refresh wait for a second details request after it.
func (s *Service) refresh(ctx context.Context) error {
	tok, err := s.tokens.Token(ctx)
	if err != nil {
		return err
	}

	listed, err := listedNodeIDs(s.db.WithContext(ctx))
	if err != nil {
		return err
	}

	got, err := s.fetch(ctx, tok, listed)
	if err != nil {
		return github.Redact(err, tok)
	}

	warning, err := github.CheckScopes(got.inbox.OAuthScopes)
	if err != nil {
		return err
	}

	if added := newIDs(nodeIDs(got.inbox), listed); len(added) > 0 {
		more, detailsErr := s.client.FetchDetails(ctx, tok, added)
		if detailsErr != nil {
			return github.Redact(fmt.Errorf("read the item details: %w", detailsErr), tok)
		}

		got.details.Add(more)
	}

	if err = s.save(ctx, got, warning); err != nil {
		return github.Redact(err, tok)
	}

	s.cacheAvatar(ctx, got.inbox.Viewer.AvatarURL)

	return nil
}

// refreshed is what one refresh read from GitHub.
type refreshed struct {
	inbox   *github.Inbox
	limits  *github.RateLimits
	details *github.Details

	// archived holds the items left out because their repository is archived.
	archived []github.Item
}

// fetch gets the inbox, the rate limits, and the details of the listed items
// concurrently. details is never nil.
func (s *Service) fetch(ctx context.Context, tok github.Token, listed []string) (refreshed, error) {
	got := refreshed{details: &github.Details{}}

	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		var err error

		got.limits, err = s.client.FetchRateLimits(gctx, tok)

		return err
	})

	g.Go(func() error {
		var err error

		got.inbox, err = s.client.FetchInbox(gctx, tok)

		return err
	})

	if len(listed) > 0 {
		g.Go(func() error {
			details, err := s.client.FetchDetails(gctx, tok, listed)
			if err != nil {
				return fmt.Errorf("read the item details: %w", err)
			}

			got.details.Add(details)

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return refreshed{}, err
	}

	if got.inbox == nil || got.limits == nil || got.inbox.Viewer.Login == "" {
		return refreshed{}, ErrIncompleteFetch
	}

	got.archived = dropArchived(got.inbox)
	dropClosed(got.inbox)

	return got, nil
}

// dropArchived takes the items of archived repositories out of the inbox's lists and
// returns them. The searches already leave archived repositories out (archived:false);
// this covers a repository archived after GitHub indexed its items, so nothing about an
// archived repository is listed, asked about, or launched. The lists' totals stay as
// GitHub counted them.
func dropArchived(inbox *github.Inbox) []github.Item {
	var dropped []github.Item

	for _, list := range []*github.SearchResult{&inbox.ReviewRequested, &inbox.Authored, &inbox.Assigned} {
		kept := list.Items[:0]

		for _, it := range list.Items {
			if it.Repo.IsArchived {
				dropped = append(dropped, it)

				continue
			}

			kept = append(kept, it)
		}

		list.Items = kept
	}

	return dropped
}

// dropClosed takes the items closed or merged since GitHub indexed them out of the
// inbox's lists. The searches ask for open items only, but the index can trail a merge
// or a close for a while; the state the query reads with each item does not. Like an
// archived repository's items, they leave every list, so a refresh never brings back an
// item that RefreshItem saw merged. The lists' totals stay as GitHub counted them.
func dropClosed(inbox *github.Inbox) {
	for _, list := range []*github.SearchResult{&inbox.ReviewRequested, &inbox.Authored, &inbox.Assigned} {
		list.Items = slices.DeleteFunc(list.Items, github.Item.Closed)
	}
}

// save stores what a refresh read in one transaction: the inbox and its items' details,
// the repositories of items left out, and the rate readings.
func (s *Service) save(ctx context.Context, got refreshed, warning github.ScopeWarning) error {
	now := s.now()
	inbox, limits, details := got.inbox, got.limits, got.details

	var items map[string]github.ItemDetails
	if details != nil {
		items = details.Items
	}

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		w := &writer{tx: tx, now: now, repos: make(map[string]int64), details: items}

		if err := w.saveAccount(inbox.Viewer.Login); err != nil {
			return err
		}

		if err := w.saveRepos(got.archived); err != nil {
			return err
		}

		if err := w.saveItems(inbox); err != nil {
			return err
		}

		if err := w.saveRates(graphQLReading(inbox.RateLimit, details), limits); err != nil {
			return err
		}

		return setPrefs(tx, now, []pref{
			{prefViewer, inbox.Viewer.Login},
			{prefViewerName, inbox.Viewer.Name},
			{prefViewerURL, inbox.Viewer.URL},
			{prefViewerAvatarURL, inbox.Viewer.AvatarURL},
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

// cacheAvatar downloads the viewer's avatar and stores it as a data URL, unless the one
// cached already came from avatarURL. It is best effort: the picture is cosmetic, so a
// failed download or write leaves the cache as it was, the UI shows the account's
// initial instead, and the next refresh tries again.
func (s *Service) cacheAvatar(ctx context.Context, avatarURL string) {
	if avatarURL == "" {
		return
	}

	db := s.db.WithContext(ctx)

	var sources []string
	if db.Raw("SELECT value FROM prefs WHERE key = ?", prefAvatarSource).Scan(&sources).Error != nil {
		return
	}

	if len(sources) > 0 && sources[0] == avatarURL {
		return
	}

	fetchCtx, cancel := context.WithTimeout(ctx, avatarTimeout)
	defer cancel()

	avatar, err := s.client.FetchAvatar(fetchCtx, avatarURL)
	if err != nil {
		return
	}

	// The source is written with the picture, so it never names a picture not stored.
	_ = db.Transaction(func(tx *gorm.DB) error {
		return setPrefs(tx, s.now(), []pref{{prefAvatar, avatar.DataURL()}, {prefAvatarSource, avatarURL}})
	})
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

// newIDs returns the IDs in ids that are not in known, in order.
func newIDs(ids, known []string) []string {
	seen := make(map[string]bool, len(known))
	for _, id := range known {
		seen[id] = true
	}

	var out []string

	for _, id := range ids {
		if !seen[id] {
			out = append(out, id)
		}
	}

	return out
}

// nodeIDs lists the node IDs of the inbox's items, each once, in first-seen order.
func nodeIDs(inbox *github.Inbox) []string {
	entries := mergeLists(inbox)
	ids := make([]string, 0, len(entries))
	seen := make(map[string]bool, len(entries))

	for _, e := range entries {
		if id := e.item.NodeID; id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}

	return ids
}

// graphQLReading is the GraphQL budget a refresh leaves: the details query's reading,
// since it ran last, with the cost of both queries; or the inbox query's alone when no
// details were asked for or GitHub sent no budget with them.
func graphQLReading(inbox github.QueryRate, details *github.Details) github.QueryRate {
	if details == nil || details.Requests == 0 || details.RateLimit.Limit == 0 {
		return inbox
	}

	reading := details.RateLimit
	reading.Cost += inbox.Cost

	return reading
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
