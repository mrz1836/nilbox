package eval_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/eval"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/item"
)

// gen draws a test's choices from a fuzzer's bytes: the same bytes always draw the same
// choices, and once they run out every draw is the first choice.
type gen struct {
	data []byte
	at   int
}

// n draws a number from 0 to k-1.
func (g *gen) n(k int) int {
	if k <= 1 || g.at >= len(g.data) {
		return 0
	}

	b := g.data[g.at]
	g.at++

	return int(b) % k
}

// pick draws one of values.
func pick[T any](g *gen, values []T) T {
	return values[g.n(len(values))]
}

// some draws one to three of values, without repeats.
func some(g *gen, values []string) []string {
	out := []string{}

	for range 1 + g.n(3) {
		if v := pick(g, values); !slices.Contains(out, v) {
			out = append(out, v)
		}
	}

	return out
}

// The pools values are drawn from: texts that match one another in some ways and not
// in others, as conditions and facts.
func textPool() []string {
	return []string{"bump", "Bump golang.org/x/net", "x/net", "b*t", "^b.*T$", "lint"}
}

func listPool() []string {
	return []string{"go.mod", "*.md", "docs/readme.md", "CI / lint", "lint", "docs/"}
}
func numberPool() []string { return []string{"0", "3", "5", "9"} }
func lengthPool() []string { return []string{"30m", "2h", "3d", "10d"} }

// genFields are the fields generated conditions read: every field but the routine's
// history, which no fact holds.
func genFields() []catalog.Field {
	return slices.DeleteFunc(catalog.Fields(), func(f catalog.Field) bool { return f.Group == catalog.GroupHistory })
}

// genClause draws a condition the catalog allows.
func genClause(g *gen) spec.Clause {
	field := pick(g, genFields())
	op, _ := catalog.OpByID(pick(g, catalog.OpsFor(field)))
	c := spec.Clause{Field: field.ID, Op: op.ID}

	switch op.Arity {
	case catalog.ArityOne:
		c.Value = genOne(g, field.Type)
	case catalog.ArityMany:
		c.Values = some(g, choicesOr(field, listPool()))
	case catalog.ArityNone:
	}

	return c
}

// genOne draws an operator's one value for a field of type t.
func genOne(g *gen, t catalog.Type) string {
	switch t {
	case catalog.TypeNumber:
		return pick(g, numberPool())
	case catalog.TypeDuration:
		return pick(g, lengthPool())
	case catalog.TypeText, catalog.TypeList, catalog.TypeBool, catalog.TypeChoice:
	}

	return pick(g, textPool())
}

// choicesOr returns a field's choices' values, or pool for a field with none.
func choicesOr(f catalog.Field, pool []string) []string {
	if len(f.Choices) == 0 {
		return pool
	}

	out := make([]string, 0, len(f.Choices))
	for _, c := range f.Choices {
		out = append(out, c.Value)
	}

	return out
}

// genValue draws a field's value: not known yet, not applying, or known.
func genValue(g *gen, f catalog.Field) facts.Value {
	switch g.n(4) {
	case 0:
		return facts.Unknown("not read yet", pick(g, []facts.Need{facts.NeedDetails, facts.NeedChecks, facts.NeedFiles}))
	case 1:
		return facts.NotApplicable("it doesn't apply")
	default:
		return known(g, f, g.n(2) == 0)
	}
}

// known draws a known value of a field's type; a list may be partial.
func known(g *gen, f catalog.Field, partial bool) facts.Value {
	switch f.Type {
	case catalog.TypeList:
		values := some(g, choicesOr(f, listPool()))
		if g.n(3) == 0 {
			values = []string{}
		}

		return facts.List(values, partial)
	case catalog.TypeNumber:
		return facts.Number(g.n(10))
	case catalog.TypeBool:
		return facts.Bool(g.n(2) == 0)
	case catalog.TypeChoice:
		return facts.Text(pick(g, choicesOr(f, nil)))
	case catalog.TypeDuration:
		return facts.Since(now.Add(-pick(g, []time.Duration{10 * time.Minute, 3 * time.Hour, 5 * 24 * time.Hour})))
	case catalog.TypeText:
	}

	return facts.Text(pick(g, textPool()))
}

// genCase draws a routine that always fires, with conditions and one group, and facts
// for every field its conditions read.
func genCase(data []byte) (spec.Spec, facts.Facts) {
	g := &gen{data: data}
	c := spec.Conditions{Match: pick(g, []spec.Match{spec.MatchAll, spec.MatchAny})}

	for range 1 + g.n(5) {
		c.Of = append(c.Of, genClause(g))
	}

	if g.n(2) == 0 {
		group := spec.Conditions{Match: pick(g, []spec.Match{spec.MatchAll, spec.MatchAny})}
		for range 1 + g.n(3) {
			group.Of = append(group.Of, genClause(g))
		}

		c.Of = append(c.Of, spec.Clause{Group: &group})
	}

	f := facts.Facts{Item: facts.Item{ID: 1, Kind: item.KindPR}, Values: map[catalog.FieldID]facts.Value{}, Updated: now}

	for _, field := range genFields() {
		f.Values[field.ID] = genValue(g, field)
	}

	return routine(spec.Trigger{Event: catalog.EventAppears}, c), f
}

