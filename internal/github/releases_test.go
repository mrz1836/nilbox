package github

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/repo"
)

// toolRepos are the repositories the latest-release fixture answers for, in alias order.
func toolRepos() []repo.Name {
	return []repo.Name{
		{Owner: "mrz1836", Repo: "mage-x"},
		{Owner: "mrz1836", Repo: "go-pre-commit"},
		{Owner: filesOwner, Repo: "no-releases"},
		{Owner: filesOwner, Repo: "gone"},
	}
}

func TestFetchLatestReleasesReadsEachRepository(t *testing.T) {
	t.Parallel()

	body := fixture(t, "latest_releases_response.json")
	client, rates := observingClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/graphql", r.URL.Path)
		assert.Equal(t, "Bearer "+fakeToken, r.Header.Get("Authorization"))

		raw, err := io.ReadAll(r.Body)
		assert.NoError(t, err)

		var req graphQLRequest
		assert.NoError(t, json.Unmarshal(raw, &req))
		assert.Contains(t, req.Query, `t0: repository(owner: "mrz1836", name: "mage-x") { latestRelease { tagName url publishedAt `+
			`releaseAssets(first: 30) { nodes { name downloadUrl size digest } } } }`)
		assert.Contains(t, req.Query, `t3: repository(owner: "acme-org", name: "gone")`)
		assert.Contains(t, req.Query, "rateLimit { cost }", "the query reports its cost")

		setRateHeaders(w, ResourceGraphQL, 4980, 20)
		_, _ = w.Write(body)
	})

	repos := toolRepos()

	got, err := client.FetchLatestReleases(t.Context(), NewToken(fakeToken), repos)
	require.NoError(t, err)
	require.Len(t, got, 2, "a repository with no release, and one GitHub can't find, are left out")

	magex := got[repos[0].Key()]
	assert.Equal(t, "v1.29.5", magex.Tag)
	assert.Equal(t, "https://github.com/mrz1836/mage-x/releases/tag/v1.29.5", magex.URL)
	assert.Equal(t, time.Date(2026, 11, 5, 4, 19, 11, 0, time.UTC), magex.PublishedAt)
	require.Len(t, magex.Assets, 7)
	assert.Equal(t, ReleaseAsset{
		Name:   "mage-x_1.29.5_darwin_arm64.tar.gz",
		URL:    "https://github.com/mrz1836/mage-x/releases/download/v1.29.5/mage-x_1.29.5_darwin_arm64.tar.gz",
		Size:   3925898,
		Digest: "sha256:72bb2aaae2d8946b0bc7ac513fb32b1765b19162a29bbc7f724cb495e39d9842",
	}, magex.Assets[0])

	assert.Equal(t, "v1.13.1", got[repos[1].Key()].Tag)

	reading := RateReading{Resource: ResourceGraphQL, Limit: 5000, Remaining: 4980, Used: 20, ResetAt: rateResetAt}
	assert.Equal(t, []observed{{reading: reading, spent: 0}, {reading: reading, spent: 1}}, rates.all(),
		"the budget hears the reading, then the query's cost of one point")
}

func TestFetchLatestReleasesWithNoRepositoriesSendsNothing(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(http.ResponseWriter, *http.Request) {
		t.Error("nothing is asked")
	})

	got, err := client.FetchLatestReleases(t.Context(), NewToken(fakeToken), nil)
	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestFetchLatestReleasesFailsOnAnError(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":null,"errors":[{"type":"FORBIDDEN","message":"Resource not accessible"}]}`)
	})

	_, err := client.FetchLatestReleases(t.Context(), NewToken(fakeToken), toolRepos()[:1])
	require.Error(t, err)
	assert.NotContains(t, err.Error(), fakeToken)
}

func TestLatestReleasesQueryQuotesNames(t *testing.T) {
	t.Parallel()

	query, err := latestReleasesQuery([]repo.Name{{Owner: `acme"org`, Repo: "x) { viewer { login } }"}})
	require.NoError(t, err)
	assert.Contains(t, query, `repository(owner: "acme\"org", name: "x) { viewer { login } }")`,
		"a name stays inside its JSON string")
}
