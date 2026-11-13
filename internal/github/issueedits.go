package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// RepoLabels are the labels of an issue's repository, the first hundred in name order, and
// how many it has.
type RepoLabels struct {
	Labels []RepoLabel
	Total  int
}

// RepoLabel is one of a repository's labels: its name, its color as "#rrggbb" or "", and
// what it says it is for.
type RepoLabel struct {
	Name        string
	Color       string
	Description string
}

// Assignable is someone an issue can be assigned to: their login, and their name, or "".
type Assignable struct {
	Login string
	Name  string
}

// ErrInvalidLabel is returned for a label name no label can have, before anything is sent.
var ErrInvalidLabel = errors.New("invalid label name")

// maxLabelRunes is the longest label name GitHub takes.
const maxLabelRunes = 50

// repoLabelsQuery reads the labels of an issue's repository, by the issue's node ID, so
// the caller needs no more than the issue. It costs one point.
const repoLabelsQuery = `query RepoLabels($id: ID!) {
  node(id: $id) {
    __typename
    ... on Issue {
      repository {
        labels(first: 100, orderBy: {field: NAME, direction: ASC}) { totalCount nodes { name color description } }
      }
    }
  }
  rateLimit { cost remaining resetAt limit used }
}
`

// assignableQuery reads the people an issue can be assigned to whose login or name
// matches $query, by the issue's node ID. It costs one point.
const assignableQuery = `query Assignable($id: ID!, $query: String!) {
  node(id: $id) {
    __typename
    ... on Issue {
      repository {
        assignableUsers(first: 20, query: $query) { nodes { login name } }
      }
    }
  }
  rateLimit { cost remaining resetAt limit used }
}
`

// FetchRepoLabels reads the labels of the repository of the issue with node ID id, the
// first hundred in name order, with one GraphQL request of one point. An issue GitHub can
// no longer find returns ErrIssueGone.
func (c *Client) FetchRepoLabels(ctx context.Context, tok Token, id string) (*RepoLabels, error) {
	labels, err := c.fetchRepoLabels(ctx, tok, id)

	return labels, Redact(err, tok)
}

// FetchAssignable reads the people the issue with node ID id can be assigned to whose
// login or name matches query, the first twenty, with one GraphQL request of one point.
// An issue GitHub can no longer find returns ErrIssueGone.
func (c *Client) FetchAssignable(ctx context.Context, tok Token, id, query string) ([]Assignable, error) {
	people, err := c.fetchAssignable(ctx, tok, id, query)

	return people, Redact(err, tok)
}

// AddLabels adds the labels named names to the issue ref, with one REST request (POST
// /repos/{owner}/{repo}/issues/{number}/labels), and returns its labels after the edit. A
// name missing from them is one GitHub didn't add, as it does for an account without
// triage access instead of refusing, and ErrEditIgnored is returned naming it.
func (c *Client) AddLabels(ctx context.Context, tok Token, ref item.Ref, names []string) ([]Label, error) {
	labels, err := c.addLabels(ctx, tok, ref, names)

	return labels, Redact(err, tok)
}

// RemoveLabel takes the label named name off the issue ref, with one REST request (DELETE
// /repos/{owner}/{repo}/issues/{number}/labels/{name}), and returns the labels left, or
// none for a label the issue no longer has, which GitHub answers as not found. A name
// still among them is ErrEditIgnored.
func (c *Client) RemoveLabel(ctx context.Context, tok Token, ref item.Ref, name string) ([]Label, error) {
	labels, err := c.removeLabel(ctx, tok, ref, name)

	return labels, Redact(err, tok)
}

// AddAssignees assigns logins to the issue ref, with one REST request (POST
// /repos/{owner}/{repo}/issues/{number}/assignees). GitHub answers with the issue; a login
// missing from its assignees is one GitHub didn't assign, as it does for someone who can't
// be instead of refusing, and ErrEditIgnored is returned naming them.
func (c *Client) AddAssignees(ctx context.Context, tok Token, ref item.Ref, logins []string) (*Edited, error) {
	edited, err := c.addAssignees(ctx, tok, ref, logins)

	return edited, Redact(err, tok)
}

// CanTriage reports a repository permission, as GitHub's GraphQL API names it, that lets
// an account change an issue's labels and assignees: triage or more.
func CanTriage(permission string) bool {
	return permission == "TRIAGE" || CanWrite(permission)
}

