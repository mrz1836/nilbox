package facts_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/trust"
)

// The moments and commits the rows use.
var (
	now     = time.Date(2026, time.November, 16, 12, 0, 0, 0, time.UTC) //nolint:gochecknoglobals // a fixed test moment
	created = now.Add(-72 * time.Hour)                                  //nolint:gochecknoglobals // a fixed test moment
	updated = now.Add(-90 * time.Minute)                                //nolint:gochecknoglobals // a fixed test moment
)

const (
	head  = "0a1b2c3d4e5f60718293a4b5c6d7e8f901234567"
	older = "ffffffffffffffffffffffffffffffffffffffff"
)

func ptr[T any](v T) *T { return &v }

// prRow is a Dependabot pull request with every part read.
func prRow() facts.Row {
	return facts.Row{
		ID: 7, Kind: item.KindPR, Repo: repo.Name{Owner: "acme-org", Repo: "example-repo"}, Number: 212,
		Title:  "build(deps): bump golang.org/x/net from 0.30.0 to 0.31.0",
		URL:    "https://github.com/acme-org/example-repo/pull/212",
		Author: "dependabot", AuthorType: trust.TypeBot, Association: "NONE",
		HeadRef: "dependabot/go_modules/golang.org/x/net-0.31.0", CreatedAt: created, UpdatedAt: updated,
		InReviewRequested: true,
		Repository: facts.RepoRow{
			Read: true, Visibility: "PUBLIC", Permission: "ADMIN", Language: ptr("Go"), HasClone: ptr(true),
			AllowUpdateBranch: true,
		},
		Details: &facts.DetailsRow{
			HeadOID: head, BaseRef: "main", Mergeable: "MERGEABLE", MergeState: "BEHIND", ReviewDecision: "REVIEW_REQUIRED",
			ChecksState: "FAILURE", ChecksPassed: 10, ChecksFailed: 2, Additions: 12, Deletions: 3, ChangedFiles: 2,
			Commits: 1, Comments: 4, Labels: []string{"dependencies", "go"}, ReviewUsers: []string{"octo-user"},
		},
		Checks: &facts.ChecksRow{Head: head, Complete: true, Checks: []facts.CheckRow{
			{Name: "Analyze (go)", Workflow: "CodeQL", Outcome: facts.CheckFailed, Required: ptr(true), CompletedAt: now.Add(-10 * time.Minute)},
			{Name: "🔒 Security Scans", Workflow: "🔒 Security Scans", Outcome: facts.CheckFailed, CompletedAt: now.Add(-6 * time.Minute)},
			{Name: "test", Workflow: "CI", Outcome: facts.CheckPassed, Required: ptr(true), CompletedAt: now.Add(-20 * time.Minute)},
		}},
		Files: &facts.FilesRow{Head: head, Complete: true, Files: []sensitive.File{{Path: "go.mod"}, {Path: "go.sum"}}},
	}
}

// env is the signed-in user's settings.
func env() facts.Env {
	return facts.Env{
		Now: now, Viewer: "octo-user", Trust: trust.NewPolicy(trust.DefaultBots()), Sensitive: sensitive.DefaultConfig(),
	}
}

