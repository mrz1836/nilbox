package describeit

import (
	"context"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/agentrun/stream"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// How a turn is followed.
const (
	// tailEvery is how often a turn's stream is read.
	tailEvery = 100 * time.Millisecond

	// emitEvery is how often, at most, the window is told a turn moved on.
	emitEvery = 250 * time.Millisecond

	// readChunk is how much of a turn's stream one read takes.
	readChunk = 256 << 10

	// quitWait is how long the app's quit waits for a turn it stopped to end, before it
	// kills what is left of it.
	quitWait = 3 * time.Second
)

// process is a turn's agent, as the follower follows it: *execpath.Proc.
type process interface {
	Done() <-chan struct{}
	Wait() (exitCode int, err error)
	Stop(ladder execpath.Ladder)
}

// liveTurn is the turn running, or starting: what its follower reads, and what ends it.
type liveTurn struct {
	// rec is its conversation, n its number, and log its stream's file; resumed says it
	// took the conversation's session up again rather than starting it.
	rec     *record
	n       int
	log     string
	resumed bool

	// parser reads its stream, and before are the proposals its conversation's folder held
	// as it started.
	parser *stream.Parser
	before map[string]bool

	// proc is its agent once started, set under the service's mu; kill ends what is left
	// of its process group; done closes once its end is recorded.
	proc process
	kill context.CancelFunc
	done chan struct{}

	// stopped says the user stopped it, and quit that the app's quit did.
	stopped atomic.Bool
	quit    atomic.Bool

	// The follower's own: why it stopped the turn (its setup was wrong), the stream open,
	// how far it was read and what it reads into, whether its init was checked, and whether
	// its end was.
	failure string
	reader  *os.File
	offset  int64
	buf     []byte
	checked bool
	over    bool
}

// stop asks the turn's agent to stop, when it has started: the ladder's SIGINT first, which
// ends a turn with its result written (the spike's S7).
func (lt *liveTurn) stop(proc process) {
	if proc != nil {
		proc.Stop(execpath.StopLadder())
	}
}

// follow follows the turn lt until its agent exits: it reads its stream as it grows, checks
// its setup from its init line, stops it once its result is in, tells the window as it
// moves on, at most every emitEvery, and records its end.
func (s *Service) follow(lt *liveTurn) {
	ticker := time.NewTicker(tailEvery)
	defer ticker.Stop()

	last, moved := time.Now(), false

	for {
		select {
		case <-lt.proc.Done():
			s.tail(lt)
			s.check(lt)
			s.end(lt)

			return
		case <-ticker.C:
			if s.tail(lt) {
				moved = true
			}

			s.check(lt)

			if moved && time.Since(last) >= emitEvery {
				s.updated(lt.rec.ID, lt.n)

				last, moved = time.Now(), false
			}
		}
	}
}

// tail feeds the turn's parser what its stream holds past what it has read, and, when it
// grew, notes the proposals its agent made since. It reports whether anything changed.
func (s *Service) tail(lt *liveTurn) bool {
	if lt.reader == nil {
		f, err := paths.OpenRegular(lt.log)
		if err != nil {
			return false
		}

		lt.reader, lt.buf = f, make([]byte, readChunk)
	}

	grew := false

	for {
		n, err := lt.reader.ReadAt(lt.buf, lt.offset)
		if n > 0 {
			_, _ = lt.parser.Write(lt.buf[:n])
			lt.offset += int64(n)
			grew = true
		}

		if err != nil || n < len(lt.buf) {
			break
		}
	}

	if !grew {
		return false
	}

	s.noteProposals(lt)

	return true
}

// check checks the turn's setup once its init line came, stopping it at once when the
// app's own tools aren't all it has, and stops what is left of it once its result is in.
func (s *Service) check(lt *liveTurn) {
	if !lt.checked {
		if in, ok := lt.parser.Init(); ok {
			lt.checked = true

			if why := initProblem(in); why != "" {
				lt.failure = why
				lt.stop(lt.proc)
			}
		}
	}

	if !lt.over && lt.parser.Over() {
		lt.over = true
		lt.stop(lt.proc)
	}
}

// initProblem says what is wrong with the setup a turn's init line shows, or "": the app's
// own MCP server must be connected, and every tool the turn has one of that server's.
func initProblem(in stream.Init) string {
	if !slices.Contains(in.MCPServers, stream.MCPServer{Name: brand.Slug, Status: "connected"}) {
		return brand.Name + "'s Autopilot tools didn't start for this turn, so it stopped before " + agentWords +
			" could go on without them. Try again; if it happens again, restart " + brand.Name + "."
	}

	foreign := foreignTools(in)
	if len(foreign) == 0 {
		return ""
	}

	if len(foreign) > 5 {
		foreign = append(foreign[:5], "more")
	}

	return "This turn had tools that aren't " + brand.Name + "'s (" + strings.Join(foreign, ", ") + "), so it " +
		"stopped before using any."
}

// foreignTools returns the tools and the MCP servers a turn's init line shows that aren't
// the app's own, each as one short line.
func foreignTools(in stream.Init) []string {
	prefix := agent.ConversationToolPrefix(agent.Claude, brand.Slug)
	names := make([]string, 0, len(in.Tools)+len(in.MCPServers))

	for _, tool := range in.Tools {
		if prefix == "" || !strings.HasPrefix(tool, prefix) {
			names = append(names, tool)
		}
	}

	for _, m := range in.MCPServers {
		if m.Name != brand.Slug {
			names = append(names, m.Name)
		}
	}

	for i, name := range names {
		names[i] = textutil.Truncate(textutil.OneLine(name), 60)
	}

	return names
}

// noteProposals records the proposals the turn's agent made so far: the proposal files in
// its conversation's folder that weren't there as it started.
func (s *Service) noteProposals(lt *liveTurn) {
	ids, err := proposalIDs(s.proposalsDir(lt.rec.ID))
	if err != nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	t := lt.rec.turn(lt.n)
	if t == nil {
		return
	}

	added := false

	for _, id := range ids {
		if !lt.before[id] && !slices.Contains(t.Proposals, id) {
			t.Proposals = append(t.Proposals, id)
			added = true
		}
	}

	if added {
		s.saveLocked(lt.rec)
	}
}

// end records how the turn ended, once its agent exited: from its result, or why the
// follower, the user, or the app's quit stopped it, and tells the window.
func (s *Service) end(lt *liveTurn) {
	if lt.reader != nil {
		_ = lt.reader.Close()
	}

	lt.parser.Flush()
	s.noteProposals(lt)

	code, _ := lt.proc.Wait() // the code says it all; the error only wraps it
	lt.kill()

	s.mu.Lock()

	if t := lt.rec.turn(lt.n); t != nil {
		finish(t, lt.parser, ending{
			failure: lt.failure, stopped: lt.stopped.Load(), quit: lt.quit.Load(), code: code, detail: lastLine(lt.log),
		}, s.clock.Now())
	}

	s.noteSession(lt)
	s.saveLocked(lt.rec)

	if s.live == lt {
		s.live = nil
	}

	s.mu.Unlock()

	s.updated(lt.rec.ID, lt.n)
	close(lt.done)
}

// noteSession notes whether the conversation's session started, as the turn lt shows it:
// a turn that got as far as its agent's own start (its init line) leaves it started, so
// the next resumes it. One whose agent ended before that, by itself, may have been refused
// for its session (one already in use, or none to resume), so the next turn names the
// session the other way, and a conversation never sticks on it. The caller holds mu.
func (s *Service) noteSession(lt *liveTurn) {
	if _, ok := lt.parser.Init(); ok {
		lt.rec.Started = true

		return
	}

	if _, ended := lt.parser.Result(); !ended && !lt.stopped.Load() && !lt.quit.Load() {
		lt.rec.Started = !lt.resumed
	}
}

// ending is what ended a turn besides its stream: why the follower stopped it, whether
// the user or the app's quit did, its agent's exit status, and the last line its agent
// wrote that isn't one of its stream's.
type ending struct {
	failure string
	stopped bool
	quit    bool
	code    int
	detail  string
}

// budgetSubtype is how Claude Code's result names a turn that reached its budget.
const budgetSubtype = "error_max_budget_usd"

// finish records the end of the turn t, read by p, as e says, at now.
func finish(t *turnRecord, p *stream.Parser, e ending, now time.Time) {
	res, ok := p.Result()
	progress := p.Progress()

	t.EndedAt = now.UTC()
	t.TokensIn, t.TokensOut = progress.TokensIn, progress.TokensOut

	if ok {
		t.Reply = res.Message
		t.Cost, t.Total, t.Estimated = res.TurnCost, res.Cost, res.Estimated
		t.TokensIn, t.TokensOut = res.TokensIn, res.TokensOut
	}

	switch {
	case e.failure != "":
		t.State, t.Error = TurnFailed, e.failure
	case ok && !res.IsError:
		t.State, t.Error = TurnDone, ""
	case e.stopped || e.quit:
		t.State, t.Error = TurnStopped, ""
	default:
		t.State, t.Error = TurnFailed, failureWords(res, ok, p.Limit(), e)
	}
}

// failureWords says why a turn that wasn't stopped ended without finishing: its result's
// error, or, with none, its agent's exit.
func failureWords(res stream.Result, ok bool, limit *stream.Limit, e ending) string {
	switch {
	case limit != nil && limit.Status == stream.LimitRejected:
		return agentWords + "'s usage limit is reached, so the turn stopped. Try again once it resets."
	case ok && res.Subtype == budgetSubtype:
		return "The turn reached its budget of " + budgetWords() + " before it finished. Ask again for less, or " +
			"carry on in your next message."
	case ok:
		why := strings.ReplaceAll(res.Subtype, "_", " ")
		if len(res.Errors) > 0 {
			why = textutil.Truncate(textutil.OneLine(res.Errors[0]), 200)
		}

		return textutil.Sentence(agentWords + " ended the turn in error: " + why)
	case e.detail != "":
		return textutil.Sentence(agentWords + " ended without finishing the turn: " + e.detail)
	case e.code > 0:
		return agentWords + " ended without finishing the turn (exit status " + strconv.Itoa(e.code) + ")."
	default:
		return agentWords + " ended without finishing the turn."
	}
}

// lastLine returns the last line of the stream at path that isn't one of the stream's own
// JSON lines, one line of at most 200 characters: what the agent said as it failed to
// start, such as an option it refused. "" for none.
func lastLine(path string) string {
	tail, _, err := paths.ReadTail(path, 8<<10)
	if err != nil {
		return ""
	}

	lines := strings.Split(string(tail), "\n")

	for _, raw := range slices.Backward(lines) {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "{") {
			continue
		}

		return textutil.Truncate(textutil.OneLine(line), 200)
	}

	return ""
}

// updated tells the window the conversation id changed, in its turn n, 0 for the
// conversation as a whole.
func (s *Service) updated(id string, n int) {
	s.sink.Emit(event.DescribeItUpdated{ConversationID: id, Turn: n})
}

// saveLocked writes the record, logging a failure: what the window shows comes from the
// record in memory, and the next write carries the change. The caller holds mu.
func (s *Service) saveLocked(r *record) {
	if err := s.saveRecord(r); err != nil {
		s.logger.Warn("describe it couldn't save a conversation", "conversation", r.ID, "error", err)
	}
}

// readAll feeds p the whole of the stream at path, its last line too.
func readAll(p *stream.Parser, path string) {
	f, err := paths.OpenRegular(path)
	if err == nil {
		_, _ = io.Copy(p, f)
		_ = f.Close()
	}

	p.Flush()
}
