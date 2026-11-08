package github

import (
	"context"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/mrz1836/nilbox/internal/textutil"
)

// Details requests are sent in batches, side by side. GitHub resolves the items of one
// request one after another (about 150 ms each, measured), so a large inbox in a single
// request would take seconds, while a few concurrent batches return within the inbox
// query's own time. Each request costs at least one point, so a batch is never smaller
// than detailsMinBatch, and at most detailsParallel are in flight: a large inbox gets
// bigger batches rather than more requests, up to detailsMaxBatch (GitHub accepts 100).
const (
	detailsMinBatch = 10
	detailsMaxBatch = 50
	detailsParallel = 4
)

// detailsQuery reads what an inbox card shows about each pull request and issue, by node
// ID: its labels and comment count and, for a pull request, its size, commits, merge
// state, check results, and reviews. It is a separate request from the inbox query
// because GitHub prices nested lists per result slot: inside the three 100-result
// searches these fields would cost about 12 points, while asking only for the items the
// inbox returned costs one or two.
//
// The counts per check state come without listing the checks themselves, and review
// requests and latest reviews come with their logins so a reviewer is counted once. The
// head commit's SHA tells a snooze when a pull request was pushed, force-pushes included.
// The fields are a fragment the item query spreads too, so a card refreshed on its own
// reads exactly what a whole refresh reads.
const detailsQuery = detailsQueryHead + detailFieldsFragment

// detailsProjectsQuery is detailsQuery that also reads the projects each issue is in
// (see issueProjectFields).
const detailsProjectsQuery = detailsQueryHead + detailFieldsProjectsFragment

// detailsQueryHead is the details query without its fragment.
const detailsQueryHead = `query Details($ids: [ID!]!) {
  nodes(ids: $ids) { ...DetailFields }
  rateLimit { cost remaining resetAt limit used }
}
`

// detailFieldsFragment is what an inbox card shows about one pull request or issue, and
// detailFieldsProjectsFragment is the same with the projects an issue is in.
const (
	detailFieldsFragment         = detailFieldsHead + detailFieldsTail
	detailFieldsProjectsFragment = detailFieldsHead + issueProjectFields + detailFieldsTail + projectFieldNameFragment
)

// detailFieldsHead is the DetailFields fragment up to the end of an issue's fields, which
// detailFieldsTail closes.
const detailFieldsHead = `
fragment DetailFields on Node {
  __typename
  ... on PullRequest {
    id additions deletions changedFiles totalCommentsCount reviewDecision mergeable headRefOid
    author { login }
    commits { totalCount }
    labels(first: 50) { nodes { name color } }
    reviewRequests(first: 50) {
      nodes {
        requestedReviewer {
          __typename
          ... on User { login }
          ... on Bot { login }
          ... on Mannequin { login }
          ... on Team { slug }
        }
      }
    }
    latestReviews(first: 50) { nodes { state author { login } } }
    statusCheckRollup {
      state
      contexts {
        checkRunCountsByState { state count }
        statusContextCountsByState { state count }
      }
    }
  }
  ... on Issue {
    id
    comments { totalCount }
    labels(first: 50) { nodes { name color } }
    subIssuesSummary { total completed }`

// detailFieldsTail closes detailFieldsHead.
const detailFieldsTail = `
  }
}
`

// issueProjectFields reads the projects an issue is in, with its option in each one's
// Status field, the field every project starts with, and its values in the project's
// other fields: single select, number, text, date, and iteration. GitHub answers them only
// for a token with the read:project scope. Items archived from a project are left out; a
// closed project is dropped when the answer is read. A project whose Status field was
// renamed or deleted answers no status.
//
// GitHub prices the field values once per project item, so the first five projects are
// read, which covers an issue in practice: a batch of 50 issues costs four points, where
// it costs one without the projects. Each value names its field and the field's type, so
// the built-in fields (the title, labels, assignees, and so on) can be told apart.
const issueProjectFields = `
    projectItems(first: 5, includeArchived: false) {
      nodes {
        project { title url closed }
        status: fieldValueByName(name: "Status") {
          ... on ProjectV2ItemFieldSingleSelectValue { name color }
        }
        fieldValues(first: 20) {
          nodes {
            __typename
            ... on ProjectV2ItemFieldSingleSelectValue { name color field { ...ProjectFieldName } }
            ... on ProjectV2ItemFieldNumberValue { number field { ...ProjectFieldName } }
            ... on ProjectV2ItemFieldTextValue { text field { ...ProjectFieldName } }
            ... on ProjectV2ItemFieldDateValue { date field { ...ProjectFieldName } }
            ... on ProjectV2ItemFieldIterationValue { title field { ...ProjectFieldName } }
          }
        }
      }
    }`

