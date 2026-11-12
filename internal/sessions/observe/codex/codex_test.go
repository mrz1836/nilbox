package codex_test

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/procs"
	"github.com/mrz1836/nilbox/internal/sessions"
	"github.com/mrz1836/nilbox/internal/sessions/observe/codex"
)

// update makes TestMapGolden rewrite its golden file from the current output.
var update = flag.Bool("update", false, "rewrite the golden files from the current output") //nolint:gochecknoglobals // a test flag

// The threads the thread_list fixture holds.
const (
	hostedThread   = "01a1bec5-d48c-7092-9332-fb198342b55d"
	embeddedThread = "01a1bebd-6f52-7942-9e77-e69115ff8ee8"
	hostedDir      = "/srv/wt/example-repo/issue-14"
	embeddedDir    = "/srv/wt/example-repo/pr-12"
)

// codexCommand is the command a Codex process runs as.
const codexCommand = "codex"

// TestMapGolden maps every recorded thread status and turn, and compares the result with
// testdata/states.golden. After a deliberate change, rewrite it with
// go test ./internal/sessions/observe/codex -run TestMapGolden -update.
func TestMapGolden(t *testing.T) {
	t.Parallel()

	var b strings.Builder

	var statuses []codex.ThreadStatus
	require.NoError(t, json.Unmarshal(fixture(t, "statuses.json"), &statuses))

	for _, s := range statuses {
		state, detail, loaded := codex.MapStatus(s)
		fmt.Fprintf(&b, "status %s %v -> %q %q loaded=%v\n", s.Type, s.ActiveFlags, state, detail, loaded)
	}

	for _, name := range []string{
		"turns_in_flight.json", "turns_completed.json", "turns_interrupted.json", "turns_failed.json", "turns_unknown.json",
	} {
		var turns struct {
			Data []codex.Turn `json:"data"`
		}

		require.NoError(t, json.Unmarshal(fixture(t, name), &turns))
		require.Len(t, turns.Data, 1)

		state, detail := codex.TurnState(turns.Data[0])
		fmt.Fprintf(&b, "turn %s -> %q %q message=%q\n", name, state, detail, codex.LastAgentMessage(turns.Data[0]))
	}

	path := filepath.Join("testdata", "states.golden")
	if *update {
		require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o644)) //nolint:gosec // committed test data
	}

	assert.Equal(t, string(fixture(t, "states.golden")), b.String())
}

// listed is the threads the thread_list fixture holds.
func listed(t *testing.T) []codex.Thread {
	t.Helper()

	var got struct {
		Data []codex.Thread `json:"data"`
	}

	require.NoError(t, json.Unmarshal(fixture(t, "thread_list.json"), &got))

	return got.Data
}

// launchedAt is when the sessions in these tests were launched: just before the hosted
// thread was created.
func launchedAt() time.Time {
	return time.Unix(1794202015, 0).UTC()
}

// rec is a Codex session launched in dir.
func rec(id, dir string) sessions.Record {
	return sessions.Record{
		ID: id, Agent: agent.Codex, Owner: "acme-org", Name: "example-repo", Kind: item.KindIssue, Number: 14,
		Dir: dir, LaunchedAt: launchedAt(), State: sessions.StateStarting,
		PromptHead: "Work on this issue #14 in acme-org/example-repo: Crash on start",
	}
}

