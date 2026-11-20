package describeit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// testNow is when the tests' clock stands, to the nanosecond, as a stamp is kept.
var testNow = time.Date(2026, time.November, 20, 5, 31, 3, 123456789, time.UTC) //nolint:gochecknoglobals // a fixed test moment

// errFake is a failure the fakes give when a test asks them to.
var errFake = errors.New("fake: it failed")

// The words and names the tests use most: what the user writes, the launch defaults'
// profile, and another.
const (
	pinText        = "Pin my PRs."
	defaultProfile = "default"
	otherProfile   = "office"
)

// readyVersion is the version of Claude Code the fakes say they are: the conversation's
// floor.
func readyVersion() string {
	return agent.ConversationFloor(agent.Claude)
}

// claudeProgram is the name of Claude Code's program, which the fake takes.
func claudeProgram(t *testing.T) string {
	t.Helper()

	a, err := agent.Lookup(agent.Claude)
	require.NoError(t, err)

	return a.Command
}

// fakeAgents is Agents with one way to start Claude Code for every profile, and the
// profiles given.
type fakeAgents struct {
	mu       sync.Mutex
	start    Start
	err      error
	profiles []Profile
	profErr  error
	asked    []string
}

// Conversation returns the start the test set, for the profile asked.
func (a *fakeAgents) Conversation(_ context.Context, profile string) (Start, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.asked = append(a.asked, profile)

	st := a.start
	if st.Profile == "" {
		st.Profile = profile
	}

	if st.Profile == "" {
		st.Profile = defaultProfile
	}

	return st, a.err
}

// Profiles returns the profiles the test set.
func (a *fakeAgents) Profiles(context.Context) ([]Profile, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	return slices.Clone(a.profiles), a.profErr
}

// set makes st how Claude Code starts from now on.
func (a *fakeAgents) set(st Start) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.start = st
}

// fakeSetup is Setup with one snapshot, which a test may change or make fail.
type fakeSetup struct {
	mu    sync.Mutex
	snap  kit.Snapshot
	err   error
	reads int
}

// Snapshot returns the snapshot the test set.
func (f *fakeSetup) Snapshot(context.Context) (kit.Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.reads++

	return f.snap, f.err
}

// count returns how many times the setup was read.
func (f *fakeSetup) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.reads
}

// fail makes the setup fail to read with err, nil to read again.
func (f *fakeSetup) fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.err = err
}

// change gives the setup another snapshot.
func (f *fakeSetup) change(snap kit.Snapshot) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.snap = snap
}

// fakeAttachments words a routine and an item by their IDs, and fails for any other.
type fakeAttachments struct{}

// Routine words the routine 7.
func (fakeAttachments) Routine(_ context.Context, id int64) (Attachment, error) {
	if id != 7 {
		return Attachment{}, errFake
	}

	return Attachment{Label: "Nightly", Words: "The user attached their routine \"Nightly\" (id 7)."}, nil
}

// Item words the item 42.
func (fakeAttachments) Item(_ context.Context, id int64) (Attachment, error) {
	if id != 42 {
		return Attachment{}, errFake
	}

	return Attachment{Label: "acme-org/example-repo#42", Words: "The user attached acme-org/example-repo#42, a pull request."}, nil
}

// waitingSink records every event, and tells waiting tests each time one comes.
type waitingSink struct {
	testutil.Events

	mu    sync.Mutex
	waits []chan struct{}
}

// Emit records e and wakes every test waiting.
func (s *waitingSink) Emit(e event.Event) {
	s.Events.Emit(e)

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, w := range s.waits {
		close(w)
	}

	s.waits = nil
}

// next returns what closes once another event comes.
func (s *waitingSink) next() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()

	w := make(chan struct{})
	s.waits = append(s.waits, w)

	return w
}

// updates returns the describeit:updated events so far.
func (s *waitingSink) updates() []event.DescribeItUpdated {
	var out []event.DescribeItUpdated

	for _, e := range s.All() {
		if u, ok := e.(event.DescribeItUpdated); ok {
			out = append(out, u)
		}
	}

	return out
}

