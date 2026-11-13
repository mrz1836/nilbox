package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
)

// The app reads the user's GitHub notifications, which the REST API calls threads, as the
// web inbox shows them (read and unread, not done), and clears one when the work behind it
// is finished: it marks the thread done, or read, through the chore lane (chore in
// edits.go). It calls three endpoints, and no other (the repoguard test
// githubnotifications_test.go keeps it so): GET /notifications, and DELETE and PATCH on
// /notifications/threads/{id}. It never marks every notification read, a repository's
// read, or changes a subscription.
//
// The listing is conditional: page 1 is sent with the ETag its last answer carried, and
// GitHub answers 304 Not Modified, which costs nothing, while page 1 is as it was. Measured
// in November 2026, github.com answered If-Modified-Since with a 200 that was charged, and
// If-None-Match with a free 304, so the client sends the ETag. A 304 says nothing of the
// later pages: a thread read or marked done there doesn't show on page 1, and only a walk
// of every page catches it.

const (
	// MaxThreadPages is the most pages of notifications the app reads: 1,000 threads.
	MaxThreadPages = 20

	// ThreadsPerPage is the most threads GitHub lists on a page, which the listing asks
	// for.
	ThreadsPerPage = 50

	// maxPollSeconds caps the X-Poll-Interval the app takes from GitHub: a day.
	maxPollSeconds = 24 * 60 * 60

	// maxAPIURLLen caps an API URL the app reads from a notification.
	maxAPIURLLen = 2048
)

// The notification endpoints' REST paths. The listing's query is always the same fixed
// parameters (threadListQuery), and a thread's path ends with its ID (threadPath).
const (
	notificationsPath = "/notifications"
	threadsPath       = "/notifications/threads/"
)

var (
	// ErrThreadNotFound is returned when GitHub answers a write to a notification thread
	// 404 Not Found, as for a thread that is already done or isn't the user's.
	ErrThreadNotFound = errors.New("GitHub found no such notification thread")

	// ErrInvalidThreadPage is returned for a page of notifications past MaxThreadPages.
	ErrInvalidThreadPage = errors.New("the app reads no such page of notifications")

	// ErrInvalidCommentURL is returned for a comment's URL that isn't the API's URL of an
	// issue's or a pull request's comment; nothing is sent.
	ErrInvalidCommentURL = errors.New("not the URL of a comment on GitHub's API")

	// errNullList is a listing whose body is null rather than a list.
	errNullList = errors.New("the list is null")
)

// nextPageLink finds the next page's link in a Link header.
var nextPageLink = regexp.MustCompile(`<([^>]*)>;\s*rel="next"`)

// ThreadQuery asks for one page of the user's notifications.
type ThreadQuery struct {
	// Page is the page to read, from 1 (0 reads page 1) to MaxThreadPages.
	Page int

	// ETag is the ETag the last answer for page 1 carried, which page 1 is sent with; ""
	// sends none. A later page is never sent with one.
	ETag string
}

// ThreadPage is one page of the user's notifications, newest updated first.
type ThreadPage struct {
	// Threads are the page's threads, but those dropped. Empty when NotModified.
	Threads []Thread

	// Dropped counts the page's threads that were left out, as one whose ID isn't a
	// positive number, or that couldn't be read at all.
	Dropped int

	// NotModified reports that GitHub answered 304 Not Modified to the ETag sent: page 1
	// is as it was in the answer that carried it, and the read cost nothing. It says
	// nothing of the later pages: a thread read or marked done there doesn't change page
	// 1, so the caller catches those only by a walk of every page.
	NotModified bool

	// ETag is page 1's ETag, for the next read of page 1: the answer's, or the one sent
	// when a 304 carried none. It is "" on every later page, which is never sent with one.
	ETag string

	// PollInterval is how long GitHub asks the app to wait before it reads the
	// notifications again (X-Poll-Interval), or 0 when it did not say.
	PollInterval time.Duration

	// NextPage is the page after this one, or 0 when there is none, or it would be past
	// MaxThreadPages.
	NextPage int

	// Capped reports that GitHub lists a page after this one, past MaxThreadPages, which
	// the app reads no further: the threads past it are left out. NextPage is then 0.
	Capped bool
}

