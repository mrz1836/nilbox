package core

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/launcher"
	"github.com/mrz1836/nilbox/internal/launcher/terminal"
	"github.com/mrz1836/nilbox/internal/notify"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/semver"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/setup"
	"github.com/mrz1836/nilbox/internal/shellenv"
	"github.com/mrz1836/nilbox/internal/statusprefs"
	"github.com/mrz1836/nilbox/internal/svcstatus"
	"github.com/mrz1836/nilbox/internal/uierr"
	"github.com/mrz1836/nilbox/internal/worktrees"
)

// Autopilot's agent steps reach the launcher, the profiles, the agents' own commands, the
// service status check, and the launch defaults through the adapters here: a routine's
// step is launched as a click's launch is (routineRequest, the same for a start and for
// Preview), what it needs on this Mac is checked (readyChecks), and what holds a start
// back on this Mac is asked right before it (agentHolds).

// agentDeps are what Autopilot's agent steps are launched, checked, and previewed through.
type agentDeps struct {
	launch   *launcher.Service
	prompts  *prompts.Store
	profiles *profiles.Store
	settings *settings.Service
	defaults *launchdefaults.Store
	inbox    *inbox.Store
	status   *svcstatus.Service
	shell    *shellenv.Reader
	search   execpath.Search
	registry terminal.Registry
	banners  notify.Poster
	home     string
	logger   *slog.Logger

	// runners runs an agent's command with an environment of its own.
	runners func(env []string) execpath.Runner

	// environ is the login shell's environment, and whether it was read; nil reads this
	// process's. The checks never log a variable's name or value.
	environ func() ([]string, bool)

	// finder finds programs as the agent does; nil searches the login shell's PATH, then
	// the well-known folders.
	finder execpath.Finder

	// github checks the GitHub sign-in as the setup does, for Check again; nil until the
	// setup is built, which is after the routines.
	github func(ctx context.Context) (setup.GitHub, error)
}

// routineRequest is a routine's agent step on one item as the launcher's own request: the
// routine's choices in the launch menu's place (a per-org or per-repository row of the
// routine's own over its step's, a repository's row over its org's), its prompt found in
// the library by its starter's key or its nickname, and the routine named for the
// launcher's holds and the prompt. Everything it leaves out inherits exactly as a click's
// launch does.
func routineRequest(ctx context.Context, d agentDeps, req autopilot.RunRequest) (launcher.LaunchRequest, error) {
	res, err := d.launch.Resolve(ctx, launcher.LaunchRequest{ItemID: req.ItemID})
	if err != nil {
		return launcher.LaunchRequest{}, err
	}

	step := req.Step
	choice := launchRowFor(step, res.Repo)

	out := launcher.LaunchRequest{
		ItemID: req.ItemID, Agent: agentOf(choice.Agent), Profile: choice.Profile, PlanMode: step.PlanMode,
		Worktree: step.Worktree,
		Routine: &launcher.RoutineLaunch{
			Name: req.Routine, Trigger: autopilot.TriggerWords(req.Trigger), Attempt: req.Attempt,
			Confirmed: slices.Clone(req.Confirmed),
		},
	}

	if choice.Model != "" {
		out.Model = new(choice.Model)
	}

	if choice.Effort != "" {
		out.Effort = new(choice.Effort)
	}

	if out.PromptID, err = promptIDOf(ctx, d.prompts, step.Prompt); err != nil {
		return launcher.LaunchRequest{}, err
	}

	return out, nil
}

// launchRowFor is the agent step's choices for the repository full ("owner/name"): its own,
// with its row for the repository's org over them, and its row for the repository over
// both.
func launchRowFor(step spec.AgentStep, full string) spec.LaunchRow {
	out := spec.LaunchRow{Agent: step.AgentID(), Profile: step.Profile, Model: step.Model, Effort: step.Effort}

	name, err := repo.Parse(full)
	if err != nil {
		return out
	}

	apply := func(row spec.LaunchRow) {
		out.Agent = firstSet(row.Agent, out.Agent)
		out.Profile = firstSet(row.Profile, out.Profile)
		out.Model = firstSet(row.Model, out.Model)
		out.Effort = firstSet(row.Effort, out.Effort)
	}

	for _, want := range []string{name.Owner, name.String()} {
		for _, row := range step.LaunchFor {
			if strings.EqualFold(row.Target, want) {
				apply(row)
			}
		}
	}

	return out
}

