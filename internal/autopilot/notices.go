package autopilot

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/eval"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// Autopilot tells the user what its routines do through notices: a routine's Notify me
// and Tell me, which the notices dispatcher runs as local steps (NewNotices), and its
// problems, each told once as it starts (NewProblemNotices). Both hand a Notice to a
// NoticeSink, which core adapts over the notifications, so the engine never reaches them.
// A notice of a burst (more than one a cycle, or a catch-up) is gathered, so more than
// three at once become one banner that sums them up.

// NoticeKind is a kind of Autopilot notice; core maps each to its kind of notification,
// whose banners the user turns on or off in Settings → Notifications.
type NoticeKind string

// The kinds of notice.
const (
	// NoticeAlert is a routine's Notify me, or its Tell me: headed by the routine's name.
	NoticeAlert NoticeKind = "alert"

	// NoticeNeedsYou is a match waiting for the user's decision.
	NoticeNeedsYou NoticeKind = "needsYou"

	// NoticeFinished is an agent run in the background that ended.
	NoticeFinished NoticeKind = "finished"

	// NoticeChecks is the verdict on the checks after a routine's fix.
	NoticeChecks NoticeKind = "checks"

	// NoticeProblem is something wrong with a routine, about no item.
	NoticeProblem NoticeKind = "problem"
)

// NoticeKinds returns every kind of notice.
func NoticeKinds() []NoticeKind {
	return []NoticeKind{NoticeAlert, NoticeNeedsYou, NoticeFinished, NoticeChecks, NoticeProblem}
}

// Notice is one notice, as the notifications record it.
type Notice struct {
	Kind NoticeKind

	// RoutineID and Routine are the routine, whose name heads an alert.
	RoutineID int64
	Routine   string

	// MatchID and RunID are the match and the run it is about, which the Autopilot tab
	// opens at, 0 for none; CreatedAt is when the most specific of them was created (the
	// run, else the match, else the routine), which tells it from a later one given its ID.
	MatchID   int64
	RunID     int64
	CreatedAt time.Time

	// Item is the item it is about, nil for none, as a problem is about no item.
	Item *ItemRef

	// Title is its line (the item's title, or the routine's name for a problem), and Body
	// what follows it.
	Title string
	Body  string

	// Banner is the routine's own choice of banner, over Settings → Notifications': true
	// shows one, false keeps it to the history, and nil follows the settings.
	Banner *bool

	// Burst reports a notice that comes with others at once, which is gathered.
	Burst bool

	// Key tells it from every other notice, so it is told once however often it is handed
	// over: a step's is its match, when the match was made, the step, and the try, and a
	// problem's its routine, its kind, and when it began. A step run again after a quit, or
	// a problem told again after a crash, hands over the same key, which the sink refuses
	// as told.
	Key string
}

// NoticeSink tells the user of a notice: core records it in the notifications, and shows
// its banner. An error says it wasn't told; a notice whose key was told already is told
// again by no one, and is no error.
type NoticeSink interface {
	Notice(ctx context.Context, n Notice) error
}

// errNoSink is a notices dispatcher with no sink to tell.
var errNoSink = errors.New("no one to tell")

// maxReasonRunes bounds the reason a notice gives, which the history shows on one line.
const maxReasonRunes = 280

// notices is the notices dispatcher (catalog.DispatchNotices): it runs Notify me and Tell
// me, telling sink.
type notices struct {
	sink NoticeSink
}

// NewNotices returns the notices dispatcher, which runs a routine's Notify me and Tell me
// steps by telling sink, as a local step, at once.
func NewNotices(sink NoticeSink) Dispatcher {
	return notices{sink: sink}
}

// Dispatch tells the user of job's notice: a step that fails to tell fails, so the match
// says so.
func (n notices) Dispatch(ctx context.Context, job Job) (Result, error) {
	if n.sink == nil {
		return Result{}, errNoSink
	}

	if job.Digest != nil {
		if err := n.sink.Notice(ctx, digestNotice(job)); err != nil {
			return Result{}, err
		}

		return Result{Summary: digestSummary(*job.Digest)}, nil
	}

	notice := alertOf(job)

	if err := n.sink.Notice(ctx, notice); err != nil {
		return Result{}, err
	}

	return Result{Summary: "Told you."}, nil
}

// alertOf is the alert job's step tells: Notify me says why the routine matched, with the
// user's own line; Tell me says the routine reached it, and how the steps before it went.
func alertOf(job Job) Notice {
	item := job.Item

	out := Notice{
		Kind: NoticeAlert, RoutineID: job.RoutineID, Routine: job.RoutineName, MatchID: job.MatchID,
		CreatedAt: job.MatchCreated, Item: &item, Title: job.Item.Title, Burst: job.Burst, Key: stepKey(job),
	}

	if job.Notices != nil {
		out.Banner = job.Notices.Alert
	}

	switch {
	case job.Step.Notify != nil:
		out.Body = joinSentences(job.Reason, job.Step.Notify.Note)

		if job.Step.Notify.Banner != nil {
			out.Banner = job.Step.Notify.Banner
		}
	case job.Step.TellMe != nil:
		out.Body = tellWords(*job.Step.TellMe, job.Before)
	}

	if out.Title == "" {
		out.Title = job.Item.Repo
	}

	return out
}

