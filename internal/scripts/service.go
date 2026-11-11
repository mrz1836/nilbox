package scripts

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/flight"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/shellenv"
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

// Git reads which branch a clone is on: *gitread.Git.
type Git interface {
	CurrentBranch(ctx context.Context, dir string) (string, error)
}

// DefaultBranches reads the default branch the app last stored for a repository, "" when
// it stored none. Core adapts the inbox's store.
type DefaultBranches interface {
	DefaultBranch(ctx context.Context, name repo.Name) (string, error)
}

// ToolSource reads what the app knows of the tools: magex, and whether mage-x and
// go-pre-commit are on. Core adapts the integrations service, with ToolsOf. It never
// fails: a tool it can't read is missing, and off.
type ToolSource interface {
	Tools(ctx context.Context) Tools
}

// Settings reads the integrations' settings: which sources Repo scripts lists, and where
// runs go. Core adapts the settings.
type Settings interface {
	Integrations(ctx context.Context) (integrationprefs.Config, error)
}

// Runs starts a run: the scriptrun engine.
type Runs interface {
	Start(ctx context.Context, spec scriptrun.Spec) (int64, error)
}

// RunHistory reads runs back from the runner's history: the scriptrun Service.
type RunHistory interface {
	Runs(ctx context.Context, filter scriptrun.Filter) ([]scriptrun.Run, error)
}

// Config holds a Service's dependencies. Clones is required; a run also needs Runs, and
// trust the database.
type Config struct {
	Clones   Clones
	Git      Git
	Branches DefaultBranches
	Tools    ToolSource
	Settings Settings
	Runs     Runs
	History  RunHistory

	// DB holds script_trust.
	DB *sql.DB

	// Env returns the login shell's environment, and whether it was read
	// (shellenv.Reader.Env): runs get it, and ${env:…} reads it. It defaults to the app's
	// own. Shell returns the user's login shell, which runs a shell command
	// (shellenv.Reader.Shell); it defaults to $SHELL, or /bin/sh.
	Env   func() ([]string, bool)
	Shell func() string

	// Runner returns what runs magex's help:commands with env, in the folder dir: a
	// scratch folder of the app's own, made under Scratch ("" is the system's temporary
	// folder). It defaults to an execpath.Exec with env and dir.
	Runner  func(env []string, dir string) execpath.Runner
	Scratch string

	Home   string
	Clock  clock.Clock
	Logger *slog.Logger
}

// Service lists each clone's scripts and runs them, asking first for a command from the
// repository's own files. The window binds it.
type Service struct {
	clones   Clones
	git      Git
	branches DefaultBranches
	tools    ToolSource
	settings Settings
	runs     Runs
	history  RunHistory
	trust    trustStore
	env      func() ([]string, bool)
	shell    func() string
	runner   func(env []string, dir string) execpath.Runner
	scratch  string
	home     string
	logger   *slog.Logger

	// listing lists one copy of magex's built-ins at a time.
	listing flight.Group[builtins]

	// mu guards the built-ins listed per copy of magex, and the magefiles read per clone,
	// each with the fingerprint it was read at.
	mu     sync.Mutex
	listed map[string]listedBuiltins
	mages  map[string]keptMagefiles
}

// keptMagefiles is a clone's magefiles as last read, and what they looked like then.
type keptMagefiles struct {
	look  string
	files magefiles
}

// NewService returns a Service over cfg.
func NewService(cfg Config) *Service {
	s := &Service{
		clones: cfg.Clones, git: cfg.Git, branches: cfg.Branches, tools: cfg.Tools, settings: cfg.Settings,
		runs: cfg.Runs, history: cfg.History, trust: newTrustStore(cfg.DB, cfg.Clock), env: cfg.Env, shell: cfg.Shell,
		runner: cfg.Runner, scratch: cfg.Scratch, home: cfg.Home, logger: logging.Or(cfg.Logger),
		listed: map[string]listedBuiltins{}, mages: map[string]keptMagefiles{},
	}

	if s.env == nil {
		s.env = func() ([]string, bool) { return os.Environ(), true }
	}

	if s.shell == nil {
		s.shell = appShell
	}

	if s.runner == nil {
		s.runner = func(env []string, dir string) execpath.Runner { return execpath.Exec{Env: env, Dir: dir} }
	}

	return s
}

