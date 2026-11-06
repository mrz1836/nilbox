package inbox

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"gorm.io/gorm"

	"github.com/mrz1836/nilbox/internal/inbox/model"
	"github.com/mrz1836/nilbox/internal/snooze"
	"github.com/mrz1836/nilbox/internal/trust"
)

// loadItemsSQL reads every item that is in at least one list, newest activity first, with
// its details when a refresh has read them (has_details) and its triage state.
const loadItemsSQL = `SELECT
    i.id, i.kind, i.number, i.title, i.url, r.owner AS repo_owner, r.name AS repo_name,
    i.author_login, i.author_type, i.author_association, i.is_cross_repository, i.is_draft, i.head_ref,
    i.item_updated_at, i.reason_review_requested, i.reason_authored, i.reason_assigned,
    d.item_id IS NOT NULL AS has_details,
    COALESCE(d.labels, '[]') AS labels, COALESCE(d.comments, 0) AS comments,
    COALESCE(d.additions, 0) AS additions, COALESCE(d.deletions, 0) AS deletions,
    COALESCE(d.changed_files, 0) AS changed_files, COALESCE(d.commits, 0) AS commits,
    COALESCE(d.mergeable, '') AS mergeable, COALESCE(d.checks_state, '') AS checks_state,
    COALESCE(d.checks_passed, 0) AS checks_passed, COALESCE(d.checks_failed, 0) AS checks_failed,
    COALESCE(d.checks_pending, 0) AS checks_pending, COALESCE(d.checks_skipped, 0) AS checks_skipped,
    COALESCE(d.review_decision, '') AS review_decision, COALESCE(d.reviewers, 0) AS reviewers,
    COALESCE(d.reviewed, 0) AS reviewed, COALESCE(d.approvals, 0) AS approvals,
    COALESCE(d.changes_requested, 0) AS changes_requested, d.fetched_at AS details_fetched_at,
    ls.dismissed_at, ls.snoozed_until, ls.wake_reasons
FROM items i
JOIN repos r ON r.id = i.repo_id
LEFT JOIN item_details d ON d.item_id = i.id
LEFT JOIN item_local_state ls ON ls.item_id = i.id
WHERE i.account_id = ?
  AND (i.reason_review_requested = 1 OR i.reason_authored = 1 OR i.reason_assigned = 1)
ORDER BY i.item_updated_at DESC, i.id DESC`

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
	ID     int64  `json:"id"`
	Kind   string `json:"kind"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`

	// Repo is "owner/name".
	Repo   string `json:"repo"`
	Author string `json:"author"`

	// UpdatedAt is the item's last activity on GitHub, as RFC 3339.
	UpdatedAt string `json:"updatedAt"`

	// Trust is the item's trust class (see the trust package): "member" for the
	// repository's owner, org members, and collaborators, "bot" for a trusted bot, and
	// "outside" for everyone else, every untrusted bot, and every fork.
	Trust string `json:"trust"`

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

