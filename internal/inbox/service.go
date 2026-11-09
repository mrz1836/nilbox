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
// One refresh runs at a time: a refresh asked for while one runs, such as Refresh Now
// during a scheduled refresh, joins it and gets its result. A refresh fetches without
// holding any lock, so an item's own refresh (RefreshItem) and a check of its files
// (CheckFiles) never wait for it; the stores are ordered by when each fetch started (see
// order.go), so a slower fetch never overwrites what a later one stored.
//
// The snapshot is built once per change and kept until the next write, so the window,
// the menu bar, and the scheduler read the same one; it is built again when the trusted
// bots or the sensitive-file settings change.
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
// The inbox edits GitHub only when the user asks, one item at a time, and only to take the
// user off it: Unassign and RemoveReviewRequest (see edits.go). Each is one REST request the
// API budget paces and counts, made only with the token of the account whose inbox is shown,
// and the item leaves that list once GitHub's answer confirms the edit.
//
// The review sheet reads and acts on a pull request through an Actor (see actor.go), which
// runs the sheet's own requests under the item's lock and, for an action, only with a token
// of the account whose inbox is shown, then records what GitHub did: a review takes the pull
// request out of the Review requested list, and a merge out of every list, at once and with
// no request of its own. A read rewrites the card, and the files it listed serve as the pull
// request's sensitive-file check, so one read serves both.
//
// An issue's details include the GitHub projects it is in, with its status in each, when
// the token has the optional read:project scope. Without it the details are read without
// them and the snapshot says so (ProjectAccess), so a missing scope never fails a refresh.
// The details of the items already listed are asked for before the token's scopes are
// known, the way the last refresh read them; when this refresh reads projects otherwise,
// every item's details are read again once the scopes are known, so they all agree.
//
// Inbox status that has no column of its own (each list's total match count, the
// account's profile, the time of the last successful refresh, the last error, a scope
// warning, whether projects can be read, and the cached avatar) is kept in the prefs
// table, so it survives a restart, under internal inbox.* keys that only this package
// writes.
package inbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/flight"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox/internal/queries"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// snapshotRetention is how long rate-limit snapshots are kept.
const snapshotRetention = 7 * 24 * time.Hour

// avatarTimeout bounds the avatar download, so a slow avatar host never holds up the
// refresh that triggered it.
const avatarTimeout = 10 * time.Second

// refreshKey names the one whole refresh that runs at a time.
const refreshKey = "refresh"

// Rate-limit resources stored in rate_snapshots.resource.
const (
	resourceGraphQL = "graphql"
	resourceCore    = "core"
	resourceSearch  = "search"
)

// Internal prefs keys for inbox status. The inbox writes them, and no other package.
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
	prefProjects             = "inbox.projects"
)

// ErrIncompleteFetch is returned when GitHub's answer lacks the signed-in account.
var ErrIncompleteFetch = errors.New("GitHub did not report the signed-in account")

// TokenSource lends a GitHub token, and takes back one GitHub refused so the next is
// borrowed fresh. *github.TokenSource implements it.
type TokenSource interface {
	Token(ctx context.Context) (github.Token, error)
	Forget(tok github.Token)
}

// Client fetches the inbox, its items' details, a single item, a pull request's head and
// changed files, the rate limits, and the viewer's avatar from GitHub, and makes the few
// edits the user asks for. *github.Client
// implements it. No error it returns holds the token's text, so the inbox stores and
// shows them as they are.
type Client interface {
	FetchInbox(ctx context.Context, tok github.Token) (*github.Inbox, error)
	FetchDetails(ctx context.Context, tok github.Token, ids []string, opts github.DetailOptions) (*github.Details, error)
	FetchItem(ctx context.Context, tok github.Token, id string, opts github.DetailOptions) (*github.ItemUpdate, error)
	FetchPullHead(ctx context.Context, tok github.Token, ref item.Ref) (*github.PullHead, error)
	FetchChangedFiles(ctx context.Context, tok github.Token, ref item.Ref, head string) (*github.ChangedFiles, error)
	FetchRateLimits(ctx context.Context, tok github.Token) (*github.RateLimits, error)
	FetchAvatar(ctx context.Context, url string) (*github.Avatar, error)

	// FetchViewer, RemoveAssignee, and RemoveReviewRequest serve the edits (edits.go).
	FetchViewer(ctx context.Context, tok github.Token) (string, error)
	RemoveAssignee(ctx context.Context, tok github.Token, ref item.Ref, login string) (*github.Edited, error)
	RemoveReviewRequest(ctx context.Context, tok github.Token, ref item.Ref, login string) (*github.Edited, error)
}

