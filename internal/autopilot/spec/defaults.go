package spec

import (
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/item"
)

// or returns *p, or def when p is nil.
func or[T any](p *T, def T) T {
	if p == nil {
		return def
	}

	return *p
}

// WaitsForChecks reports whether the trigger waits until every check has finished and
// settled: Checks fail unless turned off, and Checks pass always, since every check
// passing means every one finished.
func (t Trigger) WaitsForChecks() bool {
	if t.Event == catalog.EventChecksFailed {
		return or(t.WaitForChecks, true)
	}

	return t.Event == catalog.EventChecksPassed
}

// Settle is how long after the last check finished the trigger counts them as settled.
func (t Trigger) Settle() time.Duration {
	return time.Duration(or(t.SettleMinutes, catalog.DefaultSettleMinutes)) * time.Minute
}

// FirstCommitToo reports whether New commits fires on the first head commit it sees.
func (t Trigger) FirstCommitToo() bool {
	return or(t.FirstToo, true)
}

// OwnPushes reports whether New commits fires on a commit a routine's own run pushed.
func (t Trigger) OwnPushes() bool {
	return or(t.OwnPushesToo, false)
}

// CommentCount is how many new comments make New comments fire.
func (t Trigger) CommentCount() int {
	return or(t.Count, catalog.DefaultCount)
}

// QuietDays is how many quiet days make Goes quiet fire.
func (t Trigger) QuietDays() int {
	return or(t.Days, catalog.DefaultQuietDays)
}

// Place is where the agent step runs.
func (a AgentStep) Place() catalog.Where {
	if a.Where == "" {
		return catalog.DefaultWhere
	}

	return a.Where
}

// Tools is the agent step's tools preset.
func (a AgentStep) Tools() catalog.Preset {
	if a.Fence == nil || a.Fence.Tools == "" {
		return catalog.DefaultPreset
	}

	return a.Fence.Tools
}

// MayPush reports an agent step that can push: one in the background whose preset lets
// it.
func (a AgentStep) MayPush() bool {
	p, ok := catalog.PresetByID(a.Tools())

	return ok && p.MayPush && a.Place() == catalog.WhereBackground
}

// ReadOnly reports an agent step that changes nothing and whose review the app can read:
// one in the background under the read-only preset.
func (a AgentStep) ReadOnly() bool {
	p, ok := catalog.PresetByID(a.Tools())

	return ok && p.ReadOnly && a.Place() == catalog.WhereBackground
}

// AgentID is the step's agent, catalog.Inherit for the launch defaults'.
func (a AgentStep) AgentID() string {
	if a.Agent == "" {
		return catalog.Inherit
	}

	return a.Agent
}

// SnoozeDays is how long a Snooze step snoozes.
func (s SnoozeStep) SnoozeDays() int {
	return or(s.Days, catalog.DefaultSnoozeDays)
}

// Tries caps the runs of the routine's agent and GitHub steps on one item.
func (l Limits) Tries() int {
	return or(l.TriesPerItem, catalog.DefaultTries)
}

// Runs caps the routine's runs a day.
func (l Limits) Runs() int {
	return or(l.RunsPerDay, catalog.DefaultRunsPerDay)
}

// Wait is the wait between tries on one item.
func (l Limits) Wait() time.Duration {
	return time.Duration(or(l.WaitMinutes, catalog.DefaultWaitMinutes)) * time.Minute
}

// Kinds returns the kinds of item the watch can hold, in item.Kinds' order: every
// kind for a watch with no source, which only a manual routine has.
func (w Watch) Kinds() []item.Kind {
	if len(w.Sources) == 0 {
		return item.Kinds()
	}

	seen := map[item.Kind]bool{}

	for _, src := range w.Sources {
		for _, k := range src.Kinds() {
			seen[k] = true
		}
	}

	return inKindOrder(seen)
}

// Kinds returns the kinds of item the source can hold, in item.Kinds' order.
func (s Source) Kinds() []item.Kind {
	if s.Search != nil {
		if s.Search.Kind.Valid() {
			return []item.Kind{s.Search.Kind}
		}

		return item.Kinds()
	}

	seen := map[item.Kind]bool{}

	for _, l := range s.Lists {
		info, _ := catalog.ListSourceByID(l)
		for _, k := range info.Kinds {
			seen[k] = true
		}
	}

	return inKindOrder(seen)
}

// inKindOrder returns the kinds seen holds, in item.Kinds' order.
func inKindOrder(seen map[item.Kind]bool) []item.Kind {
	out := make([]item.Kind, 0, len(seen))

	for _, k := range item.Kinds() {
		if seen[k] {
			out = append(out, k)
		}
	}

	return out
}
