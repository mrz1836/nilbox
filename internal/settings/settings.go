// Package settings stores the user's preferences in the prefs table and serves them to
// the UI.
//
// Each preference is a typed key with a default, so a fresh database already has a
// complete set of settings. Setters validate their value before storing it and reject
// anything unknown with a sentinel error.
package settings

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/trust"
)

// Preference keys.
const (
	// KeyDefaultProfile names the profile the launch dropdown preselects.
	KeyDefaultProfile = "profile.default"

	// KeyWorktreeDefault is whether launches start in a worktree unless unticked.
	KeyWorktreeDefault = "launch.worktree"

	// KeyTerminal is the terminal app launches open in.
	KeyTerminal = "launch.terminal"

	// KeyCloneRoot is the directory whose children are the user's local clones.
	KeyCloneRoot = "clones.root"

	// KeyTrustedBots lists the bots whose items are trusted (see the trust package).
	KeyTrustedBots = trust.PrefBots
)

// Terminal choices.
const (
	// TerminalITerm2 opens launches in a new iTerm2 tab or window.
	TerminalITerm2 = "iterm2"

	// TerminalApp opens launches in a new Terminal.app window.
	TerminalApp = "terminal"
)

// Defaults for a fresh database.
const (
	DefaultProfile         = profiles.DefaultName
	DefaultWorktreeDefault = true
	DefaultTerminal        = TerminalITerm2
	DefaultCloneRoot       = "~/projects"
)

// upsertPrefSQL stores one preference, replacing any earlier value.
const upsertPrefSQL = `INSERT INTO prefs (key, value, updated_at) VALUES (?, ?, ?)
ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`

var (
	// ErrInvalidTerminal is returned for a terminal that is not one of the choices.
	ErrInvalidTerminal = errors.New("unknown terminal")

	// ErrUnknownProfile is returned when the default profile names no stored profile.
	ErrUnknownProfile = errors.New("unknown profile")

	// ErrInvalidCloneRoot is returned for a clone root that is not an existing directory.
	ErrInvalidCloneRoot = errors.New("invalid clone root")
)

// Settings is the full set of preferences as the UI shows them. Every field is a plain
// value.
type Settings struct {
	// DefaultProfile is the profile the launch dropdown preselects.
	DefaultProfile string `json:"defaultProfile"`

	// WorktreeDefault presets each row's "Start in a worktree" checkbox.
	WorktreeDefault bool `json:"worktreeDefault"`

	// Terminal is "iterm2" or "terminal".
	Terminal string `json:"terminal"`

	// CloneRoot is the clone root as entered, e.g. "~/projects".
	CloneRoot string `json:"cloneRoot"`

	// TrustedBots lists the bots (GitHub Apps) whose pull requests and issues are trusted
	// like a collaborator's, by login, sorted. It starts as trust.DefaultBots.
	TrustedBots []string `json:"trustedBots"`

	// DefaultTrustedBots is the list ResetTrustedBots restores.
	DefaultTrustedBots []string `json:"defaultTrustedBots"`
}

// Service reads and writes the preferences.
type Service struct {
	db   *gorm.DB
	home string
}

// NewService returns a Service over db. home expands a ~ clone root.
func NewService(db *gorm.DB, home string) *Service {
	return &Service{db: db, home: home}
}

// Get returns every preference, with the default for any that is unset or holds a value
// that is no longer valid.
func (s *Service) Get(ctx context.Context) (Settings, error) {
	var rows []struct {
		Key   string
		Value string
	}

	keys := []string{KeyDefaultProfile, KeyWorktreeDefault, KeyTerminal, KeyCloneRoot, KeyTrustedBots}
	if err := s.db.WithContext(ctx).Raw("SELECT key, value FROM prefs WHERE key IN ?", keys).Scan(&rows).Error; err != nil {
		return Settings{}, fmt.Errorf("read settings: %w", err)
	}

	stored := make(map[string]string, len(rows))
	for _, r := range rows {
		stored[r.Key] = r.Value
	}

	out := fromStored(stored)

	// A default profile that is no longer stored (pruned, say) falls back too, so the
	// settings never name a profile the launch dropdown cannot offer.
	if out.DefaultProfile != DefaultProfile {
		_, err := profiles.Lookup(ctx, s.db, out.DefaultProfile)
		if errors.Is(err, profiles.ErrProfileNotFound) {
			out.DefaultProfile = DefaultProfile
		} else if err != nil {
			return Settings{}, fmt.Errorf("read settings: %w", err)
		}
	}

	return out, nil
}

