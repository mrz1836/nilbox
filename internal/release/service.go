package release

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/flight"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/uierr"
)

const (
	// FreshFor is how long a candidates list is served without asking GitHub again.
	FreshFor = 5 * time.Minute

	// checkConcurrency is how many clones' states are read at once.
	checkConcurrency = 8
)

// ErrSignedOut is returned when no one is signed in to GitHub, so nothing could be asked.
var ErrSignedOut = errors.New("not signed in to GitHub")

// Clone is a clone the candidates are listed from: its absolute folder, and its origin,
// when origin is a GitHub repository.
type Clone struct {
	Dir    string
	Origin repo.Name
	OnHub  bool
}

// Clones finds the clones: Root is the clone root as set now, a cheap read, and Clones
// lists the clones under root. Core adapts the settings and localrepos.
type Clones interface {
	Root(ctx context.Context) (string, error)
	Clones(ctx context.Context, root string) ([]Clone, error)
}

// Checker reads a clone's state for its row on the page, with read-only git: the branch
// it's on, its uncommitted files, how it stands with branch on origin (whose newest commit
// GitHub says is remoteHead), and the version tags only it has, which tags (GitHub's
// newest) tell. It never fails: what it can't read, it marks unreadable.
type Checker interface {
	CloneState(ctx context.Context, dir, branch, remoteHead string, tags []Tag) CloneState
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

	// Notifier tells the user of each release that reaches GitHub; without it, nothing is
	// told.
	Notifier Notifier

	// AfterMerge reads whether a merge offers a release, and Branches the default branch
	// the app last stored for a repository, which OfferAfterMerge reads with Tools and the
	// clones. Without AfterMerge, a merge offers nothing.
	AfterMerge MergeSetting
	Branches   DefaultBranches

	// Terminal opens the user's terminal in a clone, for a check's Open in terminal.
	// Without it, OpenTerminal says the terminal can't be opened here.
	Terminal FolderOpener

	Home   string
	Clock  clock.Clock
	Events event.Sink
	Logger *slog.Logger
}

