package issues

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/flight"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// refreshAfter is how long a read from GitHub counts as current, so a sheet opened again at
// once asks nothing, unless the inbox card shows activity since.
const refreshAfter = 20 * time.Second

// ErrNotIssue is returned for an item that is not an issue.
var ErrNotIssue = errors.New("the item is not an issue")

// StoredIssue is an issue as the inbox stored it, with its repository's default branch and
// the user's access to it. Its fields match inbox.IssueItem's, so the core converts one to
// the other.
type StoredIssue struct {
	ID     int64
	Kind   item.Kind
	Number int
	NodeID string
	Title  string
	URL    string

	AuthorLogin       string
	AuthorType        string
	AuthorAssociation string

	CreatedAt time.Time
	UpdatedAt time.Time

	// Assigned reports that the issue is in the Assigned list, the only list an issue is in.
	Assigned bool

	Owner         string
	Name          string
	DefaultBranch string

	// ViewerPermission is the user's access to the repository: ADMIN, MAINTAIN, WRITE,
	// TRIAGE, or READ.
	ViewerPermission string

	// Viewer is the account whose inbox lists the issue.
	Viewer string

	HasDetails bool

	Labels   []github.Label
	Comments int
	Projects []github.ProjectItem

	SubIssues github.SubIssues

	DetailsAt time.Time
}

// ref names the issue.
func (i StoredIssue) ref() item.Ref {
	return item.Ref{Repo: repo.Name{Owner: i.Owner, Repo: i.Name}, Number: i.Number, Kind: item.KindIssue}
}

// Target is the issue a read or an action is for, as the inbox's Actor hands it over. Its
// fields match inbox.IssueTarget's.
type Target struct {
	ID        int64
	Ref       item.Ref
	NodeID    string
	Login     string
	UpdatedAt time.Time
	Assigned  bool

	// Projects says to read the issue's projects, as the inbox's last whole refresh could.
	Projects bool
}

// IssueRead is what a read found: the item as GitHub answered, which rewrites the inbox
// card. Its fields match inbox.IssueRead's.
type IssueRead struct {
	Update *github.ItemUpdate
}

// Acted is what an action made happen on GitHub. Its fields match inbox.IssueActed's.
type Acted struct {
	// Closed reports a close GitHub made, or found made: the issue leaves every list.
	Closed bool

	// Own reports that the close was the user's own and went through, not one someone
	// made first that the action found.
	Own bool
}

// Inbox is what the sheet needs of the inbox: a stored issue, and reads and actions run with
// the inbox's footing (inbox.Actor): the item's lock, the account check, and a token, borrowed
// again once when GitHub refuses one.
type Inbox interface {
	Issue(ctx context.Context, id int64) (StoredIssue, error)
	Read(ctx context.Context, id int64, read func(context.Context, github.Token, Target) (IssueRead, error)) error
	Act(ctx context.Context, id int64, act func(context.Context, github.Token, Target) (Acted, error)) error
	Peek(ctx context.Context, id int64, peek func(context.Context, github.Token, Target) error) error
}

// GitHub is the GitHub client's reads and writes the sheet uses. *github.Client implements
// it.
type GitHub interface {
	FetchIssueView(ctx context.Context, tok github.Token, id string, opts github.DetailOptions) (*github.IssueView, error)
	FetchIssueState(ctx context.Context, tok github.Token, id string) (*github.IssueState, error)
	FetchIssueTimeline(ctx context.Context, tok github.Token, id, before string) (*github.TimelinePage, error)
	AddComment(ctx context.Context, tok github.Token, ref item.Ref, body string) (*github.Comment, error)
	CloseIssue(ctx context.Context, tok github.Token, ref item.Ref, reason string) (*github.IssueClosed, error)
	FetchRepoLabels(ctx context.Context, tok github.Token, id string) (*github.RepoLabels, error)
	FetchAssignable(ctx context.Context, tok github.Token, id, query string) ([]github.Assignable, error)
	AddLabels(ctx context.Context, tok github.Token, ref item.Ref, names []string) ([]github.Label, error)
	RemoveLabel(ctx context.Context, tok github.Token, ref item.Ref, name string) ([]github.Label, error)
	AddAssignees(ctx context.Context, tok github.Token, ref item.Ref, logins []string) (*github.Edited, error)
	RemoveAssignee(ctx context.Context, tok github.Token, ref item.Ref, login string) (*github.Edited, error)
}

// Replies is the prompt library's replies the composer offers: the comment replies that
// apply to issues, each filled in for an issue as a launch on it fills a prompt.
type Replies interface {
	Offered(ctx context.Context) ([]Reply, error)
	Fill(ctx context.Context, replyID, itemID int64) (string, error)
}

