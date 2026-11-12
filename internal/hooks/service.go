package hooks

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/flight"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// Clones finds the clones under the clone root: Root is the clone root as set now, Dirs
// lists the clones directly under it, reading no git, and Origin reads which GitHub
// repository a clone's origin is, if any. Core adapts the settings and localrepos.
type Clones interface {
	Root(ctx context.Context) (string, error)
	Dirs(ctx context.Context, root string) ([]string, error)
	Origin(ctx context.Context, dir string) (name repo.Name, onGitHub bool, err error)
}

// Git is the read-only git the page reads clones with: *gitread.Git.
type Git interface {
	HookPaths(ctx context.Context, dir string) (gitread.HookPaths, error)
	Entries(ctx context.Context, dir string) ([]gitread.Entry, error)
}

// ToolSource reads what the app knows of go-pre-commit. Core adapts the integrations
// service, with ToolFactsOf. It never fails: a copy it can't read is missing.
type ToolSource interface {
	HooksTool(ctx context.Context) ToolFacts
}

// LockHolders says which job holds a clone, if any: a *clonelock.Set.
type LockHolders interface {
	Holder(path string) (clonelock.Holder, bool)
}

// Runs starts a run, and reads what a run from before a restart kept beside its record
// (scriptrun.Spec.Keep) for the Verifier: the scriptrun engine.
type Runs interface {
	Start(ctx context.Context, spec scriptrun.Spec) (int64, error)
	Kept(id int64, into any) (bool, error)
}

// RunHistory reads runs back from the runner's history: the scriptrun Service.
type RunHistory interface {
	Run(ctx context.Context, id int64) (scriptrun.Run, error)
	Runs(ctx context.Context, filter scriptrun.Filter) ([]scriptrun.Run, error)
}

// Places reads where runs go, as the integrations.runsIn setting says. Core adapts the
// settings.
type Places interface {
	RunsIn(ctx context.Context) (integrationprefs.RunsIn, error)
}

// Memory keeps what the page remembers between runs of the app, the hooks.* preferences,
// which the hooks package alone writes: a *prefs.Store.
type Memory interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
	Set(ctx context.Context, key, value string) error
}

// Config holds a Service's dependencies. Clones and Git are required; the actions and the
// checks also need Runs, and the results History.
type Config struct {
	Clones  Clones
	Git     Git
	Tools   ToolSource
	Locks   LockHolders
	Runs    Runs
	History RunHistory
	Places  Places
	Memory  Memory

	// Env returns the login shell's environment, and whether it was read
	// (shellenv.Reader.Env): go-pre-commit runs with it, and the PATH warning reads GOPATH
	// from it. It defaults to the app's own, counted as read.
	Env func() ([]string, bool)

	// Ready, when set, returns a channel closed once the login shell's environment has
	// first been read (shellenv.Reader.Ready). A run from before a restart waits for it
	// before it is judged (Verifier), so git and go-pre-commit run with the user's own.
	Ready func() <-chan struct{}

	// Runner returns what runs go-pre-commit's status with env, in the folder dir: a scratch
	// repository of the app's own. It defaults to an execpath.Exec with env and dir.
	Runner func(env []string, dir string) execpath.Runner

	// Scratch is the folder the scratch repositories go-pre-commit's status judges a hook
	// in are made in, and removed from; "" is the system's temporary folder.
	Scratch string

	// Terminal opens the user's terminal in a clone, for a row's Open in terminal, and
	// Reveal shows a clone's config in Finder, for its Open the config. Without them,
	// each says it can't.
	Terminal scriptrun.FolderOpener
	Reveal   Revealer

	Home   string
	Clock  clock.Clock
	Events event.Sink
	Logger *slog.Logger
}