// fetchRepoLabels sends the labels read.
func (c *Client) fetchRepoLabels(ctx context.Context, tok Token, id string) (*RepoLabels, error) {
	var node struct {
		Repository struct {
			Labels struct {
				TotalCount int `json:"totalCount"`
				Nodes      []*struct {
					Name        string `json:"name"`
					Color       string `json:"color"`
					Description string `json:"description"`
				} `json:"nodes"`
			} `json:"labels"`
		} `json:"repository"`
	}

	if err := c.issueRepoRead(ctx, tok, "repository labels", repoLabelsQuery, map[string]any{"id": id}, &node); err != nil {
		return nil, err
	}

	out := &RepoLabels{Labels: []RepoLabel{}}

	for _, n := range node.Repository.Labels.Nodes {
		if n != nil && n.Name != "" {
			out.Labels = append(out.Labels, RepoLabel{Name: n.Name, Color: labelColor(n.Color), Description: n.Description})
		}
	}

	out.Total = max(node.Repository.Labels.TotalCount, len(out.Labels))

	return out, nil
}

// fetchAssignable sends the assignable people read.
func (c *Client) fetchAssignable(ctx context.Context, tok Token, id, query string) ([]Assignable, error) {
	var node struct {
		Repository struct {
			AssignableUsers struct {
				Nodes []*struct {
					Login string `json:"login"`
					Name  string `json:"name"`
				} `json:"nodes"`
			} `json:"assignableUsers"`
		} `json:"repository"`
	}

	vars := map[string]any{"id": id, "query": strings.TrimSpace(query)}
	if err := c.issueRepoRead(ctx, tok, "assignable people", assignableQuery, vars, &node); err != nil {
		return nil, err
	}

	out := []Assignable{}

	for _, n := range node.Repository.AssignableUsers.Nodes {
		if n != nil && n.Login != "" {
			out = append(out, Assignable{Login: n.Login, Name: n.Name})
		}
	}

	return out, nil
}

// issueRepoRead sends a read, named name in errors, of the repository of the issue whose
// node ID vars holds, and decodes the issue's node into into. An issue GitHub can no longer
// find is ErrIssueGone, and a node that is not an issue is malformed.
func (c *Client) issueRepoRead(ctx context.Context, tok Token, name, query string, vars map[string]any, into any) error {
	if id, _ := vars["id"].(string); id == "" {
		return fmt.Errorf("%w: no node ID", ErrInvalidItemRef)
	}

	resp, _, err := postGraphQL[itemData](ctx, c, tok, name, graphQLRequest{Query: query, Variables: vars}, unlessData)
	if err != nil {
		return err
	}

	if gone, goneErr := goneNode(resp); gone || goneErr != nil {
		if goneErr != nil {
			return goneErr
		}

		return ErrIssueGone
	}

	var kind struct {
		TypeName string `json:"__typename"`
	}

	if err = json.Unmarshal(resp.Data.Node, &kind); err != nil || kind.TypeName != typeIssue {
		return fmt.Errorf("%w: the ID is not an issue", ErrMalformedResponse)
	}

	if err = json.Unmarshal(resp.Data.Node, into); err != nil {
		return fmt.Errorf("%w: decode the %s: %w", ErrMalformedResponse, name, err)
	}

	return nil
}

// restLabel is a label as a REST answer names it.
type restLabel struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// addLabels sends the edit and checks GitHub's answer.
func (c *Client) addLabels(ctx context.Context, tok Token, ref item.Ref, names []string) ([]Label, error) {
	if err := validateLabels(ref, names); err != nil {
		return nil, err
	}

	body, err := json.Marshal(map[string][]string{"labels": names})
	if err != nil {
		return nil, fmt.Errorf("encode the edit: %w", err)
	}

	raw, err := c.edit(ctx, tok, http.MethodPost, issuePath(ref)+"/labels", body)
	if err != nil {
		return nil, editError(ref, fmt.Errorf("label %s: %w", ref, err))
	}

	labels, err := decodeLabels(ref, raw)
	if err != nil {
		return nil, err
	}

	if missing := missingNames(names, labelNames(labels)); len(missing) > 0 {
		return nil, uierr.New(
			fmt.Sprintf("GitHub didn't add %s to %s. Your account may not be allowed to change its labels.",
				quotedList(missing), ref),
			fmt.Errorf("label %s: %w", ref, ErrEditIgnored),
		)
	}

	return labels, nil
}

// removeLabel sends the edit and checks GitHub's answer.
func (c *Client) removeLabel(ctx context.Context, tok Token, ref item.Ref, name string) ([]Label, error) {
	if err := validateLabels(ref, []string{name}); err != nil {
		return nil, err
	}

	raw, err := c.edit(ctx, tok, http.MethodDelete, issuePath(ref)+"/labels/"+url.PathEscape(name), nil)
	if err != nil {
		if labelGone(err) {
			return []Label{}, nil
		}

		return nil, editError(ref, fmt.Errorf("unlabel %s: %w", ref, err))
	}

	labels, err := decodeLabels(ref, raw)
	if err != nil {
		return nil, err
	}

	for _, l := range labels {
		if strings.EqualFold(l.Name, name) {
			return nil, uierr.New(
				fmt.Sprintf("GitHub kept %q on %s. Your account may not be allowed to change its labels.", name, ref),
				fmt.Errorf("unlabel %s: %w", ref, ErrEditIgnored),
			)
		}
	}

	return labels, nil
}

