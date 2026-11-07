// Package profiles discovers the user's coding-agent profiles and serves them to the UI.
//
// A profile is a config directory the agent can run with. The default ~/.claude becomes
// the "default" profile, which runs without CLAUDE_CONFIG_DIR so the agent's own default
// login is used; every ~/.claude_<name> or ~/.claude-<name> directory that holds an agent
// config becomes profile <name>, run with CLAUDE_CONFIG_DIR set to that directory. Other
// tools name their own folders ~/.claude-<name> too, so a directory counts only when it
// holds what the agent writes into a config directory (see inspectConfigDir).
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
	"sort"
	"strings"

	"github.com/mrz1836/nilbox/internal/paths"
)

const (
	// DefaultName is the profile backed by the agent's default config directory.
	DefaultName = "default"

	// PermissionFull means the profile starts with full access
	// (--dangerously-skip-permissions).
	PermissionFull = "full"

	// defaultDir is the agent's default config directory under the home directory.
	defaultDir = ".claude"
)

// configMarker is an entry the agent writes into a config directory it runs with.
type configMarker struct {
	name string
	dir  bool
}

// configMarkers are the entries that mark an agent config directory: the agent's state
// file, its settings, and its per-project session history. The agent writes the first
// on its first run with the directory.
func configMarkers() []configMarker {
	return []configMarker{{name: ".claude.json"}, {name: "settings.json"}, {name: "projects", dir: true}}
}

// ErrProfileNotFound is returned when no profile has the requested name.
var ErrProfileNotFound = errors.New("profile not found")

// namePattern is what a discovered profile name must look like.
var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Profile is one launch profile as the UI shows it. Every field is a plain value.
type Profile struct {
	// Name identifies the profile in the dropdown.
	Name string `json:"name"`

	// ConfigDir is the absolute config directory, or "" for the default profile.
	ConfigDir string `json:"configDir"`

	// Model is the --model value ("" for the agent's default).
	Model string `json:"model"`

	// Effort is the --effort value ("" for the agent's default).
	Effort string `json:"effort"`

	// PermissionMode is "full", a --permission-mode value, or "" for the agent's default.
	PermissionMode string `json:"permissionMode"`

	// PlanMode starts the session in plan mode.
	PlanMode bool `json:"planMode"`

	// AddDirs are extra directories passed with --add-dir.
	AddDirs []string `json:"addDirs"`

	// ExtraArgs are extra arguments, one argv element each.
	ExtraArgs []string `json:"extraArgs"`

	// Env holds extra environment variables for the session.
	Env map[string]string `json:"env"`

	// Folder is the profile's config directory as shown to people, with ~ for the home
	// directory: "~/.claude" for the default profile. Only List sets it.
	Folder string `json:"folder"`

	// Missing reports that the config directory no longer exists. Only List sets it.
	Missing bool `json:"missing"`
}

// Service serves the stored profiles.
type Service struct {
	store *Store
	home  string
}

// NewService returns a Service over the profiles in store. home is where the default
// profile's directory lives, and is shown as ~ in each profile's Folder.
func NewService(store *Store, home string) *Service {
	return &Service{store: store, home: home}
}

// List returns every stored profile, the default first and the rest by name, each with
// its Folder and whether that folder is missing.
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

// describeFolder sets a profile's Folder and Missing from its config directory.
func (s *Service) describeFolder(p *Profile) {
	dir := p.ConfigDir
	if dir == "" {
		dir = filepath.Join(s.home, defaultDir)
	}

	p.Folder = paths.Tilde(dir, s.home)

	_, err := os.Stat(dir)
	p.Missing = errors.Is(err, fs.ErrNotExist)
}

// Discover lists the profile directories directly under home. ~/.claude is the default
// profile; each ~/.claude_<name> or ~/.claude-<name> directory that holds an agent config
// is profile <name>. Files, names that do not match the name pattern, and directories
// with no agent config (another tool's folder, or one the agent has never run with) are
// ignored. When two directories map to the same name, ~/.claude beats ~/.claude_<name>,
// which beats ~/.claude-<name>; the loser is skipped with a warning. Discovered profiles
// start with full access, no model or effort flag, and plan mode off.
func Discover(home string) ([]Profile, error) {
	home, err := filepath.Abs(home)
	if err != nil {
		return nil, fmt.Errorf("discover profiles: %w", err)
	}

	entries, err := os.ReadDir(home)
	if err != nil {
		return nil, fmt.Errorf("discover profiles: %w", err)
	}

	found := map[string]candidate{}

	for _, entry := range entries {
		if c, ok := discoverable(home, entry.Name()); ok {
			keep(found, c)
		}
	}

	profiles := make([]Profile, 0, len(found))
	for _, c := range found {
		profiles = append(profiles, newDiscovered(c.name, c.configDir))
	}

	sortProfiles(profiles)

	return profiles, nil
}

