package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/mcpserver"
)

// toolConversation is the in-app conversation the tool mode's test serves.
const toolConversation = "6f1c2a4e-9b7d-4c3e-8a21-0d5e7f9b3c11"

// toolSession is the executable run in its tool mode as the agent's client runs it: an
// empty home and nothing else in its environment, in its session's folder, one message a
// line over its stdin and stdout.
type toolSession struct {
	t      *testing.T
	cmd    *exec.Cmd
	in     io.WriteCloser
	out    *bufio.Reader
	stderr *bytes.Buffer
}

// startToolMode starts exe with args in the folder cwd, home its only home.
func startToolMode(t *testing.T, exe, home, cwd string, args ...string) *toolSession {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), exe, args...) //nolint:gosec // the test's own binary
	cmd.Env = []string{"HOME=" + home, "TMPDIR=" + home, "PATH=/usr/bin:/bin"}
	cmd.Dir = cwd
	cmd.WaitDelay = 10 * time.Second

	in, err := cmd.StdinPipe()
	require.NoError(t, err)

	out, err := cmd.StdoutPipe()
	require.NoError(t, err)

	s := &toolSession{t: t, cmd: cmd, in: in, out: bufio.NewReader(out), stderr: &bytes.Buffer{}}
	cmd.Stderr = s.stderr

	require.NoError(t, cmd.Start())

	return s
}

// send writes one message.
func (s *toolSession) send(msg map[string]any) {
	s.t.Helper()

	line, err := json.Marshal(msg)
	require.NoError(s.t, err)

	_, err = s.in.Write(append(line, '\n'))
	require.NoError(s.t, err)
}

// request sends the request id of method with params and reads its reply's result, with
// stdin still open.
func (s *toolSession) request(id int, method string, params any) json.RawMessage {
	s.t.Helper()

	s.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})

	line, err := s.out.ReadBytes('\n')
	require.NoError(s.t, err, "stderr: %s", s.stderr)

	var r struct {
		ID     int             `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}

	require.NoError(s.t, json.Unmarshal(line, &r))
	require.Equal(s.t, id, r.ID)
	require.Empty(s.t, r.Error)

	return r.Result
}

// toolSnapshot is a setup with no routine yet, and Settings' defaults.
func toolSnapshot(t *testing.T) []byte {
	t.Helper()

	data, err := kit.EncodeSnapshot(kit.NewSnapshot(kit.Setup{Settings: autopilotprefs.Defaults()}, kit.InboxNames{},
		kit.Vocabulary{}, time.Date(2026, time.November, 20, 5, 31, 3, 0, time.UTC)))
	require.NoError(t, err)

	return data
}

// toolProposal is a proposal of a routine that notifies the user when the checks fail on
// their pull requests.
func toolProposal() kit.Proposal {
	return kit.Proposal{Summary: "Tells you when checks fail.", Routines: []kit.RoutineChange{{
		Name: "Tell me",
		Spec: spec.Spec{
			Version: spec.Version,
			Watch:   spec.Watch{Sources: []spec.Source{{Lists: []catalog.ListSource{catalog.SourceAuthored}}}},
			Trigger: spec.Trigger{Event: catalog.EventChecksFailed},
			Steps:   []spec.Step{{Notify: &spec.NotifyStep{}}},
		},
	}}}
}

func TestTheToolModeServesAndCreatesNothingButTheProposal(t *testing.T) {
	t.Parallel()

	exe, err := os.Executable()
	require.NoError(t, err)

	tests := []struct {
		where  kit.Where
		args   func(describe string) []string
		folder string
	}{
		{
			kit.InApp, func(d string) []string {
				return []string{mcpserver.Arg(d), mcpserver.ConversationArg(toolConversation)}
			},
			toolConversation,
		},
		{kit.Outside, func(d string) []string { return []string{mcpserver.Arg(d)} }, "outside"},
	}

	for _, tt := range tests {
		t.Run(string(tt.where), func(t *testing.T) {
			t.Parallel()

			home := t.TempDir()
			root := t.TempDir()
			describe := filepath.Join(root, "describe")
			cwd := filepath.Join(root, "example-repo")
			proposals := filepath.Join(describe, "proposals", tt.folder)

			for _, dir := range []string{
				filepath.Join(describe, "proposals", toolConversation),
				filepath.Join(describe, "proposals", "outside"), cwd,
			} {
				require.NoError(t, os.MkdirAll(dir, 0o700))
			}

			require.NoError(t, os.WriteFile(filepath.Join(describe, "context.json"), toolSnapshot(t), 0o600))

			before := listFiles(t, root)
			id := serveOneProposal(t, exe, home, cwd, tt.where, tt.args(describe))

			file := filepath.Join(proposals, id+".json")
			assert.Equal(t, sortedFiles(append(slices.Clone(before), file)), sortedFiles(listFiles(t, root)),
				"the tool mode writes the proposal file and nothing else: no temporary file left, no log")
			assert.Equal(t, []string{home}, listFiles(t, home), "and nothing in home")

			data, err := os.ReadFile(file) //nolint:gosec // the file the test's tool mode wrote
			require.NoError(t, err)

			f, err := kit.DecodeProposalFile(data)
			require.NoError(t, err)

			if tt.where == kit.InApp {
				assert.Equal(t, toolConversation, f.Conversation)
			} else {
				assert.Equal(t, "example-repo", f.Where, "the folder the session runs in, as the system reports it")
			}
		})
	}
}

// serveOneProposal runs a session of the tool mode where w says: initialize, the tool
// list, and one proposal, then ends it as the agent's client does, with a signal, in the
// app, and from outside by closing stdin, and returns the proposal's ID.
func serveOneProposal(t *testing.T, exe, home, cwd string, w kit.Where, args []string) string {
	t.Helper()

	s := startToolMode(t, exe, home, cwd, args...)

	var hello struct {
		ServerInfo struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
	}

	require.NoError(t, json.Unmarshal(s.request(0, "initialize", map[string]any{"protocolVersion": "2025-11-25"}), &hello))
	assert.Equal(t, brand.Slug, hello.ServerInfo.Name)

	s.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})

	var listed struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}

	require.NoError(t, json.Unmarshal(s.request(1, "tools/list", nil), &listed))
	assert.Len(t, listed.Tools, len(kit.Tools(w)))

	var proposed struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}

	require.NoError(t, json.Unmarshal(s.request(2, "tools/call", map[string]any{
		"name": kit.ToolPropose, "arguments": map[string]any{"proposal": toolProposal()},
	}), &proposed))
	require.Len(t, proposed.Content, 1)
	require.False(t, proposed.IsError, proposed.Content[0].Text)

	id := proposalIDIn(proposed.Content[0].Text)
	require.True(t, kit.ValidProposalID(id), proposed.Content[0].Text)

	if w == kit.InApp {
		require.NoError(t, s.cmd.Process.Signal(os.Interrupt))

		var exit *exec.ExitError

		require.ErrorAs(t, s.cmd.Wait(), &exit, "ended by the signal, its proposal written before it came")
	} else {
		require.NoError(t, s.in.Close())
		require.NoError(t, s.cmd.Wait(), "stdin's end ends the session: %s", s.stderr)
	}

	assert.Empty(t, s.stderr.String())

	return id
}

// proposalIDIn returns the first word in backticks in a proposal's answer: its ID.
func proposalIDIn(text string) string {
	_, rest, ok := strings.Cut(text, "`")
	if !ok {
		return ""
	}

	id, _, _ := strings.Cut(rest, "`")

	return id
}
