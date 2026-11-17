// Package runevent is the event model of an agent's background run: what the run view, the
// Activity's Working list, and the tray show of a run, whichever agent runs it. Each
// agent's stream parser (internal/agentrun) turns its own messages into these events, so
// nothing above it ever branches on the agent. It is a leaf: it imports nothing of the
// app's, so Autopilot reads it without reaching the runner.
package runevent

// Kind is what an event is.
type Kind string

// The kinds of event.
const (
	// KindStarted is the agent starting: its model and version.
	KindStarted Kind = "started"

	// KindText is something the agent said.
	KindText Kind = "text"

	// KindThinking is the agent thinking, its text left out.
	KindThinking Kind = "thinking"

	// KindTool is a tool the agent used: a command, an edit, a read, a search, a subagent,
	// or its todo list.
	KindTool Kind = "tool"

	// KindToolResult is what a tool gave back, cut short, and whether it failed.
	KindToolResult Kind = "toolResult"

	// KindDenied is a tool use the fence denied, and why.
	KindDenied Kind = "denied"

	// KindRetry is the agent retrying its provider after an error.
	KindRetry Kind = "retry"

	// KindTask is a task the agent started in the background, or its end.
	KindTask Kind = "task"

	// KindLimit is the provider's usage limit, as the agent reports it: a warning, or the
	// run refused for the limit.
	KindLimit Kind = "limit"

	// KindResult is the run's end, as the agent reports it.
	KindResult Kind = "result"
)

// Category is what a tool does, as the run view groups tools.
type Category string

// The tools' categories.
const (
	CategoryCommand  Category = "command"
	CategoryEdit     Category = "edit"
	CategoryRead     Category = "read"
	CategorySearch   Category = "search"
	CategorySubagent Category = "subagent"
	CategoryTodo     Category = "todo"
	CategoryWeb      Category = "web"
	CategoryReport   Category = "report"
	CategoryOther    Category = "other"
)

// MaxText is the most of an event's text an event keeps, in bytes: a tool's result that
// holds a whole log keeps its start.
const MaxText = 8 << 10

// Event is one event of a run.
type Event struct {
	// Seq numbers the run's events from 1, in order; the window asks for those after the
	// last it has.
	Seq int64 `json:"seq"`

	Kind Kind `json:"kind"`

	// Tool is the tool's name, and Category what it does, for a tool, its result, or a
	// denial.
	Tool     string   `json:"tool"`
	Category Category `json:"category"`

	// Text is the event's words, at most MaxText bytes: what the agent said, a command, a
	// file, a search, a todo, a result's start, a denial's reason. Cut reports text cut
	// short.
	Text string `json:"text"`
	Cut  bool   `json:"cut"`

	// Error reports a tool result that failed, a refused limit, or a result in error.
	Error bool `json:"error"`
}

// Todo is one item of the agent's todo list, as the run's steps.
type Todo struct {
	Text   string `json:"text"`
	Status string `json:"status"`
}

// Progress is how far a run has got, as the window's Working list shows it.
type Progress struct {
	// Step is its latest step, in the agent's words or the tool it uses: "Reading the
	// failed run's log", "$ pnpm test".
	Step string `json:"step"`

	// Todos are its todo list, as it last wrote it.
	Todos []Todo `json:"todos"`

	// TokensIn and TokensOut are the tokens it used so far, and CacheRead those read from
	// its provider's cache.
	TokensIn  int64 `json:"tokensIn"`
	TokensOut int64 `json:"tokensOut"`
	CacheRead int64 `json:"cacheRead"`

	// Events counts its events so far, and Unknown the messages the parser didn't know,
	// which the run view says weren't shown.
	Events  int64 `json:"events"`
	Unknown int64 `json:"unknown"`
}

// Page is a run's events after a sequence number.
type Page struct {
	// Events are the events after the one asked for, oldest first.
	Events []Event `json:"events"`

	// First is the oldest event the run still keeps in memory: a run keeps its last
	// events, and one asked for earlier ones gets these.
	First int64 `json:"first"`

	// Done reports a run that has ended: no more will come.
	Done bool `json:"done"`

	// Progress is how far it has got.
	Progress Progress `json:"progress"`
}
