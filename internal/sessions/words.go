package sessions

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// MaxWordsRunes caps a session's last message as LastWords reads it in full.
const MaxWordsRunes = 20000

// ErrNoWords is returned by a WordsReader that finds nothing the session said.
var ErrNoWords = errors.New("the session's last message can't be read")

// Words is what an agent session last said, read in full on demand.
type Words struct {
	// Text is the message, as the agent wrote it, as markdown.
	Text string `json:"text"`

	// Partial reports that Text is not the whole message: it was cut to MaxWordsRunes, or
	// it is the start the ledger keeps, because the agent's own files could not be read.
	Partial bool `json:"partial"`
}

// WordsReader reads the whole last message of one agent's session from the agent's own
// files on this Mac. It never reaches the network. Each agent's observer package provides
// one.
type WordsReader interface {
	LastWords(ctx context.Context, r Record) (string, error)
}

// LastWords returns what the session with id last said, in full, read from the agent's own
// files, so the review sheet can show a whole verdict where a card shows its start. When the
// agent's files can't be read, it returns the start the ledger keeps, marked Partial; a
// session that said nothing has empty Words.
func (s *Service) LastWords(ctx context.Context, id string) (Words, error) {
	r, err := s.store.Get(ctx, id)
	if err != nil {
		return Words{}, fmt.Errorf("read the session: %w", err)
	}

	if reader := s.words[r.Agent]; reader != nil && r.AgentSessionID != "" {
		text, readErr := reader.LastWords(ctx, r)
		if text = strings.TrimSpace(text); readErr == nil && text != "" {
			return clipWords(text), nil
		}

		if readErr != nil && !errors.Is(readErr, ErrNoWords) {
			s.logger.DebugContext(ctx, "could not read a session's last message", "agent", r.Agent, "error", readErr)
		}
	}

	return Words{Text: r.LastMessage, Partial: r.LastMessage != ""}, nil
}

// clipWords returns text as Words, cut to MaxWordsRunes.
func clipWords(text string) Words {
	if utf8.RuneCountInString(text) <= MaxWordsRunes {
		return Words{Text: text}
	}

	return Words{Text: textutil.Clip(text, MaxWordsRunes), Partial: true}
}

// wordsReaders returns the readers in cfg, by agent, leaving out nil ones.
func wordsReaders(readers map[agent.ID]WordsReader) map[agent.ID]WordsReader {
	out := make(map[agent.ID]WordsReader, len(readers))

	for id, r := range readers {
		if r != nil {
			out[id] = r
		}
	}

	return out
}
