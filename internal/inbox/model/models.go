// Package model holds the inbox's persisted records: the signed-in account, the
// repositories it sees, the unified pull request and issue cache with each item's
// details, local triage state, and GitHub rate-limit readings.
//
// The schema itself is owned by the SQL migrations; these structs only map onto it, and a
// parity test keeps the two in step.
package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrUnsupportedScanType is returned when a JSON column holds neither text nor bytes.
var ErrUnsupportedScanType = errors.New("unsupported column value for a JSON field")

// Item kinds stored in Item.Kind.
const (
	KindPR    = "pr"
	KindIssue = "issue"
)

// Account is the signed-in GitHub account.
type Account struct {
	ID        int64     `gorm:"column:id;primaryKey"`
	Login     string    `gorm:"column:login"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName maps Account to its table.
func (Account) TableName() string { return "accounts" }

// Repo is a repository seen in the inbox, with its per-repo preferences, the matched
// local clone, and what GitHub says about it (rewritten on every refresh; FetchedAt is nil
// until a refresh has described it). An archived repository's items are never listed.
type Repo struct {
	ID        int64     `gorm:"column:id;primaryKey"`
	Owner     string    `gorm:"column:owner"`
	Name      string    `gorm:"column:name"`
	Muted     bool      `gorm:"column:muted"`
	Pinned    bool      `gorm:"column:pinned"`
	LocalPath string    `gorm:"column:local_path"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`

	NodeID              string     `gorm:"column:node_id"`
	Description         string     `gorm:"column:description"`
	Visibility          string     `gorm:"column:visibility"`
	IsArchived          bool       `gorm:"column:is_archived"`
	ArchivedAt          *time.Time `gorm:"column:archived_at"`
	IsDisabled          bool       `gorm:"column:is_disabled"`
	IsLocked            bool       `gorm:"column:is_locked"`
	LockReason          string     `gorm:"column:lock_reason"`
	IsFork              bool       `gorm:"column:is_fork"`
	Parent              string     `gorm:"column:parent"`
	DefaultBranch       string     `gorm:"column:default_branch"`
	ViewerPermission    string     `gorm:"column:viewer_permission"`
	ViewerMergeMethod   string     `gorm:"column:viewer_merge_method"`
	MergeCommitAllowed  bool       `gorm:"column:merge_commit_allowed"`
	SquashMergeAllowed  bool       `gorm:"column:squash_merge_allowed"`
	RebaseMergeAllowed  bool       `gorm:"column:rebase_merge_allowed"`
	AutoMergeAllowed    bool       `gorm:"column:auto_merge_allowed"`
	DeleteBranchOnMerge bool       `gorm:"column:delete_branch_on_merge"`
	AllowUpdateBranch   bool       `gorm:"column:allow_update_branch"`
	Language            string     `gorm:"column:language"`
	LanguageColor       string     `gorm:"column:language_color"`
	PushedAt            *time.Time `gorm:"column:pushed_at"`
	FetchedAt           *time.Time `gorm:"column:fetched_at"`
}

// TableName maps Repo to its table.
func (Repo) TableName() string { return "repos" }

// Item is one cached pull request or issue. The Reason* flags record which inbox lists
// returned it on the latest refresh; AuthorType, AuthorAssociation, and IsCrossRepository
// feed the trust rule.
type Item struct {
	ID                    int64     `gorm:"column:id;primaryKey"`
	AccountID             int64     `gorm:"column:account_id"`
	RepoID                int64     `gorm:"column:repo_id"`
	Kind                  string    `gorm:"column:kind"`
	Number                int       `gorm:"column:number"`
	NodeID                string    `gorm:"column:node_id"`
	Title                 string    `gorm:"column:title"`
	URL                   string    `gorm:"column:url"`
	AuthorLogin           string    `gorm:"column:author_login"`
	AuthorType            string    `gorm:"column:author_type"`
	AuthorAssociation     string    `gorm:"column:author_association"`
	IsCrossRepository     bool      `gorm:"column:is_cross_repository"`
	IsDraft               bool      `gorm:"column:is_draft"`
	HeadRef               string    `gorm:"column:head_ref"`
	ItemCreatedAt         time.Time `gorm:"column:item_created_at"`
	ItemUpdatedAt         time.Time `gorm:"column:item_updated_at"`
	ReasonReviewRequested bool      `gorm:"column:reason_review_requested"`
	ReasonAuthored        bool      `gorm:"column:reason_authored"`
	ReasonAssigned        bool      `gorm:"column:reason_assigned"`
	FetchedAt             time.Time `gorm:"column:fetched_at"`
}

// TableName maps Item to its table.
func (Item) TableName() string { return "items" }

// ItemLocalState is local-only triage state for an item, such as a done marker.
type ItemLocalState struct {
	ItemID    int64      `gorm:"column:item_id;primaryKey;autoIncrement:false"`
	DoneAt    *time.Time `gorm:"column:done_at"`
	UpdatedAt time.Time  `gorm:"column:updated_at"`
}

// TableName maps ItemLocalState to its table.
func (ItemLocalState) TableName() string { return "item_local_state" }

// ItemDetails is what a refresh learned about an item beyond the inbox lists: what its
// inbox card shows. A refresh replaces the whole row. The pull request fields stay zero
// for an issue.
type ItemDetails struct {
	ItemID           int64     `gorm:"column:item_id;primaryKey;autoIncrement:false"`
	Labels           Labels    `gorm:"column:labels"`
	Comments         int       `gorm:"column:comments"`
	Additions        int       `gorm:"column:additions"`
	Deletions        int       `gorm:"column:deletions"`
	ChangedFiles     int       `gorm:"column:changed_files"`
	Commits          int       `gorm:"column:commits"`
	Mergeable        string    `gorm:"column:mergeable"`
	ChecksState      string    `gorm:"column:checks_state"`
	ChecksPassed     int       `gorm:"column:checks_passed"`
	ChecksFailed     int       `gorm:"column:checks_failed"`
	ChecksPending    int       `gorm:"column:checks_pending"`
	ChecksSkipped    int       `gorm:"column:checks_skipped"`
	ReviewDecision   string    `gorm:"column:review_decision"`
	Reviewers        int       `gorm:"column:reviewers"`
	Reviewed         int       `gorm:"column:reviewed"`
	Approvals        int       `gorm:"column:approvals"`
	ChangesRequested int       `gorm:"column:changes_requested"`
	FetchedAt        time.Time `gorm:"column:fetched_at"`
}

// TableName maps ItemDetails to its table.
func (ItemDetails) TableName() string { return "item_details" }

// Label is one label on an item. Color is "#rrggbb", or "" when GitHub's was not a color.
type Label struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// Labels is a list of labels stored as a JSON array. A nil list is stored as [].
//
// Value has a value receiver so a plain field is a driver.Valuer; Scan needs a pointer.
type Labels []Label //nolint:recvcheck // Valuer on the value, Scanner on the pointer

// Value encodes the labels as a JSON array.
func (l Labels) Value() (driver.Value, error) {
	if l == nil {
		return "[]", nil
	}

	b, err := json.Marshal([]Label(l))
	if err != nil {
		return nil, fmt.Errorf("encode the labels: %w", err)
	}

	return string(b), nil
}

// Scan decodes a JSON array of labels.
func (l *Labels) Scan(src any) error {
	var raw []byte

	switch v := src.(type) {
	case string:
		raw = []byte(v)
	case []byte:
		raw = v
	default:
		return fmt.Errorf("%w: %T", ErrUnsupportedScanType, src)
	}

	if err := json.Unmarshal(raw, (*[]Label)(l)); err != nil {
		return fmt.Errorf("decode the labels: %w", err)
	}

	return nil
}

// RateSnapshot is one GitHub API rate-limit reading for a resource (graphql, core, or
// search). Cost is the point cost of the query that produced it, when known.
type RateSnapshot struct {
	ID         int64     `gorm:"column:id;primaryKey"`
	AccountID  int64     `gorm:"column:account_id"`
	Resource   string    `gorm:"column:resource"`
	LimitTotal int       `gorm:"column:limit_total"`
	Remaining  int       `gorm:"column:remaining"`
	Used       int       `gorm:"column:used"`
	Cost       int       `gorm:"column:cost"`
	ResetAt    time.Time `gorm:"column:reset_at"`
	TakenAt    time.Time `gorm:"column:taken_at"`
}

// TableName maps RateSnapshot to its table.
func (RateSnapshot) TableName() string { return "rate_snapshots" }

// AllModels returns every model in this package.
func AllModels() []any {
	return []any{&Account{}, &Repo{}, &Item{}, &ItemDetails{}, &ItemLocalState{}, &RateSnapshot{}}
}
