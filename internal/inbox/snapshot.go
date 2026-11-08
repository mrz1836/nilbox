package inbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/inbox/internal/queries"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/snooze"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/trust"
)

// githubWebURL is the root of GitHub's website, for a profile link stored before the
// profile URL was.
const githubWebURL = "https://github.com/"

// Snapshot is the stored inbox as the UI shows it. Every field is a plain value.
type Snapshot struct {
	// Account is the signed-in account (all "" before the first refresh).
	Account Account `json:"account"`

	ReviewRequested List `json:"reviewRequested"`
	Authored        List `json:"authored"`
	Assigned        List `json:"assigned"`

	Rate RateGauge `json:"rate"`

	// LastRefreshedAt is the RFC 3339 time of the last successful refresh ("" if none).
	LastRefreshedAt string `json:"lastRefreshedAt"`

	// LastError is the last refresh's error message ("" when it succeeded).
	LastError string `json:"lastError"`

	// ScopeWarning is set when the token's scopes could not be verified.
	ScopeWarning string `json:"scopeWarning"`
}

// Account is the signed-in GitHub account.
type Account struct {
	// Login is the account's username ("" before the first refresh).
	Login string `json:"login"`

	// Name is the profile's display name ("" when the profile sets none).
	Name string `json:"name"`

	// ProfileURL is the account's page on GitHub.
	ProfileURL string `json:"profileUrl"`

	// Avatar is the profile picture as a data: URL, cached locally. It is "" until the
	// current avatar has been downloaded.
	Avatar string `json:"avatar"`
}

// List is one inbox list. Total is GitHub's match count, which can exceed Shown.
type List struct {
	Items []Item `json:"items"`
	Shown int    `json:"shown"`
	Total int    `json:"total"`
}

// Item is one pull request or issue in a list.
type Item struct {
	ID     int64     `json:"id"`
	Kind   item.Kind `json:"kind"`
	Number int       `json:"number"`
	Title  string    `json:"title"`
	URL    string    `json:"url"`

	// Repo is "owner/name".
	Repo   string `json:"repo"`
	Author string `json:"author"`

	// UpdatedAt is the item's last activity on GitHub, as RFC 3339.
	UpdatedAt string `json:"updatedAt"`

	// Trust is the item's trust class (see the trust package): "member" for the
	// repository's owner, org members, and collaborators, "bot" for a trusted bot, and
	// "outside" for everyone else, every untrusted bot, and every fork.
	Trust trust.Class `json:"trust"`

	// Trusted reports whether Trust is anything but "outside".
	Trusted bool `json:"trusted"`

	// AuthorIsBot reports whether GitHub reports the author as a bot.
	AuthorIsBot bool `json:"authorIsBot"`

	IsCrossRepository bool   `json:"isCrossRepository"`
	AuthorAssociation string `json:"authorAssociation"`

	// Branch is a pull request's head branch ("" for issues).
	Branch  string `json:"branch"`
	IsDraft bool   `json:"isDraft"`

	// Details is nil until a refresh has read the item's details.
	Details *Details `json:"details"`

	// Files is what checking a pull request's changed files found, with the user's
	// categories applied, or nil for an item whose files are not checked: an issue, or a
	// pull request from the repository's own people, a trusted bot, or the signed-in user
	// (see the sensitive package).
	Files *sensitive.Check `json:"files"`

	// DismissedAt is when the item was dismissed, as RFC 3339, or "" when it is not. A
	// dismissed item stays in its lists here, and the UI sets it apart until it is
	// restored.
	DismissedAt string `json:"dismissedAt"`

	// SnoozedUntil is when the item's snooze ends, as RFC 3339, or "" when it was not
	// snoozed. The UI sets the item apart until then; a time already passed means it is
	// back from a snooze.
	SnoozedUntil string `json:"snoozedUntil"`

	// WokeBy is the new activity that ended the item's snooze early, at SnoozedUntil, or
	// nil when its snooze ran its course, is still running, or there was none.
	WokeBy *snooze.Reasons `json:"wokeBy"`

	// PinnedAt is when the item was pinned, as RFC 3339, or "" when it is not. Pinned
	// items come first in each list, the most recently pinned first. A pin outlasts a
	// snooze or a dismissal.
	PinnedAt string `json:"pinnedAt"`
}

