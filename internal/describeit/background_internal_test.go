package describeit

import (
	"context"
	"os"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agentrun/runevent"
	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// crashedTurn keeps a conversation whose one turn its record says runs, as a crash left
// it, with stream as its stream, and returns the conversation's ID.
func (r *rig) crashedTurn(stream string) string {
	r.t.Helper()

	rec := newRecord(uuid.NewString(), AgentChoice{}, Start{}, testNow.Add(-time.Hour))
	rec.Turns = []turnRecord{{N: 1, Text: pinText, State: TurnRunning, StartedAt: testNow.Add(-time.Hour), Proposals: []string{}}}
	require.NoError(r.t, r.svc.saveRecord(rec))
	require.NoError(r.t, os.WriteFile(r.svc.turnLog(rec.ID, 1), []byte(stream), 0o600))

	return rec.ID
}

func TestStartClosesATurnACrashLeftRunningFromItsStream(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	id := r.crashedTurn(streamFixture(t, "turn-first.jsonl", 0))
	proposal := r.writeProposal(id, testNow.Add(-time.Hour), 0, pinProposal())

	r.svc.start(t.Context())

	conv, err := r.svc.Conversation(t.Context())
	require.NoError(t, err)
	require.Len(t, conv.Turns, 1)

	turn := conv.Turns[0]
	assert.Equal(t, TurnDone, turn.State, "a turn whose stream holds its result finished")
	assert.True(t, strings.HasPrefix(turn.Reply, "I've proposed a routine"))
	require.NotNil(t, turn.Cost)
	require.Len(t, turn.Cards, 1, "with the proposals it made")
	assert.Equal(t, proposal, turn.Cards[0].Ref.Proposal)

	rec, err := r.svc.readRecord(id)
	require.NoError(t, err)
	assert.True(t, rec.Started)
	assert.Equal(t, TurnDone, rec.Turns[0].State, "and its end is kept")
}

func TestStartClosesATurnWithoutAResultAsStopped(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		stream string
		state  TurnState
		error  string
	}{
		"a stream cut short":      {stream: streamFixture(t, "turn-first.jsonl", 4), state: TurnStopped},
		"no stream at all":        {stream: "", state: TurnStopped},
		"the app's tools missing": {stream: streamFixture(t, "turn-mcp-failed.jsonl", 0), state: TurnFailed, error: "Autopilot tools didn't start"},
		"out of its budget":       {stream: streamFixture(t, "turn-budget.jsonl", 0), state: TurnFailed, error: "budget of $2"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := newRig(t)
			r.crashedTurn(tt.stream)

			r.svc.start(t.Context())

			conv, err := r.svc.Conversation(t.Context())
			require.NoError(t, err)
			assert.Equal(t, tt.state, conv.Turns[0].State)
			assert.Contains(t, conv.Turns[0].Error, tt.error)
			assert.False(t, conv.Turns[0].EndedAt.IsZero())
		})
	}
}

func TestStartLeavesTheTurnRunningNowAlone(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	id := r.crashedTurn(streamFixture(t, "turn-first.jsonl", 2))

	rec, err := r.svc.readRecord(id)
	require.NoError(t, err)

	r.svc.mu.Lock()
	r.svc.rec, r.svc.looked, r.svc.live = rec, true, &liveTurn{rec: rec, n: 1, done: make(chan struct{})}
	r.svc.mu.Unlock()

	r.svc.start(t.Context())

	assert.Equal(t, TurnRunning, rec.Turns[0].State, "this app's own turn is its follower's to close")
}

