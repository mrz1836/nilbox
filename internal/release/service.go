package release

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/flight"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/reporules"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// checkConcurrency is how many clones' states are read at once.
const checkConcurrency = 8

// ErrSignedOut is returned when no one is signed in to GitHub, so nothing could be asked.
var ErrSignedOut = errors.New("not signed in to GitHub")

// Clone is a clone the candidates are listed from: its absolute folder, and its origin,
// when origin is a GitHub repository.
type Clone struct {
	Dir    string
	Origin repo.Name
	OnHub  bool
}

// Clones finds the clones: Roots is the clone root and the other repository locations as
// set now, a cheap read, and Clones lists the clones in them, the clone root's first.
// Core adapts the settings and localrepos.
type Clones interface {
	Roots(ctx context.Context) (localrepos.Roots, error)
	Clones(ctx context.Context, roots localrepos.Roots) ([]Clone, error)
}

// Checker reads a clone's state for its row on the page, with read-only git: the branch
// it's on, its uncommitted files, how it stands with branch on origin (whose newest commit
// GitHub says is remoteHead), and the version tags only it has, which tags (GitHub's
// newest) tell. It never fails: what it can't read, it marks unreadable.
type Checker interface {
	CloneState(ctx context.Context, dir, branch, remoteHead string, tags []Tag) CloneState
}

// RuleSource reads the repositories and orgs the user pinned or muted: core reads them
// from the prefs, which settings writes.
type RuleSource interface {
	RepoRules(ctx context.Context) (reporules.Rules, error)
}

// Config holds a Service's dependencies. GitHub and Clones are required; Prepare and
// Start also need Git, Origins, and Runs.
type Config struct {
	GitHub  GitHub
	Clones  Clones
	Checker Checker

	// Git reads the clone a release runs in, before it starts and under its lock, and
	// finds where the release left the tag and the clone once it ends.
	Git *gitread.Git

	// Origins reads which repository a clone's origin is, Tools what the app knows of
	// magex, and Locks which job holds a clone: the app's one *clonelock.Set.
	Origins OriginReader
	Tools   ToolSource
	Locks   LockHolders

	// Env returns the login shell's environment, and whether it was read
	// (shellenv.Reader.Env): a release in the app runs with it, and so do ssh-add and
	// ssh-keygen, which Runner runs. Both default as GatherConfig's do.
	Env    func() ([]string, bool)
	Runner func(env []string) execpath.Runner

	// Ready, when set, returns a channel closed once the login shell's environment has
	// first been read (shellenv.Reader.Ready). A release from before a restart waits for
	// it before it is verified, so origin is asked with the user's own ssh agent.
	Ready func() <-chan struct{}

	// Runs starts a release's run: the scriptrun engine. Places reads where runs go; without
	// it they go in the app.
	Runs   Runs
	Places Places

	// History reads a run back from the runner's history, for ReleaseResult: the scriptrun
	// Service.
	History RunHistory

	// Notifier tells the user of each release that reaches GitHub, and of each its workflow
	// published or failed; without it, nothing is told.
	Notifier Notifier

	// Workflows reads how a release's workflow on GitHub goes, and Records reads the
	// releases that ended and writes how each one's follow goes into its record: the
	// Follower needs both, and without either no release is followed.
	Workflows Workflows
	Records   RunRecords

	// AfterMerge reads whether a merge offers a release, and Branches the default branch
	// the app last stored for a repository, which OfferAfterMerge reads with Tools and the
	// clones. Without AfterMerge, a merge offers nothing.
	AfterMerge MergeSetting
	Branches   DefaultBranches

	// Terminal opens the user's terminal in a clone, for a check's Open in terminal.
	// Without it, OpenTerminal says the terminal can't be opened here.
	Terminal scriptrun.FolderOpener

	// Keeper keeps the last list between starts, so the page shows it at once while the
	// Refresher reads GitHub again. Without it, nothing is kept.
	Keeper Keeper

	// Rules reads the pinned and muted repositories and orgs: a muted one is listed in
	// GroupMuted and never asked about, and a pinned one comes first in its group. Without
	// it, nothing is pinned or muted.
	Rules RuleSource

	Home   string
	Clock  clock.Clock
	Events event.Sink
	Logger *slog.Logger
}