// projectFieldNameFragment names a project field and its type. Only the query that reads
// projects spreads it.
const projectFieldNameFragment = `
fragment ProjectFieldName on ProjectV2FieldConfiguration {
  ... on ProjectV2FieldCommon { name dataType }
}
`

// Review states a reviewer can leave; DISMISSED and PENDING reviews do not count.
const (
	reviewApproved         = "APPROVED"
	reviewChangesRequested = "CHANGES_REQUESTED"
	reviewCommented        = "COMMENTED"
)

// DetailOptions choose what a details request reads beyond the fields it always reads.
type DetailOptions struct {
	// Projects reads the projects each issue is in, with its status in each, which needs
	// the token's read:project scope. When GitHub refuses them for want of a scope, the
	// request is sent again without them, and the answer says so.
	Projects bool
}

// Details is what FetchDetails learned.
type Details struct {
	// Items maps each item's node ID to its details. An ID GitHub could not resolve, such
	// as one deleted since the inbox query, is left out.
	Items map[string]ItemDetails

	// RateLimit is the GraphQL budget after the last request; its Cost is the sum of
	// every request's cost.
	RateLimit QueryRate

	// Requests is how many requests were made: 0 when there were no IDs.
	Requests int

	// ProjectsRefused reports that GitHub refused the projects for want of a scope, so
	// the details were read without them.
	ProjectsRefused bool
}

// ItemDetails is what an inbox card shows about one pull request or issue. The pull
// request fields are zero for an issue.
type ItemDetails struct {
	Labels []Label

	// Comments counts the conversation's comments; for a pull request it includes the
	// review comments.
	Comments int

	Additions    int
	Deletions    int
	ChangedFiles int
	Commits      int

	// HeadOID is the SHA of a pull request's head commit; it changes on every push.
	HeadOID string

	// Mergeable is MERGEABLE, CONFLICTING, or UNKNOWN while GitHub is still working it out.
	Mergeable string

	Checks  Checks
	Reviews Reviews

	// ReviewRequests is who a pull request asks to review, as of the read; empty for an
	// issue.
	ReviewRequests ReviewRequests

	// Projects lists the open projects an issue is in, in GitHub's order, when they were
	// read (see DetailOptions). It is nil when they were not, when the issue is in none,
	// and for a pull request.
	Projects []ProjectItem

	// SubIssues counts an issue's sub-issues, and those closed as done. It is zero for a
	// pull request and for an issue without sub-issues.
	SubIssues SubIssues
}

// SubIssues counts an issue's sub-issues.
type SubIssues struct {
	Total int

	// Completed is how many are closed as done; never more than Total.
	Completed int
}

// ProjectItem is an issue's place in one GitHub project.
type ProjectItem struct {
	// Title is the project's title, and URL its page on GitHub.
	Title string
	URL   string

	// Status is the issue's option in the project's Status field, "" when it has none.
	Status string

	// StatusColor is the status option's color as "#rrggbb", "" when it has none.
	StatusColor string

	// Fields are the issue's values in the project's other fields, such as Priority or
	// Size, in GitHub's order. Fields without a value, the Status field, and GitHub's
	// built-in fields are left out.
	Fields []ProjectField
}

// ProjectField is an issue's value in one of a project's fields.
type ProjectField struct {
	// Name is the field's name, such as "Priority".
	Name string

	// Value is the value as the project shows it: an option's name, a number, text, a date
	// such as "Jun 27, 2026", or an iteration's title.
	Value string

	// Color is a single-select option's color as "#rrggbb", and "" for any other value.
	Color string
}

