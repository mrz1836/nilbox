package watch_test

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/watch"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
)

// update makes TestBuildGolden rewrite its golden files from the current output instead
// of comparing against them.
var update = flag.Bool("update", false, "rewrite the golden files from the current output") //nolint:gochecknoglobals // a test flag

// login is the signed-in account the tests build for, and acmeOrg the org most searches
// are in.
const (
	login   = "octo-user"
	acmeOrg = "org:acme-org"
)

// TestBuildGolden compares each search shape's query with its golden file under
// testdata/queries. After a deliberate change to the builder, rewrite them with
// go test ./internal/autopilot/watch -run TestBuildGolden -update, and review the diff.
func TestBuildGolden(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		search watch.Search
	}{
		{
			name:   "dependabot in your account and an org",
			search: watch.Search{Kind: item.KindPR, Authors: []string{"app/dependabot"}, Where: []string{watch.Me, acmeOrg}},
		},
		{
			name: "renovate by its branch in one repository",
			search: watch.Search{
				Kind: item.KindPR, Authors: []string{"app/renovate"}, Where: []string{"repo:acme-org/example-repo"},
				HeadPrefix: "renovate/",
			},
		},
		{
			name: "dependabot by its branch into main",
			search: watch.Search{
				Kind: item.KindPR, Where: []string{acmeOrg}, Base: "main", HeadPrefix: "dependabot/",
			},
		},
		{name: "asking for my review anywhere", search: watch.Search{Kind: item.KindPR, Involving: []watch.Involving{"reviewRequested"}}},
		{name: "mine of any kind", search: watch.Search{Authors: []string{watch.Me}}},
		{
			name: "issues labeled any of two and not a third",
			search: watch.Search{
				Kind: item.KindIssue, Where: []string{"user:octo-user", "org:acme-labs"}, Labels: []string{"bug", "help wanted"},
				NotLabels: []string{"wontfix", "duplicate"},
			},
		},
		{
			name: "ready pull requests with failing checks",
			search: watch.Search{
				Kind: item.KindPR, Where: []string{acmeOrg}, State: []watch.State{watch.StateReady, watch.StateFailing},
			},
		},
		{
			name: "changes requested and assigned to me",
			search: watch.Search{
				Kind: item.KindPR, Involving: []watch.Involving{watch.InvolvingAssigned},
				State: []watch.State{watch.StateChangesRequested},
			},
		},
		{
			name: "qualifiers typed by hand",
			search: watch.Search{
				Kind: item.KindPR, Where: []string{acmeOrg}, More: `language:go label:"good first issue" -is:private`,
			},
		},
		{
			name:   "a label that reads like a qualifier stays one label",
			search: watch.Search{Kind: item.KindIssue, Where: []string{acmeOrg}, Labels: []string{"bug author:x"}},
		},
		{
			name: "mentioning me in two repositories",
			search: watch.Search{
				Where:     []string{"repo:acme-org/api", "repo:acme-org/web"},
				Involving: []watch.Involving{watch.InvolvingMentioned},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			q, err := watch.Build(watch.Normalize(tt.search), login)
			require.NoError(t, err)
			assert.Equal(t, q.Text, q.Key)

			path := filepath.Join("testdata", "queries", strings.ReplaceAll(tt.name, " ", "-")+".golden")
			text := "# The query watch.Build writes for this search, after this header. Rewrite with\n" +
				"# go test ./internal/autopilot/watch -run TestBuildGolden -update\n\n" + q.Text + "\n"

			if *update {
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
				require.NoError(t, os.WriteFile(path, []byte(text), 0o644)) //nolint:gosec // committed test data, world-readable
				require.NoError(t, os.Chmod(path, 0o644))                   //nolint:gosec // committed test data, world-readable
			}

			want, err := os.ReadFile(path) //nolint:gosec // a fixed case name under testdata
			require.NoError(t, err)
			assert.Equal(t, string(want), text)
		})
	}
}

func TestBuildAlwaysAddsTheSearchsOwnQualifiers(t *testing.T) {
	t.Parallel()

	q, err := watch.Build(watch.Search{Where: []string{acmeOrg}}, login)
	require.NoError(t, err)
	assert.Equal(t, "is:open archived:false org:acme-org sort:updated-desc", q.Text)
}

