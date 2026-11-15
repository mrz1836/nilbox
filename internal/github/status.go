package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
)

const (
	// maxStatusBytes caps a status page's summary, which runs to a few kilobytes even
	// during an incident.
	maxStatusBytes = 1 << 20

	// maxStatusRedirects caps the redirects a read follows, all on the request's own host.
	maxStatusRedirects = 3
)

// ErrStatusURL is returned for a status page host that isn't one of the client's status
// hosts, a path that isn't absolute and clean, or a redirect off the request's own host.
var ErrStatusURL = errors.New("not a status page the app reads")

// DefaultStatusHosts returns the status pages FetchStatus may read from: GitHub's,
// Claude's, and OpenAI's. Each gets only the request for its summary: no token, and
// nothing of the user's.
func DefaultStatusHosts() []string {
	return []string{"www.githubstatus.com", "status.claude.com", "status.openai.com"}
}

// StatusRequest asks for a status page's summary.
type StatusRequest struct {
	// Host is the page's host, one of the client's status hosts (StatusHosts).
	Host string

	// Path is the summary's path on the host, absolute and clean, e.g.
	// /api/v2/summary.json.
	Path string

	// ETag is the ETag of the last answer, sent as If-None-Match, or "" for none.
	ETag string
}

// StatusAnswer is a status page's answer.
type StatusAnswer struct {
	// Body is the summary as the page sent it, nil when NotModified.
	Body []byte

	// ETag is the answer's ETag, or "" when it sent none. A 304 carries the request's
	// ETag on, unless it sends another.
	ETag string

	// NotModified reports that the page answered 304: the summary is the one the
	// request's ETag names.
	NotModified bool
}

// FetchStatus reads a status page's summary: one GET of req.Path from req.Host, which
// must be one of the client's status hosts. A status page isn't the API, so the read sends
// no token and spends no point of any budget. With an ETag it is conditional, and a 304
// carries no body. Only https is spoken, redirects may not leave the request's host, and
// a summary larger than 1 MiB is refused. It only reads the bytes: what they say is the
// caller's to decode.
func (c *Client) FetchStatus(ctx context.Context, req StatusRequest) (StatusAnswer, error) {
	target, err := c.statusURL(req)
	if err != nil {
		return StatusAnswer{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return StatusAnswer{}, fmt.Errorf("build the status request: %w", err)
	}

	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", c.UserAgent)

	if req.ETag != "" {
		httpReq.Header.Set("If-None-Match", req.ETag)
	}

	// A copy of the client, so the redirect rule applies to this read only.
	hc := *c.httpClient()
	hc.CheckRedirect = redirectsWithin(maxStatusRedirects, ErrStatusURL, func(rawURL string) (*url.URL, error) {
		return checkPublicURL(rawURL, []string{req.Host}, ErrStatusURL, "https://"+req.Host+"/")
	})

	resp, err := hc.Do(httpReq)
	if err != nil {
		return StatusAnswer{}, fmt.Errorf("read the status page at %s: %w", req.Host, err)
	}

	defer func() { _ = resp.Body.Close() }()

	return readStatusAnswer(resp, req)
}

// statusURL returns the URL req reads, refusing a host that isn't one of the client's
// status hosts and a path that isn't absolute and clean.
func (c *Client) statusURL(req StatusRequest) (string, error) {
	if req.Path == "" || req.Path[0] != '/' || path.Clean(req.Path) != req.Path {
		return "", fmt.Errorf("%w: the path %q isn't absolute and clean", ErrStatusURL, req.Path)
	}

	hosts := c.statusHosts()
	u := url.URL{Scheme: "https", Host: req.Host, Path: req.Path}

	if _, err := checkPublicURL(u.String(), hosts, ErrStatusURL, "a status page on "+strings.Join(hosts, ", ")); err != nil {
		return "", err
	}

	return u.String(), nil
}

// readStatusAnswer reads a status page's answer to req: a 304 to a conditional request, or
// a summary of at most maxStatusBytes.
func readStatusAnswer(resp *http.Response, req StatusRequest) (StatusAnswer, error) {
	etag := resp.Header.Get("ETag")

	switch {
	case resp.StatusCode == http.StatusNotModified && req.ETag != "":
		if etag == "" {
			etag = req.ETag
		}

		return StatusAnswer{ETag: etag, NotModified: true}, nil
	case resp.StatusCode != http.StatusOK:
		return StatusAnswer{}, fmt.Errorf("read the status page at %s: %w: HTTP %d", req.Host, ErrUnexpectedStatus,
			resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxStatusBytes+1))
	if err != nil {
		return StatusAnswer{}, fmt.Errorf("read the status page at %s: %w", req.Host, err)
	}

	if len(data) > maxStatusBytes {
		return StatusAnswer{}, fmt.Errorf("read the status page at %s: %w: it is larger than %d bytes", req.Host,
			ErrUnexpectedStatus, maxStatusBytes)
	}

	return StatusAnswer{Body: data, ETag: etag}, nil
}

// statusHosts returns the configured status hosts, or DefaultStatusHosts.
func (c *Client) statusHosts() []string {
	if len(c.StatusHosts) == 0 {
		return DefaultStatusHosts()
	}

	return c.StatusHosts
}
