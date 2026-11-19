package github

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
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

func TestDownloadAssetFollowsAtMostFiveRedirects(t *testing.T) {
	t.Parallel()

	client, servers := downloadServers(t, []http.HandlerFunc{redirectChain(serveRelease(t))}, 0)

	var got bytes.Buffer

	_, err := client.DownloadAsset(t.Context(), servers[0].URL+"/r/5", &got, 1<<20)
	require.NoError(t, err, "five redirects on the download hosts are followed")
	assert.Equal(t, releaseFile, got.String())

	_, err = client.DownloadAsset(t.Context(), servers[0].URL+"/r/6", io.Discard, 1<<20)
	require.ErrorIs(t, err, ErrDownloadURL)
	require.ErrorContains(t, err, "download: ")
	require.ErrorContains(t, err, ": not a GitHub release download: more than 5 redirects")
}

func TestDownloadAssetSaysWhatURLItWants(t *testing.T) {
	t.Parallel()

	client, servers := downloadServers(t, []http.HandlerFunc{
		func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://example.com/file", http.StatusFound)
		},
		func(http.ResponseWriter, *http.Request) { t.Error("nothing is asked") },
	}, 0, 1)
	first, second := strings.TrimPrefix(servers[0].URL, "https://"), strings.TrimPrefix(servers[1].URL, "https://")

	_, err := client.DownloadAsset(t.Context(), "http://"+first+"/file", io.Discard, 1<<20)
	require.EqualError(t, err, "not a GitHub release download: want https on "+first+", "+second)

	_, err = client.DownloadAsset(t.Context(), "://not a url", io.Discard, 1<<20)
	require.ErrorIs(t, err, ErrDownloadURL)
	require.ErrorContains(t, err, "not a GitHub release download: parse ")

	_, err = client.DownloadAsset(t.Context(), servers[0].URL+"/file", io.Discard, 1<<20)
	require.ErrorIs(t, err, ErrDownloadURL)
	require.ErrorContains(t, err, ": not a GitHub release download: want https on "+first+", "+second,
		"a redirect off the download hosts says so")

	_, err = (&Client{}).DownloadAsset(t.Context(), "http://github.com/acme-org/tool/releases/download/v1/file", io.Discard, 1)
	require.EqualError(t, err, "not a GitHub release download: want https on "+strings.Join(DefaultDownloadHosts(), ", "))
}

func TestDownloadAssetFailsPastItsSize(t *testing.T) {
	t.Parallel()

	client, servers := downloadServers(t, []http.HandlerFunc{serveRelease(t)}, 0)

	var got bytes.Buffer

	n, err := client.DownloadAsset(t.Context(), servers[0].URL+"/file", &got, 10)
	require.ErrorIs(t, err, ErrDownloadTooLarge)
	assert.Equal(t, int64(11), n, "it stops one byte past the size")
}

// pipeHost is the download host a pipe client downloads from.
const pipeHost = "downloads.example.test"

// pipeClient returns a client that downloads from pipeHost, which handler serves over
// in-memory connections: in a synctest bubble, the server's waits and the download's all
// read the bubble's clock, so a test of a slow or stalled download waits for nothing. The
// server and the connections close when the test ends.
func pipeClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()

	l := &pipeListener{conns: make(chan net.Conn), closed: make(chan struct{})}
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: time.Minute}
	served := make(chan struct{})

	go func() {
		defer close(served)

		_ = srv.Serve(l)
	}()

	// The connection stands in for one whose TLS handshake is done: the download's rules
	// on the scheme and the host are what is under test, not TLS.
	transport := &http.Transport{DialTLSContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return l.dial(ctx)
	}}

	t.Cleanup(func() {
		transport.CloseIdleConnections()

		_ = srv.Close()

		<-served
	})

	return &Client{
		HTTP: &http.Client{Transport: transport, Timeout: time.Second}, UserAgent: testUserAgent, DownloadHosts: []string{pipeHost},
	}
}

// pipeListener hands its server the far end of each in-memory connection its client dials.
type pipeListener struct {
	conns  chan net.Conn
	closed chan struct{}
	once   sync.Once
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *pipeListener) Close() error {
	l.once.Do(func() { close(l.closed) })

	return nil
}

func (l *pipeListener) Addr() net.Addr { return pipeAddr{} }

