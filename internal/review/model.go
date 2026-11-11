package review

import (
	"slices"
	"strings"
	"unicode"

	"github.com/mrz1836/nilbox/internal/bump"
	"github.com/mrz1836/nilbox/internal/sensitive"
)

// Action is something the review sheet can do with a pull request.
type Action string

// The sheet's actions.
const (
	// ActionNone is no action: the primary of a sheet that can do nothing now.
	ActionNone Action = ""

	// ActionApprove approves the pull request.
	ActionApprove Action = "approve"

	// ActionApproveMerge approves it, then merges it once GitHub says it can.
	ActionApproveMerge Action = "approve_merge"

	// ActionApproveAutoMerge approves it and turns on auto-merge, so GitHub merges it once
	// its requirements are met.
	ActionApproveAutoMerge Action = "approve_auto_merge"

	// ActionMerge merges it now.
	ActionMerge Action = "merge_now"

	// ActionAutoMerge turns on auto-merge without a review.
	ActionAutoMerge Action = "auto_merge"

	// ActionMergeBypass merges it now past the branch's rules, for a user GitHub lets do so,
	// such as a repository admin. It is only ever the user's explicit, confirmed choice.
	ActionMergeBypass Action = "merge_bypass"

	// ActionRequestChanges requests changes.
	ActionRequestChanges Action = "request_changes"

	// ActionComment leaves a review that only comments.
	ActionComment Action = "comment"

	// ActionUpdateBranch merges the base branch into the pull request's branch.
	ActionUpdateBranch Action = "update_branch"

	// ActionRebaseBot asks Dependabot to rebase its pull request, with a comment.
	ActionRebaseBot Action = "rebase_bot"
)

// merges reports an action that merges the pull request, now or once it can: every one
// of them asks the user to confirm first.
func (a Action) merges() bool {
	return slices.Contains([]Action{
		ActionApproveMerge, ActionApproveAutoMerge, ActionMerge, ActionAutoMerge, ActionMergeBypass,
	}, a)
}

// reviews reports an action that submits a review.
func (a Action) reviews() bool {
	return slices.Contains([]Action{
		ActionApprove, ActionApproveMerge, ActionApproveAutoMerge, ActionRequestChanges, ActionComment,
	}, a)
}

// commentsFirst reports a merge with no review of its own, which posts the user's text and
// line comments, when there are any, as a comment before it merges: the user's own pull
// request, where GitHub refuses an approval, then shows it was reviewed. Decide marks each
// offer with it (Offer.CommentsFirst), which is what Act and the window go by.
func (a Action) commentsFirst() bool {
	return slices.Contains([]Action{ActionMerge, ActionMergeBypass, ActionAutoMerge}, a)
}

// hasText reports text that says something: anything left once the white space the
// window's String.prototype.trim drops is gone from both ends, so the window and Act
// always agree. That is unicode.IsSpace's set but U+0085, which trim keeps, plus U+FEFF,
// which trim drops.
func hasText(s string) bool {
	return strings.TrimFunc(s, func(r rune) bool {
		return r == '\uFEFF' || (unicode.IsSpace(r) && r != '\u0085')
	}) != ""
}

// MergeMethod is how GitHub merges a pull request, as its GraphQL API names it.
type MergeMethod string

// The merge methods.
const (
	// MethodNone is no method: auto-merge off, or an action that merges nothing.
	MethodNone MergeMethod = ""

	MethodMerge  MergeMethod = "MERGE"
	MethodSquash MergeMethod = "SQUASH"
	MethodRebase MergeMethod = "REBASE"
)

// MethodPref is how the user chose to merge (the review.mergeMethod setting): as each
// repository offers first, or with one method wherever it is allowed.
type MethodPref string

// The merge method choices, as the settings store them.
const (
	PrefRepo   MethodPref = "repo"
	PrefSquash MethodPref = "squash"
	PrefMerge  MethodPref = "merge_commit"
	PrefRebase MethodPref = "rebase_merge"
)

