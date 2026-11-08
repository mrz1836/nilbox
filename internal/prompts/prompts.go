// Package prompts is the prompt library: the prompts a launch can fill in for a pull
// request or issue, with the launch settings each one sets, and the defaults for each
// kind of item.
//
// A prompt is a Go text/template over Data. Parse checks one the way a launch runs it,
// and explains each problem with its line and column. The app adds the starter prompts
// once each (see Seed), and from then on every prompt is the user's to change or delete,
// except while it is a default somewhere: Delete refuses a prompt that is in use, and
// Update refuses to stop it applying to a kind it is a default for.
//
// A Store reads and writes the library; the launcher and the launch defaults read it
// through interfaces of their own. The Service the window binds to checks prompts before
// they are saved, fills them in for previews, and says where each one is a default, which
// the launch defaults report through a UsageSource.
package prompts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// Limits on a prompt's names.
const (
	maxNameRunes     = 80
	maxNicknameRunes = 20
)

// Fields an agent.FieldError can name, matching Input's JSON names.
const (
	FieldName       = "name"
	FieldNickname   = "nickname"
	FieldAppliesTo  = "appliesTo"
	FieldBody       = "body"
	FieldModel      = agent.FieldModel
	FieldEffort     = agent.FieldEffort
	FieldExtraFlags = agent.FieldExtraFlags
)

