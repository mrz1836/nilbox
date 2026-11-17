package agentrun

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/fence"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// ReportSchema is the JSON schema of the report every background run returns, on one line,
// as the agent's structured output: how it came out, a summary, whether it pushed and
// which commit, and whether a person is needed. The agent's report is shown as the agent's;
// the app's own verdict comes only from GitHub.
const ReportSchema = `{"type":"object","required":["outcome","summary","pushed","needsPerson"],` +
	`"properties":{"outcome":{"enum":["fixed","nothing_to_do","partial","blocked","failed"]},` +
	`"summary":{"type":"string","maxLength":2000},"pushed":{"type":"boolean"},` +
	`"pushedCommit":{"type":"string","pattern":"^[0-9a-f]{40}$"},"needsPerson":{"type":"boolean"},` +
	`"reason":{"type":"string","maxLength":500}}}`

// UnattendedNote is the system note every background run gets beside its prompt: nobody
// watches it, so it asks no one, works in its worktree alone, and leaves every decision
// that changes what others see to a person.
const UnattendedNote = "You are running unattended, in the background: nobody can answer a question or approve " +
	"anything, so don't ask; work it out or report that a person is needed. Work only in this folder. Never " +
	"merge, approve, or close anything, and never force-push. When you are done, give your report in the " +
	"structured output asked for."

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

	// Cost is what it reports the run cost, in dollars, nil when it reports none, and
	// Estimated says it is an estimate of what the API would have charged, as for a run
	// signed in with the user's plan rather than an API key.
	Cost      *float64 `json:"cost"`
	Estimated bool     `json:"estimated"`

	// TokensIn, TokensOut, and CacheRead are the tokens it used, Turns its turns, and
	// DurationMS how long it ran.
	TokensIn   int64 `json:"tokensIn"`
	TokensOut  int64 `json:"tokensOut"`
	CacheRead  int64 `json:"cacheRead"`
	Turns      int64 `json:"turns"`
	DurationMS int64 `json:"durationMs"`

	// Models are the models it actually used: plan mode runs on another model than asked.
	Models []string `json:"models"`

	// TerminalReason and Errors say why it ended, as the agent does, and Denials counts
	// the tool uses its permissions denied.
	TerminalReason string   `json:"terminalReason"`
	Errors         []string `json:"errors"`
	Denials        int      `json:"denials"`
}

// Limit is the provider's usage limit, as the agent last reported it.
type Limit struct {
	// Status is allowed, allowed_warning, or rejected; Type is which window
	// ("five_hour", "seven_day", ...); ResetsAt is when it resets, in Unix seconds.
	Status   string `json:"status"`
	Type     string `json:"type"`
	ResetsAt int64  `json:"resetsAt"`
}

// Report is the agent's structured report, as ReportSchema asks for it.
type Report struct {
	Outcome      string `json:"outcome"`
	Summary      string `json:"summary"`
	Pushed       bool   `json:"pushed"`
	PushedCommit string `json:"pushedCommit"`
	NeedsPerson  bool   `json:"needsPerson"`
	Reason       string `json:"reason"`
}

// Status is how a run came out, as the app judges it from the agent's result line and
// how its program ended, never its exit code alone.
type Status string

// The ways a run comes out.
const (
	// StatusDone is a run the agent finished.
	StatusDone Status = "done"

	// StatusFailed is one that ended in error, or crashed without a word.
	StatusFailed Status = "failed"

	// StatusOutOfBudget is one that reached its budget, in dollars or tokens.
	StatusOutOfBudget Status = "outOfBudget"

	// StatusOutOfUsage is one the provider refused for the profile's usage limit.
	StatusOutOfUsage Status = "outOfUsage"

	// StatusTimedOut is one stopped at its time limit.
	StatusTimedOut Status = "timedOut"

	// StatusStopped is one the user stopped, or that was stopped as its item closed or the
	// app quit.
	StatusStopped Status = "stopped"

	// StatusInterrupted is one whose end the app couldn't see, as when the Mac shut down:
	// it keeps what its log shows, and counts as no try.
	StatusInterrupted Status = "interrupted"
)

