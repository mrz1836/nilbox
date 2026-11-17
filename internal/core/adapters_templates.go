package core

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/setup"
)

// errNotBuilt is an adapter asked before core filled in what it reads.
var errNotBuilt = errors.New("not built yet")

// templateSignals tells Autopilot's gallery what suggests a template
// (autopilot.Signals): the first-run setup's look at the clones' marker files, which counts
// those whose CI is go-fortress's, and the bots' pull requests the inbox stored lately.
// Core fills setup in once the setup is built, after Autopilot's service.
type templateSignals struct {
	inbox *inbox.Store
	setup *setup.Service
}

// TemplateSignals reads what suggests a template from what is stored and on disk alone.
func (t *templateSignals) TemplateSignals(ctx context.Context, since time.Time) (autopilot.TemplateSignals, error) {
	var out autopilot.TemplateSignals

	if t.inbox != nil {
		bots, err := t.inbox.BotPullRequests(ctx, since)
		if err != nil {
			return out, err
		}

		out.Dependabot, out.Bots = bots.Dependabot, bots.Bots
	}

	if t.setup != nil {
		signals, err := t.setup.Signals(ctx)
		if err != nil {
			return out, err
		}

		out.Fortress = signals.Fortress
	}

	return out, nil
}

// routinePromptTransfer carries an Autopilot export's prompts (autopilot.PromptTransfer)
// through the prompt library's own format. Core fills prompts in once the prompts'
// service is built, after Autopilot's.
type routinePromptTransfer struct {
	prompts *prompts.RoutinePrompts
}

// ExportPrompts returns the prompts with nicknames, in the prompts' file format.
func (t *routinePromptTransfer) ExportPrompts(ctx context.Context, nicknames []string) (json.RawMessage, int, error) {
	if t.prompts == nil {
		return nil, 0, errNotBuilt
	}

	data, n, err := t.prompts.Bundled(ctx, nicknames)
	if err != nil || n == 0 {
		return nil, 0, err
	}

	return json.RawMessage(data), n, nil
}

// PreviewPrompts says what bringing in each of raw's prompts would do.
func (t *routinePromptTransfer) PreviewPrompts(ctx context.Context, raw json.RawMessage) ([]autopilot.PromptArrival, error) {
	if t.prompts == nil {
		return nil, errNotBuilt
	}

	got, err := t.prompts.Arrivals(ctx, raw)
	if err != nil {
		return nil, err
	}

	out := make([]autopilot.PromptArrival, 0, len(got))

	for _, a := range got {
		arrives := autopilot.ArrivesDiffers

		switch {
		case a.New:
			arrives = autopilot.ArrivesNew
		case a.Same:
			arrives = autopilot.ArrivesSame
		}

		out = append(out, autopilot.PromptArrival{Nickname: a.Nickname, Arrives: arrives})
	}

	return out, nil
}

// ImportPrompts adds raw's prompts whose nickname no prompt here has.
func (t *routinePromptTransfer) ImportPrompts(ctx context.Context, raw json.RawMessage) ([]string, error) {
	if t.prompts == nil {
		return nil, errNotBuilt
	}

	return t.prompts.AddNew(ctx, raw)
}

// routineProfiles lists what this Mac runs agents with (autopilot.Profiles): each agent
// whose program is installed, with its profiles' names. Core fills profiles in once the
// profiles' service is built, after Autopilot's.
type routineProfiles struct {
	profiles *profiles.Service
}

// Profiles returns each installed agent by ID with its profiles' names.
func (p *routineProfiles) Profiles(ctx context.Context) (map[string][]string, error) {
	if p.profiles == nil {
		return nil, errNotBuilt
	}

	agents, err := p.profiles.Agents(ctx)
	if err != nil {
		return nil, err
	}

	stored, err := p.profiles.List(ctx)
	if err != nil {
		return nil, err
	}

	out := map[string][]string{}

	for _, a := range agents {
		if a.Installed {
			out[string(a.ID)] = profileNamesOf(stored, a.ID)
		}
	}

	return out, nil
}

// profileNamesOf returns the names of the stored profiles of the agent id, never nil.
func profileNamesOf(stored []profiles.Profile, id agent.ID) []string {
	names := []string{}

	for _, p := range stored {
		if p.Agent == id {
			names = append(names, p.Name)
		}
	}

	return names
}
