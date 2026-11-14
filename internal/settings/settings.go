// Package settings stores the user's preferences in the prefs table and serves them to
// the UI.
//
// Each preference is a typed key with a default, so a fresh database already has a
// complete set of settings. Setters validate their value before storing it and reject
// anything unknown with a sentinel error. Settings writes every key it declares, and the
// keys the trust, snooze, sensitive, notifyprefs, branchrules, reporules,
// integrationprefs, and threadrules packages declare and decode; see the prefs package for
// which package writes the others.
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
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/launcher/terminal"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/notifyprefs"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/prefs/prefval"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/reporules"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/snooze"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/threadrules"
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

	// KeyFollowSessions is whether the app follows the agent sessions it launches.
	KeyFollowSessions = "launch.followSessions"

	// KeyCodexEffort is whether a Codex launch passes the effort level it resolves to,
	// which Codex takes only as a config override.
	KeyCodexEffort = "agent.codex.effort"

	// KeyCodexFast is whether Codex launches start in Fast mode unless unticked.
	KeyCodexFast = "agent.codex.fast"

	// KeyCloneRoot is the directory whose children are the user's local clones, and the
	// one new clones go in. The other locations are under KeyCloneLocations.
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
	DefaultFollowSessions  = true
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

	// FollowSessions follows each agent session a launch starts from its item: what it
	// is doing, and what it last said. Sessions are recorded either way.
	FollowSessions bool `json:"followSessions"`

	// CodexEffort passes Codex the effort level a launch resolves to, and CodexFast
	// presets each Codex launch's Fast mode. Codex takes both only as config overrides,
	// and runs a session started with either outside its shared app server, where the app
	// can't see that it waits for the user, so both start off.
	CodexEffort bool `json:"codexEffort"`
	CodexFast   bool `json:"codexFast"`

	// CloneRoot is the clone root as entered, e.g. "~/projects": the folder new clones go
	// in, and the first place clones are looked for.
	CloneRoot string `json:"cloneRoot"`

	// CloneLocations are the other repository locations, in the order they were added:
	// each a folder looked into one level deep, as the clone root is, or a single clone,
	// with ~ for home. Never nil.
	CloneLocations []localrepos.Location `json:"cloneLocations"`

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

	// RepoRules are the pinned and the muted repositories and orgs: a pinned one's items
	// count as pinned in the inbox, and a muted one's are set apart in each list's Muted
	// tab and on the Release page (see the reporules package).
	RepoRules reporules.Rules `json:"repoRules"`

	// SensitiveCategories are the built-in categories in matching order, with their
	// patterns, for the settings page to describe.
	SensitiveCategories []sensitive.Category `json:"sensitiveCategories"`

	// MenuBar shows the app's icon in the menu bar, with how many items need the user and
	// a short list of them; closing the window then keeps the app running there.
	MenuBar bool `json:"menuBar"`

	// BannersOff lists the kinds of notification (see the notify package) whose banners
	// are off, the agent sessions' kinds among them until turned on. The notification
	// history keeps every inbox kind either way.
	BannersOff []notifyprefs.Kind `json:"bannersOff"`

	// Theme is the palette the app shows.
	Theme Theme `json:"theme"`

	// TextSize is the size of the app's text, apart from the size of its window.
	TextSize TextSize `json:"textSize"`

	// ToastSeconds is how long, in seconds, a message at the bottom of the window stays
	// before it closes itself, MinToastSeconds to MaxToastSeconds.
	ToastSeconds int `json:"toastSeconds"`

	// CollapsedSections lists the inbox sections the user collapsed, in the order the
	// inbox shows them.
	CollapsedSections []ListKey `json:"collapsedSections"`

	// ProjectsTipDismissed is set once the user puts away the inbox's tip about showing
	// the GitHub projects an issue is in.
	ProjectsTipDismissed bool `json:"projectsTipDismissed"`

	// ReviewTab is the review sheet's tab the user last chose, which it opens on.
	ReviewTab ReviewTab `json:"reviewTab"`

	// InboxOrder is which inbox lists are sorted by project status, and the order of the
	// statuses.
	InboxOrder InboxOrder `json:"inboxOrder"`

	// MergeMethod is how the review sheet merges a pull request.
	MergeMethod MergeMethod `json:"mergeMethod"`

	// Integrations is each integration's switch, and the copy of each tool the user chose.
	Integrations integrationprefs.Config `json:"integrations"`

	// GitHubNotifications are the rules for the user's GitHub notifications: whether the app
	// reads them, and whether, how, and when it clears one once the work behind it is
	// finished (see the threadrules package).
	GitHubNotifications threadrules.Rules `json:"githubNotifications"`

	// Refresh is how often, in seconds, the app reads the inbox, the running checks, the
	// worktrees, and the GitHub notifications again on its own, and RefreshChoices what each
	// can be set to.
	Refresh        RefreshIntervals `json:"refresh"`
	RefreshChoices RefreshChoices   `json:"refreshChoices"`
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
	git      execpath.Runner
	reader   *localrepos.Reader
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

