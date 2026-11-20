package describeit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/agentrun/runevent"
	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/mcpserver"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// The turns run a fake Claude Code (testutil.FakeTool) that replays the conversation
// spike's recorded streams, checks the arguments it was started with, keeps the prompt it
// read on stdin, and writes the proposal file the tool mode would have.

// firstTurn runs a first turn of a new conversation that replays turn-first.jsonl and
// proposes pinProposal, and returns the conversation's ID, the proposal's, and the prompt
// the agent read.
func firstTurn(t *testing.T, r *rig, m Message) (id, proposal, prompt string) {
	t.Helper()

	id = r.newConversation()
	proposal = proposalID(t, testNow, 0)
	stdin := filepath.Join(t.TempDir(), "stdin")

	r.claude(testutil.FakeRun{
		WantArgs: r.turnArgs(id, false, "", ""), StdinTo: stdin, Stdout: streamFixture(t, "turn-first.jsonl", 0),
		Files: []testutil.FakeFile{
			{Path: r.path("proposals", id, proposal+".json"), Content: string(proposalFile(t, id, proposal, testNow, pinProposal()))},
			{Path: "ran-here"},
		},
	})

	conv, err := r.svc.Send(t.Context(), m)
	require.NoError(t, err)
	require.Len(t, conv.Turns, 1)
	assert.Equal(t, TurnRunning, conv.Turns[0].State, "Send returns with the turn running")
	assert.True(t, conv.Running)

	r.waitTurn()

	got, err := os.ReadFile(stdin) //nolint:gosec // the fake's copy of its stdin
	require.NoError(t, err)

	return id, proposal, string(got)
}

func TestAFirstTurnRunsClaudeCodeWithTheAppsToolsAlone(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	id, proposal, prompt := firstTurn(t, r, Message{Text: "  Pin my PRs when their checks fail.  "})

	assert.Equal(t, "Pin my PRs when their checks fail.\n", prompt, "the prompt is the user's words, on stdin")
	assert.FileExists(t, r.path(workName, "ran-here"), "the turn runs in the one work folder")

	conv, err := r.svc.Conversation(t.Context())
	require.NoError(t, err)
	assert.Equal(t, id, conv.ID)
	assert.False(t, conv.Running)
	require.Len(t, conv.Turns, 1)

	turn := conv.Turns[0]
	assert.Equal(t, TurnDone, turn.State)
	assert.Empty(t, turn.Error)
	assert.Equal(t, 1, turn.N)
	assert.True(t, strings.HasPrefix(turn.Reply, "I've proposed a routine"), turn.Reply)
	require.NotNil(t, turn.Cost)
	assert.InDelta(t, 0.00173011, *turn.Cost, 1e-12)
	assert.True(t, turn.Estimated, "a turn on the user's plan costs an estimate")
	assert.Equal(t, testNow, turn.StartedAt)
	assert.Equal(t, testNow, turn.EndedAt)

	require.Len(t, turn.Cards, 1, "the proposal the agent made in the turn is its card")
	card := turn.Cards[0]
	assert.Equal(t, CardRef{Source: id, Proposal: proposal}, card.Ref)
	assert.Equal(t, 1, card.Turn)
	assert.Equal(t, pinProposal().Summary, card.Summary)
	require.Len(t, card.Checked.Routines, 1)
	assert.Equal(t, "Pin failing PRs", card.Checked.Routines[0].Name)
	assert.Empty(t, card.Checked.Routines[0].Problems)
	assert.False(t, card.Settled)

	require.NotNil(t, conv.Cost)
	assert.InDelta(t, 0.00173011, *conv.Cost, 1e-12, "the conversation's cost is its latest result's")
	assert.True(t, conv.Estimated)
	assert.Equal(t, int64(12+5391), conv.TokensIn)
	assert.Equal(t, int64(840), conv.TokensOut)

	rec, err := r.svc.readRecord(id)
	require.NoError(t, err)
	assert.True(t, rec.Started, "the session started: the next turn resumes it")
	assert.Equal(t, []string{proposal}, rec.Turns[0].Proposals)
}