// firstSet returns value, or else fallback for "".
func firstSet(value, fallback string) string {
	if value != "" {
		return value
	}

	return fallback
}

// agentOf is a step's agent as the launcher names it: Inherit for the launch defaults'.
func agentOf(id string) agent.ID {
	if id == "" || id == catalog.Inherit {
		return agent.Inherit
	}

	return agent.ID(id)
}

// promptIDOf finds the prompt ref names in the library, 0 for the item's own default (the
// one a click resolves to). One the library doesn't have is an error that says so.
func promptIDOf(ctx context.Context, store *prompts.Store, ref spec.PromptRef) (int64, error) {
	if ref.ItemDefault || ref.Starter == "" && ref.Nickname == "" {
		return 0, nil
	}

	all, err := store.All(ctx)
	if err != nil {
		return 0, err
	}

	if i := slices.IndexFunc(all, func(p prompts.Prompt) bool { return refNames(ref, p) }); i >= 0 {
		return all[i].ID, nil
	}

	return 0, uierr.New(fmt.Sprintf("Its prompt, “%s”, isn't in your library any more.",
		firstSet(ref.Nickname, ref.Starter)), prompts.ErrPromptNotFound)
}

// refNames reports whether ref names p: a starter's key names the user's copy of it, and a
// nickname the prompt that has it, ignoring case.
func refNames(ref spec.PromptRef, p prompts.Prompt) bool {
	if ref.Starter != "" {
		return p.StarterKey == ref.Starter
	}

	return strings.EqualFold(p.Nickname, ref.Nickname)
}

// agentRuns runs a routine's agent steps (autopilot.Runner). A run in the user's terminal is
// a click's launch, opened through the launcher with the routine's choices: it ends as the
// terminal opens, since the app follows the session from there as any session (its pill and
// panel), and a later start finds it working in the folder (the launcher's hold). A step in
// the background is the background runner's (backgroundAgents), which hands this one each
// step in the terminal; one reaching it here means this copy runs no background runner.
type agentRuns struct {
	deps agentDeps
}

// Dispatch launches job's agent step, or reports what held it back.
func (r agentRuns) Dispatch(ctx context.Context, job autopilot.Job) (autopilot.Result, error) {
	if job.Step.Agent == nil {
		return autopilot.Result{}, uierr.New("This step runs no agent.", autopilot.ErrNoAgentStep)
	}

	if place := job.Step.Agent.Place(); place != catalog.WhereTerminal {
		return autopilot.Result{
			Wait:      autopilot.BlockNotReady,
			WaitWords: "Not ready on this Mac: this copy can't run an agent in the background.",
		}, nil
	}

	req, err := routineRequest(ctx, r.deps, job.RunRequest())
	if err != nil {
		return autopilot.Result{}, err
	}

	got, err := r.deps.launch.Launch(ctx, req)

	switch {
	case errors.Is(err, worktrees.ErrHeadMoved):
		return autopilot.Result{Requeue: true, WaitWords: "Its head moved as its worktree was made."}, nil
	case err != nil:
		return autopilot.Result{}, err
	case got.Hold != nil:
		return heldResult(*got.Hold), nil
	}

	where := got.Worktree
	if where == "" {
		where = got.Dir
	}

	return autopilot.Result{
		Sent: true, Summary: fmt.Sprintf("Opened %s in %s, in %s.", got.Agent, terminal.Name(got.Terminal), where),
		Launched: &autopilot.Launched{
			SessionID: got.SessionID, Agent: string(got.Agent), Profile: got.Profile, ClonePath: got.ClonePath,
			Worktree: got.Worktree, Command: got.Permission,
		},
	}, nil
}

