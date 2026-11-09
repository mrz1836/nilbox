package github

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
)

// filesOwner owns the repository the files tests read.
const filesOwner = "acme-org"

// pullRef is the pull request the files tests read.
func pullRef() item.Ref {
	return item.Ref{Repo: repo.Name{Owner: filesOwner, Repo: "example-repo"}, Number: 7}
}

// fakePull serves one pull request's REST endpoints: its head, which heads returns in
// turn (the last one repeating), and its files, n of them, the first renamed. With link
// set, each page of files names the last page in its Link header, as GitHub's do.
type fakePull struct {
	t     *testing.T
	heads []string
	total int
	n     int
	link  bool

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

		if last := (f.n + filesPerPage - 1) / filesPerPage; f.link && last > 1 {
			w.Header().Set("Link", fmt.Sprintf(`<https://api.example.test/repositories/1/pulls/7/files?per_page=100&page=%d>; `+
				`rel="next", <https://api.example.test/repositories/1/pulls/7/files?per_page=100&page=%d>; rel="last"`,
				min(page+1, last), last))
		}

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
	t.Parallel()

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
	t.Parallel()

	fake := &fakePull{t: t, heads: []string{"aaa"}, total: 3, n: 3}
	c := newTestClient(t, fake.ServeHTTP)

	got, err := c.FetchChangedFiles(t.Context(), NewToken(fakeToken), pullRef(), "AAA")
	require.NoError(t, err)

	assert.Equal(t, "aaa", got.Head)
	assert.Len(t, got.Files, 3)
	assert.Equal(t, 1, fake.headCalls)
}

func TestFetchChangedFilesReadsAgainWhenTheHeadMoves(t *testing.T) {
	t.Parallel()

	fake := &fakePull{t: t, heads: []string{"aaa", "bbb", "bbb"}, total: 2, n: 2}
	c := newTestClient(t, fake.ServeHTTP)

	got, err := c.FetchChangedFiles(t.Context(), NewToken(fakeToken), pullRef(), "")
	require.NoError(t, err)

	assert.Equal(t, "bbb", got.Head)
	assert.Equal(t, []int{1, 1}, fake.pages)
	assert.Equal(t, 3, fake.headCalls)
}

func TestFetchChangedFilesGivesUpOnAHeadThatKeepsMoving(t *testing.T) {
	t.Parallel()

	fake := &fakePull{t: t, heads: []string{"a", "b", "c", "d"}, total: 1, n: 1}
	c := newTestClient(t, fake.ServeHTTP)

	_, err := c.FetchChangedFiles(t.Context(), NewToken(fakeToken), pullRef(), "")
	require.ErrorIs(t, err, ErrHeadMoved)
}

func TestFetchChangedFilesStopsAtGitHubsLimit(t *testing.T) {
	t.Parallel()

	fake := &fakePull{t: t, heads: []string{"aaa"}, total: 3500, n: 3500}
	c := newTestClient(t, fake.ServeHTTP)

	got, err := c.FetchChangedFiles(t.Context(), NewToken(fakeToken), pullRef(), "aaa")
	require.NoError(t, err)

	assert.Len(t, got.Files, 3000)
	assert.Equal(t, 3500, got.Total)
	assert.False(t, got.Complete)
	assert.Len(t, fake.pages, 30)
}

func TestFetchChangedFilesReadsTheLinkedPagesInOrder(t *testing.T) {
	t.Parallel()

	fake := &fakePull{t: t, heads: []string{"aaa"}, total: 1050, n: 1050, link: true}
	c := newTestClient(t, fake.ServeHTTP)

	got, err := c.FetchChangedFiles(t.Context(), NewToken(fakeToken), pullRef(), "aaa")
	require.NoError(t, err)

	require.Len(t, got.Files, 1050)
	assert.True(t, got.Complete)

	for i, f := range got.Files[1:] {
		assert.Equal(t, fmt.Sprintf("dir/file%04d.go", i+1), f.Path, "the pages are joined in order")
	}

	assert.ElementsMatch(t, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}, fake.pages, "each page is read once")
}

func TestFetchChangedFilesReadsTheLinkedPagesSideBySide(t *testing.T) {
	t.Parallel()

	// Pages 2 and 3 each wait until the other has arrived, which only happens when they
	// are read side by side; read one after another, the first gives up and fails.
	var arrived sync.WaitGroup

	arrived.Add(2)

	both := make(chan struct{})

	go func() {
		arrived.Wait()
		close(both)
	}()

	fake := &fakePull{t: t, heads: []string{"aaa"}, total: 300, n: 300, link: true}
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if page := r.URL.Query().Get("page"); page == "2" || page == "3" {
			arrived.Done()

			select {
			case <-both:
			case <-time.After(waitTimeout):
				w.WriteHeader(http.StatusGatewayTimeout)

				return
			}
		}

		fake.ServeHTTP(w, r)
	})

	got, err := c.FetchChangedFiles(t.Context(), NewToken(fakeToken), pullRef(), "aaa")
	require.NoError(t, err)
	assert.Len(t, got.Files, 300)
}

func TestFetchChangedFilesStopsAtGitHubsLimitWhenLinked(t *testing.T) {
	t.Parallel()

	fake := &fakePull{t: t, heads: []string{"aaa"}, total: 3500, n: 3500, link: true}
	c := newTestClient(t, fake.ServeHTTP)

	got, err := c.FetchChangedFiles(t.Context(), NewToken(fakeToken), pullRef(), "aaa")
	require.NoError(t, err)

	assert.Len(t, got.Files, 3000)
	assert.False(t, got.Complete)
	assert.Len(t, fake.pages, 30, "pages past GitHub's limit are not asked for")
}

