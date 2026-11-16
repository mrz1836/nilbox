package catalog

import (
	"strings"

	"github.com/mrz1836/nilbox/internal/bump"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/trust"
)

// FieldID names a field a condition reads, as "<group>.<name>".
type FieldID string

// The item's fields.
const (
	FieldKind        FieldID = "item.kind"
	FieldTitle       FieldID = "item.title"
	FieldLabels      FieldID = "item.labels"
	FieldAuthor      FieldID = "item.author"
	FieldAuthorIsBot FieldID = "item.authorIsBot"
	FieldTrust       FieldID = "item.trust"
	FieldAssociation FieldID = "item.association"
	FieldAge         FieldID = "item.age"
	FieldIdle        FieldID = "item.idle"
	FieldComments    FieldID = "item.comments"
	FieldLists       FieldID = "item.lists"
	FieldSnoozed     FieldID = "item.snoozed"
	FieldDismissed   FieldID = "item.dismissed"
	FieldPinned      FieldID = "item.pinned"
	FieldMine        FieldID = "item.mine"
)

// The repository's fields.
const (
	FieldRepoOwner    FieldID = "repo.owner"
	FieldRepoName     FieldID = "repo.name"
	FieldRepoFullName FieldID = "repo.fullName"
	FieldVisibility   FieldID = "repo.visibility"
	FieldLanguage     FieldID = "repo.language"
	FieldFork         FieldID = "repo.fork"
	FieldPermission   FieldID = "repo.permission"
	FieldHasClone     FieldID = "repo.hasClone"
	FieldRepoMuted    FieldID = "repo.muted"
	FieldRepoPinned   FieldID = "repo.pinned"
	FieldUpdateBranch FieldID = "repo.allowsUpdateBranch"
)

// The pull request's fields.
const (
	FieldDraft            FieldID = "pr.draft"
	FieldBase             FieldID = "pr.base"
	FieldHead             FieldID = "pr.head"
	FieldFromFork         FieldID = "pr.fromFork"
	FieldSize             FieldID = "pr.size"
	FieldLines            FieldID = "pr.lines"
	FieldFiles            FieldID = "pr.files"
	FieldCommits          FieldID = "pr.commits"
	FieldConflicts        FieldID = "pr.conflicts"
	FieldBehind           FieldID = "pr.behind"
	FieldReviewDecision   FieldID = "pr.reviewDecision"
	FieldApprovals        FieldID = "pr.approvals"
	FieldChangesRequested FieldID = "pr.changesRequested"
	FieldAsksMe           FieldID = "pr.asksMe"
	FieldAutoMerge        FieldID = "pr.autoMerge"
)

// The checks' fields.
const (
	FieldChecksState      FieldID = "checks.state"
	FieldChecksFailed     FieldID = "checks.failed"
	FieldChecksPending    FieldID = "checks.pending"
	FieldChecksPassed     FieldID = "checks.passed"
	FieldFailedNames      FieldID = "checks.failedNames"
	FieldCheckNames       FieldID = "checks.names"
	FieldRequiredFailed   FieldID = "checks.requiredFailed"
	FieldAwaitingApproval FieldID = "checks.awaitingApproval"
)

// The changed files' fields.
const (
	FieldPaths     FieldID = "files.paths"
	FieldSensitive FieldID = "files.sensitive"
)

// The dependency update's fields, read from the title and the head branch.
const (
	FieldDepTool    FieldID = "dep.tool"
	FieldEcosystem  FieldID = "dep.ecosystem"
	FieldPackage    FieldID = "dep.package"
	FieldFrom       FieldID = "dep.from"
	FieldTo         FieldID = "dep.to"
	FieldUpdateType FieldID = "dep.updateType"
	FieldGrouped    FieldID = "dep.grouped"
	FieldSecurity   FieldID = "dep.security"
)

// The routine's history with the item, from its ledger.
const (
	FieldTries       FieldID = "history.tries"
	FieldLastOutcome FieldID = "history.lastOutcome"
)

