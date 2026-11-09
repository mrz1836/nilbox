package launcher

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/trust"
)

// Source is where a resolved launch setting came from.
type Source string

// Sources, from the most specific to the least.
const (
	// SourceLaunch is a choice made in the launch menu for this launch only.
	SourceLaunch Source = "launch"

	// SourcePrompt is the chosen prompt's own setting.
	SourcePrompt Source = "prompt"

	// SourceRepo and SourceOrg are the repository's and its owner's launch defaults.
	SourceRepo Source = "repo"
	SourceOrg  Source = "org"

	// SourceProfile is the profile's own setting.
	SourceProfile Source = "profile"

	// SourceApp is the app's settings.
	SourceApp Source = "app"

	// SourceAgent means nothing set it, so the agent uses its own default.
	SourceAgent Source = "agent"

	// SourceNone is the source of every setting of an item whose settings could not
	// resolve (see ItemResolution.Problem).
	SourceNone Source = ""
)

var (
	// ErrNoPrompt is returned when no prompt applies to the item's kind.
	ErrNoPrompt = errors.New("no prompt applies to this kind of item; add one on the Prompts tab")

	// ErrNoProfile is returned when no profile of the agent is stored to launch with.
	ErrNoProfile = errors.New("no profile was found")
)

// noProfile says the agent has no profile, and how to make one.
func noProfile(a agent.Agent) error {
	return fmt.Errorf("%w: no %s profile was found; run %s once so it creates ~/%s", ErrNoProfile, a.Name, a.Command,
		a.ConfigDir)
}

// Setting is one resolved launch setting: its value and where it came from, and the
// value it would have without this launch's own choice, which the launch menu offers as
// the default.
type Setting struct {
	Value           string `json:"value"`
	Source          Source `json:"source"`
	Inherited       string `json:"inherited"`
	InheritedSource Source `json:"inheritedSource"`
}

// Toggle is a resolved on-or-off launch setting, like Setting.
type Toggle struct {
	Value           bool   `json:"value"`
	Source          Source `json:"source"`
	Inherited       bool   `json:"inherited"`
	InheritedSource Source `json:"inheritedSource"`
}

// PromptChoice is the resolved prompt, like Setting.
type PromptChoice struct {
	ID              int64  `json:"id"`
	Nickname        string `json:"nickname"`
	Name            string `json:"name"`
	Source          Source `json:"source"`
	InheritedID     int64  `json:"inheritedId"`
	InheritedSource Source `json:"inheritedSource"`
}

// Resolution is what a launch on one item resolves to, each setting with where it came
// from. Every field is a plain value.
type Resolution struct {
	// ItemID and Kind identify the item.
	ItemID int64     `json:"itemId"`
	Kind   item.Kind `json:"kind"`

	// Owner and Repo ("owner/name") are where launch defaults for the item are saved.
	Owner string `json:"owner"`
	Repo  string `json:"repo"`

	// Trusted reports whether the item's author is trusted.
	Trusted bool `json:"trusted"`

	// Agent is the coding agent, and AgentSource where it was chosen. AgentInherited is
	// the agent without this launch's own choice, and AgentInheritedSource where that
	// came from.
	Agent                agent.Agent `json:"agent"`
	AgentSource          Source      `json:"agentSource"`
	AgentInherited       agent.ID    `json:"agentInherited"`
	AgentInheritedSource Source      `json:"agentInheritedSource"`

	Profile  Setting      `json:"profile"`
	Prompt   PromptChoice `json:"prompt"`
	Model    Setting      `json:"model"`
	Effort   Setting      `json:"effort"`
	PlanMode Toggle       `json:"planMode"`
	Worktree Toggle       `json:"worktree"`

	// EffortOff reports that the launch passes the agent no effort level whatever the
	// layers say: the agent takes it only as a config override, and the app's settings
	// leave that off, so it uses its own. Effort is then empty.
	EffortOff bool `json:"effortOff"`

	// Fast is whether the session starts in the agent's Fast mode: the launch's own
	// choice, else the app's setting. It is off, from the agent, for one without.
	Fast Toggle `json:"fast"`

	// ExtraArgs are every extra argument for the agent, in order: the profile's, the
	// org's, the repository's, and the prompt's.
	ExtraArgs []string `json:"extraArgs"`

	// Warnings explain settings that were skipped, such as a repository default naming
	// a profile that no longer exists.
	Warnings []string `json:"warnings"`
}