// heldResult is what a launch the launcher held back makes a routine's match wait on: a
// session working where it would start queues it until that ends, and a file hold or a
// dirty worktree waits for the user to confirm exactly it.
func heldResult(h launcher.Hold) autopilot.Result {
	name := h.Worktree
	if name == "" {
		name = h.Dir
	}

	shown := &autopilot.Hold{
		Key: h.Key, Reason: string(h.Reason), Head: h.Head, Files: slices.Clone(h.Files), Checked: h.Checked,
		Total: h.Total, Error: h.Error, AtHead: h.AtHead, Dir: h.Dir, Worktree: h.Worktree, Dirty: h.Dirty,
	}

	switch h.Reason {
	case launcher.HoldRunning:
		return autopilot.Result{
			Wait:      autopilot.BlockBusyWorktree,
			WaitWords: fmt.Sprintf("Queued: %s is in use by a session in your terminal; starts when it ends.", name),
		}
	case launcher.HoldDirty:
		return autopilot.Result{
			Wait: autopilot.BlockDirty, Hold: shown,
			WaitWords: fmt.Sprintf("%s has %d uncommitted %s.", name, h.Dirty, plural(h.Dirty, "file", "files")),
		}
	case launcher.HoldAgentFiles:
		files := make([]string, 0, len(h.Files))
		for _, f := range h.Files {
			files = append(files, f.Path)
		}

		return autopilot.Result{
			Wait: autopilot.BlockHold, Hold: shown,
			WaitWords: "Held: it changes agent configuration (" + strings.Join(files, ", ") + ").",
		}
	case launcher.HoldUnchecked:
	}

	return autopilot.Result{
		Wait: autopilot.BlockHold, Hold: shown, WaitWords: "Held: its files couldn't all be checked.",
	}
}

// plural picks one or many by n.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}

	return many
}

// Stop stops nothing: a run in the terminal ends as the terminal opens, and the session is
// the user's from there.
func (agentRuns) Stop(context.Context, int64) error { return nil }

// StopAll stops nothing, as Stop.
func (agentRuns) StopAll(context.Context) error { return nil }

// Alive says a run the ledger still records as going is gone: a terminal run ends as it
// launches, so one still recorded was cut off by a quit or a crash mid-launch, maybe after
// the terminal opened, and it is never started again on its own.
func (agentRuns) Alive(context.Context, int64) (autopilot.Liveness, error) {
	return autopilot.LiveGone, nil
}

// runPreviews plans a routine's agent step on one item (autopilot.Previewer): the
// launcher's own Preview of the request a start would launch, or for a step in the
// background, its background Preview (backgroundAgents).
type runPreviews struct {
	deps       agentDeps
	background *backgroundAgents
}

// PreviewRun plans req.
func (p runPreviews) PreviewRun(ctx context.Context, req autopilot.RunRequest) (autopilot.RunPreview, error) {
	if req.Step.Place() == catalog.WhereBackground {
		if p.background == nil {
			return autopilot.RunPreview{}, uierr.New("This copy can't run an agent in the background.",
				autopilot.ErrNoAgentStep)
		}

		return p.background.PreviewRun(ctx, req)
	}

	launch, err := routineRequest(ctx, p.deps, req)
	if err != nil {
		return autopilot.RunPreview{}, err
	}

	got, err := p.deps.launch.Preview(ctx, launch)
	if err != nil {
		return autopilot.RunPreview{}, err
	}

	return previewOf(got), nil
}

// previewOf is the launcher's Preview in Autopilot's words.
func previewOf(got launcher.Preview) autopilot.RunPreview {
	res := got.Resolution
	out := autopilot.RunPreview{
		Agent: res.Agent.Name, Profile: res.Profile.Value, Model: res.Model.Value, Effort: res.Effort.Value,
		PromptName: res.Prompt.Nickname, Dir: got.Dir, Command: got.Command, Env: got.Env, Permission: got.Permission,
		Prompt: got.Prompt, Terminal: terminal.Name(got.Terminal), Warnings: res.Warnings,
		Sources: map[string]string{
			"agent": routineSource(res.AgentSource), "profile": routineSource(res.Profile.Source),
			"model": routineSource(res.Model.Source), "effort": routineSource(res.Effort.Source),
			"prompt": routineSource(res.Prompt.Source),
		},
	}

	if wt := got.Worktree; wt != nil {
		out.Worktree = &autopilot.RunWorktree{
			Name: wt.Name, Path: wt.Path, Branch: wt.Branch, Action: string(wt.Action), Base: wt.Base, Dirty: wt.Dirty,
		}
	}

	return out
}

// routineSource names where a resolved setting came from, the routine's own choice for
// what a click's launch menu would have chosen.
func routineSource(s launcher.Source) string {
	if s == launcher.SourceLaunch {
		return "routine"
	}

	return string(s)
}

// agentHolds says what holds a routine's agent start back on this Mac, right before it,
// for the item's own launch (autopilot.Holds): its agent's program and profile as the item's
// launch settings resolve them (the cheap checks, and the last sign-in check), the agent's
// provider reporting a major outage while the user asks to hold for one, a repository with
// no clone, and a tool the clone's files say it needs that the login shell's PATH lacks.
type agentHolds struct {
	deps   agentDeps
	checks *readyChecks
	prefs  autopilot.Prefs
}