// Thread is one notification thread: GitHub's notice about one subject, such as a pull
// request or an issue.
type Thread struct {
	// ID is GitHub's ID of the thread, always positive.
	ID int64

	// Unread reports a thread the user hasn't read.
	Unread bool

	// Reason is why the user was notified, in GitHub's words: review_requested, mention,
	// author, assign, subscribed, ci_activity, and others.
	Reason string

	// UpdatedAt is when the thread last changed, and LastReadAt when the user last read
	// it, zero when never.
	UpdatedAt  time.Time
	LastReadAt time.Time

	// SubjectType is what the thread is about, in GitHub's words: PullRequest, Issue,
	// Release, CheckSuite, Discussion, Commit, and others.
	SubjectType string

	// Title is the subject's title.
	Title string

	// Repo is the thread's repository; zero when GitHub named none that could be one.
	Repo repo.Name

	// Ref is the pull request or issue the subject's API URL names (ParseSubject); zero,
	// with Kind "", for any other subject. A web link to it is built from the Ref, never
	// rewritten from the API's URL.
	Ref item.Ref

	// LatestCommentURL is the subject's latest comment's API URL as GitHub gave it, which
	// is often the subject's own URL (after a merge or a close), or "" when GitHub gave
	// none. FetchLatestComment reads a comment's author from it.
	LatestCommentURL string
}

// CommentAuthor is who wrote a comment.
type CommentAuthor struct {
	// Login is the author's login, "" when GitHub names none, as for a deleted account.
	Login string

	// Type is the author's type, in GitHub's words: User, Bot, Organization, and others.
	Type string
}

// ListThreads reads one page of the user's notifications, as the web inbox shows them:
// read and unread, but not done, newest updated first. Its URL is always built from the
// same fixed parameters, and only the page changes; the next page's number is read from
// GitHub's Link header, which is never followed as given. Page 1 sent with the ETag of its
// last answer, which GitHub answers 304 Not Modified while page 1 is as it was, costs
// nothing; a 304 says nothing of the later pages (ThreadPage.NotModified).
func (c *Client) ListThreads(ctx context.Context, tok Token, q ThreadQuery) (*ThreadPage, error) {
	page, err := c.listThreads(ctx, tok, q)

	return page, Redact(err, tok)
}

// listThreads sends the listing and reads its answer.
func (c *Client) listThreads(ctx context.Context, tok Token, q ThreadQuery) (*ThreadPage, error) {
	page := max(q.Page, 1)
	if page > MaxThreadPages {
		return nil, fmt.Errorf("%w: page %d", ErrInvalidThreadPage, page)
	}

	var opts []requestOption
	if page == 1 && q.ETag != "" {
		opts = append(opts, withETag(q.ETag))
	}

	raw, header, err := c.do(ctx, tok, http.MethodGet, notificationsPath+"?"+threadListQuery(page), nil, opts...)

	if unchanged, ok := errors.AsType[*notModifiedError](err); ok {
		etag := unchanged.header.Get("ETag")
		if etag == "" {
			etag = q.ETag
		}

		return &ThreadPage{
			Threads: []Thread{}, NotModified: true, ETag: etag, PollInterval: pollInterval(unchanged.header),
		}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("list the notifications: %w", err)
	}

	base := c.apiBase()

	threads, dropped, err := decodeThreads(base, raw)
	if err != nil {
		return nil, fmt.Errorf("%w: decode the notifications: %w", ErrMalformedResponse, err)
	}

	out := &ThreadPage{Threads: threads, Dropped: dropped, PollInterval: pollInterval(header)}
	out.NextPage, out.Capped = pageAfter(nextThreadPage(base, header, page))

	// Only page 1 is ever sent with an ETag, so only its ETag is kept.
	if page == 1 {
		out.ETag = header.Get("ETag")
	}

	return out, nil
}

// MarkThreadDone marks the notification thread id done, which takes it out of the user's
// inbox on GitHub, with one REST request through the chore lane (DELETE
// /notifications/threads/{id}, which GitHub answers 204 No Content). A thread GitHub
// can't find is ErrThreadNotFound, which the caller may take as done already.
func (c *Client) MarkThreadDone(ctx context.Context, tok Token, id int64) error {
	return Redact(markedError(id, "done", c.chore(ctx, tok, http.MethodDelete, id)), tok)
}

