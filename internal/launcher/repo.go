package launcher

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// A repository's launch starts a session on a repository rather than on an inbox item, as
// the Deps page's Upgrade does: a prompt for repositories, filled in with the repository's
// fields and the session's alone, in a new worktree of its own made from the default
// branch (WorktreeRequest.Task). It is planned as an item's launch is (resolve, then the
// terminal, the agent, the worktree, and the plan), with these differences, each checked
// again by every one of ResolveRepo, PreviewRepo, and LaunchRepo:
//
//   - its clone is the one the request names, checked: one the repository locations hold,
//     the clone's own folder rather than a linked worktree, with a remote for the
//     repository;
//   - what the inbox last stored of the repository refuses one GitHub says is archived,
//     disabled, or locked, or where the user's access is read or triage alone, and what it
//     doesn't know lets the launch go ahead, saying so (RepoResolution.Notice);
//   - its prompt is the prompt for repositories the request names, never a default one;
//   - it is trusted, as the user's own repository is: no outside contributor's limits;
//   - no file is checked, and only a session running in its folder holds it back, which
//     Alongside confirms;
//   - its prompt names the worktree made, filled in again once the maker made it, since a
//     launch made meanwhile may have taken the name planned;
//   - its files are named repo-<YYYYMMDD>, its session is recorded with no item, its title
//     the prompt's name and its URL the repository's page, and its worktree has no record
//     to touch.

const (
	// taskPrefix starts a repository's launch's task, deps-<YYYYMMDD>, the user's day, the
	// base of its worktree's name.
	taskPrefix = "deps-"

	// taskDay is the layout of a task's day.
	taskDay = "20060102"

	// githubWeb is GitHub's website, which a repository's page is under.
	githubWeb = "https://github.com/"
)

// taskPattern is what a repository's launch's task looks like: taskPrefix and the day.
var taskPattern = regexp.MustCompile(`^deps-[0-9]{8}$`)

var (
	// ErrRepoClosed is returned for a repository's launch on a repository GitHub says is
	// archived, disabled, or locked, which takes no push.
	ErrRepoClosed = errors.New("GitHub takes no push to the repository")

	// ErrNoPushAccess is returned for a repository's launch on a repository whose access
	// GitHub gives the user is read or triage alone.
	ErrNoPushAccess = errors.New("the user can't push to the repository")

	// ErrNotInLocations is returned for a repository's launch from a folder the repository
	// locations don't hold as a clone.
	ErrNotInLocations = errors.New("not a clone in the repository locations")

	// ErrNoItem is returned by Resume for a session a repository's launch started, which
	// has no item to plan its resumption on.
	ErrNoItem = errors.New("the session was started for a repository, not an item")
)

// RepoLaunchRequest asks to start a session on a repository rather than on an inbox item.
// Repo, Dir, and PromptID are required; every other choice is optional, and a zero or nil
// one keeps what the repository's launch settings resolve to, as a LaunchRequest's does.
// The launch works out its worktree's name itself, from the user's day.
type RepoLaunchRequest struct {
	// Repo is the repository, "owner/name".
	Repo string `json:"repo"`

	// Dir is its clone, which the worktree is made beside, as the window names it, with ~
	// for home: a clone the repository locations hold, its own folder rather than a linked
	// worktree, with a remote for Repo.
	Dir string `json:"dir"`

	// PromptID is the prompt for repositories to fill in. There is no default one.
	PromptID int64 `json:"promptId"`

	// Agent, Profile, Model, Effort, PlanMode, and Fast are as a LaunchRequest's.
	Agent    agent.ID `json:"agent"`
	Profile  string   `json:"profile"`
	Model    *string  `json:"model"`
	Effort   *string  `json:"effort"`
	PlanMode *bool    `json:"planMode"`
	Fast     *bool    `json:"fast"`

	// Alongside is the Key of a HoldRunning hold the user confirmed, as a LaunchRequest's.
	Alongside string `json:"alongside"`
}

