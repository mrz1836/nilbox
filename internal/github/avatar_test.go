package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pngHeader is the PNG signature, enough for content sniffing to call the bytes a PNG.
const pngHeader = "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"

// newAvatarServer serves handler over TLS and returns a client whose avatar host is the
// server, and the server's base URL.
func newAvatarServer(t *testing.T, handler http.HandlerFunc) (*Client, string) {
	t.Helper()

	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)

	u, err := url.Parse(srv.URL)
	require.NoError(t, err)

	return &Client{HTTP: srv.Client(), UserAgent: testUserAgent, AvatarHost: u.Host}, srv.URL
}

func TestFetchAvatarDownloadsTheImage(t *testing.T) {
	t.Parallel()

	client, base := newAvatarServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/u/101", r.URL.Path)
		assert.Equal(t, "v=4&s=128", r.URL.RawQuery)
		assert.Empty(t, r.Header.Get("Authorization"), "avatars are public; no token is sent")
		assert.Equal(t, testUserAgent, r.Header.Get("User-Agent"))
		assert.Equal(t, "image/*", r.Header.Get("Accept"))

		// A wrong header is ignored: the type is sniffed from the bytes.
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte(pngHeader))
	})

	avatar, err := client.FetchAvatar(t.Context(), base+"/u/101?v=4&s=128")
	require.NoError(t, err)

	assert.Equal(t, "image/png", avatar.ContentType)
	assert.Equal(t, []byte(pngHeader), avatar.Data)
	assert.Equal(t, "data:image/png;base64,iVBORw0KGgoAAAANSUhEUg==", avatar.DataURL())
}

func TestFetchAvatarFollowsRedirectsOnTheHost(t *testing.T) {
	t.Parallel()

	client, base := newAvatarServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			http.Redirect(w, r, "/new", http.StatusFound)
			return
		}

		_, _ = w.Write([]byte(pngHeader))
	})

	avatar, err := client.FetchAvatar(t.Context(), base+"/old")
	require.NoError(t, err)
	assert.Equal(t, "image/png", avatar.ContentType)
}

func TestFetchAvatarRefusesURLsOffTheHost(t *testing.T) {
	t.Parallel()

	var requests atomic.Int32

	client, base := newAvatarServer(t, func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)

		_, _ = w.Write([]byte(pngHeader))
	})

	tests := []struct {
		name string
		url  string
	}{
		{name: "empty", url: ""},
		{name: "plain http", url: "http" + base[len("https"):] + "/u/101"},
		{name: "another host", url: "https://example.invalid/u/101"},
		{name: "credentials", url: "https://user:secret@" + base[len("https://"):] + "/u/101"},
		{name: "unparsable", url: "https://%zz/u/101"},
		{name: "relative", url: "/u/101"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := client.FetchAvatar(t.Context(), tt.url)
			require.ErrorIs(t, err, ErrAvatarURL)

			// Every case is refused, so none of them has sent a request by now.
			assert.Zero(t, requests.Load(), "nothing is requested for a refused URL")
		})
	}
}

func TestFetchAvatarRefusesRedirectsOffTheHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		location string
	}{
		{name: "another host", location: "https://example.invalid/u/101"},
		{name: "endless", location: "/loop"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, base := newAvatarServer(t, func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, tt.location, http.StatusFound)
			})

			_, err := client.FetchAvatar(t.Context(), base+"/loop")
			require.ErrorIs(t, err, ErrAvatarURL)
		})
	}
}

func TestFetchAvatarRefusesWhatIsNotAnImage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		status  int
		body    []byte
		wantErr error
	}{
		{name: "not found", status: http.StatusNotFound, body: []byte("missing"), wantErr: ErrUnexpectedStatus},
		{name: "html", status: http.StatusOK, body: []byte("<html><body>hi</body></html>"), wantErr: ErrAvatarImage},
		{
			name:    "svg",
			status:  http.StatusOK,
			body:    []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`),
			wantErr: ErrAvatarImage,
		},
		{
			name:    "too large",
			status:  http.StatusOK,
			body:    append([]byte(pngHeader), make([]byte, maxAvatarBytes)...),
			wantErr: ErrAvatarImage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, base := newAvatarServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write(tt.body)
			})

			_, err := client.FetchAvatar(t.Context(), base+"/u/101")
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestFetchAvatarStopsWithTheContext(t *testing.T) {
	t.Parallel()

	client, base := newAvatarServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(pngHeader))
	})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := client.FetchAvatar(ctx, base+"/u/101")
	require.ErrorIs(t, err, context.Canceled)
}

func TestAvatarHostDefaultsToGitHub(t *testing.T) {
	t.Parallel()

	client := &Client{}
	assert.Equal(t, DefaultAvatarHost, client.avatarHost())

	for _, ok := range []string{
		"https://avatars.githubusercontent.com/u/101?v=4&s=128",
		"https://AVATARS.githubusercontent.com/u/101",
	} {
		_, err := client.checkAvatarURL(ok)
		require.NoError(t, err, ok)
	}

	for _, bad := range []string{
		"https://avatars.githubusercontent.com.example.invalid/u/101",
		"https://avatars.githubusercontent.com:8443/u/101",
		"https://github.com/octo-user.png",
	} {
		_, err := client.checkAvatarURL(bad)
		require.ErrorIs(t, err, ErrAvatarURL, bad)
	}
}
