package launcher

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/fence"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// A background run is planned exactly as a launch is (resolve and trust, then the clone,
// the agent, the worktree, and the plan), with four differences: no terminal is chosen
// and no launch script written; the worktree is on whatever the resolution says; the
// fence chooses the access (resolvePermission's fenced rule, under the trust rule), and
// the profile's extra directories are left out; and its holds always hold: a session or a
// run working in the worktree, the pull request's files, checked under the unattended
// scope, and for a routine a worktree with uncommitted changes, which only the user's
// confirmation of exactly that lets through. Once its worktree is made, the run's fence is
// worked out for it (Fenced: agent.PrepareFence), and its unattended note gains the
// fence's line.

// backgroundPrompt is the file a run reads its prompt from, in its own folder
// (<data>/agent-runs/<session id>), beside its fence's files.
const backgroundPrompt = "prompt.md"

// ErrUnfenced is returned for a background run planned without a fence.
var ErrUnfenced = errors.New("a background run is always fenced")

// RunFolders names the app's own runs working in a folder now, as the sessions they are:
// an agent's background run, one from before the app restarted included. The sessions
// ledger leaves them out while sessions aren't followed, and the process list leaves out
// the app's own children, so a launch reads these too.
type RunFolders interface {
	RunsIn(ctx context.Context, dir string) []RunningSession
}

// BackgroundRequest asks for a background run's plan: a launch's choices (the item, the
// agent, the profile, the prompt, the model, and the effort), the fence, and what every
// background run passes its agent. A routine's request names the routine (Routine), whose
// user's confirmations alone let a hold through, as for its launch in the terminal; a
// request without one takes Confirmed, a person's. Worktree, PlanMode, Fast,
// FullAccessOverride, and Alongside are ignored.
type BackgroundRequest struct {
	LaunchRequest

	// Preset is the routine's tools preset, and BudgetUSD the budget in dollars the agent
	// keeps to itself, 0 for none.
	Preset    fence.Preset `json:"preset"`
	BudgetUSD float64      `json:"budgetUsd"`

	// Schema is the report the agent returns, and SystemNote the system note every
	// unattended run gets.
	Schema     string `json:"schema"`
	SystemNote string `json:"systemNote"`
}

// BackgroundPlan is a background run worked out and not started: what the runner runs,
// and the worktree to make for it first.
type BackgroundPlan struct {
	// Plan is the run: the agent's binary and its flags, the environment's changes, and
	// the worktree's folder, as planned.
	Plan Plan

	// Prompt is the filled-in prompt, which the run reads on stdin.
	Prompt string

	// Worktree asks for the item's worktree, at the head the files were checked at.
	Worktree WorktreeRequest

	// Clone is the clone the worktree is in, Resolution what the run resolved to, and
	// Profile and ConfigDir the profile it runs with.
	Clone      Clone
	Resolution Resolution
	Profile    string
	ConfigDir  string

	// SessionID is the session's ID minted for the run, and Folder its own folder,
	// <data>/agent-runs/<session id>, which its prompt and its fence's files go into.
	SessionID string
	Folder    string

	// Preset is the tools preset the run is held to: the routine's, or Read only for an
	// outside contributor's item; ReadOnly reports Read only.
	Preset   fence.Preset
	ReadOnly bool

	// Command is the run's command line as the window shows it, with home as ~.
	Command string

	// Item is the item, as its session is recorded.
	Item BackgroundItem

	// fence is what the run's fence is worked out from as it starts (Fenced): the plan's
	// request, and the item's head branch, which a push preset pushes to.
	fence fenceFrom
}

// fenceFrom is what Fenced rebuilds a plan from.
type fenceFrom struct {
	req    Request
	bg     background
	branch string
	cross  bool
	data   string
	home   string
	config string
}

// BackgroundItem is the item a background run works on.
type BackgroundItem struct {
	ID     int64
	Owner  string
	Name   string
	Kind   item.Kind
	Number int
	Title  string
	URL    string
	Head   string
}

// Backgrounds plans the background runs, and trusts their worktrees, over the launcher's
// own planning: core holds it, and the window never reaches it, since a plan names folders
// to trust.
type Backgrounds struct {
	s *Service
}

// NewBackgrounds returns what plans the background runs over s. It is apart from the
// Service, which the window binds, so the window never plans one.
func NewBackgrounds(s *Service) *Backgrounds {
	return &Backgrounds{s: s}
}

