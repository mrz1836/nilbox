package github

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mrz1836/nilbox/internal/repo"
)

// mergedBatch is the most branch names one merged-branches request asks about. Each is a
// small connection of at most mergedPerBranch pull requests, so a full request stays
// cheap; requests are sent side by side like the states requests. Each repository is
// asked about under mergedRepoPrefix and its index, as in r0, and each of its branch names
// under mergedBranchPrefix and its index, as in b0.
const (
	mergedBatch        = 50
	mergedPerBranch    = 5
	mergedParallel     = statesParallel
	mergedRepoPrefix   = "r"
	mergedBranchPrefix = "b"
)

// BranchQuery asks which of a repository's branch names were the head branch of a pull
// request that merged.
type BranchQuery struct {
	Repo     repo.Name
	Branches []string
}

// MergedPR is a merged pull request, as the cleanup of merged branches shows it.
type MergedPR struct {
	Number   int
	Title    string
	URL      string
	MergedAt time.Time

	// HeadOID is the commit its head branch was at, which GitHub keeps as the pull
	// request's head.
	HeadOID string
}

// MergedKey is the key FetchMergedPullRequests answers a repository's branch under.
func MergedKey(r repo.Name, branch string) string {
	return r.Key() + "\x00" + branch
}

// FetchMergedPullRequests returns, under MergedKey, the merged pull requests whose head
// branch had each name asked about, newest first, at most five each. A name no merged pull
// request had is left out, and so is a repository GitHub cannot resolve; any other error
// fails the whole call, so a passing problem never makes a branch look merged. A name that
// is not valid UTF-8 is never asked about, so it is left out too: a query could only ask
// about it rewritten, and so about another branch.
func (c *Client) FetchMergedPullRequests(ctx context.Context, tok Token, queries []BranchQuery) (map[string][]MergedPR, error) {
	out, err := fanOut(ctx, mergedRequests(queries), mergedParallel,
		func(ctx context.Context, asks []mergedAsk) (map[string][]MergedPR, error) {
			return c.fetchMerged(ctx, tok, asks)
		})
	if err != nil {
		return nil, Redact(err, tok)
	}

	return out, nil
}

// mergedAsk is one repository's branch names in a merged-branches request.
type mergedAsk struct {
	repo     repo.Name
	branches []string
}

// mergedRequests splits queries into requests of at most mergedBatch branch names,
// dropping empty names, names that are not valid UTF-8, and repositories with none.
func mergedRequests(queries []BranchQuery) [][]mergedAsk {
	var (
		out   [][]mergedAsk
		cur   []mergedAsk
		count int
	)

	for _, q := range queries {
		names := slices.DeleteFunc(slices.Clone(q.Branches), func(b string) bool { return b == "" || !utf8.ValidString(b) })

		for len(names) > 0 {
			take := min(len(names), mergedBatch-count)
			cur = append(cur, mergedAsk{repo: q.Repo, branches: names[:take]})
			names, count = names[take:], count+take

			if count == mergedBatch {
				out, cur, count = append(out, cur), nil, 0
			}
		}
	}

	if len(cur) > 0 {
		out = append(out, cur)
	}

	return out
}

// fetchMerged sends one merged-branches request and returns its answers by MergedKey.
func (c *Client) fetchMerged(ctx context.Context, tok Token, asks []mergedAsk) (map[string][]MergedPR, error) {
	query, err := mergedQuery(asks)
	if err != nil {
		return nil, err
	}

	resp, _, err := postGraphQL[mergedData](ctx, c, tok, "merged branches", graphQLRequest{Query: query}, unlessNotFound)
	if err != nil {
		return nil, err
	}

	out := map[string][]MergedPR{}

	for i, ask := range asks {
		branches := (*resp.Data)[batchAlias(mergedRepoPrefix, i)]
		if branches == nil {
			continue
		}

		for j, name := range ask.branches {
			if prs := branches.pullRequests(batchAlias(mergedBranchPrefix, j)); len(prs) > 0 {
				out[MergedKey(ask.repo, name)] = prs
			}
		}
	}

	return out, nil
}

// mergedQuery asks, for each repository and each of its branch names, for the merged pull
// requests whose head branch had that name. The repository is written by repoField and each
// branch name by graphQLString, so none can break out of its argument. mergedRequests has
// already dropped every name graphQLString would refuse.
func mergedQuery(asks []mergedAsk) (string, error) {
	var b strings.Builder

	b.WriteString("query MergedBranches {\n")

	for i, ask := range asks {
		field, err := repoField(batchAlias(mergedRepoPrefix, i), ask.repo)
		if err != nil {
			return "", err
		}

		fmt.Fprintf(&b, "  %s {\n", field)

		for j, branch := range ask.branches {
			ref, err := graphQLString(branch)
			if err != nil {
				return "", fmt.Errorf("encode a branch of %s: %w", ask.repo, err)
			}

			fmt.Fprintf(&b, "    %s: pullRequests(headRefName: %s, states: MERGED, first: %d, "+
				"orderBy: {field: UPDATED_AT, direction: DESC}) { nodes { number title url mergedAt headRefOid } }\n",
				batchAlias(mergedBranchPrefix, j), ref, mergedPerBranch)
		}

		b.WriteString("  }\n")
	}

	// The query's own cost, so the API budget counts it.
	b.WriteString("  rateLimit { cost }\n}\n")

	return b.String(), nil
}

// mergedData is the merged-branches query's data object: one aliased repository per ask,
// null when GitHub could not find it, or the rate limit, which is no repository.
type mergedData map[string]mergedRepo

// mergedRepo is one aliased repository: its branch aliases, each a connection of merged
// pull requests.
type mergedRepo map[string]json.RawMessage

// mergedConnection is the merged pull requests of one branch name.
type mergedConnection struct {
	Nodes []*struct {
		Number     int       `json:"number"`
		Title      string    `json:"title"`
		URL        string    `json:"url"`
		MergedAt   time.Time `json:"mergedAt"`
		HeadRefOID string    `json:"headRefOid"`
	} `json:"nodes"`
}

// pullRequests returns the merged pull requests under alias, or none when it is missing
// or unreadable.
func (r mergedRepo) pullRequests(alias string) []MergedPR {
	raw, ok := r[alias]
	if !ok {
		return nil
	}

	var conn mergedConnection
	if err := json.Unmarshal(raw, &conn); err != nil {
		return nil
	}

	out := make([]MergedPR, 0, len(conn.Nodes))

	for _, n := range conn.Nodes {
		if n == nil || n.Number <= 0 || n.MergedAt.IsZero() {
			continue
		}

		out = append(out, MergedPR{
			Number: n.Number, Title: n.Title, URL: n.URL, MergedAt: n.MergedAt.UTC(), HeadOID: n.HeadRefOID,
		})
	}

	return out
}
