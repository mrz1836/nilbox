package github

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// benchLatency is how long the benchmark server takes to answer each request, standing
// in for GitHub's round trip.
const benchLatency = 5 * time.Millisecond

// benchClient returns a client pointed at a local server that answers with handler after
// benchLatency.
func benchClient(b *testing.B, handler http.HandlerFunc) *Client {
	b.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(benchLatency)
		handler(w, r)
	}))
	b.Cleanup(srv.Close)

	return &Client{BaseURL: srv.URL, HTTP: srv.Client(), UserAgent: testUserAgent}
}

// BenchmarkDecodeInbox decodes the recorded inbox response into an Inbox, the way every
// inbox refresh decodes the one GraphQL response it gets: the JSON decoding and the
// conversion to items, without the request around them.
func BenchmarkDecodeInbox(b *testing.B) {
	raw, err := os.ReadFile(filepath.Join("testdata", "inbox_response.json"))
	require.NoError(b, err)

	var recorded graphQLResponse[graphQLData]
	require.NoError(b, json.Unmarshal(raw, &recorded))
	require.NotNil(b, recorded.Data)
	require.NotEmpty(b, recorded.Data.inbox().ReviewRequested.Items)

	b.ReportAllocs()
	b.SetBytes(int64(len(raw)))

	for b.Loop() {
		var resp graphQLResponse[graphQLData]
		if err = json.Unmarshal(raw, &resp); err != nil {
			b.Fatal(err)
		}

		_ = resp.Data.inbox()
	}
}

// BenchmarkFetchItemStates asks about 200 items, four requests' worth, as a sweep of a
// clone root with many linked worktrees does.
func BenchmarkFetchItemStates(b *testing.B) {
	client := benchClient(b, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data": {}}`)
	})

	refs := statesRefs(4 * statesBatch)

	b.ReportAllocs()

	for b.Loop() {
		if _, err := client.FetchItemStates(b.Context(), NewToken(fakeToken), refs); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkFetchReleaseStatus asks about 150 repositories, ten queries' worth, as the
// release list of a clone root with many clones does.
func BenchmarkFetchReleaseStatus(b *testing.B) {
	client := benchClient(b, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data": {}}`)
	})

	repos := releaseRepos(10 * releaseBatch)

	b.ReportAllocs()

	for b.Loop() {
		if _, err := client.FetchReleaseStatus(b.Context(), NewToken(fakeToken), repos, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// benchPullFiles is how many files the benchmark pull request changes: ten pages.
const benchPullFiles = 10 * filesPerPage

// servePullFiles answers a pull request's head and its files, benchPullFiles of them, with
// the Link header GitHub sends with each page.
func servePullFiles(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if !strings.HasSuffix(r.URL.Path, "/files") {
		_, _ = fmt.Fprintf(w, `{"state":"open","changed_files":%d,"head":{"sha":"aaa"}}`, benchPullFiles)

		return
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	last := benchPullFiles / filesPerPage
	w.Header().Set("Link", fmt.Sprintf(`<https://api.example.test%s?per_page=100&page=%d>; rel="last"`, r.URL.Path, last))

	var body strings.Builder

	body.WriteString("[")

	for i := range filesPerPage {
		if page > last {
			break
		}

		if i > 0 {
			body.WriteString(",")
		}

		fmt.Fprintf(&body, `{"filename":"dir/file%04d.go","status":"modified"}`, (page-1)*filesPerPage+i)
	}

	body.WriteString("]")

	_, _ = io.WriteString(w, body.String())
}

// BenchmarkFetchChangedFiles lists a pull request's files, ten pages of them.
func BenchmarkFetchChangedFiles(b *testing.B) {
	client := benchClient(b, servePullFiles)

	files, err := client.FetchChangedFiles(b.Context(), NewToken(fakeToken), pullRef(), "aaa")
	require.NoError(b, err)
	require.Len(b, files.Files, benchPullFiles)

	b.ReportAllocs()

	for b.Loop() {
		if _, err = client.FetchChangedFiles(b.Context(), NewToken(fakeToken), pullRef(), "aaa"); err != nil {
			b.Fatal(err)
		}
	}
}