// proposalsEvents counts the describeit:proposals events so far.
func (s *waitingSink) proposalsEvents() int {
	n := 0

	for _, e := range s.All() {
		if _, ok := e.(event.DescribeItProposals); ok {
			n++
		}
	}

	return n
}

// rig is a service in a describe folder of its own, with fake agents, setup,
// attachments, clock, and sink.
type rig struct {
	t      *testing.T
	dir    string
	exe    string
	svc    *Service
	agents *fakeAgents
	setup  *fakeSetup
	clock  *testutil.Clock
	sink   *waitingSink
	stable bool
}

// newRig returns a rig whose Claude Code isn't set up yet (agents' start is the zero one),
// whose setup is testSnapshot, and whose app runs from a stable place.
func newRig(t *testing.T) *rig {
	t.Helper()

	r := &rig{
		t: t, dir: t.TempDir(), exe: filepath.Join(t.TempDir(), brand.Slug), agents: &fakeAgents{},
		setup: &fakeSetup{snap: testSnapshot()}, clock: testutil.NewClock(testNow), sink: &waitingSink{}, stable: true,
	}

	testutil.WriteExecutable(t, r.exe, "")

	r.svc = New(Config{
		Dir: r.dir, Executable: func() string { return r.exe }, Stable: func() bool { return r.stable },
		Agents: r.agents, Setup: r.setup, Attachments: fakeAttachments{}, Clock: r.clock, Sink: r.sink,
	})

	return r
}

// path returns a path under the rig's describe folder.
func (r *rig) path(parts ...string) string {
	return filepath.Join(append([]string{r.dir}, parts...)...)
}

// claude makes the fake Claude Code play run, and the agents start it, ready.
func (r *rig) claude(run testutil.FakeRun) {
	r.t.Helper()

	path, env := testutil.FakeTool(r.t, claudeProgram(r.t), run)
	r.agents.set(Start{
		Ready: true, Program: path, Version: readyVersion(), Env: append(os.Environ(), env), Model: "sonnet",
	})
}

// profileOwn makes model and effort the profile's own, which a choice of Inherit runs.
func (r *rig) profileOwn(model, effort string) {
	r.agents.mu.Lock()
	defer r.agents.mu.Unlock()

	r.agents.start.Model, r.agents.start.Effort = model, effort
}

// waitTurn waits for the turn running now to end, failing the test after a long while.
func (r *rig) waitTurn() {
	r.t.Helper()

	select {
	case <-r.svc.turnDone():
	case <-time.After(time.Minute):
		r.t.Fatal("the turn never ended")
	}
}

// newConversation makes a new conversation with the agent new conversations start with,
// and returns its ID.
func (r *rig) newConversation() string {
	r.t.Helper()

	conv, err := r.svc.NewConversation(r.t.Context(), AgentChoice{})
	require.NoError(r.t, err)
	require.NotEmpty(r.t, conv.ID)

	return conv.ID
}

// turnArgs returns the arguments a turn of the conversation id passes Claude Code, as the
// conversation's half spells them.
func (r *rig) turnArgs(id string, resume bool, model, effort string) []string {
	r.t.Helper()

	args, err := agent.ConversationArgs(agent.Claude, agent.ConversationOptions{
		SessionID: id, Resume: resume, Name: brand.Name + ": Describe it", Model: model, Effort: effort,
		MCPConfig: r.path("conversations", id, "mcp.json"), SystemPromptFile: r.path("instructions.md"),
		Server: brand.Slug, BudgetUSD: 2,
	})
	require.NoError(r.t, err)

	return args
}

// streamFixture returns one of the conversation spike's recorded turns, the first n lines
// of it for n above 0.
func streamFixture(t *testing.T, name string, n int) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("..", "agentrun", "stream", "testdata", "conversation", name)) //nolint:gosec // a fixture the test names
	require.NoError(t, err)

	if n <= 0 {
		return string(data)
	}

	lines := strings.SplitAfter(string(data), "\n")

	return strings.Join(lines[:min(n, len(lines))], "")
}

