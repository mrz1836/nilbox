package claude_test

import (
	"context"
	"flag"
	"fmt"
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
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/procs"
	"github.com/mrz1836/nilbox/internal/sessions"
	"github.com/mrz1836/nilbox/internal/sessions/observe/claude"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// update makes TestMapStateGolden rewrite its golden file from the current output.
var update = flag.Bool("update", false, "rewrite the golden files from the current output") //nolint:gochecknoglobals // a test flag

// The sessions the agents fixture lists.
const (
	busyID     = "11111111-2222-4333-8444-555555555555"
	approvalID = "22222222-2222-4333-8444-555555555555"
	inputID    = "33333333-2222-4333-8444-555555555555"
	idleID     = "44444444-2222-4333-8444-555555555555"
)

// fixture reads a file under testdata.
func fixture(t *testing.T, name string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", name)) //nolint:gosec // the test's own testdata
	require.NoError(t, err)

	return data
}

// TestMapStateGolden maps every session the recorded `claude agents --json` lists, and
// compares the result with testdata/states.golden. After a deliberate change, rewrite it
// with go test ./internal/sessions/observe/claude -run TestMapStateGolden -update.
func TestMapStateGolden(t *testing.T) {
	t.Parallel()

	entries, err := claude.ParseAgents(fixture(t, "agents.json"))
	require.NoError(t, err)

	ids := make([]string, 0, len(entries))
	for id := range entries {
		ids = append(ids, id)
	}

	slices.Sort(ids)

	var b strings.Builder

	for _, id := range ids {
		state, detail := claude.MapState(entries[id])
		fmt.Fprintf(&b, "%s pid=%d %s %q\n", id, entries[id].PID, state, detail)
	}

	path := filepath.Join("testdata", "states.golden")
	if *update {
		require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o644)) //nolint:gosec // committed test data
	}

	assert.Equal(t, string(fixture(t, "states.golden")), b.String())
}

func TestMapState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		entry  claude.Entry
		want   sessions.State
		detail string
	}{
		{claude.Entry{Status: "busy"}, sessions.StateWorking, ""},
		{claude.Entry{Status: "idle"}, sessions.StateIdle, ""},
		{claude.Entry{Status: "waiting", WaitingFor: "permission prompt"}, sessions.StateNeedsApproval, "Permission prompt"},
		{claude.Entry{Status: "waiting", WaitingFor: "sandbox request"}, sessions.StateNeedsApproval, "Sandbox request"},
		{claude.Entry{Status: "waiting"}, sessions.StateNeedsApproval, ""},
		{claude.Entry{Status: "waiting", WaitingFor: "input needed"}, sessions.StateNeedsInput, "Input needed"},
		{claude.Entry{Status: "waiting", WaitingFor: "dialog open"}, sessions.StateNeedsInput, "Dialog open"},
		{claude.Entry{Status: "waiting", WaitingFor: "goal proposal"}, sessions.StateNeedsInput, "Goal proposal"},
		{claude.Entry{Status: "something new"}, sessions.StateWorking, ""},
		{claude.Entry{State: "failed"}, sessions.StateFailed, ""},
	}

	for _, tt := range tests {
		state, detail := claude.MapState(tt.entry)
		assert.Equal(t, tt.want, state, "%+v", tt.entry)
		assert.Equal(t, tt.detail, detail, "%+v", tt.entry)
	}
}

func TestParseAgentsRefusesWhatIsNotAList(t *testing.T) {
	t.Parallel()

	_, err := claude.ParseAgents([]byte(`{"sessions":[]}`))
	require.Error(t, err)

	got, err := claude.ParseAgents([]byte("[]\n"))
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestLastMessage(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "s.jsonl")
	require.NoError(t, os.WriteFile(path, fixture(t, "transcript.jsonl"), 0o600))

	got, err := claude.LastMessage(path)
	require.NoError(t, err)
	assert.Equal(t, "The change looks good.\nOne nit: the new **helper** has no test.", got,
		"the session's own last text, not a subagent's, and not a thinking block")

	t.Run("a long transcript is read from its tail", func(t *testing.T) {
		t.Parallel()

		long := filepath.Join(t.TempDir(), "long.jsonl")
		filler := strings.Repeat(`{"type":"attachment","text":"`+strings.Repeat("x", 1000)+`"}`+"\n", 200)
		require.NoError(t, os.WriteFile(long, append([]byte(filler), fixture(t, "transcript.jsonl")...), 0o600))

		tail, readErr := claude.LastMessage(long)
		require.NoError(t, readErr)
		assert.Contains(t, tail, "has no test")
	})

	t.Run("a transcript with no message yet", func(t *testing.T) {
		t.Parallel()

		empty := filepath.Join(t.TempDir(), "empty.jsonl")
		require.NoError(t, os.WriteFile(empty, []byte(`{"type":"custom-title"}`+"\n"+`not json`+"\n"), 0o600))

		none, readErr := claude.LastMessage(empty)
		require.NoError(t, readErr)
		assert.Empty(t, none)
	})

	_, err = claude.LastMessage(filepath.Join(t.TempDir(), "missing.jsonl"))
	require.Error(t, err)
}

