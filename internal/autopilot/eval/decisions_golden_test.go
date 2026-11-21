package eval_test

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
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
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/sensitive"
)

// update makes TestDecisionsGolden rewrite its golden files from the current output
// instead of comparing against them.
var update = flag.Bool("update", false, "rewrite the golden files from the current output") //nolint:gochecknoglobals // a test flag

// otherHead is a head commit other than the pull requests' own.
const otherHead = "ffffffffffffffffffffffffffffffffffffffff"

// goldenHeader opens every golden file of decisions.
const goldenHeader = "# Every decision Evaluate makes here, with its whole trace, in the order it ran.\n" +
	"# Rewrite with go test ./internal/autopilot/eval -run TestDecisionsGolden -update, and review the diff.\n"

// namedSpec, namedFacts, and namedHistory name a golden case's routine, item, and
// history, so a line of the golden says which decision it is.
type (
	namedSpec struct {
		name string
		spec spec.Spec

		// histories name the histories it is decided with, beside the section's own.
		histories []string
	}

	namedFacts struct {
		name  string
		facts facts.Facts
	}

	namedHistory struct {
		name    string
		history eval.History
	}
)

// goldenSection is one golden file: its routines, each decided on every item with every
// history named.
type goldenSection struct {
	file      string
	routines  []namedSpec
	items     []namedFacts
	histories []string
}

// TestDecisionsGolden decides routines on items and pins every decision, its whole trace
// included, in the golden files under testdata/decisions: the benchmark's cycle, the
// template routines, every trigger, every field with every operator it takes, and lists
// of conditions that order, group, and negate. Working out once per routine what
// Evaluate works out per item must leave every one of them byte for byte the same.
func TestDecisionsGolden(t *testing.T) {
	t.Parallel()

	for _, section := range []goldenSection{
		benchSection(t), routinesSection(t), triggersSection(t), conditionsSection(t), listsSection(t),
	} {
		t.Run(section.file, func(t *testing.T) {
			t.Parallel()

			checkGolden(t, filepath.Join("testdata", "decisions", section.file), decideSection(t, section))
		})
	}
}

// TestDecisionsGoldenWritesEveryField fails when a decision or a trace gains a field the
// golden files don't write, so a new field is pinned too.
func TestDecisionsGoldenWritesEveryField(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 4, reflect.TypeFor[eval.Decision]().NumField(), "writeDecision writes every field of a decision")
	assert.Equal(t, 5, reflect.TypeFor[eval.Trace]().NumField(), "writeDecision writes every field of a trace")
}

// decideSection decides every routine of a section on every item, with every history.
func decideSection(t *testing.T, s goldenSection) string {
	t.Helper()

	histories := map[string]eval.History{}
	for _, h := range goldenHistories() {
		histories[h.name] = h.history
	}

	var b strings.Builder

	b.WriteString(goldenHeader)

	for _, r := range s.routines {
		for _, f := range s.items {
			for _, name := range append(append([]string{}, s.histories...), r.histories...) {
				h, ok := histories[name]
				require.True(t, ok, "%s names the history %q", r.name, name)

				writeDecision(&b, r.name+" · "+f.name+" · "+name, eval.Evaluate(r.spec, f.facts, h, now))
			}
		}
	}

	return b.String()
}

// writeDecision writes one decision after a blank line: a line naming it, then every
// field of the decision and of each step of its trace, one a line, in the order the
// types declare them.
func writeDecision(b *strings.Builder, name string, d eval.Decision) {
	fmt.Fprintf(b, "\n== %s\n", name)
	fmt.Fprintf(b, "match: %s\nfireKey: %s\nneeds: %s\n", text(string(d.Match)), text(d.FireKey), needsText(d.Needs))

	switch {
	case d.Trace == nil:
		b.WriteString("trace: nil\n")
	case len(d.Trace) == 0:
		b.WriteString("trace: []\n")
	default:
		b.WriteString("trace:\n")
	}

	for _, tr := range d.Trace {
		fmt.Fprintf(b, "  - path: %s\n    condition: %s\n    result: %s\n    actual: %s\n    need: %s\n",
			text(tr.Path), text(tr.Condition), text(string(tr.Result)), text(tr.Actual), text(string(tr.Need)))
	}
}

