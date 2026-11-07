// Package settings stores the user's preferences in the prefs table and serves them to
// the UI.
//
// Each preference is a typed key with a default, so a fresh database already has a
// complete set of settings. Setters validate their value before storing it and reject
// anything unknown with a sentinel error. Settings writes every key it declares, and the
// rule keys the trust, snooze, sensitive, and notifyprefs packages declare and decode;
// see the prefs package for which package writes the others.
package settings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/launcher/terminal"
	"github.com/mrz1836/nilbox/internal/notifyprefs"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/sensitive"
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

// Terminal choices: the IDs of the terminal drivers.
const (
	// TerminalITerm2 opens launches in a new iTerm2 tab or window.
	TerminalITerm2 = terminal.ITerm2

	// TerminalApp opens launches in a new Terminal.app window.
	TerminalApp = terminal.TerminalApp
)

// Defaults for a fresh database.
const (
	DefaultProfile         = profiles.DefaultName
	DefaultWorktreeDefault = true
	DefaultAutoCleanup     = true
	DefaultTerminal        = TerminalITerm2
	DefaultCloneRoot       = "~/projects"
)

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

	// SensitiveFiles is whose pull requests have their changed files checked, which
	// built-in categories are off, and the user's own patterns (see the sensitive
	// package).
	SensitiveFiles sensitive.Config `json:"sensitiveFiles"`

	// SensitiveCategories are the built-in categories in matching order, with their
	// patterns, for the settings page to describe.
	SensitiveCategories []sensitive.Category `json:"sensitiveCategories"`

	// MenuBar shows the app's icon in the menu bar, with how many items need the user and
	// a short list of them; closing the window then keeps the app running there.
	MenuBar bool `json:"menuBar"`

	// BannersOff lists the kinds of notification (see the notify package) whose banners
	// are off. The notification history keeps every kind either way.
	BannersOff []string `json:"bannersOff"`
}

// ProfileLookup finds a stored profile by name, or returns profiles.ErrProfileNotFound.
// *profiles.Store implements it.
type ProfileLookup interface {
	Lookup(ctx context.Context, name string) (profiles.Profile, error)
}

// Service reads and writes the preferences.
type Service struct {
	prefs    *prefs.Store
	profiles ProfileLookup
	home     string
	clock    clock.Clock
	themes   ThemeNotifier
	menuBar  MenuBarNotifier
}

// NewService returns a Service over the preferences in pool. known looks up the stored
// profiles the default profile must name, and home expands a ~ clone root.
func NewService(pool *sql.DB, known ProfileLookup, home string, opts ...Option) *Service {
	s := &Service{profiles: known, home: home}

	for _, opt := range opts {
		opt(s)
	}

	s.prefs = prefs.New(pool, s.clock)

	return s
}

// WithClock stamps the stored preferences from c rather than from the system clock.
func WithClock(c clock.Clock) Option {
	return func(s *Service) { s.clock = c }
}

// Get returns every preference, with the default for any that is unset or holds a value
// that is no longer valid.
func (s *Service) Get(ctx context.Context) (Settings, error) {
	stored, err := s.prefs.GetMany(ctx,
		KeyDefaultProfile, KeyWorktreeDefault, KeyWorktreeAutoCleanup, KeyTerminal, KeyCloneRoot, KeyTrustedBots,
		KeySnoozeDays, KeyWindowSize,
		snooze.PrefWakeCommits, snooze.PrefWakeReviews, snooze.PrefWakeComments,
		sensitive.PrefScope, sensitive.PrefOff, sensitive.PrefPatterns, KeyMenuBar, notifyprefs.KeyBannersOff,
	)
	if err != nil {
		return Settings{}, err
	}

	out := fromStored(stored)

	// A default profile that is no longer stored (pruned, say) falls back too, so the
	// settings never name a profile the launch dropdown cannot offer.
	if out.DefaultProfile != DefaultProfile {
		_, err := s.profiles.Lookup(ctx, out.DefaultProfile)
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

	return s.prefs.Set(ctx, KeyTerminal, terminal)
}

// SetDefaultProfile stores the profile the launch dropdown preselects. It must name a
// stored profile.
func (s *Service) SetDefaultProfile(ctx context.Context, name string) error {
	if _, err := s.profiles.Lookup(ctx, name); err != nil {
		if errors.Is(err, profiles.ErrProfileNotFound) {
			return fmt.Errorf("%w %q", ErrUnknownProfile, name)
		}

		return err
	}

	return s.prefs.Set(ctx, KeyDefaultProfile, name)
}

// SetWorktreeDefault stores whether launches start in a worktree unless unticked.
func (s *Service) SetWorktreeDefault(ctx context.Context, on bool) error {
	return s.prefs.Set(ctx, KeyWorktreeDefault, strconv.FormatBool(on))
}

// SetWorktreeAutoCleanup stores whether finished worktrees are cleaned up on their own.
func (s *Service) SetWorktreeAutoCleanup(ctx context.Context, on bool) error {
	return s.prefs.Set(ctx, KeyWorktreeAutoCleanup, strconv.FormatBool(on))
}

// SetCloneRoot stores the clone root. It must be an absolute or ~-relative path to an
// existing directory; it is stored as entered (trimmed and cleaned), so "~/projects"
// stays portable across machines.
func (s *Service) SetCloneRoot(ctx context.Context, root string) error {
	root = strings.TrimSpace(root)

	dir, err := paths.ExpandHome(root, s.home)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidCloneRoot, err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidCloneRoot, err)
	}

	if !info.IsDir() {
		return fmt.Errorf("%w: %s is not a directory", ErrInvalidCloneRoot, paths.Tilde(dir, s.home))
	}

	return s.prefs.Set(ctx, KeyCloneRoot, filepath.Clean(root))
}

