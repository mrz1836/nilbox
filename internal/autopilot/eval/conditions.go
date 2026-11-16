package eval

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
)

// listResult is what a list of conditions came to.
type listResult struct {
	result Result
	traces []Trace

	// needs are what an unknown result needs.
	needs []facts.Need
}

// evalList decides a list of conditions, cheapest first, stopping at the first decisive
// one: a no for "all", a yes for "any". A list with no conditions is yes.
func evalList(c spec.Conditions, path string, f facts.Facts, h History, now time.Time) listResult {
	out := listResult{result: Yes}
	if len(c.Of) == 0 {
		return out
	}

	combine, decisive := All, No
	if c.Match == spec.MatchAny {
		combine, decisive = Any, Yes
	}

	results := make([]Result, 0, len(c.Of))

	for _, i := range byCost(c.Of) {
		r := evalClause(c.Of[i], fmt.Sprintf("%s.of[%d]", path, i), f, h, now)

		out.traces = append(out.traces, r.traces...)
		results = append(results, r.result)
		out.needs = append(out.needs, r.needs...)

		if r.result == decisive {
			break
		}
	}

	out.result = combine(results...)
	if out.result != Unknown {
		out.needs = nil
	}

	return out
}

// evalClause decides one condition, or one group.
func evalClause(c spec.Clause, path string, f facts.Facts, h History, now time.Time) listResult {
	if c.Group != nil {
		return evalList(*c.Group, path+".group", f, h, now)
	}

	r, tr := Condition(c, f, h, now)
	tr.Path = path

	return listResult{result: r, traces: []Trace{tr}, needs: []facts.Need{tr.Need}}
}

// byCost returns the indexes of clauses in the order they run: cheapest first, then by
// what each says, so the order never depends on where a condition sits.
func byCost(clauses []spec.Clause) []int {
	order := make([]int, len(clauses))
	for i := range order {
		order[i] = i
	}

	slices.SortStableFunc(order, func(a, b int) int {
		if d := cost(clauses[a]) - cost(clauses[b]); d != 0 {
			return int(d)
		}

		return strings.Compare(key(clauses[a]), key(clauses[b]))
	})

	return order
}

// cost is how much a condition takes to read: its field's cost, a group's dearest
// condition's, and the dearest of all for a field this build doesn't know.
func cost(c spec.Clause) catalog.Cost {
	if c.Group != nil {
		most := catalog.CostItem
		for _, cl := range c.Group.Of {
			most = max(most, cost(cl))
		}

		return most
	}

	if f, ok := catalog.FieldByID(c.Field); ok {
		return f.Cost
	}

	return catalog.CostFiles
}

// key writes what a condition says, to order conditions of the same cost.
func key(c spec.Clause) string {
	if c.Group == nil {
		return string(c.Field) + "\x00" + string(c.Op) + "\x00" + c.Value + "\x00" + strings.Join(c.Values, "\x00")
	}

	parts := make([]string, 0, len(c.Group.Of)+1)
	parts = append(parts, "\x01"+string(c.Group.Match))

	for _, cl := range c.Group.Of {
		parts = append(parts, key(cl))
	}

	slices.Sort(parts[1:])

	return strings.Join(parts, "\x02")
}

// Condition decides one condition on an item, with the reason: unknown for data not
// known yet, whatever the operator, and no for a field that doesn't apply to the item.
// The trace it returns has no path; Evaluate gives it one.
func Condition(c spec.Clause, f facts.Facts, h History, now time.Time) (Result, Trace) {
	tr := Trace{Condition: spec.Phrase(c)}

	field, ok := catalog.FieldByID(c.Field)
	if !ok {
		tr.Result, tr.Actual = Unknown, "it isn't a field this build reads"

		return tr.Result, tr
	}

	v := value(field.ID, f, h)

	switch v.Status {
	case facts.StatusNotApplicable:
		tr.Result, tr.Actual = No, v.Why
	case facts.StatusUnknown:
		tr.Result, tr.Actual, tr.Need = Unknown, v.Why, v.Need
	case facts.StatusKnown:
		r, ok := compare(field, c, v, now)
		if !ok {
			r = Unknown
		}

		tr.Result, tr.Actual = r, actualWords(field, v, now)
	}

	return tr.Result, tr
}

