package spec

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
)

var (
	// toolName matches a program's name, as Requires lists it.
	toolName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)

	// starterKey matches a starter prompt's key.
	starterKey = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

	// agentID matches an agent's ID.
	agentID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

	// clockTime matches a time of day on a 24-hour clock.
	clockTime = regexp.MustCompile(`^(?:[01][0-9]|2[0-3]):[0-5][0-9]$`)
)

// maxStarterRunes caps a starter's key.
const maxStarterRunes = 64

// trigger checks the trigger and its options against the kinds the watch holds.
func (v *validator) trigger(t Trigger, kinds []item.Kind) {
	info, ok := catalog.TriggerByEvent(t.Event)
	if !ok {
		v.add(PartTrigger, "event", "%q isn't a trigger", t.Event)

		return
	}

	if !fits(kinds, info.Applies) {
		v.add(PartTrigger, "event", "%s fires only for pull requests, and this routine watches only %s",
			info.Label, kindWords(kinds))
	}

	v.options(t, info)
	v.optionValues(t)
}

// options refuses an option the trigger doesn't take, and asks for one it needs.
func (v *validator) options(t Trigger, info catalog.Trigger) {
	set := map[catalog.Option]bool{
		catalog.OptionWaitForChecks: t.WaitForChecks != nil, catalog.OptionSettleMinutes: t.SettleMinutes != nil,
		catalog.OptionFirstToo: t.FirstToo != nil, catalog.OptionOwnPushesToo: t.OwnPushesToo != nil,
		catalog.OptionCount: t.Count != nil, catalog.OptionDays: t.Days != nil,
		catalog.OptionLabel: t.Label != "", catalog.OptionSchedule: t.Schedule != nil,
	}

	for _, o := range []catalog.Option{
		catalog.OptionWaitForChecks, catalog.OptionSettleMinutes, catalog.OptionFirstToo, catalog.OptionOwnPushesToo,
		catalog.OptionCount, catalog.OptionDays, catalog.OptionLabel, catalog.OptionSchedule,
	} {
		if set[o] && !info.Takes(o) {
			v.add(PartTrigger, string(o), "%s doesn't take this option", info.Label)
		}
	}

	if info.Takes(catalog.OptionLabel) && t.Label == "" {
		v.add(PartTrigger, string(catalog.OptionLabel), "name the label it fires on")
	}

	if info.Takes(catalog.OptionSchedule) && t.Schedule == nil {
		v.add(PartTrigger, string(catalog.OptionSchedule), "say when it fires")
	}
}

// optionValues holds each option that is set to its range.
func (v *validator) optionValues(t Trigger) {
	v.inRange(PartTrigger, string(catalog.OptionSettleMinutes), t.SettleMinutes,
		catalog.MinSettleMinutes, catalog.MaxSettleMinutes)
	v.inRange(PartTrigger, string(catalog.OptionCount), t.Count, catalog.MinCount, catalog.MaxCount)
	v.inRange(PartTrigger, string(catalog.OptionDays), t.Days, catalog.MinQuietDays, catalog.MaxQuietDays)

	if t.Label != "" {
		v.label(PartTrigger, string(catalog.OptionLabel), t.Label)
	}

	if t.Schedule != nil {
		v.schedule(*t.Schedule)
	}
}

// schedule checks when a scheduled routine fires.
func (v *validator) schedule(s Schedule) {
	switch s.Every {
	case catalog.EveryDay, catalog.EveryWeekday:
		if s.On != "" {
			v.add(PartTrigger, "schedule.on", "only a weekly schedule takes a day")
		}
	case catalog.EveryWeek:
		if !slices.Contains(catalog.Weekdays(), s.On) {
			v.add(PartTrigger, "schedule.on", "pick the day of the week it fires")
		}
	default:
		v.add(PartTrigger, "schedule.every", `pick "day", "weekday", or "week"`)
	}

	if !clockTime.MatchString(s.At) {
		v.add(PartTrigger, "schedule.at", `write the time as "HH:MM", such as "08:00"`)
	}
}

// inRange holds a number that is set to min..max.
func (v *validator) inRange(part Part, field string, n *int, low, high int) {
	if n != nil && (*n < low || *n > high) {
		v.add(part, field, "pick %d to %d", low, high)
	}
}

// label checks a label's name.
func (v *validator) label(part Part, field, l string) {
	if msg := checkText(l, catalog.MaxLabelRunes); msg != "" {
		v.add(part, field, "%s", msg)
	}
}

