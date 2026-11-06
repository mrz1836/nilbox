// Package prompts is the prompt library: the prompts a launch can fill in for a pull
// request or issue, with the launch settings each one sets, and the defaults for each
// kind of item.
//
// A prompt is a Go text/template over Data. Parse checks one the way a launch runs it,
// and explains each problem with its line and column. The app adds the starter prompts
// once each (see Seed), and from then on every prompt is the user's to change or delete,
// except while it is a default somewhere: Delete refuses a prompt that is in use, and
// Update refuses to stop it applying to a kind it is a default for.
package prompts

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/mrz1836/nilbox/internal/agentflags"
	"github.com/mrz1836/nilbox/internal/prompts/model"
)

// Limits on a prompt's names.
const (
	maxNameRunes     = 80
	maxNicknameRunes = 20
)

// Fields a FieldError can name, matching Input's JSON names.
const (
	FieldName       = "name"
	FieldNickname   = "nickname"
	FieldAppliesTo  = "appliesTo"
	FieldBody       = "body"
	FieldModel      = "model"
	FieldEffort     = "effort"
	FieldExtraFlags = "extraFlags"
)

var (
	// ErrPromptNotFound is returned for a prompt ID that is not stored.
	ErrPromptNotFound = errors.New("prompt not found")

	// ErrInvalidPrompt is returned when a prompt cannot be saved (see ValidationError).
	ErrInvalidPrompt = errors.New("the prompt can't be saved")

	// ErrPromptInUse is returned when deleting a prompt that is a default (see InUseError).
	ErrPromptInUse = errors.New("the prompt is in use")

	// ErrNotStarter is returned when resetting a prompt that did not come from a starter
	// this build ships.
	ErrNotStarter = errors.New("the prompt has no original to reset to")

	// ErrInvalidKind is returned for an item kind other than "pr" or "issue".
	ErrInvalidKind = errors.New(`kind must be "pr" or "issue"`)

	// ErrWrongKind is returned when a prompt is made the default for a kind it does not
	// apply to.
	ErrWrongKind = errors.New("the prompt doesn't apply to that kind of item")
)

// Prompt is one prompt as the UI shows it. Every field is a plain value.
type Prompt struct {
	ID int64 `json:"id"`

	// Name is the prompt's full title.
	Name string `json:"name"`

	// Nickname is the short label the launch menu shows.
	Nickname string `json:"nickname"`

	// AppliesTo is "pr", "issue", or "any".
	AppliesTo string `json:"appliesTo"`

	// Body is the Go text/template.
	Body string `json:"body"`

	// Model and Effort set --model and --effort; "" inherits.
	Model  string `json:"model"`
	Effort string `json:"effort"`

	// PlanMode and Worktree turn plan mode and the worktree on or off; nil inherits.
	PlanMode *bool `json:"planMode"`
	Worktree *bool `json:"worktree"`

	// ExtraArgs are added after the profile's and the launch defaults' arguments.
	ExtraArgs []string `json:"extraArgs"`

	// ExtraFlags is ExtraArgs on one line, quoted the way the editor shows them.
	ExtraFlags string `json:"extraFlags"`

	// StarterKey names the starter the prompt came from, or "".
	StarterKey string `json:"starterKey"`

	// Modified reports whether a starter's text or settings were changed since it was
	// added, so Reset would change it.
	Modified bool `json:"modified"`

	// Usage says where the prompt is a default.
	Usage Usage `json:"usage"`

	// UpdatedAt is when the prompt was last saved, as RFC 3339.
	UpdatedAt string `json:"updatedAt"`
}

// Usage says where a prompt is a default. A prompt in use cannot be deleted.
type Usage struct {
	// Defaults lists the kinds ("pr", "issue") the prompt is the app's default for.
	Defaults []string `json:"defaults"`

	// Orgs and Repos list the owners and "owner/name" repositories whose launch
	// defaults use it, sorted.
	Orgs  []string `json:"orgs"`
	Repos []string `json:"repos"`

	// Kinds lists every kind the prompt is a default for, anywhere.
	Kinds []string `json:"kinds"`
}

// InUse reports whether the prompt is a default anywhere.
func (u Usage) InUse() bool {
	return len(u.Defaults)+len(u.Orgs)+len(u.Repos) > 0
}

// Input is a prompt as the editor saves it.
type Input struct {
	Name      string `json:"name"`
	Nickname  string `json:"nickname"`
	AppliesTo string `json:"appliesTo"`
	Body      string `json:"body"`
	Model     string `json:"model"`
	Effort    string `json:"effort"`
	PlanMode  *bool  `json:"planMode"`
	Worktree  *bool  `json:"worktree"`

	// ExtraFlags are the extra arguments typed on one line, quoted as in a shell.
	ExtraFlags string `json:"extraFlags"`
}

