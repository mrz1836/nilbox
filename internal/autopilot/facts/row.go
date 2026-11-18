package facts

import (
	"time"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/trust"
)

// Row is what the inbox's query reads about one item, in plain values: the item, its
// repository, its triage, and, once read, its details, its head commit's check names,
// and its changed files. A part not read yet is nil, or for a single value a nil pointer
// or "", as each field says, and the fields it fills are unknown.
type Row struct {
	ID     int64
	Kind   item.Kind
	Repo   repo.Name
	Number int
	Title  string
	URL    string

	// Author is the author's login, AuthorType their GraphQL type ("User", "Bot", ...;
	// "" before a refresh read it), and Association their relationship to the
	// repository, as GitHub names it.
	Author      string
	AuthorType  string
	Association string

	// Draft, HeadRef, and CrossRepository are a pull request's.
	Draft           bool
	HeadRef         string
	CrossRepository bool

	CreatedAt time.Time
	UpdatedAt time.Time

	// The inbox lists the item is in.
	InReviewRequested bool
	InAuthored        bool
	InAssigned        bool

	// ReviewRequestedAt and AssignedAt are when the inbox last saw a request for the
	// user's review, and an assignment to the user, arrive; nil when it doesn't say.
	ReviewRequestedAt *time.Time
	AssignedAt        *time.Time

	// The item's triage on this Mac.
	SnoozedUntil *time.Time
	DismissedAt  *time.Time
	PinnedAt     *time.Time

	Repository RepoRow

	// Details are nil until the item's details are read.
	Details *DetailsRow

	// Checks are nil until the head commit's check names are read.
	Checks *ChecksRow

	// Files are nil until the pull request's changed files are checked.
	Files *FilesRow
}

// RepoRow is what is known about the item's repository.
type RepoRow struct {
	// Read reports that GitHub's facts about the repository were read: Visibility, Fork,
	// Permission, and AllowUpdateBranch are unknown until they are.
	Read bool

	// Visibility is PUBLIC, PRIVATE, or INTERNAL; Permission is the user's, ADMIN,
	// MAINTAIN, WRITE, TRIAGE, or READ.
	Visibility string
	Permission string

	Fork              bool
	AllowUpdateBranch bool

	// Language is the repository's main language, nil while it isn't read.
	Language *string

	// HasClone reports a local clone of it, nil while that isn't known.
	HasClone *bool

	// Muted and Pinned are the user's repository rules for it.
	Muted  bool
	Pinned bool
}

// DetailsRow is an item's details, as the inbox reads them.
type DetailsRow struct {
	// HeadOID is a pull request's head commit.
	HeadOID string

	// BaseRef is the branch it merges into, "" while it isn't read.
	BaseRef string

	// Mergeable is MERGEABLE, CONFLICTING, or UNKNOWN; MergeState is GitHub's
	// mergeStateStatus (BEHIND, CLEAN, DIRTY, ...), "" while it isn't read.
	Mergeable  string
	MergeState string

	// ReviewDecision is APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED, or "".
	ReviewDecision string

	// ChecksState is the head commit's overall result (SUCCESS, FAILURE, ERROR, PENDING,
	// EXPECTED), "" with no checks; the counts add up its check runs and statuses.
	// ChecksAwaiting counts every workflow run held for a maintainer's approval, whether
	// the user can approve it from the app or only on GitHub.
	ChecksState    string
	ChecksPassed   int
	ChecksFailed   int
	ChecksPending  int
	ChecksAwaiting int

	Additions    int
	Deletions    int
	ChangedFiles int
	Commits      int
	Comments     int

	// Labels are the labels' names.
	Labels []string

	Approvals        int
	ChangesRequested int

	// ReviewUsers are the logins asked to review it.
	ReviewUsers []string

	AutoMerge bool

	// FetchedAt is when the details were read, zero when it isn't known.
	FetchedAt time.Time
}

// CheckOutcome is how one check run or status ended.
type CheckOutcome string

// The outcomes of a check, as the inbox's details count them.
const (
	CheckPassed  CheckOutcome = "passed"
	CheckFailed  CheckOutcome = "failed"
	CheckPending CheckOutcome = "pending"
	CheckSkipped CheckOutcome = "skipped"
)

// ChecksRow is a head commit's checks, as read.
type ChecksRow struct {
	// Head is the commit they were read at.
	Head string

	Checks []CheckRow

	// Complete reports that Checks holds every check of the commit.
	Complete bool
}

// CheckRow is one check run or commit status.
type CheckRow struct {
	// Name is a run's name or a status's context; Workflow is a run's workflow, ""
	// for an app's own run (CodeQL's, OSV-Scanner's) and for a status.
	Name     string
	Workflow string

	Outcome CheckOutcome

	// Required reports a required check, nil when it isn't known.
	Required *bool

	// CompletedAt is when it finished (a status's creation time), zero while it runs.
	CompletedAt time.Time
}

// FilesRow is a pull request's changed files, as the sensitive-file check stores them.
type FilesRow struct {
	// Head is the commit they were listed at.
	Head string

	Files []sensitive.File

	// Complete reports that Files holds every changed file.
	Complete bool

	// Error is why the check failed, or "".
	Error string
}

// Env is what Build needs beyond the row.
type Env struct {
	// Now is the moment the facts are for: a snooze still runs when it ends after Now.
	Now time.Time

	// Viewer is the signed-in user's login.
	Viewer string

	// Trust decides the author's trust class.
	Trust trust.Policy

	// Sensitive is the user's sensitive-file settings.
	Sensitive sensitive.Config
}
