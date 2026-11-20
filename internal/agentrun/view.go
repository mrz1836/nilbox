package agentrun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/agentrun/runevent"
	"github.com/mrz1836/nilbox/internal/scriptrun"
)

var (
	// ErrNoGit is returned for a run's Diff by a runner given no Git.
	ErrNoGit = errors.New("the runner reads no git")

	// ErrNoHead is returned for the Diff of a run whose record names no head it started on,
	// or no worktree, as one from an older build.
	ErrNoHead = errors.New("the run names no head it started on")
)

// What the run view reads of a run, by the ledger's run: its events, from memory while the
// runner follows it and a page at a time from its log otherwise, so a long log never loads
// whole; how it came out; its worktree's changes since it started; and, for Take over, its
// end and its fence.

const (
	// pageLines is about how many of a log's lines one page reads, and pageBytes the most
	// of it, but for a line longer still: a tool's result can hold a whole log.
	pageLines = 200
	pageBytes = 1 << 20

	// pageChunk is how much of a log one read takes, scriptrun's largest.
	pageChunk = 256 << 10
)

// Events returns the ledger's run's events after the one numbered after: from memory while
// the runner follows it, with how far it has got; else its log's last page, with how it
// came out, which Earlier and Before read back from.
func (r *Runner) Events(ctx context.Context, runID, after int64) (runevent.Page, error) {
	r.mu.Lock()
	rn, ok := r.byLedger[runID]
	r.mu.Unlock()

	if ok {
		return rn.parser.Page(after), nil
	}

	return r.EventsBefore(ctx, runID, 0)
}

// EventsBefore returns the page of the ledger's run's events read from its log, of the
// lines before the offset before (a line's start, as Page.Before gives it), or its log's
// last page for 0: a chunk at a time back from there until it holds about pageLines lines,
// or pageBytes, or reaches the log's start. A run that ended says how it came out.
func (r *Runner) EventsBefore(ctx context.Context, runID, before int64) (runevent.Page, error) {
	id, found := r.ScriptRun(ctx, runID)
	if !found {
		return runevent.Page{}, fmt.Errorf("%w: run %d", ErrNoRun, runID)
	}

	rec, err := r.runs.Run(ctx, id)
	if err != nil {
		return runevent.Page{}, err
	}

	intent := intentIn(rec.Result.Intent)
	ended := !rec.Status.Live()

	page, err := r.logPage(ctx, id, intent.Agent, before, ended)
	if err != nil {
		return runevent.Page{}, err
	}

	if ended {
		page.Done = true
		page.Ending, page.Progress = endingOf(rec, page.Progress)
	}

	return page, nil
}

// logPage reads the page of the run id's log before the offset before, its end for 0 or
// past it. The lines a page reads are whole: the first one that starts before the page is
// left for the page before it. A run that ended reads its last line even when it never
// ended it.
func (r *Runner) logPage(ctx context.Context, id int64, agentID agent.ID, before int64, ended bool) (runevent.Page, error) {
	tail, err := r.runs.Output(ctx, id, -1, 1)
	if err != nil {
		return runevent.Page{}, err
	}

	end := before
	if end <= 0 || end > tail.Size {
		end = tail.Size
	}

	buf, start, err := r.readBack(ctx, id, end)
	if err != nil {
		return runevent.Page{}, err
	}

	if start > 0 {
		i := bytes.IndexByte(buf, '\n')
		buf, start = buf[i+1:], start+int64(i)+1
	}

	buf, start = lastLines(buf, start, pageLines)

	parser := parserFor(agentID, "").From(start)
	_, _ = parser.Write(buf)

	if ended && end == tail.Size {
		parser.Flush()
	}

	page := runevent.Page{
		Events: parser.Events(), Earlier: start > 0, Before: start, Progress: parser.Progress(),
	}

	for i := range page.Events {
		page.Events[i].Seq = 0
	}

	return page, nil
}

// readBack reads the run id's log back from end, a chunk at a time, until what it read
// holds about pageLines whole lines, or pageBytes and at least one, or starts at the log's
// start. It returns the bytes and where they start.
func (r *Runner) readBack(ctx context.Context, id, end int64) ([]byte, int64, error) {
	var (
		chunks         []string
		size, newlines int
		start          = end
	)

	for start > 0 && !fullPage(size, newlines) {
		text, from, err := r.chunkBefore(ctx, id, start)
		if err != nil {
			return nil, 0, err
		}

		if text == "" {
			break
		}

		chunks = append(chunks, text)
		size, newlines, start = size+len(text), newlines+strings.Count(text, "\n"), from
	}

	if start > 0 && newlines == 0 {
		// One line runs past every byte read, back to the log's start.
		return nil, end, nil
	}

	slices.Reverse(chunks)

	return []byte(strings.Join(chunks, "")), start, nil
}

// chunkBefore reads the run id's log's chunk that ends at end, and where it starts: "" once
// nothing more can be read before end.
func (r *Runner) chunkBefore(ctx context.Context, id, end int64) (string, int64, error) {
	from := max(0, end-pageChunk)

	chunk, err := r.runs.Output(ctx, id, from, end-from)
	if err != nil {
		return "", 0, err
	}

	if chunk.Offset >= end {
		return "", end, nil
	}

	return chunk.Text[:min(int64(len(chunk.Text)), end-chunk.Offset)], chunk.Offset, nil
}