// FieldError is a problem with one field of an Input.
type FieldError struct {
	// Field is the Input field's JSON name, such as "nickname".
	Field string `json:"field"`

	// Message is a sentence saying what is wrong and how to fix it.
	Message string `json:"message"`
}

// ValidationError lists why a prompt cannot be saved. It matches ErrInvalidPrompt.
type ValidationError struct {
	Fields []FieldError
}

// Error joins the field messages.
func (e *ValidationError) Error() string {
	messages := make([]string, 0, len(e.Fields))
	for _, f := range e.Fields {
		messages = append(messages, f.Message)
	}

	return "the prompt can't be saved: " + strings.Join(messages, " ")
}

// Is makes a ValidationError match ErrInvalidPrompt.
func (e *ValidationError) Is(target error) bool {
	return target == ErrInvalidPrompt
}

// InUseError is returned when deleting a prompt that is a default. It matches
// ErrPromptInUse.
type InUseError struct {
	Nickname string
	Usage    Usage
}

// Error says where the prompt is in use.
func (e *InUseError) Error() string {
	return fmt.Sprintf("%q can't be deleted while it's %s; choose other defaults there first",
		e.Nickname, DescribeUsage(e.Usage))
}

// Is makes an InUseError match ErrPromptInUse.
func (e *InUseError) Is(target error) bool {
	return target == ErrPromptInUse
}

// DefaultPrompts are the app's default prompt for each kind of item; 0 when no prompt
// applies to that kind.
type DefaultPrompts struct {
	PR    int64 `json:"pr"`
	Issue int64 `json:"issue"`
}

// For returns the default for kind.
func (d DefaultPrompts) For(kind string) int64 {
	if kind == KindPR {
		return d.PR
	}

	return d.Issue
}

// PreviewSource fills in Data for an inbox item. The launcher implements it.
type PreviewSource interface {
	// PreviewData returns the data a launch on the item would fill a prompt with,
	// leaving the session facts for the caller to set.
	PreviewData(ctx context.Context, itemID int64) (Data, error)
}

// Config holds a Service's dependencies. Source is optional: without it, previews use
// sample data.
type Config struct {
	DB     *gorm.DB
	Source PreviewSource
}

// Service manages the prompt library.
type Service struct {
	db     *gorm.DB
	source PreviewSource
}

// NewService returns a Service over cfg.
func NewService(cfg Config) *Service {
	return &Service{db: cfg.DB, source: cfg.Source}
}

// List returns every prompt with its usage, sorted by nickname.
func (s *Service) List(ctx context.Context) ([]Prompt, error) {
	return listWithUsage(ctx, s.db)
}

// Create saves a new prompt.
func (s *Service) Create(ctx context.Context, in Input) (Prompt, error) {
	in = in.normalized()

	var id int64

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		args, fields := validate(tx, 0, in, Usage{})
		if len(fields) > 0 {
			return &ValidationError{Fields: fields}
		}

		row := toRow(in, args)
		if err := tx.Create(&row).Error; err != nil {
			return fmt.Errorf("save the prompt: %w", err)
		}

		id = row.ID

		return nil
	})
	if err != nil {
		return Prompt{}, err
	}

	return s.get(ctx, id)
}

// Update saves changes to a prompt. A prompt that is a default for a kind must keep
// applying to that kind.
func (s *Service) Update(ctx context.Context, id int64, in Input) (Prompt, error) {
	in = in.normalized()

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, err := loadRow(tx, id)
		if err != nil {
			return err
		}

		usage, err := usageOf(tx, id)
		if err != nil {
			return err
		}

		args, fields := validate(tx, id, in, usage)
		if len(fields) > 0 {
			return &ValidationError{Fields: fields}
		}

		updated := toRow(in, args)
		updated.ID, updated.StarterKey, updated.CreatedAt = row.ID, row.StarterKey, row.CreatedAt

		if err = tx.Save(&updated).Error; err != nil {
			return fmt.Errorf("save the prompt: %w", err)
		}

		return nil
	})
	if err != nil {
		return Prompt{}, err
	}

	return s.get(ctx, id)
}

// Delete removes a prompt. A prompt that is a default anywhere is refused with an
// InUseError that says where.
func (s *Service) Delete(ctx context.Context, id int64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, err := loadRow(tx, id)
		if err != nil {
			return err
		}

		usage, err := usageOf(tx, id)
		if err != nil {
			return err
		}

		if usage.InUse() {
			return &InUseError{Nickname: row.Nickname, Usage: usage}
		}

		if err = tx.Delete(&row).Error; err != nil {
			return fmt.Errorf("delete the prompt: %w", err)
		}

		return nil
	})
}