// candidate is a profile directory found under home, with its precedence (lower wins).
// configDir is "" for the default profile; dir is always the directory found.
type candidate struct {
	name      string
	configDir string
	dir       string
	rank      int
}

// discoverable returns the profile candidate a home-directory entry makes, if any: the
// default ~/.claude directory, or a named profile directory that holds an agent config.
func discoverable(home, entry string) (candidate, bool) {
	c, ok := classify(home, entry)
	if !ok || !isDir(c.dir) {
		return candidate{}, false
	}

	if c.configDir != "" {
		if config, _ := inspectConfigDir(c.dir); !config {
			slog.Debug("skipping a profile directory with no agent config", "dir", c.dir)

			return candidate{}, false
		}
	}

	return c, true
}

// prunable reports whether Prune removes a stored profile: a named profile whose
// directory can be read but holds no agent config, and that nobody edited. An edited one
// is kept with a warning, since its settings would be lost.
func prunable(p Profile) bool {
	if p.ConfigDir == "" || p.Name == DefaultName {
		return false
	}

	config, readable := inspectConfigDir(p.ConfigDir)
	if !readable || config {
		return false
	}

	if !untouched(p) {
		slog.Warn("keeping an edited profile whose directory holds no agent config",
			"profile", p.Name, "dir", p.ConfigDir)

		return false
	}

	return true
}

// classify maps a home-directory entry name to a profile candidate.
func classify(home, entry string) (candidate, bool) {
	dir := filepath.Join(home, entry)
	if entry == defaultDir {
		return candidate{name: DefaultName, dir: dir}, true
	}

	for rank, sep := range []string{"_", "-"} {
		name, ok := strings.CutPrefix(entry, defaultDir+sep)
		if ok && namePattern.MatchString(name) {
			return candidate{name: name, configDir: dir, dir: dir, rank: rank + 1}, true
		}
	}

	return candidate{}, false
}

// keep records c unless a higher-precedence directory already claimed its name.
func keep(found map[string]candidate, c candidate) {
	prev, seen := found[c.name]
	if seen && prev.rank <= c.rank {
		slog.Warn("skipping a duplicate profile directory", "profile", c.name, "kept", prev.dir, "skipped", c.dir)

		return
	}

	if seen {
		slog.Warn("skipping a duplicate profile directory", "profile", c.name, "kept", c.dir, "skipped", prev.dir)
	}

	found[c.name] = c
}

// isDir reports whether path is a directory, following symlinks.
func isDir(path string) bool {
	info, err := os.Stat(path)

	return err == nil && info.IsDir()
}

// inspectConfigDir reports whether dir is an agent config directory: it holds at least
// one config marker of the right kind (a file, or the projects directory), following
// symlinks, since people link their settings in from elsewhere. readable is false when
// dir cannot be listed (gone, not a directory, or not permitted), so the answer is
// unknown.
func inspectConfigDir(dir string) (config, readable bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, false
	}

	present := make(map[string]bool, len(entries))
	for _, entry := range entries {
		present[entry.Name()] = true
	}

	for _, m := range configMarkers() {
		if !present[m.name] {
			continue
		}

		if info, statErr := os.Stat(filepath.Join(dir, m.name)); statErr == nil && info.IsDir() == m.dir {
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
func newDiscovered(name, configDir string) Profile {
	return Profile{
		Name:           name,
		ConfigDir:      configDir,
		PermissionMode: PermissionFull,
		AddDirs:        []string{},
		ExtraArgs:      []string{},
		Env:            map[string]string{},
	}
}

// sortProfiles orders the default profile first and the rest by name.
func sortProfiles(profiles []Profile) {
	sort.SliceStable(profiles, func(i, j int) bool {
		a, b := profiles[i].Name, profiles[j].Name
		if (a == DefaultName) != (b == DefaultName) {
			return a == DefaultName
		}

		return a < b
	})
}