func TestBuildAPullRequest(t *testing.T) {
	t.Parallel()

	f := facts.Build(prRow(), env())

	want := map[catalog.FieldID]facts.Value{
		catalog.FieldKind:             facts.Text("pr"),
		catalog.FieldTitle:            facts.Text("build(deps): bump golang.org/x/net from 0.30.0 to 0.31.0"),
		catalog.FieldLabels:           facts.List([]string{"dependencies", "go"}, false),
		catalog.FieldAuthor:           facts.Text("dependabot"),
		catalog.FieldAuthorIsBot:      facts.Bool(true),
		catalog.FieldTrust:            facts.Text("bot"),
		catalog.FieldAssociation:      facts.Text("NONE"),
		catalog.FieldAge:              facts.Since(created),
		catalog.FieldIdle:             facts.Since(updated),
		catalog.FieldComments:         facts.Number(4),
		catalog.FieldLists:            facts.List([]string{"reviewRequested"}, false),
		catalog.FieldSnoozed:          facts.Bool(false),
		catalog.FieldDismissed:        facts.Bool(false),
		catalog.FieldPinned:           facts.Bool(false),
		catalog.FieldMine:             facts.Bool(false),
		catalog.FieldRepoOwner:        facts.Text("acme-org"),
		catalog.FieldRepoName:         facts.Text("example-repo"),
		catalog.FieldRepoFullName:     facts.Text("acme-org/example-repo"),
		catalog.FieldVisibility:       facts.Text("public"),
		catalog.FieldLanguage:         facts.Text("Go"),
		catalog.FieldFork:             facts.Bool(false),
		catalog.FieldPermission:       facts.Text("admin"),
		catalog.FieldHasClone:         facts.Bool(true),
		catalog.FieldRepoMuted:        facts.Bool(false),
		catalog.FieldRepoPinned:       facts.Bool(false),
		catalog.FieldUpdateBranch:     facts.Bool(true),
		catalog.FieldDraft:            facts.Bool(false),
		catalog.FieldBase:             facts.Text("main"),
		catalog.FieldHead:             facts.Text("dependabot/go_modules/golang.org/x/net-0.31.0"),
		catalog.FieldFromFork:         facts.Bool(false),
		catalog.FieldSize:             facts.Text("S"),
		catalog.FieldLines:            facts.Number(15),
		catalog.FieldFiles:            facts.Number(2),
		catalog.FieldCommits:          facts.Number(1),
		catalog.FieldConflicts:        facts.Bool(false),
		catalog.FieldBehind:           facts.Bool(true),
		catalog.FieldReviewDecision:   facts.Text("reviewRequired"),
		catalog.FieldApprovals:        facts.Number(0),
		catalog.FieldChangesRequested: facts.Number(0),
		catalog.FieldAsksMe:           facts.Bool(true),
		catalog.FieldAutoMerge:        facts.Bool(false),
		catalog.FieldChecksState:      facts.Text("failure"),
		catalog.FieldChecksFailed:     facts.Number(2),
		catalog.FieldChecksPending:    facts.Number(0),
		catalog.FieldChecksPassed:     facts.Number(10),
		catalog.FieldFailedNames:      facts.List([]string{"CodeQL / Analyze (go)", "🔒 Security Scans / 🔒 Security Scans"}, false),
		catalog.FieldCheckNames: facts.List([]string{
			"CodeQL / Analyze (go)", "🔒 Security Scans / 🔒 Security Scans", "CI / test",
		}, false),
		catalog.FieldRequiredFailed:   facts.Bool(true),
		catalog.FieldAwaitingApproval: facts.Bool(false),
		catalog.FieldPaths:            facts.List([]string{"go.mod", "go.sum"}, false),
		catalog.FieldSensitive:        facts.List([]string{"dependencies"}, false),
		catalog.FieldDepTool:          facts.Text("dependabot"),
		catalog.FieldEcosystem:        facts.Text("gomod"),
		catalog.FieldPackage:          facts.Text("golang.org/x/net"),
		catalog.FieldFrom:             facts.Text("0.30.0"),
		catalog.FieldTo:               facts.Text("0.31.0"),
		catalog.FieldUpdateType:       facts.Text("minor"),
		catalog.FieldGrouped:          facts.Bool(false),
		catalog.FieldSecurity:         facts.Bool(false),
	}

	for id, v := range want {
		assert.Equal(t, v, f.Value(id), id)
	}

	for _, field := range catalog.Fields() {
		if field.Group == catalog.GroupHistory {
			continue
		}

		_, ok := want[field.ID]
		assert.True(t, ok, "the test covers %s", field.ID)
	}

	assert.Equal(t, facts.Text(head), f.Head)
	assert.Equal(t, facts.Since(now.Add(-6*time.Minute)), f.ChecksDone, "the newest completion time")
	assert.Equal(t, facts.Item{
		ID: 7, Kind: item.KindPR, Repo: "acme-org/example-repo", Number: 212,
		Title: "build(deps): bump golang.org/x/net from 0.30.0 to 0.31.0", URL: "https://github.com/acme-org/example-repo/pull/212",
	}, f.Item)
	assert.Equal(t, updated, f.Updated)
	assert.Equal(t, facts.StatusUnknown, f.ReviewAsked.Status, "the row doesn't say when the review was asked for")
}

