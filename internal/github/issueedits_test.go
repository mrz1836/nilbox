package github

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// bugLabel is the label the edit tests add and take off.
const bugLabel = "bug"

func TestFetchRepoLabelsReadsTheRepositorysLabels(t *testing.T) {
	t.Parallel()

	body := `{"data":{"node":{"__typename":"Issue","repository":{"labels":{"totalCount":104,"nodes":[
		{"name":"bug","color":"D73A4A","description":"Something isn't working"},
		null,
		{"name":"","color":"ffffff","description":""},
		{"name":"good first issue","color":"not a color","description":""}]}}},
		"rateLimit":{"cost":1,"remaining":4980}}}`

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		req := readIssueRequest(t, r)
		assert.Contains(t, req.Query, "labels(first: 100, orderBy: {field: NAME, direction: ASC})")
		assert.Equal(t, "I_fakeNodeId12", req.Variables.ID)

		_, _ = io.WriteString(w, body)
	})

	labels, err := client.FetchRepoLabels(t.Context(), NewToken(fakeToken), "I_fakeNodeId12")
	require.NoError(t, err)
	assert.Equal(t, &RepoLabels{
		Labels: []RepoLabel{
			{Name: bugLabel, Color: "#d73a4a", Description: "Something isn't working"},
			{Name: "good first issue"},
		},
		Total: 104,
	}, labels, "a null and a nameless label are skipped")
}

func TestFetchAssignableReadsWhoMatches(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		kind     item.Kind
		typeName string
		id       string
	}{
		{kind: item.KindIssue, typeName: "Issue", id: "I_fakeNodeId12"},
		{kind: item.KindPR, typeName: "PullRequest", id: "PR_fakeNodeId12"},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			t.Parallel()

			body := `{"data":{"node":{"__typename":"` + tc.typeName + `","repository":{"assignableUsers":{"totalCount":7,
				"nodes":[{"login":"octo-user","name":"Octo User"},null,{"login":"","name":"nobody"},
				{"login":"octo-other","name":null}]}}},"rateLimit":{"cost":1,"remaining":4980}}}`

			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				raw, err := io.ReadAll(r.Body)
				assert.NoError(t, err)

				var req struct {
					Query     string            `json:"query"`
					Variables map[string]string `json:"variables"`
				}

				assert.NoError(t, json.Unmarshal(raw, &req))
				assert.Contains(t, req.Query, "... on RepositoryNode")
				assert.Contains(t, req.Query, "assignableUsers(first: 100, query: $query) { totalCount nodes { login name } }")
				assert.Equal(t, map[string]string{"id": tc.id, "query": "octo"}, req.Variables)

				_, _ = io.WriteString(w, body)
			})

			people, err := client.FetchAssignable(t.Context(), NewToken(fakeToken), tc.kind, tc.id, "  octo ")
			require.NoError(t, err)
			assert.Equal(t, &AssignableUsers{
				People: []Person{{Login: octoUser, Name: "Octo User"}, {Login: octoOther}}, Total: 7,
			}, people)
		})
	}
}

func TestFetchAssignableCountsAtLeastWhomItNamed(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, serveItem(`{"data":{"node":{"__typename":"Issue","repository":{"assignableUsers":`+
		`{"totalCount":0,"nodes":[{"login":"octo-user"}]}}}}}`))

	people, err := client.FetchAssignable(t.Context(), NewToken(fakeToken), item.KindIssue, "I_x", "")
	require.NoError(t, err)
	assert.Equal(t, 1, people.Total)

	none := newTestClient(t, serveItem(`{"data":{"node":{"__typename":"Issue","repository":{"assignableUsers":`+
		`{"totalCount":0,"nodes":[]}}}}}`))

	people, err = none.FetchAssignable(t.Context(), NewToken(fakeToken), item.KindIssue, "I_x", "")
	require.NoError(t, err)
	assert.Equal(t, &AssignableUsers{People: []Person{}}, people, "never nil")
}

