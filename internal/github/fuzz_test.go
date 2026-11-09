package github

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
)

// bodyTransport answers every request with one fixed 200 response body, without a
// network.
type bodyTransport []byte

// RoundTrip returns the body as a JSON response.
func (b bodyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(b)),
		Request:    req,
	}, nil
}

// FuzzFetchItemStates checks that any states response body either fails with
// ErrMalformedResponse or ErrGraphQL, without the token in the message, or yields states
// only for the refs asked about, keyed by their keys, each with a state, a close time in
// UTC, and a head commit only for a pull request.
func FuzzFetchItemStates(f *testing.F) {
	for _, seed := range []string{
		`{"data": {"r0": {"item": {"__typename": "PullRequest", "title": "Add locktimes", "prState": "MERGED",
			"closedAt": "2026-11-06T18:00:00Z", "mergedAt": "2026-11-06T18:00:05-05:00", "headRefOid": "fc1c397"}},
			"r1": {"item": {"__typename": "Issue", "issueState": "CLOSED", "closedAt": "2026-11-05T10:00:00+02:00"}},
			"r2": null}}`,
		`{"data": {"r0": null}, "errors": [{"type": "NOT_FOUND", "message": "Could not resolve"}]}`,
		`{"data": null, "errors": [{"type": "FORBIDDEN", "message": "` + fakeToken + `"}]}`,
		`{"data": {"r1": {"item": {"__typename": "Issue", "issueState": ""}}, "r9": {"item": {"__typename": "Issue", "issueState": "OPEN"}}}}`,
		`{"data": {"r0": {"item": {"__typename": "Commit", "prState": "OPEN"}}}}`,
		`{"data": {"r0": {"item": {"__typename": "PullRequest", "prState": "OPEN", "closedAt": "yesterday"}}}}`,
		`{}`,
		`null`,
		"\xff\x00{",
	} {
		f.Add([]byte(seed))
	}

	refs := fuzzStateRefs()

	f.Fuzz(func(t *testing.T, body []byte) {
		c := &Client{BaseURL: DefaultBaseURL, HTTP: &http.Client{Transport: bodyTransport(body)}, UserAgent: "fuzz"}

		states, err := c.FetchItemStates(t.Context(), NewToken(fakeToken), refs)
		if err != nil {
			assert.Nil(t, states)
			assert.True(t, errorIsAny(err, ErrMalformedResponse, ErrGraphQL), "unexpected error: %v", err)
			assert.NotContains(t, err.Error(), fakeToken)

			return
		}

		require.NotNil(t, states)
		checkStates(t, refs, states)
	})
}

// fuzzStateRefs are the refs the states fuzz test asks about: two items of one
// repository written in different casings, and one of another repository.
func fuzzStateRefs() []item.Ref {
	example := repo.Name{Owner: filesOwner, Repo: "example-repo"}
	shouted := repo.Name{Owner: "Acme-Org", Repo: "Example-Repo"}
	other := repo.Name{Owner: filesOwner, Repo: "other-repo"}

	return []item.Ref{
		{Repo: example, Number: 184, Kind: item.KindPR},
		{Repo: shouted, Number: 7, Kind: item.KindIssue},
		{Repo: other, Number: 3},
	}
}

// checkStates checks the states read for refs.
func checkStates(t *testing.T, refs []item.Ref, states map[string]ItemState) {
	t.Helper()

	keys := make(map[string]bool, len(refs))
	for _, ref := range refs {
		keys[ref.Key()] = true
	}

	for key, state := range states {
		assert.True(t, keys[key], "state for %q, which was not asked about", key)
		assert.NotEmpty(t, state.State, "state of %q", key)
		assert.Equal(t, time.UTC, state.ClosedAt.Location(), "close time of %q", key)

		if !state.PullRequest {
			assert.Empty(t, state.HeadOID, "head commit of issue %q", key)
		}
	}
}

// errorIsAny reports whether err matches any of targets.
func errorIsAny(err error, targets ...error) bool {
	for _, target := range targets {
		if errors.Is(err, target) {
			return true
		}
	}

	return false
}

// FuzzDecodeFiles checks that any page of files either fails to decode or keeps exactly
// the named entries, in order, never more than the page listed, with no patch kept.
func FuzzDecodeFiles(f *testing.F) {
	for _, seed := range []string{
		`[{"filename":"a.go","status":"modified","sha":"abc","additions":1,"deletions":2,"patch":"@@ -1 +1 @@\n-a\n+b"}]`,
		`[{"filename":"img.png","status":"added"},{"filename":""},null,{"filename":"b","previous_filename":"a","status":"renamed"}]`,
		`[]`, `null`, `{}`, `[1]`, `not json`,
	} {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, raw []byte) {
		files, listed, err := decodeFiles(raw, changedFile)
		if err != nil {
			assert.Nil(t, files)

			return
		}

		assert.LessOrEqual(t, len(files), listed)

		for _, file := range files {
			assert.NotEmpty(t, file.Path)
		}
	})
}