func TestATurnsFilesAreTheAppsOwn(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	id, _, _ := firstTurn(t, r, Message{Text: pinText})

	want, err := agent.ConversationFiles(agent.Claude, agent.ConversationSpec{
		Server: brand.Slug, Command: r.exe, Args: []string{mcpserver.Arg(r.dir), mcpserver.ConversationArg(id)},
	})
	require.NoError(t, err)
	require.Len(t, want, 1)

	got, err := os.ReadFile(r.path("conversations", id, want[0].Path))
	require.NoError(t, err)
	assert.Equal(t, string(want[0].Data), string(got), "the MCP config runs the app in its tool mode, for this conversation")

	instructions, err := os.ReadFile(r.path("instructions.md"))
	require.NoError(t, err)
	assert.Equal(t, kit.SkillText(kit.InApp), string(instructions))

	data, err := os.ReadFile(r.path("context.json"))
	require.NoError(t, err)

	snap, err := kit.DecodeSnapshot(data)
	require.NoError(t, err, "the snapshot the tool mode reads was written before the turn")
	assert.Equal(t, "Nightly", snap.Setup.Routines[0].Name)

	for _, path := range []string{r.path("conversations", id, "turn-0001.prompt.md"), r.path("conversations", id, "turn-0001.jsonl")} {
		info, statErr := os.Stat(path)
		require.NoError(t, statErr)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "%s is the user's alone", filepath.Base(path))
	}

	info, err := os.Stat(r.path(workName))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
}

func TestTheNextTurnResumesAndTellsWhatTheUserDid(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	id, proposal, _ := firstTurn(t, r, Message{Text: pinText})

	require.NoError(t, r.svc.Settle(t.Context(), SettleRequest{
		Ref: CardRef{Source: id, Proposal: proposal}, Part: kit.PartRoutine, Kind: kit.OutcomeAdded,
		Words: `Added "Pin failing PRs", switched off`, RoutineID: 12,
	}))
	require.NoError(t, r.svc.Settle(t.Context(), SettleRequest{
		Ref: CardRef{Source: id, Proposal: proposal}, Part: kit.PartSettings, Kind: kit.OutcomeDiscarded,
	}))

	stdin := filepath.Join(t.TempDir(), "stdin")
	r.claude(testutil.FakeRun{
		WantArgs: r.turnArgs(id, true, "", ""), StdinTo: stdin, Stdout: streamFixture(t, "turn-resumed.jsonl", 0),
	})

	_, err := r.svc.Send(t.Context(), Message{Text: "Skip drafts."})
	require.NoError(t, err)
	r.waitTurn()

	prompt, err := os.ReadFile(stdin) //nolint:gosec // the fake's copy of its stdin
	require.NoError(t, err)
	assert.Equal(t, "Added \"Pin failing PRs\", switched off; discarded the settings change.\n\nSkip drafts.\n",
		string(prompt), "what the user did with the cards leads the next turn's prompt")

	conv, err := r.svc.Conversation(t.Context())
	require.NoError(t, err)
	require.Len(t, conv.Turns, 2)

	second := conv.Turns[1]
	assert.Equal(t, TurnDone, second.State)
	assert.Equal(t, "Added \"Pin failing PRs\", switched off; discarded the settings change.", second.Feedback)
	require.NotNil(t, second.Cost)
	assert.InDelta(t, 0.00062771, *second.Cost, 1e-12, "a resumed turn's cost is its own")
	require.NotNil(t, conv.Cost)
	assert.InDelta(t, 0.00235782, *conv.Cost, 1e-12, "and the conversation's is the latest result's total")
	assert.Equal(t, int64(12+5391+6+842), conv.TokensIn, "the conversation's tokens are its turns'")

	first := conv.Turns[0].Cards[0]
	assert.True(t, first.Settled, "every part of the first card was dealt with")
	assert.Len(t, first.Outcomes, 2)

	r.claude(testutil.FakeRun{StdinTo: stdin, Stdout: streamFixture(t, "turn-resumed.jsonl", 0)})

	_, err = r.svc.Send(t.Context(), Message{Text: "Thanks."})
	require.NoError(t, err)
	r.waitTurn()

	prompt, err = os.ReadFile(stdin) //nolint:gosec // the fake's copy of its stdin
	require.NoError(t, err)
	assert.Equal(t, "Thanks.\n", string(prompt), "what a turn told, the next doesn't tell again")
}