// addItems sorts rows into the lists whose flags they carry, deciding each item's trust
// with policy.
func (s *Snapshot) addItems(rows []itemRow, policy trust.Policy) {
	for _, r := range rows {
		item := r.item(policy)

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

// itemRow is one row of loadItemsSQL.
type itemRow struct {
	ID                    int64     `gorm:"column:id"`
	Kind                  string    `gorm:"column:kind"`
	Number                int       `gorm:"column:number"`
	Title                 string    `gorm:"column:title"`
	URL                   string    `gorm:"column:url"`
	RepoOwner             string    `gorm:"column:repo_owner"`
	RepoName              string    `gorm:"column:repo_name"`
	AuthorLogin           string    `gorm:"column:author_login"`
	AuthorType            string    `gorm:"column:author_type"`
	AuthorAssociation     string    `gorm:"column:author_association"`
	IsCrossRepository     bool      `gorm:"column:is_cross_repository"`
	IsDraft               bool      `gorm:"column:is_draft"`
	HeadRef               string    `gorm:"column:head_ref"`
	ItemUpdatedAt         time.Time `gorm:"column:item_updated_at"`
	ReasonReviewRequested bool      `gorm:"column:reason_review_requested"`
	ReasonAuthored        bool      `gorm:"column:reason_authored"`
	ReasonAssigned        bool      `gorm:"column:reason_assigned"`

	HasDetails       bool         `gorm:"column:has_details"`
	Labels           model.Labels `gorm:"column:labels"`
	Comments         int          `gorm:"column:comments"`
	Additions        int          `gorm:"column:additions"`
	Deletions        int          `gorm:"column:deletions"`
	ChangedFiles     int          `gorm:"column:changed_files"`
	Commits          int          `gorm:"column:commits"`
	Mergeable        string       `gorm:"column:mergeable"`
	ChecksState      string       `gorm:"column:checks_state"`
	ChecksPassed     int          `gorm:"column:checks_passed"`
	ChecksFailed     int          `gorm:"column:checks_failed"`
	ChecksPending    int          `gorm:"column:checks_pending"`
	ChecksSkipped    int          `gorm:"column:checks_skipped"`
	ReviewDecision   string       `gorm:"column:review_decision"`
	Reviewers        int          `gorm:"column:reviewers"`
	Reviewed         int          `gorm:"column:reviewed"`
	Approvals        int          `gorm:"column:approvals"`
	ChangesRequested int          `gorm:"column:changes_requested"`
	DetailsFetchedAt *time.Time   `gorm:"column:details_fetched_at"`

	DismissedAt  *time.Time `gorm:"column:dismissed_at"`
	SnoozedUntil *time.Time `gorm:"column:snoozed_until"`
	WakeReasons  *string    `gorm:"column:wake_reasons"`
}

// item converts a row to its DTO, deciding its trust with policy.
func (r *itemRow) item(policy trust.Policy) Item {
	isPR := r.Kind == model.KindPR
	author := trust.Author{Login: r.AuthorLogin, Type: r.AuthorType, Association: r.AuthorAssociation}
	class := policy.Classify(author, isPR, r.IsCrossRepository)

	return Item{
		ID:                r.ID,
		Kind:              r.Kind,
		Number:            r.Number,
		Title:             r.Title,
		URL:               r.URL,
		Repo:              r.RepoOwner + "/" + r.RepoName,
		Author:            r.AuthorLogin,
		UpdatedAt:         formatTime(r.ItemUpdatedAt),
		Trust:             class,
		Trusted:           class != trust.ClassOutside,
		AuthorIsBot:       author.IsBot(),
		IsCrossRepository: r.IsCrossRepository,
		AuthorAssociation: r.AuthorAssociation,
		Branch:            r.HeadRef,
		IsDraft:           r.IsDraft,
		Details:           r.details(),
		DismissedAt:       formatOptionalTime(r.DismissedAt),
		SnoozedUntil:      formatOptionalTime(r.SnoozedUntil),
		WokeBy:            decodeReasons(r.WakeReasons),
	}
}

// decodeReasons reads stored wake reasons, or nil when there are none or they cannot be
// read; a card without its reasons still shows the item as back.
func decodeReasons(raw *string) *snooze.Reasons {
	if raw == nil {
		return nil
	}

	var reasons snooze.Reasons
	if json.Unmarshal([]byte(*raw), &reasons) != nil || !reasons.Woke() {
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
		labels = append(labels, Label{Name: l.Name, Color: l.Color})
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
func lookupAccount(db *gorm.DB, login string) (int64, error) {
	if login == "" {
		return 0, nil
	}

	var ids []int64
	if err := db.Raw("SELECT id FROM accounts WHERE login = ?", login).Scan(&ids).Error; err != nil {
		return 0, fmt.Errorf("read the account: %w", err)
	}

	if len(ids) == 0 {
		return 0, nil
	}

	return ids[0], nil
}

// loadItems reads the account's listed items.
func loadItems(db *gorm.DB, accountID int64) ([]itemRow, error) {
	var rows []itemRow
	if err := db.Raw(loadItemsSQL, accountID).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("read the inbox items: %w", err)
	}

	return rows, nil
}

// loadRates reads the newest snapshot of each budget.
func loadRates(db *gorm.DB, accountID int64) (RateGauge, error) {
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
		if *r.dst, err = loadRate(db, accountID, r.resource); err != nil {
			return RateGauge{}, err
		}
	}

	return gauge, nil
}

// loadRate reads the newest snapshot of one budget.
func loadRate(db *gorm.DB, accountID int64, resource string) (Rate, error) {
	var rows []model.RateSnapshot

	err := db.Where("account_id = ? AND resource = ?", accountID, resource).
		Order("taken_at DESC").Order("id DESC").Limit(1).Find(&rows).Error
	if err != nil {
		return Rate{}, fmt.Errorf("read the %s rate limit: %w", resource, err)
	}

	if len(rows) == 0 {
		return Rate{}, nil
	}

	r := rows[0]

	return Rate{
		Known:     true,
		Limit:     r.LimitTotal,
		Remaining: r.Remaining,
		Used:      r.Used,
		Cost:      r.Cost,
		ResetAt:   formatTime(r.ResetAt),
		TakenAt:   formatTime(r.TakenAt),
	}, nil
}

// formatTime renders a stored time as RFC 3339 in UTC.
func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// formatOptionalTime renders a nullable stored time as RFC 3339 in UTC, or "" when it is
// NULL.
func formatOptionalTime(t *time.Time) string {
	if t == nil {
		return ""
	}

	return formatTime(*t)
}
