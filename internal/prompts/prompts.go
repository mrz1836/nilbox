// Package prompts is the prompt library: the prompts a launch can fill in for a pull
// request or issue, with the launch settings each one sets, and the defaults for each
// kind of item; and, beside them in the same table, the review replies the review sheet
// fills in as the text of a review (see Purpose).
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

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/logging"
	"github.com/mrz1836/nilbox/internal/textutil"
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

	// ErrNotAgentPrompt is returned when a review reply is used where an agent prompt is
	// needed: as a launch's prompt, or a default for a kind of item.
	ErrNotAgentPrompt = errors.New("a review reply can't be used to launch an agent")

	// ErrNotReply is returned when an agent prompt, or a reply for another review, is made
	// a default reply.
	ErrNotReply = errors.New("the prompt isn't a reply for that review")
)

// Purpose is what a prompt is for.
type Purpose string

// What a prompt is for.
const (
	// PurposeAgent is a prompt a launch fills in for a coding agent.
	PurposeAgent Purpose = "agent"

	// PurposeReply is a review reply: text the review sheet fills in as the body of an
	// approval, a request for changes, or a comment. It applies to pull requests, has no
	// launch settings, and is never a launch's prompt.
	PurposeReply Purpose = "reply"
)

// ReplyAction is the review a reply is written for.
type ReplyAction string

// The reviews a reply can be written for.
const (
	// ReplyNone is an agent prompt's: it writes no review.
	ReplyNone ReplyAction = ""

	ReplyApprove        ReplyAction = "approve"
	ReplyRequestChanges ReplyAction = "request_changes"
	ReplyComment        ReplyAction = "comment"
)

// ReplySlot is a default a reply can be: the reply for each review, and a second one for
// approving a bot's pull request, such as a dependency update.
type ReplySlot string

// The default replies.
const (
	SlotApprove        ReplySlot = "approve"
	SlotApproveBots    ReplySlot = "approve_bots"
	SlotRequestChanges ReplySlot = "request_changes"
	SlotComment        ReplySlot = "comment"
)

// ReplySlots returns every default reply, in the order the editor lists them.
func ReplySlots() []ReplySlot {
	return []ReplySlot{SlotApprove, SlotApproveBots, SlotRequestChanges, SlotComment}
}

// Action is the review a slot's reply must be written for.
func (s ReplySlot) Action() ReplyAction {
	switch s {
	case SlotApprove, SlotApproveBots:
		return ReplyApprove
	case SlotRequestChanges:
		return ReplyRequestChanges
	case SlotComment:
		return ReplyComment
	default:
		return ""
	}
}

// fits reports whether p can be the slot's default reply (see takes).
func (s ReplySlot) fits(p Prompt) bool {
	return s.takes(p.Purpose, p.ReplyAction, p.AppliesTo)
}

// takes reports whether a prompt with purpose, written for action and applying to
// appliesTo, can be the slot's default reply: a reply for the slot's review that applies
// to pull requests, since only the review sheet starts from a default.
func (s ReplySlot) takes(purpose Purpose, action ReplyAction, appliesTo AppliesTo) bool {
	return purpose == PurposeReply && action == s.Action() && appliesTo.Covers(item.KindPR)
}

// replyAppliesTo is what a reply written for action applies to, given what it says: a
// comment reply applies to what it says, and every other reply, a review of a pull
// request, to pull requests.
func replyAppliesTo(action ReplyAction, says AppliesTo) AppliesTo {
	if action != ReplyComment {
		return AppliesToPR
	}

	return says
}

// describe names a slot in a sentence, as in "the default for approvals".
func (s ReplySlot) describe() string {
	switch s {
	case SlotApprove:
		return "the default for approvals"
	case SlotApproveBots:
		return "the default for approvals of bots' pull requests"
	case SlotRequestChanges:
		return "the default for requests for changes"
	case SlotComment:
		return "the default for comments"
	default:
		return "the default for approvals"
	}
}

// valid reports a reply action this build knows.
func (a ReplyAction) valid() bool {
	return a == ReplyApprove || a == ReplyRequestChanges || a == ReplyComment
}

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

	// Purpose is PurposeAgent or PurposeReply.
	Purpose Purpose `json:"purpose"`

	// ReplyAction is the review a reply is written for, and "" for an agent prompt.
	ReplyAction ReplyAction `json:"replyAction"`

	// Models is each agent's model; an agent with no entry inherits.
	Models map[agent.ID]string `json:"models"`

	// Effort is the effort level of every agent; "" inherits. An agent that lacks the
	// level launches with its own default.
	Effort string `json:"effort"`

	// PlanMode and Worktree turn plan mode and the worktree on or off; nil inherits.
	PlanMode *bool `json:"planMode"`
	Worktree *bool `json:"worktree"`

	// ExtraArgs is each agent's extra arguments, added after its profile's and the launch
	// defaults' arguments.
	ExtraArgs map[agent.ID][]string `json:"extraArgs"`

	// ExtraFlags is ExtraArgs on one line per agent, quoted the way the editor shows them.
	ExtraFlags map[agent.ID]string `json:"extraFlags"`

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

	// Replies lists the default replies a reply is. Unlike the defaults above, being one
	// never keeps a reply from being deleted.
	Replies []ReplySlot `json:"replies"`
}

// InUse reports whether the prompt is a default for a launch anywhere, which keeps it from
// being deleted.
func (u Usage) InUse() bool {
	return len(u.Defaults)+len(u.Orgs)+len(u.Repos) > 0
}