var (
	// ErrPromptNotFound is returned for a prompt ID that is not stored.
	ErrPromptNotFound = errors.New("prompt not found")

	// ErrInvalidPrompt is returned when a prompt cannot be saved (see
	// agent.ValidationError).
	ErrInvalidPrompt = errors.New("the prompt can't be saved")

	// ErrPromptInUse is returned when deleting a prompt that is a default (see InUseError).
	ErrPromptInUse = errors.New("the prompt is in use")

	// ErrNotStarter is returned when resetting a prompt that did not come from a starter
	// this build ships.
	ErrNotStarter = errors.New("the prompt has no original to reset to")

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

	// AppliesTo is AppliesToPR, AppliesToIssue, or AppliesToAny.
	AppliesTo AppliesTo `json:"appliesTo"`

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

	// CreatedAt is when the prompt was added to this library, and UpdatedAt when it was
	// last saved, both as RFC 3339.
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// Usage says where a prompt is a default. A prompt in use cannot be deleted.
type Usage struct {
	// Defaults lists the kinds the prompt is the app's default for.
	Defaults []item.Kind `json:"defaults"`

	// Orgs and Repos list the owners and "owner/name" repositories whose launch
	// defaults use it, sorted.
	Orgs  []string `json:"orgs"`
	Repos []string `json:"repos"`

	// Kinds lists every kind the prompt is a default for, anywhere.
	Kinds []item.Kind `json:"kinds"`
}

// InUse reports whether the prompt is a default anywhere.
func (u Usage) InUse() bool {
	return len(u.Defaults)+len(u.Orgs)+len(u.Repos) > 0
}

// Input is a prompt as the editor saves it.
type Input struct {
	Name      string    `json:"name"`
	Nickname  string    `json:"nickname"`
	AppliesTo AppliesTo `json:"appliesTo"`
	Body      string    `json:"body"`
	Model     string    `json:"model"`
	Effort    string    `json:"effort"`
	PlanMode  *bool     `json:"planMode"`
	Worktree  *bool     `json:"worktree"`

	// ExtraFlags are the extra arguments typed on one line, quoted as in a shell.
	ExtraFlags string `json:"extraFlags"`
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
		e.Nickname, describeUsage(e.Usage))
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
func (d DefaultPrompts) For(kind item.Kind) int64 {
	if kind == item.KindPR {
		return d.PR
	}

	return d.Issue
}

// Template parses the prompt's text, failing when it has errors.
func (p Prompt) Template() (*Template, error) {
	t, diags := Parse(p.Body, p.AppliesTo)
	if t != nil {
		return t, nil
	}

	for _, d := range diags {
		if d.Severity == SeverityError {
			return nil, fmt.Errorf("%w: %q: %s", ErrInvalidTemplate, p.Nickname, d.Message)
		}
	}

	return nil, fmt.Errorf("%w: %q", ErrInvalidTemplate, p.Nickname)
}

// Library is every stored prompt, without its usage and sorted by nickname, with the
// app's default prompt for each kind of item.
type Library struct {
	Prompts  []Prompt
	Defaults DefaultPrompts
}

// PreviewSource fills in Data for an inbox item. The launcher implements it.
type PreviewSource interface {
	// PreviewData returns the data a launch on the item would fill a prompt with,
	// leaving the session facts for the caller to set.
	PreviewData(ctx context.Context, itemID int64) (Data, error)
}

// Config holds a Service's dependencies. Store is required. Usage is optional: without
// it, a prompt's usage names only the app's default prompts. Source is optional:
// without it, previews use sample data. File and Database are optional: without them,
// the library keeps no copy of itself, and Backups lists only what the other one finds.
type Config struct {
	// Store holds the prompts and the app's default prompts.
	Store *Store

	// Usage lists where launch defaults name prompts.
	Usage UsageSource

	// Source fills in previews with an inbox item's data.
	Source PreviewSource

	// File is the copy of the library kept beside the database, written after every
	// change to the library.
	File *LibraryFile

	// Database is the database file, beside which Backups finds the databases the app
	// set aside and the backups it took before updates.
	Database string

	// App is the build's version, which every exported file records.
	App string

	// Clock stamps exported files. Nil is the system clock.
	Clock clock.Clock

	// Logger hears a library file that could not be written. Nil discards.
	Logger *slog.Logger

	// Changed is told after every change to the library, once the library file is
	// written. What names prompts by nickname outside the database, such as the launch
	// defaults' copy, rewrites itself there, so a renamed prompt is never lost. Optional.
	Changed func(context.Context)
}

// Service manages the prompt library.
type Service struct {
	store    *Store
	usage    UsageSource
	source   PreviewSource
	file     *LibraryFile
	database string
	app      string
	clock    clock.Clock
	logger   *slog.Logger
	changed  func(context.Context)
}

// NewService returns a Service over cfg.
func NewService(cfg Config) *Service {
	return &Service{
		store: cfg.Store, usage: cfg.Usage, source: cfg.Source, file: cfg.File, database: cfg.Database, app: cfg.App,
		clock: clock.Or(cfg.Clock), logger: logging.Or(cfg.Logger), changed: cfg.Changed,
	}
}

// List returns every prompt with its usage, sorted by nickname.
func (s *Service) List(ctx context.Context) ([]Prompt, error) {
	all, err := s.store.All(ctx)
	if err != nil {
		return nil, err
	}

	usage, err := s.loadUsage(ctx)
	if err != nil {
		return nil, err
	}

	for i := range all {
		all[i].Usage = normalizeUsage(usage[all[i].ID])
	}

	return all, nil
}

// Create saves a new prompt.
func (s *Service) Create(ctx context.Context, in Input) (Prompt, error) {
	in = in.normalized()

	var id int64

	err := s.store.inTx(ctx, func(st *Store) error {
		args, fields := validate(ctx, st, 0, in, Usage{})
		if len(fields) > 0 {
			return invalid(fields)
		}

		var err error
		if id, err = st.insert(ctx, fromInput(in, args)); err != nil {
			return fmt.Errorf("save the prompt: %w", err)
		}

		return nil
	})
	if err != nil {
		return Prompt{}, err
	}

	s.saved(ctx)

	return s.get(ctx, id)
}

// Update saves changes to a prompt. A prompt that is a default for a kind must keep
// applying to that kind.
func (s *Service) Update(ctx context.Context, id int64, in Input) (Prompt, error) {
	in = in.normalized()

	if _, err := s.store.Lookup(ctx, id); err != nil {
		return Prompt{}, err
	}

	usage, err := s.usageOf(ctx, id)
	if err != nil {
		return Prompt{}, err
	}

	err = s.store.inTx(ctx, func(st *Store) error {
		args, fields := validate(ctx, st, id, in, usage)
		if len(fields) > 0 {
			return invalid(fields)
		}

		if saveErr := st.update(ctx, id, fromInput(in, args)); saveErr != nil {
			return fmt.Errorf("save the prompt: %w", saveErr)
		}

		return nil
	})
	if err != nil {
		return Prompt{}, err
	}

	s.saved(ctx)

	return s.get(ctx, id)
}

// Delete removes a prompt. A prompt that is a default anywhere is refused with an
// InUseError that says where.
func (s *Service) Delete(ctx context.Context, id int64) error {
	err := s.store.remove(ctx, id)
	if err == nil {
		s.saved(ctx)

		return nil
	}

	inUse, ok := errors.AsType[*InUseError](err)
	if !ok {
		return err
	}

	// The store refused it, so the prompt is a default somewhere; say where.
	if inUse.Usage, err = s.usageOf(ctx, id); err != nil {
		return err
	}

	return inUse
}

// Duplicate saves a copy of a prompt: "<title> (copy)" with a free nickname such as
// "<nickname> copy". The copy is the user's own, not a starter.
func (s *Service) Duplicate(ctx context.Context, id int64) (Prompt, error) {
	var newID int64

	err := s.store.inTx(ctx, func(st *Store) error {
		p, err := st.Lookup(ctx, id)
		if err != nil {
			return err
		}

		dup := p
		dup.StarterKey = ""
		dup.Name = textutil.Clip(p.Name+" (copy)", maxNameRunes)

		if dup.Nickname, err = st.freeNickname(ctx, p.Nickname+" copy"); err != nil {
			return err
		}

		if newID, err = st.insert(ctx, dup); err != nil {
			return fmt.Errorf("save the copy: %w", err)
		}

		return nil
	})
	if err != nil {
		return Prompt{}, err
	}

	s.saved(ctx)

	return s.get(ctx, newID)
}

// Reset restores a starter prompt's title, text, and settings, and its nickname when
// no other prompt has taken it.
func (s *Service) Reset(ctx context.Context, id int64) (Prompt, error) {
	p, err := s.store.Lookup(ctx, id)
	if err != nil {
		return Prompt{}, err
	}

	starter, ok := starterFor(p.StarterKey)
	if !ok {
		return Prompt{}, fmt.Errorf("%w: %q", ErrNotStarter, p.Nickname)
	}

	usage, err := s.usageOf(ctx, id)
	if err != nil {
		return Prompt{}, err
	}

	err = s.store.inTx(ctx, func(st *Store) error {
		return resetPrompt(ctx, st, p, starter, usage)
	})
	if err != nil {
		return Prompt{}, err
	}

	s.saved(ctx)

	return s.get(ctx, id)
}

// Defaults returns the app's default prompt for each kind of item.
func (s *Service) Defaults(ctx context.Context) (DefaultPrompts, error) {
	return s.store.Defaults(ctx)
}

// SetDefault makes a prompt the app's default for kind. The prompt must apply to that
// kind.
func (s *Service) SetDefault(ctx context.Context, kind item.Kind, id int64) error {
	k, err := item.ParseKind(string(kind))
	if err != nil {
		return err
	}

	p, err := s.store.Lookup(ctx, id)
	if err != nil {
		return err
	}

	if !p.AppliesTo.Covers(k) {
		return fmt.Errorf("%w: %q doesn't apply to %s", ErrWrongKind, p.Nickname, k.Plural())
	}

	if err = s.store.setDefault(ctx, k, id); err != nil {
		return err
	}

	s.saved(ctx)

	return nil
}

// Variables returns every variable a prompt can use, for the cheat sheet.
func (s *Service) Variables() []Variable {
	return Variables()
}

// get returns one prompt with its usage.
func (s *Service) get(ctx context.Context, id int64) (Prompt, error) {
	p, err := s.store.Lookup(ctx, id)
	if err != nil {
		return Prompt{}, err
	}

	usage, err := s.usageOf(ctx, id)
	if err != nil {
		return Prompt{}, err
	}

	p.Usage = normalizeUsage(usage)

	return p, nil
}

// saved writes the library file after a change to the library, then tells Changed. A
// failure is logged, not returned: the change is stored, and the file is written again
// after the next one. The change is made whether or not the caller still waits, so the
// file is too.
func (s *Service) saved(ctx context.Context) {
	ctx = context.WithoutCancel(ctx)

	if s.file != nil {
		if err := s.file.Save(ctx, s.store); err != nil {
			s.logger.WarnContext(ctx, "could not save the copy of the prompt library", "path", s.file.Path(), "error", err)
		}
	}

	if s.changed != nil {
		s.changed(ctx)
	}
}

// resetPrompt restores the stored prompt p to its starter, keeping p's nickname when
// another prompt has taken the starter's. usage is where p is a default.
func resetPrompt(ctx context.Context, st *Store, p Prompt, starter Starter, usage Usage) error {
	in := starter.input()
	if taken, err := st.nicknameTaken(ctx, p.ID, in.Nickname); err != nil || taken {
		in.Nickname = p.Nickname
	}

	args, fields := validate(ctx, st, p.ID, in, usage)
	if len(fields) > 0 {
		return invalid(fields)
	}

	if err := st.update(ctx, p.ID, fromInput(in, args)); err != nil {
		return fmt.Errorf("reset the prompt: %w", err)
	}

	return nil
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
		ExtraFlags: agent.Join(st.ExtraArgs),
	}
}