// Duplicate saves a copy of a prompt: "<title> (copy)" with a free nickname such as
// "<nickname> copy". The copy is the user's own, not a starter.
func (s *Service) Duplicate(ctx context.Context, id int64) (Prompt, error) {
	var newID int64

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, err := loadRow(tx, id)
		if err != nil {
			return err
		}

		nickname, err := freeNickname(tx, row.Nickname+" copy")
		if err != nil {
			return err
		}

		dup := row
		dup.ID, dup.StarterKey, dup.Nickname = 0, "", nickname
		dup.Name = truncateRunes(row.Name+" (copy)", maxNameRunes)
		dup.CreatedAt, dup.UpdatedAt = time.Time{}, time.Time{}

		if err = tx.Create(&dup).Error; err != nil {
			return fmt.Errorf("save the copy: %w", err)
		}

		newID = dup.ID

		return nil
	})
	if err != nil {
		return Prompt{}, err
	}

	return s.get(ctx, newID)
}

// Reset restores a starter prompt's title, text, and settings, and its nickname when
// no other prompt has taken it.
func (s *Service) Reset(ctx context.Context, id int64) (Prompt, error) {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return resetRow(tx, id)
	})
	if err != nil {
		return Prompt{}, err
	}

	return s.get(ctx, id)
}

// resetRow restores one starter prompt's row.
func resetRow(tx *gorm.DB, id int64) error {
	row, err := loadRow(tx, id)
	if err != nil {
		return err
	}

	starter, ok := starterFor(row.StarterKey)
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotStarter, row.Nickname)
	}

	in := starter.input()
	if taken, takenErr := nicknameTaken(tx, id, in.Nickname); takenErr != nil || taken {
		in.Nickname = row.Nickname
	}

	usage, err := usageOf(tx, id)
	if err != nil {
		return err
	}

	args, fields := validate(tx, id, in, usage)
	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}

	reset := toRow(in, args)
	reset.ID, reset.StarterKey, reset.CreatedAt = row.ID, row.StarterKey, row.CreatedAt

	if err = tx.Save(&reset).Error; err != nil {
		return fmt.Errorf("reset the prompt: %w", err)
	}

	return nil
}

// Defaults returns the app's default prompt for each kind of item.
func (s *Service) Defaults(ctx context.Context) (DefaultPrompts, error) {
	return LoadDefaults(ctx, s.db)
}

// SetDefault makes a prompt the app's default for kind ("pr" or "issue"). The prompt
// must apply to that kind.
func (s *Service) SetDefault(ctx context.Context, kind string, id int64) error {
	key, err := defaultPrefKey(kind)
	if err != nil {
		return err
	}

	row, err := loadRow(s.db.WithContext(ctx), id)
	if err != nil {
		return err
	}

	if !AppliesTo(row.AppliesTo, kind) {
		return fmt.Errorf("%w: %q doesn't apply to %s", ErrWrongKind, row.Nickname, plural(kind))
	}

	return setPref(s.db.WithContext(ctx), key, fmt.Sprint(id))
}

// Variables returns every variable a prompt can use, for the cheat sheet.
func (s *Service) Variables() []Variable {
	return Variables()
}

// get returns one prompt with its usage.
func (s *Service) get(ctx context.Context, id int64) (Prompt, error) {
	all, err := listWithUsage(ctx, s.db)
	if err != nil {
		return Prompt{}, err
	}

	for _, p := range all {
		if p.ID == id {
			return p, nil
		}
	}

	return Prompt{}, fmt.Errorf("%w (id %d)", ErrPromptNotFound, id)
}

// normalized trims the names and model, and turns Windows line endings into newlines.
func (in Input) normalized() Input {
	in.Name = strings.TrimSpace(in.Name)
	in.Nickname = strings.TrimSpace(in.Nickname)
	in.Model = strings.TrimSpace(in.Model)
	in.Body = strings.ReplaceAll(in.Body, "\r\n", "\n")

	return in
}

// input returns the starter as an Input.
func (st Starter) input() Input {
	return Input{
		Name: st.Name, Nickname: st.Nickname, AppliesTo: st.AppliesTo, Body: st.Body,
		Model: st.Model, Effort: st.Effort, PlanMode: st.PlanMode, Worktree: st.Worktree,
		ExtraFlags: agentflags.Join(st.ExtraArgs),
	}
}

// validate checks an Input for the prompt with id (0 for a new one) and returns its
// extra arguments, or the problems that stop it being saved.
func validate(db *gorm.DB, id int64, in Input, usage Usage) ([]string, []FieldError) {
	args, fields := validateFields(db, id, in, usage)

	if t, _ := Parse(in.Body, appliesToOrAny(in.AppliesTo)); t == nil {
		fields = append(fields, FieldError{Field: FieldBody, Message: "Fix the problems in the prompt's text."})
	}

	return args, fields
}