// SetTrustedBots stores the bots whose items are trusted. Each login may be written as
// GitHub shows it ("dependabot", "dependabot[bot]", or "app/dependabot"); the list is
// stored normalized and sorted, and may be empty to trust no bot.
func (s *Service) SetTrustedBots(ctx context.Context, bots []string) error {
	normalized, err := trust.NormalizeBots(bots)
	if err != nil {
		return err
	}

	return s.prefs.Set(ctx, KeyTrustedBots, trust.EncodeBots(normalized))
}

// ResetTrustedBots goes back to the default trusted bots.
func (s *Service) ResetTrustedBots(ctx context.Context) error {
	if err := s.prefs.Delete(ctx, KeyTrustedBots); err != nil {
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

	return s.prefs.Set(ctx, KeySnoozeDays, strconv.Itoa(days))
}

// SetSnoozeWake stores which new activity ends a snooze early. Every rule is stored
// together, so the rules never mix two saves.
func (s *Service) SetSnoozeWake(ctx context.Context, rules snooze.Rules) error {
	if err := rules.Validate(); err != nil {
		return err
	}

	if err := s.prefs.SetMany(ctx, rules.Encode()); err != nil {
		return fmt.Errorf("save the snooze rules: %w", err)
	}

	return nil
}

// SetSensitiveScope stores whose pull requests have their changed files checked:
// sensitive.ScopeOutside, or sensitive.ScopeCollaborators to check collaborators' too.
func (s *Service) SetSensitiveScope(ctx context.Context, scope string) error {
	if !sensitive.ValidScope(scope) {
		return fmt.Errorf("%w %q: choose %q or %q", sensitive.ErrInvalidScope, scope, sensitive.ScopeOutside,
			sensitive.ScopeCollaborators)
	}

	return s.prefs.Set(ctx, sensitive.PrefScope, scope)
}

// SetSensitiveCategory turns a built-in category of sensitive files on or off. The agent
// configuration category cannot be turned off, since launches are held back by it.
func (s *Service) SetSensitiveCategory(ctx context.Context, id string, on bool) error {
	err := s.prefs.Update(ctx, sensitive.PrefOff, func(stored string, _ bool) (string, error) {
		cfg := sensitive.DecodeConfig(map[string]string{sensitive.PrefOff: stored})

		off, err := sensitive.SetCategory(cfg.Off, id, on)
		if err != nil {
			return "", err
		}

		return sensitive.EncodeList(off), nil
	})
	if err != nil {
		return fmt.Errorf("save the sensitive file categories: %w", err)
	}

	return nil
}

// SetSensitivePatterns stores the user's own patterns of sensitive files, in order. Each
// is trimmed, empty ones and repeats are dropped, and a pattern that cannot be used is
// refused with the reason (see the sensitive package for the syntax).
func (s *Service) SetSensitivePatterns(ctx context.Context, patterns []string) error {
	normalized, err := sensitive.NormalizePatterns(patterns)
	if err != nil {
		return err
	}

	return s.prefs.Set(ctx, sensitive.PrefPatterns, sensitive.EncodeList(normalized))
}

// MatchSensitivePath reports whether a path would count as sensitive under the stored
// settings, and which category and pattern it matches, for trying patterns out.
func (s *Service) MatchSensitivePath(ctx context.Context, path string) (sensitive.PathMatch, error) {
	stored, err := s.prefs.GetMany(ctx, sensitive.PrefScope, sensitive.PrefOff, sensitive.PrefPatterns)
	if err != nil {
		return sensitive.PathMatch{}, fmt.Errorf("read the sensitive file settings: %w", err)
	}

	return sensitive.Lookup(sensitive.DecodeConfig(stored), path), nil
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
		SensitiveFiles:      sensitive.DecodeConfig(stored),
		SensitiveCategories: sensitive.Categories(),
		MenuBar:             decodeMenuBar(stored[KeyMenuBar]),
		BannersOff:          notifyprefs.DecodeBannersOff(stored[notifyprefs.KeyBannersOff]),
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