// RepoResolution is what a repository's launch resolves to: its settings, each with where
// it came from, as an item's Resolution has them (ItemID 0, Kind "", Trusted, the worktree
// always on, and the prompt the launch's own, inheriting none), and what the launch sheet
// says of the repository first. Every field is a plain value.
type RepoResolution struct {
	Resolution

	// Notice says, in a sentence or two the sheet shows as they are, what the app can't
	// tell of the repository, or what the launch does differently there, such as that
	// GitHub hasn't said whether the user can push to it; "" when there is nothing to say.
	Notice string `json:"notice"`
}

// RepoFacts reads what the inbox last stored about a repository, which a repository's
// launch checks. The app adapts the inbox's store to it.
type RepoFacts interface {
	// RepoFacts returns what the last refresh stored about the repository name, which
	// matches ignoring case; StoredRepo.Found is false when the inbox never saw it.
	RepoFacts(ctx context.Context, name repo.Name) (StoredRepo, error)
}

// StoredRepo is what the inbox last stored about a repository, as a repository's launch
// reads it: all zero when the inbox never saw it. Every field is a plain value.
//
// Its fields match the inbox's own record of them one for one, so the app converts one to
// the other directly.
type StoredRepo struct {
	// Found reports that the inbox has seen the repository, so the rest says something.
	Found bool

	// DefaultBranch is its default branch, or "" when not read.
	DefaultBranch string

	// ViewerPermission is the signed-in account's access to it as GitHub names it, ADMIN,
	// MAINTAIN, WRITE, TRIAGE, or READ, or "" when GitHub didn't say.
	ViewerPermission string

	// Archived, Disabled, and Locked are what GitHub says of it, and LockReason why it is
	// locked, as GitHub names that, such as MIGRATING, or "".
	Archived   bool
	Disabled   bool
	Locked     bool
	LockReason string

	// Fork reports a fork, and Parent the repository it was forked from, "owner/name", or
	// "" when not known.
	Fork   bool
	Parent string
}

// ResolveRepo returns a repository's launch settings with the request's choices applied,
// each with where it came from, as Resolve does an item's, and what the launch sheet says
// of the repository first (RepoResolution.Notice). It checks all LaunchRepo checks but the
// agent's program, the terminal, and the worktree: the repository, what the inbox last
// stored of it, its clone, and the prompt. It runs only the clone's read-only remote
// lookup, and writes nothing.
func (s *Service) ResolveRepo(ctx context.Context, req RepoLaunchRequest) (RepoResolution, error) {
	w, err := s.loadWorld(ctx)
	if err != nil {
		return RepoResolution{}, err
	}

	l, err := s.resolveRepo(ctx, w, req)
	if err != nil {
		return RepoResolution{}, err
	}

	return RepoResolution{Resolution: l.res, Notice: l.target.notice}, nil
}

// PreviewRepo plans a repository's launch without making its worktree, writing files, or
// opening a terminal, as Preview does an item's: the settings, the worktree it would make
// now and its name, the command, and the prompt filled in for that worktree. LaunchRepo
// runs the same plan, but its prompt names the worktree the maker made, which a launch
// made in between may have moved to the next free name.
func (s *Service) PreviewRepo(ctx context.Context, req RepoLaunchRequest) (Preview, error) {
	p, err := s.planRepo(ctx, req, planPreview, sessionRef{})
	if err != nil {
		return Preview{}, err
	}

	return p.preview(s.home), nil
}

// LaunchRepo starts a session on a repository rather than on an inbox item:
//
//  1. check the repository, what the inbox last stored of it, its clone, and the prompt,
//     and resolve the launch's settings, as for an item but for a repository's own
//     differences (see the top of repo.go);
//  2. find the agent, plan the task's new worktree and the launch, and hold the launch
//     back, starting nothing, while a session runs in the folder it would start in,
//     unless the request's Alongside confirms exactly that hold;
//  3. make the worktree, new, from the repository's default branch (see WorktreeMaker),
//     and fill the prompt in for it, by the name it was made under;
//  4. trust the folder, write the prompt file and the script, open it in the terminal
//     chosen in settings, and record the session, with no item, under the ID minted for it.
//
// The launcher itself runs only the clone's read-only remote lookup.
func (s *Service) LaunchRepo(ctx context.Context, req RepoLaunchRequest) (LaunchResult, error) {
	ref := sessionRef{id: s.newID()}

	p, err := s.planRepo(ctx, req, planLaunch, ref)
	if err != nil {
		return LaunchResult{}, err
	}

	if hold := s.heldBack(p, "", nil); hold != nil {
		return LaunchResult{Hold: hold}, nil
	}

	if err = s.ensureWorktree(ctx, &p); err != nil {
		return LaunchResult{}, err
	}

	if p.prompt, err = s.fillRepo(ctx, p); err != nil {
		return LaunchResult{}, err
	}

	return s.start(ctx, p, ref, "")
}