// Pictures serves the profile pictures of the people the sheet shows, as data: URLs keyed
// by login. *avatars.Service implements it.
type Pictures interface {
	Pictures(ctx context.Context, logins []string) (map[string]string, error)
}

// Config holds a Service's dependencies. Every one but Pictures, Events, Clock, and Logger
// is required.
type Config struct {
	// Pool is the database, which holds the sheet's cache.
	Pool *sql.DB

	Inbox   Inbox
	GitHub  GitHub
	Replies Replies

	// Pictures serves the people's profile pictures; without it the sheet shows none.
	Pictures Pictures

	// Events hears what an action is doing as it goes (event.IssueProgress), for the sheet
	// to show while the user waits.
	Events event.Sink

	Clock  clock.Clock
	Logger *slog.Logger
}

// Service is the issue sheet as the window binds it. It is safe for concurrent use.
type Service struct {
	inbox    Inbox
	github   GitHub
	replies  Replies
	pictures Pictures
	cache    *cacheStore

	// labels and searches keep what the pickers read, and labelReads and searchReads make
	// one read of each at a time.
	labels      *memo[RepoLabels]
	searches    *memo[[]github.Assignable]
	labelReads  flight.Group[RepoLabels]
	searchReads flight.Group[[]github.Assignable]
	events      event.Sink
	clock       clock.Clock
	logger      *slog.Logger

	// refreshes reads each issue once at a time.
	refreshes flight.Group[Issue]
}

// NewService returns a Service over cfg.
func NewService(cfg Config) *Service {
	return &Service{
		inbox: cfg.Inbox, github: cfg.GitHub, replies: cfg.Replies, pictures: cfg.Pictures, cache: newCacheStore(cfg.Pool),
		labels: &memo[RepoLabels]{size: labelRepos}, searches: &memo[[]github.Assignable]{size: keptSearches},
		events: event.Or(cfg.Events),
		clock:  clock.Or(cfg.Clock), logger: logging.Or(cfg.Logger),
	}
}

// Open returns the issue with id as the sheet last read it, from its cache and the inbox,
// with no request: at once, and offline. It starts a visit: LastSeenAt is GitHub's time of
// the newest activity in the read it shows, so what a refresh brings that is newer is new
// to the user, and every Refresh and ReadAgain returns the same until the next Open. Stale
// says a Refresh is due.
func (s *Service) Open(ctx context.Context, id int64) (Issue, error) {
	card, c, err := s.load(ctx, id)
	if err != nil {
		return Issue{}, err
	}

	if c == nil {
		return build(card, nil, "", false), nil
	}

	// A read that doesn't say when it was leaves the last visit's as it was.
	at, ok := parseStamp(c.view.UpdatedAt)
	if !ok {
		at, ok = parseStamp(c.seenAt)
	}

	var seenAt *time.Time

	c.seenAt = ""
	if ok {
		seenAt, c.seenAt = &at, clock.Format(at)
	}

	if err = s.cache.open(ctx, id, seenAt, s.now()); err != nil {
		s.logger.DebugContext(ctx, "could not stamp the cached issue", "error", err)
	}

	return build(card, c, c.seenAt, false), nil
}

// Refresh reads the issue with id from GitHub, stores it, and returns it: one GraphQL
// request, whose answer rewrites the inbox card too. An issue read in the last 20 seconds is
// not read again, unless its card shows activity since. Two reads of one issue never run
// side by side. An issue the read finds closed, or that GitHub can't find any more, is
// returned as read this once, and the sheet forgets it.
func (s *Service) Refresh(ctx context.Context, id int64) (Issue, error) {
	return s.refresh(ctx, id, false)
}

// ReadAgain reads the issue with id from GitHub as Refresh does, however recently it was
// read: the user asked for it, from the sheet's refresh button. A read already under way
// for the issue is joined rather than run beside it.
func (s *Service) ReadAgain(ctx context.Context, id int64) (Issue, error) {
	return s.refresh(ctx, id, true)
}

// Earlier reads the page of the issue's timeline before the cursor before, which a page
// the sheet holds gave: one GraphQL request, which stores nothing, not even in the cache.
func (s *Service) Earlier(ctx context.Context, id int64, before string) (TimelinePage, error) {
	card, err := s.issue(ctx, id)
	if err != nil {
		return TimelinePage{}, err
	}

	var page *github.TimelinePage

	err = s.inbox.Peek(ctx, id, func(ctx context.Context, tok github.Token, t Target) error {
		got, readErr := s.github.FetchIssueTimeline(ctx, tok, t.NodeID, before)
		page = got

		return readErr
	})

	switch {
	case errors.Is(err, github.ErrInvalidCursor):
		return TimelinePage{}, uierr.New("That part of the conversation can't be read. Reload the issue, then try again.",
			err)
	case err != nil:
		return TimelinePage{}, err
	}

	return TimelinePage{
		Items: timelineItems(page.Items, card.Viewer), Total: max(page.Total, len(page.Items)), Before: page.Before,
	}, nil
}

