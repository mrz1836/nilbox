package launcher

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/trust"
)

// claudeBin is the agent command the launch scripts run.
const claudeBin = "claude"

// loadItemSQL reads one cached item with its repository and cached clone path.
const loadItemSQL = `SELECT i.kind, i.number, i.url, i.title, i.head_ref, i.author_association,
	i.is_cross_repository, r.id AS repo_id, r.owner, r.name, r.local_path
FROM items i JOIN repos r ON r.id = i.repo_id
WHERE i.id = ?`

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

// Config holds a Service's dependencies. Clock is optional.
type Config struct {
	// DB is the app database: items, repositories, and profiles.
	DB *gorm.DB

	// Settings reads the default profile, worktree default, terminal, and clone root.
	Settings SettingsReader

	// Git runs the read-only `git config --get-regexp` remote lookup that finds and
	// validates clones. The launcher runs no other git command.
	Git localrepos.Runner

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
}

// LaunchRequest asks to start a session on one inbox item.
type LaunchRequest struct {
	// ItemID is the item's local ID from the inbox snapshot.
	ItemID int64 `json:"itemId"`

	// Profile names the profile to launch with; "" uses the default profile setting.
	Profile string `json:"profile"`

	// Worktree starts the session in an agent-created worktree; nil uses the worktree
	// default setting.
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

	// Profile is the profile the session was launched with.
	Profile string `json:"profile"`

	// Permission summarizes the session's permissions, e.g. "plan mode (outside contributor)".
	Permission string `json:"permission"`

	// Terminal is the terminal the launch opened in.
	Terminal string `json:"terminal"`
}

// Service launches coding-agent sessions on inbox items.
type Service struct {
	db       *gorm.DB
	settings SettingsReader
	git      localrepos.Runner
	finder   Finder
	registry Registry
	paths    appdata.Paths
	home     string
	clock    Clock
	prompts  *Prompts
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
		finder:   cfg.Finder,
		registry: cfg.Registry,
		paths:    cfg.Paths,
		home:     cfg.Home,
		clock:    clock,
		prompts:  NewPrompts(),
	}
}

// Launch starts a session on an inbox item:
//
//  1. load the item (computing its trust) and the profile (the default profile setting
//     when none is named);
//  2. find the item's local clone: the cached path when it still is a clone of the
//     repository, otherwise a scan of the clone root, whose result is cached;
//  3. decide the worktree: the request's choice, else the worktree default setting;
//  4. find the agent, build the plan, and render the starter prompt;
//  5. write the prompt file and launch script;
//  6. open the script in the terminal chosen in settings.
//
// The only git command it runs is the read-only `git -C <clone> config --get-regexp`
// remote lookup. The script only changes into the clone and runs the agent; it never
// checks out, stashes, fetches, switches branches, or creates worktrees.
func (s *Service) Launch(ctx context.Context, req LaunchRequest) (LaunchResult, error) {
	in, err := s.prepare(ctx, req)
	if err != nil {
		return LaunchResult{}, err
	}

	plan, err := BuildPlan(Request{
		Item:               in.item,
		Profile:            in.profile,
		Clone:              in.clone,
		Worktree:           in.worktree,
		FullAccessOverride: req.FullAccessOverride,
		ClaudePath:         in.claude,
	})
	if err != nil {
		return LaunchResult{}, fmt.Errorf("plan the launch: %w", err)
	}

	prompt, err := s.prompts.Render(in.item.Kind, NewPromptData(in.item, plan))
	if err != nil {
		return LaunchResult{}, err
	}

	files, err := WriteLaunchFiles(s.paths, plan, prompt, s.clock.Now())
	if err != nil {
		return LaunchResult{}, err
	}

	if err = in.driver.Open(ctx, files.Script); err != nil {
		return LaunchResult{}, err
	}

	return LaunchResult{
		ScriptPath: files.Script,
		ClonePath:  localrepos.TildePath(in.clone.Dir, s.home),
		Worktree:   worktreeDirName(plan.Worktree),
		Profile:    in.profile.Name,
		Permission: plan.Permission,
		Terminal:   in.driver.ID(),
	}, nil
}

