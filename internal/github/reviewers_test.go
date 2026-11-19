package github

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// octoOther is the reviewer most reviewer edits ask, and webTeam the team.
const (
	octoOther = "octo-other"
	webTeam   = "web"
)

// editedAt is the time the reviewer edits' answers give.
var editedAt = time.Date(2026, 1, 10, 12, 30, 0, 0, time.UTC) //nolint:gochecknoglobals // a fixed test value

func TestRequestReviewersSendsOnePost(t *testing.T) {
	t.Parallel()

	client, got := editServer(t, http.StatusCreated, `{
  "updated_at": "2026-01-10T12:30:00Z",
  "requested_reviewers": [{"login": "octo-other"}, {"login": "Octo-Third"}],
  "requested_teams": [{"slug": "web"}]
}`, nil)
	gate := &fakeGate{}
	client.Writes = gate

	edited, err := client.RequestReviewers(t.Context(), NewToken(fakeToken), editRef(),
		ReviewRequests{Users: []string{octoOther, "octo-third"}, Teams: []string{webTeam}})
	require.NoError(t, err)

	assert.Equal(t, &Edited{
		UpdatedAt: editedAt,
		ReviewRequests: ReviewRequests{
			Users: []string{octoOther, "Octo-Third"}, Teams: []string{webTeam},
		},
	}, edited)
	assert.Equal(t, []editRequest{{
		method: http.MethodPost, path: "/repos/acme-org/example-repo/pulls/42/requested_reviewers",
		body: map[string][]string{"reviewers": {octoOther, "octo-third"}, "team_reviewers": {webTeam}},
	}}, *got, "one request for every person and team")
	assert.Equal(t, []string{ResourceCore}, gate.allowed, "the gate counted it")
}

func TestRequestReviewersAsksTeamsAlone(t *testing.T) {
	t.Parallel()

	client, got := editServer(t, http.StatusCreated,
		`{"updated_at":"2026-01-10T12:30:00Z","requested_reviewers":[],"requested_teams":[{"slug":"web"}]}`, nil)

	_, err := client.RequestReviewers(t.Context(), NewToken(fakeToken), editRef(), ReviewRequests{Teams: []string{webTeam}})
	require.NoError(t, err)

	assert.Equal(t, map[string][]string{"reviewers": {}, "team_reviewers": {webTeam}}, (*got)[0].body,
		"the people are always sent, as an empty list")
}

func TestRequestReviewersNamesWhoGitHubDidNotAsk(t *testing.T) {
	t.Parallel()

	client, _ := editServer(t, http.StatusCreated,
		`{"updated_at":"2026-01-10T12:30:00Z","requested_reviewers":[{"login":"octo-third"}],"requested_teams":[]}`, nil)

	_, err := client.RequestReviewers(t.Context(), NewToken(fakeToken), editRef(),
		ReviewRequests{Users: []string{octoOther, "octo-third"}, Teams: []string{webTeam}})
	require.ErrorIs(t, err, ErrEditIgnored)
	assert.Equal(t, "GitHub didn't ask octo-other, acme-org/web to review acme-org/example-repo#42. "+
		"They may need access to the repository.", uierr.Message(err))
}

func TestRequestReviewersWordsARefusal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{
			name: "not a collaborator", status: http.StatusUnprocessableEntity,
			body: `{"message":"Reviews may only be requested from collaborators"}`,
			want: "GitHub didn't ask octo-other, acme-org/web to review acme-org/example-repo#42: " +
				"Reviews may only be requested from collaborators.",
		},
		{
			name: "unprocessable, without words", status: http.StatusUnprocessableEntity, body: `{}`,
			want: "GitHub didn't ask octo-other, acme-org/web to review acme-org/example-repo#42. " +
				"They may need access to the repository.",
		},
		{
			name: "refused", status: http.StatusForbidden, body: `{}`,
			want: "GitHub refused the change to acme-org/example-repo#42. Your account may not be allowed to make it.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, _ := editServer(t, tt.status, tt.body, nil)

			_, err := client.RequestReviewers(t.Context(), NewToken(fakeToken), editRef(),
				ReviewRequests{Users: []string{octoOther}, Teams: []string{webTeam}})
			require.Error(t, err)
			assert.Equal(t, tt.want, uierr.Message(err))
		})
	}
}

