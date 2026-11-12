package launcher

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/launcher/terminal"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// ItemStore reads the stored inbox items a launch is for, with their repositories and
// details, names the signed-in account, and remembers the clone each repository resolved
// to. The app adapts the inbox's store to it.
type ItemStore interface {
	// LaunchItems returns the stored items among ids, by id, leaving out any id with no
	// stored item.
	LaunchItems(ctx context.Context, ids []int64) ([]StoredItem, error)

	// ViewerLogin returns the signed-in account's login, or "" before the first refresh.
	ViewerLogin(ctx context.Context) (string, error)

	// RememberClone records path as the local clone of the repository with repoID.
	RememberClone(ctx context.Context, repoID int64, path string) error
}

// LaunchSettings is the part of the user's preferences a launch reads. Every field is a
// plain value.
type LaunchSettings struct {
	// DefaultAgent is the agent a launch starts when nothing more specific names one.
	DefaultAgent agent.ID

	// DefaultProfiles is each agent's profile a launch runs with when nothing more
	// specific names one.
	DefaultProfiles map[agent.ID]string

	// WorktreeDefault starts a launch in a worktree when nothing more specific decides.
	WorktreeDefault bool

	// Terminal is the ID of the terminal a launch opens in, such as terminal.ITerm2.
	Terminal terminal.ID

	// CloneRoot is the directory whose children are the user's local clones, as entered,
	// e.g. "~/projects". New clones go in it.
	CloneRoot string

	// CloneLocations are the user's other repository locations, as stored.
	CloneLocations []localrepos.Location

	// TrustedBots lists the bots whose items are trusted like a collaborator's (see the
	// trust package).
	TrustedBots []string

	// EffortOverrides passes the effort level to an agent that takes it only as a config
	// override (see agent.Agent.ConfigOverrides); off, such an agent uses its own.
	EffortOverrides bool

	// FastMode starts each session in Fast mode, where the agent has one, unless the
	// launch says otherwise.
	FastMode bool
}

// SettingsReader reads the launch settings as they are now, so every launch sees the
// user's latest choices. The app adapts the settings service to it.
type SettingsReader interface {
	Get(ctx context.Context) (LaunchSettings, error)
}

// ProfileReader reads the stored agent profiles, the default first and the rest by name.
// *profiles.Store implements it.
type ProfileReader interface {
	All(ctx context.Context) ([]profiles.Profile, error)
}

// PromptReader reads the prompt library: every stored prompt, and the app's default
// prompt for each kind of item. *prompts.Store implements it.
type PromptReader interface {
	Library(ctx context.Context) (prompts.Library, error)
}

// DefaultsReader reads every org's and repository's launch defaults.
// *launchdefaults.Store implements it.
type DefaultsReader interface {
	All(ctx context.Context) ([]launchdefaults.Defaults, error)
}

// Config holds a Service's dependencies. Clock and Launches are optional.
type Config struct {
	// Items reads the items launched on and the signed-in account, and remembers each
	// repository's clone.
	Items ItemStore

	// Settings reads the default profile, worktree default, terminal, clone root, and
	// trusted bots.
	Settings SettingsReader

	// Profiles reads the profiles a launch runs with.
	Profiles ProfileReader

	// Prompts reads the prompts a launch fills in, and the default prompts.
	Prompts PromptReader

	// Defaults reads the launch defaults of the item's owner and repository.
	Defaults DefaultsReader

	// Git runs the read-only `git config --get-regexp` remote lookup that finds and
	// validates clones. The launcher runs no other git command; the Worktrees maker runs
	// its own.
	Git execpath.Runner

	// GH runs `gh repo clone owner/name <dir>` when the user confirms cloning a
	// repository that has no local clone. It runs nothing else; without it, Clone fails.
	GH execpath.Runner

	// Finder finds the agent's binary.
	Finder execpath.Finder

	// Registry holds the terminal drivers for this OS.
	Registry terminal.Registry

	// Paths is where launch scripts and prompt files are written.
	Paths appdata.Paths

	// Home is the user's home directory; it expands a ~ clone root.
	Home string

	// Clock stamps launch files. It defaults to the system clock.
	Clock clock.Clock

	// Worktrees finds or makes the item's worktree a session starts in. Without it, a
	// launch in a worktree fails with ErrNoWorktrees.
	Worktrees WorktreeMaker

	// Sessions records each session started in a worktree, with its agent. Optional.
	Sessions SessionRecorder

	// Ledger records every session a launch starts, so the app follows it from its item,
	// and finds the session Resume reopens. Without it, nothing is followed and Resume
	// fails with ErrNoLedger.
	Ledger SessionLedger

	// NewID mints each session's own ID. It defaults to a random UUID.
	NewID func() string

	// Trust holds each agent's Truster. An agent without one is started without its
	// workspace trusted first.
	Trust map[agent.ID]Truster

	// Files checks the changed files of pull requests from outside the repository, so a
	// launch on one that changes agent files waits for the user to confirm, and its
	// prompt names its sensitive files. Without it, no launch is held back.
	Files FileChecker

	// Logger receives what a launch cannot report as its error: a session record or a
	// clone path that could not be stored, and old launch files that could not be
	// pruned. Without it they are not logged.
	Logger *slog.Logger
}