// limits checks the routine's limits.
func (v *validator) limits(l Limits) {
	switch l.OncePer {
	case "", catalog.OncePerItem, catalog.OncePerHeadCommit, catalog.OncePerDay:
	default:
		v.add(PartLimits, "oncePer", `pick "item", "headCommit", or "day"`)
	}

	v.inRange(PartLimits, "triesPerItem", l.TriesPerItem, catalog.MinTries, catalog.MaxTries)
	v.inRange(PartLimits, "runsPerDay", l.RunsPerDay, catalog.MinRunsPerDay, catalog.MaxRunsPerDay)
	v.inRange(PartLimits, "waitMinutes", l.WaitMinutes, catalog.MinWaitMinutes, catalog.MaxWaitMinutes)

	if l.ActiveHours != nil {
		v.activeHours(*l.ActiveHours)
	}

	if l.BudgetPerDay != nil {
		v.budget(PartLimits, "budgetPerDay", *l.BudgetPerDay, budgetRange{
			minCents: catalog.MinDayCents, maxCents: catalog.MaxDayCents,
			minTokens: catalog.MinDayTokens, maxTokens: catalog.MaxDayTokens,
		})
	}
}

// activeHours checks the days and hours a routine may start in.
func (v *validator) activeHours(h ActiveHours) {
	if len(h.Days) == 0 {
		v.add(PartLimits, "activeHours.days", "pick at least one day")
	}

	for i, d := range h.Days {
		if !slices.Contains(catalog.Weekdays(), d) {
			v.add(PartLimits, fmt.Sprintf("activeHours.days[%d]", i), "%q isn't a day of the week", d)
		}
	}

	for _, t := range []struct{ field, value string }{{"activeHours.from", h.From}, {"activeHours.to", h.To}} {
		if !clockTime.MatchString(t.value) {
			v.add(PartLimits, t.field, `write the time as "HH:MM", such as "08:00"`)
		}
	}

	if h.From == h.To {
		v.add(PartLimits, "activeHours.to", "end the hours at another time than they start")
	}
}

// budgetRange is a budget's bounds in cents and in tokens.
type budgetRange struct {
	minCents, maxCents   int
	minTokens, maxTokens int64
}

// budget checks a budget: one unit, within its bounds, dollars to the cent.
func (v *validator) budget(part Part, field string, b Budget, r budgetRange) {
	switch {
	case (b.USD == nil) == (b.Tokens == nil):
		v.add(part, field, `set it in dollars ("usd") or in tokens ("tokens"), one of them`)
	case b.USD != nil:
		cents := *b.USD * 100
		if math.Abs(cents-math.Round(cents)) > 1e-6 || cents < float64(r.minCents) || cents > float64(r.maxCents) {
			v.add(part, field+".usd", "pick $%s to $%s, to the cent", dollars(r.minCents), dollars(r.maxCents))
		}
	default:
		if *b.Tokens < r.minTokens || *b.Tokens > r.maxTokens {
			v.add(part, field+".tokens", "pick %d to %d tokens", r.minTokens, r.maxTokens)
		}
	}
}

// dollars writes cents as dollars: "0.50", "50".
func dollars(cents int) string {
	if cents%100 == 0 {
		return strconv.Itoa(cents / 100)
	}

	return fmt.Sprintf("%d.%02d", cents/100, cents%100)
}

// prompt checks a prompt's name. A reply takes no item default, since it is no prompt.
func (v *validator) prompt(field string, p PromptRef, reply bool) {
	switch {
	case promptNames(p) != 1:
		v.add(PartSteps, field, "name a prompt: a starter, a nickname, or the item's own default")
	case p.ItemDefault && reply:
		v.add(PartSteps, field, "name the reply by its nickname or its starter")
	case p.Starter != "" && (!starterKey.MatchString(p.Starter) || len(p.Starter) > maxStarterRunes):
		v.add(PartSteps, field+".starter", "%q isn't a starter's key", p.Starter)
	case p.Nickname != "":
		if msg := checkText(p.Nickname, MaxNicknameRunes); msg != "" {
			v.add(PartSteps, field+".nickname", "%s", msg)
		}
	}
}

// promptNames counts the ways a prompt is named: exactly one for a valid one.
func promptNames(p PromptRef) int {
	set := 0

	for _, on := range []bool{p.Starter != "", p.Nickname != "", p.ItemDefault} {
		if on {
			set++
		}
	}

	return set
}

// target checks a launch row's target: an org or user, or a repository.
func validTarget(t string) bool {
	if strings.Contains(t, "/") {
		_, err := repo.Parse(t)

		return err == nil
	}

	return repo.ValidOwner(t)
}

// name checks a profile's, a model's, or an effort's name: "" to inherit.
func (v *validator) name(field, s string) {
	if s == "" {
		return
	}

	if msg := checkText(s, MaxTextRunes); msg != "" {
		v.add(PartSteps, field, "%s", msg)
	}
}

// agentName checks an agent's ID.
func (v *validator) agentName(field, a string, inherit bool) {
	if (inherit && a == catalog.Inherit) || (a != catalog.Inherit && agentID.MatchString(a)) {
		return
	}

	v.add(PartSteps, field, "%q isn't an agent", a)
}