func TestBuildUnknownUntilRead(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*facts.Row)
		field  catalog.FieldID
		need   facts.Need
	}{
		{name: "details not read", mutate: func(r *facts.Row) { r.Details = nil }, field: catalog.FieldLabels, need: facts.NeedDetails},
		{name: "details not read, so no head", mutate: func(r *facts.Row) { r.Details = nil }, field: catalog.FieldFailedNames, need: facts.NeedDetails},
		{name: "check names not read", mutate: func(r *facts.Row) { r.Checks = nil }, field: catalog.FieldFailedNames, need: facts.NeedChecks},
		{
			name: "check names read at an older commit", mutate: func(r *facts.Row) { r.Checks.Head = older },
			field: catalog.FieldCheckNames, need: facts.NeedChecks,
		},
		{name: "files not checked", mutate: func(r *facts.Row) { r.Files = nil }, field: catalog.FieldSensitive, need: facts.NeedFiles},
		{
			name: "the file check failed", mutate: func(r *facts.Row) { r.Files.Error = "GitHub timed out" },
			field: catalog.FieldPaths, need: facts.NeedFiles,
		},
		{
			name: "files checked at an older commit", mutate: func(r *facts.Row) { r.Files.Head = older },
			field: catalog.FieldPaths, need: facts.NeedFiles,
		},
		{name: "mergeability not worked out", mutate: func(r *facts.Row) { r.Details.Mergeable = "UNKNOWN" }, field: catalog.FieldConflicts, need: facts.NeedDetails},
		{name: "the merge state not read", mutate: func(r *facts.Row) { r.Details.MergeState = "" }, field: catalog.FieldBehind, need: facts.NeedDetails},
		{name: "the base not read", mutate: func(r *facts.Row) { r.Details.BaseRef = "" }, field: catalog.FieldBase, need: facts.NeedDetails},
		{name: "the repository not read", mutate: func(r *facts.Row) { r.Repository.Read = false }, field: catalog.FieldVisibility},
		{name: "the language not read", mutate: func(r *facts.Row) { r.Repository.Language = nil }, field: catalog.FieldLanguage},
		{name: "no clone known", mutate: func(r *facts.Row) { r.Repository.HasClone = nil }, field: catalog.FieldHasClone},
		{name: "no account", mutate: func(r *facts.Row) { r.Author = "x" }, field: catalog.FieldMine},
		{name: "the author's type not read", mutate: func(r *facts.Row) { r.AuthorType = "" }, field: catalog.FieldAuthorIsBot},
		{name: "the association not read", mutate: func(r *facts.Row) { r.Association = "" }, field: catalog.FieldAssociation},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			row := prRow()
			tt.mutate(&row)

			e := env()
			if tt.field == catalog.FieldMine {
				e.Viewer = ""
			}

			v := facts.Build(row, e).Value(tt.field)
			assert.Equal(t, facts.StatusUnknown, v.Status)
			assert.Equal(t, tt.need, v.Need)
			assert.NotEmpty(t, v.Why)
		})
	}
}

func TestBuildWithoutDetailsHasNoHead(t *testing.T) {
	t.Parallel()

	row := prRow()
	row.Details = nil

	f := facts.Build(row, env())
	assert.Equal(t, facts.NeedDetails, f.Head.Need)
	assert.Equal(t, facts.NeedDetails, f.ChecksDone.Need)
}