// WithEvents emits the theme, text size, and menu bar changes into sink once they are
// stored.
func WithEvents(sink event.Sink) Option {
	return func(s *Service) { s.events = sink }
}

// WithGit reads a clone location's origin with git, one read-only git config, for the
// preview and the check of the repository locations. Without it, they name no repository.
func WithGit(git execpath.Runner) Option {
	return func(s *Service) { s.git = git }
}

// WithReader reads the other repository locations through reader, which bounds how long
// each read may take, so one that hangs reads as not answering rather than holding the
// check of the locations. Without it they are read directly.
func WithReader(reader *localrepos.Reader) Option {
	return func(s *Service) { s.reader = reader }
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
	values, err := checkTerminal(id)
	if err != nil {
		return Settings{}, err
	}

	return s.save(ctx, setMany(values, ""))
}

// checkTerminal refuses a terminal that is not one of the choices, and returns its write.
func checkTerminal(id terminal.ID) (map[string]string, error) {
	if !validTerminal(id) {
		return nil, fmt.Errorf("%w %q: choose %q or %q", ErrInvalidTerminal, id, TerminalITerm2, TerminalApp)
	}

	return map[string]string{KeyTerminal: string(id)}, nil
}

// FollowSessions returns whether the app follows the agent sessions it launches: the
// stored setting, or DefaultFollowSessions when none is stored or it cannot be read.
func (s *Service) FollowSessions(ctx context.Context) (bool, error) {
	stored, _, err := s.prefs.Get(ctx, KeyFollowSessions)
	if err != nil {
		return DefaultFollowSessions, fmt.Errorf("read whether to follow sessions: %w", err)
	}

	return prefval.Bool(stored, DefaultFollowSessions), nil
}

// SetFollowSessions stores whether the app follows the agent sessions it launches, and
// returns the settings with it. While it is off, no agent is asked about its sessions,
// and the window shows none; launches are still recorded. Once it is stored, an
// event.SessionsUpdated is emitted, so the window and the menu bar show the sessions, or
// stop showing them, at once.
func (s *Service) SetFollowSessions(ctx context.Context, on bool) (Settings, error) {
	saved, err := s.save(ctx, setting(KeyFollowSessions, prefval.EncodeBool(on)))
	if err != nil {
		return saved, err
	}

	s.events.Emit(event.SessionsUpdated{})

	return saved, nil
}

// SetCodexEffort stores whether a Codex launch passes the effort level it resolves to, and
// returns the settings with it. Off, Codex uses the effort in its own config.
func (s *Service) SetCodexEffort(ctx context.Context, on bool) (Settings, error) {
	return s.save(ctx, setting(KeyCodexEffort, prefval.EncodeBool(on)))
}

// SetCodexFast stores whether Codex launches start in Fast mode unless unticked, and
// returns the settings with it.
func (s *Service) SetCodexFast(ctx context.Context, on bool) (Settings, error) {
	return s.save(ctx, setting(KeyCodexFast, prefval.EncodeBool(on)))
}

