// Package spec is a routine's definition: what it watches, the trigger it fires on, the
// conditions it checks, the steps it runs in order, and its limits, as one JSON document
// with a version.
//
// A spec is checked strictly on every save and every load: Decode refuses a field the
// format doesn't have, a key given twice, and anything after the document; Normalize
// trims, sorts, and removes repeats; and Validate holds every value to its range and
// the steps to their order, naming the part and the field of each problem. Load does all
// three, and a spec it can't load (from a newer build, unreadable, or no longer valid) is
// broken, not fatal: its routine says why and doesn't run, and the rest carry on.
//
// The spec holds only the definition: a routine's name, mode (Dry run, Ask first,
// Autopilot), on or off switch, and account are its row's, so switching a routine on
// never touches its definition. Prompts are named by nickname or by starter and profiles
// by agent and name, never by ID, so a spec works on another Mac.
//
// Format version 1 holds every kind of step, including those whose dispatchers come
// later, so a later build adds the code that runs a step, never a format change. A newer
// build migrates an older version in Go; an older build refuses a newer one rather than
// guess at it.
//
// Sentence writes a spec as the one sentence the builder, the routine list, the menu bar,
// and an export all show. The package is a leaf: it imports only catalog, watch, item,
// repo, sensitive (whose patterns a condition on paths uses), and textutil.
package spec

import (
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/watch"
)

// Version is the format version this build writes and reads.
const Version = 1

// Spec is a routine's definition.
type Spec struct {
	// Version is the format version it was written in.
	Version int `json:"version"`

	// Watch is which items it looks at.
	Watch Watch `json:"watch"`

	// Trigger is what makes it fire: one per routine.
	Trigger Trigger `json:"trigger"`

	// Conditions are which of the items that fire count.
	Conditions Conditions `json:"conditions"`

	// Steps are what it does, in order.
	Steps []Step `json:"steps"`

	// Limits are how often, and how much.
	Limits Limits `json:"limits"`

	// Notify is which of the routine's notices show a banner, over Settings →
	// Notifications'; nil takes the settings'.
	Notify *Notices `json:"notify,omitempty"`

	// Requires are the tools it needs beyond what readiness works out on its own, such
	// as "pnpm" or "govulncheck".
	Requires []string `json:"requires,omitempty"`
}

// Watch is which items a routine looks at: those in any of its sources.
type Watch struct {
	Sources []Source `json:"sources"`
}

// Source is one of a watch's sources: inbox lists, or a search. Exactly one is set.
type Source struct {
	// Lists are inbox lists, which the inbox reads on every refresh already.
	Lists []catalog.ListSource `json:"lists,omitempty"`

	// Search is a GitHub search the builder writes from simple fields.
	Search *watch.Search `json:"search,omitempty"`
}

// Trigger is what makes a routine fire, with its options. An option the trigger doesn't
// take is refused; one left out takes its default.
type Trigger struct {
	Event catalog.Event `json:"event"`

	// WaitForChecks, for Checks fail, waits until every check has finished and settled.
	WaitForChecks *bool `json:"waitForChecks,omitempty"`

	// SettleMinutes, for Checks fail and Checks pass, is how long after the last check
	// finished the checks count as settled.
	SettleMinutes *int `json:"settleMinutes,omitempty"`

	// FirstToo, for New commits, fires on the first head commit the routine sees too.
	FirstToo *bool `json:"firstToo,omitempty"`

	// OwnPushesToo, for New commits, fires on a commit a routine's own run pushed.
	OwnPushesToo *bool `json:"ownPushesToo,omitempty"`

	// Count, for New comments, is how many new comments make it fire.
	Count *int `json:"count,omitempty"`

	// Days, for Goes quiet, is how many quiet days make it fire.
	Days *int `json:"days,omitempty"`

	// Label, for Labeled, is the label it fires on.
	Label string `json:"label,omitempty"`

	// Schedule, for On a schedule, is when it fires.
	Schedule *Schedule `json:"schedule,omitempty"`
}