// dial makes a connection and hands its far end to the server.
func (l *pipeListener) dial(ctx context.Context) (net.Conn, error) {
	near, far := net.Pipe()

	select {
	case l.conns <- far:
		return near, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

// pipeAddr is the address of an in-memory connection's listener.
type pipeAddr struct{}

func (pipeAddr) Network() string { return "pipe" }
func (pipeAddr) String() string  { return pipeHost }

func TestDownloadAssetStopsWhenItStalls(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		stall time.Duration // the client's own, none when zero
		want  time.Duration
	}{
		{name: "once nothing arrived for 30 seconds", want: defaultDownloadStall},
		{name: "once nothing arrived for the client's own stall", stall: 5 * time.Second, want: 5 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				client := pipeClient(t, func(w http.ResponseWriter, r *http.Request) {
					_, _ = io.WriteString(w, "a start")
					w.(http.Flusher).Flush() //nolint:forcetypeassert // the server's writer flushes

					<-r.Context().Done()
				})
				client.HTTP.Timeout = 0
				client.stall = tt.stall

				begun := time.Now()

				var got bytes.Buffer

				_, err := client.DownloadAsset(t.Context(), "https://"+pipeHost+"/file", &got, 1<<20)
				require.ErrorIs(t, err, ErrDownloadStalled)
				assert.Equal(t, "a start", got.String(), "what arrived before the stall is written")
				assert.Equal(t, tt.want, time.Since(begun), "it gives up after the stall, and no sooner")
			})
		})
	}
}

func TestDownloadAssetIgnoresTheClientTimeout(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		client := pipeClient(t, func(w http.ResponseWriter, r *http.Request) {
			for range 4 {
				_, _ = io.WriteString(w, "chunk ")
				w.(http.Flusher).Flush() //nolint:forcetypeassert // the server's writer flushes

				select {
				case <-time.After(20 * time.Second):
				case <-r.Context().Done():
					return // the download gave up
				}
			}
		})
		client.HTTP.Timeout = 30 * time.Second

		begun := time.Now()

		var got bytes.Buffer

		_, err := client.DownloadAsset(t.Context(), "https://"+pipeHost+"/file", &got, 1<<20)
		require.NoError(t, err, "a slow download that keeps arriving outlasts the API's timeout")
		assert.Equal(t, strings.Repeat("chunk ", 4), got.String())
		assert.Equal(t, 80*time.Second, time.Since(begun), "it took longer than the client's timeout allows")
	})
}

func TestDownloadAssetFailsOnAnErrorStatus(t *testing.T) {
	t.Parallel()

	client, servers := downloadServers(t, []http.HandlerFunc{
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) },
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) },
	}, 0, 1)

	_, err := client.DownloadAsset(t.Context(), servers[0].URL+"/file", io.Discard, 1<<20)
	require.ErrorIs(t, err, ErrUnexpectedStatus)
	require.ErrorIs(t, err, ErrDownloadMissing, "a 404 says the release has no such file")

	_, err = client.DownloadAsset(t.Context(), servers[1].URL+"/file", io.Discard, 1<<20)
	require.ErrorIs(t, err, ErrUnexpectedStatus)
	require.NotErrorIs(t, err, ErrDownloadMissing)
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

// eofOnCancel is a transport whose response bodies end cleanly, as if the file ended, once
// the request is canceled: the order a canceled request's body can read in, which a real
// connection shows only now and then.
type eofOnCancel struct{ next http.RoundTripper }

func (e eofOnCancel) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := e.next.RoundTrip(r)
	if err != nil {
		return nil, err //nolint:wrapcheck // the transport's own error, passed through
	}

	resp.Body = &eofBody{ReadCloser: resp.Body, done: r.Context().Done()}

	return resp, nil
}

// eofBody reads its first chunk, then waits for the request to be canceled and ends.
type eofBody struct {
	io.ReadCloser

	done <-chan struct{}
	read bool
}

func (b *eofBody) Read(p []byte) (int, error) {
	if !b.read {
		b.read = true

		return b.ReadCloser.Read(p) //nolint:wrapcheck // the body's own read
	}

	<-b.done

	return 0, io.EOF
}

func TestDownloadAssetStopsWhenCanceledThoughTheBodyEndsCleanly(t *testing.T) {
	t.Parallel()

	client, servers := downloadServers(t, []http.HandlerFunc{
		func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, "a start")
			w.(http.Flusher).Flush() //nolint:forcetypeassert // httptest's writer flushes

			<-r.Context().Done()
		},
	}, 0)
	client.HTTP.Timeout = 0
	client.HTTP.Transport = eofOnCancel{next: client.HTTP.Transport}

	ctx, cancel := context.WithCancel(t.Context())
	time.AfterFunc(50*time.Millisecond, cancel)

	_, err := client.DownloadAsset(ctx, servers[0].URL+"/file", io.Discard, 1<<20)
	require.ErrorIs(t, err, context.Canceled, "a canceled download is no success, however its body ended")
}