// Details is what an item's inbox card shows beyond its title. The pull request fields
// are zero, or "", for an issue.
type Details struct {
	Labels []Label `json:"labels"`

	// Comments counts the conversation's comments, review comments included for a pull
	// request.
	Comments int `json:"comments"`

	Additions    int `json:"additions"`
	Deletions    int `json:"deletions"`
	ChangedFiles int `json:"changedFiles"`
	Commits      int `json:"commits"`

	// Mergeable is MERGEABLE, CONFLICTING, or UNKNOWN while GitHub works it out.
	Mergeable string `json:"mergeable"`

	Checks  Checks  `json:"checks"`
	Reviews Reviews `json:"reviews"`

	// FetchedAt is when the details were read, as RFC 3339.
	FetchedAt string `json:"fetchedAt"`
}

// Label is one label on an item.
type Label struct {
	Name string `json:"name"`

	// Color is "#rrggbb", or "" when GitHub's value was not a color.
	Color string `json:"color"`
}

// Checks counts the head commit's check runs and commit statuses by outcome.
type Checks struct {
	// State is GitHub's overall result: SUCCESS, FAILURE, ERROR, PENDING, or EXPECTED,
	// and "" when the head commit has no checks.
	State string `json:"state"`

	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Pending int `json:"pending"`
	Skipped int `json:"skipped"`
}

// Reviews counts who was asked to review a pull request and who has.
type Reviews struct {
	// Decision is APPROVED, CHANGES_REQUESTED, or REVIEW_REQUIRED, and "" when the
	// repository requires no review.
	Decision string `json:"decision"`

	// Reviewers is everyone asked to review or who has, the author excluded; Reviewed
	// is how many of them have reviewed and are not asked to review again.
	Reviewers int `json:"reviewers"`
	Reviewed  int `json:"reviewed"`

	// Approvals and ChangesRequested count the reviewers whose latest review is one.
	Approvals        int `json:"approvals"`
	ChangesRequested int `json:"changesRequested"`
}

// RateGauge holds the latest reading of each budget.
type RateGauge struct {
	GraphQL Rate `json:"graphql"`
	Core    Rate `json:"core"`
	Search  Rate `json:"search"`
}

// Rate is one budget reading. Known is false until the first refresh records one.
type Rate struct {
	Known     bool `json:"known"`
	Limit     int  `json:"limit"`
	Remaining int  `json:"remaining"`
	Used      int  `json:"used"`

	// Cost is the point cost of the inbox query (GraphQL only).
	Cost int `json:"cost"`

	ResetAt string `json:"resetAt"`
	TakenAt string `json:"takenAt"`
}

// Snapshot returns the stored inbox: the three lists, the latest rate readings, and the
// refresh status.
//
// The snapshot is kept until the next write, and only built again when something changed
// what it shows: reading a kept one costs a read of the settings it depends on. The
// lists of every snapshot returned share their items with the one kept, so a caller
// must not change them.
func (s *Service) Snapshot(ctx context.Context) (Snapshot, error) {
	seen := s.snapshots.load()

	if seen != nil && seen.snap != nil {
		stored, err := s.prefs.GetMany(ctx, settingsPrefs()...)
		if err != nil {
			return Snapshot{}, fmt.Errorf("read the inbox status: %w", err)
		}

		if keyOf(stored) == seen.settings {
			return *seen.snap, nil
		}
	}

	snap, settings, err := s.buildSnapshot(ctx)
	if err != nil {
		return snap, err
	}

	s.snapshots.keep(seen, snap, settings)

	return snap, nil
}

// buildSnapshot reads the stored inbox, and returns it with the settings it was built
// under. When the account cannot be read, the snapshot returned holds the status alone.
func (s *Service) buildSnapshot(ctx context.Context) (Snapshot, settingsKey, error) {
	status, err := readStatus(ctx, s.prefs)
	if err != nil {
		return Snapshot{}, settingsKey{}, err
	}

	settings := keyOf(status)
	snap := newSnapshot(status)

	accountID, err := lookupAccount(ctx, s.q, status[prefViewer])
	if err != nil || accountID == 0 {
		return snap, settings, err
	}

	rows, err := loadItems(ctx, s.q, accountID)
	if err != nil {
		return Snapshot{}, settingsKey{}, err
	}

	snap.addItems(rows, s.scopes.scope(status))
	snap.applyTotals(status)

	if snap.Rate, err = loadRates(ctx, s.q, accountID); err != nil {
		return Snapshot{}, settingsKey{}, err
	}

	return snap, settings, nil
}

