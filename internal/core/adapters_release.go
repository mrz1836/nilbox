package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/launcher/terminal"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/notify"
	prefsstore "github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/release"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/reporules"
	"github.com/mrz1836/nilbox/internal/settings"
)

// releaseRules reads the pinned and muted repositories and orgs for the Release page, in
// one read of the prefs (release.RuleSource).
type releaseRules struct {
	prefs *prefsstore.Store
}

// RepoRules returns the rules as stored.
func (r releaseRules) RepoRules(ctx context.Context) (reporules.Rules, error) {
	stored, err := r.prefs.GetMany(ctx, reporules.Keys()...)
	if err != nil {
		return reporules.Rules{}, fmt.Errorf("read the pinned and muted repositories: %w", err)
	}

	return reporules.Decode(stored), nil
}

// releaseClones lists the clones in the repository locations the settings name, for the
// Release page, with each one's origin when origin is a GitHub repository
// (release.Clones).
type releaseClones struct {
	settings *settings.Service
	home     string
	git      execpath.Runner
	reader   *localrepos.Reader
}

// Roots returns the clone root and the other locations as they are set now.
func (c releaseClones) Roots(ctx context.Context) (localrepos.Roots, error) {
	all, err := c.settings.Get(ctx)
	if err != nil {
		return localrepos.Roots{}, err
	}

	return rootsOf(all, c.home, c.reader)
}

// Clones lists the clones in roots and reads their remotes.
func (c releaseClones) Clones(ctx context.Context, roots localrepos.Roots) ([]release.Clone, error) {
	listed, err := roots.Clones(ctx)
	if err != nil {
		return nil, fmt.Errorf("list the clones: %w", err)
	}

	found, err := localrepos.Scan(ctx, listed.Dirs, c.git)
	if err != nil {
		return nil, fmt.Errorf("read the clones' remotes: %w", err)
	}

	out := make([]release.Clone, 0, len(found))

	for _, r := range found {
		origin, onHub := r.Remotes["origin"]
		out = append(out, release.Clone{Dir: r.Path, Origin: origin, OnHub: onHub})
	}

	return out, nil
}

// releaseGitHub asks GitHub what the Release page needs, borrowing the token as the other
// reads do (release.GitHub). With no token at all, as when signed out, nothing is asked,
// and the error is release.ErrSignedOut.
type releaseGitHub struct {
	tokens github.Tokens
	client *github.Client
}

// ReleaseStatus is round one: each repository's state, default branch, and newest tags.
// answered hears each batch GitHub answers.
func (r releaseGitHub) ReleaseStatus(
	ctx context.Context, repos []repo.Name, answered func(n int),
) (map[string]release.RepoStatus, error) {
	got, err := github.WithToken(ctx, r.tokens, func(tok github.Token) (map[string]github.ReleaseStatus, error) {
		return r.client.FetchReleaseStatus(ctx, tok, repos, answered)
	})
	if err != nil {
		return nil, releaseSignedOut(err)
	}

	out := make(map[string]release.RepoStatus, len(got))

	for key, st := range got {
		tags := make([]release.Tag, 0, len(st.Tags))
		for _, t := range st.Tags {
			tags = append(tags, release.Tag{Name: t.Name, Commit: t.CommitOID})
		}

		out[key] = release.RepoStatus{
			Archived: st.Archived, Permission: st.ViewerPermission, DefaultBranch: st.DefaultBranch,
			HeadOID: st.Head.OID, HeadAt: st.Head.CommittedAt, Headline: st.Head.Headline,
			Checks: release.Checks(st.HeadChecks),
			Tags:   tags,
		}
	}

	return out, nil
}

// ReleaseCompare is round two: how far each default branch is ahead of its version tag.
// answered hears each batch GitHub answers.
func (r releaseGitHub) ReleaseCompare(
	ctx context.Context, reqs []release.CompareRequest, answered func(n int),
) (map[string]release.Compare, error) {
	asks := make([]github.CompareRequest, 0, len(reqs))
	for _, q := range reqs {
		asks = append(asks, github.CompareRequest{Repo: q.Repo, Tag: q.Tag, Branch: q.Branch})
	}

	got, err := github.WithToken(ctx, r.tokens, func(tok github.Token) (map[string]github.ReleaseCompare, error) {
		return r.client.FetchReleaseCompare(ctx, tok, asks, answered)
	})
	if err != nil {
		return nil, releaseSignedOut(err)
	}

	out := make(map[string]release.Compare, len(got))

	for key, c := range got {
		commits := make([]release.Commit, 0, len(c.Commits))
		for _, cm := range c.Commits {
			commits = append(commits, release.Commit{OID: cm.OID, Headline: cm.Headline, At: cm.CommittedAt})
		}

		out[key] = release.Compare{Ahead: c.AheadBy, Commits: commits}
	}

	return out, nil
}

