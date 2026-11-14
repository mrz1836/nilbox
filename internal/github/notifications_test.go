package github

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
)

const (
	// threadsQuery is the listing's fixed query for page 1.
	threadsQuery = "all=true&is_done=false&per_page=50&page=1"

	// weakETag is an ETag as a 200 gives it, and strongETag the same as a 304 echoes it.
	weakETag   = `W/"b5ff00c4a1e7d2"`
	strongETag = `"b5ff00c4a1e7d2"`

	// firstThread is the ID of the fixture's first thread, a pull request.
	firstThread int64 = 20000000001
)

// sentRequest is what the fake GitHub received.
type sentRequest struct {
	method, path, query, ifNoneMatch string
	body                             string
}

// requestLog keeps the requests a fake GitHub received.
type requestLog struct {
	mu  sync.Mutex
	got []sentRequest
}

// add records r, whose body was raw.
func (l *requestLog) add(r *http.Request, raw []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.got = append(l.got, sentRequest{
		method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, ifNoneMatch: r.Header.Get("If-None-Match"),
		body: string(raw),
	})
}

// all returns the requests received, in order.
func (l *requestLog) all() []sentRequest {
	l.mu.Lock()
	defer l.mu.Unlock()

	return append([]sentRequest(nil), l.got...)
}

// answer is how the fake GitHub answers one request.
type answer struct {
	status  int
	body    string
	headers map[string]string
}

// fakeGitHub returns a client whose server records each request and answers it with
// respond, given the server's own URL, with a REST budget reading of 100 used, and a
// recorder for the client's budget readings.
func fakeGitHub(t *testing.T, respond func(base string, r *http.Request) answer) (*Client, *requestLog, *recordRates) {
	t.Helper()

	log := &requestLog{}

	var client *Client

	client = newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		log.add(r, raw)

		a := respond(client.BaseURL, r)

		setRateHeaders(w, ResourceCore, 4900, 100)

		for k, v := range a.headers {
			w.Header().Set(k, v)
		}

		w.WriteHeader(a.status)
		_, _ = io.WriteString(w, a.body)
	})

	rates := &recordRates{}
	client.Rates = rates

	return client, log, rates
}

// serveThreads answers every request with the recorded listing, every API URL in it on
// the server's own, as GitHub's would be on its own API, and headers.
func serveThreads(t *testing.T, headers map[string]string) func(string, *http.Request) answer {
	t.Helper()

	recorded := string(fixture(t, "notifications_response.json"))

	return func(base string, _ *http.Request) answer {
		return answer{status: http.StatusOK, body: strings.ReplaceAll(recorded, DefaultBaseURL, base), headers: headers}
	}
}

// exampleName is acme-org/example-repo.
func exampleName() repo.Name {
	return repo.Name{Owner: filesOwner, Repo: exampleRepo}
}

// exampleRef is a pull request or issue of acme-org/example-repo.
func exampleRef(number int, kind item.Kind) item.Ref {
	return item.Ref{Repo: exampleName(), Number: number, Kind: kind}
}

func TestListThreads200(t *testing.T) {
	t.Parallel()

	client, log, rates := fakeGitHub(t, serveThreads(t, map[string]string{"ETag": weakETag, "X-Poll-Interval": "60"}))

	page, err := client.ListThreads(t.Context(), NewToken(fakeToken), ThreadQuery{})
	require.NoError(t, err)

	assert.Equal(t, []sentRequest{{method: http.MethodGet, path: "/notifications", query: threadsQuery}}, log.all(),
		"page 1, from the fixed parameters, unconditional without an ETag")

	assert.False(t, page.NotModified)
	assert.Equal(t, weakETag, page.ETag)
	assert.Equal(t, time.Minute, page.PollInterval)
	assert.Zero(t, page.NextPage, "a single page has no Link")
	assert.Zero(t, page.Dropped)
	require.Len(t, page.Threads, 7)

	assert.Equal(t, Thread{
		ID: firstThread, Unread: true, Reason: "review_requested",
		UpdatedAt:   time.Date(2026, 11, 12, 18, 4, 5, 0, time.UTC),
		SubjectType: "PullRequest", Title: "Retry the sync worker after a timeout",
		Repo: exampleName(), Ref: exampleRef(42, item.KindPR),
		LatestCommentURL: client.BaseURL + "/repos/acme-org/example-repo/issues/comments/30000000001",
	}, page.Threads[0])

	assert.Equal(t, time.Date(2026, 11, 12, 9, 31, 12, 0, time.UTC), page.Threads[1].LastReadAt)
	assert.False(t, page.Threads[1].Unread)
	assert.Equal(t, int64(27000000007), page.Threads[6].ID, "an ID past int32")

	observed := rates.all()
	require.Len(t, observed, 1)
	assert.Equal(t, 1, observed[0].spent, "a listing GitHub answered in full costs a point")
}

