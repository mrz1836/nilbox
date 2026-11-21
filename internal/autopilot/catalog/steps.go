package catalog

import "slices"

import "github.com/mrz1836/nilbox/internal/item"

// StepKind is one kind of step a routine's Then runs, as the spec spells its key.
type StepKind string

// The kinds of step.
const (
	StepNotify        StepKind = "notify"
	StepPin           StepKind = "pin"
	StepSnooze        StepKind = "snooze"
	StepDismiss       StepKind = "dismiss"
	StepRestore       StepKind = "restore"
	StepMarkDone      StepKind = "markDone"
	StepGitHub        StepKind = "github"
	StepAgent         StepKind = "agent"
	StepWaitForChecks StepKind = "waitForChecks"
	StepMerge         StepKind = "merge"
	StepPostReview    StepKind = "postReview"
	StepTellMe        StepKind = "tellMe"

	// StepScript runs one of the user's own Repo scripts in the item's clone.
	StepScript StepKind = "script"
)

// Dispatcher names the part of the engine that runs a kind of step. Each is built on
// its own; until it is, its steps are listed but not Offered.
type Dispatcher string

// The dispatchers.
const (
	// DispatchTriage runs the inbox's own triage: pin, snooze, dismiss, and restore.
	DispatchTriage Dispatcher = "triage"

	// DispatchNotices posts the routine's notices: Notify me and Tell me.
	DispatchNotices Dispatcher = "notices"

	// DispatchGitHub sends the app's own GitHub writes, through the sheets' services and
	// the write gate, and marks a notification done through the chore lane.
	DispatchGitHub Dispatcher = "github"

	// DispatchAgent starts a coding agent, in the background or in the user's terminal.
	DispatchAgent Dispatcher = "agent"

	// DispatchVerify follows the checks on the commit an agent pushed: the engine's own,
	// which judges a verifying match on every cycle from what the inbox stored.
	DispatchVerify Dispatcher = "verify"

	// DispatchMerge merges, or turns auto-merge back on, inside GitHub's own rules.
	DispatchMerge Dispatcher = "merging"

	// DispatchReview posts a read-only agent's review.
	DispatchReview Dispatcher = "review"

	// DispatchScripts runs one of the user's own Repo scripts, in the app, in the item's
	// clone, through Repo scripts' own run.
	DispatchScripts Dispatcher = "scripts"
)

// Dispatchers returns every dispatcher.
func Dispatchers() []Dispatcher {
	return []Dispatcher{
		DispatchTriage, DispatchNotices, DispatchGitHub, DispatchAgent, DispatchVerify, DispatchMerge, DispatchReview,
		DispatchScripts,
	}
}

// Outcome is how a step ended, which a later step's Only if names. A step whose Only if
// doesn't hold is skipped, and the routine goes on.
type Outcome string

// The outcomes a step's Only if can name.
const (
	// OutcomePassedChecks: the checks passed, from the latest Wait for the checks.
	OutcomePassedChecks Outcome = "passed"

	// OutcomeFailingChecks: the checks still fail, from the latest Wait for the checks.
	OutcomeFailingChecks Outcome = "stillFails"

	// OutcomePushed: the agent step pushed.
	OutcomePushed Outcome = "pushed"

	// OutcomeNotPushed: the agent step pushed nothing.
	OutcomeNotPushed Outcome = "notPushed"

	// OutcomeApprove: a read-only agent's verdict was to approve.
	OutcomeApprove Outcome = "approve"

	// OutcomeRequestChanges: a read-only agent's verdict was to request changes.
	OutcomeRequestChanges Outcome = "requestChanges"
)

// OutcomeInfo describes an outcome for the builder's Only if menu.
type OutcomeInfo struct {
	ID Outcome `json:"id"`

	// Label says it: "the checks passed".
	Label string `json:"label"`

	// From is the kind of step that ends with it; Only if names the latest step of that
	// kind before it.
	From StepKind `json:"from"`
}

// Outcomes returns every outcome Only if can name.
func Outcomes() []OutcomeInfo {
	return []OutcomeInfo{
		{ID: OutcomePassedChecks, Label: "the checks passed", From: StepWaitForChecks},
		{ID: OutcomeFailingChecks, Label: "the checks still fail", From: StepWaitForChecks},
		{ID: OutcomePushed, Label: "it pushed", From: StepAgent},
		{ID: OutcomeNotPushed, Label: "it didn't push", From: StepAgent},
		{ID: OutcomeApprove, Label: "its verdict was approve", From: StepAgent},
		{ID: OutcomeRequestChanges, Label: "its verdict was request changes", From: StepAgent},
	}
}

