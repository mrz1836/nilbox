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
// without asking about every other one.
const itemQuery = `query Item($id: ID!) {
  node(id: $id) { ...ItemFields ...DetailFields }
  rateLimit { cost remaining resetAt limit used }
}
` + itemFieldsFragment + repoFieldsFragment + detailFieldsFragment

// ItemUpdate is what FetchItem learned about one item.
type ItemUpdate struct {
	// Found is false when GitHub could not resolve the ID, because the item was deleted
	// or the token can no longer see it. Item and Details are zero then.
	Found bool

	Item    Item
	Details ItemDetails

	// RateLimit is the GraphQL budget after the request, including its cost.
	RateLimit QueryRate
}

// FetchItem reads one pull request or issue, by node ID, as of now. Unlike the inbox
// query's searches, it never waits on GitHub's index, so it sees a merge or a close at
// once.
func (c *Client) FetchItem(ctx context.Context, tok Token, id string) (*ItemUpdate, error) {
	update, err := c.fetchItem(ctx, tok, id)

	return update, Redact(err, tok)
}

// fetchItem sends the item query and decodes the response.
func (c *Client) fetchItem(ctx context.Context, tok Token, id string) (*ItemUpdate, error) {
	req := graphQLRequest{Query: itemQuery, Variables: map[string]any{"id": id}}

	resp, _, err := postGraphQL[itemData](ctx, c, tok, "item", req, unlessData)
	if err != nil {
		return nil, err
	}

	return itemUpdate(resp)
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

	if isNull(r.Data.Node) {
		for _, e := range r.Errors {
			if e.Type != errorNotFound {
				return nil, graphQLErr(r.Errors)
			}
		}

		return update, nil
	}

	var (
		node    graphQLNode
		details detailsNode
	)

	if err := json.Unmarshal(r.Data.Node, &node); err != nil {
		return nil, fmt.Errorf("%w: decode the item: %w", ErrMalformedResponse, err)
	}

	if err := json.Unmarshal(r.Data.Node, &details); err != nil {
		return nil, fmt.Errorf("%w: decode the item's details: %w", ErrMalformedResponse, err)
	}

	item, ok := node.item()
	if !ok {
		return nil, fmt.Errorf("%w: the ID is not a pull request or an issue", ErrMalformedResponse)
	}

	if _, update.Details, ok = details.details(); !ok {
		return nil, fmt.Errorf("%w: the item has no details", ErrMalformedResponse)
	}

	update.Found, update.Item = true, item

	return update, nil
}

// isNull reports a JSON value that is absent or null.
func isNull(raw json.RawMessage) bool {
	return len(raw) == 0 || string(raw) == "null"
}