func TestListThreads304SpendsNothing(t *testing.T) {
	t.Parallel()

	client, log, rates := fakeGitHub(t, func(_ string, r *http.Request) answer {
		if r.Header.Get("If-None-Match") != weakETag {
			return answer{status: http.StatusOK, body: "[]"}
		}

		return answer{status: http.StatusNotModified, headers: map[string]string{
			"ETag": strongETag, "X-Poll-Interval": "60", "Last-Modified": "Thu, 12 Nov 2026 18:04:05 GMT",
		}}
	})

	page, err := client.ListThreads(t.Context(), NewToken(fakeToken), ThreadQuery{Page: 1, ETag: weakETag})
	require.NoError(t, err)

	assert.Equal(t, &ThreadPage{Threads: []Thread{}, NotModified: true, ETag: strongETag, PollInterval: time.Minute}, page)
	assert.Equal(t, []sentRequest{{method: http.MethodGet, path: "/notifications", query: threadsQuery, ifNoneMatch: weakETag}},
		log.all(), "the ETag goes back as the 200 gave it")

	observed := rates.all()
	require.Len(t, observed, 1, "the 304's reading is still heard")
	assert.Zero(t, observed[0].spent, "a 304 costs nothing")
	assert.Equal(t, RateReading{Resource: ResourceCore, Limit: 5000, Remaining: 4900, Used: 100, ResetAt: rateResetAt},
		observed[0].reading)
}

func TestListThreads304KeepsTheETagSentWhenItCarriesNone(t *testing.T) {
	t.Parallel()

	client, _, _ := fakeGitHub(t, func(string, *http.Request) answer {
		return answer{status: http.StatusNotModified}
	})

	page, err := client.ListThreads(t.Context(), NewToken(fakeToken), ThreadQuery{ETag: weakETag})
	require.NoError(t, err)
	assert.True(t, page.NotModified)
	assert.Equal(t, weakETag, page.ETag)
	assert.Zero(t, page.PollInterval)
}

func TestAnUnconditional304IsAStatusError(t *testing.T) {
	t.Parallel()

	client, log, rates := fakeGitHub(t, func(string, *http.Request) answer {
		return answer{status: http.StatusNotModified}
	})

	// Page 2 is never sent with the ETag, so its 304 is no answer to one.
	_, err := client.ListThreads(t.Context(), NewToken(fakeToken), ThreadQuery{Page: 2, ETag: weakETag})
	require.ErrorIs(t, err, ErrUnexpectedStatus)
	require.NotErrorIs(t, err, ErrNotModified)

	status, ok := asStatus(err)
	require.True(t, ok)
	assert.Equal(t, http.StatusNotModified, status.StatusCode)
	assert.Empty(t, log.all()[0].ifNoneMatch, "a later page is never conditional")
	assert.Equal(t, 1, rates.all()[0].spent, "a 304 nothing asked for is charged as any answer")
}

func TestListThreadsSendsOnlyAnETag(t *testing.T) {
	t.Parallel()

	client, log, _ := fakeGitHub(t, func(string, *http.Request) answer {
		return answer{status: http.StatusOK, body: "[]"}
	})

	for _, etag := range []string{
		"b5ff00", `W/b5ff00`, `"b5ff00"` + "\r\nX-Injected: 1", `"b5 ff"`, `"b5"ff"`, `"` + strings.Repeat("a", 300) + `"`,
		"\"\x7f\"", `W/`, `"`,
	} {
		_, err := client.ListThreads(t.Context(), NewToken(fakeToken), ThreadQuery{ETag: etag})
		require.NoError(t, err, "%q", etag)
	}

	for _, sent := range log.all() {
		assert.Empty(t, sent.ifNoneMatch, "a value that isn't an ETag is never sent")
	}
}

func TestListThreadsKeepsOnlyPage1sETag(t *testing.T) {
	t.Parallel()

	client, _, _ := fakeGitHub(t, func(string, *http.Request) answer {
		return answer{status: http.StatusOK, body: "[]", headers: map[string]string{"ETag": weakETag}}
	})

	first, err := client.ListThreads(t.Context(), NewToken(fakeToken), ThreadQuery{})
	require.NoError(t, err)
	assert.Equal(t, weakETag, first.ETag)

	second, err := client.ListThreads(t.Context(), NewToken(fakeToken), ThreadQuery{Page: 2})
	require.NoError(t, err)
	assert.Empty(t, second.ETag, "a later page is never sent with an ETag, so none is kept")
}

func TestWithETagIsSentOnlyWithAGet(t *testing.T) {
	t.Parallel()

	client, log, _ := fakeGitHub(t, func(string, *http.Request) answer {
		return answer{status: http.StatusNoContent}
	})

	_, _, err := client.do(t.Context(), NewToken(fakeToken), http.MethodPatch, "/x", nil, withETag(weakETag))
	require.NoError(t, err)
	assert.Empty(t, log.all()[0].ifNoneMatch)
}

func TestListThreads403(t *testing.T) {
	t.Parallel()

	client, _, rates := fakeGitHub(t, func(string, *http.Request) answer {
		return answer{status: http.StatusForbidden, body: `{"message":"Resource not accessible by personal access token"}`}
	})

	page, err := client.ListThreads(t.Context(), NewToken(fakeToken), ThreadQuery{ETag: weakETag})
	require.ErrorIs(t, err, ErrUnexpectedStatus)
	assert.Nil(t, page)

	status, ok := asStatus(err)
	require.True(t, ok)
	assert.Equal(t, http.StatusForbidden, status.StatusCode)
	assert.Contains(t, err.Error(), "Resource not accessible")
	assert.NotContains(t, err.Error(), fakeToken)
	assert.Equal(t, 1, rates.all()[0].spent)
}