// world is everything resolution reads besides the item: the app's settings, the prompt
// library, the profiles, and the launch defaults.
type world struct {
	settings LaunchSettings
	defaults prompts.DefaultPrompts
	prompts  map[int64]prompts.Prompt
	profiles map[profileKey]profiles.Profile
	rules    launchdefaults.Index

	// policy decides items' trust with the trusted bots in the settings.
	policy trust.Policy
}

// loadWorld reads everything resolution needs: the settings, the prompt library, the
// stored profiles, and the launch defaults.
func (s *Service) loadWorld(ctx context.Context) (world, error) {
	prefs, err := s.settings.Get(ctx)
	if err != nil {
		return world{}, err
	}

	library, err := s.prompts.Library(ctx)
	if err != nil {
		return world{}, err
	}

	stored, err := s.profiles.All(ctx)
	if err != nil {
		return world{}, err
	}

	rules, err := s.defaults.All(ctx)
	if err != nil {
		return world{}, err
	}

	w := world{
		settings: prefs, defaults: library.Defaults, rules: launchdefaults.NewIndex(rules),
		policy:   trust.NewPolicy(prefs.TrustedBots),
		prompts:  make(map[int64]prompts.Prompt, len(library.Prompts)),
		profiles: make(map[profileKey]profiles.Profile, len(stored)),
	}

	for _, p := range library.Prompts {
		w.prompts[p.ID] = p
	}

	for _, p := range stored {
		w.profiles[profileKey{agent: p.Agent, name: p.Name}] = p
	}

	return w, nil
}

// profileKey identifies a stored profile: its agent and its name.
type profileKey struct {
	agent agent.ID
	name  string
}

// profileOf returns the agent's profile named name.
func (w world) profileOf(id agent.ID, name string) (profiles.Profile, bool) {
	p, ok := w.profiles[profileKey{agent: id, name: name}]

	return p, ok
}

// profileNames returns the names of the agent's profiles, sorted.
func (w world) profileNames(id agent.ID) []string {
	var names []string

	for key := range w.profiles {
		if key.agent == id {
			names = append(names, key.name)
		}
	}

	slices.Sort(names)

	return names
}

// subject is what resolution needs to know about an item.
type subject struct {
	id      int64
	kind    item.Kind
	repo    repo.Name
	trusted bool
}

// resolver resolves one item's launch settings against a world.
type resolver struct {
	w        world
	item     subject
	org      *launchdefaults.Defaults
	repo     *launchdefaults.Defaults
	agent    agent.Agent
	warnings []string
}

// resolve works out every launch setting for an item: the request's own choices first,
// then the chosen prompt's settings, the repository's launch defaults, its owner's, the
// profile's, and the app's settings. The agent is resolved first, and every per-agent
// setting (the profile, the model, and the extra flags) is that agent's. A stored default
// that no longer makes sense (an agent this build does not know, a profile or prompt that
// is gone, a prompt for the other kind of item, or an effort level the agent lacks) is
// skipped with a warning; a request naming an agent, profile, or prompt that does not
// exist is an error.
func resolve(w world, it subject, req LaunchRequest) (Resolution, profiles.Profile, prompts.Prompt, error) {
	r := &resolver{w: w, item: it}
	r.org, r.repo = w.rules.For(it.repo)

	res := Resolution{
		ItemID: it.id, Kind: it.kind, Owner: it.repo.Owner, Repo: it.repo.String(), Trusted: it.trusted,
	}

	if err := r.resolveAgent(req.Agent, &res); err != nil {
		return Resolution{}, profiles.Profile{}, prompts.Prompt{}, err
	}

	profile, err := r.profile(req.Profile, &res)
	if err != nil {
		return Resolution{}, profiles.Profile{}, prompts.Prompt{}, err
	}

	prompt, err := r.prompt(req.PromptID, &res)
	if err != nil {
		return Resolution{}, profiles.Profile{}, prompts.Prompt{}, err
	}

	id := r.agent.ID
	res.Model = r.text(req.Model, prompt.Models[id], func(d *launchdefaults.Defaults) string { return d.Models[id] },
		profile.Model, SourceProfile)
	res.Effort, res.EffortOff = r.agentEffort(req.Effort, prompt.Effort, profile.Effort)
	res.Fast = r.fast(req.Fast)
	res.PlanMode = r.toggle(req.PlanMode, prompt.PlanMode, func(d *launchdefaults.Defaults) *bool { return d.PlanMode },
		profile.PlanMode, SourceProfile)
	res.Worktree = r.toggle(req.Worktree, prompt.Worktree, func(d *launchdefaults.Defaults) *bool { return d.Worktree },
		w.settings.WorktreeDefault, SourceApp)

	if res.ExtraArgs, err = r.extraArgs(profile, prompt); err != nil {
		return Resolution{}, profiles.Profile{}, prompts.Prompt{}, err
	}

	res.Warnings = textutil.NonNil(r.warnings)

	return res, profile, prompt, nil
}

