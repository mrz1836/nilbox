package autopilot

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilot/watch"
	"github.com/mrz1836/nilbox/internal/item"
)

// whyViewer is the account signed in as Why didn't it run? reads it.
const whyViewer = "octo-user"

// stored is the facts of a pull request in repo with values, as the inbox stored them.
func stored(kind item.Kind, repo string, values map[catalog.FieldID]facts.Value) facts.Facts {
	return facts.Facts{Item: facts.Item{ID: 1, Kind: kind, Repo: repo, Number: 1}, Values: values}
}

// searching is a routine whose one source is s.
func searching(s watch.Search) spec.Spec {
	return spec.Spec{Watch: spec.Watch{Sources: []spec.Source{{Search: &s}}}}
}

func TestWhyItIsntInASearchSaysTheFirstFieldItDoesntFit(t *testing.T) {
	t.Parallel()

	pr := item.KindPR
	fits := "Its search should find it, but the last refresh didn't bring it: it shows once a refresh reads the search again."

	for _, c := range []struct {
		name   string
		search watch.Search
		f      facts.Facts
		want   string
	}{
		{
			"an issue in a search of pull requests",
			watch.Search{Kind: item.KindPR},
			stored(item.KindIssue, "acme-org/example-repo", nil),
			"Not in its search: it's an issue, and the search finds pull requests.",
		},
		{
			"a pull request in a search of issues",
			watch.Search{Kind: item.KindIssue},
			stored(pr, "acme-org/example-repo", nil),
			"Not in its search: it's a pull request, and the search finds issues.",
		},
		{
			"an author the search doesn't name",
			watch.Search{Authors: []string{"@me", "app/dependabot", "octocat"}},
			stored(pr, "acme-org/example-repo", map[catalog.FieldID]facts.Value{catalog.FieldAuthor: facts.Text("someone")}),
			"Not in its search: its author is someone, not you, Dependabot, or octocat.",
		},
		{
			"the user, as @me",
			watch.Search{Authors: []string{"@me"}},
			stored(pr, "acme-org/example-repo", map[catalog.FieldID]facts.Value{catalog.FieldAuthor: facts.Text("Octo-User")}),
			fits,
		},
		{
			"a bot, as its app",
			watch.Search{Authors: []string{"app/renovate"}},
			stored(pr, "acme-org/example-repo", map[catalog.FieldID]facts.Value{catalog.FieldAuthor: facts.Text("renovate[bot]")}),
			fits,
		},
		{
			"a login, ignoring case",
			watch.Search{Authors: []string{"OctoCat"}},
			stored(pr, "acme-org/example-repo", map[catalog.FieldID]facts.Value{catalog.FieldAuthor: facts.Text("octocat")}),
			fits,
		},
		{
			"a repository in none of its places",
			watch.Search{Where: []string{"@me", "org:acme-org", "repo:other-org/example-repo", "plain"}},
			stored(pr, "elsewhere/example-repo", nil),
			"Not in its search: it's in elsewhere/example-repo, not your account, acme-org, other-org/example-repo, or plain.",
		},
		{
			"the user's own repository",
			watch.Search{Where: []string{"@me"}},
			stored(pr, "octo-user/example-repo", nil), fits,
		},
		{
			"a repository the search names",
			watch.Search{Where: []string{"repo:acme-org/example-repo"}},
			stored(pr, "acme-org/example-repo", nil), fits,
		},
		{
			"none of its labels",
			watch.Search{Labels: []string{"bug", "security"}},
			stored(pr, "acme-org/example-repo", map[catalog.FieldID]facts.Value{
				catalog.FieldLabels: facts.List([]string{"docs"}, false),
			}),
			"Not in its search: it isn't labeled bug or security.",
		},
		{
			"a label it leaves out",
			watch.Search{Labels: []string{"Bug"}, NotLabels: []string{"wontfix"}},
			stored(pr, "acme-org/example-repo", map[catalog.FieldID]facts.Value{
				catalog.FieldLabels: facts.List([]string{"bug", "wontfix"}, false),
			}),
			"Not in its search: it's labeled wontfix, which the search leaves out.",
		},
		{
			"labels not read yet",
			watch.Search{Labels: []string{"bug"}},
			stored(pr, "acme-org/example-repo", nil), fits,
		},
		{
			"another base",
			watch.Search{Base: "main"},
			stored(pr, "acme-org/example-repo", map[catalog.FieldID]facts.Value{catalog.FieldBase: facts.Text("develop")}),
			"Not in its search: it merges into develop, not main.",
		},
		{
			"a branch of another prefix",
			watch.Search{HeadPrefix: "dependabot/"},
			stored(pr, "acme-org/example-repo", map[catalog.FieldID]facts.Value{catalog.FieldHead: facts.Text("feature/x")}),
			"Not in its search: its branch feature/x doesn't start with dependabot/.",
		},
		{
			"a draft in a search of pull requests out of draft",
			watch.Search{State: []watch.State{watch.StateReady}},
			stored(pr, "acme-org/example-repo", map[catalog.FieldID]facts.Value{catalog.FieldDraft: facts.Bool(true)}),
			"Not in its search: it's a draft, and the search finds pull requests out of draft.",
		},
		{
			"out of draft in a search of drafts",
			watch.Search{State: []watch.State{watch.StateDraft}},
			stored(pr, "acme-org/example-repo", map[catalog.FieldID]facts.Value{catalog.FieldDraft: facts.Bool(false)}),
			"Not in its search: it isn't a draft, and the search finds drafts.",
		},
		{
			"a draft in a search of drafts",
			watch.Search{State: []watch.State{watch.StateDraft}},
			stored(pr, "acme-org/example-repo", map[catalog.FieldID]facts.Value{catalog.FieldDraft: facts.Bool(true)}),
			fits,
		},
	} {
		assert.Equal(t, []string{c.want}, watchMisses(searching(c.search), c.f, whyViewer), c.name)
	}
}

func TestWhyItIsntInAnInboxListSaysWhyItIsInTheInboxInstead(t *testing.T) {
	t.Parallel()

	lists := func(l ...catalog.ListSource) spec.Spec {
		return spec.Spec{Watch: spec.Watch{Sources: []spec.Source{{Lists: l}}}}
	}
	inLists := func(l ...string) facts.Facts {
		return stored(item.KindPR, "acme-org/example-repo", map[catalog.FieldID]facts.Value{
			catalog.FieldLists: facts.List(l, false),
		})
	}

	assert.Equal(t, []string{"It isn't in your inbox."},
		watchMisses(lists(catalog.SourceReviewRequested), inLists(), whyViewer))
	assert.Equal(t, []string{"It isn't asking for your review or assigned to you: it's in your inbox, opened by you."},
		watchMisses(lists(catalog.SourceReviewRequested, catalog.SourceAssigned), inLists(string(catalog.SourceAuthored)),
			whyViewer))
	assert.Equal(t, []string{"It isn't asking for your review: it's in your inbox, opened by you and mentioned."},
		watchMisses(lists(catalog.SourceReviewRequested), inLists(string(catalog.SourceAuthored), "mentioned"), whyViewer),
		"a reason the catalog doesn't name is said as it is")
}