// MarkThreadRead marks the notification thread id read, which leaves it in the user's
// inbox on GitHub, with one REST request through the chore lane (PATCH
// /notifications/threads/{id}, which GitHub answers 205 Reset Content). A thread GitHub
// can't find is ErrThreadNotFound.
func (c *Client) MarkThreadRead(ctx context.Context, tok Token, id int64) error {
	return Redact(markedError(id, "read", c.chore(ctx, tok, http.MethodPatch, id)), tok)
}

// markedError is err, what a chore marking the thread id as done or read returned, with
// the thread named, and a 404 named ErrThreadNotFound; nil when the chore went through.
func markedError(id int64, as string, err error) error {
	if status, ok := errors.AsType[*StatusError](err); ok && status.StatusCode == http.StatusNotFound {
		return fmt.Errorf("mark thread %d %s: %w: %w", id, as, ErrThreadNotFound, err)
	}

	if err != nil {
		return fmt.Errorf("mark thread %d %s: %w", id, as, err)
	}

	return nil
}

// threadListQuery is the listing's query for page: the web inbox's threads (all=true reads
// the read ones too, is_done=false leaves the done ones out), as many to a page as GitHub
// lists, always in this order.
func threadListQuery(page int) string {
	return "all=true&is_done=false&per_page=" + strconv.Itoa(ThreadsPerPage) + "&page=" + strconv.Itoa(page)
}

// threadPath is the REST path of the notification thread id.
func threadPath(id int64) string {
	return threadsPath + strconv.FormatInt(id, 10)
}

// pollInterval reads an answer's X-Poll-Interval, in whole seconds, or returns 0 when it
// has none that can be read, or one past a day.
func pollInterval(h http.Header) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(h.Get("X-Poll-Interval")))
	if err != nil || seconds <= 0 || seconds > maxPollSeconds {
		return 0
	}

	return time.Duration(seconds) * time.Second
}

// nextThreadPage reads the number of the page after current from an answer's Link header,
// and only the number: the link must be the API's own listing (base's scheme, host, and
// path), and its page one number after current or more; 0 for anything else. The caller
// holds it to MaxThreadPages.
func nextThreadPage(base *url.URL, header http.Header, current int) int {
	m := nextPageLink.FindStringSubmatch(header.Get("Link"))
	if m == nil || base == nil {
		return 0
	}

	link, err := url.Parse(m[1])
	if err != nil || !isListing(base, link) {
		return 0
	}

	next, ok := onlyPage(link.RawQuery)
	if !ok || next <= current {
		return 0
	}

	return next
}

// pageAfter is the page the app reads after one whose Link names next (0 for none): next,
// or 0 past MaxThreadPages, which is never asked for, though GitHub listing it says threads
// are left out (capped).
func pageAfter(next int) (page int, capped bool) {
	if next > MaxThreadPages {
		return 0, true
	}

	return next, false
}

// NotificationsRefused reports whether err is GitHub refusing the token the notifications
// listing itself, as it does a fine-grained token: 404, or 403 that is neither a rate limit
// (rateLimited) nor carries a Retry-After. A rate limit says to wait, not that the token
// can't read them; a 401 is the token itself, which WithToken borrows again.
func NotificationsRefused(err error) bool {
	status, ok := errors.AsType[*StatusError](err)
	if !ok {
		return false
	}

	switch status.StatusCode {
	case http.StatusNotFound:
		return true
	case http.StatusForbidden:
		return !rateLimited(status) && status.RetryAfter == 0
	default:
		return false
	}
}

// isListing reports whether link is the API's own listing of notifications: on base, at
// base's path and the listing's.
func isListing(base, link *url.URL) bool {
	listing, ok := strings.CutPrefix(link.EscapedPath(), basePath(base))

	return ok && sameAPI(base, link) && listing == notificationsPath
}

// onlyPage reads the page number a link's query names, when it names one alone.
func onlyPage(rawQuery string) (int, bool) {
	query, err := url.ParseQuery(rawQuery)
	if err != nil || len(query["page"]) != 1 {
		return 0, false
	}

	page, err := strconv.Atoi(query["page"][0])

	return page, err == nil
}

