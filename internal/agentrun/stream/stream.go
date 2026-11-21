// Package stream reads a coding agent's stream as its run writes it: Claude Code's
// `claude -p --output-format stream-json --verbose` and Codex's `codex exec --json`. Each
// agent's parser turns its own messages into the one event model (runevent), and keeps
// how far the run has got, the agent's own end (Result), the provider's usage limit, and,
// for Claude Code, what its init line says of its own setup (Init).
//
// It is a leaf: it imports only runevent and the standard library, so the background
// runner (internal/agentrun) and anything else that follows an agent's stream read it
// without reaching a run engine or the store.
package stream

import (
	"bytes"
	"slices"
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
	// KeepEvents is how many events a parser keeps in memory; the run view of a run with
	// more reads the rest from its log.
	KeepEvents = 2000

	// maxLine is the longest line a parser reads; a longer one is skipped whole and counted
	// as unknown, so a runaway line never holds the app's memory.
	maxLine = 64 << 20

	// maxMessage is the most of the agent's final message a run keeps: a read-only run's
	// final message is its notes.
	maxMessage = 50 << 10
)

// lineReader reads one complete line of an agent's stream into the parser's state, through
// the parser's methods. It is called with the parser's lock held.
type lineReader func(s *Parser, line []byte)

// Parser is a run's parser of its agent's stream (Claude or Codex): an io.Writer fed the
// run's log in order, from one goroutine at a time, which any goroutine may read.
type Parser struct {
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

	// events are the last KeepEvents events, and seq the last one's number.
	events []runevent.Event
	seq    int64

	progress runevent.Progress

	// usage holds each assistant message's latest usage, by its ID, which a message the
	// stream repeats per content block counts once.
	usage map[string]tokens

	result *Result
	limit  *Limit

	// init is what the agent's init line said of its setup, nil until it comes.
	init *Init

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

// newParser returns a parser that reads each line with read; dir is the run's folder, which
// the events write paths from.
func newParser(read lineReader, dir string) *Parser {
	return &Parser{
		read: read, dir: dir, usage: map[string]tokens{}, items: map[string]bool{},
		progress: runevent.Progress{Todos: []runevent.Todo{}},
	}
}

// Write feeds the parser more of the log. It never fails.
func (s *Parser) Write(p []byte) (int, error) {
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
func (s *Parser) Page(after int64) runevent.Page {
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
func (s *Parser) Progress() runevent.Progress {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.progressLocked()
}

// Result returns the agent's own end, once its result line came.
func (s *Parser) Result() (Result, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.result == nil {
		return Result{}, false
	}

	return *s.result, true
}

// Limit returns the provider's usage limit as the agent last reported it, or nil.
func (s *Parser) Limit() *Limit {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.limit == nil {
		return nil
	}

	l := *s.limit

	return &l
}

// Init returns what the agent's init line said of its own setup, once it came: only Claude
// Code's stream has one. A turn reads it to check its own tools and MCP servers before it
// goes on.
func (s *Parser) Init() (Init, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.init == nil {
		return Init{}, false
	}

	in := *s.init
	in.Tools = append([]string{}, s.init.Tools...)
	in.MCPServers = append([]MCPServer{}, s.init.MCPServers...)

	return in, true
}

// AgentSessionID returns the agent's own ID of the conversation, once its stream names it
// (Codex's thread.started), or "".
func (s *Parser) AgentSessionID() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.agentSession
}

// Over reports that the agent's result line came: the run is done, whatever of it is left
// running.
func (s *Parser) Over() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.result != nil
}

// From starts the parser at offset in the log, a line's start, as for a page read from its
// middle: its events' offsets count from there. It returns the parser, which must not have
// read anything yet.
func (s *Parser) From(offset int64) *Parser {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.pos, s.lineStart = offset, offset

	return s
}

// Flush reads a last line the log never ended, as a run's that has ended may be: the log
// won't grow, so the line is whole.
func (s *Parser) Flush() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.readPending()
}

// Events returns every event the parser keeps, oldest first.
func (s *Parser) Events() []runevent.Event {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]runevent.Event{}, s.events...)
}

// LastWords returns the last thing the agent said, or its latest step: what a run that
// didn't report shows.
func (s *Parser) LastWords() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, v := range slices.Backward(s.events) {
		if v.Kind == runevent.KindText {
			return v.Text
		}
	}

	return s.progress.Step
}

// readPending reads the line held, whole now, unless it ran past maxLine, and starts the
// next one where the parser has read to. The caller holds mu.
func (s *Parser) readPending() {
	if !s.skipping && len(bytes.TrimSpace(s.pending)) > 0 {
		s.line, s.part = s.lineStart, 0
		s.read(s, s.pending)
	}

	s.pending, s.skipping = s.pending[:0], false
	s.lineStart = s.pos
}

// hold keeps part of a line not ended yet, dropping one past maxLine.
func (s *Parser) hold(part []byte) {
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
func (s *Parser) emit(e runevent.Event) {
	e.Text, e.Cut = cut(e.Text, runevent.MaxText)
	e.Offset, e.Part, e.At = s.line, s.part, s.at
	s.part++

	s.seq++
	e.Seq = s.seq
	s.progress.Events = s.seq

	s.events = append(s.events, e)
	if len(s.events) > KeepEvents {
		s.events = append(s.events[:0], s.events[len(s.events)-KeepEvents:]...)
	}
}

// unknown counts a message the parser didn't know.
func (s *Parser) unknown() {
	s.progress.Unknown++
}

// stamp notes when a line says it was written, an RFC 3339 time; anything else is left
// alone, so a line with none keeps the latest time before it.
func (s *Parser) stamp(at string) {
	if at == "" {
		return
	}

	if t, err := time.Parse(time.RFC3339Nano, at); err == nil {
		s.at = t.UTC().Format(time.RFC3339Nano)
	}
}

// step sets the run's latest step.
func (s *Parser) step(text string) {
	if text != "" {
		s.progress.Step, _ = cut(text, 200)
	}
}

// addUsage records an assistant message's usage, by its ID.
func (s *Parser) addUsage(id string, t tokens) {
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
func (s *Parser) progressLocked() runevent.Progress {
	p := s.progress
	p.Todos = append([]runevent.Todo{}, s.progress.Todos...)

	return p
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