// harness is an Observer over a fake home, a fake claude, and fake processes.
type harness struct {
	home   string
	runner *envRunner
	procs  *fakeProcs
	alive  map[int]bool
	obs    *claude.Observer
	now    time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	h := &harness{
		home:   t.TempDir(),
		runner: &envRunner{out: string(fixture(t, "agents.json"))},
		procs:  &fakeProcs{},
		alive:  map[int]bool{},
		now:    time.Date(2026, 11, 9, 5, 11, 30, 0, time.UTC),
	}

	require.NoError(t, os.MkdirAll(filepath.Join(h.home, ".claude", "sessions"), 0o700))

	h.obs = claude.New(claude.Config{
		Finder:  testutil.Finder{Path: "/opt/agent/claude"},
		Run:     h.runner.with,
		Environ: func() []string { return []string{"HOME=" + h.home, "CLAUDE_CONFIG_DIR=/elsewhere", "PATH=/usr/bin"} },
		Home:    h.home,
		Procs:   h.procs,
		Alive:   func(pid int) bool { return h.alive[pid] },
	})

	return h
}

// record is a session the app launched at the harness's time, with the agent's ID id.
func (h *harness) record(id string) sessions.Record {
	return sessions.Record{
		ID: "app-" + id[:8], AgentSessionID: id, Agent: agent.Claude, Owner: "acme-org", Name: "example-repo",
		Kind: item.KindPR, Number: 12, Dir: "/srv/wt/example-repo/pr-12", LaunchedAt: h.now.Add(-time.Minute),
		State: sessions.StateStarting,
	}
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

func TestObserveMapsListedSessions(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	busy, waiting, input, idle := h.record(busyID), h.record(approvalID), h.record(inputID), h.record(idleID)

	got := h.observe(t, busy, waiting, input, idle)

	assert.Equal(t, sessions.Observation{ID: busy.ID, State: sessions.StateWorking, PID: 41002, Seen: true}, got[busy.ID])
	assert.Equal(t, sessions.StateNeedsApproval, got[waiting.ID].State)
	assert.Equal(t, "Permission prompt", got[waiting.ID].Detail)
	assert.Equal(t, sessions.StateNeedsInput, got[input.ID].State)
	assert.Equal(t, sessions.StateIdle, got[idle.ID].State)
	assert.Equal(t, 41005, got[idle.ID].PID)

	calls := h.runner.all()
	require.Len(t, calls, 1, "one config directory, one listing")
	assert.Equal(t, []string{"/opt/agent/claude", "agents", "--json"}, calls[0].argv)
	assert.NotContains(t, calls[0].env, "CLAUDE_CONFIG_DIR=/elsewhere", "the default profile unsets the config")
	assert.Contains(t, calls[0].env, "HOME="+h.home)
}

func TestObserveListsEachConfigDirectoryWithItsOwnConfig(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	work := filepath.Join(h.home, ".claude_work")
	a, b := h.record(busyID), h.record(idleID)
	b.ConfigDir = work

	h.observe(t, a, b)

	calls := h.runner.all()
	require.Len(t, calls, 2)
	assert.NotContains(t, strings.Join(calls[0].env, " "), "CLAUDE_CONFIG_DIR=")
	assert.Contains(t, calls[1].env, "CLAUDE_CONFIG_DIR="+work)
}

func TestObserveAsksAgainOnlyOnAHintOrAfterTheSafetyPoll(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	r := h.record(busyID)

	h.observe(t, r)
	h.now = h.now.Add(time.Second)
	h.observe(t, r)
	assert.Len(t, h.runner.all(), 1, "nothing changed in the sessions folder")

	sessionsDir := filepath.Join(h.home, ".claude", "sessions")
	testutil.WriteFile(t, filepath.Join(sessionsDir, "41002.json"), "{}")
	require.NoError(t, os.Chtimes(sessionsDir, h.now, h.now.Add(time.Hour)))

	h.now = h.now.Add(time.Second)
	h.observe(t, r)
	assert.Len(t, h.runner.all(), 2, "the sessions folder changed")

	h.now = h.now.Add(time.Second)
	h.observe(t, r)
	assert.Len(t, h.runner.all(), 2)

	h.now = h.now.Add(claude.SafetyPoll)
	h.observe(t, r)
	assert.Len(t, h.runner.all(), 3, "the safety poll")

	other := h.record(idleID)
	h.now = h.now.Add(time.Second)
	h.observe(t, r, other)
	assert.Len(t, h.runner.all(), 4, "a new session is listed at once")
}

func TestObservePollsWithoutAHint(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	require.NoError(t, os.RemoveAll(filepath.Join(h.home, ".claude", "sessions")))

	r := h.record(busyID)
	h.observe(t, r)

	h.now = h.now.Add(claude.BlindPoll - time.Second)
	h.observe(t, r)
	assert.Len(t, h.runner.all(), 1)

	h.now = h.now.Add(time.Second)
	h.observe(t, r)
	assert.Len(t, h.runner.all(), 2)
}

func TestObserveSessionsNoLongerListed(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.runner.out = "[]"

	starting := h.record("99999999-2222-4333-8444-555555555555")
	starting.LaunchedAt = h.now.Add(-10 * time.Second)

	quit := h.record("88888888-2222-4333-8444-555555555555")
	quit.PID = 501

	running := h.record("77777777-2222-4333-8444-555555555555")
	running.PID, running.State = 502, sessions.StateIdle
	h.alive[502] = true

	neverListed := h.record("66666666-2222-4333-8444-555555555555")
	neverListed.LaunchedAt = h.now.Add(-claude.Grace - time.Second)

	stuck := h.record("55555555-2222-4333-8444-555555555555")
	stuck.LaunchedAt, stuck.Dir = h.now.Add(-claude.Grace-time.Second), "/srv/wt/example-repo/pr-99"
	h.procs.list = []procs.Process{{PID: 503, Command: "2.1.319", Dir: stuck.Dir}}

	got := h.observe(t, starting, quit, running, neverListed, stuck)

	assert.Equal(t, sessions.StateStarting, got[starting.ID].State, "still within its grace")
	assert.Equal(t, sessions.StateEnded, got[quit.ID].State, "its process is gone")
	assert.Equal(t, sessions.Observation{ID: running.ID}, got[running.ID], "running and briefly unlisted: kept as it was")
	assert.Equal(t, sessions.StateEnded, got[neverListed.ID].State, "never listed, and nothing runs in its folder")
	assert.Equal(t, sessions.StateUnknown, got[stuck.ID].State, "something runs in its folder")
	assert.Equal(t, 503, got[stuck.ID].PID)
}

func TestObserveEndsASessionLeftOutPastTheGraceThoughItsProcessIDRuns(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.runner.out = "[]"

	r := h.record("88888888-2222-4333-8444-555555555555")
	r.PID, r.State = 509, sessions.StateWorking
	h.alive[509] = true

	assert.Equal(t, sessions.Observation{ID: r.ID}, h.observe(t, r)[r.ID], "briefly left out: kept")

	h.now = h.now.Add(claude.AbsentGrace)
	assert.Equal(t, sessions.StateEnded, h.observe(t, r)[r.ID].State,
		"left out that long, the process ID belongs to another program")
}

func TestObserveAsksForSessionsThatEndedOnlyWhenTheFolderChanges(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.runner.out = "[]"

	ended := h.record(idleID)
	ended.State, ended.LaunchedAt = sessions.StateEnded, h.now.Add(-48*time.Hour)

	assert.Empty(t, h.observe(t, ended), "an ended session not listed has nothing to report")
	require.Len(t, h.runner.all(), 1, "listed once when it is new")

	for range 3 {
		h.now = h.now.Add(claude.SafetyPoll)
		assert.Empty(t, h.observe(t, ended))
	}

	assert.Len(t, h.runner.all(), 1, "no safety poll for sessions that ended")
	assert.Zero(t, h.procs.calls, "nor any look at the running processes")

	sessionsDir := filepath.Join(h.home, ".claude", "sessions")
	testutil.WriteFile(t, filepath.Join(sessionsDir, "41005.json"), "{}")
	require.NoError(t, os.Chtimes(sessionsDir, h.now, h.now.Add(time.Hour)))

	h.runner.out = string(fixture(t, "agents.json"))
	h.now = h.now.Add(time.Second)

	got := h.observe(t, ended)
	assert.Len(t, h.runner.all(), 2, "a session started in the folder lists them again")
	assert.Equal(t, sessions.Observation{ID: ended.ID, State: sessions.StateIdle, PID: 41005, Seen: true}, got[ended.ID],
		"resumed outside the app")

	t.Run("without a sessions folder", func(t *testing.T) {
		t.Parallel()

		blind := newHarness(t)
		require.NoError(t, os.RemoveAll(filepath.Join(blind.home, ".claude", "sessions")))

		r := blind.record(idleID)
		r.State = sessions.StateEnded

		blind.observe(t, r)
		blind.now = blind.now.Add(time.Hour)
		blind.observe(t, r)

		assert.Len(t, blind.runner.all(), 1, "no blind poll for sessions that ended")
	})
}

func TestObserveFollowsTheConversationItsProcessMovedOnTo(t *testing.T) {
	t.Parallel()

	const cleared = "aaaaaaaa-2222-4333-8444-555555555555"

	h := newHarness(t)
	h.alive[41002] = true

	r := h.record(cleared)
	r.PID, r.State = 41002, sessions.StateIdle

	got := h.observe(t, r)
	assert.Equal(t, sessions.Observation{
		ID: r.ID, AgentSessionID: busyID, State: sessions.StateWorking, PID: 41002, Seen: true, Relinked: true,
	}, got[r.ID], "its process lists another conversation in its folder: /clear started it")

	t.Run("not one another session follows", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t)
		h.alive[41002] = true

		moved := h.record(cleared)
		moved.PID, moved.State = 41002, sessions.StateIdle

		got := h.observe(t, moved, h.record(busyID))
		assert.Equal(t, sessions.Observation{ID: moved.ID}, got[moved.ID], "kept as it was, briefly unlisted")
	})

	t.Run("not one in another folder", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t)
		h.alive[41002] = true

		elsewhere := h.record(cleared)
		elsewhere.PID, elsewhere.State, elsewhere.Dir = 41002, sessions.StateIdle, "/srv/wt/example-repo/pr-99"

		assert.Equal(t, sessions.Observation{ID: elsewhere.ID}, h.observe(t, elsewhere)[elsewhere.ID],
			"the process ID went to a session in another folder")
	})
}

