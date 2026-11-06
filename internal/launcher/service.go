package launcher

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/mrz1836/nilbox/internal/agentflags"
	"github.com/mrz1836/nilbox/internal/agents"
	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/launchlog"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/workspacetrust"
)

// claudeBin is the command Claude Code runs as.
const claudeBin = "claude"

// cacheClonePathSQL remembers the clone a repository resolved to.
const cacheClonePathSQL = `UPDATE repos SET local_path = ?, updated_at = ? WHERE id = ?`

// ErrItemNotFound is returned when the launched item is not in the local inbox.
var ErrItemNotFound = errors.New("item not found; refresh the inbox and try again")

// SettingsReader reads the user's preferences. *settings.Service implements it.
type SettingsReader interface {
	Get(ctx context.Context) (settings.Settings, error)
}

// Finder resolves a program name to an executable's absolute path. execpath.Finder
// implements it.
type Finder interface {
	Find(name string) (string, error)
}

// Clock tells the time.
type Clock interface {
	Now() time.Time
}

// LaunchRecorder records the worktrees sessions are launched in. *launchlog.Store
// implements it.
type LaunchRecorder interface {
	Record(ctx context.Context, l launchlog.Launch) error
}

// Config holds a Service's dependencies. Clock and Launches are optional.
type Config struct {
	// DB is the app database: items, repositories, profiles, prompts, and launch
	// defaults.
	DB *gorm.DB

	// Settings reads the default profile, worktree default, terminal, and clone root.
	Settings SettingsReader

	// Git runs the read-only `git config --get-regexp` remote lookup that finds and
	// validates clones. The launcher runs no other git command.
	Git localrepos.Runner

	// GH runs `gh repo clone owner/name <dir>` when the user confirms cloning a
	// repository that has no local clone. It runs nothing else; without it, Clone fails.
	GH localrepos.Runner

	// Finder finds the agent binary.
	Finder Finder

	// Registry holds the terminal drivers for this OS.
	Registry Registry

	// Paths is where launch scripts and prompt files are written.
	Paths appdata.Paths

	// Home is the user's home directory; it expands a ~ clone root.
	Home string

	// Clock stamps launch files. It defaults to the system clock.
	Clock Clock

	// Launches records each worktree a session is launched in, and the item it is for,
	// so the Worktrees view links it and its cleanup finds it.
	Launches LaunchRecorder
}

// LaunchRequest asks to start a session on one inbox item. Every choice is optional:
// a zero or nil one keeps what the item's launch settings resolve to.
type LaunchRequest struct {
	// ItemID is the item's local ID from the inbox snapshot.
	ItemID int64 `json:"itemId"`

	// Profile names the profile to launch with; "" keeps the resolved profile.
	Profile string `json:"profile"`

	// PromptID is the prompt to fill in; 0 keeps the resolved prompt.
	PromptID int64 `json:"promptId"`

	// Model and Effort replace the resolved --model and --effort; "" asks for the
	// agent's own default. nil keeps the resolved value.
	Model  *string `json:"model"`
	Effort *string `json:"effort"`

	// PlanMode starts the session in plan mode, or not; nil keeps the resolved setting.
	PlanMode *bool `json:"planMode"`

	// Worktree starts the session in an agent-created worktree, or not; nil keeps the
	// resolved setting.
	Worktree *bool `json:"worktree"`

	// FullAccessOverride lifts the outside-contributor restriction for this launch only.
	// The profile's own plan and permission settings still apply.
	FullAccessOverride bool `json:"fullAccessOverride"`
}

// LaunchResult describes a started launch. Every field is a plain value.
type LaunchResult struct {
	// ScriptPath is the launch script the terminal was asked to run.
	ScriptPath string `json:"scriptPath"`

	// ClonePath is the clone the session starts in, shown relative to home with ~.
	ClonePath string `json:"clonePath"`

	// Worktree is the name of the worktree the agent creates or resumes under the
	// clone's .claude/worktrees directory, or "" when the session starts in the clone.
	Worktree string `json:"worktree"`

	// Prompt is the nickname of the prompt the session was started with.
	Prompt string `json:"prompt"`

	// Profile is the profile the session was launched with.
	Profile string `json:"profile"`

	// Permission summarizes the session's permissions, e.g. "plan mode (outside contributor)".
	Permission string `json:"permission"`

	// Terminal is the terminal the launch opened in.
	Terminal string `json:"terminal"`
}