// refresh reads the issue with id from GitHub, one read of it at a time: a read already
// under way is joined, so every caller, one that forces the read included, gets an answer
// read from GitHub. Without force, a read in the last refreshAfter whose card shows nothing
// newer is served as it is, and no read is made.
func (s *Service) refresh(ctx context.Context, id int64, force bool) (Issue, error) {
	if !force {
		card, c, err := s.load(ctx, id)
		if err != nil {
			return Issue{}, err
		}

		if !stale(card, c) && s.now().Sub(c.fetchedAt) < refreshAfter {
			return build(card, c, c.seenAt, false), nil
		}
	}

	return s.refreshes.Do(ctx, strconv.FormatInt(id, 10), func(ctx context.Context) (Issue, error) {
		return s.read(ctx, id)
	})
}

// read reads the issue with id from GitHub through the inbox, which rewrites the card from
// the answer, and keeps the rest in the cache under the inbox's lock on the item, so no
// action on the issue runs meanwhile.
func (s *Service) read(ctx context.Context, id int64) (Issue, error) {
	card, c, err := s.load(ctx, id)
	if err != nil {
		return Issue{}, err
	}

	var closing *Issue

	err = s.inbox.Read(ctx, id, func(ctx context.Context, tok github.Token, t Target) (IssueRead, error) {
		view, readErr := s.github.FetchIssueView(ctx, tok, t.NodeID, github.DetailOptions{Projects: t.Projects})
		if readErr != nil {
			return IssueRead{}, readErr
		}

		shown, keepErr := s.keep(ctx, card, c, view)
		if keepErr != nil {
			return IssueRead{}, keepErr
		}

		closing = shown

		return IssueRead{Update: &view.Update}, nil
	})
	if err != nil {
		return Issue{}, err
	}

	if closing != nil {
		return *closing, nil
	}

	if card, c, err = s.load(ctx, id); err != nil {
		return Issue{}, err
	}

	return build(card, c, c.lastSeen(), false), nil
}

// keep stores what a read found: the view of an open issue, in the cache. An issue closed,
// or that GitHub can't find, leaves the cache, and is returned built from the read, for the
// sheet to show this once; c is the cache before the read.
func (s *Service) keep(ctx context.Context, card StoredIssue, c *cached, view *github.IssueView) (*Issue, error) {
	u := &view.Update
	if u.Found && (u.Item.NodeID != card.NodeID || u.Item.IsPR) {
		return nil, fmt.Errorf("%w: the answer is about another item than %d", github.ErrMalformedResponse, card.ID)
	}

	if u.Found && !u.Item.Closed() {
		return nil, s.cache.save(ctx, card.ID, viewOf(view, card.Viewer), s.now())
	}

	if err := s.cache.drop(ctx, card.ID); err != nil {
		s.logger.DebugContext(ctx, "could not forget the closed issue", "error", err)
	}

	if !u.Found {
		is := build(card, c, c.lastSeen(), true)
		is.Stale, is.FetchedAt = false, clock.Format(s.now())

		return &is, nil
	}

	read := &cached{view: viewOf(view, card.Viewer), fetchedAt: s.now()}
	is := build(cardOf(card, u), read, c.lastSeen(), false)

	return &is, nil
}

// load reads the stored issue with id and the sheet's cache of it, nil when it never read
// the issue.
func (s *Service) load(ctx context.Context, id int64) (StoredIssue, *cached, error) {
	card, err := s.issue(ctx, id)
	if err != nil {
		return StoredIssue{}, nil, err
	}

	c, err := s.cache.load(ctx, id)
	if err != nil {
		return StoredIssue{}, nil, err
	}

	return card, c, nil
}

// issue reads the stored issue with id.
func (s *Service) issue(ctx context.Context, id int64) (StoredIssue, error) {
	card, err := s.inbox.Issue(ctx, id)
	if err != nil {
		return StoredIssue{}, err
	}

	if card.Kind != item.KindIssue || card.NodeID == "" {
		return StoredIssue{}, fmt.Errorf("%w (id %d)", ErrNotIssue, id)
	}

	return card, nil
}

// now returns the current time in UTC.
func (s *Service) now() time.Time {
	return s.clock.Now().UTC()
}