// Input is a prompt as the editor saves it. A reply's AppliesTo and launch settings are
// ignored: it applies to pull requests and has none.
type Input struct {
	Name      string    `json:"name"`
	Nickname  string    `json:"nickname"`
	AppliesTo AppliesTo `json:"appliesTo"`
	Body      string    `json:"body"`

	// Purpose is what a new prompt is for; "" is an agent prompt. A saved prompt keeps
	// the purpose it was created with.
	Purpose Purpose `json:"purpose"`

	// ReplyAction is the review a reply is written for; ignored for an agent prompt.
	ReplyAction ReplyAction `json:"replyAction"`

	Models   map[agent.ID]string `json:"models"`
	Effort   string              `json:"effort"`
	PlanMode *bool               `json:"planMode"`
	Worktree *bool               `json:"worktree"`

	// ExtraFlags is each agent's extra arguments typed on one line, quoted as in a shell.
	ExtraFlags map[agent.ID]string `json:"extraFlags"`
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

// DefaultPrompts are the app's default prompt for each kind of item, 0 when no prompt
// applies to that kind, and its default replies.
type DefaultPrompts struct {
	PR    int64 `json:"pr"`
	Issue int64 `json:"issue"`

	// Replies are the default replies.
	Replies ReplyDefaults `json:"replies"`
}

// ReplyDefaults are the default reply for each review, by prompt ID; 0 where none is
// chosen, which leaves the review's text empty.
type ReplyDefaults struct {
	Approve int64 `json:"approve"`

	// ApproveBots is the reply for approving a bot's pull request, such as a dependency
	// update.
	ApproveBots    int64 `json:"approveBots"`
	RequestChanges int64 `json:"requestChanges"`
	Comment        int64 `json:"comment"`
}

// For returns the default reply for slot.
func (r ReplyDefaults) For(slot ReplySlot) int64 {
	switch slot {
	case SlotApprove:
		return r.Approve
	case SlotApproveBots:
		return r.ApproveBots
	case SlotRequestChanges:
		return r.RequestChanges
	case SlotComment:
		return r.Comment
	default:
		return 0
	}
}

// Slots lists the slots whose default is id, in ReplySlots order.
func (r ReplyDefaults) Slots(id int64) []ReplySlot {
	out := []ReplySlot{}

	for _, slot := range ReplySlots() {
		if id != 0 && r.For(slot) == id {
			out = append(out, slot)
		}
	}

	return out
}

// For returns the default for kind.
func (d DefaultPrompts) For(kind item.Kind) int64 {
	if kind == item.KindPR {
		return d.PR
	}

	return d.Issue
}

// IsReply reports a review reply.
func (p Prompt) IsReply() bool {
	return p.Purpose == PurposeReply
}

// Template parses the prompt's text, as a reply's in reply mode, failing when it has
// errors.
func (p Prompt) Template() (*Template, error) {
	t, diags := parseFor(p.Purpose, p.Body, p.AppliesTo)
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
		checked, fields := validate(ctx, st, 0, in, Usage{})
		if len(fields) > 0 {
			return invalid(fields)
		}

		var err error
		if id, err = st.insert(ctx, fromInput(in, checked)); err != nil {
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
	stored, err := s.store.Lookup(ctx, id)
	if err != nil {
		return Prompt{}, err
	}

	// A prompt keeps the purpose it was created with.
	in.Purpose = stored.Purpose
	in = in.normalized()

	usage, err := s.usageOf(ctx, id)
	if err != nil {
		return Prompt{}, err
	}

	err = s.store.inTx(ctx, func(st *Store) error {
		checked, fields := validate(ctx, st, id, in, usage)
		if len(fields) > 0 {
			return invalid(fields)
		}

		if saveErr := st.update(ctx, id, fromInput(in, checked)); saveErr != nil {
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

	if p.IsReply() {
		return fmt.Errorf("%w: %q", ErrNotAgentPrompt, p.Nickname)
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

// SetReplyDefault makes the reply with id the default for slot, or, with id 0, leaves the
// slot with no default, so that review starts with no text. The prompt must be a reply for
// the slot's review that applies to pull requests.
func (s *Service) SetReplyDefault(ctx context.Context, slot ReplySlot, id int64) error {
	if slot.Action() == "" {
		return fmt.Errorf("%w: unknown default reply %q", ErrNotReply, slot)
	}

	if id != 0 {
		p, err := s.store.Lookup(ctx, id)
		if err != nil {
			return err
		}

		if !p.IsReply() || p.ReplyAction != slot.Action() {
			return fmt.Errorf("%w: %q can't be %s", ErrNotReply, p.Nickname, slot.describe())
		}

		if !slot.fits(p) {
			return fmt.Errorf("%w: %q doesn't apply to pull requests, so it can't be %s", ErrWrongKind, p.Nickname,
				slot.describe())
		}
	}

	if err := s.store.setReplyDefault(ctx, slot, id); err != nil {
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

	checked, fields := validate(ctx, st, p.ID, in, usage)
	if len(fields) > 0 {
		return invalid(fields)
	}

	if err := st.update(ctx, p.ID, fromInput(in, checked)); err != nil {
		return fmt.Errorf("reset the prompt: %w", err)
	}

	return nil
}

// input returns the starter as an Input.
func (st Starter) input() Input {
	return Input{
		Name: st.Name, Nickname: st.Nickname, AppliesTo: st.AppliesTo, Body: st.Body, Purpose: st.purpose(),
		ReplyAction: st.ReplyAction,
		Models:      agent.CloneModels(st.Models), Effort: st.Effort, PlanMode: st.PlanMode, Worktree: st.Worktree,
		ExtraFlags: agent.JoinArgs(st.ExtraArgs),
	}
}