// Page is the Hooks page: every clone under the clone root with a go-pre-commit config,
// grouped, and those without one.
type Page struct {
	// Tool is go-pre-commit, as the page heads it.
	Tool ToolStatus `json:"tool"`

	// Root is the clone root, with ~.
	Root string `json:"root"`

	// Repos are the clones with a config, by group, then by name.
	Repos []Repo `json:"repos"`

	// NotSetUp are the clones without one, by name.
	NotSetUp []Clone `json:"notSetUp"`

	// PathWarning says the hook can't find go-pre-commit, and ToolNote that the checks
	// install their own tools.
	PathWarning PathWarning `json:"pathWarning"`
	ToolNote    ToolNote    `json:"toolNote"`

	// CheckedAt is when the clones were last scanned, RFC 3339; Error says why the last
	// scan failed while the one before it is shown.
	CheckedAt string `json:"checkedAt"`
	Error     string `json:"error"`
}

// Clone is a clone without a go-pre-commit config: its folder's name, and its folder,
// with ~.
type Clone struct {
	Name string `json:"name"`
	Dir  string `json:"dir"`
}

// Repo is one clone with a go-pre-commit config, as the page lists it.
type Repo struct {
	// Repo is its origin's repository, "owner/name", or "" when origin isn't on GitHub;
	// Name is what the row shows, the repository or else the folder's name; and Dir is
	// its folder, with ~, which every request names it by.
	Repo string `json:"repo"`
	Name string `json:"name"`
	Dir  string `json:"dir"`

	// Group is where the page lists it, State what its pre-commit hook is, and Action
	// what its button does, or "" for none.
	Group  Group  `json:"group"`
	State  State  `json:"state"`
	Action Action `json:"action"`

	// OtherTool names another tool's hook: husky, pre-commit, or lefthook; "" for one it
	// can't name, or go-pre-commit's own.
	OtherTool string `json:"otherTool"`

	// HooksDir is the folder git runs its hooks from, with ~. Shared reports one other
	// repositories use too, outside the clone's git folder and work tree, and SharedWith
	// how many of the clones under the clone root use it, this one included; an action
	// there needs the user to confirm the folder.
	HooksDir   string `json:"hooksDir"`
	Shared     bool   `json:"shared"`
	SharedWith int    `json:"sharedWith"`

	// AlsoInstalled are the other hook types that carry go-pre-commit's hook, which
	// Uninstall removes too, and Backups the hooks go-pre-commit set aside when it
	// replaced another tool's, the last of which Uninstall puts back.
	AlsoInstalled []string `json:"alsoInstalled"`
	Backups       []string `json:"backups"`

	// CIVersion is the go-pre-commit the repository's CI pins (GO_PRE_COMMIT_VERSION),
	// without a leading v, or "".
	CIVersion string `json:"ciVersion"`

	// Checks are go-pre-commit's checks, each on or off in the repository.
	Checks []CheckSetting `json:"checks"`

	// Busy says what holds the clone now, as a sentence, or "".
	Busy string `json:"busy"`

	// LastChecks is the newest checks run in the clone, zero when there is none.
	LastChecks LastRun `json:"lastChecks"`

	// Error says why the clone's hooks couldn't be read, or "".
	Error string `json:"error"`
}

// CheckSetting is one check and whether the repository turns it on.
type CheckSetting struct {
	Name Check `json:"name"`
	On   bool  `json:"on"`
}

// LastRun is a run as a row mentions it: its ID, zero for none, how it ended, and when,
// RFC 3339.
type LastRun struct {
	ID         int64            `json:"id"`
	Status     scriptrun.Status `json:"status"`
	Summary    string           `json:"summary"`
	FinishedAt string           `json:"finishedAt"`
}