func TestLink(t *testing.T) {
	t.Parallel()

	threads := listed(t)

	got, ok := codex.Link(threads, rec("s1", hostedDir), map[string]bool{})
	require.True(t, ok)
	assert.Equal(t, hostedThread, got.ID, "same folder, created after the launch, not the ephemeral helper")

	_, ok = codex.Link(threads, rec("s1", hostedDir), map[string]bool{hostedThread: true})
	assert.False(t, ok, "a thread taken by another session")

	old := rec("s2", embeddedDir)
	_, ok = codex.Link(threads, old, map[string]bool{})
	assert.False(t, ok, "the thread in that folder was created before the launch")

	old.LaunchedAt = time.Unix(1794201440, 0)
	got, ok = codex.Link(threads, old, map[string]bool{})
	require.True(t, ok)
	assert.Equal(t, embeddedThread, got.ID)

	t.Run("the prompt breaks a tie", func(t *testing.T) {
		t.Parallel()

		other := codex.Thread{ID: "earlier", Cwd: hostedDir, Preview: "Something else", CreatedAt: 1794202016}
		mine := codex.Thread{
			ID: "mine", Cwd: hostedDir, CreatedAt: 1794202018,
			Preview: "Work on this issue #14 in  acme-org/example-repo: Crash on start\nhttps://example.com",
		}

		got, ok := codex.Link([]codex.Thread{other, mine}, rec("s1", hostedDir), map[string]bool{})
		require.True(t, ok)
		assert.Equal(t, "mine", got.ID)

		noPrompt := rec("s1", hostedDir)
		noPrompt.PromptHead = ""

		got, ok = codex.Link([]codex.Thread{mine, other}, noPrompt, map[string]bool{})
		require.True(t, ok)
		assert.Equal(t, "earlier", got.ID, "without a prompt to compare, the earliest")
	})

	t.Run("a subagent's thread is never the launch's", func(t *testing.T) {
		t.Parallel()

		parent := "p"
		sub := codex.Thread{ID: "sub", Cwd: hostedDir, CreatedAt: 1794202016, ParentThreadID: &parent}

		_, ok := codex.Link([]codex.Thread{sub}, rec("s1", hostedDir), map[string]bool{})
		assert.False(t, ok)
	})
}

func TestClientSendsOnlyTheAllowedMethods(t *testing.T) {
	t.Parallel()

	client, server := net.Pipe()
	d := newFakeDaemon(t, server, func(string, json.RawMessage) (json.RawMessage, *codex.RPCError) {
		return json.RawMessage(`{}`), nil
	})

	c, err := codex.Connect(t.Context(), client, codex.ClientInfo{Name: "test", Version: "0"})
	require.NoError(t, err)

	t.Cleanup(func() { _ = c.Close() })

	for _, method := range []string{
		"turn/start", "turn/steer", "turn/interrupt", "thread/start", "thread/resume", "thread/fork",
		"thread/archive", "thread/delete", "thread/setName", "thread/shellCommand", "review/start", "",
	} {
		err = c.Call(t.Context(), method, map[string]any{}, nil)
		require.ErrorIs(t, err, codex.ErrNotAllowed, method)
	}

	for _, method := range codex.Allowed() {
		require.NoError(t, c.Call(t.Context(), method, map[string]any{}, nil), method)
	}

	assert.Equal(t, append([]string{"initialize", "initialized"}, codex.Allowed()...), d.methods(),
		"nothing refused reached the daemon")

	init := d.requests("initialize")
	require.NotEmpty(t, init)
	assert.Contains(t, string(init[0].Params), `"optOutNotificationMethods"`)
	assert.Contains(t, string(init[0].Params), `"clientInfo":{"name":"test"`)
}

func TestClientNeverAnswersTheDaemonsRequests(t *testing.T) {
	t.Parallel()

	client, server := net.Pipe()
	d := newFakeDaemon(t, server, func(string, json.RawMessage) (json.RawMessage, *codex.RPCError) {
		return json.RawMessage(`{"data":[]}`), nil
	})

	c, err := codex.Connect(t.Context(), client, codex.ClientInfo{Name: "test", Version: "0"})
	require.NoError(t, err)

	t.Cleanup(func() { _ = c.Close() })

	d.push(t, `{"id":99,"method":"item/commandExecution/requestApproval","params":{"threadId":"x"}}`)
	d.push(t, `{"method":"thread/status/changed","params":{"threadId":"x","status":{"type":"idle"}}}`)

	select {
	case <-c.Changed():
	case <-time.After(5 * time.Second):
		require.FailNow(t, "a status change did not wake the client")
	}

	require.NoError(t, c.Call(t.Context(), "thread/loaded/list", map[string]any{}, nil))
	assert.Equal(t, []string{"initialize", "initialized", "thread/loaded/list"}, d.methods(),
		"the approval request was left unanswered")
}

