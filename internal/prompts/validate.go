package prompts

import (
	"cmp"
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/item"
)

// Limits on a prompt's names.
const (
	maxNameRunes     = 80
	maxNicknameRunes = 20
)

// Fields an agent.FieldError can name, matching Input's JSON names. A per-agent field's
// error for one agent names it with agent.FieldFor, as in "models.codex".
const (
	FieldName       = "name"
	FieldNickname   = "nickname"
	FieldAppliesTo  = "appliesTo"
	FieldAction     = "replyAction"
	FieldBody       = "body"
	FieldModels     = agent.FieldModels
	FieldEffort     = agent.FieldEffort
	FieldExtraFlags = agent.FieldExtraFlags
)

// normalized trims the names and each agent's model, and turns Windows line endings into
// newlines. An Input with no purpose is an agent prompt's, whose reply action is cleared;
// a reply has no launch settings, and applies to pull requests unless it is a comment
// reply that names what it applies to: an approval and a request for changes are reviews.
func (in Input) normalized() Input {
	in.Name = strings.TrimSpace(in.Name)
	in.Nickname = strings.TrimSpace(in.Nickname)
	in.Body = strings.ReplaceAll(in.Body, "\r\n", "\n")

	if in.Purpose != PurposeReply {
		in.Purpose, in.ReplyAction = PurposeAgent, ""
	} else {
		in.AppliesTo = replyAppliesTo(in.ReplyAction, cmp.Or(in.AppliesTo, AppliesToPR))
		in.Effort, in.PlanMode, in.Worktree = "", nil, nil
		in.Models, in.ExtraFlags = nil, nil
	}

	models := make(map[agent.ID]string, len(in.Models))
	for id, model := range in.Models {
		models[id] = strings.TrimSpace(model)
	}

	in.Models = models

	return in
}

// validate checks an Input for the prompt with id (0 for a new one) and returns its
// models and extra arguments ready to store, or the problems that stop it being saved.
func validate(ctx context.Context, st *Store, id int64, in Input, usage Usage) (agent.Checked, []agent.FieldError) {
	checked, fields := validateFields(ctx, st, id, in, usage)

	if t, _ := parseFor(in.Purpose, in.Body, appliesToOrAny(in.AppliesTo)); t == nil {
		fields = append(fields, agent.FieldError{Field: FieldBody, Message: "Fix the problems in the prompt's text."})
	}

	return checked, fields
}

// parseFor checks a prompt's text the way its purpose fills it in, for the kinds it applies
// to: a reply's in reply mode.
func parseFor(purpose Purpose, body string, appliesTo AppliesTo) (*Template, []Diagnostic) {
	if purpose == PurposeReply {
		return ParseReply(body, appliesTo)
	}

	return Parse(body, appliesTo)
}

// validateFields checks every field but the text, which Parse checks.
func validateFields(ctx context.Context, st *Store, id int64, in Input, usage Usage) (agent.Checked, []agent.FieldError) {
	var fields []agent.FieldError

	add := func(field, message string) {
		if message != "" {
			fields = append(fields, agent.FieldError{Field: field, Message: message})
		}
	}

	add(FieldName, checkName(in.Name))
	add(FieldNickname, checkNickname(ctx, st, id, in.Nickname))

	if in.Purpose == PurposeReply {
		add(FieldAction, checkReplyAction(in.ReplyAction, usage))
		add(FieldAppliesTo, checkReplyAppliesTo(in.AppliesTo, usage))

		return agent.Checked{Models: map[agent.ID]string{}, Args: map[agent.ID][]string{}}, fields
	}

	add(FieldAppliesTo, checkAppliesTo(in.AppliesTo, usage))

	checked, overrides := agent.CheckSettings(agent.Settings{
		Models: in.Models, Effort: in.Effort, ExtraFlags: in.ExtraFlags,
	})

	return checked, append(fields, overrides...)
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

// checkReplyAction checks the review a reply is written for: one of the three, and the
// one every default it is needs.
func checkReplyAction(action ReplyAction, usage Usage) string {
	if !action.valid() {
		return "Choose the review the reply is for: an approval, a request for changes, or a comment."
	}

	for _, slot := range usage.Replies {
		if slot.Action() != action {
			return fmt.Sprintf("It's %s, so it has to stay a reply for that review.", slot.describe())
		}
	}

	return ""
}

// checkReplyAppliesTo checks what a reply applies to: one of the three choices, and pull
// requests while it is a default reply, which only the review sheet starts from.
func checkReplyAppliesTo(appliesTo AppliesTo, usage Usage) string {
	switch appliesTo {
	case AppliesToPR, AppliesToIssue, AppliesToAny:
	default:
		return "Choose what the reply is for: pull requests, issues, or both."
	}

	if len(usage.Replies) > 0 && !appliesTo.Covers(item.KindPR) {
		return fmt.Sprintf("It's %s, so it has to apply to pull requests.", usage.Replies[0].describe())
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

// fromInput returns a validated Input with its checked models and extra arguments as a
// Prompt to store.
func fromInput(in Input, checked agent.Checked) Prompt {
	return Prompt{
		Name: in.Name, Nickname: in.Nickname, AppliesTo: in.AppliesTo, Body: in.Body, Purpose: in.Purpose,
		ReplyAction: in.ReplyAction,
		Models:      agent.CloneModels(checked.Models), Effort: in.Effort, PlanMode: in.PlanMode, Worktree: in.Worktree,
		ExtraArgs: agent.CloneArgs(checked.Args),
	}
}
