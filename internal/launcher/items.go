package launcher

import (
	"context"
	"fmt"
	"strings"

	"github.com/mrz1836/nilbox/internal/inbox"
	inboxmodel "github.com/mrz1836/nilbox/internal/inbox/model"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/trust"
)

// loadItemsSQL reads cached items with their repository, their details when a refresh has
// read them, and the repository's cached clone path.
const loadItemsSQL = `SELECT i.id, i.kind, i.number, i.url, i.title, i.head_ref, i.author_login,
	i.author_type, i.author_association, i.is_cross_repository, i.is_draft,
	r.id AS repo_id, r.owner, r.name, r.local_path, r.default_branch,
	COALESCE(d.labels, '[]') AS labels, COALESCE(d.additions, 0) AS additions,
	COALESCE(d.deletions, 0) AS deletions, COALESCE(d.changed_files, 0) AS changed_files,
	COALESCE(d.commits, 0) AS commits, COALESCE(d.mergeable, '') AS mergeable,
	COALESCE(d.checks_state, '') AS checks_state, COALESCE(d.review_decision, '') AS review_decision
FROM items i
JOIN repos r ON r.id = i.repo_id
LEFT JOIN item_details d ON d.item_id = i.id
WHERE i.id IN ?
ORDER BY i.id`

// repoRef is the repository an item belongs to, as stored.
type repoRef struct {
	id        int64
	owner     string
	name      string
	localPath string
}

// launchItemRow is one row of loadItemsSQL.
type launchItemRow struct {
	ID                int64             `gorm:"column:id"`
	Kind              string            `gorm:"column:kind"`
	Number            int               `gorm:"column:number"`
	URL               string            `gorm:"column:url"`
	Title             string            `gorm:"column:title"`
	HeadRef           string            `gorm:"column:head_ref"`
	AuthorLogin       string            `gorm:"column:author_login"`
	AuthorType        string            `gorm:"column:author_type"`
	AuthorAssociation string            `gorm:"column:author_association"`
	IsCrossRepository bool              `gorm:"column:is_cross_repository"`
	IsDraft           bool              `gorm:"column:is_draft"`
	RepoID            int64             `gorm:"column:repo_id"`
	Owner             string            `gorm:"column:owner"`
	Name              string            `gorm:"column:name"`
	LocalPath         string            `gorm:"column:local_path"`
	DefaultBranch     string            `gorm:"column:default_branch"`
	Labels            inboxmodel.Labels `gorm:"column:labels"`
	Additions         int               `gorm:"column:additions"`
	Deletions         int               `gorm:"column:deletions"`
	ChangedFiles      int               `gorm:"column:changed_files"`
	Commits           int               `gorm:"column:commits"`
	Mergeable         string            `gorm:"column:mergeable"`
	ChecksState       string            `gorm:"column:checks_state"`
	ReviewDecision    string            `gorm:"column:review_decision"`
}

// loadItem reads one item, or ErrItemNotFound.
func (s *Service) loadItem(ctx context.Context, id int64) (launchItemRow, error) {
	rows, err := s.loadItems(ctx, []int64{id})
	if err != nil {
		return launchItemRow{}, err
	}

	if len(rows) == 0 {
		return launchItemRow{}, fmt.Errorf("%w (id %d)", ErrItemNotFound, id)
	}

	return rows[0], nil
}

// loadItems reads the stored items among ids, in ID order.
func (s *Service) loadItems(ctx context.Context, ids []int64) ([]launchItemRow, error) {
	var rows []launchItemRow
	if err := s.db.WithContext(ctx).Raw(loadItemsSQL, ids).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("load the item: %w", err)
	}

	return rows, nil
}

// author is who opened the item.
func (r launchItemRow) author() trust.Author {
	return trust.Author{Login: r.AuthorLogin, Type: r.AuthorType, Association: r.AuthorAssociation}
}

// class is the item's trust class under policy.
func (r launchItemRow) class(policy trust.Policy) string {
	return policy.Classify(r.author(), r.Kind == KindPR, r.IsCrossRepository)
}

