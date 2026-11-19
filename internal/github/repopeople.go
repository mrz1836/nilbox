package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/mrz1836/nilbox/internal/repo"
)

// RepoPeoplePulls is how many of a repository's most recently updated pull requests
// FetchRepoPeople reads.
const RepoPeoplePulls = 50

// repoTeamsMax is how many of a repository's teams FetchRepoTeams reads: one page.
const repoTeamsMax = 100

// ErrTeamsHidden is returned when GitHub won't list a repository's teams to the account,
// refusing (403) or hiding them (404), as it may for a token without the scope or an
// account without access to them: the caller falls back to the teams it saw asked to
// review.
var ErrTeamsHidden = errors.New("GitHub won't list the repository's teams")

// repoPeopleQuery reads who works on the repository of a pull request, by the pull
// request's node ID: whether an organization owns it, and its 50 most recently updated pull
// requests, each with who opened it, each reviewer's latest review, and who it asks to
// review, people and teams. Every author comes with their association with the repository
// (OWNER, MEMBER, COLLABORATOR, CONTRIBUTOR, ...), which tells the people with access apart.
//
// GitHub prices it at one point (measured in November 2026 on a repository with 50 open and
// closed pull requests read): the pull requests are one connection of 50, and each one's
// reviews and requests 50 more each, 101 nodes in all, which its point formula counts as
// one.
const repoPeopleQuery = `query RepoPeople($id: ID!) {
  node(id: $id) {
    __typename
    ... on PullRequest {
      repository {
        owner { __typename }
        pullRequests(first: ` + repoPeoplePullsText + `, orderBy: {field: UPDATED_AT, direction: DESC}) {
          nodes {
            number updatedAt authorAssociation
            author { __typename login ... on User { name } }
            latestReviews(first: 10) {
              nodes { submittedAt authorAssociation author { __typename login ... on User { name } } }
            }
            reviewRequests(first: 10) {
              nodes { requestedReviewer { __typename ... on User { login name } ... on Team { slug name } } }
            }
          }
        }
      }
    }
  }
  rateLimit { cost remaining resetAt limit used }
}
`

// repoPeoplePullsText is RepoPeoplePulls in the query's text.
const repoPeoplePullsText = "50"

// typeOrganization is the GraphQL type of a repository owner that is an organization.
const typeOrganization = "Organization"

// RepoPeople is who works on a repository, as its recent pull requests show it.
type RepoPeople struct {
	// Org reports a repository an organization owns, whose teams a pull request can ask to
	// review.
	Org bool

	// Pulls are its most recently updated pull requests, the latest first, never nil.
	Pulls []RepoPull
}

// RepoPull is one of a repository's pull requests, as FetchRepoPeople read it. People only:
// bots, mannequins, and deleted accounts are left out.
type RepoPull struct {
	Number    int
	UpdatedAt time.Time

	// Author is who opened it, with their association, or zero for a bot's, a mannequin's,
	// or a deleted account's.
	Author Participant

	// Reviews are the reviewers' latest reviews, each with when it was submitted, never nil.
	Reviews []Participant

	// Requested are the people it asks to review, and Teams the teams, never nil.
	Requested []Person
	Teams     []Team
}

// Participant is a person who took part in a pull request: who they are, their association
// with the repository as of then, and when (a review's submission; zero for a pull
// request's author).
type Participant struct {
	Login       string
	Name        string
	Association string
	At          time.Time
}

// Team is a team asked to review, or listed on a repository: its slug, which a review
// request names it by, and its display name, "" when the answer gave none.
type Team struct {
	Slug string
	Name string
}

// FetchRepoPeople reads who works on the repository of the pull request with node ID id,
// from its RepoPeoplePulls most recently updated pull requests, with one GraphQL request of
// one point. A pull request GitHub can no longer find returns ErrPullGone.
func (c *Client) FetchRepoPeople(ctx context.Context, tok Token, id string) (*RepoPeople, error) {
	people, err := c.fetchRepoPeople(ctx, tok, id)

	return people, Redact(err, tok)
}

// FetchRepoTeams reads the teams with access to the repository name, an organization's,
// the first hundred, with one REST request (GET /repos/{owner}/{repo}/teams). A refusal or
// a not-found answer is ErrTeamsHidden.
func (c *Client) FetchRepoTeams(ctx context.Context, tok Token, name repo.Name) ([]Team, error) {
	teams, err := c.fetchRepoTeams(ctx, tok, name)

	return teams, Redact(err, tok)
}

// graphQLPeopleActor is an author as the people read asks for one.
type graphQLPeopleActor struct {
	TypeName string `json:"__typename"`
	Login    string `json:"login"`
	Name     string `json:"name"`
}

// person reports the actor as a person, and false for a bot, a mannequin, a deleted
// account (null), or one without a login.
func (a *graphQLPeopleActor) person() (Person, bool) {
	if a == nil || a.TypeName != typeUser || a.Login == "" {
		return Person{}, false
	}

	return Person{Login: a.Login, Name: a.Name}, true
}

// repoPeopleNode is the pull request node of the people read.
type repoPeopleNode struct {
	TypeName   string `json:"__typename"`
	Repository *struct {
		Owner struct {
			TypeName string `json:"__typename"`
		} `json:"owner"`
		PullRequests struct {
			Nodes []*repoPeoplePull `json:"nodes"`
		} `json:"pullRequests"`
	} `json:"repository"`
}

