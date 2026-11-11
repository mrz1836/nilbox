package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/repo"
)

const (
	// pushEvent is the event of a workflow run a push started. It names a GitHub Actions
	// event, read from GitHub, never a git command, so it is written as a raw string:
	// TestNoGitCommandChangesAClone looks for git's verbs as quoted words.
	pushEvent = `push`

	// tagRunsMax is how many runs the tag's runs request lists: a tag's push starts one run
	// per workflow that runs on tags.
	tagRunsMax = 100
)

// The states of a workflow run, as GitHub's REST API names them.
const (
	runCompleted = "completed"

	conclusionFailure        = "failure"
	conclusionTimedOut       = "timed_out"
	conclusionStartupFailure = "startup_failure"
)

// releaseProgressQuery reads the release GitHub has for a tag, in one query of one point.
const releaseProgressQuery = `query ReleaseProgress($owner: String!, $name: String!, $tag: String!) {
  repository(owner: $owner, name: $name) {
    release(tagName: $tag) { url isDraft publishedAt }
  }
  rateLimit { cost }
}
`

var (
	// ErrInvalidProgressRequest is returned for a ProgressRequest without a repository, a
	// tag, or a commit's full SHA; it is never sent.
	ErrInvalidProgressRequest = errors.New("invalid release progress request")

	// ErrRepositoryNotFound is returned when GitHub can't find the repository asked about,
	// or the token can't see it.
	ErrRepositoryNotFound = errors.New("GitHub couldn't find the repository")
)

// ProgressRequest asks how a release's workflow goes.
type ProgressRequest struct {
	Repo repo.Name

	// Tag is the release's tag, as in v1.4.3, and Commit the full SHA of the commit it
	// tags.
	Tag    string
	Commit string
}

// valid reports whether r can be sent: a repository, a tag, and a commit's full SHA, in
// hex, of SHA-1's length or SHA-256's.
func (r ProgressRequest) valid() bool {
	return r.Repo.Valid() && r.Tag != "" && (len(r.Commit) == 40 || len(r.Commit) == 64) && isHex(r.Commit)
}

// isHex reports whether s holds only hex digits.
func isHex(s string) bool {
	for _, c := range s {
		switch {
		case '0' <= c && c <= '9', 'a' <= c && c <= 'f', 'A' <= c && c <= 'F':
		default:
			return false
		}
	}

	return true
}

// ReleaseProgress is how a release's workflow on GitHub goes, as of now.
type ReleaseProgress struct {
	// Release is the release GitHub has for the tag, or nil while it has none, as before
	// the workflow drafts it.
	Release *TagRelease

	// Runs are the tag's own workflow runs: those a push of the tag started at its commit,
	// newest first. They are left unread, and empty, once the release is published. Never
	// nil.
	Runs []WorkflowRun
}

// TagRelease is the release GitHub has for a tag.
type TagRelease struct {
	// URL is its page on GitHub.
	URL string

	// Draft reports a draft, which no one but the repository's writers sees.
	Draft bool

	// PublishedAt is when it was published, or zero while it isn't.
	PublishedAt time.Time
}

// WorkflowRun is one workflow run.
type WorkflowRun struct {
	// Name is the workflow's name, as goreleaser.
	Name string

	// Status is where it is: queued, in_progress, completed, and the rest of GitHub's
	// states. Conclusion is how it ended (success, failure, and the rest), or "" until it
	// has.
	Status     string
	Conclusion string

	// URL is its page on GitHub.
	URL string
}

// Published reports the release published: GitHub has it, it isn't a draft, and it has a
// time it was published.
func (p ReleaseProgress) Published() bool {
	return p.Release != nil && !p.Release.Draft && !p.Release.PublishedAt.IsZero()
}

// Completed reports a run that has ended.
func (r WorkflowRun) Completed() bool {
	return r.Status == runCompleted
}

// Failed reports a run that concluded in failure, timed out, or never started. One
// canceled isn't a failure: it may be run again.
func (r WorkflowRun) Failed() bool {
	return slices.Contains([]string{conclusionFailure, conclusionTimedOut, conclusionStartupFailure}, r.Conclusion)
}

// FetchReleaseProgress reads how a release's workflow on GitHub goes, as of now: the
// release GitHub has for the tag, with one GraphQL query of one point, and, unless that
// release is published, the tag's own workflow runs, with one REST request (GET
// /repos/{owner}/{name}/actions/runs, by the push event, the tag as the run's branch, and
// the tag's commit), which P0 found names a tag push's runs exactly. The budget counts
// both, like every other request. A repository GitHub can't find fails with
// ErrRepositoryNotFound.
func (c *Client) FetchReleaseProgress(ctx context.Context, tok Token, req ProgressRequest) (*ReleaseProgress, error) {
	progress, err := c.fetchReleaseProgress(ctx, tok, req)

	return progress, Redact(err, tok)
}