// text writes s as it is, so the golden reads as the window shows it, and quoted where
// that would hide something: empty, with space at an end, or holding a character that
// doesn't print, a line break among them.
func text(s string) string {
	if s == "" || strings.TrimSpace(s) != s || strings.ContainsFunc(s, func(r rune) bool { return !strconv.IsPrint(r) }) {
		return strconv.Quote(s)
	}

	return s
}

// needsText writes needs, telling nil from empty.
func needsText(needs []facts.Need) string {
	if needs == nil {
		return "nil"
	}

	words := make([]string, 0, len(needs))
	for _, n := range needs {
		words = append(words, text(string(n)))
	}

	return "[" + strings.Join(words, ", ") + "]"
}

// checkGolden compares got with the golden file at path, or rewrites it with -update. A
// difference names the first line that differs and the decision it is in, not the whole
// file.
func checkGolden(t *testing.T, path, got string) {
	t.Helper()

	if *update {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644)) //nolint:gosec // committed test data, world-readable
		require.NoError(t, os.Chmod(path, 0o644))                  //nolint:gosec // committed test data, world-readable
	}

	data, err := os.ReadFile(path) //nolint:gosec // a fixed name under testdata
	require.NoError(t, err)

	if string(data) == got {
		return
	}

	want, have := strings.Split(string(data), "\n"), strings.Split(got, "\n")
	for i := range max(len(want), len(have)) {
		if w, g := lineAt(want, i), lineAt(have, i); w != g {
			t.Fatalf("%s differs first at line %d, in %s\nwant: %s\ngot:  %s", path, i+1, decisionAt(have, i), w, g)
		}
	}
}

// lineAt returns line i, or a marker past the end.
func lineAt(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}

	return "<end of file>"
}

// decisionAt names the decision line i is in.
func decisionAt(lines []string, i int) string {
	for j := min(i, len(lines)-1); j >= 0; j-- {
		if name, ok := strings.CutPrefix(lines[j], "== "); ok {
			return name
		}
	}

	return "the header"
}

// goldenHistories are the histories a routine is decided with: none yet, a routine that
// tried and saw the item at another commit, one whose own run pushed the head, one whose
// agent is working on it, one that first saw the head, and a click's run.
func goldenHistories() []namedHistory {
	return []namedHistory{
		{name: "fresh", history: eval.History{}},
		{name: "seen", history: eval.History{
			Tries: 2, LastOutcome: catalog.OutcomeFailed, FirstHead: otherHead, OwnHeads: []string{}, CommentsSeen: ptr(3),
		}},
		{name: "own-push", history: eval.History{
			Tries: 1, LastOutcome: catalog.OutcomeDone, FirstHead: otherHead, OwnHeads: []string{strings.ToUpper(head)},
			CommentsSeen: ptr(4),
		}},
		{name: "working", history: eval.History{FirstHead: otherHead, OwnHeads: []string{}, Working: true, CommentsSeen: ptr(9)}},
		{name: "first-head", history: eval.History{FirstHead: strings.ToUpper(head), OwnHeads: []string{}, CommentsSeen: ptr(0)}},
		{name: "manual", history: eval.History{Manual: "click-7", OwnHeads: []string{}}},
	}
}

// benchItem is the benchmark's pull request i, as BenchmarkEvaluateACycle builds it.
func benchItem(i int) facts.Facts {
	row := prRow()
	row.ID, row.Number = int64(i+1), i+1

	return build(row)
}

// benchRoutines are the benchmark's 20 routines, as BenchmarkEvaluateACycle builds them:
// the two pattern routines, then every template, repeated as needed.
func benchRoutines(t *testing.T) []namedSpec {
	t.Helper()

	routines := make([]namedSpec, 0, 20)

	for i, s := range patternRoutines() {
		require.NoError(t, spec.Validate(spec.Normalize(s)))

		routines = append(routines, namedSpec{name: fmt.Sprintf("pattern-%d", i+1), spec: s})
	}

	for len(routines) < 20 {
		routines = append(routines, templateSpecs(t)...)
	}

	return routines[:20]
}

// templateSpecs loads every template fixture of the spec package, named after its file.
func templateSpecs(t *testing.T) []namedSpec {
	t.Helper()

	entries, err := os.ReadDir(filepath.Join("..", "spec", "testdata", "templates"))
	require.NoError(t, err)

	out := make([]namedSpec, 0, len(entries))

	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".json")
		out = append(out, namedSpec{name: "template " + name, spec: template(t, name)})
	}

	return out
}

