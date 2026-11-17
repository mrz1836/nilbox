package agentrun

import (
	"bytes"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/mrz1836/nilbox/internal/agentrun/runevent"
)

// What every agent's stream parser shares: it is fed the run's log as it grows, splits it
// into lines itself (a line may be megabytes long, as a tool result holding a whole log is,
// and the last may not have ended yet), and keeps the last events with their sequence
// numbers, the todo list, the tokens used, and the agent's own end once it comes.

const (
	// keepEvents is how many events a run keeps in memory; the run view of a run with more
	// reads the rest from its log.
	keepEvents = 2000

	// maxLine is the longest line a parser reads; a longer one is skipped whole and counted
	// as unknown, so a runaway line never holds the app's memory.
	maxLine = 64 << 20

	// maxMessage is the most of the agent's final message a run keeps: a read-only run's
	// final message is its notes.
	maxMessage = 50 << 10
)

// lineReader reads one complete line of an agent's stream into the parser's state, through
// the stream's methods. It is called with the stream's lock held.
type lineReader func(s *stream, line []byte)

// stream is a run's parser: an io.Writer fed the run's log in order, from one goroutine at
// a time, which any goroutine may read.
type stream struct {
	read lineReader
	dir  string

	mu sync.Mutex

	// pending is a line not ended yet, and skipping reports one past maxLine, whose rest is
	// dropped up to its end.
	pending  []byte
	skipping bool

	// pos is how far into the log the parser has read, lineStart where the line being
	// read starts, line where the line whose events are emitted starts, and part how many
	// events that line gave so far.
	pos       int64
	lineStart int64
	line      int64
	part      int

	// at is the latest time a line said it was written, as RFC 3339, or "".
	at string

	// events are the last keepEvents events, and seq the last one's number.
	events []runevent.Event
	seq    int64

	progress runevent.Progress

	// usage holds each assistant message's latest usage, by its ID, which a message the
	// stream repeats per content block counts once.
	usage map[string]tokens

	result *Result
	limit  *Limit

	// keySource is where the agent's init line says its API key came from, "none" for a
	// sign-in with the user's plan.
	keySource string

	// agentSession is the agent's own ID of the conversation, when the agent picks it and
	// its stream names it (Codex's thread), "" otherwise.
	agentSession string

	// lastMessage is the last thing the agent said, whole up to maxMessage, for an agent
	// whose end carries no message of its own (Codex), and items the tool items whose
	// start was shown.
	lastMessage string
	items       map[string]bool
}

// tokens is one message's usage.
type tokens struct {
	in, out, cacheRead int64
}

// newStream returns a parser that reads each line with read; dir is the run's folder, which
// the events write paths from.
func newStream(read lineReader, dir string) *stream {
	return &stream{
		read: read, dir: dir, usage: map[string]tokens{}, items: map[string]bool{},
		progress: runevent.Progress{Todos: []runevent.Todo{}},
	}
}

// Write feeds the parser more of the log. It never fails.
func (s *stream) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	n := len(p)

	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			s.hold(p)
			s.pos += int64(len(p))

			break
		}

		s.hold(p[:i])
		s.pos += int64(i) + 1
		p = p[i+1:]

		s.readPending()
	}

	return n, nil
}

// Page returns the events after the one numbered after, and how far the run has got. The
// events it no longer keeps in memory are the log's, before the first one's line.
func (s *stream) Page(after int64) runevent.Page {
	s.mu.Lock()
	defer s.mu.Unlock()

	page := runevent.Page{Events: []runevent.Event{}, Progress: s.progressLocked()}

	if len(s.events) > 0 {
		first := s.events[0]
		page.First, page.Before, page.Earlier = first.Seq, first.Offset, first.Seq > 1 && first.Offset > 0
	}

	for _, e := range s.events {
		if e.Seq > after {
			page.Events = append(page.Events, e)
		}
	}

	return page
}

// Progress returns how far the run has got.
func (s *stream) Progress() runevent.Progress {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.progressLocked()
}

// Result returns the agent's own end, once its result line came.
func (s *stream) Result() (Result, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.result == nil {
		return Result{}, false
	}

	return *s.result, true
}

