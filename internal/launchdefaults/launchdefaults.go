// Package launchdefaults stores launch defaults for orgs and repositories: the coding
// agent, each agent's profile, model, and extra flags, the default prompt for each kind of
// item, and the effort, plan mode, and worktree that launches on their items start from.
//
// An owner's defaults (Name "") apply to every repository of that owner, and a
// repository's own defaults win over them. Every setting inherits when it is "", nil, or
// has no entry for the agent, so a row only holds what differs. The launcher resolves the
// full order: the app's settings, then the owner's defaults, the repository's, the chosen
// prompt's, and last the launch menu's.
package launchdefaults

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// Fields an agent.FieldError can name, matching Input's JSON names. A per-agent field's
// error for one agent names it with agent.FieldFor, as in "profiles.codex".
const (
	FieldOwner         = "owner"
	FieldName          = "name"
	FieldAgent         = "agent"
	FieldProfiles      = "profiles"
	FieldPRPromptID    = "prPromptId"
	FieldIssuePromptID = "issuePromptId"
	FieldModels        = agent.FieldModels
	FieldEffort        = agent.FieldEffort
	FieldExtraFlags    = agent.FieldExtraFlags
)

var (
	// ErrInvalidDefaults is returned when launch defaults cannot be saved (see
	// agent.ValidationError).
	ErrInvalidDefaults = errors.New("the launch defaults can't be saved")

	// ErrNotFound is returned when removing defaults that are not stored.
	ErrNotFound = errors.New("no launch defaults are stored for that org or repository")
)

// Defaults are one org's or repository's launch defaults. Every field is a plain value.
type Defaults struct {
	// Owner is the org or user; Name is the repository, or "" for the owner's defaults.
	Owner string `json:"owner"`
	Name  string `json:"name"`

	// Agent is the coding agent; agent.Inherit uses the app's default agent.
	Agent agent.ID `json:"agent"`

	// Profiles names each agent's profile; an agent with no entry inherits.
	Profiles map[agent.ID]string `json:"profiles"`

	// PRPromptID and IssuePromptID are the default prompts; nil inherits.
	PRPromptID    *int64 `json:"prPromptId"`
	IssuePromptID *int64 `json:"issuePromptId"`

	// Models is each agent's model; an agent with no entry inherits.
	Models map[agent.ID]string `json:"models"`

	// Effort is the effort level of every agent; "" inherits. An agent that lacks the
	// level launches with its own default.
	Effort string `json:"effort"`

	// PlanMode and Worktree turn plan mode and the worktree on or off; nil inherits.
	PlanMode *bool `json:"planMode"`
	Worktree *bool `json:"worktree"`

	// ExtraArgs is each agent's extra arguments, added after its profile's (an owner's
	// before its repository's), and ExtraFlags is them on one line, quoted.
	ExtraArgs  map[agent.ID][]string `json:"extraArgs"`
	ExtraFlags map[agent.ID]string   `json:"extraFlags"`

	// UpdatedAt is when the defaults were last saved, as RFC 3339.
	UpdatedAt string `json:"updatedAt"`
}

// Target names the org or repository a Defaults is for: "acme-org" or
// "acme-org/example-repo".
func (d Defaults) Target() string {
	return target(d.Owner, d.Name)
}

// Input is launch defaults as the editor saves them.
type Input struct {
	Owner         string              `json:"owner"`
	Name          string              `json:"name"`
	Agent         agent.ID            `json:"agent"`
	Profiles      map[agent.ID]string `json:"profiles"`
	PRPromptID    *int64              `json:"prPromptId"`
	IssuePromptID *int64              `json:"issuePromptId"`
	Models        map[agent.ID]string `json:"models"`
	Effort        string              `json:"effort"`
	PlanMode      *bool               `json:"planMode"`
	Worktree      *bool               `json:"worktree"`

	// ExtraFlags is each agent's extra arguments typed on one line, quoted as in a shell.
	ExtraFlags map[agent.ID]string `json:"extraFlags"`
}