// Service lists the clones and their hooks, runs go-pre-commit's actions and checks in
// them, and reads their results back. The window binds it.
type Service struct {
	clones   Clones
	git      Git
	tools    ToolSource
	locks    LockHolders
	runs     Runs
	history  RunHistory
	places   Places
	memory   Memory
	env      func() ([]string, bool)
	ready    func() <-chan struct{}
	runner   func(env []string, dir string) execpath.Runner
	scratch  string
	terminal scriptrun.FolderOpener
	reveal   Revealer
	home     string
	clock    clock.Clock
	events   event.Sink
	logger   *slog.Logger

	// judged keeps which hook bytes each copy of go-pre-commit said were current.
	judged verdicts

	// scanning runs one scan of each kind at a time; a caller that comes while one runs
	// gets its answer.
	scanning flight.Group[struct{}]

	// lifeMu guards what holds the page's calls and their scans to the app's lifetime
	// (Scans): quitting refuses one once the app quits, and cancels ends each under way, by
	// its number, the last of which was lastHeld. going counts those under way, which Scans
	// waits for.
	lifeMu   sync.Mutex
	quitting bool
	cancels  map[uint64]context.CancelFunc
	lastHeld uint64
	going    sync.WaitGroup

	// mu guards the last scan: the clone root it read, when, the copy of go-pre-commit it
	// judged the hooks with, why the next one failed, the clones with a config and those
	// without, and how many scans of every clone were kept; and the clones to scan again
	// before the page is served, each by its mark's number, of the marks counts.
	mu       sync.Mutex
	has      bool
	root     string
	scanned  time.Time
	judgedBy string
	failure  string
	repos    []scannedRepo
	notSetUp []scannedClone
	scans    uint64
	stale    map[string]uint64
	marks    uint64
}

// NewService returns a Service over cfg.
func NewService(cfg Config) *Service {
	s := &Service{
		clones: cfg.Clones, git: cfg.Git, tools: cfg.Tools, locks: cfg.Locks, runs: cfg.Runs, history: cfg.History,
		places: cfg.Places, memory: cfg.Memory, env: cfg.Env, ready: cfg.Ready, runner: cfg.Runner, scratch: cfg.Scratch,
		terminal: cfg.Terminal, reveal: cfg.Reveal, home: cfg.Home,
		clock: clock.Or(cfg.Clock), events: event.Or(cfg.Events), logger: logging.Or(cfg.Logger),
		stale: map[string]uint64{}, cancels: map[uint64]context.CancelFunc{},
	}

	if s.env == nil {
		s.env = appEnv
	}

	if s.runner == nil {
		s.runner = func(env []string, dir string) execpath.Runner { return execpath.Exec{Env: env, Dir: dir} }
	}

	return s
}

// Repos returns the Hooks page. It serves the last scan while it is under FreshFor old,
// was read from the clone root as set now, and no copy of go-pre-commit took the place of
// the one it judged with, scanning again first only the clones an action or a check ran
// in; refresh, or anything else, scans every clone again and emits event.HooksUpdated. A
// scan that fails keeps the last page, with its error, until one succeeds. A scan runs
// apart from the caller that started it, within ScanTimeout: a caller whose context ends
// stops waiting, and the scan goes on for the next. What holds each clone, and its newest
// checks run, are read for every call. The whole call, its scan included, runs within the
// app's lifetime (Scans): the app's quit ends it, and refuses one after it.
func (s *Service) Repos(ctx context.Context, refresh bool) (Page, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	done, err := s.hold(cancel)
	if err != nil {
		return Page{}, err
	}

	defer done()

	return s.readPage(ctx, refresh)
}

// readPage is Repos, within the app's lifetime.
func (s *Service) readPage(ctx context.Context, refresh bool) (Page, error) {
	root, err := s.clones.Root(ctx)
	if err != nil {
		return Page{}, fmt.Errorf("read the clone root: %w", err)
	}

	tool := s.tool(ctx)

	s.mu.Lock()
	age := s.clock.Now().Sub(s.scanned)
	fresh := s.has && s.root == root && s.judgedBy == judgeKey(tool) && age >= 0 && age < FreshFor
	s.mu.Unlock()

	key, scan := scanAllKey+root, func(ctx context.Context) error { return s.scanAll(ctx, root, tool) }

	if fresh && !refresh {
		key, scan = scanStaleKey+root, func(ctx context.Context) error { return s.scanStale(ctx, tool) }
	}

	detached := func(ctx context.Context) (struct{}, error) {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ScanTimeout)
		defer cancel()

		done, refused := s.hold(cancel) // the app's quit ends it too
		if refused != nil {
			return struct{}{}, refused
		}

		defer done()

		return struct{}{}, scan(ctx)
	}

	if _, err = s.scanning.Do(ctx, key, detached); err != nil {
		return Page{}, err
	}

	return s.page(ctx, root, tool), nil
}

