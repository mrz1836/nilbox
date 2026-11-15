package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const (
	// DefaultStatusHost is GitHub's status page, the only host FetchStatus reads from.
	DefaultStatusHost = "www.githubstatus.com"

	// statusPath is the status page's summary: the overall state, each component's, and
	// the incidents and maintenance not yet resolved, a few kilobytes in all.
	statusPath = "/api/v2/summary.json"

	// maxStatusBytes caps the summary read, which runs to a few kilobytes even during an
	// incident.
	maxStatusBytes = 1 << 20

	// maxStatusRedirects caps the redirects the read follows, all on the status host.
	maxStatusRedirects = 3
)

// ErrStatusURL is returned for a status page URL, or a redirect, off the status host.
var ErrStatusURL = errors.New("not GitHub's status page")

// The overall states GitHub's status page reports (ServiceStatus.Indicator).
const (
	// StatusNone is every system operational.
	StatusNone = "none"

	// StatusMinor is a minor incident, such as degraded performance.
	StatusMinor = "minor"

	// StatusMajor is a major incident, such as a partial outage.
	StatusMajor = "major"

	// StatusCritical is a critical incident, such as a major outage.
	StatusCritical = "critical"

	// StatusMaintenance is maintenance under way.
	StatusMaintenance = "maintenance"
)

// ServiceStatus is what GitHub's status page says now.
type ServiceStatus struct {
	// Indicator is the overall state, one of the Status constants, as the page words it;
	// a value this build doesn't know is passed on as it is.
	Indicator string

	// Description is the page's own words for it, e.g. "All Systems Operational".
	Description string

	// Incidents are the incidents not yet resolved, and the maintenance under way, newest
	// first, as the page lists them.
	Incidents []StatusIncident

	// Components are the parts of GitHub that aren't operational, such as Actions, in the
	// page's order.
	Components []StatusComponent
}

// StatusIncident is an incident not yet resolved, or maintenance under way.
type StatusIncident struct {
	// Name is the incident's title, e.g. "Incident with Actions".
	Name string

	// Status is where it stands: investigating, identified, monitoring, in_progress, ...
	Status string

	// Impact is how bad it is: none, minor, major, critical, or maintenance.
	Impact string

	// URL is the incident's page on the status host.
	URL string
}

// StatusComponent is a part of GitHub that isn't operational.
type StatusComponent struct {
	// Name is the component's name, e.g. "Actions".
	Name string

	// Status is its state: degraded_performance, partial_outage, major_outage, or
	// under_maintenance.
	Status string
}

// statusSummary is the status page's summary, as much of it as the app reads.
type statusSummary struct {
	Status struct {
		Indicator   string `json:"indicator"`
		Description string `json:"description"`
	} `json:"status"`
	Components []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
		Group  bool   `json:"group"`
	} `json:"components"`
	Incidents   []summaryIncident `json:"incidents"`
	Maintenance []summaryIncident `json:"scheduled_maintenances"`
}

// summaryIncident is an incident or a scheduled maintenance in the summary.
type summaryIncident struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Impact string `json:"impact"`
}

// FetchStatus reads GitHub's status page's summary: one GET from the status host, which
// isn't the API, so it sends no token and spends no point of any budget. Redirects may not
// leave the host.
func (c *Client) FetchStatus(ctx context.Context) (*ServiceStatus, error) {
	host := c.statusHost()
	u := url.URL{Scheme: "https", Host: host, Path: statusPath}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build the status request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.UserAgent)

	// A copy of the client, so the redirect rule applies to this read only.
	hc := *c.httpClient()
	hc.CheckRedirect = redirectsWithin(maxStatusRedirects, ErrStatusURL, func(rawURL string) (*url.URL, error) {
		return checkPublicURL(rawURL, []string{host}, ErrStatusURL, "https://"+host+"/")
	})

	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("read GitHub's status: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("read GitHub's status: %w: HTTP %d", ErrUnexpectedStatus, resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxStatusBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read GitHub's status: %w", err)
	}

	if len(data) > maxStatusBytes {
		return nil, fmt.Errorf("read GitHub's status: %w: it is larger than %d bytes", ErrUnexpectedStatus,
			maxStatusBytes)
	}

	var summary statusSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		return nil, fmt.Errorf("decode GitHub's status: %w", err)
	}

	if summary.Status.Indicator == "" {
		return nil, fmt.Errorf("decode GitHub's status: %w: it names no state", ErrUnexpectedStatus)
	}

	return summary.status(host), nil
}

// status converts the summary, linking each incident to its page on host.
func (s statusSummary) status(host string) *ServiceStatus {
	out := &ServiceStatus{
		Indicator:   s.Status.Indicator,
		Description: s.Status.Description,
		Incidents:   []StatusIncident{},
		Components:  s.notOperational(),
	}

	for _, i := range s.Incidents {
		if i.linkable() {
			out.Incidents = append(out.Incidents, i.incident(host))
		}
	}

	// Only maintenance under way counts; one scheduled for later or completed says
	// nothing about GitHub now.
	for _, m := range s.Maintenance {
		if m.underWay() && m.linkable() {
			m.Impact = StatusMaintenance
			out.Incidents = append(out.Incidents, m.incident(host))
		}
	}

	return out
}

// notOperational lists the components that aren't operational, in the page's order. A
// group's state only repeats its members', so groups are left out.
func (s statusSummary) notOperational() []StatusComponent {
	out := []StatusComponent{}

	for _, comp := range s.Components {
		if !comp.Group && comp.Status != "operational" {
			out = append(out, StatusComponent{Name: comp.Name, Status: comp.Status})
		}
	}

	return out
}

// linkable reports whether the incident's ID makes one path segment, so its page is on
// the status host.
func (i summaryIncident) linkable() bool {
	return i.ID != "" && !strings.Contains(i.ID, "/")
}

// underWay reports whether a scheduled maintenance is going on now.
func (i summaryIncident) underWay() bool {
	return i.Status == "in_progress" || i.Status == "verifying"
}

// incident converts the incident, linking it to its page on host.
func (i summaryIncident) incident(host string) StatusIncident {
	page := url.URL{Scheme: "https", Host: host, Path: "/incidents/" + i.ID}

	return StatusIncident{Name: i.Name, Status: i.Status, Impact: i.Impact, URL: page.String()}
}

// statusHost returns the configured status host, or GitHub's.
func (c *Client) statusHost() string {
	if c.StatusHost == "" {
		return DefaultStatusHost
	}

	return c.StatusHost
}
