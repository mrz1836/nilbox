package autopilot

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/draft"
	"github.com/mrz1836/nilbox/internal/autopilot/eval"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/trust"
)

// updateGallery makes TestEveryTemplatesSentenceIsPinned rewrite its golden files.
var updateGallery = flag.Bool("update", false, "rewrite the golden files from the current output") //nolint:gochecknoglobals // a test flag

// galleryNow is the moment the gallery's fixtures are decided at, and galleryViewer the
// account signed in.
var galleryNow = time.Date(2026, time.November, 16, 12, 0, 0, 0, time.UTC) //nolint:gochecknoglobals // a fixed test moment

const (
	galleryViewer = "octo-user"
	galleryHead   = "0a1b2c3d4e5f60718293a4b5c6d7e8f901234567"
)

// filled returns t's spec with every blank filled as quick setup would: a where with your
// account and an org, a list of check names with a flaky one.
func filled(t GalleryTemplate) spec.Spec {
	sp := cloneSpec(t.Spec)

	for _, b := range t.Blanks {
		switch b.Kind {
		case BlankWhere:
			sp.Watch.Sources[b.Source].Search.Where = []string{"@me", "org:acme-org"}
		case BlankChecks:
			sp.Conditions.Of[b.Condition].Values = []string{"e2e / browser"}
		}
	}

	return spec.Normalize(sp)
}

// cloneSpec copies a spec through its JSON.
func cloneSpec(s spec.Spec) spec.Spec {
	data, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}

	var out spec.Spec
	if err = json.Unmarshal(data, &out); err != nil {
		panic(err)
	}

	return out
}

func TestEveryTemplateHasItsFileAndEveryFileATemplate(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir("gallery")
	require.NoError(t, err)

	files := make([]string, 0, len(entries))

	for _, e := range entries {
		files = append(files, strings.TrimSuffix(e.Name(), ".json"))
	}

	gallery := Gallery()
	keys := make([]string, 0, len(gallery))

	for _, g := range gallery {
		keys = append(keys, g.Key)

		checked, keyErr := draft.CheckTemplate(g.Key)
		require.NoError(t, keyErr, "a template's key is one Save takes")
		assert.Equal(t, g.Key, checked, "as it is")
		assert.NotEmpty(t, g.Name)
		assert.NotEmpty(t, g.Icon)
		assert.NotEmpty(t, g.Summary)
		assert.True(t, g.Suggests.Valid(), g.Key)
	}

	assert.ElementsMatch(t, files, keys)
}

func TestEveryTemplatesSpecValidates(t *testing.T) {
	t.Parallel()

	for _, g := range Gallery() {
		t.Run(g.Key, func(t *testing.T) {
			t.Parallel()

			required := slices.ContainsFunc(g.Blanks, func(b Blank) bool { return b.Required })
			if !required {
				require.NoError(t, spec.Validate(g.Spec), "a template with no blank it needs validates as it ships")
			}

			got := filled(g)
			require.NoError(t, spec.Validate(got), "it validates once its blanks are filled")

			data, err := spec.Encode(got)
			require.NoError(t, err)

			_, err = spec.Load(data)
			require.NoError(t, err, "it stores and loads as Save does")
		})
	}
}

func TestEveryBlankIsAPartOfItsTemplate(t *testing.T) {
	t.Parallel()

	for _, g := range Gallery() {
		for _, b := range g.Blanks {
			switch b.Kind {
			case BlankWhere:
				require.Less(t, b.Source, len(g.Spec.Watch.Sources), g.Key)
				assert.NotNil(t, g.Spec.Watch.Sources[b.Source].Search, "%s: a where fills a search", g.Key)
			case BlankChecks:
				require.Less(t, b.Condition, len(g.Spec.Conditions.Of), g.Key)
				field, ok := catalog.FieldByID(g.Spec.Conditions.Of[b.Condition].Field)
				require.True(t, ok, g.Key)
				assert.Equal(t, catalog.FieldFailedNames, field.ID, "%s: check names fill a condition on them", g.Key)
			default:
				t.Errorf("%s: a blank of no kind quick setup asks for: %q", g.Key, b.Kind)
			}

			assert.NotEmpty(t, b.Label, g.Key)
		}
	}
}