func TestStartPrunesWhatIsOlderThanThirtyDays(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	old := newRecord(uuid.NewString(), AgentChoice{}, Start{}, testNow.Add(-31*24*time.Hour))
	require.NoError(t, r.svc.saveRecord(old))
	r.writeProposal(old.ID, testNow.Add(-31*24*time.Hour), 0, pinProposal())

	recent := newRecord(uuid.NewString(), AgentChoice{}, Start{}, testNow.Add(-29*24*time.Hour))
	require.NoError(t, r.svc.saveRecord(recent))

	orphan, fresh := uuid.NewString(), uuid.NewString()
	require.NoError(t, os.MkdirAll(r.path("proposals", orphan), 0o700))
	require.NoError(t, os.MkdirAll(r.path("proposals", fresh), 0o700))

	long := time.Now().Add(-40 * 24 * time.Hour)
	require.NoError(t, os.Chtimes(r.path("proposals", orphan), long, long))

	r.clock.Set(time.Now())
	r.svc.start(t.Context())

	assert.NoDirExists(t, r.path("conversations", old.ID), "a conversation older than 30 days is gone")
	assert.NoDirExists(t, r.path("proposals", old.ID), "with its proposals")
	assert.DirExists(t, r.path("conversations", recent.ID))
	assert.NoDirExists(t, r.path("proposals", orphan), "a folder of proposals no conversation keeps goes once old")
	assert.DirExists(t, r.path("proposals", fresh))
	assert.DirExists(t, r.path("proposals", Outside))
}

func TestStartReadiesTheFolder(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.svc.start(t.Context())

	for _, dir := range []string{workName, conversationsName, proposalsName + "/" + Outside} {
		info, err := os.Stat(r.path(dir))
		require.NoError(t, err, dir)
		assert.Equal(t, os.FileMode(0o700), info.Mode().Perm(), dir)
	}

	instructions, err := os.ReadFile(r.path("instructions.md"))
	require.NoError(t, err)
	assert.Equal(t, kit.SkillText(kit.InApp), string(instructions))

	data, err := os.ReadFile(r.path("context.json"))
	require.NoError(t, err)

	_, err = kit.DecodeSnapshot(data)
	require.NoError(t, err)

	bare := New(Config{Dir: t.TempDir()})
	bare.start(t.Context())
	assert.NoFileExists(t, bare.contextFile(), "a service with no setup writes no snapshot")
}

func TestTheSnapshotIsWrittenOncePerBurstOfChanges(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		w := NewWorker(r.svc)

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})

		go func() {
			defer close(done)

			r.svc.keepContext(ctx, w.changed)
		}()

		for range 5 {
			w.Changed()
			time.Sleep(100 * time.Millisecond)
		}

		synctest.Wait()
		assert.Zero(t, r.setup.count(), "a burst still going writes nothing yet")

		time.Sleep(contextQuiet)
		synctest.Wait()
		assert.Equal(t, 1, r.setup.count(), "once the burst ends, the snapshot is read and written once")
		assert.FileExists(t, r.path("context.json"))

		// A change every 350 ms keeps every burst going: one is written each time the
		// longest wait is up.
		for range 20 {
			w.Changed()
			time.Sleep(350 * time.Millisecond)
		}

		synctest.Wait()
		assert.Equal(t, 1+2, r.setup.count(), "a steady stream of changes still writes now and then")

		cancel()
		<-done
	})
}

func TestTheWorkerDoesNothingWhileAutopilotIsOff(t *testing.T) {
	t.Parallel()

	for name, off := range map[string]func(r *rig){
		"switched off":          func(r *rig) { r.autopilot.set(false) },
		"its switch unreadable": func(r *rig) { r.autopilot.fail(errFake) },
		"no switch to read":     func(r *rig) { r.svc.cfg.Switch = nil },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				r := newRig(t)
				off(r)

				// What Autopilot on left behind: a turn a crash left running, and a card from
				// the user's own Claude Code.
				id := r.crashedTurn(streamFixture(t, "turn-first.jsonl", 0))
				r.writeProposal(Outside, testNow, 0, pinProposal())
				before := r.tree()

				w, stop := r.runWorker()

				for range 3 {
					w.Changed()
					time.Sleep(time.Minute)
				}

				synctest.Wait()
				assert.Equal(t, before, r.tree(), "nothing written: no snapshot, instructions, plugin, link, or folder")
				assert.Zero(t, r.setup.count(), "and the setup never read")

				rec, err := r.svc.readRecord(id)
				require.NoError(t, err)
				assert.Equal(t, TurnRunning, rec.Turns[0].State, "a turn a crash left running waits for Autopilot")

				r.writeProposal(Outside, testNow, 1, pinProposal())
				time.Sleep(2 * outsideEvery)
				synctest.Wait()
				assert.Zero(t, r.sink.proposalsEvents(), "a card from outside is no news: the folder isn't followed")

				stop()

				_, err = r.svc.Send(t.Context(), Message{Text: pinText})
				require.ErrorIs(t, err, errClosing, "and as the app quits, no turn starts after")
			})
		})
	}
}

