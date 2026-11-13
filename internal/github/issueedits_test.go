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

	body := `{"data":{"node":{"__typename":"Issue","repository":{"assignableUsers":{"nodes":[
		{"login":"octo-user","name":"Octo User"},null,{"login":"","name":"nobody"},{"login":"octo-other","name":null}]}}},
		"rateLimit":{"cost":1,"remaining":4980}}}`

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		assert.NoError(t, err)

		var req struct {
			Query     string            `json:"query"`
			Variables map[string]string `json:"variables"`
		}

		assert.NoError(t, json.Unmarshal(raw, &req))
		assert.Contains(t, req.Query, "assignableUsers(first: 20, query: $query)")
		assert.Equal(t, map[string]string{"id": "I_fakeNodeId12", "query": "octo"}, req.Variables)

		_, _ = io.WriteString(w, body)
	})

	people, err := client.FetchAssignable(t.Context(), NewToken(fakeToken), "I_fakeNodeId12", "  octo ")
	require.NoError(t, err)
	assert.Equal(t, []Assignable{{Login: "octo-user", Name: "Octo User"}, {Login: "octo-other"}}, people)
}

func TestTheIssueReadsOfAnIssueThatIsGoneOrNone(t *testing.T) {
	t.Parallel()

	gone := newTestClient(t, serveItem(`{"data":{"node":null},"errors":[{"type":"NOT_FOUND","message":"gone"}]}`))

	_, err := gone.FetchRepoLabels(t.Context(), NewToken(fakeToken), "I_gone")
	require.ErrorIs(t, err, ErrIssueGone)

	_, err = gone.FetchAssignable(t.Context(), NewToken(fakeToken), "I_gone", "")
	require.ErrorIs(t, err, ErrIssueGone)

	for name, body := range map[string]string{
		"a pull request": `{"data":{"node":{"__typename":"PullRequest"}}}`,
		"not an object":  `{"data":{"node":"x"}}`,
		"labels of another shape": `{"data":{"node":{"__typename":"Issue","repository":` +
			`{"labels":{"nodes":"x"}}}}}`,
	} {
		_, err = newTestClient(t, serveItem(body)).FetchRepoLabels(t.Context(), NewToken(fakeToken), "I_x")
		require.ErrorIs(t, err, ErrMalformedResponse, name)
	}

	failing := newTestClient(t, serveItem(`{"data":{"node":null},"errors":[{"type":"FORBIDDEN","message":"no"}]}`))

	_, err = failing.FetchAssignable(t.Context(), NewToken(fakeToken), "I_x", "")
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
	pull := issueRef()
	pull.Kind = item.KindPR

	for name, names := range map[string][]string{
		"none":         nil,
		"empty":        {" "},
		"too long":     {strings.Repeat("x", 51)},
		"a line break": {"bug\nfix"},
	} {
		_, err := client.AddLabels(t.Context(), NewToken(fakeToken), issueRef(), names)
		require.ErrorIs(t, err, ErrInvalidLabel, name)
	}

	_, err := client.AddLabels(t.Context(), NewToken(fakeToken), pull, []string{bugLabel})
	require.ErrorIs(t, err, ErrInvalidItemRef, "a pull request's labels aren't the issue sheet's to change")

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
	assert.Equal(t, &Edited{UpdatedAt: time.Date(2026, 1, 10, 12, 30, 0, 0, time.UTC)}, edited)
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

func TestCanTriage(t *testing.T) {
	t.Parallel()

	for permission, want := range map[string]bool{
		"ADMIN": true, "MAINTAIN": true, "WRITE": true, "TRIAGE": true, "READ": false, "": false,
	} {
		assert.Equal(t, want, CanTriage(permission), permission)
	}
}