// Preview is a launch planned but not started: what it resolved to, where it runs, the
// command it runs, and the prompt it passes. Launch runs exactly this plan.
type Preview struct {
	Resolution Resolution `json:"resolution"`

	// Dir is the directory the session starts in, with ~ for home.
	Dir string `json:"dir"`

	// Worktree is the worktree the agent creates or resumes, or "".
	Worktree string `json:"worktree"`

	// Command is the agent's command line, quoted as a shell would read it, with the
	// prompt shown as <prompt> and home as ~.
	Command string `json:"command"`

	// Env lists the environment changes, such as "CLAUDE_CONFIG_DIR=~/.claude_work" or
	// "unset CLAUDE_CONFIG_DIR".
	Env []string `json:"env"`

	// Permission summarizes the session's permissions.
	Permission string `json:"permission"`

	// Prompt is the filled-in prompt, exactly as the session receives it.
	Prompt string `json:"prompt"`

	// Terminal is the terminal the launch opens in.
	Terminal string `json:"terminal"`
}

// Service launches coding-agent sessions on inbox items.
type Service struct {
	db       *gorm.DB
	settings SettingsReader
	git      localrepos.Runner
	gh       localrepos.Runner
	finder   Finder
	registry Registry
	paths    appdata.Paths
	home     string
	clock    Clock
	launches LaunchRecorder

	// cloneMu runs one Clone at a time.
	cloneMu sync.Mutex
}

// NewService returns a launch Service over cfg.
func NewService(cfg Config) *Service {
	clock := cfg.Clock
	if clock == nil {
		clock = systemClock{}
	}

	return &Service{
		db:       cfg.DB,
		settings: cfg.Settings,
		git:      cfg.Git,
		gh:       cfg.GH,
		finder:   cfg.Finder,
		registry: cfg.Registry,
		paths:    cfg.Paths,
		home:     cfg.Home,
		clock:    clock,
		launches: cfg.Launches,
	}
}

