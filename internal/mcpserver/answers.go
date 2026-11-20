package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// answer is a tool's answer: its text, and whether it refuses the call.
type answer struct {
	text    string
	refused bool
}

// said is an answer that serves the call.
func said(text string) answer {
	return answer{text: text}
}

// refusal is an answer that refuses the call, saying why and what to do.
func refusal(text string) answer {
	return answer{text: text, refused: true}
}

// callResult is an answer as tools/call gives it: its text, and isError for a refusal,
// which the agent reads as a failed call it can act on. It never carries
// structuredContent, which the agent's client would show in place of the text (the
// spike's S8).
type callResult struct {
	Content []textContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// textContent is a text in a tool's result.
type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// result is a as tools/call gives it.
func (a answer) result() callResult {
	return callResult{Content: []textContent{{Type: "text", Text: a.text}}, IsError: a.refused}
}

// callTool answers one call of the tool name, one of those served here, with its
// arguments raw.
func (s *server) callTool(ctx context.Context, name string, raw json.RawMessage) answer {
	args, err := readArguments(raw, argumentKeys(name))
	if err != nil {
		return refusal(textutil.Sentence(err.Error()) + " " + takes(name))
	}

	switch name {
	case kit.ToolGuide:
		return s.guide()
	case kit.ToolReference:
		return s.reference(args[argTopic])
	case kit.ToolMySetup:
		return s.mySetup(args[argRoutine])
	case kit.ToolInboxNames:
		return s.inboxNames()
	case kit.ToolCheck:
		return s.check(ctx, args[argProposal])
	case kit.ToolPropose:
		return s.propose(ctx, args[argProposal])
	case kit.ToolProposalStatus:
		return s.proposalStatus()
	default:
		return refusal(tick(name) + " isn't one of the tools here.")
	}
}

// guide answers autopilot_guide: how Autopilot works, read first.
func (s *server) guide() answer {
	snap, err := s.snapshot()
	if err != nil {
		return snapshotRefusal(err)
	}

	return said(kit.Guide(snap.Vocabulary))
}

// reference answers autopilot_reference: the topic raw names, in full.
func (s *server) reference(raw json.RawMessage) answer {
	var topic string

	if err := json.Unmarshal(raw, &topic); err != nil || topic == "" {
		return refusal("Name the topic to read. " + takes(kit.ToolReference))
	}

	snap, err := s.snapshot()
	if err != nil {
		return snapshotRefusal(err)
	}

	text, ok := kit.Topic(topic, snap.Vocabulary)
	if !ok {
		return refusal(fmt.Sprintf("There is no topic %q. The topics are %s.", textutil.Truncate(topic, 40),
			topicList()))
	}

	return said(text)
}

// mySetup answers autopilot_my_setup: the user's setup, or the one routine raw names by
// its id, as much of it as the agent may read where it runs.
func (s *server) mySetup(raw json.RawMessage) answer {
	var id *int64

	if err := json.Unmarshal(orNull(raw), &id); err != nil {
		return refusal("`routine` is a routine's id, a whole number. " + takes(kit.ToolMySetup))
	}

	snap, err := s.snapshot()
	if err != nil {
		return snapshotRefusal(err)
	}

	if id == nil {
		return said(kit.SetupWords(snap, s.where))
	}

	text, ok := kit.RoutineWords(snap, s.where, *id)
	if !ok {
		return refusal(fmt.Sprintf("The user has no routine with id %d: %s without `routine` lists each routine "+
			"with its id.", *id, tick(kit.ToolMySetup)))
	}

	return said(text)
}

// inboxNames answers autopilot_inbox_names, which the app's own conversation alone has:
// the names in the user's inbox.
func (s *server) inboxNames() answer {
	snap, err := s.snapshot()
	if err != nil {
		return snapshotRefusal(err)
	}

	return said(kit.InboxWords(snap.Inbox))
}

// check answers autopilot_check: the proposal raw holds, checked against the whole
// snapshot, as the app checks its card, wherever the agent runs.
func (s *server) check(ctx context.Context, raw json.RawMessage) answer {
	p, err := proposalOf(raw)
	if err != nil {
		return proposalRefusal(kit.ToolCheck, err)
	}

	snap, err := s.snapshot()
	if err != nil {
		return snapshotRefusal(err)
	}

	return said(kit.Check(ctx, snap, p).Words())
}

// errNoProposal is a call that gives no proposal: none, or not an object.
var errNoProposal = errors.New("give the proposal as `proposal`, one JSON object")

// proposalOf reads the proposal a call gives as raw, as kit reads one: strictly, within
// its caps. An agent sometimes gives an object's JSON as a string, which is read the
// same.
func proposalOf(raw json.RawMessage) (kit.Proposal, error) {
	raw = bytes.TrimSpace(raw)

	if bytes.HasPrefix(raw, []byte(`"`)) {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return kit.Proposal{}, errNoProposal
		}

		raw = bytes.TrimSpace([]byte(text))
	}

	if !bytes.HasPrefix(raw, []byte("{")) {
		return kit.Proposal{}, errNoProposal
	}

	return kit.DecodeProposal(raw)
}

// proposalRefusal refuses a call of tool whose proposal can't be read, saying why and what
// to do.
func proposalRefusal(tool string, err error) answer {
	if errors.Is(err, errNoProposal) {
		return refusal(textutil.Sentence(err.Error()) + " " + takes(tool))
	}

	next := " Fix it, then check it again with `" + kit.ToolCheck + "`."
	if tool == kit.ToolPropose {
		next = " Nothing was proposed: fix it, check it with `" + kit.ToolCheck + "`, then propose it."
	}

	return refusal(textutil.Sentence(err.Error()) + next)
}

// snapshotRefusal refuses a call that needs the snapshot, which can't be read: err says
// why.
func snapshotRefusal(err error) answer {
	if errors.Is(err, fs.ErrNotExist) {
		return refusal("The user's setup isn't there to read yet: " + brand.Name + " writes it while it runs. Ask " +
			"the user to open " + brand.Name + ", then try again.")
	}

	return refusal(textutil.WithReason("The user's setup can't be read", reason(err)) + " Ask the user to open " +
		brand.Name + ", which writes it again, then try again.")
}

// reason says what went wrong in err without the path it went wrong at: the agent reads
// what failed, never where the app keeps the user's data.
func reason(err error) string {
	var (
		pe *fs.PathError
		le *os.LinkError
	)

	switch {
	case errors.As(err, &pe):
		return pe.Op + ": " + pe.Err.Error()
	case errors.As(err, &le):
		return le.Op + ": " + le.Err.Error()
	default:
		return strings.TrimSpace(err.Error())
	}
}

// orNull returns raw, or JSON's null for none.
func orNull(raw json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 {
		return json.RawMessage("null")
	}

	return raw
}