func TestListThreads404(t *testing.T) {
	t.Parallel()

	client, _, _ := fakeGitHub(t, func(string, *http.Request) answer {
		return answer{status: http.StatusNotFound, body: `{"message":"Not Found"}`}
	})

	page, err := client.ListThreads(t.Context(), NewToken(fakeToken), ThreadQuery{})
	require.ErrorIs(t, err, ErrUnexpectedStatus)
	assert.Nil(t, page)

	status, ok := asStatus(err)
	require.True(t, ok)
	assert.Equal(t, http.StatusNotFound, status.StatusCode)
}

// asStatus finds the *StatusError in err's chain.
func asStatus(err error) (*StatusError, bool) {
	return errors.AsType[*StatusError](err)
}

func TestListThreadsLinkNextPageRebuilt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		link   func(base string) string
		want   int
		capped bool
	}{
		{
			name: "GitHub's own, without is_done",
			link: func(base string) string {
				return "<" + base + `/notifications?all=true&per_page=50&page=2>; rel="next", <` + base +
					`/notifications?all=true&per_page=50&page=10>; rel="last"`
			},
			want: 2,
		},
		{
			name: "extra parameters",
			link: func(base string) string {
				return "<" + base + `/notifications?participating=true&since=2020-01-01T00:00:00Z&page=3&per_page=1>; rel="next"`
			},
			want: 3,
		},
		{
			name: "another host",
			link: func(string) string { return `<https://api.example.com/notifications?page=2>; rel="next"` },
		},
		{
			name: "another scheme",
			link: func(base string) string {
				return "<" + strings.Replace(base, "http://", "https://", 1) + `/notifications?page=2>; rel="next"`
			},
		},
		{
			name: "a user in the URL",
			link: func(base string) string {
				return "<" + strings.Replace(base, "http://", "http://someone@", 1) + `/notifications?page=2>; rel="next"`
			},
		},
		{
			name: "another path",
			link: func(base string) string {
				return "<" + base + `/repos/acme-org/example-repo/notifications?page=2>; rel="next"`
			},
		},
		{
			name: "a thread's path",
			link: func(base string) string { return "<" + base + `/notifications/threads/2?page=2>; rel="next"` },
		},
		{
			name: "a relative link",
			link: func(string) string { return `</notifications?page=2>; rel="next"` },
		},
		{
			name: "the same page",
			link: func(base string) string { return "<" + base + `/notifications?page=1>; rel="next"` },
		},
		{
			name:   "past the last page read",
			link:   func(base string) string { return "<" + base + `/notifications?page=21>; rel="next"` },
			capped: true,
		},
		{
			name: "two pages",
			link: func(base string) string { return "<" + base + `/notifications?page=2&page=3>; rel="next"` },
		},
		{
			name: "no number",
			link: func(base string) string { return "<" + base + `/notifications?page=two>; rel="next"` },
		},
		{
			name: "only the last",
			link: func(base string) string { return "<" + base + `/notifications?page=10>; rel="last"` },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, log, _ := fakeGitHub(t, func(base string, _ *http.Request) answer {
				return answer{status: http.StatusOK, body: "[]", headers: map[string]string{"Link": tt.link(base)}}
			})

			page, err := client.ListThreads(t.Context(), NewToken(fakeToken), ThreadQuery{})
			require.NoError(t, err)
			assert.Equal(t, tt.want, page.NextPage)
			assert.Equal(t, tt.capped, page.Capped, "only a page past the last the app reads leaves threads out")

			if tt.want == 0 {
				return
			}

			_, err = client.ListThreads(t.Context(), NewToken(fakeToken), ThreadQuery{Page: page.NextPage, ETag: weakETag})
			require.NoError(t, err)

			sent := log.all()
			require.Len(t, sent, 2)
			assert.Equal(t, sentRequest{
				method: http.MethodGet, path: "/notifications",
				query: "all=true&is_done=false&per_page=50&page=" + strconv.Itoa(tt.want),
			}, sent[1], "the next page is asked for with the fixed parameters alone, and no ETag")
		})
	}
}

func TestListThreadsStopsAt20Pages(t *testing.T) {
	t.Parallel()

	// GitHub's Link always names one more page, out of 99.
	client, log, _ := fakeGitHub(t, func(base string, r *http.Request) answer {
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if err != nil {
			return answer{status: http.StatusBadRequest} // the listing then fails
		}

		link := "<" + base + "/notifications?all=true&per_page=50&page=" + strconv.Itoa(page+1) + `>; rel="next", <` +
			base + `/notifications?all=true&per_page=50&page=99>; rel="last"`

		return answer{status: http.StatusOK, body: "[]", headers: map[string]string{"Link": link}}
	})

	var asked []int

	for page := 1; page != 0; {
		asked = append(asked, page)
		require.LessOrEqual(t, len(asked), MaxThreadPages, "the pages never end")

		got, err := client.ListThreads(t.Context(), NewToken(fakeToken), ThreadQuery{Page: page})
		require.NoError(t, err)
		assert.Equal(t, page == MaxThreadPages, got.Capped, "page %d: only the last read leaves threads out", page)

		page = got.NextPage
	}

	assert.Len(t, asked, MaxThreadPages, "every page through the 20th, then no more")
	assert.Equal(t, MaxThreadPages, asked[len(asked)-1])
	assert.Len(t, log.all(), MaxThreadPages)

	_, err := client.ListThreads(t.Context(), NewToken(fakeToken), ThreadQuery{Page: MaxThreadPages + 1})
	require.ErrorIs(t, err, ErrInvalidThreadPage)
	assert.Len(t, log.all(), MaxThreadPages, "a page past the last is never asked for")
}