// planRepo works out a repository's launch of the session ref names, as plan does an
// item's: the terminal driver first, so an OS without terminal support fails before
// anything runs; then the repository and its clone, checked, and the settings; the agent,
// the task's worktree, and the plan; held back for a session running where it would start,
// unless req's Alongside confirms it, in mode planLaunch alone; and the prompt filled in
// for the worktree planned.
func (s *Service) planRepo(ctx context.Context, req RepoLaunchRequest, mode planMode, ref sessionRef) (planned, error) {
	w, err := s.loadWorld(ctx)
	if err != nil {
		return planned{}, err
	}

	driver, err := s.registry.Get(w.settings.Terminal)
	if err != nil {
		return planned{}, err
	}

	l, err := s.resolveRepo(ctx, w, req)
	if err != nil {
		return planned{}, err
	}

	tmpl, err := l.prompt.Template()
	if err != nil {
		return planned{}, err
	}

	p := planned{
		row: l.target.row(l.prompt), res: l.res, clone: l.target.clone, driver: driver, task: s.taskNow(), tmpl: tmpl,
	}

	if p, err = s.placeIn(ctx, p, l.request(w, p, ref)); err != nil {
		return planned{}, err
	}

	if p.running = s.runningHold(ctx, p, req.Alongside, mode != planPreview); p.running != nil {
		return p, nil
	}

	p.prompt, err = s.fillRepo(ctx, p)

	return p, err
}

// repoLaunch is a repository's launch resolved: what it is for, checked, and its settings.
type repoLaunch struct {
	target  repoTarget
	res     Resolution
	profile profiles.Profile
	prompt  prompts.Prompt
}

// resolveRepo reads req's repository, resolves the launch's settings against w, the prompt
// for repositories it names among them, then checks the repository by what the inbox last
// stored of it, and its clone (repoTargetOf).
func (s *Service) resolveRepo(ctx context.Context, w world, req RepoLaunchRequest) (repoLaunch, error) {
	name, err := repo.Parse(req.Repo)
	if err != nil {
		return repoLaunch{}, uierr.New(fmt.Sprintf("%q isn't a GitHub repository's name.", req.Repo), err)
	}

	res, profile, prompt, err := resolve(w, subject{repo: name, trusted: true, repository: true}, req.launch())
	if err != nil {
		return repoLaunch{}, err
	}

	t, err := s.repoTargetOf(ctx, w, name, req.Dir)
	if err != nil {
		return repoLaunch{}, err
	}

	return repoLaunch{target: t, res: res, profile: profile, prompt: prompt}, nil
}

// request is what BuildPlan plans the launch p from: the repository in place of an item,
// trusted, the resolved agent and the profile with the resolved settings in place of its
// own, the session's name and ID, and the task.
func (l repoLaunch) request(w world, p planned, ref sessionRef) Request {
	return Request{
		Item:    Item{URL: p.row.URL, Title: p.row.Title, Repo: l.target.name.String(), Trusted: true},
		Agent:   l.res.Agent.ID,
		Profile: effectiveProfile(l.profile, l.res), SessionName: repoSessionName(l.prompt.Nickname, l.target.name),
		SessionID: ref.id, Fast: l.res.Fast.Value, UseAPIKey: w.settings.APIKeys[l.res.Agent.ID], Task: p.task,
	}
}

// launch is the item's launch request req's choices make, as resolve reads one.
func (req RepoLaunchRequest) launch() LaunchRequest {
	return LaunchRequest{
		Agent: req.Agent, Profile: req.Profile, PromptID: req.PromptID, Model: req.Model, Effort: req.Effort,
		PlanMode: req.PlanMode, Fast: req.Fast, Alongside: req.Alongside,
	}
}