// method is the merge method a choice names, or "" for PrefRepo.
func (p MethodPref) method() MergeMethod {
	switch p {
	case PrefSquash:
		return MethodSquash
	case PrefMerge:
		return MethodMerge
	case PrefRebase:
		return MethodRebase
	case PrefRepo:
		return ""
	default:
		return ""
	}
}

// Outcome is what an action came to.
type Outcome string

// Outcomes.
const (
	// OutcomeReviewed is a review submitted.
	OutcomeReviewed Outcome = "reviewed"

	// OutcomeMerged is a pull request merged, approved first or not.
	OutcomeMerged Outcome = "merged"

	// OutcomeAutoMerge is auto-merge turned on, approved first or not.
	OutcomeAutoMerge Outcome = "auto_merge"

	// OutcomeApprovedNotMerged is an approval GitHub took, and a merge it would not make
	// yet; Reason says why.
	OutcomeApprovedNotMerged Outcome = "approved_not_merged"

	// OutcomeCommentedNotMerged is the comment a merge posted first, which GitHub took, and
	// a merge, or auto-merge, it would not make; Reason says why.
	OutcomeCommentedNotMerged Outcome = "commented_not_merged"

	// OutcomeBranchUpdating is a branch update GitHub started.
	OutcomeBranchUpdating Outcome = "branch_updating"

	// OutcomeAskedBot is Dependabot asked to rebase.
	OutcomeAskedBot Outcome = "asked_bot"

	// OutcomePushedSince is an action refused, or never sent, because the pull request was
	// pushed to since the sheet read it.
	OutcomePushedSince Outcome = "pushed_since"
)

// PatchState says whether a file's diff came with it, and why not.
type PatchState string

// Patch states.
const (
	// PatchShown is a file whose Patch holds its diff.
	PatchShown PatchState = ""

	// PatchOmitted is a file whose diff the cache holds but Open left out, past the size it
	// returns at once; Patches reads it.
	PatchOmitted PatchState = "omitted"

	// PatchNone is a file GitHub shows no diff for: a binary file, or one too large.
	PatchNone PatchState = "no_content"

	// PatchCapped is a file whose diff the cache did not keep, past its size per pull
	// request; GitHub shows it.
	PatchCapped PatchState = "cache_cap"
)

// ChangeKind is what kind of change a pull request makes, judged from its files.
type ChangeKind string

// Kinds of change.
const (
	ChangeDependencies ChangeKind = "dependencies"
	ChangeDocs         ChangeKind = "docs"
	ChangeTests        ChangeKind = "tests"
	ChangeCI           ChangeKind = "ci"
	ChangeCode         ChangeKind = "code"
)

// ReadyState is how close a pull request is to merging.
type ReadyState string

// Readiness states.
const (
	ReadyMerge     ReadyState = "ready"
	ReadyUnstable  ReadyState = "unstable"
	ReadyBlocked   ReadyState = "blocked"
	ReadyBehind    ReadyState = "behind"
	ReadyConflicts ReadyState = "conflicts"
	ReadyDraft     ReadyState = "draft"
	ReadyQueue     ReadyState = "queue"
	ReadyAutoMerge ReadyState = "auto_merge"
	ReadyUnknown   ReadyState = "unknown"
	ReadyClosed    ReadyState = "closed"
)

// ReplyAction is the review a reply is written for.
type ReplyAction string

// The reviews a reply can be for.
const (
	ReplyApprove        ReplyAction = "approve"
	ReplyRequestChanges ReplyAction = "request_changes"
	ReplyComment        ReplyAction = "comment"
)