// Label is one label on an item.
type Label struct {
	Name string

	// Color is "#rrggbb" in lower case, or "" when GitHub's value is not a color.
	Color string
}

// Checks counts the head commit's check runs and commit statuses by outcome.
type Checks struct {
	// State is GitHub's overall result: SUCCESS, FAILURE, ERROR, PENDING, or EXPECTED,
	// and "" when the head commit has no checks.
	State string

	Passed  int
	Failed  int
	Pending int
	Skipped int
}

// Reviews counts who was asked to review a pull request and who has.
type Reviews struct {
	// Decision is APPROVED, CHANGES_REQUESTED, or REVIEW_REQUIRED, and "" when the
	// repository requires no review.
	Decision string

	// Reviewers is everyone asked to review or who has reviewed, the author excluded.
	Reviewers int

	// Reviewed is how many of them have reviewed and are not asked to review again.
	Reviewed int

	// Approvals and ChangesRequested count the reviewers whose latest review is one.
	Approvals        int
	ChangesRequested int
}

// FetchDetails reads the details of the items with the given node IDs, in concurrent
// batches, with what opts adds. GitHub's errors about single items it cannot resolve are
// not failures: those items are simply left out.
func (c *Client) FetchDetails(ctx context.Context, tok Token, ids []string, opts DetailOptions) (*Details, error) {
	details, err := c.fetchDetails(ctx, tok, ids, opts)

	return details, Redact(err, tok)
}

// Add merges other into d: its items, its requests, and its cost. The budget reading
// kept is the latest of the two (see QueryRate.Later).
func (d *Details) Add(other *Details) {
	if other == nil {
		return
	}

	if d.Items == nil {
		d.Items = make(map[string]ItemDetails, len(other.Items))
	}

	maps.Copy(d.Items, other.Items)

	cost := d.RateLimit.Cost + other.RateLimit.Cost
	if d.Requests == 0 || (other.Requests > 0 && other.RateLimit.Later(d.RateLimit)) {
		d.RateLimit = other.RateLimit
	}

	d.RateLimit.Cost = cost
	d.Requests += other.Requests
	d.ProjectsRefused = d.ProjectsRefused || other.ProjectsRefused
}

// detailsBatches splits ids into the batches FetchDetails sends.
func detailsBatches(ids []string) [][]string {
	size := min(detailsMaxBatch, max(detailsMinBatch, (len(ids)+detailsParallel-1)/detailsParallel))

	return slices.Collect(slices.Chunk(ids, size))
}

