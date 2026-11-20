// Package assignees is what both sheets' assignee pickers share: whom they offer to assign
// to an issue or a pull request, in what order, and how that offer is learned and searched
// with as few requests to GitHub as it takes.
//
// A Finder offers people from the local People directory alone (Candidates), with no
// request: the user, the item's author and participants, the people assigned in the
// repository before, those who work on it, the rest of those GitHub listed as assignable
// there, and, while that list is cut short, the people who work on the owner's other
// repositories. It reads GitHub's whole list of a repository's assignable users at most
// weekly (Learn), one page of 100 for one point, and a search of it (Search) only when
// nothing the directory holds answers it: a whole list that named everyone answers every
// search, a search that named everyone it matched answers every longer search for a week,
// and any search answers the same words for ten minutes. One read runs at a time per
// repository and search, whichever sheet asks, so core builds one Finder for both.
//
// The package knows no GitHub client and neither sheet: each sheet hands it the item (Item)
// and the read to make (Read), and core adapts the People directory to its Directory.
package assignees

// Source is why the picker offers someone, which names the section they are listed in.
type Source string

// The sources, in the order a list offers them.
const (
	// SourceYou is the signed-in user.
	SourceYou Source = "you"

	// SourceAuthor opened the issue or the pull request.
	SourceAuthor Source = "author"

	// SourceParticipant took part in it: commented, or reviewed a pull request.
	SourceParticipant Source = "participant"

	// SourceAssignedBefore was seen assigned in the repository before.
	SourceAssignedBefore Source = "assigned_before"

	// SourceWorksHere works on the repository, as the People directory knows it.
	SourceWorksHere Source = "works_here"

	// SourceListed is one GitHub listed as assignable in the repository.
	SourceListed Source = "listed"

	// SourceOrg works on another of the owner's repositories.
	SourceOrg Source = "org"

	// SourceFound is one a search of GitHub just found, whom the app knew nothing else of.
	SourceFound Source = "found"
)

// Candidate is someone the picker offers to assign.
type Candidate struct {
	Login string `json:"login"`
	Name  string `json:"name"`

	// Mine reports the signed-in user.
	Mine bool `json:"mine"`

	// Source is why the picker offers them.
	Source Source `json:"source"`

	// Reviewed, Authored, and Committed count the repository's pull requests they reviewed,
	// opened, and committed to, of those the app knows; for SourceOrg, of the repository
	// Where names.
	Reviewed  int `json:"reviewed"`
	Authored  int `json:"authored"`
	Committed int `json:"committed"`

	// AssignedAt is when the app last saw them assigned in the repository, and LastAt when it
	// last saw them work there, as RFC 3339, or "".
	AssignedAt string `json:"assignedAt"`
	LastAt     string `json:"lastAt"`

	// Where is the owner's other repository they were last seen working on, as
	// "owner/name", for SourceOrg; "" for any other.
	Where string `json:"where"`
}

// Candidates is who the picker offers to assign to an issue or a pull request.
type Candidates struct {
	// People are those it offers, in the order of their Source, each once, never a bot, at
	// most 300, never nil.
	People []Candidate `json:"people"`

	// Total is how many people GitHub said can be assigned in the repository, as the last
	// read of its whole list counted them, and Complete reports that the read named every
	// one, so a search never asks GitHub. ReadAt is when that read was, as RFC 3339, or ""
	// before one was, and Stale whether one is due (Learn).
	Total    int    `json:"total"`
	Complete bool   `json:"complete"`
	ReadAt   string `json:"readAt"`
	Stale    bool   `json:"stale"`
}