// RefreshUsage hears what each whole refresh spent of the GraphQL budget, in points, and
// how many pull requests and issues it tracked, for the API budget the window shows.
// *budget.Tracker implements it.
type RefreshUsage interface {
	Refreshed(points, items int)
}

// Config holds a Service's dependencies. Clock, Events, Arrivals, Usage, and Logger are
// optional.
type Config struct {
	// Pool is the database's connection pool. It holds the inbox's tables, and the prefs
	// that keep the inbox status and the settings the inbox reads.
	Pool *sql.DB

	Tokens TokenSource
	Client Client
	Clock  clock.Clock

	// Events hears an event.InboxUpdated whenever the stored inbox or its status changed.
	Events event.Sink

	// Arrivals is told about the items that newly need the user after each refresh.
	Arrivals ArrivalSink

	// Usage is told what each refresh it stores cost, and how many items it tracked.
	Usage RefreshUsage

	// Logger receives what a call cannot report as its error, at Debug: an avatar that
	// could not be downloaded, and a snapshot that could not be read after a write.
	// Without it they are not logged.
	Logger *slog.Logger
}

// Service refreshes the inbox from GitHub and serves snapshots of it. It is safe for
// concurrent use.
type Service struct {
	db       *sql.DB
	q        *queries.Queries
	prefs    *prefs.Store
	tokens   TokenSource
	client   Client
	clock    clock.Clock
	events   event.Sink
	arrivals ArrivalSink
	usage    RefreshUsage
	logger   *slog.Logger

	// refreshes runs one whole refresh at a time; a refresh asked for while one runs
	// joins it.
	refreshes flight.Group[Snapshot]

	// items runs the work on one item one piece at a time.
	items itemLocks

	// order keeps the stores of what GitHub said in the order their fetches started.
	order writeOrder

	snapshots snapshotCache
	scopes    scopeCache

	// viewers remembers the account the last token used belongs to, for the edits.
	viewers viewerCache
}

// NewService returns a Service. A nil Clock uses the system clock.
func NewService(cfg Config) *Service {
	c := clock.Or(cfg.Clock)

	return &Service{
		db: cfg.Pool, q: queries.New(cfg.Pool), prefs: prefs.New(cfg.Pool, c), tokens: cfg.Tokens,
		client: cfg.Client, clock: c, events: event.Or(cfg.Events), arrivals: cfg.Arrivals, usage: cfg.Usage,
		logger: logging.Or(cfg.Logger),
	}
}

// Refresh fetches the inbox from GitHub, stores it, and returns the new snapshot. On
// failure what the user is told about the error (see uierr.Message) is recorded as the
// snapshot's last error, unless ctx was canceled, and the stored inbox is left as it
// was; the returned snapshot then shows it.
//
// A call made while a refresh runs joins it and returns its result, so a manual refresh
// and a scheduled one never run side by side. A caller whose ctx ends stops waiting; the
// refresh goes on for the others.
//
// A refresh that stored what GitHub said tells the arrival sink, once, about the items
// that newly need the user.
func (s *Service) Refresh(ctx context.Context) (Snapshot, error) {
	return s.refreshes.Do(ctx, refreshKey, s.runRefresh)
}