// Resolve returns the launch settings for one item with the request's choices applied,
// each with where it came from. It reads only the database: it looks for no clone and
// writes nothing.
func (s *Service) Resolve(ctx context.Context, req LaunchRequest) (Resolution, error) {
	w, err := loadWorld(ctx, s.db, s.settings)
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

	w, err := loadWorld(ctx, s.db, s.settings)
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
			res = Resolution{ItemID: row.ID, Kind: row.Kind, Owner: row.Owner, Repo: row.Owner + "/" + row.Name}
			out = append(out, ItemResolution{Resolution: res, Problem: sentenceCase(resolveErr.Error())})

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
// so the prompt shown is the prompt sent.
func (s *Service) Preview(ctx context.Context, req LaunchRequest) (Preview, error) {
	p, err := s.plan(ctx, req)
	if err != nil {
		return Preview{}, err
	}

	return p.preview(s.home), nil
}

// Launch starts a session on an inbox item:
//
//  1. load the item (computing its trust) and resolve its launch settings: the
//     request's choices, then the prompt's, the repository's and its owner's launch
//     defaults, the profile's, and the app's settings;
//  2. find the item's local clone: the cached path when it still is a clone of the
//     repository, otherwise a scan of the clone root, whose result is cached;
//  3. find the agent, build the plan, and fill in the prompt;
//  4. mark the clone trusted in the agent config the session runs with, since a
//     worktree session cannot show the agent's workspace trust dialog;
//  5. write the prompt file and launch script;
//  6. open the script in the terminal chosen in settings;
//  7. record the worktree, if the session starts in one, with the item it is for.
//
// The only git command it runs is the read-only `git -C <clone> config --get-regexp`
// remote lookup. The script only changes into the clone and runs the agent; it never
// checks out, stashes, fetches, switches branches, or creates worktrees.
func (s *Service) Launch(ctx context.Context, req LaunchRequest) (LaunchResult, error) {
	p, err := s.plan(ctx, req)
	if err != nil {
		return LaunchResult{}, err
	}

	if err = s.trustClone(ctx, p); err != nil {
		return LaunchResult{}, err
	}

	files, err := WriteLaunchFiles(s.paths, p.plan, p.prompt, s.clock.Now())
	if err != nil {
		return LaunchResult{}, err
	}

	if err = p.driver.Open(ctx, files.Script); err != nil {
		return LaunchResult{}, err
	}

	s.recordLaunch(ctx, p)

	return LaunchResult{
		ScriptPath: files.Script,
		ClonePath:  localrepos.TildePath(p.clone.Dir, s.home),
		Worktree:   worktreeDirName(p.plan.Worktree),
		Prompt:     p.res.Prompt.Nickname,
		Profile:    p.res.Profile.Value,
		Permission: p.plan.Permission,
		Terminal:   p.driver.ID(),
	}, nil
}

// planned is a launch worked out but not started.
type planned struct {
	row    launchItemRow
	res    Resolution
	plan   Plan
	prompt string
	clone  Clone
	driver Driver
}

// plan works out a launch. The terminal driver is chosen before the clone is resolved,
// so an OS without terminal support fails before anything runs or is written.
func (s *Service) plan(ctx context.Context, req LaunchRequest) (planned, error) {
	w, err := loadWorld(ctx, s.db, s.settings)
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

	p := planned{row: row, res: res}

	if p.driver, err = s.registry.Get(w.settings.Terminal); err != nil {
		return planned{}, err
	}

	if p.clone, err = s.resolveClone(ctx, row.repoRef(), w.settings.CloneRoot); err != nil {
		return planned{}, err
	}

	bin, err := s.finder.Find(res.Agent.Command)
	if err != nil {
		return planned{}, fmt.Errorf("find the agent: %w", err)
	}

	p.plan, err = BuildPlan(Request{
		Item: row.item(w.policy), Profile: effectiveProfile(profile, res), Clone: p.clone, Worktree: res.Worktree.Value,
		FullAccessOverride: req.FullAccessOverride, ClaudePath: bin, SessionName: sessionName(prompt.Nickname, row),
	})
	if err != nil {
		return planned{}, fmt.Errorf("plan the launch: %w", err)
	}

	if p.prompt, err = s.fillIn(ctx, row.promptData(w.policy, profile.Name, ""), prompt.Nickname, tmpl, p.plan); err != nil {
		return planned{}, err
	}

	return p, nil
}

// recordLaunch records the worktree a session was launched in, with its item. The launch
// has started by then, and a missing record only means the worktree is linked by its
// name, so a failure is logged rather than returned.
func (s *Service) recordLaunch(ctx context.Context, p planned) {
	if s.launches == nil || p.plan.Worktree == "" {
		return
	}

	err := s.launches.Record(ctx, launchlog.Launch{
		ClonePath: p.clone.Dir, Worktree: worktreeDirName(p.plan.Worktree), Owner: p.row.Owner, Name: p.row.Name,
		Kind: p.row.Kind, Number: p.row.Number, LaunchedAt: s.clock.Now(),
	})
	if err != nil {
		slog.WarnContext(ctx, "could not record the worktree launch", "error", err)
	}
}

// trustClone marks the clone trusted in the agent config the session reads (the
// profile's CLAUDE_CONFIG_DIR, or home for the default profile), so the agent starts
// without its workspace trust dialog. A --worktree session cannot show that dialog: it
// exits with "Workspace trust not yet accepted" in a repository not trusted yet.
func (s *Service) trustClone(ctx context.Context, p planned) error {
	if p.res.Agent.ID != agents.Claude {
		return nil
	}

	file := workspacetrust.ConfigFile(p.plan.Env[envConfigDir], s.home)

	if _, err := workspacetrust.Grant(ctx, file, p.clone.Dir); err != nil {
		return fmt.Errorf("trust %s for the agent: %w", localrepos.TildePath(p.clone.Dir, s.home), err)
	}

	return nil
}

// fillIn fills in a launch's prompt from the item's data, adding the signed-in account
// and the planned session.
func (s *Service) fillIn(ctx context.Context, data prompts.Data, nickname string, tmpl *prompts.Template,
	plan Plan,
) (string, error) {
	viewer, err := inbox.ViewerLogin(ctx, s.db)
	if err != nil {
		return "", err
	}

	data.Me = viewer
	data = data.WithSession(plan.Worktree != "", plan.WorktreeOnPRHead)

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

	return Preview{
		Resolution: p.res,
		Dir:        localrepos.TildePath(p.clone.Dir, home),
		Worktree:   worktreeDirName(p.plan.Worktree),
		Command:    strings.TrimSpace(p.res.Agent.Command + " <prompt> " + agentflags.Join(args)),
		Env:        env,
		Permission: p.plan.Permission,
		Prompt:     p.prompt,
		Terminal:   p.driver.ID(),
	}
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

// resolveClone finds the clone a launch starts in. A cached path is used while it is
// still a clone of the repository; otherwise the clone root is scanned and the match is
// cached for next time.
func (s *Service) resolveClone(ctx context.Context, repo repoRef, root string) (Clone, error) {
	if clone, ok := s.cachedClone(ctx, repo); ok {
		return clone, nil
	}

	if err := ctx.Err(); err != nil {
		return Clone{}, err
	}

	match, err := localrepos.Resolver{Root: root, Home: s.home, Git: s.git}.Resolve(ctx, repo.owner, repo.name)
	if err != nil {
		return Clone{}, err
	}

	if match.Repo.Path != repo.localPath {
		s.cacheClonePath(ctx, repo.id, match.Repo.Path)
	}

	return Clone{Dir: match.Repo.Path, OriginIsItemRepo: match.OriginMatched()}, nil
}

// cachedClone validates the cached clone path: it must still be a git working tree with
// a remote that points at the repository.
func (s *Service) cachedClone(ctx context.Context, repo repoRef) (Clone, bool) {
	if repo.localPath == "" {
		return Clone{}, false
	}

	local, err := localrepos.Inspect(ctx, repo.localPath, s.git)
	if err != nil {
		return Clone{}, false
	}

	remote, ok := local.RemoteFor(repo.owner, repo.name)
	if !ok {
		return Clone{}, false
	}

	match := localrepos.Match{Repo: local, MatchedRemote: remote}

	return Clone{Dir: local.Path, OriginIsItemRepo: match.OriginMatched()}, true
}

// cacheClonePath remembers a repository's clone. A failure only costs a rescan next
// time, so it is logged rather than failing the launch.
func (s *Service) cacheClonePath(ctx context.Context, repoID int64, path string) {
	err := s.db.WithContext(ctx).Exec(cacheClonePathSQL, path, s.clock.Now().UTC(), repoID).Error
	if err != nil {
		slog.Warn("could not remember a repository's local clone", "error", err)
	}
}

// worktreeDirName returns the name of the worktree directory the agent uses for a
// --worktree argument: "#<n>" becomes "pr-<n>", and any other name is used as is.
func worktreeDirName(arg string) string {
	if n, ok := strings.CutPrefix(arg, worktreePRHeadForm); ok {
		return worktreePRPrefix + n
	}

	return arg
}

// sentenceCase capitalizes the first letter of an error message and ends it with a
// period, for showing it as a sentence.
func sentenceCase(msg string) string {
	if msg == "" {
		return msg
	}

	msg = strings.ToUpper(msg[:1]) + msg[1:]
	if !strings.HasSuffix(msg, ".") {
		msg += "."
	}

	return msg
}

// systemClock is the real clock.
type systemClock struct{}

// Now returns the current time.
func (systemClock) Now() time.Time { return time.Now() }
