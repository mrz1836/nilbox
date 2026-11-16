package eval_test

import (
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

// opCase is one operator against a field's sample value.
type opCase struct {
	op     catalog.Op
	value  string
	values []string
	want   eval.Result
}

// clause is the case's condition on field.
func (c opCase) clause(field catalog.FieldID) spec.Clause {
	return spec.Clause{Field: field, Op: c.op, Value: c.value, Values: c.values}
}

// textCases are every text operator against "Bump golang.org/x/net".
func textCases() []opCase {
	return []opCase{
		{op: catalog.OpIs, value: "bump GOLANG.org/x/net", want: eval.Yes},
		{op: catalog.OpIs, value: "bump", want: eval.No},
		{op: catalog.OpIsNot, value: "bump", want: eval.Yes},
		{op: catalog.OpIsNot, value: "BUMP golang.org/x/net", want: eval.No},
		{op: catalog.OpContains, value: "X/NET", want: eval.Yes},
		{op: catalog.OpContains, value: "y/net", want: eval.No},
		{op: catalog.OpNotContains, value: "y/net", want: eval.Yes},
		{op: catalog.OpNotContains, value: "golang", want: eval.No},
		{op: catalog.OpStartsWith, value: "bump g", want: eval.Yes},
		{op: catalog.OpStartsWith, value: "x/net", want: eval.No},
		{op: catalog.OpMatches, value: "bump *x/?et", want: eval.Yes},
		{op: catalog.OpMatches, value: "*.org", want: eval.No},
		{op: catalog.OpMatchesRegex, value: `^bump [a-z.]+/x/NET$`, want: eval.Yes},
		{op: catalog.OpMatchesRegex, value: `^x/net`, want: eval.No},
	}
}

// listCases are every list operator against "go.mod" and "docs/readme.md", whose
// patterns match alike as globs and as .gitignore does.
func listCases() []opCase {
	return []opCase{
		{op: catalog.OpHasAny, values: []string{"zz", "GO.MOD"}, want: eval.Yes},
		{op: catalog.OpHasAny, values: []string{"go"}, want: eval.No},
		{op: catalog.OpHasAll, values: []string{"go.mod", "docs/readme.md"}, want: eval.Yes},
		{op: catalog.OpHasAll, values: []string{"go.mod", "go.sum"}, want: eval.No},
		{op: catalog.OpHasNone, values: []string{"go.sum"}, want: eval.Yes},
		{op: catalog.OpHasNone, values: []string{"go.sum", "go.mod"}, want: eval.No},
		{op: catalog.OpAnyContains, values: []string{"zz", "README"}, want: eval.Yes},
		{op: catalog.OpAnyContains, values: []string{"sum"}, want: eval.No},
		{op: catalog.OpNoneContains, values: []string{"sum"}, want: eval.Yes},
		{op: catalog.OpNoneContains, values: []string{"MOD"}, want: eval.No},
		{op: catalog.OpAnyMatches, values: []string{"*.txt", "*.md"}, want: eval.Yes},
		{op: catalog.OpAnyMatches, values: []string{"*.txt"}, want: eval.No},
		{op: catalog.OpAllMatch, values: []string{"*.md", "go.*"}, want: eval.Yes},
		{op: catalog.OpAllMatch, values: []string{"*.md"}, want: eval.No},
		{op: catalog.OpIsEmpty, want: eval.No},
		{op: catalog.OpNotEmpty, want: eval.Yes},
	}
}

// choiceListCases are every operator a list of choices takes, against its first choice.
func choiceListCases(f catalog.Field) []opCase {
	first, second := f.Choices[0].Value, f.Choices[1].Value

	return []opCase{
		{op: catalog.OpHasAny, values: []string{second, first}, want: eval.Yes},
		{op: catalog.OpHasAny, values: []string{second}, want: eval.No},
		{op: catalog.OpHasAll, values: []string{first}, want: eval.Yes},
		{op: catalog.OpHasAll, values: []string{first, second}, want: eval.No},
		{op: catalog.OpHasNone, values: []string{second}, want: eval.Yes},
		{op: catalog.OpHasNone, values: []string{first}, want: eval.No},
		{op: catalog.OpIsEmpty, want: eval.No},
		{op: catalog.OpNotEmpty, want: eval.Yes},
	}
}

// numberCases are every number operator against 5.
func numberCases() []opCase {
	return []opCase{
		{op: catalog.OpEq, value: "5", want: eval.Yes},
		{op: catalog.OpEq, value: "4", want: eval.No},
		{op: catalog.OpNe, value: "4", want: eval.Yes},
		{op: catalog.OpNe, value: "5", want: eval.No},
		{op: catalog.OpLt, value: "6", want: eval.Yes},
		{op: catalog.OpLt, value: "5", want: eval.No},
		{op: catalog.OpLte, value: "5", want: eval.Yes},
		{op: catalog.OpLte, value: "4", want: eval.No},
		{op: catalog.OpGt, value: "4", want: eval.Yes},
		{op: catalog.OpGt, value: "5", want: eval.No},
		{op: catalog.OpGte, value: "5", want: eval.Yes},
		{op: catalog.OpGte, value: "6", want: eval.No},
	}
}

// choiceCases are every choice operator against the field's first choice.
func choiceCases(f catalog.Field) []opCase {
	first, second := f.Choices[0].Value, f.Choices[1].Value

	return []opCase{
		{op: catalog.OpOneOf, values: []string{second, first}, want: eval.Yes},
		{op: catalog.OpOneOf, values: []string{second}, want: eval.No},
		{op: catalog.OpNoneOf, values: []string{second}, want: eval.Yes},
		{op: catalog.OpNoneOf, values: []string{first}, want: eval.No},
	}
}

// sample returns a known value for a field, and every operator it takes against that
// value; tries is the history's count, for the routine's own tries.
func sample(f catalog.Field) (facts.Value, []opCase) {
	switch f.Type {
	case catalog.TypeText:
		return facts.Text("Bump golang.org/x/net"), textCases()
	case catalog.TypeList:
		if len(f.Choices) > 0 {
			return facts.List([]string{f.Choices[0].Value}, false), choiceListCases(f)
		}

		return facts.List([]string{"go.mod", "docs/readme.md"}, false), listCases()
	case catalog.TypeNumber:
		return facts.Number(tries), numberCases()
	case catalog.TypeBool:
		return facts.Bool(true), []opCase{{op: catalog.OpIsYes, want: eval.Yes}, {op: catalog.OpIsNo, want: eval.No}}
	case catalog.TypeChoice:
		return facts.Text(f.Choices[0].Value), choiceCases(f)
	case catalog.TypeDuration:
		return facts.Since(now.Add(-72 * time.Hour)), []opCase{
			{op: catalog.OpMoreThan, value: "2d", want: eval.Yes},
			{op: catalog.OpMoreThan, value: "4d", want: eval.No},
			{op: catalog.OpLessThan, value: "73h", want: eval.Yes},
			{op: catalog.OpLessThan, value: "60m", want: eval.No},
		}
	}

	return facts.Value{}, nil
}

// tries is the number fields' sample, the history's tries among them.
const tries = 5

// TestEveryFieldEveryOperator decides every field with every operator it takes: on a
// known value, on one not known yet, which is unknown whatever the operator, and on an
// item the field doesn't apply to, which is no.
func TestEveryFieldEveryOperator(t *testing.T) {
	t.Parallel()

	for _, field := range catalog.Fields() {
		t.Run(string(field.ID), func(t *testing.T) {
			t.Parallel()

			v, cases := sample(field)
			covered := map[catalog.Op]bool{}

			for _, c := range cases {
				covered[c.op] = true

				checkCase(t, field, v, c)
			}

			for _, op := range catalog.OpsFor(field) {
				assert.True(t, covered[op], "%s %s has a case", field.ID, op)
			}
		})
	}
}

// checkCase decides one operator on a field: against its sample, against a value not
// known yet, and on an item it doesn't apply to. The routine's own history is always
// known, so only its sample is decided.
func checkCase(t *testing.T, field catalog.Field, v facts.Value, c opCase) {
	t.Helper()

	history := eval.History{Tries: tries}
	cl := c.clause(field.ID)

	assert.Equal(t, c.want, decide(cl, itemFacts(item.KindPR, field.ID, v), history),
		"%s %s %q %q", field.ID, c.op, c.value, c.values)

	if field.Group == catalog.GroupHistory {
		return
	}

	unknown := itemFacts(item.KindPR, field.ID, facts.Unknown("not read yet", facts.NeedChecks))
	assert.Equal(t, eval.Unknown, decide(cl, unknown, history), "%s %s on an unknown", field.ID, c.op)

	na := itemFacts(item.KindIssue, field.ID, facts.NotApplicable("it's an issue"))
	assert.Equal(t, eval.No, decide(cl, na, history), "%s %s where it doesn't apply", field.ID, c.op)
}

func TestHistoryFieldsComeFromTheLedger(t *testing.T) {
	t.Parallel()

	f := itemFacts(item.KindPR, catalog.FieldTitle, facts.Text("x"))
	outcome := spec.Clause{Field: catalog.FieldLastOutcome, Op: catalog.OpOneOf, Values: []string{catalog.OutcomeGaveUp}}

	assert.Equal(t, eval.Yes, decide(outcome, f, eval.History{LastOutcome: catalog.OutcomeGaveUp}))
	assert.Equal(t, eval.No, decide(outcome, f, eval.History{}), "none yet")

	none := spec.Clause{Field: catalog.FieldLastOutcome, Op: catalog.OpOneOf, Values: []string{catalog.OutcomeNone}}
	assert.Equal(t, eval.Yes, decide(none, f, eval.History{}))
}

// TestPartialLists checks a list read only in part: a hit is as sure as in a whole list,
// a miss isn't, and every negation follows.
func TestPartialLists(t *testing.T) {
	t.Parallel()

	partial := facts.List([]string{"CI / lint"}, true)
	nothingYet := facts.List([]string{}, true)

	tests := []struct {
		name   string
		v      facts.Value
		op     catalog.Op
		values []string
		want   eval.Result
	}{
		{name: "a hit", v: partial, op: catalog.OpAnyContains, values: []string{"lint"}, want: eval.Yes},
		{name: "a miss", v: partial, op: catalog.OpAnyContains, values: []string{"security"}, want: eval.Unknown},
		{name: "a hit, negated", v: partial, op: catalog.OpNoneContains, values: []string{"lint"}, want: eval.No},
		{name: "a miss, negated", v: partial, op: catalog.OpNoneContains, values: []string{"security"}, want: eval.Unknown},
		{name: "all held", v: partial, op: catalog.OpHasAll, values: []string{"ci / lint"}, want: eval.Yes},
		{name: "some missing", v: partial, op: catalog.OpHasAll, values: []string{"CI / lint", "x"}, want: eval.Unknown},
		{name: "every one matches so far", v: partial, op: catalog.OpAllMatch, values: []string{"CI / *"}, want: eval.Unknown},
		{name: "one doesn't match", v: partial, op: catalog.OpAllMatch, values: []string{"CD / *"}, want: eval.No},
		{name: "nothing read yet", v: nothingYet, op: catalog.OpIsEmpty, want: eval.Unknown},
		{name: "something read", v: partial, op: catalog.OpIsEmpty, want: eval.No},
		{name: "something read, negated", v: partial, op: catalog.OpNotEmpty, want: eval.Yes},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := spec.Clause{Field: catalog.FieldFailedNames, Op: tt.op, Values: tt.values}
			assert.Equal(t, tt.want, decide(c, itemFacts(item.KindPR, catalog.FieldFailedNames, tt.v), eval.History{}))
		})
	}
}