func TestBuildChecks(t *testing.T) {
	t.Parallel()

	finished := now.Add(-3 * time.Minute)

	tests := []struct {
		name     string
		checks   facts.ChecksRow
		failed   facts.Value
		names    facts.Value
		required facts.Value
		done     facts.Status
	}{
		{
			name: "a name run twice, once failing and once skipped, fails once",
			checks: facts.ChecksRow{Head: head, Complete: true, Checks: []facts.CheckRow{
				{Name: "lint", Workflow: "CI", Outcome: facts.CheckSkipped, CompletedAt: finished.Add(-time.Hour)},
				{Name: "lint", Workflow: "CI", Outcome: facts.CheckFailed, Required: ptr(false), CompletedAt: finished},
			}},
			failed: facts.List([]string{"CI / lint"}, false), names: facts.List([]string{"CI / lint"}, false),
			required: facts.Bool(false), done: facts.StatusKnown,
		},
		{
			name: "an app's run and a status have no workflow",
			checks: facts.ChecksRow{Head: head, Complete: true, Checks: []facts.CheckRow{
				{Name: "CodeQL", Outcome: facts.CheckFailed, CompletedAt: finished},
				{Name: "ci/circleci: build", Outcome: facts.CheckPassed, Required: ptr(true), CompletedAt: finished},
			}},
			failed: facts.List([]string{"CodeQL"}, false), names: facts.List([]string{"CodeQL", "ci/circleci: build"}, false),
			required: facts.Unknown("whether a failed check is required isn't known", facts.NeedNothing),
			done:     facts.StatusKnown,
		},
		{
			name: "a skipped run with no completion time isn't running",
			checks: facts.ChecksRow{Head: head, Complete: true, Checks: []facts.CheckRow{
				{Name: "deploy", Workflow: "CD", Outcome: facts.CheckSkipped},
				{Name: "test", Workflow: "CI", Outcome: facts.CheckPassed, CompletedAt: finished},
			}},
			failed: facts.List([]string{}, false), names: facts.List([]string{"CD / deploy", "CI / test"}, false),
			required: facts.Bool(false), done: facts.StatusKnown,
		},
		{
			name: "a check still running",
			checks: facts.ChecksRow{Head: head, Complete: true, Checks: []facts.CheckRow{
				{Name: "test", Workflow: "CI", Outcome: facts.CheckPending},
			}},
			failed: facts.List([]string{}, false), names: facts.List([]string{"CI / test"}, false),
			required: facts.Bool(false), done: facts.StatusUnknown,
		},
		{
			name: "only some checks read",
			checks: facts.ChecksRow{Head: head, Checks: []facts.CheckRow{
				{Name: "test", Workflow: "CI", Outcome: facts.CheckFailed, Required: ptr(false), CompletedAt: finished},
			}},
			failed: facts.List([]string{"CI / test"}, true), names: facts.List([]string{"CI / test"}, true),
			required: facts.Unknown("whether a failed check is required isn't known", facts.NeedNothing),
			done:     facts.StatusUnknown,
		},
		{
			name:   "no checks",
			checks: facts.ChecksRow{Head: head, Complete: true},
			failed: facts.List([]string{}, false), names: facts.List([]string{}, false),
			required: facts.Bool(false), done: facts.StatusUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			row := prRow()
			row.Checks = &tt.checks

			f := facts.Build(row, env())
			assert.Equal(t, tt.failed, f.Value(catalog.FieldFailedNames))
			assert.Equal(t, tt.names, f.Value(catalog.FieldCheckNames))
			assert.Equal(t, tt.required, f.Value(catalog.FieldRequiredFailed))
			assert.Equal(t, tt.done, f.ChecksDone.Status)

			if tt.done == facts.StatusKnown {
				assert.Equal(t, finished, f.ChecksDone.Since, "settledness comes from the newest completion time")
			}
		})
	}
}

func TestBuildFiles(t *testing.T) {
	t.Parallel()

	row := prRow()
	row.Files = &facts.FilesRow{Head: head, Files: []sensitive.File{
		{Path: "internal/app.go"},
		{Path: "ci/build.yml", PreviousPath: ".github/workflows/build.yml", Change: "renamed"},
		{Path: "CLAUDE.md"},
	}}

	f := facts.Build(row, env())
	assert.Equal(t, facts.List([]string{"internal/app.go", "ci/build.yml", ".github/workflows/build.yml", "CLAUDE.md"}, true),
		f.Value(catalog.FieldPaths), "a renamed file's earlier path counts, and an incomplete list is partial")
	assert.Equal(t, facts.List([]string{"workflows", "agent"}, true), f.Value(catalog.FieldSensitive))
}

