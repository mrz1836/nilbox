package github

import (
	"context"
	"encoding/json"
)

// ItemsMax is the most items FetchItems reads in one request: GitHub's own limit on the
// IDs one nodes lookup takes.
const ItemsMax = 100

// itemsQuery reads several pull requests or issues by node ID, each as itemQuery reads
// one, in one request, so the inbox can follow the pull requests whose checks are running
// for about a point a round, however many there are. itemsProjectsQuery reads an issue's
// projects too.
const (
	itemsQuery         = itemsQueryHead + detailFieldsFragment
	itemsProjectsQuery = itemsQueryHead + detailFieldsProjectsFragment
)

// itemsQueryHead is the items query without the DetailFields fragment.
const itemsQueryHead = `query Items($ids: [ID!]!) {
  nodes(ids: $ids) { ...ItemFields ...DetailFields }
  rateLimit { cost remaining resetAt limit used }
}
` + itemFieldsFragment + repoFieldsFragment

// ItemUpdates is what FetchItems learned about several items.
type ItemUpdates struct {
	// Items are the items GitHub answered for, by node ID, each Found. One it answered
	// with null for is left out, never read as gone: without each error's path, an item
	// deleted can't be told from a passing problem, and the next whole refresh tells.
	Items map[string]ItemUpdate

	// RateLimit is the GraphQL budget after the request, including its cost.
	RateLimit QueryRate

	// ProjectsRefused reports that GitHub refused the projects for want of a scope, so
	// the items were read without them.
	ProjectsRefused bool
}

// FetchItems reads several pull requests or issues, by node ID, as of now, with what opts
// adds to their details, in one request, as FetchItem reads one. It reads at most ItemsMax;
// the caller asks for no more.
func (c *Client) FetchItems(ctx context.Context, tok Token, ids []string, opts DetailOptions) (*ItemUpdates, error) {
	updates, err := c.fetchItems(ctx, tok, ids[:min(len(ids), ItemsMax)], opts)

	return updates, Redact(err, tok)
}

// fetchItems sends the items query. When GitHub refuses the projects opts asks for, for
// want of a scope, it sends the query again without them.
func (c *Client) fetchItems(ctx context.Context, tok Token, ids []string, opts DetailOptions) (*ItemUpdates, error) {
	updates, err := c.sendItems(ctx, tok, ids, opts)
	if !opts.Projects || !refusedForScope(err) {
		return updates, err
	}

	if updates, err = c.sendItems(ctx, tok, ids, DetailOptions{}); err != nil {
		return nil, err
	}

	updates.ProjectsRefused = true

	return updates, nil
}

// sendItems sends the items query, with what opts adds, and decodes the response. Errors
// with data are about single items, whose nodes come back null; errors without data are
// a real failure.
func (c *Client) sendItems(ctx context.Context, tok Token, ids []string, opts DetailOptions) (*ItemUpdates, error) {
	req := graphQLRequest{Query: opts.itemsQuery(), Variables: map[string]any{"ids": ids}}

	resp, _, err := postGraphQL[itemsData](ctx, c, tok, "items", req, unlessData)
	if err != nil {
		return nil, err
	}

	rate := resp.Data.RateLimit.queryRate()
	out := &ItemUpdates{
		Items: make(map[string]ItemUpdate, len(resp.Data.Nodes)), RateLimit: rate,
		ProjectsRefused: opts.Projects && scopeRefused(resp.Errors),
	}

	for _, raw := range resp.Data.Nodes {
		if isNull(raw) {
			continue
		}

		item, details, err := decodeItemNode(raw)
		if err != nil {
			return nil, err
		}

		out.Items[item.NodeID] = ItemUpdate{Found: true, Item: item, Details: details, RateLimit: rate}
	}

	return out, nil
}

// itemsQuery returns the items query that reads what o asks for.
func (o DetailOptions) itemsQuery() string {
	if o.Projects {
		return itemsProjectsQuery
	}

	return itemsQuery
}

// itemsData is the items query's data object. Each node is kept raw, because the inbox's
// item fields and the details decode from the same object.
type itemsData struct {
	Nodes     []json.RawMessage `json:"nodes"`
	RateLimit graphQLRateLimit  `json:"rateLimit"`
}