// Hold says what holds job's start back, or "" to let it through.
func (h agentHolds) Hold(ctx context.Context, job autopilot.Job) (autopilot.Blocker, string, error) {
	if job.Step.Agent == nil {
		return "", "", nil
	}

	req, err := routineRequest(ctx, h.deps, job.RunRequest())
	if err != nil {
		return autopilot.BlockNotReady, "Not ready on this Mac: " + uierr.Message(err), nil
	}

	res, err := h.deps.launch.Resolve(ctx, req)
	if err != nil {
		return autopilot.BlockNotReady, "Not ready on this Mac: " + uierr.Message(err), nil
	}

	needs := []autopilot.Need{
		{Kind: autopilot.NeedAgent, Agent: string(res.Agent.ID)},
		{Kind: autopilot.NeedProfile, Agent: string(res.Agent.ID), Profile: res.Profile.Value},
	}

	if job.Step.Agent.Place() == catalog.WhereTerminal {
		needs = append(needs, autopilot.Need{Kind: autopilot.NeedTerminal})
	}

	checks, err := h.checks.Check(ctx, needs, false)
	if err != nil {
		return "", "", err
	}

	if words := unmet(res.Agent, checks, job.Step.Agent.Place()); words != "" {
		return autopilot.BlockNotReady, "Not ready on this Mac: " + words, nil
	}

	if b, words := h.outage(ctx, res.Agent); b != "" {
		return b, words, nil
	}

	return h.cloneHolds(ctx, job)
}

// unmet says what of checks keeps the agent a from starting where place says: a
// problem, or a program too old for the background; "" for none.
func unmet(a agent.Agent, checks []autopilot.ReadyCheck, place catalog.Where) string {
	for _, c := range checks {
		if c.Level == autopilot.ReadyProblem {
			return c.Words()
		}

		if words := tooOld(a, c, place); words != "" {
			return words
		}
	}

	return ""
}

// tooOld says an agent's program is older than the version whose flags every run of it
// in the background uses (agent.Headless.MinVersion), from its check c of the program; ""
// for a run in the terminal, a check of something else, or a version that can't be read.
func tooOld(a agent.Agent, c autopilot.ReadyCheck, place catalog.Where) string {
	if place != catalog.WhereBackground || c.Need.Kind != autopilot.NeedAgent || c.Level != autopilot.ReadyOK {
		return ""
	}

	h, err := agent.HeadlessOf(a.ID)
	if err != nil || h.MinVersion == "" {
		return ""
	}

	if older, ok := semver.Compare(c.Found, h.MinVersion); ok && older < 0 {
		return fmt.Sprintf("%s %s is older than %s, which runs agents in the background. Update it, then Check again.",
			a.Name, c.Found, h.MinVersion)
	}

	return ""
}

// outage holds an agent's start while its provider reports a major or critical outage on
// the part the agent depends on, when Settings → Autopilot asks to (on unless turned off).
func (h agentHolds) outage(ctx context.Context, a agent.Agent) (autopilot.Blocker, string) {
	if h.deps.status == nil || h.prefs == nil {
		return "", ""
	}

	cfg, err := h.prefs.Autopilot(ctx)
	if err != nil || !cfg.HoldOnOutage {
		return "", ""
	}

	provider, ok := outageOf(h.deps.status.Status(ctx), statusprefs.PartID(a.StatusPart))
	if !ok {
		return "", ""
	}

	return autopilot.BlockOutage, fmt.Sprintf("Queued: %s reports a major outage; starts once it's back.", provider)
}

// outageOf says whether part is down in st, major or critical, and names its provider.
func outageOf(st svcstatus.Status, part statusprefs.PartID) (string, bool) {
	for _, p := range st.Providers {
		for _, a := range p.Affected {
			if a.ID == part && (a.State == svcstatus.StateMajor || a.State == svcstatus.StateCritical) {
				return p.Name, true
			}
		}
	}

	return "", false
}