// benchSection is the benchmark's cycle: its 20 routines on its first and last pull
// requests, which differ only in their ID and number.
func benchSection(t *testing.T) goldenSection {
	t.Helper()

	return goldenSection{
		file:      "bench.golden",
		routines:  benchRoutines(t),
		items:     []namedFacts{{name: "bench pull 1", facts: benchItem(0)}, {name: "bench pull 500", facts: benchItem(499)}},
		histories: []string{"fresh"},
	}
}

// routinesSection is every template routine and the benchmark's pattern routines on the
// golden items, before and after a routine saw the item.
func routinesSection(t *testing.T) goldenSection {
	t.Helper()

	routines := benchRoutines(t)[:2]
	routines = append(routines, templateSpecs(t)...)

	return goldenSection{file: "routines.golden", routines: routines, items: goldenItems(), histories: []string{"fresh", "seen"}}
}

// goldenItems are the items the golden decisions are for: the benchmark's failing
// Dependabot pull request, the same one with nothing read yet, a passing one approved and
// behind, a person's draft from a fork read only in part, and an issue.
func goldenItems() []namedFacts {
	return []namedFacts{
		{name: "pr failing", facts: build(prRow())},
		{name: "pr unread", facts: build(unreadRow())},
		{name: "pr green", facts: build(greenRow())},
		{name: "pr partial", facts: build(partialRow())},
		{name: "issue", facts: build(issueRow())},
	}
}

// moreItems are the golden items and, for the triggers and the lists, pull requests whose
// check names and files aren't read yet, whose runs wait for a maintainer, and that have
// no checks.
func moreItems() []namedFacts {
	checksUnread := prRow()
	checksUnread.Checks, checksUnread.Files = nil, nil

	waiting := prRow()
	passing(&waiting)
	waiting.Details.ChecksAwaiting = 1

	none := prRow()
	none.Details.ChecksState, none.Details.ChecksFailed, none.Details.ChecksPassed = "", 0, 0
	none.Checks = &facts.ChecksRow{Head: head, Complete: true}

	return append(goldenItems(),
		namedFacts{name: "pr checks unread", facts: build(checksUnread)},
		namedFacts{name: "pr waiting on a maintainer", facts: build(waiting)},
		namedFacts{name: "pr without checks", facts: build(none)},
	)
}

// itemsNamed returns the golden items named.
func itemsNamed(t *testing.T, names ...string) []namedFacts {
	t.Helper()

	all := moreItems()
	out := make([]namedFacts, 0, len(names))

	for _, name := range names {
		i := slices.IndexFunc(all, func(f namedFacts) bool { return f.name == name })
		require.GreaterOrEqual(t, i, 0, "no golden item %q", name)

		out = append(out, all[i])
	}

	return out
}

// unreadRow is the benchmark's pull request with nothing but the item read.
func unreadRow() facts.Row {
	row := prRow()
	row.AuthorType, row.Association = "", ""
	row.Repository = facts.RepoRow{}
	row.Details, row.Checks, row.Files = nil, nil, nil

	return row
}

// greenRow is a Dependabot security update whose checks passed hours ago, approved and
// behind its base, in the user's review and assigned lists.
func greenRow() facts.Row {
	row := prRow()
	row.Title = "[Security] Bump lodash from 4.17.20 to 4.17.21"
	row.HeadRef = "dependabot/npm_and_yarn/lodash-4.17.21"
	row.UpdatedAt = now.Add(-10 * 24 * time.Hour)
	row.InAssigned = true
	row.ReviewRequestedAt, row.AssignedAt = ptr(now.Add(-time.Hour)), ptr(now.Add(-2*time.Hour))
	row.PinnedAt = ptr(now.Add(-time.Hour))
	row.Repository = facts.RepoRow{
		Read: true, Visibility: "PRIVATE", Permission: "WRITE", AllowUpdateBranch: true, Language: ptr("TypeScript"),
		HasClone: ptr(true),
	}
	row.Details = &facts.DetailsRow{
		HeadOID: head, BaseRef: "main", Mergeable: "MERGEABLE", MergeState: "BEHIND", ReviewDecision: "APPROVED",
		ChecksState: "SUCCESS", ChecksPassed: 2, Additions: 40, Deletions: 12, ChangedFiles: 2, Commits: 1, Comments: 9,
		Labels: []string{"Needs-Review", "dependencies", "needs-review"}, Approvals: 1, ReviewUsers: []string{"octo-user"},
		AutoMerge: true,
	}
	row.Checks = &facts.ChecksRow{Head: head, Complete: true, Checks: []facts.CheckRow{
		{Name: "test", Workflow: "CI", Outcome: facts.CheckPassed, Required: ptr(true), CompletedAt: now.Add(-3 * time.Hour)},
		{Name: "lint", Workflow: "CI", Outcome: facts.CheckPassed, CompletedAt: now.Add(-4 * time.Hour)},
	}}
	row.Files = &facts.FilesRow{Head: head, Complete: true, Files: []sensitive.File{{Path: "package.json"}, {Path: "pnpm-lock.yaml"}}}

	return row
}