func TestListThreadsCappedOnlyWhenGitHubListsMore(t *testing.T) {
	t.Parallel()

	// A full page of check suites' threads, which name no item.
	entries := make([]string, 0, ThreadsPerPage)
	for i := range ThreadsPerPage {
		entries = append(entries, `{"id": "`+strconv.Itoa(20000000001+i)+`", "subject": {"type": "CheckSuite"}}`)
	}

	full := "[" + strings.Join(entries, ",") + "]"

	// The 20th page, full, and GitHub's last: no Link names a page after it.
	client, _, _ := fakeGitHub(t, func(string, *http.Request) answer {
		return answer{status: http.StatusOK, body: full}
	})

	page, err := client.ListThreads(t.Context(), NewToken(fakeToken), ThreadQuery{Page: MaxThreadPages})
	require.NoError(t, err)
	require.Len(t, page.Threads, ThreadsPerPage)
	assert.Zero(t, page.NextPage)
	assert.False(t, page.Capped, "nothing past it, however full")

	// The same page, with GitHub naming the next.
	client, _, _ = fakeGitHub(t, func(base string, _ *http.Request) answer {
		return answer{status: http.StatusOK, body: full, headers: map[string]string{
			"Link": "<" + base + `/notifications?all=true&per_page=50&page=21>; rel="next"`,
		}}
	})

	page, err = client.ListThreads(t.Context(), NewToken(fakeToken), ThreadQuery{Page: MaxThreadPages})
	require.NoError(t, err)
	assert.Zero(t, page.NextPage, "never read")
	assert.True(t, page.Capped, "but threads are left out")
	assert.Equal(t, 50, ThreadsPerPage)
}

func TestNotificationsRefused(t *testing.T) {
	t.Parallel()

	status := func(code int, message string, retry time.Duration) error {
		return fmt.Errorf("list the notifications: %w", &StatusError{StatusCode: code, Message: message, RetryAfter: retry})
	}

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "not found", err: status(http.StatusNotFound, "Not Found", 0), want: true},
		{
			name: "forbidden to a fine-grained token",
			err:  status(http.StatusForbidden, "Resource not accessible by personal access token", 0), want: true,
		},
		{name: "forbidden, its token redacted", err: Redact(status(http.StatusForbidden, "", 0), NewToken(fakeToken)), want: true},
		{name: "a rate limit", err: status(http.StatusForbidden, "API rate limit exceeded for user ID 1.", 0)},
		{name: "a secondary rate limit", err: status(http.StatusForbidden, "You have exceeded a secondary rate limit.", 0)},
		{name: "forbidden, wait and try again", err: status(http.StatusForbidden, "", time.Minute)},
		{name: "too many requests", err: status(http.StatusTooManyRequests, "", 0)},
		{name: "bad credentials", err: status(http.StatusUnauthorized, "Bad credentials", 0)},
		{name: "GitHub failing", err: status(http.StatusBadGateway, "", 0)},
		{name: "an answer that can't be read", err: fmt.Errorf("decode the notifications: %w", ErrMalformedResponse)},
		{name: "none", err: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, NotificationsRefused(tt.err))
		})
	}
}

func TestRateLimited(t *testing.T) {
	t.Parallel()

	status := func(code int, message string, retry time.Duration) error {
		return fmt.Errorf("read the latest comment: %w", &StatusError{StatusCode: code, Message: message, RetryAfter: retry})
	}

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "a rate limit", err: status(http.StatusForbidden, "API rate limit exceeded for user ID 1.", 0), want: true},
		{name: "a secondary rate limit", err: status(http.StatusForbidden, "You have exceeded a secondary rate limit.", 0), want: true},
		{name: "too many requests", err: status(http.StatusTooManyRequests, "", 0), want: true},
		{name: "wait and try again", err: status(http.StatusForbidden, "", time.Minute), want: true},
		{name: "its token redacted", err: Redact(status(http.StatusTooManyRequests, "", 0), NewToken(fakeToken)), want: true},
		{name: "forbidden", err: status(http.StatusForbidden, "Resource not accessible by personal access token", 0)},
		{name: "not found", err: status(http.StatusNotFound, "Not Found", 0)},
		{name: "GitHub failing", err: status(http.StatusBadGateway, "", 0)},
		{name: "no answer", err: ErrMalformedResponse},
		{name: "none", err: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, RateLimited(tt.err))
		})
	}
}

func TestListThreadsDropsBadIDs(t *testing.T) {
	t.Parallel()

	body := `[
		{"id": "20000000001", "subject": {"type": "Issue"}},
		{"id": "abc"}, {"id": "-5"}, {"id": "0"}, {"id": ""}, {"id": 20000000002}, {"id": "+5"}, {"id": " 5"},
		{"id": "1e5"}, {"id": "99999999999999999999"}, {}, null, "a thread", 7,
		{"id": "20000000003", "unread": "yes"},
		{"id": "27000000000", "subject": {"type": "Release"}}
	]`

	client, _, _ := fakeGitHub(t, func(string, *http.Request) answer {
		return answer{status: http.StatusOK, body: body}
	})

	page, err := client.ListThreads(t.Context(), NewToken(fakeToken), ThreadQuery{})
	require.NoError(t, err)

	ids := make([]int64, 0, len(page.Threads))
	for _, thread := range page.Threads {
		ids = append(ids, thread.ID)
	}

	assert.Equal(t, []int64{20000000001, 27000000000}, ids)
	assert.Equal(t, 14, page.Dropped, "each thread left out is counted")
}

