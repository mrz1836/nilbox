package agentrun

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agentrun/runevent"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// narration returns n lines of Claude Code's stream, each an assistant's message saying
// "Step <i>", written a second apart from start.
func narration(t *testing.T, start time.Time, from, n int) string {
	t.Helper()

	var b strings.Builder

	for i := from; i < from+n; i++ {
		line, err := json.Marshal(map[string]any{
			"type": "assistant", "timestamp": start.Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano),
			"message": map[string]any{
				"id": fmt.Sprintf("msg_%d", i), "content": []map[string]any{{"type": "text", "text": fmt.Sprintf("Step %d", i)}},
				"usage": map[string]any{"input_tokens": 1, "output_tokens": 1},
			},
		})
		require.NoError(t, err)

		b.Write(line)
		b.WriteByte('\n')
	}

	return b.String()
}

// hugeResult is a user's message holding a tool's result of size bytes, as a tool that
// prints a whole log gives back.
func hugeResult(t *testing.T, size int) string {
	t.Helper()

	line, err := json.Marshal(map[string]any{
		"type": "user", "message": map[string]any{
			"content": []map[string]any{{"type": "tool_result", "content": strings.Repeat("x", size)}},
		},
	})
	require.NoError(t, err)

	return string(line) + "\n"
}

// pageAll reads the run's timeline as the run view does: its last page, then back a page at
// a time to its start. It returns every page.
func pageAll(t *testing.T, r *rig, runID int64) []runevent.Page {
	t.Helper()

	page, err := r.runner.Events(t.Context(), runID, 0)
	require.NoError(t, err)

	pages := []runevent.Page{page}

	for page.Earlier {
		before := page.Before

		page, err = r.runner.EventsBefore(t.Context(), runID, before)
		require.NoError(t, err)

		for _, e := range page.Events {
			require.Less(t, e.Offset, before, "a page holds only what is before the one after it")
		}

		require.Less(t, len(pages), 100, "the pages reach the log's start")

		pages = append(pages, page)
	}

	return pages
}

func TestTheRunViewPagesALongLogBack(t *testing.T) {
	t.Parallel()

	const steps = 1200

	r := newRig(t)
	start := time.Date(2026, 11, 16, 6, 15, 0, 0, time.UTC)

	stdout := lines(t, "success-structured.jsonl", 1) + narration(t, start, 0, steps/2) +
		hugeResult(t, 5<<20) + narration(t, start, steps/2, steps/2) + lastLine(t, "success-structured.jsonl")
	plan := r.plan(testutil.FakeRun{Stdout: stdout})
	r.start(plan)
	r.end(plan.RunID)

	pages := pageAll(t, r, plan.RunID)

	last := pages[0]
	assert.True(t, last.Done)
	require.NotNil(t, last.Ending, "an ended run says how it came out")
	assert.True(t, last.Ending.Reported, "its report, from its record")
	assert.NotEmpty(t, last.Ending.Summary)
	require.NotEmpty(t, last.Events)
	assert.Equal(t, runevent.KindResult, last.Events[len(last.Events)-1].Kind, "the last page ends with the result")
	assert.Greater(t, len(pages), steps/pageLines, "a long log is read a page at a time, never whole")

	var (
		texts []string
		huge  *runevent.Event
	)

	seen := map[[2]int64]bool{}

	for i := len(pages) - 1; i >= 0; i-- {
		assert.LessOrEqual(t, len(pages[i].Events), 2*pageLines, "each page is a page's worth")

		for _, e := range pages[i].Events {
			key := [2]int64{e.Offset, int64(e.Part)}
			require.False(t, seen[key], "no event shows twice")
			seen[key] = true

			switch {
			case e.Kind == runevent.KindText && strings.HasPrefix(e.Text, "Step "):
				texts = append(texts, e.Text)
			case e.Kind == runevent.KindToolResult:
				huge = &e
			}
		}
	}

	require.Len(t, texts, steps, "every step, once, across the pages")

	for i, text := range texts {
		assert.Equal(t, fmt.Sprintf("Step %d", i), text, "oldest first")
	}

	require.NotNil(t, huge, "a 5 MB line is read whole, and its event kept")
	assert.True(t, huge.Cut)
	assert.LessOrEqual(t, len(huge.Text), runevent.MaxText)

	first := pages[len(pages)-1]
	assert.False(t, first.Earlier)
	assert.Equal(t, runevent.KindStarted, first.Events[0].Kind, "the first page starts with the run's start")
	assert.Equal(t, start.Format(time.RFC3339Nano), first.Events[1].At, "each event carries when the agent wrote it")
}