// Empty reports whether every setting inherits.
func (in Input) Empty() bool {
	return in.Agent == agent.Inherit && !anySet(in.Profiles) && in.PRPromptID == nil && in.IssuePromptID == nil &&
		!anySet(in.Models) && in.Effort == "" && in.PlanMode == nil && in.Worktree == nil && !anySet(in.ExtraFlags)
}

// anySet reports whether any agent has a value that is not blank.
func anySet(m map[agent.ID]string) bool {
	for _, v := range m {
		if strings.TrimSpace(v) != "" {
			return true
		}
	}

	return false
}

// Target is an org or repository the inbox has seen, offered when adding defaults.
type Target struct {
	// Owner is the org or user; Name is the repository, or "" for the owner.
	Owner string `json:"owner"`
	Name  string `json:"name"`

	// Items counts the inbox items listed for it now.
	Items int `json:"items"`
}

// ProfileLookup finds an agent's stored profile by name, or returns
// profiles.ErrProfileNotFound. *profiles.Store implements it.
type ProfileLookup interface {
	Lookup(ctx context.Context, id agent.ID, name string) (profiles.Profile, error)
}

// PromptLookup finds a stored prompt by ID, or returns prompts.ErrPromptNotFound.
// *prompts.Store implements it.
type PromptLookup interface {
	Lookup(ctx context.Context, id int64) (prompts.Prompt, error)
}

// RepoSource lists the repositories the inbox has seen, for choosing where to add
// defaults.
type RepoSource interface {
	// Repos returns every repository that is not archived, by owner and then name
	// ignoring case, each with how many of its items the inbox lists now.
	Repos(ctx context.Context) ([]Target, error)
}

// Config holds a Service's dependencies. Store, Profiles, and Prompts are required;
// Repos is needed only by Targets.
type Config struct {
	// Store holds the launch defaults.
	Store *Store

	// Profiles looks up the profiles a default may name.
	Profiles ProfileLookup

	// Prompts looks up the prompts a default may name.
	Prompts PromptLookup

	// Repos lists the repositories offered when adding defaults.
	Repos RepoSource

	// File is the copy of the launch defaults kept beside the database, written after
	// every change to them. Optional.
	File *File

	// Logger hears a copy that could not be written. Nil discards.
	Logger *slog.Logger
}

// Service manages the launch defaults.
type Service struct {
	store    *Store
	profiles ProfileLookup
	prompts  PromptLookup
	repos    RepoSource
	file     *File
	logger   *slog.Logger
}

// NewService returns a Service over cfg.
func NewService(cfg Config) *Service {
	return &Service{
		store: cfg.Store, profiles: cfg.Profiles, prompts: cfg.Prompts, repos: cfg.Repos, file: cfg.File,
		logger: logging.Or(cfg.Logger),
	}
}

// List returns every stored launch default, by owner and then repository, each owner's
// own defaults first.
func (s *Service) List(ctx context.Context) ([]Defaults, error) {
	return s.store.All(ctx)
}

// Check validates launch defaults being edited, as Save would, without saving them.
func (s *Service) Check(ctx context.Context, in Input) ([]agent.FieldError, error) {
	_, fields, err := validate(ctx, s.profiles, s.prompts, in.normalized())

	return textutil.NonNil(fields), err
}

// Save stores an org's or repository's launch defaults, replacing any it had. Defaults
// where every setting inherits are removed instead, since they would change nothing.
func (s *Service) Save(ctx context.Context, in Input) (Defaults, error) {
	in = in.normalized()

	checked, fields, err := validate(ctx, s.profiles, s.prompts, in)
	if err != nil {
		return Defaults{}, err
	}

	if len(fields) > 0 {
		return Defaults{}, &agent.ValidationError{Err: ErrInvalidDefaults, Fields: fields}
	}

	if in.Empty() {
		if err = s.store.remove(ctx, in.Owner, in.Name); err != nil && !errors.Is(err, ErrNotFound) {
			return Defaults{}, err
		}

		s.saved(ctx)

		return emptyDefaults(in.Owner, in.Name), nil
	}

	stored, err := s.store.save(ctx, in, checked)
	if err != nil {
		return Defaults{}, err
	}

	s.saved(ctx)

	return stored, nil
}