// Schedule is when a scheduled routine fires: every day, weekday, or week, at a local
// time of day.
type Schedule struct {
	Every catalog.Every `json:"every"`

	// At is the time of day, "HH:MM" on a 24-hour clock.
	At string `json:"at"`

	// On is the day of a weekly schedule.
	On catalog.Weekday `json:"on,omitempty"`
}

// Match is how a list of conditions combines.
type Match string

// The ways to combine conditions.
const (
	// MatchAll is yes when every condition is yes.
	MatchAll Match = "all"

	// MatchAny is yes when one condition is.
	MatchAny Match = "any"
)

// Conditions are a routine's conditions, or one group of them.
type Conditions struct {
	Match Match    `json:"match"`
	Of    []Clause `json:"of"`
}

// Clause is one condition, or a group of them, one level deep. Exactly one of Field and
// Group is set.
type Clause struct {
	// Field is the field the condition reads.
	Field catalog.FieldID `json:"field,omitempty"`

	// Op is how it compares.
	Op catalog.Op `json:"op,omitempty"`

	// Value is an operator's one value, and Values its several: a number is written as
	// its digits, and a length as "30m", "2h", or "14d".
	Value  string   `json:"value,omitempty"`
	Values []string `json:"values,omitempty"`

	// Group is a group of conditions, which holds no group itself.
	Group *Conditions `json:"group,omitempty"`
}

// Empty is a step, or an action, that takes no settings.
type Empty struct{}

// Step is one step of a routine's Then. Exactly one kind is set.
type Step struct {
	Notify        *NotifyStep    `json:"notify,omitempty"`
	Pin           *Empty         `json:"pin,omitempty"`
	Snooze        *SnoozeStep    `json:"snooze,omitempty"`
	Dismiss       *Empty         `json:"dismiss,omitempty"`
	Restore       *Empty         `json:"restore,omitempty"`
	MarkDone      *Empty         `json:"markDone,omitempty"`
	GitHub        []GitHubAction `json:"github,omitempty"`
	Agent         *AgentStep     `json:"agent,omitempty"`
	WaitForChecks *WaitStep      `json:"waitForChecks,omitempty"`
	Merge         *MergeStep     `json:"merge,omitempty"`
	PostReview    *ReviewStep    `json:"postReview,omitempty"`
	TellMe        *TellStep      `json:"tellMe,omitempty"`

	// OnlyIf runs the step only when an earlier step ended so; a step whose Only if
	// doesn't hold is skipped, and the routine goes on.
	OnlyIf catalog.Outcome `json:"onlyIf,omitempty"`
}

// NotifyStep is Notify me.
type NotifyStep struct {
	// A line of the user's own, which the notice carries beside the reason.
	Note string `json:"note,omitempty"`

	// Banner shows a banner (true) or only an entry in the Notifications page (false);
	// nil follows the routine's notices.
	Banner *bool `json:"banner,omitempty"`
}

// SnoozeStep is Snooze, under the user's wake rules.
type SnoozeStep struct {
	// Days is how long it snoozes; nil is catalog.DefaultSnoozeDays.
	Days *int `json:"days,omitempty"`
}

// GitHubAction is one of a GitHub step's actions, sent in the routine's mode. Exactly
// one is set.
type GitHubAction struct {
	// Rebase asks Dependabot to rebase, as the review sheet does.
	Rebase *Empty `json:"rebase,omitempty"`

	// UpdateBranch updates the branch from its base, as the review sheet does.
	UpdateBranch *Empty `json:"updateBranch,omitempty"`

	// Reply comments one of the user's saved replies, filled in for the item.
	Reply *ReplyAction `json:"reply,omitempty"`

	// Labels adds and removes labels, as the issue sheet does.
	Labels *LabelsAction `json:"labels,omitempty"`
}

// ReplyAction is the reply a GitHub step comments.
type ReplyAction struct {
	Prompt PromptRef `json:"prompt"`
}

// LabelsAction is the labels a GitHub step adds and removes.
type LabelsAction struct {
	Add    []string `json:"add,omitempty"`
	Remove []string `json:"remove,omitempty"`
}