// value returns a field's value: the routine's history from the ledger, the rest from
// the facts.
func value(id catalog.FieldID, f facts.Facts, h History) facts.Value {
	if id == catalog.FieldTries {
		return facts.Number(h.Tries)
	}

	if id != catalog.FieldLastOutcome {
		return f.Value(id)
	}

	if h.LastOutcome == "" {
		return facts.Text(catalog.OutcomeNone)
	}

	return facts.Text(h.LastOutcome)
}

// of turns a bool into a result.
func of(b bool) Result {
	if b {
		return Yes
	}

	return No
}

// compare decides a known value by the condition's operator, and reports false for an
// operator its field doesn't take.
func compare(field catalog.Field, c spec.Clause, v facts.Value, now time.Time) (Result, bool) {
	if !catalog.Takes(field, c.Op) {
		return Unknown, false
	}

	switch field.Type {
	case catalog.TypeText:
		return textOp(c.Op, v.Text, c.Value)
	case catalog.TypeList:
		return listOp(field, c.Op, v, c.Values)
	case catalog.TypeNumber:
		return numberOp(c.Op, v.Number, c.Value)
	case catalog.TypeBool:
		return of((c.Op == catalog.OpIsYes) == v.Bool), true
	case catalog.TypeChoice:
		in := slices.ContainsFunc(c.Values, func(w string) bool { return strings.EqualFold(w, v.Text) })

		return of(in == (c.Op == catalog.OpOneOf)), true
	case catalog.TypeDuration:
		return durationOp(c.Op, now.Sub(v.Since), c.Value)
	}

	return Unknown, false
}

// textTests are how each text operator but the regex compares a text with the one it
// wants, both lower-cased.
//
//nolint:gochecknoglobals // a fixed table
var textTests = map[catalog.Op]func(actual, want string) bool{
	catalog.OpIs:          func(a, w string) bool { return a == w },
	catalog.OpIsNot:       func(a, w string) bool { return a != w },
	catalog.OpContains:    strings.Contains,
	catalog.OpNotContains: func(a, w string) bool { return !strings.Contains(a, w) },
	catalog.OpStartsWith:  strings.HasPrefix,
	catalog.OpMatches:     func(a, w string) bool { return globMatch(w, a) },
}

// textOp compares a text ignoring case.
func textOp(op catalog.Op, actual, want string) (Result, bool) {
	if op == catalog.OpMatchesRegex {
		re, ok := regexes.get(want)
		if !ok {
			return Unknown, true
		}

		return of(re.MatchString(actual)), true
	}

	test, ok := textTests[op]
	if !ok {
		return Unknown, false
	}

	return of(test(strings.ToLower(actual), strings.ToLower(want))), true
}

// listTest is what a list operator looks for in a list, and whether it says the
// opposite of finding it.
type listTest struct {
	finds  func(list, values []string, match matcher) bool
	negate bool
}

// matcher reports whether a list's element matches one of an operator's patterns.
type matcher func(elem, pattern string) bool

// listTests are what each list operator looks for.
//
//nolint:gochecknoglobals // a fixed table
var listTests = map[catalog.Op]listTest{
	catalog.OpHasAny:       {finds: findsEqual},
	catalog.OpHasNone:      {finds: findsEqual, negate: true},
	catalog.OpHasAll:       {finds: holdsAll},
	catalog.OpAnyContains:  {finds: findsContaining},
	catalog.OpNoneContains: {finds: findsContaining, negate: true},
	catalog.OpAnyMatches:   {finds: findsMatching},
	catalog.OpAllMatch:     {finds: findsMissing, negate: true},
	catalog.OpNotEmpty:     {finds: findsAny},
	catalog.OpIsEmpty:      {finds: findsAny, negate: true},
}

