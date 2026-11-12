package github

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// itemsRequest is the body FetchItems sends.
type itemsRequest struct {
	Query     string `json:"query"`
	Variables struct {
		IDs []string `json:"ids"`
	} `json:"variables"`
}

// itemsBody is an items answer: the item fixture's pull request, then a null for an ID
// GitHub couldn't find, with its error.
func itemsBody(t *testing.T) []byte {
	t.Helper()

	var single struct {
		Data struct {
			Node      json.RawMessage `json:"node"`
			RateLimit json.RawMessage `json:"rateLimit"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(fixture(t, "item_response.json"), &single))

	body, err := json.Marshal(map[string]any{
		"data": map[string]any{
			"nodes":     []json.RawMessage{single.Data.Node, json.RawMessage("null")},
			"rateLimit": single.Data.RateLimit,
		},
		"errors": []map[string]any{{"type": "NOT_FOUND", "message": "Could not resolve to a node"}},
	})
	require.NoError(t, err)

	return body
}

func TestFetchItemsReadsSeveralItemsInOneRequest(t *testing.T) {
	t.Parallel()

	body := itemsBody(t)
	requests := 0

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++

		raw, err := io.ReadAll(r.Body)
		assert.NoError(t, err)

		var req itemsRequest
		assert.NoError(t, json.Unmarshal(raw, &req))
		assert.Contains(t, req.Query, "nodes(ids: $ids) { ...ItemFields ...DetailFields }")
		assert.Contains(t, req.Query, "rateLimit { cost remaining resetAt limit used }")
		assert.Equal(t, []string{"PR_fakeNodeId12", "PR_fakeGone"}, req.Variables.IDs)

		_, _ = w.Write(body)
	})

	got, err := client.FetchItems(t.Context(), NewToken(fakeToken), []string{"PR_fakeNodeId12", "PR_fakeGone"},
		DetailOptions{})
	require.NoError(t, err)
	assert.Equal(t, 1, requests)

	require.Len(t, got.Items, 1, "a null node is left out, never read as gone")
	update := got.Items["PR_fakeNodeId12"]
	assert.True(t, update.Found)
	assert.Equal(t, "Add a dark mode toggle", update.Item.Title)
	assert.Equal(t, 120, update.Details.Additions)
	assert.Equal(t, got.RateLimit, update.RateLimit)
	assert.False(t, got.ProjectsRefused)
}

func TestFetchItemsReadsAtMostOneLookupsWorth(t *testing.T) {
	t.Parallel()

	ids := make([]string, ItemsMax+5)
	for i := range ids {
		ids[i] = "PR_fake" + strings.Repeat("x", i%7)
	}

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var req itemsRequest
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Len(t, req.Variables.IDs, ItemsMax)

		_, _ = io.WriteString(w, `{"data":{"nodes":[],"rateLimit":{"cost":1,"remaining":4999}}}`)
	})

	got, err := client.FetchItems(t.Context(), NewToken(fakeToken), ids, DetailOptions{})
	require.NoError(t, err)
	assert.Empty(t, got.Items)
}

func TestFetchItemsFailsWithoutData(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, serveItem(`{"errors":[{"type":"FORBIDDEN","message":"no"}]}`))

	_, err := client.FetchItems(t.Context(), NewToken(fakeToken), []string{"PR_fakeNodeId12"}, DetailOptions{})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), fakeToken)
}
