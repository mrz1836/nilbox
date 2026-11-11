package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/mrz1836/nilbox/internal/repo"
)

// What tells a release's own workflow run apart from the rest of the check suites on the
// tag's commit, as P0 found them on real releases: the commit also carries other apps'
// suites that stay QUEUED forever, schedule and dynamic runs, and the branch's own push
// run, none of which say anything about the release.
const (
	// actionsApp is GitHub Actions' app slug: only its suites are workflow runs.
	actionsApp = "github-actions"

	// pushEvent is the event of a run a push started, the tag's or the branch's. It names a
	// GitHub Actions event, read from GitHub, never a git command, so it is written as a raw
	// string: TestNoGitCommandChangesAClone looks for git's verbs as quoted words.
	pushEvent = `push`
)

// The states of a check suite, as GitHub's CheckStatusState and CheckConclusionState name
// them.
const (
	suiteCompleted = "COMPLETED"

	conclusionFailure        = "FAILURE"
	conclusionTimedOut       = "TIMED_OUT"
	conclusionStartupFailure = "STARTUP_FAILURE"
)

// releaseProgressQuery reads how a release's workflow goes, in one query of one point:
// the release GitHub has for the tag, if any, and the newest check suites on the tag's
// commit, each with its app, its state, the branch GitHub ran it for, and its workflow
// run's page and event. The commit is the one the release verified origin's tag points
// at.
const releaseProgressQuery = `query ReleaseProgress($owner: String!, $name: String!, $tag: String!, $oid: GitObjectID!) {
  repository(owner: $owner, name: $name) {
    release(tagName: $tag) { url isDraft publishedAt }
    object(oid: $oid) {
      ... on Commit {
        checkSuites(last: 50) {
          nodes {
            app { slug name }
            status conclusion
            branch { name }
            workflowRun { url event workflow { name } }
          }
        }
      }
    }
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

	// Suites are the newest check suites on the tag's commit, at most 50, oldest first.
	// Never nil.
	Suites []CheckSuite
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

// CheckSuite is one check suite on a commit: one app's checks, or one workflow run.
type CheckSuite struct {
	// App is the app's slug, as github-actions or codecov, and AppName its name; both ""
	// when GitHub doesn't say.
	App     string
	AppName string

	// Status is where it is: QUEUED, IN_PROGRESS, COMPLETED, and the rest of GitHub's
	// CheckStatusState. Conclusion is how it ended (SUCCESS, FAILURE, and the rest of its
	// CheckConclusionState), or "" until it has.
	Status     string
	Conclusion string

	// Branch is the branch GitHub ran it for, or "" for none, as for a run a tag's push
	// started.
	Branch string

	// Event is what started its workflow run (push, schedule, dynamic, ...), RunURL the
	// run's page, and Workflow the workflow's name; all "" for a suite that is no workflow
	// run.
	Event    string
	RunURL   string
	Workflow string
}

// Published reports the release published: GitHub has it, it isn't a draft, and it has a
// time it was published.
func (p ReleaseProgress) Published() bool {
	return p.Release != nil && !p.Release.Draft && !p.Release.PublishedAt.IsZero()
}

// Failed returns the release's own workflow run that failed, and reports whether one did:
// the tag's own push runs (TagRun) have all completed, and one of them concluded in
// failure, timed out, or never started. While one of them still goes, the release may
// still be published. Every other suite on the commit is left out: other apps' (which
// may stay queued forever), schedule and dynamic runs, and the branch's own push run.
func (p ReleaseProgress) Failed(tag string) (CheckSuite, bool) {
	var failed *CheckSuite

	for i, s := range p.Suites {
		if !s.TagRun(tag) {
			continue
		}

		if s.Status != suiteCompleted {
			return CheckSuite{}, false
		}

		if failed == nil && s.failure() {
			failed = &p.Suites[i]
		}
	}

	if failed == nil {
		return CheckSuite{}, false
	}

	return *failed, true
}

// TagRun reports whether s is one of tag's own push runs: a GitHub Actions run a push
// started, for no branch, or for the tag itself, as GitHub names a tag's push. A run for a
// branch is the branch's own push of the commit.
func (s CheckSuite) TagRun(tag string) bool {
	return s.App == actionsApp && s.Event == pushEvent && (s.Branch == "" || s.Branch == tag)
}

// failure reports a suite that concluded in failure, timed out, or never started. One
// canceled isn't a failure: it may be run again.
func (s CheckSuite) failure() bool {
	return slices.Contains([]string{conclusionFailure, conclusionTimedOut, conclusionStartupFailure}, s.Conclusion)
}

// FetchReleaseProgress reads how a release's workflow on GitHub goes, as of now, with one
// GraphQL query of one point, which the budget counts like every other: the release
// GitHub has for the tag, and the newest check suites on the tag's commit. A repository
// GitHub can't find fails with ErrRepositoryNotFound.
func (c *Client) FetchReleaseProgress(ctx context.Context, tok Token, req ProgressRequest) (*ReleaseProgress, error) {
	progress, err := c.fetchReleaseProgress(ctx, tok, req)

	return progress, Redact(err, tok)
}

// fetchReleaseProgress sends the progress query and decodes the answer.
func (c *Client) fetchReleaseProgress(ctx context.Context, tok Token, req ProgressRequest) (*ReleaseProgress, error) {
	if !req.valid() {
		return nil, fmt.Errorf("%w: %q %q %q", ErrInvalidProgressRequest, req.Repo.String(), req.Tag, req.Commit)
	}

	body := graphQLRequest{Query: releaseProgressQuery, Variables: map[string]any{
		"owner": req.Repo.Owner, "name": req.Repo.Repo, "tag": req.Tag, "oid": req.Commit,
	}}

	resp, _, err := postGraphQL[progressData](ctx, c, tok, "release progress", body, unlessData)
	if err != nil {
		return nil, err
	}

	if resp.Data.Repository == nil {
		if len(resp.Errors) > 0 {
			return nil, fmt.Errorf("%w %s: %w", ErrRepositoryNotFound, req.Repo.String(), graphQLErr(resp.Errors))
		}

		return nil, fmt.Errorf("%w %s", ErrRepositoryNotFound, req.Repo.String())
	}

	return resp.Data.Repository.progress(), nil
}

// progressData is the progress query's data object.
type progressData struct {
	Repository *progressRepo `json:"repository"`
}

// progressRepo is the repository's answer: its release for the tag, null while it has
// none, and the tag's commit, null when GitHub doesn't have it.
type progressRepo struct {
	Release *struct {
		URL         string     `json:"url"`
		IsDraft     bool       `json:"isDraft"`
		PublishedAt *time.Time `json:"publishedAt"`
	} `json:"release"`
	Object json.RawMessage `json:"object"`
}

// progressCommit is the tag's commit, with its check suites.
type progressCommit struct {
	CheckSuites *struct {
		Nodes []*progressSuite `json:"nodes"`
	} `json:"checkSuites"`
}

// progressSuite is one check suite on the tag's commit.
type progressSuite struct {
	App *struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	} `json:"app"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	Branch     *struct {
		Name string `json:"name"`
	} `json:"branch"`
	WorkflowRun *struct {
		URL      string `json:"url"`
		Event    string `json:"event"`
		Workflow *struct {
			Name string `json:"name"`
		} `json:"workflow"`
	} `json:"workflowRun"`
}