// listOp compares a list. A find in a partial list is as sure as in a whole one, but a
// miss isn't, since what wasn't read may hold it: finding is yes, and missing is no in a
// whole list and unknown in a partial one. A negation negates that, so it is unknown
// wherever the operator it negates is. "All match" over an empty list is no, never
// vacuously yes: a pull request that changes no file doesn't have every changed path
// under docs/; while the list may hold more it is unknown.
func listOp(field catalog.Field, op catalog.Op, v facts.Value, values []string) (Result, bool) {
	test, ok := listTests[op]
	if !ok {
		return Unknown, false
	}

	if op == catalog.OpAllMatch && len(v.List) == 0 {
		return allOfNothing(v.Partial), true
	}

	r := Yes

	switch {
	case test.finds(v.List, values, elementMatcher(field, op)):
	case v.Partial:
		r = Unknown
	default:
		r = No
	}

	if test.negate {
		return r.Not(), true
	}

	return r, true
}

// allOfNothing is what "all match" says of an empty list: no, or unknown while the list
// may hold more than was read.
func allOfNothing(partial bool) Result {
	if partial {
		return Unknown
	}

	return No
}

// elementMatcher returns how a list field's elements match an operator's patterns: as
// .gitignore does for paths, else with * and ?, ignoring case either way; nil for an
// operator that matches no pattern.
func elementMatcher(field catalog.Field, op catalog.Op) matcher {
	if op != catalog.OpAnyMatches && op != catalog.OpAllMatch {
		return nil
	}

	if field.Patterns != catalog.PatternsGitignore {
		return func(elem, pattern string) bool { return globMatch(strings.ToLower(pattern), strings.ToLower(elem)) }
	}

	return func(elem, pattern string) bool {
		p, ok := pathPatterns.get(pattern)

		return ok && p.Match(elem)
	}
}

// findsEqual reports an element equal to a value, ignoring case.
func findsEqual(list, values []string, _ matcher) bool {
	return anyPair(list, values, strings.EqualFold)
}

// findsContaining reports an element holding a value, ignoring case.
func findsContaining(list, values []string, _ matcher) bool {
	return anyPair(list, values, func(e, v string) bool {
		return strings.Contains(strings.ToLower(e), strings.ToLower(v))
	})
}

// findsMatching reports an element matching a pattern.
func findsMatching(list, patterns []string, match matcher) bool {
	return anyPair(list, patterns, match)
}

// findsMissing reports an element matching none of the patterns.
func findsMissing(list, patterns []string, match matcher) bool {
	return slices.ContainsFunc(list, func(e string) bool {
		return !slices.ContainsFunc(patterns, func(p string) bool { return match(e, p) })
	})
}

// findsAny reports a list with anything in it.
func findsAny(list, _ []string, _ matcher) bool {
	return len(list) > 0
}

// anyPair reports an element and a value that match.
func anyPair(list, values []string, match func(elem, value string) bool) bool {
	return slices.ContainsFunc(list, func(e string) bool {
		return slices.ContainsFunc(values, func(v string) bool { return match(e, v) })
	})
}

// holdsAll reports a list that holds every value, ignoring case.
func holdsAll(list, values []string, _ matcher) bool {
	for _, v := range values {
		if !slices.ContainsFunc(list, func(e string) bool { return strings.EqualFold(e, v) }) {
			return false
		}
	}

	return true
}

// numberTests are how each number operator compares a number with the one it wants.
//
//nolint:gochecknoglobals // a fixed table
var numberTests = map[catalog.Op]func(actual, want int) bool{
	catalog.OpEq:  func(a, w int) bool { return a == w },
	catalog.OpNe:  func(a, w int) bool { return a != w },
	catalog.OpLt:  func(a, w int) bool { return a < w },
	catalog.OpLte: func(a, w int) bool { return a <= w },
	catalog.OpGt:  func(a, w int) bool { return a > w },
	catalog.OpGte: func(a, w int) bool { return a >= w },
}

// numberOp compares a number.
func numberOp(op catalog.Op, actual int, want string) (Result, bool) {
	w, err := strconv.Atoi(want)
	test, ok := numberTests[op]

	if err != nil || !ok {
		return Unknown, false
	}

	return of(test(actual, w)), true
}

// durationOp compares how long ago something happened with a length.
func durationOp(op catalog.Op, elapsed time.Duration, want string) (Result, bool) {
	length, err := catalog.ParseLength(want)

	switch {
	case err != nil:
		return Unknown, false
	case op == catalog.OpMoreThan:
		return of(elapsed > length), true
	case op == catalog.OpLessThan:
		return of(elapsed < length), true
	default:
		return Unknown, false
	}
}