func TestBuildMakesYourAccountTheLogin(t *testing.T) {
	t.Parallel()

	s := watch.Search{Where: []string{watch.Me}}

	q, err := watch.Build(s, "Octo-User")
	require.NoError(t, err)
	assert.Contains(t, q.Text, "user:octo-user")

	other, err := watch.Build(s, "another-user")
	require.NoError(t, err)
	assert.NotEqual(t, q.Key, other.Key, "a search for your account follows whichever account is signed in")

	_, err = watch.Build(s, "")
	require.ErrorIs(t, err, watch.ErrNoAccount)

	_, err = watch.Build(s, "not a login")
	require.ErrorIs(t, err, watch.ErrInvalidValue)
}

func TestBuildRefuses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		search watch.Search
		field  string
		err    error
	}{
		{name: "no scope", search: watch.Search{Kind: item.KindPR, Authors: []string{"app/dependabot"}}, field: "where", err: watch.ErrNoScope},
		{name: "a negated scope", search: watch.Search{More: "-repo:acme-org/api"}, field: "where", err: watch.ErrNoScope},
		{
			name: "a label that would close its quote", search: watch.Search{Where: []string{watch.Me}, Labels: []string{`bug" author:x`}},
			field: "labels[0]", err: watch.ErrInvalidValue,
		},
		{
			name: "a label holding a comma", search: watch.Search{Where: []string{watch.Me}, NotLabels: []string{"a,b"}},
			field: "notLabels[0]", err: watch.ErrInvalidValue,
		},
		{
			name: "a label over GitHub's length", search: watch.Search{Where: []string{watch.Me}, Labels: []string{strings.Repeat("l", 51)}},
			field: "labels[0]", err: watch.ErrInvalidValue,
		},
		{
			name: "a branch holding a space", search: watch.Search{Where: []string{watch.Me}, Base: "main is:closed"},
			field: "base", err: watch.ErrInvalidValue,
		},
		{
			name:   "a label ending in a backslash, which could escape its quote",
			search: watch.Search{Where: []string{watch.Me}, Labels: []string{`a\`}, NotLabels: []string{"z author:x"}},
			field:  "labels[0]", err: watch.ErrInvalidValue,
		},
		{
			name: "a head prefix ending in a backslash", search: watch.Search{Where: []string{watch.Me}, HeadPrefix: `dependabot\`},
			field: "headPrefix", err: watch.ErrInvalidValue,
		},
		{
			name: "a base holding a non-breaking space", search: watch.Search{Where: []string{watch.Me}, Base: "main\u00a0author:x"},
			field: "base", err: watch.ErrInvalidValue,
		},
		{
			name: "a backslash typed by hand", search: watch.Search{Where: []string{watch.Me}, More: `label:"a\" author:x`},
			field: "more", err: watch.ErrInvalidValue,
		},
		{
			name: "free text typed by hand", search: watch.Search{Where: []string{watch.Me}, More: "language:go security"},
			field: "more", err: watch.ErrQualifier,
		},
		{
			name: "a qualifier the search sets itself", search: watch.Search{Where: []string{watch.Me}, More: "is:closed"},
			field: "more", err: watch.ErrQualifier,
		},
		{
			name: "sorting typed by hand", search: watch.Search{Where: []string{watch.Me}, More: "sort:created-asc"},
			field: "more", err: watch.ErrQualifier,
		},
		{
			name: "a quote left open", search: watch.Search{Where: []string{watch.Me}, More: `label:"help wanted`},
			field: "more", err: nil,
		},
		{
			name: "a bot by its bare name", search: watch.Search{Where: []string{watch.Me}, Authors: []string{"dependabot"}},
			field: "authors[0]", err: watch.ErrInvalidValue,
		},
		{
			name: "a bot by its bare name, typed by hand", search: watch.Search{Where: []string{watch.Me}, More: "author:renovate"},
			field: "more", err: watch.ErrInvalidValue,
		},
		{
			name: "a place without a qualifier", search: watch.Search{Where: []string{"acme-org"}},
			field: "where[0]", err: watch.ErrInvalidValue,
		},
		{
			name:   "a pull request's state on issues",
			search: watch.Search{Kind: item.KindIssue, Where: []string{watch.Me}, State: []watch.State{watch.StateReady}},
			field:  "state[0]", err: watch.ErrPullRequestsOnly,
		},
		{
			name: "a pull request's branch on issues", search: watch.Search{Kind: item.KindIssue, Where: []string{watch.Me}, HeadPrefix: "x/"},
			field: "headPrefix", err: watch.ErrPullRequestsOnly,
		},
		{
			name:   "a review request on issues",
			search: watch.Search{Kind: item.KindIssue, Involving: []watch.Involving{watch.InvolvingReviewRequested}},
			field:  "involving[0]", err: watch.ErrPullRequestsOnly,
		},
		{
			name:   "two states no item can be in",
			search: watch.Search{Where: []string{watch.Me}, State: []watch.State{watch.StateReady, watch.StateDraft}},
			field:  "state", err: watch.ErrInvalidValue,
		},
		{name: "an unknown kind", search: watch.Search{Kind: "discussion", Where: []string{watch.Me}}, field: "kind", err: watch.ErrInvalidValue},
		{
			name: "too many places", search: watch.Search{Where: strings.Split(strings.Repeat("org:acme-org,", 11), ",")[:11]},
			field: "where", err: watch.ErrTooMany,
		},
		{
			name: "over 256 characters", search: watch.Search{Kind: item.KindPR, Authors: []string{"app/dependabot"}, Where: []string{acmeOrg}, More: tooLong},
			field: "more", err: watch.ErrTooLong,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := watch.Build(tt.search, login)
			require.Error(t, err)

			var fe *watch.FieldError

			require.ErrorAs(t, err, &fe)
			assert.Equal(t, tt.field, fe.Field)

			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
			}
		})
	}
}

