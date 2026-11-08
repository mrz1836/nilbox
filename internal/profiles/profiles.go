// Package profiles discovers the user's coding-agent profiles and serves them to the UI.
//
// A profile is a config directory one coding agent can run with. Each agent's default
// directory (~/.claude for Claude Code, ~/.codex for Codex) becomes that agent's "default"
// profile, which runs without the agent's config variable (CLAUDE_CONFIG_DIR, CODEX_HOME)
// so its own default login is used; every ~/.<dir>_<name> or ~/.<dir>-<name> directory that
// holds that agent's config becomes the agent's profile <name>, run with the variable set
// to that directory. Other tools name their own folders the same way (~/.codexbar,
// ~/.claude-<name>), so a directory counts only when it holds what the agent writes into a
// config directory (see agent.Agent.Markers).
//
// Discovery runs on every start and never changes a stored profile: one the user edited
// keeps its settings, and one whose directory disappeared keeps its row. Prune removes
// only rows that hold nothing worth keeping: unedited profiles whose directory is still
// there but holds no agent config.
//
// The Store owns the stored profiles; every other package reads them through it, or
// through a narrow interface it satisfies. The Service is what the window binds.
package profiles

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/paths"
)

const (
	// DefaultName is the profile backed by an agent's default config directory.
	DefaultName = "default"

	// PermissionFull means the profile starts with full access (Claude's
	// --dangerously-skip-permissions, Codex's --dangerously-bypass-approvals-and-sandbox).
	PermissionFull = "full"
)

// ErrProfileNotFound is returned when the agent has no profile with the requested name.
var ErrProfileNotFound = errors.New("profile not found")

// namePattern is what a discovered profile name must look like.
var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Profile is one launch profile as the UI shows it. Every field is a plain value.
type Profile struct {
	// Agent is the coding agent the profile runs.
	Agent agent.ID `json:"agent"`

	// Name identifies the profile among the agent's profiles.
	Name string `json:"name"`

	// ConfigDir is the absolute config directory, or "" for the default profile.
	ConfigDir string `json:"configDir"`

	// Model is the model the profile runs ("" for the agent's default).
	Model string `json:"model"`

	// Effort is the effort level ("" for the agent's default).
	Effort string `json:"effort"`

	// PermissionMode is "full", one of the agent's permission modes, or "" for the
	// agent's default.
	PermissionMode string `json:"permissionMode"`

	// PlanMode starts the session planning only: Claude's plan mode, Codex's read-only
	// sandbox.
	PlanMode bool `json:"planMode"`

	// AddDirs are extra directories passed with --add-dir.
	AddDirs []string `json:"addDirs"`

	// ExtraArgs are extra arguments, one argv element each.
	ExtraArgs []string `json:"extraArgs"`

	// Env holds extra environment variables for the session.
	Env map[string]string `json:"env"`

	// Folder is the profile's config directory as shown to people, with ~ for the home
	// directory: "~/.claude" for Claude's default profile. Only List sets it.
	Folder string `json:"folder"`

	// Missing reports that the config directory no longer exists. Only List sets it.
	Missing bool `json:"missing"`

	// SignedIn reports that the agent can sign in with the profile: its sign-in file is in
	// the config directory, or its env sets a key the agent signs in with. It is always
	// true for an agent whose sign-in the app does not check. Only List sets it.
	SignedIn bool `json:"signedIn"`
}

// Service serves the stored profiles and the agents they run.
type Service struct {
	store  *Store
	home   string
	finder execpath.Finder
	models *modelsCache
}

// NewService returns a Service over the profiles in store. home is where each agent's
// default profile directory lives, and is shown as ~ in each profile's Folder. finder
// finds each agent's program for Agents; nil reports every agent as not installed.
func NewService(store *Store, home string, finder execpath.Finder) *Service {
	return &Service{store: store, home: home, finder: finder, models: newModelsCache()}
}

// List returns every stored profile, grouped by agent in the agents' order, each agent's
// default first and the rest by name, each with its Folder, whether that folder is
// missing, and whether it is signed in.
func (s *Service) List(ctx context.Context) ([]Profile, error) {
	profiles, err := s.store.All(ctx)
	if err != nil {
		return nil, err
	}

	for i := range profiles {
		s.describeFolder(&profiles[i])
	}

	return profiles, nil
}

