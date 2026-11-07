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
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/trust"
)

// Where a resolved launch setting came from, from the most specific to the least.
const (
	// SourceLaunch is a choice made in the launch menu for this launch only.
	SourceLaunch = "launch"

	// SourcePrompt is the chosen prompt's own setting.
	SourcePrompt = "prompt"

	// SourceRepo and SourceOrg are the repository's and its owner's launch defaults.
	SourceRepo = "repo"
	SourceOrg  = "org"

	// SourceProfile is the profile's own setting.
	SourceProfile = "profile"

	// SourceApp is the app's settings.
	SourceApp = "app"

	// SourceAgent means nothing set it, so the agent uses its own default.
	SourceAgent = "agent"
)

var (
	// ErrNoPrompt is returned when no prompt applies to the item's kind.
	ErrNoPrompt = errors.New("no prompt applies to this kind of item; add one on the Prompts tab")

	// ErrNoProfile is returned when no profile is stored to launch with.
	ErrNoProfile = errors.New("no agent profile was found; run the agent once so it creates ~/.claude")
)

// Setting is one resolved launch setting: its value and where it came from, and the
// value it would have without this launch's own choice, which the launch menu offers as
// the default.
type Setting struct {
	Value           string `json:"value"`
	Source          string `json:"source"`
	Inherited       string `json:"inherited"`
	InheritedSource string `json:"inheritedSource"`
}

// Toggle is a resolved on-or-off launch setting, like Setting.
type Toggle struct {
	Value           bool   `json:"value"`
	Source          string `json:"source"`
	Inherited       bool   `json:"inherited"`
	InheritedSource string `json:"inheritedSource"`
}

// PromptChoice is the resolved prompt, like Setting.
type PromptChoice struct {
	ID              int64  `json:"id"`
	Nickname        string `json:"nickname"`
	Name            string `json:"name"`
	Source          string `json:"source"`
	InheritedID     int64  `json:"inheritedId"`
	InheritedSource string `json:"inheritedSource"`
}

// Resolution is what a launch on one item resolves to, each setting with where it came
// from. Every field is a plain value.
type Resolution struct {
	// ItemID and Kind identify the item.
	ItemID int64  `json:"itemId"`
	Kind   string `json:"kind"`

	// Owner and Repo ("owner/name") are where launch defaults for the item are saved.
	Owner string `json:"owner"`
	Repo  string `json:"repo"`

	// Trusted reports whether the item's author is trusted.
	Trusted bool `json:"trusted"`

	// Agent is the coding tool, and AgentSource where it was chosen.
	Agent       agent.Agent `json:"agent"`
	AgentSource string      `json:"agentSource"`

	Profile  Setting      `json:"profile"`
	Prompt   PromptChoice `json:"prompt"`
	Model    Setting      `json:"model"`
	Effort   Setting      `json:"effort"`
	PlanMode Toggle       `json:"planMode"`
	Worktree Toggle       `json:"worktree"`

	// ExtraArgs are every extra argument, in order: the profile's, the org's, the
	// repository's, and the prompt's.
	ExtraArgs []string `json:"extraArgs"`

	// Warnings explain settings that were skipped, such as a repository default naming
	// a profile that no longer exists.
	Warnings []string `json:"warnings"`
}

// world is everything resolution reads besides the item: the app's settings, the prompt
// library, the profiles, and the launch defaults.
type world struct {
	settings settings.Settings
	defaults prompts.DefaultPrompts
	prompts  map[int64]prompts.Prompt
	profiles map[string]profiles.Profile
	rules    launchdefaults.Index

	// policy decides items' trust with the trusted bots in the settings.
	policy trust.Policy
}

// loadWorld reads everything resolution needs from the database, the settings, and the
// stored profiles.
func (s *Service) loadWorld(ctx context.Context) (world, error) {
	prefs, err := s.settings.Get(ctx)
	if err != nil {
		return world{}, err
	}

	defaults, err := prompts.LoadDefaults(ctx, s.db)
	if err != nil {
		return world{}, err
	}

	all, err := prompts.All(ctx, s.db)
	if err != nil {
		return world{}, err
	}

	stored, err := s.profiles.All(ctx)
	if err != nil {
		return world{}, err
	}

	rules, err := launchdefaults.All(ctx, s.db)
	if err != nil {
		return world{}, err
	}

	w := world{
		settings: prefs, defaults: defaults, rules: launchdefaults.NewIndex(rules),
		policy:  trust.NewPolicy(prefs.TrustedBots),
		prompts: make(map[int64]prompts.Prompt, len(all)), profiles: make(map[string]profiles.Profile, len(stored)),
	}

	for _, p := range all {
		w.prompts[p.ID] = p
	}

	for _, p := range stored {
		w.profiles[p.Name] = p
	}

	return w, nil
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
	warnings []string
}