// repoTarget is what a repository's launch is for, checked: the repository, its clone with
// the clone's remote for it, what the inbox last stored of it, and what the launch sheet
// says of it first, "" for nothing.
type repoTarget struct {
	name   repo.Name
	clone  Clone
	facts  StoredRepo
	notice string
}

// repoTargetOf checks the repository name by what the inbox last stored of it (repoAllows),
// and dir, as the window names its clone (repoClone).
func (s *Service) repoTargetOf(ctx context.Context, w world, name repo.Name, dir string) (repoTarget, error) {
	facts, err := s.storedRepo(ctx, name)
	if err != nil {
		return repoTarget{}, err
	}

	if err = repoAllows(name, facts); err != nil {
		return repoTarget{}, err
	}

	clone, err := s.repoClone(ctx, w, name, dir)
	if err != nil {
		return repoTarget{}, err
	}

	return repoTarget{name: name, clone: clone, facts: facts, notice: repoNotice(name, facts)}, nil
}

// row is what a repository's launch shows and records in place of an item: the
// repository, its page as the URL, the prompt's name as the title, and the default branch
// the inbox last stored, with no item, kind, or number.
func (t repoTarget) row(p prompts.Prompt) launchItemRow {
	return launchItemRow{StoredItem: StoredItem{
		Owner: t.name.Owner, Name: t.name.Repo, URL: githubWeb + t.name.String(), Title: p.Name,
		DefaultBranch: t.facts.DefaultBranch,
	}}
}

// storedRepo reads what the inbox last stored of name: nothing found when the Service has
// no RepoFacts.
func (s *Service) storedRepo(ctx context.Context, name repo.Name) (StoredRepo, error) {
	if s.repoFacts == nil {
		return StoredRepo{}, nil
	}

	facts, err := s.repoFacts.RepoFacts(ctx, name)
	if err != nil {
		return StoredRepo{}, fmt.Errorf("read what the inbox knows of %s: %w", name, err)
	}

	return facts, nil
}

// repoAllows refuses, in words the window shows, a repository GitHub says takes no push:
// one archived, disabled, or locked, or one where the user's access is known and is read or
// triage alone. What isn't known lets the launch go ahead, which repoNotice says.
func repoAllows(name repo.Name, f StoredRepo) error {
	closed := ""

	switch {
	case !f.Found:
		return nil
	case f.Archived:
		closed = "archived"
	case f.Disabled:
		closed = "disabled"
	case f.Locked:
		closed = "locked" + lockWhy(f.LockReason)
	}

	if closed != "" {
		return uierr.New(fmt.Sprintf("GitHub says %s is %s, so nothing can be pushed to it.", name, closed),
			fmt.Errorf("%w: %s is %s", ErrRepoClosed, name, closed))
	}

	if access := readOnlyAccess(f.ViewerPermission); access != "" {
		return uierr.New(fmt.Sprintf("You can't push to %s: GitHub gives you %s access to it.", name, access),
			fmt.Errorf("%w: %s gives %s", ErrNoPushAccess, name, f.ViewerPermission))
	}

	return nil
}

// readOnlyAccess names an access GitHub gives that pushes nothing, "read" or "triage", or
// is "" for any other.
func readOnlyAccess(permission string) string {
	switch permission {
	case "READ", "TRIAGE":
		return strings.ToLower(permission)
	default:
		return ""
	}
}

// pushAccess reports an access GitHub gives that pushes: ADMIN, MAINTAIN, or WRITE.
func pushAccess(permission string) bool {
	return permission == "ADMIN" || permission == "MAINTAIN" || permission == "WRITE"
}

// lockWhy says why GitHub locked a repository, as in " (trade restriction)", from its lock
// reason, or "" for none, or for one other than an upper-case word GitHub names them with.
func lockWhy(reason string) string {
	if reason == "" || strings.Trim(reason, "ABCDEFGHIJKLMNOPQRSTUVWXYZ_") != "" {
		return ""
	}

	return " (" + strings.ToLower(strings.ReplaceAll(reason, "_", " ")) + ")"
}

