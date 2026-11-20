package catalog

import "github.com/mrz1836/nilbox/internal/item"

// Event is what makes a routine fire: a state Autopilot checks on each refresh, since
// it polls rather than hears webhooks, with a fire key that makes it act once per
// occurrence.
type Event string

// The triggers.
const (
	EventAppears          Event = "appears"
	EventNewCommits       Event = "new_commits"
	EventChecksFailed     Event = "checks_failed"
	EventChecksPassed     Event = "checks_passed"
	EventChangesRequested Event = "changes_requested"
	EventApproved         Event = "approved"
	EventMergeConflicts   Event = "merge_conflicts"
	EventFallsBehind      Event = "falls_behind"
	EventLabeled          Event = "labeled"
	EventReadyForReview   Event = "ready_for_review"
	EventReviewRequested  Event = "review_requested"
	EventAssigned         Event = "assigned"
	EventNewComments      Event = "new_comments"
	EventGoesQuiet        Event = "goes_quiet"
	EventSchedule         Event = "schedule"
	EventManual           Event = "manual"
)

// Option names one of a trigger's options, as the spec's trigger object spells it.
type Option string

// The triggers' options.
const (
	// OptionWaitForChecks waits until every check on the head commit has finished and
	// settled before Checks fail fires. On by default.
	OptionWaitForChecks Option = "waitForChecks"

	// OptionSettleMinutes is how long after the last check finished the checks count as
	// settled, so a workflow that starts only after another finishes isn't missed.
	OptionSettleMinutes Option = "settleMinutes"

	// OptionFirstToo makes New commits fire on the first head commit the routine sees too.
	// On by default.
	OptionFirstToo Option = "firstToo"

	// OptionOwnPushesToo makes New commits fire on a commit a routine's own run pushed,
	// which it ignores by default so a routine never chases itself.
	OptionOwnPushesToo Option = "ownPushesToo"

	// OptionCount is how many new comments make New comments fire.
	OptionCount Option = "count"

	// OptionDays is how many quiet days make Goes quiet fire.
	OptionDays Option = "days"

	// OptionLabel is the label Labeled fires on.
	OptionLabel Option = "label"

	// OptionSchedule is when On a schedule fires.
	OptionSchedule Option = "schedule"
)

// Range is a whole-number setting's bounds and its default, in Unit.
type Range struct {
	Min     int    `json:"min"`
	Max     int    `json:"max"`
	Default int    `json:"default"`
	Unit    string `json:"unit"`
}

// Holds reports whether n is within the range.
func (r Range) Holds(n int) bool {
	return n >= r.Min && n <= r.Max
}

// The options' ranges.
const (
	MinSettleMinutes, MaxSettleMinutes, DefaultSettleMinutes = 0, 60, 2
	MinCount, MaxCount, DefaultCount                         = 1, 100, 1
	MinQuietDays, MaxQuietDays, DefaultQuietDays             = 1, 365, 7

	// MaxLabelRunes is the longest label GitHub allows.
	MaxLabelRunes = 50
)

// OptionInfo describes one of a trigger's options for the builder.
type OptionInfo struct {
	ID    Option `json:"id"`
	Label string `json:"label"`

	// Default is a yes-or-no option's default.
	Default bool `json:"default"`

	// Range is a number option's bounds and default, nil for any other.
	Range *Range `json:"range"`
}

// Trigger describes one trigger for the builder.
type Trigger struct {
	Event Event `json:"event"`

	// Label is the builder's name for it: "Checks fail".
	Label string `json:"label"`

	// Help says when it fires: "has a failed or errored check on its head commit".
	Help string `json:"help"`

	// Kinds are the kinds of item it fires for.
	Kinds []item.Kind `json:"kinds"`

	// OncePer says what one occurrence is, so it fires once per it: "head commit".
	OncePer string `json:"oncePer"`

	// Options are the options it takes.
	Options []OptionInfo `json:"options"`
}

// Applies reports whether the trigger fires for an item of kind.
func (t Trigger) Applies(kind item.Kind) bool {
	for _, k := range t.Kinds {
		if k == kind {
			return true
		}
	}

	return false
}

// Takes reports whether the trigger takes an option.
func (t Trigger) Takes(o Option) bool {
	for _, info := range t.Options {
		if info.ID == o {
			return true
		}
	}

	return false
}