func TestClientReportsTheDaemonsErrors(t *testing.T) {
	t.Parallel()

	client, server := net.Pipe()
	newFakeDaemon(t, server, func(method string, _ json.RawMessage) (json.RawMessage, *codex.RPCError) {
		if method == "thread/turns/list" {
			return nil, &codex.RPCError{Code: -32600, Message: "thread is not materialized yet"}
		}

		return json.RawMessage(`{}`), nil
	})

	c, err := codex.Connect(t.Context(), client, codex.ClientInfo{Name: "test", Version: "0"})
	require.NoError(t, err)

	err = c.Call(t.Context(), "thread/turns/list", map[string]any{"threadId": "x"}, nil)

	var rpcErr *codex.RPCError
	require.ErrorAs(t, err, &rpcErr)
	assert.Equal(t, int64(-32600), rpcErr.Code)

	require.NoError(t, c.Close())

	err = c.Call(t.Context(), "thread/read", map[string]any{}, nil)
	require.ErrorIs(t, err, codex.ErrClosed)
}

// daemonScript answers a fake daemon's calls from fixtures that the test can swap.
type daemonScript struct {
	mu      sync.Mutex
	list    json.RawMessage
	threads map[string]json.RawMessage
	turns   map[string]json.RawMessage
}

func (s *daemonScript) answer(method string, params json.RawMessage) (json.RawMessage, *codex.RPCError) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var p struct {
		ThreadID string `json:"threadId"`
	}

	_ = json.Unmarshal(params, &p)

	switch method {
	case "thread/list":
		return s.list, nil
	case "thread/read":
		if th, ok := s.threads[p.ThreadID]; ok {
			return json.RawMessage(`{"thread":` + string(th) + `}`), nil
		}

		return nil, &codex.RPCError{Code: -32600, Message: "no rollout found"}
	case "thread/turns/list":
		if turns, ok := s.turns[p.ThreadID]; ok {
			return turns, nil
		}

		return nil, &codex.RPCError{Code: -32600, Message: "thread is not materialized yet"}
	default:
		return json.RawMessage(`{}`), nil
	}
}

// set replaces what the daemon says of one thread.
func (s *daemonScript) set(id, thread string, turns json.RawMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.threads[id] = json.RawMessage(thread)

	if turns != nil {
		s.turns[id] = turns
	}
}

// harness is an Observer over a fake daemon and fake processes.
type harness struct {
	script *daemonScript
	dialer *fakeDialer
	procs  *fakeProcs
	alive  map[int]bool
	obs    *codex.Observer
	now    time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	h := &harness{
		script: &daemonScript{
			list: fixture(t, "thread_list.json"), threads: map[string]json.RawMessage{},
			turns: map[string]json.RawMessage{},
		},
		procs: &fakeProcs{},
		alive: map[int]bool{},
		now:   launchedAt().Add(10 * time.Second),
	}
	h.dialer = &fakeDialer{t: t, answer: h.script.answer}
	h.obs = codex.New(codex.Config{
		Dialer: h.dialer, Home: t.TempDir(), Procs: h.procs, Alive: func(pid int) bool { return h.alive[pid] },
	})

	t.Cleanup(h.obs.Close)

	return h
}

// observe runs one Observe and returns its observations by the app's ID.
func (h *harness) observe(t *testing.T, live ...sessions.Record) map[string]sessions.Observation {
	t.Helper()

	out := map[string]sessions.Observation{}
	for _, o := range h.obs.Observe(t.Context(), h.now, live) {
		out[o.ID] = o
	}

	return out
}

// thread is a thread/read answer's thread with status and updated.
func thread(id, dir, status string, updated int64) string {
	return fmt.Sprintf(`{"id":%q,"cwd":%q,"preview":"","createdAt":1794202017,"updatedAt":%d,"status":%s}`,
		id, dir, updated, status)
}

