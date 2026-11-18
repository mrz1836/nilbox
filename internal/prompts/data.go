package prompts

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"

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

	// maxFailedChecksRunes caps the list of failed checks' names.
	maxFailedChecksRunes = 300

	// maxRefRunes caps a remote's name and a commit, both short in practice.
	maxRefRunes = 100

	// maxRoutineRunes caps a routine's name, as the routine list does, and its trigger's
	// words.
	maxRoutineRunes = 80

	// maxAttempt caps the try a routine's run is on, which its own limits keep to 10.
	maxAttempt = 100

	// sampleHeadCommit is the sample pull request's made-up head commit, and
	// sampleReviewedCommit the made-up commit the user's last review of it was on.
	sampleHeadCommit     = "4f2a9c1e8b7d6a5f4e3d2c1b0a9f8e7d6c5b4a39"
	sampleReviewedCommit = "9e8d7c6b5a4f3e2d1c0b9a8f7e6d5c4b3a2f1e0d"
)

// commitPattern is a commit's ID as a prompt may hold it: 7 to 64 lower-case hex digits,
// from an abbreviated SHA-1 to a full SHA-256.
var commitPattern = regexp.MustCompile(`^[0-9a-f]{7,64}$`)

// CommitID returns s lower-cased when it is a commit's ID, 7 to 64 hex digits, and ""
// otherwise, so a value someone else could choose never fills a variable that holds a
// commit.
func CommitID(s string) string {
	if s = strings.ToLower(s); commitPattern.MatchString(s) {
		return s
	}

	return ""
}

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
	groupItem      = "Item"
	groupRepo      = "Repository"
	groupPR        = "Pull request"
	groupSession   = "Session"
	groupAutopilot = "Autopilot"
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

	// Remote is the clone's git remote for the repository, such as "origin", or
	// "upstream" in a fork's clone; "" when the launch has no clone.
	Remote string

	// RemoteIsPlain reports that Remote is a plain name a command can use as it is (see
	// plainName). It is worked out from Remote whenever the prompt is filled in, so it
	// can't be set apart from it.
	RemoteIsPlain bool

	// Branch is a pull request's head branch.
	Branch string

	// BranchIsPlain reports that Branch, exactly as GitHub reported it, is a plain name a
	// command can use as it is (see plainName), as every Dependabot and Renovate branch
	// is. It is worked out from Branch whenever the prompt is filled in, so it can't be
	// set apart from it.
	BranchIsPlain bool

	// HeadCommit is a pull request's head commit, the one whose files the launch checked
	// or else the one the last refresh read; "" for an issue, or before a refresh read it.
	HeadCommit string

	// ReviewedCommit is the commit the user's last review of a pull request was on, when
	// that review requested changes or approved it; "" otherwise, and for an issue. It is
	// only ever a commit's ID (see CommitID), so a command may name it.
	ReviewedCommit string

	// Checks is "passing", "failing", or "pending", and "" when the head commit has none.
	Checks string

	// FailedChecks lists the names of the head commit's failed checks, separated by
	// commas, and is "" when they aren't known.
	FailedChecks string

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

	// Routine is the name of the Autopilot routine that started the session, cut to 80
	// characters; "" for a launch of the user's own.
	Routine string

	// Trigger says what made the routine run, as the builder names its trigger, such as
	// "Checks fail"; "" for a launch of the user's own.
	Trigger string

	// Attempt is which try of the routine's this session is on the item, 1 for its first;
	// 0 for a launch of the user's own.
	Attempt int

	// Unattended reports a session nobody is there to answer: a routine's run in the
	// background. A launch of the user's own, and a routine's in the user's terminal,
	// are attended.
	Unattended bool
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
		{
			Name: "Remote", Group: groupRepo, Type: typeText,
			Description: `The clone's remote for the repository, such as "origin", or empty without a clone`,
		},
		{
			Name: "RemoteIsPlain", Group: groupRepo, Type: typeYesNo,
			Description: "The remote's name is plain (ASCII letters, digits, and -_./:,+@%=, not starting with - " +
				"or =), so a command can name it",
		},
		{Name: "Branch", Group: groupPR, Type: typeText, Description: "The head branch", PROnly: true},
		{
			Name: "BranchIsPlain", Group: groupPR, Type: typeYesNo, PROnly: true,
			Description: "The head branch, as GitHub reported it, is plain (ASCII letters, digits, and -_./:,+@%=, " +
				"not starting with - or =), so a command can name it",
		},
		{
			Name: "HeadCommit", Group: groupPR, Type: typeText, PROnly: true,
			Description: "The head commit, or empty before a refresh read it",
		},
		{
			Name: "ReviewedCommit", Group: groupPR, Type: typeText, PROnly: true,
			Description: "The commit your last review was on, when your verdict requested changes or approved; " +
				"empty otherwise",
		},
		{Name: "Checks", Group: groupPR, Type: typeText, Description: `"passing", "failing", "pending", or empty`, PROnly: true},
		{
			Name: "FailedChecks", Group: groupPR, Type: typeText, PROnly: true,
			Description: "The head commit's failed checks, separated by commas, or empty when they aren't known",
		},
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
		{
			Name: "Routine", Group: groupAutopilot, Type: typeText,
			Description: "The Autopilot routine that started it, or empty for your own launch",
		},
		{
			Name: "Trigger", Group: groupAutopilot, Type: typeText,
			Description: `What made the routine run, such as "Checks fail", or empty for your own launch`,
		},
		{
			Name: "Attempt", Group: groupAutopilot, Type: typeNumber,
			Description: "Which of the routine's tries on the item this is, 1 for the first, or 0 for your own launch",
		},
		{
			Name: "Unattended", Group: groupAutopilot, Type: typeYesNo,
			Description: "Nobody is there to answer: a routine's run in the background",
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
	d.Remote, d.HeadCommit, d.ReviewedCommit = "origin", sampleHeadCommit, sampleReviewedCommit
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
// collapse. The title and labels are cut to their limits, and ReviewedCommit is kept only
// when it is a commit's ID (CommitID). BranchIsPlain and RemoteIsPlain are worked out from
// the values d was given, before any of that, and hold only when it changed nothing.
func (d Data) sanitized() Data {
	branch, remote := d.Branch, d.Remote

	for _, field := range []*string{
		&d.Kind, &d.URL, &d.Author, &d.Repo, &d.Owner, &d.RepoName, &d.DefaultBranch, &d.Remote,
		&d.Branch, &d.HeadCommit, &d.Checks, &d.Review, &d.Worktree, &d.Profile, &d.Me, &d.SensitiveFiles,
		&d.Dependency, &d.FromVersion, &d.ToVersion, &d.UpdateType,
	} {
		*field = sanitizeLine(*field)
	}

	d.Title = textutil.Clip(sanitizeLine(d.Title), maxTitleRunes)
	d.Labels = textutil.Clip(sanitizeLine(d.Labels), maxLabelsRunes)
	d.FailedChecks = textutil.Clip(sanitizeLine(d.FailedChecks), maxFailedChecksRunes)
	d.SensitiveFiles = textutil.Clip(d.SensitiveFiles, maxSensitiveRunes)

	for _, field := range []*string{&d.Dependency, &d.FromVersion, &d.ToVersion} {
		*field = textutil.Clip(*field, maxTitleRunes)
	}

	for _, field := range []*string{&d.Remote, &d.HeadCommit} {
		*field = textutil.Clip(*field, maxRefRunes)
	}

	d.ReviewedCommit = CommitID(sanitizeLine(d.ReviewedCommit))

	d.Routine = textutil.Clip(sanitizeLine(d.Routine), maxRoutineRunes)
	d.Trigger = textutil.Clip(sanitizeLine(d.Trigger), maxRoutineRunes)
	d.Attempt = min(max(d.Attempt, 0), maxAttempt)

	d.BranchIsPlain = plainName(branch) && d.Branch == branch
	d.RemoteIsPlain = plainName(remote) && d.Remote == remote

	return d
}

// plainName reports a name a command can use as it is: one word every POSIX shell reads as
// itself (textutil.ShellSafe: ASCII letters, digits, and =-_./:,+@%, not starting with =),
// which git can't read as an option either, since it doesn't start with "-". Such a name
// holds nothing sanitizing removes, so what a prompt shows of it is what GitHub reported:
// no bidirectional control or trailing space hides a branch such as main behind another.
func plainName(s string) bool {
	return textutil.ShellSafe(s) && !strings.HasPrefix(s, "-")
}

// Covers reports whether a prompt that applies to a can launch an item of kind.
func (a AppliesTo) Covers(kind item.Kind) bool {
	return a == AppliesToAny || a == AppliesTo(kind)
}

// sanitizeLine turns s into a single line of plain text: control characters (including
// newlines and tabs) become spaces, bidirectional-text controls are removed, and runs of
// whitespace collapse to one space (textutil.OneLine, which Autopilot's facts share).
func sanitizeLine(s string) string {
	return textutil.OneLine(s)
}