// Candidates is the Release page's list: every clone with a GitHub origin, grouped, and
// how many clones have none. CheckedAt is when GitHub was last asked, RFC 3339. Error
// says why the last refresh failed, while the list from before it is still shown.
type Candidates struct {
	Repos         []Candidate `json:"repos"`
	WithoutGitHub int         `json:"withoutGitHub"`
	CheckedAt     string      `json:"checkedAt"`
	Error         string      `json:"error"`
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
	terminal   FolderOpener

	// factsMu guards facts: the facts each clone's sheet was last shown, by its folder,
	// resolved.
	factsMu sync.Mutex
	facts   map[string]keptFacts

	// refreshes runs one refresh at a time; a caller that comes while one runs gets its
	// answer.
	refreshes flight.Group[Candidates]

	// mu guards the last list, the clone root it was read from, when it was last asked
	// for, and why that failed, if it did.
	mu      sync.Mutex
	last    Candidates
	root    string
	tried   time.Time
	failure string
	has     bool
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
		facts:    map[string]keptFacts{},
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

// Candidates returns the repositories the Release page lists. It serves the last list
// while it is under five minutes old and was read from the clone root as set now, unless
// refresh is set; otherwise it asks GitHub, in two rounds of batched queries, reads the
// unreleased clones' state, and emits event.ReleasesUpdated. A refresh that fails keeps
// the last list, with its error, until one succeeds, and fails only when there is none.
func (s *Service) Candidates(ctx context.Context, refresh bool) (Candidates, error) {
	root, err := s.clones.Root(ctx)
	if err != nil {
		return Candidates{}, fmt.Errorf("read the clone root: %w", err)
	}

	s.mu.Lock()
	served, fresh := s.served(), s.has && s.root == root && s.clock.Now().Sub(s.tried) < FreshFor
	s.mu.Unlock()

	if fresh && !refresh {
		return served, nil
	}

	return s.refreshes.Do(ctx, root, func(ctx context.Context) (Candidates, error) {
		return s.refresh(ctx, root)
	})
}

// served is the last list as the page shows it, with the last refresh's failure. The
// caller holds mu.
func (s *Service) served() Candidates {
	out := s.last
	out.Error = s.failure

	return out
}

// refresh asks GitHub and the clones under root again, keeping the last list when it
// fails. A refresh cut short, as when the page closes, keeps nothing and records nothing.
func (s *Service) refresh(ctx context.Context, root string) (Candidates, error) {
	got, err := s.gather(ctx, root)
	if err == nil {
		err = ctx.Err() // a list read as the call ended holds clones marked unreadable
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if ctx.Err() != nil {
		return Candidates{}, ctx.Err()
	}

	s.tried = s.clock.Now()

	if err != nil {
		if !s.has || s.root != root {
			return Candidates{}, err
		}

		s.logger.WarnContext(ctx, "couldn't list the repositories to release; showing the last list", "error", err)
		s.failure = uierr.Message(err)
		s.events.Emit(event.ReleasesUpdated{})

		return s.served(), nil
	}

	s.last, s.root, s.failure, s.has = got, root, "", true
	s.events.Emit(event.ReleasesUpdated{})

	return got, nil
}

// gather lists the clones, asks GitHub about their origins, and reads the clones whose
// repository has changes to release.
func (s *Service) gather(ctx context.Context, root string) (Candidates, error) {
	all, err := s.clones.Clones(ctx, root)
	if err != nil {
		return Candidates{}, fmt.Errorf("list the clones: %w", err)
	}

	out := Candidates{Repos: []Candidate{}}

	listed := make([]clone, 0, len(all))

	for _, c := range all {
		if !c.OnHub {
			out.WithoutGitHub++

			continue
		}

		listed = append(listed, clone{dir: c.Dir, origin: c.Origin})
	}

	if len(listed) == 0 {
		out.CheckedAt = s.clock.Now().UTC().Format(time.RFC3339)

		return out, nil
	}

	statuses, compares, err := s.askGitHub(ctx, listed)
	if err != nil {
		return Candidates{}, err
	}

	for _, c := range listed {
		key := c.origin.Key()
		status, found := statuses[key]
		since, compared := compares[key]

		cand := candidateFor(c, status, found, since, compared)
		cand.Dir = paths.Tilde(c.dir, s.home)
		out.Repos = append(out.Repos, cand)
	}

	s.checkClones(ctx, out.Repos, statuses)
	sortCandidates(out.Repos)

	out.CheckedAt = s.clock.Now().UTC().Format(time.RFC3339)

	return out, nil
}

// askGitHub runs the two rounds: every repository's status, then, for each with a
// version tag not on its newest commit, how far the default branch is ahead of it.
func (s *Service) askGitHub(ctx context.Context, listed []clone) (map[string]RepoStatus, map[string]Compare, error) {
	repos := uniqueRepos(listed)

	statuses, err := s.github.ReleaseStatus(ctx, repos)
	if err != nil {
		return nil, nil, wordGitHub(err)
	}

	reqs := compareRequests(repos, statuses)
	if len(reqs) == 0 {
		return statuses, map[string]Compare{}, nil
	}

	compares, err := s.github.ReleaseCompare(ctx, reqs)
	if err != nil {
		return nil, nil, wordGitHub(err)
	}

	return statuses, compares, nil
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

// checkClones reads the state of each unreleased clone, a few at a time.
func (s *Service) checkClones(ctx context.Context, list []Candidate, statuses map[string]RepoStatus) {
	if s.checker == nil {
		return
	}

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

			state := s.checker.CloneState(ctx, list[i].dir, status.DefaultBranch, status.HeadOID, status.Tags)
			list[i].Clone = &state
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
