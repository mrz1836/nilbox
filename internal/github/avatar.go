package github

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

const (
	// DefaultAvatarHost is GitHub's avatar host, the only host FetchAvatar downloads from.
	DefaultAvatarHost = "avatars.githubusercontent.com"

	// maxAvatarBytes caps a downloaded avatar. A 128-pixel avatar is a few kilobytes.
	maxAvatarBytes = 1 << 20

	// maxAvatarRedirects caps the redirects an avatar download follows, all on the host.
	maxAvatarRedirects = 3
)

var (
	// ErrAvatarURL is returned for an avatar URL that is not https on the avatar host.
	ErrAvatarURL = errors.New("not a GitHub avatar URL")

	// ErrAvatarImage is returned when the download is not a PNG, JPEG, GIF, or WebP image.
	ErrAvatarImage = errors.New("the avatar is not a supported image")
)

// avatarTypes are the image types an avatar may be. SVG is left out because it can
// carry script.
//
//nolint:gochecknoglobals // read-only lookup table
var avatarTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
}

// Avatar is a downloaded profile picture.
type Avatar struct {
	// ContentType is the image's media type, sniffed from its bytes, e.g. "image/png".
	ContentType string

	// Data is the image itself.
	Data []byte
}

// DataURL returns the image as a data: URL, which the UI can show without a network
// request of its own.
func (a *Avatar) DataURL() string {
	return "data:" + a.ContentType + ";base64," + base64.StdEncoding.EncodeToString(a.Data)
}

// FetchAvatar downloads the profile picture at rawURL, a Viewer's AvatarURL. The URL must
// be https on the avatar host, and redirects may not leave that host. No token is sent,
// because avatars are public. The image type is sniffed from the bytes rather than taken
// from the response header, and only raster image types are accepted.
func (c *Client) FetchAvatar(ctx context.Context, rawURL string) (*Avatar, error) {
	u, err := c.checkAvatarURL(rawURL)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build the avatar request: %w", err)
	}

	req.Header.Set("Accept", "image/*")
	req.Header.Set("User-Agent", c.UserAgent)

	// A copy of the client, so the redirect rule applies to this download only.
	hc := *c.httpClient()
	hc.CheckRedirect = redirectsWithin(maxAvatarRedirects, ErrAvatarURL, c.checkAvatarURL)

	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download the avatar: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download the avatar: %w: HTTP %d", ErrUnexpectedStatus, resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxAvatarBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read the avatar: %w", err)
	}

	if len(data) > maxAvatarBytes {
		return nil, fmt.Errorf("%w: it is larger than %d bytes", ErrAvatarImage, maxAvatarBytes)
	}

	contentType := http.DetectContentType(data)
	if !avatarTypes[contentType] {
		return nil, fmt.Errorf("%w: got %s", ErrAvatarImage, contentType)
	}

	return &Avatar{ContentType: contentType, Data: data}, nil
}

// checkAvatarURL parses rawURL and accepts it only as https on the avatar host, with no
// credentials in it.
func (c *Client) checkAvatarURL(rawURL string) (*url.URL, error) {
	host := c.avatarHost()

	return checkPublicURL(rawURL, []string{host}, ErrAvatarURL, "https://"+host+"/")
}

// avatarHost returns the configured avatar host, or GitHub's.
func (c *Client) avatarHost() string {
	if c.AvatarHost == "" {
		return DefaultAvatarHost
	}

	return c.AvatarHost
}

// LoginAvatarSize is the size, in pixels, FetchLoginAvatar asks for: a picture shown 20
// points square, on a display of two pixels to the point.
const LoginAvatarSize = 40

// ErrAvatarLogin is returned for a login FetchLoginAvatar won't ask the avatar host about:
// one that isn't a user's, such as a bot's.
var ErrAvatarLogin = errors.New("not a login with a GitHub avatar")

// FetchLoginAvatar downloads the profile picture of the account with login, LoginAvatarSize
// pixels square, from the avatar host by the login alone, as FetchAvatar does: no API
// request, so no token and no point of the budget. GitHub answers a login it doesn't know
// with its default picture. A login only a user or an organization could have is asked
// about (validLogin): a bot's, which GitHub answers with a pattern made from its name
// rather than the app's picture, fails with ErrAvatarLogin.
func (c *Client) FetchLoginAvatar(ctx context.Context, login string) (*Avatar, error) {
	if !validLogin(login) {
		return nil, fmt.Errorf("%w: %q", ErrAvatarLogin, login)
	}

	u := url.URL{
		Scheme: "https", Host: c.avatarHost(), Path: "/" + login, RawQuery: "s=" + strconv.Itoa(LoginAvatarSize),
	}

	return c.FetchAvatar(ctx, u.String())
}

// maxLoginLength is the longest login validLogin takes: GitHub's own logins run to 39
// characters, and a managed user's adds an underscore and the enterprise's short code.
const maxLoginLength = 64

// validLogin reports whether login is one a user or an organization could have: ASCII
// letters, digits, hyphens, and underscores, starting with a letter or a digit, at most
// maxLoginLength long. A bot's login, which GitHub writes with "[bot]", is not.
func validLogin(login string) bool {
	if login == "" || len(login) > maxLoginLength || login[0] == '-' || login[0] == '_' {
		return false
	}

	for _, r := range login {
		if !loginRune(r) {
			return false
		}
	}

	return true
}

// loginRune reports whether a login may hold r: an ASCII letter or digit, a hyphen, or an
// underscore.
func loginRune(r rune) bool {
	return r == '-' || r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}