// SetDefaultAgent stores the coding agent launches use when no launch default names one,
// and returns the settings with it.
func (s *Service) SetDefaultAgent(ctx context.Context, id agent.ID) (Settings, error) {
	values, err := checkDefaultAgent(id)
	if err != nil {
		return Settings{}, err
	}

	return s.save(ctx, setMany(values, ""))
}

// checkDefaultAgent refuses an agent this build does not know, and returns its write.
func checkDefaultAgent(id agent.ID) (map[string]string, error) {
	if !agent.Known(id) {
		return nil, fmt.Errorf("%w %q", ErrUnknownAgent, id)
	}

	return map[string]string{KeyDefaultAgent: string(id)}, nil
}

// SetDefaultProfile stores the agent's profile launches use when no launch default names
// one, and returns the settings with it. It must name one of the agent's stored profiles.
func (s *Service) SetDefaultProfile(ctx context.Context, id agent.ID, name string) (Settings, error) {
	values, err := s.checkDefaultProfile(ctx, id, name)
	if err != nil {
		return Settings{}, err
	}

	return s.save(ctx, setMany(values, ""))
}

// KeyDefaultProfile is the key of the agent's default profile, profile.default.<agent>.
func KeyDefaultProfile(id agent.ID) string {
	return keyDefaultProfilePrefix + string(id)
}

// SetWorktreeDefault stores whether launches start in a worktree unless unticked, and
// returns the settings with it.
func (s *Service) SetWorktreeDefault(ctx context.Context, on bool) (Settings, error) {
	return s.save(ctx, setting(KeyWorktreeDefault, prefval.EncodeBool(on)))
}

// SetWorktreeAutoCleanup stores whether finished worktrees are cleaned up on their own,
// and returns the settings with it.
func (s *Service) SetWorktreeAutoCleanup(ctx context.Context, on bool) (Settings, error) {
	return s.save(ctx, setting(KeyWorktreeAutoCleanup, prefval.EncodeBool(on)))
}

// SetBranchRules stores the rules for deleting local branches, and returns the settings
// with them. Every rule is stored together, so the rules never mix two saves; a protected
// pattern that cannot match a branch is refused, and nothing is stored.
func (s *Service) SetBranchRules(ctx context.Context, rules branchrules.Rules) (Settings, error) {
	values, err := rules.Encode()
	if err != nil {
		return Settings{}, err
	}

	return s.save(ctx, setMany(values, "save the branch rules"))
}

// SetCloneRoot stores the clone root, and returns the settings with it. It must be an
// absolute or ~-relative path to an existing directory; it is stored as entered (trimmed
// and cleaned), so "~/projects" stays portable across machines.
func (s *Service) SetCloneRoot(ctx context.Context, root string) (Settings, error) {
	values, err := s.checkCloneRoot(root)
	if err != nil {
		return Settings{}, err
	}

	return s.save(ctx, setMany(values, ""))
}

// SetTrustedBots stores the bots whose items are trusted, and returns the settings with
// them. Each login may be written as GitHub shows it ("dependabot", "dependabot[bot]", or
// "app/dependabot"); the list is stored normalized and sorted, and may be empty to trust
// no bot.
func (s *Service) SetTrustedBots(ctx context.Context, bots []string) (Settings, error) {
	values, err := checkTrustedBots(bots)
	if err != nil {
		return Settings{}, err
	}

	return s.save(ctx, setMany(values, ""))
}

// checkTrustedBots refuses a login that is not a bot's, and returns the write of the list,
// normalized and sorted.
func checkTrustedBots(bots []string) (map[string]string, error) {
	normalized, err := trust.NormalizeBots(bots)
	if err != nil {
		return nil, err
	}

	return map[string]string{KeyTrustedBots: trust.EncodeBots(normalized)}, nil
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
	values, err := checkSnoozeDays(days)
	if err != nil {
		return Settings{}, err
	}

	return s.save(ctx, setMany(values, ""))
}