func TestObserveAHostedThreadLive(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.procs.list = []procs.Process{{PID: 700, PPID: 690, Command: codexCommand, Dir: hostedDir}}
	h.alive[700] = true
	h.script.set(hostedThread, thread(hostedThread, hostedDir, `{"type":"active","activeFlags":["waitingOnApproval"]}`, 10),
		nil)

	r := rec("s1", hostedDir)
	got := h.observe(t, r)

	assert.Equal(t, sessions.Observation{
		ID: "s1", AgentSessionID: hostedThread, State: sessions.StateNeedsApproval, PID: 700,
	}, got["s1"], "linked, its process found, and its live status read")

	r.AgentSessionID, r.PID = hostedThread, 700
	d := h.dialer.daemon()
	reads := d.count("thread/read")

	h.now = h.now.Add(time.Second)
	h.observe(t, r)
	assert.Equal(t, reads, d.count("thread/read"), "not due: the daemon said nothing changed")

	h.script.set(hostedThread, thread(hostedThread, hostedDir, `{"type":"idle"}`, 11), fixture(t, "turns_completed.json"))
	d.push(t, `{"method":"thread/status/changed","params":{"threadId":"`+hostedThread+`","status":{"type":"idle"}}}`)

	require.Eventually(t, func() bool {
		h.now = h.now.Add(10 * time.Millisecond)

		return h.observe(t, r)["s1"].State == sessions.StateIdle
	}, 5*time.Second, 10*time.Millisecond, "the change woke the observer before the poll")

	got = h.observe(t, r)
	assert.Equal(t, "The fix is right; one test is missing.", got["s1"].LastMessage)

	for _, method := range d.methods() {
		assert.Contains(t, append(codex.Allowed(), "initialized"), method)
	}

	assert.Equal(t, []string{""}, h.dialer.dials(), "one connection, to the default Codex home")
}

func TestObserveAnEmbeddedThreadByItsTurns(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.alive[701] = true
	h.procs.list = []procs.Process{{PID: 701, PPID: 600, Command: codexCommand, Dir: embeddedDir}}

	r := rec("s2", embeddedDir)
	r.AgentSessionID, r.PID = embeddedThread, 701

	h.script.set(embeddedThread, thread(embeddedThread, embeddedDir, `{"type":"notLoaded"}`, 100),
		fixture(t, "turns_in_flight.json"))

	got := h.observe(t, r)
	assert.Equal(t, sessions.StateWorking, got["s2"].State, "a turn in flight on a thread the daemon does not host")
	assert.Equal(t, codex.OutsideServer, got["s2"].Detail, "and the card says why it shows no more")
	assert.Equal(t, "I'll run the tests now.", got["s2"].LastMessage)

	d := h.dialer.daemon()
	turns := d.count("thread/turns/list")

	h.now = h.now.Add(codex.Poll)
	h.observe(t, r)
	assert.Equal(t, turns+1, d.count("thread/turns/list"), "while its turn works, its turns are read every poll")

	h.script.set(embeddedThread, thread(embeddedThread, embeddedDir, `{"type":"notLoaded"}`, 200),
		fixture(t, "turns_completed.json"))

	h.now = h.now.Add(codex.Poll)
	got = h.observe(t, r)
	assert.Equal(t, sessions.StateIdle, got["s2"].State)
	assert.Equal(t, "The fix is right; one test is missing.", got["s2"].LastMessage)

	turns = d.count("thread/turns/list")

	h.now = h.now.Add(codex.Poll)
	h.observe(t, r)
	assert.Equal(t, turns, d.count("thread/turns/list"), "the thread did not change since its turn ended, so its turns are not read again")

	h.script.set(embeddedThread, thread(embeddedThread, embeddedDir, `{"type":"notLoaded"}`, 300),
		fixture(t, "turns_failed.json"))

	h.now = h.now.Add(codex.Poll)
	got = h.observe(t, r)
	assert.Equal(t, "The last turn failed: The model is overloaded.", got["s2"].Detail, "a turn's own detail comes first")
}