func TestRemoveReviewRequestsSendsOneDelete(t *testing.T) {
	t.Parallel()

	client, got := editServer(t, http.StatusOK,
		`{"updated_at":"2026-01-10T12:30:00Z","requested_reviewers":[{"login":"octo-third"}],"requested_teams":[]}`, nil)

	edited, err := client.RemoveReviewRequests(t.Context(), NewToken(fakeToken), editRef(),
		ReviewRequests{Users: []string{octoOther}, Teams: []string{webTeam}})
	require.NoError(t, err)

	assert.Equal(t, &Edited{
		UpdatedAt: editedAt, ReviewRequests: ReviewRequests{Users: []string{"octo-third"}, Teams: []string{}},
	}, edited)
	assert.Equal(t, []editRequest{{
		method: http.MethodDelete, path: "/repos/acme-org/example-repo/pulls/42/requested_reviewers",
		body: map[string][]string{"reviewers": {octoOther}, "team_reviewers": {webTeam}},
	}}, *got)
}

func TestRemoveReviewRequestsNamesWhoIsStillAsked(t *testing.T) {
	t.Parallel()

	client, _ := editServer(t, http.StatusOK,
		`{"updated_at":"2026-01-10T12:30:00Z","requested_reviewers":[{"login":"OCTO-OTHER"}],"requested_teams":[{"slug":"web"}]}`,
		nil)

	_, err := client.RemoveReviewRequests(t.Context(), NewToken(fakeToken), editRef(),
		ReviewRequests{Users: []string{octoOther, "octo-third"}, Teams: []string{webTeam}})
	require.ErrorIs(t, err, ErrEditIgnored)
	assert.Equal(t, "GitHub still asks octo-other, acme-org/web to review acme-org/example-repo#42. "+
		"Your account may not be allowed to change its reviewers.", uierr.Message(err))
}

func TestRemoveReviewRequestsWordsARefusal(t *testing.T) {
	t.Parallel()

	client, _ := editServer(t, http.StatusNotFound, `{"message":"Not Found"}`, nil)

	_, err := client.RemoveReviewRequests(t.Context(), NewToken(fakeToken), editRef(), ReviewRequests{Users: []string{octoOther}})
	require.Error(t, err)
	assert.Equal(t, "GitHub could not find acme-org/example-repo#42, or your account cannot see it.", uierr.Message(err))
}

func TestReviewerEditsAreCheckedBeforeSending(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ref  item.Ref
		r    ReviewRequests
		want error
	}{
		{name: "no one", ref: editRef(), want: ErrInvalidLogin},
		{name: "a login GitHub can't issue", ref: editRef(), r: ReviewRequests{Users: []string{"not a login"}}, want: ErrInvalidLogin},
		{name: "a slug GitHub can't issue", ref: editRef(), r: ReviewRequests{Teams: []string{"web/api"}}, want: ErrInvalidTeam},
		{name: "an empty slug", ref: editRef(), r: ReviewRequests{Teams: []string{""}}, want: ErrInvalidTeam},
		{name: "no pull request", ref: item.Ref{}, r: ReviewRequests{Users: []string{octoOther}}, want: ErrInvalidItemRef},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, got := editServer(t, http.StatusOK, `{}`, nil)

			_, err := client.RequestReviewers(t.Context(), NewToken(fakeToken), tt.ref, tt.r)
			require.ErrorIs(t, err, tt.want)

			_, err = client.RemoveReviewRequests(t.Context(), NewToken(fakeToken), tt.ref, tt.r)
			require.ErrorIs(t, err, tt.want)

			assert.Empty(t, *got, "nothing is sent")
		})
	}

	assert.True(t, validTeamSlug("web_team.v2-x"))
	assert.False(t, validTeamSlug(string(make([]byte, maxTeamSlug+1))))
}

func TestReviewerEditsFailOnAMalformedAnswer(t *testing.T) {
	t.Parallel()

	client, _ := editServer(t, http.StatusOK, `not json`, nil)

	_, err := client.RequestReviewers(t.Context(), NewToken(fakeToken), editRef(), ReviewRequests{Users: []string{octoOther}})
	require.ErrorIs(t, err, ErrMalformedResponse)
}