func TestBuildDependencyUpdates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*facts.Row)
		want   map[catalog.FieldID]facts.Value
	}{
		{
			name: "a grouped security update",
			mutate: func(r *facts.Row) {
				r.Title = "[Security] Bump the npm_and_yarn group across 2 directories with 3 updates"
				r.HeadRef = "dependabot/npm_and_yarn/multi-1a2b3c4d5e"
			},
			want: map[catalog.FieldID]facts.Value{
				catalog.FieldEcosystem: facts.Text("npm"),
				catalog.FieldGrouped:   facts.Bool(true),
				catalog.FieldSecurity:  facts.Bool(true),
				catalog.FieldPackage:   facts.Unknown("a grouped update names no single package", facts.NeedNothing),
			},
		},
		{
			name: "Renovate's, whose branch names no ecosystem",
			mutate: func(r *facts.Row) {
				r.Author, r.Title, r.HeadRef = "renovate", "Update dependency eslint to v9", "renovate/major-eslint-monorepo"
			},
			want: map[catalog.FieldID]facts.Value{
				catalog.FieldDepTool:    facts.Text("renovate"),
				catalog.FieldEcosystem:  facts.Unknown("the head branch doesn't name an ecosystem", facts.NeedNothing),
				catalog.FieldUpdateType: facts.Text("major"),
				catalog.FieldFrom:       facts.Unknown("the title doesn't say the version it updates from", facts.NeedNothing),
			},
		},
		{
			name:   "a title that doesn't read as an update",
			mutate: func(r *facts.Row) { r.Title = "Fix the login flow" },
			want: map[catalog.FieldID]facts.Value{
				catalog.FieldDepTool:    facts.Text("dependabot"),
				catalog.FieldUpdateType: facts.Unknown("the title doesn't read as a dependency update", facts.NeedNothing),
				catalog.FieldEcosystem:  facts.Text("gomod"),
			},
		},
		{
			name:   "a person's pull request",
			mutate: func(r *facts.Row) { r.Author, r.AuthorType = "octo-user", "User" },
			want: map[catalog.FieldID]facts.Value{
				catalog.FieldDepTool:    facts.Text(catalog.ToolNone),
				catalog.FieldUpdateType: facts.NotApplicable("Dependabot or Renovate didn't open it"),
			},
		},
		{
			name:   "an author whose type isn't read",
			mutate: func(r *facts.Row) { r.AuthorType = "" },
			want: map[catalog.FieldID]facts.Value{
				catalog.FieldDepTool:   facts.Unknown("the author's type isn't read yet", facts.NeedNothing),
				catalog.FieldEcosystem: facts.Unknown("the author's type isn't read yet", facts.NeedNothing),
			},
		},
		{
			name:   "a branch of the other bot's",
			mutate: func(r *facts.Row) { r.HeadRef = "renovate/golang.org-x-net-0.x" },
			want: map[catalog.FieldID]facts.Value{
				catalog.FieldEcosystem: facts.Unknown("the head branch doesn't name an ecosystem", facts.NeedNothing),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			row := prRow()
			tt.mutate(&row)

			f := facts.Build(row, env())
			for id, v := range tt.want {
				assert.Equal(t, v, f.Value(id), id)
			}
		})
	}
}

func TestBuildAnIssue(t *testing.T) {
	t.Parallel()

	row := facts.Row{
		ID: 9, Kind: item.KindIssue, Repo: repo.Name{Owner: "acme-org", Repo: "example-repo"}, Number: 30,
		Title: "Crash on start", Author: "octo-user", AuthorType: "User", Association: "MEMBER", InAssigned: true,
		CreatedAt: created, UpdatedAt: updated, Repository: facts.RepoRow{Read: true, Visibility: "PRIVATE"},
		Details: &facts.DetailsRow{Labels: []string{"bug"}, Comments: 2},
	}

	f := facts.Build(row, env())

	for _, field := range catalog.Fields() {
		v := f.Value(field.ID)
		if !field.Applies(item.KindIssue) {
			assert.Equal(t, facts.NotApplicable("it's an issue"), v, field.ID)
		}
	}

	assert.Equal(t, facts.Text("member"), f.Value(catalog.FieldTrust))
	assert.Equal(t, facts.Bool(true), f.Value(catalog.FieldMine))
	assert.Equal(t, facts.List([]string{"bug"}, false), f.Value(catalog.FieldLabels))
	assert.Equal(t, facts.StatusNotApplicable, f.Head.Status)
}