// Step describes one kind of step for the builder and the engine.
type Step struct {
	Kind StepKind `json:"kind"`

	// Label is the builder's name for it: "Run a coding agent".
	Label string `json:"label"`

	// Help says what it does.
	Help string `json:"help"`

	// Dispatcher is what runs it.
	Dispatcher Dispatcher `json:"dispatcher"`

	// Local reports a step that runs at once wherever it sits, whatever the routine's
	// mode, since it changes only what this Mac shows.
	Local bool `json:"local"`

	// AsksFirst reports a step that follows the routine's mode: in Ask first, the first
	// such step waits for the user's OK, and the steps after it go on their own.
	AsksFirst bool `json:"asksFirst"`

	// Undo reports a step the routine's Activity can take back.
	Undo bool `json:"undo"`

	// Kinds are the kinds of item it can act on.
	Kinds []item.Kind `json:"kinds"`

	// Offered reports a step the builder offers: one whose dispatcher is built.
	Offered bool `json:"offered"`
}

// Applies reports whether the step can act on an item of kind.
func (s Step) Applies(kind item.Kind) bool {
	return slices.Contains(s.Kinds, kind)
}

// Steps returns every kind of step, in the builder's order.
func Steps() []Step {
	return []Step{
		{
			Kind: StepNotify, Label: "Notify me", Dispatcher: DispatchNotices, Local: true, Kinds: both(), Offered: true,
			Help: "A banner and an entry in the Notifications page, with the reason.",
		},
		{
			Kind: StepPin, Label: "Pin", Dispatcher: DispatchTriage, Local: true, Undo: true, Kinds: both(), Offered: true,
			Help: "Pins it in the inbox, marked with the routine that did it.",
		},
		{
			Kind: StepSnooze, Label: "Snooze", Dispatcher: DispatchTriage, Local: true, Undo: true, Kinds: both(),
			Offered: true,
			Help:    "Snoozes it for a number of days, under your wake rules. What you set aside yourself stays as you left it.",
		},
		{
			Kind: StepDismiss, Label: "Dismiss", Dispatcher: DispatchTriage, Local: true, Undo: true, Kinds: both(),
			Offered: true,
			Help:    "Dismisses it from the inbox until it's restored. What you set aside yourself stays as you left it.",
		},
		{
			Kind: StepRestore, Label: "Restore", Dispatcher: DispatchTriage, Local: true, Undo: true, Kinds: both(),
			Offered: true,
			Help:    "Brings it back from a snooze or a dismissal, yours too, to your attention when the routine's conditions hold.",
		},
		{
			Kind: StepMarkDone, Label: "Mark the GitHub notification done", Dispatcher: DispatchGitHub, Local: true,
			Kinds: both(), Offered: true,
			Help: "Marks this item's GitHub notifications done, as the Notifications page's Done does. It changes " +
				"only your own notifications, and one marked done can be moved back on GitHub.",
		},
		{
			Kind: StepGitHub, Label: "GitHub actions", Dispatcher: DispatchGitHub, AsksFirst: true, Kinds: both(),
			Offered: true,
			Help: "The app's own writes, as the review and issue sheets send them, each pinned to the head read " +
				"right before it goes.",
		},
		{
			Kind: StepAgent, Label: "Run a coding agent", Dispatcher: DispatchAgent, AsksFirst: true, Kinds: both(),
			Offered: true, Help: "Starts the routine's agent on the item with a library prompt.",
		},
		scriptStep(),
		{
			Kind: StepWaitForChecks, Label: "Wait for the checks", Dispatcher: DispatchVerify, Kinds: prOnly(),
			Offered: true,
			Help:    "Follows the checks on the agent's pushed commit until every one has finished and settled.",
		},
		{
			Kind: StepMerge, Label: "Merge it", Dispatcher: DispatchMerge, AsksFirst: true, Kinds: prOnly(),
			Offered: true,
			Help:    "Once the checks passed, turns auto-merge back on if it was on, else merges when GitHub allows.",
		},
		{
			Kind: StepPostReview, Label: "Post the agent's review", Dispatcher: DispatchReview, AsksFirst: true,
			Kinds: prOnly(), Offered: true,
			Help: "Posts what a read-only agent step wrote, as a comment or as its verdict.",
		},
		{
			Kind: StepTellMe, Label: "Tell me", Dispatcher: DispatchNotices, Local: true, Kinds: both(), Offered: true,
			Help: "A notice when the routine reaches this step, with what happened.",
		},
	}
}

