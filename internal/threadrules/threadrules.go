// Package threadrules holds the user's rules for their GitHub notifications, the threads
// GitHub keeps for each pull request and issue: whether the app reads them, whether it
// clears one on its own once the work behind it is finished, whether clearing marks it
// done or read, and which moments count as finished. Decide applies the rules to one
// thread and one such moment.
//
// Settings writes the rules, under the keys declared here, and what reads and clears the
// threads obeys them. It is a leaf: it imports only the item kinds and the preference
// encoding, never the store, the GitHub client, or a service, so settings can write the
// rules without depending on what clears the threads.
package threadrules

import (
	"errors"
	"slices"
	"time"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/prefs/prefval"
)

// The preferences the rules are stored under. Settings writes them.
const (
	// KeyRead holds whether the app reads the user's GitHub notifications: "true" or
	// "false", on when unset.
	KeyRead = "threadrules.read"

	// KeyAuto holds whether the app clears a notification on its own once the work behind
	// it is finished: "true" or "false", off when unset, so nothing on GitHub changes
	// until the user turns it on.
	KeyAuto = "threadrules.auto"

	// KeyAction holds what clearing a notification does: "done" or "read", done when
	// unset or anything else.
	KeyAction = "threadrules.action"

	// KeyTriggersOff lists, as a JSON array, the triggers the user turned off. A trigger
	// is on unless listed, so one added later starts on.
	KeyTriggersOff = "threadrules.triggersOff"

	// KeyWatching holds whether a notification from a repository the user only watches
	// is cleared when GitHub says its item is finished: "true" or "false", on when unset.
	KeyWatching = "threadrules.watching"

	// KeyBotActivity holds whether a bot's activity after the item was finished still
	// counts as seen: "true" or "false", on when unset.
	KeyBotActivity = "threadrules.botActivity"

	// KeyCleanupCardDismissed holds whether the user put away the card that first offers
	// to clean up their notifications: "true" or "false", off when unset.
	KeyCleanupCardDismissed = "threadrules.cleanupCardDismissed"
)

// Grace is how long after the moment an item was finished a thread may still have been
// updated and count as seen: GitHub updates a thread for the merge or close itself, and
// for what follows it at once, such as a deleted branch.
const Grace = 2 * time.Minute

// WatchingReason is GitHub's reason for a thread the user gets only because they watch
// its repository.
const WatchingReason = "subscribed"

var (
	// ErrUnknownAction is returned for an Action that is not one of Actions.
	ErrUnknownAction = errors.New("unknown way to clear a notification")

	// ErrUnknownTrigger is returned for a Trigger that is not one of Triggers, nor
	// TriggerByHand.
	ErrUnknownTrigger = errors.New("unknown trigger")

	// ErrNoSwitch is returned for TriggerByHand, which is always on.
	ErrNoSwitch = errors.New("clearing by hand has no switch")

	// ErrUnknownSwitch is returned for a Switch that is not one of Switches.
	ErrUnknownSwitch = errors.New("unknown switch")
)

// Action is what clearing a notification does.
type Action string

// The ways to clear a notification.
const (
	// ActionDone marks the notification done, which takes it out of the user's GitHub
	// inbox. It is the default.
	ActionDone Action = "done"

	// ActionRead marks the notification read, which leaves it in the inbox.
	ActionRead Action = "read"
)

// actions are the ways to clear a notification, the default first: the one list Actions,
// Valid, and DecodeAction read.
//
//nolint:gochecknoglobals // a fixed table
var actions = [...]Action{ActionDone, ActionRead}

// Actions returns the ways to clear a notification, the default first.
func Actions() []Action {
	return slices.Clone(actions[:])
}

// Valid reports whether a is one of Actions.
func (a Action) Valid() bool {
	return slices.Contains(actions[:], a)
}

// DecodeAction reads a stored Action: as stored when it is one of Actions, else the
// default, ActionDone.
func DecodeAction(stored string) Action {
	if a := Action(stored); a.Valid() {
		return a
	}

	return actions[0]
}

// EncodeAction returns the stored form of a. An Action that is not one of Actions is
// refused with ErrUnknownAction.
func EncodeAction(a Action) (string, error) {
	if !a.Valid() {
		return "", ErrUnknownAction
	}

	return string(a), nil
}

// Trigger is a moment the work behind a notification is finished.
type Trigger string