func TestEveryTemplateRunsAStarterThisBuildShips(t *testing.T) {
	t.Parallel()

	shipped := map[string]bool{}
	for _, st := range prompts.Starters() {
		shipped[st.Key] = true
	}

	for _, g := range Gallery() {
		for _, ref := range promptRefs(g.Spec) {
			assert.NotEmpty(t, ref.Starter, "%s names its prompts by starter, which every Mac has", g.Key)
			assert.True(t, shipped[ref.Starter], "%s runs %q, which no starter is", g.Key, ref.Starter)
		}
	}
}

// fixtureEnv is what the fixtures' facts are built with.
func fixtureEnv() facts.Env {
	return facts.Env{
		Now: galleryNow, Viewer: galleryViewer, Trust: trust.NewPolicy(trust.DefaultBots()),
		Sensitive: sensitive.DefaultConfig(),
	}
}

// pr is a pull request of the viewer's repository, with its details, checks, and files
// read, every check passed and settled.
func pr(author, authorType string) facts.Row {
	return facts.Row{
		ID: 7, Kind: item.KindPR, Repo: repo.Name{Owner: galleryViewer, Repo: "example-repo"}, Number: 88,
		Title: "Make the widget faster", URL: "https://github.com/octo-user/example-repo/pull/88",
		Author: author, AuthorType: authorType, Association: "MEMBER", HeadRef: "widget",
		CreatedAt: galleryNow.Add(-72 * time.Hour), UpdatedAt: galleryNow.Add(-time.Hour),
		Repository: facts.RepoRow{Read: true, Visibility: "PRIVATE", Permission: "ADMIN", AllowUpdateBranch: true},
		Details: &facts.DetailsRow{
			HeadOID: galleryHead, BaseRef: "main", Mergeable: "MERGEABLE", MergeState: "CLEAN",
			ReviewDecision: "REVIEW_REQUIRED", ChecksState: "SUCCESS", ChecksPassed: 2, Additions: 40, Deletions: 8,
			ChangedFiles: 2, Commits: 1,
		},
		Checks: &facts.ChecksRow{Head: galleryHead, Complete: true, Checks: []facts.CheckRow{
			{Name: "test", Workflow: "CI", Outcome: facts.CheckPassed, CompletedAt: galleryNow.Add(-30 * time.Minute)},
			{Name: "lint", Workflow: "CI", Outcome: facts.CheckPassed, CompletedAt: galleryNow.Add(-25 * time.Minute)},
		}},
		Files: &facts.FilesRow{Head: galleryHead, Complete: true, Files: []sensitive.File{{Path: "widget.go"}, {Path: "widget_test.go"}}},
	}
}

// dependabot is a Dependabot pull request bumping a module a minor version.
func dependabot() facts.Row {
	r := pr("dependabot", trust.TypeBot)
	r.Title, r.Association = "build(deps): bump golang.org/x/net from 0.30.0 to 0.31.0", "NONE"
	r.HeadRef = "dependabot/go_modules/golang.org/x/net-0.31.0"
	r.Files.Files = []sensitive.File{{Path: "go.mod"}, {Path: "go.sum"}}

	return r
}

// quietBug is an issue assigned to the viewer, labeled bug, with no word on it for days.
func quietBug(days int, labels ...string) facts.Row {
	quiet := galleryNow.Add(-time.Duration(days) * 24 * time.Hour)

	return facts.Row{
		ID: 9, Kind: item.KindIssue, Repo: repo.Name{Owner: galleryViewer, Repo: "example-repo"}, Number: 12,
		Title: "The widget crashes on start", URL: "https://github.com/octo-user/example-repo/issues/12",
		Author: "octo-reporter", AuthorType: "User", Association: "NONE", InAssigned: true,
		CreatedAt: quiet.Add(-24 * time.Hour), UpdatedAt: quiet,
		Repository: facts.RepoRow{Read: true, Visibility: "PUBLIC", Permission: "ADMIN"},
		Details:    &facts.DetailsRow{Labels: labels, Comments: 1},
	}
}

// failing returns r with its checks failing on names, settled.
func failing(r facts.Row, names ...string) facts.Row {
	r.Details.ChecksState, r.Details.ChecksFailed = "FAILURE", len(names)

	checks := make([]facts.CheckRow, 0, 1+len(names))
	checks = append(checks, facts.CheckRow{
		Name: "test", Workflow: "CI", Outcome: facts.CheckPassed, CompletedAt: galleryNow.Add(-30 * time.Minute),
	})

	for _, n := range names {
		wf, job, _ := strings.Cut(n, " / ")
		checks = append(checks, facts.CheckRow{Name: job, Workflow: wf, Outcome: facts.CheckFailed, CompletedAt: galleryNow.Add(-20 * time.Minute)})
	}

	r.Checks.Checks = checks

	return r
}