// partialRow is the user's own draft from a fork, with merge conflicts and changes asked
// for, whose checks are still running and whose files were listed only in part.
func partialRow() facts.Row {
	row := prRow()
	row.Title = "Fix the login flow"
	row.Author, row.AuthorType, row.Association = "octo-user", "User", "MEMBER"
	row.HeadRef, row.Draft, row.CrossRepository = "fix/login", true, true
	row.InReviewRequested, row.InAuthored = false, true
	row.UpdatedAt = now.Add(-time.Minute)
	row.Details = &facts.DetailsRow{
		HeadOID: head, BaseRef: "release-1.2", Mergeable: "CONFLICTING", MergeState: "DIRTY",
		ReviewDecision: "CHANGES_REQUESTED", ChecksState: "PENDING", ChecksPassed: 1, ChecksFailed: 1, ChecksPending: 2,
		ChecksAwaiting: 1, Additions: 300, Deletions: 250, ChangedFiles: 40, Commits: 7, Comments: 2,
		Labels: []string{"bug"}, ChangesRequested: 2,
	}
	row.Checks = &facts.ChecksRow{Head: head, Checks: []facts.CheckRow{
		{Name: "test", Workflow: "CI", Outcome: facts.CheckFailed, Required: ptr(false), CompletedAt: now.Add(-time.Minute)},
		{Name: "lint", Workflow: "CI", Outcome: facts.CheckPending},
	}}
	row.Files = &facts.FilesRow{Head: head, Files: []sensitive.File{{Path: ".github/workflows/ci.yml"}, {Path: "docs/login.md"}}}

	return row
}

// issueRow is a contributor's bug report, assigned to the user and quiet for two weeks.
func issueRow() facts.Row {
	return facts.Row{
		ID: 9, Kind: item.KindIssue, Repo: repo.Name{Owner: "acme-org", Repo: "example-repo"}, Number: 30,
		Title: "Crash on start", Author: "octo-contributor", AuthorType: "User", Association: "CONTRIBUTOR",
		InAssigned: true, CreatedAt: now.Add(-30 * 24 * time.Hour), UpdatedAt: now.Add(-14 * 24 * time.Hour),
		Repository: facts.RepoRow{Read: true, Visibility: "PUBLIC", Permission: "TRIAGE"},
		Details:    &facts.DetailsRow{Labels: []string{"bug", "Needs-Repro"}, Comments: 6},
	}
}

// valid returns s normalized, as Load returns it, failing the test when it isn't valid.
func valid(t *testing.T, name string, s spec.Spec) namedSpec {
	t.Helper()

	s = spec.Normalize(s)
	require.NoError(t, spec.Validate(s), name)

	return namedSpec{name: name, spec: s}
}

// triggersSection is every trigger, with its options, on the golden items: those that
// read the routine's history with each history, and a click's run where it decides the
// fire key.
func triggersSection(t *testing.T) goldenSection {
	t.Helper()

	history := []string{"seen", "own-push", "working", "first-head"}
	click := []string{"manual"}

	cases := slices.Concat(triggerCases(), scheduleCases())
	routines := make([]namedSpec, 0, len(cases))

	for _, tc := range cases {
		s := routine(tc.trigger, tc.conditions)
		s.Limits.OncePer = tc.once

		r := valid(t, tc.name, s)

		switch {
		case tc.history:
			r.histories = history
		case tc.click:
			r.histories = click
		}

		routines = append(routines, r)
	}

	return goldenSection{file: "triggers.golden", routines: routines, items: moreItems(), histories: []string{"fresh"}}
}

