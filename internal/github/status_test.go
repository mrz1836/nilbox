package github

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// summaryPath is the path the tests read, a Statuspage summary's.
const summaryPath = "/api/v2/summary.json"

// newStatusServer serves handler over TLS and returns a client whose one status host is
// the server, and that host.
func newStatusServer(t *testing.T, handler http.HandlerFunc) (*Client, string) {
	t.Helper()

	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)

	u, err := url.Parse(srv.URL)
	require.NoError(t, err)

	return &Client{HTTP: srv.Client(), UserAgent: testUserAgent, StatusHosts: []string{u.Host}}, u.Host
}

func TestFetchStatusReadsTheSummaryAsSent(t *testing.T) {
	t.Parallel()

	client, host := newStatusServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, summaryPath, r.URL.Path)
		assert.Empty(t, r.Header.Get("Authorization"), "a status page is public; no token is sent")
		assert.Empty(t, r.Header.Get("If-None-Match"), "no ETag, no condition")
		assert.Equal(t, testUserAgent, r.Header.Get("User-Agent"))
		assert.Equal(t, "application/json", r.Header.Get("Accept"))

		w.Header().Set("ETag", `W/"abc"`)
		_, _ = w.Write([]byte(`{"status": {"indicator": "none"}}`))
	})

	got, err := client.FetchStatus(t.Context(), StatusRequest{Host: host, Path: summaryPath})
	require.NoError(t, err)

	assert.Equal(t, StatusAnswer{Body: []byte(`{"status": {"indicator": "none"}}`), ETag: `W/"abc"`}, got)
}

func TestFetchStatusSendsTheETagAndTakesA304(t *testing.T) {
	t.Parallel()

	client, host := newStatusServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `W/"abc"` {
			w.WriteHeader(http.StatusNotModified)

			return
		}

		w.Header().Set("ETag", `W/"def"`)
		w.WriteHeader(http.StatusNotModified)
	})

	got, err := client.FetchStatus(t.Context(), StatusRequest{Host: host, Path: summaryPath, ETag: `W/"abc"`})
	require.NoError(t, err)
	assert.Equal(t, StatusAnswer{ETag: `W/"abc"`, NotModified: true}, got, "a 304 carries the ETag sent on")

	got, err = client.FetchStatus(t.Context(), StatusRequest{Host: host, Path: summaryPath, ETag: `W/"old"`})
	require.NoError(t, err)
	assert.Equal(t, StatusAnswer{ETag: `W/"def"`, NotModified: true}, got, "or the one it names")

	_, err = client.FetchStatus(t.Context(), StatusRequest{Host: host, Path: summaryPath})
	require.ErrorIs(t, err, ErrUnexpectedStatus, "a 304 to a request without an ETag answers nothing")
}

func TestFetchStatusRefusesAHostOffTheList(t *testing.T) {
	t.Parallel()

	var reads atomic.Int32

	client, _ := newStatusServer(t, func(http.ResponseWriter, *http.Request) { reads.Add(1) })

	for _, host := range []string{"status.example.com", "", "user@" + client.StatusHosts[0]} {
		_, err := client.FetchStatus(t.Context(), StatusRequest{Host: host, Path: summaryPath})
		require.ErrorIs(t, err, ErrStatusURL, host)
	}

	assert.Zero(t, reads.Load(), "nothing is sent")
}

func TestFetchStatusRefusesAPathThatIsntAbsoluteAndClean(t *testing.T) {
	t.Parallel()

	var reads atomic.Int32

	client, host := newStatusServer(t, func(http.ResponseWriter, *http.Request) { reads.Add(1) })

	for _, p := range []string{"", "api/v2/summary.json", "/api/../summary.json", "/api//summary.json", "/api/"} {
		_, err := client.FetchStatus(t.Context(), StatusRequest{Host: host, Path: p})
		require.ErrorIs(t, err, ErrStatusURL, p)
	}

	assert.Zero(t, reads.Load(), "nothing is sent")
}

func TestFetchStatusFollowsRedirectsOnTheHostOnly(t *testing.T) {
	t.Parallel()

	client, host := newStatusServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == summaryPath {
			http.Redirect(w, r, "/moved.json", http.StatusFound)

			return
		}

		_, _ = w.Write([]byte(`{}`))
	})

	got, err := client.FetchStatus(t.Context(), StatusRequest{Host: host, Path: summaryPath})
	require.NoError(t, err)
	assert.Equal(t, []byte(`{}`), got.Body)

	var off atomic.Int32

	elsewhere := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		off.Add(1)
	}))
	t.Cleanup(elsewhere.Close)

	elsewhereURL, err := url.Parse(elsewhere.URL)
	require.NoError(t, err)

	away, awayHost := newStatusServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/summary.json", http.StatusFound)
	})
	// Even a host on the list is off limits once the read started on another.
	away.StatusHosts = append(away.StatusHosts, elsewhereURL.Host)

	_, err = away.FetchStatus(t.Context(), StatusRequest{Host: awayHost, Path: summaryPath})
	require.ErrorIs(t, err, ErrStatusURL)
	assert.Zero(t, off.Load(), "a redirect off the host is never followed")
}

func TestFetchStatusRefusesWhatIsntASummary(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		status int
		body   string
	}{
		"an error status":  {http.StatusServiceUnavailable, `{}`},
		"a body too large": {http.StatusOK, `{"x": "` + strings.Repeat("a", maxStatusBytes) + `"}`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			client, host := newStatusServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})

			got, err := client.FetchStatus(t.Context(), StatusRequest{Host: host, Path: summaryPath})
			require.ErrorIs(t, err, ErrUnexpectedStatus)
			assert.Contains(t, err.Error(), "read the status page at "+host)
			assert.Equal(t, StatusAnswer{}, got)
		})
	}
}

func TestStatusHostsDefaultToTheThreePages(t *testing.T) {
	t.Parallel()

	want := []string{"www.githubstatus.com", "status.claude.com", "status.openai.com"}

	assert.Equal(t, want, DefaultStatusHosts())
	assert.Equal(t, want, (&Client{}).statusHosts())
	assert.Equal(t, want, NewClient("1.0.0").StatusHosts)
}
