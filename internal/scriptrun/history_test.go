package scriptrun

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

func TestRetentionDropsTheOldestFinishedRunsFirst(t *testing.T) {
	t.Parallel()

	// Newest first: a run going, then finished runs of 10 bytes each, and the oldest one
	// going too.
	recs := []Record{
		{ID: 7, Status: StatusRunning},
		{ID: 6, Status: StatusSucceeded, LogBytes: 10},
		{ID: 5, Status: StatusFailed, LogBytes: 10},
		{ID: 4, Status: StatusStopped, LogBytes: 10},
		{ID: 3, Status: StatusInterrupted, LogBytes: 10},
		{ID: 2, Status: StatusSucceeded, LogBytes: 10},
		{ID: 1, Status: StatusOrphaned},
	}
	size := func(id int64) int64 { return map[int64]int64{7: 5, 1: 100}[id] }

	assert.Empty(t, retention{runs: 7, bytes: 1000}.drop(recs, size), "within both limits")
	assert.Equal(t, []int64{2, 3, 4}, retention{runs: 4, bytes: 1000}.drop(recs, size),
		"the oldest finished go first, and a run going never does")
	assert.Equal(t, []int64{2, 3, 4}, retention{runs: 100, bytes: 125}.drop(recs, size),
		"until what is left fits: 5 + 100 for the runs going, and two logs of 10")
	assert.Equal(t, []int64{2, 3, 4, 5}, retention{runs: 100, bytes: 120}.drop(recs, size),
		"a run going counts by its log so far")
	assert.Equal(t, []int64{2, 3, 4, 5, 6}, retention{runs: 1, bytes: 0}.drop(recs, size),
		"when only runs going are left, nothing more goes")
}

func TestTheHistoryKeepsItsNewestRuns(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.e.keep = retention{runs: 3, bytes: KeepLogBytes}

	going := h.start(h.spec(testutil.FakeRun{Stdout: readyLine, Sleep: time.Minute}))
	h.waitReady(going)

	finished := make([]int64, 0, 4)

	for i := range 4 {
		id := h.start(h.spec(testutil.FakeRun{Stdout: "run " + strconv.Itoa(i) + "\n"}))
		h.ended(id)

		finished = append(finished, id)
	}

	require.Eventually(t, func() bool { return h.count() == 3 }, wait, poll)

	runs, err := h.s.Runs(t.Context(), Filter{})
	require.NoError(t, err)

	ids := make([]int64, 0, len(runs))
	for _, r := range runs {
		ids = append(ids, r.ID)
	}

	assert.Equal(t, []int64{finished[3], finished[2], going}, ids, "the oldest finished runs went, the run going stayed")

	for _, gone := range finished[:2] {
		_, statErr := os.Stat(h.e.files.log(gone))
		require.ErrorIs(t, statErr, os.ErrNotExist, "run %d's log went with it", gone)
	}

	assert.Equal(t, readyLine, h.logText(going))
}

func TestTheHistoryKeepsItsLogsUnderTheirLimit(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.e.keep = retention{runs: KeepRuns, bytes: 25}

	ids := make([]int64, 0, 4)

	for range 4 {
		id := h.start(h.spec(testutil.FakeRun{Stdout: "123456789\n"}))
		h.ended(id)

		ids = append(ids, id)
	}

	require.Eventually(t, func() bool { return h.count() == 2 }, wait, poll, "two logs of 10 bytes fit in 25")

	_, err := h.e.store.get(t.Context(), ids[1])
	require.ErrorIs(t, err, ErrNotFound)

	_, err = h.e.store.get(t.Context(), ids[3])
	require.NoError(t, err)
}

func TestALongLogIsCutToItsHeadAndTail(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "1.log")

	var body strings.Builder
	for i := range 100 {
		body.WriteString("line " + strconv.Itoa(i) + "\n")
	}

	testutil.WriteFile(t, path, body.String())

	c := compaction{over: 200, head: 30, tail: 40}

	size, err := c.compact(path)
	require.NoError(t, err)

	got, err := os.ReadFile(path) //nolint:gosec // the test's own temp file
	require.NoError(t, err)
	assert.Equal(t, int64(len(got)), size)

	text := string(got)
	assert.True(t, strings.HasPrefix(text, "line 0\nline 1\nline 2\nline 3\n"), "the head ends at a line break: %q", text)
	assert.NotContains(t, text, "line 4\n", "the head stays within its size")
	assert.True(t, strings.HasSuffix(text, "line 96\nline 97\nline 98\nline 99\n"), "the tail starts at a line: %q", text)
	assert.NotContains(t, text, "line 95", "the tail stays within its size")
	assert.Contains(t, text, "\n[")
	assert.Contains(t, text, "of output left out here: a long log keeps its first 30 bytes and its last 40 bytes.]\n")

	_, err = os.Stat(filepath.Join(filepath.Dir(path), "1.log.compact"))
	require.ErrorIs(t, err, os.ErrNotExist, "nothing is left beside it")

	again, err := c.compact(path)
	require.NoError(t, err)
	assert.Equal(t, size, again, "a log within the limit is left as it is")

	missing, err := c.compact(filepath.Join(t.TempDir(), "2.log"))
	require.NoError(t, err)
	assert.Zero(t, missing)
}