// galleryFixtures are, for each template, items it matches and items it doesn't, each
// as the inbox would store it.
func galleryFixtures() map[string]struct{ match, miss []facts.Row } {
	authored := func(r facts.Row) facts.Row { r.InAuthored, r.Author = true, galleryViewer; return r }
	reviewAsked := func(r facts.Row) facts.Row { r.InReviewRequested = true; return r }
	conflicting := func(r facts.Row) facts.Row {
		r.Details.Mergeable, r.Details.MergeState = "CONFLICTING", "DIRTY"
		return r
	}
	behind := func(r facts.Row) facts.Row { r.Details.MergeState = "BEHIND"; return r }
	changesAsked := func(r facts.Row) facts.Row { r.Details.ReviewDecision = "CHANGES_REQUESTED"; return r }
	draft := func(r facts.Row) facts.Row { r.Draft = true; return r }
	major := func(r facts.Row) facts.Row {
		r.Title = "build(deps): bump golang.org/x/net from 0.30.0 to 1.0.0"
		return r
	}

	return map[string]struct{ match, miss []facts.Row }{
		"fix-dependabot-security": {
			match: []facts.Row{
				failing(dependabot(), "🔒 Security Scans / 🔒 Security Scans"), failing(dependabot(), "CodeQL / Analyze (go)"),
			},
			miss: []facts.Row{failing(dependabot(), "CI / test (go 1.26)"), dependabot()},
		},
		"heal-dependabot-then-merge": {
			match: []facts.Row{failing(dependabot(), "CI / test (go 1.26)")},
			miss:  []facts.Row{failing(major(dependabot()), "CI / test (go 1.26)"), dependabot()},
		},
		"review-members": {
			match: []facts.Row{pr("octo-member", "User")},
			miss: []facts.Row{func() facts.Row {
				r := pr("octo-member", "User")
				r.Files.Files = append(r.Files.Files, sensitive.File{Path: ".github/workflows/ci.yml"})

				return r
			}(), func() facts.Row {
				r := pr("octo-member", "User")
				r.Details.Additions = 900

				return r
			}()},
		},
		"snooze-passing-bot-prs": {
			match: []facts.Row{reviewAsked(dependabot())},
			miss:  []facts.Row{reviewAsked(major(dependabot())), reviewAsked(failing(dependabot(), "CI / test"))},
		},
		"tell-me-about-security-failures": {
			match: []facts.Row{failing(dependabot(), "govulncheck / scan")},
			miss:  []facts.Row{failing(dependabot(), "CI / test")},
		},
		"address-requested-changes": {
			match: []facts.Row{changesAsked(authored(pr("x", "User")))},
			miss:  []facts.Row{authored(pr("x", "User"))},
		},
		"resolve-merge-conflicts": {
			match: []facts.Row{conflicting(authored(pr("x", "User")))},
			miss:  []facts.Row{draft(conflicting(authored(pr("x", "User")))), authored(pr("x", "User"))},
		},
		"re-run-flaky-checks-once": {
			match: []facts.Row{failing(authored(pr("x", "User")), "e2e / browser"), failing(dependabot(), "e2e / browser")},
			miss:  []facts.Row{failing(authored(pr("x", "User")), "CI / test")},
		},
		"ask-dependabot-to-rebase": {
			match: []facts.Row{conflicting(dependabot())},
			miss:  []facts.Row{dependabot()},
		},
		"ask-for-a-repro-on-quiet-bugs": {
			match: []facts.Row{quietBug(15, "bug")},
			miss:  []facts.Row{quietBug(15, "bug", "needs-repro"), quietBug(15, "enhancement"), quietBug(3, "bug")},
		},
		"update-my-prs-when-behind": {
			match: []facts.Row{behind(authored(pr("x", "User")))},
			miss:  []facts.Row{draft(behind(authored(pr("x", "User")))), authored(pr("x", "User"))},
		},
		"fix-failing-checks-on-my-prs": {
			match: []facts.Row{failing(authored(pr("x", "User")), "CI / test")},
			miss:  []facts.Row{draft(failing(authored(pr("x", "User")), "CI / test")), authored(pr("x", "User"))},
		},
	}
}