// scriptStep is Run one of my scripts, in an entry of its own.
func scriptStep() Step {
	return Step{
		Kind: StepScript, Label: "Run one of my scripts", Dispatcher: DispatchScripts, AsksFirst: true, Kinds: both(),
		Offered: true,
		Help: "Runs one of your own Repo scripts in the item's clone, in the app, as Repo scripts runs it. A script " +
			"set to ask before running waits for your OK each time.",
	}
}

// GitHubActionKind is one of the GitHub step's actions, as the spec spells its key.
type GitHubActionKind string

// The GitHub step's actions: the review and issue sheets' own writes, which Autopilot may
// send in the routine's mode. A merge, an approval, a close, and approving workflow runs
// are none of them.
const (
	GitHubRebase       GitHubActionKind = "rebase"
	GitHubUpdateBranch GitHubActionKind = "updateBranch"
	GitHubReply        GitHubActionKind = "reply"
	GitHubLabels       GitHubActionKind = "labels"
)

// GitHubAction describes one of the GitHub step's actions for the builder.
type GitHubAction struct {
	Kind GitHubActionKind `json:"kind"`

	// Label is the builder's name for it: "Ask Dependabot to rebase".
	Label string `json:"label"`

	// Help says how far it can be taken back.
	Help string `json:"help"`

	// Dispatcher is what sends it: always DispatchGitHub.
	Dispatcher Dispatcher `json:"dispatcher"`

	// Kinds are the kinds of item it acts on.
	Kinds []item.Kind `json:"kinds"`

	// Offered reports an action the builder offers.
	Offered bool `json:"offered"`
}

// GitHubActions returns the GitHub step's actions, in the builder's order: the four writes
// the review and issue sheets already send, each with how far it can be taken back.
func GitHubActions() []GitHubAction {
	return []GitHubAction{
		{
			Kind: GitHubRebase, Label: "Ask Dependabot to rebase", Dispatcher: DispatchGitHub, Kinds: prOnly(), Offered: true,
			Help: "Comments @dependabot rebase, as the review sheet does, on a Dependabot pull request behind its base " +
				"or in conflict. Dependabot force-pushes its branch, which can't be taken back.",
		},
		{
			Kind: GitHubUpdateBranch, Label: "Update branch", Dispatcher: DispatchGitHub, Kinds: prOnly(), Offered: true,
			Help: "Merges the base branch into the pull request's branch, as the review sheet does. The merge commit " +
				"stays.",
		},
		{
			Kind: GitHubReply, Label: "Comment one of my replies", Dispatcher: DispatchGitHub, Kinds: both(), Offered: true,
			Help: "Posts one of your comment replies, filled in for the item, as the sheets post one. It can be " +
				"deleted, but GitHub has told people by then.",
		},
		{
			Kind: GitHubLabels, Label: "Add or remove labels", Dispatcher: DispatchGitHub, Kinds: both(), Offered: true,
			Help: "Adds and takes off labels, as the issue sheet does. They can be put back by hand, and a label can " +
				"start a repository's label-triggered workflows.",
		},
	}
}

// Where is where an agent step runs.
type Where string

// The places.
const (
	// WhereBackground runs headless, always in the item's worktree, fenced.
	WhereBackground Where = "background"

	// WhereTerminal runs in the user's terminal, exactly as a click's launch does.
	WhereTerminal Where = "terminal"

	// DefaultWhere is the agent step's place when its spec names none.
	DefaultWhere = WhereBackground
)

// PlaceInfo describes where an agent step can run, for the builder.
type PlaceInfo struct {
	ID    Where  `json:"id"`
	Label string `json:"label"`
	Help  string `json:"help"`

	// Offered reports a place this build runs an agent in. An agent step set to run where
	// this build can't is a problem the builder shows, so it is never saved to wait for a
	// later build.
	Offered bool `json:"offered"`
}

// Places returns where an agent step can run, the default first.
func Places() []PlaceInfo {
	return []PlaceInfo{
		{
			ID: WhereBackground, Label: "In the background", Offered: true,
			Help: "Headless, always in the item's worktree, held in by the fence.",
		},
		{
			ID: WhereTerminal, Label: "In my terminal", Offered: true,
			Help: "Exactly a click's launch: the same holds, worktree, terminal, and session.",
		},
	}
}