func TestFetchPullReviewReadsSuggestionsAndAssociations(t *testing.T) {
	t.Parallel()

	var answer map[string]any
	require.NoError(t, json.Unmarshal(fixture(t, "pull_review_response.json"), &answer))

	node, ok := answer["data"].(map[string]any)["node"].(map[string]any)
	require.True(t, ok)

	node["suggestedReviewers"] = []any{
		map[string]any{"isAuthor": true, "isCommenter": false, "reviewer": map[string]any{"login": "octo-author", "name": "Octo Author"}},
		nil,
		map[string]any{"isAuthor": false, "isCommenter": true, "reviewer": nil},
		map[string]any{"isAuthor": false, "isCommenter": true, "reviewer": map[string]any{"login": "octo-commenter", "name": nil}},
	}
	node["reviewRequests"] = map[string]any{"nodes": []any{
		map[string]any{"requestedReviewer": map[string]any{"__typename": typeUser, "login": "octo-asked", "name": "Octo Asked"}},
		map[string]any{"requestedReviewer": map[string]any{"__typename": typeUser, "login": "Octo-Asked"}},
		map[string]any{"requestedReviewer": map[string]any{"__typename": "Bot", "login": "copilot-reviewer"}},
		map[string]any{"requestedReviewer": map[string]any{"__typename": "Team", "slug": webTeam, "name": "Web"}},
		map[string]any{"requestedReviewer": nil},
		nil,
	}}

	reviews, ok := node["reviewBodies"].(map[string]any)["nodes"].([]any)
	require.True(t, ok)

	first, ok := reviews[1].(map[string]any)
	require.True(t, ok)

	first["authorAssociation"] = "MEMBER"
	first["author"] = map[string]any{"__typename": typeUser, "login": "octo-reviewer", "name": "Octo Reviewer"}

	body, err := json.Marshal(answer)
	require.NoError(t, err)

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var req itemRequest

		raw, readErr := io.ReadAll(r.Body)
		assert.NoError(t, readErr)
		assert.NoError(t, json.Unmarshal(raw, &req))
		assert.Contains(t, req.Query, "suggestedReviewers { isAuthor isCommenter reviewer { login name } }")
		assert.Contains(t, req.Query, "authorAssociation author { __typename login ... on User { name } }")

		_, _ = w.Write(body)
	})

	review, err := client.FetchPullReview(t.Context(), NewToken(fakeToken), "PR_fakeNodeId94", "")
	require.NoError(t, err)

	assert.Equal(t, []SuggestedReviewer{
		{Login: "octo-author", Name: "Octo Author", ChangedFiles: true},
		{Login: "octo-commenter", Commented: true},
	}, review.Suggested, "nulls and a suggestion without a reviewer are skipped")
	assert.Equal(t, []Person{{Login: "octo-asked", Name: "Octo Asked"}}, review.Requested,
		"people only, each once")
	assert.Equal(t, []Team{{Slug: webTeam, Name: "Web"}}, review.RequestedTeams)
	assert.Equal(t, Review{
		Author: "octo-reviewer", State: "APPROVED", Commit: sha1s("1"),
		URL:         "https://github.com/acme-org/example-repo/pull/94#pullrequestreview-2",
		SubmittedAt: time.Date(2026, 11, 9, 17, 5, 0, 0, time.UTC), AuthorType: typeUser, AuthorName: "Octo Reviewer",
		Association: "MEMBER",
	}, review.Reviews[1])
}

func TestFetchRepoPeopleReadsWhoWorksOnTheRepository(t *testing.T) {
	t.Parallel()

	body := `{"data":{"node":{"__typename":"PullRequest","repository":{
  "owner":{"__typename":"Organization"},
  "pullRequests":{"nodes":[
    {"number":12,"updatedAt":"2026-11-08T10:00:00Z","authorAssociation":"MEMBER",
     "author":{"__typename":"User","login":"octo-author","name":"Octo Author"},
     "latestReviews":{"nodes":[
       {"submittedAt":"2026-11-08T11:00:00Z","authorAssociation":"COLLABORATOR","author":{"__typename":"User","login":"octo-reviewer","name":null}},
       {"submittedAt":"2026-11-08T11:30:00Z","authorAssociation":"NONE","author":{"__typename":"Bot","login":"helper"}},
       {"submittedAt":null,"authorAssociation":"NONE","author":null},
       null]},
     "reviewRequests":{"nodes":[
       {"requestedReviewer":{"__typename":"User","login":"octo-asked","name":"Octo Asked"}},
       {"requestedReviewer":{"__typename":"Team","slug":"web","name":"Web"}},
       {"requestedReviewer":{"__typename":"Bot"}},
       {"requestedReviewer":{"__typename":"Team","slug":""}},
       {"requestedReviewer":null},
       null]}},
    {"number":11,"updatedAt":"2026-11-07T10:00:00Z","authorAssociation":"CONTRIBUTOR",
     "author":{"__typename":"Bot","login":"dependabot"},
     "latestReviews":{"nodes":[]},"reviewRequests":{"nodes":[]}},
    {"number":0},
    null
  ]}}},
  "rateLimit":{"cost":1,"remaining":4990}}}`

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var req itemRequest

		raw, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		assert.NoError(t, json.Unmarshal(raw, &req))
		assert.Equal(t, "PR_fakeNodeId94", req.Variables.ID)
		assert.Contains(t, req.Query, "pullRequests(first: 50, orderBy: {field: UPDATED_AT, direction: DESC})")

		_, _ = io.WriteString(w, body)
	})

	got, err := client.FetchRepoPeople(t.Context(), NewToken(fakeToken), "PR_fakeNodeId94")
	require.NoError(t, err)

	assert.Equal(t, &RepoPeople{Org: true, Pulls: []RepoPull{
		{
			Number: 12, UpdatedAt: time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC),
			Author: Participant{Login: "octo-author", Name: "Octo Author", Association: "MEMBER"},
			Reviews: []Participant{{
				Login: "octo-reviewer", Association: "COLLABORATOR", At: time.Date(2026, 11, 8, 11, 0, 0, 0, time.UTC),
			}},
			Requested: []Person{{Login: "octo-asked", Name: "Octo Asked"}},
			Teams:     []Team{{Slug: webTeam, Name: "Web"}},
		},
		{
			Number: 11, UpdatedAt: time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC),
			Reviews: []Participant{}, Requested: []Person{}, Teams: []Team{},
		},
	}}, got, "people only: a bot's pull request has no author, and bots, nulls, and nameless teams are skipped")
	assert.Equal(t, RepoPeoplePulls, 50)
	assert.Contains(t, repoPeopleQuery, "first: "+repoPeoplePullsText)
}

