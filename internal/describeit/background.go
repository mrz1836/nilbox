package describeit

import (
	"context"
	"os"
	"time"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/agentrun/stream"
)

// The background work of the conversation's half: readying the folder as the app starts,
// writing the snapshot again after each burst of changes, and stopping a turn as the app
// quits.

const (
	// contextQuiet is how long the snapshot's writer waits after a change for the burst it
	// starts to end, and contextMaxWait the longest it waits in all, so a steady stream of
	// changes still writes the snapshot now and then.
	contextQuiet   = 500 * time.Millisecond
	contextMaxWait = 3 * time.Second

	// killWait is how long the app's quit waits for a turn it killed to be recorded.
	killWait = time.Second
)

// start readies the describe folder as the app starts: a turn a crash left running closed
// from its stream, conversations older than 30 days pruned, and the snapshot and the
// instructions written.
func (s *Service) start(ctx context.Context) {
	for _, dir := range []string{s.cfg.Dir, s.workDir(), s.path(conversationsName), s.proposalsDir(Outside)} {
		if err := ensureDir(dir); err != nil {
			s.logger.WarnContext(ctx, "describe it couldn't make its folder", "error", err)
		}
	}

	s.tidyConversations(ctx)
	s.pruneOutside(ctx)

	if err := s.writeInstructions(); err != nil {
		s.logger.WarnContext(ctx, "describe it couldn't write its instructions", "error", err)
	}

	if err := s.writeContext(ctx); err != nil {
		s.logger.WarnContext(ctx, "describe it couldn't write the snapshot", "error", err)
	}
}

// tidyConversations removes the conversations that started more than keepConversations
// ago, with their proposals, and closes a turn a crash left running in any other, from
// its stream. The turn running now is left alone.
func (s *Service) tidyConversations(ctx context.Context) {
	now := s.clock.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	current := s.currentLocked()
	busy := s.busyLocked(current)
	kept := map[string]bool{}

	for _, id := range s.conversationIDs() {
		kept[id] = true

		if id != busy {
			s.tidyLocked(ctx, id, current, now)
		}
	}

	s.pruneOrphans(ctx, kept, now)
}

// busyLocked returns the conversation of the turn running, or getting ready in current,
// "" for none: its record is that turn's to change. The caller holds mu.
func (s *Service) busyLocked(current *record) string {
	switch {
	case s.live == nil:
		return ""
	case s.live.rec != nil:
		return s.live.rec.ID
	case current != nil:
		return current.ID
	default:
		return ""
	}
}

// tidyLocked removes the conversation id when it started more than keepConversations
// before now, and else closes a turn a crash left running in it. The caller holds mu.
func (s *Service) tidyLocked(ctx context.Context, id string, current *record, now time.Time) {
	rec := current
	if rec == nil || rec.ID != id {
		var err error
		if rec, err = s.readRecord(id); err != nil {
			s.logger.WarnContext(ctx, "describe it couldn't read a conversation", "conversation", id, "error", err)

			return
		}
	}

	if !rec.expired(now) {
		if s.closeInterrupted(rec) {
			s.saveLocked(rec)
		}

		return
	}

	s.removeConversation(id)

	if s.rec == rec {
		s.rec = nil
	}
}

// pruneOrphans removes the proposal folders of conversations no longer kept, once they are
// older than keepConversations.
func (s *Service) pruneOrphans(ctx context.Context, kept map[string]bool, now time.Time) {
	entries, err := os.ReadDir(s.path(proposalsName))
	if err != nil {
		return
	}

	for _, e := range entries {
		id := e.Name()
		if !e.IsDir() || kept[id] || !agent.ValidSessionID(id) || !olderThan(e, now, keepConversations) {
			continue
		}

		if err = os.RemoveAll(s.proposalsDir(id)); err != nil {
			s.logger.WarnContext(ctx, "describe it couldn't remove old proposals", "conversation", id, "error", err)
		}
	}
}

// olderThan reports the entry e last changed more than d before now.
func olderThan(e os.DirEntry, now time.Time, d time.Duration) bool {
	info, err := e.Info()

	return err == nil && now.Sub(info.ModTime()) > d
}

// closeInterrupted closes each turn of rec its record says runs, as a crash or a kill left
// it, from its stream, and reports whether there was one.
func (s *Service) closeInterrupted(rec *record) bool {
	closed := false

	for i := range rec.Turns {
		if rec.Turns[i].State == TurnRunning {
			s.closeFromLog(rec, &rec.Turns[i])

			closed = true
		}
	}

	return closed
}

// closeFromLog records how the turn t of rec ended as its stream says: from its result,
// or as stopped by the app's quit when its stream has none, with the proposals its agent
// made that no turn records.
func (s *Service) closeFromLog(rec *record, t *turnRecord) {
	log := s.turnLog(rec.ID, t.N)

	p := stream.Claude(s.workDir())
	readAll(p, log)

	e := ending{detail: lastLine(log)}

	if in, ok := p.Init(); ok {
		e.failure = initProblem(in)
		rec.Started = true
	}

	if _, ok := p.Result(); !ok {
		e.quit = true
	}

	ended := s.clock.Now()
	if info, err := os.Stat(log); err == nil && info.ModTime().After(t.StartedAt) {
		ended = info.ModTime()
	}

	finish(t, p, e, ended)

	known := rec.proposals()

	ids, err := proposalIDs(s.proposalsDir(rec.ID))
	if err != nil {
		return
	}

	for _, id := range ids {
		if _, ok := known[id]; !ok {
			t.Proposals = append(t.Proposals, id)
		}
	}
}

// keepContext writes the snapshot again after each burst of changes, until ctx ends.
func (s *Service) keepContext(ctx context.Context, changed <-chan struct{}) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-changed:
		}

		if !settle(ctx, changed) {
			return
		}

		if err := s.writeContext(ctx); err != nil && ctx.Err() == nil {
			s.logger.WarnContext(ctx, "describe it couldn't write the snapshot", "error", err)
		}
	}
}

// settle waits for a burst of changes to end: contextQuiet with none, or contextMaxWait in
// all. It reports false once ctx ends.
func settle(ctx context.Context, changed <-chan struct{}) bool {
	quiet := time.NewTimer(contextQuiet)
	defer quiet.Stop()

	most := time.NewTimer(contextMaxWait)
	defer most.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-changed:
			quiet.Reset(contextQuiet)
		case <-quiet.C:
			return true
		case <-most.C:
			return true
		}
	}
}

// shutdown stops a turn still running as the app quits: the agent is asked to stop, and
// what is left of it is killed once it has had quitWait to. No turn starts after.
func (s *Service) shutdown() {
	s.mu.Lock()

	s.closing = true

	lt := s.live
	if lt == nil {
		s.mu.Unlock()

		return
	}

	lt.quit.Store(true)
	proc := lt.proc

	s.mu.Unlock()

	lt.stop(proc)

	if waitFor(lt.done, quitWait) {
		return
	}

	s.mu.Lock()
	kill := lt.kill
	s.mu.Unlock()

	if kill != nil {
		kill()
	}

	if !waitFor(lt.done, killWait) {
		s.logger.Warn("describe it's turn didn't end as the app quit")
	}
}

// waitFor waits for done to close, at most d, and reports whether it did.
func waitFor(done <-chan struct{}, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}