// The triggers. Every one but TriggerByHand has a switch, and each is on until the user
// turns it off.
const (
	// TriggerMerged is a pull request merged, in the app or anywhere else.
	TriggerMerged Trigger = "merged"

	// TriggerPRClosed is a pull request closed without merging, in the app or anywhere
	// else.
	TriggerPRClosed Trigger = "pr_closed"

	// TriggerIssueClosed is an issue closed, in the app or anywhere else.
	TriggerIssueClosed Trigger = "issue_closed"

	// TriggerReviewed is a review the user submitted in the app that approves a pull
	// request or requests changes.
	TriggerReviewed Trigger = "reviewed"

	// TriggerDismissed is an item the user dismissed in the app.
	TriggerDismissed Trigger = "dismissed"

	// TriggerUnassigned is an item the user took themselves off in the app: unassigned
	// themselves, or removed themselves as a reviewer.
	TriggerUnassigned Trigger = "unassigned"

	// TriggerByHand is the user clearing a notification themselves, from an item's menu
	// or the cleanup sheet. It has no switch and no guard but the thread's own.
	TriggerByHand Trigger = "by_hand"
)

// triggerRule is what one trigger is, and what it needs of a thread to clear it.
type triggerRule struct {
	// trigger is the trigger.
	trigger Trigger

	// switched is whether the user can turn the trigger off.
	switched bool

	// kind is the kind of item the trigger can finish, "" for either.
	kind item.Kind

	// needs is the state that bears the trigger out, "" for any.
	needs State
}

// triggerRules are every trigger, those with a switch in the order the settings list
// them: the one table Triggers, Valid, HasSwitch, AppliesTo, MatchesState, and the
// triggers' encoding read. Only a pull request can be merged or reviewed.
//
//nolint:gochecknoglobals // a fixed table
var triggerRules = [...]triggerRule{
	{trigger: TriggerMerged, switched: true, kind: item.KindPR, needs: StateMerged},
	{trigger: TriggerPRClosed, switched: true, kind: item.KindPR, needs: StateClosed},
	{trigger: TriggerIssueClosed, switched: true, kind: item.KindIssue, needs: StateClosed},
	{trigger: TriggerReviewed, switched: true, kind: item.KindPR},
	{trigger: TriggerDismissed, switched: true},
	{trigger: TriggerUnassigned, switched: true},
	{trigger: TriggerByHand},
}

// Triggers returns the triggers that have a switch, in the order the settings list them.
// TriggerByHand is not among them.
func Triggers() []Trigger {
	out := make([]Trigger, 0, len(triggerRules))

	for i := range triggerRules {
		if triggerRules[i].switched {
			out = append(out, triggerRules[i].trigger)
		}
	}

	return out
}

// Valid reports whether t is one of Triggers or TriggerByHand.
func (t Trigger) Valid() bool {
	_, ok := t.rule()

	return ok
}

// HasSwitch reports whether t is one of Triggers, the ones the user can turn off.
func (t Trigger) HasSwitch() bool {
	rule, ok := t.rule()

	return ok && rule.switched
}

// AppliesTo reports whether t can finish an item of kind k: a merge, a pull request's
// close, and a review only a pull request, an issue's close only an issue, and the rest
// either. No trigger applies to a kind that is neither, and an unknown trigger to none.
func (t Trigger) AppliesTo(k item.Kind) bool {
	rule, ok := t.rule()

	return ok && k.Valid() && (rule.kind == "" || rule.kind == k)
}

// MatchesState reports whether s, the state GitHub last reported for the item, bears t
// out: a merge needs MERGED, and a close CLOSED (a merged pull request is MERGED, not
// CLOSED); what the user did in the app needs no state. An unknown trigger matches none.
func (t Trigger) MatchesState(s State) bool {
	rule, ok := t.rule()

	return ok && (rule.needs == "" || rule.needs == s)
}

// rule returns t's entry in triggerRules, and false for a trigger that has none.
func (t Trigger) rule() (triggerRule, bool) {
	for i := range triggerRules {
		if triggerRules[i].trigger == t {
			return triggerRules[i], true
		}
	}

	return triggerRule{}, false
}

// DecodeTriggersOff reads the triggers that are off from the stored value of
// KeyTriggersOff, in the order of Triggers, each once. A value that is unset or can't be
// read lists none, and a name that is not one of Triggers, TriggerByHand's included, is
// dropped.
func DecodeTriggersOff(stored string) []Trigger {
	listed, _ := prefval.List[Trigger](stored)

	return triggersOff(listed, "", false)
}