func TestALiveRunsEarlierEventsAreReadFromItsLog(t *testing.T) {
	t.Parallel()

	const steps = keepEvents + 300

	r := newRig(t)
	start := time.Date(2026, 11, 16, 6, 15, 0, 0, time.UTC)

	plan := r.plan(testutil.FakeRun{Stdout: narration(t, start, 0, steps), Sleep: time.Minute})
	r.start(plan)

	var page runevent.Page

	require.Eventually(t, func() bool {
		got, err := r.runner.Events(t.Context(), plan.RunID, 0)
		page = got

		return err == nil && got.Progress.Events == steps
	}, waitFor, 10*time.Millisecond, "the run's stream is read")

	assert.False(t, page.Done)
	assert.Nil(t, page.Ending, "a run still going has no ending")
	assert.Len(t, page.Events, keepEvents, "memory keeps the last events")
	require.True(t, page.Earlier, "the rest are its log's")
	assert.Equal(t, page.Events[0].Offset, page.Before)
	assert.Equal(t, fmt.Sprintf("Step %d", steps-keepEvents), page.Events[0].Text)

	earlier, err := r.runner.EventsBefore(t.Context(), plan.RunID, page.Before)
	require.NoError(t, err)
	require.NotEmpty(t, earlier.Events)
	assert.False(t, earlier.Done)
	assert.Equal(t, fmt.Sprintf("Step %d", steps-keepEvents-1), earlier.Events[len(earlier.Events)-1].Text,
		"the page before memory's first event ends just before it")

	after, err := r.runner.Events(t.Context(), plan.RunID, page.Events[len(page.Events)-1].Seq)
	require.NoError(t, err)
	assert.Empty(t, after.Events, "following asks only for what is new")

	require.NoError(t, r.runner.Stop(t.Context(), plan.RunID))
	r.end(plan.RunID)
}

func TestWaitReturnsOnceTheRunsEndIsTold(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	plan := r.plan(testutil.FakeRun{
		Stdout: lines(t, "sigint.jsonl", 2), Sleep: time.Minute,
		OnInterrupt: &testutil.FakeInterrupt{Stdout: lastLine(t, "sigint.jsonl")},
	})
	r.start(plan)

	require.NoError(t, r.runner.Wait(t.Context(), 999), "a run the runner doesn't follow has nothing to wait for")

	short, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	require.ErrorIs(t, r.runner.Wait(short, plan.RunID), context.DeadlineExceeded, "it waits while the run works")

	var (
		mu     sync.Mutex
		states []SessionState
	)

	waited := make(chan error, 1)

	go func() {
		err := r.runner.Wait(t.Context(), plan.RunID)

		mu.Lock()
		states = r.sessions.of(plan.SessionID)
		mu.Unlock()

		waited <- err
	}()

	require.NoError(t, r.runner.Stop(t.Context(), plan.RunID))

	select {
	case err := <-waited:
		require.NoError(t, err)
	case <-time.After(waitFor):
		t.Fatal("Wait never returned")
	}

	mu.Lock()
	defer mu.Unlock()

	assert.Contains(t, states, SessionEnded, "by then its session is recorded as ended, so it can be resumed")
	assert.Equal(t, StatusStopped, r.end(plan.RunID).Outcome.Status)
}

func TestTakenOfNamesTheRunAndItsFence(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	plan := r.plan(testutil.FakeRun{Stdout: agentLinesOf(t, "success-structured.jsonl")})
	id := r.start(plan)
	r.end(plan.RunID)

	taken, ok := r.runner.TakenOf(t.Context(), id)
	require.True(t, ok)
	assert.Equal(t, Taken{RunID: plan.RunID, Preset: "readOnly", Folder: plan.Folder}, taken,
		"read from the run's record once it ended")

	_, ok = r.runner.TakenOf(t.Context(), id+100)
	assert.False(t, ok)
}

// agentLinesOf returns a whole fixture.
func agentLinesOf(t *testing.T, name string) string {
	t.Helper()

	return string(fixture(t, name))
}

// fakeGit answers a run's reads of its worktree.
type fakeGit struct {
	mu    sync.Mutex
	diffs []string
}

func (f *fakeGit) Head(context.Context, string) (string, error) { return "", nil }

func (f *fakeGit) ChangedFiles(context.Context, string, string, string) ([]string, error) {
	return []string{}, nil
}

func (f *fakeGit) WorktreeDiff(_ context.Context, dir, from string) (gitread.WorktreeDiff, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.diffs = append(f.diffs, dir+"@"+from)

	return gitread.WorktreeDiff{Patch: "+fixed\n", Untracked: []string{"new.txt"}}, nil
}

func TestDiffReadsTheRunsWorktreeSinceItsHead(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	git := &fakeGit{}
	r.runner.git = git

	plan := r.plan(testutil.FakeRun{Stdout: agentLinesOf(t, "success-structured.jsonl")})
	plan.HeadBefore = strings.Repeat("a", 40)
	r.start(plan)
	r.end(plan.RunID)

	diff, err := r.runner.Diff(t.Context(), plan.RunID)
	require.NoError(t, err)
	assert.Equal(t, runevent.Diff{From: plan.HeadBefore, Patch: "+fixed\n", Untracked: []string{"new.txt"}}, diff)
	assert.Equal(t, []string{plan.Dir + "@" + plan.HeadBefore}, git.diffs, "the run's worktree, since its head")

	_, err = r.runner.Diff(t.Context(), 999)
	require.ErrorIs(t, err, ErrNoRun)

	noHead := r.plan(testutil.FakeRun{Stdout: agentLinesOf(t, "success-structured.jsonl")})
	r.start(noHead)
	r.end(noHead.RunID)

	_, err = r.runner.Diff(t.Context(), noHead.RunID)
	require.ErrorIs(t, err, ErrNoHead)
}