// appShell is $SHELL from the app's own environment when it is absolute, else /bin/sh.
func appShell() string {
	if sh := shellenv.Lookup(os.Environ(), "SHELL"); filepath.IsAbs(sh) {
		return sh
	}

	return chainShell
}

// Repos returns the clones under the clone root, for the page's picker: the ones a script
// ran in most recently first, then the rest by name.
func (s *Service) Repos(ctx context.Context) ([]Clone, error) {
	l, err := s.listClones(ctx)
	if err != nil {
		return nil, err
	}

	recent := s.recent(ctx)

	out := make([]Clone, 0, len(l.dirs))
	for _, d := range l.dirs {
		out = append(out, Clone{Name: filepath.Base(d), Dir: paths.Tilde(d, s.home)})
	}

	slices.SortStableFunc(out, func(a, b Clone) int {
		ra, okA := recent[a.Dir]
		if !okA {
			ra = len(recent)
		}

		rb, okB := recent[b.Dir]
		if !okB {
			rb = len(recent)
		}

		return cmp.Or(cmp.Compare(ra, rb), cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)),
			cmp.Compare(a.Name, b.Name))
	})

	return out, nil
}

// List returns Repo scripts for the clone at dir, as the picker lists it, with ~: its
// sources in the page's order, as the settings turn them on, each script with what
// running it asks first, or the page marked off while the scripts are. It runs nothing in
// the clone: its files are read, and magex's built-ins are listed in a folder of the
// app's own, once per copy of magex, a list that failed included, until refresh.
func (s *Service) List(ctx context.Context, dir string, refresh bool) (Page, error) {
	c, err := s.cloneAt(ctx, dir)
	if err != nil {
		return Page{}, err
	}

	cfg, err := s.config(ctx)
	if err != nil {
		return Page{}, err
	}

	tools := s.toolsNow(ctx)
	page := Page{
		Repo: repoText(c.name), Name: c.display(), Dir: paths.Tilde(c.dir, s.home), Switches: cfg.ScriptSources,
		Sections: []Section{},
	}

	if page.Why = scriptsOff(cfg, tools); page.Why != "" {
		page.Off = true

		return page, nil
	}

	if refresh {
		s.forgetBuiltins(tools.Magex)
	}

	records, err := s.trust.forRepo(ctx, c.name)
	if err != nil {
		return Page{}, err
	}

	page.Sections = append(page.Sections, Section{Source: SourceYours, Title: "Yours", Entries: []Entry{}, Common: []string{}})
	page.Branch, page.FromBranch = s.branch(ctx, c)

	for _, src := range []Source{SourceT3, SourceTasks, SourceMagex} {
		page.Sections = append(page.Sections, s.section(ctx, c, src, cfg, tools.Magex, records))
	}

	return page, nil
}

// recent ranks the clones scripts ran in, by folder with ~: 0 for the newest run's.
func (s *Service) recent(ctx context.Context) map[string]int {
	out := map[string]int{}

	if s.history == nil {
		return out
	}

	runs, err := s.history.Runs(ctx, scriptrun.Filter{Kinds: []scriptrun.Kind{scriptrun.KindScript}})
	if err != nil {
		s.logger.WarnContext(ctx, "couldn't read the script runs", "error", err)
	}

	for _, r := range runs { // newest first
		if _, seen := out[r.Dir]; !seen {
			out[r.Dir] = len(out)
		}
	}

	return out
}

// section reads one source's scripts in the clone for the page.
func (s *Service) section(ctx context.Context, c clone, src Source, cfg integrationprefs.Config, tool ToolFacts,
	records map[trustKey]trustRecord,
) Section {
	sec := Section{Source: src, Title: sourceTitle(src), Entries: []Entry{}, Common: []string{}}

	switch {
	case sourceOff(cfg, src) != "":
		sec.Off = true

		return sec
	case src == SourceMagex && !tool.Usable():
		sec.Note = tool.why()

		return sec
	case src == SourceMagex:
		sec.Note = "magex targets run the repository's own code and its .github/env settings, as any build does."
	}

	got, err := s.read(ctx, c, src, tool, false)
	sec.Found, sec.Error = got.found, uierr.Message(err)

	for _, sc := range got.scripts {
		sec.Entries = append(sec.Entries, withTrust(sc, records, c.display()))
	}

	for _, name := range commonTargets() {
		if got.builtins.names[name] {
			sec.Common = append(sec.Common, ref(SourceMagex, name))
		}
	}

	return sec
}