// Remove deletes an org's (name "") or a repository's launch defaults.
func (s *Service) Remove(ctx context.Context, owner, name string) error {
	if err := s.store.remove(ctx, strings.TrimSpace(owner), strings.TrimSpace(name)); err != nil {
		return err
	}

	s.saved(ctx)

	return nil
}

// Targets lists the owners and repositories the inbox has seen, each owner before its
// repositories, for choosing where to add defaults. Archived repositories are left out.
// An owner's Items adds up its repositories'.
func (s *Service) Targets(ctx context.Context) ([]Target, error) {
	repos, err := s.repos.Repos(ctx)
	if err != nil {
		return nil, fmt.Errorf("list the repositories: %w", err)
	}

	out := make([]Target, 0, len(repos))
	owner := -1

	for _, r := range repos {
		if owner < 0 || !strings.EqualFold(out[owner].Owner, r.Owner) {
			out = append(out, Target{Owner: r.Owner})
			owner = len(out) - 1
		}

		out[owner].Items += r.Items
		out = append(out, r)
	}

	return out, nil
}

// saved writes the copy of the launch defaults after a change to them, or to a prompt
// they name. A failure is logged, not returned: the change is stored, and the copy is
// written again after the next one. The change is made whether or not the caller still
// waits, so the copy is too.
func (s *Service) saved(ctx context.Context) {
	if s.file == nil {
		return
	}

	ctx = context.WithoutCancel(ctx)

	if err := s.file.Save(ctx, s.store, s.prompts); err != nil {
		s.logger.WarnContext(ctx, "could not save the copy of the launch defaults", "path", s.file.Path(), "error", err)
	}
}

// Index holds launch defaults by org and repository, matched ignoring case.
type Index map[string]Defaults

// NewIndex indexes launch defaults by their target.
func NewIndex(all []Defaults) Index {
	idx := make(Index, len(all))
	for _, d := range all {
		idx[repo.Name{Owner: d.Owner, Repo: d.Name}.Key()] = d
	}

	return idx
}

// For returns an org's defaults and a repository's own, each nil when none are stored.
func (idx Index) For(name repo.Name) (org, own *Defaults) {
	if d, ok := idx[repo.Name{Owner: name.Owner}.Key()]; ok {
		org = &d
	}

	if d, ok := idx[name.Key()]; ok && name.Repo != "" {
		own = &d
	}

	return org, own
}

// emptyDefaults returns the defaults of a target that sets nothing.
func emptyDefaults(owner, name string) Defaults {
	return Defaults{
		Owner: owner, Name: name, Profiles: map[agent.ID]string{}, Models: map[agent.ID]string{},
		ExtraArgs: map[agent.ID][]string{}, ExtraFlags: map[agent.ID]string{},
	}
}

// normalized trims every text field, and leaves out each agent's entry that is blank.
func (in Input) normalized() Input {
	in.Owner = strings.TrimSpace(in.Owner)
	in.Name = strings.TrimSpace(in.Name)
	in.Agent = agent.ID(strings.TrimSpace(string(in.Agent)))
	in.Profiles = trimmed(in.Profiles)
	in.Models = trimmed(in.Models)
	in.ExtraFlags = trimmed(in.ExtraFlags)

	return in
}

// trimmed returns m with each value trimmed and blank ones left out; never nil.
func trimmed(m map[agent.ID]string) map[agent.ID]string {
	out := make(map[agent.ID]string, len(m))

	for id, v := range m {
		if v = strings.TrimSpace(v); v != "" {
			out[agent.ID(strings.TrimSpace(string(id)))] = v
		}
	}

	return out
}

