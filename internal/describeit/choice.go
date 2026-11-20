package describeit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/semver"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// agentFileVersion is the format version of the file of the agent new conversations start
// with.
const agentFileVersion = 1

// maxAgentFileBytes caps that file: three short names.
const maxAgentFileBytes = 64 << 10

// agentFile is the agent new conversations start with, as agent.json keeps it.
type agentFile struct {
	Version int    `json:"version"`
	Profile string `json:"profile"`
	Model   string `json:"model"`
	Effort  string `json:"effort"`
}

// Agent returns the agent new conversations start with, as Settings → Autopilot chose it:
// each part "" to inherit.
func (s *Service) Agent(ctx context.Context) (AgentChoice, error) {
	return s.agentChoice(ctx), nil
}

// SetAgent sets the agent new conversations start with, refusing a profile Claude Code
// doesn't have and a model or effort the agent's own checks refuse, and returns it as
// stored.
func (s *Service) SetAgent(ctx context.Context, c AgentChoice) (AgentChoice, error) {
	c, err := s.checkChoice(ctx, c)
	if err != nil {
		return AgentChoice{}, err
	}

	data, err := json.Marshal(agentFile{Version: agentFileVersion, Profile: c.Profile, Model: c.Model, Effort: c.Effort})
	if err != nil {
		return AgentChoice{}, uierr.New("Describe it couldn't save the agent.", err)
	}

	if err = ensureDir(s.cfg.Dir); err == nil {
		err = appdata.WriteFile(s.agentFile(), data)
	}

	if err != nil {
		return AgentChoice{}, uierr.New("Describe it couldn't save the agent.", err)
	}

	return c, nil
}

// agentChoice reads the agent new conversations start with: each part inherited when the
// file is missing or can't be read.
func (s *Service) agentChoice(ctx context.Context) AgentChoice {
	data, err := readFile(s.agentFile(), maxAgentFileBytes)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			s.logger.WarnContext(ctx, "describe it couldn't read its agent; inheriting", "error", err)
		}

		return AgentChoice{}
	}

	var f agentFile
	if err = json.Unmarshal(data, &f); err != nil || f.Version != agentFileVersion {
		s.logger.WarnContext(ctx, "describe it's agent isn't one; inheriting", "error", err, "version", f.Version)

		return AgentChoice{}
	}

	c := AgentChoice{Profile: f.Profile, Model: f.Model, Effort: f.Effort}
	if agent.ValidateModel(c.Model) != nil || agent.ValidateEffort(agent.Claude, c.Effort) != nil || !oneLine(c.Profile) {
		s.logger.WarnContext(ctx, "describe it's agent holds a value it can't use; inheriting")

		return AgentChoice{}
	}

	return c
}

// checkChoice returns c trimmed, once the agent's own checks take its model and effort and
// Claude Code has its profile.
func (s *Service) checkChoice(ctx context.Context, c AgentChoice) (AgentChoice, error) {
	c = AgentChoice{
		Profile: strings.TrimSpace(c.Profile), Model: strings.TrimSpace(c.Model), Effort: strings.TrimSpace(c.Effort),
	}

	if err := agent.ValidateModel(c.Model); err != nil {
		return AgentChoice{}, uierr.New(fmt.Sprintf("%q isn't a model name %s takes.", textutil.Truncate(c.Model, 40),
			agentWords), err)
	}

	if err := agent.ValidateEffort(agent.Claude, c.Effort); err != nil {
		return AgentChoice{}, uierr.New(fmt.Sprintf("%q isn't an effort %s takes: choose %s.",
			textutil.Truncate(c.Effort, 40), agentWords, orSeries(agent.EffortLevels(agent.Claude))), err)
	}

	if c.Profile == "" {
		return c, nil
	}

	profiles, err := s.profiles(ctx)
	if err != nil {
		return AgentChoice{}, err
	}

	if !slices.ContainsFunc(profiles, func(p Profile) bool { return p.Name == c.Profile }) {
		return AgentChoice{}, uierr.New(fmt.Sprintf("%s has no profile named %q.", agentWords,
			textutil.Truncate(c.Profile, 40)), errNoProfile)
	}

	return c, nil
}

// profiles returns Claude Code's profiles, the launch defaults' first.
func (s *Service) profiles(ctx context.Context) ([]Profile, error) {
	if s.cfg.Agents == nil {
		return nil, uierr.New("Describe it can't reach "+agentWords+" in this build.", errNoAgents)
	}

	profiles, err := s.cfg.Agents.Profiles(ctx)
	if err != nil {
		return nil, uierr.New("Describe it couldn't read "+agentWords+"'s profiles.", err)
	}

	return textutil.NonNil(profiles), nil
}

// conversationStart returns how to start Claude Code with profile, "" for the launch
// defaults' own.
func (s *Service) conversationStart(ctx context.Context, profile string) (Start, error) {
	if s.cfg.Agents == nil {
		return Start{}, uierr.New("Describe it can't reach "+agentWords+" in this build.", errNoAgents)
	}

	start, err := s.cfg.Agents.Conversation(ctx, profile)
	if err != nil {
		return Start{}, uierr.New("Describe it couldn't find "+agentWords+".", err)
	}

	return start, nil
}

// readiness says whether start can run a turn, and when not why and what to do, each a
// sentence: Claude Code found, at least agent.ConversationFloor, and signed in.
func readiness(start Start, err error) (ready bool, problem, fix string) {
	switch {
	case err != nil:
		return false, uierr.Message(err), ""
	case !start.Ready:
		problem = start.Problem
		if problem == "" {
			problem = agentWords + " isn't ready for Describe it."
		}

		return false, problem, start.Fix
	case belowFloor(start.Version):
		problem, fix = floorWords(start.Version)

		return false, problem, fix
	default:
		return true, "", ""
	}
}

// belowFloor reports a version of Claude Code older than a conversation needs; one that
// can't be read is left to the agents' own readiness.
func belowFloor(version string) bool {
	c, ok := semver.Compare(version, agent.ConversationFloor(agent.Claude))

	return ok && c < 0
}

// oneLine reports text on one line, without control characters: "" is one.
func oneLine(text string) bool {
	return textutil.OneLine(text) == text
}

// orSeries joins choices as a sentence offers them: "a", "a or b", "a, b, or c".
func orSeries(choices []string) string {
	if len(choices) < 3 {
		return strings.Join(choices, " or ")
	}

	return strings.Join(choices[:len(choices)-1], ", ") + ", or " + choices[len(choices)-1]
}
