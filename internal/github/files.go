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

// PullDiff is a pull request's changed files, with their diffs, at one head commit.
type PullDiff struct {
	// Head is the head commit the files belong to.
	Head string

	// Total is how many files the pull request changes, which can exceed len(Files).
	Total int

	// Files are the files GitHub listed: every one, or the first 3,000.
	Files []PullFile

	// Complete reports that Files holds every changed file.
	Complete bool
}

// PullFile is one file a pull request changes, with its diff.
type PullFile struct {
	// Path is the file's path after the change, and PreviousPath a renamed or copied
	// file's path before it, or "".
	Path         string
	PreviousPath string

	// Status is GitHub's word for the change, as ChangedFile's.
	Status string

	// BlobSHA is the file's blob as GitHub lists it.
	BlobSHA string

	// Patch is the file's diff as GitHub shows it, without the file headers. HasPatch is
	// false when GitHub shows none: for a binary file, and one too large to show.
	Patch    string
	HasPatch bool

	Additions int
	Deletions int
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

// FetchPullDiff lists the files a pull request changes with their diffs, as GitHub's
// REST API shows them, at one head commit: head, the head already known (read first when
// it is ""). Each listed file names the commit it was read at, so a list whose files all
// name head, and that holds every file, costs no second reading of the head; otherwise the
// head is read after the list, as FetchChangedFiles does, and a list read while the pull
// request was pushed to is read again for the new head. ErrHeadMoved is returned when the
// head moves again.
//
// It costs one REST request per 100 files, and one more when the head must be read.
func (c *Client) FetchPullDiff(ctx context.Context, tok Token, ref item.Ref, head string) (*PullDiff, error) {
	diff, err := c.fetchPullDiff(ctx, tok, ref, head)

	return diff, Redact(err, tok)
}

// fetchPullDiff is FetchPullDiff without the redaction.
func (c *Client) fetchPullDiff(ctx context.Context, tok Token, ref item.Ref, head string) (*PullDiff, error) {
	if err := validateRef(ref); err != nil {
		return nil, err
	}

	for range fileReadAttempts {
		diff, now, err := c.readDiffAt(ctx, tok, ref, head)
		if err != nil || diff != nil {
			return diff, err
		}

		head = now
	}

	return nil, ErrHeadMoved
}

// readDiffAt lists the files with their diffs at head (read first when it is ""). When
// the list belongs to head, it returns it; otherwise it returns no list and the head as it
// is now.
func (c *Client) readDiffAt(ctx context.Context, tok Token, ref item.Ref, head string) (*PullDiff, string, error) {
	if head == "" {
		before, err := c.fetchPullHead(ctx, tok, ref)
		if err != nil {
			return nil, "", err
		}

		head = before.Head
	}

	listed, err := listFiles(ctx, c, tok, ref, pullFile)
	if err != nil {
		return nil, "", err
	}

	files := make([]PullFile, 0, len(listed))
	for _, f := range listed {
		files = append(files, f.file)
	}

	if at, ok := listedAt(listed); ok && strings.EqualFold(at, head) && len(files) < maxListedFiles {
		return &PullDiff{Head: head, Total: len(files), Files: files, Complete: true}, head, nil
	}

	after, err := c.fetchPullHead(ctx, tok, ref)
	if err != nil {
		return nil, "", err
	}

	if !strings.EqualFold(after.Head, head) {
		return nil, after.Head, nil
	}

	return &PullDiff{
		Head: after.Head, Total: after.ChangedFiles, Files: files, Complete: len(files) >= after.ChangedFiles,
	}, after.Head, nil
}

// listedFile is a listed file with its diff, and the head commit GitHub listed it at ("",
// when the entry does not say, as for a removed file).
type listedFile struct {
	file PullFile
	at   string
}

// pullFile keeps a listed file with its diff, and the commit its contents link names.
func pullFile(f restFile) (listedFile, bool) {
	if f.Filename == "" {
		return listedFile{}, false
	}

	file := PullFile{
		Path: f.Filename, PreviousPath: f.PreviousFilename, Status: f.Status, BlobSHA: f.SHA,
		Additions: max(f.Additions, 0), Deletions: max(f.Deletions, 0),
	}

	if f.Patch != nil {
		file.Patch, file.HasPatch = *f.Patch, true
	}

	at := ""
	if f.Status != "removed" {
		at = contentsRef(f.ContentsURL)
	}

	return listedFile{file: file, at: at}, true
}

// contentsRef reads the commit a file's contents link names, as in
// ".../contents/a.go?ref=<sha>", or "" when it names none.
func contentsRef(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}

	return u.Query().Get("ref")
}