func TestTheIssueReadsOfAnIssueThatIsGoneOrNone(t *testing.T) {
	t.Parallel()

	gone := newTestClient(t, serveItem(`{"data":{"node":null},"errors":[{"type":"NOT_FOUND","message":"gone"}]}`))

	_, err := gone.FetchRepoLabels(t.Context(), NewToken(fakeToken), "I_gone")
	require.ErrorIs(t, err, ErrIssueGone)

	_, err = gone.FetchAssignable(t.Context(), NewToken(fakeToken), item.KindIssue, "I_gone", "")
	require.ErrorIs(t, err, ErrIssueGone)

	_, err = gone.FetchAssignable(t.Context(), NewToken(fakeToken), item.KindPR, "PR_gone", "")
	require.ErrorIs(t, err, ErrPullGone, "a pull request that is gone says so")

	for name, body := range map[string]string{
		"a pull request": `{"data":{"node":{"__typename":"PullRequest"}}}`,
		"not an object":  `{"data":{"node":"x"}}`,
		"labels of another shape": `{"data":{"node":{"__typename":"Issue","repository":` +
			`{"labels":{"nodes":"x"}}}}}`,
	} {
		_, err = newTestClient(t, serveItem(body)).FetchRepoLabels(t.Context(), NewToken(fakeToken), "I_x")
		require.ErrorIs(t, err, ErrMalformedResponse, name)
	}

	issue := newTestClient(t, serveItem(`{"data":{"node":{"__typename":"Issue"}}}`))

	_, err = issue.FetchAssignable(t.Context(), NewToken(fakeToken), item.KindPR, "PR_x", "")
	require.ErrorIs(t, err, ErrMalformedResponse, "an issue where a pull request was asked for")

	_, err = issue.FetchAssignable(t.Context(), NewToken(fakeToken), item.Kind("discussion"), "D_x", "")
	require.ErrorIs(t, err, ErrInvalidItemRef, "neither an issue nor a pull request")

	failing := newTestClient(t, serveItem(`{"data":{"node":null},"errors":[{"type":"FORBIDDEN","message":"no"}]}`))

	_, err = failing.FetchAssignable(t.Context(), NewToken(fakeToken), item.KindIssue, "I_x", "")
	require.ErrorIs(t, err, ErrGraphQL)

	_, err = failing.FetchRepoLabels(t.Context(), NewToken(fakeToken), "")
	require.ErrorIs(t, err, ErrInvalidItemRef)
}

func TestAddLabelsSendsOneGatedEdit(t *testing.T) {
	t.Parallel()

	client, got := editServer(t, http.StatusOK,
		`[{"name":"bug","color":"d73a4a"},{"name":"Good First Issue","color":"7057ff"}]`, nil)
	gate := &fakeGate{}
	client.Writes = gate

	labels, err := client.AddLabels(t.Context(), NewToken(fakeToken), issueRef(), []string{"good first issue"})
	require.NoError(t, err)

	assert.Equal(t, []Label{{Name: bugLabel, Color: "#d73a4a"}, {Name: "Good First Issue", Color: "#7057ff"}}, labels)
	assert.Equal(t, []editRequest{{
		method: http.MethodPost, path: "/repos/" + filesOwner + "/" + exampleRepo + "/issues/12/labels",
		body: map[string][]string{"labels": {"good first issue"}},
	}}, *got)
	assert.Equal(t, []string{ResourceCore}, gate.allowed, "the gate counted it")
}

func TestAPullRequestsLabelsGoToTheIssuesEndpoint(t *testing.T) {
	t.Parallel()

	client, got := editServer(t, http.StatusOK, `[{"name":"needs-repro","color":"d73a4a"}]`, nil)
	pull := issueRef()
	pull.Kind = item.KindPR

	_, err := client.AddLabels(t.Context(), NewToken(fakeToken), pull, []string{"needs-repro"})
	require.NoError(t, err)

	_, err = client.RemoveLabel(t.Context(), NewToken(fakeToken), pull, "bug")
	require.NoError(t, err)

	path := "/repos/" + filesOwner + "/" + exampleRepo + "/issues/12/labels"
	assert.Equal(t, []editRequest{
		{method: http.MethodPost, path: path, body: map[string][]string{"labels": {"needs-repro"}}},
		{method: http.MethodDelete, path: path + "/bug"},
	}, *got)
}