// resolve works out every launch setting for an item: the request's own choices first,
// then the chosen prompt's settings, the repository's launch defaults, its owner's, the
// profile's, and the app's settings. A stored default that no longer makes sense (a
// profile or prompt that is gone, or a prompt for the other kind of item) is skipped
// with a warning; a request naming one is an error.
func resolve(w world, it subject, req LaunchRequest) (Resolution, profiles.Profile, prompts.Prompt, error) {
	r := &resolver{w: w, item: it}
	r.org, r.repo = w.rules.For(it.repo)

	res := Resolution{
		ItemID: it.id, Kind: string(it.kind), Owner: it.repo.Owner, Repo: it.repo.String(), Trusted: it.trusted,
	}
	res.Agent, res.AgentSource = r.agent()

	profile, err := r.profile(req.Profile, &res)
	if err != nil {
		return Resolution{}, profiles.Profile{}, prompts.Prompt{}, err
	}

	prompt, err := r.prompt(req.PromptID, &res)
	if err != nil {
		return Resolution{}, profiles.Profile{}, prompts.Prompt{}, err
	}

	res.Model = r.text(req.Model, prompt.Model, func(d *launchdefaults.Defaults) string { return d.Model },
		profile.Model, SourceProfile)
	res.Effort = r.text(req.Effort, prompt.Effort, func(d *launchdefaults.Defaults) string { return d.Effort },
		profile.Effort, SourceProfile)
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
	source   string
}

// agent resolves the coding tool: the repository's, else the org's, else the app's.
func (r *resolver) agent() (agent.Agent, string) {
	for _, l := range r.layers() {
		if l.defaults.Agent == "" {
			continue
		}

		a, err := agent.Lookup(l.defaults.Agent)
		if err == nil {
			return a, l.source
		}

		r.warn("The %s default names a coding tool this version doesn't know (%q), so %s is used.",
			l.source, l.defaults.Agent, agent.Default().Name)
	}

	return agent.Default(), SourceApp
}

// profile resolves the profile: the request's, else the repository's, the org's, or the
// app's default profile, else the default profile, else the first one stored.
func (r *resolver) profile(requested string, res *Resolution) (profiles.Profile, error) {
	inherited, source, err := r.inheritedProfile()
	if err != nil {
		return profiles.Profile{}, err
	}

	res.Profile = Setting{Value: inherited.Name, Source: source, Inherited: inherited.Name, InheritedSource: source}

	if requested == "" {
		return inherited, nil
	}

	p, ok := r.w.profiles[requested]
	if !ok {
		return profiles.Profile{}, fmt.Errorf("%w: %q", profiles.ErrProfileNotFound, requested)
	}

	res.Profile.Value, res.Profile.Source = p.Name, SourceLaunch

	return p, nil
}

// inheritedProfile resolves the profile without the request's choice.
func (r *resolver) inheritedProfile() (profiles.Profile, string, error) {
	for _, c := range r.layers() {
		if name := c.defaults.Profile; name != "" {
			if p, ok := r.w.profiles[name]; ok {
				return p, c.source, nil
			}

			r.warn("The %s default profile %q no longer exists, so it was skipped.", c.source, name)
		}
	}

	if p, ok := r.w.profiles[r.w.settings.DefaultProfile]; ok {
		return p, SourceApp, nil
	}

	if p, ok := r.w.profiles[profiles.DefaultName]; ok {
		return p, SourceApp, nil
	}

	names := make([]string, 0, len(r.w.profiles))
	for name := range r.w.profiles {
		names = append(names, name)
	}

	if len(names) == 0 {
		return profiles.Profile{}, "", ErrNoProfile
	}

	slices.Sort(names)

	return r.w.profiles[names[0]], SourceApp, nil
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

	if !prompts.AppliesTo(p.AppliesTo, r.item.kind) {
		return prompts.Prompt{}, fmt.Errorf("%w: %q isn't for %s", prompts.ErrWrongKind, p.Nickname,
			r.item.kind.Plural())
	}

	res.Prompt.ID, res.Prompt.Nickname, res.Prompt.Name, res.Prompt.Source = p.ID, p.Nickname, p.Name, SourceLaunch

	return p, nil
}

// inheritedPrompt resolves the prompt without the request's choice.
func (r *resolver) inheritedPrompt() (prompts.Prompt, string, error) {
	for _, l := range r.layers() {
		id := l.defaults.PRPromptID
		if r.item.kind == item.KindIssue {
			id = l.defaults.IssuePromptID
		}

		if id == nil {
			continue
		}

		if p, ok := r.w.prompts[*id]; ok && prompts.AppliesTo(p.AppliesTo, r.item.kind) {
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
	base, baseSource string,
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
	base bool, baseSource string,
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

// extraArgs returns every extra argument in order: the profile's, the org's, the
// repository's, and the prompt's. The launch defaults' and the prompt's are checked
// again, in case the database was edited by hand.
func (r *resolver) extraArgs(profile profiles.Profile, prompt prompts.Prompt) ([]string, error) {
	args := slices.Clone(profile.ExtraArgs)

	for _, l := range slices.Backward(r.layers()) {
		if err := agent.CheckExtra(l.defaults.ExtraArgs); err != nil {
			return nil, fmt.Errorf("the %s launch defaults' flags: %w", l.source, err)
		}

		args = append(args, l.defaults.ExtraArgs...)
	}

	if err := agent.CheckExtra(prompt.ExtraArgs); err != nil {
		return nil, fmt.Errorf("the %q prompt's flags: %w", prompt.Nickname, err)
	}

	return textutil.NonNil(append(args, prompt.ExtraArgs...)), nil
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