// validate checks an Input and returns its models and extra arguments ready to store, or
// the problems that stop it being saved. An error means the checks themselves could not
// run.
func validate(ctx context.Context, known ProfileLookup, library PromptLookup, in Input) (agent.Checked, []agent.FieldError, error) {
	var fields []agent.FieldError

	add := func(field, message string) {
		if message != "" {
			fields = append(fields, agent.FieldError{Field: field, Message: message})
		}
	}

	add(FieldOwner, checkOwner(in.Owner))
	add(FieldName, checkName(in.Name))
	add(FieldAgent, checkAgent(in.Agent))

	profileFields, err := checkProfiles(ctx, known, in.Profiles)
	if err != nil {
		return agent.Checked{}, nil, err
	}

	fields = append(fields, profileFields...)

	for _, p := range []struct {
		field string
		kind  item.Kind
		id    *int64
	}{{FieldPRPromptID, item.KindPR, in.PRPromptID}, {FieldIssuePromptID, item.KindIssue, in.IssuePromptID}} {
		msg, promptErr := checkPrompt(ctx, library, p.id, p.kind)
		if promptErr != nil {
			return agent.Checked{}, nil, promptErr
		}

		add(p.field, msg)
	}

	checked, settingFields := agent.CheckSettings(agent.Settings{
		Models: in.Models, Effort: in.Effort, ExtraFlags: in.ExtraFlags,
	})

	return checked, append(fields, settingFields...), nil
}

// checkOwner checks an org or user name.
func checkOwner(owner string) string {
	if !repo.ValidOwner(owner) {
		return "Choose an org or user, such as acme-org."
	}

	return ""
}

// checkName checks a repository name, which may be "" for an owner's defaults.
func checkName(name string) string {
	if name != "" && !repo.ValidRepo(name) {
		return "A repository name holds only letters, digits, and . _ -, can't start with -, and can't be . or .. alone."
	}

	return ""
}

// checkAgent checks the coding agent, which may be agent.Inherit.
func checkAgent(id agent.ID) string {
	if id != agent.Inherit && !agent.Known(id) {
		return fmt.Sprintf("There's no coding agent called %q.", id)
	}

	return ""
}

// checkProfiles checks each agent's profile, in the agents' order.
func checkProfiles(ctx context.Context, known ProfileLookup, names map[agent.ID]string) ([]agent.FieldError, error) {
	var fields []agent.FieldError

	for _, id := range slices.Sorted(maps.Keys(names)) {
		a, err := agent.Lookup(id)
		if err != nil || id == agent.Inherit {
			fields = append(fields, agent.FieldError{
				Field: FieldProfiles, Message: fmt.Sprintf("There's no coding agent called %q.", id),
			})

			continue
		}

		_, err = known.Lookup(ctx, id, names[id])

		switch {
		case errors.Is(err, profiles.ErrProfileNotFound):
			fields = append(fields, agent.FieldError{
				Field:   agent.FieldFor(FieldProfiles, id),
				Message: fmt.Sprintf("There's no %s profile called %q.", a.Name, names[id]),
			})
		case err != nil:
			return nil, err
		}
	}

	return fields, nil
}

// checkPrompt checks a default prompt for kind, which may be nil to inherit.
func checkPrompt(ctx context.Context, library PromptLookup, id *int64, kind item.Kind) (string, error) {
	if id == nil {
		return "", nil
	}

	p, err := library.Lookup(ctx, *id)

	switch {
	case errors.Is(err, prompts.ErrPromptNotFound):
		return "That prompt no longer exists; choose another.", nil
	case err != nil:
		return "", err
	case p.IsReply():
		return fmt.Sprintf("%q is a review reply, not a prompt an agent can launch with.", p.Nickname), nil
	case !p.AppliesTo.Covers(kind):
		return fmt.Sprintf("%q isn't a prompt for %s.", p.Nickname, kind.Plural()), nil
	default:
		return "", nil
	}
}

// target names an org ("acme-org") or a repository ("acme-org/example-repo").
func target(owner, name string) string {
	if name == "" {
		return owner
	}

	return repo.Name{Owner: owner, Repo: name}.String()
}