// restThread is one notification thread as the REST API lists it. GitHub's nulls decode
// to zero values.
type restThread struct {
	ID         string    `json:"id"`
	Unread     bool      `json:"unread"`
	Reason     string    `json:"reason"`
	UpdatedAt  time.Time `json:"updated_at"`
	LastReadAt time.Time `json:"last_read_at"`
	Subject    struct {
		Title            string `json:"title"`
		URL              string `json:"url"`
		LatestCommentURL string `json:"latest_comment_url"`
		Type             string `json:"type"`
	} `json:"subject"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

// decodeThreads decodes one page of threads, reading each subject against base, the API's
// root. It returns the threads it kept and how many it dropped: one that can't be read,
// or whose ID isn't a positive number. Only a body that isn't a list fails.
func decodeThreads(base *url.URL, raw []byte) ([]Thread, int, error) {
	var batch []json.RawMessage

	if err := json.Unmarshal(raw, &batch); err != nil {
		return nil, 0, err
	}

	if batch == nil {
		return nil, 0, errNullList
	}

	threads := make([]Thread, 0, len(batch))
	dropped := 0

	for _, entry := range batch {
		thread, ok := decodeThread(base, entry)
		if !ok {
			dropped++

			continue
		}

		threads = append(threads, thread)
	}

	return threads, dropped, nil
}

// decodeThread decodes one thread; ok is false for one that can't be read, or whose ID
// isn't a positive number.
func decodeThread(base *url.URL, raw json.RawMessage) (Thread, bool) {
	var t restThread

	if err := json.Unmarshal(raw, &t); err != nil {
		return Thread{}, false
	}

	id, ok := threadID(t.ID)
	if !ok {
		return Thread{}, false
	}

	out := Thread{
		ID: id, Unread: t.Unread, Reason: t.Reason, UpdatedAt: t.UpdatedAt.UTC(), LastReadAt: t.LastReadAt.UTC(),
		SubjectType: t.Subject.Type, Title: t.Subject.Title, LatestCommentURL: t.Subject.LatestCommentURL,
	}

	if name, err := repo.Parse(t.Repository.FullName); err == nil {
		out.Repo = name
	}

	out.Ref, _ = parseSubject(base, t.Subject.URL)

	return out, true
}

// threadID reads a thread's ID, a string of digits, as a positive int64.
func threadID(s string) (int64, bool) {
	if s == "" {
		return 0, false
	}

	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}

	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}

	return id, true
}

// ParseSubject reads a notification subject's API URL into the pull request or issue it
// names: the client's API root's scheme and host, and exactly the path
// /repos/{owner}/{name}/pulls/{number} or /repos/{owner}/{name}/issues/{number}, with a
// valid owner and name and a positive number written plainly. Anything else, "" (a null
// URL, as a check suite's) included, names no item: ok is false, and the Ref is zero, with
// Kind "".
func (c *Client) ParseSubject(raw string) (item.Ref, bool) {
	return parseSubject(c.apiBase(), raw)
}

// parseSubject is ParseSubject against base, the API's root.
func parseSubject(base *url.URL, raw string) (item.Ref, bool) {
	segments, ok := apiPathSegments(base, raw, 4)
	if !ok {
		return item.Ref{}, false
	}

	var kind item.Kind

	switch segments[2] {
	case "pulls":
		kind = item.KindPR
	case "issues":
		kind = item.KindIssue
	default:
		return item.Ref{}, false
	}

	name, err := repo.New(segments[0], segments[1])
	if err != nil {
		return item.Ref{}, false
	}

	number, err := strconv.Atoi(segments[3])
	if err != nil || number <= 0 || strconv.Itoa(number) != segments[3] {
		return item.Ref{}, false
	}

	return item.Ref{Repo: name, Number: number, Kind: kind}, true
}

// FetchLatestComment reads who wrote a comment, from its API URL as a notification gives
// it (Thread.LatestCommentURL), with one REST request. The URL must be the client's API
// root's scheme and host, with exactly the path /repos/{owner}/{name}/issues/comments/{id}
// or /repos/{owner}/{name}/pulls/comments/{id}; anything else, the subject's own URL
// included, is refused with ErrInvalidCommentURL before anything is sent. Only the path
// built again from what was checked is sent, never the URL as given.
func (c *Client) FetchLatestComment(ctx context.Context, tok Token, rawURL string) (*CommentAuthor, error) {
	author, err := c.fetchLatestComment(ctx, tok, rawURL)

	return author, Redact(err, tok)
}

// fetchLatestComment checks the URL, sends the request, and reads the comment's author.
func (c *Client) fetchLatestComment(ctx context.Context, tok Token, rawURL string) (*CommentAuthor, error) {
	path, ok := commentPath(c.apiBase(), rawURL)
	if !ok {
		return nil, ErrInvalidCommentURL
	}

	raw, _, err := c.do(ctx, tok, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("read the latest comment: %w", err)
	}

	var answer struct {
		User *struct {
			Login string `json:"login"`
			Type  string `json:"type"`
		} `json:"user"`
	}

	if err = json.Unmarshal(raw, &answer); err != nil {
		return nil, fmt.Errorf("%w: decode the latest comment: %w", ErrMalformedResponse, err)
	}

	if answer.User == nil {
		return &CommentAuthor{}, nil
	}

	return &CommentAuthor{Login: answer.User.Login, Type: answer.User.Type}, nil
}

// commentPath checks a comment's API URL against base, the API's root, and returns the
// REST path built again from what it checked, without base's own path, as do takes it.
func commentPath(base *url.URL, raw string) (string, bool) {
	segments, ok := apiPathSegments(base, raw, 5)
	if !ok || (segments[2] != "issues" && segments[2] != "pulls") || segments[3] != "comments" {
		return "", false
	}

	if !repo.ValidOwner(segments[0]) || !repo.ValidRepo(segments[1]) {
		return "", false
	}

	id, ok := threadID(segments[4])
	if !ok || strconv.FormatInt(id, 10) != segments[4] {
		return "", false
	}

	return "/repos/" + url.PathEscape(segments[0]) + "/" + url.PathEscape(segments[1]) + "/" + segments[2] +
		"/comments/" + strconv.FormatInt(id, 10), true
}

// apiPathSegments parses raw, an API URL GitHub gave, and returns the n segments of its
// path after base's own path and /repos/, as written: ok is false unless it is on base
// (sameAPI), with no query, and has exactly n segments, none empty.
func apiPathSegments(base *url.URL, raw string, n int) ([]string, bool) {
	u, ok := apiURL(base, raw)
	if !ok {
		return nil, false
	}

	rest, ok := strings.CutPrefix(u.EscapedPath(), basePath(base)+"/repos/")
	if !ok {
		return nil, false
	}

	segments := strings.Split(rest, "/")
	if len(segments) != n || slices.Contains(segments, "") {
		return nil, false
	}

	return segments, true
}

// apiURL parses raw, an API URL GitHub gave, when it is on base (sameAPI) with no query.
func apiURL(base *url.URL, raw string) (*url.URL, bool) {
	if base == nil || raw == "" || len(raw) > maxAPIURLLen {
		return nil, false
	}

	u, err := url.Parse(raw)
	if err != nil || !sameAPI(base, u) || u.RawQuery != "" || u.ForceQuery {
		return nil, false
	}

	return u, true
}

// sameAPI reports whether u is on base, the API's root: the same scheme and host, written
// alike, with no user or fragment.
func sameAPI(base, u *url.URL) bool {
	return u.Scheme == base.Scheme && u.Host == base.Host && u.Host != "" && u.User == nil && u.Opaque == "" &&
		u.Fragment == "" && u.RawFragment == ""
}

// basePath is base's own path, without a trailing slash: "" for GitHub's API.
func basePath(base *url.URL) string {
	return strings.TrimRight(base.EscapedPath(), "/")
}

// apiBase parses the client's API root, or returns nil when it can't be.
func (c *Client) apiBase() *url.URL {
	base, err := url.Parse(c.baseURL())
	if err != nil || base.Scheme == "" || base.Host == "" {
		return nil
	}

	return base
}