// LaunchRequest asks to start a session on one inbox item. Every choice is optional:
// a zero or nil one keeps what the item's launch settings resolve to.
type LaunchRequest struct {
	// ItemID is the item's local ID from the inbox snapshot.
	ItemID int64 `json:"itemId"`

	// Agent is the coding agent to start; agent.Inherit keeps the resolved agent.
	Agent agent.ID `json:"agent"`

	// Profile names the agent's profile to launch with; "" keeps the resolved profile.
	Profile string `json:"profile"`

	// PromptID is the prompt to fill in; 0 keeps the resolved prompt.
	PromptID int64 `json:"promptId"`

	// Model and Effort replace the resolved model and effort level; "" asks for the
	// agent's own default. nil keeps the resolved value.
	Model  *string `json:"model"`
	Effort *string `json:"effort"`

	// PlanMode starts the session in plan mode, or not; nil keeps the resolved setting.
	PlanMode *bool `json:"planMode"`

	// Fast starts the session in the agent's Fast mode, or not; nil keeps the resolved
	// setting. An agent without Fast mode ignores it.
	Fast *bool `json:"fast"`

	// Worktree starts the session in the item's worktree, or in the clone; nil keeps the
	// resolved setting.
	Worktree *bool `json:"worktree"`

	// FullAccessOverride lifts the outside-contributor restriction for this launch only.
	// The profile's own plan and permission settings still apply.
	FullAccessOverride bool `json:"fullAccessOverride"`

	// Confirmed is the Key of a Hold the user confirmed: the launch goes ahead if it
	// would be held back for exactly that reason again, and is held back otherwise.
	Confirmed string `json:"confirmed"`

	// Alongside is the Key of a HoldRunning hold the user confirmed: the launch starts
	// beside exactly those sessions, and is held back again for any other. It is kept
	// apart from Confirmed, so a launch held back for both reasons is confirmed for each.
	Alongside string `json:"alongside"`
}

// LaunchResult describes a started launch, or one held back. Every field is a plain
// value.
type LaunchResult struct {
	// Hold is set when the launch was held back for the user to confirm, and nothing was
	// started; the other fields are empty then.
	Hold *Hold `json:"hold"`

	// ScriptPath is the launch script the terminal was asked to run.
	ScriptPath string `json:"scriptPath"`

	// ClonePath is the clone the session starts in, or whose worktree it starts in,
	// shown relative to home with ~.
	ClonePath string `json:"clonePath"`

	// Dir is the directory the session starts in, shown relative to home with ~.
	Dir string `json:"dir"`

	// Worktree is the name of the item's worktree the session starts in, or "" when it
	// starts in the clone, and WorktreeAction what the launch did to it.
	Worktree       string         `json:"worktree"`
	WorktreeAction WorktreeAction `json:"worktreeAction"`

	// Notice says what the launch worked around, such as a worktree started from the
	// last fetched default branch, or "".
	Notice string `json:"notice"`

	// Agent is the agent the session runs.
	Agent agent.ID `json:"agent"`

	// Prompt is the nickname of the prompt the session was started with.
	Prompt string `json:"prompt"`

	// Profile is the profile the session was launched with.
	Profile string `json:"profile"`

	// Permission summarizes the session's permissions, e.g. "plan mode (outside contributor)".
	Permission string `json:"permission"`

	// Terminal is the terminal the launch opened in.
	Terminal terminal.ID `json:"terminal"`

	// SessionID is the session's own ID, which the app follows it by.
	SessionID string `json:"sessionId"`
}

