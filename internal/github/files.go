package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/mrz1836/nilbox/internal/item"
)

// The REST API lists a pull request's files 100 to a page, and at most 3,000 of them.
const (
	filesPerPage   = 100
	maxListedFiles = 3000
	maxFilePages   = maxListedFiles / filesPerPage
)

// filesParallel is how many pages of files are read at once once the first page has
// said how many there are.
const filesParallel = 4

// lastPageLink finds the last page's link in a Link header, as GitHub sends it with
// every page of a list longer than one.
var lastPageLink = regexp.MustCompile(`<([^>]*)>;\s*rel="last"`)

// fileReadAttempts is how many times FetchChangedFiles reads the files before giving up
// on a pull request that keeps being pushed to.
const fileReadAttempts = 2

// ErrHeadMoved is returned when a pull request was pushed to each time its files were
// read, so no list can be tied to one commit.
var ErrHeadMoved = errors.New("the pull request was pushed to while its files were read; try again")

// PullHead is where a pull request stands as of now.
type PullHead struct {
	// Head is the head commit's SHA.
	Head string

	// ChangedFiles is how many files the pull request changes.
	ChangedFiles int

	// State is "open" or "closed".
	State string
}

// ChangedFiles is a pull request's changed files at one head commit.
type ChangedFiles struct {
	// Head is the head commit the list belongs to.
	Head string

	// Total is how many files the pull request changes, which can exceed len(Files).
	Total int

	// Files are the files GitHub listed: every one, or the first 3,000.
	Files []ChangedFile

	// Complete reports that Files holds every changed file.
	Complete bool
}

// ChangedFile is one file a pull request changes.
type ChangedFile struct {
	// Path is the file's path after the change.
	Path string

	// PreviousPath is a renamed or copied file's path before the change, or "".
	PreviousPath string

	// Status is GitHub's word for the change: added, removed, modified, renamed, copied,
	// changed, or unchanged.
	Status string
}

// FetchPullHead reads a pull request's head commit and size as of now, with one REST
// request.
func (c *Client) FetchPullHead(ctx context.Context, tok Token, ref item.Ref) (*PullHead, error) {
	head, err := c.fetchPullHead(ctx, tok, ref)

	return head, Redact(err, tok)
}

// FetchChangedFiles lists the files a pull request changes, with each renamed file's
// previous path, through the REST API: GitHub's GraphQL API does not report where a
// renamed file came from. The list is read between two readings of the head commit, so
// it belongs to one commit: when the head moved meanwhile, it is read again for the new
// head, and ErrHeadMoved is returned if the head moves again. head is the head commit
// already known, or "" to read it first.
//
// It costs one REST request per 100 files, plus one or two for the head.
func (c *Client) FetchChangedFiles(ctx context.Context, tok Token, ref item.Ref, head string) (*ChangedFiles, error) {
	files, err := c.fetchChangedFiles(ctx, tok, ref, head)

	return files, Redact(err, tok)
}

// fetchChangedFiles is FetchChangedFiles without the redaction.
func (c *Client) fetchChangedFiles(ctx context.Context, tok Token, ref item.Ref, head string) (*ChangedFiles, error) {
	if err := validateRef(ref); err != nil {
		return nil, err
	}

	for range fileReadAttempts {
		files, now, err := c.readFilesAt(ctx, tok, ref, head)
		if err != nil || files != nil {
			return files, err
		}

		head = now
	}

	return nil, ErrHeadMoved
}

// readFilesAt lists the files and reads the head again. When the head is still head
// (read first when it is ""), it returns the list; otherwise it returns no list and the
// head as it is now.
func (c *Client) readFilesAt(ctx context.Context, tok Token, ref item.Ref, head string) (*ChangedFiles, string, error) {
	if head == "" {
		before, err := c.fetchPullHead(ctx, tok, ref)
		if err != nil {
			return nil, "", err
		}

		head = before.Head
	}

	files, err := c.listFiles(ctx, tok, ref)
	if err != nil {
		return nil, "", err
	}

	after, err := c.fetchPullHead(ctx, tok, ref)
	if err != nil {
		return nil, "", err
	}

	if !strings.EqualFold(after.Head, head) {
		return nil, after.Head, nil
	}

	return &ChangedFiles{
		Head: after.Head, Total: after.ChangedFiles, Files: files, Complete: len(files) >= after.ChangedFiles,
	}, after.Head, nil
}

// fetchPullHead sends GET /repos/{owner}/{name}/pulls/{number}.
func (c *Client) fetchPullHead(ctx context.Context, tok Token, ref item.Ref) (*PullHead, error) {
	if err := validateRef(ref); err != nil {
		return nil, err
	}

	raw, _, err := c.do(ctx, tok, http.MethodGet, pullPath(ref), nil)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", ref, err)
	}

	var pull struct {
		State        string `json:"state"`
		ChangedFiles int    `json:"changed_files"`
		Head         struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}

	if err = json.Unmarshal(raw, &pull); err != nil {
		return nil, fmt.Errorf("%w: decode %s: %w", ErrMalformedResponse, ref, err)
	}

	if pull.Head.SHA == "" {
		return nil, fmt.Errorf("%w: %s has no head commit", ErrMalformedResponse, ref)
	}

	return &PullHead{Head: pull.Head.SHA, ChangedFiles: max(pull.ChangedFiles, 0), State: pull.State}, nil
}