func TestTheWorkerCatchesUpOnceAutopilotIsTurnedOn(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		r.autopilot.set(false)
		id := r.crashedTurn(streamFixture(t, "turn-first.jsonl", 0))

		w, stop := r.runWorker()

		synctest.Wait()
		require.NoFileExists(t, r.path(contextName), "off, nothing is written")

		r.autopilot.set(true)
		w.Changed()
		synctest.Wait()

		for _, dir := range []string{workName, conversationsName, proposalsName + "/" + Outside} {
			assert.DirExists(t, r.path(dir), "the folder is readied once Autopilot is on")
		}

		assert.FileExists(t, r.path(contextName), "the snapshot written")
		assert.FileExists(t, r.path(instructionsName), "and the instructions")
		assert.FileExists(t, r.path(pluginName, ".claude-plugin", "marketplace.json"), "the plugin kept current")

		link, err := os.Readlink(r.svc.linkFile())
		require.NoError(t, err)
		assert.Equal(t, r.exe, link, "and its link pointed at the app")

		rec, err := r.svc.readRecord(id)
		require.NoError(t, err)
		assert.Equal(t, TurnDone, rec.Turns[0].State, "a turn a crash left running is closed from its stream")

		r.writeProposal(Outside, testNow, 0, pinProposal())
		time.Sleep(outsideEvery)
		synctest.Wait()
		assert.Equal(t, 1, r.sink.proposalsEvents(), "the outside folder is followed")

		reads := r.setup.count()

		w.Changed()
		time.Sleep(contextQuiet)
		synctest.Wait()
		assert.Equal(t, reads+1, r.setup.count(), "and the snapshot written again after a change")

		stop()
	})
}

func TestTheWorkerStopsOnceAutopilotIsTurnedOff(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		w, stop := r.runWorker()

		synctest.Wait()
		require.FileExists(t, r.path(contextName), "on, the worker readies the folder as it starts")

		r.autopilot.set(false)
		w.Changed()
		time.Sleep(contextQuiet)
		synctest.Wait()

		reads := r.setup.count()

		for range 3 {
			w.Changed()
			time.Sleep(time.Minute)
		}

		r.writeProposal(Outside, testNow, 0, pinProposal())
		time.Sleep(2 * outsideEvery)
		synctest.Wait()
		assert.Equal(t, reads, r.setup.count(), "off, no change writes the snapshot")
		assert.Zero(t, r.sink.proposalsEvents(), "and the outside folder isn't followed")

		r.autopilot.set(true)
		w.Changed()
		synctest.Wait()
		assert.Equal(t, reads+1, r.setup.count(), "on again, it catches up at once")

		stop()
	})
}

func TestTheWorkerRunsUntilTheAppQuits(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	w := NewWorker(r.svc)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		w.Run(ctx)
	}()

	r.claude(testutil.FakeRun{
		Stdout: streamFixture(t, "turn-sigint.jsonl", 2), WaitFor: r.path("never"), Sleep: time.Minute,
		OnInterrupt: &testutil.FakeInterrupt{Stdout: strings.TrimPrefix(streamFixture(t, "turn-sigint.jsonl", 0),
			streamFixture(t, "turn-sigint.jsonl", 2))},
	})

	id := r.newConversation()

	_, err := r.svc.Send(t.Context(), Message{Text: "Do something slow."})
	require.NoError(t, err)

	waitForEvents(t, r, id, 1, func(p runevent.Page) bool { return len(p.Events) >= 2 })

	cancel()

	select {
	case <-done:
	case <-time.After(time.Minute):
		t.Fatal("the worker never returned")
	}

	conv, err := r.svc.Conversation(t.Context())
	require.NoError(t, err)
	assert.Equal(t, TurnStopped, conv.Turns[0].State, "the app's quit stopped the turn")
	assert.FileExists(t, r.path("plugin", ".claude-plugin", "marketplace.json"), "the plugin was kept current")

	w.Run(t.Context())
}
