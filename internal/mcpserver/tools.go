package mcpserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// The arguments the tools take, by their keys.
const (
	argTopic    = "topic"
	argRoutine  = "routine"
	argProposal = "proposal"
)

// toolList is what tools/list answers: every tool served here, on one page.
type toolList struct {
	Tools []toolInfo `json:"tools"`
}

// toolInfo is one tool as tools/list gives it: its name, what it is for, and its input's
// schema, kept small, since every request the agent makes carries the list (the spike's
// S11): a proposal's whole schema is a topic of its own.
type toolInfo struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	InputSchema objectSchema `json:"inputSchema"`
}

// objectSchema is a tool's input: one object, holding its properties and no other key.
type objectSchema struct {
	Type                 string              `json:"type"`
	Properties           map[string]property `json:"properties,omitempty"`
	Required             []string            `json:"required,omitempty"`
	AdditionalProperties bool                `json:"additionalProperties"`
}

// property is one of a tool's arguments: its type, what it is, and the values it may take,
// when they are few.
type property struct {
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Enum        []string `json:"enum,omitempty"`
}

// toolInfos returns the tools served where w says, in the order kit.Tools lists them.
func toolInfos(w kit.Where) []toolInfo {
	names := kit.Tools(w)
	out := make([]toolInfo, 0, len(names))

	for _, name := range names {
		out = append(out, toolInfoOf(name, w))
	}

	return out
}

// toolInfoOf returns what tools/list says of the tool name where w says.
func toolInfoOf(name string, w kit.Where) toolInfo {
	info := toolInfo{Name: name, Description: toolDescription(name, w), InputSchema: objectSchema{Type: "object"}}

	switch name {
	case kit.ToolReference:
		info.InputSchema.Properties = map[string]property{argTopic: {
			Type: "string", Description: "The topic to read, one of the guide's.", Enum: kit.Topics(),
		}}
		info.InputSchema.Required = []string{argTopic}
	case kit.ToolMySetup:
		info.InputSchema.Properties = map[string]property{argRoutine: {
			Type: "integer", Description: "A routine's id, to read that one routine; leave it out to read the whole setup.",
		}}
	case kit.ToolCheck, kit.ToolPropose:
		info.InputSchema.Properties = map[string]property{argProposal: {
			Type: "object",
			Description: "The proposal, one JSON object: topic `proposal` of `" + kit.ToolReference + "` says its parts, " +
				"with a worked example, and topic `schema` gives its JSON Schema.",
		}}
		info.InputSchema.Required = []string{argProposal}
	}

	return info
}

// toolDescription says what the tool name is for, where w says.
func toolDescription(name string, w kit.Where) string {
	switch name {
	case kit.ToolGuide:
		return "How Autopilot works in " + brand.Name + ", what a proposal holds, how to work, and the topics `" +
			kit.ToolReference + "` reads. Read it first."
	case kit.ToolReference:
		return "One topic in full, every key and value spelled as this version of " + brand.Name + " spells it."
	case kit.ToolMySetup:
		if w == kit.Outside {
			return "The user's routines by name and sentence, their prompts' nicknames, and Autopilot's settings. " +
				"Read it before you propose anything."
		}

		return "The user's routines with their definitions, their prompts, scripts, and agent profiles, " +
			"Autopilot's settings, and the rules routines lean on. Read it before you propose anything."
	case kit.ToolInboxNames:
		return "The names in the user's inbox, the most common first: repositories, owners, people, bots, labels, " +
			"and checks, as a routine names them."
	case kit.ToolCheck:
		return "Checks a proposal against the user's setup as the Autopilot tab will, without proposing it: each " +
			"problem and warning where it is, the sentence each routine reads as, and the risks the user will see."
	case kit.ToolPropose:
		return "Puts a proposal in " + brand.Name + "'s Autopilot tab for the user to review, once it checks " +
			"without a problem. Nothing changes until the user applies it there."
	case kit.ToolProposalStatus:
		return "What the user did with your proposals: each part they added, saved, applied, discarded, or opened " +
			"in the builder, and each part still waiting for them."
	default:
		return ""
	}
}

// errArguments is a tool call's arguments the tool can't read: not one JSON object, or
// holding a key the tool doesn't take.
var errArguments = errors.New("the arguments can't be read")

// argumentKeys are the keys the tool name takes.
func argumentKeys(name string) []string {
	switch name {
	case kit.ToolReference:
		return []string{argTopic}
	case kit.ToolMySetup:
		return []string{argRoutine}
	case kit.ToolCheck, kit.ToolPropose:
		return []string{argProposal}
	default:
		return nil
	}
}

// readArguments reads a tool call's arguments: one JSON object, or none, holding only
// keys.
func readArguments(raw json.RawMessage, keys []string) (map[string]json.RawMessage, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return map[string]json.RawMessage{}, nil
	}

	var args map[string]json.RawMessage

	if err := json.Unmarshal(raw, &args); err != nil || args == nil {
		return nil, fmt.Errorf("%w: they aren't one JSON object", errArguments)
	}

	given := make([]string, 0, len(args))
	for key := range args {
		given = append(given, key)
	}

	slices.Sort(given)

	for _, key := range given {
		if !slices.Contains(keys, key) {
			return nil, fmt.Errorf("%w: %q isn't one of them", errArguments, textutil.Truncate(key, 40))
		}
	}

	return args, nil
}

// takes says what the tool name takes, as a refusal of its arguments ends.
func takes(name string) string {
	tool := tick(name)

	switch name {
	case kit.ToolReference:
		return tool + " takes `topic`, the topic to read, such as `{\"topic\": \"steps\"}`. The topics are " +
			topicList() + "."
	case kit.ToolMySetup:
		return tool + " takes nothing for the whole setup, or `routine`, a routine's id, for that routine alone, " +
			"such as `{\"routine\": 7}`."
	case kit.ToolCheck, kit.ToolPropose:
		return tool + " takes `proposal`, the proposal as one JSON object, such as `{\"proposal\": {\"summary\": " +
			"\"…\", \"routines\": [ … ]}}`: topic `proposal` of `" + kit.ToolReference + "` says its parts."
	default:
		return tool + " takes no arguments: `{}`."
	}
}

// topicList lists the reference's topics, each in backticks.
func topicList() string {
	topics := kit.Topics()

	ticked := make([]string, 0, len(topics))
	for _, t := range topics {
		ticked = append(ticked, tick(t))
	}

	return strings.Join(ticked, ", ")
}

// tick writes s as code, in backticks.
func tick(s string) string {
	return "`" + s + "`"
}