// sourceRead is one source as read: whether the clone has it, its scripts, and for magex
// its built-ins.
type sourceRead struct {
	found    bool
	scripts  []script
	builtins builtins
}

// read reads src's scripts in the clone c. fresh reads the magefiles from their bytes, as
// a run does, rather than as last read; t3.json and tasks.json are read every time. An
// error says why the source, or part of it, couldn't be read.
func (s *Service) read(ctx context.Context, c clone, src Source, tool ToolFacts, fresh bool) (sourceRead, error) {
	var (
		out sourceRead
		err error
	)

	switch src {
	case SourceT3:
		out.scripts, out.found, err = readT3(c.dir)
	case SourceTasks:
		out.scripts, out.found, err = readTasks(c.dir)
	case SourceMagex:
		if !tool.Usable() {
			return out, uierr.New(tool.why(), ErrCantRun)
		}

		if out.builtins, err = s.builtinsOf(ctx, tool); err != nil {
			return out, uierr.New("Couldn't list magex's targets.", err)
		}

		mf := s.magefilesOf(c.dir, fresh)
		out.found, out.scripts = true, magexScripts(out.builtins, mf)

		if mf.readErr != nil {
			err = uierr.New(magefilesMessage(mf.readErr), mf.readErr)
		}
	case SourceYours:
	}

	return out, err
}

// magefilesOf returns the clone at dir's magefiles: as last read while they look the same
// on disk, else read again; fresh always reads them again, from their bytes.
func (s *Service) magefilesOf(dir string, fresh bool) magefiles {
	look := fingerprint(dir)

	if !fresh {
		s.mu.Lock()
		kept, ok := s.mages[dir]
		s.mu.Unlock()

		if ok && kept.look == look {
			return kept.files
		}
	}

	mf := readMagefiles(dir)

	s.mu.Lock()
	s.mages[dir] = keptMagefiles{look: look, files: mf}
	s.mu.Unlock()

	return mf
}

// find returns the script called name in src, read afresh when fresh, or says it is gone.
func (s *Service) find(ctx context.Context, c clone, src Source, name string, tool ToolFacts, fresh bool,
) (script, error) {
	got, err := s.read(ctx, c, src, tool, fresh)

	for _, sc := range got.scripts {
		if sc.name == name {
			return sc, nil
		}
	}

	if err != nil {
		return script{}, err
	}

	return script{}, uierr.New(fmt.Sprintf("%q is no longer in %s.", name, sourceFile(src)),
		fmt.Errorf("%w: %s", ErrNotFound, ref(src, name)))
}

// config reads the integrations' settings.
func (s *Service) config(ctx context.Context) (integrationprefs.Config, error) {
	if s.settings == nil {
		return integrationprefs.Decode(nil), nil
	}

	cfg, err := s.settings.Integrations(ctx)
	if err != nil {
		return integrationprefs.Config{}, fmt.Errorf("read the settings: %w", err)
	}

	return cfg, nil
}

// toolsNow returns what the app knows of the tools now.
func (s *Service) toolsNow(ctx context.Context) Tools {
	if s.tools == nil {
		return Tools{}
	}

	return s.tools.Tools(ctx)
}

// tool returns what the app knows of magex now.
func (s *Service) tool(ctx context.Context) ToolFacts {
	return s.toolsNow(ctx).Magex
}

// scriptsOff says, while the Scripts integration is off, so; "" while it is on. Its
// switch's automatic is on while mage-x or go-pre-commit is.
func scriptsOff(cfg integrationprefs.Config, t Tools) string {
	sw := cfg.Switches[integrationprefs.Scripts]

	switch {
	case integrations.ScriptsOn(sw, t.MageXOn, t.GoPreCommitOn):
		return ""
	case sw == integrationprefs.SwitchOff:
		return "Scripts are turned off in Settings → Integrations."
	default:
		return "Scripts turn on with mage-x or go-pre-commit. Turn either on, or Scripts itself, in " +
			"Settings → Integrations."
	}
}