// repoNotice is what the launch sheet says of the repository first, from what the inbox
// last stored of it: that GitHub hasn't said whether the user can push to it, and, for a
// fork, that the pull request opens on the fork itself, since the prompt names it; "" for
// nothing.
func repoNotice(name repo.Name, f StoredRepo) string {
	var lines []string

	if !f.Found || !pushAccess(f.ViewerPermission) {
		lines = append(lines, fmt.Sprintf("GitHub hasn't said whether you can push to %s. If you can't, the agent "+
			"stops at the push and says so.", name))
	}

	if f.Fork {
		of := ""
		if parent, err := repo.Parse(f.Parent); err == nil {
			of = " of " + parent.String()
		}

		lines = append(lines, fmt.Sprintf("%s is a fork%s, so the pull request opens on the fork itself.", name, of))
	}

	return strings.Join(lines, " ")
}

// repoClone checks dir, as the window names a clone of name, with ~ for home: one the
// repository locations hold, the clone's own folder rather than one of its linked
// worktrees, with a remote that points at name. It returns the clone and that remote.
func (s *Service) repoClone(ctx context.Context, w world, name repo.Name, dir string) (Clone, error) {
	expanded, err := paths.ExpandHome(dir, s.home)
	if err != nil {
		return Clone{}, uierr.New("Choose a clone of "+name.String()+" to launch from.",
			fmt.Errorf("%w: %w", ErrNotTheClone, err))
	}

	shown := paths.Tilde(expanded, s.home)

	roots, err := w.settings.roots(s.home, s.reader)
	if err != nil {
		return Clone{}, err
	}

	if !roots.Holds(expanded) {
		return Clone{}, uierr.New(shown+" isn't a clone in your repository locations.",
			fmt.Errorf("%w: %s", ErrNotInLocations, expanded))
	}

	// Past Holds, which keeps out a hidden folder, the whole disk, and anything no location
	// holds, CheckAddable refuses a linked worktree alone.
	if err = localrepos.CheckAddable(expanded, s.home); err != nil {
		return Clone{}, uierr.New(shown+" is a linked worktree, not a clone of its own. Launch from its clone instead.",
			fmt.Errorf("%w: %w", ErrNotTheClone, err))
	}

	return s.cloneOf(ctx, expanded, name)
}

// taskNow is a repository's launch's task as of now: deps-<YYYYMMDD>, the user's day.
func (s *Service) taskNow() string {
	return taskPrefix + s.clock.Now().In(s.zone).Format(taskDay)
}

// fillRepo fills a repository's launch's prompt in for the worktree it starts in, as
// planned or as made: the repository's names, its default branch, and the clone's remote
// for it, as read, which the prompt's plainness checks weigh; the profile and the
// signed-in account; and the worktree's name. It sets no item's field, so the prompt ends
// with no item's notice.
func (s *Service) fillRepo(ctx context.Context, p planned) (string, error) {
	viewer, err := s.items.ViewerLogin(ctx)
	if err != nil {
		return "", err
	}

	data := prompts.RepoData(p.row.repo(), p.defaultBranch(), p.clone.Remote)
	data.Profile, data.Me = p.res.Profile.Value, viewer

	text, err := p.tmpl.Render(data.WithWorktree(p.worktreeName()))
	if err != nil {
		return "", fmt.Errorf("fill in the %q prompt: %w", p.res.Prompt.Nickname, err)
	}

	return text, nil
}

// defaultBranch is the default branch a repository's launch's prompt names: the one its
// worktree starts from, as made or as planned, which is the inbox's when that is a branch's
// name, else the one the inbox last stored, "" when neither is known.
func (p planned) defaultBranch() string {
	if p.worktree != nil && p.worktree.DefaultBranch != "" {
		return p.worktree.DefaultBranch
	}

	return p.row.DefaultBranch
}

// repoSessionName names a repository's launch's session in the agent's prompt box, its
// resume list, and the terminal's title, as sessionName does an item's: the prompt's
// nickname, then the repository.
func repoSessionName(nickname string, name repo.Name) string {
	return nickname + " · " + name.Repo
}