func TestObserveATurnThatEndsInTheSecondItWasRead(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.alive[701] = true
	h.procs.list = []procs.Process{{PID: 701, PPID: 600, Command: codexCommand, Dir: embeddedDir}}

	r := rec("s2", embeddedDir)
	r.AgentSessionID, r.PID = embeddedThread, 701

	set := func(updated int64, turns string) {
		h.script.set(embeddedThread, thread(embeddedThread, embeddedDir, `{"type":"notLoaded"}`, updated), fixture(t, turns))
	}

	set(100, "turns_in_flight.json")
	assert.Equal(t, sessions.StateWorking, h.observe(t, r)["s2"].State)

	// Codex stamps updates in whole seconds, so a turn that ends in the second it was read
	// in leaves the thread's update as it was.
	set(100, "turns_completed.json")

	h.now = h.now.Add(codex.Poll)
	assert.Equal(t, sessions.StateIdle, h.observe(t, r)["s2"].State, "a working turn is read again until it ends")

	// A turn read in the second of the thread's last update can change in that second too.
	h.now = h.now.Add(codex.Poll)
	set(h.now.Unix(), "turns_completed.json")
	assert.Equal(t, sessions.StateIdle, h.observe(t, r)["s2"].State)

	set(h.now.Unix(), "turns_in_flight.json")

	h.now = h.now.Add(codex.Poll)
	assert.Equal(t, sessions.StateWorking, h.observe(t, r)["s2"].State,
		"a turn read in the second of the last update is read again")
}

func TestObserveAThreadWhoseTurnsCannotBeRead(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.alive[701] = true
	h.procs.list = []procs.Process{{PID: 701, PPID: 600, Command: codexCommand, Dir: embeddedDir}}

	r := rec("s2", embeddedDir)
	r.AgentSessionID, r.PID = embeddedThread, 701

	h.script.set(embeddedThread, thread(embeddedThread, embeddedDir, `{"type":"notLoaded"}`, 100), nil)
	assert.Equal(t, sessions.StateUnknown, h.observe(t, r)["s2"].State, "its turns can't be read, so nothing says it works")

	h.script.set(embeddedThread, thread(embeddedThread, embeddedDir, `{"type":"notLoaded"}`, 101),
		json.RawMessage(`{"data":[]}`))

	h.now = h.now.Add(codex.Poll)
	assert.Equal(t, sessions.StateUnknown, h.observe(t, r)["s2"].State, "a thread with no turn yet")
}

func TestMapsWhatItDoesNotKnowAsUnknown(t *testing.T) {
	t.Parallel()

	state, _, loaded := codex.MapStatus(codex.ThreadStatus{Type: "somethingNew"})
	assert.True(t, loaded)
	assert.Equal(t, sessions.StateUnknown, state, "a thread status this build doesn't know")

	completed := int64(1794202020)

	state, _ = codex.TurnState(codex.Turn{Status: "somethingNew", CompletedAt: &completed})
	assert.Equal(t, sessions.StateUnknown, state, "a turn status this build doesn't know")

	state, _ = codex.TurnState(codex.Turn{Status: "somethingNew"})
	assert.Equal(t, sessions.StateUnknown, state)
}

func TestObserveTwoSessionsInOneFolderKeepTheirOwnProcesses(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.alive[800], h.alive[801], h.alive[802] = true, true, true
	h.procs.list = []procs.Process{
		{PID: 802, PPID: 600, Command: codexCommand, Dir: hostedDir},
		{PID: 801, PPID: 600, Command: codexCommand, Dir: hostedDir},
		{PID: 800, PPID: 600, Command: codexCommand, Dir: hostedDir},
	}

	a, b, c := rec("a", hostedDir), rec("b", hostedDir), rec("c", hostedDir)
	a.AgentSessionID, a.PID = "t-a", 800
	b.AgentSessionID, b.PID = "t-b", 801

	h.script.set("t-a", thread("t-a", hostedDir, `{"type":"idle"}`, 10), nil)
	h.script.set("t-b", thread("t-b", hostedDir, `{"type":"idle"}`, 10), nil)

	got := h.observe(t, a, b, c)
	assert.Equal(t, 800, got["a"].PID, "a session keeps its own process")
	assert.Equal(t, 801, got["b"].PID)
	assert.Equal(t, 802, got["c"].PID, "a process another session has is never taken")

	h.alive[801] = false
	h.procs.list = slices.DeleteFunc(h.procs.list, func(p procs.Process) bool { return p.PID == 801 })

	h.now = h.now.Add(codex.Recheck)
	got = h.observe(t, a, b)
	assert.Equal(t, sessions.StateEnded, got["b"].State)
	assert.Equal(t, sessions.StateIdle, got["a"].State, "the other session's process still runs")
	assert.Equal(t, 800, got["a"].PID)
}