func TestACompactionCutsBetweenCharactersWithoutLineBreaks(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "1.log")
	testutil.WriteFile(t, path, strings.Repeat("✓", 100)) // three bytes each, no line break

	size, err := compaction{over: 50, head: 10, tail: 20}.compact(path)
	require.NoError(t, err)

	got, err := os.ReadFile(path) //nolint:gosec // the test's own temp file
	require.NoError(t, err)
	assert.Equal(t, int64(len(got)), size)
	assert.True(t, utf8.Valid(got), "no character was cut in two: %q", got)
	assert.True(t, strings.HasPrefix(string(got), "✓✓✓\n["), "%q", got)
	assert.True(t, strings.HasSuffix(string(got), "]\n✓✓✓✓✓✓"), "%q", got)
}

func TestARunsLongLogIsCutWhenItEnds(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.e.compaction = compaction{over: 100, head: 20, tail: 40}

	out := &collector{}
	lines := strings.Repeat("0123456789\n", 30)

	spec := h.spec(testutil.FakeRun{Stdout: lines})
	spec.Output = out

	id := h.start(spec)
	rec := h.ended(id)

	assert.Equal(t, lines, out.String(), "the reader read the whole log before it was cut")

	text := h.logText(id)
	assert.Equal(t, int64(len(text)), rec.LogBytes)
	assert.Less(t, len(text), len(lines))
	assert.Contains(t, text, "of output left out here")
}

func TestOutputPagesThroughALog(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	var body strings.Builder
	for i := range 300 {
		body.WriteString("line " + strconv.Itoa(i) + " héllo wörld ✓ 🎉\n")
	}

	id := h.start(h.spec(testutil.FakeRun{Stdout: body.String()}))
	h.ended(id)

	var (
		got    strings.Builder
		offset int64
		pages  int
	)

	for {
		chunk, err := h.s.Output(t.Context(), id, offset, 101)
		require.NoError(t, err)
		require.True(t, utf8.ValidString(chunk.Text), "page %d is cut between characters", pages)
		require.Equal(t, offset, chunk.Offset)
		require.Equal(t, int64(body.Len()), chunk.Size)

		got.WriteString(chunk.Text)
		offset = chunk.Next
		pages++

		if chunk.Done {
			break
		}

		require.Greater(t, chunk.Next, chunk.Offset, "each page moves on")
		require.Less(t, pages, 1000)
	}

	assert.Equal(t, body.String(), got.String())
	assert.Greater(t, pages, 1)

	tail, err := h.s.Output(t.Context(), id, -50, 50)
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(body.String(), tail.Text), "a negative offset reads the end")
	assert.True(t, utf8.ValidString(tail.Text))
	assert.True(t, tail.Done)
	assert.LessOrEqual(t, len(tail.Text), 50)

	past, err := h.s.Output(t.Context(), id, int64(body.Len())+10, 50)
	require.NoError(t, err)
	assert.True(t, past.Restart, "an offset past the end, as after a log was cut, starts over")
	assert.Empty(t, past.Text)

	whole, err := h.s.Output(t.Context(), id, 0, 0)
	require.NoError(t, err)
	assert.Equal(t, body.String(), whole.Text, "a limit of 0 reads up to 256 KB")

	_, err = h.s.Output(t.Context(), id+100, 0, 10)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestOutputOfARunGoing(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	id := h.start(h.spec(testutil.FakeRun{Stdout: readyLine, Sleep: time.Minute}))
	h.waitReady(id)

	chunk, err := h.s.Output(t.Context(), id, 0, 0)
	require.NoError(t, err)
	assert.Equal(t, readyLine, chunk.Text)
	assert.False(t, chunk.Done, "more may come")

	run, err := h.s.Run(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, int64(len(readyLine)), run.LogBytes, "a run going reports its log so far")
}

