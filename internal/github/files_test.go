package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// filesOwner owns the repository the files tests read.
const filesOwner = "acme-org"

// pullRef is the pull request the files tests read.
func pullRef() ItemRef {
	return ItemRef{Owner: filesOwner, Name: "example-repo", Number: 7}
}

// fakePull serves one pull request's REST endpoints: its head, which heads returns in
// turn (the last one repeating), and its files, n of them, the first renamed.
type fakePull struct {
	t     *testing.T
	heads []string
	total int
	n     int

	mu        sync.Mutex
	headCalls int
	pages     []int
}

func (f *fakePull) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	assert.Equal(f.t, "Bearer "+fakeToken, r.Header.Get("Authorization"))
	w.Header().Set("Content-Type", "application/json")

	switch r.URL.Path {
	case "/repos/acme-org/example-repo/pulls/7":
		head := f.heads[min(f.headCalls, len(f.heads)-1)]
		f.headCalls++

		_, _ = fmt.Fprintf(w, `{"state":"open","changed_files":%d,"head":{"sha":%q}}`, f.total, head)
	case "/repos/acme-org/example-repo/pulls/7/files":
		assert.Equal(f.t, "100", r.URL.Query().Get("per_page"))

		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if !assert.NoError(f.t, err) {
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		f.pages = append(f.pages, page)

		_, _ = w.Write(f.page(page))
	default:
		http.NotFound(w, r)
	}
}

// page renders one page of files, with a patch each, as GitHub does.
func (f *fakePull) page(page int) []byte {
	type file struct {
		Filename         string `json:"filename"`
		Status           string `json:"status"`
		PreviousFilename string `json:"previous_filename,omitempty"`
		Patch            string `json:"patch"`
	}

	out := []file{}

	for i := (page - 1) * 100; i < min(page*100, f.n); i++ {
		fl := file{Filename: fmt.Sprintf("dir/file%04d.go", i), Status: "modified", Patch: "@@ -1 +1 @@"}
		if i == 0 {
			fl = file{Filename: "docs/old.yml", Status: "renamed", PreviousFilename: ".github/workflows/ci.yml"}
		}

		out = append(out, fl)
	}

	b, err := json.Marshal(out)
	assert.NoError(f.t, err)

	return b
}

func TestFetchChangedFilesReadsEveryPage(t *testing.T) {
	fake := &fakePull{t: t, heads: []string{"aaa"}, total: 205, n: 205}
	c := newTestClient(t, fake.ServeHTTP)

	got, err := c.FetchChangedFiles(t.Context(), NewToken(fakeToken), pullRef(), "")
	require.NoError(t, err)

	assert.Equal(t, "aaa", got.Head)
	assert.Equal(t, 205, got.Total)
	assert.True(t, got.Complete)
	require.Len(t, got.Files, 205)
	assert.Equal(t, ChangedFile{Path: "docs/old.yml", PreviousPath: ".github/workflows/ci.yml", Status: "renamed"}, got.Files[0])
	assert.Equal(t, ChangedFile{Path: "dir/file0204.go", Status: "modified"}, got.Files[204])
	assert.Equal(t, []int{1, 2, 3}, fake.pages)
	assert.Equal(t, 2, fake.headCalls, "the head is read before and after the list")
}

func TestFetchChangedFilesWithAKnownHeadSkipsTheFirstRead(t *testing.T) {
	fake := &fakePull{t: t, heads: []string{"aaa"}, total: 3, n: 3}
	c := newTestClient(t, fake.ServeHTTP)

	got, err := c.FetchChangedFiles(t.Context(), NewToken(fakeToken), pullRef(), "AAA")
	require.NoError(t, err)

	assert.Equal(t, "aaa", got.Head)
	assert.Len(t, got.Files, 3)
	assert.Equal(t, 1, fake.headCalls)
}

func TestFetchChangedFilesReadsAgainWhenTheHeadMoves(t *testing.T) {
	fake := &fakePull{t: t, heads: []string{"aaa", "bbb", "bbb"}, total: 2, n: 2}
	c := newTestClient(t, fake.ServeHTTP)

	got, err := c.FetchChangedFiles(t.Context(), NewToken(fakeToken), pullRef(), "")
	require.NoError(t, err)

	assert.Equal(t, "bbb", got.Head)
	assert.Equal(t, []int{1, 1}, fake.pages)
	assert.Equal(t, 3, fake.headCalls)
}

func TestFetchChangedFilesGivesUpOnAHeadThatKeepsMoving(t *testing.T) {
	fake := &fakePull{t: t, heads: []string{"a", "b", "c", "d"}, total: 1, n: 1}
	c := newTestClient(t, fake.ServeHTTP)

	_, err := c.FetchChangedFiles(t.Context(), NewToken(fakeToken), pullRef(), "")
	require.ErrorIs(t, err, ErrHeadMoved)
}

func TestFetchChangedFilesStopsAtGitHubsLimit(t *testing.T) {
	fake := &fakePull{t: t, heads: []string{"aaa"}, total: 3500, n: 3500}
	c := newTestClient(t, fake.ServeHTTP)

	got, err := c.FetchChangedFiles(t.Context(), NewToken(fakeToken), pullRef(), "aaa")
	require.NoError(t, err)

	assert.Len(t, got.Files, 3000)
	assert.Equal(t, 3500, got.Total)
	assert.False(t, got.Complete)
	assert.Len(t, fake.pages, 30)
}

func TestFetchChangedFilesRedactsErrors(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found for ` + fakeToken + `"}`))
	})

	_, err := c.FetchChangedFiles(t.Context(), NewToken(fakeToken), pullRef(), "aaa")
	require.ErrorIs(t, err, ErrUnexpectedStatus)
	assert.Contains(t, err.Error(), "acme-org/example-repo#7")
	assert.NotContains(t, err.Error(), fakeToken)
}

func TestFetchChangedFilesRefusesABadReference(t *testing.T) {
	c := newTestClient(t, func(http.ResponseWriter, *http.Request) {
		t.Fatal("no request is sent")
	})

	for _, ref := range []ItemRef{{Name: "x", Number: 1}, {Owner: "x", Number: 1}, {Owner: "x", Name: "y"}} {
		_, err := c.FetchChangedFiles(t.Context(), NewToken(fakeToken), ref, "")
		require.ErrorIs(t, err, ErrInvalidItemRef)

		_, err = c.FetchPullHead(t.Context(), NewToken(fakeToken), ref)
		require.ErrorIs(t, err, ErrInvalidItemRef)
	}
}

func TestFetchPullHead(t *testing.T) {
	fake := &fakePull{t: t, heads: []string{"abc123"}, total: 12, n: 12}
	c := newTestClient(t, fake.ServeHTTP)

	got, err := c.FetchPullHead(t.Context(), NewToken(fakeToken), pullRef())
	require.NoError(t, err)
	assert.Equal(t, &PullHead{Head: "abc123", ChangedFiles: 12, State: "open"}, got)
}

func TestFetchPullHeadRefusesAnswersWithoutAHead(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"state":"open","head":{}}`))
	})

	_, err := c.FetchPullHead(t.Context(), NewToken(fakeToken), pullRef())
	require.ErrorIs(t, err, ErrMalformedResponse)
}

func TestPullPathEscapesItsParts(t *testing.T) {
	assert.Equal(t, "/repos/a%2Fb/c%3Fd/pulls/3", pullPath(ItemRef{Owner: "a/b", Name: "c?d", Number: 3}))
}