func TestEveryTemplateMatchesItsFixtureItems(t *testing.T) {
	t.Parallel()

	fixtures := galleryFixtures()

	for _, g := range Gallery() {
		t.Run(g.Key, func(t *testing.T) {
			t.Parallel()

			fx, ok := fixtures[g.Key]
			require.True(t, ok, "every template has fixture items")
			require.NotEmpty(t, fx.match)

			sp := filled(g)

			for i, row := range fx.match {
				f := facts.Build(row, fixtureEnv())
				d := eval.Evaluate(sp, f, eval.History{}, galleryNow)
				assert.Equal(t, eval.Yes, d.Match, "match %d: %+v", i, d.Trace)
				assert.NotEmpty(t, d.FireKey)
				assert.True(t, inWatch(sp, f), "match %d is in what the template watches", i)
			}

			for i, row := range fx.miss {
				d := eval.Evaluate(sp, facts.Build(row, fixtureEnv()), eval.History{}, galleryNow)
				assert.Equal(t, eval.No, d.Match, "miss %d: %+v", i, d.Trace)
			}
		})
	}
}

// inWatch reports an item that one of sp's sources holds, as far as its facts tell.
func inWatch(sp spec.Spec, f facts.Facts) bool {
	for _, src := range sp.Watch.Sources {
		for _, l := range src.Lists {
			if slices.Contains(f.Value(catalog.FieldLists).List, string(l)) {
				return true
			}
		}

		if src.Search != nil && searchMiss(*src.Search, f, galleryViewer) == "" {
			return true
		}
	}

	return false
}

func TestEveryTemplatesSentenceIsPinned(t *testing.T) {
	t.Parallel()

	names := func(p spec.PromptRef) string {
		for _, st := range prompts.Starters() {
			if st.Key == p.Starter {
				return st.Nickname
			}
		}

		return ""
	}

	for _, g := range Gallery() {
		t.Run(g.Key, func(t *testing.T) {
			t.Parallel()

			got := spec.Sentence(filled(g), names) + "\n"
			path := filepath.Join("testdata", "gallery", g.Key+".golden")

			if *updateGallery {
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
				require.NoError(t, os.WriteFile(path, []byte(got), 0o600))

				return
			}

			want, err := os.ReadFile(path) //nolint:gosec // a fixture's name
			require.NoError(t, err, "rewrite with go test ./internal/autopilot -run TestEveryTemplatesSentenceIsPinned -update")
			assert.Equal(t, string(want), got)
		})
	}
}

// allOffers offers every step, action, and place, as a build with every part does.
func allOffers() *Offers {
	o := &Offers{}

	for _, s := range catalog.Steps() {
		o.Steps = append(o.Steps, s.Kind)
	}

	o.Actions = []catalog.GitHubActionKind{
		catalog.GitHubRebase, catalog.GitHubUpdateBranch, catalog.GitHubReply, catalog.GitHubLabels,
	}

	for _, p := range catalog.Places() {
		o.Places = append(o.Places, p.ID)
	}

	return o
}

func TestATemplateSaysWhatThisBuildCantRunOfItYet(t *testing.T) {
	t.Parallel()

	flagship, ok := TemplateByKey("fix-dependabot-security")
	require.True(t, ok)

	terminalOnly := &Offers{
		Steps: []catalog.StepKind{catalog.StepNotify, catalog.StepAgent, catalog.StepTellMe}, Places: []catalog.Where{catalog.WhereTerminal},
	}
	assert.Equal(t, []string{"agents in the background", "“Wait for the checks”"}, terminalOnly.Needs(flagship.Spec))
	assert.Equal(t,
		"It needs agents in the background and “Wait for the checks”, which this version of "+brand.Name+" can't run yet.",
		draft.NeedsWords(terminalOnly.Needs(flagship.Spec)))

	rebase, ok := TemplateByKey("ask-dependabot-to-rebase")
	require.True(t, ok)
	assert.Equal(t, []string{"“GitHub actions”"}, terminalOnly.Needs(rebase.Spec))

	stepButNoAction := allOffers()
	stepButNoAction.Actions = nil
	assert.Equal(t, []string{"“Ask Dependabot to rebase”"}, stepButNoAction.Needs(rebase.Spec),
		"a GitHub step whose action isn't offered names the action")

	// Once every part lands, every template simply works, with no change to it.
	for _, g := range Gallery() {
		assert.Empty(t, allOffers().Needs(g.Spec), g.Key)
	}

	// What this build runs, its templates run as they ship: the local steps, the notices,
	// an agent in the terminal or the background, and the GitHub step's actions.
	for _, key := range []string{
		"snooze-passing-bot-prs", "tell-me-about-security-failures", "address-requested-changes",
		"ask-dependabot-to-rebase", "update-my-prs-when-behind", "ask-for-a-repro-on-quiet-bugs",
		"resolve-merge-conflicts", "re-run-flaky-checks-once",
	} {
		g, found := TemplateByKey(key)
		require.True(t, found, key)
		assert.Empty(t, draft.CatalogOffers().Needs(g.Spec), "%s runs in this build", key)
	}
}