func TestListThreadsFailsOnABodyThatIsNoList(t *testing.T) {
	t.Parallel()

	for _, body := range []string{`{"message":"x"}`, `null`, ``, `[`} {
		client, _, _ := fakeGitHub(t, func(string, *http.Request) answer {
			return answer{status: http.StatusOK, body: body}
		})

		_, err := client.ListThreads(t.Context(), NewToken(fakeToken), ThreadQuery{})
		require.ErrorIs(t, err, ErrMalformedResponse, "%q", body)
	}
}

func TestListThreadsSubjects(t *testing.T) {
	t.Parallel()

	client, _, _ := fakeGitHub(t, serveThreads(t, nil))

	page, err := client.ListThreads(t.Context(), NewToken(fakeToken), ThreadQuery{})
	require.NoError(t, err)

	type subject struct {
		kind string
		ref  item.Ref
		repo string
	}

	example := exampleName().String()

	got := make([]subject, 0, len(page.Threads))
	for _, thread := range page.Threads {
		got = append(got, subject{kind: thread.SubjectType, ref: thread.Ref, repo: thread.Repo.String()})
	}

	assert.Equal(t, []subject{
		{kind: "PullRequest", ref: exampleRef(42, item.KindPR), repo: example},
		{kind: "Issue", ref: exampleRef(7, item.KindIssue), repo: example},
		{kind: "Release", repo: "acme-org/other-repo"},
		{kind: "CheckSuite", repo: example},
		{kind: "PullRequest", repo: example}, // another host
		{kind: "PullRequest", repo: example}, // a path past the pull request
		{kind: "Discussion", repo: example},
	}, got)

	assert.Empty(t, page.Threads[3].LatestCommentURL, "a null latest comment")

	// The issue's latest comment is the issue itself, which the caller tells without asking.
	latest, ok := client.ParseSubject(page.Threads[1].LatestCommentURL)
	assert.True(t, ok)
	assert.Equal(t, page.Threads[1].Ref, latest)
}

func TestParseSubject(t *testing.T) {
	t.Parallel()

	client := &Client{BaseURL: DefaultBaseURL}
	api, apiHost := DefaultBaseURL, strings.TrimPrefix(DefaultBaseURL, "https://")

	tests := []struct {
		raw  string
		want item.Ref
	}{
		{raw: api + "/repos/acme-org/example-repo/pulls/42", want: exampleRef(42, item.KindPR)},
		{raw: api + "/repos/acme-org/example-repo/issues/7", want: exampleRef(7, item.KindIssue)},
		{raw: "HTTPS://" + apiHost + "/repos/acme-org/example-repo/issues/7", want: exampleRef(7, item.KindIssue)},
		{raw: ""},
		{raw: api + "/repos/acme-org/example-repo/releases/123456789"},
		{raw: api + "/repos/acme-org/example-repo/pulls/42/files"},
		{raw: api + "/repos/acme-org/example-repo/pulls/42/"},
		{raw: api + "/repos/acme-org/example-repo/pulls/0"},
		{raw: api + "/repos/acme-org/example-repo/pulls/-4"},
		{raw: api + "/repos/acme-org/example-repo/pulls/042"},
		{raw: api + "/repos/acme-org/example-repo/pulls/+42"},
		{raw: api + "/repos/acme-org/example-repo/pulls/99999999999999999999"},
		{raw: api + "/repos/-acme/example-repo/pulls/42"},
		{raw: api + "/repos/acme.org/example-repo/pulls/42"},
		{raw: api + "/repos/acme-org/../pulls/42"},
		{raw: api + "/repos/acme-org%2Fexample-repo/pulls/42"},
		{raw: api + "/repos/%61cme-org/example-repo/pulls/42"},
		{raw: api + "/repos//acme-org/example-repo/pulls/42"},
		{raw: api + "/repos/acme-org/example-repo/pulls/42?page=2"},
		{raw: api + "/repos/acme-org/example-repo/pulls/42?"},
		{raw: api + "/repos/acme-org/example-repo/pulls/42#top"},
		{raw: "https://someone@" + apiHost + "/repos/acme-org/example-repo/pulls/42"},
		{raw: "https://" + apiHost + ":443/repos/acme-org/example-repo/pulls/42"},
		{raw: "https://" + strings.ToUpper(apiHost) + "/repos/acme-org/example-repo/pulls/42"},
		{raw: "http://" + apiHost + "/repos/acme-org/example-repo/pulls/42"},
		{raw: api + ".example.com/repos/acme-org/example-repo/pulls/42"},
		{raw: "https://api.example.com/repos/acme-org/example-repo/pulls/42"},
		{raw: "//" + apiHost + "/repos/acme-org/example-repo/pulls/42"},
		{raw: "/repos/acme-org/example-repo/pulls/42"},
		{raw: "https://github.com/acme-org/example-repo/pull/42"},
		{raw: api + "/acme-org/example-repo/pulls/42"},
		{raw: api + "/repos/acme-org/example-repo/commits/1111111"},
	}

	for _, tt := range tests {
		got, ok := client.ParseSubject(tt.raw)
		assert.Equal(t, tt.want, got, tt.raw)
		assert.Equal(t, tt.want != item.Ref{}, ok, tt.raw)
	}
}