func TestFetchChangedFilesEndsAtAPageThatIsNotFull(t *testing.T) {
	t.Parallel()

	// GitHub's last page says 3, but page 2 comes back short, so the list ends there, as
	// it would read in turn.
	fake := &fakePull{t: t, heads: []string{"aaa"}, total: 300, n: 300, link: true}
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			w.Header().Set("Link", `<https://api.example.test/x?page=3>; rel="last"`)
			_, _ = io.WriteString(w, `[{"filename":"short.go","status":"added"}]`)

			return
		}

		fake.ServeHTTP(w, r)
	})

	got, err := c.FetchChangedFiles(t.Context(), NewToken(fakeToken), pullRef(), "aaa")
	require.NoError(t, err)
	require.Len(t, got.Files, 101)
	assert.Equal(t, "short.go", got.Files[100].Path)
}

func TestFetchChangedFilesFailsWhenALinkedPageFails(t *testing.T) {
	t.Parallel()

	fake := &fakePull{t: t, heads: []string{"aaa"}, total: 300, n: 300, link: true}
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "3" {
			w.WriteHeader(http.StatusBadGateway)

			return
		}

		fake.ServeHTTP(w, r)
	})

	_, err := c.FetchChangedFiles(t.Context(), NewToken(fakeToken), pullRef(), "aaa")
	require.ErrorIs(t, err, ErrUnexpectedStatus)
}

func TestLastPage(t *testing.T) {
	t.Parallel()

	tests := map[string]int{
		"": 0,
		`<https://api.example.test/x?per_page=100&page=2>; rel="next", <https://api.example.test/x?per_page=100&page=7>; rel="last"`: 7,
		`<https://api.example.test/x?page=2>; rel="next"`:    0,
		`<https://api.example.test/x?page=many>; rel="last"`: 0,
		`<https://api.example.test/x?page=0>; rel="last"`:    0,
		`<://bad url?page=4>; rel="last"`:                    0,
		`<https://api.example.test/x?page=3>;rel="last"`:     3,
	}

	for link, want := range tests {
		header := http.Header{}
		if link != "" {
			header.Set("Link", link)
		}

		assert.Equal(t, want, lastPage(header), link)
	}
}

func TestFetchChangedFilesErrorsNameThePullRequest(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	// The handler runs on the server's goroutine, where t.Fatal must not be called, so it
	// only counts the requests and the test checks the count.
	var requests atomic.Int32

	c := newTestClient(t, func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	})

	for _, ref := range []item.Ref{{Repo: repo.Name{Repo: "x"}, Number: 1}, {Repo: repo.Name{Owner: "x"}, Number: 1}, {Repo: repo.Name{Owner: "x", Repo: "y"}}} {
		_, err := c.FetchChangedFiles(t.Context(), NewToken(fakeToken), ref, "")
		require.ErrorIs(t, err, ErrInvalidItemRef)

		_, err = c.FetchPullHead(t.Context(), NewToken(fakeToken), ref)
		require.ErrorIs(t, err, ErrInvalidItemRef)
	}

	assert.Zero(t, requests.Load(), "no request is sent")
}

func TestFetchPullHead(t *testing.T) {
	t.Parallel()

	fake := &fakePull{t: t, heads: []string{"abc123"}, total: 12, n: 12}
	c := newTestClient(t, fake.ServeHTTP)

	got, err := c.FetchPullHead(t.Context(), NewToken(fakeToken), pullRef())
	require.NoError(t, err)
	assert.Equal(t, &PullHead{Head: "abc123", ChangedFiles: 12, State: "open"}, got)
}

func TestFetchPullHeadRefusesAnswersWithoutAHead(t *testing.T) {
	t.Parallel()

	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"state":"open","head":{}}`))
	})

	_, err := c.FetchPullHead(t.Context(), NewToken(fakeToken), pullRef())
	require.ErrorIs(t, err, ErrMalformedResponse)
}

func TestPullPathEscapesItsParts(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "/repos/a%2Fb/c%3Fd/pulls/3", pullPath(item.Ref{Repo: repo.Name{Owner: "a/b", Repo: "c?d"}, Number: 3}))
}

func TestFetchChangedFilesKeepsOnlyPathsAndStatus(t *testing.T) {
	t.Parallel()

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/acme-org/example-repo/pulls/7" {
			_, _ = io.WriteString(w, `{"state":"open","changed_files":2,"head":{"sha":"ccc"}}`)

			return
		}

		_, _ = io.WriteString(w, `[
			{"filename":"go.sum","status":"modified","sha":"b1","additions":3,"deletions":1,"patch":"@@ -1 +1,3 @@\n-a\n+b\n+c\n+d"},
			{"filename":"logo.png","status":"added","sha":"b2","additions":0,"deletions":0},
			{"filename":"","status":"modified"}
		]`)
	})

	got, err := c.FetchChangedFiles(t.Context(), NewToken(fakeToken), pullRef(), "ccc")
	require.NoError(t, err)

	assert.Equal(t, &ChangedFiles{
		Head: "ccc", Total: 2, Complete: true,
		Files: []ChangedFile{{Path: "go.sum", Status: "modified"}, {Path: "logo.png", Status: "added"}},
	}, got, "the patches GitHub sends with the list are dropped, as before")
}