// Review is everything the review sheet shows about one pull request.
type Review struct {
	// ID is the inbox item.
	ID int64 `json:"id"`

	// Repo is "owner/name", and Number, Title, and URL the pull request's.
	Repo   string `json:"repo"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`

	// State is OPEN, MERGED, or CLOSED, as GitHub last said.
	State string `json:"state"`

	// Author is who opened it, and IsBot whether that is a bot.
	Author string `json:"author"`
	IsBot  bool   `json:"isBot"`

	// Mine reports the signed-in user's own pull request.
	Mine bool `json:"mine"`

	Draft bool `json:"draft"`

	// HeadRef is the branch it merges from, and BaseRef the one it merges into.
	HeadRef string `json:"headRef"`
	BaseRef string `json:"baseRef"`

	// ReviewRequested reports that it is in the Review requested list.
	ReviewRequested bool `json:"reviewRequested"`

	Labels    []string `json:"labels"`
	Additions int      `json:"additions"`
	Deletions int      `json:"deletions"`
	Commits   int      `json:"commits"`
	Comments  int      `json:"comments"`

	// Body is the description, as markdown.
	Body string `json:"body"`

	// Bump is what a dependency update's title says about it, or nil for any other pull
	// request.
	Bump *bump.Update `json:"bump"`

	// Change is what kind of change it makes, and where.
	Change Change `json:"change"`

	// Files are the files it changes, in GitHub's order; TotalFiles counts them all, and
	// FilesComplete reports that Files holds every one.
	Files         []File `json:"files"`
	TotalFiles    int    `json:"totalFiles"`
	FilesComplete bool   `json:"filesComplete"`

	Checks  []Check       `json:"checks"`
	Reviews []ReviewEntry `json:"reviews"`

	// Readiness says how close it is to merging.
	Readiness Readiness `json:"readiness"`

	// Offers are the actions the sheet offers, and Primary the one its button runs.
	Offers  []Offer `json:"offers"`
	Primary Action  `json:"primary"`

	// MergeMethods are the methods the repository allows, and DefaultMethod the one a
	// merge uses unless the user picks another.
	MergeMethods  []MergeMethod `json:"mergeMethods"`
	DefaultMethod MergeMethod   `json:"defaultMethod"`

	// AutoMerge is the method auto-merge merges with, "" while it is off.
	AutoMerge MergeMethod `json:"autoMerge"`

	// Head is the head commit everything above was read at, which an action names.
	Head string `json:"head"`

	// FetchedAt is when the sheet last read the pull request from GitHub, as RFC 3339, or
	// "" before it has.
	FetchedAt string `json:"fetchedAt"`

	// Stale reports that what the sheet shows is not current: it was never read from
	// GitHub, or the inbox has seen a push since.
	Stale bool `json:"stale"`
}

// Change is what kind of change a pull request makes.
type Change struct {
	Kind ChangeKind `json:"kind"`

	// Label names the kind, as in "Dependency update".
	Label string `json:"label"`

	// Areas are the folders it changes most, at most three, most changed first ("." for
	// the root).
	Areas []string `json:"areas"`
}

// File is one file a pull request changes.
type File struct {
	Path         string `json:"path"`
	PreviousPath string `json:"previousPath"`

	// Status is GitHub's word for the change: added, removed, modified, renamed, ...
	Status string `json:"status"`

	Additions int `json:"additions"`
	Deletions int `json:"deletions"`

	// Patch is the file's diff, when PatchState is PatchShown.
	Patch      string     `json:"patch"`
	PatchState PatchState `json:"patchState"`

	// Category is the short name of the sensitive category the file is in, and Level that
	// category's level; "" for a file in none.
	Category string          `json:"category"`
	Level    sensitive.Level `json:"level"`
}

// FilePatch is one file's diff, as Patches reads it.
type FilePatch struct {
	Path       string     `json:"path"`
	Patch      string     `json:"patch"`
	PatchState PatchState `json:"patchState"`
}

// Check is one check run or commit status on the head commit.
type Check struct {
	Name     string `json:"name"`
	Workflow string `json:"workflow"`

	// State is GitHub's word, and Outcome where the check counts: passed, failed, pending,
	// or skipped.
	State   string `json:"state"`
	Outcome string `json:"outcome"`

	URL      string `json:"url"`
	Required bool   `json:"required"`
}

