package agentrun

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/agentrun/runevent"
)

// Claude Code's stream, `claude -p --output-format stream-json --verbose`, as S1 recorded
// it from version 2.1.325: one JSON message a line, of the types system (init, api_retry,
// task_started, task_notification, permission_denied, and others it only notes),
// assistant, user, rate_limit_event, and result, the run's last word. A run is judged by
// its result line, never its exit code: a SIGINT ends with a result in error and exit 0.

// claudeLine is what the parser reads of one line; every other field is skipped.
type claudeLine struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`

	// Timestamp is when an assistant's or a user's message was written.
	Timestamp string `json:"timestamp"`

	// system/init
	Model        string `json:"model"`
	Version      string `json:"claude_code_version"`
	APIKeySource string `json:"apiKeySource"`

	// system/api_retry
	Attempt    int    `json:"attempt"`
	MaxRetries int    `json:"max_retries"`
	ErrorText  string `json:"error"`

	// system/task_started, system/task_notification
	Description string `json:"description"`
	Summary     string `json:"summary"`
	Status      string `json:"status"`

	// system/permission_denied
	ToolName       string `json:"tool_name"`
	DecisionReason string `json:"decision_reason"`

	// assistant and user, an object; a system message's, a string
	Message json.RawMessage `json:"message"`

	// rate_limit_event
	RateLimit *claudeRateLimit `json:"rate_limit_info"`

	// result
	IsError          bool                     `json:"is_error"`
	ResultText       string                   `json:"result"`
	StructuredOutput json.RawMessage          `json:"structured_output"`
	TotalCost        *float64                 `json:"total_cost_usd"`
	Usage            *claudeUsage             `json:"usage"`
	ModelUsage       map[string]claudeModelUs `json:"modelUsage"`
	NumTurns         int64                    `json:"num_turns"`
	DurationMS       int64                    `json:"duration_ms"`
	TerminalReason   string                   `json:"terminal_reason"`
	Errors           []string                 `json:"errors"`
	Denials          []json.RawMessage        `json:"permission_denials"`
}

// claudeMessage is an assistant's or a user's message.
type claudeMessage struct {
	ID      string          `json:"id"`
	Content json.RawMessage `json:"content"`
	Usage   *claudeUsage    `json:"usage"`
}

// claudeBlock is one block of a message's content.
type claudeBlock struct {
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	Name    string          `json:"name"`
	Input   json.RawMessage `json:"input"`
	Content json.RawMessage `json:"content"`
	IsError bool            `json:"is_error"`
}

// claudeUsage is a message's or a run's tokens.
type claudeUsage struct {
	Input         int64 `json:"input_tokens"`
	CacheCreation int64 `json:"cache_creation_input_tokens"`
	CacheRead     int64 `json:"cache_read_input_tokens"`
	Output        int64 `json:"output_tokens"`
}

// claudeModelUs is one model's share of a run.
type claudeModelUs struct {
	CostUSD float64 `json:"costUSD"`
}

// claudeRateLimit is the provider's usage limit.
type claudeRateLimit struct {
	Status   string `json:"status"`
	Type     string `json:"rateLimitType"`
	ResetsAt int64  `json:"resetsAt"`
}

// claudeSystemNotes are the system messages the parser knows and shows nothing of.
func claudeSystemNotes() []string {
	return []string{
		"thinking_tokens", "background_tasks_changed", "task_updated", "hook_started", "hook_response", "status",
		"compact_boundary", "files_persisted",
	}
}

// newClaudeParser returns a parser of Claude Code's stream, for a run in dir.
func newClaudeParser(dir string) *stream {
	return newStream(readClaudeLine, dir)
}

// readClaudeLine reads one line of Claude Code's stream.
func readClaudeLine(s *stream, raw []byte) {
	var line claudeLine
	if err := json.Unmarshal(raw, &line); err != nil {
		s.unknown()

		return
	}

	s.stamp(line.Timestamp)

	switch line.Type {
	case "system":
		claudeSystem(s, line)
	case "assistant":
		claudeAssistant(s, messageOf(line.Message))
	case "user":
		claudeUser(s, messageOf(line.Message))
	case "rate_limit_event":
		claudeLimit(s, line.RateLimit)
	case "result":
		claudeResult(s, line)
	default:
		s.unknown()
	}
}

// messageOf reads an assistant's or a user's message, nil when it isn't one.
func messageOf(raw json.RawMessage) *claudeMessage {
	var m claudeMessage
	if len(raw) == 0 || json.Unmarshal(raw, &m) != nil {
		return nil
	}

	return &m
}

// claudeSystem reads a system message.
func claudeSystem(s *stream, line claudeLine) {
	switch line.Subtype {
	case "init":
		s.keySource = line.APIKeySource
		text := strings.TrimSpace(line.Model + " · Claude Code " + line.Version)
		s.emit(runevent.Event{Kind: runevent.KindStarted, Text: text})
		s.step("Starting")
	case "api_retry":
		text := "Retrying (" + strconv.Itoa(line.Attempt) + " of " + strconv.Itoa(line.MaxRetries) + ")"
		if line.ErrorText != "" {
			text += ": " + line.ErrorText
		}

		s.emit(runevent.Event{Kind: runevent.KindRetry, Text: text})
	case "task_started":
		s.emit(runevent.Event{Kind: runevent.KindTask, Text: line.Description})
	case "task_notification":
		s.emit(runevent.Event{Kind: runevent.KindTask, Text: strings.TrimSpace(line.Status + ": " + line.Summary)})
	case "permission_denied":
		s.emit(runevent.Event{
			Kind: runevent.KindDenied, Tool: line.ToolName, Category: claudeCategory(line.ToolName),
			Text: line.DecisionReason, Error: true,
		})
	default:
		for _, note := range claudeSystemNotes() {
			if line.Subtype == note {
				return
			}
		}

		s.unknown()
	}
}

// claudeAssistant reads an assistant's message: what it said, its thinking, and the tools
// it used, and its usage.
func claudeAssistant(s *stream, m *claudeMessage) {
	if m == nil {
		s.unknown()

		return
	}

	if m.Usage != nil {
		s.addUsage(m.ID, tokens{in: m.Usage.Input + m.Usage.CacheCreation, out: m.Usage.Output, cacheRead: m.Usage.CacheRead})
	}

	var blocks []claudeBlock
	if err := json.Unmarshal(m.Content, &blocks); err != nil {
		s.unknown()

		return
	}

	for _, b := range blocks {
		switch b.Type {
		case "text":
			if text := strings.TrimSpace(b.Text); text != "" {
				s.emit(runevent.Event{Kind: runevent.KindText, Text: text})
				s.step(firstLine(text))
			}
		case "thinking", "redacted_thinking":
			s.emit(runevent.Event{Kind: runevent.KindThinking})
		case "tool_use":
			claudeTool(s, b)
		default:
			s.unknown()
		}
	}
}

// claudeToolInput is what the parser reads of a tool's input.
type claudeToolInput struct {
	Command     string `json:"command"`
	FilePath    string `json:"file_path"`
	Path        string `json:"path"`
	Pattern     string `json:"pattern"`
	Description string `json:"description"`
	URL         string `json:"url"`
	Query       string `json:"query"`
	Todos       []struct {
		Content    string `json:"content"`
		Status     string `json:"status"`
		ActiveForm string `json:"activeForm"`
	} `json:"todos"`
}

// claudeTool reads one tool use: its event, the run's step, and, for TodoWrite, its todo
// list.
func claudeTool(s *stream, b claudeBlock) {
	var in claudeToolInput

	_ = json.Unmarshal(b.Input, &in) // a tool with an input the parser doesn't know shows its name

	category := claudeCategory(b.Name)
	text, step := b.Name, b.Name

	switch category {
	case runevent.CategoryCommand:
		text = "$ " + in.Command
		step = firstLine(text)
	case runevent.CategoryRead:
		text = s.rel(in.FilePath)
		step = "Reading " + text
	case runevent.CategoryEdit:
		text = s.rel(in.FilePath)
		step = "Editing " + text
	case runevent.CategorySearch:
		text = strings.TrimSpace(in.Pattern + " " + s.rel(in.Path))
		step = "Searching for " + in.Pattern
	case runevent.CategorySubagent:
		text, step = in.Description, in.Description
	case runevent.CategoryWeb:
		text = strings.TrimSpace(in.URL + in.Query)
		step = "Reading " + text
	case runevent.CategoryTodo:
		text, step = claudeTodos(s, in), ""
	case runevent.CategoryReport:
		text, step = "", "Writing its report"
	case runevent.CategoryOther:
	}

	s.emit(runevent.Event{Kind: runevent.KindTool, Tool: b.Name, Category: category, Text: text})
	s.step(step)
}

// claudeTodos keeps the todo list a TodoWrite wrote, steps the run to the todo in
// progress, and returns what the event says.
func claudeTodos(s *stream, in claudeToolInput) string {
	todos := make([]runevent.Todo, 0, len(in.Todos))
	done := 0

	for _, t := range in.Todos {
		todos = append(todos, runevent.Todo{Text: t.Content, Status: t.Status})

		switch t.Status {
		case "completed":
			done++
		case "in_progress":
			s.step(cmpOr(t.ActiveForm, t.Content))
		}
	}

	s.progress.Todos = todos

	return strconv.Itoa(done) + " of " + strconv.Itoa(len(todos)) + " done"
}

// claudeUser reads a user's message: the tools' results.
func claudeUser(s *stream, m *claudeMessage) {
	if m == nil {
		s.unknown()

		return
	}

	var blocks []claudeBlock
	if err := json.Unmarshal(m.Content, &blocks); err != nil {
		var text string
		if json.Unmarshal(m.Content, &text) != nil {
			s.unknown()
		}

		return
	}

	for _, b := range blocks {
		switch b.Type {
		case "tool_result":
			s.emit(runevent.Event{Kind: runevent.KindToolResult, Text: resultText(b.Content), Error: b.IsError})
		case "text":
		default:
			s.unknown()
		}
	}
}

// resultText reads a tool's result: a string, or text blocks.
func resultText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.TrimSpace(text)
	}

	var blocks []claudeBlock
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}

	parts := make([]string, 0, len(blocks))

	for _, b := range blocks {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
	}

	return strings.TrimSpace(strings.Join(parts, "\n"))
}

// claudeLimit reads the provider's usage limit: a warning or a refusal shows.
func claudeLimit(s *stream, rl *claudeRateLimit) {
	if rl == nil {
		s.unknown()

		return
	}

	s.limit = &Limit{Status: rl.Status, Type: rl.Type, ResetsAt: rl.ResetsAt}

	if rl.Status != "allowed" {
		s.emit(runevent.Event{Kind: runevent.KindLimit, Text: rl.Status + " · " + rl.Type, Error: rl.Status == "rejected"})
	}
}

// onPlan reports an agent signed in with the user's plan rather than an API key, by the
// key source its init line names ("none"), or none named: its cost is then an estimate of
// what the API would have charged.
func onPlan(source string) bool {
	return source == "" || source == "none"
}

// claudeResult reads the run's result: its end, what it cost, and its report.
func claudeResult(s *stream, line claudeLine) {
	r := Result{
		Subtype: line.Subtype, IsError: line.IsError, Cost: line.TotalCost, Turns: line.NumTurns,
		Estimated:  line.TotalCost != nil && onPlan(s.keySource),
		DurationMS: line.DurationMS, TerminalReason: line.TerminalReason, Errors: append([]string{}, line.Errors...),
		Denials: len(line.Denials), Models: []string{},
	}

	r.Message, _ = cut(strings.TrimSpace(line.ResultText), maxMessage)

	if len(line.StructuredOutput) > 0 && string(line.StructuredOutput) != "null" {
		r.Report = append(json.RawMessage{}, line.StructuredOutput...)
	}

	if u := line.Usage; u != nil {
		r.TokensIn, r.TokensOut, r.CacheRead = u.Input+u.CacheCreation, u.Output, u.CacheRead
		s.progress.TokensIn, s.progress.TokensOut, s.progress.CacheRead = r.TokensIn, r.TokensOut, r.CacheRead
	} else {
		r.TokensIn, r.TokensOut, r.CacheRead = s.progress.TokensIn, s.progress.TokensOut, s.progress.CacheRead
	}

	for model := range line.ModelUsage {
		r.Models = append(r.Models, model)
	}

	sortStrings(r.Models)

	s.result = &r

	text := "Finished"
	if r.IsError {
		text = "Ended with " + strings.ReplaceAll(r.Subtype, "_", " ")
	}

	s.emit(runevent.Event{Kind: runevent.KindResult, Text: text, Error: r.IsError})
	s.step(text)
}

// claudeCategory says what one of Claude Code's tools does.
func claudeCategory(tool string) runevent.Category {
	switch tool {
	case "Bash", "BashOutput", "KillShell", "KillBash":
		return runevent.CategoryCommand
	case "Read", "NotebookRead", "LS":
		return runevent.CategoryRead
	case "Edit", "MultiEdit", "Write", "NotebookEdit":
		return runevent.CategoryEdit
	case "Grep", "Glob":
		return runevent.CategorySearch
	case "Task", "Agent":
		return runevent.CategorySubagent
	case "TodoWrite":
		return runevent.CategoryTodo
	case "WebFetch", "WebSearch":
		return runevent.CategoryWeb
	case "StructuredOutput":
		return runevent.CategoryReport
	default:
		return runevent.CategoryOther
	}
}

// rel writes path from the run's folder, when it is inside it.
func (s *stream) rel(path string) string {
	if s.dir == "" || path == "" || !filepath.IsAbs(path) {
		return path
	}

	if rel, err := filepath.Rel(s.dir, path); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}

	return path
}

// firstLine returns text's first line.
func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")

	return line
}

// cmpOr returns a, or b when a is "".
func cmpOr(a, b string) string {
	if a != "" {
		return a
	}

	return b
}
