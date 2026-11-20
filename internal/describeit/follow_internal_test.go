package describeit

import (
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agentrun/stream"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/execpath"
)

// The follower runs in a synctest bubble over a fake process, whose stream the test
// writes, so every tick and every emit lands when the test says.

// fakeProcess is a turn's agent that exits when the test says, or when it is stopped, if
// it was made to.
type fakeProcess struct {
	done       chan struct{}
	once       sync.Once
	stops      atomic.Int32
	code       int
	exitOnStop bool
}

// newFakeProcess returns a running fake process.
func newFakeProcess(exitOnStop bool) *fakeProcess {
	return &fakeProcess{done: make(chan struct{}), exitOnStop: exitOnStop}
}

// Done closes once the process exits.
func (p *fakeProcess) Done() <-chan struct{} { return p.done }

// Wait waits for the process to exit, and returns its exit status.
func (p *fakeProcess) Wait() (int, error) {
	<-p.done

	return p.code, nil
}

// Stop counts the stop, and exits when the process was made to.
func (p *fakeProcess) Stop(execpath.Ladder) {
	p.stops.Add(1)

	if p.exitOnStop {
		p.exit()
	}
}

// exit ends the process.
func (p *fakeProcess) exit() {
	p.once.Do(func() { close(p.done) })
}

// liveTurn makes the first turn of a new conversation run as proc, its stream an empty
// file, as Send leaves it for its follower, and returns it and the stream's file.
func (r *rig) liveTurn(proc process, killed *atomic.Bool) (*liveTurn, *os.File) {
	r.t.Helper()

	rec := newRecord("00000000-0000-4000-8000-000000000001", AgentChoice{}, Start{}, testNow)
	rec.Turns = []turnRecord{{N: 1, Text: pinText, State: TurnRunning, StartedAt: testNow, Proposals: []string{}}}

	require.NoError(r.t, r.svc.saveRecord(rec))

	log := r.svc.turnLog(rec.ID, 1)
	f, err := os.Create(log) //nolint:gosec // the turn's stream, in the test's folder
	require.NoError(r.t, err)
	r.t.Cleanup(func() { _ = f.Close() })

	lt := &liveTurn{
		rec: rec, n: 1, log: log, parser: stream.Claude(r.svc.workDir()), before: map[string]bool{}, proc: proc,
		kill: func() { killed.Store(true) }, done: make(chan struct{}),
	}

	r.svc.mu.Lock()
	r.svc.rec, r.svc.looked, r.svc.live = rec, true, lt
	r.svc.mu.Unlock()

	return lt, f
}

// write appends text to the turn's stream.
func write(t *testing.T, f *os.File, text string) {
	t.Helper()

	_, err := f.WriteString(text)
	require.NoError(t, err)
}

// after moves the bubble's clock on by d and waits until the follower has settled.
func after(d time.Duration) {
	time.Sleep(d)
	synctest.Wait()
}

func TestTheFollowerTellsTheWindowAtMostEveryQuarterSecond(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		proc := newFakeProcess(false)

		var killed atomic.Bool

		lt, f := r.liveTurn(proc, &killed)

		go r.svc.follow(lt)

		// The stream is read every tailEvery (100 ms), and the window told at the first read
		// at least emitEvery (250 ms) after it last was: Send told it as the turn started.
		write(t, f, streamFixture(t, "turn-first.jsonl", 2))
		after(tailEvery)
		assert.Empty(t, r.sink.updates(), "the turn started a moment ago, which the window was told of")
		assert.Equal(t, int64(1), lt.parser.Progress().Events, "but its stream is read every tick")

		after(2 * tailEvery)
		require.Len(t, r.sink.updates(), 1, "at the first read a quarter second on, the window is told")

		write(t, f, streamFixture(t, "turn-first.jsonl", 6)[len(streamFixture(t, "turn-first.jsonl", 2)):])
		after(tailEvery)
		after(tailEvery)
		assert.Len(t, r.sink.updates(), 1, "more within the quarter second waits")

		after(tailEvery)
		assert.Len(t, r.sink.updates(), 2, "and is told once the quarter second is up")

		after(time.Second)
		assert.Len(t, r.sink.updates(), 2, "nothing new, nothing told")

		proc.exit()
		<-lt.done
		synctest.Wait()

		require.Len(t, r.sink.updates(), 3, "the turn's end is told at once")
		assert.Equal(t, event.DescribeItUpdated{ConversationID: lt.rec.ID, Turn: 1}, r.sink.updates()[2])
		assert.True(t, killed.Load(), "what is left of its group is let go")
		assert.Equal(t, TurnFailed, lt.rec.Turns[0].State, "a stream that ended without its result failed")
	})
}