func TestChunkOfHoldsBackAHalfWrittenCharacter(t *testing.T) {
	t.Parallel()

	log := []byte("ok ✓")
	cut := log[:len(log)-1] // the check mark's last byte isn't written yet

	going := chunkOf(bytes.NewReader(cut), int64(len(cut)), 0, 100, false)
	assert.Equal(t, "ok ", going.Text, "a character half written waits for the rest")
	assert.Equal(t, int64(3), going.Next)

	ended := chunkOf(bytes.NewReader(cut), int64(len(cut)), 0, 100, true)
	assert.Equal(t, int64(len(cut)), ended.Next, "at the end of a finished log, what there is is all there is")
	assert.True(t, ended.Done)

	marks := []byte("✓✓")
	fromEnd := chunkOf(bytes.NewReader(marks), int64(len(marks)), -1, 4, true)
	assert.Equal(t, "✓", fromEnd.Text, "a read from the end starts at a character")
	assert.Equal(t, int64(3), fromEnd.Offset)
}

func TestOutputOfALogNotWrittenYet(t *testing.T) {
	t.Parallel()

	chunk, err := readChunk(filepath.Join(t.TempDir(), "9.log"), 12, 10, false)
	require.NoError(t, err)
	assert.Equal(t, Chunk{Offset: 12, Next: 12}, chunk)
}

func TestMarkSeen(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	id := h.start(h.spec(testutil.FakeRun{}))
	h.ended(id)

	run, err := h.s.Run(t.Context(), id)
	require.NoError(t, err)
	assert.False(t, run.Seen)

	require.NoError(t, h.s.MarkSeen(t.Context(), id))

	run, err = h.s.Run(t.Context(), id)
	require.NoError(t, err)
	assert.True(t, run.Seen)

	require.ErrorIs(t, h.s.MarkSeen(t.Context(), id+100), ErrNotFound)
}

func TestClearHistoryKeepsRunsGoing(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	going := h.start(h.spec(testutil.FakeRun{Stdout: readyLine, Sleep: time.Minute}))
	done1 := h.start(h.spec(testutil.FakeRun{Stdout: "one\n"}))
	done2 := h.start(h.spec(testutil.FakeRun{Stdout: "two\n", Exit: 1}))

	h.waitReady(going)
	h.ended(done1)
	h.ended(done2)

	require.NoError(t, h.s.ClearHistory(t.Context()))

	runs, err := h.s.Runs(t.Context(), Filter{})
	require.NoError(t, err)
	require.Len(t, runs, 1)
	assert.Equal(t, going, runs[0].ID)
	assert.Equal(t, readyLine, h.logText(going))
	assert.Empty(t, h.logText(done1), "a cleared run's log goes with it")
	assert.Empty(t, h.logText(done2))

	next := h.start(h.spec(testutil.FakeRun{Stdout: "next\n"}))
	assert.Equal(t, done1, next, "the database hands a cleared run's ID out again")
	h.ended(next)
	assert.Equal(t, "next\n", h.logText(next), "and the new run keeps its own log")
}