// cloneHolds holds a start for a repository with no clone on this Mac, which waits for the
// user (Clone and run), and for a tool its clone's files say it needs that the login
// shell's PATH lacks, the one the agent runs with. It only checks that files exist, and
// runs nothing in the clone.
func (h agentHolds) cloneHolds(ctx context.Context, job autopilot.Job) (autopilot.Blocker, string, error) {
	target, err := h.deps.launch.CloneTarget(ctx, job.Item.ID)
	if err != nil {
		return "", "", err
	}

	if !target.Cloned {
		return autopilot.BlockNoClone, "No local clone of " + job.Item.Repo + ".", nil
	}

	dir, err := paths.ExpandHome(target.Dir, h.deps.home)
	if err != nil {
		return "", "", err
	}

	for _, tool := range repoTools(dir) {
		if !h.checks.found(tool) {
			return autopilot.BlockMissingTool, fmt.Sprintf("%s needs %s, which isn't on this Mac.", job.Item.Repo, tool), nil
		}
	}

	return "", "", nil
}

// toolMarker is a file in a clone that says which tool an agent working there needs.
type toolMarker struct {
	file string
	tool string
}

// toolMarkers are the markers, in the order their tools are named.
func toolMarkers() []toolMarker {
	return []toolMarker{
		{"go.mod", "go"},
		{"pnpm-lock.yaml", "pnpm"},
		{"magefiles", "magex"},
		{".mage.yaml", "magex"},
		{"Makefile", "make"},
		{"Cargo.toml", "cargo"},
		{"pyproject.toml", "python3"},
	}
}

// repoTools returns the tools the clone at dir's files say it needs, each once, in the
// markers' order, from os.Lstat alone.
func repoTools(dir string) []string {
	var out []string

	for _, m := range toolMarkers() {
		if _, err := os.Lstat(filepath.Join(dir, m.file)); err == nil && !slices.Contains(out, m.tool) {
			out = append(out, m.tool)
		}
	}

	return out
}

// readyChecks checks what routines need on this Mac (autopilot.ReadinessChecker), over the
// app's own checks: the inbox's last refresh, and the setup's GitHub check on Check again;
// the finder over the login shell's PATH; the profiles; each agent's version and sign-in
// commands, which run without the API key variables unless the user lets a key sign the
// agent in; the terminal chosen; and whether banners are allowed. The slow results (an
// agent's version, a profile's sign-in) are kept for the readiness's own time and asked
// again on Check again.
type readyChecks struct {
	deps agentDeps

	mu   sync.Mutex
	slow map[string]slowCheck
}

// slowCheck is a slow check's result, and when it was taken.
type slowCheck struct {
	check autopilot.ReadyCheck
	at    time.Time
}

// slowFor is how long a slow check's result stands, as the readiness's own.
const slowFor = 10 * time.Minute

// newReadyChecks returns the checks over deps.
func newReadyChecks(deps agentDeps) *readyChecks {
	return &readyChecks{deps: deps, slow: map[string]slowCheck{}}
}

// Defaults returns what Inherit resolves to: the app's default agent, and each agent's
// default profile.
func (r *readyChecks) Defaults(ctx context.Context) (autopilot.LaunchDefaults, error) {
	s, err := r.deps.settings.Get(ctx)
	if err != nil {
		return autopilot.LaunchDefaults{}, err
	}

	out := autopilot.LaunchDefaults{Agent: string(agent.Default().ID), Profiles: map[string]string{}}
	if s.DefaultAgent != agent.Inherit {
		out.Agent = string(s.DefaultAgent)
	}

	for id, name := range s.DefaultProfiles {
		out.Profiles[string(id)] = name
	}

	return out, nil
}

// Check checks each need, in order.
func (r *readyChecks) Check(ctx context.Context, needs []autopilot.Need, fresh bool) ([]autopilot.ReadyCheck, error) {
	s, err := r.deps.settings.Get(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]autopilot.ReadyCheck, 0, len(needs))
	now := time.Now()

	for _, n := range needs {
		c := r.check(ctx, s, n, fresh)
		c.Need, c.ID, c.CheckedAt = n, n.ID(), now
		out = append(out, c)
	}

	return out, nil
}

// check checks one need.
func (r *readyChecks) check(ctx context.Context, s settings.Settings, n autopilot.Need, fresh bool) autopilot.ReadyCheck {
	switch n.Kind {
	case autopilot.NeedGitHub:
		return r.github(ctx, fresh)
	case autopilot.NeedTool:
		return r.tool(n.Tool)
	case autopilot.NeedAgent:
		return r.agentCheck(ctx, n.Agent, fresh)
	case autopilot.NeedProfile:
		return r.profile(ctx, s, n.Agent, n.Profile, fresh)
	case autopilot.NeedTerminal:
		return r.terminalCheck(s)
	case autopilot.NeedBanners:
		return r.bannersCheck()
	case autopilot.NeedPlace:
	}

	return autopilot.ReadyCheck{Label: string(n.Kind), Level: autopilot.ReadyOK, Found: "found"}
}