func TestFetchRepoPeopleOfWhatIsNotThere(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want error
	}{
		{
			name: "a pull request GitHub can no longer find",
			body: `{"data":{"node":null,"rateLimit":{"cost":1}},"errors":[{"type":"NOT_FOUND","message":"Could not resolve"}]}`,
			want: ErrPullGone,
		},
		{
			name: "an issue", body: `{"data":{"node":{"__typename":"Issue"},"rateLimit":{"cost":1}}}`, want: ErrMalformedResponse,
		},
		{
			name: "not an object", body: `{"data":{"node":{"__typename":"PullRequest","repository":[]},"rateLimit":{"cost":1}}}`,
			want: ErrMalformedResponse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, tt.body) })

			_, err := client.FetchRepoPeople(t.Context(), NewToken(fakeToken), "PR_fakeNodeId94")
			require.ErrorIs(t, err, tt.want)
		})
	}

	client := newTestClient(t, func(http.ResponseWriter, *http.Request) { t.Error("nothing is sent without an ID") })

	_, err := client.FetchRepoPeople(t.Context(), NewToken(fakeToken), "")
	require.ErrorIs(t, err, ErrInvalidItemRef)
}

func TestFetchRepoTeams(t *testing.T) {
	t.Parallel()

	name := repo.Name{Owner: filesOwner, Repo: exampleRepo}

	t.Run("lists the teams", func(t *testing.T) {
		t.Parallel()

		client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/repos/acme-org/example-repo/teams", r.URL.Path)
			assert.Equal(t, "100", r.URL.Query().Get("per_page"))
			setRateHeaders(w, ResourceCore, 4900, 100)
			_, _ = io.WriteString(w, `[{"slug":"web","name":"Web"},{"slug":"","name":"nameless"},{"slug":"api","name":"API"}]`)
		})

		teams, err := client.FetchRepoTeams(t.Context(), NewToken(fakeToken), name)
		require.NoError(t, err)
		assert.Equal(t, []Team{{Slug: webTeam, Name: "Web"}, {Slug: "api", Name: "API"}}, teams)
	})

	for _, status := range []int{http.StatusForbidden, http.StatusNotFound} {
		t.Run("hidden "+http.StatusText(status), func(t *testing.T) {
			t.Parallel()

			client, _ := editServer(t, status, `{"message":"Not Found"}`, nil)

			_, err := client.FetchRepoTeams(t.Context(), NewToken(fakeToken), name)
			require.ErrorIs(t, err, ErrTeamsHidden)
		})
	}

	t.Run("another failure", func(t *testing.T) {
		t.Parallel()

		client, _ := editServer(t, http.StatusInternalServerError, `{}`, nil)

		_, err := client.FetchRepoTeams(t.Context(), NewToken(fakeToken), name)
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrTeamsHidden)
	})

	t.Run("a malformed answer", func(t *testing.T) {
		t.Parallel()

		client, _ := editServer(t, http.StatusOK, `{}`, nil)

		_, err := client.FetchRepoTeams(t.Context(), NewToken(fakeToken), name)
		require.ErrorIs(t, err, ErrMalformedResponse)
	})

	t.Run("no repository", func(t *testing.T) {
		t.Parallel()

		client, got := editServer(t, http.StatusOK, `[]`, nil)

		_, err := client.FetchRepoTeams(t.Context(), NewToken(fakeToken), repo.Name{Owner: filesOwner})
		require.ErrorIs(t, err, ErrInvalidItemRef)
		assert.Empty(t, *got)
	})
}