func TestRunsFiltersByKindAndRepository(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	checks := h.spec(testutil.FakeRun{})
	checks.Kind = KindChecks
	h.ended(h.start(checks))

	other := h.spec(testutil.FakeRun{})
	other.Repo.Owner, other.Repo.Repo = "Example-Org", "Example-Repo"
	h.ended(h.start(other))

	folder := h.spec(testutil.FakeRun{})
	folder.Repo.Owner, folder.Repo.Repo = "", ""
	folderID := h.start(folder)
	h.ended(folderID)

	all, err := h.s.Runs(t.Context(), Filter{})
	require.NoError(t, err)
	assert.Len(t, all, 3)

	hooks, err := h.s.Runs(t.Context(), Filter{Kinds: []Kind{KindHooks, KindChecks}})
	require.NoError(t, err)
	require.Len(t, hooks, 1)
	assert.Equal(t, KindChecks, hooks[0].Kind)

	found, err := h.s.Runs(t.Context(), Filter{Repo: " example-org/EXAMPLE "})
	require.NoError(t, err)
	require.Len(t, found, 1)
	assert.Equal(t, "Example-Org/Example-Repo", found[0].Repo)

	none, err := h.s.Runs(t.Context(), Filter{Kinds: []Kind{KindRelease}})
	require.NoError(t, err)
	assert.NotNil(t, none, "the window gets [], never null")
	assert.Empty(t, none)

	run, err := h.s.Run(t.Context(), folderID)
	require.NoError(t, err)
	assert.Empty(t, run.Repo, "a folder that isn't a repository's has none")
	assert.NotNil(t, run.Argv)
	assert.NotNil(t, run.Result.Warnings)
	assert.NotNil(t, run.Result.Unrecognized)
	assert.NotEmpty(t, run.StartedAt)
	assert.NotEmpty(t, run.FinishedAt)
	assert.False(t, run.CanStop)
	assert.False(t, run.CanRunAgain, "no Rerunner is registered")

	_, err = h.s.Run(t.Context(), folderID+100)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestTheWindowSeesTheFolderWithHome(t *testing.T) {
	t.Parallel()

	home := testutil.RealTempDir(t)
	h := newHarness(t, func(cfg *Config) { cfg.Home = home })

	spec := h.spec(testutil.FakeRun{})
	spec.Dir = filepath.Join(home, "projects", "sdk")
	require.NoError(t, os.MkdirAll(spec.Dir, 0o700))

	run, err := h.s.Run(t.Context(), h.ended(h.start(spec)).ID)
	require.NoError(t, err)
	assert.Equal(t, "~/projects/sdk", run.Dir)
}

func TestRunAgainWorksTheCommandOutAgain(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	first := h.spec(testutil.FakeRun{Stdout: "the old command\n"})
	firstID := h.start(first)
	h.ended(firstID)

	now := h.spec(testutil.FakeRun{Stdout: "the command as it is now\n"})
	now.Dir = first.Dir
	now.Kind = ""

	rerun := &fakeRerunner{spec: now}
	h.e.RegisterRerunner(KindScript, rerun)

	run, err := h.s.Run(t.Context(), firstID)
	require.NoError(t, err)
	assert.True(t, run.CanRunAgain)

	id, err := h.s.RunAgain(t.Context(), firstID)
	require.NoError(t, err)
	assert.NotEqual(t, firstID, id)

	rec := h.ended(id)
	assert.Equal(t, now.Argv, rec.Argv, "the command was worked out again, not replayed")
	assert.NotEqual(t, first.Argv, rec.Argv)
	assert.Equal(t, KindScript, rec.Kind)
	assert.Equal(t, "the command as it is now\n", h.logText(id))

	require.Len(t, rerun.asked, 1)
	assert.Equal(t, firstID, rerun.asked[0].ID)
	assert.Equal(t, first.Dir, rerun.asked[0].Dir, "the Rerunner gets the record, folder and all")
}

func TestRunAgainIsRefusedWhereItCantWork(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	release := h.spec(testutil.FakeRun{})
	release.Kind = KindRelease
	releaseID := h.start(release)
	h.ended(releaseID)

	h.e.RegisterRerunner(KindRelease, &fakeRerunner{spec: release})

	_, err := h.s.RunAgain(t.Context(), releaseID)
	require.ErrorIs(t, err, ErrNoRerun)
	assert.Equal(t, "A release runs again from its sheet, never from here.", uierr.Message(err))

	run, err := h.s.Run(t.Context(), releaseID)
	require.NoError(t, err)
	assert.False(t, run.CanRunAgain, "never offered for a release")

	hooks := h.spec(testutil.FakeRun{})
	hooks.Kind = KindHooks
	hooksID := h.start(hooks)
	h.ended(hooksID)

	_, err = h.s.RunAgain(t.Context(), hooksID)
	require.ErrorIs(t, err, ErrNoRerun, "a kind with no Rerunner")

	h.e.RegisterRerunner(KindHooks, &fakeRerunner{err: errGone})

	_, err = h.s.RunAgain(t.Context(), hooksID)
	require.ErrorIs(t, err, errGone)

	h.e.RegisterRerunner(KindHooks, &fakeRerunner{spec: release})

	_, err = h.s.RunAgain(t.Context(), hooksID)
	require.ErrorIs(t, err, ErrInvalidSpec, "a Rerunner can't turn a run into a release")
	assert.Equal(t, 2, h.count(), "nothing new ran")

	_, err = h.s.RunAgain(t.Context(), hooksID+100)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestOpenInTerminalIsNotAvailableYet(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	id := h.start(h.spec(testutil.FakeRun{}))
	h.ended(id)

	err := h.s.OpenInTerminal(t.Context(), id)
	require.ErrorIs(t, err, ErrNotAvailable)
	assert.Equal(t, "Opening a run in your terminal isn't available yet.", uierr.Message(err))

	require.ErrorIs(t, h.s.OpenInTerminal(t.Context(), id+100), ErrNotFound)
}

func TestQuitAnywayCallsQuit(t *testing.T) {
	t.Parallel()

	quits := 0
	h := newHarness(t, func(cfg *Config) { cfg.Quit = func() { quits++ } })

	h.s.QuitAnyway(t.Context())
	assert.Equal(t, 1, quits)

	NewService(NewEngine(Config{})).QuitAnyway(t.Context()) // without Quit, nothing happens
}

func TestSizeText(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "1 byte", sizeText(1))
	assert.Equal(t, "12 bytes", sizeText(12))
	assert.Equal(t, "4 KB", sizeText(4<<10))
	assert.Equal(t, "1.5 KB", sizeText(1536))
	assert.Equal(t, "12 MB", sizeText(12<<20))
	assert.Equal(t, "2.5 MB", sizeText(5<<19))
}
