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
	"github.com/mrz1836/nilbox/internal/snooze"
	"github.com/mrz1836/nilbox/internal/trust"
)

// Preference keys.
const (
	// KeyDefaultProfile names the profile the launch dropdown preselects.
	KeyDefaultProfile = "profile.default"

	// KeyWorktreeDefault is whether launches start in a worktree unless unticked.
	KeyWorktreeDefault = "launch.worktree"

	// KeyWorktreeAutoCleanup is whether a worktree is cleaned up on its own once its pull
	// request merges or its issue closes (see the worktrees package for what is checked).
	KeyWorktreeAutoCleanup = "worktrees.autoCleanup"

	// KeyTerminal is the terminal app launches open in.
	KeyTerminal = "launch.terminal"

	// KeyCloneRoot is the directory whose children are the user's local clones.
	KeyCloneRoot = "clones.root"

	// KeyTrustedBots lists the bots whose items are trusted (see the trust package).
	KeyTrustedBots = trust.PrefBots

	// KeySnoozeDays is how many days an inbox row's Snooze button hides an item. The
	// wake rules are stored under the other snooze.* keys (see the snooze package).
	KeySnoozeDays = snooze.PrefDays
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
	DefaultAutoCleanup     = true
	DefaultTerminal        = TerminalITerm2
	DefaultCloneRoot       = "~/projects"
	DefaultSnoozeDays      = snooze.DefaultDays
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

	// WorktreeAutoCleanup cleans up a finished worktree on its own once its pull request
	// merges or its issue closes.
	WorktreeAutoCleanup bool `json:"worktreeAutoCleanup"`

	// Terminal is "iterm2" or "terminal".
	Terminal string `json:"terminal"`

	// CloneRoot is the clone root as entered, e.g. "~/projects".
	CloneRoot string `json:"cloneRoot"`

	// TrustedBots lists the bots (GitHub Apps) whose pull requests and issues are trusted
	// like a collaborator's, by login, sorted. It starts as trust.DefaultBots.
	TrustedBots []string `json:"trustedBots"`

	// DefaultTrustedBots is the list ResetTrustedBots restores.
	DefaultTrustedBots []string `json:"defaultTrustedBots"`

	// SnoozeDays is how many days an inbox row's Snooze button hides an item; its menu
	// offers other lengths for one snooze.
	SnoozeDays int `json:"snoozeDays"`

	// SnoozeWake is which new activity ends a snooze early.
	SnoozeWake snooze.Rules `json:"snoozeWake"`

	// WindowSize is the size the window opens at.
	WindowSize WindowSize `json:"windowSize"`

	// DefaultWindowSize is the size a fresh database opens the window at.
	DefaultWindowSize WindowSize `json:"defaultWindowSize"`
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

	keys := []string{
		KeyDefaultProfile, KeyWorktreeDefault, KeyWorktreeAutoCleanup, KeyTerminal, KeyCloneRoot, KeyTrustedBots,
		KeySnoozeDays, KeyWindowSize,
		snooze.PrefWakeCommits, snooze.PrefWakeReviews, snooze.PrefWakeComments,
	}
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

// SetWorktreeAutoCleanup stores whether finished worktrees are cleaned up on their own.
func (s *Service) SetWorktreeAutoCleanup(ctx context.Context, on bool) error {
	return s.set(ctx, KeyWorktreeAutoCleanup, strconv.FormatBool(on))
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

// SetSnoozeDays stores how many days the Snooze button hides an item:
// snooze.MinDays to snooze.MaxDays.
func (s *Service) SetSnoozeDays(ctx context.Context, days int) error {
	if !snooze.ValidDays(days) {
		return fmt.Errorf("%w: %d days; choose %d to %d", snooze.ErrInvalidDays, days, snooze.MinDays, snooze.MaxDays)
	}

	return s.set(ctx, KeySnoozeDays, strconv.Itoa(days))
}

// SetSnoozeWake stores which new activity ends a snooze early. Every rule is stored
// together, so the rules never mix two saves.
func (s *Service) SetSnoozeWake(ctx context.Context, rules snooze.Rules) error {
	if err := rules.Validate(); err != nil {
		return err
	}

	now := time.Now().UTC()

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for key, value := range rules.Encode() {
			if err := tx.Exec(upsertPrefSQL, key, value, now).Error; err != nil {
				return fmt.Errorf("save setting %s: %w", key, err)
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("save the snooze rules: %w", err)
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
		DefaultProfile:      DefaultProfile,
		WorktreeDefault:     DefaultWorktreeDefault,
		WorktreeAutoCleanup: DefaultAutoCleanup,
		Terminal:            DefaultTerminal,
		CloneRoot:           DefaultCloneRoot,
		TrustedBots:         trust.DecodeBots(rawBots, hasBots),
		DefaultTrustedBots:  trust.DefaultBots(),
		SnoozeDays:          snooze.DecodeDays(stored),
		SnoozeWake:          snooze.DecodeRules(stored),
		WindowSize:          DefaultWindowSize(),
		DefaultWindowSize:   DefaultWindowSize(),
	}

	if v := stored[KeyDefaultProfile]; v != "" {
		out.DefaultProfile = v
	}

	if v, err := strconv.ParseBool(stored[KeyWorktreeDefault]); err == nil {
		out.WorktreeDefault = v
	}

	if v, err := strconv.ParseBool(stored[KeyWorktreeAutoCleanup]); err == nil {
		out.WorktreeAutoCleanup = v
	}

	if v := stored[KeyTerminal]; validTerminal(v) {
		out.Terminal = v
	}

	if v := strings.TrimSpace(stored[KeyCloneRoot]); v != "" {
		out.CloneRoot = v
	}

	if v, ok := decodeWindowSize(stored[KeyWindowSize]); ok {
		out.WindowSize = v
	}

	return out
}

// validTerminal reports whether terminal is one of the choices.
func validTerminal(terminal string) bool {
	return terminal == TerminalITerm2 || terminal == TerminalApp
}
