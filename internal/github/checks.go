package github

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
)

// Check names are read in batches of checksBatch pull requests, side by side, at most
// checksParallel at a time, each pull request under checksPrefix and its index, as in p0.
// A batch costs one point whatever its size; GitHub reads a pull request's checks one
// after another, so 50 came near its 10-second limit (one 504 in three, measured in
// November 2026), and a batch stops at 25.
const (
	checksBatch    = 25
	checksParallel = detailsParallel
	checksPrefix   = "p"

	// checksPage is the most checks read of one head commit: 50 pull requests held at most
	// 47 each (measured in November 2026).
	checksPage = 100
)

// checkFieldsFragment is what a read of a pull request's checks reads: its head commit and
// each check run and commit status of it, with the times it started and completed. It is
// modeled on the review sheet's checkList selection, which reads no times. Whether a check
// is required needs one pull request's ID as its argument (isRequired(pullRequestId:)),
// which a fragment can't take, so each pull request's alias asks for it beside the
// fragment, over the same contexts in the same order (checksQuery).
const checkFieldsFragment = `
fragment CheckFields on Node {
  __typename
  ... on PullRequest {
    id headRefOid
    checkList: statusCheckRollup {
      state
      contexts(first: 100) {
        totalCount
        pageInfo { hasNextPage }
        nodes {
          __typename
          ... on CheckRun {
            name conclusion status detailsUrl startedAt completedAt
            checkSuite { app { name } workflowRun { workflow { name } } }
          }
          ... on StatusContext { context state targetUrl createdAt }
        }
      }
    }
  }
}
`

// HeadChecks is one pull request's checks on its head commit, as read.
type HeadChecks struct {
	// Head is the head commit they were read at.
	Head string

	// Checks are its check runs and commit statuses, in GitHub's order. A name may repeat,
	// once for each event that ran it, each with its own outcome.
	Checks []HeadCheck

	// Complete reports that Checks holds every check of the commit: a commit with more
	// than checksPage is read in part, its names partial.
	Complete bool

	// Rollup is the commit's overall check state, over every check, read or not (SUCCESS,
	// FAILURE, ERROR, PENDING, or EXPECTED), "" with no checks.
	Rollup string
}

// Settled returns the newest completion time of the checks once every one has finished,
// and false while one runs or there are none. A check runs while it is pending, or has no
// completion time and wasn't skipped: a skipped run may never say it completed, and can
// say it completed before it started, so start times never count. A commit read in part
// has settled once every check read has finished and its Rollup says the rest have too,
// at the newest time read, since the rest's times aren't.
func (h HeadChecks) Settled() (time.Time, bool) {
	var newest time.Time

	for _, c := range h.Checks {
		if c.Running() {
			return time.Time{}, false
		}

		if c.CompletedAt.After(newest) {
			newest = c.CompletedAt
		}
	}

	switch {
	case len(h.Checks) == 0:
		return time.Time{}, false
	case h.Complete:
		return newest, true
	default:
		return newest, RollupFinished(h.Rollup)
	}
}

// RollupFinished reports whether a commit's overall check state says every check has
// finished: SUCCESS, FAILURE, or ERROR.
func RollupFinished(state string) bool {
	switch state {
	case "SUCCESS", "FAILURE", "ERROR":
		return true
	default:
		return false
	}
}

// HeadCheck is one check run or commit status of a head commit, with its times.
type HeadCheck struct {
	// Kind is CheckKindRun or CheckKindStatus.
	Kind string

	// Name is a run's name or a status's context. Workflow is the workflow a run belongs
	// to, "" for an app's own run (CodeQL's, OSV-Scanner's) and for a status; App is the
	// app whose suite holds a run.
	Name     string
	Workflow string
	App      string

	// Status is a run's status (QUEUED, IN_PROGRESS, COMPLETED, ...), and Conclusion its
	// conclusion once completed; a status's state is its Conclusion.
	Status     string
	Conclusion string

	// Outcome is how it counts: passed, failed, pending, or skipped.
	Outcome string

	// URL is its page on GitHub, or the page its status links to.
	URL string

	// Required reports a check the base branch requires, nil when GitHub didn't say.
	Required *bool

	// StartedAt and CompletedAt are when a run started and completed, and a status's
	// creation time for both; CompletedAt is zero while a run goes.
	StartedAt   time.Time
	CompletedAt time.Time
}

// Running reports a check not finished: pending, or with no completion time and not
// skipped, as the routines' facts count it.
func (c HeadCheck) Running() bool {
	return c.Outcome == CheckPending || (c.CompletedAt.IsZero() && c.Outcome != CheckSkipped)
}

// ChecksRead is what FetchChecks read.
type ChecksRead struct {
	// Items holds each pull request's checks, by node ID. One GitHub didn't answer for, or
	// that isn't a pull request, is left out.
	Items map[string]HeadChecks

	// RateLimit is the GraphQL budget after the latest request; its Cost is the sum of
	// every request's cost.
	RateLimit QueryRate

	// Requests is how many requests were sent.
	Requests int
}