// PromptRef names a library prompt by its starter's key or its nickname, or the item's
// own default prompt. Exactly one is set.
type PromptRef struct {
	// Starter is a starter prompt's key, such as "fix-checks": the user's copy of it,
	// whatever they renamed it.
	Starter string `json:"starter,omitempty"`

	// Nickname is a prompt's nickname, unique ignoring case.
	Nickname string `json:"nickname,omitempty"`

	// ItemDefault is the item's own default prompt, as a click picks it.
	ItemDefault bool `json:"itemDefault,omitempty"`
}

// AgentStep is Run a coding agent. Every setting left out inherits what a click would
// use.
type AgentStep struct {
	// Agent is "inherit", the launch defaults' agent, or an agent's ID.
	Agent string `json:"agent,omitempty"`

	// Prompt is the prompt it runs.
	Prompt PromptRef `json:"prompt"`

	// Where is where it runs; "" is catalog.DefaultWhere, the background.
	Where catalog.Where `json:"where,omitempty"`

	// Profile is the agent's profile by name, "" to inherit.
	Profile string `json:"profile,omitempty"`

	// Fallbacks are the profiles to use, in order, when one is out of usage.
	Fallbacks []ProfileRef `json:"fallbacks,omitempty"`

	// Model and Effort are the agent's own, "" to inherit.
	Model  string `json:"model,omitempty"`
	Effort string `json:"effort,omitempty"`

	// PlanMode and Worktree are a terminal run's, nil to inherit; in the background the
	// fence's tools decide plan mode, and the worktree is always on.
	PlanMode *bool `json:"planMode,omitempty"`
	Worktree *bool `json:"worktree,omitempty"`

	// LaunchFor are rows that override the agent, profile, model, and effort for one org
	// or repository, for this routine only.
	LaunchFor []LaunchRow `json:"launchFor,omitempty"`

	// Fence is a background run's budget, time limit, and tools; nil takes the defaults.
	Fence *Fence `json:"fence,omitempty"`
}

// ProfileRef names an agent's profile.
type ProfileRef struct {
	Agent   string `json:"agent"`
	Profile string `json:"profile"`
}

// LaunchRow overrides an agent step's launch settings for one org or repository.
type LaunchRow struct {
	// Target is an org (or user), "acme-org", or a repository, "acme-org/api". A
	// repository's row wins over its org's.
	Target string `json:"target"`

	Agent   string `json:"agent,omitempty"`
	Profile string `json:"profile,omitempty"`
	Model   string `json:"model,omitempty"`
	Effort  string `json:"effort,omitempty"`
}

// Fence is what holds a background run in.
type Fence struct {
	// Budget is the run's budget; nil is catalog.DefaultRunCents in dollars.
	Budget *Budget `json:"budget,omitempty"`

	// TimeoutMinutes is the run's time limit; nil is catalog.DefaultTimeoutMinutes.
	TimeoutMinutes *int `json:"timeoutMinutes,omitempty"`

	// Tools is the tools preset; "" is catalog.DefaultPreset.
	Tools catalog.Preset `json:"tools,omitempty"`

	// Allow and Deny are extra commands, which never loosen the fence's hard lines.
	Allow []string `json:"allow,omitempty"`
	Deny  []string `json:"deny,omitempty"`
}

// Budget is an amount in dollars or in tokens. Exactly one is set.
type Budget struct {
	USD    *float64 `json:"usd,omitempty"`
	Tokens *int64   `json:"tokens,omitempty"`
}

// WaitStep is Wait for the checks, on the commit the agent pushed.
type WaitStep struct {
	// SettleMinutes is how long after the last check finished the checks count as
	// settled; nil is catalog.DefaultSettleMinutes.
	SettleMinutes *int `json:"settleMinutes,omitempty"`

	// GiveUpHours is how long it waits before it gives up; nil is
	// catalog.DefaultGiveUpHours.
	GiveUpHours *int `json:"giveUpHours,omitempty"`
}

// MergeStep is Merge it, inside GitHub's own rules.
type MergeStep struct {
	How catalog.MergeHow `json:"how"`

	// Method is the merge method; "" is the repository's own.
	Method catalog.MergeMethod `json:"method,omitempty"`
}