// TestAllOfNothingIsNo checks that an "all" operator over an empty list is no, never
// vacuously yes: a pull request that changes no file doesn't match "every changed path
// matches docs/", and one whose files aren't all read yet is unknown.
func TestAllOfNothingIsNo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		c    spec.Clause
		v    facts.Value
		want eval.Result
	}{
		{
			name: "every path matches, of none",
			c:    spec.Clause{Field: catalog.FieldPaths, Op: catalog.OpAllMatch, Values: []string{"docs/"}},
			v:    facts.List([]string{}, false), want: eval.No,
		},
		{
			name: "every path matches, of none read yet",
			c:    spec.Clause{Field: catalog.FieldPaths, Op: catalog.OpAllMatch, Values: []string{"docs/"}},
			v:    facts.List([]string{}, true), want: eval.Unknown,
		},
		{
			name: "every name matches, of none",
			c:    spec.Clause{Field: catalog.FieldCheckNames, Op: catalog.OpAllMatch, Values: []string{"*"}},
			v:    facts.List([]string{}, false), want: eval.No,
		},
		{
			name: "all of the labels, of none",
			c:    spec.Clause{Field: catalog.FieldLabels, Op: catalog.OpHasAll, Values: []string{"bug"}},
			v:    facts.List([]string{}, false), want: eval.No,
		},
		{
			name: "every path matches, of one",
			c:    spec.Clause{Field: catalog.FieldPaths, Op: catalog.OpAllMatch, Values: []string{"docs/"}},
			v:    facts.List([]string{"docs/readme.md"}, false), want: eval.Yes,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, decide(tt.c, itemFacts(item.KindPR, tt.c.Field, tt.v), eval.History{}))
		})
	}
}