// Group is a heading the builder lists fields under.
type Group string

// The groups of fields, in the builder's order.
const (
	GroupItem       Group = "item"
	GroupRepo       Group = "repo"
	GroupPR         Group = "pr"
	GroupChecks     Group = "checks"
	GroupFiles      Group = "files"
	GroupDependency Group = "dep"
	GroupHistory    Group = "history"
)

// GroupInfo names a group of fields.
type GroupInfo struct {
	ID    Group  `json:"id"`
	Label string `json:"label"`
}

// Groups returns the groups of fields, in the builder's order.
func Groups() []GroupInfo {
	return []GroupInfo{
		{ID: GroupItem, Label: "Item"},
		{ID: GroupRepo, Label: "Repository"},
		{ID: GroupPR, Label: "Pull request"},
		{ID: GroupChecks, Label: "Checks"},
		{ID: GroupFiles, Label: "Files"},
		{ID: GroupDependency, Label: "Dependency update"},
		{ID: GroupHistory, Label: "History"},
	}
}

// Cost says how much reading a field's value takes, so conditions run cheapest first.
type Cost int

// The costs, from the cheapest.
const (
	// CostItem is read with the item, its repository, and the routine's ledger.
	CostItem Cost = iota

	// CostDetails needs the item's details.
	CostDetails

	// CostChecks needs the head commit's check names.
	CostChecks

	// CostFiles needs the pull request's changed files.
	CostFiles
)

// Patterns says how a list field's patterns match.
type Patterns string

// The pattern styles.
const (
	// PatternsGlob matches with * for any run of characters and ? for one.
	PatternsGlob Patterns = "glob"

	// PatternsGitignore matches paths as .gitignore does, as the sensitive-file check
	// does.
	PatternsGitignore Patterns = "gitignore"
)

// Choice is one value a choice field, or a list field of choices, can hold.
type Choice struct {
	// Value is what the spec stores, compared ignoring case.
	Value string `json:"value"`

	// Label is what the builder and the sentence show.
	Label string `json:"label"`
}

// Words are how the routine's sentence and its reasons say a field.
type Words struct {
	// Noun names the field: "the title", "the labels", "the lines changed".
	Noun string `json:"noun"`

	// Plural reports a noun that takes "are".
	Plural bool `json:"plural"`

	// One names one element of a list: "a label", "a failed check's name".
	One string `json:"one"`

	// Yes and No say a yes-or-no field's two values: "it's a draft", "it isn't a draft".
	// For a list, they say it is empty and it isn't: "no check failed", "a check failed".
	Yes string `json:"yes"`
	No  string `json:"no"`
}

// Field is one value a condition can read about an item.
type Field struct {
	ID    FieldID `json:"id"`
	Group Group   `json:"group"`
	Type  Type    `json:"type"`

	// Label is what the builder's field menu shows.
	Label string `json:"label"`

	// Words are how the sentence says it.
	Words Words `json:"words"`

	// Kinds are the kinds of item the field applies to. On an item of another kind a
	// condition on it is no, whatever its operator, since the routine can't mean it.
	Kinds []item.Kind `json:"kinds"`

	// Choices are a choice field's values, or a list field's when its list holds them.
	Choices []Choice `json:"choices"`

	// Patterns is how a list field's patterns match.
	Patterns Patterns `json:"patterns,omitempty"`

	// Cost is how much reading it takes.
	Cost Cost `json:"cost"`
}

// Applies reports whether the field applies to an item of kind.
func (f Field) Applies(kind item.Kind) bool {
	for _, k := range f.Kinds {
		if k == kind {
			return true
		}
	}

	return false
}

// Choice returns the field's choice whose value is v, ignoring case.
func (f Field) Choice(v string) (Choice, bool) {
	for _, c := range f.Choices {
		if strings.EqualFold(c.Value, v) {
			return c, true
		}
	}

	return Choice{}, false
}