// The keys a scan runs under, one at a time, with the clone root after them: a scan of
// every clone, and a scan again of the clones an action or a check ran in.
const (
	scanAllKey   = "all\x00"
	scanStaleKey = "stale\x00"
)

// scanAll scans every clone under root and keeps what it found, with the marks of the
// clones to scan again that were there when it began cleared; a mark set while it ran
// stays. A scan that fails keeps the last one, with why, unless there is none to keep, or
// root changed. A scan cut short, as by ScanTimeout, keeps nothing.
func (s *Service) scanAll(ctx context.Context, root string, tool ToolFacts) error {
	s.mu.Lock()
	marked := maps.Clone(s.stale)
	s.mu.Unlock()

	repos, others, err := s.scan(ctx, root, tool)
	if err == nil {
		err = ctx.Err()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if ctx.Err() != nil {
		return ctx.Err()
	}

	if err != nil {
		if !s.has || s.root != root {
			return err
		}

		s.logger.WarnContext(ctx, "couldn't scan the clones for hooks; showing the last scan", "error", err)
		s.failure = uierr.Message(err)
		s.events.Emit(event.HooksUpdated{})

		return nil
	}

	s.has, s.root, s.scanned, s.failure, s.judgedBy = true, root, s.clock.Now(), "", judgeKey(tool)
	s.repos, s.notSetUp = repos, others
	s.scans++
	s.unmark(marked)
	s.events.Emit(event.HooksUpdated{})

	return nil
}

// scanStale scans again the clones an action or a check ran in, reading their configs
// afresh, and puts them in place, clearing the marks it scanned for. A scan cut short keeps
// nothing, and nor does one that a scan of every clone overtook.
func (s *Service) scanStale(ctx context.Context, tool ToolFacts) error {
	s.mu.Lock()
	scans := s.scans
	marked := map[string]uint64{}

	var clones []configured

	for _, r := range s.repos {
		if n, ok := s.stale[r.key]; ok {
			marked[r.key] = n
			clones = append(clones, configured{dir: r.dir})
		}
	}

	s.mu.Unlock()

	if len(clones) == 0 {
		return nil
	}

	for i := range clones {
		clones[i].cfg = readConfig(clones[i].dir)
	}

	fresh := s.scanRepos(ctx, clones, tool)
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.scans != scans {
		return nil // a scan of every clone came meanwhile, with its own reads
	}

	for _, f := range fresh {
		if i := slices.IndexFunc(s.repos, func(r scannedRepo) bool { return r.key == f.key }); i >= 0 {
			s.repos[i] = f
		}
	}

	s.unmark(marked)
	countShared(s.repos, s.notSetUp)

	return nil
}

// unmark clears the marks of the clones to scan again that marked holds, unless a newer
// mark took a clone's place since. The caller holds mu.
func (s *Service) unmark(marked map[string]uint64) {
	for key, n := range marked {
		if s.stale[key] == n {
			delete(s.stale, key)
		}
	}
}

// markStale has the next Repos scan the clone at dir again, and tells the window.
func (s *Service) markStale(dir string) {
	s.mu.Lock()
	s.marks++
	s.stale[paths.Resolved(dir)] = s.marks
	s.mu.Unlock()

	s.events.Emit(event.HooksUpdated{})
}

// page returns the page from the last scan, with what holds each clone and its newest
// checks run read now.
func (s *Service) page(ctx context.Context, root string, tool ToolFacts) Page {
	env, _ := s.env()
	last := s.lastChecks(ctx)
	note := s.toolNote(ctx, tool)

	s.mu.Lock()
	defer s.mu.Unlock()

	out := Page{
		Tool: tool.status(s.home), Root: paths.Tilde(root, s.home), Repos: make([]Repo, 0, len(s.repos)),
		NotSetUp: make([]Clone, 0, len(s.notSetUp)), PathWarning: pathWarning(tool, env, s.home), ToolNote: note,
		CheckedAt: stamp(s.scanned), Error: s.failure,
	}

	for _, r := range s.repos {
		row := r.row
		row.AlsoInstalled, row.Backups, row.Checks = slices.Clone(row.AlsoInstalled), slices.Clone(row.Backups),
			slices.Clone(row.Checks)
		row.LastChecks = last[row.Dir]

		if s.locks != nil {
			if h, busy := s.locks.Holder(r.dir); busy {
				row.Busy = h.Sentence()
			}
		}

		out.Repos = append(out.Repos, row)
	}

	slices.SortStableFunc(out.Repos, func(a, b Repo) int {
		return cmp.Or(cmp.Compare(groupRank(a.Group), groupRank(b.Group)), compareNames(a.Name, b.Name))
	})

	for _, c := range s.notSetUp {
		out.NotSetUp = append(out.NotSetUp, c.clone)
	}

	return out
}

// toolNote works out the note about the tools the checks install: shown while the copy in
// use lacks FeatureVerifiedTools, and open until a checks run ended in the app.
func (s *Service) toolNote(ctx context.Context, tool ToolFacts) ToolNote {
	if tool.Path == "" || tool.Has(integrations.FeatureVerifiedTools) {
		return ToolNote{}
	}

	ran := false

	if s.memory != nil {
		v, ok, err := s.memory.Get(ctx, checksRanKey)
		ran = err == nil && ok && v == "true"
	}

	return ToolNote{Show: true, Open: !ran}
}

// lastChecks returns the newest checks run in each clone, by its folder with ~.
func (s *Service) lastChecks(ctx context.Context) map[string]LastRun {
	out := map[string]LastRun{}

	if s.history == nil {
		return out
	}

	runs, err := s.history.Runs(ctx, scriptrun.Filter{Kinds: []scriptrun.Kind{scriptrun.KindChecks}})
	if err != nil {
		s.logger.WarnContext(ctx, "couldn't read the checks runs", "error", err)

		return out
	}

	for _, r := range runs { // newest first
		if _, seen := out[r.Dir]; !seen {
			out[r.Dir] = LastRun{ID: r.ID, Status: r.Status, Summary: r.Summary, FinishedAt: r.FinishedAt}
		}
	}

	return out
}

// tool returns what the app knows of go-pre-commit now.
func (s *Service) tool(ctx context.Context) ToolFacts {
	if s.tools == nil {
		return ToolFacts{Features: []integrations.Feature{}, Copies: []string{}}
	}

	return s.tools.HooksTool(ctx)
}

// judgeKey names the copy of go-pre-commit a scan judged the hooks with, and whether it
// could be used: another, or one turned on or off, makes the scan out of date.
func judgeKey(tool ToolFacts) string {
	return fmt.Sprintf("%s %t", tool.key(), tool.Usable())
}

// groupRank orders the groups as the page lists them.
func groupRank(g Group) int {
	return slices.Index([]Group{GroupAttention, GroupReady, GroupOff}, g)
}

// compareNames orders names case-insensitively, then as written.
func compareNames(a, b string) int {
	return cmp.Or(cmp.Compare(strings.ToLower(a), strings.ToLower(b)), cmp.Compare(a, b))
}

// stamp writes t as RFC 3339, or "" for the zero time.
func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	return clock.Format(t)
}

// appEnv is the app's own environment, counted as read, for a Service with no Env.
func appEnv() ([]string, bool) {
	return os.Environ(), true
}

// display names a clone for the user: its repository, or else its folder's name.
func display(name repo.Name, dir string) string {
	if name.Valid() {
		return name.String()
	}

	return filepath.Base(dir)
}