// newSnapshot starts a snapshot with empty lists and the stored status.
func newSnapshot(prefs map[string]string) Snapshot {
	return Snapshot{
		Account:         accountFromPrefs(prefs),
		ReviewRequested: List{Items: []Item{}},
		Authored:        List{Items: []Item{}},
		Assigned:        List{Items: []Item{}},
		LastRefreshedAt: prefs[prefLastRefreshAt],
		LastError:       prefs[prefLastError],
		ScopeWarning:    prefs[prefScopeWarning],
	}
}

// accountFromPrefs reads the signed-in account. The cached avatar is used only while it
// is the picture GitHub last reported, so another account's avatar never shows.
func accountFromPrefs(prefs map[string]string) Account {
	login := prefs[prefViewer]
	if login == "" {
		return Account{}
	}

	account := Account{Login: login, Name: prefs[prefViewerName], ProfileURL: prefs[prefViewerURL]}

	if account.ProfileURL == "" {
		account.ProfileURL = githubWebURL + login
	}

	if source := prefs[prefAvatarSource]; source != "" && source == prefs[prefViewerAvatarURL] {
		account.Avatar = prefs[prefAvatar]
	}

	return account
}

// addItems sorts rows into the lists whose flags they carry, deciding each item's trust,
// and whether its files are checked, with scope.
func (s *Snapshot) addItems(rows []itemRow, scope fileScope) {
	for _, r := range rows {
		item := r.item(scope)

		if r.ReasonReviewRequested {
			s.ReviewRequested.Items = append(s.ReviewRequested.Items, item)
		}

		if r.ReasonAuthored {
			s.Authored.Items = append(s.Authored.Items, item)
		}

		if r.ReasonAssigned {
			s.Assigned.Items = append(s.Assigned.Items, item)
		}
	}
}

// applyTotals sets each list's Shown and Total. A stored total below the shown count
// (or no stored total) falls back to the shown count.
func (s *Snapshot) applyTotals(prefs map[string]string) {
	for _, l := range []struct {
		list *List
		key  string
	}{
		{&s.ReviewRequested, prefTotalReviewRequested},
		{&s.Authored, prefTotalAuthored},
		{&s.Assigned, prefTotalAssigned},
	} {
		l.list.Shown = len(l.list.Items)
		total, _ := strconv.Atoi(prefs[l.key]) // a missing or bad total falls back to Shown
		l.list.Total = max(total, l.list.Shown)
	}
}

// itemRow is one listed item as a snapshot reads it, with the columns of missing
// details or a missing file check at their zero values.
type itemRow struct {
	ID                    int64
	Kind                  item.Kind
	Number                int
	Title                 string
	URL                   string
	RepoOwner             string
	RepoName              string
	AuthorLogin           string
	AuthorType            string
	AuthorAssociation     string
	IsCrossRepository     bool
	IsDraft               bool
	HeadRef               string
	ItemUpdatedAt         time.Time
	ReasonReviewRequested bool
	ReasonAuthored        bool
	ReasonAssigned        bool

	// HasDetails reports whether a refresh has read the item's details.
	HasDetails       bool
	Labels           []storedLabel
	Comments         int
	Additions        int
	Deletions        int
	ChangedFiles     int
	Commits          int
	Mergeable        string
	ChecksState      string
	ChecksPassed     int
	ChecksFailed     int
	ChecksPending    int
	ChecksSkipped    int
	ReviewDecision   string
	Reviewers        int
	Reviewed         int
	Approvals        int
	ChangesRequested int
	DetailsFetchedAt *time.Time
	HeadOID          string

	// Stored is the item's last file check, or nil when there is none.
	Stored *sensitive.Result

	DismissedAt  *time.Time
	SnoozedUntil *time.Time
	WakeReasons  *string
	PinnedAt     *time.Time
}