// listFiles reads every page of GET /repos/{owner}/{name}/pulls/{number}/files, up to
// the 3,000 files GitHub lists. The first page's Link header says how many pages there
// are, and the rest are read a few at a time; without one, they are read in turn.
func (c *Client) listFiles(ctx context.Context, tok Token, ref item.Ref) ([]ChangedFile, error) {
	first, err := c.listFilesPage(ctx, tok, ref, 1)
	if err != nil || !first.full {
		return first.files, err
	}

	last := min(first.last, maxFilePages)
	if last < 2 {
		return c.listFilesInTurn(ctx, tok, ref, first.files)
	}

	pages := make([]filesPage, last+1)
	pages[1] = first

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(filesParallel)

	for page := 2; page <= last; page++ {
		g.Go(func() error {
			var pageErr error

			pages[page], pageErr = c.listFilesPage(gctx, tok, ref, page)

			return pageErr
		})
	}

	if err = g.Wait(); err != nil {
		return nil, err
	}

	var out []ChangedFile

	// The list ends at the first page that is not full, as it does when read in turn.
	for _, p := range pages[1:] {
		out = append(out, p.files...)

		if !p.full {
			break
		}
	}

	return out, nil
}

// listFilesInTurn reads the pages after the first one at a time, for a list whose first
// page did not say how many there are, until one is not full.
func (c *Client) listFilesInTurn(ctx context.Context, tok Token, ref item.Ref, out []ChangedFile) ([]ChangedFile, error) {
	for page := 2; page <= maxFilePages; page++ {
		p, err := c.listFilesPage(ctx, tok, ref, page)
		if err != nil {
			return nil, err
		}

		out = append(out, p.files...)

		if !p.full {
			break
		}
	}

	return out, nil
}

// filesPage is one page of a pull request's files: the files, whether the page was full,
// so another may follow, and the number of the last page, or 0 when GitHub did not say.
type filesPage struct {
	files []ChangedFile
	full  bool
	last  int
}

// listFilesPage reads one page of a pull request's files.
func (c *Client) listFilesPage(ctx context.Context, tok Token, ref item.Ref, page int) (filesPage, error) {
	query := url.Values{"per_page": {strconv.Itoa(filesPerPage)}, "page": {strconv.Itoa(page)}}

	raw, header, err := c.do(ctx, tok, http.MethodGet, pullPath(ref)+"/files?"+query.Encode(), nil)
	if err != nil {
		return filesPage{}, fmt.Errorf("list the files of %s: %w", ref, err)
	}

	var batch []struct {
		Filename         string `json:"filename"`
		Status           string `json:"status"`
		PreviousFilename string `json:"previous_filename"`
	}

	if err = json.Unmarshal(raw, &batch); err != nil {
		return filesPage{}, fmt.Errorf("%w: decode the files of %s: %w", ErrMalformedResponse, ref, err)
	}

	files := make([]ChangedFile, 0, len(batch))

	for _, f := range batch {
		if f.Filename != "" {
			files = append(files, ChangedFile{Path: f.Filename, PreviousPath: f.PreviousFilename, Status: f.Status})
		}
	}

	return filesPage{files: files, full: len(batch) == filesPerPage, last: lastPage(header)}, nil
}

// lastPage reads the number of the last page from a Link header, or returns 0 when it
// names none.
func lastPage(header http.Header) int {
	m := lastPageLink.FindStringSubmatch(header.Get("Link"))
	if m == nil {
		return 0
	}

	link, err := url.Parse(m[1])
	if err != nil {
		return 0
	}

	n, err := strconv.Atoi(link.Query().Get("page"))
	if err != nil || n < 1 {
		return 0
	}

	return n
}

// pullPath is a pull request's REST path, with its owner and name escaped.
func pullPath(ref item.Ref) string {
	return "/repos/" + url.PathEscape(ref.Repo.Owner) + "/" + url.PathEscape(ref.Repo.Repo) + "/pulls/" +
		strconv.Itoa(ref.Number)
}

// validateRef rejects a reference that names no pull request: a missing owner or name, or
// a number that is not positive.
func validateRef(ref item.Ref) error {
	if ref.Repo.Owner == "" || ref.Repo.Repo == "" || ref.Number <= 0 {
		return fmt.Errorf("%w: %q/%q#%d", ErrInvalidItemRef, ref.Repo.Owner, ref.Repo.Repo, ref.Number)
	}

	return nil
}