// branch returns the branch the clone is on, and that branch again when it isn't the
// repository's default: the one the app stored, or else main or master.
func (s *Service) branch(ctx context.Context, c clone) (branch, from string) {
	if s.git == nil {
		return "", ""
	}

	branch, err := s.git.CurrentBranch(ctx, c.dir)
	if err != nil || branch == "" {
		return "", ""
	}

	if s.isDefault(ctx, c, branch) {
		return branch, ""
	}

	return branch, branch
}

// isDefault reports whether branch is the clone's repository's default branch: the one the
// app stored, or else main or master.
func (s *Service) isDefault(ctx context.Context, c clone, branch string) bool {
	def := ""
	if s.branches != nil && c.name.Valid() {
		def, _ = s.branches.DefaultBranch(ctx, c.name)
	}

	if def == "" {
		return branch == "main" || branch == "master"
	}

	return branch == def
}

// sourceOff says, for a source the settings turn off, so; "" when it is on.
func sourceOff(cfg integrationprefs.Config, src Source) string {
	switch {
	case (src == SourceT3 || src == SourceTasks) && !cfg.ScriptSources.RepoFiles:
		return "Commands from repository files are turned off in Settings → Integrations."
	case src == SourceMagex && !cfg.ScriptSources.MagexTargets:
		return "magex targets are turned off in Settings → Integrations."
	default:
		return ""
	}
}

// sourceTitle is what the page heads a source with.
func sourceTitle(src Source) string {
	switch src {
	case SourceT3:
		return "From " + t3File
	case SourceTasks:
		return "From " + tasksFile
	case SourceMagex:
		return "magex"
	case SourceYours:
	}

	return "Yours"
}

// sourceFile names where a source's scripts are, as a sentence names it.
func sourceFile(src Source) string {
	switch src {
	case SourceT3:
		return t3File
	case SourceTasks:
		return tasksFile
	case SourceMagex:
		return "magex's targets"
	case SourceYours:
	}

	return "your scripts"
}

// clone is a clone a request names: its folder, absolute, and its origin's repository on
// GitHub, or the zero Name.
type clone struct {
	dir  string
	name repo.Name
}

// display names the clone for the user: its repository, or else its folder's name.
func (c clone) display() string {
	if c.name.Valid() {
		return c.name.String()
	}

	return filepath.Base(c.dir)
}

// listing is the clone root as set when it was read, and the clones under it.
type listing struct {
	root string
	dirs []string
}

// listClones reads the clone root as set now and lists the clones under it.
func (s *Service) listClones(ctx context.Context) (listing, error) {
	root, err := s.clones.Root(ctx)
	if err != nil {
		return listing{}, fmt.Errorf("read the clone root: %w", err)
	}

	dirs, err := s.clones.Dirs(ctx, root)
	if err != nil {
		return listing{}, fmt.Errorf("list the clones: %w", err)
	}

	return listing{root: root, dirs: dirs}, nil
}

// cloneAt finds the clone a request names by its folder, with ~: only a clone directly
// under the clone root is one. Its repository is its origin's.
func (s *Service) cloneAt(ctx context.Context, shown string) (clone, error) {
	l, err := s.listClones(ctx)
	if err != nil {
		return clone{}, err
	}

	dir, err := paths.ExpandHome(shown, s.home)
	if err != nil {
		return clone{}, fmt.Errorf("%w: the folder %q: %w", ErrInvalidRequest, shown, err)
	}

	i := slices.Index(l.dirs, dir)
	if i < 0 {
		i = slices.IndexFunc(l.dirs, func(d string) bool { return paths.Same(d, dir) })
	}

	if i < 0 {
		return clone{}, uierr.New(fmt.Sprintf("%s isn't a clone in %s.", paths.Tilde(dir, s.home),
			paths.Tilde(l.root, s.home)), fmt.Errorf("%w: %s", ErrNoClone, dir))
	}

	c := clone{dir: l.dirs[i]}

	if name, onHub, originErr := s.clones.Origin(ctx, c.dir); originErr == nil && onHub {
		c.name = name
	}

	return c, nil
}

// repoText writes a repository as "owner/name", or "" for none.
func repoText(name repo.Name) string {
	if !name.Valid() {
		return ""
	}

	return name.String()
}
