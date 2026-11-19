package autopilot

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// Readiness is what each routine needs on this Mac, and whether it has it. Any Mac with
// the app can run routines; each checks it has what its routines need, so a routine that
// can't run here says what's missing and how to fix it, rather than failing halfway
// through a run.
//
//   - What a routine needs is worked out from its spec alone (Needs): GitHub for its watch,
//     banners for its notices, and for an agent step its agent's program and profile (and
//     each per-org row's and fallback's), gh and git, the terminal or the place it runs in,
//     plus what it Requires.
//   - Each need is checked by a ReadinessChecker, which core adapts over the app's own
//     checks (setup's, the profiles, the finder over the login shell's PATH, each agent's
//     version and sign-in commands), each result a plain value: ok, a warning, or a problem,
//     with what was found and what fixes it. Results are kept for readyFor (10 minutes).
//   - A routine is ready when each of its needs is ok or only a warning. One that isn't
//     still evaluates (Test and Dry run work), but its agent steps wait as blocked:
//     not_ready, naming the fix, and the user is told once per new problem. Once fixed, an
//     Autopilot routine's runs start by themselves, and an Ask first routine's after the
//     user's OK, as they would have anyway.
//   - The checks run as the engine starts (on a worker, never on the cold start), when a
//     routine is saved or switched on, every readyFor, and on Check again (CheckReadiness),
//     which also asks GitHub how the sign-in stands. Right before each start, the cheap ones
//     run again for the item's own agent and profile (core's Holds).

// readyFor is how long a readiness check stands.
const readyFor = 10 * time.Minute

// ReadyLevel is how a readiness check came out.
type ReadyLevel string

// The levels.
const (
	ReadyOK      ReadyLevel = "ok"
	ReadyWarning ReadyLevel = "warning"
	ReadyProblem ReadyLevel = "problem"
)

// NeedKind is a kind of thing a routine needs on this Mac.
type NeedKind string

// The kinds of need.
const (
	// NeedGitHub is the GitHub sign-in, with the scopes the watches read with.
	NeedGitHub NeedKind = "github"

	// NeedTool is a program on the login shell's PATH, the one an agent runs with.
	NeedTool NeedKind = "tool"

	// NeedAgent is a coding agent's program.
	NeedAgent NeedKind = "agent"

	// NeedProfile is one of an agent's profiles, present and signed in.
	NeedProfile NeedKind = "profile"

	// NeedTerminal is the terminal launches open in, for an agent in the terminal.
	NeedTerminal NeedKind = "terminal"

	// NeedBanners is the app's permission to show banners, for a notice: without it the
	// history still fills, so it's only ever a warning.
	NeedBanners NeedKind = "banners"

	// NeedPlace is where an agent step runs, which this build must run agents in.
	NeedPlace NeedKind = "place"
)

// Need is one thing a routine needs on this Mac.
type Need struct {
	Kind NeedKind `json:"kind"`

	// Agent and Profile name an agent's profile (NeedProfile), or Agent an agent
	// (NeedAgent).
	Agent   string `json:"agent"`
	Profile string `json:"profile"`

	// Tool is a program's name (NeedTool).
	Tool string `json:"tool"`

	// Place is where an agent step runs (NeedPlace).
	Place catalog.Where `json:"place"`

	// Fallback reports a need only an agent step's fallbacks bring, which this Mac lacking
	// only warns of: the step still runs on its own profile, and a start passes over a
	// fallback that can't run here.
	Fallback bool `json:"fallback"`
}

// ID names the need, the same for every routine that has it: "github", "tool:pnpm",
// "agent:claude", "profile:claude/work", "terminal", "banners", "place:background".
func (n Need) ID() string {
	switch n.Kind {
	case NeedTool:
		return "tool:" + n.Tool
	case NeedAgent:
		return "agent:" + n.Agent
	case NeedProfile:
		return "profile:" + n.Agent + "/" + n.Profile
	case NeedPlace:
		return "place:" + string(n.Place)
	case NeedGitHub, NeedTerminal, NeedBanners:
		return string(n.Kind)
	}

	return string(n.Kind)
}