// launchInputs is everything a launch is planned from.
type launchInputs struct {
	item     Item
	profile  profiles.Profile
	clone    Clone
	driver   Driver
	claude   string
	worktree bool
}

// prepare gathers a launch's inputs. The terminal driver is chosen before the clone is
// resolved, so an OS without terminal support fails before anything runs or is written.
func (s *Service) prepare(ctx context.Context, req LaunchRequest) (launchInputs, error) {
	prefs, err := s.settings.Get(ctx)
	if err != nil {
		return launchInputs{}, err
	}

	item, repo, err := s.loadItem(ctx, req.ItemID)
	if err != nil {
		return launchInputs{}, err
	}

	in := launchInputs{item: item, worktree: prefs.WorktreeDefault}
	if req.Worktree != nil {
		in.worktree = *req.Worktree
	}

	if in.profile, err = s.loadProfile(ctx, req.Profile, prefs.DefaultProfile); err != nil {
		return launchInputs{}, err
	}

	if in.driver, err = s.registry.Get(prefs.Terminal); err != nil {
		return launchInputs{}, err
	}

	if in.clone, err = s.resolveClone(ctx, repo, prefs.CloneRoot); err != nil {
		return launchInputs{}, err
	}

	if in.claude, err = s.finder.Find(claudeBin); err != nil {
		return launchInputs{}, fmt.Errorf("find the agent: %w", err)
	}

	return in, nil
}

// repoRef is the repository an item belongs to, as stored.
type repoRef struct {
	id        int64
	owner     string
	name      string
	localPath string
}

// launchItemRow is one row of loadItemSQL.
type launchItemRow struct {
	Kind              string `gorm:"column:kind"`
	Number            int    `gorm:"column:number"`
	URL               string `gorm:"column:url"`
	Title             string `gorm:"column:title"`
	HeadRef           string `gorm:"column:head_ref"`
	AuthorAssociation string `gorm:"column:author_association"`
	IsCrossRepository bool   `gorm:"column:is_cross_repository"`
	RepoID            int64  `gorm:"column:repo_id"`
	Owner             string `gorm:"column:owner"`
	Name              string `gorm:"column:name"`
	LocalPath         string `gorm:"column:local_path"`
}

// loadItem reads an item and its repository, and decides whether the item is trusted.
func (s *Service) loadItem(ctx context.Context, id int64) (Item, repoRef, error) {
	var rows []launchItemRow
	if err := s.db.WithContext(ctx).Raw(loadItemSQL, id).Scan(&rows).Error; err != nil {
		return Item{}, repoRef{}, fmt.Errorf("load item: %w", err)
	}

	if len(rows) == 0 {
		return Item{}, repoRef{}, fmt.Errorf("%w (id %d)", ErrItemNotFound, id)
	}

	row := rows[0]
	item := Item{
		Kind:    row.Kind,
		Number:  row.Number,
		URL:     row.URL,
		Title:   row.Title,
		Repo:    row.Owner + "/" + row.Name,
		Branch:  row.HeadRef,
		Trusted: trust.Trusted(row.AuthorAssociation, row.Kind == KindPR, row.IsCrossRepository),
	}

	return item, repoRef{id: row.RepoID, owner: row.Owner, name: row.Name, localPath: row.LocalPath}, nil
}

// loadProfile returns the named profile, or the default profile setting's when name is "".
func (s *Service) loadProfile(ctx context.Context, name, defaultName string) (profiles.Profile, error) {
	if name == "" {
		name = defaultName
	}

	return profiles.Lookup(ctx, s.db, name)
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

// systemClock is the real clock.
type systemClock struct{}

// Now returns the current time.
func (systemClock) Now() time.Time { return time.Now() }