// goldenTrigger is one trigger of the golden's: its options, the routine's once-per, the
// conditions after it, and whether its decision reads the history or a click's run.
type goldenTrigger struct {
	name       string
	trigger    spec.Trigger
	once       catalog.OncePer
	conditions spec.Conditions
	history    bool
	click      bool
}

// triggerCases are every trigger but the schedule with its options.
func triggerCases() []goldenTrigger {
	on := func(e catalog.Event) spec.Trigger { return spec.Trigger{Event: e} }
	paths := spec.Conditions{Of: []spec.Clause{
		{Field: catalog.FieldPaths, Op: catalog.OpAnyMatches, Values: []string{"go.*"}},
		{Field: catalog.FieldSize, Op: catalog.OpOneOf, Values: []string{"XS", "S"}},
	}}

	return []goldenTrigger{
		{name: "appears", trigger: on(catalog.EventAppears), click: true},
		{name: "appears once per item", trigger: on(catalog.EventAppears), once: catalog.OncePerItem, click: true},
		{name: "appears once per head", trigger: on(catalog.EventAppears), once: catalog.OncePerHeadCommit, click: true},
		{name: "appears once per day", trigger: on(catalog.EventAppears), once: catalog.OncePerDay, click: true},
		{name: "new commits", trigger: on(catalog.EventNewCommits), history: true},
		{name: "new commits, not the first", trigger: spec.Trigger{Event: catalog.EventNewCommits, FirstToo: ptr(false)}, history: true},
		{name: "new commits, own pushes too", trigger: spec.Trigger{Event: catalog.EventNewCommits, OwnPushesToo: ptr(true)}, history: true},
		{name: "checks fail", trigger: on(catalog.EventChecksFailed), click: true},
		{name: "checks fail, not waiting", trigger: spec.Trigger{Event: catalog.EventChecksFailed, WaitForChecks: ptr(false)}},
		{name: "checks fail, settling 30m", trigger: spec.Trigger{Event: catalog.EventChecksFailed, SettleMinutes: ptr(30)}},
		{name: "checks fail, then paths and size", trigger: on(catalog.EventChecksFailed), conditions: paths},
		{name: "checks pass", trigger: on(catalog.EventChecksPassed)},
		{name: "checks pass, settling 0m", trigger: spec.Trigger{Event: catalog.EventChecksPassed, SettleMinutes: ptr(0)}},
		{name: "checks pass once per day", trigger: on(catalog.EventChecksPassed), once: catalog.OncePerDay},
		{name: "changes requested", trigger: on(catalog.EventChangesRequested)},
		{name: "approved", trigger: on(catalog.EventApproved)},
		{name: "approved, then paths and size", trigger: on(catalog.EventApproved), conditions: paths},
		{name: "merge conflicts", trigger: on(catalog.EventMergeConflicts)},
		{name: "falls behind", trigger: on(catalog.EventFallsBehind)},
		{name: "labeled needs-review", trigger: spec.Trigger{Event: catalog.EventLabeled, Label: "needs-review"}},
		{name: "labeled Bug", trigger: spec.Trigger{Event: catalog.EventLabeled, Label: "Bug"}, once: catalog.OncePerHeadCommit},
		{name: "ready for review", trigger: on(catalog.EventReadyForReview)},
		{name: "review requested", trigger: on(catalog.EventReviewRequested)},
		{name: "assigned", trigger: on(catalog.EventAssigned)},
		{name: "new comments", trigger: on(catalog.EventNewComments), history: true},
		{name: "new comments, 5", trigger: spec.Trigger{Event: catalog.EventNewComments, Count: ptr(5)}, history: true},
		{name: "goes quiet", trigger: on(catalog.EventGoesQuiet)},
		{name: "goes quiet, 1 day", trigger: spec.Trigger{Event: catalog.EventGoesQuiet, Days: ptr(1)}},
		{name: "manual", trigger: on(catalog.EventManual), click: true},
	}
}

