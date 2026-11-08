package prompts

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// CheckRequest asks to check a prompt being edited and to fill it in for a preview.
type CheckRequest struct {
	// ID is the prompt being edited, or 0 for a new one. It lets the nickname keep its
	// own name and the kind stay covered where the prompt is a default.
	ID int64 `json:"id"`

	// Input is the prompt as it stands in the editor.
	Input Input `json:"input"`

	// ItemID is the inbox item to preview with, or 0 for a sample item.
	ItemID int64 `json:"itemId"`

	// InWorktree and AtPRHead set the preview session: whether it starts in a worktree
	// and, for a pull request, whether that worktree starts at its head.
	InWorktree bool `json:"inWorktree"`
	AtPRHead   bool `json:"atPrHead"`
}

// CheckResult is what the editor shows for a prompt being edited.
type CheckResult struct {
	// Fields lists problems with the title, nickname, kind, model, effort, and flags.
	Fields []agent.FieldError `json:"fields"`

	// Diagnostics lists problems in the text, placed for the editor.
	Diagnostics []Diagnostic `json:"diagnostics"`

	// Preview is the prompt filled in for the preview item, or "" while the text has
	// errors.
	Preview string `json:"preview"`

	// Notice is the line added after the prompt for an outside contributor's item, or "".
	Notice string `json:"notice"`

	// PreviewError says why the preview could not be filled in, or "".
	PreviewError string `json:"previewError"`

	// PreviewNote explains a preview that uses a sample item instead of the one asked
	// for, or "".
	PreviewNote string `json:"previewNote"`

	// Sample reports whether the preview used sample data.
	Sample bool `json:"sample"`

	// Values holds each variable's value for the preview item, as {{.Name}} prints it.
	Values map[string]string `json:"values"`

	// Characters counts the filled-in prompt, notice included.
	Characters int `json:"characters"`
}

// Check checks a prompt being edited, as Create and Update would, and fills it in for a
// preview item: an inbox item when one is named and the prompt applies to it, else
// sample data. Problems are reported in the result; an error means the check itself
// could not run.
func (s *Service) Check(ctx context.Context, req CheckRequest) (CheckResult, error) {
	in := req.Input.normalized()

	var usage Usage

	if req.ID != 0 {
		u, err := s.usageOf(ctx, req.ID)
		if err != nil {
			return CheckResult{}, err
		}

		usage = u
	}

	_, fields := validateFields(ctx, s.store, req.ID, in, usage)
	tmpl, diags := Parse(in.Body, appliesToOrAny(in.AppliesTo))

	data, note := s.previewData(ctx, req, appliesToOrAny(in.AppliesTo))
	result := CheckResult{
		Fields: textutil.NonNil(fields), Diagnostics: textutil.NonNil(diags), Values: data.Values(),
		PreviewNote: note, Sample: note != "" || req.ItemID == 0,
	}

	if tmpl == nil {
		return result, nil
	}

	out, err := tmpl.Execute(data)
	if err != nil {
		result.PreviewError = textutil.Sentence(previewMessage(err))

		return result, nil
	}

	result.Preview, result.Notice = out, Notice(data)
	result.Characters = utf8.RuneCountInString(out)

	if result.Notice != "" {
		result.Characters += utf8.RuneCountInString(result.Notice) + 2
	}

	return result, nil
}

// previewData returns the data to preview with and, when it had to fall back to sample
// data for an item that was asked for, why.
func (s *Service) previewData(ctx context.Context, req CheckRequest, appliesTo AppliesTo) (Data, string) {
	sample := Sample(item.KindPR)
	if appliesTo == AppliesToIssue {
		sample = Sample(item.KindIssue)
	}

	sample = sample.WithSession(req.InWorktree, req.AtPRHead)

	if req.ItemID == 0 || s.source == nil {
		return sample, ""
	}

	data, err := s.source.PreviewData(ctx, req.ItemID)
	if err != nil {
		return sample, "That item is no longer in the inbox, so the preview uses a sample."
	}

	if kind := data.kind(); !appliesTo.Covers(kind) {
		return sample, "This prompt isn't for " + kind.Plural() + ", so the preview uses a sample."
	}

	return data.WithSession(req.InWorktree, req.AtPRHead), ""
}

// previewMessage explains why a prompt could not be filled in for the preview.
func previewMessage(err error) string {
	switch {
	case errors.Is(err, ErrLeadingDash):
		return `for this item the prompt starts with "-", so a launch would be refused. Start it with a word`
	case errors.Is(err, ErrEmptyPrompt):
		return "for this item the prompt is empty"
	default:
		return err.Error()
	}
}
