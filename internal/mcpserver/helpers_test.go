package mcpserver_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/mcpserver"
)

// conversationID is the in-app conversation the tests serve.
const conversationID = "6f1c2a4e-9b7d-4c3e-8a21-0d5e7f9b3c11"

// sessionFolder is the base name of the folder an outside session runs in.
const sessionFolder = "example-repo"

// The moments the fixtures hold: when the routines last changed, to the nanosecond as the
// database stamps one, when the snapshot was written, and when the tool mode runs.
var (
	routinesAt = time.Date(2026, time.November, 20, 5, 31, 3, 123456789, time.UTC) //nolint:gochecknoglobals // a fixed test moment
	snapshotAt = routinesAt.Add(time.Hour)                                         //nolint:gochecknoglobals // a fixed test moment
	servedAt   = snapshotAt.Add(time.Hour)                                         //nolint:gochecknoglobals // a fixed test moment
)

// places are where the tool mode serves an agent: in the app, and in the user's own
// Claude Code.
func places() []kit.Where {
	return []kit.Where{kit.InApp, kit.Outside}
}

// checksFail is a routine that runs steps when the checks fail on the user's own pull
// requests.
func checksFail(steps ...spec.Step) spec.Spec {
	return spec.Spec{
		Version: spec.Version,
		Watch:   spec.Watch{Sources: []spec.Source{{Lists: []catalog.ListSource{catalog.SourceAuthored}}}},
		Trigger: spec.Trigger{Event: catalog.EventChecksFailed},
		Steps:   steps,
	}
}

// notify is Notify me.
func notify() spec.Step {
	return spec.Step{Notify: &spec.NotifyStep{}}
}

// lintScript is a step that runs the user's script Lint.
func lintScript() spec.Step {
	return spec.Step{Script: &spec.ScriptStep{ID: 3, Name: "Lint"}}
}

// fixtureSnapshot is a setup to serve: the routine Nightly, on in Ask first; a library of
// a starter's copy and a prompt of the user's own; the script Lint; an agent profile;
// Settings' defaults; rules trusting Dependabot with acme-org/api pinned; the inbox's
// names; and a template and the starters.
func fixtureSnapshot() kit.Snapshot {
	return kit.NewSnapshot(kit.Setup{
		Routines: []kit.Routine{{
			ID: 7, Name: "Nightly", Description: "Tells me when my checks fail.", Enabled: true,
			Mode: autopilotprefs.ModeAsk, Sentence: "When checks fail on pull requests opened by you, notify me.",
			Spec: checksFail(notify()), UpdatedAt: routinesAt,
		}},
		Prompts: []kit.Prompt{
			{
				Starter: "fix-checks", Nickname: "Fix checks", Name: "Fix a pull request's failing checks",
				Purpose: kit.PurposeAgent, AppliesTo: kit.AppliesToPR,
			},
			{Nickname: "Triage", Name: "Triage an item", Purpose: kit.PurposeAgent, AppliesTo: kit.AppliesToAny},
		},
		Scripts:  []kit.Script{{ID: 3, Name: "Lint", Shows: "every clone"}},
		Profiles: []kit.Profile{{Agent: "claude", Name: "work"}},
		Settings: autopilotprefs.Defaults(),
		Rules:    kit.Rules{TrustedBots: []string{"dependabot"}, Pinned: []string{"acme-org/api"}},
	}, kit.InboxNames{
		Repositories: []string{"acme-org/api", "acme-org/web"}, Owners: []string{"acme-org"},
		People: []string{"octocat"}, Bots: []string{"dependabot[bot]"}, Labels: []string{"bug"},
		Checks: []string{"CI / test"},
	}, kit.Vocabulary{
		Templates: []kit.Template{{
			Key: "tell-me-when-checks-fail", Name: "Tell me when checks fail", Suggests: autopilotprefs.ModeAuto,
			Spec: checksFail(notify()),
		}},
		Starters: []kit.Starter{{
			Key: "fix-checks", Name: "Fix a pull request's failing checks", Nickname: "Fix checks",
			AppliesTo: kit.AppliesToPR, Purpose: kit.PurposeAgent,
		}},
		Variables: []kit.Variable{{Name: "Repo", Group: "Repository", Type: "text"}},
	}, snapshotAt)
}