func TestAMessageCarriesWhatItAttaches(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	_, _, prompt := firstTurn(t, r, Message{
		Text: "Change this to skip drafts.", Attach: []AttachRef{{Kind: AttachRoutine, ID: 7}, {Kind: AttachItem, ID: 42}},
	})

	assert.Equal(t, "Change this to skip drafts.\n\nThe user attached their routine \"Nightly\" (id 7).\n\n"+
		"The user attached acme-org/example-repo#42, a pull request.\n", prompt)

	conv, err := r.svc.Conversation(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []Attachment{
		{Kind: AttachRoutine, ID: 7, Label: "Nightly", Words: "The user attached their routine \"Nightly\" (id 7)."},
		{
			Kind: AttachItem, ID: 42, Label: "acme-org/example-repo#42",
			Words: "The user attached acme-org/example-repo#42, a pull request.",
		},
	}, conv.Turns[0].Attach)
}

func TestTheConversationsAgentPicksTheModelAndEffort(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.agents.profiles = []Profile{{Name: defaultProfile, Default: true}, {Name: otherProfile}}
	r.claude(testutil.FakeRun{})

	conv, err := r.svc.NewConversation(t.Context(), AgentChoice{Profile: otherProfile, Model: "opus", Effort: "high"})
	require.NoError(t, err)
	assert.Equal(t, AgentChoice{Profile: otherProfile, Model: "opus", Effort: "high"}, conv.Agent)
	assert.Equal(t, otherProfile, conv.Profile)
	assert.Equal(t, "opus", conv.Model)

	r.claude(testutil.FakeRun{WantArgs: r.turnArgs(conv.ID, false, "opus", "high"), Stdout: streamFixture(t, "turn-first.jsonl", 0)})

	_, err = r.svc.Send(t.Context(), Message{Text: pinText})
	require.NoError(t, err)
	r.waitTurn()

	got, err := r.svc.Conversation(t.Context())
	require.NoError(t, err)
	assert.Equal(t, TurnDone, got.Turns[0].State, "the turn ran with the conversation's model and effort")
	assert.Contains(t, r.agents.asked, otherProfile, "with the conversation's profile")
}

func TestStoppingATurnEndsItAsStopped(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	id := r.newConversation()

	r.claude(testutil.FakeRun{
		Stdout: streamFixture(t, "turn-sigint.jsonl", 2), WaitFor: filepath.Join(t.TempDir(), "never"), Sleep: time.Minute,
		OnInterrupt: &testutil.FakeInterrupt{Stdout: strings.TrimPrefix(streamFixture(t, "turn-sigint.jsonl", 0),
			streamFixture(t, "turn-sigint.jsonl", 2))},
	})

	_, err := r.svc.Send(t.Context(), Message{Text: "Do something slow."})
	require.NoError(t, err)

	waitForEvents(t, r, id, 1, func(p runevent.Page) bool { return len(p.Events) >= 2 })

	st, err := r.svc.Status(t.Context())
	require.NoError(t, err)
	assert.True(t, st.Running)

	require.NoError(t, r.svc.Stop(t.Context(), "00000000-0000-4000-8000-000000000009"), "another conversation's stop is no-one's")
	require.NoError(t, r.svc.Stop(t.Context(), id))
	r.waitTurn()

	conv, err := r.svc.Conversation(t.Context())
	require.NoError(t, err)

	turn := conv.Turns[0]
	assert.Equal(t, TurnStopped, turn.State, "a turn the user stopped ends as stopped, its result in error")
	assert.Empty(t, turn.Error)
	require.NotNil(t, turn.Cost, "what it cost until then is in its result")

	page, err := r.svc.Events(t.Context(), id, 1, 0)
	require.NoError(t, err)
	assert.True(t, page.Done)
	assert.Equal(t, runevent.KindResult, page.Events[len(page.Events)-1].Kind)

	require.NoError(t, r.svc.Stop(t.Context(), id), "stopping a turn that ended is nothing")
}