// Preview is a launch planned but not started: what it resolved to, where it runs, the
// command it runs, and the prompt it passes. Launch runs exactly this plan.
type Preview struct {
	Resolution Resolution `json:"resolution"`

	// Dir is the directory the session starts in, with ~ for home.
	Dir string `json:"dir"`

	// Worktree is the item's worktree the session starts in, and what the launch would do
	// to it, or nil when it starts in the clone.
	Worktree *WorktreePlan `json:"worktree"`

	// Command is the agent's command line, quoted as a shell would read it, with the
	// prompt shown as <prompt> and home as ~.
	Command string `json:"command"`

	// Env lists the environment changes, such as "CODEX_HOME=~/.codex_work" or
	// "unset CLAUDE_CONFIG_DIR".
	Env []string `json:"env"`

	// Permission summarizes the session's permissions.
	Permission string `json:"permission"`

	// Prompt is the filled-in prompt, exactly as the session receives it.
	Prompt string `json:"prompt"`

	// Terminal is the terminal the launch opens in.
	Terminal terminal.ID `json:"terminal"`
}

// Service launches coding-agent sessions on inbox items.
type Service struct {
	items     ItemStore
	settings  SettingsReader
	profiles  ProfileReader
	prompts   PromptReader
	defaults  DefaultsReader
	git       execpath.Runner
	gh        execpath.Runner
	finder    execpath.Finder
	registry  terminal.Registry
	paths     appdata.Paths
	home      string
	clock     clock.Clock
	worktrees WorktreeMaker
	sessions  SessionRecorder
	ledger    SessionLedger
	newID     func() string
	trust     map[agent.ID]Truster
	files     FileChecker
	logger    *slog.Logger

	// cloneMu runs one Clone at a time.
	cloneMu sync.Mutex
}

// NewService returns a launch Service over cfg.
func NewService(cfg Config) *Service {
	if cfg.NewID == nil {
		cfg.NewID = newSessionID
	}

	return &Service{
		items:     cfg.Items,
		settings:  cfg.Settings,
		profiles:  cfg.Profiles,
		prompts:   cfg.Prompts,
		defaults:  cfg.Defaults,
		git:       cfg.Git,
		gh:        cfg.GH,
		finder:    cfg.Finder,
		registry:  cfg.Registry,
		paths:     cfg.Paths,
		home:      cfg.Home,
		clock:     clock.Or(cfg.Clock),
		worktrees: cfg.Worktrees,
		sessions:  cfg.Sessions,
		ledger:    cfg.Ledger,
		newID:     cfg.NewID,
		trust:     cfg.Trust,
		files:     cfg.Files,
		logger:    logging.Or(cfg.Logger),
	}
}

// Resolve returns the launch settings for one item with the request's choices applied,
// each with where it came from. It reads only the database: it looks for no clone and
// writes nothing.
func (s *Service) Resolve(ctx context.Context, req LaunchRequest) (Resolution, error) {
	w, err := s.loadWorld(ctx)
	if err != nil {
		return Resolution{}, err
	}

	row, err := s.loadItem(ctx, req.ItemID)
	if err != nil {
		return Resolution{}, err
	}

	res, _, _, err := resolve(w, row.subject(w.policy), req)

	return res, err
}