// Plan plans a background run on an item, or holds it back, starting nothing: a session
// or a run working in the item's worktree holds it as HoldRunning, which no one confirms
// (the run waits for it to end); a pull request's files, checked against GitHub now
// under the unattended scope, and for a routine a worktree with uncommitted changes, hold
// it unless the request confirms exactly that hold (heldBack, a launch's own rule).
func (b *Backgrounds) Plan(ctx context.Context, req BackgroundRequest) (BackgroundPlan, *Hold, error) {
	s := b.s

	p, err := s.planBackground(ctx, req, planLaunch)
	if err != nil {
		return BackgroundPlan{}, nil, err
	}

	if hold := s.heldBack(p.planned, req.Confirmed, req.Routine); hold != nil {
		return BackgroundPlan{}, hold, nil
	}

	return p.background(s.home, s.paths.Root), nil, nil
}

// Made checks the worktree made for a background run as Plan checked the one it planned:
// one with uncommitted changes holds a routine's run unless its user confirmed exactly
// those (HoldDirty), as a launch's does. A request without a routine is never held so.
func (b *Backgrounds) Made(bp BackgroundPlan, wt Worktree, routine *RoutineLaunch) *Hold {
	if routine == nil {
		return nil
	}

	name := item.WorktreeName(bp.Worktree.Kind, bp.Worktree.Number)
	if dirty := holdDirty(&wt, paths.Tilde(wt.Path, b.s.home), name); dirty != nil && !routine.confirms(dirty.Key) {
		return dirty
	}

	return nil
}

// Preview plans a background run without holding it or asking GitHub anything, for the
// builder's Preview: the prompt, the worktree, the permissions, and the command line with
// the background flags, as the run would start.
func (b *Backgrounds) Preview(ctx context.Context, req BackgroundRequest) (Preview, error) {
	s := b.s

	p, err := s.planBackground(ctx, req, planPreview)
	if err != nil {
		return Preview{}, err
	}

	preview := p.preview(s.home)
	preview.Command, preview.Terminal = p.background(s.home, s.paths.Root).Command, ""

	return preview, nil
}

// backgroundPlanned is a background run worked out.
type backgroundPlanned struct {
	planned

	sessionID string
	folder    string
	req       Request
	bg        background
}

// planBackground works out a background run: in mode planLaunch it reads the sessions and
// runs in the worktree and checks the files against GitHub; in planPreview, neither.
func (s *Service) planBackground(ctx context.Context, req BackgroundRequest, mode planMode) (backgroundPlanned, error) {
	launch := req.LaunchRequest
	launch.Worktree, launch.PlanMode, launch.Fast, launch.FullAccessOverride, launch.Alongside = new(true), nil, new(false),
		false, ""

	w, err := s.loadWorld(ctx)
	if err != nil {
		return backgroundPlanned{}, err
	}

	row, err := s.loadItem(ctx, launch.ItemID)
	if err != nil {
		return backgroundPlanned{}, err
	}

	res, profile, prompt, err := resolve(w, row.subject(w.policy), launch)
	if err != nil {
		return backgroundPlanned{}, err
	}

	tmpl, err := prompt.Template()
	if err != nil {
		return backgroundPlanned{}, err
	}

	p := backgroundPlanned{sessionID: s.newID()}
	p.folder = filepath.Join(s.paths.AgentRuns, p.sessionID)
	p.req = Request{
		Item: row.item(w.policy), Agent: res.Agent.ID, Profile: effectiveProfile(profile, res), SessionID: p.sessionID,
		PromptFile: filepath.Join(p.folder, backgroundPrompt), UseAPIKey: w.settings.APIKeys[res.Agent.ID],
		Fence: &Fence{Preset: req.Preset, RunDir: p.folder},
	}
	p.bg = background{schema: req.Schema, note: req.SystemNote, budgetUSD: req.BudgetUSD}

	if p.planned, err = s.placeBackground(ctx, w, row, res, &p.req, p.bg); err != nil {
		return backgroundPlanned{}, err
	}

	if done, holdErr := s.backgroundHolds(ctx, &p, mode); done || holdErr != nil {
		return p, holdErr
	}

	data := withRoutine(row.promptData(w.policy, profile.Name, "", p.check), launch.Routine)
	data.Unattended = true
	p.prompt, err = s.fillIn(ctx, data, prompt.Nickname, tmpl, p.planned)

	return p, err
}

