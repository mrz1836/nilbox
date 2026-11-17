package core

import (
	"context"
	"database/sql"
	"log/slog"
	"path/filepath"

	"github.com/mrz1836/nilbox/internal/appinfo"
	"github.com/mrz1836/nilbox/internal/backup"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/launcher/terminal"
	"github.com/mrz1836/nilbox/internal/localrepos"
	prefsstore "github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/setup"
)

// setupDeps are what the first-run setup reaches beyond its own memory: the GitHub sign-in,
// the GitHub CLI's version, the terminals, the clones, the runs, and the backup saved before
// a reset.
type setupDeps struct {
	tokens    *github.TokenSource
	client    *github.Client
	info      *appinfo.Service
	registry  terminal.Registry
	settings  *settings.Service
	locations *localrepos.Reader
	runs      *scriptrun.Engine
	backup    *backup.Service
}

// newSetup builds the first-run setup's service over the prefs in pool, which it alone
// writes the setup.* keys of.
func newSetup(pool *sql.DB, opts Options, deps setupDeps, logger *slog.Logger) *setup.Service {
	apps := []string{"/Applications", filepath.Join(opts.Home, "Applications")}

	return setup.NewService(setup.Config{
		Memory:  prefsstore.New(pool, opts.Clock),
		Account: setupAccount{tokens: deps.tokens, client: deps.client},
		CLI:     setupCLI{info: deps.info},
		// iTerm2 is installed when its bundle is in /Applications or ~/Applications;
		// Terminal comes with the system.
		Terminals:    func() []terminal.Choice { return deps.registry.Installed(apps...) },
		Clones:       setupClones{settings: deps.settings, home: opts.Home, reader: deps.locations},
		Runs:         deps.runs,
		Saver:        backup.NewSaver(deps.backup),
		ResetRequest: opts.Paths.ResetRequest,
		Clock:        opts.Clock,
		Events:       opts.Events,
		Logger:       logger,
	})
}

// adoptSetup decides, once per database, whether the first-run setup shows (setup.Adopt):
// an install from before it, whose inbox ever signed in, is adopted as done. A failure is
// logged, and the setup then reads as done, so it never traps the user.
func adoptSetup(ctx context.Context, pool *sql.DB, c clock.Clock, logger *slog.Logger) {
	if _, err := setup.Adopt(ctx, prefsstore.New(pool, c), inbox.NewStore(pool, c)); err != nil {
		logger.WarnContext(ctx, "could not decide whether the setup shows; it reads as done", "error", err)
	}
}

// adoptSections carries the window's memory of the inbox's former Authored and Assigned
// lists over to Yours (settings.AdoptSections), once per database. A failure is logged,
// and the window then shows Yours as it would on a fresh database.
func adoptSections(ctx context.Context, pool *sql.DB, c clock.Clock, logger *slog.Logger) {
	if err := settings.AdoptSections(ctx, pool, c); err != nil {
		logger.WarnContext(ctx, "could not carry the inbox sections over; Yours starts as new", "error", err)
	}
}

// setupAccount reads the GitHub CLI's sign-in for the setup: the token the app borrows,
// with the shared client's one-point viewer query, tried once more with a fresh token
// when GitHub refuses the one held, as every other read is.
type setupAccount struct {
	tokens *github.TokenSource
	client *github.Client
}

// SignIn implements setup.Account.
func (a setupAccount) SignIn(ctx context.Context) (github.SignIn, error) {
	return github.WithToken(ctx, a.tokens, func(tok github.Token) (github.SignIn, error) {
		return a.client.FetchSignIn(ctx, tok)
	})
}

// setupCLI reads the GitHub CLI's version as Settings → About shows it, kept until its
// command changes.
type setupCLI struct {
	info *appinfo.Service
}

// GHVersion implements setup.CLI.
func (c setupCLI) GHVersion(ctx context.Context) (string, bool) {
	for _, t := range c.info.ToolVersions(ctx) {
		if t.Tool == "gh" {
			return t.Version, t.Installed
		}
	}

	return "", false
}

// setupClones lists the clones in the repository locations the settings name now, reading
// no git, with a location that doesn't answer in time skipped (setup.Clones).
type setupClones struct {
	settings *settings.Service
	home     string
	reader   *localrepos.Reader
}

// Clones implements setup.Clones.
func (c setupClones) Clones(ctx context.Context) ([]string, error) {
	roots, err := settingsPart(ctx, c.settings, func(s settings.Settings) settings.Settings { return s })
	if err != nil {
		return nil, err
	}

	r, err := rootsOf(roots, c.home, c.reader)
	if err != nil {
		return nil, err
	}

	found, err := r.Clones(ctx)
	if err != nil {
		return nil, err
	}

	return found.Dirs, nil
}