// labelGone reports GitHub's answer to removing a label the issue doesn't have: not found,
// saying the label doesn't exist, which leaves the issue as asked.
func labelGone(err error) bool {
	status, ok := errors.AsType[*StatusError](err)

	return ok && status.StatusCode == http.StatusNotFound && strings.Contains(strings.ToLower(status.Message), "label")
}

// addAssignees sends the edit and checks GitHub's answer.
func (c *Client) addAssignees(ctx context.Context, tok Token, ref item.Ref, logins []string) (*Edited, error) {
	if len(logins) == 0 {
		return nil, fmt.Errorf("%w: no one to assign", ErrInvalidLogin)
	}

	for _, login := range logins {
		if err := validateEdit(ref, login); err != nil {
			return nil, err
		}
	}

	body, err := json.Marshal(map[string][]string{"assignees": logins})
	if err != nil {
		return nil, fmt.Errorf("encode the edit: %w", err)
	}

	raw, err := c.edit(ctx, tok, http.MethodPost, issuePath(ref)+"/assignees", body)
	if err != nil {
		return nil, editError(ref, fmt.Errorf("assign %s: %w", ref, err))
	}

	var answer struct {
		UpdatedAt time.Time   `json:"updated_at"`
		Assignees []restLogin `json:"assignees"`
	}

	if err = json.Unmarshal(raw, &answer); err != nil {
		return nil, fmt.Errorf("%w: decode %s: %w", ErrMalformedResponse, ref, err)
	}

	assigned := make([]string, 0, len(answer.Assignees))
	for _, a := range answer.Assignees {
		assigned = append(assigned, a.Login)
	}

	if missing := missingNames(logins, assigned); len(missing) > 0 {
		return nil, uierr.New(
			fmt.Sprintf("GitHub didn't assign %s to %s. They may not be someone it can be assigned to.",
				strings.Join(missing, ", "), ref),
			fmt.Errorf("assign %s: %w", ref, ErrEditIgnored),
		)
	}

	return &Edited{UpdatedAt: answer.UpdatedAt.UTC()}, nil
}

// decodeLabels reads an issue's labels from a REST answer.
func decodeLabels(ref item.Ref, raw []byte) ([]Label, error) {
	var answer []restLabel

	if err := json.Unmarshal(raw, &answer); err != nil {
		return nil, fmt.Errorf("%w: decode the labels of %s: %w", ErrMalformedResponse, ref, err)
	}

	out := make([]Label, 0, len(answer))
	for _, l := range answer {
		out = append(out, Label{Name: l.Name, Color: labelColor(l.Color)})
	}

	return out, nil
}

// labelNames are the labels' names.
func labelNames(labels []Label) []string {
	out := make([]string, 0, len(labels))
	for _, l := range labels {
		out = append(out, l.Name)
	}

	return out
}

// missingNames are the names asked for that have, ignoring case, no match among got.
func missingNames(asked, got []string) []string {
	var missing []string

	for _, name := range asked {
		found := false

		for _, g := range got {
			if strings.EqualFold(g, name) {
				found = true

				break
			}
		}

		if !found {
			missing = append(missing, name)
		}
	}

	return missing
}

// quotedList names labels in a sentence, each quoted.
func quotedList(names []string) string {
	quoted := make([]string, 0, len(names))
	for _, n := range names {
		quoted = append(quoted, fmt.Sprintf("%q", n))
	}

	return strings.Join(quoted, ", ")
}

// validateLabels checks an edit's issue and label names before anything is sent: an issue,
// and at least one name, each one a label could have.
func validateLabels(ref item.Ref, names []string) error {
	if err := validateRef(ref); err != nil {
		return err
	}

	if ref.Kind != item.KindIssue {
		return fmt.Errorf("%w: %s is not an issue", ErrInvalidItemRef, ref)
	}

	if len(names) == 0 {
		return fmt.Errorf("%w: no label", ErrInvalidLabel)
	}

	for _, name := range names {
		if strings.TrimSpace(name) == "" || utf8.RuneCountInString(name) > maxLabelRunes ||
			strings.ContainsFunc(name, unicode.IsControl) {
			return fmt.Errorf("%w: %q", ErrInvalidLabel, name)
		}
	}

	return nil
}