func TestObserveTakesOnlyAClaudeProcessForAnUnlistedSession(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		procs     []procs.Process
		cannotAsk bool
		want      sessions.State
		pid       int
	}{
		"another program, Claude Code answering": {
			procs: []procs.Process{{PID: 600, Command: "node"}}, want: sessions.StateEnded,
		},
		"another program, Claude Code not answering": {
			procs: []procs.Process{{PID: 600, Command: "node"}}, cannotAsk: true, want: sessions.StateUnknown,
		},
		"the claude command": {
			procs: []procs.Process{{PID: 600, Command: "zsh"}, {PID: 601, Command: "/usr/local/bin/claude"}},
			want:  sessions.StateUnknown, pid: 601,
		},
		"a build named for its version": {
			procs: []procs.Process{{PID: 602, Command: "2.1.319"}}, want: sessions.StateUnknown, pid: 602,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			h.runner.out = "[]"

			if tt.cannotAsk {
				h.runner.err = assert.AnError
			}

			r := h.record("99999999-2222-4333-8444-555555555555")
			r.LaunchedAt = h.now.Add(-claude.Grace - time.Second)

			for _, p := range tt.procs {
				p.Dir = r.Dir
				h.procs.list = append(h.procs.list, p)
			}

			got := h.observe(t, r)[r.ID]
			assert.Equal(t, tt.want, got.State)
			assert.Equal(t, tt.pid, got.PID, "another program's process is never taken for the session's")
		})
	}
}