// ResolveItems returns each item's default launch settings, as Resolve with no choices
// would, for the inbox's launch buttons. An item whose settings cannot resolve, such as
// one no prompt applies to, carries the reason in Problem; items no longer stored are
// left out.
func (s *Service) ResolveItems(ctx context.Context, ids []int64) ([]ItemResolution, error) {
	out := []ItemResolution{}
	if len(ids) == 0 {
		return out, nil
	}

	w, err := s.loadWorld(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := s.loadItems(ctx, ids)
	if err != nil {
		return nil, err
	}

	for _, row := range rows {
		res, _, _, resolveErr := resolve(w, row.subject(w.policy), LaunchRequest{ItemID: row.ID})
		if resolveErr != nil {
			out = append(out, ItemResolution{
				Resolution: row.unresolved(inheritedAgent(w, row.subject(w.policy))), Problem: uierr.Message(resolveErr),
			})

			continue
		}

		out = append(out, ItemResolution{Resolution: res})
	}

	return out, nil
}

// ItemResolution is one item's default launch settings, or why they cannot resolve.
type ItemResolution struct {
	Resolution

	// Problem says why the item cannot be launched as it stands, or "".
	Problem string `json:"problem"`
}

// Preview plans a launch without writing files or opening a terminal: the resolved
// settings, the clone, the command, and the filled-in prompt. Launch runs the same plan,
// so the prompt shown is the prompt sent, except that Launch checks a pull request's
// files against GitHub again where Preview uses the last check.
func (s *Service) Preview(ctx context.Context, req LaunchRequest) (Preview, error) {
	p, err := s.plan(ctx, req, planPreview, sessionRef{})
	if err != nil {
		return Preview{}, err
	}

	return p.preview(s.home), nil
}

// Launch starts a session on an inbox item:
//
//  1. load the item (computing its trust) and resolve its launch settings: the agent
//     first, then the request's choices, the prompt's, the repository's and its owner's
//     launch defaults, the profile's, and the app's settings;
//  2. find the item's local clone: the cached path when it still is a clone of the
//     repository, otherwise a scan of the clone root, whose result is cached;
//  3. find the agent and, for a worktree launch, what its worktree would be, and build
//     the plan, and hold the launch back, starting nothing and asking GitHub nothing,
//     while a session the app follows runs in the folder it would start in, unless the
//     request confirms that hold;
//  4. for a pull request whose files are checked, check them against GitHub as of now,
//     and fill in the prompt, which names its sensitive files;
//  5. hold the launch back, starting nothing, when the pull request changes agent files
//     or its files could not all be checked, unless the request confirms that hold;
//  6. find or make the item's worktree, which fetches the pull request's head, or the
//     default branch for an issue, only when there is none yet (see WorktreeMaker);
//  7. mark the directory trusted in the agent config the session runs with, since the
//     agent would otherwise stop at its workspace trust question;
//  8. write the prompt file and launch script;
//  9. open the script in the terminal chosen in settings;
//  10. record the session in the worktree, with its agent, and in the ledger, under the
//     ID minted for it, so the item's card follows it.
//
// The launcher itself runs only the read-only `git -C <clone> config --get-regexp` remote
// lookup. The script only changes into the directory and runs the agent; it never checks
// out, stashes, fetches, switches branches, or creates worktrees.
func (s *Service) Launch(ctx context.Context, req LaunchRequest) (LaunchResult, error) {
	ref := sessionRef{id: s.newID()}

	p, err := s.plan(ctx, req, planLaunch, ref)
	if err != nil {
		return LaunchResult{}, err
	}

	if hold := s.heldBack(p, req.Confirmed); hold != nil {
		return LaunchResult{Hold: hold}, nil
	}

	if err = s.ensureWorktree(ctx, &p); err != nil {
		return LaunchResult{}, err
	}

	return s.start(ctx, p, ref, "")
}

// heldBack returns what holds a planned launch or resume back: the sessions running where
// it would start, then the pull request's files unless confirmed names that hold, or nil.
func (s *Service) heldBack(p planned, confirmed string) *Hold {
	if p.running != nil {
		return p.running
	}

	hold := holdFor(p.check, p.atHead(), paths.Tilde(p.plan.Dir, s.home), p.worktreeName())
	if hold != nil && hold.Key != confirmed {
		return hold
	}

	return nil
}

// planned is a launch worked out but not started.
type planned struct {
	row    launchItemRow
	res    Resolution
	plan   Plan
	prompt string
	clone  Clone
	driver terminal.Driver

	// worktree is the item's worktree the session starts in, or would, or nil when it
	// starts in the clone.
	worktree *Worktree

	// check is the pull request's file check, or nil when its files are not checked.
	check *sensitive.Check

	// running holds a launch back for the sessions running where it would start, or is
	// nil. A plan held back for them is not finished: it has no file check or prompt.
	running *Hold
}

// worktreeName is the item's worktree name, or "" when the session starts in the clone.
func (p planned) worktreeName() string {
	if p.worktree == nil {
		return ""
	}

	return item.WorktreeName(p.row.Kind, p.row.Number)
}

// atHead reports a session that starts in a pull request's worktree, which holds its
// files from the moment it starts.
func (p planned) atHead() bool {
	return p.worktree != nil && p.row.Kind == item.KindPR
}

// checkedHead is the pull request's head commit whose files the launch checked, or "".
func (p planned) checkedHead() string {
	if p.check == nil || p.row.Kind != item.KindPR {
		return ""
	}

	return strings.ToLower(p.check.Head)
}

// onPRHead reports a session that starts in a pull request's worktree at its head: one
// made now, from the head fetched for it, or one whose commit is the head the launch
// checked or the last refresh read. A worktree resumed after the pull request moved on,
// or taken over from an older session, is not, so its prompt asks the agent to check
// the pull request out.
func (p planned) onPRHead() bool {
	if !p.atHead() {
		return false
	}

	if p.worktree.Action == WorktreeCreate {
		return true
	}

	head := cmp.Or(p.checkedHead(), strings.ToLower(p.row.HeadOID))

	return head != "" && strings.EqualFold(p.worktree.Head, head)
}

// worktreeRequest asks for the item's worktree in the launch's clone, made at the head
// the launch checked when it checked one.
func (p planned) worktreeRequest() WorktreeRequest {
	return WorktreeRequest{
		Clone: p.clone.Dir, Remote: p.clone.Remote, Owner: p.row.Owner, Name: p.row.Name, Kind: p.row.Kind,
		Number: p.row.Number, DefaultBranch: p.row.DefaultBranch, Head: p.checkedHead(),
	}
}

// planMode is what a plan is for.
type planMode int

// The plans: a preview, which is never held back and asks GitHub nothing; a launch, held
// back for a session running where it starts and for the pull request's files as GitHub
// has them now; and a resume, held back for both too, with the files as last checked, so
// it asks GitHub nothing either.
const (
	planPreview planMode = iota
	planLaunch
	planResume
)

// plan works out a launch of the session ref names. The terminal driver is chosen before
// the clone is resolved, so an OS without terminal support fails before anything runs or
// is written. A pull request's files are checked last, against GitHub for a launch, so a
// launch that cannot be planned asks GitHub nothing. A resume has no prompt to fill in.
func (s *Service) plan(ctx context.Context, req LaunchRequest, mode planMode, ref sessionRef) (planned, error) {
	w, err := s.loadWorld(ctx)
	if err != nil {
		return planned{}, err
	}

	row, err := s.loadItem(ctx, req.ItemID)
	if err != nil {
		return planned{}, err
	}

	res, profile, prompt, err := resolve(w, row.subject(w.policy), req)
	if err != nil {
		return planned{}, err
	}

	tmpl, err := prompt.Template()
	if err != nil {
		return planned{}, err
	}

	p, err := s.place(ctx, w, row, res, Request{
		Item: row.item(w.policy), Agent: res.Agent.ID, Profile: effectiveProfile(profile, res),
		FullAccessOverride: req.FullAccessOverride, SessionName: sessionName(prompt.Nickname, row),
		SessionID: ref.id, ResumeID: ref.resume, Fast: res.Fast.Value,
	})
	if err != nil {
		return planned{}, err
	}

	// A launch held back for a running session asks GitHub nothing: confirmed, it plans
	// again and checks the files then. A preview is never held.
	if p.running = s.runningHold(ctx, p, req.Alongside, mode != planPreview); p.running != nil {
		return p, nil
	}

	fresh := mode == planLaunch
	if p.check, err = s.fileCheck(ctx, row.ID, fresh); err != nil && !errors.Is(err, sensitive.ErrNotChecked) {
		return planned{}, err
	}

	if p.plan.NoPrompt {
		return p, nil
	}

	p.prompt, err = s.fillIn(ctx, row.promptData(w.policy, profile.Name, "", p.check), prompt.Nickname, tmpl, p)

	return p, err
}

// place works out where and how a launch runs: the terminal driver, the clone, the
// item's worktree when the launch uses one, the agent binary, and the plan for req, which
// needs only its directory and binary filled in. The worktree is only planned here; Launch
// makes it once the launch is not held back.
func (s *Service) place(ctx context.Context, w world, row launchItemRow, res Resolution, req Request) (planned, error) {
	p := planned{row: row, res: res}

	var err error

	if p.driver, err = s.registry.Get(w.settings.Terminal); err != nil {
		return planned{}, err
	}

	roots, err := w.settings.roots(s.home)
	if err != nil {
		return planned{}, err
	}

	if p.clone, err = s.resolveClone(ctx, row.repoRef(), roots); err != nil {
		return planned{}, err
	}

	if req.BinaryPath, err = s.finder.Find(res.Agent.Command); err != nil {
		return planned{}, uierr.New(fmt.Sprintf("%s isn't installed (%s wasn't found).", res.Agent.Name,
			res.Agent.Command), fmt.Errorf("find %s: %w", res.Agent.Command, err))
	}

	req.Dir = p.clone.Dir

	if res.Worktree.Value {
		if s.worktrees == nil {
			return planned{}, ErrNoWorktrees
		}

		wt, planErr := s.worktrees.PlanWorktree(ctx, p.worktreeRequest())
		if planErr != nil {
			return planned{}, planErr
		}

		p.worktree, req.Dir = &wt, wt.Path
	}

	if p.plan, err = BuildPlan(req); err != nil {
		return planned{}, fmt.Errorf("plan the launch: %w", err)
	}

	return p, nil
}

// ensureWorktree finds or makes the item's worktree the planned launch starts in, and
// starts the plan there. A launch in the clone has nothing to make.
func (s *Service) ensureWorktree(ctx context.Context, p *planned) error {
	if p.worktree == nil {
		return nil
	}

	wt, err := s.worktrees.EnsureWorktree(ctx, p.worktreeRequest())
	if err != nil {
		return err
	}

	p.worktree, p.plan.Dir = &wt, wt.Path

	return nil
}

// fileCheck returns an item's file check: as of now when fresh is set, else the last
// one. It returns sensitive.ErrNotChecked for an item whose files are not checked, and
// for every item when the service has no FileChecker.
func (s *Service) fileCheck(ctx context.Context, itemID int64, fresh bool) (*sensitive.Check, error) {
	if s.files == nil {
		return nil, sensitive.ErrNotChecked
	}

	var (
		check *sensitive.Check
		err   error
	)

	if fresh {
		check, err = s.files.CheckFiles(ctx, itemID)
	} else {
		check, err = s.files.StoredFiles(ctx, itemID)
	}

	switch {
	case errors.Is(err, sensitive.ErrNotChecked):
		return nil, err
	case err != nil:
		return nil, fmt.Errorf("check the pull request's files: %w", err)
	default:
		return check, nil
	}
}

// recordSession records the session started in the item's worktree, with its agent. The
// launch has started by then, and a missing record only means the Worktrees view shows
// an older session, so a failure is logged rather than returned.
func (s *Service) recordSession(ctx context.Context, p planned) {
	if s.sessions == nil || p.worktree == nil {
		return
	}

	err := s.sessions.Touch(ctx, p.clone.Dir, p.worktreeName(), p.res.Agent.ID, s.clock.Now())
	if err != nil {
		s.logger.WarnContext(ctx, "could not record the session", "error", err)
	}
}

// trustDir marks the directory the session starts in trusted in the agent config the
// session reads (the profile's config directory, or the agent's default), with the clone
// too when that is a worktree, since an agent may judge a worktree by its repository. The
// agent would otherwise stop at its workspace trust question, or start with less access
// than the launch chose.
func (s *Service) trustDir(ctx context.Context, p planned) error {
	truster, ok := s.trust[p.res.Agent.ID]
	if !ok || truster == nil {
		return nil
	}

	dirs := []string{p.plan.Dir}
	if p.plan.Dir != p.clone.Dir {
		dirs = append(dirs, p.clone.Dir)
	}

	if err := truster.Trust(ctx, p.plan.Env[p.res.Agent.ConfigEnv], dirs); err != nil {
		return fmt.Errorf("trust %s for %s: %w", paths.Tilde(p.plan.Dir, s.home), p.res.Agent.Name, err)
	}

	return nil
}

// fillIn fills in a launch's prompt from the item's data, adding the signed-in account
// and the planned session.
func (s *Service) fillIn(ctx context.Context, data prompts.Data, nickname string, tmpl *prompts.Template,
	p planned,
) (string, error) {
	viewer, err := s.items.ViewerLogin(ctx)
	if err != nil {
		return "", err
	}

	data.Me = viewer
	data = data.WithSession(p.worktree != nil, p.onPRHead())

	text, err := tmpl.Render(data)
	if err != nil {
		return "", fmt.Errorf("fill in the %q prompt: %w", nickname, err)
	}

	return text, nil
}

// preview describes a planned launch for the UI, with home shown as ~.
func (p planned) preview(home string) Preview {
	args := make([]string, 0, len(p.plan.Args))
	for _, arg := range p.plan.Args {
		args = append(args, tildeIn(arg, home))
	}

	env := make([]string, 0, len(p.plan.Unset)+len(p.plan.Env))
	for _, name := range p.plan.Unset {
		env = append(env, "unset "+name)
	}

	for _, name := range slices.Sorted(maps.Keys(p.plan.Env)) {
		env = append(env, name+"="+tildeIn(p.plan.Env[name], home))
	}

	command := p.res.Agent.Command + " <prompt> " + agent.Join(args)
	if p.plan.PromptLast {
		command = p.res.Agent.Command + " " + agent.Join(args) + " <prompt>"
	}

	preview := Preview{
		Resolution: p.res,
		Dir:        paths.Tilde(p.plan.Dir, home),
		Command:    strings.Join(strings.Fields(command), " "),
		Env:        env,
		Permission: p.plan.Permission,
		Prompt:     p.prompt,
		Terminal:   p.driver.ID(),
	}

	if wt := p.worktree; wt != nil {
		preview.Worktree = &WorktreePlan{
			Name: p.worktreeName(), Path: paths.Tilde(wt.Path, home), Branch: wt.Branch, Action: wt.Action,
			Base: wt.Base, Dirty: wt.Dirty,
		}
	}

	return preview
}

// tildeIn writes home as ~ wherever it appears in s, for display.
func tildeIn(s, home string) string {
	if home == "" || home == "/" {
		return s
	}

	return strings.ReplaceAll(s, home, "~")
}

// sessionName names a session in the agent's prompt box, its resume list, and the
// terminal's title: the prompt's nickname, then the repository and the item's number.
func sessionName(nickname string, row launchItemRow) string {
	return fmt.Sprintf("%s · %s#%d", nickname, row.Name, row.Number)
}

// roots builds the repository locations the settings name.
func (l LaunchSettings) roots(home string) (localrepos.Roots, error) {
	return localrepos.NewRoots(l.CloneRoot, l.CloneLocations, home)
}

// resolveClone finds the clone a launch starts in. A cached path is used while it is
// still a clone of the repository; otherwise the repository locations are scanned and
// the match is cached for next time.
func (s *Service) resolveClone(ctx context.Context, ref repoRef, roots localrepos.Roots) (Clone, error) {
	if clone, ok := s.cachedClone(ctx, ref); ok {
		return clone, nil
	}

	if err := ctx.Err(); err != nil {
		return Clone{}, err
	}

	match, err := localrepos.Resolver{Roots: roots, Home: s.home, Git: s.git}.Resolve(ctx, ref.name)
	if err != nil {
		return Clone{}, err
	}

	if match.Repo.Path != ref.localPath {
		s.cacheClonePath(ctx, ref.id, match.Repo.Path)
	}

	return Clone{Dir: match.Repo.Path, Remote: match.MatchedRemote}, nil
}

// cachedClone validates the cached clone path: it must still be a git working tree with
// a remote that points at the repository.
func (s *Service) cachedClone(ctx context.Context, ref repoRef) (Clone, bool) {
	if ref.localPath == "" {
		return Clone{}, false
	}

	local, err := localrepos.Inspect(ctx, ref.localPath, s.git)
	if err != nil {
		return Clone{}, false
	}

	remote, ok := local.RemoteFor(ref.name)
	if !ok {
		return Clone{}, false
	}

	return Clone{Dir: local.Path, Remote: remote}, true
}

// cacheClonePath remembers a repository's clone. A failure only costs a rescan next
// time, so it is logged rather than failing the launch.
func (s *Service) cacheClonePath(ctx context.Context, repoID int64, path string) {
	if err := s.items.RememberClone(ctx, repoID, path); err != nil {
		s.logger.WarnContext(ctx, "could not remember a repository's local clone", "error", err)
	}
}