// scheduleCases are schedules due before now, after it, and on another day, on Monday,
// November 16 at noon.
func scheduleCases() []goldenTrigger {
	at := func(every catalog.Every, on catalog.Weekday, hhmm string) spec.Trigger {
		return spec.Trigger{Event: catalog.EventSchedule, Schedule: &spec.Schedule{Every: every, On: on, At: hhmm}}
	}

	return []goldenTrigger{
		{name: "schedule every day at 09:00", trigger: at(catalog.EveryDay, "", "09:00")},
		{name: "schedule every weekday at 13:00", trigger: weekday("13:00")},
		{name: "schedule every weekday at 11:59", trigger: weekday("11:59")},
		{name: "schedule every Tuesday at 09:00", trigger: at(catalog.EveryWeek, catalog.Tuesday, "09:00")},
		{name: "schedule every Monday at 08:30", trigger: at(catalog.EveryWeek, catalog.Monday, "08:30"), click: true},
	}
}

// conditionsSection is every field with every operator it takes, one condition a
// routine, on the golden items that read it known, unknown, and not applying: the first
// of the operator's cases in sample, which TestEveryFieldEveryOperator decides.
func conditionsSection(t *testing.T) goldenSection {
	t.Helper()

	routines := make([]namedSpec, 0, 256)

	for _, field := range catalog.Fields() {
		_, cases := sample(field)

		for _, op := range catalog.OpsFor(field) {
			c := firstCase(t, cases, op)
			s := routine(spec.Trigger{Event: catalog.EventAppears}, spec.Conditions{Of: []spec.Clause{c.clause(field.ID)}})
			r := valid(t, string(field.ID)+" "+string(op), s)

			if field.Group == catalog.GroupHistory {
				r.histories = []string{"seen", "own-push"}
			}

			routines = append(routines, r)
		}
	}

	return goldenSection{
		file: "conditions.golden", routines: routines, items: itemsNamed(t, "pr failing", "pr unread", "issue"),
		histories: []string{"fresh"},
	}
}

// firstCase returns the first of cases for op.
func firstCase(t *testing.T, cases []opCase, op catalog.Op) opCase {
	t.Helper()

	for _, c := range cases {
		if c.op == op {
			return c
		}
	}

	require.Failf(t, "no case", "sample has no case for %s", op)

	return opCase{}
}

// listsSection is lists of conditions on the golden items: in cost order and, at one
// cost, in the order of what they say, whatever order they are written in; "all" and
// "any"; groups; negations; no conditions; and, as a spec from a newer build could hold
// them, a field, an operator, a value, and a trigger this build doesn't read.
func listsSection(t *testing.T) goldenSection {
	t.Helper()

	routines := make([]namedSpec, 0, 24)

	for _, tc := range listCasesGolden() {
		r := valid(t, tc.name, routine(tc.trigger, tc.conditions))
		r.histories = tc.histories
		routines = append(routines, r)
	}

	for _, tc := range newerCases() {
		r := namedSpec{name: tc.name, spec: spec.Normalize(routine(tc.trigger, tc.conditions)), histories: tc.histories}
		require.Error(t, spec.Validate(r.spec), "%s is a spec this build doesn't take", tc.name)

		routines = append(routines, r)
	}

	items := append(goldenItems(), itemsNamed(t, "pr checks unread")...)

	return goldenSection{file: "lists.golden", routines: routines, items: items, histories: []string{"fresh"}}
}

// listCase is one list of conditions after a trigger.
type listCase struct {
	name       string
	trigger    spec.Trigger
	conditions spec.Conditions
	histories  []string
}

