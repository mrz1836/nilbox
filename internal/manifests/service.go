package manifests

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/flight"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/reporules"
)

// Clones finds the clones in the repository locations: Roots is the clone root and the
// other locations as set now, Dirs lists the clones in them, reading no git
// (localrepos.Roots.Clones), and Origin reads which GitHub repository a clone's origin is,
// if any. Core adapts the settings and localrepos, as it does for Hooks.
type Clones interface {
	Roots(ctx context.Context) (localrepos.Roots, error)
	Dirs(ctx context.Context, roots localrepos.Roots) ([]string, error)
	Origin(ctx context.Context, dir string) (name repo.Name, onGitHub bool, err error)
}

// RuleSource reads the repositories and orgs the user pinned or muted: core reads them
// from the prefs, which settings writes.
type RuleSource interface {
	RepoRules(ctx context.Context) (reporules.Rules, error)
}

// DefaultBranches reads the default branch the app last stored for a repository, "" when
// it stored none. Core adapts the inbox's store.
type DefaultBranches interface {
	DefaultBranch(ctx context.Context, name repo.Name) (string, error)
}

// Git is the read-only git Deps reads clones with, through gitread's allowlist: the
// default branch's tip as the clone last fetched it, HEAD, a commit's tree, the newest
// commit to change a manifest, a blob, whether the clone is shallow, and where its
// FETCH_HEAD is. *gitread.Git.
type Git interface {
	OriginHead(ctx context.Context, dir string) (string, error)
	TrackingCommit(ctx context.Context, dir, branch string) (string, error)
	Head(ctx context.Context, dir string) (string, error)
	Tree(ctx context.Context, dir, commit string, keep func(path string) bool) ([]gitread.TreeEntry, error)
	LastChange(ctx context.Context, dir, from, manifest, lockfile string) (gitread.Commit, error)
	Blob(ctx context.Context, dir, oid string) ([]byte, error)
	Shallow(ctx context.Context, dir string) (bool, error)
	FetchHead(ctx context.Context, dir string) (string, error)
}

// Config holds a Service's dependencies. Clones and Git are required.
type Config struct {
	Clones Clones
	Git    Git

	// Rules reads the pinned and muted repositories, Branches the default branches the
	// app stored. Without Rules nothing is pinned or muted, and without Branches the
	// default branch is the clone's own (gitread.DefaultBranch).
	Rules    RuleSource
	Branches DefaultBranches

	// Terminal opens the user's terminal in a clone, for a row's Open in terminal, and
	// Reveal shows a manifest in Finder, for its Reveal in Finder. Without them, each
	// says it can't.
	Terminal FolderOpener
	Reveal   Revealer

	// DB is the database the repositories the user marked up to date are kept in
	// (deps_checks, which the package owns). Without it, nothing is kept, and no clone is
	// ever marked.
	DB *sql.DB

	Home   string
	Clock  clock.Clock
	Events event.Sink
	Logger *slog.Logger
}

// Service reads every clone's manifests for the Deps page. The window binds it.
type Service struct {
	clones   Clones
	git      Git
	rules    RuleSource
	branches DefaultBranches
	terminal FolderOpener
	reveal   Revealer
	checks   checkStore
	home     string
	clock    clock.Clock
	events   event.Sink
	logger   *slog.Logger

	// scanning runs one read of the clones per set of locations at a time; a caller that
	// comes while one runs gets its answer.
	scanning flight.Group[struct{}]

	// lifeMu guards what holds the page's calls and their reads to the app's lifetime
	// (Scans): quitting refuses one once the app quits, and cancels ends each under way, by
	// its number, the last of which was lastHeld. going counts those under way, which Scans
	// waits for.
	lifeMu   sync.Mutex
	quitting bool
	cancels  map[uint64]context.CancelFunc
	lastHeld uint64
	going    sync.WaitGroup

	// mu guards the last read of the clones: whether there is one, the locations and the
	// rules it read under, when it landed, why the read after it failed, and the page it
	// made; what each clone's last read found (kept), by its folder resolved; and the
	// pinned and muted repositories as last read (lastRules), and the repositories marked
	// up to date (lastChecks), which stand while they can't be read.
	mu         sync.Mutex
	has        bool
	roots      localrepos.Roots
	ruled      reporules.Rules
	scanned    time.Time
	failure    string
	page       Page
	kept       map[string]keptClone
	lastRules  reporules.Rules
	lastChecks map[string]time.Time
}

