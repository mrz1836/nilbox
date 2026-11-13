package threadrules_test

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/threadrules"
)

// decideInput is one fuzzed call of Decide, spelled in the types the fuzzer generates.
type decideInput struct {
	kind, reason, state, after, trigger, action, off string
	read, unread, outside, auto, watching, bots      bool
	updated, at                                      int64
}

// call builds Decide's arguments from in: the times as Unix nanoseconds, a thread
// updated at 0 as one whose update time isn't known.
func (in decideInput) call() (threadrules.Rules, threadrules.Thread, threadrules.Moment) {
	rules := threadrules.Rules{
		Read: in.read, Auto: in.auto, Action: threadrules.Action(in.action), Watching: in.watching,
		BotActivity: in.bots, TriggersOff: threadrules.DecodeTriggersOff(in.off),
	}
	thread := threadrules.Thread{
		Kind: item.Kind(in.kind), Unread: in.unread, Reason: in.reason, State: threadrules.State(in.state),
		AfterMoment: threadrules.AfterMoment(in.after),
	}

	if in.updated != 0 {
		thread.UpdatedAt = time.Unix(0, in.updated).UTC()
	}

	moment := threadrules.Moment{Trigger: threadrules.Trigger(in.trigger), At: time.Unix(0, in.at).UTC(), Outside: in.outside}

	return rules, thread, moment
}

// FuzzDecide checks Decide's promises on any thread, moment, and rules: it never clears a
// thread about anything but a pull request or an issue, nor any thread while reading the
// notifications is off, nor one already read while clearing marks one read; clearing by
// hand is never held; with clearing on its own off, only clearing by hand clears; any
// other clear is by a trigger that is on, applies to the item's kind, and matches its
// state, of a thread read or known to be quiet since the moment, but for a bot's activity
// the rules let pass; and a verdict's reason goes with its kind.
func FuzzDecide(f *testing.F) {
	at := finished.UnixNano()
	grace := int64(threadrules.Grace)

	for _, seed := range []decideInput{
		{kind: "pr", state: "MERGED", trigger: "merged", read: true, unread: true, auto: true, updated: at, at: at},
		{
			kind: "issue", state: "CLOSED", trigger: "issue_closed", read: true, unread: true, auto: true,
			updated: at + grace, at: at,
		},
		{
			kind: "issue", state: "CLOSED", trigger: "issue_closed", read: true, unread: true, auto: true,
			updated: at + grace + 1, at: at,
		},
		{kind: "pr", state: "OPEN", trigger: "by_hand", action: "read", read: true, updated: at, at: at},
		{kind: "pr", state: "OPEN", trigger: "by_hand", unread: true, auto: true, updated: at, at: at},
		{kind: "", state: "MERGED", trigger: "by_hand", read: true, unread: true, auto: true, updated: at, at: at},
		{
			kind: "pr", state: "CLOSED", trigger: "pr_closed", read: true, unread: true, auto: true, off: `["pr_closed"]`,
			at: at,
		},
		{
			kind: "pr", reason: "subscribed", state: "MERGED", trigger: "merged", read: true, unread: true, outside: true,
			auto: true, updated: at, at: at,
		},
		{
			kind: "pr", state: "MERGED", trigger: "merged", after: "bot", read: true, unread: true, auto: true, bots: true,
			updated: at + int64(time.Hour), at: at,
		},
		{kind: "pr", state: "MERGED", trigger: "merged", read: true, unread: true, auto: true, updated: 0, at: at},
		{
			kind: "pr", state: "MERGED", trigger: "merged", after: "bot", read: true, unread: true, auto: true, bots: true,
			updated: 0, at: at,
		},
		{kind: "issue", state: "OPEN", trigger: "reviewed", read: true, unread: true, auto: true, updated: at, at: at},
		{kind: "discussion", state: "", trigger: "commented", action: "archive", off: `{"x":1}`, updated: -1, at: 1},
	} {
		f.Add(seed.kind, seed.reason, seed.state, seed.after, seed.trigger, seed.action, seed.off, seed.read,
			seed.unread, seed.outside, seed.auto, seed.watching, seed.bots, seed.updated, seed.at)
	}

	f.Fuzz(func(t *testing.T, kind, reason, state, after, trigger, action, off string,
		read, unread, outside, auto, watching, bots bool, updated, momentAt int64,
	) {
		rules, thread, moment := decideInput{
			kind: kind, reason: reason, state: state, after: after, trigger: trigger, action: action, off: off,
			read: read, unread: unread, outside: outside, auto: auto, watching: watching, bots: bots, updated: updated,
			at: momentAt,
		}.call()

		got := threadrules.Decide(rules, thread, moment)

		checkVerdict(t, got, moment)
		checkPromises(t, got, rules, thread, moment)
	})
}