func TestParseSubjectUnderABasePath(t *testing.T) {
	t.Parallel()

	client := &Client{BaseURL: "https://code.example.com/api/v3/"}

	got, ok := client.ParseSubject("https://code.example.com/api/v3/repos/acme-org/example-repo/issues/7")
	assert.True(t, ok)
	assert.Equal(t, exampleRef(7, item.KindIssue), got)

	_, ok = client.ParseSubject("https://code.example.com/repos/acme-org/example-repo/issues/7")
	assert.False(t, ok, "the base's own path comes first")

	_, ok = (&Client{BaseURL: "::not a url"}).ParseSubject(DefaultBaseURL + "/repos/acme-org/example-repo/issues/7")
	assert.False(t, ok, "a base that can't be read accepts nothing")
}

// fakeChoreGate is a ChoreGate that allows chores until refuse is set, and records them.
type fakeChoreGate struct {
	mu      sync.Mutex
	refuse  error
	allowed []string
	held    []time.Duration
}

func (g *fakeChoreGate) AllowChore(resource string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.refuse != nil {
		return g.refuse
	}

	g.allowed = append(g.allowed, resource)

	return nil
}

func (g *fakeChoreGate) HoldWrites(wait time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.held = append(g.held, wait)
}

// record returns what the gate allowed and held.
func (g *fakeChoreGate) record() ([]string, []time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()

	return append([]string(nil), g.allowed...), append([]time.Duration(nil), g.held...)
}

// choreServer returns a client with a chore gate whose server answers every request with
// status, body, and headers.
func choreServer(t *testing.T, status int, body string, headers map[string]string) (*Client, *requestLog, *fakeChoreGate) {
	t.Helper()

	client, log, _ := fakeGitHub(t, func(string, *http.Request) answer {
		return answer{status: status, body: body, headers: headers}
	})

	gate := &fakeChoreGate{}
	client.Chores = gate

	return client, log, gate
}

func TestMarkThreadDone204(t *testing.T) {
	t.Parallel()

	client, log, gate := choreServer(t, http.StatusNoContent, "", nil)

	require.NoError(t, client.MarkThreadDone(t.Context(), NewToken(fakeToken), firstThread))

	assert.Equal(t, []sentRequest{{method: http.MethodDelete, path: "/notifications/threads/20000000001"}}, log.all())

	allowed, held := gate.record()
	assert.Equal(t, []string{ResourceCore}, allowed, "the chore gate counted it")
	assert.Empty(t, held)

	spent := client.Rates.(*recordRates).all() //nolint:forcetypeassert // the fake GitHub's recorder
	require.Len(t, spent, 1)
	assert.Equal(t, 1, spent[0].spent, "a chore spends one REST point")
}

func TestMarkThreadRead205(t *testing.T) {
	t.Parallel()

	client, log, gate := choreServer(t, http.StatusResetContent, "", nil)

	require.NoError(t, client.MarkThreadRead(t.Context(), NewToken(fakeToken), 27000000007))

	assert.Equal(t, []sentRequest{{method: http.MethodPatch, path: "/notifications/threads/27000000007"}}, log.all())

	allowed, _ := gate.record()
	assert.Equal(t, []string{ResourceCore}, allowed)
}

func TestMarkThreadDone404IsNotFound(t *testing.T) {
	t.Parallel()

	client, _, _ := choreServer(t, http.StatusNotFound, `{"message":"Not Found"}`, nil)

	err := client.MarkThreadDone(t.Context(), NewToken(fakeToken), firstThread)
	require.ErrorIs(t, err, ErrThreadNotFound)
	require.ErrorIs(t, err, ErrUnexpectedStatus, "GitHub's answer stays in the chain")
	assert.NotContains(t, err.Error(), fakeToken)

	require.ErrorIs(t, client.MarkThreadRead(t.Context(), NewToken(fakeToken), firstThread), ErrThreadNotFound)
}

func TestMarkThreadDoneKeepsOtherRefusals(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusUnprocessableEntity, http.StatusInternalServerError, http.StatusGone} {
		client, _, _ := choreServer(t, status, `{"message":"no"}`, nil)

		err := client.MarkThreadDone(t.Context(), NewToken(fakeToken), firstThread)
		require.ErrorIs(t, err, ErrUnexpectedStatus, status)
		require.NotErrorIs(t, err, ErrThreadNotFound, status)
	}
}

func TestChoreHoldsOn429RetryAfter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		status  int
		body    string
		headers map[string]string
		want    []time.Duration
	}{
		{
			name: "too many requests, with Retry-After", status: http.StatusTooManyRequests,
			headers: map[string]string{"Retry-After": "30"}, want: []time.Duration{30 * time.Second},
		},
		{
			name: "a secondary limit with Retry-After", status: http.StatusForbidden,
			body:    `{"message":"You have exceeded a secondary rate limit."}`,
			headers: map[string]string{"Retry-After": "120"}, want: []time.Duration{2 * time.Minute},
		},
		{
			name: "a secondary limit without Retry-After", status: http.StatusForbidden,
			body: `{"message":"You have exceeded a secondary rate limit."}`, want: []time.Duration{secondaryLimitWait},
		},
		{
			name: "too many requests, without Retry-After", status: http.StatusTooManyRequests,
			want: []time.Duration{secondaryLimitWait},
		},
		{name: "another refusal", status: http.StatusForbidden, body: `{"message":"Forbidden"}`},
		{name: "a failure", status: http.StatusBadGateway},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, _, gate := choreServer(t, tt.status, tt.body, tt.headers)

			err := client.MarkThreadDone(t.Context(), NewToken(fakeToken), firstThread)
			require.ErrorIs(t, err, ErrUnexpectedStatus)

			_, held := gate.record()
			if tt.want == nil {
				assert.Empty(t, held)
			} else {
				assert.Equal(t, tt.want, held)
			}
		})
	}
}

