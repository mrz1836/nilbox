package hooks

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// errNoConfig is a clone that had a go-pre-commit config when it was listed, and has none
// now.
var errNoConfig = errors.New("the clone has no go-pre-commit config")

// scannedRepo is a clone with a go-pre-commit config, as the scan found it.
type scannedRepo struct {
	// dir is the clone's folder, absolute, and key the same resolved, which clone locks
	// and stale marks go by.
	dir string
	key string

	// hooks is the folder git runs its hooks from, resolved, or "" when it couldn't be
	// read.
	hooks string

	row Repo
}

// scannedClone is a clone without a go-pre-commit config.
type scannedClone struct {
	dir   string
	hooks string
	clone Clone
}

// scan lists the clones under root and scans each with a config, four at a time. When
// one's hooks folder is shared, it reads the others' too, so the count of clones that use
// it is right.
func (s *Service) scan(ctx context.Context, root string, tool ToolFacts) ([]scannedRepo, []scannedClone, error) {
	dirs, err := s.clones.Dirs(ctx, root)
	if err != nil {
		return nil, nil, uierr.New("Couldn't list the clones in "+paths.Tilde(root, s.home)+".", err)
	}

	var withConfig []configured

	others := []scannedClone{}

	for _, dir := range dirs {
		if cfg := readConfig(dir); cfg.present {
			withConfig = append(withConfig, configured{dir: dir, cfg: cfg})

			continue
		}

		others = append(others, scannedClone{
			dir: dir, clone: Clone{Name: filepath.Base(dir), Dir: paths.Tilde(dir, s.home)},
		})
	}

	repos := s.scanRepos(ctx, withConfig, tool)

	if slices.ContainsFunc(repos, func(r scannedRepo) bool { return r.row.Shared }) {
		s.readHooksFolders(ctx, others)
	}

	countShared(repos, others)

	return repos, others, nil
}

// configured is a clone with a go-pre-commit config: its folder, and the config as it was
// read when the clone was listed.
type configured struct {
	dir string
	cfg repoConfig
}

// scanRepos scans clones, a few at a time, in their order.
func (s *Service) scanRepos(ctx context.Context, clones []configured, tool ToolFacts) []scannedRepo {
	out := make([]scannedRepo, len(clones))
	dirs := make([]string, len(clones))

	for i, c := range clones {
		dirs[i] = c.dir
	}

	parallel(dirs, func(i int, _ string) { out[i] = s.scanRepo(ctx, clones[i], tool) })

	return out
}

// readHooksFolders reads where git runs the hooks of each clone without a config, a few at
// a time, to count the clones that share a folder. One it can't read counts for none.
func (s *Service) readHooksFolders(ctx context.Context, others []scannedClone) {
	dirs := make([]string, len(others))
	for i, c := range others {
		dirs[i] = c.dir
	}

	parallel(dirs, func(i int, dir string) {
		if hp, err := s.git.HookPaths(ctx, dir); err == nil {
			others[i].hooks = paths.Resolved(hp.Hooks)
		}
	})
}

// parallel calls fn for each of dirs, scanParallel at a time, and returns once all have.
func parallel(dirs []string, fn func(i int, dir string)) {
	var wg sync.WaitGroup

	slots := make(chan struct{}, scanParallel)

	for i, dir := range dirs {
		slots <- struct{}{}

		wg.Go(func() {
			defer func() { <-slots }()

			fn(i, dir)
		})
	}

	wg.Wait()
}

// scanRepo scans the clone c: its origin, where git runs its hooks, and its hooks there,
// with its config as c holds it. What it can't read, the row says.
func (s *Service) scanRepo(ctx context.Context, c configured, tool ToolFacts) scannedRepo {
	dir, cfg := c.dir, c.cfg

	name, onHub, err := s.clones.Origin(ctx, dir)
	if err != nil || !onHub {
		name = repo.Name{}
	}

	out := scannedRepo{dir: dir, key: paths.Resolved(dir)}
	row := &out.row
	*row = Repo{
		Repo: repoText(name), Name: display(name, dir), Dir: paths.Tilde(dir, s.home), AlsoInstalled: []string{},
		Backups: []string{}, CIVersion: cfg.ciVersion(), Checks: cfg.settings(),
	}

	l := s.look(ctx, dir)

	switch {
	case !cfg.present:
		row.State, row.Error = StateUnreadable, uierr.Message(uierr.New("There's no go-pre-commit config here anymore.",
			errNoConfig))
	case l.err != nil:
		row.State, row.Error = StateUnreadable, uierr.Message(l.err)
	default:
		out.hooks = paths.Resolved(l.paths.Hooks)
		row.HooksDir, row.Shared = paths.Tilde(l.paths.Hooks, s.home), l.shared
		row.AlsoInstalled = slices.DeleteFunc(slices.Clone(l.marked), func(t string) bool { return t == hookPreCommit })
		row.Backups = l.backups
		row.State, row.OtherTool = s.judge(ctx, l.hook, tool)

		if l.hook.err != nil {
			row.Error = uierr.Message(uierr.New("Couldn't read this clone's pre-commit hook.", l.hook.err))
		}
	}

	classify(row, cfg.enabled())

	return out
}

