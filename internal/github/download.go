package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

const (
	// maxDownloadRedirects caps the redirects a release download follows: GitHub sends
	// github.com's download link to its asset host once.
	maxDownloadRedirects = 5

	// defaultDownloadStall is how long a download may go without a byte before it stops.
	defaultDownloadStall = 30 * time.Second
)

var (
	// ErrDownloadURL is returned for a download URL, or a redirect, that isn't https on
	// one of the download hosts.
	ErrDownloadURL = errors.New("not a GitHub release download")

	// ErrDownloadTooLarge is returned when a download passes the size it was allowed.
	ErrDownloadTooLarge = errors.New("the download is larger than allowed")

	// ErrDownloadStalled is returned when a download sends nothing for too long.
	ErrDownloadStalled = errors.New("the download stalled")
)

// DefaultDownloadHosts returns the hosts a release download may come from: github.com,
// whose download links redirect to GitHub's release asset host, and the older asset host.
func DefaultDownloadHosts() []string {
	return []string{"github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com"}
}

// DownloadAsset streams the public release file at rawURL into w, and returns how many
// bytes it wrote. The URL must be https on one of the download hosts (DownloadHosts), and
// so must every redirect, at most five of them. No token is sent: the repositories are
// public, and the download hosts are not the API, so the budget isn't touched. A download
// longer than maxBytes fails with ErrDownloadTooLarge, and one that sends nothing for 30
// seconds with ErrDownloadStalled; ctx bounds the whole of it. What was written before a
// failure stays written: the caller throws it away.
func (c *Client) DownloadAsset(ctx context.Context, rawURL string, w io.Writer, maxBytes int64) (int64, error) {
	u, err := c.checkDownloadURL(rawURL)
	if err != nil {
		return 0, err
	}

	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, fmt.Errorf("build the download request: %w", err)
	}

	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Set("User-Agent", c.UserAgent)

	// A copy of the client, so the redirect rule applies to this download only, without
	// the client's timeout, which would cut a slow download short: the stall timer and ctx
	// bound it instead.
	hc := *c.httpClient()
	hc.Timeout = 0
	hc.CheckRedirect = c.checkDownloadRedirect

	stall := time.AfterFunc(c.downloadStall(), func() { cancel(ErrDownloadStalled) })
	defer stall.Stop()

	resp, err := hc.Do(req)
	if err != nil {
		return 0, downloadError(ctx, err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("download %s: %w: HTTP %d", u.Path, ErrUnexpectedStatus, resp.StatusCode)
	}

	body := &stallReader{r: resp.Body, timer: stall, stall: c.downloadStall()}

	n, err := io.Copy(w, io.LimitReader(body, maxBytes+1))
	if err != nil {
		return n, downloadError(ctx, err)
	}

	if n > maxBytes {
		return n, fmt.Errorf("%w: more than %d bytes", ErrDownloadTooLarge, maxBytes)
	}

	return n, nil
}

// downloadError words a failed download, naming a stall as one.
func downloadError(ctx context.Context, err error) error {
	if errors.Is(context.Cause(ctx), ErrDownloadStalled) {
		return fmt.Errorf("%w: nothing arrived for a while", ErrDownloadStalled)
	}

	return fmt.Errorf("download: %w", err)
}

// stallReader resets the stall timer whenever bytes arrive.
type stallReader struct {
	r     io.Reader
	timer *time.Timer
	stall time.Duration
}

// Read reads from the body, and resets the timer when it read something.
func (s *stallReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if n > 0 {
		s.timer.Reset(s.stall)
	}

	return n, err //nolint:wrapcheck // io.Reader's own errors, io.EOF among them
}

// checkDownloadURL parses rawURL and accepts it only as https on a download host, with no
// credentials in it.
func (c *Client) checkDownloadURL(rawURL string) (*url.URL, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDownloadURL, err)
	}

	hosts := c.downloadHosts()
	if u.Scheme != "https" || u.User != nil || !slices.ContainsFunc(hosts, func(h string) bool {
		return strings.EqualFold(u.Host, h)
	}) {
		return nil, fmt.Errorf("%w: want https on %s", ErrDownloadURL, strings.Join(hosts, ", "))
	}

	return u, nil
}

// checkDownloadRedirect lets a download follow a few redirects, each on a download host.
func (c *Client) checkDownloadRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > maxDownloadRedirects {
		return fmt.Errorf("%w: more than %d redirects", ErrDownloadURL, maxDownloadRedirects)
	}

	_, err := c.checkDownloadURL(req.URL.String())

	return err
}

// downloadHosts returns the configured download hosts, or DefaultDownloadHosts.
func (c *Client) downloadHosts() []string {
	if len(c.DownloadHosts) == 0 {
		return DefaultDownloadHosts()
	}

	return c.DownloadHosts
}

// downloadStall returns how long a download may go without a byte.
func (c *Client) downloadStall() time.Duration {
	if c.stall > 0 {
		return c.stall
	}

	return defaultDownloadStall
}