// NewService returns a Service over cfg.
func NewService(cfg Config) *Service {
	return &Service{
		clones: cfg.Clones, git: cfg.Git, rules: cfg.Rules, branches: cfg.Branches, terminal: cfg.Terminal,
		reveal: cfg.Reveal, checks: newCheckStore(cfg.DB), home: cfg.Home, clock: clock.Or(cfg.Clock),
		events: event.Or(cfg.Events), logger: logging.Or(cfg.Logger), cancels: map[uint64]context.CancelFunc{},
		kept: map[string]keptClone{}, page: emptyPage(), lastChecks: map[string]time.Time{},
	}
}

// List returns the Deps page. It serves the last read of the clones while it is under
// FreshFor old and was read from the repository locations, and under the pinned and muted
// repositories, as they are now; refresh, or anything else, reads every clone again,
// which costs one rev-parse for each clone whose commit to read is unchanged, and tells
// the window how far it has got (event.ManifestsProgress). A read that fails keeps the
// last page, with its error, until one succeeds. A read runs apart from the caller that
// started it, within ScanTimeout: a caller whose context ends stops waiting, and the read
// goes on for the next. The whole call runs within the app's lifetime (Scans): the app's
// quit ends it, and refuses one after it. Whichever page it serves carries the checks as
// they are now (MarkChecked), read each time, which a read of the clones never waits on.
func (s *Service) List(ctx context.Context, refresh bool) (Page, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	done, err := s.hold(cancel)
	if err != nil {
		return Page{}, err
	}

	defer done()

	return s.readPage(ctx, refresh)
}

// readPage is List, within the app's lifetime.
func (s *Service) readPage(ctx context.Context, refresh bool) (Page, error) {
	roots, err := s.clones.Roots(ctx)
	if err != nil {
		return Page{}, fmt.Errorf("read the repository locations: %w", err)
	}

	rules := s.readRules(ctx)

	if !refresh && s.fresh(roots, rules) {
		return s.servePage(ctx), nil
	}

	detached := func(ctx context.Context) (struct{}, error) {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ScanTimeout)
		defer cancel()

		done, refused := s.hold(cancel) // the app's quit ends it too
		if refused != nil {
			return struct{}{}, refused
		}

		defer done()

		return struct{}{}, s.scanAll(ctx, roots, rules)
	}

	if _, err = s.scanning.Do(ctx, scanKey+roots.Key(), detached); err != nil {
		return Page{}, err
	}

	return s.servePage(ctx), nil
}

// scanKey starts the key a read of the clones runs under, one at a time, with the
// locations' key after it.
const scanKey = "all\x00"

// fresh reports whether the last read may be served as it is: one landed under FreshFor
// ago, from roots, under rules.
func (s *Service) fresh(roots localrepos.Roots, rules reporules.Rules) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	age := s.clock.Now().Sub(s.scanned)

	return s.has && s.roots.Equal(roots) && s.ruled.Equal(rules) && age >= 0 && age < FreshFor
}

// readRules reads the pinned and muted repositories as they are now. Rules that can't be
// read are logged, and the last ones read stand; with no RuleSource, nothing is pinned or
// muted.
func (s *Service) readRules(ctx context.Context) reporules.Rules {
	s.mu.Lock()
	last := s.lastRules
	s.mu.Unlock()

	if s.rules == nil {
		return last
	}

	rules, err := s.rules.RepoRules(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.WarnContext(ctx, "couldn't read the pinned and muted repositories; using the last ones read",
				"error", err)
		}

		return last
	}

	s.mu.Lock()
	s.lastRules = rules
	s.mu.Unlock()

	return rules
}

// lastPage returns a copy of the last read's page, with when it landed and why the read
// after it failed.
func (s *Service) lastPage() Page {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := s.page
	out.Clones = make([]Clone, 0, len(s.page.Clones))

	for _, c := range s.page.Clones {
		out.Clones = append(out.Clones, c.copied())
	}

	out.CheckedAt, out.Error = stamp(s.scanned), s.failure

	return out
}

// copied returns c with lists of its own, so a caller's changes never reach what is kept.
func (c Clone) copied() Clone {
	c.Tools, c.Workspaces = slices.Clone(c.Tools), slices.Clone(c.Workspaces)
	c.Modules = slices.Clone(c.Modules)

	for i := range c.Modules {
		c.Modules[i].Workspaces = slices.Clone(c.Modules[i].Workspaces)
	}

	return c
}

// emptyPage is the page before any read: no clones, as an empty list.
func emptyPage() Page {
	return Page{Clones: []Clone{}}
}

// stamp writes t as RFC 3339, or "" for the zero time.
func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	return clock.Format(t)
}
