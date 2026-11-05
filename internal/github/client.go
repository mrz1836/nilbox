package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/brand"
)

const (
	// DefaultBaseURL is GitHub's public API.
	DefaultBaseURL = "https://api.github.com"

	// defaultTimeout bounds one API call when the caller's context has no deadline.
	defaultTimeout = 30 * time.Second

	// maxResponseBytes caps how much of a response is read. A full inbox is a few
	// hundred kilobytes.
	maxResponseBytes = 8 << 20

	// maxMessageRunes caps how much of an API error message is kept.
	maxMessageRunes = 300

	// scopesHeader carries a classic OAuth token's granted scopes.
	scopesHeader = "X-OAuth-Scopes"

	typePullRequest = "PullRequest"
	typeIssue       = "Issue"
)

var (
	// ErrGraphQL is returned when a GraphQL response carries errors.
	ErrGraphQL = errors.New("the GitHub GraphQL API returned errors")

	// ErrUnexpectedStatus matches a *StatusError with errors.Is.
	ErrUnexpectedStatus = errors.New("unexpected response status from the GitHub API")

	// ErrMalformedResponse is returned for a response body that can't be used.
	ErrMalformedResponse = errors.New("malformed response from the GitHub API")
)

// StatusError is a non-2xx response from the GitHub API.
type StatusError struct {
	StatusCode int
	Message    string
}

// Error describes the status, GitHub's message, and a hint for a rejected token.
func (e *StatusError) Error() string {
	msg := fmt.Sprintf("the GitHub API returned HTTP %d", e.StatusCode)
	if e.Message != "" {
		msg += ": " + e.Message
	}

	if e.StatusCode == http.StatusUnauthorized {
		msg += " (run gh auth login)"
	}

	return msg
}

// Is reports whether target is ErrUnexpectedStatus.
func (e *StatusError) Is(target error) bool {
	return target == ErrUnexpectedStatus
}

// Client calls the GitHub REST and GraphQL APIs.
type Client struct {
	// BaseURL is the API root, without a trailing slash. Tests point it at a local server.
	BaseURL string

	// HTTP sends the requests.
	HTTP *http.Client

	// UserAgent identifies the app to GitHub.
	UserAgent string

	// AvatarHost is the only host FetchAvatar downloads from, as host or host:port. It
	// defaults to DefaultAvatarHost; tests point it at a local server.
	AvatarHost string
}

// NewClient returns a client for GitHub's public API that identifies itself with the
// app's slug and version.
func NewClient(version string) *Client {
	if version == "" {
		version = "dev"
	}

	return &Client{
		BaseURL:    DefaultBaseURL,
		HTTP:       &http.Client{Timeout: defaultTimeout},
		UserAgent:  brand.Slug + "/" + version,
		AvatarHost: DefaultAvatarHost,
	}
}

// FetchInbox runs the batched inbox query: one POST to /graphql. The result includes the
// token's X-OAuth-Scopes header for CheckScopes.
func (c *Client) FetchInbox(ctx context.Context, tok Token) (*Inbox, error) {
	inbox, err := c.fetchInbox(ctx, tok)

	return inbox, Redact(err, tok)
}

// FetchRateLimits reads the REST, GraphQL, and search budgets from GET /rate_limit, which
// does not count against them.
func (c *Client) FetchRateLimits(ctx context.Context, tok Token) (*RateLimits, error) {
	limits, err := c.fetchRateLimits(ctx, tok)

	return limits, Redact(err, tok)
}

// fetchInbox sends the query and decodes the response.
func (c *Client) fetchInbox(ctx context.Context, tok Token) (*Inbox, error) {
	body, err := json.Marshal(graphQLRequest{Query: inboxQuery})
	if err != nil {
		return nil, fmt.Errorf("encode the inbox query: %w", err)
	}

	data, header, err := c.do(ctx, tok, http.MethodPost, "/graphql", body)
	if err != nil {
		return nil, err
	}

	var resp graphQLResponse
	if err = json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("%w: decode the inbox response: %w", ErrMalformedResponse, err)
	}

	if len(resp.Errors) > 0 {
		return nil, resp.err()
	}

	if resp.Data == nil {
		return nil, fmt.Errorf("%w: the inbox response has no data", ErrMalformedResponse)
	}

	inbox := resp.Data.inbox()
	inbox.OAuthScopes = header.Get(scopesHeader)

	return inbox, nil
}