// ok, warn, and problem are the three outcomes of a check named label.
func ok(label, found string) autopilot.ReadyCheck {
	return autopilot.ReadyCheck{Label: label, Level: autopilot.ReadyOK, Found: found}
}

func warn(label, found, fix string, command bool) autopilot.ReadyCheck {
	return autopilot.ReadyCheck{Label: label, Level: autopilot.ReadyWarning, Found: found, Fix: fix, Command: command}
}

func problem(label, found, fix string, command bool) autopilot.ReadyCheck {
	return autopilot.ReadyCheck{Label: label, Level: autopilot.ReadyProblem, Found: found, Fix: fix, Command: command}
}

// github checks the GitHub sign-in: from the inbox's last refresh, at no cost, or with the
// setup's own check (one point) when fresh.
func (r *readyChecks) github(ctx context.Context, fresh bool) autopilot.ReadyCheck {
	if fresh && r.deps.github != nil {
		g, err := r.deps.github(ctx)
		if err == nil {
			return githubCheck(g)
		}
	}

	st, err := r.deps.inbox.ReadStatus(ctx)

	switch {
	case err != nil:
		return warn("GitHub", "couldn't be read", "", false)
	case st.SignedOut:
		return problem("GitHub", "is signed out", setup.FixSignIn, true)
	case st.Viewer == "":
		return warn("GitHub", "hasn't been read yet", "", false)
	default:
		return ok("GitHub", "signed in as @"+st.Viewer+", from the last refresh")
	}
}

// githubCheck words the setup's GitHub check.
func githubCheck(g setup.GitHub) autopilot.ReadyCheck {
	switch g.State {
	case setup.GitHubReady:
		return ok("GitHub", "signed in as @"+g.Login+", with repo and read:org")
	case setup.GitHubMissingScopes:
		return problem("GitHub", "is missing "+strings.Join(g.Missing, " and "), g.Fix, true)
	case setup.GitHubUnreachable:
		return warn("GitHub", "couldn't be reached", "", false)
	case setup.GitHubNoCLI, setup.GitHubSignedOut, setup.GitHubRefused:
	}

	return problem("GitHub", strings.TrimSuffix(strings.ToLower(firstSet(g.Message, "isn't signed in")), "."), g.Fix,
		g.Fix != "")
}

// search finds programs as the agent does: on the login shell's PATH, then the well-known
// folders.
func (r *readyChecks) search() execpath.Finder {
	if r.deps.finder != nil {
		return r.deps.finder
	}

	s := r.deps.search
	if r.deps.shell != nil {
		s.Path = r.deps.shell.Path()
	}

	return s
}

// found reports whether the agent's PATH finds a program.
func (r *readyChecks) found(name string) bool {
	_, err := r.search().Find(name)

	return err == nil
}

// tool checks a program the agent runs with.
func (r *readyChecks) tool(name string) autopilot.ReadyCheck {
	if r.found(name) {
		return ok(name, "found")
	}

	switch name {
	case "gh":
		return problem(name, "isn't on this Mac", setup.FixInstall, true)
	case "git":
		return problem(name, "isn't on this Mac", "xcode-select --install", true)
	default:
		return problem(name, "isn't on this Mac", "Install it, then Check again.", false)
	}
}

// agentCheck checks an agent's program: found, and its version (slow).
func (r *readyChecks) agentCheck(ctx context.Context, id string, fresh bool) autopilot.ReadyCheck {
	a, err := agent.Lookup(agent.ID(id))
	if err != nil {
		return problem(id, "isn't an agent this version knows", "", false)
	}

	path, err := r.search().Find(a.Command)
	if err != nil {
		return problem(a.Name, "isn't installed", "Install "+a.Name+", then Check again.", false)
	}

	return r.slowly("agent:"+id, fresh, func() autopilot.ReadyCheck {
		stdout, _, runErr := r.runner(nil).Run(ctx, path, a.VersionArgs...)
		if v := agent.ParseVersion(stdout); runErr == nil && v != "" {
			return ok(a.Name, v)
		}

		return warn(a.Name, "is installed, but its version couldn't be read", "", false)
	})
}

// runner runs an agent's command with env, the login shell's when nil.
func (r *readyChecks) runner(env []string) execpath.Runner {
	if r.deps.runners == nil {
		return execpath.Exec{Search: r.deps.search, Env: env}
	}

	return r.deps.runners(env)
}