// EncodeTriggersOff writes the triggers that are off as the value of KeyTriggersOff, in
// the order of Triggers, each once, leaving out any that is not one of Triggers.
func EncodeTriggersOff(off []Trigger) string {
	return prefval.EncodeList(triggersOff(off, "", false))
}

// SetTrigger turns trigger on or off in off, the triggers that are off, and returns the
// new list in the order of Triggers. TriggerByHand is refused with ErrNoSwitch, and any
// other trigger that is not one of Triggers with ErrUnknownTrigger.
func SetTrigger(off []Trigger, trigger Trigger, on bool) ([]Trigger, error) {
	switch {
	case trigger == TriggerByHand:
		return nil, ErrNoSwitch
	case !trigger.HasSwitch():
		return nil, ErrUnknownTrigger
	}

	return triggersOff(off, trigger, !on), nil
}

// triggersOff returns the triggers of Triggers that listed holds, in their order, each
// once, never nil, with changed off or not as off says ("" changes none).
func triggersOff(listed []Trigger, changed Trigger, off bool) []Trigger {
	out := []Trigger{}

	for i := range triggerRules {
		t := triggerRules[i].trigger
		if !triggerRules[i].switched {
			continue
		}

		isOff := slices.Contains(listed, t)
		if t == changed {
			isOff = off
		}

		if isOff {
			out = append(out, t)
		}
	}

	return out
}

// Switch names one of the rules' switches, for the settings' one setter of them.
type Switch string

// The switches.
const (
	// SwitchRead is whether the app reads the user's GitHub notifications.
	SwitchRead Switch = "read"

	// SwitchAuto is whether the app clears a notification on its own.
	SwitchAuto Switch = "auto"

	// SwitchWatching is whether a notification from a repository the user only watches
	// is cleared when GitHub says its item is finished.
	SwitchWatching Switch = "watching"

	// SwitchBotActivity is whether a bot's activity after the item was finished still
	// counts as seen.
	SwitchBotActivity Switch = "botActivity"
)

// switchKeys are the switches in the order the settings list them, each with the
// preference key it is stored under: the one table Switches, Valid, Key, and Keys read.
//
//nolint:gochecknoglobals // a fixed table
var switchKeys = [...]struct {
	name Switch
	key  string
}{
	{name: SwitchRead, key: KeyRead},
	{name: SwitchAuto, key: KeyAuto},
	{name: SwitchWatching, key: KeyWatching},
	{name: SwitchBotActivity, key: KeyBotActivity},
}

// Switches returns the switches, in the order the settings list them.
func Switches() []Switch {
	out := make([]Switch, 0, len(switchKeys))

	for i := range switchKeys {
		out = append(out, switchKeys[i].name)
	}

	return out
}

// Valid reports whether s is one of Switches.
func (s Switch) Valid() bool {
	return s.Key() != ""
}

// Key returns the preference key s is stored under, or "" for a Switch that is not one
// of Switches.
func (s Switch) Key() string {
	for i := range switchKeys {
		if switchKeys[i].name == s {
			return switchKeys[i].key
		}
	}

	return ""
}

// Rules are the user's rules for their GitHub notifications, as the settings show them.
type Rules struct {
	// Read is whether the app reads the user's GitHub notifications. On by default. Off,
	// nothing touches a notification, not even by hand.
	Read bool `json:"read"`

	// Auto is whether the app clears a notification on its own once the work behind it is
	// finished. Off by default: nothing on GitHub changes until the user turns it on.
	Auto bool `json:"auto"`

	// Action is what clearing a notification does, ActionDone by default. Decide reads
	// anything but ActionRead as ActionDone.
	Action Action `json:"action"`

	// TriggersOff are the triggers the user turned off, in the order of Triggers. Never
	// nil.
	TriggersOff []Trigger `json:"triggersOff"`

	// Watching is whether a notification from a repository the user only watches (GitHub's
	// reason WatchingReason) is cleared when GitHub says its item is finished. On by
	// default. What the user did in the app clears such a notification either way.
	Watching bool `json:"watching"`

	// BotActivity is whether a bot's activity after the item was finished still counts
	// as seen, so it doesn't hold the notification back. On by default.
	BotActivity bool `json:"botActivity"`

	// CleanupCardDismissed is whether the user put away the card that first offers to
	// clean up their notifications ("Not now").
	CleanupCardDismissed bool `json:"cleanupCardDismissed"`
}

// Defaults returns the rules of a fresh database.
func Defaults() Rules {
	return Decode(nil)
}