// PlaceByID returns the place with id.
func PlaceByID(id Where) (PlaceInfo, bool) {
	for _, p := range Places() {
		if p.ID == id {
			return p, true
		}
	}

	return PlaceInfo{}, false
}

// Preset is a tools preset: what a background agent may do, whatever its profile allows.
type Preset string

// The tools presets.
const (
	PresetReadOnly     Preset = "readOnly"
	PresetGitHubTasks  Preset = "githubTasks"
	PresetEditTest     Preset = "editTest"
	PresetEditTestPush Preset = "editTestPush"
	PresetProfile      Preset = "profile"

	// DefaultPreset is the agent step's tools preset when its spec names none.
	DefaultPreset = PresetEditTestPush
)

// PresetInfo describes a tools preset for the builder.
type PresetInfo struct {
	ID    Preset `json:"id"`
	Label string `json:"label"`
	Help  string `json:"help"`

	// MayPush reports a preset under which the agent can push, so Wait for the checks can
	// follow it.
	MayPush bool `json:"mayPush"`

	// ReadOnly reports the preset that changes nothing, so Post the agent's review and a
	// verdict's Only if can follow it.
	ReadOnly bool `json:"readOnly"`
}

// Presets returns every tools preset, from the narrowest.
func Presets() []PresetInfo {
	return []PresetInfo{
		{ID: PresetReadOnly, Label: "Read only", Help: "Reads and plans; changes nothing.", ReadOnly: true},
		{
			ID: PresetGitHubTasks, Label: "GitHub tasks only",
			Help: "Only the gh commands for small GitHub changes: re-run failed jobs, a comment, a label.",
		},
		{ID: PresetEditTest, Label: "Edit and test, no push", Help: "Edits and runs the repository's checks; never pushes."},
		{
			ID: PresetEditTestPush, Label: "Edit, test, and push to the PR's branch", MayPush: true,
			Help: "As Edit and test, plus exactly one push, to the pull request's own branch.",
		},
		{
			ID: PresetProfile, Label: "The profile's own permissions", MayPush: true,
			Help: "Exactly what a click passes, within the fence's hard lines.",
		},
	}
}

// MergeHow is how Merge it merges.
type MergeHow string

// The ways to merge.
const (
	// MergeAutoIfItWasOn turns GitHub's auto-merge back on if it was on before the
	// match, and otherwise merges when GitHub says it's mergeable.
	MergeAutoIfItWasOn MergeHow = "autoMergeIfItWasOn"

	// MergeWhenMergeable merges when GitHub says it's mergeable.
	MergeWhenMergeable MergeHow = "whenMergeable"
)

// MergeHowInfo describes a way Merge it merges, for the builder.
type MergeHowInfo struct {
	ID    MergeHow `json:"id"`
	Label string   `json:"label"`
	Help  string   `json:"help"`
}

// MergeHows returns the ways Merge it merges, in the builder's order.
func MergeHows() []MergeHowInfo {
	return []MergeHowInfo{
		{
			ID: MergeAutoIfItWasOn, Label: "auto-merge if it was on, else when GitHub allows",
			Help: "Turns GitHub's auto-merge back on if it was on before the match, and otherwise merges when GitHub " +
				"says it's mergeable.",
		},
		{ID: MergeWhenMergeable, Label: "when GitHub allows", Help: "Merges when GitHub says it's mergeable."},
	}
}

// MergeMethod is the method Merge it merges with; "" is the repository's own, as
// Settings → Reviewing picks it.
type MergeMethod string

// The merge methods.
const (
	MergeDefault MergeMethod = ""
	MergeCommit  MergeMethod = "merge"
	MergeSquash  MergeMethod = "squash"
	MergeRebase  MergeMethod = "rebase"
)

// MergeMethodInfo describes a merge method for the builder.
type MergeMethodInfo struct {
	ID    MergeMethod `json:"id"`
	Label string      `json:"label"`
	Help  string      `json:"help"`
}