// classify gives a row its group and its button.
func classify(row *Repo, enabled bool) {
	switch row.State {
	case StateMissing:
		row.Action = ActionInstall
	case StateOutdated:
		row.Action = ActionUpdate
	case StateNotExecutable:
		row.Action = ActionReinstall
	case StateOther:
		row.Action = ActionReplace
	case StateReady, StateUnknown, StateUnreadable:
		row.Action = ""
	}

	switch {
	case !enabled:
		row.Group = GroupOff
	case row.State == StateReady || row.State == StateUnknown:
		row.Group = GroupReady
	default:
		row.Group = GroupAttention
	}
}

// countShared counts, for each clone with a shared hooks folder, the clones under the clone
// root that use that folder, with or without a config.
func countShared(repos []scannedRepo, others []scannedClone) {
	count := map[string]int{}

	for _, r := range repos {
		if r.hooks != "" {
			count[r.hooks]++
		}
	}

	for _, c := range others {
		if c.hooks != "" {
			count[c.hooks]++
		}
	}

	for i := range repos {
		repos[i].row.SharedWith = 0
		if repos[i].row.Shared {
			repos[i].row.SharedWith = count[repos[i].hooks]
		}
	}
}

// sharedCount returns how many clones the last scan found using the hooks folder hooks,
// resolved, or 0 when it found none.
func (s *Service) sharedCount(hooks string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	n := 0

	for _, r := range s.repos {
		if r.hooks == hooks {
			n++
		}
	}

	for _, c := range s.notSetUp {
		if c.hooks == hooks {
			n++
		}
	}

	return n
}

// clonesHooks is a clone's hooks as git and the files say now.
type clonesHooks struct {
	// paths are where git runs the clone's hooks from, and shared reports a folder outside
	// both its git folder and its work tree.
	paths  gitread.HookPaths
	shared bool

	// hook is its pre-commit hook, marked the hook types that carry go-pre-commit's
	// marker, and backups the hooks go-pre-commit set aside.
	hook    hookFile
	marked  []string
	backups []string

	// err says why the hooks folder couldn't be read.
	err error
}

// look reads the hooks of the clone at dir.
func (s *Service) look(ctx context.Context, dir string) clonesHooks {
	hp, err := s.git.HookPaths(ctx, dir)
	if err != nil {
		return clonesHooks{err: uierr.New("Couldn't read where git runs this clone's hooks.", err)}
	}

	return clonesHooks{
		paths: hp, shared: isShared(hp), hook: readHook(filepath.Join(hp.Hooks, hookPreCommit)),
		marked: markedTypes(hp.Hooks), backups: backups(hp.Hooks),
	}
}

// isShared reports a hooks folder other repositories can share: outside the clone's
// common git folder, and outside its work tree, where a tool such as husky 9 keeps its
// hooks for this clone alone.
func isShared(hp gitread.HookPaths) bool {
	hooks := paths.Resolved(hp.Hooks)

	return !within(hooks, paths.Resolved(hp.CommonDir)) && !within(hooks, paths.Resolved(hp.Toplevel))
}

// within reports whether path is dir or inside it; both are clean.
func within(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, strings.TrimSuffix(dir, string(filepath.Separator))+string(filepath.Separator))
}

// runEnv is the environment go-pre-commit runs with: no update banner and no color, as
// every run of a tool gets it (integrations.Definition.RunEnv).
func runEnv(base []string, runsIn integrationprefs.RunsIn) []string {
	return integrations.GoPreCommit().RunEnv(base, runsIn)
}

// repoText writes a repository as "owner/name", or "" for none.
func repoText(name repo.Name) string {
	if !name.Valid() {
		return ""
	}

	return name.String()
}