// describeFolder sets a profile's Folder, Missing, and SignedIn from its config directory.
func (s *Service) describeFolder(p *Profile) {
	dir := configDirOf(*p, s.home)

	p.Folder = paths.Tilde(dir, s.home)

	_, err := os.Stat(dir)
	p.Missing = errors.Is(err, fs.ErrNotExist)
	p.SignedIn = signedIn(*p, dir)
}

// configDirOf returns the directory a profile runs with: its ConfigDir, or its agent's
// default directory under home for the default profile.
func configDirOf(p Profile, home string) string {
	if p.ConfigDir != "" {
		return p.ConfigDir
	}

	a, err := agent.Lookup(p.Agent)
	if err != nil {
		return ""
	}

	return filepath.Join(home, a.ConfigDir)
}

// signedIn reports whether the agent can sign in with a profile whose config directory is
// dir. Only the sign-in file's presence is checked: it holds credentials, so it is never
// read.
func signedIn(p Profile, dir string) bool {
	a, err := agent.Lookup(p.Agent)
	if err != nil || a.AuthFile == "" {
		return true
	}

	for _, name := range a.AuthEnv {
		if p.Env[name] != "" {
			return true
		}
	}

	if dir == "" {
		return false
	}

	info, err := os.Stat(filepath.Join(dir, a.AuthFile))

	return err == nil && info.Mode().IsRegular()
}

// Discover lists the profile directories of every agent directly under home. Each agent's
// default directory (~/.claude, ~/.codex) is its default profile; each
// ~/.<dir>_<name> or ~/.<dir>-<name> directory that holds the agent's config is its profile
// <name>. Files, names that do not match the name pattern, and directories with no agent
// config (another tool's folder, or one the agent has never run with) are ignored. When
// two of an agent's directories map to the same name, the default directory beats
// ~/.<dir>_<name>, which beats ~/.<dir>-<name>; the loser is skipped with a warning logged
// to log (nil discards it). Discovered profiles start with full access, no model or effort
// flag, and plan mode off.
func Discover(home string, log *slog.Logger) ([]Profile, error) {
	log = logging.Or(log)

	home, err := filepath.Abs(home)
	if err != nil {
		return nil, fmt.Errorf("discover profiles: %w", err)
	}

	entries, err := os.ReadDir(home)
	if err != nil {
		return nil, fmt.Errorf("discover profiles: %w", err)
	}

	found := map[profileKey]candidate{}

	for _, a := range agent.All() {
		for _, entry := range entries {
			if c, ok := discoverable(home, a, entry.Name(), log); ok {
				keep(found, c, log)
			}
		}
	}

	profiles := make([]Profile, 0, len(found))
	for _, c := range found {
		profiles = append(profiles, newDiscovered(c.agent, c.name, c.configDir))
	}

	sortProfiles(profiles)

	return profiles, nil
}

// profileKey identifies a profile: its agent and its name.
type profileKey struct {
	agent agent.ID
	name  string
}

// candidate is a profile directory found under home, with its precedence (lower wins).
// configDir is "" for the default profile; dir is always the directory found.
type candidate struct {
	agent     agent.ID
	name      string
	configDir string
	dir       string
	rank      int
}

// discoverable returns the profile candidate a home-directory entry makes for agent a, if
// any: the agent's default directory, or a named profile directory that holds its config.
func discoverable(home string, a agent.Agent, entry string, log *slog.Logger) (candidate, bool) {
	c, ok := classify(home, a, entry)
	if !ok || !isDir(c.dir) {
		return candidate{}, false
	}

	if c.configDir != "" {
		if config, _ := inspectConfigDir(c.dir, a.Markers); !config {
			log.Debug("skipping a profile directory with no agent config", "agent", a.ID, "dir", c.dir)

			return candidate{}, false
		}
	}

	return c, true
}

// prunable reports whether Prune removes a stored profile: a named profile of a known
// agent whose directory can be read but holds none of the agent's config, and that nobody
// edited. An edited one is kept with a warning, since its settings would be lost.
func prunable(ctx context.Context, p Profile, log *slog.Logger) bool {
	a, err := agent.Lookup(p.Agent)
	if err != nil || p.Agent == agent.Inherit || p.ConfigDir == "" || p.Name == DefaultName {
		return false
	}

	config, readable := inspectConfigDir(p.ConfigDir, a.Markers)
	if !readable || config {
		return false
	}

	if !untouched(p) {
		log.WarnContext(ctx, "keeping an edited profile whose directory holds no agent config",
			"agent", p.Agent, "profile", p.Name, "dir", p.ConfigDir)

		return false
	}

	return true
}