// ReleaseWorkflow reads how a release's workflow on GitHub goes, through the shared client,
// so the budget counts it (release.Workflows): whether GitHub published the release, with
// one query, and, until it has, the tag's own workflow runs, with one REST request.
func (r releaseGitHub) ReleaseWorkflow(ctx context.Context, name repo.Name, tag, commit string) (release.Workflow, error) {
	got, err := github.WithToken(ctx, r.tokens, func(tok github.Token) (*github.ReleaseProgress, error) {
		return r.client.FetchReleaseProgress(ctx, tok, github.ProgressRequest{Repo: name, Tag: tag, Commit: commit})
	})
	if err != nil {
		return release.Workflow{}, releaseSignedOut(err)
	}

	out := release.Workflow{Published: got.Published(), Runs: make([]release.TagRun, 0, len(got.Runs))}
	if got.Release != nil {
		out.ReleaseURL = got.Release.URL
	}

	for _, run := range got.Runs {
		out.Runs = append(out.Runs, release.TagRun{
			Workflow: run.Name, URL: run.URL, Done: run.Completed(), Failed: run.Failed(),
		})
	}

	return out, nil
}

// releaseOrigins reads which GitHub repository a clone's origin is, with the read-only
// remote lookup localrepos.Inspect runs (release.OriginReader).
type releaseOrigins struct {
	git execpath.Runner
}

// Origin returns the repository the clone at dir's origin points at, and whether it is a
// GitHub repository.
func (o releaseOrigins) Origin(ctx context.Context, dir string) (repo.Name, bool, error) {
	found, err := localrepos.Inspect(ctx, dir, o.git)
	if err != nil {
		return repo.Name{}, false, err
	}

	origin, onGitHub := found.Remotes["origin"]

	return origin, onGitHub, nil
}

// releaseTool reads what the integrations know of magex, the copy in use and its
// definition's features, for a release (release.ToolSource).
type releaseTool struct {
	tools *integrations.Service
}

// ReleaseTool returns magex's facts; a list that can't be read is a magex that's missing.
func (r releaseTool) ReleaseTool(ctx context.Context) release.ToolFacts {
	def := integrations.MageX()

	return release.ToolFactsOf(def, toolByID(toolList(ctx, r.tools), def.ID))
}

// releaseSettings reads what Release needs of the settings: where runs go, Runs in and the
// terminal chosen for launches, by name (release.Places), and whether a merge offers a
// release (release.MergeSetting).
type releaseSettings struct {
	settings *settings.Service
}

// Where returns where runs go.
func (p releaseSettings) Where(ctx context.Context) (release.Where, error) {
	return settingsPart(ctx, p.settings, func(s settings.Settings) release.Where {
		return release.Where{RunsIn: s.Integrations.RunsIn, Terminal: terminal.Name(s.Terminal)}
	})
}

// OfferAfterMerge returns the integrations.offerAfterMerge setting.
func (p releaseSettings) OfferAfterMerge(ctx context.Context) (bool, error) {
	return settingsPart(ctx, p.settings, func(s settings.Settings) bool { return s.Integrations.OfferAfterMerge })
}

// releaseNotices tells the user of a release with a notification about no item, and its
// banner when that kind is on (release.Notifier): its tag reached GitHub (release_done),
// its workflow published it (release_published), or its workflow failed (release_failed).
// Each opens its page.
type releaseNotices struct {
	dispatcher *notify.Dispatcher
}

// Released records a release that reached GitHub, and shows its banner.
func (n releaseNotices) Released(ctx context.Context, notice release.Notice) {
	n.notify(ctx, notify.Entry{Kind: notify.KindReleaseDone}, notice)
}

// Published records a release its workflow published, and shows its banner.
func (n releaseNotices) Published(ctx context.Context, notice release.Notice) {
	n.notify(ctx, notify.Entry{Kind: notify.KindReleasePublished}, notice)
}

// WorkflowFailed records a release whose workflow failed, and shows its banner.
func (n releaseNotices) WorkflowFailed(ctx context.Context, notice release.Notice) {
	n.notify(ctx, notify.Entry{Kind: notify.KindReleaseFailed}, notice)
}

// notify records notice as a notification of entry's kind, and shows its banner.
func (n releaseNotices) notify(ctx context.Context, entry notify.Entry, notice release.Notice) {
	entry.Title, entry.Body, entry.URL = notice.Title, notice.Body, notice.URL
	entry.RepoOwner, entry.RepoName = notice.Repo.Owner, notice.Repo.Repo

	n.dispatcher.Notify(ctx, entry)
}

// releaseSignedOut marks gh's "no one is signed in" as release.ErrSignedOut. A missing gh
// keeps the client's own words, which say to install it.
func releaseSignedOut(err error) error {
	if errors.Is(err, github.ErrNotLoggedIn) {
		return fmt.Errorf("%w: %w", release.ErrSignedOut, err)
	}

	return err
}