// seeds are the corpus every property starts from.
func seeds() [][]byte {
	return [][]byte{
		{},
		{0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
		{255, 254, 253, 252, 251, 250, 249},
		[]byte("routines decide alike however their conditions sit"),
		{1, 3, 0, 0, 2, 7, 9, 0, 1, 1, 4, 4, 2, 0, 0, 3, 6, 5, 8, 1, 2, 3},
	}
}

// traceSet writes the decided steps of a trace without where each sat, sorted.
func traceSet(d eval.Decision) []string {
	out := make([]string, 0, len(d.Trace))
	for _, tr := range d.Trace {
		out = append(out, tr.Condition+"|"+string(tr.Result)+"|"+tr.Actual+"|"+string(tr.Need))
	}

	slices.Sort(out)

	return out
}

// shuffle reorders clauses as g draws.
func shuffle(g *gen, clauses []spec.Clause) []spec.Clause {
	out := slices.Clone(clauses)
	for i := len(out) - 1; i > 0; i-- {
		j := g.n(i + 1)
		out[i], out[j] = out[j], out[i]
	}

	return out
}

// FuzzShuffledConditionsDecideAlike checks that the order of a routine's conditions, and
// of a group's, never changes its decision: the result, the fire key, what it needs,
// and which conditions it checked with what outcome.
func FuzzShuffledConditionsDecideAlike(f *testing.F) {
	for _, seed := range seeds() {
		f.Add(seed, []byte{3, 1, 4, 1, 5, 9, 2, 6})
	}

	f.Fuzz(func(t *testing.T, data, order []byte) {
		s, fs := genCase(data)
		before := eval.Evaluate(s, fs, eval.History{}, now)

		g := &gen{data: order}
		shuffled := s
		shuffled.Conditions.Of = shuffle(g, s.Conditions.Of)

		for i, cl := range shuffled.Conditions.Of {
			if cl.Group != nil {
				group := *cl.Group
				group.Of = shuffle(g, group.Of)
				shuffled.Conditions.Of[i].Group = &group
			}
		}

		after := eval.Evaluate(shuffled, fs, eval.History{}, now)

		assert.Equal(t, before.Match, after.Match)
		assert.Equal(t, before.FireKey, after.FireKey)
		assert.Equal(t, before.Needs, after.Needs)
		assert.Equal(t, traceSet(before), traceSet(after))
	})
}

// FuzzNegationOfAnUnknownIsUnknown checks that a condition and its negation say the
// opposite about a known value, and both say unknown about one that isn't known: "a
// failed check's name doesn't contain X" is never yes before the names are read.
func FuzzNegationOfAnUnknownIsUnknown(f *testing.F) {
	for _, seed := range seeds() {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		g := &gen{data: data}
		c := genClause(g)

		op, _ := catalog.OpByID(c.Op)
		if op.Negation == "" {
			return
		}

		field, _ := catalog.FieldByID(c.Field)
		v := genValue(g, field)
		fs := itemFacts(item.KindPR, c.Field, v)

		neg := c
		neg.Op = op.Negation

		r, rn := decide(c, fs, eval.History{}), decide(neg, fs, eval.History{})

		switch v.Status {
		case facts.StatusUnknown:
			assert.Equal(t, eval.Unknown, r)
			assert.Equal(t, eval.Unknown, rn)
		case facts.StatusNotApplicable:
			assert.Equal(t, eval.No, r, "a field that doesn't apply is no, whatever the operator")
			assert.Equal(t, eval.No, rn)
		case facts.StatusKnown:
			assert.Equal(t, r.Not(), rn, "%s %s against %+v", c.Field, c.Op, v)
		}
	})
}

// complete makes every value that isn't known known, and every partial list whole with
// more in it, as reading what was missing would.
func complete(g *gen, fs facts.Facts) facts.Facts {
	out := fs
	out.Values = map[catalog.FieldID]facts.Value{}

	for _, field := range genFields() {
		v := fs.Value(field.ID)

		switch {
		case v.Status == facts.StatusUnknown:
			v = known(g, field, false)
		case v.Status == facts.StatusKnown && v.Partial:
			more := known(g, field, false)
			v = facts.List(append(slices.Clone(v.List), more.List...), false)
		}

		out.Values[field.ID] = v
	}

	return out
}

// FuzzNeverYesOnAnUnknownItNeeds checks that a decision is never yes (nor no) while a
// condition it needs is unknown: reading what is missing, whatever it turns out to be,
// never turns a yes into anything else, nor a no.
func FuzzNeverYesOnAnUnknownItNeeds(f *testing.F) {
	for _, seed := range seeds() {
		f.Add(seed, []byte{9, 8, 7, 6, 5, 4, 3, 2, 1, 0})
	}

	f.Fuzz(func(t *testing.T, data, fill []byte) {
		s, fs := genCase(data)
		before := eval.Evaluate(s, fs, eval.History{}, now)

		if before.Match == eval.Unknown {
			require.NotEmpty(t, unknownSteps(before), "an unknown decision has an unknown step behind it")

			return
		}

		after := eval.Evaluate(s, complete(&gen{data: fill}, fs), eval.History{}, now)
		assert.Equal(t, before.Match, after.Match, "a definite decision stands whatever the missing data says")
	})
}

// unknownSteps returns the conditions of a decision that came to unknown.
func unknownSteps(d eval.Decision) []string {
	var out []string

	for _, tr := range d.Trace {
		if tr.Result == eval.Unknown {
			out = append(out, strings.TrimSpace(tr.Condition))
		}
	}

	return out
}