func TestTemplatesAreSuggestedOnlyByWhatThisMacKnows(t *testing.T) {
	t.Parallel()

	assert.Empty(t, suggestedWhy([]SignalKind{SignalFortress, SignalDependabot}, TemplateSignals{}),
		"nothing known suggests nothing")
	assert.Equal(t, "3 of your clones run go-fortress’s security checks.",
		suggestedWhy([]SignalKind{SignalFortress, SignalDependabot}, TemplateSignals{Fortress: 3, Dependabot: 9}))
	assert.Equal(t, "Dependabot opened 9 pull requests in your inbox this week.",
		suggestedWhy([]SignalKind{SignalFortress, SignalDependabot}, TemplateSignals{Dependabot: 9}))
	assert.Equal(t, "One of your clones runs go-fortress’s security checks.",
		suggestedWhy([]SignalKind{SignalFortress}, TemplateSignals{Fortress: 1}))
	assert.Empty(t, suggestedWhy(nil, TemplateSignals{Fortress: 3, Dependabot: 9, Bots: 2}),
		"a template nothing suggests is never suggested")
}

func TestARoutineFromAnItemFillsInWhatTheItemSays(t *testing.T) {
	t.Parallel()

	f := facts.Build(failing(dependabot(), "CodeQL / Analyze (go)"), fixtureEnv())
	sp := itemSpec(f, galleryViewer, draft.CatalogOffers())

	require.Len(t, sp.Watch.Sources, 1)
	assert.Equal(t, item.KindPR, sp.Watch.Sources[0].Search.Kind)
	assert.Equal(t, []string{"app/dependabot"}, sp.Watch.Sources[0].Search.Authors)
	assert.Equal(t, []string{"@me"}, sp.Watch.Sources[0].Search.Where, "the viewer's own repository is your account")
	assert.Equal(t, catalog.EventChecksFailed, sp.Trigger.Event)
	require.Len(t, sp.Conditions.Of, 1)
	assert.Equal(t, catalog.FieldFailedNames, sp.Conditions.Of[0].Field)
	assert.Equal(t, catalog.OpAnyContains, sp.Conditions.Of[0].Op)
	assert.Equal(t, []string{"CodeQL / Analyze (go)"}, sp.Conditions.Of[0].Values)
	require.Len(t, sp.Steps, 1)
	assert.True(t, sp.Steps[0].Agent.Prompt.ItemDefault, "the prompt a click would launch")
	assert.Equal(t, catalog.WhereBackground, sp.Steps[0].Agent.Place(), "in the background, where this build runs agents")
	terminalOnly := &Offers{Steps: []catalog.StepKind{catalog.StepAgent}, Places: []catalog.Where{catalog.WhereTerminal}}
	assert.Equal(t, catalog.WhereTerminal, itemSpec(f, galleryViewer, terminalOnly).Steps[0].Agent.Place(),
		"in the terminal, in a build that runs agents there alone")
	require.NoError(t, spec.Validate(sp))

	d := eval.Evaluate(sp, f, eval.History{}, galleryNow)
	assert.Equal(t, eval.Yes, d.Match, "the item it came from matches it")

	assert.Equal(t, []spec.Clause{{Field: catalog.FieldUpdateType, Op: catalog.OpOneOf, Values: []string{"minor"}}},
		itemSuggestions(f), "its update type, unticked")
	assert.Equal(t, "Dependabot’s pull requests in your account", itemRoutineName(f, galleryViewer))

	other := pr("octo-other", "User")
	other.Repo = repo.Name{Owner: "acme-org", Repo: "api"}
	other.Details.Mergeable = "CONFLICTING"
	other.Details.Labels = []string{"bug"}
	of := facts.Build(other, fixtureEnv())
	osp := itemSpec(of, galleryViewer, draft.CatalogOffers())
	assert.Equal(t, []string{"org:acme-org"}, osp.Watch.Sources[0].Search.Where)
	assert.Equal(t, []string{"octo-other"}, osp.Watch.Sources[0].Search.Authors)
	assert.Equal(t, catalog.EventMergeConflicts, osp.Trigger.Event)
	assert.Empty(t, osp.Conditions.Of)
	assert.Equal(t, []spec.Clause{{Field: catalog.FieldLabels, Op: catalog.OpHasAny, Values: []string{"bug"}}},
		itemSuggestions(of))
	assert.Equal(t, "octo-other’s pull requests in acme-org", itemRoutineName(of, galleryViewer))

	changes := pr("octo-other", "User")
	changes.Details.ReviewDecision = "CHANGES_REQUESTED"
	assert.Equal(t, catalog.EventChangesRequested,
		itemSpec(facts.Build(changes, fixtureEnv()), galleryViewer, draft.CatalogOffers()).Trigger.Event)
	assert.Equal(t, catalog.EventAppears,
		itemSpec(facts.Build(pr("octo-other", "User"), fixtureEnv()), galleryViewer, draft.CatalogOffers()).Trigger.Event)
}