// waitForEvents waits until the events of the conversation id's turn n satisfy ok.
func waitForEvents(t *testing.T, r *rig, id string, n int, ok func(runevent.Page) bool) {
	t.Helper()

	deadline := time.After(time.Minute)

	for {
		next := r.sink.next()

		page, err := r.svc.Events(t.Context(), id, n, 0)
		require.NoError(t, err)

		if ok(page) {
			return
		}

		select {
		case <-next:
		case <-deadline:
			t.Fatal("the turn's events never came")
		}
	}
}

func TestATurnOutOfItsBudgetFails(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.newConversation()
	r.claude(testutil.FakeRun{Stdout: streamFixture(t, "turn-budget.jsonl", 0), Exit: 1})

	_, err := r.svc.Send(t.Context(), Message{Text: "Do everything."})
	require.NoError(t, err)
	r.waitTurn()

	conv, err := r.svc.Conversation(t.Context())
	require.NoError(t, err)
	assert.Equal(t, TurnFailed, conv.Turns[0].State)
	assert.Contains(t, conv.Turns[0].Error, "budget of $2", "it says the turn's budget")
}

func TestATurnWhoseToolsDidntStartIsStoppedAtOnce(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	id := r.newConversation()

	r.claude(testutil.FakeRun{
		Stdout: streamFixture(t, "turn-mcp-failed.jsonl", 1), WaitFor: filepath.Join(t.TempDir(), "never"), Sleep: time.Minute,
		OnInterrupt: &testutil.FakeInterrupt{Exit: 0},
	})

	_, err := r.svc.Send(t.Context(), Message{Text: pinText})
	require.NoError(t, err)
	r.waitTurn()

	conv, err := r.svc.Conversation(t.Context())
	require.NoError(t, err)
	assert.Equal(t, TurnFailed, conv.Turns[0].State, "a turn without the app's tools stops before it improvises")
	assert.Contains(t, conv.Turns[0].Error, "Autopilot tools didn't start")

	page, err := r.svc.Events(t.Context(), id, 1, 0)
	require.NoError(t, err)
	require.NotEmpty(t, page.Events)
	assert.Equal(t, runevent.KindStarted, page.Events[0].Kind)
}

func TestATurnWithAToolNotTheAppsIsStopped(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.newConversation()

	initLine := `{"type":"system","subtype":"init","tools":["` + agent.ConversationToolPrefix(agent.Claude, brand.Slug) +
		`autopilot_guide","Bash"],"mcp_servers":[{"name":"` + brand.Slug + `","status":"connected"},` +
		`{"name":"other","status":"connected"}],"model":"m","claude_code_version":"` + readyVersion() + `"}` + "\n"

	r.claude(testutil.FakeRun{
		Stdout: initLine, WaitFor: filepath.Join(t.TempDir(), "never"), Sleep: time.Minute,
		OnInterrupt: &testutil.FakeInterrupt{Exit: 0},
	})

	_, err := r.svc.Send(t.Context(), Message{Text: pinText})
	require.NoError(t, err)
	r.waitTurn()

	conv, err := r.svc.Conversation(t.Context())
	require.NoError(t, err)
	assert.Equal(t, TurnFailed, conv.Turns[0].State)
	assert.Contains(t, conv.Turns[0].Error, "tools that aren't "+brand.Name+"'s (Bash, other)")
}