// MergeMethods returns the methods Merge it merges with, the repository's own ("") first.
// Squash and rebase are labeled by their own names, as the builder's menu shows them.
func MergeMethods() []MergeMethodInfo {
	return []MergeMethodInfo{
		{
			ID: MergeDefault, Label: "the repository's own method",
			Help: "Merges with the method Settings → Reviewing's Merge method picks, as the review sheet does: by " +
				"default, the one GitHub's merge button offers first in that repository, and auto-merge's own " +
				"on a pull request whose auto-merge is on.",
		},
		{ID: MergeCommit, Label: "merge commit", Help: "Adds every commit to the base branch, joined by a merge commit."},
		{
			ID: MergeSquash, Label: string(MergeSquash),
			Help: "Combines the pull request's commits into one commit on the base branch.",
		},
		{
			ID: MergeRebase, Label: string(MergeRebase),
			Help: "Adds each commit to the base branch on its own, with no merge commit.",
		},
	}
}

// ReviewAs is how Post the agent's review posts it.
type ReviewAs string

// The ways to post a review.
const (
	// ReviewAsComment posts the review as a comment.
	ReviewAsComment ReviewAs = "comment"

	// ReviewAsVerdict posts it as its verdict: a request for changes, or an approval when
	// approving is allowed, else a comment.
	ReviewAsVerdict ReviewAs = "verdict"
)

// ReviewAsInfo describes a way Post the agent's review posts it, for the builder.
type ReviewAsInfo struct {
	ID    ReviewAs `json:"id"`
	Label string   `json:"label"`
	Help  string   `json:"help"`
}

// ReviewAses returns the ways Post the agent's review posts it, in the builder's order.
func ReviewAses() []ReviewAsInfo {
	return []ReviewAsInfo{
		{ID: ReviewAsComment, Label: "as a comment", Help: "Posts the agent's review as a comment, whatever its verdict."},
		{
			ID: ReviewAsVerdict, Label: "as its verdict",
			Help: "Posts it as its verdict: a request for changes, or, with Allow approving on, an approval of a pull " +
				"request Autopilot may approve; any other approval posts as a comment.",
		},
	}
}

// TellWhen is what Tell me says.
type TellWhen string

// The notices Tell me posts.
const (
	// TellReached says the routine reached the step.
	TellReached TellWhen = "reached"

	// TellVerdict says how the steps before it ended: merged, still failing, the review
	// is ready.
	TellVerdict TellWhen = "verdict"
)

// TellWhenInfo describes what Tell me says, for the builder.
type TellWhenInfo struct {
	ID    TellWhen `json:"id"`
	Label string   `json:"label"`
	Help  string   `json:"help"`
}

// TellWhens returns what Tell me can say, the default first.
func TellWhens() []TellWhenInfo {
	return []TellWhenInfo{
		{ID: TellReached, Label: "that it got here", Help: "Tells you the routine reached this step."},
		{
			ID: TellVerdict, Label: "how the steps before it ended",
			Help: "Tells you how the steps before it ended: merged, still failing, or the review is ready.",
		},
	}
}

// OncePer is a routine's own once-per, over its trigger's.
type OncePer string

// The once-pers.
const (
	OncePerItem       OncePer = "item"
	OncePerHeadCommit OncePer = "headCommit"
	OncePerDay        OncePer = "day"
)

// ListSource is a reason an item is in the inbox, which a routine can watch: GitHub asks
// for the user's review, the user opened it, or it is assigned to them. An item can have
// more than one. The IDs are the inbox's three searches, which a stored spec keeps.
type ListSource string

// The inbox reasons.
const (
	SourceReviewRequested ListSource = ListReviewRequested
	SourceAuthored        ListSource = ListAuthored
	SourceAssigned        ListSource = ListAssigned
)

// ListSourceInfo describes an inbox reason: Label names it on its own, as the builder's
// Watch does, and Phrase says it of an item, after "it's", as a sentence does ("asking for
// your review").
type ListSourceInfo struct {
	ID     ListSource  `json:"id"`
	Label  string      `json:"label"`
	Phrase string      `json:"phrase"`
	Kinds  []item.Kind `json:"kinds"`
}

// ListSources returns the inbox reasons a routine can watch, with the kinds each holds: a
// review is asked only of a pull request.
func ListSources() []ListSourceInfo {
	return []ListSourceInfo{
		{ID: SourceReviewRequested, Label: "Review requested", Phrase: "asking for your review", Kinds: prOnly()},
		{ID: SourceAuthored, Label: "Opened by you", Phrase: "opened by you", Kinds: both()},
		{ID: SourceAssigned, Label: "Assigned to you", Phrase: "assigned to you", Kinds: both()},
	}
}
