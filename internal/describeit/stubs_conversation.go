package describeit

import (
	"context"
	"errors"

	"github.com/mrz1836/nilbox/internal/agentrun/runevent"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// The conversation's half of the service, until it is built: each method says what it
// does, and refuses for now. The bound API is fixed here, so the window builds against it.

// errNotBuilt is returned by a part of the service that isn't built yet.
var errNotBuilt = errors.New("not built yet")

// notBuilt is the window's words for errNotBuilt.
func notBuilt() error {
	return uierr.New("Describe it isn't ready in this build yet.", errNotBuilt)
}

// Status says whether a turn can run now (Claude Code found, at least
// agent.ConversationFloor, and signed in, through Agents), with which agent (Agent,
// Inherit resolved), whether a turn runs, and how many cards from the user's own Claude
// Code wait. It reads no GitHub.
func (s *Service) Status(_ context.Context) (Status, error) {
	return Status{}, notBuilt()
}

// Agent returns the agent new conversations start with, as Settings → Autopilot chose it:
// each part "" to inherit.
func (s *Service) Agent(_ context.Context) (AgentChoice, error) {
	return AgentChoice{}, notBuilt()
}

// SetAgent sets the agent new conversations start with, refusing a profile Claude Code
// doesn't have and a model or effort the agent's own checks refuse, and returns it as
// stored.
func (s *Service) SetAgent(_ context.Context, _ AgentChoice) (AgentChoice, error) {
	return AgentChoice{}, notBuilt()
}

// Conversation returns the current conversation, each turn with its cards checked against
// the live snapshot, or an empty one (ID "") when there is none. A conversation that
// started more than 30 days ago is gone.
func (s *Service) Conversation(_ context.Context) (Conversation, error) {
	return Conversation{Turns: []Turn{}}, notBuilt()
}

// NewConversation sets the current conversation aside and makes an empty one, with the
// agent a, or Agent's for the zero choice. It is refused while a turn runs.
func (s *Service) NewConversation(_ context.Context, _ AgentChoice) (Conversation, error) {
	return Conversation{Turns: []Turn{}}, notBuilt()
}

// Send sends m as the current conversation's next turn, making a conversation with
// Agent's agent when there is none, and returns at once with the turn running: the
// window follows it through describeit:updated and Events. The turn's prompt is m's text,
// its attachments' words (Attachments), and what the user did with the cards since the
// last turn (kit.OutcomeWords); a fresh snapshot is written first. It is refused while a
// turn runs, for an empty message, and while Status isn't ready, each worded.
func (s *Service) Send(_ context.Context, _ Message) (Conversation, error) {
	return Conversation{Turns: []Turn{}}, notBuilt()
}

// Stop stops the turn running in the conversation id, if any: the agent is asked to stop
// (execpath.StopLadder), its turn ends as stopped, and the conversation goes on from it.
func (s *Service) Stop(_ context.Context, _ string) error {
	return notBuilt()
}

// Events returns the events of turn in the conversation id after the sequence number
// after: what the agent said and each tool it used, read from the turn's stream, a page at
// a time, as a background run's are (autopilot.Service.RunEvents).
func (s *Service) Events(_ context.Context, _ string, _ int, _ int64) (runevent.Page, error) {
	return runevent.Page{Events: []runevent.Event{}}, notBuilt()
}

// Settle records what the user did with one part of a card, in its proposals folder's
// outcomes, which the next turn tells the agent and the tool mode's proposal status reads.
// A part already settled keeps its first outcome.
func (s *Service) Settle(_ context.Context, _ SettleRequest) error {
	return notBuilt()
}
