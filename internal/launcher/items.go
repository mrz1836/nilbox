package launcher

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/trust"
)

// repoRef is the repository an item belongs to, as stored.
type repoRef struct {
	id        int64
	name      repo.Name
	localPath string
}

// StoredItem is a stored item as a launch reads it: the item, its repository with the
// clone last remembered for it, and the details a prompt can name, which are zero until
// a refresh has read them. The pull request fields are zero, or "", for an issue.
//
// Its fields match the inbox's own record of an item one for one, so the app converts
// one to the other directly.
type StoredItem struct {
	ID     int64
	Kind   item.Kind
	Number int
	URL    string
	Title  string

	// HeadRef is a pull request's head branch.
	HeadRef string

	AuthorLogin       string
	AuthorType        string
	AuthorAssociation string
	IsCrossRepository bool
	IsDraft           bool

	// RepoID, Owner, and Name are the item's repository.
	RepoID int64
	Owner  string
	Name   string

	// LocalPath is the clone last remembered for the repository, or "".
	LocalPath     string
	DefaultBranch string

	// Labels names the item's labels, in GitHub's order.
	Labels []string

	Additions    int
	Deletions    int
	ChangedFiles int
	Commits      int

	// Mergeable, ChecksState, and ReviewDecision are GitHub's words, as a refresh read
	// them.
	Mergeable      string
	ChecksState    string
	ReviewDecision string
}

// launchItemRow is a stored item as a launch reads it.
type launchItemRow struct {
	StoredItem
}

// loadItem reads one item, or item.ErrNotFound.
func (s *Service) loadItem(ctx context.Context, id int64) (launchItemRow, error) {
	rows, err := s.loadItems(ctx, []int64{id})
	if err != nil {
		return launchItemRow{}, err
	}

	if len(rows) == 0 {
		return launchItemRow{}, fmt.Errorf("%w (id %d)", item.ErrNotFound, id)
	}

	return rows[0], nil
}

// loadItems reads the stored items among ids, in ID order.
func (s *Service) loadItems(ctx context.Context, ids []int64) ([]launchItemRow, error) {
	items, err := s.items.LaunchItems(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("load the item: %w", err)
	}

	rows := make([]launchItemRow, 0, len(items))
	for _, it := range items {
		rows = append(rows, launchItemRow{StoredItem: it})
	}

	return rows, nil
}

// author is who opened the item.
func (r launchItemRow) author() trust.Author {
	return trust.Author{Login: r.AuthorLogin, Type: r.AuthorType, Association: r.AuthorAssociation}
}

// class is the item's trust class under policy.
func (r launchItemRow) class(policy trust.Policy) trust.Class {
	return policy.Classify(r.author(), r.Kind == item.KindPR, r.IsCrossRepository)
}

// subject is what resolving the item's launch settings needs.
func (r launchItemRow) subject(policy trust.Policy) subject {
	return subject{id: r.ID, kind: r.Kind, repo: r.repo(), trusted: r.class(policy) != trust.ClassOutside}
}

// unresolved is the resolution of the item when its settings cannot resolve: the item
// alone, every setting from SourceNone, and empty lists rather than nil ones.
func (r launchItemRow) unresolved() Resolution {
	return Resolution{
		ItemID: r.ID, Kind: r.Kind, Owner: r.Owner, Repo: r.repo().String(),
		ExtraArgs: []string{}, Warnings: []string{},
	}
}

// item is what planning a launch needs.
func (r launchItemRow) item(policy trust.Policy) Item {
	return Item{
		Kind: r.Kind, Number: r.Number, URL: r.URL, Title: r.Title, Repo: r.repo().String(),
		Branch: r.HeadRef, Trusted: r.class(policy) != trust.ClassOutside,
	}
}

// repo is the item's repository's name.
func (r launchItemRow) repo() repo.Name {
	return repo.Name{Owner: r.Owner, Repo: r.Name}
}

// repoRef is the item's repository.
func (r launchItemRow) repoRef() repoRef {
	return repoRef{id: r.RepoID, name: r.repo(), localPath: r.LocalPath}
}

// promptData is what a prompt is filled in from for the item, judged by policy and
// launched with profile by viewer, with the sensitive files its file check found (nil
// when its files are not checked). The session facts are left for WithSession.
func (r launchItemRow) promptData(policy trust.Policy, profile, viewer string, check *sensitive.Check) prompts.Data {
	isPR := r.Kind == item.KindPR

	d := prompts.Data{
		Kind: r.Kind.Noun(), IsPR: isPR, Number: r.Number, Title: r.Title, URL: r.URL,
		Author: r.AuthorLogin, IsBot: r.author().IsBot(), Labels: strings.Join(r.Labels, ", "),
		Repo: r.repo().String(), Owner: r.Owner, RepoName: r.Name, DefaultBranch: r.DefaultBranch,
		Profile: profile, Me: viewer, OutsideContributor: r.class(policy) == trust.ClassOutside,
	}

	if isPR {
		d.IsDraft, d.Branch, d.HasConflicts = r.IsDraft, r.HeadRef, r.Mergeable == "CONFLICTING"
		d.Checks, d.Review = checksWord(r.ChecksState), reviewWord(r.ReviewDecision)
		d.Additions, d.Deletions, d.ChangedFiles, d.Commits = r.Additions, r.Deletions, r.ChangedFiles, r.Commits

		if check != nil {
			d.SensitiveFiles = check.Summary()
		}
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

	w, err := p.s.loadWorld(ctx)
	if err != nil {
		return prompts.Data{}, err
	}

	profile := w.settings.DefaultProfile

	r := &resolver{w: w, item: row.subject(w.policy)}
	r.org, r.repo = w.rules.For(row.repo())

	if inherited, _, profileErr := r.inheritedProfile(); profileErr == nil {
		profile = inherited.Name
	}

	viewer, err := p.s.items.ViewerLogin(ctx)
	if err != nil {
		return prompts.Data{}, err
	}

	check, err := p.s.fileCheck(ctx, row.ID, false)
	if err != nil && !errors.Is(err, sensitive.ErrNotChecked) {
		return prompts.Data{}, err
	}

	return row.promptData(w.policy, profile, viewer, check), nil
}