// tooLong are hand-typed qualifiers that take a query over 256 characters.
const tooLong = "label:alpha-bravo-charlie label:delta-echo-foxtrot label:golf-hotel-india label:juliet-kilo-lima " +
	"label:mike-november-oscar label:papa-quebec-romeo label:sierra-tango-uniform label:victor-whiskey-xray"

func TestTooLongSaysToSplit(t *testing.T) {
	t.Parallel()

	problems := watch.Check(watch.Search{Kind: item.KindPR, Authors: []string{"app/dependabot"}, Where: []string{acmeOrg}, More: tooLong})
	require.Len(t, problems, 1)
	assert.Contains(t, problems[0].Error(), "split it into two searches")
}

func TestCheckFitsTheLongestLogin(t *testing.T) {
	t.Parallel()

	// Within 256 characters with a short login, past them with the longest GitHub allows.
	s := watch.Search{
		Where: []string{watch.Me},
		More: "label:aaaaaaaaaaaa label:bbbbbbbbbbbb label:cccccccccccc label:dddddddddddd label:eeeeeeeeeeee " +
			"label:ffffffffffff label:gggggggggggg label:hhhhhhhhhhhh label:iiiiiiiiiiii label:jjjjjjjjjjjj",
	}

	q, err := watch.Build(s, "me")
	require.NoError(t, err)
	assert.LessOrEqual(t, utf8.RuneCountInString(q.Text), watch.MaxQueryRunes)

	problems := watch.Check(s)
	require.Len(t, problems, 1)
	require.ErrorIs(t, problems[0], watch.ErrTooLong)
}

func TestCheckReportsEveryProblem(t *testing.T) {
	t.Parallel()

	problems := watch.Check(watch.Search{Kind: item.KindIssue, Authors: []string{"not a login"}, Base: "main"})

	fields := make([]string, 0, len(problems))
	for _, p := range problems {
		fields = append(fields, p.Field)
	}

	assert.Equal(t, []string{"authors[0]", "base", "where"}, fields)
}