// Outcome is how a run came out, as its record keeps it (the run's result's Outcome).
type Outcome struct {
	Status Status `json:"status"`

	// Summary says it in a sentence: "Finished: nothing to do.", "Out of budget ($0.50).",
	// "Stopped after 30 minutes.".
	Summary string `json:"summary"`

	// Result is the agent's own end, nil when it never reported one.
	Result *Result `json:"result"`

	// Report is its structured report, nil when it gave none, and NoReport says so in the
	// window's words with the last of its narration.
	Report   *Report `json:"report"`
	NoReport string  `json:"noReport"`

	// Limit is the usage limit that refused it, for a run out of usage.
	Limit *Limit `json:"limit"`

	// TokensIn and TokensOut are what it used, its result's or, with none, what its
	// stream counted.
	TokensIn  int64 `json:"tokensIn"`
	TokensOut int64 `json:"tokensOut"`
	CacheRead int64 `json:"cacheRead"`

	// Commit is the worktree's commit as the run ended, and Changed the files its commits
	// changed since the head before it.
	Commit  string   `json:"commit"`
	Changed []string `json:"changed"`

	// Unknown counts the messages the parser didn't know.
	Unknown int64 `json:"unknown"`

	// Sandbox is what a sandboxed run's sandbox check left in its folder (fence.Sandboxed):
	// held, leaked, or unchecked, none for a run with no sandbox, and "" for one whose
	// policy couldn't be read.
	Sandbox fence.SandboxState `json:"sandbox,omitempty"`
}

// leakedWords says a run's sandbox didn't hold.
const leakedWords = "Its sandbox didn't hold: its commands could write outside its worktree, so it is failed. " +
	"Check what it changed before you use it."

// sandboxOf reads what a run's sandbox check left in its folder, "" when its policy can't
// be read.
func sandboxOf(folder string) fence.SandboxState {
	if folder == "" {
		return ""
	}

	p, err := fence.ReadPolicy(folder)
	if err != nil {
		return ""
	}

	return fence.Sandboxed(p, nil)
}

// sandboxed judges a run's outcome by its sandbox check too: a sandbox that leaked fails a
// run that ended on its own, whatever its result says, since its commands weren't held to
// the fence. A run stopped, interrupted, or past its time limit says so already.
func sandboxed(out Outcome, state fence.SandboxState) Outcome {
	out.Sandbox = state

	if state != fence.SandboxLeaked {
		return out
	}

	switch out.Status {
	case StatusStopped, StatusInterrupted, StatusTimedOut:
	case StatusDone, StatusFailed, StatusOutOfBudget, StatusOutOfUsage:
		out.Status, out.Summary = StatusFailed, leakedWords
	}

	return out
}

// judgment is what judge needs of a run.
type judgment struct {
	end     scriptrun.Ending
	fence   Fence
	result  *Result
	limit   *Limit
	words   string
	tokens  int64
	unknown int64
}

// judge works out how a run came out: by the agent's result line above all, then how its
// program ended. A stop the user or the app asked for, and the time limit, say so whatever
// the result says, since a stopped agent ends its turn in error.
func judge(j judgment) Outcome {
	out := Outcome{Result: j.result, Limit: nil, Unknown: j.unknown, Changed: []string{}}

	if j.result != nil {
		out.Report = parseReport(j.result.Report)
		out.TokensIn, out.TokensOut, out.CacheRead = j.result.TokensIn, j.result.TokensOut, j.result.CacheRead
	}

	if out.Report == nil {
		out.NoReport = noReport(j.words)
	}

	out.Status, out.Summary = verdict(j, out)

	if out.Status == StatusOutOfUsage {
		out.Limit = j.limit
	}

	return out
}

// verdict returns how a run came out, and its sentence: what stopped it first, then what
// its result says.
func verdict(j judgment, out Outcome) (Status, string) {
	if status, words, ok := stopVerdict(j); ok {
		return status, words
	}

	return resultVerdict(j, out)
}

// stopVerdict returns how a run the app stopped came out: at its time limit, as the app
// quit, by the user, or at its budget in tokens.
func stopVerdict(j judgment) (Status, string, bool) {
	overTokens := j.fence.BudgetTokens > 0 && j.tokens >= j.fence.BudgetTokens

	switch {
	case j.end.TimedOut:
		return StatusTimedOut, "Stopped after " + minutesText(j.fence.Timeout) + ".", true
	case j.end.Quit:
		// No one chose to stop it: its match comes back under Needs you, as after a crash.
		return StatusInterrupted, "Interrupted as the app quit.", true
	case j.end.Stopped:
		return StatusStopped, "Stopped.", true
	case overTokens && (j.result == nil || j.result.IsError):
		return StatusOutOfBudget, "Out of budget (" + textutil.Counted(int(j.fence.BudgetTokens), "token", "tokens") + ").", true
	default:
		return "", "", false
	}
}