// clauses are the conditions the list cases combine.
func clauses() map[string]spec.Clause {
	return map[string]spec.Clause{
		"title bump":       {Field: catalog.FieldTitle, Op: catalog.OpContains, Value: "bump"},
		"title net":        {Field: catalog.FieldTitle, Op: catalog.OpContains, Value: "net"},
		"title not wip":    {Field: catalog.FieldTitle, Op: catalog.OpNotContains, Value: "wip"},
		"title is not x":   {Field: catalog.FieldTitle, Op: catalog.OpIsNot, Value: "x"},
		"title is x":       {Field: catalog.FieldTitle, Op: catalog.OpIs, Value: "x"},
		"author":           {Field: catalog.FieldAuthor, Op: catalog.OpIs, Value: "dependabot"},
		"kind pr":          {Field: catalog.FieldKind, Op: catalog.OpOneOf, Values: []string{"pr"}},
		"bot":              {Field: catalog.FieldAuthorIsBot, Op: catalog.OpIsYes},
		"trust not out":    {Field: catalog.FieldTrust, Op: catalog.OpNoneOf, Values: []string{"outside"}},
		"trust out":        {Field: catalog.FieldTrust, Op: catalog.OpOneOf, Values: []string{"outside"}},
		"labels deps":      {Field: catalog.FieldLabels, Op: catalog.OpHasAny, Values: []string{"dependencies"}},
		"labels security":  {Field: catalog.FieldLabels, Op: catalog.OpHasAny, Values: []string{"security"}},
		"labels blocked":   {Field: catalog.FieldLabels, Op: catalog.OpHasAny, Values: []string{"blocked"}},
		"no blocked":       {Field: catalog.FieldLabels, Op: catalog.OpHasNone, Values: []string{"blocked"}},
		"lines lt 100":     {Field: catalog.FieldLines, Op: catalog.OpLt, Value: "100"},
		"lines ne 0":       {Field: catalog.FieldLines, Op: catalog.OpNe, Value: "0"},
		"not draft":        {Field: catalog.FieldDraft, Op: catalog.OpIsNo},
		"draft":            {Field: catalog.FieldDraft, Op: catalog.OpIsYes},
		"conflicts":        {Field: catalog.FieldConflicts, Op: catalog.OpIsYes},
		"behind":           {Field: catalog.FieldBehind, Op: catalog.OpIsYes},
		"failed security":  {Field: catalog.FieldFailedNames, Op: catalog.OpAnyContains, Values: []string{"security"}},
		"failed *security": {Field: catalog.FieldFailedNames, Op: catalog.OpAnyMatches, Values: []string{"*Security*"}},
		"failed lint":      {Field: catalog.FieldFailedNames, Op: catalog.OpAnyContains, Values: []string{"lint"}},
		"no failed lint":   {Field: catalog.FieldFailedNames, Op: catalog.OpNoneContains, Values: []string{"lint"}},
		"paths go":         {Field: catalog.FieldPaths, Op: catalog.OpAnyMatches, Values: []string{"go.*"}},
		"paths docs":       {Field: catalog.FieldPaths, Op: catalog.OpAllMatch, Values: []string{"docs/"}},
		"workflows":        {Field: catalog.FieldSensitive, Op: catalog.OpHasAny, Values: []string{"workflows"}},
		"tries lt 3":       {Field: catalog.FieldTries, Op: catalog.OpLt, Value: "3"},
		"not gave up":      {Field: catalog.FieldLastOutcome, Op: catalog.OpNoneOf, Values: []string{catalog.OutcomeGaveUp}},
	}
}

// of makes a list of the named conditions, matching match.
func of(match spec.Match, names ...string) spec.Conditions {
	all := clauses()
	out := spec.Conditions{Match: match, Of: make([]spec.Clause, 0, len(names))}

	for _, n := range names {
		c, ok := all[n]
		if !ok {
			panic("no clause " + n)
		}

		out.Of = append(out.Of, c)
	}

	return out
}

// group makes a group of the named conditions, matching match.
func group(match spec.Match, names ...string) spec.Clause {
	g := of(match, names...)

	return spec.Clause{Group: &g}
}

// with returns c with more clauses after its own.
func with(c spec.Conditions, more ...spec.Clause) spec.Conditions {
	c.Of = append(c.Of, more...)

	return c
}

