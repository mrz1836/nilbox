package github

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/item"
)

// FuzzParseSubject checks that any subject URL either names no item, with a zero Ref, or
// is on GitHub's API host and names a valid pull request or issue whose path, built again
// from the Ref, is the URL's own path and parses back to the same Ref; and that a comment
// URL the client would read is on that host too, its path built again from what was
// checked.
func FuzzParseSubject(f *testing.F) {
	api, host := DefaultBaseURL, strings.TrimPrefix(DefaultBaseURL, "https://")

	for _, seed := range []string{
		api + "/repos/acme-org/example-repo/pulls/42",
		api + "/repos/acme-org/example-repo/issues/7",
		api + "/repos/acme-org/example-repo/releases/123456789",
		api + "/repos/acme-org/example-repo/issues/comments/30000000001",
		api + "/repos/acme-org/example-repo/pulls/comments/30000000001",
		api + "/repos/acme-org/example-repo/pulls/42/files",
		api + "/repos/acme-org/example-repo/pulls/042",
		api + "/repos/%61cme-org/example-repo/pulls/42",
		api + "/repos/acme-org%2Fexample-repo/pulls/42",
		api + ".example.com/repos/acme-org/example-repo/pulls/42",
		api + "@example.com/repos/acme-org/example-repo/pulls/42",
		"https://example.com\\@" + host + "/repos/acme-org/example-repo/pulls/42",
		api + ":443/repos/acme-org/example-repo/pulls/42",
		"http://" + host + "/repos/acme-org/example-repo/pulls/42",
		"//" + host + "/repos/acme-org/example-repo/pulls/42",
		api + "/repos/acme-org/example-repo/pulls/42?x=1#y",
		api + "/repos/-acme/../pulls/9999999999999999999",
		"",
		"\x00",
	} {
		f.Add(seed)
	}

	base, err := url.Parse(DefaultBaseURL)
	require.NoError(f, err)

	f.Fuzz(func(t *testing.T, raw string) {
		ref, ok := parseSubject(base, raw)
		if !ok {
			assert.Equal(t, item.Ref{}, ref)
		} else {
			u, err := url.Parse(raw)
			require.NoError(t, err)
			assert.Equal(t, host, u.Host, "no host but the API's")
			assert.Equal(t, "https", u.Scheme)
			assert.True(t, ref.Repo.Valid())
			assert.Positive(t, ref.Number)

			collection := map[item.Kind]string{item.KindPR: "pulls", item.KindIssue: "issues"}[ref.Kind]
			require.NotEmpty(t, collection, "a kind is pr or issue")

			path := "/repos/" + ref.Repo.Owner + "/" + ref.Repo.Repo + "/" + collection + "/" + strconv.Itoa(ref.Number)
			assert.Equal(t, path, u.EscapedPath(), "the ref round-trips through the path")

			again, okAgain := parseSubject(base, DefaultBaseURL+path)
			assert.True(t, okAgain)
			assert.Equal(t, ref, again)
		}

		if path, ok := commentPath(base, raw); ok {
			u, err := url.Parse(raw)
			require.NoError(t, err)
			assert.Equal(t, host, u.Host, "no host but the API's")
			assert.Equal(t, u.EscapedPath(), path, "the path sent is the one checked")
			assert.True(t, strings.HasPrefix(path, "/repos/"))

			again, okAgain := commentPath(base, DefaultBaseURL+path)
			assert.True(t, okAgain)
			assert.Equal(t, path, again)
		}
	})
}

// FuzzDecodeThreads checks that any listing body either fails with ErrMalformedResponse,
// without the token in the message, or yields threads with positive IDs whose Ref is
// zero or a valid pull request or issue, with every listed thread kept or counted as
// dropped.
func FuzzDecodeThreads(f *testing.F) {
	for _, seed := range []string{
		`[{"id":"20000000001","unread":true,"reason":"mention","updated_at":"2026-11-12T18:04:05Z","last_read_at":null,
			"subject":{"title":"A","url":"` + DefaultBaseURL + `/repos/acme-org/example-repo/pulls/42",
			"latest_comment_url":"` + DefaultBaseURL + `/repos/acme-org/example-repo/issues/comments/1","type":"PullRequest"},
			"repository":{"full_name":"acme-org/example-repo"}}]`,
		`[{"id":"27000000000","subject":{"url":null,"latest_comment_url":null,"type":"CheckSuite"}}]`,
		`[{"id":"abc"},{"id":"-1"},{"id":"0"},{"id":7},{"id":"99999999999999999999"},null,"x",{}]`,
		`[{"id":"1","updated_at":"yesterday"},{"id":"2","repository":{"full_name":"../.."}}]`,
		`[{"id":"3","subject":{"url":"https://api.example.com/repos/acme-org/example-repo/issues/1"}}]`,
		`[]`,
		`null`,
		`{"message":"` + fakeToken + `"}`,
		"\xff\x00[",
	} {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, body []byte) {
		c := &Client{BaseURL: DefaultBaseURL, HTTP: &http.Client{Transport: bodyTransport(body)}, UserAgent: "fuzz"}

		page, err := c.ListThreads(t.Context(), NewToken(fakeToken), ThreadQuery{})
		if err != nil {
			require.ErrorIs(t, err, ErrMalformedResponse)
			assert.NotContains(t, err.Error(), fakeToken)

			return
		}

		var listed []json.RawMessage
		require.NoError(t, json.Unmarshal(body, &listed))
		assert.Len(t, listed, len(page.Threads)+page.Dropped, "every thread is kept or counted")
		assert.False(t, page.NotModified)
		assert.Zero(t, page.NextPage)
		assert.False(t, page.Capped)
		assert.NotNil(t, page.Threads)

		for _, thread := range page.Threads {
			checkThread(t, thread)
		}
	})
}

// checkThread checks one decoded thread: a positive ID, a time in UTC, and a Ref and a
// repository that are zero or valid.
func checkThread(t *testing.T, thread Thread) {
	t.Helper()

	assert.Positive(t, thread.ID)
	assert.Equal(t, time.UTC, thread.UpdatedAt.Location())

	if thread.Ref != (item.Ref{}) {
		assert.True(t, thread.Ref.Kind.Valid())
		assert.True(t, thread.Ref.Repo.Valid())
		assert.Positive(t, thread.Ref.Number)
	}

	if thread.Repo.Owner != "" {
		assert.True(t, thread.Repo.Valid())
	}
}
