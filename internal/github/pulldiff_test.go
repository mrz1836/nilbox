package github

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// diffPull serves one pull request's head and files with their diffs. Each file's
// contents link names the commit listed (at, or the head when at is ""), as GitHub's do;
// heads are returned in turn by the head endpoint, the last one repeating.
type diffPull struct {
	t     *testing.T
	heads []string
	at    []string
	n     int
	total int

	mu        sync.Mutex
	headCalls int
	lists     int
}

func (d *diffPull) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	defer d.mu.Unlock()

	switch r.URL.Path {
	case "/repos/acme-org/example-repo/pulls/7":
		head := d.heads[min(d.headCalls, len(d.heads)-1)]
		d.headCalls++

		_, _ = fmt.Fprintf(w, `{"state":"open","changed_files":%d,"head":{"sha":%q}}`, d.total, head)
	case "/repos/acme-org/example-repo/pulls/7/files":
		at := d.at[min(d.lists, len(d.at)-1)]
		d.lists++

		_, _ = w.Write(d.files(at))
	default:
		http.NotFound(w, r)
	}
}

// files is one page of n files listed at the commit at ("" names none).
func (d *diffPull) files(at string) []byte {
	type file struct {
		Filename    string  `json:"filename"`
		Status      string  `json:"status"`
		SHA         string  `json:"sha"`
		Additions   int     `json:"additions"`
		Deletions   int     `json:"deletions"`
		Patch       *string `json:"patch,omitempty"`
		ContentsURL string  `json:"contents_url,omitempty"`
	}

	patch := "@@ -1 +1 @@\n-a\n+b"
	out := make([]file, 0, d.n+1)

	for i := range d.n {
		f := file{Filename: fmt.Sprintf("dir/f%d.go", i), Status: "modified", SHA: "b" + fmt.Sprint(i), Additions: 1, Deletions: 1, Patch: &patch}

		if i == 1 {
			f = file{Filename: "logo.png", Status: "added", SHA: "b1"}
		}

		if at != "" {
			f.ContentsURL = "https://api.example.test/repos/acme-org/example-repo/contents/" + f.Filename + "?ref=" + at
		}

		out = append(out, f)
	}

	removed := file{Filename: "old.go", Status: "removed", ContentsURL: "https://api.example.test/x?ref=elsewhere"}
	out = append(out, removed)

	b, err := json.Marshal(out)
	assert.NoError(d.t, err)

	return b
}

func TestFetchPullDiffTrustsTheCommitTheFilesName(t *testing.T) {
	t.Parallel()

	fake := &diffPull{t: t, heads: []string{"h1"}, at: []string{"h1"}, n: 2, total: 3}
	client := newTestClient(t, fake.ServeHTTP)

	diff, err := client.FetchPullDiff(t.Context(), NewToken(fakeToken), pullRef(), "h1")
	require.NoError(t, err)

	assert.Equal(t, 0, fake.headCalls, "files that name the known head need no second reading")
	assert.Equal(t, "h1", diff.Head)
	assert.Equal(t, 3, diff.Total)
	assert.True(t, diff.Complete)
	require.Len(t, diff.Files, 3)
	assert.Equal(t, PullFile{
		Path: "dir/f0.go", Status: "modified", BlobSHA: "b0", Patch: "@@ -1 +1 @@\n-a\n+b", HasPatch: true,
		Additions: 1, Deletions: 1,
	}, diff.Files[0])
	assert.Equal(t, PullFile{Path: "logo.png", Status: "added", BlobSHA: "b1"}, diff.Files[1], "a binary file has no patch")
	assert.Equal(t, "old.go", diff.Files[2].Path, "a removed file's link is not read")
}

func TestFetchPullDiffReadsTheHeadWhenTheFilesDontSay(t *testing.T) {
	t.Parallel()

	fake := &diffPull{t: t, heads: []string{"h1"}, at: []string{""}, n: 2, total: 3}
	client := newTestClient(t, fake.ServeHTTP)

	diff, err := client.FetchPullDiff(t.Context(), NewToken(fakeToken), pullRef(), "")
	require.NoError(t, err)

	assert.Equal(t, 2, fake.headCalls, "the head is read before and after the list")
	assert.Equal(t, "h1", diff.Head)
	assert.True(t, diff.Complete)
}

func TestFetchPullDiffReadsAgainWhenTheHeadMoved(t *testing.T) {
	t.Parallel()

	fake := &diffPull{t: t, heads: []string{"h2"}, at: []string{"h2"}, n: 2, total: 3}
	client := newTestClient(t, fake.ServeHTTP)

	diff, err := client.FetchPullDiff(t.Context(), NewToken(fakeToken), pullRef(), "h1")
	require.NoError(t, err)

	assert.Equal(t, "h2", diff.Head)
	assert.Equal(t, 2, fake.lists, "the list read for the old head is read again for the new one")
	assert.Equal(t, 1, fake.headCalls)
}

func TestFetchPullDiffGivesUpOnAHeadThatKeepsMoving(t *testing.T) {
	t.Parallel()

	fake := &diffPull{t: t, heads: []string{"h2", "h3"}, at: []string{"h2", "h3"}, n: 1, total: 2}
	client := newTestClient(t, fake.ServeHTTP)

	_, err := client.FetchPullDiff(t.Context(), NewToken(fakeToken), pullRef(), "h1")
	require.ErrorIs(t, err, ErrHeadMoved)
}

func TestFetchPullDiffFailures(t *testing.T) {
	t.Parallel()

	_, err := newTestClient(t, http.NotFound).FetchPullDiff(t.Context(), NewToken(fakeToken), editRef(), "")
	require.ErrorIs(t, err, ErrUnexpectedStatus, "the first head read fails")

	_, err = newTestClient(t, http.NotFound).FetchPullDiff(t.Context(), NewToken(fakeToken), pullRef(), "h1")
	require.ErrorIs(t, err, ErrUnexpectedStatus, "the list fails")

	bad := pullRef()
	bad.Number = 0

	_, err = newTestClient(t, http.NotFound).FetchPullDiff(t.Context(), NewToken(fakeToken), bad, "h1")
	require.ErrorIs(t, err, ErrInvalidItemRef)

	listOnly := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/acme-org/example-repo/pulls/7/files" {
			_, _ = io.WriteString(w, `[{"filename":"a.go","status":"modified"}]`)

			return
		}

		http.NotFound(w, r)
	})

	_, err = listOnly.FetchPullDiff(t.Context(), NewToken(fakeToken), pullRef(), "h1")
	require.ErrorIs(t, err, ErrUnexpectedStatus, "the head read after the list fails")
}

func TestListedAt(t *testing.T) {
	t.Parallel()

	at, ok := listedAt(nil)
	assert.False(t, ok)
	assert.Empty(t, at)

	at, ok = listedAt([]listedFile{{at: ""}, {at: "H1"}, {at: "h1"}})
	assert.True(t, ok)
	assert.Equal(t, "H1", at)

	_, ok = listedAt([]listedFile{{at: "h1"}, {at: "h2"}})
	assert.False(t, ok, "files from two commits name no one head")

	assert.Empty(t, contentsRef("::not a url"))
	assert.Equal(t, "abc", contentsRef("https://api.example.test/x?ref=abc"))

	_, ok = pullFile(restFile{})
	assert.False(t, ok, "an entry without a name is skipped")
}