// fetchDetails sends the batches side by side and merges the answers in batch order.
func (c *Client) fetchDetails(ctx context.Context, tok Token, ids []string, opts DetailOptions) (*Details, error) {
	batches := detailsBatches(ids)
	answers := make([]*Details, len(batches))

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(detailsParallel)

	for i, batch := range batches {
		g.Go(func() error {
			answer, err := c.fetchDetailsBatch(gctx, tok, batch, opts)
			answers[i] = answer

			return err
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	out := &Details{Items: make(map[string]ItemDetails, len(ids))}
	for _, answer := range answers {
		out.Add(answer)
	}

	return out, nil
}

// fetchDetailsBatch sends the details query for one batch of IDs. When GitHub refuses
// the projects opts asks for, for want of a scope, it sends the batch again without them.
func (c *Client) fetchDetailsBatch(ctx context.Context, tok Token, ids []string, opts DetailOptions) (*Details, error) {
	answer, err := c.sendDetails(ctx, tok, ids, opts)
	if !opts.Projects || !refusedForScope(err) {
		return answer, err
	}

	if answer, err = c.sendDetails(ctx, tok, ids, DetailOptions{}); err != nil {
		return nil, err
	}

	answer.ProjectsRefused = true

	return answer, nil
}

// sendDetails sends the details query for one batch of IDs, with what opts adds. Errors
// with data are about single items (one deleted or hidden since the inbox query), and
// their nodes come back null; GitHub refusing the projects with the rest of the data
// counts as a refusal. Errors without data are a real failure.
func (c *Client) sendDetails(ctx context.Context, tok Token, ids []string, opts DetailOptions) (*Details, error) {
	req := graphQLRequest{Query: opts.detailsQuery(), Variables: map[string]any{"ids": ids}}

	resp, _, err := postGraphQL[detailsData](ctx, c, tok, "details", req, unlessData)
	if err != nil {
		return nil, err
	}

	answer := resp.Data.details()
	answer.ProjectsRefused = opts.Projects && scopeRefused(resp.Errors)

	return answer, nil
}

// detailsQuery returns the details query that reads what o asks for.
func (o DetailOptions) detailsQuery() string {
	if o.Projects {
		return detailsProjectsQuery
	}

	return detailsQuery
}

// itemQuery returns the item query that reads what o asks for.
func (o DetailOptions) itemQuery() string {
	if o.Projects {
		return itemProjectsQuery
	}

	return itemQuery
}

// detailsData is the details query's data object.
type detailsData struct {
	Nodes     []*detailsNode   `json:"nodes"`
	RateLimit graphQLRateLimit `json:"rateLimit"`
}

// details converts one batch's answer: its items, its budget reading, and one request.
func (d *detailsData) details() *Details {
	out := &Details{Items: make(map[string]ItemDetails, len(d.Nodes)), RateLimit: d.RateLimit.queryRate(), Requests: 1}

	for _, n := range d.Nodes {
		if id, item, ok := n.details(); ok {
			out.Items[id] = item
		}
	}

	return out
}

// queryRate converts the query's rateLimit object.
func (r graphQLRateLimit) queryRate() QueryRate {
	return QueryRate{Cost: r.Cost, Limit: r.Limit, Remaining: r.Remaining, Used: r.Used, ResetAt: r.ResetAt.UTC()}
}

// detailsNode is one node of the details query: a pull request, an issue, or null.
// GitHub's nulls decode to zero values.
type detailsNode struct {
	TypeName           string           `json:"__typename"`
	ID                 string           `json:"id"`
	Additions          int              `json:"additions"`
	Deletions          int              `json:"deletions"`
	ChangedFiles       int              `json:"changedFiles"`
	TotalCommentsCount int              `json:"totalCommentsCount"`
	ReviewDecision     string           `json:"reviewDecision"`
	Mergeable          string           `json:"mergeable"`
	HeadRefOID         string           `json:"headRefOid"`
	Author             *graphQLActor    `json:"author"`
	Commits            graphQLCount     `json:"commits"`
	Comments           graphQLCount     `json:"comments"`
	Labels             graphQLLabels    `json:"labels"`
	ReviewRequests     graphQLRequests  `json:"reviewRequests"`
	LatestReviews      graphQLReviews   `json:"latestReviews"`
	StatusCheckRollup  *graphQLRollup   `json:"statusCheckRollup"`
	ProjectItems       graphQLProjects  `json:"projectItems"`
	SubIssuesSummary   graphQLSubIssues `json:"subIssuesSummary"`
}

// graphQLSubIssues is an issue's sub-issue counts.
type graphQLSubIssues struct {
	Total     int `json:"total"`
	Completed int `json:"completed"`
}

// subIssues converts the counts, keeping them in bounds.
func (s graphQLSubIssues) subIssues() SubIssues {
	total := max(s.Total, 0)

	return SubIssues{Total: total, Completed: min(max(s.Completed, 0), total)}
}

// graphQLCount is a connection asked only for its size.
type graphQLCount struct {
	TotalCount int `json:"totalCount"`
}

// graphQLLabels is an item's labels.
type graphQLLabels struct {
	Nodes []*struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	} `json:"nodes"`
}

// graphQLProjects is an issue's places in projects.
type graphQLProjects struct {
	Nodes []*graphQLProjectItem `json:"nodes"`
}

// graphQLProjectItem is an issue's place in one project. A project the token cannot see
// comes back null.
type graphQLProjectItem struct {
	Project *struct {
		Title  string `json:"title"`
		URL    string `json:"url"`
		Closed bool   `json:"closed"`
	} `json:"project"`
	Status *struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	} `json:"status"`
	FieldValues struct {
		Nodes []*graphQLFieldValue `json:"nodes"`
	} `json:"fieldValues"`
}

// graphQLFieldValue is an item's value in one project field. Which value field is set
// depends on the type; the others decode to zero values.
type graphQLFieldValue struct {
	TypeName string   `json:"__typename"`
	Name     string   `json:"name"`
	Color    string   `json:"color"`
	Number   *float64 `json:"number"`
	Text     string   `json:"text"`
	Date     string   `json:"date"`
	Title    string   `json:"title"`
	Field    *struct {
		Name     string `json:"name"`
		DataType string `json:"dataType"`
	} `json:"field"`
}

// graphQLRequests is a pull request's pending review requests.
type graphQLRequests struct {
	Nodes []*struct {
		RequestedReviewer *struct {
			TypeName string `json:"__typename"`
			Login    string `json:"login"`
			Slug     string `json:"slug"`
		} `json:"requestedReviewer"`
	} `json:"nodes"`
}

// graphQLReviews is a pull request's latest review per reviewer.
type graphQLReviews struct {
	Nodes []*struct {
		State  string        `json:"state"`
		Author *graphQLActor `json:"author"`
	} `json:"nodes"`
}

// graphQLRollup is the head commit's combined check result.
type graphQLRollup struct {
	State    string `json:"state"`
	Contexts struct {
		CheckRunCountsByState      []graphQLStateCount `json:"checkRunCountsByState"`
		StatusContextCountsByState []graphQLStateCount `json:"statusContextCountsByState"`
	} `json:"contexts"`
}

// graphQLStateCount is how many checks are in one state.
type graphQLStateCount struct {
	State string `json:"state"`
	Count int    `json:"count"`
}

// details converts a pull request or issue node; ok is false for null and other nodes.
func (n *detailsNode) details() (id string, item ItemDetails, ok bool) {
	if n == nil || n.ID == "" {
		return "", ItemDetails{}, false
	}

	switch n.TypeName {
	case typeIssue:
		return n.ID, ItemDetails{
			Labels: n.Labels.labels(), Comments: n.Comments.TotalCount, Projects: n.ProjectItems.projects(),
			SubIssues: n.SubIssuesSummary.subIssues(),
		}, true
	case typePullRequest:
		author := ""
		if n.Author != nil {
			author = n.Author.Login
		}

		return n.ID, ItemDetails{
			Labels:         n.Labels.labels(),
			Comments:       n.TotalCommentsCount,
			Additions:      n.Additions,
			Deletions:      n.Deletions,
			ChangedFiles:   n.ChangedFiles,
			Commits:        n.Commits.TotalCount,
			HeadOID:        n.HeadRefOID,
			Mergeable:      n.Mergeable,
			Checks:         n.StatusCheckRollup.checks(),
			Reviews:        n.reviews(author),
			ReviewRequests: n.ReviewRequests.requests(),
		}, true
	default:
		return "", ItemDetails{}, false
	}
}

// labels converts the label nodes, skipping nulls and unnamed labels.
func (l graphQLLabels) labels() []Label {
	out := make([]Label, 0, len(l.Nodes))

	for _, n := range l.Nodes {
		if n != nil && n.Name != "" {
			out = append(out, Label{Name: n.Name, Color: labelColor(n.Color)})
		}
	}

	return out
}

// projects converts the project items, skipping nulls, the projects the token cannot
// see, and closed projects. It returns nil when none are left.
func (p graphQLProjects) projects() []ProjectItem {
	var out []ProjectItem

	for _, n := range p.Nodes {
		if place, ok := n.place(); ok {
			out = append(out, place)
		}
	}

	return out
}

// place converts one project item; ok is false for null, a project the token cannot see,
// and a closed project.
func (n *graphQLProjectItem) place() (ProjectItem, bool) {
	if n == nil || n.Project == nil || n.Project.Closed || n.Project.Title == "" {
		return ProjectItem{}, false
	}

	place := ProjectItem{Title: n.Project.Title, URL: n.Project.URL}
	if n.Status != nil && n.Status.Name != "" {
		place.Status, place.StatusColor = n.Status.Name, projectColor(n.Status.Color)
	}

	for _, v := range n.FieldValues.Nodes {
		if field, ok := v.field(); ok {
			place.Fields = append(place.Fields, field)
		}
	}

	return place, true
}

// Project field data types the inbox leaves out of an item's fields: the Status field,
// which it reads on its own, and the title, which the card shows already.
const (
	fieldTypeTitle        = "TITLE"
	fieldTypeSingleSelect = "SINGLE_SELECT"
	statusFieldName       = "Status"
)

// maxFieldRunes caps a text field's value, which a project lets run long.
const maxFieldRunes = 120

// field converts one field value; ok is false for a null value, one without a field or
// value, the title, the Status field, and the built-in fields this build does not read.
func (v *graphQLFieldValue) field() (ProjectField, bool) {
	if !v.kept() {
		return ProjectField{}, false
	}

	value, color := v.value()

	return ProjectField{Name: v.Field.Name, Value: value, Color: color}, value != ""
}

// kept reports whether the value is one of a field the inbox shows: one with a name, and
// neither the title nor the Status field.
func (v *graphQLFieldValue) kept() bool {
	if v == nil || v.Field == nil || v.Field.Name == "" || v.Field.DataType == fieldTypeTitle {
		return false
	}

	return v.Field.DataType != fieldTypeSingleSelect || v.Field.Name != statusFieldName
}

// value is the value as the project shows it, and a single-select option's color; both ""
// for a type this build does not read.
func (v *graphQLFieldValue) value() (value, color string) {
	switch v.TypeName {
	case "ProjectV2ItemFieldSingleSelectValue":
		return v.Name, projectColor(v.Color)
	case "ProjectV2ItemFieldNumberValue":
		if v.Number == nil {
			return "", ""
		}

		return strconv.FormatFloat(*v.Number, 'f', -1, 64), ""
	case "ProjectV2ItemFieldTextValue":
		return textutil.Truncate(strings.Join(strings.Fields(v.Text), " "), maxFieldRunes), ""
	case "ProjectV2ItemFieldDateValue":
		return fieldDate(v.Date), ""
	case "ProjectV2ItemFieldIterationValue":
		return v.Title, ""
	default:
		return "", ""
	}
}

// fieldDate writes a date field's "2026-06-27" as "Jun 27, 2026", or keeps a value it
// cannot read as it is.
func fieldDate(date string) string {
	day, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return date
	}

	return day.Format("Jan 2, 2006")
}