// classify maps a home-directory entry name to a profile candidate of agent a.
func classify(home string, a agent.Agent, entry string) (candidate, bool) {
	dir := filepath.Join(home, entry)
	if entry == a.ConfigDir {
		return candidate{agent: a.ID, name: DefaultName, dir: dir}, true
	}

	for rank, sep := range []string{"_", "-"} {
		name, ok := strings.CutPrefix(entry, a.ConfigDir+sep)
		if ok && namePattern.MatchString(name) {
			return candidate{agent: a.ID, name: name, configDir: dir, dir: dir, rank: rank + 1}, true
		}
	}

	return candidate{}, false
}

// keep records c unless a higher-precedence directory already claimed its agent's name.
func keep(found map[profileKey]candidate, c candidate, log *slog.Logger) {
	key := profileKey{agent: c.agent, name: c.name}

	prev, seen := found[key]
	if seen && prev.rank <= c.rank {
		log.Warn("skipping a duplicate profile directory", "agent", c.agent, "profile", c.name, "kept", prev.dir,
			"skipped", c.dir)

		return
	}

	if seen {
		log.Warn("skipping a duplicate profile directory", "agent", c.agent, "profile", c.name, "kept", c.dir,
			"skipped", prev.dir)
	}

	found[key] = c
}

// isDir reports whether path is a directory, following symlinks.
func isDir(path string) bool {
	info, err := os.Stat(path)

	return err == nil && info.IsDir()
}

// inspectConfigDir reports whether dir is an agent config directory: it holds at least
// one of the agent's markers, of the right kind (a file, or a directory), following
// symlinks, since people link their settings in from elsewhere. readable is false when
// dir cannot be listed (gone, not a directory, or not permitted), so the answer is
// unknown.
func inspectConfigDir(dir string, markers []agent.Marker) (config, readable bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, false
	}

	present := make(map[string]bool, len(entries))
	for _, entry := range entries {
		present[entry.Name()] = true
	}

	for _, m := range markers {
		if !present[m.Name] {
			continue
		}

		if info, statErr := os.Stat(filepath.Join(dir, m.Name)); statErr == nil && info.IsDir() == m.Dir {
			return true, true
		}
	}

	return false, true
}

// untouched reports whether a stored profile still has the settings discovery gives a
// new one (see newDiscovered).
func untouched(p Profile) bool {
	return p.Model == "" && p.Effort == "" && p.PermissionMode == PermissionFull && !p.PlanMode &&
		len(p.AddDirs) == 0 && len(p.ExtraArgs) == 0 && len(p.Env) == 0
}

// newDiscovered returns a discovered profile with the discovery defaults.
func newDiscovered(id agent.ID, name, configDir string) Profile {
	return Profile{
		Agent:          id,
		Name:           name,
		ConfigDir:      configDir,
		PermissionMode: PermissionFull,
		AddDirs:        []string{},
		ExtraArgs:      []string{},
		Env:            map[string]string{},
	}
}

// sortProfiles orders profiles by agent, in the agents' order with any unknown agent
// after them by ID, and each agent's default profile first and the rest by name.
func sortProfiles(profiles []Profile) {
	slices.SortStableFunc(profiles, func(a, b Profile) int {
		if c := compareAgents(a.Agent, b.Agent); c != 0 {
			return c
		}

		switch {
		case a.Name == b.Name:
			return 0
		case a.Name == DefaultName:
			return -1
		case b.Name == DefaultName:
			return 1
		default:
			return strings.Compare(a.Name, b.Name)
		}
	})
}

// compareAgents orders agents as agent.IDs does, any unknown one after them by ID.
func compareAgents(a, b agent.ID) int {
	ids := agent.IDs()
	ia, ib := slices.Index(ids, a), slices.Index(ids, b)

	switch {
	case ia == ib && ia >= 0:
		return 0
	case ia < 0 && ib < 0:
		return strings.Compare(string(a), string(b))
	case ia < 0:
		return 1
	case ib < 0:
		return -1
	default:
		return ia - ib
	}
}