// fetchReleaseProgress reads the release, then the tag's runs while it isn't published.
func (c *Client) fetchReleaseProgress(ctx context.Context, tok Token, req ProgressRequest) (*ReleaseProgress, error) {
	if !req.valid() {
		return nil, fmt.Errorf("%w: %q %q %q", ErrInvalidProgressRequest, req.Repo.String(), req.Tag, req.Commit)
	}

	release, err := c.fetchTagRelease(ctx, tok, req)
	if err != nil {
		return nil, err
	}

	out := &ReleaseProgress{Release: release, Runs: []WorkflowRun{}}
	if out.Published() {
		return out, nil
	}

	if out.Runs, err = c.fetchTagRuns(ctx, tok, req); err != nil {
		return nil, err
	}

	return out, nil
}

// fetchTagRelease sends the progress query: the release GitHub has for the tag, or nil.
func (c *Client) fetchTagRelease(ctx context.Context, tok Token, req ProgressRequest) (*TagRelease, error) {
	body := graphQLRequest{Query: releaseProgressQuery, Variables: map[string]any{
		"owner": req.Repo.Owner, "name": req.Repo.Repo, "tag": req.Tag,
	}}

	resp, _, err := postGraphQL[progressData](ctx, c, tok, "release progress", body, unlessData)
	if err != nil {
		return nil, err
	}

	repository := resp.Data.Repository
	if repository == nil {
		if len(resp.Errors) > 0 {
			return nil, fmt.Errorf("%w %s: %w", ErrRepositoryNotFound, req.Repo.String(), graphQLErr(resp.Errors))
		}

		return nil, fmt.Errorf("%w %s", ErrRepositoryNotFound, req.Repo.String())
	}

	rel := repository.Release
	if rel == nil {
		return nil, nil //nolint:nilnil // no release yet is an answer, not an error
	}

	out := &TagRelease{URL: rel.URL, Draft: rel.IsDraft}
	if rel.PublishedAt != nil {
		out.PublishedAt = rel.PublishedAt.UTC()
	}

	return out, nil
}

// fetchTagRuns lists the tag's own workflow runs: those whose event is push, whose branch,
// as GitHub names a tag push's, is the tag, and whose commit is the tag's. GitHub filters
// by all three; the answer is checked again, so a run for any other branch, tag, or commit
// never counts.
func (c *Client) fetchTagRuns(ctx context.Context, tok Token, req ProgressRequest) ([]WorkflowRun, error) {
	query := url.Values{}
	query.Set("event", pushEvent)
	query.Set("branch", req.Tag)
	query.Set("head_sha", req.Commit)
	query.Set("exclude_pull_requests", "true")
	query.Set("per_page", strconv.Itoa(tagRunsMax))

	path := "/repos/" + url.PathEscape(req.Repo.Owner) + "/" + url.PathEscape(req.Repo.Repo) + "/actions/runs?" +
		query.Encode()

	raw, _, err := c.do(ctx, tok, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("read the workflow runs of %s %s: %w", req.Repo.String(), req.Tag, err)
	}

	var body tagRunsData
	if err = json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("%w: decode the workflow runs: %w", ErrMalformedResponse, err)
	}

	out := []WorkflowRun{}

	for _, r := range body.WorkflowRuns {
		if r.Event != pushEvent || r.HeadBranch != req.Tag || !strings.EqualFold(r.HeadSHA, req.Commit) {
			continue
		}

		run := WorkflowRun{Name: r.Name, Status: r.Status, URL: r.HTMLURL}
		if r.Conclusion != nil {
			run.Conclusion = *r.Conclusion
		}

		out = append(out, run)
	}

	return out, nil
}

// progressData is the progress query's data object.
type progressData struct {
	Repository *struct {
		Release *struct {
			URL         string     `json:"url"`
			IsDraft     bool       `json:"isDraft"`
			PublishedAt *time.Time `json:"publishedAt"`
		} `json:"release"`
	} `json:"repository"`
}

// tagRunsData is the workflow runs request's answer.
type tagRunsData struct {
	WorkflowRuns []struct {
		Name       string  `json:"name"`
		HeadBranch string  `json:"head_branch"`
		HeadSHA    string  `json:"head_sha"`
		Event      string  `json:"event"`
		Status     string  `json:"status"`
		Conclusion *string `json:"conclusion"`
		HTMLURL    string  `json:"html_url"`
	} `json:"workflow_runs"`
}