// ReviewStep is Post the agent's review.
type ReviewStep struct {
	As catalog.ReviewAs `json:"as"`

	// AllowApprove lets a verdict approve; off, an approval posts as a comment.
	AllowApprove bool `json:"allowApprove,omitempty"`
}

// TellStep is Tell me.
type TellStep struct {
	// When is what it says; "" is catalog.TellReached.
	When catalog.TellWhen `json:"when,omitempty"`
}

// Limits are a routine's own limits. Each left out takes its default.
type Limits struct {
	// OncePer is the routine's own once-per; "" is its trigger's.
	OncePer catalog.OncePer `json:"oncePer,omitempty"`

	// TriesPerItem caps the runs of its agent and GitHub steps on one item.
	TriesPerItem *int `json:"triesPerItem,omitempty"`

	// RunsPerDay caps its runs a day.
	RunsPerDay *int `json:"runsPerDay,omitempty"`

	// WaitMinutes is the wait between tries on one item.
	WaitMinutes *int `json:"waitMinutes,omitempty"`

	// ActiveHours are when it may start; nil is always.
	ActiveHours *ActiveHours `json:"activeHours,omitempty"`

	// BudgetPerDay is its own daily budget; nil is none, beside the global cap.
	BudgetPerDay *Budget `json:"budgetPerDay,omitempty"`
}

// ActiveHours are the days and the local hours a routine may start in. From after To
// runs past midnight.
type ActiveHours struct {
	Days []catalog.Weekday `json:"days"`
	From string            `json:"from"`
	To   string            `json:"to"`
}

// Notices are which of a routine's notices show a banner (true) or only go to the
// Notifications page (false); nil follows Settings → Notifications'.
type Notices struct {
	Alert    *bool `json:"alert,omitempty"`
	NeedsYou *bool `json:"needsYou,omitempty"`
	Finished *bool `json:"finished,omitempty"`
	Checks   *bool `json:"checks,omitempty"`
	Problem  *bool `json:"problem,omitempty"`
}

// Kinds returns the kinds the step sets, in the catalog's order: exactly one for a
// valid step.
func (s Step) Kinds() []catalog.StepKind {
	set := []struct {
		kind catalog.StepKind
		on   bool
	}{
		{catalog.StepNotify, s.Notify != nil},
		{catalog.StepPin, s.Pin != nil},
		{catalog.StepSnooze, s.Snooze != nil},
		{catalog.StepDismiss, s.Dismiss != nil},
		{catalog.StepRestore, s.Restore != nil},
		{catalog.StepMarkDone, s.MarkDone != nil},
		{catalog.StepGitHub, s.GitHub != nil},
		{catalog.StepAgent, s.Agent != nil},
		{catalog.StepWaitForChecks, s.WaitForChecks != nil},
		{catalog.StepMerge, s.Merge != nil},
		{catalog.StepPostReview, s.PostReview != nil},
		{catalog.StepTellMe, s.TellMe != nil},
	}

	out := make([]catalog.StepKind, 0, 1)

	for _, k := range set {
		if k.on {
			out = append(out, k.kind)
		}
	}

	return out
}

// Kind returns the step's kind, or "" for a step that sets none or several.
func (s Step) Kind() catalog.StepKind {
	if kinds := s.Kinds(); len(kinds) == 1 {
		return kinds[0]
	}

	return ""
}

// Kinds returns the actions the action sets: exactly one for a valid action.
func (a GitHubAction) Kinds() []catalog.GitHubActionKind {
	set := []struct {
		kind catalog.GitHubActionKind
		on   bool
	}{
		{catalog.GitHubRebase, a.Rebase != nil},
		{catalog.GitHubUpdateBranch, a.UpdateBranch != nil},
		{catalog.GitHubReply, a.Reply != nil},
		{catalog.GitHubLabels, a.Labels != nil},
	}

	out := make([]catalog.GitHubActionKind, 0, 1)

	for _, k := range set {
		if k.on {
			out = append(out, k.kind)
		}
	}

	return out
}

// Kind returns the action's kind, or "" for one that sets none or several.
func (a GitHubAction) Kind() catalog.GitHubActionKind {
	if kinds := a.Kinds(); len(kinds) == 1 {
		return kinds[0]
	}

	return ""
}