// runRefresh is one whole refresh, run once for every caller that asked meanwhile.
func (s *Service) runRefresh(ctx context.Context) (Snapshot, error) {
	arrivals, err := s.refresh(ctx)
	if err != nil {
		err = fmt.Errorf("refresh the inbox: %w", err)
		if ctx.Err() != nil {
			return Snapshot{}, err
		}

		recordErr := s.recordFailure(ctx, err)

		// The refresh error is what the caller needs; the snapshot read is best effort.
		snap, readErr := s.changed(ctx)
		if readErr != nil {
			s.logger.DebugContext(ctx, "could not read the inbox after a failed refresh", "error", readErr)
		}

		return snap, errors.Join(err, recordErr)
	}

	snap, err := s.changed(ctx)
	if err == nil {
		s.announce(ctx, arrivals)
	}

	return snap, err
}

// changed empties the snapshot cache after a write, reads the snapshot again so it is
// kept, says the inbox changed, and returns the snapshot. Reading before saying so lets
// everyone told read the kept snapshot instead of building their own.
func (s *Service) changed(ctx context.Context) (Snapshot, error) {
	s.snapshots.empty()

	snap, err := s.Snapshot(ctx)

	s.notify()

	return snap, err
}

// refresh does one fetch-and-store cycle. It fetches without holding a lock, and holds
// the store lock only to write (see order.go).
func (s *Service) refresh(ctx context.Context) ([]Arrival, error) {
	listed, err := s.listedNodeIDs(ctx)
	if err != nil {
		return nil, err
	}

	last, err := s.projectAccess(ctx)
	if err != nil {
		return nil, err
	}

	var seq uint64

	defer func() { s.endRefresh(seq) }()

	got, err := github.WithToken(ctx, s.tokens, func(tok github.Token) (refreshed, error) {
		// Each try numbers its own fetch, so a retry with a fresh token is ordered by when
		// it started, not by when the refused try did.
		seq = s.startRefresh()

		return s.fetchAll(ctx, tok, listed, last)
	})
	if err != nil {
		return nil, err
	}

	arrivals, err := s.save(ctx, seq, got)
	if err == nil && s.usage != nil {
		s.usage.Refreshed(graphQLReading(got.inbox.RateLimit, got.details).Cost, len(nodeIDs(got.inbox)))
	}

	return arrivals, err
}

// fetchAll reads everything one refresh stores, with tok. The details of the items
// already listed are read alongside the inbox query, which takes as long, with the
// projects the last refresh could read (last); only items new since the last refresh wait
// for a second details request after it (see readMoreDetails). The file checks and the
// avatar download then run side by side.
func (s *Service) fetchAll(ctx context.Context, tok github.Token, listed []string, last ProjectAccess) (refreshed, error) {
	asked := readOptions(last)

	got, err := s.fetch(ctx, tok, listed, asked)
	if err != nil {
		return refreshed{}, err
	}

	if got.warning, err = github.CheckScopes(got.inbox.OAuthScopes); err != nil {
		return refreshed{}, err
	}

	if err = s.readMoreDetails(ctx, tok, &got, listed, asked, last); err != nil {
		return refreshed{}, err
	}

	var (
		g      errgroup.Group
		files  map[string]fileCheck
		avatar *downloadedAvatar
	)

	g.Go(func() error {
		var checkErr error

		files, checkErr = s.checkFiles(ctx, tok, got)

		return checkErr
	})

	g.Go(func() error {
		avatar = s.downloadAvatar(ctx, got.inbox.Viewer.AvatarURL)

		return nil
	})

	if err = g.Wait(); err != nil {
		return refreshed{}, err
	}

	got.files, got.avatar = files, avatar

	return got, nil
}

