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
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/branchrules"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/launcher/terminal"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/notifyprefs"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/snooze"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/trust"
)

// Preference keys.
const (
	// KeyDefaultAgent is the coding agent launches use when no launch default names one.
	KeyDefaultAgent = "agent.default"

	// keyDefaultProfilePrefix starts each agent's default profile key (see
	// KeyDefaultProfile).
	keyDefaultProfilePrefix = "profile.default."

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
	DefaultAgent           = agent.Claude
	DefaultProfile         = profiles.DefaultName
	DefaultWorktreeDefault = true
	DefaultAutoCleanup     = true
	DefaultTerminal        = TerminalITerm2
	DefaultCloneRoot       = "~/projects"
)

var (
	// ErrInvalidTerminal is returned for a terminal that is not one of the choices.
	ErrInvalidTerminal = errors.New("unknown terminal")

	// ErrUnknownProfile is returned when a default profile names no stored profile.
	ErrUnknownProfile = errors.New("unknown profile")

	// ErrUnknownAgent is returned for a default agent this build does not know.
	ErrUnknownAgent = errors.New("unknown coding agent")

	// ErrInvalidCloneRoot is returned for a clone root that is not an existing directory.
	ErrInvalidCloneRoot = errors.New("invalid clone root")
)

// Settings is the full set of preferences as the UI shows them. Every field is a plain
// value.
type Settings struct {
	// DefaultAgent is the coding agent launches use when no launch default names one.
	DefaultAgent agent.ID `json:"defaultAgent"`

	// DefaultProfiles is each agent's profile launches use when no launch default names
	// one. It has an entry for every agent.
	DefaultProfiles map[agent.ID]string `json:"defaultProfiles"`

	// WorktreeDefault presets each row's "Start in a worktree" checkbox.
	WorktreeDefault bool `json:"worktreeDefault"`

	// WorktreeAutoCleanup cleans up a finished worktree on its own once its pull request
	// merges or its issue closes.
	WorktreeAutoCleanup bool `json:"worktreeAutoCleanup"`

	// Terminal is TerminalITerm2 or TerminalApp.
	Terminal terminal.ID `json:"terminal"`

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

	// Branches are the rules for deleting local branches: which go with a worktree that
	// is removed or cleaned up, whether a merged pull request's branch starts checked in
	// the cleanup of merged branches, and which are never deleted. Each deletion is
	// confirmed with checkboxes that start from them.
	Branches branchrules.Rules `json:"branches"`

	// DefaultProtectedBranches is the protected list before the user changes it.
	DefaultProtectedBranches []string `json:"defaultProtectedBranches"`

	// SensitiveCategories are the built-in categories in matching order, with their
	// patterns, for the settings page to describe.
	SensitiveCategories []sensitive.Category `json:"sensitiveCategories"`

	// MenuBar shows the app's icon in the menu bar, with how many items need the user and
	// a short list of them; closing the window then keeps the app running there.
	MenuBar bool `json:"menuBar"`

	// BannersOff lists the kinds of notification (see the notify package) whose banners
	// are off. The notification history keeps every kind either way.
	BannersOff []notifyprefs.Kind `json:"bannersOff"`

	// Theme is the palette the app shows.
	Theme Theme `json:"theme"`

	// CollapsedSections lists the inbox sections the user collapsed, in the order the
	// inbox shows them.
	CollapsedSections []ListKey `json:"collapsedSections"`

	// ProjectsTipDismissed is set once the user puts away the inbox's tip about showing
	// the GitHub projects an issue is in.
	ProjectsTipDismissed bool `json:"projectsTipDismissed"`

	// InboxOrder is which inbox lists are sorted by project status, and the order of the
	// statuses.
	InboxOrder InboxOrder `json:"inboxOrder"`
}

// ProfileLookup finds an agent's stored profile by name, or returns
// profiles.ErrProfileNotFound. *profiles.Store implements it.
type ProfileLookup interface {
	Lookup(ctx context.Context, id agent.ID, name string) (profiles.Profile, error)
}

// Service reads and writes the preferences.
//
// Each setter stores its change and reads the preferences back in one transaction, then
// returns the settings with the change, so a setter that fails has stored nothing. Only
// the check that the default profiles are still stored runs after the change is
// committed, and a failure there is logged rather than reported, since the change has
// happened.
type Service struct {
	pool     *sql.DB
	prefs    *prefs.Store
	profiles ProfileLookup
	home     string
	clock    clock.Clock
	events   event.Sink
	logger   *slog.Logger
}

