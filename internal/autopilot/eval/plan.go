package eval

import (
	"fmt"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
)

// Plan is a routine's spec made ready to decide on many items: what a decision works out
// from the spec alone (the order each list of conditions runs in, each condition's path
// and phrase, and the trigger's words) worked out once, not once per item. Its decisions
// are byte for byte those Evaluate makes, traces included. A plan never changes once
// prepared, so evaluations may share it at once.
type Plan struct {
	spec       spec.Spec
	trigger    triggerPlan
	conditions listPlan
}

// triggerPlan is a trigger as it is decided: its catalog entry, its test, whether this
// build reads it, and the words its trace says it with.
type triggerPlan struct {
	trigger   spec.Trigger
	info      catalog.Trigger
	test      triggerTest
	known     bool
	condition string
}

// listPlan is a list of conditions as it runs: how its results combine, the result that
// stops it, and its conditions in the order they run.
type listPlan struct {
	combine  func(...Result) Result
	decisive Result
	clauses  []clausePlan
}

// clausePlan is one condition, with where it is in the spec and how the routine's
// sentence says it, or one group.
type clausePlan struct {
	clause spec.Clause
	path   string
	phrase string
	group  *listPlan
}

// Prepare works out what deciding s takes from s alone. s is a spec as Evaluate takes it,
// and stays as it is while the plan is used.
func Prepare(s spec.Spec) *Plan {
	return &Plan{spec: s, trigger: prepareTrigger(s.Trigger), conditions: prepareList(s.Conditions, "conditions")}
}

// prepareTrigger looks a trigger up once: an event this build doesn't read keeps the
// catalog's empty entry, whose words a click's run still says.
func prepareTrigger(t spec.Trigger) triggerPlan {
	info, ok := catalog.TriggerByEvent(t.Event)
	test, known := triggerTests[t.Event]

	return triggerPlan{trigger: t, info: info, test: test, known: ok && known, condition: info.Label + ": " + info.Help}
}

// prepareList orders a list of conditions as it runs, cheapest first (byCost), each with
// its path: "conditions.of[2]", "conditions.of[1].group.of[0]".
func prepareList(c spec.Conditions, path string) listPlan {
	out := listPlan{combine: All, decisive: No}
	if c.Match == spec.MatchAny {
		out.combine, out.decisive = Any, Yes
	}

	if len(c.Of) == 0 {
		return out
	}

	out.clauses = make([]clausePlan, 0, len(c.Of))
	for _, i := range byCost(c.Of) {
		out.clauses = append(out.clauses, prepareClause(c.Of[i], fmt.Sprintf("%s.of[%d]", path, i)))
	}

	return out
}

// prepareClause prepares one condition, phrased, or one group.
func prepareClause(c spec.Clause, path string) clausePlan {
	if c.Group != nil {
		g := prepareList(*c.Group, path+".group")

		return clausePlan{clause: c, path: path, group: &g}
	}

	return clausePlan{clause: c, path: path, phrase: spec.Phrase(c)}
}

// Evaluate decides whether the plan's spec matches the item f describes at now, with what
// the routine's ledger knows of it, as Evaluate does.
func (p *Plan) Evaluate(f facts.Facts, h History, now time.Time) Decision {
	trig, key, trace := p.trigger.decide(f, h, now)

	d := Decision{Match: trig, Trace: []Trace{trace}, Needs: []facts.Need{}}

	if trig == Yes {
		d.FireKey = fireKey(p.spec, key, f, h, now)
	}

	if trig == No {
		return d
	}

	cond := p.conditions.decide(f, h, now)
	d.Trace = append(d.Trace, cond.traces...)
	d.Match = All(trig, cond.result)

	if d.Match == Unknown {
		var missing []facts.Need
		if trig == Unknown {
			missing = append(missing, trace.Need)
		}

		if cond.result == Unknown {
			missing = append(missing, cond.needs...)
		}

		d.Needs = ordered(missing)
	}

	return d
}

// decide decides whether the trigger holds for the item, and the occurrence's fire key.
// A click's run holds whatever the trigger, once per click.
func (t *triggerPlan) decide(f facts.Facts, h History, now time.Time) (Result, string, Trace) {
	tr := Trace{Path: "trigger", Condition: t.condition}

	var out fired

	switch {
	case h.Manual != "":
		out = yes("manual:"+h.Manual, "you ran it")
	case !t.known:
		tr.Condition = string(t.trigger.Event)
		out = fired{result: Unknown, actual: "it isn't a trigger this build reads"}
	case !t.info.Applies(f.Item.Kind):
		out = no("it's an issue, and " + t.info.Label + " fires only for pull requests")
	default:
		out = t.test(t.trigger, f, h, now)
	}

	tr.Result, tr.Actual, tr.Need = out.result, out.actual, out.need

	return out.result, out.key, tr
}

// decide decides a list of conditions in the order it runs, stopping at the first
// decisive one: a no for "all", a yes for "any". A list with no conditions is yes.
func (l *listPlan) decide(f facts.Facts, h History, now time.Time) listResult {
	out := listResult{result: Yes}
	if len(l.clauses) == 0 {
		return out
	}

	results := make([]Result, 0, len(l.clauses))

	for i := range l.clauses {
		r := l.clauses[i].decide(f, h, now)

		out.traces = append(out.traces, r.traces...)
		results = append(results, r.result)
		out.needs = append(out.needs, r.needs...)

		if r.result == l.decisive {
			break
		}
	}

	out.result = l.combine(results...)
	if out.result != Unknown {
		out.needs = nil
	}

	return out
}

// decide decides one condition, or one group.
func (c *clausePlan) decide(f facts.Facts, h History, now time.Time) listResult {
	if c.group != nil {
		return c.group.decide(f, h, now)
	}

	r, tr := condition(c.clause, c.phrase, f, h, now)
	tr.Path = c.path

	return listResult{result: r, traces: []Trace{tr}, needs: []facts.Need{tr.Need}}
}