// SetTerminal stores the terminal launches open in: "iterm2" or "terminal".
func (s *Service) SetTerminal(ctx context.Context, terminal string) error {
	if !validTerminal(terminal) {
		return fmt.Errorf("%w %q: choose %q or %q", ErrInvalidTerminal, terminal, TerminalITerm2, TerminalApp)
	}

	return s.set(ctx, KeyTerminal, terminal)
}

// SetDefaultProfile stores the profile the launch dropdown preselects. It must name a
// stored profile.
func (s *Service) SetDefaultProfile(ctx context.Context, name string) error {
	if _, err := profiles.Lookup(ctx, s.db, name); err != nil {
		if errors.Is(err, profiles.ErrProfileNotFound) {
			return fmt.Errorf("%w %q", ErrUnknownProfile, name)
		}

		return err
	}

	return s.set(ctx, KeyDefaultProfile, name)
}

// SetWorktreeDefault stores whether launches start in a worktree unless unticked.
func (s *Service) SetWorktreeDefault(ctx context.Context, on bool) error {
	return s.set(ctx, KeyWorktreeDefault, strconv.FormatBool(on))
}

// SetCloneRoot stores the clone root. It must be an absolute or ~-relative path to an
// existing directory; it is stored as entered (trimmed and cleaned), so "~/projects"
// stays portable across machines.
func (s *Service) SetCloneRoot(ctx context.Context, root string) error {
	root = strings.TrimSpace(root)

	dir, err := localrepos.ExpandRoot(root, s.home)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidCloneRoot, err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidCloneRoot, err)
	}

	if !info.IsDir() {
		return fmt.Errorf("%w: %s is not a directory", ErrInvalidCloneRoot, localrepos.TildePath(dir, s.home))
	}

	return s.set(ctx, KeyCloneRoot, filepath.Clean(root))
}

// SetTrustedBots stores the bots whose items are trusted. Each login may be written as
// GitHub shows it ("dependabot", "dependabot[bot]", or "app/dependabot"); the list is
// stored normalized and sorted, and may be empty to trust no bot.
func (s *Service) SetTrustedBots(ctx context.Context, bots []string) error {
	normalized, err := trust.NormalizeBots(bots)
	if err != nil {
		return err
	}

	return s.set(ctx, KeyTrustedBots, trust.EncodeBots(normalized))
}

// ResetTrustedBots goes back to the default trusted bots.
func (s *Service) ResetTrustedBots(ctx context.Context) error {
	if err := s.db.WithContext(ctx).Exec("DELETE FROM prefs WHERE key = ?", KeyTrustedBots).Error; err != nil {
		return fmt.Errorf("reset the trusted bots: %w", err)
	}

	return nil
}

// set stores one preference.
func (s *Service) set(ctx context.Context, key, value string) error {
	if err := s.db.WithContext(ctx).Exec(upsertPrefSQL, key, value, time.Now().UTC()).Error; err != nil {
		return fmt.Errorf("save setting %s: %w", key, err)
	}

	return nil
}

// fromStored builds Settings from stored values, falling back to the defaults.
func fromStored(stored map[string]string) Settings {
	rawBots, hasBots := stored[KeyTrustedBots]

	out := Settings{
		DefaultProfile:     DefaultProfile,
		WorktreeDefault:    DefaultWorktreeDefault,
		Terminal:           DefaultTerminal,
		CloneRoot:          DefaultCloneRoot,
		TrustedBots:        trust.DecodeBots(rawBots, hasBots),
		DefaultTrustedBots: trust.DefaultBots(),
	}

	if v := stored[KeyDefaultProfile]; v != "" {
		out.DefaultProfile = v
	}

	if v, err := strconv.ParseBool(stored[KeyWorktreeDefault]); err == nil {
		out.WorktreeDefault = v
	}

	if v := stored[KeyTerminal]; validTerminal(v) {
		out.Terminal = v
	}

	if v := strings.TrimSpace(stored[KeyCloneRoot]); v != "" {
		out.CloneRoot = v
	}

	return out
}

// validTerminal reports whether terminal is one of the choices.
func validTerminal(terminal string) bool {
	return terminal == TerminalITerm2 || terminal == TerminalApp
}