// Triggers returns every trigger, in the builder's order.
func Triggers() []Trigger {
	return []Trigger{
		{
			Event: EventAppears, Label: "Appears", Help: "is in the watch", Kinds: both(), OncePer: "item",
		},
		{
			Event: EventNewCommits, Label: "New commits", Kinds: prOnly(), OncePer: "head commit",
			Help: "has a head commit this routine hasn't seen", Options: []OptionInfo{
				{ID: OptionFirstToo, Label: "Also when it first appears", Default: true},
				{ID: OptionOwnPushesToo, Label: "Also on commits a routine pushed"},
			},
		},
		{
			Event: EventChecksFailed, Label: "Checks fail", Kinds: prOnly(), OncePer: "head commit",
			Help: "has a failed or errored check on its head commit", Options: []OptionInfo{
				{ID: OptionWaitForChecks, Label: "Wait until every check has finished", Default: true},
				settleOption(),
			},
		},
		{
			Event: EventChecksPassed, Label: "Checks pass", Kinds: prOnly(), OncePer: "head commit",
			Help: "has every check passing, and settled, on its head commit", Options: []OptionInfo{settleOption()},
		},
		{
			Event: EventChangesRequested, Label: "Changes requested", Kinds: prOnly(), OncePer: "review round",
			Help: "gets a new request for changes, or its review decision turns to one",
		},
		{
			Event: EventApproved, Label: "Approved", Kinds: prOnly(), OncePer: "head commit",
			Help: "is approved: the decision, or a new approval",
		},
		{
			Event: EventMergeConflicts, Label: "Merge conflicts", Kinds: prOnly(), OncePer: "head commit",
			Help: "conflicts with its base branch",
		},
		{
			Event: EventFallsBehind, Label: "Falls behind", Kinds: prOnly(), OncePer: "head commit and base",
			Help: "is behind its base branch, with no conflict",
		},
		{
			Event: EventLabeled, Label: "Labeled", Kinds: both(), OncePer: "item and label", Help: "carries a given label",
			Options: []OptionInfo{{ID: OptionLabel, Label: "Label"}},
		},
		{
			Event: EventReadyForReview, Label: "Ready for review", Kinds: prOnly(), OncePer: "item",
			Help: "isn't a draft, whether it opened ready or turned ready",
		},
		{
			Event: EventReviewRequested, Label: "Review requested", Kinds: prOnly(), OncePer: "request",
			Help: "newly asks for your review",
		},
		{
			Event: EventAssigned, Label: "Assigned to me", Kinds: both(), OncePer: "assignment",
			Help: "is newly assigned to you",
		},
		{
			Event: EventNewComments, Label: "New comments", Kinds: both(), OncePer: "comment count",
			Help: "gains new comments since the routine last fired, or since it first saw the item",
			Options: []OptionInfo{{
				ID: OptionCount, Label: "Comments",
				Range: &Range{Min: MinCount, Max: MaxCount, Default: DefaultCount, Unit: "comments"},
			}},
		},
		{
			Event: EventGoesQuiet, Label: "Goes quiet", Kinds: both(), OncePer: "last activity",
			Help: "has had no activity for a number of days", Options: []OptionInfo{{
				ID: OptionDays, Label: "Days",
				Range: &Range{Min: MinQuietDays, Max: MaxQuietDays, Default: DefaultQuietDays, Unit: "days"},
			}},
		},
		{
			Event: EventSchedule, Label: "On a schedule", Kinds: both(), OncePer: "scheduled time",
			Help: "holds the conditions at a set time", Options: []OptionInfo{{ID: OptionSchedule, Label: "When"}},
		},
		{
			Event: EventManual, Label: "Only when I run it", Kinds: both(), OncePer: "each click",
			Help: "is picked from an item's menu by you",
		},
	}
}

// settleOption is the checks triggers' settle time.
func settleOption() OptionInfo {
	return OptionInfo{
		ID: OptionSettleMinutes, Label: "Settle for",
		Range: &Range{Min: MinSettleMinutes, Max: MaxSettleMinutes, Default: DefaultSettleMinutes, Unit: "minutes"},
	}
}

// Every is how often a schedule fires.
type Every string

// The schedules.
const (
	EveryDay     Every = "day"
	EveryWeekday Every = "weekday"
	EveryWeek    Every = "week"
)

// EveryInfo describes how often a schedule fires, for the builder.
type EveryInfo struct {
	ID    Every  `json:"id"`
	Label string `json:"label"`
	Help  string `json:"help"`
}

// Everies returns how often a schedule can fire, in the builder's order.
func Everies() []EveryInfo {
	return []EveryInfo{
		{ID: EveryDay, Label: "Every day", Help: "Fires every day, at its time."},
		{ID: EveryWeekday, Label: "Every weekday", Help: "Fires Monday to Friday, at its time."},
		{ID: EveryWeek, Label: "Every week", Help: "Fires once a week, on its day, at its time."},
	}
}

// Weekday names a day of the week as the spec spells it.
type Weekday string

// The days of the week, from Monday.
const (
	Monday    Weekday = "mon"
	Tuesday   Weekday = "tue"
	Wednesday Weekday = "wed"
	Thursday  Weekday = "thu"
	Friday    Weekday = "fri"
	Saturday  Weekday = "sat"
	Sunday    Weekday = "sun"
)

// Weekdays returns the days of the week, from Monday.
func Weekdays() []Weekday {
	return []Weekday{Monday, Tuesday, Wednesday, Thursday, Friday, Saturday, Sunday}
}