func TestPathsMatchAsGitignoreDoes(t *testing.T) {
	t.Parallel()

	f := itemFacts(item.KindPR, catalog.FieldPaths, facts.List([]string{".github/workflows/ci.yml", "src/app.go"}, false))

	for _, tt := range []struct {
		pattern string
		want    eval.Result
	}{
		{pattern: ".github/workflows/", want: eval.Yes},
		{pattern: "ci.yml", want: eval.Yes},
		{pattern: "/app.go", want: eval.No},
		{pattern: "src/**/*.go", want: eval.Yes},
		{pattern: "*.rs", want: eval.No},
	} {
		c := spec.Clause{Field: catalog.FieldPaths, Op: catalog.OpAnyMatches, Values: []string{tt.pattern}}
		assert.Equal(t, tt.want, decide(c, f, eval.History{}), tt.pattern)
	}
}

func TestConditionSaysWhy(t *testing.T) {
	t.Parallel()

	f := build(prRow())

	r, tr := eval.Condition(spec.Clause{
		Field: catalog.FieldFailedNames, Op: catalog.OpAnyContains, Values: []string{"security"},
	}, f, eval.History{}, now)

	assert.Equal(t, eval.Yes, r)
	assert.Equal(t, eval.Trace{
		Condition: "a failed check's name contains security", Result: eval.Yes,
		Actual: `"CodeQL / Analyze (go)", "🔒 Security Scans / 🔒 Security Scans"`,
	}, tr)

	row := prRow()
	row.Checks = nil

	r, tr = eval.Condition(spec.Clause{
		Field: catalog.FieldFailedNames, Op: catalog.OpNoneContains, Values: []string{"security"},
	}, build(row), eval.History{}, now)

	assert.Equal(t, eval.Unknown, r, "a negation of what isn't known yet is unknown too")
	assert.Equal(t, facts.NeedChecks, tr.Need)
	assert.Equal(t, "the check names aren't read yet", tr.Actual)
}

