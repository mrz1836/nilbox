package prompts

import (
	"fmt"
	"reflect"
	"strings"
	"unicode"

	"github.com/mrz1836/nilbox/internal/bump"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// Text limits for the values a prompt is filled with.
const (
	// maxTitleRunes caps how much of an item's title goes into a prompt.
	maxTitleRunes = 200

	// maxLabelsRunes caps the list of label names.
	maxLabelsRunes = 300

	// maxSensitiveRunes caps the list of sensitive files.
	maxSensitiveRunes = 1000
)

// AppliesTo is which kinds of item a prompt can launch.
type AppliesTo string

// What a prompt applies to.
const (
	AppliesToPR              = AppliesTo(item.KindPR)
	AppliesToIssue           = AppliesTo(item.KindIssue)
	AppliesToAny   AppliesTo = "any"
)

// Variable types, as the cheat sheet names them.
const (
	typeText   = "text"
	typeNumber = "number"
	typeYesNo  = "yes/no"
)

// Variable groups, in the order the cheat sheet shows them.
const (
	groupItem    = "Item"
	groupRepo    = "Repository"
	groupPR      = "Pull request"
	groupSession = "Session"
)

// Data is what a prompt's variables are filled from: {{.Title}} reads Title. It holds
// the item's address and short facts about it, never its body or comments: the session
// reads those itself with gh, so their text stays information rather than instructions.
// Every text field is sanitized before it is used (see Data.sanitized).
type Data struct {
	// Kind is "pull request" or "issue".
	Kind string

	// IsPR reports whether the item is a pull request.
	IsPR bool

	// Number is the pull request or issue number.
	Number int

	// Title is the item's title, cut to 200 characters.
	Title string

	// URL is the item's web address.
	URL string

	// Author is the login of the item's author.
	Author string

	// IsBot reports whether GitHub reports the author as a bot, such as Dependabot.
	IsBot bool

	// Labels lists the item's label names, separated by commas.
	Labels string

	// IsDraft reports whether a pull request is a draft.
	IsDraft bool

	// Repo is the repository as "owner/name".
	Repo string

	// Owner is the repository's owner, a user or an org.
	Owner string

	// RepoName is the repository's name without its owner.
	RepoName string

	// DefaultBranch is the repository's default branch.
	DefaultBranch string

	// Branch is a pull request's head branch.
	Branch string

	// Checks is "passing", "failing", or "pending", and "" when the head commit has none.
	Checks string

	// Review is "approved", "changes requested", or "review required", and "" when the
	// repository requires no review.
	Review string

	// HasConflicts reports whether a pull request conflicts with its base branch.
	HasConflicts bool

	// Additions and Deletions count a pull request's changed lines.
	Additions int
	Deletions int

	// ChangedFiles counts the files a pull request changes.
	ChangedFiles int

	// Commits counts a pull request's commits.
	Commits int

	// SensitiveFiles lists a pull request's sensitive files, each with its category, as
	// in ".github/workflows/ci.yml (Workflows)", separated by commas. It is empty unless
	// the pull request's files are checked and some are sensitive (see the sensitive
	// package).
	SensitiveFiles string

	// Dependency is the dependency a bot's update pull request bumps, as its title names
	// it, such as "golang.org/x/net"; "" for any other item, and for an update of a group
	// of dependencies.
	Dependency string

	// FromVersion and ToVersion are the versions the update moves the dependency between,
	// as its title writes them; "" when the title doesn't say.
	FromVersion string
	ToVersion   string

	// UpdateType is how big the update is: "patch", "minor", or "major", and "" when the
	// title can't tell.
	UpdateType string

	// InWorktree reports whether the session starts in its own worktree.
	InWorktree bool

	// WorktreeOnPRHead reports whether that worktree already starts at the pull
	// request's head, so it needs no checkout.
	WorktreeOnPRHead bool

	// Worktree is the session's worktree name, such as "pr-7", or "".
	Worktree string

	// Profile is the name of the profile the session runs with.
	Profile string

	// Me is the login of the signed-in GitHub account.
	Me string

	// OutsideContributor reports whether the item comes from outside the repository's
	// members and collaborators.
	OutsideContributor bool
}

// Variable describes one of Data's fields for the cheat sheet.
type Variable struct {
	// Name is the field, written {{.Name}} in a prompt.
	Name string `json:"name"`

	// Group is the cheat sheet section: Item, Repository, Pull request, or Session.
	Group string `json:"group"`

	// Type is "text", "number", or "yes/no".
	Type string `json:"type"`

	// Description says what the value is.
	Description string `json:"description"`

	// PROnly reports that the value is empty (or zero, or false) for issues.
	PROnly bool `json:"prOnly"`
}

// Variables returns every variable a prompt can use, in cheat sheet order.
func Variables() []Variable {
	return []Variable{
		{Name: "Kind", Group: groupItem, Type: typeText, Description: `"pull request" or "issue"`},
		{Name: "IsPR", Group: groupItem, Type: typeYesNo, Description: "The item is a pull request"},
		{Name: "Number", Group: groupItem, Type: typeNumber, Description: "The pull request or issue number"},
		{Name: "Title", Group: groupItem, Type: typeText, Description: "The title, cut to 200 characters"},
		{Name: "URL", Group: groupItem, Type: typeText, Description: "The web address on GitHub"},
		{Name: "Author", Group: groupItem, Type: typeText, Description: "Who opened it"},
		{Name: "IsBot", Group: groupItem, Type: typeYesNo, Description: "A bot opened it, such as Dependabot"},
		{Name: "Labels", Group: groupItem, Type: typeText, Description: "Label names, separated by commas"},
		{Name: "IsDraft", Group: groupItem, Type: typeYesNo, Description: "The pull request is a draft", PROnly: true},
		{Name: "Repo", Group: groupRepo, Type: typeText, Description: "The repository, as owner/name"},
		{Name: "Owner", Group: groupRepo, Type: typeText, Description: "The repository's owner"},
		{Name: "RepoName", Group: groupRepo, Type: typeText, Description: "The repository's name"},
		{Name: "DefaultBranch", Group: groupRepo, Type: typeText, Description: "The repository's default branch"},
		{Name: "Branch", Group: groupPR, Type: typeText, Description: "The head branch", PROnly: true},
		{Name: "Checks", Group: groupPR, Type: typeText, Description: `"passing", "failing", "pending", or empty`, PROnly: true},
		{
			Name: "Review", Group: groupPR, Type: typeText, PROnly: true,
			Description: `"approved", "changes requested", "review required", or empty`,
		},
		{Name: "HasConflicts", Group: groupPR, Type: typeYesNo, Description: "Conflicts with its base branch", PROnly: true},
		{Name: "Additions", Group: groupPR, Type: typeNumber, Description: "Lines added", PROnly: true},
		{Name: "Deletions", Group: groupPR, Type: typeNumber, Description: "Lines removed", PROnly: true},
		{Name: "ChangedFiles", Group: groupPR, Type: typeNumber, Description: "Files changed", PROnly: true},
		{Name: "Commits", Group: groupPR, Type: typeNumber, Description: "Commits", PROnly: true},
		{
			Name: "SensitiveFiles", Group: groupPR, Type: typeText, PROnly: true,
			Description: "Sensitive files it changes, such as workflows, with their categories, or empty",
		},
		{
			Name: "Dependency", Group: groupPR, Type: typeText, PROnly: true,
			Description: "The dependency a bot's update bumps, such as golang.org/x/net, or empty",
		},
		{Name: "FromVersion", Group: groupPR, Type: typeText, Description: "The version it moves from, or empty", PROnly: true},
		{Name: "ToVersion", Group: groupPR, Type: typeText, Description: "The version it moves to, or empty", PROnly: true},
		{
			Name: "UpdateType", Group: groupPR, Type: typeText, PROnly: true,
			Description: `"patch", "minor", "major", or empty when it can't be told`,
		},
		{Name: "InWorktree", Group: groupSession, Type: typeYesNo, Description: "The session starts in its own worktree"},
		{
			Name: "WorktreeOnPRHead", Group: groupSession, Type: typeYesNo, PROnly: true,
			Description: "That worktree already starts at the pull request's head",
		},
		{Name: "Worktree", Group: groupSession, Type: typeText, Description: `The worktree's name, such as "pr-7", or empty`},
		{Name: "Profile", Group: groupSession, Type: typeText, Description: "The profile the session runs with"},
		{Name: "Me", Group: groupSession, Type: typeText, Description: "Your GitHub login"},
		{
			Name: "OutsideContributor", Group: groupSession, Type: typeYesNo,
			Description: "It comes from outside the repository's members and collaborators",
		},
	}
}

// Sample returns made-up data for a pull request or an issue, used to check a prompt and
// to preview it when the inbox is empty. The session starts in the clone.
func Sample(kind item.Kind) Data {
	d := Data{
		Kind: item.KindIssue.Noun(), Number: 12, Title: "Crash on start",
		URL: "https://github.com/acme-org/example-repo/issues/12", Author: "octo-user", Labels: "bug",
		Repo: "acme-org/example-repo", Owner: "acme-org", RepoName: "example-repo", DefaultBranch: "main",
		Profile: "default", Me: "octo-user",
	}

	if kind != item.KindPR {
		return d
	}

	d.Kind, d.IsPR, d.Number, d.Title = item.KindPR.Noun(), true, 7, "Add the widget cache"
	d.URL, d.Labels, d.Branch = "https://github.com/acme-org/example-repo/pull/7", "enhancement", "widget-cache"
	d.Checks, d.Review = "passing", "review required"
	d.Additions, d.Deletions, d.ChangedFiles, d.Commits = 120, 30, 6, 3

	return d
}

// WithSession sets the session facts: whether it starts in a worktree and, for a pull
// request, whether that worktree starts at the pull request's head. The worktree name
// follows the launcher's (see item.WorktreeName).
func (d Data) WithSession(inWorktree, atPRHead bool) Data {
	d.InWorktree = inWorktree
	d.WorktreeOnPRHead = inWorktree && atPRHead && d.IsPR
	d.Worktree = ""

	if inWorktree {
		d.Worktree = item.WorktreeName(d.kind(), d.Number)
	}

	return d
}

// Values returns each variable's value as {{.Name}} prints it, for the cheat sheet. The
// values are sanitized the way a launch sanitizes them.
func (d Data) Values() map[string]string {
	v := reflect.ValueOf(d.sanitized())
	out := make(map[string]string, v.NumField())

	for i := range v.NumField() {
		out[v.Type().Field(i).Name] = fmt.Sprint(v.Field(i).Interface())
	}

	return out
}

// WithUpdate fills in what a bot's dependency update says about itself, from its title
// (see bump.Parse): the dependency, the versions, and how big the update is. It leaves d as
// it is for an issue, for a pull request a person opened, and for a title that is not a
// dependency update.
func (d Data) WithUpdate(title string) Data {
	if !d.IsPR || !d.IsBot {
		return d
	}

	u, ok := bump.Parse(title)
	if !ok {
		return d
	}

	d.Dependency, d.FromVersion, d.ToVersion, d.UpdateType = u.Name, u.From, u.To, string(u.Level)

	return d
}

// kind returns the kind of item d is for.
func (d Data) kind() item.Kind {
	if d.IsPR {
		return item.KindPR
	}

	return item.KindIssue
}

// sanitized returns d with every text field made a single line of plain text: control
// characters become spaces, bidirectional controls are removed, and runs of whitespace
// collapse. The title and labels are cut to their limits.
func (d Data) sanitized() Data {
	for _, field := range []*string{
		&d.Kind, &d.URL, &d.Author, &d.Repo, &d.Owner, &d.RepoName, &d.DefaultBranch,
		&d.Branch, &d.Checks, &d.Review, &d.Worktree, &d.Profile, &d.Me, &d.SensitiveFiles,
		&d.Dependency, &d.FromVersion, &d.ToVersion, &d.UpdateType,
	} {
		*field = sanitizeLine(*field)
	}

	d.Title = textutil.Clip(sanitizeLine(d.Title), maxTitleRunes)
	d.Labels = textutil.Clip(sanitizeLine(d.Labels), maxLabelsRunes)
	d.SensitiveFiles = textutil.Clip(d.SensitiveFiles, maxSensitiveRunes)

	for _, field := range []*string{&d.Dependency, &d.FromVersion, &d.ToVersion} {
		*field = textutil.Clip(*field, maxTitleRunes)
	}

	return d
}

// Covers reports whether a prompt that applies to a can launch an item of kind.
func (a AppliesTo) Covers(kind item.Kind) bool {
	return a == AppliesToAny || a == AppliesTo(kind)
}

// sanitizeLine turns s into a single line of plain text: control characters (including
// newlines and tabs) become spaces, bidirectional-text controls are removed, and runs of
// whitespace collapse to one space.
func sanitizeLine(s string) string {
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case unicode.Is(unicode.Bidi_Control, r):
			return -1
		case unicode.IsControl(r):
			return ' '
		default:
			return r
		}
	}, s)

	return strings.Join(strings.Fields(cleaned), " ")
}