// forStart reports a need an agent's start depends on: a routine whose agent can't run
// here waits, while one whose GitHub sign-in is gone waits for it on its own (signed_out)
// and one without banners still notifies.
func (n Need) forStart() bool {
	return n.Kind != NeedGitHub && n.Kind != NeedBanners
}

// LaunchDefaults is what Inherit resolves to for readiness: the app's default agent, and
// each agent's default profile. An org's or a repository's own launch defaults, which a
// click would follow too, are checked right before each start, for the item's own.
type LaunchDefaults struct {
	Agent    string            `json:"agent"`
	Profiles map[string]string `json:"profiles"`
}

// profileOf returns the profile an agent step runs agent with when it names none.
func (d LaunchDefaults) profileOf(agent string) string {
	if p := d.Profiles[agent]; p != "" {
		return p
	}

	return "default"
}

// Needs returns what a routine with spec sp needs on this Mac, each once, in a stable
// order: GitHub, the banners for a notice, for an agent step its place (and the terminal,
// for a terminal run), each agent and profile it may run (its own, its per-org rows', and
// its fallbacks'), gh and git, and then what it Requires. It is pure.
func Needs(sp spec.Spec, d LaunchDefaults) []Need {
	out := []Need{{Kind: NeedGitHub}}
	add := func(n Need) {
		switch i := slices.IndexFunc(out, func(have Need) bool { return have.ID() == n.ID() }); {
		case i < 0:
			out = append(out, n)
		case !n.Fallback:
			out[i].Fallback = false
		}
	}

	for _, st := range sp.Steps {
		switch {
		case st.Notify != nil || st.TellMe != nil:
			add(Need{Kind: NeedBanners})
		case st.Agent != nil:
			for _, n := range agentNeeds(*st.Agent, d) {
				add(n)
			}
		}
	}

	for _, tool := range sp.Requires {
		add(Need{Kind: NeedTool, Tool: tool})
	}

	return out
}

// agentNeeds returns what one agent step needs.
func agentNeeds(a spec.AgentStep, d LaunchDefaults) []Need {
	out := []Need{{Kind: NeedPlace, Place: a.Place()}}

	if a.Place() == catalog.WhereTerminal {
		out = append(out, Need{Kind: NeedTerminal})
	}

	pairs := []spec.ProfileRef{{Agent: a.AgentID(), Profile: a.Profile}}

	for _, row := range a.LaunchFor {
		if row.Agent != "" || row.Profile != "" {
			pairs = append(pairs, spec.ProfileRef{Agent: firstOf(row.Agent, a.AgentID()), Profile: row.Profile})
		}
	}

	for _, p := range pairs {
		out = append(out, d.profileNeeds(p)...)
	}

	for _, p := range a.Fallbacks {
		for _, n := range d.profileNeeds(p) {
			n.Fallback = true
			out = append(out, n)
		}
	}

	return append(out, Need{Kind: NeedTool, Tool: "gh"}, Need{Kind: NeedTool, Tool: "git"})
}

// profileNeeds returns what running p's agent with its profile needs: the agent's program,
// and the profile, each as Inherit resolves when p leaves it out.
func (d LaunchDefaults) profileNeeds(p spec.ProfileRef) []Need {
	agent := p.Agent
	if agent == "" || agent == catalog.Inherit {
		agent = d.Agent
	}

	if agent == "" {
		return nil
	}

	profile := firstOf(p.Profile, d.profileOf(agent))

	return []Need{{Kind: NeedAgent, Agent: agent}, {Kind: NeedProfile, Agent: agent, Profile: profile}}
}

// firstOf returns the first of values that isn't "".
func firstOf(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}

	return ""
}