// FetchChecks reads the checks of each pull request in ids on its head commit, in batches
// of checksBatch sent side by side, each with whether the base branch requires it. GitHub's
// errors about single pull requests leave those out; any other error fails the call.
func (c *Client) FetchChecks(ctx context.Context, tok Token, ids []string) (*ChecksRead, error) {
	read, err := c.fetchChecks(ctx, tok, ids)

	return read, Redact(err, tok)
}

// fetchChecks sends the batches and merges the answers.
func (c *Client) fetchChecks(ctx context.Context, tok Token, ids []string) (*ChecksRead, error) {
	batches := slices.Collect(slices.Chunk(uniqueIDs(ids), checksBatch))
	answers := make([]*ChecksRead, len(batches))

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(checksParallel)

	for i, batch := range batches {
		g.Go(func() error {
			answer, err := c.sendChecks(gctx, tok, batch)
			answers[i] = answer

			return err
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	out := &ChecksRead{Items: make(map[string]HeadChecks, len(ids))}

	for _, a := range answers {
		if a == nil {
			continue
		}

		for id, h := range a.Items {
			out.Items[id] = h
		}

		cost := out.RateLimit.Cost + a.RateLimit.Cost
		if out.Requests == 0 || a.RateLimit.Later(out.RateLimit) {
			out.RateLimit = a.RateLimit
		}

		out.RateLimit.Cost = cost
		out.Requests += a.Requests
	}

	return out, nil
}

// uniqueIDs returns ids without "" and without repeats, in order.
func uniqueIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))

	for _, id := range ids {
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}

	return out
}

// checksQuery asks for each pull request's checks under its own alias, with whether each
// is required, which takes the pull request's own ID. The IDs are written with
// graphQLString, so none can break out of its argument.
func checksQuery(ids []string) (string, error) {
	var b strings.Builder

	b.WriteString("query Checks {\n")

	for i, id := range ids {
		literal, err := graphQLString(id)
		if err != nil {
			return "", fmt.Errorf("encode the pull request's ID: %w", err)
		}

		fmt.Fprintf(&b, "  %s: node(id: %s) {\n    ...CheckFields\n", batchAlias(checksPrefix, i), literal)
		fmt.Fprintf(&b, "    ... on PullRequest {\n      req: statusCheckRollup {\n        contexts(first: %d) {\n", checksPage)
		b.WriteString("          nodes {\n")
		fmt.Fprintf(&b, "            ... on CheckRun { isRequired(pullRequestId: %s) }\n", literal)
		fmt.Fprintf(&b, "            ... on StatusContext { isRequired(pullRequestId: %s) }\n", literal)
		b.WriteString("          }\n        }\n      }\n    }\n  }\n")
	}

	b.WriteString("  rateLimit { cost remaining resetAt limit used }\n}\n")
	b.WriteString(checkFieldsFragment)

	return b.String(), nil
}

// sendChecks sends one batch's request and reads each pull request's answer.
func (c *Client) sendChecks(ctx context.Context, tok Token, ids []string) (*ChecksRead, error) {
	query, err := checksQuery(ids)
	if err != nil {
		return nil, err
	}

	resp, _, err := postGraphQL[map[string]json.RawMessage](ctx, c, tok, "checks", graphQLRequest{Query: query}, unlessData)
	if err != nil {
		return nil, err
	}

	data := *resp.Data
	out := &ChecksRead{Items: make(map[string]HeadChecks, len(ids)), RateLimit: aliasedRate(data), Requests: 1}

	for i := range ids {
		id, h, ok, err := readHeadChecks(data[batchAlias(checksPrefix, i)])
		if err != nil {
			return nil, err
		}

		if ok {
			out.Items[id] = h
		}
	}

	return out, nil
}

// aliasedRate reads the budget an aliased query's answer reports under rateLimit, zero
// when it reports none.
func aliasedRate(data map[string]json.RawMessage) QueryRate {
	var rate graphQLRateLimit

	if raw, ok := data["rateLimit"]; !ok || json.Unmarshal(raw, &rate) != nil {
		return QueryRate{}
	}

	return rate.queryRate()
}

// readHeadChecks reads one pull request's answer; ok is false for a null one, or one
// that isn't a pull request.
func readHeadChecks(raw json.RawMessage) (string, HeadChecks, bool, error) {
	if isNull(raw) {
		return "", HeadChecks{}, false, nil
	}

	var n checksNode
	if err := json.Unmarshal(raw, &n); err != nil {
		return "", HeadChecks{}, false, fmt.Errorf("%w: decode the checks: %w", ErrMalformedResponse, err)
	}

	id, h, ok := n.headChecks()

	return id, h, ok, nil
}