// Keys returns every preference the rules are stored under.
func Keys() []string {
	keys := make([]string, 0, len(switchKeys)+3)

	for i := range switchKeys {
		keys = append(keys, switchKeys[i].key)
	}

	return append(keys, KeyAction, KeyTriggersOff, KeyCleanupCardDismissed)
}

// Decode reads the rules from stored preferences, each written as the settings store
// it: a switch with prefval.EncodeBool, the action with EncodeAction, and the triggers
// that are off with EncodeTriggersOff. A value that is unset or can't be read keeps its
// default, an Action that is not one of Actions reads as ActionDone, and a trigger that
// is not one of Triggers is dropped.
func Decode(stored map[string]string) Rules {
	return Rules{
		Read:                 prefval.Bool(stored[KeyRead], true),
		Auto:                 prefval.Bool(stored[KeyAuto], false),
		Action:               DecodeAction(stored[KeyAction]),
		TriggersOff:          DecodeTriggersOff(stored[KeyTriggersOff]),
		Watching:             prefval.Bool(stored[KeyWatching], true),
		BotActivity:          prefval.Bool(stored[KeyBotActivity], true),
		CleanupCardDismissed: prefval.Bool(stored[KeyCleanupCardDismissed], false),
	}
}

// On reports whether trigger is on: TriggerByHand always, one of Triggers unless the user
// turned it off, and any other never.
func (r Rules) On(trigger Trigger) bool {
	return trigger == TriggerByHand || trigger.HasSwitch() && !slices.Contains(r.TriggersOff, trigger)
}

// State is the state of a thread's pull request or issue, as GitHub last reported it.
type State string

// The states.
const (
	// StateUnknown is a state not reported, or not known yet.
	StateUnknown State = ""

	// StateOpen is an open pull request or issue, one reopened included.
	StateOpen State = "OPEN"

	// StateClosed is a pull request closed without merging, or a closed issue.
	StateClosed State = "CLOSED"

	// StateMerged is a merged pull request.
	StateMerged State = "MERGED"
)

// AfterMoment is who acted on a thread after the moment its item was finished, from a
// later check of its activity.
//
// It is a promise about all of that activity, not its latest: AfterBot means every
// activity after the moment is known to be a bot's. A caller that has ever seen a
// person's activity on the thread after the moment keeps AfterPerson until the thread is
// read, even when a bot acted last, so a bot's comment never hides a person's.
type AfterMoment string

// Who acted after the moment.
const (
	// AfterUnknown is no check made, or none that could tell.
	AfterUnknown AfterMoment = ""

	// AfterBot is bots alone: every activity after the moment is known to be a bot's.
	AfterBot AfterMoment = "bot"

	// AfterPerson is a person, at least once since the moment, whoever acted last. It
	// stays until the thread is read.
	AfterPerson AfterMoment = "person"
)

// Thread is what Decide needs to know about one notification thread.
type Thread struct {
	// Kind is the kind of the thread's subject, or "" when it is neither a pull request
	// nor an issue.
	Kind item.Kind

	// Unread is whether the thread is unread.
	Unread bool

	// UpdatedAt is when GitHub last updated the thread. The zero time is not known, and
	// an unread thread that isn't known to be quiet since the moment isn't seen.
	UpdatedAt time.Time

	// Reason is GitHub's reason the user gets the thread, such as WatchingReason.
	Reason string

	// State is the state of the thread's subject, as GitHub last reported it.
	State State

	// AfterMoment is who acted on the thread after the moment, as far as a later check
	// could tell. AfterBot promises that every activity after the moment is a bot's: a
	// caller that ever saw a person's activity after the moment keeps AfterPerson until
	// the thread is read.
	AfterMoment AfterMoment
}

// Moment is when and how the work behind a thread was finished.
type Moment struct {
	// Trigger is what finished it.
	Trigger Trigger

	// At is when it was finished: the user's click in the app, the review's time, or
	// GitHub's closedAt.
	At time.Time

	// Outside is whether the moment comes from GitHub's state (a merge or a close the
	// app found), rather than from something the user did in the app. The app's own
	// merges and closes are not Outside.
	Outside bool
}

// VerdictKind is what Decide says to do with a thread.
type VerdictKind string

// The verdicts.
const (
	// VerdictClear clears the thread, as Rules.Action says.
	VerdictClear VerdictKind = "clear"

	// VerdictSkip leaves the thread alone, for its Reason.
	VerdictSkip VerdictKind = "skip"

	// VerdictHold leaves the thread alone for now: it changed after the moment, and the
	// user may not have seen what changed.
	VerdictHold VerdictKind = "hold"
)

// Reason is why Decide skips or holds a thread.
type Reason string