// itemRowOf converts a ListItems row, decoding its labels and its file check.
func itemRowOf(r queries.ListItemsRow) (itemRow, error) {
	var labels []storedLabel

	if r.Labels != nil {
		decoded, err := store.DecodeJSON[[]storedLabel](*r.Labels)
		if err != nil {
			return itemRow{}, fmt.Errorf("read the labels of item %d: %w", r.ID, err)
		}

		labels = decoded
	}

	stored, err := filesColumns{
		itemID: r.FilesItemID, head: r.FilesHeadOID, total: r.FilesTotal, files: r.Files,
		complete: r.FilesComplete, err: r.FilesError, checkedAt: r.FilesCheckedAt,
	}.result()
	if err != nil {
		return itemRow{}, fmt.Errorf("item %d: %w", r.ID, err)
	}

	return itemRow{
		ID: r.ID, Kind: item.Kind(r.Kind), Number: int(r.Number), Title: r.Title, URL: r.URL,
		RepoOwner: r.RepoOwner, RepoName: r.RepoName, AuthorLogin: r.AuthorLogin, AuthorType: r.AuthorType,
		AuthorAssociation: r.AuthorAssociation, IsCrossRepository: r.IsCrossRepository, IsDraft: r.IsDraft,
		HeadRef: r.HeadRef, ItemUpdatedAt: r.ItemUpdatedAt, ReasonReviewRequested: r.ReasonReviewRequested,
		ReasonAuthored: r.ReasonAuthored, ReasonAssigned: r.ReasonAssigned,

		HasDetails: r.DetailsItemID != nil, Labels: labels, Comments: int(value(r.Comments)),
		Additions: int(value(r.Additions)), Deletions: int(value(r.Deletions)),
		ChangedFiles: int(value(r.ChangedFiles)), Commits: int(value(r.Commits)), Mergeable: value(r.Mergeable),
		ChecksState: value(r.ChecksState), ChecksPassed: int(value(r.ChecksPassed)),
		ChecksFailed: int(value(r.ChecksFailed)), ChecksPending: int(value(r.ChecksPending)),
		ChecksSkipped: int(value(r.ChecksSkipped)), ReviewDecision: value(r.ReviewDecision),
		Reviewers: int(value(r.Reviewers)), Reviewed: int(value(r.Reviewed)), Approvals: int(value(r.Approvals)),
		ChangesRequested: int(value(r.ChangesRequested)), DetailsFetchedAt: r.DetailsFetchedAt,
		HeadOID: value(r.HeadOID),

		Stored: stored,

		DismissedAt: r.DismissedAt, SnoozedUntil: r.SnoozedUntil, WakeReasons: r.WakeReasons, PinnedAt: r.PinnedAt,
	}, nil
}

// item converts a row to its DTO, deciding its trust, and whether its files are
// checked, with scope.
func (r *itemRow) item(scope fileScope) Item {
	isPR := r.Kind == item.KindPR
	author := trust.Author{Login: r.AuthorLogin, Type: r.AuthorType, Association: r.AuthorAssociation}
	class := scope.policy.Classify(author, isPR, r.IsCrossRepository)

	return Item{
		ID:                r.ID,
		Kind:              r.Kind,
		Number:            r.Number,
		Title:             r.Title,
		URL:               r.URL,
		Repo:              repo.Name{Owner: r.RepoOwner, Repo: r.RepoName}.String(),
		Author:            r.AuthorLogin,
		UpdatedAt:         clock.Format(r.ItemUpdatedAt),
		Trust:             class,
		Trusted:           class != trust.ClassOutside,
		AuthorIsBot:       author.IsBot(),
		IsCrossRepository: r.IsCrossRepository,
		AuthorAssociation: r.AuthorAssociation,
		Branch:            r.HeadRef,
		IsDraft:           r.IsDraft,
		Details:           r.details(),
		Files:             r.checkedFiles(scope, isPR, author),
		DismissedAt:       formatOptionalTime(r.DismissedAt),
		SnoozedUntil:      formatOptionalTime(r.SnoozedUntil),
		WokeBy:            decodeReasons(r.WakeReasons),
		PinnedAt:          formatOptionalTime(r.PinnedAt),
	}
}

// checkedFiles evaluates the row's last file check with scope's categories, or returns nil
// when its files are not checked.
func (r *itemRow) checkedFiles(scope fileScope, isPR bool, author trust.Author) *sensitive.Check {
	if !scope.checks(isPR, author, r.IsCrossRepository) {
		return nil
	}

	check := scope.matcher.Evaluate(r.Stored, r.HeadOID, r.URL)

	return &check
}

