package github

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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

// FuzzGraphQLString checks that graphQLString refuses any value that is not valid UTF-8,
// never rewriting it, and writes any other as one string GraphQL reads back as exactly
// that value: a JSON string, whose escapes are GraphQL's, so its quotes and backslashes
// are escaped, with no C0 control character (U+0000 to U+001F, line breaks among them)
// written raw. No owner, name, branch, or tag can break out of its argument. DEL and the
// C1 control characters, which a GraphQL string may hold, may be written raw.
func FuzzGraphQLString(f *testing.F) {
	for _, seed := range []string{
		"", filesOwner, `acme"org\`, `main\"`, "v1\") { x }\n#", hostileValue,
		"\r\n\t\b\f\x00\x1f", "\x7f\u0080\u0085\u009f", "<&>\u2028\u2029", "bad \xff\xfe", "feat-\xff",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, s string) {
		quoted, err := graphQLString(s)
		if !utf8.ValidString(s) {
			require.ErrorIs(t, err, errNotUTF8)
			assert.Empty(t, quoted)

			return
		}

		require.NoError(t, err)

		var back string
		require.NoError(t, json.Unmarshal([]byte(quoted), &back), "%q is not one string", quoted)
		assert.Equal(t, s, back)
		assert.False(t, strings.ContainsFunc(quoted, func(r rune) bool { return r < 0x20 }),
			"%q holds a raw C0 control character", quoted)
	})
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

// FuzzDecodeIssueView checks that any answer to the issue sheet's view either fails with
// ErrMalformedResponse or ErrGraphQL, without the token in the message, or yields a view
// that is all zero for an issue GitHub can't find, and otherwise one of an issue: never a
// pull request, its lists never nil, its counts never below what it lists, and its
// timeline's items each with an ID, one of the types the sheet reads, and a time in UTC.
func FuzzDecodeIssueView(f *testing.F) {
	for _, seed := range []string{
		`{"data": {"node": {"__typename": "Issue", "id": "I_1", "number": 12, "issueState": "OPEN",
			"repository": {"name": "example-repo", "owner": {"login": "acme-org"}}, "comments": {"totalCount": 2},
			"labels": {"nodes": []}, "assignees": {"totalCount": 1, "nodes": [{"login": "octo-user"}]},
			"subIssueList": {"nodes": [{"__typename": "Issue", "number": 3,
				"repository": {"nameWithOwner": "acme-org/example-repo"}}, null]},
			"fixes": {"totalCount": 1, "nodes": [{"__typename": "PullRequest", "number": 4, "prState": "MERGED",
				"repository": {"nameWithOwner": "acme-org/example-repo"}, "mergedAt": "2026-11-10T08:00:00+02:00"}]},
			"reactionGroups": [{"content": "HEART", "reactors": {"totalCount": 2}}, null],
			"timeline": {"totalCount": 3, "pageInfo": {"hasPreviousPage": true, "startCursor": "c"}, "nodes": [
				{"__typename": "IssueComment", "id": "IC_1", "createdAt": "2026-11-09T10:00:00-05:00", "body": "hi"},
				{"__typename": "ConnectedEvent", "id": "CE_1", "source": {"__typename": "Issue", "number": 12,
					"repository": {"nameWithOwner": "acme-org/example-repo"}}},
				{"__typename": "SubscribedEvent", "id": "SE_1"}, null]}}}}`,
		`{"data": {"node": null}, "errors": [{"type": "NOT_FOUND", "message": "Could not resolve"}]}`,
		`{"data": null, "errors": [{"type": "FORBIDDEN", "message": "` + fakeToken + `"}]}`,
		`{"data": {"node": {"__typename": "PullRequest", "id": "PR_1", "number": 7, "prState": "OPEN",
			"repository": {"name": "example-repo", "owner": {"login": "acme-org"}}}}}`,
		`{"data": {"node": {"__typename": "Issue", "id": "I_1", "number": 1, "repository": {"name": "r"},
			"timeline": {"nodes": {}}}}}`,
		`{}`,
		`null`,
		"\xff\x00{",
	} {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, body []byte) {
		c := &Client{BaseURL: DefaultBaseURL, HTTP: &http.Client{Transport: bodyTransport(body)}, UserAgent: "fuzz"}

		view, err := c.FetchIssueView(t.Context(), NewToken(fakeToken), "I_1", DetailOptions{})
		if err != nil {
			assert.Nil(t, view)
			assert.True(t, errorIsAny(err, ErrMalformedResponse, ErrGraphQL), "unexpected error: %v", err)
			assert.NotContains(t, err.Error(), fakeToken)

			return
		}

		require.NotNil(t, view)
		checkIssueView(t, view)
	})
}

// checkIssueView checks a view FetchIssueView returned.
func checkIssueView(t *testing.T, view *IssueView) {
	t.Helper()

	if !view.Update.Found {
		assert.Empty(t, view.Timeline.Items)
		assert.Empty(t, view.Assignees.Logins)

		return
	}

	assert.False(t, view.Update.Item.IsPR)
	assert.NotNil(t, view.SubIssues)
	assert.NotNil(t, view.Fixes)
	assert.NotNil(t, view.Reactions)
	assert.NotNil(t, view.Assignees.Logins)
	assert.NotNil(t, view.Timeline.Items)
	assert.GreaterOrEqual(t, view.Assignees.Total, len(view.Assignees.Logins))
	assert.GreaterOrEqual(t, view.Participants.Total, len(view.Participants.Logins))
	assert.GreaterOrEqual(t, view.Timeline.Total, len(view.Timeline.Items))
	assert.True(t, view.Timeline.HasEarlier || view.Timeline.Before == "", "a cursor only for earlier items")

	for _, it := range view.Timeline.Items {
		assert.NotEmpty(t, it.ID)
		assert.Contains(t, []string{
			TimelineComment, TimelineCrossReferenced, TimelineConnected, TimelineDisconnected, TimelineClosed,
			TimelineReopened, TimelineAssigned, TimelineUnassigned, TimelineLabeled, TimelineUnlabeled,
			TimelineRenamed, TimelineMarkedAsDuplicate,
		}, it.Type)
		assert.Equal(t, time.UTC, it.CreatedAt.Location())
	}

	for _, fix := range view.Fixes {
		assert.True(t, fix.Ref.IsPR)
		assert.Equal(t, time.UTC, fix.MergedAt.Location())
	}

	for _, r := range view.Reactions {
		assert.Positive(t, r.Count)
	}
}