// stepKey is the key of job's notice: its match, when the match was made (a match ID is
// handed out again once Clear history removes it), the step, and the try, each written
// once in the ledger before the step runs, so a step run again tells the same key.
func stepKey(job Job) string {
	return "autopilot:step:" + strconv.FormatInt(job.MatchID, 10) + ":" +
		job.MatchCreated.UTC().Format(time.RFC3339Nano) + ":" + strconv.Itoa(job.Index) + ":" + strconv.Itoa(job.Attempt)
}

// problemNoticeKey is the key of p's notice: the problem (its routine, its kind, and its search)
// and when it began, which the engine keeps before it tells it, so a problem told again
// tells the same key, and one that ends and comes back a new one.
func problemNoticeKey(p Problem) string {
	return "autopilot:problem:" + p.Key + ":" + p.Since.UTC().Format(time.RFC3339Nano)
}

// tellWords says what a Tell me step tells: that the routine reached it, then how each
// step before it ended, which the steps that make outcomes say more of.
func tellWords(st spec.TellStep, before []MatchStep) string {
	sofar := soFar(before)

	if st.When == catalog.TellVerdict && sofar != "" {
		return sofar
	}

	return joinSentences("It reached Tell me", sofar)
}

// soFar says how each step before ended: "Notify me: done. Pin: skipped (its Only if
// didn't hold)."
func soFar(before []MatchStep) string {
	parts := make([]string, 0, len(before))

	for _, st := range before {
		words := st.Label + ": " + stepWords(st)
		if note := strings.TrimSuffix(strings.TrimSpace(st.Note), "."); note != "" && st.Status != stepDone {
			words += " (" + lowerFirst(note) + ")"
		}

		parts = append(parts, textutil.EndSentence(words))
	}

	return strings.Join(parts, " ")
}

// stepWords says how a step ended, with its outcome.
func stepWords(st MatchStep) string {
	status := st.Status
	if status == stepPending {
		status = "not run"
	}

	for _, o := range catalog.Outcomes() {
		if string(o.ID) == st.Outcome {
			return status + ", " + o.Label
		}
	}

	return status
}

// lowerFirst lower-cases the first letter of s, so a note reads inside a sentence.
func lowerFirst(s string) string {
	for i, r := range s {
		return strings.ToLower(string(r)) + s[i+len(string(r)):]
	}

	return s
}

// joinSentences ends each part as a sentence and joins those that say something.
func joinSentences(parts ...string) string {
	out := make([]string, 0, len(parts))

	for _, p := range parts {
		if p = textutil.Sentence(p); p != "" {
			out = append(out, p)
		}
	}

	return textutil.Truncate(strings.Join(out, " "), maxReasonRunes)
}

// reasonOf says why a match matched, from its decision's trace: what the trigger saw, then
// each condition that held, with what it compared. "1 check failed on a1b2c3d; a failed
// check's name contains “security” (Security / gitleaks)".
func reasonOf(trace []eval.Trace) string {
	parts := make([]string, 0, len(trace))

	for i, tr := range trace {
		if words := traceWords(i == 0 && tr.Path == "trigger", tr); words != "" {
			parts = append(parts, words)
		}
	}

	return textutil.Truncate(textutil.OneLine(strings.Join(parts, "; ")), maxReasonRunes)
}

// traceWords says one step of a trace for a reason: what the trigger saw, or a condition
// that held with what it compared; "" for one that says nothing a reason needs.
func traceWords(trigger bool, tr eval.Trace) string {
	switch {
	case trigger:
		return tr.Actual
	case tr.Result != eval.Yes || tr.Condition == "":
		return ""
	case tr.Actual == "":
		return tr.Condition
	default:
		return tr.Condition + " (" + tr.Actual + ")"
	}
}

// ProblemNotice is a problem as the user is told of it.
type ProblemNotice struct {
	Problem

	// Routine is the routine's name, "" when it can't be read, and RoutineCreated when it
	// was made, which tells it from a later routine given its ID.
	Routine        string
	RoutineCreated time.Time

	// Banner is the routine's own choice of banner for its problems, nil to follow the
	// settings, as a routine that won't load always does.
	Banner *bool

	// Burst reports a problem told with others at once, which is gathered.
	Burst bool
}

// problemNotices tells of each problem through a NoticeSink.
type problemNotices struct {
	sink NoticeSink
}

// NewProblemNotices returns the ProblemSink that tells of each problem through sink, as
// a notice about no item that opens the routine.
func NewProblemNotices(sink NoticeSink) ProblemSink {
	return problemNotices{sink: sink}
}

// Problem tells of p.
func (n problemNotices) Problem(ctx context.Context, p ProblemNotice) error {
	if n.sink == nil {
		return errNoSink
	}

	title := p.Routine
	if title == "" {
		title = "A routine"
	}

	return n.sink.Notice(ctx, Notice{
		Kind: NoticeProblem, RoutineID: p.RoutineID, Routine: p.Routine, CreatedAt: p.RoutineCreated,
		Title: title, Body: textutil.Sentence(p.Detail), Banner: p.Banner, Burst: p.Burst,
		Key: problemNoticeKey(p.Problem),
	})
}