func TestAddLabelsSeesAnEditGitHubIgnored(t *testing.T) {
	t.Parallel()

	client, _ := editServer(t, http.StatusOK, `[{"name":"bug","color":"d73a4a"}]`, nil)

	_, err := client.AddLabels(t.Context(), NewToken(fakeToken), issueRef(), []string{bugLabel, "triage", "p1"})
	require.ErrorIs(t, err, ErrEditIgnored)
	assert.Equal(t, `GitHub didn't add "triage", "p1" to `+filesOwner+"/"+exampleRepo+"#12. "+
		"Your account may not be allowed to change its labels.", uierr.Message(err))

	refused, _ := editServer(t, http.StatusForbidden, `{"message":"Must have triage access"}`, nil)

	_, err = refused.AddLabels(t.Context(), NewToken(fakeToken), issueRef(), []string{bugLabel})
	require.Error(t, err)
	assert.Contains(t, uierr.Message(err), "Must have triage access")

	odd, _ := editServer(t, http.StatusOK, `{"name":"bug"}`, nil)

	_, err = odd.AddLabels(t.Context(), NewToken(fakeToken), issueRef(), []string{bugLabel})
	require.ErrorIs(t, err, ErrMalformedResponse)
}

func TestLabelEditsRefuseWhatNoLabelCanBe(t *testing.T) {
	t.Parallel()

	client, got := editServer(t, http.StatusOK, `[]`, nil)
	unknown := issueRef()
	unknown.Kind = "discussion"

	for name, names := range map[string][]string{
		"none":         nil,
		"empty":        {" "},
		"too long":     {strings.Repeat("x", 51)},
		"a line break": {"bug\nfix"},
	} {
		_, err := client.AddLabels(t.Context(), NewToken(fakeToken), issueRef(), names)
		require.ErrorIs(t, err, ErrInvalidLabel, name)
	}

	_, err := client.AddLabels(t.Context(), NewToken(fakeToken), unknown, []string{bugLabel})
	require.ErrorIs(t, err, ErrInvalidItemRef, "only an issue or a pull request has labels")

	_, err = client.RemoveLabel(t.Context(), NewToken(fakeToken), issueRef(), "")
	require.ErrorIs(t, err, ErrInvalidLabel)
	assert.Empty(t, *got, "nothing was sent")
}

func TestRemoveLabelSendsOneEditByTheLabelsName(t *testing.T) {
	t.Parallel()

	var paths []string

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.EscapedPath())

		setRateHeaders(w, ResourceCore, 4900, 100)
		_, _ = io.WriteString(w, `[{"name":"bug","color":"d73a4a"}]`)
	})

	labels, err := client.RemoveLabel(t.Context(), NewToken(fakeToken), issueRef(), "good first/issue")
	require.NoError(t, err)
	assert.Equal(t, []Label{{Name: bugLabel, Color: "#d73a4a"}}, labels)
	assert.Equal(t, []string{
		"DELETE /repos/" + filesOwner + "/" + exampleRepo + "/issues/12/labels/good%20first%2Fissue",
	}, paths, "the name is one path segment")
}

func TestRemoveLabelOfALabelTheIssueDoesntHave(t *testing.T) {
	t.Parallel()

	gone, _ := editServer(t, http.StatusNotFound, `{"message":"Label does not exist"}`, nil)

	labels, err := gone.RemoveLabel(t.Context(), NewToken(fakeToken), issueRef(), bugLabel)
	require.NoError(t, err, "it is off already")
	assert.Empty(t, labels)

	missing, _ := editServer(t, http.StatusNotFound, `{"message":"Not Found"}`, nil)

	_, err = missing.RemoveLabel(t.Context(), NewToken(fakeToken), issueRef(), bugLabel)
	require.Error(t, err)
	assert.Contains(t, uierr.Message(err), "GitHub could not find", "the issue itself is gone")

	kept, _ := editServer(t, http.StatusOK, `[{"name":"Bug","color":"d73a4a"}]`, nil)

	_, err = kept.RemoveLabel(t.Context(), NewToken(fakeToken), issueRef(), bugLabel)
	require.ErrorIs(t, err, ErrEditIgnored)
	assert.Equal(t, `GitHub kept "bug" on `+filesOwner+"/"+exampleRepo+"#12. "+
		"Your account may not be allowed to change its labels.", uierr.Message(err))
}