func TestObserveListsThreadsByTheFolderAndWhereItLeads(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	root := t.TempDir()
	target := filepath.Join(root, "target")
	link := filepath.Join(root, "link")

	require.NoError(t, os.Mkdir(target, 0o700))
	require.NoError(t, os.Symlink(target, link))

	resolved, err := filepath.EvalSymlinks(link)
	require.NoError(t, err)

	h.observe(t, rec("s1", link))

	lists := h.dialer.daemon().requests("thread/list")
	require.Len(t, lists, 1)

	var params struct {
		Cwd []string `json:"cwd"`
	}

	require.NoError(t, json.Unmarshal(lists[0].Params, &params))
	assert.ElementsMatch(t, []string{link, resolved}, params.Cwd,
		"Codex filters by the exact folder it recorded, which has its links resolved")
}

func TestObserveBoundsTheDial(t *testing.T) {
	t.Parallel()

	d := &deadlineDialer{}
	obs := codex.New(codex.Config{Dialer: d, Home: t.TempDir()})

	t.Cleanup(obs.Close)

	obs.Observe(t.Context(), launchedAt(), []sessions.Record{rec("s1", hostedDir)})

	require.Len(t, d.left, 1)
	assert.LessOrEqual(t, d.left[0], 3*time.Second, "a daemon that hangs holds following up only briefly")
}

// deadlineDialer fails every Dial, recording how long each had left.
type deadlineDialer struct {
	left []time.Duration
}

func (d *deadlineDialer) Dial(ctx context.Context, _ string) (*codex.Client, error) {
	if deadline, ok := ctx.Deadline(); ok {
		d.left = append(d.left, time.Until(deadline))
	}

	return nil, errDialRefused
}

// rolloutStamp is a session_meta payload's timestamp field for at.
func rolloutStamp(at time.Time) string {
	return `,"timestamp":"` + at.UTC().Format(time.RFC3339Nano) + `"`
}

// writeRollout writes a rollout file for thread id, started at started in dir, into the
// day folder of written under codexHome, with extra added to its first line's payload, and
// stamps it written then. It returns the file's path.
func writeRollout(t *testing.T, codexHome string, started time.Time, id, dir, extra string, written time.Time) string {
	t.Helper()

	dayDir := filepath.Join(codexHome, "sessions", written.Local().Format("2006/01/02")) //nolint:gosmopolitan // Codex's folders are local days
	require.NoError(t, os.MkdirAll(dayDir, 0o700))

	name := "rollout-" + started.Local().Format("2006-01-02T15-04-05") + "-" + id + ".jsonl" //nolint:gosmopolitan // as Codex names them
	path := filepath.Join(dayDir, name)
	line := `{"type":"session_meta","payload":{"id":"` + id + `","cwd":"` + dir + `"` + extra + `}}` + "\n"

	require.NoError(t, os.WriteFile(path, []byte(line), 0o600))
	require.NoError(t, os.Chtimes(path, written, written))

	return path
}

func TestObserveWithoutTheDaemonLinksTheThreadTheLaunchStarted(t *testing.T) {
	t.Parallel()

	launch := launchedAt()
	own := launch.Add(3 * time.Second)

	for name, other := range map[string]struct {
		started time.Time
		extra   string
	}{
		"an older thread still being written": {
			started: launch.Add(-3 * time.Hour), extra: rolloutStamp(launch.Add(-3*time.Hour)) + `,"source":"cli"`,
		},
		"an older thread whose first line has no time": {started: launch.Add(-3 * time.Hour), extra: `,"source":"cli"`},
		"a subagent's thread": {
			started: launch.Add(2 * time.Second),
			extra: rolloutStamp(launch.Add(2*time.Second)) +
				`,"source":{"subagent":{"thread_spawn":{"parent_thread_id":"own-thread","depth":1}}}`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			h.dialer.err = errDialRefused

			codexHome := t.TempDir()
			writeRollout(t, codexHome, other.started, "other-thread", embeddedDir, other.extra, h.now)
			writeRollout(t, codexHome, own, "own-thread", embeddedDir, rolloutStamp(own)+`,"source":"cli"`, h.now)

			r := rec("s1", embeddedDir)
			r.ConfigDir = codexHome

			assert.Equal(t, "own-thread", h.observe(t, r)["s1"].AgentSessionID)
		})
	}
}