// NewService returns a Service over the preferences in pool. known looks up the stored
// profiles the default profile must name, and home expands a ~ clone root.
func NewService(pool *sql.DB, known ProfileLookup, home string, opts ...Option) *Service {
	s := &Service{pool: pool, profiles: known, home: home}

	for _, opt := range opts {
		opt(s)
	}

	s.prefs = prefs.New(pool, s.clock)
	s.events = event.Or(s.events)
	s.logger = logging.Or(s.logger)

	return s
}

// Option configures a Service.
type Option func(*Service)

// WithClock stamps the stored preferences from c rather than from the system clock.
func WithClock(c clock.Clock) Option {
	return func(s *Service) { s.clock = c }
}

// WithEvents emits the theme and menu bar changes into sink once they are stored.
func WithEvents(sink event.Sink) Option {
	return func(s *Service) { s.events = sink }
}

// WithLogger logs to logger what a setter cannot report as its error. Without it, or
// with nil, that is not logged.
func WithLogger(logger *slog.Logger) Option {
	return func(s *Service) { s.logger = logger }
}

// Get returns every preference, with the default for any that is unset or holds a value
// that is no longer valid.
func (s *Service) Get(ctx context.Context) (Settings, error) {
	stored, err := s.prefs.GetMany(ctx, settingKeys()...)
	if err != nil {
		return Settings{}, err
	}

	out := fromStored(stored)

	if err = s.checkDefaultProfiles(ctx, &out); err != nil {
		return Settings{}, fmt.Errorf("read settings: %w", err)
	}

	return out, nil
}

// SetTerminal stores the terminal launches open in, TerminalITerm2 or TerminalApp, and
// returns the settings with it.
func (s *Service) SetTerminal(ctx context.Context, id terminal.ID) (Settings, error) {
	if !validTerminal(id) {
		return Settings{}, fmt.Errorf("%w %q: choose %q or %q", ErrInvalidTerminal, id, TerminalITerm2, TerminalApp)
	}

	return s.save(ctx, setting(KeyTerminal, string(id)))
}

// SetDefaultAgent stores the coding agent launches use when no launch default names one,
// and returns the settings with it.
func (s *Service) SetDefaultAgent(ctx context.Context, id agent.ID) (Settings, error) {
	if !agent.Known(id) {
		return Settings{}, fmt.Errorf("%w %q", ErrUnknownAgent, id)
	}

	return s.save(ctx, setting(KeyDefaultAgent, string(id)))
}

// SetDefaultProfile stores the agent's profile launches use when no launch default names
// one, and returns the settings with it. It must name one of the agent's stored profiles.
func (s *Service) SetDefaultProfile(ctx context.Context, id agent.ID, name string) (Settings, error) {
	if !agent.Known(id) {
		return Settings{}, fmt.Errorf("%w %q", ErrUnknownAgent, id)
	}

	if _, err := s.profiles.Lookup(ctx, id, name); err != nil {
		if errors.Is(err, profiles.ErrProfileNotFound) {
			return Settings{}, fmt.Errorf("%w %q", ErrUnknownProfile, name)
		}

		return Settings{}, err
	}

	return s.save(ctx, setting(KeyDefaultProfile(id), name))
}

// KeyDefaultProfile is the key of the agent's default profile, profile.default.<agent>.
func KeyDefaultProfile(id agent.ID) string {
	return keyDefaultProfilePrefix + string(id)
}

// SetWorktreeDefault stores whether launches start in a worktree unless unticked, and
// returns the settings with it.
func (s *Service) SetWorktreeDefault(ctx context.Context, on bool) (Settings, error) {
	return s.save(ctx, setting(KeyWorktreeDefault, strconv.FormatBool(on)))
}

// SetWorktreeAutoCleanup stores whether finished worktrees are cleaned up on their own,
// and returns the settings with it.
func (s *Service) SetWorktreeAutoCleanup(ctx context.Context, on bool) (Settings, error) {
	return s.save(ctx, setting(KeyWorktreeAutoCleanup, strconv.FormatBool(on)))
}

// SetBranchRules stores the rules for deleting local branches, and returns the settings
// with them. Every rule is stored together, so the rules never mix two saves; a protected
// pattern that cannot match a branch is refused, and nothing is stored.
func (s *Service) SetBranchRules(ctx context.Context, rules branchrules.Rules) (Settings, error) {
	values, err := rules.Encode()
	if err != nil {
		return Settings{}, err
	}

	return s.save(ctx, func(ctx context.Context, p *prefs.Store) error {
		if err := p.SetMany(ctx, values); err != nil {
			return fmt.Errorf("save the branch rules: %w", err)
		}

		return nil
	})
}