// ReviewEntry is one reviewer's latest review.
type ReviewEntry struct {
	Author string `json:"author"`
	State  string `json:"state"`
	Body   string `json:"body"`
	URL    string `json:"url"`

	// SubmittedAt is when it was submitted, as RFC 3339, or "".
	SubmittedAt string `json:"submittedAt"`

	// Outdated reports a review left on an earlier head.
	Outdated bool `json:"outdated"`
}

// Readiness says how close a pull request is to merging.
type Readiness struct {
	State ReadyState `json:"state"`

	// Sentence says it in a line, as in "Needs 1 approval · checks running".
	Sentence string `json:"sentence"`

	// Needs lists what it still needs, each a short phrase.
	Needs []string `json:"needs"`
}

// Offer is one action the sheet offers.
type Offer struct {
	Action Action `json:"action"`

	// Label says exactly what the action does, as in "Approve & squash-merge".
	Label string `json:"label"`

	// Enabled reports that the action can run now, and Reason, for one that can't, why.
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason"`

	// Confirm reports an action that asks the user to confirm first: every merge.
	Confirm bool `json:"confirm"`

	// Warnings are what the user must accept before the action runs.
	Warnings []string `json:"warnings"`

	// Reviews reports an action that submits a review of its own, which the text and line
	// comments go with.
	Reviews bool `json:"reviews"`

	// CommentsFirst reports a merge with no review of its own, which posts the text and line
	// comments, when there are any, as a comment before it merges, and CommentBlocked why it
	// can't post one now, with how to merge without it, or "" when it can.
	CommentsFirst  bool   `json:"commentsFirst"`
	CommentBlocked string `json:"commentBlocked"`
}

// ActRequest is an action the user asked for.
type ActRequest struct {
	ItemID int64  `json:"itemId"`
	Action Action `json:"action"`

	// Head is the head commit the user saw. A pull request pushed to since is refused.
	Head string `json:"head"`

	// Body is the review's or comment's text. A merge with no review of its own (merge now,
	// past the rules, or when ready) posts it, with Comments, as a comment first, when
	// either holds anything.
	Body string `json:"body"`

	// Comments are the review's line comments.
	Comments []LineComment `json:"comments"`

	// Method is the merge method the user picked, or "" for the default.
	Method MergeMethod `json:"method"`

	// Accept reports that the user accepted the action's warnings.
	Accept bool `json:"accept"`
}

// Result is what an action came to. A partial success, such as an approval GitHub took
// and a merge it would not make yet, is a result, never an error.
type Result struct {
	Outcome Outcome `json:"outcome"`

	// Message says what happened, for the toast.
	Message string `json:"message"`

	// Reason says why GitHub would not merge, for OutcomeApprovedNotMerged and
	// OutcomeCommentedNotMerged.
	Reason string `json:"reason"`

	// Steps are what happened, in order.
	Steps []string `json:"steps"`

	// Commented reports that a merge posted the user's text and line comments as a comment
	// first, which GitHub took whatever the merge came to, so the draft no longer holds
	// anything to send.
	Commented bool `json:"commented"`

	// LeftReview reports that the pull request left the Review requested list, and
	// LeftInbox that it left every list.
	LeftReview bool `json:"leftReview"`
	LeftInbox  bool `json:"leftInbox"`

	// Method is the method a merge or auto-merge uses, or "".
	Method MergeMethod `json:"method"`
}

// Reply is a review reply from the prompt library.
type Reply struct {
	ID       int64       `json:"id"`
	Name     string      `json:"name"`
	Nickname string      `json:"nickname"`
	Action   ReplyAction `json:"action"`
}

// ReplyList is the replies for one review, with the defaults.
type ReplyList struct {
	Replies []Reply `json:"replies"`

	// Default is the reply a review of a person's pull request starts with, and
	// DefaultBots one of a bot's; 0 for none, which starts with no text.
	Default     int64 `json:"default"`
	DefaultBots int64 `json:"defaultBots"`
}