// validate checks an Input for the prompt with id (0 for a new one) and returns its
// extra arguments, or the problems that stop it being saved.
func validate(ctx context.Context, st *Store, id int64, in Input, usage Usage) ([]string, []agent.FieldError) {
	args, fields := validateFields(ctx, st, id, in, usage)

	if t, _ := Parse(in.Body, appliesToOrAny(in.AppliesTo)); t == nil {
		fields = append(fields, agent.FieldError{Field: FieldBody, Message: "Fix the problems in the prompt's text."})
	}

	return args, fields
}

// validateFields checks every field but the text, which Parse checks.
func validateFields(ctx context.Context, st *Store, id int64, in Input, usage Usage) ([]string, []agent.FieldError) {
	var fields []agent.FieldError

	add := func(field, message string) {
		if message != "" {
			fields = append(fields, agent.FieldError{Field: field, Message: message})
		}
	}

	add(FieldName, checkName(in.Name))
	add(FieldNickname, checkNickname(ctx, st, id, in.Nickname))
	add(FieldAppliesTo, checkAppliesTo(in.AppliesTo, usage))

	args, overrides := agent.CheckOverrides(in.Model, in.Effort, in.ExtraFlags)

	return args, append(fields, overrides...)
}

// invalid is the error for a prompt that cannot be saved for the reasons in fields.
func invalid(fields []agent.FieldError) error {
	return &agent.ValidationError{Err: ErrInvalidPrompt, Fields: fields}
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
func checkNickname(ctx context.Context, st *Store, id int64, nickname string) string {
	switch {
	case nickname == "":
		return "Give the prompt a nickname for the launch menu."
	case utf8.RuneCountInString(nickname) > maxNicknameRunes:
		return fmt.Sprintf("Keep the nickname to %d characters so it fits the launch button.", maxNicknameRunes)
	case strings.ContainsFunc(nickname, unicode.IsControl):
		return "Keep the nickname on one line."
	}

	taken, err := st.nicknameTaken(ctx, id, nickname)
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
func checkAppliesTo(appliesTo AppliesTo, usage Usage) string {
	switch appliesTo {
	case AppliesToPR, AppliesToIssue, AppliesToAny:
	default:
		return "Choose what the prompt is for: pull requests, issues, or both."
	}

	for _, kind := range usage.Kinds {
		if !appliesTo.Covers(kind) {
			return fmt.Sprintf("It's a default for %s (%s), so it has to apply to them.",
				kind.Plural(), describeUsage(usage))
		}
	}

	return ""
}

// appliesToOrAny returns appliesTo when it is valid, else "any", so a prompt's text can
// be checked while its kind is still being chosen.
func appliesToOrAny(appliesTo AppliesTo) AppliesTo {
	if appliesTo == AppliesToPR || appliesTo == AppliesToIssue {
		return appliesTo
	}

	return AppliesToAny
}

// fromInput returns a validated Input with its extra arguments as a Prompt to store.
func fromInput(in Input, args []string) Prompt {
	return Prompt{
		Name: in.Name, Nickname: in.Nickname, AppliesTo: in.AppliesTo, Body: in.Body,
		Model: in.Model, Effort: in.Effort, PlanMode: in.PlanMode, Worktree: in.Worktree,
		ExtraArgs: slices.Clone(args),
	}
}