// layers returns the repository's and the org's launch defaults, most specific first,
// with their sources, leaving out the ones not stored.
func (r *resolver) layers() []sourced {
	var out []sourced

	if r.repo != nil {
		out = append(out, sourced{defaults: r.repo, source: SourceRepo})
	}

	if r.org != nil {
		out = append(out, sourced{defaults: r.org, source: SourceOrg})
	}

	return out
}

// sourced is a set of launch defaults with where it comes from.
type sourced struct {
	defaults *launchdefaults.Defaults
	source   Source
}

// resolveAgent resolves the coding agent: the request's, else the repository's, the
// org's, or the app's default agent, else Claude.
func (r *resolver) resolveAgent(requested agent.ID, res *Resolution) error {
	inherited, source := r.inheritedAgent()
	r.agent, res.AgentSource = inherited, source
	res.AgentInherited, res.AgentInheritedSource = inherited.ID, source

	if requested != agent.Inherit {
		a, err := agent.Lookup(requested)
		if err != nil {
			return err
		}

		r.agent, res.AgentSource = a, SourceLaunch
	}

	res.Agent = r.agent

	return nil
}

// inheritedAgent returns the agent a launch on the item starts when nothing chooses one.
func inheritedAgent(w world, it subject) agent.Agent {
	r := &resolver{w: w, item: it}
	r.org, r.repo = w.rules.For(it.repo)
	a, _ := r.inheritedAgent()

	return a
}

// inheritedAgent resolves the agent without the request's choice.
func (r *resolver) inheritedAgent() (agent.Agent, Source) {
	for _, l := range r.layers() {
		if l.defaults.Agent == agent.Inherit {
			continue
		}

		if a, err := agent.Lookup(l.defaults.Agent); err == nil {
			return a, l.source
		}

		r.warn("The %s default names a coding agent this version doesn't know (%q), so it was skipped.",
			l.source, l.defaults.Agent)
	}

	if a, err := agent.Lookup(r.w.settings.DefaultAgent); err == nil && r.w.settings.DefaultAgent != agent.Inherit {
		return a, SourceApp
	}

	return agent.Default(), SourceApp
}

// effort keeps a resolved effort level only when the agent takes it. A level it lacks,
// such as Codex's ultra for Claude, is skipped with a warning, and the agent's own
// default is used.
func (r *resolver) effort(s Setting) Setting {
	check := func(value string, source Source) (string, Source) {
		if value == "" || agent.ValidateEffort(r.agent.ID, value) == nil {
			return value, source
		}

		r.warn("%s doesn't take the %q effort level, so its default is used.", r.agent.Name, value)

		return "", SourceAgent
	}

	s.Inherited, s.InheritedSource = check(s.Inherited, s.InheritedSource)

	if s.Source == SourceLaunch {
		s.Value, s.Source = check(s.Value, s.Source)
	} else {
		s.Value, s.Source = s.Inherited, s.InheritedSource
	}

	return s
}