// do sends one authenticated request and returns the body of a 2xx response.
func (c *Client) do(ctx context.Context, tok Token, method, path string, body []byte) ([]byte, http.Header, error) {
	if tok.IsZero() {
		return nil, nil, ErrNotLoggedIn
	}

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL()+path, reader)
	if err != nil {
		return nil, nil, fmt.Errorf("build the GitHub API request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+tok.Reveal())
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", c.UserAgent)

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("call the GitHub API: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	data, err := readBody(resp.Body)
	if err != nil {
		return nil, nil, err
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, nil, &StatusError{StatusCode: resp.StatusCode, Message: apiMessage(data)}
	}

	return data, resp.Header, nil
}

// baseURL returns the configured API root without a trailing slash.
func (c *Client) baseURL() string {
	if c.BaseURL == "" {
		return DefaultBaseURL
	}

	return strings.TrimRight(c.BaseURL, "/")
}

// httpClient returns the configured HTTP client, or a default one with a timeout.
func (c *Client) httpClient() *http.Client {
	if c.HTTP == nil {
		return &http.Client{Timeout: defaultTimeout}
	}

	return c.HTTP
}

// readBody reads a response body up to maxResponseBytes.
func readBody(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read the GitHub API response: %w", err)
	}

	if len(data) > maxResponseBytes {
		return nil, fmt.Errorf("%w: the response is larger than %d bytes", ErrMalformedResponse, maxResponseBytes)
	}

	return data, nil
}

// apiMessage extracts GitHub's error message from a JSON error body.
func apiMessage(data []byte) string {
	var body struct {
		Message string `json:"message"`
	}

	if json.Unmarshal(data, &body) != nil {
		return ""
	}

	return truncate(body.Message)
}

// truncate collapses whitespace and caps a message at maxMessageRunes.
func truncate(msg string) string {
	msg = strings.Join(strings.Fields(msg), " ")
	if runes := []rune(msg); len(runes) > maxMessageRunes {
		return string(runes[:maxMessageRunes]) + "…"
	}

	return msg
}

// Inbox is the decoded result of the batched inbox query.
type Inbox struct {
	// Viewer is the token's account.
	Viewer Viewer

	ReviewRequested SearchResult
	Authored        SearchResult
	Assigned        SearchResult

	// RateLimit is the GraphQL budget after this query, including its cost.
	RateLimit QueryRate

	// OAuthScopes is the response's X-OAuth-Scopes header ("" when absent).
	OAuthScopes string
}

// Viewer is the account a token belongs to.
type Viewer struct {
	// Login is the account's username.
	Login string

	// Name is the profile's display name ("" when the profile sets none).
	Name string

	// URL is the account's profile page on GitHub.
	URL string

	// AvatarURL is the profile picture on GitHub's avatar host. FetchAvatar downloads it.
	AvatarURL string
}

// SearchResult is one aliased search: the total match count and the items returned.
type SearchResult struct {
	// IssueCount is GitHub's total match count, which can exceed len(Items).
	IssueCount int
	Items      []Item
}

// Item is one pull request or issue from a search.
type Item struct {
	IsPR      bool
	NodeID    string
	Number    int
	Title     string
	URL       string
	CreatedAt time.Time
	UpdatedAt time.Time

	// IsDraft and HeadRef are set for pull requests only.
	IsDraft bool
	HeadRef string

	AuthorLogin string

	// AuthorAssociation is the author's relationship to the repository (OWNER, MEMBER,
	// COLLABORATOR, CONTRIBUTOR, NONE, ...).
	AuthorAssociation string

	// IsCrossRepository reports a pull request opened from another repository (a fork).
	IsCrossRepository bool

	RepoOwner string
	RepoName  string
}

// QueryRate is the GraphQL budget reported by the query itself.
type QueryRate struct {
	Cost      int
	Limit     int
	Remaining int
	Used      int
	ResetAt   time.Time
}

// graphQLRequest is a GraphQL request body.
type graphQLRequest struct {
	Query string `json:"query"`
}

// graphQLResponse is the inbox query's response body.
type graphQLResponse struct {
	Data   *graphQLData   `json:"data"`
	Errors []graphQLError `json:"errors"`
}

// err joins the response's error messages under ErrGraphQL.
func (r *graphQLResponse) err() error {
	msgs := make([]string, 0, len(r.Errors))
	for _, e := range r.Errors {
		msgs = append(msgs, e.Message)
	}

	return fmt.Errorf("%w: %s", ErrGraphQL, truncate(strings.Join(msgs, "; ")))
}

// graphQLError is one entry of a GraphQL errors array.
type graphQLError struct {
	Message string `json:"message"`
}

// graphQLData is the inbox query's data object.
type graphQLData struct {
	Viewer          graphQLViewer    `json:"viewer"`
	ReviewRequested graphQLSearch    `json:"reviewRequested"`
	Authored        graphQLSearch    `json:"authored"`
	Assigned        graphQLSearch    `json:"assigned"`
	RateLimit       graphQLRateLimit `json:"rateLimit"`
}

// inbox converts the decoded data.
func (d *graphQLData) inbox() *Inbox {
	return &Inbox{
		Viewer: Viewer{
			Login:     d.Viewer.Login,
			Name:      d.Viewer.Name,
			URL:       d.Viewer.URL,
			AvatarURL: d.Viewer.AvatarURL,
		},
		ReviewRequested: d.ReviewRequested.result(),
		Authored:        d.Authored.result(),
		Assigned:        d.Assigned.result(),
		RateLimit: QueryRate{
			Cost:      d.RateLimit.Cost,
			Limit:     d.RateLimit.Limit,
			Remaining: d.RateLimit.Remaining,
			Used:      d.RateLimit.Used,
			ResetAt:   d.RateLimit.ResetAt.UTC(),
		},
	}
}

// graphQLActor is a user or bot.
type graphQLActor struct {
	Login string `json:"login"`
}

// graphQLViewer is the query's viewer object. Name is null when the profile sets none,
// which decodes to "".
type graphQLViewer struct {
	Login     string `json:"login"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	AvatarURL string `json:"avatarUrl"`
}

// graphQLRepository is an item's repository.
type graphQLRepository struct {
	Name  string       `json:"name"`
	Owner graphQLActor `json:"owner"`
}

// graphQLRateLimit is the query's rateLimit object.
type graphQLRateLimit struct {
	Cost      int       `json:"cost"`
	Limit     int       `json:"limit"`
	Remaining int       `json:"remaining"`
	Used      int       `json:"used"`
	ResetAt   time.Time `json:"resetAt"`
}

// graphQLSearch is one aliased search.
type graphQLSearch struct {
	IssueCount int            `json:"issueCount"`
	Nodes      []*graphQLNode `json:"nodes"`
}

// result keeps the pull requests and issues, skipping null and other nodes.
func (s *graphQLSearch) result() SearchResult {
	items := make([]Item, 0, len(s.Nodes))

	for _, n := range s.Nodes {
		if item, ok := n.item(); ok {
			items = append(items, item)
		}
	}

	return SearchResult{IssueCount: s.IssueCount, Items: items}
}

// graphQLNode is one search node: a pull request, an issue, or something else.
type graphQLNode struct {
	TypeName          string             `json:"__typename"`
	ID                string             `json:"id"`
	Number            int                `json:"number"`
	Title             string             `json:"title"`
	URL               string             `json:"url"`
	CreatedAt         time.Time          `json:"createdAt"`
	UpdatedAt         time.Time          `json:"updatedAt"`
	IsDraft           bool               `json:"isDraft"`
	HeadRefName       string             `json:"headRefName"`
	AuthorAssociation string             `json:"authorAssociation"`
	IsCrossRepository bool               `json:"isCrossRepository"`
	Author            *graphQLActor      `json:"author"`
	Repository        *graphQLRepository `json:"repository"`
}

// item converts a pull request or issue node; ok is false for anything else.
func (n *graphQLNode) item() (Item, bool) {
	if n == nil || n.Repository == nil || n.Number <= 0 {
		return Item{}, false
	}

	if n.TypeName != typePullRequest && n.TypeName != typeIssue {
		return Item{}, false
	}

	item := Item{
		IsPR:              n.TypeName == typePullRequest,
		NodeID:            n.ID,
		Number:            n.Number,
		Title:             n.Title,
		URL:               n.URL,
		CreatedAt:         n.CreatedAt.UTC(),
		UpdatedAt:         n.UpdatedAt.UTC(),
		AuthorAssociation: n.AuthorAssociation,
		RepoOwner:         n.Repository.Owner.Login,
		RepoName:          n.Repository.Name,
	}

	if item.IsPR {
		item.IsDraft = n.IsDraft
		item.HeadRef = n.HeadRefName
		item.IsCrossRepository = n.IsCrossRepository
	}

	if n.Author != nil {
		item.AuthorLogin = n.Author.Login
	}

	return item, true
}