// Candidates is the Release page's list: every clone with a GitHub origin, grouped, and
// how many clones have none. CheckedAt is when GitHub was last asked, RFC 3339. Error
// says why the last refresh failed, while the list from before it is still shown.
//
// Kept reports the list kept from a past start, shown at once after a restart: GitHub
// hasn't been read since, and CheckedAt says how old it is. Refreshing reports a read of
// GitHub under way behind the page; event.ReleasesUpdated says when it lands.
type Candidates struct {
	Repos         []Candidate `json:"repos"`
	WithoutGitHub int         `json:"withoutGitHub"`
	CheckedAt     string      `json:"checkedAt"`
	Error         string      `json:"error"`
	Kept          bool        `json:"kept"`
	Refreshing    bool        `json:"refreshing"`
}

// Service lists what can be released, prepares a release's sheet, starts the release,
// reads a release's result back, and says whether a merge offers a release. The window
// binds it.
type Service struct {
	github  GitHub
	clones  Clones
	checker Checker
	home    string
	clock   clock.Clock
	events  event.Sink
	logger  *slog.Logger

	// gatherer gathers a release's facts, and git reads where a release left the tag and
	// the clone; both are nil without Config.Git. env is the environment a release runs
	// with, and ready says when it is the shell's; runs starts a release, places says where
	// runs go, history reads a run back, and notifier tells of a release on GitHub. origins,
	// tools, afterMerge, and branches serve OfferAfterMerge.
	gatherer   *Gatherer
	git        verifyGit
	env        func() ([]string, bool)
	ready      func() <-chan struct{}
	runs       Runs
	places     Places
	history    RunHistory
	notifier   Notifier
	origins    OriginReader
	tools      ToolSource
	afterMerge MergeSetting
	branches   DefaultBranches
	terminal   scriptrun.FolderOpener

	// workflows reads a release's workflow on GitHub, and records the releases' records,
	// for the Follower. followMu guards toFollow, the releases handed to the Follower since
	// it last looked, and wakeFollow wakes it (queueFollow).
	workflows  Workflows
	records    RunRecords
	followMu   sync.Mutex
	toFollow   map[int64]followed
	wakeFollow chan struct{}

	// keeper keeps the last list between starts, and progressEvery is how often, at most, a
	// refresh tells the window how far it has got.
	keeper        Keeper
	progressEvery time.Duration

	// ruleSource reads the pinned and muted repositories (readRules).
	ruleSource RuleSource

	// wake asks the Refresher to read the list again behind the page, and wakePatch asks it
	// to read again the repositories released since (queuePatch).
	wake      chan struct{}
	wakePatch chan struct{}

	// reading lets one read of the list run at a time, a refresh or a patch, so neither
	// lands over the other with older answers (startReading).
	reading chan struct{}

	// factsMu guards facts: the facts each clone's sheet was last shown, by its folder,
	// resolved.
	factsMu sync.Mutex
	facts   map[string]keptFacts

	// refreshes runs one refresh at a time; a caller that comes while one runs gets its
	// answer.
	refreshes flight.Group[Candidates]

	// mu guards the last list, the repository locations it was read from (their Key),
	// whether it is current (its last read landed, and nothing put it out of date since),
	// and why that read failed, if it did. kept reports the last list as the one kept from a past start, which no
	// refresh has replaced yet, and restored that it was read. background reports the
	// Refresher running, and pending a refresh it was asked for that hasn't landed.
	// unpatched holds the keys of the repositories released, or merged into, since the
	// Refresher last patched the list, patching reports a patch queued or under way, so the
	// list is served as refreshing, and outdated counts the times the list went out of date
	// (outOfDate). rules are the pinned and muted repositories as last read, which the list is
	// served under (arranged).
	mu         sync.Mutex
	last       Candidates
	root       string
	current    bool
	failure    string
	has        bool
	kept       bool
	restored   bool
	background bool
	pending    bool
	unpatched  map[string]bool
	patching   bool
	outdated   int
	rules      reporules.Rules
}