// rig is a describe folder as the app keeps it: the snapshot written, the conversation's
// proposals folder and the outside one made; and the folder an outside session runs in.
type rig struct {
	dir  string
	cwd  string
	snap kit.Snapshot
}

// newRig returns a describe folder holding fixtureSnapshot.
func newRig(t *testing.T) rig {
	t.Helper()

	r := rig{
		dir:  filepath.Join(t.TempDir(), "describe"),
		cwd:  filepath.Join(t.TempDir(), sessionFolder),
		snap: fixtureSnapshot(),
	}

	require.NoError(t, os.MkdirAll(r.proposals(kit.InApp), 0o700))
	require.NoError(t, os.MkdirAll(r.proposals(kit.Outside), 0o700))
	require.NoError(t, os.MkdirAll(r.cwd, 0o700))
	r.writeSnapshot(t, r.snap)

	return r
}

// writeSnapshot writes snap as the app's snapshot.
func (r rig) writeSnapshot(t *testing.T, snap kit.Snapshot) {
	t.Helper()

	data, err := kit.EncodeSnapshot(snap)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(r.dir, "context.json"), data, 0o600))
}

// proposals is the proposals folder of w: the conversation's in the app, else the
// outside one.
func (r rig) proposals(w kit.Where) string {
	if w == kit.InApp {
		return filepath.Join(r.dir, "proposals", conversationID)
	}

	return filepath.Join(r.dir, "proposals", "outside")
}

// args are the tool mode's arguments where w says.
func (r rig) args(w kit.Where) []string {
	if w == kit.InApp {
		return []string{mcpserver.Arg(r.dir), mcpserver.ConversationArg(conversationID)}
	}

	return []string{mcpserver.Arg(r.dir)}
}

// host is the tests' host: servedAt, counting randomness, and the session's folder.
func (r rig) host() mcpserver.Host {
	return mcpserver.Host{Now: servedAt, Random: &counter{}, Cwd: r.cwd}
}

// files lists the proposals folder of w, by name.
func (r rig) files(t *testing.T, w kit.Where) []string {
	t.Helper()

	entries, err := os.ReadDir(r.proposals(w))
	require.NoError(t, err)

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}

	return names
}

// readProposal reads the proposal file id where w says, as the app reads one.
func (r rig) readProposal(t *testing.T, w kit.Where, id string) kit.ProposalFile {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(r.proposals(w), id+".json")) //nolint:gosec // a file the test's tool mode wrote
	require.NoError(t, err)

	f, err := kit.DecodeProposalFile(data)
	require.NoError(t, err)

	return f
}

// counter is randomness that counts, so each proposal's ID differs, and reads the same on
// every run.
type counter struct {
	n byte
}

// Read fills p with the next bytes of the count.
func (c *counter) Read(p []byte) (int, error) {
	for i := range p {
		c.n++
		p[i] = c.n
	}

	return len(p), nil
}

// reply is one answer the tool mode wrote.
type reply struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *replyError     `json:"error"`
}

// replyError is a reply's error.
type replyError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// toolResult is a tools/call's result.
type toolResult struct {
	Content           []textContent   `json:"content"`
	IsError           bool            `json:"isError"`
	StructuredContent json.RawMessage `json:"structuredContent"`
}

// textContent is one part of a tool's result.
type textContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// text returns the result's one text, failing t on any other content.
func (r toolResult) text(t *testing.T) string {
	t.Helper()

	require.Len(t, r.Content, 1)
	assert.Equal(t, "text", r.Content[0].Type)
	assert.Empty(t, r.StructuredContent, "never structured content: the client shows it in place of the text")

	return r.Content[0].Text
}

// parseReplies reads the replies in out, one a line, failing t on a line that isn't one.
func parseReplies(t *testing.T, out []byte) []reply {
	t.Helper()

	var replies []reply

	for line := range strings.Lines(string(out)) {
		var r reply

		require.NoError(t, json.Unmarshal([]byte(line), &r), "each line is one reply: %s", line)
		assert.Equal(t, "2.0", r.JSONRPC)
		replies = append(replies, r)
	}

	return replies
}