func TestWhyAnItemIsntInTheWatch(t *testing.T) {
	t.Parallel()

	flagship, ok := TemplateByKey("fix-dependabot-security")
	require.True(t, ok)

	person := pr("octo-other", "User")
	assert.Equal(t, []string{"Not in its search: its author is octo-other, not Dependabot."},
		watchMisses(flagship.Spec, facts.Build(person, fixtureEnv()), galleryViewer))

	elsewhere := dependabot()
	elsewhere.Repo = repo.Name{Owner: "acme-labs", Repo: "web"}
	assert.Equal(t, []string{"Not in its search: it's in acme-labs/web, not your account."},
		watchMisses(flagship.Spec, facts.Build(elsewhere, fixtureEnv()), galleryViewer))

	assert.Equal(t,
		[]string{"Its search should find it, but the last refresh didn't bring it: it shows once a refresh reads the search again."},
		watchMisses(flagship.Spec, facts.Build(dependabot(), fixtureEnv()), galleryViewer))

	snooze, ok := TemplateByKey("snooze-passing-bot-prs")
	require.True(t, ok)

	authored := dependabot()
	authored.InAuthored = true
	assert.Equal(t, []string{"It isn't asking for your review or assigned to you: it's in your inbox, opened by you."},
		watchMisses(snooze.Spec, facts.Build(authored, fixtureEnv()), galleryViewer))

	review, ok := TemplateByKey("review-members")
	require.True(t, ok)

	drafted := pr("octo-member", "User")
	drafted.Draft = true
	assert.Equal(t, []string{"Not in its search: it's a draft, and the search finds pull requests out of draft."},
		watchMisses(review.Spec, facts.Build(drafted, fixtureEnv()), galleryViewer))
}

func TestACleanApprovalIsOneThatRanToItsEndWithNothingFailing(t *testing.T) {
	t.Parallel()

	clean := `[{"step":{"agent":{}},"status":"done","outcome":"pushed"},{"step":{"tellMe":{}},"status":"done"}]`
	assert.True(t, cleanApprovalOf(true, StateDone, ReasonRan, clean))
	assert.False(t, cleanApprovalOf(false, StateDone, ReasonRan, clean), "one that ran without the user's OK")
	assert.False(t, cleanApprovalOf(true, StateDismissed, ReasonYouSkipped, clean), "one the user skipped")
	assert.False(t, cleanApprovalOf(true, StateSkipped, ReasonTries, clean), "one that ran out of tries")
	assert.False(t, cleanApprovalOf(true, StateDone, ReasonRan, `[{"step":{"agent":{}},"status":"failed"}]`))
	assert.False(t, cleanApprovalOf(true, StateDone, ReasonRan, `[{"step":{"agent":{}},"status":"done","outcome":"notPushed"}]`),
		"an agent that pushed nothing")
	assert.False(t, cleanApprovalOf(true, StateDone, ReasonRan, `[{"step":{"waitForChecks":{}},"status":"done","outcome":"stillFails"}]`),
		"checks that still fail")
}

// cleanApprovalOf is cleanApproval of a match with those columns.
func cleanApprovalOf(approved bool, state State, reason Reason, steps string) bool {
	return cleanApproval(queries.ListRoutineAskedRow{Approved: approved, State: string(state), Reason: string(reason), Steps: steps})
}

func TestTheGallerysModesAreHintsAlone(t *testing.T) {
	t.Parallel()

	for _, g := range Gallery() {
		assert.Contains(t, []autopilotprefs.Mode{autopilotprefs.ModeAsk, autopilotprefs.ModeAuto}, g.Suggests, g.Key)
	}
}