func TestATurnThatEndsWithoutAResultSaysWhy(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.newConversation()
	r.claude(testutil.FakeRun{Stdout: "Error: Session ID is already in use.\n", Exit: 1})

	_, err := r.svc.Send(t.Context(), Message{Text: pinText})
	require.NoError(t, err)
	r.waitTurn()

	conv, err := r.svc.Conversation(t.Context())
	require.NoError(t, err)
	assert.Equal(t, TurnFailed, conv.Turns[0].State)
	assert.Equal(t, agentWords+" ended without finishing the turn: Error: Session ID is already in use.", conv.Turns[0].Error)

	rec, err := r.svc.readRecord(conv.ID)
	require.NoError(t, err)
	assert.True(t, rec.Started, "a turn refused before its init names the session the other way next")

	r.claude(testutil.FakeRun{
		WantArgs: r.turnArgs(conv.ID, true, "", ""), Stdout: "Error: No conversation found with session ID.\n", Exit: 1,
	})

	_, err = r.svc.Send(t.Context(), Message{Text: pinText})
	require.NoError(t, err)
	r.waitTurn()

	rec, err = r.svc.readRecord(conv.ID)
	require.NoError(t, err)
	assert.False(t, rec.Started, "and the other way again, so a conversation never sticks on its session")

	r.claude(testutil.FakeRun{WantArgs: r.turnArgs(conv.ID, false, "", ""), Stdout: streamFixture(t, "turn-first.jsonl", 0)})

	_, err = r.svc.Send(t.Context(), Message{Text: pinText})
	require.NoError(t, err)
	r.waitTurn()

	got, err := r.svc.Conversation(t.Context())
	require.NoError(t, err)
	assert.Equal(t, TurnDone, got.Turns[2].State)

	rec, err = r.svc.readRecord(conv.ID)
	require.NoError(t, err)
	assert.True(t, rec.Started, "a turn that got as far as its init started the session")
}

func TestATurnSentBeforeTheWorkerClosedACrashedOneClosesItFirst(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	id := r.crashedTurn(streamFixture(t, "turn-first.jsonl", 0))
	r.claude(testutil.FakeRun{WantArgs: r.turnArgs(id, true, "", ""), Stdout: streamFixture(t, "turn-resumed.jsonl", 0)})

	_, err := r.svc.Send(t.Context(), Message{Text: "Skip drafts."})
	require.NoError(t, err)
	r.waitTurn()

	conv, err := r.svc.Conversation(t.Context())
	require.NoError(t, err)
	require.Len(t, conv.Turns, 2)
	assert.Equal(t, TurnDone, conv.Turns[0].State, "the crashed turn is closed from its stream")
	assert.Equal(t, TurnDone, conv.Turns[1].State, "and the new one resumes the session it started")
}

func TestAnExpiredConversationIsntCarriedOn(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	old := r.newConversation()
	r.clock.Advance(keepConversations + time.Hour)
	r.claude(testutil.FakeRun{Stdout: streamFixture(t, "turn-first.jsonl", 0)})

	conv, err := r.svc.Send(t.Context(), Message{Text: pinText})
	require.NoError(t, err)
	assert.NotEqual(t, old, conv.ID, "a message starts a new conversation once the old one is gone")
	assert.NoDirExists(t, r.path("conversations", old))
	r.waitTurn()
}

func TestATurnWhoseAgentCantStartIsKeptAsFailed(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.agents.set(Start{Ready: true, Program: filepath.Join(t.TempDir(), "missing"), Version: readyVersion()})

	conv, err := r.svc.Send(t.Context(), Message{Text: pinText})
	require.Error(t, err)
	assert.Equal(t, agentWords+" couldn't start, so the turn didn't run.", uierr.Message(err))
	require.Len(t, conv.Turns, 1, "the turn is kept, with why it failed")
	assert.Equal(t, TurnFailed, conv.Turns[0].State)
	assert.False(t, conv.Running)

	st, err := r.svc.Status(t.Context())
	require.NoError(t, err)
	assert.False(t, st.Running, "and nothing runs")
}