// checksNode is one pull request's answer: its checks, and, over the same contexts in the
// same order, whether each is required.
type checksNode struct {
	TypeName   string `json:"__typename"`
	ID         string `json:"id"`
	HeadRefOID string `json:"headRefOid"`
	CheckList  *struct {
		State    string `json:"state"`
		Contexts struct {
			TotalCount int `json:"totalCount"`
			PageInfo   struct {
				HasNextPage bool `json:"hasNextPage"`
			} `json:"pageInfo"`
			Nodes []*checkContext `json:"nodes"`
		} `json:"contexts"`
	} `json:"checkList"`
	Req *struct {
		Contexts struct {
			Nodes []*struct {
				IsRequired *bool `json:"isRequired"`
			} `json:"nodes"`
		} `json:"contexts"`
	} `json:"req"`
}

// checkContext is one check run or commit status as CheckFields reads it.
type checkContext struct {
	TypeName    string    `json:"__typename"`
	Name        string    `json:"name"`
	Conclusion  string    `json:"conclusion"`
	Status      string    `json:"status"`
	DetailsURL  string    `json:"detailsUrl"`
	StartedAt   time.Time `json:"startedAt"`
	CompletedAt time.Time `json:"completedAt"`
	CheckSuite  *struct {
		App *struct {
			Name string `json:"name"`
		} `json:"app"`
		WorkflowRun *struct {
			Workflow *struct {
				Name string `json:"name"`
			} `json:"workflow"`
		} `json:"workflowRun"`
	} `json:"checkSuite"`
	Context   string    `json:"context"`
	State     string    `json:"state"`
	TargetURL string    `json:"targetUrl"`
	CreatedAt time.Time `json:"createdAt"`
}

// headChecks converts a pull request's answer; ok is false for anything else. A head
// commit with no checks is complete with none.
func (n *checksNode) headChecks() (string, HeadChecks, bool) {
	if n.TypeName != typePullRequest || n.ID == "" {
		return "", HeadChecks{}, false
	}

	out := HeadChecks{Head: n.HeadRefOID, Checks: []HeadCheck{}, Complete: true}
	if n.CheckList == nil {
		return n.ID, out, true
	}

	out.Rollup = n.CheckList.State
	required := n.required()

	ctxs := n.CheckList.Contexts
	for i, c := range ctxs.Nodes {
		check, ok := c.check()
		if !ok {
			continue
		}

		if i < len(required) {
			check.Required = required[i]
		}

		out.Checks = append(out.Checks, check)
	}

	out.Complete = !ctxs.PageInfo.HasNextPage && ctxs.TotalCount <= len(ctxs.Nodes)

	return n.ID, out, true
}

// required lists whether each context is required, in the contexts' order, nil where
// GitHub didn't say.
func (n *checksNode) required() []*bool {
	if n.Req == nil {
		return nil
	}

	out := make([]*bool, 0, len(n.Req.Contexts.Nodes))

	for _, r := range n.Req.Contexts.Nodes {
		var required *bool
		if r != nil {
			required = r.IsRequired
		}

		out = append(out, required)
	}

	return out
}

// check converts one context; ok is false for a null one or another type.
func (c *checkContext) check() (HeadCheck, bool) {
	if c == nil {
		return HeadCheck{}, false
	}

	switch c.TypeName {
	case "CheckRun":
		out := c.run()

		return out, out.Name != ""
	case "StatusContext":
		out := HeadCheck{
			Kind: CheckKindStatus, Name: c.Context, Conclusion: c.State, URL: c.TargetURL,
			StartedAt: c.CreatedAt.UTC(), Outcome: statusOutcome(c.State).word(),
		}

		if out.Outcome != CheckPending {
			out.CompletedAt = out.StartedAt
		}

		return out, out.Name != ""
	default:
		return HeadCheck{}, false
	}
}

// run converts a check run, with the workflow and the app its suite names.
func (c *checkContext) run() HeadCheck {
	out := HeadCheck{
		Kind: CheckKindRun, Name: c.Name, Status: c.Status, Conclusion: c.Conclusion, URL: c.DetailsURL,
		StartedAt: c.StartedAt.UTC(), CompletedAt: c.CompletedAt.UTC(), Outcome: runOutcome(c.Status, c.Conclusion),
	}

	s := c.CheckSuite
	if s == nil {
		return out
	}

	if s.App != nil {
		out.App = s.App.Name
	}

	if s.WorkflowRun != nil && s.WorkflowRun.Workflow != nil {
		out.Workflow = s.WorkflowRun.Workflow.Name
	}

	return out
}

// runOutcome is how a check run counts: pending until it completes, then by its
// conclusion, as the details count it (checkRunOutcome).
func runOutcome(status, conclusion string) string {
	if status != "COMPLETED" || conclusion == "" {
		return CheckPending
	}

	return checkRunOutcome(conclusion).word()
}