func TestChoreRefusesOtherPathsAndMethods(t *testing.T) {
	t.Parallel()

	client, log, gate := choreServer(t, http.StatusNoContent, "", nil)
	tok := NewToken(fakeToken)

	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, "", "delete", "PATCH "} {
		require.ErrorIs(t, client.chore(t.Context(), tok, method, firstThread), ErrChoreRefused, "%q", method)
	}

	for _, id := range []int64{0, -1, -firstThread} {
		require.ErrorIs(t, client.chore(t.Context(), tok, http.MethodDelete, id), ErrChoreRefused, id)
		require.ErrorIs(t, client.MarkThreadDone(t.Context(), tok, id), ErrChoreRefused, id)
		require.ErrorIs(t, client.MarkThreadRead(t.Context(), tok, id), ErrChoreRefused, id)
	}

	assert.Empty(t, log.all(), "nothing is sent")

	allowed, _ := gate.record()
	assert.Empty(t, allowed, "nor counted")

	assert.Equal(t, "/notifications/threads/20000000001", threadPath(firstThread), "a thread's path is its ID alone")
}

func TestChoreNilGateRefuses(t *testing.T) {
	t.Parallel()

	client, log, _ := choreServer(t, http.StatusNoContent, "", nil)
	client.Chores = nil
	client.Writes = &fakeGate{} // the edits' gate never lets a chore through

	require.ErrorIs(t, client.MarkThreadDone(t.Context(), NewToken(fakeToken), firstThread), ErrChoreRefused)
	require.ErrorIs(t, client.MarkThreadRead(t.Context(), NewToken(fakeToken), firstThread), ErrChoreRefused)
	assert.Empty(t, log.all())
}

func TestChoreWaitsForTheGate(t *testing.T) {
	t.Parallel()

	client, log, gate := choreServer(t, http.StatusNoContent, "", nil)
	gate.refuse = errGateClosed

	require.ErrorIs(t, client.MarkThreadDone(t.Context(), NewToken(fakeToken), firstThread), errGateClosed)
	assert.Empty(t, log.all(), "a refused chore is never sent")
}

func TestASlowDownHoldsBothLanes(t *testing.T) {
	t.Parallel()

	tok := NewToken(fakeToken)
	slowDown := map[string]string{"Retry-After": "30"}

	tests := []struct {
		name string
		send func(t *testing.T, edits *fakeGate, chores *fakeChoreGate)
		want time.Duration
	}{
		{
			name: "a chore",
			send: func(t *testing.T, edits *fakeGate, chores *fakeChoreGate) {
				t.Helper()

				client, _, _ := choreServer(t, http.StatusTooManyRequests, "", slowDown)
				client.Writes, client.Chores = edits, chores

				require.ErrorIs(t, client.MarkThreadDone(t.Context(), tok, firstThread), ErrUnexpectedStatus)
			},
			want: 30 * time.Second,
		},
		{
			name: "a REST edit",
			send: func(t *testing.T, edits *fakeGate, chores *fakeChoreGate) {
				t.Helper()

				client, _ := editServer(t, http.StatusTooManyRequests, "", slowDown)
				client.Writes, client.Chores = edits, chores

				_, err := client.RemoveAssignee(t.Context(), tok, editRef(), octoUser)
				require.ErrorIs(t, err, ErrUnexpectedStatus)
			},
			want: 30 * time.Second,
		},
		{
			name: "a GraphQL mutation",
			send: func(t *testing.T, edits *fakeGate, chores *fakeChoreGate) {
				t.Helper()

				client, _ := graphQLServer(t, http.StatusOK,
					`{"data":null,"errors":[{"type":"RATE_LIMITED","message":"API rate limit exceeded"}]}`, nil)
				client.Writes, client.Chores = edits, chores

				_, err := mutate[touchData](t.Context(), client, tok, "touch", graphQLRequest{Query: testMutation})
				require.ErrorIs(t, err, ErrGraphQL)
			},
			want: secondaryLimitWait,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			edits, chores := &fakeGate{}, &fakeChoreGate{}
			tt.send(t, edits, chores)

			_, choresHeld := chores.record()
			assert.Equal(t, []time.Duration{tt.want}, choresHeld, "the chore lane is held")

			edits.mu.Lock()
			defer edits.mu.Unlock()

			assert.Equal(t, []time.Duration{tt.want}, edits.held, "the edit lane is held")
		})
	}
}

func TestChoreWithoutATokenIsNotCounted(t *testing.T) {
	t.Parallel()

	client, log, gate := choreServer(t, http.StatusNoContent, "", nil)

	require.Error(t, client.MarkThreadDone(t.Context(), Token{}, firstThread))

	allowed, _ := gate.record()
	assert.Empty(t, allowed)
	assert.Empty(t, log.all())
}

