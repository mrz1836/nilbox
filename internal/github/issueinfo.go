package github

import (
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/textutil"
)

// issueInfoFields reads how an issue is planned, beyond the projects it is in: its type,
// its milestone, the issue it is a sub-issue of, how many open issues block it and it
// blocks, and its values in its organization's issue fields (Priority, Effort, and so
// on). All but the field values are single objects, which GitHub's point formula never
// counts; the field values are one connection per issue, which leaves a batch's price
// where it was: a batch of 25 issues cost one point with them and without, and two with
// the projects either way (measured in November 2026). An issue whose organization has no
// issue fields, as every issue in a personal repository, answers none.
//
// Each kind of field value aliases its value apart (number, text, date), so values of
// different types never share a name, which GraphQL would refuse to merge.
const issueInfoFields = `
    issueType { name color }
    milestone { title url dueOn state progressPercentage }
    parent { __typename number title url repository { nameWithOwner } }
    issueDependenciesSummary { blockedBy blocking }
    issueFieldValues(first: 20) {
      nodes {
        __typename
        ... on IssueFieldSingleSelectValue { name color field { ... on IssueFieldSingleSelect { name } } }
        ... on IssueFieldMultiSelectValue { options { name } field { ... on IssueFieldMultiSelect { name } } }
        ... on IssueFieldNumberValue { number: value field { ... on IssueFieldNumber { name } } }
        ... on IssueFieldTextValue { text: value field { ... on IssueFieldText { name } } }
        ... on IssueFieldDateValue { date: value field { ... on IssueFieldDate { name } } }
      }
    }`

// IssueInfo is how an issue is planned, beyond the projects it is in.
type IssueInfo struct {
	// Type is the issue's type, such as Task, zero when it has none. Its Description is
	// not read.
	Type IssueType

	// Fields are the issue's values in its organization's issue fields, such as Priority,
	// in GitHub's order; nil when it has none. Fields without a value are left out.
	Fields []IssueField

	// Milestone is the milestone the issue is in, nil when it is in none.
	Milestone *Milestone

	// Parent is the issue it is a sub-issue of, nil when it is none's, or the viewer
	// cannot see it. Its State is not read.
	Parent *ItemRef

	// BlockedBy counts the open issues that block it, and Blocking the open issues it
	// blocks.
	BlockedBy int
	Blocking  int
}

// IssueField is an issue's value in one of its organization's issue fields.
type IssueField struct {
	// Name is the field's name, such as "Priority".
	Name string

	// Value is the value as GitHub shows it: an option's name, the options' names joined
	// by commas, a number, text, or a date such as "Jun 27, 2026".
	Value string

	// Color is a single-select option's color as "#rrggbb", and "" for any other value.
	Color string
}

// graphQLIssueType is an issue's type.
type graphQLIssueType struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// graphQLDependencies counts the open issues an issue is blocked by and blocking.
type graphQLDependencies struct {
	BlockedBy int `json:"blockedBy"`
	Blocking  int `json:"blocking"`
}

// graphQLIssueFields is an issue's values in its organization's issue fields.
type graphQLIssueFields struct {
	Nodes []*graphQLIssueFieldValue `json:"nodes"`
}

// graphQLIssueFieldValue is an issue's value in one issue field. Which value field is set
// depends on the type; the others decode to zero values.
type graphQLIssueFieldValue struct {
	TypeName string   `json:"__typename"`
	Name     string   `json:"name"`
	Color    string   `json:"color"`
	Number   *float64 `json:"number"`
	Text     string   `json:"text"`
	Date     string   `json:"date"`
	Options  []*struct {
		Name string `json:"name"`
	} `json:"options"`
	Field *struct {
		Name string `json:"name"`
	} `json:"field"`
}

// issueInfo converts what the node read of how an issue is planned.
func (n *detailsNode) issueInfo() IssueInfo {
	info := IssueInfo{
		Fields:    n.IssueFieldValues.fields(),
		Milestone: n.Milestone.milestone(),
		Parent:    n.Parent.itemRef(),
		BlockedBy: max(n.IssueDependenciesSummary.BlockedBy, 0),
		Blocking:  max(n.IssueDependenciesSummary.Blocking, 0),
	}

	if t := n.IssueType; t != nil && t.Name != "" {
		info.Type = IssueType{Name: t.Name, Color: projectColor(t.Color)}
	}

	return info
}

// fields converts the field values, skipping nulls, values without a field, and empty
// values. It returns nil when none are left.
func (f graphQLIssueFields) fields() []IssueField {
	var out []IssueField

	for _, v := range f.Nodes {
		if v == nil || v.Field == nil || v.Field.Name == "" {
			continue
		}

		if value, color := v.value(); value != "" {
			out = append(out, IssueField{Name: v.Field.Name, Value: value, Color: color})
		}
	}

	return out
}

// value is the value as GitHub shows it, and a single-select option's color; both "" for
// a type this build does not read.
func (v *graphQLIssueFieldValue) value() (value, color string) {
	switch v.TypeName {
	case "IssueFieldSingleSelectValue":
		return v.Name, projectColor(v.Color)
	case "IssueFieldMultiSelectValue":
		names := make([]string, 0, len(v.Options))
		for _, o := range v.Options {
			if o != nil && o.Name != "" {
				names = append(names, o.Name)
			}
		}

		return strings.Join(names, ", "), ""
	case "IssueFieldNumberValue":
		if v.Number == nil {
			return "", ""
		}

		return strconv.FormatFloat(*v.Number, 'f', -1, 64), ""
	case "IssueFieldTextValue":
		return textutil.Truncate(strings.Join(strings.Fields(v.Text), " "), maxFieldRunes), ""
	case "IssueFieldDateValue":
		return fieldDate(v.Date), ""
	default:
		return "", ""
	}
}