func TestBuildTrust(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*facts.Row)
		want   facts.Status
		class  string
	}{
		{name: "a trusted bot", mutate: func(*facts.Row) {}, want: facts.StatusKnown, class: "bot"},
		{name: "a member", mutate: func(r *facts.Row) { r.Association, r.AuthorType = "MEMBER", "" }, want: facts.StatusKnown, class: "member"},
		{
			name: "a fork's pull request, whoever opened it", mutate: func(r *facts.Row) { r.CrossRepository, r.AuthorType = true, "" },
			want: facts.StatusKnown, class: "outside",
		},
		{name: "someone who may be a trusted bot", mutate: func(r *facts.Row) { r.AuthorType = "" }, want: facts.StatusUnknown},
		{
			name:   "a person whose association isn't read",
			mutate: func(r *facts.Row) { r.Author, r.AuthorType, r.Association = "octo-user", "User", "" },
			want:   facts.StatusUnknown,
		},
		{
			name:   "a trusted bot whose association isn't read",
			mutate: func(r *facts.Row) { r.Association = "" }, want: facts.StatusKnown, class: "bot",
		},
		{
			name:   "an untrusted bot whose association isn't read",
			mutate: func(r *facts.Row) { r.Author, r.Association = "some-app", "" }, want: facts.StatusKnown, class: "outside",
		},
		{
			name:   "a fork's pull request whose association isn't read",
			mutate: func(r *facts.Row) { r.CrossRepository, r.AuthorType, r.Association = true, "User", "" },
			want:   facts.StatusKnown, class: "outside",
		},
		{name: "a bot not on the list", mutate: func(r *facts.Row) { r.Author = "some-app" }, want: facts.StatusKnown, class: "outside"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			row := prRow()
			tt.mutate(&row)

			v := facts.Build(row, env()).Value(catalog.FieldTrust)
			assert.Equal(t, tt.want, v.Status)
			assert.Equal(t, tt.class, v.Text)
		})
	}
}

func TestBuildTriageAtTheMoment(t *testing.T) {
	t.Parallel()

	row := prRow()
	row.SnoozedUntil = ptr(now.Add(time.Hour))
	row.PinnedAt = ptr(created)

	f := facts.Build(row, env())
	assert.Equal(t, facts.Bool(true), f.Value(catalog.FieldSnoozed))
	assert.Equal(t, facts.Bool(true), f.Value(catalog.FieldPinned))

	row.SnoozedUntil = ptr(now.Add(-time.Hour))
	assert.Equal(t, facts.Bool(false), facts.Build(row, env()).Value(catalog.FieldSnoozed), "a snooze that ended")
}

func TestBuildMakesTextOneLine(t *testing.T) {
	t.Parallel()

	row := prRow()
	row.Title = "Fix\nthe \u202edrawkcab\u202c bug\t" + strings.Repeat("x", 300)
	row.Details.Labels = []string{"needs\nreview", "Needs review"}

	f := facts.Build(row, env())
	title := f.Value(catalog.FieldTitle).Text

	assert.True(t, strings.HasPrefix(title, "Fix the drawkcab bug x"), title)
	assert.Len(t, []rune(title), 200, "a title is capped as a prompt's is")
	assert.Equal(t, facts.List([]string{"needs review"}, false), f.Value(catalog.FieldLabels), "labels once each, ignoring case")
}

func TestValueOfAnUnfilledField(t *testing.T) {
	t.Parallel()

	v := facts.Build(prRow(), env()).Value(catalog.FieldTries)
	assert.Equal(t, facts.StatusUnknown, v.Status, "the history is the ledger's, not the row's")
}
