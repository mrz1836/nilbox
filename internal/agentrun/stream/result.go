package stream

import (
	"encoding/json"
	"strings"
)

// Result is the agent's own end of a run, from its result line.
type Result struct {
	// Subtype is the agent's word for how it ended ("success", "error_max_budget_usd",
	// "error_during_execution"), and IsError whether it ended in error.
	Subtype string `json:"subtype"`
	IsError bool   `json:"isError"`

	// Message is its final message, at most 50 KB: a read-only run's notes.
	Message string `json:"message"`

	// Report is its structured output, as it gave it, or nil.
	Report json.RawMessage `json:"report"`

	// Cost is what it reports the run cost, in dollars, nil when it reports none: for a run
	// that resumed a conversation, Claude Code reports the whole conversation's cost so far
	// (total_cost_usd). Estimated says Cost and TurnCost are estimates of what the API would
	// have charged, as for a run signed in with the user's plan rather than an API key.
	Cost      *float64 `json:"cost"`
	Estimated bool     `json:"estimated"`

	// TurnCost is what this run alone cost, in dollars, nil when the agent reports none:
	// the sum of the costs in Claude Code's model_usage_since_last_result, which counts only
	// the run's own turn, where Cost counts every turn of a conversation it resumed. A run
	// that starts its conversation costs the same in both.
	TurnCost *float64 `json:"turnCost"`

	// TokensIn, TokensOut, and CacheRead are the tokens it used, Turns its turns, and
	// DurationMS how long it ran.
	TokensIn   int64 `json:"tokensIn"`
	TokensOut  int64 `json:"tokensOut"`
	CacheRead  int64 `json:"cacheRead"`
	Turns      int64 `json:"turns"`
	DurationMS int64 `json:"durationMs"`

	// Models are the models it actually used: plan mode runs on another model than asked.
	// For a run that resumed a conversation, they are every turn's.
	Models []string `json:"models"`

	// TerminalReason and Errors say why it ended, as the agent does, and Denials counts
	// the tool uses its permissions denied.
	TerminalReason string   `json:"terminalReason"`
	Errors         []string `json:"errors"`
	Denials        int      `json:"denials"`
}

// The statuses of a usage limit the agent reports: LimitRejected refused the run, and
// LimitWarning lets it go on near the limit.
const (
	LimitRejected = "rejected"
	LimitWarning  = "allowed_warning"
)

// LimitName names a usage limit as the agent reports its type: "five_hour" is the
// five-hour limit, "seven_day" the weekly one, and "seven_day_opus" the weekly Opus one.
func LimitName(kind string) string {
	switch {
	case kind == "":
		return ""
	case kind == "five_hour":
		return "five-hour"
	case strings.HasPrefix(kind, "seven_day"):
		model := strings.TrimPrefix(strings.TrimPrefix(kind, "seven_day"), "_")
		if model == "" {
			return "weekly"
		}

		return "weekly " + strings.ToUpper(model[:1]) + model[1:]
	default:
		return strings.ReplaceAll(kind, "_", " ")
	}
}

// limitWords says what a usage limit the agent reports means, as its run's line words it:
// near its plan's limit, out of usage for it, and which limit, never the agent's own codes
// ("Near its plan's weekly limit").
func limitWords(status, kind string) string {
	limit := "its plan's usage limit"
	if name := LimitName(kind); name != "" {
		limit = "its plan's " + name + " limit"
	}

	switch status {
	case LimitWarning:
		return "Near " + limit
	case LimitRejected:
		return "Out of usage: " + limit + " is reached"
	default:
		return "Usage limit, " + strings.ReplaceAll(status, "_", " ") + ": " + limit
	}
}

// Limit is the provider's usage limit, as the agent last reported it.
type Limit struct {
	// Status is allowed, allowed_warning, or rejected (LimitRejected); Type is which window
	// ("five_hour", "seven_day", ...); ResetsAt is when it resets, in Unix seconds.
	Status   string `json:"status"`
	Type     string `json:"type"`
	ResetsAt int64  `json:"resetsAt"`
}

// Init is what an agent's init line says of its own setup as its run starts, Claude Code's
// system/init: a turn that runs only the app's own tools checks them here before it goes
// on.
type Init struct {
	// Model is the model it runs, Version its own version, and APIKeySource where its API
	// key came from ("none" for a sign-in with the user's plan).
	Model        string `json:"model"`
	Version      string `json:"version"`
	APIKeySource string `json:"apiKeySource"`

	// SessionID is its ID of the conversation.
	SessionID string `json:"sessionId"`

	// Tools are every tool it may use, and MCPServers the MCP servers it started, each as
	// it came up; neither is ever nil.
	Tools      []string    `json:"tools"`
	MCPServers []MCPServer `json:"mcpServers"`
}

// MCPServer is an MCP server as an agent's init line names it: its name, and its status
// ("connected", "failed", ...).
type MCPServer struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}