// slowly returns the slow check key's result as last taken within slowFor, unless fresh,
// else takes it with run.
func (r *readyChecks) slowly(key string, fresh bool, run func() autopilot.ReadyCheck) autopilot.ReadyCheck {
	r.mu.Lock()
	kept, have := r.slow[key]
	r.mu.Unlock()

	if have && !fresh && time.Since(kept.at) < slowFor {
		return kept.check
	}

	c := run()

	r.mu.Lock()
	r.slow[key] = slowCheck{check: c, at: time.Now()}
	r.mu.Unlock()

	return c
}

// profile checks an agent's profile: stored, its folder there, and signed in, by the
// agent's own sign-in command run with the profile's config folder (slow). The command runs
// without the variables that would sign the agent in with an API key, unless the user lets
// a key do so (Settings → Launching), when a key in the shell's environment counts as ready
// too.
func (r *readyChecks) profile(ctx context.Context, s settings.Settings, id, name string, fresh bool,
) autopilot.ReadyCheck {
	a, err := agent.Lookup(agent.ID(id))
	if err != nil {
		return problem("Profile "+name, "is for an agent this version doesn't know", "", false)
	}

	label := "Profile " + name + " (" + a.Name + ")"

	p, err := r.deps.profiles.Lookup(ctx, a.ID, name)
	if err != nil {
		return problem(label, "isn't on this Mac", "Run "+a.Command+" once with that profile, then Check again.", false)
	}

	dir := p.ConfigDir
	if dir == "" {
		dir = filepath.Join(r.deps.home, a.ConfigDir)
	}

	if _, statErr := os.Stat(dir); errors.Is(statErr, fs.ErrNotExist) {
		return problem(label, "has no folder at "+paths.Tilde(dir, r.deps.home), "", false)
	}

	path, err := r.search().Find(a.Command)
	if err != nil {
		return problem(label, "can't be checked: "+a.Name+" isn't installed", "", false)
	}

	useKey := s.APIKeys()[a.ID]
	env := signInEnv(r.shellEnv(), a, p.ConfigDir, useKey)

	if useKey && keySet(env, a) {
		return ok(label, "an API key from your shell signs it in")
	}

	return r.slowly("profile:"+id+"/"+name, fresh, func() autopilot.ReadyCheck {
		// Codex writes its sign-in to stderr alone; Claude Code's JSON comes on stdout.
		stdout, stderr, runErr := r.runner(env).Run(ctx, path, a.SignInArgs...)
		out := append(append(slices.Clone(stdout), '\n'), stderr...)

		return signInCheck(label, agent.ParseSignIn(a.ID, out, runErr == nil), loginFix(a, p.ConfigDir, r.deps.home))
	})
}

// shellEnv is the login shell's environment, or this process's before it is read.
func (r *readyChecks) shellEnv() []string {
	if r.deps.environ != nil {
		if env, read := r.deps.environ(); read {
			return env
		}
	}

	return os.Environ()
}

// signInEnv is the environment an agent's sign-in check runs with: base with the profile's
// config folder set (unset for the default profile), and without the API key variables
// unless useKey, so it says whether the profile itself is signed in.
func signInEnv(base []string, a agent.Agent, configDir string, useKey bool) []string {
	env := shellenv.Without(base, a.ConfigEnv)
	if !useKey {
		env = shellenv.Without(env, a.KeyEnv...)
	}

	if configDir != "" {
		env = shellenv.With(env, a.ConfigEnv, configDir)
	}

	return env
}

// keySet reports whether env sets one of the agent's API key variables.
func keySet(env []string, a agent.Agent) bool {
	for _, key := range a.KeyEnv {
		if shellenv.Lookup(env, key) != "" {
			return true
		}
	}

	return false
}

// loginFix is the command that signs the profile in, which the user runs in a terminal.
func loginFix(a agent.Agent, configDir, home string) string {
	cmd := a.Command + " " + strings.Join(a.LoginArgs, " ")
	if configDir == "" {
		return cmd
	}

	return a.ConfigEnv + "=" + paths.Tilde(configDir, home) + " " + cmd
}

// signInWarnWithin is how soon a sign-in's expiry makes it a warning.
const signInWarnWithin = 7 * 24 * time.Hour