// checkVerdict checks a verdict's shape: one of the kinds, a reason exactly when it isn't
// a clear (new activity alone holds), and the moment's trigger.
func checkVerdict(t *testing.T, got threadrules.Verdict, m threadrules.Moment) {
	t.Helper()

	assert.Equal(t, m.Trigger, got.Trigger)

	switch got.Kind {
	case threadrules.VerdictClear:
		assert.Empty(t, got.Reason)
	case threadrules.VerdictHold:
		assert.Equal(t, threadrules.ReasonNewActivity, got.Reason)
	case threadrules.VerdictSkip:
		assert.NotEmpty(t, got.Reason)
		assert.NotEqual(t, threadrules.ReasonNewActivity, got.Reason)
	default:
		require.Fail(t, "an unknown verdict", "%q", got.Kind)
	}
}

// checkPromises checks what Decide promises never to do.
func checkPromises(t *testing.T, got threadrules.Verdict, r threadrules.Rules, th threadrules.Thread,
	m threadrules.Moment,
) {
	t.Helper()

	cleared := got.Kind == threadrules.VerdictClear

	if !th.Kind.Valid() {
		assert.Equal(t, threadrules.ReasonNotItem, got.Reason, "a thread about anything else is never touched")
	}

	if !r.Read {
		assert.False(t, cleared, "with reading off, nothing clears, not even by hand")
	}

	if r.Action == threadrules.ActionRead && !th.Unread {
		assert.False(t, cleared, "a thread already read is never marked read again")
	}

	if m.Trigger == threadrules.TriggerByHand {
		assert.NotEqual(t, threadrules.VerdictHold, got.Kind, "clearing by hand is never held")
	}

	if cleared && m.Trigger != threadrules.TriggerByHand {
		checkTriggerClear(t, r, th, m)
	}
}

// checkTriggerClear checks a thread a trigger cleared: clearing on its own is on, the
// trigger is known, on, and fits the item, and the thread is read or known to be quiet
// since the moment, but for a bot's activity the rules let pass.
func checkTriggerClear(t *testing.T, r threadrules.Rules, th threadrules.Thread, m threadrules.Moment) {
	t.Helper()

	assert.True(t, r.Auto, "with clearing on its own off, only clearing by hand clears")
	assert.True(t, r.On(m.Trigger), "only a trigger that is on clears")
	assert.True(t, slices.Contains(threadrules.Triggers(), m.Trigger), "only a known trigger clears")
	assert.True(t, m.Trigger.AppliesTo(th.Kind), "only a trigger that can finish the item clears")
	assert.True(t, m.Trigger.MatchesState(th.State), "only a state that bears the trigger out clears")

	botsPass := r.BotActivity && th.AfterMoment == threadrules.AfterBot
	if th.Unread && !botsPass {
		assert.False(t, th.UpdatedAt.IsZero(), "an unread thread updated at a time not known is not seen")
		assert.False(t, th.UpdatedAt.After(m.At.Add(threadrules.Grace)), "an unread thread updated past the grace is not seen")
	}
}

// FuzzDecode checks that any stored values decode to rules whose triggers are known, each
// once and in the order of Triggers, and which read back unchanged once stored again as
// the settings store each key.
func FuzzDecode(f *testing.F) {
	for _, seed := range [][7]string{
		{"", "", "", "", "", "", ""},
		{storedOff, storedOn, "read", `["unassigned","merged"]`, storedOff, "0", storedOn},
		{"yes", "on", "archive", `{"merged":true}`, " true", "maybe", "2"},
		{"T", "F", "READ", `["by_hand","by_hand","later",""]`, "1", "f", "FALSE"},
		{"null", "null", "null", "null", "null", "null", "null"},
		{storedOn, storedOn, "done", `["merged",7]`, storedOn, storedOn, storedOn},
	} {
		f.Add(seed[0], seed[1], seed[2], seed[3], seed[4], seed[5], seed[6])
	}

	f.Fuzz(func(t *testing.T, read, auto, action, off, watching, bots, card string) {
		rules := threadrules.Decode(map[string]string{
			threadrules.KeyRead: read, threadrules.KeyAuto: auto, threadrules.KeyAction: action,
			threadrules.KeyTriggersOff: off, threadrules.KeyWatching: watching, threadrules.KeyBotActivity: bots,
			threadrules.KeyCleanupCardDismissed: card,
		})

		assert.True(t, rules.Action.Valid(), "action %q", rules.Action)
		require.NotNil(t, rules.TriggersOff)

		last := -1

		for _, trigger := range rules.TriggersOff {
			at := slices.Index(threadrules.Triggers(), trigger)
			assert.Greater(t, at, last, "%q is unknown, out of order, or repeated in %q", trigger, rules.TriggersOff)

			last = at
		}

		assert.Equal(t, rules, threadrules.Decode(storedAs(t, rules)))
	})
}