func TestConditionOnAFieldThisBuildDoesntRead(t *testing.T) {
	t.Parallel()

	r, tr := eval.Condition(spec.Clause{Field: "checks.flaky", Op: catalog.OpIsYes}, facts.Facts{}, eval.History{}, now)
	assert.Equal(t, eval.Unknown, r)
	assert.Contains(t, tr.Actual, "isn't a field")

	r, _ = eval.Condition(spec.Clause{Field: catalog.FieldTitle, Op: catalog.OpGt, Value: "1"},
		itemFacts(item.KindPR, catalog.FieldTitle, facts.Text("x")), eval.History{}, now)
	assert.Equal(t, eval.Unknown, r, "an operator the field doesn't take")
}

func TestResults(t *testing.T) {
	t.Parallel()

	y, n, u := eval.Yes, eval.No, eval.Unknown

	assert.Equal(t, n, y.Not())
	assert.Equal(t, y, n.Not())
	assert.Equal(t, u, u.Not())

	assert.Equal(t, y, eval.All())
	assert.Equal(t, y, eval.All(y, y))
	assert.Equal(t, u, eval.All(y, u))
	assert.Equal(t, n, eval.All(u, n), "no as soon as one is no")

	assert.Equal(t, n, eval.Any())
	assert.Equal(t, n, eval.Any(n, n))
	assert.Equal(t, u, eval.Any(n, u))
	assert.Equal(t, y, eval.Any(u, y), "yes as soon as one is yes")
}