// readMoreDetails reads, once the token's scopes are known, the details the first
// request left out: those of the items new to the lists. When this refresh reads projects
// and the listed items' details were read without them, or the other way round, as when
// the read:project scope was just granted, it reads every item's details again, so all of
// them agree. It records in got whether the projects were read.
func (s *Service) readMoreDetails(
	ctx context.Context, tok github.Token, got *refreshed, listed []string, asked github.DetailOptions, last ProjectAccess,
) error {
	listedRead := asked.Projects && !got.details.ProjectsRefused
	read := github.DetailOptions{
		Projects: readsProjects(github.ProjectsScope(got.inbox.OAuthScopes), last) && !got.details.ProjectsRefused,
	}

	ids := nodeIDs(got.inbox)

	pending := newIDs(ids, listed)
	if read.Projects != listedRead {
		pending = ids
	}

	if len(pending) > 0 {
		more, err := s.client.FetchDetails(ctx, tok, pending, read)
		if err != nil {
			return fmt.Errorf("read the item details: %w", err)
		}

		got.details.Add(more)
	}

	got.projects = accessOf(read, got.details.ProjectsRefused)

	return nil
}

// refreshed is what one refresh read from GitHub.
type refreshed struct {
	inbox   *github.Inbox
	limits  *github.RateLimits
	details *github.Details

	// warning is set when the token's scopes could not be verified.
	warning github.ScopeWarning

	// projects is whether the details include the projects each issue is in.
	projects ProjectAccess

	// archived holds the items left out because their repository is archived.
	archived []github.Item

	// files holds the outcome of each file check the refresh ran, by node ID.
	files map[string]fileCheck

	// avatar is the viewer's avatar when it was downloaded, or nil when the one cached is
	// current or the download failed.
	avatar *downloadedAvatar
}

// downloadedAvatar is the viewer's avatar as a data URL, and where it came from.
type downloadedAvatar struct {
	dataURL string
	source  string
}

// fetch gets the inbox, the rate limits, and the details of the listed items, with what
// opts adds, concurrently. details is never nil.
func (s *Service) fetch(ctx context.Context, tok github.Token, listed []string, opts github.DetailOptions) (refreshed, error) {
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
			details, err := s.client.FetchDetails(gctx, tok, listed, opts)
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

	s.viewers.remember(tok, got.inbox.Viewer.Login)

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

// save stores what the refresh with seq read, in one transaction under the store lock:
// the inbox and its items' details, the repositories of items left out, the rate
// readings, and the avatar, then the item writes stored while it was fetching that came
// from later fetches (see order.go). An item whose own refresh is among those keeps the
// details and file check it stored, so the snoozes woken and the arrivals told are judged
// on its newer details. It returns the items that newly need the user (see Arrival),
// leaving out any such a later write took out of the lists.
func (s *Service) save(ctx context.Context, seq uint64, got refreshed) ([]Arrival, error) {
	var items map[string]github.ItemDetails
	if got.details != nil {
		items = got.details.Items
	}

	s.order.mu.Lock()
	defer s.order.mu.Unlock()

	replays := s.takeReplays(seq)

	var (
		arrivals []Arrival
		w        *writer
	)

	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		w = s.newWriter(tx)
		w.details, w.files, w.keep = items, got.files, rewritten(replays)

		var err error

		if arrivals, err = w.saveRefresh(ctx, got); err != nil {
			return err
		}

		unlisted, err := s.replay(ctx, tx, replays)
		arrivals = slices.DeleteFunc(arrivals, func(a Arrival) bool { return unlisted[a.ItemID] })

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("save the inbox: %w", err)
	}

	s.storedBy(seq, w)

	return arrivals, nil
}