// Fields returns every field, grouped in Groups' order.
func Fields() []Field {
	out := itemFields()
	out = append(out, repoFields()...)
	out = append(out, prFields()...)
	out = append(out, checkFields()...)
	out = append(out, fileFields()...)
	out = append(out, depFields()...)

	return append(out, historyFields()...)
}

// both and prOnly are the kinds a field applies to.
func both() []item.Kind   { return item.Kinds() }
func prOnly() []item.Kind { return []item.Kind{item.KindPR} }

// boolWords says a yes-or-no field's two values.
func boolWords(yes, no string) Words { return Words{Yes: yes, No: no} }

// noun names a field that is one value, and plural one that takes "are".
func noun(n string) Words   { return Words{Noun: n} }
func plural(n string) Words { return Words{Noun: n, Plural: true} }

// list names a list field: the list, one element, and the words for empty and not.
func list(n, one, empty, some string) Words {
	return Words{Noun: n, Plural: true, One: one, Yes: empty, No: some}
}

func itemFields() []Field {
	return []Field{
		{
			ID: FieldKind, Group: GroupItem, Type: TypeChoice, Label: "Kind", Words: noun("it"), Kinds: both(),
			Choices: []Choice{{Value: string(item.KindPR), Label: "a pull request"}, {Value: string(item.KindIssue), Label: "an issue"}},
		},
		{ID: FieldTitle, Group: GroupItem, Type: TypeText, Label: "Title", Words: noun("the title"), Kinds: both()},
		{
			ID: FieldLabels, Group: GroupItem, Type: TypeList, Label: "Labels", Kinds: both(), Cost: CostDetails,
			Words: list("the labels", "a label", "it has no labels", "it has a label"), Patterns: PatternsGlob,
		},
		{ID: FieldAuthor, Group: GroupItem, Type: TypeText, Label: "Author", Words: noun("the author's login"), Kinds: both()},
		{
			ID: FieldAuthorIsBot, Group: GroupItem, Type: TypeBool, Label: "Author is a bot", Kinds: both(),
			Words: boolWords("the author is a bot", "the author isn't a bot"),
		},
		{
			ID: FieldTrust, Group: GroupItem, Type: TypeChoice, Label: "Trust", Words: noun("the author"), Kinds: both(),
			Choices: []Choice{
				{Value: string(trust.ClassMember), Label: "a member"},
				{Value: string(trust.ClassBot), Label: "a trusted bot"},
				{Value: string(trust.ClassOutside), Label: "an outside contributor"},
			},
		},
		{
			ID: FieldAssociation, Group: GroupItem, Type: TypeChoice, Label: "Author's association", Kinds: both(),
			Words: noun("the author's association"), Choices: associationChoices(),
		},
		{ID: FieldAge, Group: GroupItem, Type: TypeDuration, Label: "Age", Words: noun("it's been open"), Kinds: both()},
		{
			ID: FieldIdle, Group: GroupItem, Type: TypeDuration, Label: "Time since its last activity",
			Words: noun("it's been quiet"), Kinds: both(),
		},
		{
			ID: FieldComments, Group: GroupItem, Type: TypeNumber, Label: "Comments", Words: plural("the comments"),
			Kinds: both(), Cost: CostDetails,
		},
		{
			ID: FieldLists, Group: GroupItem, Type: TypeList, Label: "Inbox lists", Kinds: both(),
			Words:   list("its inbox lists", "an inbox list", "it's in no inbox list", "it's in an inbox list"),
			Choices: listChoices(),
		},
		{
			ID: FieldSnoozed, Group: GroupItem, Type: TypeBool, Label: "Snoozed", Kinds: both(),
			Words: boolWords("it's snoozed", "it isn't snoozed"),
		},
		{
			ID: FieldDismissed, Group: GroupItem, Type: TypeBool, Label: "Dismissed", Kinds: both(),
			Words: boolWords("it's dismissed", "it isn't dismissed"),
		},
		{
			ID: FieldPinned, Group: GroupItem, Type: TypeBool, Label: "Pinned", Kinds: both(),
			Words: boolWords("it's pinned", "it isn't pinned"),
		},
		{
			ID: FieldMine, Group: GroupItem, Type: TypeBool, Label: "Opened by me", Kinds: both(),
			Words: boolWords("you opened it", "you didn't open it"),
		},
	}
}