// NewService returns a Service over cfg.
func NewService(cfg Config) *Service {
	s := &Service{
		github: cfg.GitHub, clones: cfg.Clones, checker: cfg.Checker, home: cfg.Home,
		clock: clock.Or(cfg.Clock), events: event.Or(cfg.Events), logger: logging.Or(cfg.Logger),
		env: cfg.Env, ready: cfg.Ready, runs: cfg.Runs, places: cfg.Places, history: cfg.History,
		notifier: cfg.Notifier, origins: cfg.Origins, tools: cfg.Tools, afterMerge: cfg.AfterMerge,
		branches: cfg.Branches,
		terminal: cfg.Terminal,
		keeper:   cfg.Keeper, progressEvery: progressEvery, ruleSource: cfg.Rules,
		workflows: cfg.Workflows, records: cfg.Records, toFollow: map[int64]followed{},
		wakeFollow: make(chan struct{}, 1),
		wake:       make(chan struct{}, 1), wakePatch: make(chan struct{}, 1), reading: make(chan struct{}, 1),
		facts: map[string]keptFacts{}, unpatched: map[string]bool{},
	}

	if s.env == nil {
		s.env = appEnv
	}

	if cfg.Git != nil {
		s.git = cfg.Git
		s.gatherer = NewGatherer(GatherConfig{
			Git: cfg.Git, GitHub: cfg.GitHub, Origins: cfg.Origins, Tools: cfg.Tools, Locks: cfg.Locks, Env: s.env,
			Runner: cfg.Runner, Home: cfg.Home,
		})
	}

	return s
}

// Candidates returns the repositories the Release page lists. It serves the last list,
// however old, while it is current and was read from the repository locations as set now,
// unless refresh is set: the whole list is read again only when the user asks (Read GitHub
// again), none was read yet, the locations changed, or the list went out of date
// (outOfDate). Otherwise it asks GitHub, in two rounds of batched queries, reads the
// unreleased clones' state, and emits event.ReleasesUpdated, telling the window how far it
// has got meanwhile (event.ReleasesProgress). A refresh that fails keeps the last list,
// with its error, until the user reads it again, and fails only when there is none.
//
// After a restart, the list kept from the last start, when it was read from the repository
// locations as set now, is served at once, marked Kept and Refreshing, while the Refresher
// reads GitHub again behind the page; without the Refresher, the call waits for GitHub.
//
// A release that ends, or a pull request merged in the review sheet, changes one
// repository's rows alone: while the Refresher runs, it reads that repository alone again
// and patches its rows into the list, which stays as current as it was (queuePatch,
// queueMerged). Meanwhile the list is served as it was, marked Refreshing.
//
// Every list is served under the pinned and muted repositories as they are now, read on
// each call: a muted repository is listed in GroupMuted, and GitHub is never asked about
// one muted as the list is read; a pinned one comes first in its group. Muting a listed
// repository asks nothing, and unmuting one that was never read reads it alone, as a
// release's patch does (liftMuted).
func (s *Service) Candidates(ctx context.Context, refresh bool) (Candidates, error) {
	roots, err := s.clones.Roots(ctx)
	if err != nil {
		return Candidates{}, fmt.Errorf("read the repository locations: %w", err)
	}

	s.restore(ctx)
	s.readRules(ctx)

	if served, ok := s.serveNow(roots.Key(), refresh); ok {
		return served, nil
	}

	return s.refreshes.Do(ctx, roots.Key(), func(ctx context.Context) (Candidates, error) {
		return s.refresh(ctx, roots)
	})
}

// serveNow returns the last list read from the locations root keys (Roots.Key) when it is
// served without waiting for GitHub: while it is current, unless refresh is set, or, when
// it is the list kept from a past start, at once, asking the Refresher to read GitHub
// again behind it.
func (s *Service) serveNow(root string, refresh bool) (Candidates, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if refresh || !s.has || s.root != root {
		return Candidates{}, false
	}

	if s.current {
		return s.served(), true
	}

	if !s.kept || !s.background {
		return Candidates{}, false
	}

	s.askBehind()

	return s.served(), true
}

// served is the last list as the page shows it, with the last refresh's failure, whether
// it was kept from a past start, and whether a refresh runs behind it. The caller holds mu.
func (s *Service) served() Candidates {
	out := s.last
	out.Repos = arranged(s.last.Repos, s.rules)
	out.Error, out.Kept, out.Refreshing = s.failure, s.kept, s.pending || s.patching

	return out
}

