package autopilot

import (
	"context"
	"errors"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// A routine's agent step is launched exactly as a click's launch is, with the routine's
// choices in the launch menu's place: core turns a RunRequest into the launcher's own
// request, the same way for the start (the agent runner) and for Preview, so what Preview
// shows is what runs.

// ErrNoAgentStep is returned for a Preview of a routine that runs no agent.
var ErrNoAgentStep = errors.New("the routine runs no agent")

// RunRequest is a routine's agent step on one item, as its runner launches it.
type RunRequest struct {
	// ItemID is the item, and RoutineID and Routine the routine, by its name.
	ItemID    int64
	RoutineID int64
	Routine   string

	// Trigger is the routine's trigger, and Attempt which of its tries on the item this
	// is, 1 for the first.
	Trigger catalog.Event
	Attempt int

	// Step is the agent step, with the routine's launch choices.
	Step spec.AgentStep

	// Confirmed are the Keys of the holds the user confirmed.
	Confirmed []string
}

// Previewer plans a routine's agent step on one item without starting anything: core's
// over the launcher's own Preview.
type Previewer interface {
	PreviewRun(ctx context.Context, req RunRequest) (RunPreview, error)
}

// RunPreview is a routine's agent step planned on one item and not started: what it
// resolves to (each launch choice where it came from, as the launch menu names it), where
// it runs, the command, and the prompt it passes, which are the launcher's own Preview's.
type RunPreview struct {
	// Agent, Profile, Model, Effort, and PromptName are what the launch resolves to, each
	// with where it came from in Sources ("repo", "routine", ...), by the same names.
	Agent      string            `json:"agent"`
	Profile    string            `json:"profile"`
	Model      string            `json:"model"`
	Effort     string            `json:"effort"`
	PromptName string            `json:"promptName"`
	Sources    map[string]string `json:"sources"`

	// Dir is where it starts, with ~ for home, and Worktree its worktree, nil when it
	// starts in the clone.
	Dir      string       `json:"dir"`
	Worktree *RunWorktree `json:"worktree"`

	// Command is the agent's command line, the prompt shown as <prompt>, and Env the
	// changes to its environment ("unset ANTHROPIC_API_KEY").
	Command string   `json:"command"`
	Env     []string `json:"env"`

	// Permission summarizes its permissions, Prompt is the filled-in prompt, and Terminal
	// the terminal it opens in.
	Permission string `json:"permission"`
	Prompt     string `json:"prompt"`
	Terminal   string `json:"terminal"`

	// Warnings are what the resolution skipped, such as a launch default naming a
	// profile that is gone.
	Warnings []string `json:"warnings"`
}

// RunWorktree is the worktree a previewed run starts in, as the launch menu's Preview
// shows it.
type RunWorktree struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Branch string `json:"branch"`
	Action string `json:"action"`
	Base   string `json:"base"`
	Dirty  int    `json:"dirty"`
}

// PreviewRun plans the draft d's agent step on the item itemID, as it would start: the
// launcher's own plan of exactly the launch a click with the same choices makes, built and
// not started, with nothing written or opened. The draft needn't be saved, but must have
// no problem.
func (s *Service) PreviewRun(ctx context.Context, d Draft, itemID int64) (RunPreview, error) {
	if s.previews == nil {
		return emptyPreview(), uierr.New("Preview isn't available here.", ErrNoAgentStep)
	}

	sp, problems, err := s.loadDraft(ctx, d.Spec)
	if err != nil {
		return emptyPreview(), err
	}

	if len(problems) > 0 {
		return emptyPreview(), uierr.New("Fix the routine's problems first: "+problems[0].Message, &ProblemsError{
			err: &spec.ValidationError{Problems: problems},
		})
	}

	for _, st := range sp.Steps {
		if st.Agent == nil {
			continue
		}

		got, previewErr := s.previews.PreviewRun(ctx, RunRequest{
			ItemID: itemID, RoutineID: d.ID, Routine: d.Name, Trigger: sp.Trigger.Event, Attempt: 1, Step: *st.Agent,
		})
		if previewErr != nil {
			return emptyPreview(), previewErr
		}

		return nonNilPreview(got), nil
	}

	return emptyPreview(), uierr.New("This routine runs no agent, so there's nothing to preview.", ErrNoAgentStep)
}

// emptyPreview is a preview with its lists empty, for an error's result.
func emptyPreview() RunPreview {
	return nonNilPreview(RunPreview{})
}

// nonNilPreview returns p with no nil list or map.
func nonNilPreview(p RunPreview) RunPreview {
	if p.Sources == nil {
		p.Sources = map[string]string{}
	}

	if p.Env == nil {
		p.Env = []string{}
	}

	if p.Warnings == nil {
		p.Warnings = []string{}
	}

	return p
}
