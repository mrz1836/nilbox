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
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launchdefaults/model"
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

// targetsSQL lists the owners and repositories the inbox has seen, with how many of
// their items are listed now.
const targetsSQL = `SELECT r.owner, r.name, COUNT(i.id) AS items
FROM repos r
LEFT JOIN items i ON i.repo_id = r.id
  AND (i.reason_review_requested = 1 OR i.reason_authored = 1 OR i.reason_assigned = 1)
WHERE r.is_archived = 0
GROUP BY r.id
ORDER BY r.owner COLLATE NOCASE, r.name COLLATE NOCASE`

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

// Service manages the launch defaults.
type Service struct {
	db       *gorm.DB
	profiles ProfileLookup
}

// NewService returns a Service over db. known looks up the profiles a default may name.
func NewService(db *gorm.DB, known ProfileLookup) *Service {
	return &Service{db: db, profiles: known}
}

// List returns every stored launch default, by owner and then repository, each owner's
// own defaults first.
func (s *Service) List(ctx context.Context) ([]Defaults, error) {
	return All(ctx, s.db)
}

// Check validates launch defaults being edited, as Save would, without saving them.
func (s *Service) Check(ctx context.Context, in Input) ([]agent.FieldError, error) {
	_, fields, err := validate(ctx, s.db, s.profiles, in.normalized())

	return textutil.NonNil(fields), err
}

// Save stores an org's or repository's launch defaults, replacing any it had. Defaults
// where every setting inherits are removed instead, since they would change nothing.
func (s *Service) Save(ctx context.Context, in Input) (Defaults, error) {
	in = in.normalized()
	db := s.db.WithContext(ctx)

	args, fields, err := validate(ctx, db, s.profiles, in)
	if err != nil {
		return Defaults{}, err
	}

	if len(fields) > 0 {
		return Defaults{}, &agent.ValidationError{Err: ErrInvalidDefaults, Fields: fields}
	}

	if in.Empty() {
		if err = remove(db, in.Owner, in.Name); err != nil && !errors.Is(err, ErrNotFound) {
			return Defaults{}, err
		}

		return Defaults{Owner: in.Owner, Name: in.Name, ExtraArgs: []string{}}, nil
	}

	row := toRow(in, args)

	err = db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "owner"}, {Name: "name"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"agent", "profile", "pr_prompt_id", "issue_prompt_id", "model", "effort", "plan_mode", "worktree",
			"extra_args", "updated_at",
		}),
	}).Create(&row).Error
	if err != nil {
		return Defaults{}, fmt.Errorf("save the launch defaults for %s: %w", target(in.Owner, in.Name), err)
	}

	return lookup(db, in.Owner, in.Name)
}

// Remove deletes an org's (name "") or a repository's launch defaults.
func (s *Service) Remove(ctx context.Context, owner, name string) error {
	return remove(s.db.WithContext(ctx), strings.TrimSpace(owner), strings.TrimSpace(name))
}

// Targets lists the owners and repositories the inbox has seen, each owner before its
// repositories, for choosing where to add defaults. Archived repositories are left out.
// An owner's Items adds up its repositories'.
func (s *Service) Targets(ctx context.Context) ([]Target, error) {
	var repos []Target
	if err := s.db.WithContext(ctx).Raw(targetsSQL).Scan(&repos).Error; err != nil {
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

// All returns every stored launch default, by owner and then repository, each owner's
// own defaults first.
func All(ctx context.Context, db *gorm.DB) ([]Defaults, error) {
	var rows []model.LaunchDefault

	err := db.WithContext(ctx).Order("owner COLLATE NOCASE").Order("name COLLATE NOCASE").Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("read the launch defaults: %w", err)
	}

	out := make([]Defaults, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromRow(row))
	}

	return out, nil
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
func validate(ctx context.Context, db *gorm.DB, known ProfileLookup, in Input) ([]string, []agent.FieldError, error) {
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
		msg, promptErr := checkPrompt(ctx, db, p.id, p.kind)
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
func checkPrompt(ctx context.Context, db *gorm.DB, id *int64, kind item.Kind) (string, error) {
	if id == nil {
		return "", nil
	}

	p, err := prompts.Lookup(ctx, db, *id)

	switch {
	case errors.Is(err, prompts.ErrPromptNotFound):
		return "That prompt no longer exists; choose another.", nil
	case err != nil:
		return "", err
	case !prompts.AppliesTo(p.AppliesTo, kind):
		return fmt.Sprintf("%q isn't a prompt for %s.", p.Nickname, kind.Plural()), nil
	default:
		return "", nil
	}
}

// lookup reads one stored row as Defaults.
func lookup(db *gorm.DB, owner, name string) (Defaults, error) {
	var rows []model.LaunchDefault
	if err := db.Where("owner = ? AND name = ?", owner, name).Limit(1).Find(&rows).Error; err != nil {
		return Defaults{}, fmt.Errorf("read the launch defaults for %s: %w", target(owner, name), err)
	}

	if len(rows) == 0 {
		return Defaults{}, fmt.Errorf("%w: %s", ErrNotFound, target(owner, name))
	}

	return fromRow(rows[0]), nil
}

// remove deletes one stored row.
func remove(db *gorm.DB, owner, name string) error {
	res := db.Where("owner = ? AND name = ?", owner, name).Delete(&model.LaunchDefault{})
	if res.Error != nil {
		return fmt.Errorf("remove the launch defaults for %s: %w", target(owner, name), res.Error)
	}

	if res.RowsAffected == 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, target(owner, name))
	}

	return nil
}

// target names an org ("acme-org") or a repository ("acme-org/example-repo").
func target(owner, name string) string {
	if name == "" {
		return owner
	}

	return repo.Name{Owner: owner, Repo: name}.String()
}

// toRow converts a validated Input and its extra arguments to a row.
func toRow(in Input, args []string) model.LaunchDefault {
	now := time.Now().UTC()

	return model.LaunchDefault{
		Owner: in.Owner, Name: in.Name, Agent: in.Agent, Profile: in.Profile,
		PRPromptID: in.PRPromptID, IssuePromptID: in.IssuePromptID, Model: in.Model, Effort: in.Effort,
		PlanMode: in.PlanMode, Worktree: in.Worktree, ExtraArgs: append([]string{}, args...),
		CreatedAt: now, UpdatedAt: now,
	}
}

// fromRow converts a row to Defaults. A nil list becomes an empty one.
func fromRow(row model.LaunchDefault) Defaults {
	args := slices.Clone([]string(row.ExtraArgs))
	if args == nil {
		args = []string{}
	}

	return Defaults{
		Owner: row.Owner, Name: row.Name, Agent: row.Agent, Profile: row.Profile,
		PRPromptID: row.PRPromptID, IssuePromptID: row.IssuePromptID, Model: row.Model, Effort: row.Effort,
		PlanMode: row.PlanMode, Worktree: row.Worktree, ExtraArgs: args, ExtraFlags: agent.Join(args),
		UpdatedAt: clock.Format(row.UpdatedAt),
	}
}