// refresh asks GitHub and the clones in roots again, keeping the last list when it
// fails. A list that lands is kept for the next start too. A refresh cut short, as when
// the page closes, keeps nothing and records nothing.
//
// It waits for a patch under way to land first (startReading). A list that went out of
// date while it ran, as when a release ended without the Refresher, lands out of date too:
// GitHub may have answered before the release reached it. A repository released before it
// began, whose patch is still queued, is read here, so the patch leaves it out.
func (s *Service) refresh(ctx context.Context, roots localrepos.Roots) (Candidates, error) {
	root := roots.Key()

	if err := s.startReading(ctx); err != nil {
		return Candidates{}, err
	}

	defer s.doneReading()

	s.mu.Lock()
	outdated, covered := s.outdated, maps.Clone(s.unpatched)
	s.mu.Unlock()

	progress := newListProgress(s.events, s.progressEvery)

	got, err := s.gather(ctx, roots, progress)
	if err == nil {
		err = ctx.Err() // a list read as the call ended holds clones marked unreadable
	}

	progress.End()

	s.mu.Lock()
	defer s.mu.Unlock()

	if ctx.Err() != nil {
		return Candidates{}, ctx.Err()
	}

	s.current = s.outdated == outdated

	if err != nil {
		if !s.has || s.root != root {
			return Candidates{}, err
		}

		s.logger.WarnContext(ctx, "couldn't list the repositories to release; showing the last list", "error", err)
		s.failure, s.pending = uierr.Message(err), false
		s.events.Emit(event.ReleasesUpdated{})

		return s.served(), nil
	}

	s.last, s.root, s.failure, s.has, s.kept, s.pending = got, root, "", true, false, false
	s.keep(context.WithoutCancel(ctx), got, root)

	// The repositories released before it began are read already: no patch reads them again.
	for key := range covered {
		delete(s.unpatched, key)
	}

	// A repository unmuted while it ran was left unread: it is read alone now.
	s.liftMuted()

	s.events.Emit(event.ReleasesUpdated{})

	out := got
	out.Repos = arranged(got.Repos, s.rules)

	return out, nil
}

// readRules reads the pinned and muted repositories as they are now and serves the list
// under them. When they changed, a repository no longer muted that the list holds unread is
// read alone (liftMuted). Rules that can't be read are logged, and the last ones stand.
func (s *Service) readRules(ctx context.Context) {
	if s.ruleSource == nil {
		return
	}

	rules, err := s.ruleSource.RepoRules(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.logger.WarnContext(ctx, "couldn't read the pinned and muted repositories; using the last ones read",
				"error", err)
		}

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if rules.Equal(s.rules) {
		return
	}

	s.rules = rules
	s.liftMuted()
}