// checkSnoozeDays refuses a snooze length outside snooze.MinDays to snooze.MaxDays, and
// returns its write.
func checkSnoozeDays(days int) (map[string]string, error) {
	if !snooze.ValidDays(days) {
		return nil, fmt.Errorf("%w: %d days; choose %d to %d", snooze.ErrInvalidDays, days, snooze.MinDays,
			snooze.MaxDays)
	}

	return map[string]string{KeySnoozeDays: strconv.Itoa(days)}, nil
}

// SetSnoozeWake stores which new activity ends a snooze early, and returns the settings
// with it. Every rule is stored together, so the rules never mix two saves.
func (s *Service) SetSnoozeWake(ctx context.Context, rules snooze.Rules) (Settings, error) {
	values, err := checkSnoozeWake(rules)
	if err != nil {
		return Settings{}, err
	}

	return s.save(ctx, setMany(values, "save the snooze rules"))
}

// checkSnoozeWake refuses wake rules out of range, and returns their writes.
func checkSnoozeWake(rules snooze.Rules) (map[string]string, error) {
	if err := rules.Validate(); err != nil {
		return nil, err
	}

	return rules.Encode(), nil
}

// SetSensitiveScope stores whose pull requests have their changed files checked,
// sensitive.ScopeOutside or sensitive.ScopeCollaborators to check collaborators' too, and
// returns the settings with it.
func (s *Service) SetSensitiveScope(ctx context.Context, scope sensitive.Scope) (Settings, error) {
	values, err := checkSensitiveScope(scope)
	if err != nil {
		return Settings{}, err
	}

	return s.save(ctx, setMany(values, ""))
}

// checkSensitiveScope refuses a scope that is not one of the choices, and returns its
// write.
func checkSensitiveScope(scope sensitive.Scope) (map[string]string, error) {
	if !scope.Valid() {
		return nil, fmt.Errorf("%w %q: choose %q or %q", sensitive.ErrInvalidScope, scope, sensitive.ScopeOutside,
			sensitive.ScopeCollaborators)
	}

	return map[string]string{sensitive.PrefScope: string(scope)}, nil
}

// checkSensitiveOff refuses a list of the built-in categories turned off that names one
// this build doesn't know or one that can't be turned off, and returns its write.
func checkSensitiveOff(ids []string) (map[string]string, error) {
	off := []string{}

	for _, id := range ids {
		var err error
		if off, err = sensitive.SetCategory(off, id, false); err != nil {
			return nil, err
		}
	}

	return map[string]string{sensitive.PrefOff: prefval.EncodeList(off)}, nil
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

			return prefval.EncodeList(off), nil
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
	values, err := checkSensitivePatterns(patterns)
	if err != nil {
		return Settings{}, err
	}

	return s.save(ctx, setMany(values, ""))
}

// checkSensitivePatterns refuses a pattern that can't be used, and returns the write of
// the patterns, normalized.
func checkSensitivePatterns(patterns []string) (map[string]string, error) {
	normalized, err := sensitive.NormalizePatterns(patterns)
	if err != nil {
		return nil, err
	}

	return map[string]string{sensitive.PrefPatterns: prefval.EncodeList(normalized)}, nil
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

// checkDefaultProfile refuses an agent this build does not know, and a profile not among
// the agent's stored profiles, and returns its write.
func (s *Service) checkDefaultProfile(ctx context.Context, id agent.ID, name string) (map[string]string, error) {
	if !agent.Known(id) {
		return nil, fmt.Errorf("%w %q", ErrUnknownAgent, id)
	}

	if _, err := s.profiles.Lookup(ctx, id, name); err != nil {
		if errors.Is(err, profiles.ErrProfileNotFound) {
			return nil, fmt.Errorf("%w %q", ErrUnknownProfile, name)
		}

		return nil, err
	}

	return map[string]string{KeyDefaultProfile(id): name}, nil
}

// checkCloneRoot refuses a clone root that is not an absolute or ~-relative path to an
// existing directory, and returns its write: the root as entered, trimmed and cleaned.
func (s *Service) checkCloneRoot(root string) (map[string]string, error) {
	root = strings.TrimSpace(root)

	dir, err := paths.ExpandHome(root, s.home)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidCloneRoot, err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidCloneRoot, err)
	}

	if !info.IsDir() {
		return nil, fmt.Errorf("%w: %s is not a directory", ErrInvalidCloneRoot, paths.Tilde(dir, s.home))
	}

	return map[string]string{KeyCloneRoot: filepath.Clean(root)}, nil
}