func TestAddAssigneesSendsOneEditAndSeesWhoGitHubLeftOut(t *testing.T) {
	t.Parallel()

	client, got := editServer(t, http.StatusCreated,
		`{"updated_at":"2026-01-10T12:30:00Z","assignees":[{"login":"Octo-Other"}]}`, nil)

	edited, err := client.AddAssignees(t.Context(), NewToken(fakeToken), issueRef(), []string{"octo-other"})
	require.NoError(t, err)
	assert.Equal(t, &Edited{UpdatedAt: time.Date(2026, 1, 10, 12, 30, 0, 0, time.UTC), Assignees: []string{"Octo-Other"}},
		edited, "who it is assigned to after the edit")
	assert.Equal(t, []editRequest{{
		method: http.MethodPost, path: "/repos/" + filesOwner + "/" + exampleRepo + "/issues/12/assignees",
		body: map[string][]string{"assignees": {"octo-other"}},
	}}, *got)

	_, err = client.AddAssignees(t.Context(), NewToken(fakeToken), issueRef(), []string{"octo-other", "octo-third"})
	require.ErrorIs(t, err, ErrEditIgnored)
	assert.Equal(t, "GitHub didn't assign octo-third to "+filesOwner+"/"+exampleRepo+"#12. "+
		"They may not be someone it can be assigned to.", uierr.Message(err))

	_, err = client.AddAssignees(t.Context(), NewToken(fakeToken), issueRef(), nil)
	require.ErrorIs(t, err, ErrInvalidLogin)

	_, err = client.AddAssignees(t.Context(), NewToken(fakeToken), issueRef(), []string{"not a login"})
	require.ErrorIs(t, err, ErrInvalidLogin)

	odd, _ := editServer(t, http.StatusOK, `[]`, nil)

	_, err = odd.AddAssignees(t.Context(), NewToken(fakeToken), issueRef(), []string{"octo-other"})
	require.ErrorIs(t, err, ErrMalformedResponse)

	refused, _ := editServer(t, http.StatusForbidden, ``, nil)

	_, err = refused.AddAssignees(t.Context(), NewToken(fakeToken), issueRef(), []string{"octo-other"})
	require.Error(t, err)
	assert.Contains(t, uierr.Message(err), "refused the change")
}

func TestRemoveAssigneesSendsOneDeleteAndNamesWhomGitHubKept(t *testing.T) {
	t.Parallel()

	client, got := editServer(t, http.StatusOK,
		`{"updated_at":"2026-01-10T12:30:00Z","assignees":[{"login":"octo-third"},{"login":""}]}`, nil)
	gate := &fakeGate{}
	client.Writes = gate
	pull := issueRef()
	pull.Kind = item.KindPR

	edited, err := client.RemoveAssignees(t.Context(), NewToken(fakeToken), pull, []string{octoUser, octoOther})
	require.NoError(t, err)
	assert.Equal(t, &Edited{UpdatedAt: time.Date(2026, 1, 10, 12, 30, 0, 0, time.UTC), Assignees: []string{"octo-third"}},
		edited, "who is still assigned, a blank login left out")
	assert.Equal(t, []editRequest{{
		method: http.MethodDelete, path: "/repos/" + filesOwner + "/" + exampleRepo + "/issues/12/assignees",
		body: map[string][]string{"assignees": {octoUser, octoOther}},
	}}, *got, "a pull request's assignees are at the issues endpoint, everyone in one request")
	assert.Equal(t, []string{ResourceCore}, gate.allowed, "the gate counted it")

	_, err = client.RemoveAssignees(t.Context(), NewToken(fakeToken), pull, []string{"Octo-Third", octoUser})
	require.ErrorIs(t, err, ErrEditIgnored)
	assert.Equal(t, "GitHub kept Octo-Third assigned to "+filesOwner+"/"+exampleRepo+"#12. "+
		"Your account may not be allowed to change its assignees.", uierr.Message(err))

	_, err = client.RemoveAssignees(t.Context(), NewToken(fakeToken), pull, nil)
	require.ErrorIs(t, err, ErrInvalidLogin)

	_, err = client.RemoveAssignees(t.Context(), NewToken(fakeToken), pull, []string{octoUser, "not a login"})
	require.ErrorIs(t, err, ErrInvalidLogin)
	assert.Len(t, *got, 2, "nothing sent for a login GitHub can't have issued")

	refused, _ := editServer(t, http.StatusNotFound, `{"message":"Not Found"}`, nil)

	_, err = refused.RemoveAssignees(t.Context(), NewToken(fakeToken), pull, []string{octoUser})
	require.Error(t, err)
	assert.Contains(t, uierr.Message(err), "could not find")
}

func TestCanTriage(t *testing.T) {
	t.Parallel()

	for permission, want := range map[string]bool{
		"ADMIN": true, "MAINTAIN": true, "WRITE": true, "TRIAGE": true, "READ": false, "": false,
	} {
		assert.Equal(t, want, CanTriage(permission), permission)
	}
}