// backgroundHolds reads what may hold a background run back: for a launch, the sessions
// and runs in its worktree, which end the plan there, and then the pull request's files,
// against GitHub now for a launch, as last checked for a preview. It reports a plan held
// for a run in its worktree.
func (s *Service) backgroundHolds(ctx context.Context, p *backgroundPlanned, mode planMode) (bool, error) {
	if mode != planPreview {
		if p.running = s.runningHold(ctx, p.planned, "", true); p.running != nil {
			return true, nil
		}
	}

	var err error

	kind := checkStored
	if mode == planLaunch {
		kind = checkUnattended
	}

	if p.check, err = s.fileCheck(ctx, p.row.ID, kind); err != nil && !errors.Is(err, sensitive.ErrNotChecked) {
		return false, err
	}

	return false, nil
}

// background is what a background run's plan adds to a launch's.
type background struct {
	schema    string
	note      string
	budgetUSD float64
}

// placeBackground works out where and how a background run goes: the clone, the agent
// binary, the item's worktree, always, and the plan, with no terminal.
func (s *Service) placeBackground(ctx context.Context, w world, row launchItemRow, res Resolution, req *Request,
	bg background,
) (planned, error) {
	p := planned{row: row, res: res}

	roots, err := w.settings.roots(s.home, s.reader)
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

	if s.worktrees == nil {
		return planned{}, ErrNoWorktrees
	}

	wt, err := s.worktrees.PlanWorktree(ctx, p.worktreeRequest())
	if err != nil {
		return planned{}, err
	}

	p.worktree, req.Dir = &wt, wt.Path

	if p.plan, err = BuildBackground(*req, bg); err != nil {
		return planned{}, fmt.Errorf("plan the background run: %w", err)
	}

	return p, nil
}

// BuildBackground plans a background run. It is pure: the agent's headless flags around
// the access resolvePermission chooses from the request's fence (an outside contributor's
// item Read only, whatever the fence says), the profile's model, effort, extra arguments
// (refused when they set what the fence sets or would loosen it), and environment, and
// no extra directory: the profile's are left out.
func BuildBackground(req Request, bg background) (Plan, error) {
	a, err := validateRequest(req)
	if err != nil {
		return Plan{}, err
	}

	if req.Fence == nil {
		return Plan{}, ErrUnfenced
	}

	perm, err := resolvePermission(a, req.Item.Trusted, false, req.Profile, req.Fence)
	if err != nil {
		return Plan{}, err
	}

	if err = agent.CheckAccess(a.ID, req.Profile.ExtraArgs); err != nil {
		return Plan{}, fmt.Errorf("%w: %w", ErrConflictingExtraArg, err)
	}

	if err = agent.CheckFenced(a.ID, req.Profile.ExtraArgs); err != nil {
		return Plan{}, fmt.Errorf("%w: %w", ErrConflictingExtraArg, err)
	}

	args, err := agent.HeadlessArgs(a.ID, agent.HeadlessOptions{
		Model: req.Profile.Model, Effort: req.Profile.Effort, Access: perm.access, SessionID: req.SessionID,
		Schema: bg.schema, BudgetUSD: bg.budgetUSD, SystemNote: bg.note, Extra: req.Profile.ExtraArgs,
	})
	if err != nil {
		return Plan{}, err
	}

	env, unset, err := AgentEnv(a, req.Profile, req.UseAPIKey)
	if err != nil {
		return Plan{}, err
	}

	plan := Plan{
		Kind: req.Item.Kind, Number: req.Item.Number, Agent: a.ID, Dir: req.Dir, Binary: req.BinaryPath, Args: args,
		Env: env, Unset: unset, PromptFile: req.PromptFile, Permission: perm.summary, access: perm.access,
	}

	if err = checkNUL(plan); err != nil {
		return Plan{}, err
	}

	return plan, nil
}

// FenceIn is what a background run's fence is worked out from as the run starts, beside
// its plan: the worktree made for it, the app's own executable, which the fence hook runs
// in its hook mode, the run's environment, read for the tools' cache folders, and the
// routine's extra commands.
type FenceIn struct {
	Worktree string
	Program  string
	Env      map[string]string
	Allow    []string
	Deny     []string
}

