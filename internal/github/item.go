package github

import (
	"context"
	"encoding/json"
	"fmt"
)

// errorNotFound is GitHub's error type for an ID it cannot resolve: the node was deleted,
// or the token cannot see it.
const errorNotFound = "NOT_FOUND"

// itemQuery reads one pull request or issue by node ID: what the inbox query reads about
// it, its state included, and what the details query reads, in one request. It costs one
// point, where a whole refresh costs a few, so a single card can be brought up to date
// without asking about every other one. itemProjectsQuery reads an issue's projects too,
// as detailsProjectsQuery does.
const (
	itemQuery         = itemQueryHead + detailFieldsFragment
	itemProjectsQuery = itemQueryHead + detailFieldsProjectsFragment
)

// itemQueryHead is the item query without the DetailFields fragment.
const itemQueryHead = `query Item($id: ID!) {
  node(id: $id) { ...ItemFields ...DetailFields }
  rateLimit { cost remaining resetAt limit used }
}
` + itemFieldsFragment + repoFieldsFragment

// ItemUpdate is what FetchItem learned about one item.
type ItemUpdate struct {
	// Found is false when GitHub could not resolve the ID, because the item was deleted
	// or the token can no longer see it. Item and Details are zero then.
	Found bool

	Item    Item
	Details ItemDetails

	// RateLimit is the GraphQL budget after the request, including its cost.
	RateLimit QueryRate

	// ProjectsRefused reports that GitHub refused the projects for want of a scope, so
	// the item was read without them.
	ProjectsRefused bool
}

// FetchItem reads one pull request or issue, by node ID, as of now, with what opts adds
// to its details. Unlike the inbox query's searches, it never waits on GitHub's index, so
// it sees a merge or a close at once.
func (c *Client) FetchItem(ctx context.Context, tok Token, id string, opts DetailOptions) (*ItemUpdate, error) {
	update, err := c.fetchItem(ctx, tok, id, opts)

	return update, Redact(err, tok)
}

// fetchItem sends the item query. When GitHub refuses the projects opts asks for, for
// want of a scope, it sends the query again without them.
func (c *Client) fetchItem(ctx context.Context, tok Token, id string, opts DetailOptions) (*ItemUpdate, error) {
	update, err := c.sendItem(ctx, tok, id, opts)
	if !opts.Projects || !refusedForScope(err) {
		return update, err
	}

	if update, err = c.sendItem(ctx, tok, id, DetailOptions{}); err != nil {
		return nil, err
	}

	update.ProjectsRefused = true

	return update, nil
}

// sendItem sends the item query, with what opts adds, and decodes the response.
func (c *Client) sendItem(ctx context.Context, tok Token, id string, opts DetailOptions) (*ItemUpdate, error) {
	req := graphQLRequest{Query: opts.itemQuery(), Variables: map[string]any{"id": id}}

	resp, _, err := postGraphQL[itemData](ctx, c, tok, "item", req, unlessData)
	if err != nil {
		return nil, err
	}

	update, err := itemUpdate(resp)
	if err != nil {
		return nil, err
	}

	update.ProjectsRefused = opts.Projects && scopeRefused(resp.Errors)

	return update, nil
}

// itemData is the item query's data object. The node is kept raw, because the inbox's
// item fields and the details decode from the same object.
type itemData struct {
	Node      json.RawMessage  `json:"node"`
	RateLimit graphQLRateLimit `json:"rateLimit"`
}

// itemUpdate converts the response. A null node is an item that is gone only when GitHub
// says it could not find it; a null node for any other error is a failure, so a passing
// problem never takes an item out of the inbox.
func itemUpdate(r *graphQLResponse[itemData]) (*ItemUpdate, error) {
	update := &ItemUpdate{RateLimit: r.Data.RateLimit.queryRate()}

	if gone, err := goneNode(r); gone || err != nil {
		if err != nil {
			return nil, err
		}

		return update, nil
	}

	item, details, err := decodeItemNode(r.Data.Node)
	if err != nil {
		return nil, err
	}

	update.Found, update.Item, update.Details = true, item, details

	return update, nil
}

// decodeItemNode decodes one item node, which holds both the inbox's item fields and the
// details.
func decodeItemNode(raw json.RawMessage) (Item, ItemDetails, error) {
	var (
		node    graphQLNode
		details detailsNode
	)

	if err := json.Unmarshal(raw, &node); err != nil {
		return Item{}, ItemDetails{}, fmt.Errorf("%w: decode the item: %w", ErrMalformedResponse, err)
	}

	if err := json.Unmarshal(raw, &details); err != nil {
		return Item{}, ItemDetails{}, fmt.Errorf("%w: decode the item's details: %w", ErrMalformedResponse, err)
	}

	item, ok := node.item()
	if !ok {
		return Item{}, ItemDetails{}, fmt.Errorf("%w: the ID is not a pull request or an issue", ErrMalformedResponse)
	}

	_, itemDetails, ok := details.details()
	if !ok {
		return Item{}, ItemDetails{}, fmt.Errorf("%w: the item has no details", ErrMalformedResponse)
	}

	return item, itemDetails, nil
}

// goneNode reports a null node GitHub could not find, which is an item that is gone; a
// null node for any other error is that error, so a passing problem never reads as gone.
func goneNode(r *graphQLResponse[itemData]) (bool, error) {
	if !isNull(r.Data.Node) {
		return false, nil
	}

	for _, e := range r.Errors {
		if e.Type != errorNotFound {
			return false, graphQLErr(r.Errors)
		}
	}

	return true, nil
}

// isNull reports a JSON value that is absent or null.
func isNull(raw json.RawMessage) bool {
	return len(raw) == 0 || string(raw) == "null"
}