// agentEffort resolves the effort level the agent is passed, and reports true when it is
// passed none whatever the layers say: it takes one only as a config override, and the
// app's settings leave that off.
func (r *resolver) agentEffort(requested *string, prompt, profile string) (Setting, bool) {
	if r.agent.ConfigOverrides && !r.w.settings.EffortOverrides {
		return Setting{Source: SourceApp, InheritedSource: SourceApp}, true
	}

	return r.effort(r.text(requested, prompt, func(d *launchdefaults.Defaults) string { return d.Effort }, profile,
		SourceProfile)), false
}

// fast resolves Fast mode for an agent that has one: the request's choice, else the app's
// setting. An agent without one is off, by its own default.
func (r *resolver) fast(requested *bool) Toggle {
	if !r.agent.FastMode {
		return Toggle{Source: SourceAgent, InheritedSource: SourceAgent}
	}

	t := Toggle{Value: r.w.settings.FastMode, Source: SourceApp, Inherited: r.w.settings.FastMode, InheritedSource: SourceApp}
	if requested != nil {
		t.Value, t.Source = *requested, SourceLaunch
	}

	return t
}

// profile resolves the agent's profile: the request's, else the repository's, the org's,
// or the app's default profile for the agent, else the agent's default profile, else its
// first one stored.
func (r *resolver) profile(requested string, res *Resolution) (profiles.Profile, error) {
	inherited, source, err := r.inheritedProfile()
	if err != nil {
		return profiles.Profile{}, err
	}

	res.Profile = Setting{Value: inherited.Name, Source: source, Inherited: inherited.Name, InheritedSource: source}

	if requested == "" {
		return inherited, nil
	}

	p, ok := r.w.profileOf(r.agent.ID, requested)
	if !ok {
		return profiles.Profile{}, fmt.Errorf("%w: %s %q", profiles.ErrProfileNotFound, r.agent.Name, requested)
	}

	res.Profile.Value, res.Profile.Source = p.Name, SourceLaunch

	return p, nil
}

// inheritedProfile resolves the agent's profile without the request's choice.
func (r *resolver) inheritedProfile() (profiles.Profile, Source, error) {
	id := r.agent.ID

	for _, c := range r.layers() {
		if name := c.defaults.Profiles[id]; name != "" {
			if p, ok := r.w.profileOf(id, name); ok {
				return p, c.source, nil
			}

			r.warn("The %s default %s profile %q no longer exists, so it was skipped.", c.source, r.agent.Name, name)
		}
	}

	if p, ok := r.w.profileOf(id, r.w.settings.DefaultProfiles[id]); ok {
		return p, SourceApp, nil
	}

	if p, ok := r.w.profileOf(id, profiles.DefaultName); ok {
		return p, SourceApp, nil
	}

	names := r.w.profileNames(id)
	if len(names) == 0 {
		return profiles.Profile{}, "", noProfile(r.agent)
	}

	p, _ := r.w.profileOf(id, names[0])

	return p, SourceApp, nil
}

// prompt resolves the prompt: the request's, else the repository's, the org's, or the
// app's default for the item's kind.
func (r *resolver) prompt(requested int64, res *Resolution) (prompts.Prompt, error) {
	inherited, source, err := r.inheritedPrompt()
	if err != nil {
		return prompts.Prompt{}, err
	}

	res.Prompt = PromptChoice{
		ID: inherited.ID, Nickname: inherited.Nickname, Name: inherited.Name, Source: source,
		InheritedID: inherited.ID, InheritedSource: source,
	}

	if requested == 0 {
		return inherited, nil
	}

	p, ok := r.w.prompts[requested]
	if !ok {
		return prompts.Prompt{}, fmt.Errorf("%w (id %d)", prompts.ErrPromptNotFound, requested)
	}

	if !p.AppliesTo.Covers(r.item.kind) {
		return prompts.Prompt{}, fmt.Errorf("%w: %q isn't for %s", prompts.ErrWrongKind, p.Nickname,
			r.item.kind.Plural())
	}

	res.Prompt.ID, res.Prompt.Nickname, res.Prompt.Name, res.Prompt.Source = p.ID, p.Nickname, p.Name, SourceLaunch

	return p, nil
}

