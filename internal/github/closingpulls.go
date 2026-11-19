package github

// closingPullsField reads the first five pull requests that close an issue once they
// merge: GitHub's closing references, from a pull request whose description says it
// closes the issue or one someone linked to it by hand, open or merged (GitHub leaves out
// one closed without merging). Each comes with who opened it and whether its branch is in
// a fork, so the inbox can decide whether it trusts the claim (see inbox's ClosingPull).
//
// The link is read on the issue's side alone, which costs nothing: a batch of 25 issues
// cost one point with it and without, and two with the projects either way (measured in
// November 2026). The same link read on each pull request's side (closingIssuesReferences)
// would cost a point more for every batch of 25 pull requests, and the inbox finds what a
// listed pull request closes from the listed issues' links instead. The repository, the
// author, and the scalars are single objects, which GitHub's point formula never counts.
//
// It is aliased so it never meets the issue sheet's own closing pull requests (fixes, which
// reads closed ones too) on the node both spread.
const closingPullsField = `
    linkedPulls: closedByPullRequestsReferences(first: 5) {
      nodes {
        number title url prState: state isDraft isCrossRepository authorAssociation
        author { __typename login }
        repository { nameWithOwner }
      }
    }`

// ClosingPull is a pull request that closes an issue once it merges, open or merged, as an
// issue's details read it.
type ClosingPull struct {
	// Repo is its repository as "owner/name", which may be another than the issue's.
	Repo   string
	Number int
	Title  string
	URL    string

	// Merged reports one that merged; it is open otherwise.
	Merged  bool
	IsDraft bool

	// AuthorLogin, AuthorType, and AuthorAssociation are who opened it, and
	// IsCrossRepository reports a branch in a fork: what its trust is decided from.
	AuthorLogin       string
	AuthorType        string
	AuthorAssociation string
	IsCrossRepository bool
}

// graphQLClosingPulls is an issue's closing pull requests (closingPullsField).
type graphQLClosingPulls struct {
	Nodes []*graphQLClosingPull `json:"nodes"`
}

// graphQLClosingPull is one closing pull request.
type graphQLClosingPull struct {
	Number            int           `json:"number"`
	Title             string        `json:"title"`
	URL               string        `json:"url"`
	PRState           string        `json:"prState"`
	IsDraft           bool          `json:"isDraft"`
	IsCrossRepository bool          `json:"isCrossRepository"`
	AuthorAssociation string        `json:"authorAssociation"`
	Author            *graphQLActor `json:"author"`
	Repository        *struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
}

// closingPulls converts the closing pull requests in GitHub's order, skipping nulls, one
// without a repository or a number, and one neither open nor merged. It returns nil when
// none are left.
func (c graphQLClosingPulls) closingPulls() []ClosingPull {
	var out []ClosingPull

	for _, n := range c.Nodes {
		if n == nil || n.Repository == nil || n.Repository.NameWithOwner == "" || n.Number <= 0 {
			continue
		}

		if n.PRState != StateOpen && n.PRState != StateMerged {
			continue
		}

		pull := ClosingPull{
			Repo: n.Repository.NameWithOwner, Number: n.Number, Title: n.Title, URL: n.URL,
			Merged: n.PRState == StateMerged, IsDraft: n.IsDraft,
			AuthorAssociation: n.AuthorAssociation, IsCrossRepository: n.IsCrossRepository,
		}

		if n.Author != nil {
			pull.AuthorLogin, pull.AuthorType = n.Author.Login, n.Author.TypeName
		}

		out = append(out, pull)
	}

	return out
}