// Fenced works a background run's fence out for the worktree made for it
// (agent.PrepareFence: the policy its hook holds the run to, the agent's settings, and
// for a sandboxed preset its sandbox and check, in the run's folder), and returns the plan
// again with the worktree as its folder and the fence's line added to its unattended note,
// beside the files the runner writes before the run starts (FencedRun.Write).
func (b *Backgrounds) Fenced(bp BackgroundPlan, in FenceIn) (BackgroundPlan, agent.FencedRun, error) {
	from := bp.fence

	if !filepath.IsAbs(in.Worktree) {
		return BackgroundPlan{}, agent.FencedRun{}, fmt.Errorf("%w: the run's worktree isn't an absolute path", ErrUnfenced)
	}

	access := bp.Plan.access.InRun(bp.Folder)

	run, err := agent.PrepareFence(bp.Plan.Agent, access, fence.Spec{
		Preset: bp.Preset, Worktree: in.Worktree, Repo: bp.Item.Owner + "/" + bp.Item.Name, Number: bp.Item.Number,
		Branch: from.branch, Remote: bp.Clone.Remote, CrossRepository: from.cross, Tools: fence.RepoTools(in.Worktree),
		Allow: in.Allow, Deny: in.Deny, RunDir: bp.Folder, Home: from.home, DataDir: from.data, ConfigDir: from.config,
		Env: in.Env,
	}, in.Program)
	if err != nil {
		return BackgroundPlan{}, agent.FencedRun{}, err
	}

	bg := from.bg
	bg.note = strings.TrimSpace(bg.note + "\n\n" + run.Note)
	req := from.req
	req.Dir = in.Worktree

	plan, err := BuildBackground(req, bg)
	if err != nil {
		return BackgroundPlan{}, agent.FencedRun{}, err
	}

	bp.Plan, bp.fence.bg, bp.fence.req = plan, bg, req
	bp.Command = commandLine(bp.Resolution.Agent.Command, plan.Args, from.home)

	return bp, run, nil
}

// Trust marks a background run's worktree, and its clone, trusted in the agent's config
// the run reads, as a launch does its folder: the agent would otherwise stop at its
// workspace trust question, which no one answers.
func (b *Backgrounds) Trust(ctx context.Context, bp BackgroundPlan) error {
	a, err := agent.Lookup(bp.Plan.Agent)
	if err != nil {
		return err
	}

	return b.s.trustDir(ctx, planned{plan: bp.Plan, clone: bp.Clone, res: Resolution{Agent: a}})
}

// background describes the planned run for the runner, with home shown as ~ in its
// command line.
func (p backgroundPlanned) background(home, dataDir string) BackgroundPlan {
	preset := fence.ReadOnly
	if f := p.plan.access.Fence; f != nil {
		preset = f.Preset
	}

	config := p.plan.Env[p.res.Agent.ConfigEnv]
	if config == "" && p.res.Agent.ConfigDir != "" {
		config = filepath.Join(home, p.res.Agent.ConfigDir)
	}

	return BackgroundPlan{
		Plan: p.plan, Prompt: p.prompt, Worktree: p.worktreeRequest(), Clone: p.clone, Resolution: p.res,
		Profile: p.res.Profile.Value, ConfigDir: p.plan.Env[p.res.Agent.ConfigEnv], SessionID: p.sessionID,
		Folder: p.folder, Preset: preset, ReadOnly: preset == fence.ReadOnly,
		Command: commandLine(p.res.Agent.Command, p.plan.Args, home),
		Item: BackgroundItem{
			ID: p.row.ID, Owner: p.row.Owner, Name: p.row.Name, Kind: p.row.Kind, Number: p.row.Number, Title: p.row.Title,
			URL: p.row.URL, Head: p.checkedHead(),
		},
		fence: fenceFrom{
			req: p.req, bg: p.bg, branch: p.row.HeadRef, cross: p.row.IsCrossRepository, data: dataDir, home: home,
			config: config,
		},
	}
}

// commandLine is a run's command line as the window shows it, with home as ~ and the
// prompt read from its file.
func commandLine(program string, args []string, home string) string {
	shown := make([]string, 0, len(args))
	for _, arg := range args {
		shown = append(shown, tildeIn(arg, home))
	}

	return strings.Join(strings.Fields(program+" "+agent.Join(shown)), " ") + " < " + backgroundPrompt
}