// The reasons.
const (
	// ReasonNotItem is a thread about neither a pull request nor an issue, which is never
	// touched, not even by hand.
	ReasonNotItem Reason = "not_item"

	// ReasonAlreadyRead is a thread already read while clearing marks one read.
	ReasonAlreadyRead Reason = "already_read"

	// ReasonOff is reading the notifications turned off, which leaves every thread alone,
	// even by hand, or clearing on its own turned off.
	ReasonOff Reason = "off"

	// ReasonRuleOff is a trigger turned off or unknown, one that can't finish the
	// thread's kind of item, or a thread from a repository the user only watches while
	// such threads are left alone.
	ReasonRuleOff Reason = "rule_off"

	// ReasonStillOpen is an item whose state doesn't bear the trigger out, one reopened
	// included.
	ReasonStillOpen Reason = "still_open"

	// ReasonNewActivity is an unread thread not known to be quiet since the moment: one
	// updated more than Grace after it, or whose update time isn't known, unless the
	// rules let a bot's activity pass.
	ReasonNewActivity Reason = "new_activity"
)

// Verdict is what Decide says to do with a thread, and why.
type Verdict struct {
	// Kind is the verdict.
	Kind VerdictKind `json:"kind"`

	// Reason is why the thread is skipped or held, "" when it is cleared.
	Reason Reason `json:"reason"`

	// Trigger is the moment's trigger.
	Trigger Trigger `json:"trigger"`
}

// Decide says what to do with thread t once the work behind it was finished at moment m,
// under rules r. It reads no clock and allocates nothing. The first rule that applies
// wins:
//
//  1. A thread about neither a pull request nor an issue is skipped, not even cleared by
//     hand.
//  2. While reading the notifications is off, the thread is skipped, even by hand.
//  3. While clearing marks a thread read, one already read is skipped.
//  4. Clearing by hand clears.
//  5. While clearing on its own is off, the thread is skipped.
//  6. A trigger turned off, one that can't finish the thread's kind of item, or a moment
//     from GitHub's state on a thread from a repository the user only watches, while
//     Rules.Watching is off, skips the thread.
//  7. A state that doesn't bear the trigger out (Trigger.MatchesState) skips it.
//  8. A thread not seen is held: one unread and updated more than Grace after the
//     moment, or at a time not known, unless Rules.BotActivity lets a bot's activity
//     pass (see AfterBot).
//  9. Otherwise the thread is cleared.
func Decide(r Rules, t Thread, m Moment) Verdict {
	switch {
	case !t.Kind.Valid():
		return skip(m, ReasonNotItem)
	case !r.Read:
		return skip(m, ReasonOff)
	case r.Action == ActionRead && !t.Unread:
		return skip(m, ReasonAlreadyRead)
	case m.Trigger == TriggerByHand:
		return Verdict{Kind: VerdictClear, Trigger: m.Trigger}
	case !r.Auto:
		return skip(m, ReasonOff)
	case !r.applies(t, m):
		return skip(m, ReasonRuleOff)
	case !m.Trigger.MatchesState(t.State):
		return skip(m, ReasonStillOpen)
	case !r.seen(t, m):
		return Verdict{Kind: VerdictHold, Reason: ReasonNewActivity, Trigger: m.Trigger}
	default:
		return Verdict{Kind: VerdictClear, Trigger: m.Trigger}
	}
}

// applies reports whether the rules let m's trigger clear t: the trigger is on and can
// finish t's kind of item, and, for a moment from GitHub's state, t is not from a
// repository the user only watches unless such threads are cleared too.
func (r Rules) applies(t Thread, m Moment) bool {
	if !r.On(m.Trigger) || !m.Trigger.AppliesTo(t.Kind) {
		return false
	}

	return !m.Outside || t.Reason != WatchingReason || r.Watching
}

// seen reports whether the user has seen t as of m: it is read, or it was last updated
// at a known time no later than Grace after the moment, or, while the rules let a bot
// pass, every activity after the moment was a bot's.
func (r Rules) seen(t Thread, m Moment) bool {
	quiet := !t.UpdatedAt.IsZero() && !t.UpdatedAt.After(m.At.Add(Grace))
	if !t.Unread || quiet {
		return true
	}

	return r.BotActivity && t.AfterMoment == AfterBot
}

// skip returns the verdict that skips a thread at m for reason.
func skip(m Moment, reason Reason) Verdict {
	return Verdict{Kind: VerdictSkip, Reason: reason, Trigger: m.Trigger}
}