// projectColor turns a project option's color, such as BLUE, into "#rrggbb", or "" for
// a color this build does not know. The shades are GitHub's mid-tones, which read on a
// light and a dark background alike.
func projectColor(color string) string {
	switch color {
	case "GRAY":
		return "#8c959f"
	case "BLUE":
		return "#218bff"
	case "GREEN":
		return "#2da44e"
	case "YELLOW":
		return "#bf8700"
	case "ORANGE":
		return "#e16f24"
	case "RED":
		return "#fa4549"
	case "PINK":
		return "#e85aad"
	case "PURPLE":
		return "#a475f9"
	default:
		return ""
	}
}

// labelColor turns GitHub's "0e8a16" into "#0e8a16", or "" when it is not a color.
func labelColor(color string) string {
	if len(color) != 6 {
		return ""
	}

	for _, r := range color {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return ""
		}
	}

	return "#" + strings.ToLower(color)
}

// checks counts the rollup's check runs and statuses by outcome. A nil rollup means the
// head commit has no checks.
func (r *graphQLRollup) checks() Checks {
	if r == nil {
		return Checks{}
	}

	c := Checks{State: r.State}

	for _, s := range r.Contexts.CheckRunCountsByState {
		c.add(checkRunOutcome(s.State), s.Count)
	}

	for _, s := range r.Contexts.StatusContextCountsByState {
		c.add(statusOutcome(s.State), s.Count)
	}

	return c
}

