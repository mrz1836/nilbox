package github

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// releaseFile is what the test release download serves.
const releaseFile = "mage-x_1.30.2_darwin_arm64.tar.gz contents"

// downloadServers starts TLS servers and returns a client that trusts all of them and
// downloads only from those listed in allowed (indexes into the servers).
func downloadServers(t *testing.T, handlers []http.HandlerFunc, allowed ...int) (*Client, []*httptest.Server) {
	t.Helper()

	pool := x509.NewCertPool()
	servers := make([]*httptest.Server, 0, len(handlers))

	for _, h := range handlers {
		srv := httptest.NewTLSServer(h)
		t.Cleanup(srv.Close)
		pool.AddCert(srv.Certificate())
		servers = append(servers, srv)
	}

	hosts := make([]string, 0, len(allowed))
	for _, i := range allowed {
		hosts = append(hosts, strings.TrimPrefix(servers[i].URL, "https://"))
	}

	transport := http.DefaultTransport.(*http.Transport).Clone() //nolint:forcetypeassert // the standard transport
	transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	t.Cleanup(transport.CloseIdleConnections)

	client := &Client{
		HTTP: &http.Client{Transport: transport, Timeout: time.Second}, UserAgent: testUserAgent, DownloadHosts: hosts,
	}

	return client, servers
}

// serveRelease answers with the release file, failing the test on an Authorization header.
func serveRelease(t *testing.T) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.Header.Get("Authorization"), "no token goes to a download host")
		assert.Equal(t, "application/octet-stream", r.Header.Get("Accept"))

		_, _ = io.WriteString(w, releaseFile)
	}
}

func TestDownloadAssetStreamsTheFile(t *testing.T) {
	t.Parallel()

	client, servers := downloadServers(t, []http.HandlerFunc{serveRelease(t)}, 0)

	var got bytes.Buffer

	n, err := client.DownloadAsset(t.Context(), servers[0].URL+"/acme-org/tool/releases/download/v1.30.2/file", &got, 1<<20)
	require.NoError(t, err)
	assert.Equal(t, int64(len(releaseFile)), n)
	assert.Equal(t, releaseFile, got.String())
}

func TestDownloadAssetFollowsARedirectOnlyToADownloadHost(t *testing.T) {
	t.Parallel()

	var asset, elsewhere *httptest.Server

	redirectTo := func(target **httptest.Server) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, (*target).URL+"/asset", http.StatusFound)
		}
	}

	client, servers := downloadServers(t, []http.HandlerFunc{
		redirectTo(&asset), serveRelease(t), redirectTo(&elsewhere),
		func(http.ResponseWriter, *http.Request) { t.Error("an off-list host is never asked") },
	}, 0, 1, 2)
	asset, elsewhere = servers[1], servers[3]

	var got bytes.Buffer

	_, err := client.DownloadAsset(t.Context(), servers[0].URL+"/file", &got, 1<<20)
	require.NoError(t, err, "github.com sends the download to its asset host")
	assert.Equal(t, releaseFile, got.String())

	got.Reset()

	_, err = client.DownloadAsset(t.Context(), servers[2].URL+"/file", &got, 1<<20)
	require.ErrorIs(t, err, ErrDownloadURL, "a redirect off the download hosts is refused")
	assert.Empty(t, got.String())
}

func TestDownloadAssetRefusesAURLOffTheDownloadHosts(t *testing.T) {
	t.Parallel()

	client, servers := downloadServers(t, []http.HandlerFunc{
		func(http.ResponseWriter, *http.Request) { t.Error("nothing is asked") },
	}, 0)
	host := strings.TrimPrefix(servers[0].URL, "https://")

	for _, raw := range []string{
		"http://" + host + "/file",
		"https://user:secret@" + host + "/file",
		"https://example.com/file",
		"https://" + host + ".example.com/file",
		"://not a url",
	} {
		_, err := client.DownloadAsset(t.Context(), raw, io.Discard, 1<<20)
		require.ErrorIs(t, err, ErrDownloadURL, raw)
	}
}

func TestDownloadAssetStopsAfterFiveRedirects(t *testing.T) {
	t.Parallel()

	var self *httptest.Server

	client, servers := downloadServers(t, []http.HandlerFunc{
		func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, self.URL+r.URL.Path+"x", http.StatusFound) //nolint:gosec // the test's own server, redirecting to itself
		},
	}, 0)
	self = servers[0]

	_, err := client.DownloadAsset(t.Context(), self.URL+"/x", io.Discard, 1<<20)
	require.ErrorIs(t, err, ErrDownloadURL)
	assert.Contains(t, err.Error(), "more than 5 redirects")
}

func TestDownloadAssetFailsPastItsSize(t *testing.T) {
	t.Parallel()

	client, servers := downloadServers(t, []http.HandlerFunc{serveRelease(t)}, 0)

	var got bytes.Buffer

	n, err := client.DownloadAsset(t.Context(), servers[0].URL+"/file", &got, 10)
	require.ErrorIs(t, err, ErrDownloadTooLarge)
	assert.Equal(t, int64(11), n, "it stops one byte past the size")
}

func TestDownloadAssetStopsWhenItStalls(t *testing.T) {
	t.Parallel()

	client, servers := downloadServers(t, []http.HandlerFunc{
		func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, "a start")
			w.(http.Flusher).Flush() //nolint:forcetypeassert // httptest's writer flushes

			<-r.Context().Done()
		},
	}, 0)
	client.stall = 100 * time.Millisecond
	client.HTTP.Timeout = 0

	_, err := client.DownloadAsset(t.Context(), servers[0].URL+"/file", io.Discard, 1<<20)
	require.ErrorIs(t, err, ErrDownloadStalled)
}

func TestDownloadAssetIgnoresTheClientTimeout(t *testing.T) {
	t.Parallel()

	client, servers := downloadServers(t, []http.HandlerFunc{
		func(w http.ResponseWriter, _ *http.Request) {
			for range 4 {
				_, _ = io.WriteString(w, "chunk ")
				w.(http.Flusher).Flush() //nolint:forcetypeassert // httptest's writer flushes
				time.Sleep(50 * time.Millisecond)
			}
		},
	}, 0)
	client.HTTP.Timeout = 100 * time.Millisecond

	var got bytes.Buffer

	_, err := client.DownloadAsset(t.Context(), servers[0].URL+"/file", &got, 1<<20)
	require.NoError(t, err, "a slow download that keeps arriving outlasts the API's timeout")
	assert.Equal(t, strings.Repeat("chunk ", 4), got.String())
}

func TestDownloadAssetFailsOnAnErrorStatus(t *testing.T) {
	t.Parallel()

	client, servers := downloadServers(t, []http.HandlerFunc{
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) },
	}, 0)

	_, err := client.DownloadAsset(t.Context(), servers[0].URL+"/file", io.Discard, 1<<20)
	require.ErrorIs(t, err, ErrUnexpectedStatus)
}

func TestDownloadAssetStopsWhenCanceled(t *testing.T) {
	t.Parallel()

	client, servers := downloadServers(t, []http.HandlerFunc{
		func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, "a start")
			w.(http.Flusher).Flush() //nolint:forcetypeassert // httptest's writer flushes

			<-r.Context().Done()
		},
	}, 0)
	client.HTTP.Timeout = 0

	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(50*time.Millisecond, cancel)

	_, err := client.DownloadAsset(ctx, servers[0].URL+"/file", io.Discard, 1<<20)
	require.ErrorIs(t, err, context.Canceled)
}