// signInCheck words a profile's sign-in. The account's email is never kept: only whether
// it is signed in, how, its subscription, and when it expires.
func signInCheck(label string, in agent.SignIn, fix string) autopilot.ReadyCheck {
	switch in.State {
	case agent.SignedIn:
		found := "signed in"
		if in.Subscription != "" {
			found += " · " + in.Subscription
		}

		if in.ExpiresAt != nil && time.Until(*in.ExpiresAt) < signInWarnWithin {
			days := max(int(time.Until(*in.ExpiresAt).Hours()/24), 0)

			return warn(label, fmt.Sprintf("%s, but it expires in %d %s", found, days, plural(days, "day", "days")), fix, true)
		}

		return ok(label, found)
	case agent.SignedOut:
		return problem(label, "is signed out", fix, true)
	case agent.SignInUnknown:
	}

	return warn(label, "is there, but its sign-in couldn't be read", fix, true)
}

// terminalCheck checks the terminal launches open in.
func (r *readyChecks) terminalCheck(s settings.Settings) autopilot.ReadyCheck {
	apps := []string{"/Applications", filepath.Join(r.deps.home, "Applications")}

	for _, c := range r.deps.registry.Installed(apps...) {
		if c.ID != s.Terminal {
			continue
		}

		if c.Installed {
			return ok(c.Name, "installed")
		}

		return problem(c.Name, "isn't installed", "Choose another terminal in Settings → Launching.", false)
	}

	return problem(terminal.Name(s.Terminal), "isn't one this Mac can open", "Choose another terminal in Settings → Launching.",
		false)
}

// bannersCheck checks whether banners are allowed: without them the notices still fill the
// history, so it's only ever a warning.
func (r *readyChecks) bannersCheck() autopilot.ReadyCheck {
	if r.deps.banners == nil {
		return ok("Banners", "not available in this build; the notices go to the history")
	}

	switch r.deps.banners.Status() {
	case notify.StatusOff:
		return warn("Banners", "are off", "System Settings → Notifications", false)
	case notify.StatusOn:
		return ok("Banners", "allowed")
	case notify.StatusUnavailable:
	}

	return ok("Banners", "not available in this build; the notices go to the history")
}

// outsideRules reads where the user lets Autopilot run agents on outside contributors'
// items: the launch defaults' choice per org and repository (autopilot.OutsideRules).
type outsideRules struct {
	defaults *launchdefaults.Store
}

// Outside returns the launch defaults' index.
func (o outsideRules) Outside(ctx context.Context) (autopilot.OutsideIndex, error) {
	all, err := o.defaults.All(ctx)
	if err != nil {
		return nil, err
	}

	return outsideIndex{idx: launchdefaults.NewIndex(all)}, nil
}

// outsideIndex answers by the launch defaults: a repository's own choice over its org's.
type outsideIndex struct {
	idx launchdefaults.Index
}

// Allowed reports whether the repository full lets agents run on its outside
// contributors' items.
func (o outsideIndex) Allowed(full string) (bool, string) {
	name, err := repo.Parse(full)
	if err != nil {
		return false, ""
	}

	return o.idx.OutsideAllowed(name)
}

// routinePrompts tells the prompt library where the routines name its prompts, and the
// routines of a prompt renamed (prompts.RoutineSource, prompts.Config.Renamed).
type routinePrompts struct {
	follower *autopilot.PromptFollower
	logger   *slog.Logger
}

// RoutineUses returns where the routines name prompts.
func (r routinePrompts) RoutineUses(ctx context.Context) ([]prompts.RoutineUse, error) {
	uses, err := r.follower.Uses(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]prompts.RoutineUse, 0, len(uses))
	for _, u := range uses {
		out = append(out, prompts.RoutineUse{Routine: u.Routine, Starter: u.Starter, Nickname: u.Nickname})
	}

	return out, nil
}

// Renamed has the routines follow a prompt renamed, logging a failure: the routine then
// says its prompt isn't in the library, before any start.
func (r routinePrompts) Renamed(ctx context.Context, from, to string) {
	if _, err := r.follower.Renamed(ctx, from, to); err != nil {
		r.logger.WarnContext(ctx, "the routines could not follow a prompt renamed", "error", err)
	}
}

// The adapters implement the engine's seams.
var (
	_ autopilot.Runner           = agentRuns{}
	_ autopilot.Holds            = agentHolds{}
	_ autopilot.ReadinessChecker = (*readyChecks)(nil)
	_ autopilot.Previewer        = runPreviews{}
	_ autopilot.OutsideRules     = outsideRules{}
	_ prompts.RoutineSource      = routinePrompts{}
)
