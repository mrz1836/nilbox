package github

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// statusSummaryJSON is a status page summary during an incident, trimmed from the shape
// www.githubstatus.com answers with.
const statusSummaryJSON = `{
  "page": {"id": "kctbh9vrtdwd", "name": "GitHub", "url": "https://www.githubstatus.com"},
  "components": [
    {"id": "a", "name": "Git Operations", "status": "operational", "group": false},
    {"id": "b", "name": "Actions", "status": "partial_outage", "group": false},
    {"id": "c", "name": "Pull Requests", "status": "degraded_performance", "group": false},
    {"id": "d", "name": "Everything", "status": "partial_outage", "group": true}
  ],
  "incidents": [
    {"id": "x1y2z3", "name": "Incident with Actions", "status": "investigating", "impact": "major",
     "shortlink": "https://stspg.io/abc"},
    {"id": "../../elsewhere", "name": "A bad id", "status": "investigating", "impact": "minor"}
  ],
  "scheduled_maintenances": [
    {"id": "m1", "name": "Database upgrade", "status": "in_progress", "impact": "maintenance"},
    {"id": "m2", "name": "Next week", "status": "scheduled", "impact": "maintenance"}
  ],
  "status": {"indicator": "major", "description": "Partial System Outage"}
}`

// newStatusServer serves handler over TLS and returns a client whose status host is the
// server.
func newStatusServer(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()

	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)

	u, err := url.Parse(srv.URL)
	require.NoError(t, err)

	return &Client{HTTP: srv.Client(), UserAgent: testUserAgent, StatusHost: u.Host}
}

func TestFetchStatusReadsTheSummary(t *testing.T) {
	t.Parallel()

	var host string

	client := newStatusServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, statusPath, r.URL.Path)
		assert.Empty(t, r.Header.Get("Authorization"), "the status page is public; no token is sent")
		assert.Equal(t, testUserAgent, r.Header.Get("User-Agent"))
		assert.Equal(t, "application/json", r.Header.Get("Accept"))

		host = r.Host
		_, _ = w.Write([]byte(statusSummaryJSON))
	})

	status, err := client.FetchStatus(t.Context())
	require.NoError(t, err)

	assert.Equal(t, &ServiceStatus{
		Indicator:   StatusMajor,
		Description: "Partial System Outage",
		Incidents: []StatusIncident{
			{
				Name: "Incident with Actions", Status: "investigating", Impact: "major",
				URL: "https://" + host + "/incidents/x1y2z3",
			},
			{
				Name: "Database upgrade", Status: "in_progress", Impact: StatusMaintenance,
				URL: "https://" + host + "/incidents/m1",
			},
		},
		Components: []StatusComponent{
			{Name: "Actions", Status: "partial_outage"},
			{Name: "Pull Requests", Status: "degraded_performance"},
		},
	}, status, "an id that isn't one path segment, maintenance not under way, and a group are left out")
}

func TestFetchStatusOperationalListsNothing(t *testing.T) {
	t.Parallel()

	client := newStatusServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"components": [{"name": "Actions", "status": "operational"}], "incidents": [],
			"status": {"indicator": "none", "description": "All Systems Operational"}}`))
	})

	status, err := client.FetchStatus(t.Context())
	require.NoError(t, err)

	assert.Equal(t, &ServiceStatus{
		Indicator: StatusNone, Description: "All Systems Operational",
		Incidents: []StatusIncident{}, Components: []StatusComponent{},
	}, status)
}

func TestFetchStatusFollowsRedirectsOnTheHostOnly(t *testing.T) {
	t.Parallel()

	client := newStatusServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == statusPath {
			http.Redirect(w, r, "/moved.json", http.StatusFound)

			return
		}

		_, _ = w.Write([]byte(`{"status": {"indicator": "minor", "description": "Minor Service Outage"}}`))
	})

	status, err := client.FetchStatus(t.Context())
	require.NoError(t, err)
	assert.Equal(t, StatusMinor, status.Indicator)

	var off atomic.Int32

	elsewhere := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		off.Add(1)
	}))
	t.Cleanup(elsewhere.Close)

	away := newStatusServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/summary.json", http.StatusFound)
	})

	_, err = away.FetchStatus(t.Context())
	require.ErrorIs(t, err, ErrStatusURL)
	assert.Zero(t, off.Load(), "a redirect off the host is never followed")
}

func TestFetchStatusRefusesWhatIsntASummary(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		status int
		body   string
	}{
		"an error status":  {http.StatusServiceUnavailable, `{}`},
		"not JSON":         {http.StatusOK, `<html>down</html>`},
		"no state":         {http.StatusOK, `{"status": {}}`},
		"a body too large": {http.StatusOK, `{"x": "` + strings.Repeat("a", maxStatusBytes) + `"}`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			client := newStatusServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})

			status, err := client.FetchStatus(t.Context())
			require.Error(t, err)
			assert.Nil(t, status)
		})
	}
}

func TestStatusHostDefaultsToGitHubs(t *testing.T) {
	t.Parallel()

	assert.Equal(t, DefaultStatusHost, (&Client{}).statusHost())
	assert.Equal(t, DefaultStatusHost, NewClient("1.0.0").StatusHost)
}