func TestObserveWithoutTheDaemonLooksInEachLaunchsOwnDays(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.dialer.err = errDialRefused

	codexHome := t.TempDir()
	own := launchedAt().Add(3 * time.Second)
	writeRollout(t, codexHome, own, "own-thread", embeddedDir, rolloutStamp(own), h.now)

	old := rec("old", hostedDir)
	old.ConfigDir, old.LaunchedAt = codexHome, launchedAt().Add(-10*24*time.Hour)

	r := rec("s1", embeddedDir)
	r.ConfigDir = codexHome

	assert.Equal(t, "own-thread", h.observe(t, old, r)["s1"].AgentSessionID,
		"a session left unlinked long ago does not move where a new one's thread is looked for")
}

func TestObserveWithoutTheDaemonReadsEachRolloutOnce(t *testing.T) {
	t.Parallel()

	t.Run("a whole first line is read once", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t)
		h.dialer.err = errDialRefused

		codexHome := t.TempDir()
		started := launchedAt().Add(3 * time.Second)
		path := writeRollout(t, codexHome, started, "elsewhere-thread", "/srv/wt/example-repo/pr-99", rolloutStamp(started), h.now)

		r := rec("s1", embeddedDir)
		r.ConfigDir = codexHome

		assert.Empty(t, h.observe(t, r)["s1"].AgentSessionID)

		// Codex never rewrites a first line, so a file read again would show it here.
		line := `{"type":"session_meta","payload":{"id":"elsewhere-thread","cwd":"` + embeddedDir + `"` + rolloutStamp(started) + `}}` + "\n"
		require.NoError(t, os.WriteFile(path, []byte(line), 0o600))
		require.NoError(t, os.Chtimes(path, h.now, h.now))

		h.now = h.now.Add(codex.Poll)
		assert.Empty(t, h.observe(t, r)["s1"].AgentSessionID, "the file was not read again")
	})

	t.Run("a first line still being written is read again", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t)
		h.dialer.err = errDialRefused

		codexHome := t.TempDir()
		started := launchedAt().Add(3 * time.Second)
		path := writeRollout(t, codexHome, started, "own-thread", embeddedDir, rolloutStamp(started), h.now)

		whole, err := os.ReadFile(path) //nolint:gosec // the test's own temp file
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, whole[:len(whole)/2], 0o600)) //nolint:gosec // the test's own temp file
		require.NoError(t, os.Chtimes(path, h.now, h.now))

		r := rec("s1", embeddedDir)
		r.ConfigDir = codexHome

		assert.Empty(t, h.observe(t, r)["s1"].AgentSessionID)

		require.NoError(t, os.WriteFile(path, whole, 0o600)) //nolint:gosec // the test's own temp file
		require.NoError(t, os.Chtimes(path, h.now, h.now))

		h.now = h.now.Add(codex.Poll)
		assert.Equal(t, "own-thread", h.observe(t, r)["s1"].AgentSessionID)
	})
}

func TestObserveSessionsEndAndStart(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	quit := rec("s1", hostedDir)
	quit.AgentSessionID, quit.PID = hostedThread, 702
	h.script.set(hostedThread, thread(hostedThread, hostedDir, `{"type":"idle"}`, 10), nil)

	starting := rec("s2", "/srv/wt/example-repo/pr-40")
	neverStarted := rec("s3", "/srv/wt/example-repo/pr-41")
	neverStarted.LaunchedAt = h.now.Add(-codex.Grace - time.Second)

	got := h.observe(t, quit, starting, neverStarted)
	assert.Equal(t, sessions.StateEnded, got["s1"].State, "its process is gone, whatever its thread says")
	assert.Equal(t, sessions.StateStarting, got["s2"].State)
	assert.Equal(t, sessions.StateEnded, got["s3"].State, "no process in its folder past the grace")
}