// pluginFixture returns one of the spike's recorded plugin command outputs, for this
// plugin's marketplace.
func pluginFixture(t *testing.T, name string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("..", "agent", "testdata", "conversation", "plugin", name)) //nolint:gosec // a fixture the test names
	require.NoError(t, err)

	return strings.ReplaceAll(string(data), brand.Slug+"-local", PluginMarketplace)
}

// routineUpdated is when the snapshot's routine last changed.
var routineUpdated = time.Date(2026, time.November, 19, 9, 0, 0, 0, time.UTC) //nolint:gochecknoglobals // a fixed test moment

// testSnapshot is a setup to check cards against: the routine Nightly (7), on in Ask
// first, which tells the user when the checks fail on their pull requests; a starter's
// copy that fixes checks; Settings' defaults; and Dependabot trusted.
func testSnapshot() kit.Snapshot {
	return kit.NewSnapshot(kit.Setup{
		Routines: []kit.Routine{{
			ID: 7, Name: "Nightly", Enabled: true, Mode: autopilotprefs.ModeAsk, Spec: checksFailSpec(notifyStep()),
			UpdatedAt: routineUpdated,
		}},
		Prompts: []kit.Prompt{{
			Starter: "fix-checks", Nickname: "Fix checks", Name: "Fix a pull request's failing checks",
			Purpose: kit.PurposeAgent, AppliesTo: kit.AppliesToPR,
		}},
		Settings: autopilotprefs.Defaults(),
		Rules:    kit.Rules{TrustedBots: []string{"dependabot"}},
	}, kit.InboxNames{Repositories: []string{"acme-org/example-repo"}}, kit.Vocabulary{}, testNow)
}

// checksFailSpec is a routine that runs steps when the checks fail on the user's own pull
// requests.
func checksFailSpec(steps ...spec.Step) spec.Spec {
	return spec.Spec{
		Version: spec.Version,
		Watch:   spec.Watch{Sources: []spec.Source{{Lists: []catalog.ListSource{catalog.SourceAuthored}}}},
		Trigger: spec.Trigger{Event: catalog.EventChecksFailed},
		Steps:   steps,
	}
}

// notifyStep is Notify me.
func notifyStep() spec.Step {
	return spec.Step{Notify: &spec.NotifyStep{}}
}

// pinStep is Pin it.
func pinStep() spec.Step {
	return spec.Step{Pin: &spec.Empty{}}
}

// pinProposal proposes a routine that pins the user's pull requests when their checks
// fail, and one agent at a time.
func pinProposal() kit.Proposal {
	one := 1

	return kit.Proposal{
		Summary:  "Pins your pull requests when their checks fail.",
		Routines: []kit.RoutineChange{{Name: "Pin failing PRs", Mode: autopilotprefs.ModeAuto, Spec: checksFailSpec(pinStep())}},
		Settings: &kit.SettingsChange{AgentsAtOnce: &one},
	}
}

// proposalID returns a proposal's ID for the moment at, its random part from n.
func proposalID(t *testing.T, at time.Time, n byte) string {
	t.Helper()

	id, err := kit.NewProposalID(at, strings.NewReader(strings.Repeat(string(rune('a'+n)), 6)))
	require.NoError(t, err)

	return id
}

// proposalFile returns the proposal p as the tool mode writes it into the folder of
// source, with the ID id, made at at.
func proposalFile(t *testing.T, source, id string, at time.Time, p kit.Proposal) []byte {
	t.Helper()

	f := kit.ProposalFile{ID: id, At: at, SnapshotAt: at, Proposal: p}
	if source == Outside {
		f.Where = "example-repo"
	} else {
		f.Conversation = source
	}

	data, err := kit.EncodeProposalFile(f)
	require.NoError(t, err)

	return data
}

// writeProposal writes the proposal p into the folder of source as the tool mode would,
// and returns its ID.
func (r *rig) writeProposal(source string, at time.Time, n byte, p kit.Proposal) string {
	r.t.Helper()

	id := proposalID(r.t, at, n)

	require.NoError(r.t, os.MkdirAll(r.path("proposals", source), 0o700))
	require.NoError(r.t, os.WriteFile(r.path("proposals", source, id+".json"), proposalFile(r.t, source, id, at, p), 0o600))

	return id
}