// listCasesGolden are the lists of conditions the golden decides.
func listCasesGolden() []listCase {
	appears := spec.Trigger{Event: catalog.EventAppears}
	byCost := []string{"paths go", "failed security", "labels deps", "title bump"}
	sameCost := []string{"title net", "author", "title bump", "kind pr"}

	return []listCase{
		{name: "all, dearest first as written", trigger: appears, conditions: of(spec.MatchAll, byCost...)},
		{name: "any, dearest first as written", trigger: appears, conditions: of(spec.MatchAny, byCost...)},
		{name: "all, one cost", trigger: appears, conditions: of(spec.MatchAll, sameCost...)},
		{name: "all, one cost, written the other way", trigger: appears, conditions: of(spec.MatchAll, reversed(sameCost)...)},
		{name: "any, one cost", trigger: appears, conditions: of(spec.MatchAny, sameCost...)},
		{
			name: "all, with groups", trigger: appears,
			conditions: with(of(spec.MatchAll, "title not wip", "bot"),
				group(spec.MatchAny, "failed *security", "labels security"), group(spec.MatchAll, "lines lt 100", "not draft")),
		},
		{
			name: "all, with groups written the other way", trigger: appears,
			conditions: with(spec.Conditions{Match: spec.MatchAll},
				group(spec.MatchAll, "not draft", "lines lt 100"), group(spec.MatchAny, "labels security", "failed *security"),
				clauses()["bot"], clauses()["title not wip"]),
		},
		{
			name: "any of groups", trigger: appears,
			conditions: with(spec.Conditions{Match: spec.MatchAny},
				group(spec.MatchAll, "draft", "conflicts"), group(spec.MatchAny, "behind", "workflows"), clauses()["paths docs"]),
		},
		{name: "a group alone", trigger: appears, conditions: with(spec.Conditions{Match: spec.MatchAll}, group(spec.MatchAny, "conflicts", "behind"))},
		{
			name: "all of negations", trigger: appears,
			conditions: of(spec.MatchAll, "title is not x", "title not wip", "no blocked", "no failed lint", "lines ne 0", "not draft", "trust not out"),
		},
		{
			name: "any of the negations' opposites", trigger: appears,
			conditions: of(spec.MatchAny, "title is x", "labels blocked", "failed lint", "draft", "trust out", "paths docs"),
		},
		{name: "unknown before no", trigger: appears, conditions: of(spec.MatchAll, "workflows", "labels deps", "kind pr")},
		{name: "no conditions", trigger: appears, conditions: spec.Conditions{}},
		{name: "any of no conditions", trigger: appears, conditions: spec.Conditions{Match: spec.MatchAny}},
		{
			name: "the routine's history", trigger: appears, conditions: of(spec.MatchAll, "tries lt 3", "not gave up", "title bump"),
			histories: []string{"seen", "own-push"},
		},
		{
			name: "an unknown trigger and unknown conditions", trigger: spec.Trigger{Event: catalog.EventMergeConflicts},
			conditions: of(spec.MatchAll, "workflows", "failed security", "labels deps"),
		},
		{
			name: "a trigger that doesn't hold", trigger: spec.Trigger{Event: catalog.EventApproved},
			conditions: of(spec.MatchAll, "title bump", "paths go"),
		},
	}
}

// newerCases are lists of conditions and triggers this build doesn't read, as a spec
// from a newer build could hold them: Validate refuses them, and Evaluate is unknown on
// them, never wrong.
func newerCases() []listCase {
	appears := spec.Trigger{Event: catalog.EventAppears}
	mood := spec.Clause{Field: "item.mood", Op: catalog.OpIs, Value: "happy"}

	return []listCase{
		{name: "newer: a field this build doesn't read", trigger: appears, conditions: with(of(spec.MatchAll, "title bump"), mood)},
		{
			name: "newer: a field this build doesn't read, in a group", trigger: appears,
			conditions: with(of(spec.MatchAny, "draft"), spec.Clause{Group: &spec.Conditions{
				Match: spec.MatchAll, Of: []spec.Clause{mood, clauses()["bot"]},
			}}),
		},
		{
			name: "newer: operators and values this build doesn't take", trigger: appears,
			conditions: spec.Conditions{Match: spec.MatchAny, Of: []spec.Clause{
				{Field: catalog.FieldTitle, Op: catalog.OpHasAny, Values: []string{"bump"}},
				{Field: catalog.FieldLines, Op: catalog.OpGt, Value: "many"},
				{Field: catalog.FieldAge, Op: catalog.OpMoreThan, Value: "forever"},
				{Field: catalog.FieldTitle, Op: catalog.OpMatchesRegex, Value: "("},
				{Field: catalog.FieldPaths, Op: catalog.OpAnyMatches, Values: []string{"[z-a]"}},
			}},
		},
		{
			name: "newer: a trigger this build doesn't read", trigger: spec.Trigger{Event: "deployed"},
			conditions: of(spec.MatchAll, "title bump", "workflows"), histories: []string{"manual"},
		},
	}
}

// reversed returns names in the opposite order.
func reversed(names []string) []string {
	out := make([]string, 0, len(names))
	for i := len(names) - 1; i >= 0; i-- {
		out = append(out, names[i])
	}

	return out
}