func TestEvaluateCombinesAndSaysWhatsMissing(t *testing.T) {
	t.Parallel()

	row := prRow()
	row.Checks, row.Files = nil, nil
	f := build(row)

	security := spec.Clause{Field: catalog.FieldFailedNames, Op: catalog.OpAnyContains, Values: []string{"security"}}
	clean := spec.Clause{Field: catalog.FieldSensitive, Op: catalog.OpIsEmpty}
	draft := spec.Clause{Field: catalog.FieldDraft, Op: catalog.OpIsYes}
	bot := spec.Clause{Field: catalog.FieldAuthorIsBot, Op: catalog.OpIsYes}

	tests := []struct {
		name  string
		c     spec.Conditions
		match eval.Result
		needs []facts.Need
		ran   int
	}{
		{
			name: "all, unknown", c: spec.Conditions{Of: []spec.Clause{bot, security, clean}}, match: eval.Unknown,
			needs: []facts.Need{facts.NeedChecks, facts.NeedFiles}, ran: 3,
		},
		{
			name: "all, stopped by a cheap no", c: spec.Conditions{Of: []spec.Clause{security, clean, draft}}, match: eval.No,
			needs: []facts.Need{}, ran: 1,
		},
		{
			name: "any, decided by a cheap yes", c: spec.Conditions{Match: spec.MatchAny, Of: []spec.Clause{security, bot}},
			match: eval.Yes, needs: []facts.Need{}, ran: 1,
		},
		{
			name: "a group that is no needs nothing",
			c: spec.Conditions{Match: spec.MatchAny, Of: []spec.Clause{
				security, {Group: &spec.Conditions{Match: spec.MatchAll, Of: []spec.Clause{draft, clean}}},
			}},
			match: eval.Unknown, needs: []facts.Need{facts.NeedChecks}, ran: 2,
		},
		{name: "none", c: spec.Conditions{Match: spec.MatchAny}, match: eval.Yes, needs: []facts.Need{}, ran: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d := eval.Evaluate(routine(spec.Trigger{Event: catalog.EventAppears}, tt.c), f, eval.History{}, now)
			assert.Equal(t, tt.match, d.Match)
			assert.Equal(t, tt.needs, d.Needs)
			require.Len(t, d.Trace, tt.ran+1, "the trigger, then each condition that ran")
			assert.Equal(t, "trigger", d.Trace[0].Path)
		})
	}
}

func TestEvaluateRunsTheCheapestFirst(t *testing.T) {
	t.Parallel()

	row := prRow()
	row.Checks, row.Files = nil, nil

	files := spec.Clause{Field: catalog.FieldSensitive, Op: catalog.OpIsEmpty}
	checks := spec.Clause{Field: catalog.FieldFailedNames, Op: catalog.OpIsEmpty}
	draft := spec.Clause{Field: catalog.FieldDraft, Op: catalog.OpIsNo}

	d := eval.Evaluate(routine(spec.Trigger{Event: catalog.EventAppears}, spec.Conditions{Of: []spec.Clause{files, checks, draft}}),
		build(row), eval.History{}, now)

	paths := make([]string, 0, len(d.Trace))
	for _, tr := range d.Trace {
		paths = append(paths, tr.Path)
	}

	assert.Equal(t, []string{"trigger", "conditions.of[2]", "conditions.of[1]", "conditions.of[0]"}, paths)
}

func TestEvaluateTheFlagship(t *testing.T) {
	t.Parallel()

	s := template(t, "fix-dependabot-security")

	d := eval.Evaluate(s, build(prRow()), eval.History{}, now)
	assert.Equal(t, eval.Yes, d.Match)
	assert.Equal(t, "checks_failed:commit:"+head, d.FireKey)

	unread := prRow()
	unread.Checks = nil

	d = eval.Evaluate(s, build(unread), eval.History{}, now)
	assert.Equal(t, eval.Unknown, d.Match, "the check names aren't read, so whether one says security isn't known")
	assert.Equal(t, []facts.Need{facts.NeedChecks}, d.Needs)
	assert.Empty(t, d.FireKey, "the trigger waits for the checks to settle, which their names tell")

	lint := prRow()
	lint.Checks.Checks = []facts.CheckRow{{Name: "lint", Workflow: "CI", Outcome: facts.CheckFailed, CompletedAt: now.Add(-time.Hour)}}

	d = eval.Evaluate(s, build(lint), eval.History{}, now)
	assert.Equal(t, eval.No, d.Match)
	assert.Equal(t, "checks_failed:commit:"+head, d.FireKey, "the trigger fired, so the key says which occurrence")

	issue := prRow()
	issue.Kind = item.KindIssue

	d = eval.Evaluate(s, build(issue), eval.History{}, now)
	assert.Equal(t, eval.No, d.Match)
	assert.Contains(t, d.Trace[0].Actual, "fires only for pull requests")
}