// decodeReasons reads stored wake reasons, or nil when there are none or they cannot be
// read; a card without its reasons still shows the item as back.
func decodeReasons(raw *string) *snooze.Reasons {
	if raw == nil {
		return nil
	}

	var reasons snooze.Reasons
	if json.Unmarshal([]byte(*raw), &reasons) != nil {
		return nil
	}

	// A review outcome this build does not know, as another build may have stored, is
	// read as none.
	if !reasons.Review.Valid() {
		reasons.Review = snooze.ReviewNone
	}

	if !reasons.Woke() {
		return nil
	}

	return &reasons
}

// details converts the row's details, or nil when no refresh has read them.
func (r *itemRow) details() *Details {
	if !r.HasDetails {
		return nil
	}

	labels := make([]Label, 0, len(r.Labels))
	for _, l := range r.Labels {
		labels = append(labels, Label(l))
	}

	return &Details{
		Labels:       labels,
		Comments:     r.Comments,
		Additions:    r.Additions,
		Deletions:    r.Deletions,
		ChangedFiles: r.ChangedFiles,
		Commits:      r.Commits,
		Mergeable:    r.Mergeable,
		Checks: Checks{
			State: r.ChecksState, Passed: r.ChecksPassed, Failed: r.ChecksFailed,
			Pending: r.ChecksPending, Skipped: r.ChecksSkipped,
		},
		Reviews: Reviews{
			Decision: r.ReviewDecision, Reviewers: r.Reviewers, Reviewed: r.Reviewed,
			Approvals: r.Approvals, ChangesRequested: r.ChangesRequested,
		},
		FetchedAt: formatOptionalTime(r.DetailsFetchedAt),
	}
}

// lookupAccount returns the id of the account with login, or 0 when there is none.
func lookupAccount(ctx context.Context, q *queries.Queries, login string) (int64, error) {
	if login == "" {
		return 0, nil
	}

	id, err := q.AccountID(ctx, login)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return 0, nil
	case err != nil:
		return 0, fmt.Errorf("read the account: %w", err)
	default:
		return id, nil
	}
}

// loadItems reads the account's listed items, in the order ListItems returns them.
func loadItems(ctx context.Context, q *queries.Queries, accountID int64) ([]itemRow, error) {
	stored, err := q.ListItems(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("read the inbox items: %w", err)
	}

	rows := make([]itemRow, 0, len(stored))

	for _, r := range stored {
		row, rowErr := itemRowOf(r)
		if rowErr != nil {
			return nil, fmt.Errorf("read the inbox items: %w", rowErr)
		}

		rows = append(rows, row)
	}

	return rows, nil
}

// loadRates reads the newest snapshot of each budget.
func loadRates(ctx context.Context, q *queries.Queries, accountID int64) (RateGauge, error) {
	var (
		gauge RateGauge
		err   error
	)

	for _, r := range []struct {
		dst      *Rate
		resource string
	}{
		{&gauge.GraphQL, resourceGraphQL},
		{&gauge.Core, resourceCore},
		{&gauge.Search, resourceSearch},
	} {
		if *r.dst, err = loadRate(ctx, q, accountID, r.resource); err != nil {
			return RateGauge{}, err
		}
	}

	return gauge, nil
}

// loadRate reads the newest snapshot of one budget.
func loadRate(ctx context.Context, q *queries.Queries, accountID int64, resource string) (Rate, error) {
	r, err := q.GetLatestRate(ctx, queries.GetLatestRateParams{AccountID: accountID, Resource: resource})

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Rate{}, nil
	case err != nil:
		return Rate{}, fmt.Errorf("read the %s rate limit: %w", resource, err)
	}

	return Rate{
		Known:     true,
		Limit:     int(r.LimitTotal),
		Remaining: int(r.Remaining),
		Used:      int(r.Used),
		Cost:      int(r.Cost),
		ResetAt:   clock.Format(r.ResetAt),
		TakenAt:   clock.Format(r.TakenAt),
	}, nil
}

// formatOptionalTime renders a nullable stored time as RFC 3339 in UTC, or "" when it is
// NULL.
func formatOptionalTime(t *time.Time) string {
	if t == nil {
		return ""
	}

	return clock.Format(*t)
}