func repoFields() []Field {
	return []Field{
		{
			ID: FieldRepoOwner, Group: GroupRepo, Type: TypeText, Label: "Owner", Words: noun("the repository's owner"),
			Kinds: both(),
		},
		{
			ID: FieldRepoName, Group: GroupRepo, Type: TypeText, Label: "Name", Words: noun("the repository's name"),
			Kinds: both(),
		},
		{
			ID: FieldRepoFullName, Group: GroupRepo, Type: TypeText, Label: "Owner and name",
			Words: noun("the repository"), Kinds: both(),
		},
		{
			ID: FieldVisibility, Group: GroupRepo, Type: TypeChoice, Label: "Visibility", Words: noun("the repository"),
			Kinds: both(), Choices: []Choice{
				{Value: "public", Label: "public"}, {Value: "private", Label: "private"}, {Value: "internal", Label: "internal"},
			},
		},
		{
			ID: FieldLanguage, Group: GroupRepo, Type: TypeText, Label: "Language", Words: noun("the repository's language"),
			Kinds: both(),
		},
		{
			ID: FieldFork, Group: GroupRepo, Type: TypeBool, Label: "Is a fork", Kinds: both(),
			Words: boolWords("the repository is a fork", "the repository isn't a fork"),
		},
		{
			ID: FieldPermission, Group: GroupRepo, Type: TypeChoice, Label: "Your permission", Words: noun("your permission"),
			Kinds: both(), Choices: []Choice{
				{Value: "admin", Label: "admin"},
				{Value: "maintain", Label: "maintain"},
				{Value: "write", Label: "write"},
				{Value: "triage", Label: "triage"},
				{Value: "read", Label: "read"},
			},
		},
		{
			ID: FieldHasClone, Group: GroupRepo, Type: TypeBool, Label: "Has a local clone", Kinds: both(),
			Words: boolWords("you have a clone of it", "you have no clone of it"),
		},
		{
			ID: FieldRepoMuted, Group: GroupRepo, Type: TypeBool, Label: "Is muted", Kinds: both(),
			Words: boolWords("the repository is muted", "the repository isn't muted"),
		},
		{
			ID: FieldRepoPinned, Group: GroupRepo, Type: TypeBool, Label: "Is pinned", Kinds: both(),
			Words: boolWords("the repository is pinned", "the repository isn't pinned"),
		},
		{
			ID: FieldUpdateBranch, Group: GroupRepo, Type: TypeBool, Label: "Allows updating branches", Kinds: prOnly(),
			Words: boolWords("the repository allows updating branches", "the repository doesn't allow updating branches"),
		},
	}
}