// outcome is where a check state is counted.
type outcome int

const (
	outcomePending outcome = iota
	outcomePassed
	outcomeFailed
	outcomeSkipped
)

// add counts n checks with an outcome.
func (c *Checks) add(o outcome, n int) {
	switch o {
	case outcomePassed:
		c.Passed += n
	case outcomeFailed:
		c.Failed += n
	case outcomeSkipped:
		c.Skipped += n
	case outcomePending:
		c.Pending += n
	}
}

// checkRunOutcome maps a check run's state. Neutral runs pass, as GitHub treats them;
// a state not listed is still in progress.
func checkRunOutcome(state string) outcome {
	switch state {
	case "SUCCESS", "NEUTRAL":
		return outcomePassed
	case "SKIPPED":
		return outcomeSkipped
	case "FAILURE", "TIMED_OUT", "CANCELLED", "ACTION_REQUIRED", "STARTUP_FAILURE", "STALE": //nolint:misspell // GitHub's enum value
		return outcomeFailed
	default:
		return outcomePending
	}
}

// statusOutcome maps a commit status's state.
func statusOutcome(state string) outcome {
	switch state {
	case "SUCCESS":
		return outcomePassed
	case "FAILURE", "ERROR":
		return outcomeFailed
	default:
		return outcomePending
	}
}