// listedAt returns the one commit the listed files name, and whether they all name the
// same one; ok is false when none names a commit, or two name different ones.
func listedAt(files []listedFile) (string, bool) {
	at := ""

	for _, f := range files {
		switch {
		case f.at == "":
		case at == "":
			at = f.at
		case !strings.EqualFold(at, f.at):
			return "", false
		}
	}

	return at, at != ""
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

	files, err := listFiles(ctx, c, tok, ref, changedFile)
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

// restFile is one entry of GET /repos/{owner}/{name}/pulls/{number}/files, as GitHub
// sends it. Patch is the file's diff, which GitHub leaves out (null or missing) for a
// binary file and one too large to show.
type restFile struct {
	Filename         string  `json:"filename"`
	Status           string  `json:"status"`
	PreviousFilename string  `json:"previous_filename"`
	SHA              string  `json:"sha"`
	Additions        int     `json:"additions"`
	Deletions        int     `json:"deletions"`
	Patch            *string `json:"patch"`
	ContentsURL      string  `json:"contents_url"`
}

// fileConv turns one listed file into what a caller keeps of it; ok is false for an entry
// to skip, such as one without a name. Each page is converted as it is read, so what a
// caller does not keep, such as the patches, is dropped page by page.
type fileConv[F any] func(f restFile) (F, bool)

// changedFile keeps a listed file's paths and status.
func changedFile(f restFile) (ChangedFile, bool) {
	if f.Filename == "" {
		return ChangedFile{}, false
	}

	return ChangedFile{Path: f.Filename, PreviousPath: f.PreviousFilename, Status: f.Status}, true
}

// listFiles reads every page of GET /repos/{owner}/{name}/pulls/{number}/files, up to
// the 3,000 files GitHub lists, converting each file with conv. The first page's Link
// header says how many pages there are, and the rest are read a few at a time; without
// one, they are read in turn. Go methods cannot take type parameters, so the paging is
// a set of functions over the client.
func listFiles[F any](ctx context.Context, c *Client, tok Token, ref item.Ref, conv fileConv[F]) ([]F, error) {
	first, err := listFilesPage(ctx, c, tok, ref, 1, conv)
	if err != nil || !first.full {
		return first.files, err
	}

	last := min(first.last, maxFilePages)
	if last < 2 {
		return listFilesInTurn(ctx, c, tok, ref, first.files, conv)
	}

	pages := make([]filesPage[F], last+1)
	pages[1] = first

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(filesParallel)

	for page := 2; page <= last; page++ {
		g.Go(func() error {
			var pageErr error

			pages[page], pageErr = listFilesPage(gctx, c, tok, ref, page, conv)

			return pageErr
		})
	}

	if err = g.Wait(); err != nil {
		return nil, err
	}

	var out []F

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
func listFilesInTurn[F any](
	ctx context.Context, c *Client, tok Token, ref item.Ref, out []F, conv fileConv[F],
) ([]F, error) {
	for page := 2; page <= maxFilePages; page++ {
		p, err := listFilesPage(ctx, c, tok, ref, page, conv)
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
type filesPage[F any] struct {
	files []F
	full  bool
	last  int
}

// listFilesPage reads one page of a pull request's files.
func listFilesPage[F any](
	ctx context.Context, c *Client, tok Token, ref item.Ref, page int, conv fileConv[F],
) (filesPage[F], error) {
	query := url.Values{"per_page": {strconv.Itoa(filesPerPage)}, "page": {strconv.Itoa(page)}}

	raw, header, err := c.do(ctx, tok, http.MethodGet, pullPath(ref)+"/files?"+query.Encode(), nil)
	if err != nil {
		return filesPage[F]{}, fmt.Errorf("list the files of %s: %w", ref, err)
	}

	files, listed, err := decodeFiles(raw, conv)
	if err != nil {
		return filesPage[F]{}, fmt.Errorf("%w: decode the files of %s: %w", ErrMalformedResponse, ref, err)
	}

	return filesPage[F]{files: files, full: listed == filesPerPage, last: lastPage(header)}, nil
}

// decodeFiles decodes one page of files and converts each with conv. It returns what conv
// kept and how many entries the page listed, which says whether it was full.
func decodeFiles[F any](raw []byte, conv fileConv[F]) ([]F, int, error) {
	var batch []restFile

	if err := json.Unmarshal(raw, &batch); err != nil {
		return nil, 0, err
	}

	files := make([]F, 0, len(batch))

	for _, f := range batch {
		if kept, ok := conv(f); ok {
			files = append(files, kept)
		}
	}

	return files, len(batch), nil
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