func TestTheFollowerStopsATurnWithoutTheAppsTools(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		proc := newFakeProcess(true)

		var killed atomic.Bool

		lt, f := r.liveTurn(proc, &killed)

		go r.svc.follow(lt)

		write(t, f, streamFixture(t, "turn-mcp-failed.jsonl", 1))
		after(tailEvery)

		assert.Equal(t, int32(1), proc.stops.Load(), "the turn is stopped as soon as its init shows")
		<-lt.done

		assert.Equal(t, TurnFailed, lt.rec.Turns[0].State)
		assert.Contains(t, lt.rec.Turns[0].Error, "Autopilot tools didn't start")

		rec, err := r.svc.readRecord(lt.rec.ID)
		require.NoError(t, err)
		assert.Equal(t, TurnFailed, rec.Turns[0].State, "and its end is kept")
		assert.True(t, rec.Started, "its session started all the same")
	})
}

func TestTheFollowerStopsWhatIsLeftOnceTheResultIsIn(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		proc := newFakeProcess(false)

		var killed atomic.Bool

		lt, f := r.liveTurn(proc, &killed)

		go r.svc.follow(lt)

		write(t, f, streamFixture(t, "turn-first.jsonl", 0))
		after(tailEvery)
		after(tailEvery)

		assert.Equal(t, int32(1), proc.stops.Load(), "the result is in: the rest of the turn is stopped, once")

		proc.exit()
		<-lt.done

		assert.Equal(t, TurnDone, lt.rec.Turns[0].State, "stopping it once its result came is no stop of the user's")
	})
}

func TestTheAppsQuitStopsATurn(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		proc := newFakeProcess(true)

		var killed atomic.Bool

		lt, f := r.liveTurn(proc, &killed)

		go r.svc.follow(lt)

		write(t, f, streamFixture(t, "turn-sigint.jsonl", 2))
		after(tailEvery)

		r.svc.shutdown()
		synctest.Wait()

		assert.Equal(t, int32(1), proc.stops.Load())
		assert.Equal(t, TurnStopped, lt.rec.Turns[0].State, "a turn the app's quit ended is stopped")

		_, err := r.svc.Send(t.Context(), Message{Text: "Again."})
		require.Error(t, err, "no turn starts once the app quits")

		_, err = r.svc.NewConversation(t.Context(), AgentChoice{})
		require.Error(t, err)
	})
}

func TestTheAppsQuitKillsATurnThatWontStop(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		proc := newFakeProcess(false)

		var killed atomic.Bool

		lt, _ := r.liveTurn(proc, &killed)

		lt.kill = func() {
			killed.Store(true)
			proc.exit()
		}

		go r.svc.follow(lt)

		start := time.Now()

		r.svc.shutdown()

		assert.True(t, killed.Load(), "a turn that ignores its stop is killed")
		assert.Equal(t, quitWait, time.Since(start), "once it had its while to stop")
		assert.Equal(t, TurnStopped, lt.rec.Turns[0].State)
	})
}

func TestTheAppsQuitWithNoTurnIsQuick(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.svc.shutdown()

	_, err := r.svc.Send(t.Context(), Message{Text: "Again."})
	require.Error(t, err)
	assert.ErrorIs(t, err, errClosing)
}