// saveRefresh stores what a refresh read, in the writer's transaction, and returns the
// items that newly need the user.
func (w *writer) saveRefresh(ctx context.Context, got refreshed) ([]Arrival, error) {
	inbox := got.inbox

	if err := w.saveAccount(ctx, inbox.Viewer.Login); err != nil {
		return nil, err
	}

	watch, err := w.watchArrivals(ctx, inbox)
	if err != nil {
		return nil, err
	}

	if err = w.saveRepos(ctx, got.archived); err != nil {
		return nil, err
	}

	if err = w.saveItems(ctx, inbox); err != nil {
		return nil, err
	}

	arrivals, err := w.arrivals(ctx, watch)
	if err != nil {
		return nil, err
	}

	if err = w.saveRates(ctx, graphQLReading(inbox.RateLimit, got.details), got.limits); err != nil {
		return nil, err
	}

	status := map[string]string{
		prefViewer:               inbox.Viewer.Login,
		prefViewerName:           inbox.Viewer.Name,
		prefViewerURL:            inbox.Viewer.URL,
		prefViewerAvatarURL:      inbox.Viewer.AvatarURL,
		prefTotalReviewRequested: strconv.Itoa(inbox.ReviewRequested.IssueCount),
		prefTotalAuthored:        strconv.Itoa(inbox.Authored.IssueCount),
		prefTotalAssigned:        strconv.Itoa(inbox.Assigned.IssueCount),
		prefLastRefreshAt:        clock.Format(w.now),
		prefLastError:            "",
		prefScopeWarning:         string(got.warning),
		prefProjects:             string(got.projects),
	}

	// The source is written with the picture, so it never names a picture not stored.
	if a := got.avatar; a != nil {
		status[prefAvatar], status[prefAvatarSource] = a.dataURL, a.source
	}

	return arrivals, w.prefs.SetMany(ctx, status)
}

// downloadAvatar downloads the viewer's avatar from avatarURL, unless the one cached
// already came from there. It is best effort: the picture is cosmetic, so a failed
// download is logged at Debug and returns nil, the cache stays as it was, the UI shows
// the account's initial instead, and the next refresh tries again.
func (s *Service) downloadAvatar(ctx context.Context, avatarURL string) *downloadedAvatar {
	if avatarURL == "" {
		return nil
	}

	source, _, err := s.prefs.Get(ctx, prefAvatarSource)
	if err != nil {
		s.logger.DebugContext(ctx, "could not read where the cached avatar came from", "error", err)

		return nil
	}

	if source == avatarURL {
		return nil
	}

	fetchCtx, cancel := context.WithTimeout(ctx, avatarTimeout)
	defer cancel()

	avatar, err := s.client.FetchAvatar(fetchCtx, avatarURL)
	if err != nil {
		s.logger.DebugContext(ctx, "could not download the avatar", "error", err)

		return nil
	}

	return &downloadedAvatar{dataURL: avatar.DataURL(), source: avatarURL}
}

// recordFailure stores what the user is told about a failed refresh as the last error.
func (s *Service) recordFailure(ctx context.Context, err error) error {
	if recErr := s.prefs.Set(ctx, prefLastError, uierr.Message(err)); recErr != nil {
		return fmt.Errorf("record the refresh error: %w", recErr)
	}

	return nil
}

// newWriter returns a writer for one change stored in tx: its queries and its prefs run
// in tx, so everything it writes commits or rolls back together.
func (s *Service) newWriter(tx *sql.Tx) *writer {
	return newWriter(s.q.WithTx(tx), s.prefs.WithTx(tx), s.now())
}

// notify tells the events sink that the inbox changed.
func (s *Service) notify() {
	s.events.Emit(event.InboxUpdated{})
}

// now returns the current time in UTC, which keeps stored timestamps sortable.
func (s *Service) now() time.Time {
	return s.clock.Now().UTC()
}

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
		for _, it := range items {
			key := it.Ref().Key()

			e, ok := byKey[key]
			if !ok {
				e = &entry{item: it}
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

// graphQLReading is the GraphQL budget a refresh leaves, with the cost of every query it
// sent: the latest reading of the inbox query's and the details requests' (see
// QueryRate.Later), since the inbox query runs alongside the first details requests and
// can answer after them; or the inbox query's alone when no details were asked for or
// GitHub sent no budget with them.
func graphQLReading(inbox github.QueryRate, details *github.Details) github.QueryRate {
	if details == nil || details.Requests == 0 || details.RateLimit.Limit == 0 {
		return inbox
	}

	reading := details.RateLimit
	if inbox.Later(reading) {
		reading = inbox
	}

	reading.Cost = inbox.Cost + details.RateLimit.Cost

	return reading
}