// ReadyCheck is how one need checked out on this Mac.
type ReadyCheck struct {
	// Need is what was checked, and ID its name.
	Need Need   `json:"need"`
	ID   string `json:"id"`

	// Label names it in the window: "GitHub", "gh", "Claude", "Profile work".
	Label string `json:"label"`

	// Level is how it came out.
	Level ReadyLevel `json:"level"`

	// Found says what was found: "2.1.325", "signed in · max", "isn't installed".
	Found string `json:"found"`

	// Fix says how to fix a problem or a warning: a command to run in a terminal, when
	// Command, else where to go ("System Settings → Notifications"), "" for none.
	// The app never signs in for the user, since signing in needs a browser and the user.
	Fix     string `json:"fix"`
	Command bool   `json:"command"`

	// CheckedAt is when it was checked.
	CheckedAt time.Time `json:"checkedAt"`
}

// Words says a check's outcome as a sentence: "Claude Code isn't installed. Install it,
// then Check again." or "The work profile is signed out. Run `claude auth login`." Its
// label is a name, kept as it is spelled ("iTerm2", "gh"), never upper-cased to start it.
func (c ReadyCheck) Words() string {
	text := textutil.EndSentence(c.Label + " " + c.Found)

	switch {
	case c.Fix == "":
		return text
	case c.Command:
		return text + " Run `" + c.Fix + "`, then Check again."
	default:
		return text + " " + textutil.Sentence(c.Fix)
	}
}

// ReadinessChecker checks what routines need on this Mac. Core adapts the app's own
// checks to it. Check returns one result per need, in order; fresh asks what is slow or
// costs a request again (each agent's version and sign-in, GitHub's sign-in, a point),
// and otherwise answers those from what it last found within readyFor. An error means no
// check could run.
type ReadinessChecker interface {
	Check(ctx context.Context, needs []Need, fresh bool) ([]ReadyCheck, error)

	// Defaults returns what Inherit resolves to.
	Defaults(ctx context.Context) (LaunchDefaults, error)
}

// Readiness is this Mac's readiness, as Settings → Autopilot's This Mac and the routines'
// badges show it.
type Readiness struct {
	// CheckedAt is when the checks last ran, nil before they did; Checking reports
	// checks under way.
	CheckedAt *time.Time `json:"checkedAt"`
	Checking  bool       `json:"checking"`

	// Checks are every check the routines need, the basics first.
	Checks []ReadyCheck `json:"checks"`

	// Routines are each enabled routine's readiness, in the list's order; NotReady counts
	// those that aren't ready.
	Routines []RoutineReady `json:"routines"`
	NotReady int            `json:"notReady"`
}

// RoutineReady is one routine's readiness.
type RoutineReady struct {
	RoutineID int64  `json:"routineId"`
	Name      string `json:"name"`

	// Ready reports a routine whose needs are each ok or only a warning; Problems are the
	// needs that aren't, and Warnings those only warned of.
	Ready    bool         `json:"ready"`
	Problems []ReadyCheck `json:"problems"`
	Warnings []ReadyCheck `json:"warnings"`
}

// readiness keeps the last results of the readiness checks, which the Service and the
// engine share: the engine reads them as it decides a start, never waiting on a check.
type readiness struct {
	checker ReadinessChecker
	offers  *Offers

	mu       sync.Mutex
	results  map[string]ReadyCheck
	defaults LaunchDefaults
	at       time.Time
	checking bool

	// due reports a change since the last check (a routine saved or switched on), which
	// the engine checks again for.
	due bool
}

// newReadiness returns the readiness over checker; nil checks nothing, and every routine
// is ready.
func newReadiness(checker ReadinessChecker, offers *Offers) *readiness {
	return &readiness{checker: checker, offers: offers.orCatalog(), results: map[string]ReadyCheck{}}
}

// readySnapshot is what a cycle reads of readiness: the results, as they were.
type readySnapshot struct {
	on       bool
	checked  bool
	results  map[string]ReadyCheck
	defaults LaunchDefaults
}

