package inbox

import (
	"fmt"
	"strconv"
	"time"

	"gorm.io/gorm"

	"github.com/mrz1836/nilbox/internal/inbox/model"
	"github.com/mrz1836/nilbox/internal/trust"
)

// loadItemsSQL reads every item that is in at least one list, newest activity first.
const loadItemsSQL = `SELECT
    i.id, i.kind, i.number, i.title, i.url, r.owner AS repo_owner, r.name AS repo_name,
    i.author_login, i.author_association, i.is_cross_repository, i.is_draft, i.head_ref,
    i.item_updated_at, i.reason_review_requested, i.reason_authored, i.reason_assigned
FROM items i
JOIN repos r ON r.id = i.repo_id
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

	// Trusted applies the outside-contributor rule to AuthorAssociation and
	// IsCrossRepository.
	Trusted           bool   `json:"trusted"`
	IsCrossRepository bool   `json:"isCrossRepository"`
	AuthorAssociation string `json:"authorAssociation"`

	// Branch is a pull request's head branch ("" for issues).
	Branch  string `json:"branch"`
	IsDraft bool   `json:"isDraft"`
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

// addItems sorts rows into the lists whose flags they carry.
func (s *Snapshot) addItems(rows []itemRow) {
	for _, r := range rows {
		item := r.item()

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
	AuthorAssociation     string    `gorm:"column:author_association"`
	IsCrossRepository     bool      `gorm:"column:is_cross_repository"`
	IsDraft               bool      `gorm:"column:is_draft"`
	HeadRef               string    `gorm:"column:head_ref"`
	ItemUpdatedAt         time.Time `gorm:"column:item_updated_at"`
	ReasonReviewRequested bool      `gorm:"column:reason_review_requested"`
	ReasonAuthored        bool      `gorm:"column:reason_authored"`
	ReasonAssigned        bool      `gorm:"column:reason_assigned"`
}

// item converts a row to its DTO.
func (r *itemRow) item() Item {
	isPR := r.Kind == model.KindPR

	return Item{
		ID:                r.ID,
		Kind:              r.Kind,
		Number:            r.Number,
		Title:             r.Title,
		URL:               r.URL,
		Repo:              r.RepoOwner + "/" + r.RepoName,
		Author:            r.AuthorLogin,
		UpdatedAt:         formatTime(r.ItemUpdatedAt),
		Trusted:           trust.Trusted(r.AuthorAssociation, isPR, r.IsCrossRepository),
		IsCrossRepository: r.IsCrossRepository,
		AuthorAssociation: r.AuthorAssociation,
		Branch:            r.HeadRef,
		IsDraft:           r.IsDraft,
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