// progress converts the repository's answer. A commit that can't be read, or GitHub
// doesn't have, holds no suites.
func (r *progressRepo) progress() *ReleaseProgress {
	out := &ReleaseProgress{Suites: []CheckSuite{}}

	if rel := r.Release; rel != nil {
		out.Release = &TagRelease{URL: rel.URL, Draft: rel.IsDraft}
		if rel.PublishedAt != nil {
			out.Release.PublishedAt = rel.PublishedAt.UTC()
		}
	}

	var commit progressCommit
	if len(r.Object) == 0 || json.Unmarshal(r.Object, &commit) != nil || commit.CheckSuites == nil {
		return out
	}

	for _, n := range commit.CheckSuites.Nodes {
		if n != nil {
			out.Suites = append(out.Suites, n.suite())
		}
	}

	return out
}

// suite converts one check suite.
func (n *progressSuite) suite() CheckSuite {
	s := CheckSuite{Status: n.Status, Conclusion: n.Conclusion}

	if n.App != nil {
		s.App, s.AppName = n.App.Slug, n.App.Name
	}

	if n.Branch != nil {
		s.Branch = n.Branch.Name
	}

	if run := n.WorkflowRun; run != nil {
		s.Event, s.RunURL = run.Event, run.URL
		if run.Workflow != nil {
			s.Workflow = run.Workflow.Name
		}
	}

	return s
}