// resultVerdict returns how a run came out by its result line, or the lack of one.
func resultVerdict(j judgment, out Outcome) (Status, string) {
	switch {
	case j.result == nil && j.end.Interrupted != "":
		return StatusInterrupted, j.end.Interrupted
	case j.result == nil:
		return StatusFailed, "It ended without a result. " + out.NoReport
	case j.result.Subtype == "error_max_budget_usd":
		return StatusOutOfBudget, "Out of budget (" + dollars(j.fence.BudgetUSD) + ")."
	case j.result.IsError && j.limit != nil && j.limit.Status == "rejected":
		return StatusOutOfUsage, outOfUsageWords(*j.limit)
	case j.result.IsError:
		return StatusFailed, "It failed: " + errorWords(*j.result)
	default:
		return StatusDone, doneWords(out)
	}
}

// parseReport reads a structured report, nil when there is none or it isn't one.
func parseReport(raw json.RawMessage) *Report {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}

	var r Report
	if err := json.Unmarshal(raw, &r); err != nil || r.Outcome == "" {
		return nil
	}

	r.Summary, _ = cut(r.Summary, 2000)
	r.Reason, _ = cut(r.Reason, 500)

	return &r
}

// noReport says a run gave no report, with the last of its narration.
func noReport(words string) string {
	if words = strings.TrimSpace(words); words == "" {
		return "The agent didn't report."
	}

	words, _ = cut(firstLine(words), 300)

	return "The agent didn't report. It last said: " + words
}

// doneWords says how a finished run came out: its report's summary, or its final
// message's first line.
func doneWords(out Outcome) string {
	switch {
	case out.Report != nil && out.Report.Summary != "":
		return "Finished: " + firstLine(out.Report.Summary)
	case out.Result != nil && out.Result.Message != "":
		return "Finished: " + firstLine(out.Result.Message)
	default:
		return "Finished."
	}
}

// errorWords says why the agent ended in error.
func errorWords(r Result) string {
	switch {
	case len(r.Errors) > 0 && !strings.HasPrefix(r.Errors[0], "["):
		return textutil.Sentence(r.Errors[0])
	case r.TerminalReason != "":
		return textutil.Sentence(strings.ReplaceAll(r.TerminalReason, "_", " "))
	default:
		return textutil.Sentence(strings.ReplaceAll(r.Subtype, "_", " "))
	}
}

// outOfUsageWords says a run was refused for its profile's usage limit, which limit, and
// when it resets: "Out of usage: its plan's five-hour limit is reached. It resets Mon 15:04."
func outOfUsageWords(l Limit) string {
	words := "Out of usage"
	if name := limitName(l.Type); name != "" {
		words += ": its plan's " + name + " limit is reached"
	}

	words += "."

	if l.ResetsAt > 0 {
		words += " It resets " + time.Unix(l.ResetsAt, 0).Format("Mon 15:04") + "."
	}

	return words
}

// limitName names a usage limit as the agent reports its type: "five_hour" is the
// five-hour limit, "seven_day" the weekly one, and "seven_day_opus" the weekly Opus one.
func limitName(kind string) string {
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

// dollars writes an amount in dollars: "$0.50".
func dollars(amount float64) string {
	return "$" + strconv.FormatFloat(amount, 'f', 2, 64)
}

// minutesText writes a time limit: "30 minutes", "1 minute".
func minutesText(d time.Duration) string {
	m := int(d / time.Minute)
	if m <= 0 {
		return d.String()
	}

	return textutil.Counted(m, "minute", "minutes")
}

// scriptStatus is the run's record's status for an outcome.
func scriptStatus(s Status) scriptrun.Status {
	switch s {
	case StatusDone:
		return scriptrun.StatusSucceeded
	case StatusStopped:
		return scriptrun.StatusStopped
	case StatusInterrupted:
		return scriptrun.StatusInterrupted
	case StatusFailed, StatusOutOfBudget, StatusOutOfUsage, StatusTimedOut:
		return scriptrun.StatusFailed
	default:
		return scriptrun.StatusFailed
	}
}

// sortStrings sorts s in place.
func sortStrings(s []string) {
	slices.Sort(s)
}

// String names an outcome for the logs.
func (o Outcome) String() string {
	return fmt.Sprintf("%s: %s", o.Status, o.Summary)
}
