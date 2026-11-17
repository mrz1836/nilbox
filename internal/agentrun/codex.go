package agentrun

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/agentrun/runevent"
)

// Codex's stream, `codex exec --json`, as S6 recorded it from codex-cli 0.163.0: one JSON
// event a line, of the types thread.started (the thread's ID, which Codex picks and the
// run's session is linked to), turn.started, item.started, item.updated, and
// item.completed (an agent_message, reasoning, a command_execution, a file_change, an
// mcp_tool_call, a web_search, a todo_list, or an error item, which carries a warning),
// then turn.completed, with the turn's tokens, or turn.failed; and top-level error events,
// its retries. A stream that ends with neither turn.completed nor turn.failed was cut off,
// as a SIGINT leaves it: exit 1 and no last event. It reports tokens, never a cost; the
// report is the last agent_message, the JSON its strict schema asked for. A call its fence
// hook refused never appears in it (the hook logs those in the run's folder).

// codexLine is what the parser reads of one line; every other field is skipped.
type codexLine struct {
	Type     string      `json:"type"`
	ThreadID string      `json:"thread_id"`
	Item     *codexItem  `json:"item"`
	Usage    *codexUsage `json:"usage"`

	// Error is a failed turn's, and Message a top-level error event's.
	Error   *codexError `json:"error"`
	Message string      `json:"message"`
}

// codexItem is one item of a turn.
type codexItem struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Status string `json:"status"`

	// agent_message, reasoning, and an error item's warning
	Text    string `json:"text"`
	Message string `json:"message"`

	// command_execution
	Command  string `json:"command"`
	Output   string `json:"aggregated_output"`
	ExitCode *int   `json:"exit_code"`

	// file_change
	Changes []struct {
		Path string `json:"path"`
		Kind string `json:"kind"`
	} `json:"changes"`

	// mcp_tool_call
	Server string          `json:"server"`
	Tool   string          `json:"tool"`
	Result json.RawMessage `json:"result"`

	// web_search
	Query string `json:"query"`

	// todo_list
	Items []struct {
		Text      string `json:"text"`
		Completed bool   `json:"completed"`
	} `json:"items"`
}

// codexUsage is a turn's tokens: its input counts what it read from the cache, and wrote to
// it, within it.
type codexUsage struct {
	Input     int64 `json:"input_tokens"`
	CacheRead int64 `json:"cached_input_tokens"`
	Output    int64 `json:"output_tokens"`
}

// codexError is why a turn failed.
type codexError struct {
	Message string `json:"message"`
}

// newCodexParser returns a parser of Codex's stream, for a run in dir.
func newCodexParser(dir string) *stream {
	return newStream(readCodexLine, dir)
}

// readCodexLine reads one line of Codex's stream.
func readCodexLine(s *stream, raw []byte) {
	var line codexLine
	if err := json.Unmarshal(raw, &line); err != nil {
		s.unknown()

		return
	}

	switch line.Type {
	case "thread.started":
		s.agentSession = line.ThreadID
		s.emit(runevent.Event{Kind: runevent.KindStarted, Text: "Codex"})
		s.step("Starting")
	case "turn.started":
		s.step("Working")
	case "item.started", "item.updated", "item.completed":
		codexItemEvent(s, line.Type, line.Item)
	case "turn.completed":
		codexTurnCompleted(s, line.Usage)
	case "turn.failed":
		message := ""
		if line.Error != nil {
			message = line.Error.Message
		}

		codexTurnFailed(s, message)
	case "error":
		codexRetry(s, line.Message)
	default:
		s.unknown()
	}
}

// codexItemEvent reads one item event: a tool's start shows the tool, its end what it gave
// back, and the agent's words show once they are whole.
func codexItemEvent(s *stream, kind string, item *codexItem) {
	if item == nil {
		s.unknown()

		return
	}

	switch item.Type {
	case "agent_message":
		if kind == "item.completed" {
			codexMessage(s, item.Text)
		}
	case "reasoning":
		if kind == "item.completed" {
			s.emit(runevent.Event{Kind: runevent.KindThinking})
		}
	case "command_execution", "file_change", "mcp_tool_call", "web_search":
		codexTool(s, kind, item)
	case "todo_list":
		codexTodos(s, item)
	case "error":
		// A warning, which every fenced run starts with (the hook bypass); it ends nothing.
	default:
		s.unknown()
	}
}