func TestObserveEndsASessionWhoseProcessIDWentToAnotherProgram(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.alive[730] = true
	h.procs.list = []procs.Process{{PID: 730, PPID: 600, Command: codexCommand, Dir: hostedDir}}

	r := rec("s1", hostedDir)
	r.AgentSessionID, r.PID = hostedThread, 730
	h.script.set(hostedThread, thread(hostedThread, hostedDir, `{"type":"idle"}`, 10), nil)

	assert.Equal(t, sessions.StateIdle, h.observe(t, r)["s1"].State)

	h.procs.list = []procs.Process{{PID: 730, PPID: 1, Command: "Safari", Dir: "/"}}

	h.now = h.now.Add(time.Second)
	assert.Equal(t, sessions.StateIdle, h.observe(t, r)["s1"].State, "checked again only every Recheck")

	h.now = h.now.Add(codex.Recheck)
	assert.Equal(t, sessions.StateEnded, h.observe(t, r)["s1"].State, "its ID now runs another program")
}

func TestObserveLeavesTheDaemonAndCodexProcessesApart(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.procs.list = []procs.Process{
		{PID: 1, Command: "launchd", Dir: "/"},
		{PID: 710, PPID: 1, Command: codexCommand, Dir: hostedDir},
		{PID: 711, PPID: 600, Command: "zsh", Dir: hostedDir},
	}

	r := rec("s1", hostedDir)
	r.LaunchedAt = h.now.Add(-codex.Grace - time.Second)

	got := h.observe(t, r)
	assert.Equal(t, sessions.StateEnded, got["s1"].State, "the daemon and a shell in the folder are not the session")
}

func TestObserveWithoutTheDaemon(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.dialer.err = errDialRefused

	codexHome := t.TempDir()
	dayDir := filepath.Join(codexHome, "sessions", h.now.Local().Format("2006/01/02")) //nolint:gosmopolitan // Codex's folders are local days
	require.NoError(t, os.MkdirAll(dayDir, 0o700))

	name := "rollout-2026-11-09T00-17-28-" + embeddedThread + ".jsonl"
	data, err := os.ReadFile(filepath.Join("testdata", "rollout", name))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dayDir, name), data, 0o600)) //nolint:gosec // the test's own temp dir
	require.NoError(t, os.Chtimes(filepath.Join(dayDir, name), h.now, h.now))

	h.procs.list = []procs.Process{{PID: 720, PPID: 600, Command: codexCommand, Dir: embeddedDir}}
	h.alive[720] = true

	// The rollout's thread started just after this launch, as its first line says.
	r := rec("s1", embeddedDir)
	r.ConfigDir, r.LaunchedAt = codexHome, time.Unix(1794201440, 0)

	got := h.observe(t, r)
	assert.Equal(t, embeddedThread, got["s1"].AgentSessionID, "linked through its rollout file")
	assert.Equal(t, sessions.StateUnknown, got["s1"].State, "running, and how is unavailable")
	assert.Contains(t, got["s1"].Detail, "isn't reachable")
	assert.Equal(t, 720, got["s1"].PID)

	h.now = h.now.Add(time.Second)
	h.observe(t, r)
	assert.Len(t, h.dialer.dials(), 1, "it waits before connecting again")

	h.now = h.now.Add(codex.Redial)
	h.observe(t, r)
	assert.Equal(t, []string{codexHome, codexHome}, h.dialer.dials())
}

func TestObserveClosesWhatItNoLongerNeeds(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	r := rec("s1", hostedDir)

	h.observe(t, r)
	d := h.dialer.daemon()

	h.observe(t)

	select {
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the connection stayed open with no session to follow")
	case <-waitClosed(d):
	}
}

// waitClosed is closed once the daemon's connection reads its end.
func waitClosed(d *fakeDaemon) <-chan struct{} {
	done := make(chan struct{})

	go func() {
		<-d.ready

		d.mu.Lock()
		conn := d.conn
		d.mu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		for {
			if _, _, err := conn.Read(ctx); err != nil {
				close(done)

				return
			}
		}
	}()

	return done
}

// fakeProcs is a procs.Lister that lists list, or fails with err.
type fakeProcs struct {
	list []procs.Process
	err  error
}

func (f *fakeProcs) Processes(context.Context) ([]procs.Process, error) {
	return f.list, f.err
}