func TestObserveFindsATranscriptUnderAnyConfigDirectoryName(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	work := filepath.Join(h.home, ".claude[work]")
	testutil.WriteFile(t, filepath.Join(work, "projects", "-srv-wt-example-repo-pr-15", idleID+".jsonl"),
		string(fixture(t, "transcript.jsonl")))
	testutil.WriteFile(t, filepath.Join(h.home, ".claudew", "projects", "p", idleID+".jsonl"),
		`{"type":"assistant","message":{"role":"assistant","content":"a sibling profile's"}}`+"\n")

	r := h.record(idleID)
	r.ConfigDir = work

	assert.Contains(t, h.observe(t, r)[r.ID].LastMessage, "has no test",
		"the config directory's name is read as named, not as a pattern")
}

func TestObserveWhenClaudeCannotBeAsked(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.runner.err = assert.AnError

	running := h.record(busyID)
	running.PID = 41002
	h.alive[41002] = true

	gone := h.record(idleID)
	gone.PID = 41005

	got := h.observe(t, running, gone)
	assert.Empty(t, got[running.ID].State, "one failed listing leaves the session as it was")
	assert.Equal(t, sessions.StateEnded, got[gone.ID].State)

	h.now = h.now.Add(claude.AbsentGrace)

	got = h.observe(t, running)
	assert.Equal(t, sessions.StateUnknown, got[running.ID].State, "status unavailable, not a guess")
	assert.NotEmpty(t, got[running.ID].Detail)

	h.runner.err = nil
	h.now = h.now.Add(claude.SafetyPoll)

	got = h.observe(t, running)
	assert.Equal(t, sessions.StateWorking, got[running.ID].State, "listed again once Claude Code answers")

	h.runner.err = assert.AnError
	h.now = h.now.Add(claude.SafetyPoll)

	got = h.observe(t, running)
	assert.Empty(t, got[running.ID].State, "a failure after an answer gets its grace again")

	h.procs.err = procs.ErrUnsupported
	unlisted := h.record("99999999-2222-4333-8444-555555555555")
	unlisted.LaunchedAt = h.now.Add(-claude.Grace - time.Second)

	got = h.observe(t, unlisted)
	assert.Equal(t, sessions.StateUnknown, got[unlisted.ID].State, "nothing can tell whether it runs")
}