// lastLines returns the last n lines of whole lines that start at start, and where they
// start: a chunk read back holds more lines than a page shows.
func lastLines(buf []byte, start int64, n int) ([]byte, int64) {
	end := len(buf)
	if end > 0 && buf[end-1] == '\n' {
		end-- // the last line's own end
	}

	for i := end - 1; i >= 0; i-- {
		if buf[i] != '\n' {
			continue
		}

		if n--; n == 0 {
			return buf[i+1:], start + int64(i) + 1
		}
	}

	return buf, start
}

// fullPage reports size bytes read back from a line's start, holding newlines line ends,
// that make a page: about pageLines whole lines, or pageBytes and at least one. The first
// line may have started before them.
func fullPage(size, newlines int) bool {
	whole := newlines - 1

	return whole >= pageLines || (size >= pageBytes && whole >= 1)
}

// endingOf reads how a run came out from its record, and its progress as it ended, over
// progress, what its log's page counted: the tokens and the todo list are the run's whole.
func endingOf(rec scriptrun.Run, progress runevent.Progress) (*runevent.Ending, runevent.Progress) {
	out := outcomeIn(rec.Result.Outcome)

	e := &runevent.Ending{
		Summary: out.Summary, NoReport: out.NoReport, Commit: out.Commit, Changed: nonNilStrings(out.Changed),
		Todos: nonNilTodos(out.Todos), Models: []string{}, Refused: out.Refused,
	}

	if e.Summary == "" {
		e.Summary = rec.Summary
	}

	if res := out.Result; res != nil {
		e.Message, e.Turns, e.DurationMS, e.Models = res.Message, res.Turns, res.DurationMS, nonNilStrings(res.Models)
	}

	if rep := out.Report; rep != nil {
		e.Reported, e.Outcome, e.Report, e.Pushed, e.PushedCommit = true, rep.Outcome, rep.Summary, rep.Pushed, rep.PushedCommit
		e.NeedsPerson, e.Reason = rep.NeedsPerson, rep.Reason
	}

	progress.Todos = e.Todos
	progress.TokensIn, progress.TokensOut, progress.CacheRead = out.TokensIn, out.TokensOut, out.CacheRead
	progress.Unknown = max(progress.Unknown, out.Unknown)
	progress.Step = ""

	return e, progress
}

// outcomeIn reads a run's outcome from its record.
func outcomeIn(raw any) Outcome {
	var out Outcome

	body, err := json.Marshal(raw)
	if err == nil {
		_ = json.Unmarshal(body, &out) // a record without one reads as the zero outcome
	}

	return out
}

// Diff returns the ledger's run's worktree's changes since the head it started on,
// committed or not, and the files git doesn't track there, read-only.
func (r *Runner) Diff(ctx context.Context, runID int64) (runevent.Diff, error) {
	if r.git == nil {
		return runevent.Diff{}, ErrNoGit
	}

	id, found := r.ScriptRun(ctx, runID)
	if !found {
		return runevent.Diff{}, fmt.Errorf("%w: run %d", ErrNoRun, runID)
	}

	rec, err := r.runs.Run(ctx, id)
	if err != nil {
		return runevent.Diff{}, err
	}

	in := intentIn(rec.Result.Intent)
	if in.HeadBefore == "" || !filepath.IsAbs(in.Dir) {
		return runevent.Diff{}, fmt.Errorf("%w: run %d", ErrNoHead, runID)
	}

	got, err := r.git.WorktreeDiff(ctx, in.Dir, in.HeadBefore)
	if err != nil {
		return runevent.Diff{}, err
	}

	return runevent.Diff{From: in.HeadBefore, Patch: got.Patch, Cut: got.Cut, Untracked: nonNilStrings(got.Untracked)}, nil
}

// Wait returns once the ledger's run's end is told, or its start failed, or at once for a
// run the runner doesn't follow; or ctx's error once it ends first. Take over waits for it,
// so the run's conversation is whole on disk before it resumes.
func (r *Runner) Wait(ctx context.Context, runID int64) error {
	r.mu.Lock()

	rn := r.byLedger[runID]
	if rn == nil {
		rn = r.starting[runID]
	}

	r.mu.Unlock()

	if rn == nil {
		return nil
	}

	select {
	case <-rn.done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for the run to end: %w", ctx.Err())
	}
}

// Taken is a background run as Take over needs it: the ledger's run, and the fence its
// conversation resumes under.
type Taken struct {
	// RunID is the ledger's run.
	RunID int64

	// Preset is its tools preset, in the catalog's words, and Folder its own folder, which
	// holds its fence's settings.
	Preset string
	Folder string
}

// TakenOf returns the background run id in the run history is, for Take over, and whether
// the history holds it as an agent's run.
func (r *Runner) TakenOf(ctx context.Context, id int64) (Taken, bool) {
	r.mu.Lock()
	rn := r.byScript[id]
	r.mu.Unlock()

	if rn != nil {
		return Taken{RunID: rn.intent.RunID, Preset: rn.intent.Fence.Preset, Folder: rn.intent.Folder}, true
	}

	rec, err := r.runs.Run(ctx, id)
	if err != nil || rec.Kind != scriptrun.KindAgent {
		return Taken{}, false
	}

	in := intentIn(rec.Result.Intent)

	return Taken{RunID: in.RunID, Preset: in.Fence.Preset, Folder: in.Folder}, in.RunID > 0
}

// nonNilStrings returns list, or an empty list for nil.
func nonNilStrings(list []string) []string {
	if list == nil {
		return []string{}
	}

	return list
}

// nonNilTodos returns list, or an empty list for nil.
func nonNilTodos(list []runevent.Todo) []runevent.Todo {
	if list == nil {
		return []runevent.Todo{}
	}

	return list
}