func prFields() []Field {
	return []Field{
		{
			ID: FieldDraft, Group: GroupPR, Type: TypeBool, Label: "Draft", Kinds: prOnly(),
			Words: boolWords("it's a draft", "it isn't a draft"),
		},
		{
			ID: FieldBase, Group: GroupPR, Type: TypeText, Label: "Base branch", Words: noun("the base branch"),
			Kinds: prOnly(), Cost: CostDetails,
		},
		{ID: FieldHead, Group: GroupPR, Type: TypeText, Label: "Head branch", Words: noun("the head branch"), Kinds: prOnly()},
		{
			ID: FieldFromFork, Group: GroupPR, Type: TypeBool, Label: "From a fork", Kinds: prOnly(),
			Words: boolWords("it's from a fork", "it isn't from a fork"),
		},
		{
			ID: FieldSize, Group: GroupPR, Type: TypeChoice, Label: "Size", Words: noun("its size"), Kinds: prOnly(),
			Cost: CostDetails, Choices: sizeChoices(),
		},
		{
			ID: FieldLines, Group: GroupPR, Type: TypeNumber, Label: "Lines changed", Words: plural("the lines changed"),
			Kinds: prOnly(), Cost: CostDetails,
		},
		{
			ID: FieldFiles, Group: GroupPR, Type: TypeNumber, Label: "Files changed", Words: plural("the files changed"),
			Kinds: prOnly(), Cost: CostDetails,
		},
		{
			ID: FieldCommits, Group: GroupPR, Type: TypeNumber, Label: "Commits", Words: plural("the commits"),
			Kinds: prOnly(), Cost: CostDetails,
		},
		{
			ID: FieldConflicts, Group: GroupPR, Type: TypeBool, Label: "Has conflicts", Kinds: prOnly(), Cost: CostDetails,
			Words: boolWords("it has merge conflicts", "it has no merge conflicts"),
		},
		{
			ID: FieldBehind, Group: GroupPR, Type: TypeBool, Label: "Behind its base", Kinds: prOnly(), Cost: CostDetails,
			Words: boolWords("it's behind its base branch", "it isn't behind its base branch"),
		},
		{
			ID: FieldReviewDecision, Group: GroupPR, Type: TypeChoice, Label: "Review decision",
			Words: noun("the review decision"), Kinds: prOnly(), Cost: CostDetails, Choices: []Choice{
				{Value: "approved", Label: "approved"},
				{Value: "changesRequested", Label: "changes requested"},
				{Value: "reviewRequired", Label: "review required"},
				{Value: "none", Label: "none"},
			},
		},
		{
			ID: FieldApprovals, Group: GroupPR, Type: TypeNumber, Label: "Approvals", Words: plural("the approvals"),
			Kinds: prOnly(), Cost: CostDetails,
		},
		{
			ID: FieldChangesRequested, Group: GroupPR, Type: TypeNumber, Label: "Changes requested",
			Words: plural("the requests for changes"), Kinds: prOnly(), Cost: CostDetails,
		},
		{
			ID: FieldAsksMe, Group: GroupPR, Type: TypeBool, Label: "Asks for my review", Kinds: prOnly(), Cost: CostDetails,
			Words: boolWords("it asks for your review", "it doesn't ask for your review"),
		},
		{
			ID: FieldAutoMerge, Group: GroupPR, Type: TypeBool, Label: "Auto-merge on", Kinds: prOnly(), Cost: CostDetails,
			Words: boolWords("auto-merge is on", "auto-merge is off"),
		},
	}
}

func checkFields() []Field {
	return []Field{
		{
			ID: FieldChecksState, Group: GroupChecks, Type: TypeChoice, Label: "State", Words: noun("the checks' state"),
			Kinds: prOnly(), Cost: CostDetails, Choices: []Choice{
				{Value: "success", Label: "passing"},
				{Value: "failure", Label: "failing"},
				{Value: "error", Label: "erroring"},
				{Value: "pending", Label: "pending"},
				{Value: "expected", Label: "expected"},
				{Value: "none", Label: "no checks"},
			},
		},
		{
			ID: FieldChecksFailed, Group: GroupChecks, Type: TypeNumber, Label: "Failed checks",
			Words: plural("the failed checks"), Kinds: prOnly(), Cost: CostDetails,
		},
		{
			ID: FieldChecksPending, Group: GroupChecks, Type: TypeNumber, Label: "Pending checks",
			Words: plural("the pending checks"), Kinds: prOnly(), Cost: CostDetails,
		},
		{
			ID: FieldChecksPassed, Group: GroupChecks, Type: TypeNumber, Label: "Passed checks",
			Words: plural("the passed checks"), Kinds: prOnly(), Cost: CostDetails,
		},
		{
			ID: FieldFailedNames, Group: GroupChecks, Type: TypeList, Label: "A failed check's name", Kinds: prOnly(),
			Words:    list("the failed checks' names", "a failed check's name", "no check failed", "a check failed"),
			Patterns: PatternsGlob, Cost: CostChecks,
		},
		{
			ID: FieldCheckNames, Group: GroupChecks, Type: TypeList, Label: "A check's name", Kinds: prOnly(),
			Words:    list("the checks' names", "a check's name", "it has no checks", "it has checks"),
			Patterns: PatternsGlob, Cost: CostChecks,
		},
		{
			ID: FieldRequiredFailed, Group: GroupChecks, Type: TypeBool, Label: "A required check failed", Kinds: prOnly(),
			Words: boolWords("a required check failed", "no required check failed"), Cost: CostChecks,
		},
		{
			ID: FieldAwaitingApproval, Group: GroupChecks, Type: TypeBool, Label: "Runs awaiting approval",
			Kinds: prOnly(), Cost: CostDetails,
			Words: boolWords("workflow runs wait for a maintainer", "no workflow run waits for a maintainer"),
		},
	}
}