func TestObserveReadsTheLastMessageOnlyWhenTheTranscriptChanged(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	project := filepath.Join(h.home, ".claude", "projects", "-srv-wt-example-repo-pr-12")
	path := filepath.Join(project, idleID+".jsonl")
	testutil.WriteFile(t, path, string(fixture(t, "transcript.jsonl")))

	r := h.record(idleID)

	got := h.observe(t, r)
	assert.Equal(t, "The change looks good.\nOne nit: the new **helper** has no test.", got[r.ID].LastMessage)

	require.NoError(t, os.WriteFile(path, []byte(`{"type":"assistant","isSidechain":false,"message":{"role":"assistant",`+
		`"content":[{"type":"text","text":"Done."}]}}`+"\n"), 0o600))
	require.NoError(t, os.Chtimes(path, h.now, h.now.Add(time.Hour)))

	got = h.observe(t, r)
	assert.Equal(t, "Done.", got[r.ID].LastMessage)
}

func TestCloseForgetsWhatWasLearned(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	r := h.record(busyID)

	h.observe(t, r)
	h.obs.Close()
	h.observe(t, r)

	assert.Len(t, h.runner.all(), 2, "listed again after Close")
}

// call is one program the envRunner ran, with its environment.
type call struct {
	env  []string
	argv []string
}

// envRunner is a fake runner factory: every runner it makes records its calls, with the
// environment it was made with, and prints out or fails with err.
type envRunner struct {
	mu    sync.Mutex
	calls []call
	out   string
	err   error
}

func (r *envRunner) with(env []string) execpath.Runner {
	return runnerFunc(func(_ context.Context, name string, args ...string) ([]byte, []byte, error) {
		r.mu.Lock()
		defer r.mu.Unlock()

		r.calls = append(r.calls, call{env: slices.Clone(env), argv: append([]string{name}, args...)})

		return []byte(r.out), nil, r.err
	})
}

func (r *envRunner) all() []call {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.calls)
}

// runnerFunc is an execpath.Runner that is a function.
type runnerFunc func(ctx context.Context, name string, args ...string) ([]byte, []byte, error)

func (f runnerFunc) Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error) {
	return f(ctx, name, args...)
}

// fakeProcs is a procs.Lister that lists list, or fails with err, and counts its calls.
type fakeProcs struct {
	list  []procs.Process
	err   error
	calls int
}

func (f *fakeProcs) Processes(context.Context) ([]procs.Process, error) {
	f.calls++

	return f.list, f.err
}
