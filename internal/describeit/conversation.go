package describeit

import (
	"context"

	"github.com/google/uuid"

	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// Status says whether a turn can run now (Claude Code found, at least
// agent.ConversationFloor, and signed in, through Agents), with which agent (Agent,
// Inherit resolved), whether a turn runs, and how many cards from the user's own Claude
// Code wait. It reads no GitHub.
func (s *Service) Status(ctx context.Context) (Status, error) {
	choice := s.agentChoice(ctx)
	start, err := s.conversationStart(ctx, choice.Profile)

	st := Status{
		Agent: choice, Version: start.Version, Profile: resolved(start.Profile, choice.Profile),
		Model: resolved(choice.Model, start.Model), Effort: resolved(choice.Effort, start.Effort),
	}
	st.Ready, st.Problem, st.Fix = readiness(start, err)

	s.mu.Lock()
	st.Running = s.live != nil
	s.mu.Unlock()

	st.Pending = s.pendingOutside(ctx)

	return st, nil
}

// Conversation returns the current conversation, each turn with its cards checked against
// the live snapshot, or an empty one (ID "") when there is none. A conversation that
// started more than 30 days ago is gone.
func (s *Service) Conversation(ctx context.Context) (Conversation, error) {
	s.mu.Lock()

	rec := s.currentLocked()
	if rec != nil && rec.expired(s.clock.Now()) && s.live == nil {
		s.removeConversation(rec.ID)
		s.rec, rec = nil, nil
	}

	if rec == nil {
		s.mu.Unlock()

		conv := emptyConversation()
		conv.Agent = s.agentChoice(ctx)

		return conv, nil
	}

	copied, running := rec.clone(), s.runningLocked(rec.ID)

	s.mu.Unlock()

	return s.view(ctx, copied, running), nil
}

// runningLocked reports a turn of the conversation id running now. The caller holds mu.
func (s *Service) runningLocked(id string) bool {
	return s.live != nil && s.live.rec != nil && s.live.rec.ID == id && s.live.proc != nil
}

// NewConversation sets the current conversation aside and makes an empty one, with the
// agent a, or Agent's for the zero choice. It is refused while a turn runs.
func (s *Service) NewConversation(ctx context.Context, a AgentChoice) (Conversation, error) {
	if err := s.idle(); err != nil {
		return emptyConversation(), err
	}

	if a == (AgentChoice{}) {
		a = s.agentChoice(ctx)
	} else {
		var err error
		if a, err = s.checkChoice(ctx, a); err != nil {
			return emptyConversation(), err
		}
	}

	start, err := s.conversationStart(ctx, a.Profile)
	if err != nil {
		start = Start{}
	}

	rec := newRecord(uuid.NewString(), a, start, s.clock.Now())

	s.mu.Lock()

	if s.live != nil {
		s.mu.Unlock()

		return emptyConversation(), uierr.New(busyWords, errBusy)
	}

	if err = s.saveRecord(rec); err != nil {
		s.mu.Unlock()

		return emptyConversation(), uierr.New("Describe it couldn't start a new conversation.", err)
	}

	s.rec, s.looked = rec, true
	copied := rec.clone()

	s.mu.Unlock()

	s.updated(rec.ID, 0)

	return s.view(ctx, copied, false), nil
}

// idle refuses while a turn runs, or the app quits.
func (s *Service) idle() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch {
	case s.closing:
		return uierr.New(closingWords, errClosing)
	case s.live != nil:
		return uierr.New(busyWords, errBusy)
	default:
		return nil
	}
}

// Stop stops the turn running in the conversation id, if any: the agent is asked to stop
// (execpath.StopLadder), its turn ends as stopped, and the conversation goes on from it.
func (s *Service) Stop(_ context.Context, id string) error {
	s.mu.Lock()

	lt := s.live
	if lt == nil || !s.turnOfLocked(lt, id) {
		s.mu.Unlock()

		return nil
	}

	lt.stopped.Store(true)
	proc := lt.proc

	s.mu.Unlock()

	lt.stop(proc)

	return nil
}

// turnOfLocked reports lt a turn of the conversation id: of its record, or, for one still
// getting ready, of the current conversation. The caller holds mu.
func (s *Service) turnOfLocked(lt *liveTurn, id string) bool {
	if lt.rec != nil {
		return lt.rec.ID == id
	}

	return s.rec != nil && s.rec.ID == id
}

// view returns the conversation rec as the window shows it, running or not: each turn
// with its cards, read from their files and checked against the live snapshot, and what
// it cost so far.
func (s *Service) view(ctx context.Context, rec record, running bool) Conversation {
	conv := Conversation{
		ID: rec.ID, Agent: rec.Agent, Profile: rec.Profile, Model: rec.Model, Effort: rec.Effort,
		CreatedAt: rec.CreatedAt, Turns: make([]Turn, 0, len(rec.Turns)), Running: running,
	}

	cards := s.cardReader(ctx, rec.ID)

	for _, t := range rec.Turns {
		turn := Turn{
			N: t.N, Text: t.Text, Attach: textutil.NonNil(t.Attach), Feedback: t.Feedback, State: t.State, Error: t.Error,
			StartedAt: t.StartedAt, EndedAt: t.EndedAt, Reply: t.Reply, Cost: t.Cost, Estimated: t.Estimated,
			Cards: make([]Card, 0, len(t.Proposals)),
		}

		for _, id := range t.Proposals {
			if card, ok := cards.card(id, t.N); ok {
				turn.Cards = append(turn.Cards, card)
			}
		}

		conv.TokensIn += t.TokensIn
		conv.TokensOut += t.TokensOut

		if t.Total != nil {
			conv.Cost, conv.Estimated = t.Total, t.Estimated
		}

		conv.Turns = append(conv.Turns, turn)
	}

	return conv
}