// validateFields checks every field but the text, which Parse checks.
func validateFields(db *gorm.DB, id int64, in Input, usage Usage) ([]string, []FieldError) {
	var fields []FieldError

	add := func(field, message string) {
		if message != "" {
			fields = append(fields, FieldError{Field: field, Message: message})
		}
	}

	add(FieldName, checkName(in.Name))
	add(FieldNickname, checkNickname(db, id, in.Nickname))
	add(FieldAppliesTo, checkAppliesTo(in.AppliesTo, usage))

	if err := agentflags.ValidateModel(in.Model); err != nil {
		add(FieldModel, `A model is one word, such as "opus" or "sonnet", that doesn't start with "-".`)
	}

	if err := agentflags.ValidateEffort(in.Effort); err != nil {
		add(FieldEffort, "Choose an effort level from the list.")
	}

	args, message := checkFlags(in.ExtraFlags)
	add(FieldExtraFlags, message)

	return args, fields
}

// checkName checks a prompt's title.
func checkName(name string) string {
	switch {
	case name == "":
		return "Give the prompt a title."
	case utf8.RuneCountInString(name) > maxNameRunes:
		return fmt.Sprintf("Keep the title to %d characters.", maxNameRunes)
	case strings.ContainsFunc(name, unicode.IsControl):
		return "Keep the title on one line."
	default:
		return ""
	}
}

// checkNickname checks a prompt's nickname: short, one line, and not another prompt's.
func checkNickname(db *gorm.DB, id int64, nickname string) string {
	switch {
	case nickname == "":
		return "Give the prompt a nickname for the launch menu."
	case utf8.RuneCountInString(nickname) > maxNicknameRunes:
		return fmt.Sprintf("Keep the nickname to %d characters so it fits the launch button.", maxNicknameRunes)
	case strings.ContainsFunc(nickname, unicode.IsControl):
		return "Keep the nickname on one line."
	}

	taken, err := nicknameTaken(db, id, nickname)
	if err != nil {
		return "Couldn't check whether the nickname is free."
	}

	if taken {
		return fmt.Sprintf("Another prompt is already called %q.", nickname)
	}

	return ""
}

// checkAppliesTo checks what a prompt applies to: one of the three choices, covering
// every kind it is a default for.
func checkAppliesTo(appliesTo string, usage Usage) string {
	switch appliesTo {
	case AppliesToPR, AppliesToIssue, AppliesToAny:
	default:
		return "Choose what the prompt is for: pull requests, issues, or both."
	}

	for _, kind := range usage.Kinds {
		if !AppliesTo(appliesTo, kind) {
			return fmt.Sprintf("It's a default for %s (%s), so it has to apply to them.",
				plural(kind), DescribeUsage(usage))
		}
	}

	return ""
}

// checkFlags reads extra flags typed on one line and checks them.
func checkFlags(line string) ([]string, string) {
	args, err := agentflags.Split(line)
	if err != nil {
		return nil, "A quote in the flags is never closed."
	}

	if err = agentflags.CheckExtra(args); err != nil {
		if errors.Is(err, agentflags.ErrEmptyFlag) {
			return nil, "Remove the empty flag ('')."
		}

		return nil, sentence(err.Error())
	}

	return args, ""
}

// appliesToOrAny returns appliesTo when it is valid, else "any", so a prompt's text can
// be checked while its kind is still being chosen.
func appliesToOrAny(appliesTo string) string {
	if appliesTo == AppliesToPR || appliesTo == AppliesToIssue {
		return appliesTo
	}

	return AppliesToAny
}

// DescribeUsage says where a prompt is a default, as in "the default for pull requests,
// and a default for acme-org and 2 repositories".
func DescribeUsage(u Usage) string {
	var parts []string

	for _, kind := range u.Defaults {
		parts = append(parts, "the default for "+plural(kind))
	}

	var places []string

	if len(u.Orgs) > 0 {
		places = append(places, countedNames(u.Orgs, "orgs"))
	}

	if len(u.Repos) > 0 {
		places = append(places, countedNames(u.Repos, "repositories"))
	}

	if len(places) > 0 {
		parts = append(parts, "a default for "+strings.Join(places, " and "))
	}

	if len(parts) == 0 {
		return "not used anywhere"
	}

	return strings.Join(parts, ", and ")
}

// countedNames names one item, or counts several: "acme-org" or "3 orgs".
func countedNames(names []string, many string) string {
	if len(names) == 1 {
		return names[0]
	}

	return fmt.Sprintf("%d %s", len(names), many)
}

// toRow converts a validated Input and its extra arguments to a row.
func toRow(in Input, args []string) model.Prompt {
	return model.Prompt{
		Name: in.Name, Nickname: in.Nickname, AppliesTo: in.AppliesTo, Body: in.Body,
		Model: in.Model, Effort: in.Effort, PlanMode: in.PlanMode, Worktree: in.Worktree,
		ExtraArgs: append([]string{}, args...),
	}
}