// serveAll runs the tool mode on the lines in stdin to their end, and returns its status,
// its replies, and what it wrote on stderr.
func serveAll(t *testing.T, args []string, h mcpserver.Host, stdin string) (int, []reply, string) {
	t.Helper()

	var stdout, stderr bytes.Buffer

	code, handled := mcpserver.ServeOn(args, strings.NewReader(stdin), &stdout, &stderr, h)
	require.True(t, handled)

	return code, parseReplies(t, stdout.Bytes()), stderr.String()
}

// session is the tool mode serving one session over pipes, as the agent's client runs it:
// each message written, then its reply read, with stdin open throughout.
type session struct {
	t      *testing.T
	in     *io.PipeWriter
	out    *bufio.Reader
	stderr *bytes.Buffer
	done   chan int
	next   int
}

// startSession starts the tool mode on args and h, in a goroutine of the test's own: run
// it in a synctest bubble, which fails the test at once should the tool mode wait for
// stdin to end before it answers.
func startSession(t *testing.T, args []string, h mcpserver.Host) *session {
	t.Helper()

	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	s := &session{t: t, in: inW, out: bufio.NewReader(outR), stderr: &bytes.Buffer{}, done: make(chan int, 1)}

	go func() {
		code, _ := mcpserver.ServeOn(args, inR, outW, s.stderr, h)
		_ = outW.Close()

		s.done <- code
	}()

	t.Cleanup(func() {
		_ = inW.Close()
		_ = outR.Close()
	})

	return s
}

// send writes one message, as one line.
func (s *session) send(msg any) {
	s.t.Helper()

	line, err := json.Marshal(msg)
	require.NoError(s.t, err)

	_, err = s.in.Write(append(line, '\n'))
	require.NoError(s.t, err)
}

// read reads the next reply.
func (s *session) read() reply {
	s.t.Helper()

	line, err := s.out.ReadBytes('\n')
	require.NoError(s.t, err)

	var r reply

	require.NoError(s.t, json.Unmarshal(line, &r))

	return r
}

// request sends a request of method with params, under the next ID, and reads its reply,
// which echoes the ID.
func (s *session) request(method string, params any) reply {
	s.t.Helper()

	s.next++
	s.send(map[string]any{"jsonrpc": "2.0", "id": s.next, "method": method, "params": params})

	r := s.read()
	assert.JSONEq(s.t, string(mustJSON(s.t, s.next)), string(r.ID))

	return r
}

// call calls tool with args, and returns its result.
func (s *session) call(tool string, args any) toolResult {
	s.t.Helper()

	r := s.request("tools/call", map[string]any{"name": tool, "arguments": args})
	require.Nil(s.t, r.Error, "a tool's refusal is its result, never an error")

	var res toolResult

	require.NoError(s.t, json.Unmarshal(r.Result, &res))

	return res
}

// close ends stdin, and returns the status the tool mode ended with.
func (s *session) close() int {
	s.t.Helper()

	require.NoError(s.t, s.in.Close())

	return <-s.done
}

// mustJSON returns v as JSON.
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()

	data, err := json.Marshal(v)
	require.NoError(t, err)

	return data
}

// newRoutine is a proposal of one new routine, Pin it, named name, that runs sp.
func newRoutine(name string, sp spec.Spec) kit.Proposal {
	return kit.Proposal{Summary: "Adds " + name + ".", Routines: []kit.RoutineChange{{Name: name, Spec: sp}}}
}

// proposalArgs are a check's or a proposal's arguments: p as `proposal`.
func proposalArgs(t *testing.T, p kit.Proposal) map[string]json.RawMessage {
	t.Helper()

	return map[string]json.RawMessage{"proposal": mustJSON(t, p)}
}

// idOf returns the proposal ID a proposal's answer names, failing t when it names none.
func idOf(t *testing.T, text string) string {
	t.Helper()

	for field := range strings.FieldsFuncSeq(text, func(r rune) bool { return r == '`' }) {
		if kit.ValidProposalID(field) {
			return field
		}
	}

	require.Fail(t, "the answer names no proposal ID", text)

	return ""
}
