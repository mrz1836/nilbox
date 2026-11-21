package describeit

import (
	"context"
	"fmt"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/agentrun/runevent"
	"github.com/mrz1836/nilbox/internal/agentrun/stream"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// Events returns the events of turn in the conversation id after the sequence number
// after: what the agent said and each tool it used, read from the turn's stream, a page at
// a time, as a background run's are (autopilot.Service.RunEvents).
//
// While the turn runs, they come from its follower's parser, and a turn still getting
// ready, whose agent hasn't started, has none yet; neither is done. Once it has ended, its
// stream is read again whole by a parser of its own, which numbers its events as the
// follower's did, so the window asks on after the last it has across the turn's end.
func (s *Service) Events(_ context.Context, id string, turn int, after int64) (runevent.Page, error) {
	s.mu.Lock()

	if lt := s.live; lt != nil && lt.rec != nil && lt.rec.ID == id && lt.n == turn {
		parser := lt.parser

		s.mu.Unlock()

		if parser == nil {
			return runevent.EmptyPage(), nil
		}

		return parser.Page(after).Listed(), nil
	}

	known := s.knownTurnLocked(id, turn)

	s.mu.Unlock()

	if !known {
		return runevent.EmptyPage(), uierr.New("That turn isn't one of Describe it's any more.",
			fmt.Errorf("%w: turn %d of %q", errNoConversation, turn, id))
	}

	p := stream.Claude(s.workDir())
	readAll(p, s.turnLog(id, turn))

	page := p.Page(after).Listed()
	page.Done = true

	return page, nil
}

// knownTurnLocked reports the conversation id kept, with its turn n: the current
// conversation's, or a conversation set aside, read from its record. The caller holds mu.
func (s *Service) knownTurnLocked(id string, n int) bool {
	if !agent.ValidSessionID(id) || n < 1 {
		return false
	}

	if s.rec != nil && s.rec.ID == id {
		return s.rec.turn(n) != nil
	}

	r, err := s.readRecord(id)

	return err == nil && r.turn(n) != nil
}