func fileFields() []Field {
	return []Field{
		{
			ID: FieldPaths, Group: GroupFiles, Type: TypeList, Label: "Changed paths", Kinds: prOnly(),
			Words:    list("the changed paths", "a changed path", "no file changed", "a file changed"),
			Patterns: PatternsGitignore, Cost: CostFiles,
		},
		{
			ID: FieldSensitive, Group: GroupFiles, Type: TypeList, Label: "Sensitive categories touched", Kinds: prOnly(),
			Words: list("the sensitive categories touched", "a sensitive category",
				"no sensitive file changed", "a sensitive file changed"),
			Choices: sensitiveChoices(), Cost: CostFiles,
		},
	}
}

func depFields() []Field {
	return []Field{
		{
			ID: FieldDepTool, Group: GroupDependency, Type: TypeChoice, Label: "Tool", Words: noun("the update's tool"),
			Kinds: prOnly(), Choices: []Choice{
				{Value: string(bump.ToolDependabot), Label: "Dependabot"},
				{Value: string(bump.ToolRenovate), Label: "Renovate"},
				{Value: ToolNone, Label: "neither"},
			},
		},
		{
			ID: FieldEcosystem, Group: GroupDependency, Type: TypeChoice, Label: "Ecosystem", Words: noun("the ecosystem"),
			Kinds: prOnly(), Choices: ecosystemChoices(),
		},
		{ID: FieldPackage, Group: GroupDependency, Type: TypeText, Label: "Package", Words: noun("the package"), Kinds: prOnly()},
		{
			ID: FieldFrom, Group: GroupDependency, Type: TypeText, Label: "From version",
			Words: noun("the version it updates from"), Kinds: prOnly(),
		},
		{
			ID: FieldTo, Group: GroupDependency, Type: TypeText, Label: "To version",
			Words: noun("the version it updates to"), Kinds: prOnly(),
		},
		{
			ID: FieldUpdateType, Group: GroupDependency, Type: TypeChoice, Label: "Update type",
			Words: noun("the update type"), Kinds: prOnly(), Choices: []Choice{
				{Value: string(bump.LevelMajor), Label: "major"},
				{Value: string(bump.LevelMinor), Label: "minor"},
				{Value: string(bump.LevelPatch), Label: "patch"},
			},
		},
		{
			ID: FieldGrouped, Group: GroupDependency, Type: TypeBool, Label: "Grouped update", Kinds: prOnly(),
			Words: boolWords("it's a grouped update", "it isn't a grouped update"),
		},
		{
			ID: FieldSecurity, Group: GroupDependency, Type: TypeBool, Label: "Security update", Kinds: prOnly(),
			Words: boolWords("it's a security update", "it isn't a security update"),
		},
	}
}

func historyFields() []Field {
	return []Field{
		{
			ID: FieldTries, Group: GroupHistory, Type: TypeNumber, Label: "This routine's tries",
			Words: plural("this routine's tries"), Kinds: both(),
		},
		{
			ID: FieldLastOutcome, Group: GroupHistory, Type: TypeChoice, Label: "Its last outcome",
			Words: noun("this routine's last outcome"), Kinds: both(), Choices: outcomeChoices(),
		},
	}
}