// codexMessage reads what the agent said: its last message is the run's final one, which a
// read-only run's notes are and the strict schema's report is.
func codexMessage(s *stream, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}

	s.lastMessage, _ = cut(text, maxMessage)
	s.emit(runevent.Event{Kind: runevent.KindText, Text: text})
	s.step(firstLine(text))
}

// codexTool reads a tool's start and end: its event once, as it starts or else as it ends,
// and its result as it ends.
func codexTool(s *stream, kind string, item *codexItem) {
	started := s.items[item.ID]

	if kind == "item.updated" {
		return
	}

	if !started {
		s.items[item.ID] = true
		category, text, step := codexToolOf(s, item)
		s.emit(runevent.Event{Kind: runevent.KindTool, Tool: item.Type, Category: category, Text: text})
		s.step(step)
	}

	if kind == "item.completed" {
		delete(s.items, item.ID)
		codexToolEnd(s, item)
	}
}

// codexToolEnd reads a tool's end: what a command or an MCP tool gave back, and a tool that
// failed.
func codexToolEnd(s *stream, item *codexItem) {
	failed := item.Status == "failed" || item.Status == "declined" || item.ExitCode != nil && *item.ExitCode != 0

	switch {
	case item.Type == "command_execution":
		s.emit(runevent.Event{Kind: runevent.KindToolResult, Text: strings.TrimSpace(item.Output), Error: failed})
	case item.Type == "mcp_tool_call" && len(item.Result) > 0 && string(item.Result) != "null":
		s.emit(runevent.Event{Kind: runevent.KindToolResult, Text: strings.TrimSpace(string(item.Result)), Error: failed})
	case failed:
		s.emit(runevent.Event{Kind: runevent.KindToolResult, Text: item.Status, Error: true})
	}
}

// codexToolOf says what a tool item does: its category, its event's text, and the run's
// step.
func codexToolOf(s *stream, item *codexItem) (runevent.Category, string, string) {
	switch item.Type {
	case "command_execution":
		text := "$ " + codexCommand(item.Command)

		return runevent.CategoryCommand, text, firstLine(text)
	case "file_change":
		paths := make([]string, 0, len(item.Changes))
		for _, c := range item.Changes {
			paths = append(paths, s.rel(c.Path))
		}

		text := strings.Join(paths, ", ")

		return runevent.CategoryEdit, text, "Editing " + text
	case "web_search":
		return runevent.CategoryWeb, item.Query, "Searching for " + item.Query
	default:
		text := strings.Trim(item.Server+"."+item.Tool, ".")

		return runevent.CategoryOther, text, text
	}
}