// snapshot returns the results as they are now.
func (r *readiness) snapshot() readySnapshot {
	if r == nil || r.checker == nil {
		return readySnapshot{}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	return readySnapshot{on: true, checked: !r.at.IsZero(), results: r.results, defaults: r.defaults}
}

// markDue has the engine check again soon.
func (r *readiness) markDue() {
	if r == nil {
		return
	}

	r.mu.Lock()
	r.due = true
	r.mu.Unlock()
}

// stale reports results older than readyFor, none at all, or a change since.
func (r *readiness) stale(now time.Time) bool {
	if r == nil || r.checker == nil {
		return false
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	return !r.checking && (r.due || r.at.IsZero() || now.Sub(r.at) >= readyFor)
}

// begin marks a check under way, and reports false when one is.
func (r *readiness) begin() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.checking {
		return false
	}

	r.checking, r.due = true, false

	return true
}

// run checks the needs of the routines in specs (fresh as CheckReadiness asks), and keeps
// the results. A need this build answers itself (where an agent runs) never reaches the
// checker.
func (r *readiness) run(ctx context.Context, specs []spec.Spec, fresh bool, now time.Time) error {
	if r == nil || r.checker == nil {
		return nil
	}

	defer func() {
		r.mu.Lock()
		r.checking = false
		r.mu.Unlock()
	}()

	defaults, err := r.checker.Defaults(ctx)
	if err != nil {
		return err
	}

	needs := allNeeds(specs, defaults)
	results := map[string]ReadyCheck{}
	asked := make([]Need, 0, len(needs))

	for _, n := range needs {
		if n.Kind == NeedPlace {
			results[n.ID()] = r.placeCheck(n, now)

			continue
		}

		asked = append(asked, n)
	}

	got, err := r.checker.Check(ctx, asked, fresh)
	if err != nil {
		return err
	}

	for _, c := range got {
		c.ID = c.Need.ID()
		results[c.ID] = c
	}

	r.mu.Lock()
	r.results, r.defaults, r.at = results, defaults, now
	r.mu.Unlock()

	return nil
}

// allNeeds returns what every routine of specs needs, each once, after GitHub and the
// tools it borrows, which This Mac always shows.
func allNeeds(specs []spec.Spec, defaults LaunchDefaults) []Need {
	needs := []Need{{Kind: NeedGitHub}, {Kind: NeedTool, Tool: "gh"}, {Kind: NeedTool, Tool: "git"}}

	for _, sp := range specs {
		for _, n := range Needs(sp, defaults) {
			if !slices.ContainsFunc(needs, func(have Need) bool { return have.ID() == n.ID() }) {
				needs = append(needs, n)
			}
		}
	}

	return needs
}

// placeCheck checks where an agent step runs against what this build runs.
func (r *readiness) placeCheck(n Need, now time.Time) ReadyCheck {
	info, _ := catalog.PlaceByID(n.Place)
	c := ReadyCheck{Need: n, ID: n.ID(), Label: "Agent " + strings.ToLower(info.Label), Level: ReadyOK, CheckedAt: now}

	if !r.offers.runsAt(n.Place) {
		c.Level, c.Found = ReadyProblem, "isn't something this version of "+brand.Name+" can run yet"
		c.Fix = "Change the step to run " + strings.ToLower(r.offers.placeWords()) + "."
	}

	return c
}

// lastChecked returns when the checks last ran, and whether they run now.
func (r *readiness) lastChecked() (*time.Time, bool) {
	if r == nil {
		return nil, false
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.at.IsZero() {
		return nil, r.checking
	}

	return new(r.at), r.checking
}

// routineReady works out one routine's readiness from s, or nil while a need of it was
// never checked.
func (s readySnapshot) routineReady(id int64, name string, sp spec.Spec, startOnly bool) (*RoutineReady, bool) {
	out := &RoutineReady{RoutineID: id, Name: name, Ready: true, Problems: []ReadyCheck{}, Warnings: []ReadyCheck{}}

	for _, n := range Needs(sp, s.defaults) {
		if startOnly && !n.forStart() {
			continue
		}

		c, ok := s.results[n.ID()]
		if !ok {
			return nil, false
		}

		switch {
		case c.Level == ReadyProblem && !n.Fallback:
			out.Ready = false
			out.Problems = append(out.Problems, c)
		case c.Level == ReadyWarning, c.Level == ReadyProblem:
			// A fallback this Mac can't run only warns: the step runs on its own profile.
			out.Warnings = append(out.Warnings, c)
		}
	}

	return out, true
}

// Readiness returns this Mac's readiness as the checks last found it: checked now when
// they never ran, else as kept, while the engine checks again on its own as they go
// stale.
func (s *Service) Readiness(ctx context.Context) (Readiness, error) {
	if when, checking := s.ready.lastChecked(); when == nil && !checking {
		return s.CheckReadiness(ctx)
	}

	return s.readinessNow(ctx)
}

// CheckReadiness runs every readiness check again now, the slow ones too and GitHub's
// sign-in (one point), and returns what they found. The engine then decides on what it
// found, so a routine that was waiting to be ready starts.
func (s *Service) CheckReadiness(ctx context.Context) (Readiness, error) {
	if s.ready.begin() {
		if err := s.ready.run(ctx, s.enabledSpecs(ctx), true, s.clock.Now()); err != nil {
			s.logger.WarnContext(ctx, "could not check what this Mac has for Autopilot", "error", err)
		}
	}

	if s.engine != nil {
		s.engine.Wake()
	}

	return s.readinessNow(ctx)
}

// enabledSpecs returns the specs of the enabled routines of the account signed in that
// load, the ones readiness checks for.
func (s *Service) enabledSpecs(ctx context.Context) []spec.Spec {
	routines, err := s.List(ctx)
	if err != nil {
		s.logger.WarnContext(ctx, "could not read the routines to check what this Mac has", "error", err)

		return nil
	}

	var out []spec.Spec

	for _, r := range routines {
		if r.Enabled && !r.OtherAccount && r.BrokenReason == "" {
			out = append(out, r.Spec)
		}
	}

	return out
}

// readinessNow builds the window's Readiness from the results kept.
func (s *Service) readinessNow(ctx context.Context) (Readiness, error) {
	when, checking := s.ready.lastChecked()
	out := Readiness{CheckedAt: when, Checking: checking, Checks: []ReadyCheck{}, Routines: []RoutineReady{}}

	snap := s.ready.snapshot()
	if !snap.on {
		return out, nil
	}

	out.Checks = sortedChecks(snap.results)

	routines, err := s.List(ctx)
	if err != nil {
		return out, err
	}

	for _, r := range routines {
		if !r.Enabled || r.OtherAccount || r.BrokenReason != "" {
			continue
		}

		rr, ok := snap.routineReady(r.ID, r.Name, r.Spec, false)
		out.Checking = out.Checking || !ok

		if ok {
			out.Routines = append(out.Routines, *rr)
			out.NotReady += countIf(!rr.Ready)
		}
	}

	return out, nil
}

// sortedChecks returns results in This Mac's order, then by name.
func sortedChecks(results map[string]ReadyCheck) []ReadyCheck {
	out := make([]ReadyCheck, 0, len(results))
	for _, c := range results {
		out = append(out, c)
	}

	slices.SortFunc(out, func(a, b ReadyCheck) int {
		if d := checkOrder(a) - checkOrder(b); d != 0 {
			return d
		}

		return strings.Compare(a.ID, b.ID)
	})

	return out
}

// countIf is 1 when ok, else 0.
func countIf(ok bool) int {
	if ok {
		return 1
	}

	return 0
}

// checkOrder orders This Mac's checks: GitHub and the tools it borrows first, then the
// agents and their profiles, the terminal, the places, the banners, and other tools.
func checkOrder(c ReadyCheck) int {
	switch {
	case c.Need.Kind == NeedGitHub:
		return 0
	case c.ID == "tool:gh":
		return 1
	case c.ID == "tool:git":
		return 2
	case c.Need.Kind == NeedAgent:
		return 3
	case c.Need.Kind == NeedProfile:
		return 4
	case c.Need.Kind == NeedTerminal:
		return 5
	case c.Need.Kind == NeedPlace:
		return 6
	case c.Need.Kind == NeedBanners:
		return 7
	default:
		return 8
	}
}
