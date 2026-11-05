// Package profiles discovers the user's coding-agent profiles and serves them to the UI.
//
// A profile is a config directory the agent can run with. The default ~/.claude becomes
// the "default" profile, which runs without CLAUDE_CONFIG_DIR so the agent's own default
// login is used; every ~/.claude_<name> or ~/.claude-<name> directory becomes profile
// <name>, run with CLAUDE_CONFIG_DIR set to that directory.
//
// Discovery runs on every start and only ever adds rows: a profile the user edited keeps
// its settings, and a profile whose directory disappeared keeps its row.
package profiles

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/mrz1836/nilbox/internal/profiles/model"
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

// ErrProfileNotFound is returned when no profile has the requested name.
var ErrProfileNotFound = errors.New("profile not found")

// namePattern is what a discovered profile name must look like.
func namePattern() *regexp.Regexp {
	return regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
}

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
}

// Service serves the stored profiles.
type Service struct {
	db *gorm.DB
}

// NewService returns a Service over db.
func NewService(db *gorm.DB) *Service {
	return &Service{db: db}
}

// List returns every stored profile, the default first and the rest by name.
func (s *Service) List(ctx context.Context) ([]Profile, error) {
	var rows []model.ClaudeProfile
	if err := s.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list profiles: %w", err)
	}

	profiles := make([]Profile, 0, len(rows))
	for _, row := range rows {
		profiles = append(profiles, fromRow(row))
	}

	sortProfiles(profiles)

	return profiles, nil
}

// Discover lists the profile directories directly under home. ~/.claude is the default
// profile; each ~/.claude_<name> or ~/.claude-<name> directory is profile <name>. Files
// and names that do not match the name pattern are ignored. When two directories map to
// the same name, ~/.claude beats ~/.claude_<name>, which beats ~/.claude-<name>; the loser
// is skipped with a warning. Discovered profiles start with full access, no model or
// effort flag, and plan mode off.
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
	pattern := namePattern()

	for _, entry := range entries {
		c, ok := classify(home, entry.Name(), pattern)
		if !ok || !isDir(c.dir) {
			continue
		}

		keep(found, c)
	}

	profiles := make([]Profile, 0, len(found))
	for _, c := range found {
		profiles = append(profiles, newDiscovered(c.name, c.configDir))
	}

	sortProfiles(profiles)

	return profiles, nil
}

// Sync stores every discovered profile that has no row yet. It never updates or deletes
// a row.
func Sync(ctx context.Context, db *gorm.DB, found []Profile) error {
	if len(found) == 0 {
		return nil
	}

	rows := make([]model.ClaudeProfile, 0, len(found))
	for _, p := range found {
		rows = append(rows, toRow(p))
	}

	err := db.WithContext(ctx).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "name"}}, DoNothing: true}).
		Create(&rows).Error
	if err != nil {
		return fmt.Errorf("store discovered profiles: %w", err)
	}

	return nil
}

// Lookup returns the stored profile named name, or ErrProfileNotFound.
func Lookup(ctx context.Context, db *gorm.DB, name string) (Profile, error) {
	var rows []model.ClaudeProfile
	if err := db.WithContext(ctx).Where("name = ?", name).Limit(1).Find(&rows).Error; err != nil {
		return Profile{}, fmt.Errorf("look up profile: %w", err)
	}

	if len(rows) == 0 {
		return Profile{}, fmt.Errorf("%w: %q", ErrProfileNotFound, name)
	}

	return fromRow(rows[0]), nil
}

// candidate is a profile directory found under home, with its precedence (lower wins).
// configDir is "" for the default profile; dir is always the directory found.
type candidate struct {
	name      string
	configDir string
	dir       string
	rank      int
}

// classify maps a home-directory entry name to a profile candidate.
func classify(home, entry string, pattern *regexp.Regexp) (candidate, bool) {
	dir := filepath.Join(home, entry)
	if entry == defaultDir {
		return candidate{name: DefaultName, dir: dir}, true
	}

	for rank, sep := range []string{"_", "-"} {
		name, ok := strings.CutPrefix(entry, defaultDir+sep)
		if ok && pattern.MatchString(name) {
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

// fromRow converts a stored row to a Profile. Nil lists and maps become empty ones.
func fromRow(row model.ClaudeProfile) Profile {
	p := Profile{
		Name:           row.Name,
		ConfigDir:      row.ConfigDir,
		Model:          row.Model,
		Effort:         row.Effort,
		PermissionMode: row.PermissionMode,
		PlanMode:       row.PlanMode,
		AddDirs:        append([]string{}, row.AddDirs...),
		ExtraArgs:      append([]string{}, row.ExtraArgs...),
		Env:            make(map[string]string, len(row.Env)),
	}

	maps.Copy(p.Env, row.Env)

	return p
}

// toRow converts a Profile to a row for insertion.
func toRow(p Profile) model.ClaudeProfile {
	return model.ClaudeProfile{
		Name:           p.Name,
		ConfigDir:      p.ConfigDir,
		Model:          p.Model,
		Effort:         p.Effort,
		PermissionMode: p.PermissionMode,
		PlanMode:       p.PlanMode,
		AddDirs:        model.StringList(p.AddDirs),
		ExtraArgs:      model.StringList(p.ExtraArgs),
		Env:            model.StringMap(p.Env),
	}
}