// inheritedPrompt resolves the prompt without the request's choice.
func (r *resolver) inheritedPrompt() (prompts.Prompt, Source, error) {
	for _, l := range r.layers() {
		id := l.defaults.PRPromptID
		if r.item.kind == item.KindIssue {
			id = l.defaults.IssuePromptID
		}

		if id == nil {
			continue
		}

		if p, ok := r.w.prompts[*id]; ok && p.AppliesTo.Covers(r.item.kind) {
			return p, l.source, nil
		}

		r.warn("The %s default prompt for %s no longer exists, so it was skipped.", l.source,
			r.item.kind.Plural())
	}

	if p, ok := r.w.prompts[r.w.defaults.For(r.item.kind)]; ok {
		return p, SourceApp, nil
	}

	return prompts.Prompt{}, "", fmt.Errorf("%w (%s)", ErrNoPrompt, r.item.kind.Plural())
}

// text resolves a text setting: the request's, else the prompt's, the repository's, the
// org's, else base (from baseSource), else "" for the agent's default.
func (r *resolver) text(requested *string, fromPrompt string, pick func(*launchdefaults.Defaults) string,
	base string, baseSource Source,
) Setting {
	value, source := base, baseSource

	if base == "" {
		source = SourceAgent
	}

	for _, l := range slices.Backward(r.layers()) {
		if v := pick(l.defaults); v != "" {
			value, source = v, l.source
		}
	}

	if fromPrompt != "" {
		value, source = fromPrompt, SourcePrompt
	}

	s := Setting{Value: value, Source: source, Inherited: value, InheritedSource: source}
	if requested != nil {
		s.Value, s.Source = *requested, SourceLaunch
	}

	return s
}

// toggle resolves an on-or-off setting the way text resolves text, with base as the
// value nothing else set.
func (r *resolver) toggle(requested, fromPrompt *bool, pick func(*launchdefaults.Defaults) *bool,
	base bool, baseSource Source,
) Toggle {
	value, source := base, baseSource

	for _, l := range slices.Backward(r.layers()) {
		if v := pick(l.defaults); v != nil {
			value, source = *v, l.source
		}
	}

	if fromPrompt != nil {
		value, source = *fromPrompt, SourcePrompt
	}

	t := Toggle{Value: value, Source: source, Inherited: value, InheritedSource: source}
	if requested != nil {
		t.Value, t.Source = *requested, SourceLaunch
	}

	return t
}

// extraArgs returns every extra argument for the agent, in order: the profile's, the
// org's, the repository's, and the prompt's. The launch defaults' and the prompt's are
// checked again, in case the database was edited by hand, and then the whole list, since
// an option that ends one layer takes the next layer's first argument as its value, as
// an org's -c would a repository's model=x.
func (r *resolver) extraArgs(profile profiles.Profile, prompt prompts.Prompt) ([]string, error) {
	id := r.agent.ID
	args := slices.Clone(profile.ExtraArgs)

	for _, l := range slices.Backward(r.layers()) {
		if err := agent.CheckExtra(id, l.defaults.ExtraArgs[id]); err != nil {
			return nil, fmt.Errorf("the %s launch defaults' %s flags: %w", l.source, r.agent.Name, err)
		}

		args = append(args, l.defaults.ExtraArgs[id]...)
	}

	if err := agent.CheckExtra(id, prompt.ExtraArgs[id]); err != nil {
		return nil, fmt.Errorf("the %q prompt's %s flags: %w", prompt.Nickname, r.agent.Name, err)
	}

	args = append(args, prompt.ExtraArgs[id]...)
	if err := agent.CheckExtra(id, args); err != nil {
		return nil, fmt.Errorf("the %s flags the profile, the launch defaults, and the prompt add together: %w",
			r.agent.Name, err)
	}

	return textutil.NonNil(args), nil
}

// warn records a warning.
func (r *resolver) warn(format string, args ...any) {
	r.warnings = append(r.warnings, fmt.Sprintf(format, args...))
}

// effectiveProfile returns the profile a launch runs with: the resolved profile with the
// resolved model, effort, plan mode, and extra arguments in place of its own.
func effectiveProfile(p profiles.Profile, res Resolution) profiles.Profile {
	p.Model, p.Effort, p.PlanMode = res.Model.Value, res.Effort.Value, res.PlanMode.Value
	p.ExtraArgs = slices.Clone(res.ExtraArgs)

	return p
}