// fromStored builds Settings from stored values, falling back to the defaults.
func fromStored(stored map[string]string) Settings {
	rawBots, hasBots := stored[KeyTrustedBots]

	out := Settings{
		DefaultAgent:        DefaultAgent,
		DefaultProfiles:     map[agent.ID]string{},
		WorktreeDefault:     prefval.Bool(stored[KeyWorktreeDefault], DefaultWorktreeDefault),
		WorktreeAutoCleanup: prefval.Bool(stored[KeyWorktreeAutoCleanup], DefaultAutoCleanup),
		Terminal:            DefaultTerminal,
		FollowSessions:      prefval.Bool(stored[KeyFollowSessions], DefaultFollowSessions),
		CodexEffort:         prefval.Bool(stored[KeyCodexEffort], false),
		CodexFast:           prefval.Bool(stored[KeyCodexFast], false),
		CloneRoot:           storedCloneRoot(stored),
		CloneLocations:      decodeCloneLocations(stored[KeyCloneLocations]),
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
		RepoRules:                reporules.Decode(stored),
		MenuBar:                  prefval.Bool(stored[KeyMenuBar], DefaultMenuBar),
		BannersOff: notifyprefs.DecodeBannersOff(stored[notifyprefs.KeyBannersOff],
			stored[notifyprefs.KeyBannersOn]),
		Theme:             themes().decode(stored[KeyTheme]),
		TextSize:          textSizes().decode(stored[KeyTextSize]),
		ToastSeconds:      decodeToastSeconds(stored[KeyToastSeconds]),
		CollapsedSections: decodeCollapsed(stored),

		ProjectsTipDismissed: prefval.Bool(stored[KeyProjectsTipDismissed], false),
		ReviewTab:            reviewTabs().decode(stored[KeyReviewTab]),
		InboxOrder:           decodeInboxOrder(stored),
		MergeMethod:          mergeMethods().decode(stored[KeyMergeMethod]),
		Integrations:         integrationprefs.Decode(stored),
		GitHubNotifications:  threadrules.Decode(stored),
		Refresh:              decodeRefreshIntervals(stored),
		RefreshChoices:       refreshChoices(),
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

	if v := terminal.ID(stored[KeyTerminal]); validTerminal(v) {
		out.Terminal = v
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

// setMany returns a write that stores each of values under its key, all of them or none;
// a failure is wrapped as what, when it isn't "".
func setMany(values map[string]string, what string) func(context.Context, *prefs.Store) error {
	return func(ctx context.Context, p *prefs.Store) error {
		err := p.SetMany(ctx, values)
		if err != nil && what != "" {
			return fmt.Errorf("%s: %w", what, err)
		}

		return err
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
		KeyWorktreeDefault, KeyWorktreeAutoCleanup, KeyTerminal, KeyFollowSessions, KeyCodexEffort, KeyCodexFast,
		KeyCloneRoot, KeyCloneLocations, KeyTrustedBots,
		KeySnoozeDays, KeyWindowSize,
		snooze.PrefWakeCommits, snooze.PrefWakeReviews, snooze.PrefWakeComments,
		sensitive.PrefScope, sensitive.PrefOff, sensitive.PrefPatterns, KeyMenuBar, notifyprefs.KeyBannersOff, notifyprefs.KeyBannersOn, KeyTheme,
		KeyTextSize, KeyToastSeconds, KeyProjectsTipDismissed, KeyReviewTab, KeyMergeMethod, KeyRefreshInbox, KeyRefreshChecks, KeyRefreshWorktrees,
		KeyRefreshNotifications,
	), append(append(append(append(collapsedKeys(), orderKeys()...), branchrules.Keys()...), reporules.Keys()...),
		append(integrationprefs.Keys(), threadrules.Keys()...)...)...)
}