// repoPeoplePull is one pull request of the people read.
type repoPeoplePull struct {
	Number            int                 `json:"number"`
	UpdatedAt         time.Time           `json:"updatedAt"`
	AuthorAssociation string              `json:"authorAssociation"`
	Author            *graphQLPeopleActor `json:"author"`
	LatestReviews     struct {
		Nodes []*struct {
			SubmittedAt       time.Time           `json:"submittedAt"`
			AuthorAssociation string              `json:"authorAssociation"`
			Author            *graphQLPeopleActor `json:"author"`
		} `json:"nodes"`
	} `json:"latestReviews"`
	ReviewRequests struct {
		Nodes []*struct {
			RequestedReviewer *struct {
				TypeName string `json:"__typename"`
				Login    string `json:"login"`
				Slug     string `json:"slug"`
				Name     string `json:"name"`
			} `json:"requestedReviewer"`
		} `json:"nodes"`
	} `json:"reviewRequests"`
}

// fetchRepoPeople sends the people read and decodes the answer.
func (c *Client) fetchRepoPeople(ctx context.Context, tok Token, id string) (*RepoPeople, error) {
	node, err := c.sendRepoPeople(ctx, tok, id)
	if err != nil {
		return nil, err
	}

	out := &RepoPeople{Org: node.Repository.Owner.TypeName == typeOrganization, Pulls: []RepoPull{}}

	for _, p := range node.Repository.PullRequests.Nodes {
		if p != nil && p.Number > 0 {
			out.Pulls = append(out.Pulls, p.pull())
		}
	}

	return out, nil
}

// sendRepoPeople sends the people read and decodes the pull request's node, one with a
// repository. A pull request GitHub can no longer find is ErrPullGone.
func (c *Client) sendRepoPeople(ctx context.Context, tok Token, id string) (*repoPeopleNode, error) {
	if id == "" {
		return nil, fmt.Errorf("%w: no node ID", ErrInvalidItemRef)
	}

	req := graphQLRequest{Query: repoPeopleQuery, Variables: map[string]any{"id": id}}

	resp, _, err := postGraphQL[itemData](ctx, c, tok, "repository people", req, unlessData)
	if err != nil {
		return nil, err
	}

	gone, err := goneNode(resp)

	switch {
	case err != nil:
		return nil, err
	case gone:
		return nil, ErrPullGone
	}

	var node repoPeopleNode
	if err = json.Unmarshal(resp.Data.Node, &node); err != nil {
		return nil, fmt.Errorf("%w: decode the repository people: %w", ErrMalformedResponse, err)
	}

	if node.TypeName != typePullRequest || node.Repository == nil {
		return nil, fmt.Errorf("%w: the ID is not a pull request with a repository", ErrMalformedResponse)
	}

	return &node, nil
}

// pull converts one pull request of the people read, leaving out who isn't a person.
func (p *repoPeoplePull) pull() RepoPull {
	out := RepoPull{Number: p.Number, UpdatedAt: p.UpdatedAt.UTC(), Reviews: p.reviews()}

	if who, ok := p.Author.person(); ok {
		out.Author = Participant{Login: who.Login, Name: who.Name, Association: p.AuthorAssociation}
	}

	out.Requested, out.Teams = p.requests()

	return out
}

// reviews converts the reviewers' latest reviews, people only, never nil.
func (p *repoPeoplePull) reviews() []Participant {
	out := []Participant{}

	for _, r := range p.LatestReviews.Nodes {
		if r == nil {
			continue
		}

		if who, ok := r.Author.person(); ok {
			out = append(out, Participant{
				Login: who.Login, Name: who.Name, Association: r.AuthorAssociation, At: r.SubmittedAt.UTC(),
			})
		}
	}

	return out
}

// requests converts who the pull request asks to review: the people and the teams, never
// nil, skipping nulls, bots, and nameless teams.
func (p *repoPeoplePull) requests() ([]Person, []Team) {
	people, teams := []Person{}, []Team{}

	for _, r := range p.ReviewRequests.Nodes {
		if r == nil || r.RequestedReviewer == nil {
			continue
		}

		switch who := r.RequestedReviewer; {
		case who.TypeName == typeUser && who.Login != "":
			people = append(people, Person{Login: who.Login, Name: who.Name})
		case who.TypeName == typeTeam && who.Slug != "":
			teams = append(teams, Team{Slug: who.Slug, Name: who.Name})
		}
	}

	return people, teams
}

// fetchRepoTeams sends the teams read and decodes the answer.
func (c *Client) fetchRepoTeams(ctx context.Context, tok Token, name repo.Name) ([]Team, error) {
	if !name.Valid() {
		return nil, fmt.Errorf("%w: %q", ErrInvalidItemRef, name.String())
	}

	path := "/repos/" + url.PathEscape(name.Owner) + "/" + url.PathEscape(name.Repo) + "/teams?per_page=" +
		strconv.Itoa(repoTeamsMax)

	raw, _, err := c.do(ctx, tok, http.MethodGet, path, nil)

	switch {
	case teamsHidden(err):
		return nil, fmt.Errorf("%w: %s: %w", ErrTeamsHidden, name, err)
	case err != nil:
		return nil, fmt.Errorf("read the teams of %s: %w", name, err)
	}

	var answer []struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	}

	if err = json.Unmarshal(raw, &answer); err != nil {
		return nil, fmt.Errorf("%w: decode the teams of %s: %w", ErrMalformedResponse, name, err)
	}

	out := make([]Team, 0, len(answer))

	for _, t := range answer {
		if t.Slug != "" {
			out = append(out, Team{Slug: t.Slug, Name: t.Name})
		}
	}

	return out, nil
}

// teamsHidden reports GitHub refusing (403) or hiding (404) a repository's teams from the
// account, rather than its token or its rate limits.
func teamsHidden(err error) bool {
	status, ok := errors.AsType[*StatusError](err)
	if !ok || IsBadCredentials(err) || RateLimited(err) {
		return false
	}

	return status.StatusCode == http.StatusForbidden || status.StatusCode == http.StatusNotFound
}