// subject is what resolving the item's launch settings needs.
func (r launchItemRow) subject(policy trust.Policy) subject {
	return subject{
		id: r.ID, kind: r.Kind, owner: r.Owner, name: r.Name, trusted: r.class(policy) != trust.ClassOutside,
	}
}

// item is what planning a launch needs.
func (r launchItemRow) item(policy trust.Policy) Item {
	return Item{
		Kind: r.Kind, Number: r.Number, URL: r.URL, Title: r.Title, Repo: r.Owner + "/" + r.Name,
		Branch: r.HeadRef, Trusted: r.class(policy) != trust.ClassOutside,
	}
}

// repoRef is the item's repository.
func (r launchItemRow) repoRef() repoRef {
	return repoRef{id: r.RepoID, owner: r.Owner, name: r.Name, localPath: r.LocalPath}
}

// promptData is what a prompt is filled in from for the item, judged by policy and
// launched with profile by viewer. The session facts are left for WithSession.
func (r launchItemRow) promptData(policy trust.Policy, profile, viewer string) prompts.Data {
	isPR := r.Kind == KindPR

	labels := make([]string, 0, len(r.Labels))
	for _, l := range r.Labels {
		labels = append(labels, l.Name)
	}

	d := prompts.Data{
		Kind: prompts.KindName(r.Kind), IsPR: isPR, Number: r.Number, Title: r.Title, URL: r.URL,
		Author: r.AuthorLogin, IsBot: r.author().IsBot(), Labels: strings.Join(labels, ", "),
		Repo: r.Owner + "/" + r.Name, Owner: r.Owner, RepoName: r.Name, DefaultBranch: r.DefaultBranch,
		Profile: profile, Me: viewer, OutsideContributor: r.class(policy) == trust.ClassOutside,
	}

	if isPR {
		d.IsDraft, d.Branch, d.HasConflicts = r.IsDraft, r.HeadRef, r.Mergeable == "CONFLICTING"
		d.Checks, d.Review = checksWord(r.ChecksState), reviewWord(r.ReviewDecision)
		d.Additions, d.Deletions, d.ChangedFiles, d.Commits = r.Additions, r.Deletions, r.ChangedFiles, r.Commits
	}

	return d
}

// checksWord names GitHub's overall check result the way prompts read it.
func checksWord(state string) string {
	switch state {
	case "SUCCESS":
		return "passing"
	case "FAILURE", "ERROR":
		return "failing"
	case "PENDING", "EXPECTED":
		return "pending"
	default:
		return ""
	}
}

// reviewWord names GitHub's review decision the way prompts read it.
func reviewWord(decision string) string {
	switch decision {
	case "APPROVED":
		return "approved"
	case "CHANGES_REQUESTED":
		return "changes requested"
	case "REVIEW_REQUIRED":
		return "review required"
	default:
		return ""
	}
}

// NewPromptSource returns the prompts.PreviewSource the prompt editor previews with: an
// inbox item's data, filled in the way a launch fills it, with the profile its launch
// settings resolve to and without any session facts.
func NewPromptSource(s *Service) prompts.PreviewSource {
	return promptSource{s: s}
}

// promptSource is NewPromptSource's PreviewSource.
type promptSource struct {
	s *Service
}

// PreviewData returns the data a launch on the item fills a prompt with.
func (p promptSource) PreviewData(ctx context.Context, itemID int64) (prompts.Data, error) {
	row, err := p.s.loadItem(ctx, itemID)
	if err != nil {
		return prompts.Data{}, err
	}

	w, err := loadWorld(ctx, p.s.db, p.s.settings)
	if err != nil {
		return prompts.Data{}, err
	}

	profile := w.settings.DefaultProfile

	r := &resolver{w: w, item: row.subject(w.policy)}
	r.org, r.repo = w.rules.For(row.Owner, row.Name)

	if inherited, _, profileErr := r.inheritedProfile(); profileErr == nil {
		profile = inherited.Name
	}

	viewer, err := inbox.ViewerLogin(ctx, p.s.db)
	if err != nil {
		return prompts.Data{}, err
	}

	return row.promptData(w.policy, profile, viewer), nil
}