// Limit returns the provider's usage limit as the agent last reported it, or nil.
func (s *stream) Limit() *Limit {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.limit == nil {
		return nil
	}

	l := *s.limit

	return &l
}

// AgentSessionID returns the agent's own ID of the conversation, once its stream names it
// (Codex's thread.started), or "".
func (s *stream) AgentSessionID() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.agentSession
}

// Over reports that the agent's result line came: the run is done, whatever of it is left
// running.
func (s *stream) Over() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.result != nil
}

// from starts the parser at offset in the log, a line's start, as for a page read from its
// middle: its events' offsets count from there.
func (s *stream) from(offset int64) *stream {
	s.pos, s.lineStart = offset, offset

	return s
}

// flush reads a last line the log never ended, as a run's that has ended may be: the log
// won't grow, so the line is whole.
func (s *stream) flush() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.readPending()
}

// all returns every event the parser keeps, oldest first.
func (s *stream) all() []runevent.Event {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]runevent.Event{}, s.events...)
}

// readPending reads the line held, whole now, unless it ran past maxLine, and starts the
// next one where the parser has read to. The caller holds mu.
func (s *stream) readPending() {
	if !s.skipping && len(bytes.TrimSpace(s.pending)) > 0 {
		s.line, s.part = s.lineStart, 0
		s.read(s, s.pending)
	}

	s.pending, s.skipping = s.pending[:0], false
	s.lineStart = s.pos
}

// hold keeps part of a line not ended yet, dropping one past maxLine.
func (s *stream) hold(part []byte) {
	if s.skipping {
		return
	}

	if len(s.pending)+len(part) > maxLine {
		s.pending, s.skipping = s.pending[:0], true
		s.progress.Unknown++

		return
	}

	s.pending = append(s.pending, part...)
}

// emit records an event, cutting its text to runevent.MaxText, at its line's offset and
// the latest time a line gave.
func (s *stream) emit(e runevent.Event) {
	e.Text, e.Cut = cut(e.Text, runevent.MaxText)
	e.Offset, e.Part, e.At = s.line, s.part, s.at
	s.part++

	s.seq++
	e.Seq = s.seq
	s.progress.Events = s.seq

	s.events = append(s.events, e)
	if len(s.events) > keepEvents {
		s.events = append(s.events[:0], s.events[len(s.events)-keepEvents:]...)
	}
}

// unknown counts a message the parser didn't know.
func (s *stream) unknown() {
	s.progress.Unknown++
}

// stamp notes when a line says it was written, an RFC 3339 time; anything else is left
// alone, so a line with none keeps the latest time before it.
func (s *stream) stamp(at string) {
	if at == "" {
		return
	}

	if t, err := time.Parse(time.RFC3339Nano, at); err == nil {
		s.at = t.UTC().Format(time.RFC3339Nano)
	}
}

// step sets the run's latest step.
func (s *stream) step(text string) {
	if text != "" {
		s.progress.Step, _ = cut(text, 200)
	}
}

// addUsage records an assistant message's usage, by its ID.
func (s *stream) addUsage(id string, t tokens) {
	if id == "" {
		id = "#" + strconv.Itoa(len(s.usage))
	}

	s.usage[id] = t

	var sum tokens
	for _, u := range s.usage {
		sum.in, sum.out, sum.cacheRead = sum.in+u.in, sum.out+u.out, sum.cacheRead+u.cacheRead
	}

	s.progress.TokensIn, s.progress.TokensOut, s.progress.CacheRead = sum.in, sum.out, sum.cacheRead
}

// progressLocked returns a copy of the progress.
func (s *stream) progressLocked() runevent.Progress {
	p := s.progress
	p.Todos = append([]runevent.Todo{}, s.progress.Todos...)

	return p
}

// lastWords returns the last thing the agent said, or its latest step: what a run that
// didn't report shows.
func (s *stream) lastWords() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := len(s.events) - 1; i >= 0; i-- {
		if s.events[i].Kind == runevent.KindText {
			return s.events[i].Text
		}
	}

	return s.progress.Step
}

// cut returns text cut to at most limit bytes, between characters, and whether it was.
func cut(text string, limit int) (string, bool) {
	if len(text) <= limit {
		return text, false
	}

	end := limit
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}

	return text[:end], true
}