// ToolNone is dep.tool's value for an item neither bot opened.
const ToolNone = "none"

// ReviewNone, ChecksNone, and OutcomeNone are the choices for a pull request with no
// review decision, a head commit with no checks, and an item the routine never acted on.
const (
	ReviewNone  = "none"
	ChecksNone  = "none"
	OutcomeNone = "none"
)

// The last outcomes the ledger records for a routine on an item.
const (
	OutcomeDone       = "done"
	OutcomeFailed     = "failed"
	OutcomeGaveUp     = "gaveUp"
	OutcomeDismissed  = "dismissed"
	OutcomePassed     = "passed"
	OutcomeStillFails = "stillFails"
)

func outcomeChoices() []Choice {
	return []Choice{
		{Value: OutcomeNone, Label: "none yet"},
		{Value: OutcomeDone, Label: "done"},
		{Value: OutcomeFailed, Label: "failed"},
		{Value: OutcomeGaveUp, Label: "gave up"},
		{Value: OutcomeDismissed, Label: "skipped by you"},
		{Value: OutcomePassed, Label: "the checks passed"},
		{Value: OutcomeStillFails, Label: "the checks still fail"},
	}
}

// The inbox lists, as item.lists holds them.
const (
	ListReviewRequested = "reviewRequested"
	ListAuthored        = "authored"
	ListAssigned        = "assigned"
)

func listChoices() []Choice {
	return []Choice{
		{Value: ListReviewRequested, Label: "Review requested"},
		{Value: ListAuthored, Label: "Authored"},
		{Value: ListAssigned, Label: "Assigned"},
	}
}

// associationChoices are GitHub's author associations, as GitHub names them.
func associationChoices() []Choice {
	return []Choice{
		{Value: "OWNER", Label: "owner"},
		{Value: "MEMBER", Label: "member"},
		{Value: "COLLABORATOR", Label: "collaborator"},
		{Value: "CONTRIBUTOR", Label: "contributor"},
		{Value: "FIRST_TIME_CONTRIBUTOR", Label: "first-time contributor"},
		{Value: "FIRST_TIMER", Label: "first-timer"},
		{Value: "MANNEQUIN", Label: "mannequin"},
		{Value: "NONE", Label: "none"},
	}
}

// Size grades are the inbox card's: a pull request's lines added and removed together,
// at most SizeXSMax for XS, and so on, XL beyond SizeLMax.
const (
	SizeXSMax = 10
	SizeSMax  = 50
	SizeMMax  = 200
	SizeLMax  = 500
)

func sizeChoices() []Choice {
	return []Choice{
		{Value: "XS", Label: "XS"},
		{Value: "S", Label: "S"},
		{Value: "M", Label: "M"},
		{Value: "L", Label: "L"},
		{Value: "XL", Label: "XL"},
	}
}

// SizeOf grades a pull request by the lines it adds and removes together, as the inbox
// card does.
func SizeOf(lines int) string {
	switch {
	case lines <= SizeXSMax:
		return "XS"
	case lines <= SizeSMax:
		return "S"
	case lines <= SizeMMax:
		return "M"
	case lines <= SizeLMax:
		return "L"
	default:
		return "XL"
	}
}

func sensitiveChoices() []Choice {
	cats := sensitive.Categories()
	out := make([]Choice, 0, len(cats)+1)

	for _, c := range cats {
		out = append(out, Choice{Value: c.ID, Label: strings.ToLower(c.Short)})
	}

	return append(out, Choice{Value: sensitive.CategoryCustom, Label: "your patterns"})
}

func ecosystemChoices() []Choice {
	ecos := bump.Ecosystems()
	out := make([]Choice, 0, len(ecos))

	for _, e := range ecos {
		out = append(out, Choice{Value: string(e.ID), Label: e.Label})
	}

	return out
}