// reviews counts the pull request's reviewers: everyone with a pending request plus
// everyone whose latest review counts, the author left out (replying in a review thread
// files a review by the author). A reviewer asked again after reviewing owes a new
// review, so is not counted as having reviewed.
func (n *detailsNode) reviews(author string) Reviews {
	r := Reviews{Decision: n.ReviewDecision}
	requested := n.ReviewRequests.reviewers()
	reviewed := n.LatestReviews.reviewers(author, &r)

	r.Reviewers = len(requested)

	for key := range reviewed {
		if !requested[key] {
			r.Reviewers++
			r.Reviewed++
		}
	}

	return r
}

// requests lists who the pull request asks to review: the people and bots by login, and
// the teams by slug, each once, in GitHub's order.
func (q graphQLRequests) requests() ReviewRequests {
	out := ReviewRequests{Users: []string{}, Teams: []string{}}
	seen := make(map[string]bool, len(q.Nodes))

	for _, req := range q.Nodes {
		if req == nil || req.RequestedReviewer == nil {
			continue
		}

		who := req.RequestedReviewer

		key := reviewerKey(who.TypeName, who.Login, who.Slug)
		if key == "" || seen[key] {
			continue
		}

		seen[key] = true

		if who.TypeName == "Team" {
			out.Teams = append(out.Teams, who.Slug)
		} else {
			out.Users = append(out.Users, who.Login)
		}
	}

	return out
}

// reviewers is everyone with a pending review request, by reviewerKey.
func (q graphQLRequests) reviewers() map[string]bool {
	out := make(map[string]bool, len(q.Nodes))

	for _, req := range q.Nodes {
		if req == nil || req.RequestedReviewer == nil {
			continue
		}

		who := req.RequestedReviewer
		if key := reviewerKey(who.TypeName, who.Login, who.Slug); key != "" {
			out[key] = true
		}
	}

	return out
}

// reviewers is everyone but author whose latest review counts (approved, changes
// requested, or commented), by reviewerKey. It counts the approvals and change requests
// into r as it goes.
func (q graphQLReviews) reviewers(author string, r *Reviews) map[string]bool {
	out := make(map[string]bool, len(q.Nodes))

	for _, rev := range q.Nodes {
		if rev == nil || rev.Author == nil || rev.Author.Login == "" || strings.EqualFold(rev.Author.Login, author) {
			continue
		}

		switch rev.State {
		case reviewApproved:
			r.Approvals++
		case reviewChangesRequested:
			r.ChangesRequested++
		case reviewCommented:
		default:
			continue
		}

		out[reviewerKey("User", rev.Author.Login, "")] = true
	}

	return out
}

// reviewerKey identifies a reviewer: a team by its slug, anyone else by login, ignoring
// case.
func reviewerKey(typeName, login, slug string) string {
	if typeName == "Team" {
		if slug == "" {
			return ""
		}

		return "team:" + strings.ToLower(slug)
	}

	if login == "" {
		return ""
	}

	return "user:" + strings.ToLower(login)
}
