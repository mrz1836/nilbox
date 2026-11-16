package eval

import (
	"strconv"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// fired is what a trigger came to: its result, the fire key of the occurrence it fired
// for, and why.
type fired struct {
	result Result
	key    string
	actual string
	need   facts.Need
}

// yes, no, and unknownOf make a trigger's outcome.
func yes(key, actual string) fired { return fired{result: Yes, key: key, actual: actual} }
func no(actual string) fired       { return fired{result: No, actual: actual} }

func unknownOf(v facts.Value) fired {
	return fired{result: Unknown, actual: v.Why, need: v.Need}
}

// evalTrigger decides whether the trigger holds for the item, and the occurrence's fire
// key. A click's run holds whatever the trigger, once per click.
func evalTrigger(t spec.Trigger, f facts.Facts, h History, now time.Time) (Result, string, Trace) {
	info, ok := catalog.TriggerByEvent(t.Event)
	tr := Trace{Path: "trigger", Condition: info.Label + ": " + info.Help}

	var out fired

	switch fn, known := triggerTests[t.Event]; {
	case h.Manual != "":
		out = yes("manual:"+h.Manual, "you ran it")
	case !ok || !known:
		tr.Condition = string(t.Event)
		out = fired{result: Unknown, actual: "it isn't a trigger this build reads"}
	case !info.Applies(f.Item.Kind):
		out = no("it's an issue, and " + info.Label + " fires only for pull requests")
	default:
		out = fn(t, f, h, now)
	}

	tr.Result, tr.Actual, tr.Need = out.result, out.actual, out.need

	return out.result, out.key, tr
}

// triggerTest decides one trigger.
type triggerTest func(spec.Trigger, facts.Facts, History, time.Time) fired

// triggerTests decide each trigger.
//
//nolint:gochecknoglobals // a fixed table
var triggerTests = map[catalog.Event]triggerTest{
	catalog.EventAppears: func(spec.Trigger, facts.Facts, History, time.Time) fired {
		return yes(itemKey(catalog.EventAppears), "it's in the watch")
	},
	catalog.EventNewCommits:       newCommits,
	catalog.EventChecksFailed:     checksFailed,
	catalog.EventChecksPassed:     checksPassed,
	catalog.EventChangesRequested: changesRequested,
	catalog.EventApproved:         approved,
	catalog.EventMergeConflicts:   mergeConflicts,
	catalog.EventFallsBehind:      fallsBehind,
	catalog.EventLabeled:          labeled,
	catalog.EventReadyForReview:   readyForReview,
	catalog.EventReviewRequested: func(_ spec.Trigger, f facts.Facts, _ History, _ time.Time) fired {
		return listed(catalog.EventReviewRequested, catalog.ListReviewRequested, f.ReviewAsked, f)
	},
	catalog.EventAssigned: func(_ spec.Trigger, f facts.Facts, _ History, _ time.Time) fired {
		return listed(catalog.EventAssigned, catalog.ListAssigned, f.Assigned, f)
	},
	catalog.EventNewComments: newComments,
	catalog.EventGoesQuiet:   goesQuiet,
	catalog.EventSchedule:    schedule,
	catalog.EventManual: func(spec.Trigger, facts.Facts, History, time.Time) fired {
		return no("it runs only when you run it")
	},
}

// itemKey, commitKey, and dayKey are fire keys once per item, per head commit, and per
// local day.
func itemKey(e catalog.Event) string { return string(e) + ":item" }

func commitKey(e catalog.Event, oid string) string {
	return string(e) + ":commit:" + strings.ToLower(oid)
}

func dayKey(e catalog.Event, now time.Time) string {
	return string(e) + ":day:" + now.Format(time.DateOnly)
}

// stamp writes a moment for a fire key.
func stamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// short writes a commit as its first seven characters.
func short(oid string) string {
	const n = 7
	if len(oid) > n {
		return oid[:n]
	}

	return oid
}

// known returns the first value that isn't known, as a trigger's outcome, or false
// when all are known.
func known(values ...facts.Value) (fired, bool) {
	for _, v := range values {
		if !v.Known() {
			return unknownOf(v), true
		}
	}

	return fired{}, false
}

func newCommits(t spec.Trigger, f facts.Facts, h History, _ time.Time) fired {
	if out, missing := known(f.Head); missing {
		return out
	}

	oid := f.Head.Text

	switch {
	case !t.OwnPushes() && containsAny(h.OwnHeads, oid):
		return no("a routine's own run pushed " + short(oid))
	case !t.FirstCommitToo() && (h.FirstHead == "" || strings.EqualFold(h.FirstHead, oid)):
		return no("it's the first commit this routine has seen, " + short(oid))
	}

	return yes(commitKey(catalog.EventNewCommits, oid), "its head is "+short(oid))
}

// containsAny reports whether list holds s, ignoring case.
func containsAny(list []string, s string) bool {
	for _, e := range list {
		if strings.EqualFold(e, s) {
			return true
		}
	}

	return false
}

func checksFailed(t spec.Trigger, f facts.Facts, _ History, now time.Time) fired {
	failed, pending := f.Value(catalog.FieldChecksFailed), f.Value(catalog.FieldChecksPending)
	if out, missing := known(f.Head, failed, pending); missing {
		return out
	}

	if failed.Number == 0 {
		return no("no check failed")
	}

	if t.WaitsForChecks() {
		if pending.Number > 0 {
			return no(textutil.Counted(pending.Number, "check is", "checks are") + " still running")
		}

		if s := settled(t, f, now); s.result != Yes {
			return s
		}
	}

	return yes(commitKey(catalog.EventChecksFailed, f.Head.Text),
		textutil.Counted(failed.Number, "check", "checks")+" failed on "+short(f.Head.Text))
}

func checksPassed(t spec.Trigger, f facts.Facts, _ History, now time.Time) fired {
	failed, pending := f.Value(catalog.FieldChecksFailed), f.Value(catalog.FieldChecksPending)
	passed, awaiting := f.Value(catalog.FieldChecksPassed), f.Value(catalog.FieldAwaitingApproval)

	if out, missing := known(f.Head, failed, pending, passed, awaiting); missing {
		return out
	}

	switch {
	case failed.Number > 0:
		return no(textutil.Counted(failed.Number, "check", "checks") + " failed")
	case pending.Number > 0:
		return no(textutil.Counted(pending.Number, "check is", "checks are") + " still running")
	case passed.Number == 0:
		return no("it has no checks")
	case awaiting.Bool:
		return no("workflow runs wait for a maintainer to approve them")
	}

	if s := settled(t, f, now); s.result != Yes {
		return s
	}

	return yes(commitKey(catalog.EventChecksPassed, f.Head.Text),
		"every check passed on "+short(f.Head.Text))
}

// settled reports checks that finished long enough ago to count as settled, so a
// workflow that starts only after another finishes isn't missed.
func settled(t spec.Trigger, f facts.Facts, now time.Time) fired {
	settle := t.Settle()
	if settle == 0 {
		return yes("", "")
	}

	if out, missing := known(f.ChecksDone); missing {
		return out
	}

	if since := now.Sub(f.ChecksDone.Since); since < settle {
		return no("the last check finished " + elapsedWords(since) + " ago, and the checks settle after " +
			elapsedWords(settle))
	}

	return yes("", "")
}

func changesRequested(_ spec.Trigger, f facts.Facts, _ History, _ time.Time) fired {
	decision, count := f.Value(catalog.FieldReviewDecision), f.Value(catalog.FieldChangesRequested)
	if out, missing := known(f.Head, decision, count); missing {
		return out
	}

	if decision.Text != "changesRequested" && count.Number == 0 {
		return no("no one asked for changes")
	}

	return yes(commitKey(catalog.EventChangesRequested, f.Head.Text)+":requests:"+strconv.Itoa(count.Number),
		textutil.Counted(count.Number, "request", "requests")+" for changes")
}

func approved(_ spec.Trigger, f facts.Facts, _ History, _ time.Time) fired {
	decision, count := f.Value(catalog.FieldReviewDecision), f.Value(catalog.FieldApprovals)
	if out, missing := known(f.Head, decision, count); missing {
		return out
	}

	if decision.Text != "approved" && count.Number == 0 {
		return no("it isn't approved")
	}

	return yes(commitKey(catalog.EventApproved, f.Head.Text), textutil.Counted(count.Number, "approval", "approvals"))
}

func mergeConflicts(_ spec.Trigger, f facts.Facts, _ History, _ time.Time) fired {
	conflicts := f.Value(catalog.FieldConflicts)
	if out, missing := known(f.Head, conflicts); missing {
		return out
	}

	if !conflicts.Bool {
		return no("it has no merge conflicts")
	}

	return yes(commitKey(catalog.EventMergeConflicts, f.Head.Text), "it conflicts with its base branch")
}

// fallsBehind holds while the pull request is behind its base, once per head commit and
// base branch. It waits for the base to be read, so the one occurrence never fires twice,
// once without its base and once with it.
func fallsBehind(_ spec.Trigger, f facts.Facts, _ History, _ time.Time) fired {
	behind, base := f.Value(catalog.FieldBehind), f.Value(catalog.FieldBase)
	if out, missing := known(f.Head, behind, base); missing {
		return out
	}

	if !behind.Bool {
		return no("it isn't behind its base branch")
	}

	return yes(commitKey(catalog.EventFallsBehind, f.Head.Text)+":base:"+base.Text, "it's behind "+base.Text)
}

func labeled(t spec.Trigger, f facts.Facts, _ History, _ time.Time) fired {
	labels := f.Value(catalog.FieldLabels)
	if out, missing := known(labels); missing {
		return out
	}

	if !containsAny(labels.List, t.Label) {
		return no("it isn't labeled " + t.Label)
	}

	return yes(string(catalog.EventLabeled)+":label:"+strings.ToLower(t.Label), "it's labeled "+t.Label)
}

func readyForReview(_ spec.Trigger, f facts.Facts, _ History, _ time.Time) fired {
	draft := f.Value(catalog.FieldDraft)
	if out, missing := known(draft); missing {
		return out
	}

	if draft.Bool {
		return no("it's a draft")
	}

	return yes(itemKey(catalog.EventReadyForReview), "it isn't a draft")
}

// listed holds while the item is in an inbox list, once per arrival in it: per the
// moment the inbox saw it arrive, or, when the inbox doesn't say, once while it stays.
func listed(e catalog.Event, list string, arrived facts.Value, f facts.Facts) fired {
	lists := f.Value(catalog.FieldLists)
	if out, missing := known(lists); missing {
		return out
	}

	if !containsAny(lists.List, list) {
		return no("it isn't in your " + list + " list")
	}

	if arrived.Known() {
		return yes(string(e)+":at:"+stamp(arrived.Since), "it arrived "+arrived.Since.UTC().Format(time.RFC3339))
	}

	return yes(string(e)+":listed", "it's in your "+list+" list")
}

func newComments(t spec.Trigger, f facts.Facts, h History, _ time.Time) fired {
	comments := f.Value(catalog.FieldComments)
	if out, missing := known(comments); missing {
		return out
	}

	if h.CommentsSeen == nil {
		return no("the routine counts new comments from when it first sees the item")
	}

	if n := comments.Number - *h.CommentsSeen; n < t.CommentCount() {
		return no(textutil.Counted(max(n, 0), "new comment", "new comments") + " so far")
	}

	return yes(string(catalog.EventNewComments)+":comments:"+strconv.Itoa(comments.Number),
		textutil.Counted(comments.Number-*h.CommentsSeen, "new comment", "new comments"))
}

func goesQuiet(t spec.Trigger, f facts.Facts, _ History, now time.Time) fired {
	idle := now.Sub(f.Updated)
	if idle < time.Duration(t.QuietDays())*24*time.Hour {
		return no("its last activity was " + elapsedWords(idle) + " ago")
	}

	return yes(string(catalog.EventGoesQuiet)+":since:"+stamp(f.Updated), "quiet for "+elapsedWords(idle))
}

// schedule holds from the scheduled time until the end of a day it is due, in now's own
// location, once that day: a time missed while the Mac slept runs on waking, the same
// day.
func schedule(t spec.Trigger, _ facts.Facts, _ History, now time.Time) fired {
	if t.Schedule == nil {
		return no("it has no schedule")
	}

	hh, mm, ok := clock(t.Schedule.At)
	if !ok || !dueOn(*t.Schedule, now.Weekday()) {
		return no("it isn't due today")
	}

	at := time.Date(now.Year(), now.Month(), now.Day(), hh, mm, 0, 0, now.Location())
	if now.Before(at) {
		return no("it's due at " + t.Schedule.At)
	}

	return yes(string(catalog.EventSchedule)+":"+now.Format(time.DateOnly), "it was due at "+t.Schedule.At)
}

// clock reads "HH:MM".
func clock(s string) (int, int, bool) {
	h, m, found := strings.Cut(s, ":")
	if !found {
		return 0, 0, false
	}

	hh, errH := strconv.Atoi(h)
	mm, errM := strconv.Atoi(m)

	return hh, mm, errH == nil && errM == nil
}

// dueOn reports whether a schedule is due on a day of the week.
func dueOn(s spec.Schedule, day time.Weekday) bool {
	switch s.Every {
	case catalog.EveryDay:
		return true
	case catalog.EveryWeekday:
		return day != time.Saturday && day != time.Sunday
	case catalog.EveryWeek:
		return catalog.Weekdays()[(int(day)+6)%7] == s.On
	}

	return false
}

// fireKey is the occurrence a match is recorded for: the trigger's own, or the
// routine's own once-per over it. A click's run is once per click whatever it says.
func fireKey(s spec.Spec, natural string, f facts.Facts, h History, now time.Time) string {
	if h.Manual != "" {
		return natural
	}

	switch s.Limits.OncePer {
	case catalog.OncePerItem:
		return itemKey(s.Trigger.Event)
	case catalog.OncePerHeadCommit:
		if f.Head.Known() {
			return commitKey(s.Trigger.Event, f.Head.Text)
		}
	case catalog.OncePerDay:
		return dayKey(s.Trigger.Event, now)
	}

	return natural
}