// startReading waits for the read of the list under way, a refresh or a patch, to land,
// and then lets this one run, until doneReading. One at a time, a refresh that asked
// GitHub before a release ended lands before the release's patch reads its repository, so
// the patch lands over it, never under it. It fails only when ctx ends first.
func (s *Service) startReading(ctx context.Context) error {
	select {
	case s.reading <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// doneReading lets the next read of the list run.
func (s *Service) doneReading() {
	<-s.reading
}

// gather lists the clones, asks GitHub about their origins, and reads the clones whose
// repository has changes to release, telling progress of each phase.
func (s *Service) gather(ctx context.Context, roots localrepos.Roots, progress *listProgress) (Candidates, error) {
	progress.Start(event.ReleasesFindingClones, 0)

	all, err := s.clones.Clones(ctx, roots)
	if err != nil {
		return Candidates{}, fmt.Errorf("list the clones: %w", err)
	}

	out := Candidates{Repos: []Candidate{}}

	s.mu.Lock()
	rules := s.rules
	s.mu.Unlock()

	listed := make([]clone, 0, len(all))
	muted := []Candidate{}

	for _, c := range all {
		if !c.OnHub {
			out.WithoutGitHub++

			continue
		}

		// A muted repository is listed apart, and GitHub is never asked about it.
		if rules.IsMuted(c.Origin) {
			muted = append(muted, mutedCandidate(clone{dir: c.Dir, origin: c.Origin}, s.home))

			continue
		}

		listed = append(listed, clone{dir: c.Dir, origin: c.Origin})
	}

	if len(listed) == 0 {
		out.Repos = append(out.Repos, muted...)
		sortCandidates(out.Repos)
		out.CheckedAt = s.clock.Now().UTC().Format(time.RFC3339)

		return out, nil
	}

	statuses, compares, err := s.askGitHub(ctx, listed, progress)
	if err != nil {
		return Candidates{}, err
	}

	out.Repos = s.candidates(listed, statuses, compares)
	s.checkClones(ctx, out.Repos, statuses, progress)
	out.Repos = append(out.Repos, muted...)
	sortCandidates(out.Repos)

	out.CheckedAt = s.clock.Now().UTC().Format(time.RFC3339)

	return out, nil
}

// askGitHub runs the two rounds: every repository's status, then, for each with a
// version tag not on its newest commit, how far the default branch is ahead of it. Each
// round counts its repositories off as GitHub answers them.
func (s *Service) askGitHub(
	ctx context.Context, listed []clone, progress *listProgress,
) (map[string]RepoStatus, map[string]Compare, error) {
	repos := uniqueRepos(listed)

	progress.Start(event.ReleasesAskingGitHub, len(repos))

	statuses, err := s.github.ReleaseStatus(ctx, repos, progress.Add)
	if err != nil {
		return nil, nil, wordGitHub(err)
	}

	reqs := compareRequests(repos, statuses)
	if len(reqs) == 0 {
		return statuses, map[string]Compare{}, nil
	}

	progress.Start(event.ReleasesComparing, len(reqs))

	compares, err := s.github.ReleaseCompare(ctx, reqs, progress.Add)
	if err != nil {
		return nil, nil, wordGitHub(err)
	}

	return statuses, compares, nil
}

// candidates works out each listed clone's row from GitHub's answers, its folder written
// with ~ for home.
func (s *Service) candidates(listed []clone, statuses map[string]RepoStatus, compares map[string]Compare) []Candidate {
	out := make([]Candidate, 0, len(listed))

	for _, c := range listed {
		key := c.origin.Key()
		status, found := statuses[key]
		since, compared := compares[key]

		cand := candidateFor(c, status, found, since, compared)
		cand.Dir = paths.Tilde(c.dir, s.home)
		out = append(out, cand)
	}

	return out
}

// uniqueRepos returns the clones' repositories, each once: two clones of one repository
// ask about it once.
func uniqueRepos(listed []clone) []repo.Name {
	seen := map[string]bool{}
	repos := make([]repo.Name, 0, len(listed))

	for _, c := range listed {
		if key := c.origin.Key(); !seen[key] {
			seen[key] = true

			repos = append(repos, c.origin)
		}
	}

	return repos
}

// compareRequests asks, for each repository with a version tag off its newest commit,
// how far the default branch is ahead of the tag.
func compareRequests(repos []repo.Name, statuses map[string]RepoStatus) []CompareRequest {
	reqs := make([]CompareRequest, 0, len(repos))

	for _, name := range repos {
		status, ok := statuses[name.Key()]
		if !ok || status.DefaultBranch == "" {
			continue
		}

		if tag, has := newestVersionTag(status.Tags); has && tag.Commit != status.HeadOID {
			reqs = append(reqs, CompareRequest{Repo: name, Tag: tag.Name, Branch: status.DefaultBranch})
		}
	}

	return reqs
}

// checkClones reads the state of each unreleased clone, a few at a time, counting them
// off as each is read.
func (s *Service) checkClones(
	ctx context.Context, list []Candidate, statuses map[string]RepoStatus, progress *listProgress,
) {
	unreleased := 0

	for _, c := range list {
		if c.Group == GroupUnreleased {
			unreleased++
		}
	}

	if s.checker == nil || unreleased == 0 {
		return
	}

	progress.Start(event.ReleasesCheckingClones, unreleased)

	var wg sync.WaitGroup

	slots := make(chan struct{}, checkConcurrency)

	for i := range list {
		if list[i].Group != GroupUnreleased {
			continue
		}

		status := statuses[list[i].origin.Key()]

		slots <- struct{}{}

		wg.Go(func() {
			defer func() { <-slots }()

			list[i].Clone = new(s.checker.CloneState(ctx, list[i].dir, status.DefaultBranch, status.HeadOID, status.Tags))

			progress.Add(1)
		})
	}

	wg.Wait()
}

// wordGitHub words a GitHub failure for the page: signed out says so, a failure the
// client already worded keeps its words, and anything else says GitHub couldn't be read.
func wordGitHub(err error) error {
	if errors.Is(err, ErrSignedOut) {
		return uierr.New("Sign in to GitHub first, so the Release page can read your repositories.", err)
	}

	if _, worded := errors.AsType[*uierr.Error](err); worded {
		return err
	}

	return uierr.New("Couldn't read your repositories from GitHub.", err)
}