// TestAnySpaceStaysQuoted checks that a hand-typed value holding any kind of space, a
// non-breaking one included, stays quoted, normalized or built, so it can never split
// into a second qualifier.
func TestAnySpaceStaysQuoted(t *testing.T) {
	t.Parallel()

	s := watch.Search{Where: []string{acmeOrg}, More: "label:\"bug\u00a0author:x\""}
	n := watch.Normalize(s)
	assert.Equal(t, "label:\"bug\u00a0author:x\"", n.More)
	assert.Equal(t, n, watch.Normalize(n))

	q, err := watch.Build(n, login)
	require.NoError(t, err)
	assert.Contains(t, q.Text, "label:\"bug\u00a0author:x\"")
	assert.NotContains(t, q.Text, " author:x")
}

func TestIdenticalSearchesShareAKey(t *testing.T) {
	t.Parallel()

	a := watch.Search{
		Kind: "PR", Authors: []string{"Dependabot[bot]"}, Where: []string{"org:Acme-Org", watch.Me},
		Labels: []string{"security", "deps", "Deps"}, More: "language:go  -is:private",
	}
	b := watch.Search{
		Kind: item.KindPR, Authors: []string{"app/dependabot", "dependabot"}, Where: []string{watch.Me, acmeOrg},
		Labels: []string{"deps", "security"}, More: "-is:private language:go",
	}

	qa, err := watch.Build(watch.Normalize(a), login)
	require.NoError(t, err)

	qb, err := watch.Build(watch.Normalize(b), login)
	require.NoError(t, err)

	assert.Equal(t, qa.Key, qb.Key)
}

func TestNormalize(t *testing.T) {
	t.Parallel()

	got := watch.Normalize(watch.Search{
		Kind: " Issue ", Authors: []string{" Octo-User ", "renovate", "renovate[bot]", ""},
		Where: []string{" REPO:Acme-Org/API ", acmeOrg, acmeOrg}, Involving: []watch.Involving{"mentioned", "assigned", "mentioned"},
		Labels: []string{"b", "A", "a"}, Base: " main ", HeadPrefix: " dependabot/ ", More: ` Label:"x y"  language:go `,
	})

	assert.Equal(t, watch.Search{
		Kind: item.KindIssue, Authors: []string{"app/renovate", "octo-user"}, Where: []string{acmeOrg, "repo:acme-org/api"},
		Involving: []watch.Involving{watch.InvolvingAssigned, watch.InvolvingMentioned}, Labels: []string{"A", "b"},
		Base: "main", HeadPrefix: "dependabot/", More: `label:"x y" language:go`,
	}, got)
	assert.Equal(t, got, watch.Normalize(got))
	assert.Equal(t, watch.Search{}, watch.Normalize(watch.Search{}), "an empty list is left out")
}

// FuzzBuild checks that any search builds or is refused without a panic, and that a
// query Build writes is scoped, short enough, and carries the search's own qualifiers;
// that Normalize is stable; and that Check and Build agree.
func FuzzBuild(f *testing.F) {
	f.Add("pr", "app/dependabot", watch.Me, "bug", "wontfix", "main", "dependabot/", "language:go", login)
	f.Add("issue", watch.Me, "repo:acme-org/api", `a"b`, "", "", "", `label:"x`, "")
	f.Add("", "", "", "", "", "a b", "", "is:closed", login)

	f.Fuzz(func(t *testing.T, kind, author, where, label, notLabel, base, head, more, account string) {
		s := watch.Search{
			Kind: item.Kind(kind), Authors: []string{author}, Where: []string{where}, Labels: []string{label},
			NotLabels: []string{notLabel}, Base: base, HeadPrefix: head, More: more,
		}

		n := watch.Normalize(s)
		assert.Equal(t, n, watch.Normalize(n), "normalizing twice changes nothing")

		q, err := watch.Build(n, account)
		if err == nil {
			assert.LessOrEqual(t, utf8.RuneCountInString(q.Text), watch.MaxQueryRunes)
			assert.True(t, strings.Contains(q.Text, "is:open archived:false") && strings.HasSuffix(q.Text, " sort:updated-desc"))
			assert.Equal(t, q.Text, q.Key)
		}

		if len(watch.Check(n)) == 0 && repo.ValidOwner(account) {
			require.NoError(t, err, "a search Check passes builds for any account")
		}
	})
}