func TestFetchLatestCommentChecksURL(t *testing.T) {
	t.Parallel()

	client, log, _ := fakeGitHub(t, func(string, *http.Request) answer {
		return answer{status: http.StatusOK, body: `{"user":{"login":"octo-user","type":"User"}}`}
	})
	base := client.BaseURL
	host := strings.TrimPrefix(base, "http://")

	for _, raw := range []string{
		"",
		"https://api.example.com/repos/acme-org/example-repo/issues/comments/30000000001",
		"https://" + host + "/repos/acme-org/example-repo/issues/comments/30000000001",
		"http://someone@" + host + "/repos/acme-org/example-repo/issues/comments/30000000001",
		"//" + host + "/repos/acme-org/example-repo/issues/comments/30000000001",
		"/repos/acme-org/example-repo/issues/comments/30000000001",
		base + "/repos/acme-org/example-repo/issues/7",
		base + "/repos/acme-org/example-repo/pulls/42",
		base + "/repos/acme-org/other-repo/releases/123456789",
		base + "/repos/acme-org/example-repo/commits/comments/30000000001",
		base + "/repos/acme-org/example-repo/issues/comments/30000000001/reactions",
		base + "/repos/acme-org/example-repo/issues/comments/abc",
		base + "/repos/acme-org/example-repo/issues/comments/0",
		base + "/repos/acme-org/example-repo/issues/comments/030000000001",
		base + "/repos/acme-org/example-repo/issues/comments/30000000001?per_page=100",
		base + "/repos/acme-org/example-repo/issues/comments/30000000001#x",
		base + "/repos/-acme/example-repo/issues/comments/30000000001",
		base + "/repos/acme-org/../issues/comments/30000000001",
		base + "/repos/acme-org%2F..%2F../example-repo/issues/comments/30000000001",
		base + "/notifications/threads/20000000001",
	} {
		_, err := client.FetchLatestComment(t.Context(), NewToken(fakeToken), raw)
		require.ErrorIs(t, err, ErrInvalidCommentURL, raw)
	}

	assert.Empty(t, log.all(), "nothing refused is ever sent")

	for _, kind := range []string{"issues", "pulls"} {
		author, err := client.FetchLatestComment(t.Context(), NewToken(fakeToken),
			base+"/repos/acme-org/example-repo/"+kind+"/comments/30000000002")
		require.NoError(t, err)
		assert.Equal(t, &CommentAuthor{Login: octoUser, Type: "User"}, author)
	}

	assert.Equal(t, []sentRequest{
		{method: http.MethodGet, path: "/repos/acme-org/example-repo/issues/comments/30000000002"},
		{method: http.MethodGet, path: "/repos/acme-org/example-repo/pulls/comments/30000000002"},
	}, log.all(), "only the path built again from what was checked is sent")
}

func TestFetchLatestCommentBot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want CommentAuthor
	}{
		{
			name: "a bot",
			body: `{"id":30000000001,"user":{"login":"dependabot[bot]","id":49699333,"type":"Bot"},"body":"Rebasing."}`,
			want: CommentAuthor{Login: "dependabot[bot]", Type: "Bot"},
		},
		{
			name: "a person",
			body: `{"id":30000000001,"user":{"login":"octo-reviewer","type":"User"},"body":"Looks good."}`,
			want: CommentAuthor{Login: "octo-reviewer", Type: "User"},
		},
		{name: "a deleted account", body: `{"id":30000000001,"user":null,"body":"Gone."}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, _, rates := fakeGitHub(t, func(string, *http.Request) answer {
				return answer{status: http.StatusOK, body: tt.body}
			})

			author, err := client.FetchLatestComment(t.Context(), NewToken(fakeToken),
				client.BaseURL+"/repos/acme-org/example-repo/issues/comments/30000000001")
			require.NoError(t, err)
			assert.Equal(t, &tt.want, author)
			assert.Equal(t, 1, rates.all()[0].spent)
		})
	}
}

func TestFetchLatestCommentFails(t *testing.T) {
	t.Parallel()

	client, _, _ := fakeGitHub(t, func(string, *http.Request) answer {
		return answer{status: http.StatusOK, body: `[`}
	})

	_, err := client.FetchLatestComment(t.Context(), NewToken(fakeToken),
		client.BaseURL+"/repos/acme-org/example-repo/issues/comments/30000000001")
	require.ErrorIs(t, err, ErrMalformedResponse)

	gone, _, _ := fakeGitHub(t, func(string, *http.Request) answer {
		return answer{status: http.StatusNotFound, body: `{"message":"Not Found"}`}
	})

	_, err = gone.FetchLatestComment(t.Context(), NewToken(fakeToken),
		gone.BaseURL+"/repos/acme-org/example-repo/issues/comments/30000000001")
	require.ErrorIs(t, err, ErrUnexpectedStatus)
}

func TestPollInterval(t *testing.T) {
	t.Parallel()

	for value, want := range map[string]time.Duration{
		"60": time.Minute, " 120 ": 2 * time.Minute, "": 0, "0": 0, "-60": 0, "soon": 0, "86400": 24 * time.Hour,
		"86401": 0, "99999999999999999999": 0,
	} {
		h := http.Header{}
		h.Set("X-Poll-Interval", value)
		assert.Equal(t, want, pollInterval(h), value)
	}
}