// codexCommand is the command a command_execution ran, without the login shell Codex runs
// it in: "/bin/zsh -lc 'git add add.go'" is "git add add.go".
func codexCommand(command string) string {
	shell, inner, ok := strings.Cut(command, " -lc ")
	if !ok || strings.ContainsAny(shell, " \t") {
		return command
	}

	switch {
	case len(inner) >= 2 && inner[0] == '\'' && inner[len(inner)-1] == '\'':
		return strings.ReplaceAll(inner[1:len(inner)-1], `'\''`, `'`)
	case len(inner) >= 2 && inner[0] == '"' && inner[len(inner)-1] == '"':
		return strings.NewReplacer(`\"`, `"`, `\\`, `\`, `\$`, `$`, "\\`", "`").Replace(inner[1 : len(inner)-1])
	default:
		return inner
	}
}

// codexTodos keeps the todo list Codex wrote, stepping the run to the first not done.
func codexTodos(s *stream, item *codexItem) {
	todos := make([]runevent.Todo, 0, len(item.Items))
	done, next := 0, ""

	for _, t := range item.Items {
		status := "pending"
		if t.Completed {
			status, done = "completed", done+1
		} else if next == "" {
			status, next = "in_progress", t.Text
		}

		todos = append(todos, runevent.Todo{Text: t.Text, Status: status})
	}

	s.progress.Todos = todos
	s.step(next)

	if !s.items[item.ID] {
		s.items[item.ID] = true
		s.emit(runevent.Event{
			Kind: runevent.KindTool, Tool: "todo_list", Category: runevent.CategoryTodo,
			Text: strconv.Itoa(done) + " of " + strconv.Itoa(len(todos)) + " done",
		})
	}
}

// codexTurnCompleted reads the turn's end: the run is done, its report its last message.
func codexTurnCompleted(s *stream, u *codexUsage) {
	if u != nil {
		s.addUsage("turn", tokens{in: max(u.Input-u.CacheRead, 0), out: u.Output, cacheRead: u.CacheRead})
	}

	r := codexResult(s, "success", false)
	r.Report = codexReport(s.lastMessage)
	s.result = &r

	s.emit(runevent.Event{Kind: runevent.KindResult, Text: "Finished"})
	s.step("Finished")
}

// codexTurnFailed reads a failed turn: an error, or the profile's usage limit refusing it.
func codexTurnFailed(s *stream, message string) {
	r := codexResult(s, "turn_failed", true)
	if message = strings.TrimSpace(message); message != "" {
		r.Errors = []string{codexErrorText(message)}
	}

	if l := codexLimitOf(message); l != nil {
		s.limit, r.Subtype = l, "usage_limit"
	}

	s.result = &r

	text := "Ended with an error"
	if s.limit != nil && s.limit.Status == limitRejected {
		text = "Ended out of usage"
	}

	s.emit(runevent.Event{Kind: runevent.KindResult, Text: text, Error: true})
	s.step(text)
}

// codexResult is the run's end as Codex reports it: tokens, never a cost, and no models.
func codexResult(s *stream, subtype string, isError bool) Result {
	return Result{
		Subtype: subtype, IsError: isError, Message: s.lastMessage, Turns: 1, TokensIn: s.progress.TokensIn,
		TokensOut: s.progress.TokensOut, CacheRead: s.progress.CacheRead, Errors: []string{}, Models: []string{},
	}
}

// codexRetry reads a top-level error event: Codex retrying its provider, or refused for the
// profile's usage limit, which its turn then fails for.
func codexRetry(s *stream, message string) {
	text := codexErrorText(message)
	if l := codexLimitOf(message); l != nil {
		s.limit = l
		s.emit(runevent.Event{Kind: runevent.KindLimit, Text: text, Error: true})

		return
	}

	s.emit(runevent.Event{Kind: runevent.KindRetry, Text: text})
}

// codexReport is the structured report the last message holds, as the strict schema asks
// for it: a JSON object, or nil for a message that isn't one.
func codexReport(message string) json.RawMessage {
	body := bytes.TrimSpace([]byte(message))
	if len(body) == 0 || body[0] != '{' || !json.Valid(body) {
		return nil
	}

	return append(json.RawMessage{}, body...)
}

// codexErrorText is an error's words: a provider's error, which Codex passes on as its
// JSON, by its message.
func codexErrorText(message string) string {
	var wrapped struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if json.Unmarshal([]byte(message), &wrapped) == nil && wrapped.Error.Message != "" {
		return wrapped.Error.Message
	}

	return message
}

// limitRejected is a usage limit's status that refused the run.
const limitRejected = "rejected"

// codexLimitCodes are the codes Codex gives a turn its profile's usage refused (codex-cli
// 0.163.0's binary holds them): its plan's limit reached, its quota or credits used up.
func codexLimitCodes() []string {
	return []string{
		"usage_limit_reached", "quota_exceeded", "usage_not_included", "workspace_owner_credits_depleted",
		"workspace_member_credits_depleted", "workspace_owner_usage_limit_reached",
		"workspace_member_usage_limit_reached",
	}
}

// codexLimitOf returns the usage limit an error says refused the run, or nil: one of
// codexLimitCodes, or the words "usage limit" ("You've hit your usage limit."). Which limit,
// and when it resets, only the thread's rollout says (LimitReader).
func codexLimitOf(message string) *Limit {
	lower := strings.ToLower(message)

	for _, code := range codexLimitCodes() {
		if strings.Contains(lower, code) {
			return &Limit{Status: limitRejected}
		}
	}

	if strings.Contains(lower, "usage limit") {
		return &Limit{Status: limitRejected}
	}

	return nil
}