func TestSendIsRefusedWhenItCantRunATurn(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	tests := []struct {
		name  string
		setup func(r *rig)
		msg   Message
		want  string
	}{
		{name: "an empty message", msg: Message{Text: "  \n "}, want: "Write what you want Autopilot to do."},
		{
			name: "a message too long", msg: Message{Text: strings.Repeat("a", maxMessageRunes+1)},
			want: "Keep a message under 20000 characters.",
		},
		{
			name: "too many attachments", msg: Message{Text: "x", Attach: make([]AttachRef, maxAttachments+1)},
			want: "Attach at most 10 things to a message.",
		},
		{
			name: "claude code not ready", msg: Message{Text: "x"},
			setup: func(r *rig) {
				r.agents.set(Start{Problem: agentWords + " isn't signed in.", Fix: "Sign in, then try again."})
			},
			want: agentWords + " isn't signed in. Sign in, then try again.",
		},
		{
			name: "claude code too old", msg: Message{Text: "x"},
			setup: func(r *rig) { r.agents.set(Start{Ready: true, Program: "/bin/true", Version: "2.0.1"}) },
			want:  agentWords + " 2.0.1 is older than Describe it needs (" + readyVersion() + "). Update " + agentWords + ", then try again.",
		},
		{
			name: "no executable", msg: Message{Text: "x"},
			setup: func(r *rig) { r.exe = "" },
			want:  brand.Name + " can't find its own program to give " + agentWords + " its tools.",
		},
		{
			name: "an attachment of no kind", msg: Message{Text: "x", Attach: []AttachRef{{Kind: "file", ID: 1}}},
			want: "Describe it can't attach that.",
		},
		{
			name: "an attachment that isn't there", msg: Message{Text: "x", Attach: []AttachRef{{Kind: AttachRoutine, ID: 99}}},
			want: "Describe it couldn't read what you attached.",
		},
		{
			name: "a setup that can't be read", msg: Message{Text: "x"},
			setup: func(r *rig) { r.setup.fail(errFake) },
			want:  "Describe it couldn't read your setup.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newRig(t)
			r.claude(testutil.FakeRun{})

			if tt.setup != nil {
				tt.setup(r)
			}

			conv, err := r.svc.Send(ctx, tt.msg)
			require.Error(t, err)
			assert.Equal(t, tt.want, uierr.Message(err))
			assert.NotNil(t, conv.Turns)

			st, err := r.svc.Status(ctx)
			require.NoError(t, err)
			assert.False(t, st.Running, "a refused message leaves nothing running")
		})
	}
}

func TestOneTurnRunsAtATime(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	id := r.newConversation()

	r.claude(testutil.FakeRun{
		Stdout: streamFixture(t, "turn-sigint.jsonl", 2), WaitFor: filepath.Join(t.TempDir(), "never"), Sleep: time.Minute,
		OnInterrupt: &testutil.FakeInterrupt{},
	})

	_, err := r.svc.Send(t.Context(), Message{Text: "First."})
	require.NoError(t, err)

	_, err = r.svc.Send(t.Context(), Message{Text: "Second."})
	require.Error(t, err)
	assert.Equal(t, busyWords, uierr.Message(err))

	_, err = r.svc.NewConversation(t.Context(), AgentChoice{})
	require.Error(t, err)
	assert.Equal(t, busyWords, uierr.Message(err), "a new conversation waits for the turn too")

	require.NoError(t, r.svc.Stop(t.Context(), id))
	r.waitTurn()

	conv, err := r.svc.Conversation(t.Context())
	require.NoError(t, err)
	assert.Len(t, conv.Turns, 1)
}

func TestTheWindowIsToldAsATurnMovesOn(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	id, _, _ := firstTurn(t, r, Message{Text: pinText})

	updates := r.sink.updates()
	require.GreaterOrEqual(t, len(updates), 3, "the conversation made, the turn started, and its end")
	assert.Equal(t, event.DescribeItUpdated{ConversationID: id}, updates[0], "the new conversation as a whole")
	assert.Equal(t, event.DescribeItUpdated{ConversationID: id, Turn: 1}, updates[1], "the turn as it starts")
	assert.Equal(t, event.DescribeItUpdated{ConversationID: id, Turn: 1}, updates[len(updates)-1], "and as it ends")
}
