package eval_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/eval"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/trust"
)

// now is the moment the tests decide at.
var now = time.Date(2026, time.November, 16, 12, 0, 0, 0, time.UTC) //nolint:gochecknoglobals // a fixed test moment

// head is the pull requests' head commit.
const head = "0a1b2c3d4e5f60718293a4b5c6d7e8f901234567"

func ptr[T any](v T) *T { return &v }

// prRow is a Dependabot pull request whose two security checks failed and settled.
func prRow() facts.Row {
	return facts.Row{
		ID: 7, Kind: item.KindPR, Repo: repo.Name{Owner: "acme-org", Repo: "example-repo"}, Number: 212,
		Title:  "build(deps): bump golang.org/x/net from 0.30.0 to 0.31.0",
		Author: "dependabot", AuthorType: trust.TypeBot, Association: "NONE",
		HeadRef: "dependabot/go_modules/golang.org/x/net-0.31.0", CreatedAt: now.Add(-72 * time.Hour),
		UpdatedAt: now.Add(-90 * time.Minute), InReviewRequested: true,
		Repository: facts.RepoRow{Read: true, Visibility: "PUBLIC", Permission: "ADMIN"},
		Details: &facts.DetailsRow{
			HeadOID: head, BaseRef: "main", Mergeable: "MERGEABLE", MergeState: "CLEAN", ReviewDecision: "REVIEW_REQUIRED",
			ChecksState: "FAILURE", ChecksPassed: 10, ChecksFailed: 2, Additions: 12, Deletions: 3, Comments: 4,
			Labels: []string{"dependencies"},
		},
		Checks: &facts.ChecksRow{Head: head, Complete: true, Checks: []facts.CheckRow{
			{Name: "Analyze (go)", Workflow: "CodeQL", Outcome: facts.CheckFailed, CompletedAt: now.Add(-10 * time.Minute)},
			{Name: "🔒 Security Scans", Workflow: "🔒 Security Scans", Outcome: facts.CheckFailed, CompletedAt: now.Add(-6 * time.Minute)},
			{Name: "test", Workflow: "CI", Outcome: facts.CheckPassed, CompletedAt: now.Add(-20 * time.Minute)},
		}},
		Files: &facts.FilesRow{Head: head, Complete: true, Files: []sensitive.File{{Path: "go.mod"}, {Path: "go.sum"}}},
	}
}

// build makes a row's facts for the signed-in user.
func build(r facts.Row) facts.Facts {
	return facts.Build(r, facts.Env{
		Now: now, Viewer: "octo-user", Trust: trust.NewPolicy(trust.DefaultBots()), Sensitive: sensitive.DefaultConfig(),
	})
}

// template loads one of the spec package's template fixtures.
func template(t *testing.T, name string) spec.Spec {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("..", "spec", "testdata", "templates", name+".json")) //nolint:gosec // a fixture
	require.NoError(t, err)

	s, err := spec.Load(data)
	require.NoError(t, err)

	return s
}

// routine returns a valid routine with trigger t and conditions c, on Dependabot's pull
// requests in acme-org.
func routine(t spec.Trigger, c spec.Conditions) spec.Spec {
	if c.Match == "" {
		c.Match = spec.MatchAll
	}

	return spec.Spec{
		Version: spec.Version,
		Watch:   spec.Watch{Sources: []spec.Source{{Lists: []catalog.ListSource{catalog.SourceAuthored}}}},
		Trigger: t, Conditions: c, Steps: []spec.Step{{Pin: &spec.Empty{}}},
	}
}

// itemFacts are facts about an item of kind with one field's value.
func itemFacts(kind item.Kind, id catalog.FieldID, v facts.Value) facts.Facts {
	return facts.Facts{
		Item: facts.Item{ID: 1, Kind: kind}, Values: map[catalog.FieldID]facts.Value{id: v}, Updated: now,
	}
}

// decide evaluates one condition.
func decide(c spec.Clause, f facts.Facts, h eval.History) eval.Result {
	r, _ := eval.Condition(c, f, h, now)

	return r
}
