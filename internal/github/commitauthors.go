package github

import (
	"context"
	"slices"
)

// commitAuthorsQuery reads the newest RecentCommitsMax commits of each pull request in
// $ids, each with the GitHub login its author's email matches, so the inbox can tell a
// push the viewer made to a pull request they requested changes on, such as a merge of
// its base, from the author's. The inbox asks only about a pull request whose head moved
// past the viewer's verdict since it last asked, so most refreshes send none. It cost one
// point for 15 pull requests (measured in November 2026), as GitHub prices a batch of up
// to commitAuthorsBatch, one commits connection each; the same commits asked for in the
// fourth search cost every refresh a point more (measured too).
const commitAuthorsQuery = `query CommitAuthors($ids: [ID!]!) {
  nodes(ids: $ids) {
    ... on PullRequest {
      id
      recentCommits: commits(last: ` + recentCommitsMaxText + `) { nodes { commit { oid author { user { login } } } } }
    }
  }
  rateLimit { cost remaining resetAt limit used }
}`

// RecentCommitsMax is how many of a pull request's newest commits FetchCommitAuthors
// reads, and recentCommitsMaxText the same in the query's text.
const (
	RecentCommitsMax     = 20
	recentCommitsMaxText = "20"
)

// commitAuthorsBatch is the most pull requests one commit authors request asks about.
const commitAuthorsBatch = 50

// CommitAuthor is one of a pull request's commits: its SHA, and the GitHub login its
// author's email matches, "" when it matches no account.
type CommitAuthor struct {
	OID   string
	Login string
}

// FetchCommitAuthors reads the newest RecentCommitsMax commits of each pull request in
// ids, by node ID, oldest first, never nil, one request per commitAuthorsBatch. A node
// GitHub can't resolve, or that is no pull request, is left out of the answer.
func (c *Client) FetchCommitAuthors(ctx context.Context, tok Token, ids []string) (map[string][]CommitAuthor, error) {
	out := make(map[string][]CommitAuthor, len(ids))

	for batch := range slices.Chunk(ids, commitAuthorsBatch) {
		if err := c.fetchCommitAuthors(ctx, tok, batch, out); err != nil {
			return nil, Redact(err, tok)
		}
	}

	return out, nil
}

// commitAuthorsData is the commit authors query's data object. GitHub's nulls decode to
// zero values.
type commitAuthorsData struct {
	Nodes     []*commitAuthorsNode `json:"nodes"`
	RateLimit graphQLRateLimit     `json:"rateLimit"`
}

// commitAuthorsNode is one pull request in a commit authors answer.
type commitAuthorsNode struct {
	ID            string `json:"id"`
	RecentCommits *struct {
		Nodes []*struct {
			Commit *struct {
				OID    string `json:"oid"`
				Author *struct {
					User *graphQLActor `json:"user"`
				} `json:"author"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"recentCommits"`
}

// commits converts the pull request's commits, oldest first, skipping nulls and commits
// without a SHA.
func (n *commitAuthorsNode) commits() []CommitAuthor {
	out := make([]CommitAuthor, 0, len(n.RecentCommits.Nodes))

	for _, cn := range n.RecentCommits.Nodes {
		if cn == nil || cn.Commit == nil || cn.Commit.OID == "" {
			continue
		}

		commit := CommitAuthor{OID: cn.Commit.OID}
		if a := cn.Commit.Author; a != nil && a.User != nil {
			commit.Login = a.User.Login
		}

		out = append(out, commit)
	}

	return out
}

// fetchCommitAuthors sends the query for one batch, and adds each pull request's commits
// to out. Errors with data are about single nodes, which come back null.
func (c *Client) fetchCommitAuthors(ctx context.Context, tok Token, ids []string, out map[string][]CommitAuthor) error {
	req := graphQLRequest{Query: commitAuthorsQuery, Variables: map[string]any{"ids": ids}}

	resp, _, err := postGraphQL[commitAuthorsData](ctx, c, tok, "commit authors", req, unlessData)
	if err != nil {
		return err
	}

	for _, n := range resp.Data.Nodes {
		if n != nil && n.ID != "" && n.RecentCommits != nil {
			out[n.ID] = n.commits()
		}
	}

	return nil
}
