// Package launchdefaults stores launch defaults for orgs and repositories: the coding
// tool, profile, default prompt for each kind of item, model, effort, plan mode,
// worktree, and extra flags that launches on their items start from.
//
// An owner's defaults (Name "") apply to every repository of that owner, and a
// repository's own defaults win over them. Every setting inherits when it is "" or nil,
// so a row only holds what differs. The launcher resolves the full order: the app's
// settings, then the owner's defaults, the repository's, the chosen prompt's, and last
// the launch menu's.
package launchdefaults

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// Fields an agent.FieldError can name, matching Input's JSON names.
const (
	FieldOwner         = "owner"
	FieldName          = "name"
	FieldAgent         = "agent"
	FieldProfile       = "profile"
	FieldPRPromptID    = "prPromptId"
	FieldIssuePromptID = "issuePromptId"
	FieldModel         = agent.FieldModel
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

	// Agent is the coding tool's ID; "" inherits (the app's tool, Claude).
	Agent string `json:"agent"`

	// Profile names the profile; "" inherits.
	Profile string `json:"profile"`

	// PRPromptID and IssuePromptID are the default prompts; nil inherits.
	PRPromptID    *int64 `json:"prPromptId"`
	IssuePromptID *int64 `json:"issuePromptId"`

	// Model and Effort set --model and --effort; "" inherits.
	Model  string `json:"model"`
	Effort string `json:"effort"`

	// PlanMode and Worktree turn plan mode and the worktree on or off; nil inherits.
	PlanMode *bool `json:"planMode"`
	Worktree *bool `json:"worktree"`

	// ExtraArgs are added after the profile's arguments (an owner's before its
	// repository's), and ExtraFlags is them on one line, quoted.
	ExtraArgs  []string `json:"extraArgs"`
	ExtraFlags string   `json:"extraFlags"`

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
	Owner         string `json:"owner"`
	Name          string `json:"name"`
	Agent         string `json:"agent"`
	Profile       string `json:"profile"`
	PRPromptID    *int64 `json:"prPromptId"`
	IssuePromptID *int64 `json:"issuePromptId"`
	Model         string `json:"model"`
	Effort        string `json:"effort"`
	PlanMode      *bool  `json:"planMode"`
	Worktree      *bool  `json:"worktree"`

	// ExtraFlags are the extra arguments typed on one line, quoted as in a shell.
	ExtraFlags string `json:"extraFlags"`
}

// Empty reports whether every setting inherits.
func (in Input) Empty() bool {
	return in.Agent == "" && in.Profile == "" && in.PRPromptID == nil && in.IssuePromptID == nil &&
		in.Model == "" && in.Effort == "" && in.PlanMode == nil && in.Worktree == nil &&
		strings.TrimSpace(in.ExtraFlags) == ""
}

// Target is an org or repository the inbox has seen, offered when adding defaults.
type Target struct {
	// Owner is the org or user; Name is the repository, or "" for the owner.
	Owner string `json:"owner"`
	Name  string `json:"name"`

	// Items counts the inbox items listed for it now.
	Items int `json:"items"`
}

// ProfileLookup finds a stored profile by name, or returns profiles.ErrProfileNotFound.
// *profiles.Store implements it.
type ProfileLookup interface {
	Lookup(ctx context.Context, name string) (profiles.Profile, error)
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

	args, fields, err := validate(ctx, s.profiles, s.prompts, in)
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

		return Defaults{Owner: in.Owner, Name: in.Name, ExtraArgs: []string{}}, nil
	}

	stored, err := s.store.save(ctx, in, args)
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

// normalized trims every text field.
func (in Input) normalized() Input {
	in.Owner = strings.TrimSpace(in.Owner)
	in.Name = strings.TrimSpace(in.Name)
	in.Agent = strings.TrimSpace(in.Agent)
	in.Profile = strings.TrimSpace(in.Profile)
	in.Model = strings.TrimSpace(in.Model)

	return in
}

// validate checks an Input and returns its extra arguments, or the problems that stop it
// being saved. An error means the checks themselves could not run.
func validate(ctx context.Context, known ProfileLookup, library PromptLookup, in Input) ([]string, []agent.FieldError, error) {
	var fields []agent.FieldError

	add := func(field, message string) {
		if message != "" {
			fields = append(fields, agent.FieldError{Field: field, Message: message})
		}
	}

	add(FieldOwner, checkOwner(in.Owner))
	add(FieldName, checkName(in.Name))
	add(FieldAgent, checkAgent(in.Agent))

	profileMsg, err := checkProfile(ctx, known, in.Profile)
	if err != nil {
		return nil, nil, err
	}

	add(FieldProfile, profileMsg)

	for _, p := range []struct {
		field string
		kind  item.Kind
		id    *int64
	}{{FieldPRPromptID, item.KindPR, in.PRPromptID}, {FieldIssuePromptID, item.KindIssue, in.IssuePromptID}} {
		msg, promptErr := checkPrompt(ctx, library, p.id, p.kind)
		if promptErr != nil {
			return nil, nil, promptErr
		}

		add(p.field, msg)
	}

	args, flagFields := agent.CheckOverrides(in.Model, in.Effort, in.ExtraFlags)

	return args, append(fields, flagFields...), nil
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

// checkAgent checks the coding tool, which may be "" to inherit.
func checkAgent(id string) string {
	if _, err := agent.Lookup(id); err != nil {
		return fmt.Sprintf("There's no coding tool called %q.", id)
	}

	return ""
}

// checkProfile checks the profile, which may be "" to inherit.
func checkProfile(ctx context.Context, known ProfileLookup, profile string) (string, error) {
	if profile == "" {
		return "", nil
	}

	_, err := known.Lookup(ctx, profile)

	switch {
	case errors.Is(err, profiles.ErrProfileNotFound):
		return fmt.Sprintf("There's no profile called %q.", profile), nil
	case err != nil:
		return "", err
	default:
		return "", nil
	}
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
