package core

import (
	"context"
	"errors"
	"fmt"

	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/hooks"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/scripts"
	"github.com/mrz1836/nilbox/internal/settings"
)

// latestReleases hands the integrations the tools' latest releases from GitHub. It
// implements integrations.Releases.
type latestReleases struct {
	tokens github.Tokens
	client *github.Client
}

// LatestReleases asks GitHub for each repository's latest release. A token GitHub
// refuses is borrowed again once; with no token at all, as when signed out, nothing is
// asked, and the error is integrations.ErrSignedOut.
func (r latestReleases) LatestReleases(ctx context.Context, repos []repo.Name) (map[string]integrations.Release, error) {
	got, err := github.WithToken(ctx, r.tokens, func(tok github.Token) (map[string]github.LatestRelease, error) {
		return r.client.FetchLatestReleases(ctx, tok, repos)
	})

	switch {
	case errors.Is(err, github.ErrNotLoggedIn) || errors.Is(err, github.ErrGHNotFound):
		return nil, fmt.Errorf("%w: %w", integrations.ErrSignedOut, err)
	case err != nil:
		return nil, err
	}

	out := make(map[string]integrations.Release, len(got))

	for key, release := range got {
		assets := make([]integrations.Asset, 0, len(release.Assets))
		for _, a := range release.Assets {
			assets = append(assets, integrations.Asset{Name: a.Name, URL: a.URL, Size: a.Size, Digest: a.Digest})
		}

		out[key] = integrations.Release{Tag: release.Tag, URL: release.URL, PublishedAt: release.PublishedAt, Assets: assets}
	}

	return out, nil
}

// toolList returns what the integrations show of every tool, for the pages that run one:
// Release, Hooks, and Repo scripts. A list that can't be read holds no tool, every one of
// them then missing and off, and no read of the user's shell. It never waits for a
// detection: List serves the last one.
func toolList(ctx context.Context, tools *integrations.Service) integrations.Tools {
	list, err := tools.List(ctx)
	if err != nil {
		return integrations.Tools{}
	}

	return list
}

// toolByID returns the tool id in list, or the zero Tool, a missing one, when list holds
// none.
func toolByID(list integrations.Tools, id integrationprefs.ID) integrations.Tool {
	for _, t := range list.Tools {
		if t.ID == id {
			return t
		}
	}

	return integrations.Tool{}
}

// hooksClones lists the clones in the repository locations the settings name, for the
// Hooks and Repo scripts pages, reading no git, and a clone's origin with the read-only
// remote lookup localrepos.Inspect runs (hooks.Clones, scripts.Clones).
type hooksClones struct {
	settings *settings.Service
	home     string
	git      execpath.Runner
}

// Roots returns the clone root and the other locations as they are set now.
func (c hooksClones) Roots(ctx context.Context) (localrepos.Roots, error) {
	return releaseClones{settings: c.settings, home: c.home}.Roots(ctx)
}

// Dirs lists the clones in roots.
func (c hooksClones) Dirs(ctx context.Context, roots localrepos.Roots) ([]string, error) {
	found, err := roots.Clones(ctx)
	if err != nil {
		return nil, err
	}

	return found.Dirs, nil
}

// Origin returns the repository the clone at dir's origin points at, and whether it is a
// GitHub repository.
func (c hooksClones) Origin(ctx context.Context, dir string) (repo.Name, bool, error) {
	return releaseOrigins{git: c.git}.Origin(ctx, dir)
}

// hooksTool reads what the integrations know of go-pre-commit, every copy found and its
// definition's features, for the Hooks page (hooks.ToolSource).
type hooksTool struct {
	tools *integrations.Service
}

// HooksTool returns go-pre-commit's facts; a list that can't be read is a go-pre-commit
// that's missing, with the user's shell unread.
func (h hooksTool) HooksTool(ctx context.Context) hooks.ToolFacts {
	def := integrations.GoPreCommit()
	list := toolList(ctx, h.tools)
	tool := toolByID(list, def.ID)

	return hooks.ToolFactsOf(def, tool, list.ShellRead && tool.ID == def.ID)
}

// scriptsTools reads what the integrations know of magex, and whether mage-x and
// go-pre-commit are on, for Repo scripts (scripts.ToolSource).
type scriptsTools struct {
	tools *integrations.Service
}

// Tools returns the tools' facts; a list that can't be read is every tool missing and off.
func (s scriptsTools) Tools(ctx context.Context) scripts.Tools {
	list := toolList(ctx, s.tools)

	return scripts.ToolsOf(toolByID(list, integrationprefs.MageX), toolByID(list, integrationprefs.GoPreCommit))
}

// integrationSettings reads the integrations' settings: all of them, for Repo scripts'
// sources, switch, and where runs go (scripts.Settings), or only where runs go, for the
// Hooks page (hooks.Places).
type integrationSettings struct {
	settings *settings.Service
}

// Integrations returns the integrations' settings.
func (p integrationSettings) Integrations(ctx context.Context) (integrationprefs.Config, error) {
	return settingsPart(ctx, p.settings, func(s settings.Settings) integrationprefs.Config { return s.Integrations })
}

// RunsIn returns the integrations.runsIn setting.
func (p integrationSettings) RunsIn(ctx context.Context) (integrationprefs.RunsIn, error) {
	return settingsPart(ctx, p.settings, func(s settings.Settings) integrationprefs.RunsIn { return s.Integrations.RunsIn })
}