// SetCloneRoot stores the clone root, and returns the settings with it. It must be an
// absolute or ~-relative path to an existing directory; it is stored as entered (trimmed
// and cleaned), so "~/projects" stays portable across machines.
func (s *Service) SetCloneRoot(ctx context.Context, root string) (Settings, error) {
	root = strings.TrimSpace(root)

	dir, err := paths.ExpandHome(root, s.home)
	if err != nil {
		return Settings{}, fmt.Errorf("%w: %w", ErrInvalidCloneRoot, err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		return Settings{}, fmt.Errorf("%w: %w", ErrInvalidCloneRoot, err)
	}

	if !info.IsDir() {
		return Settings{}, fmt.Errorf("%w: %s is not a directory", ErrInvalidCloneRoot, paths.Tilde(dir, s.home))
	}

	return s.save(ctx, setting(KeyCloneRoot, filepath.Clean(root)))
}

// SetTrustedBots stores the bots whose items are trusted, and returns the settings with
// them. Each login may be written as GitHub shows it ("dependabot", "dependabot[bot]", or
// "app/dependabot"); the list is stored normalized and sorted, and may be empty to trust
// no bot.
func (s *Service) SetTrustedBots(ctx context.Context, bots []string) (Settings, error) {
	normalized, err := trust.NormalizeBots(bots)
	if err != nil {
		return Settings{}, err
	}

	return s.save(ctx, setting(KeyTrustedBots, trust.EncodeBots(normalized)))
}

// ResetTrustedBots goes back to the default trusted bots, and returns the settings with
// them.
func (s *Service) ResetTrustedBots(ctx context.Context) (Settings, error) {
	return s.save(ctx, func(ctx context.Context, p *prefs.Store) error {
		if err := p.Delete(ctx, KeyTrustedBots); err != nil {
			return fmt.Errorf("reset the trusted bots: %w", err)
		}

		return nil
	})
}

// SetSnoozeDays stores how many days the Snooze button hides an item, snooze.MinDays to
// snooze.MaxDays, and returns the settings with it.
func (s *Service) SetSnoozeDays(ctx context.Context, days int) (Settings, error) {
	if !snooze.ValidDays(days) {
		return Settings{}, fmt.Errorf("%w: %d days; choose %d to %d", snooze.ErrInvalidDays, days, snooze.MinDays,
			snooze.MaxDays)
	}

	return s.save(ctx, setting(KeySnoozeDays, strconv.Itoa(days)))
}

// SetSnoozeWake stores which new activity ends a snooze early, and returns the settings
// with it. Every rule is stored together, so the rules never mix two saves.
func (s *Service) SetSnoozeWake(ctx context.Context, rules snooze.Rules) (Settings, error) {
	if err := rules.Validate(); err != nil {
		return Settings{}, err
	}

	return s.save(ctx, func(ctx context.Context, p *prefs.Store) error {
		if err := p.SetMany(ctx, rules.Encode()); err != nil {
			return fmt.Errorf("save the snooze rules: %w", err)
		}

		return nil
	})
}

// SetSensitiveScope stores whose pull requests have their changed files checked,
// sensitive.ScopeOutside or sensitive.ScopeCollaborators to check collaborators' too, and
// returns the settings with it.
func (s *Service) SetSensitiveScope(ctx context.Context, scope sensitive.Scope) (Settings, error) {
	if !scope.Valid() {
		return Settings{}, fmt.Errorf("%w %q: choose %q or %q", sensitive.ErrInvalidScope, scope, sensitive.ScopeOutside,
			sensitive.ScopeCollaborators)
	}

	return s.save(ctx, setting(sensitive.PrefScope, string(scope)))
}

// SetSensitiveCategory turns a built-in category of sensitive files on or off, and returns
// the settings with it. The agent configuration category cannot be turned off, since
// launches are held back by it.
func (s *Service) SetSensitiveCategory(ctx context.Context, id string, on bool) (Settings, error) {
	return s.save(ctx, func(ctx context.Context, p *prefs.Store) error {
		err := p.Update(ctx, sensitive.PrefOff, func(stored string, _ bool) (string, error) {
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
	})
}

// SetSensitivePatterns stores the user's own patterns of sensitive files, in order, and
// returns the settings with them. Each is trimmed, empty ones and repeats are dropped, and
// a pattern that cannot be used is refused with the reason (see the sensitive package for
// the syntax).
func (s *Service) SetSensitivePatterns(ctx context.Context, patterns []string) (Settings, error) {
	normalized, err := sensitive.NormalizePatterns(patterns)
	if err != nil {
		return Settings{}, err
	}

	return s.save(ctx, setting(sensitive.PrefPatterns, sensitive.EncodeList(normalized)))
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
		DefaultAgent:        DefaultAgent,
		DefaultProfiles:     map[agent.ID]string{},
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
		Branches:            branchrules.Decode(stored),

		DefaultProtectedBranches: branchrules.DefaultProtected(),
		MenuBar:                  decodeMenuBar(stored[KeyMenuBar]),
		BannersOff:               notifyprefs.DecodeBannersOff(stored[notifyprefs.KeyBannersOff]),
		Theme:                    decodeTheme(stored[KeyTheme]),
		CollapsedSections:        decodeCollapsed(stored),

		ProjectsTipDismissed: decodeProjectsTipDismissed(stored[KeyProjectsTipDismissed]),
		InboxOrder:           decodeInboxOrder(stored),
	}

	if v := agent.ID(stored[KeyDefaultAgent]); agent.Known(v) {
		out.DefaultAgent = v
	}

	for _, id := range agent.IDs() {
		out.DefaultProfiles[id] = DefaultProfile

		if v := stored[KeyDefaultProfile(id)]; v != "" {
			out.DefaultProfiles[id] = v
		}
	}

	if v, err := strconv.ParseBool(stored[KeyWorktreeDefault]); err == nil {
		out.WorktreeDefault = v
	}

	if v, err := strconv.ParseBool(stored[KeyWorktreeAutoCleanup]); err == nil {
		out.WorktreeAutoCleanup = v
	}

	if v := terminal.ID(stored[KeyTerminal]); validTerminal(v) {
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

// validTerminal reports whether id is one of the choices.
func validTerminal(id terminal.ID) bool {
	return id == TerminalITerm2 || id == TerminalApp
}

// save runs write and reads every preference back in one transaction, and returns the
// settings as stored. When either fails, the transaction rolls back and nothing is
// stored. The default profile is checked once the change is committed; a failure there
// is logged, and the settings name the stored profile.
func (s *Service) save(ctx context.Context, write func(context.Context, *prefs.Store) error) (Settings, error) {
	var stored map[string]string

	err := store.InTx(ctx, s.pool, func(tx *sql.Tx) error {
		inTx := s.prefs.WithTx(tx)
		if err := write(ctx, inTx); err != nil {
			return err
		}

		read, err := inTx.GetMany(ctx, settingKeys()...)
		stored = read

		return err
	})
	if err != nil {
		return Settings{}, err
	}

	out := fromStored(stored)

	if err = s.checkDefaultProfiles(ctx, &out); err != nil {
		s.logger.WarnContext(ctx, "the setting was saved, but the default profiles could not be checked", "error", err)
	}

	return out, nil
}

// setting returns a write that stores value under key.
func setting(key, value string) func(context.Context, *prefs.Store) error {
	return func(ctx context.Context, p *prefs.Store) error {
		return p.Set(ctx, key, value)
	}
}

// checkDefaultProfiles falls back to DefaultProfile for each agent whose default names a
// profile that is no longer stored (pruned, say), so the settings never name one the
// launch menu cannot offer.
func (s *Service) checkDefaultProfiles(ctx context.Context, out *Settings) error {
	for _, id := range agent.IDs() {
		name := out.DefaultProfiles[id]
		if name == DefaultProfile {
			continue
		}

		_, err := s.profiles.Lookup(ctx, id, name)

		switch {
		case errors.Is(err, profiles.ErrProfileNotFound):
			out.DefaultProfiles[id] = DefaultProfile
		case err != nil:
			return err
		}
	}

	return nil
}

// settingKeys returns every preference key the settings read.
func settingKeys() []string {
	keys := []string{KeyDefaultAgent}
	for _, id := range agent.IDs() {
		keys = append(keys, KeyDefaultProfile(id))
	}

	return append(append(keys,
		KeyWorktreeDefault, KeyWorktreeAutoCleanup, KeyTerminal, KeyCloneRoot, KeyTrustedBots,
		KeySnoozeDays, KeyWindowSize,
		snooze.PrefWakeCommits, snooze.PrefWakeReviews, snooze.PrefWakeComments,
		sensitive.PrefScope, sensitive.PrefOff, sensitive.PrefPatterns, KeyMenuBar, notifyprefs.KeyBannersOff, KeyTheme,
		KeyProjectsTipDismissed,
	), append(append(collapsedKeys(), orderKeys()...), branchrules.Keys()...)...)
}
