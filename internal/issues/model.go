package issues

import "github.com/mrz1836/nilbox/internal/item"

// Action is something the issue sheet can do with an issue.
type Action string

// The sheet's actions.
const (
	// ActionNone is no action: the primary of a sheet that can do nothing now.
	ActionNone Action = ""

	// ActionComment posts the user's text as a comment.
	ActionComment Action = "comment"

	// ActionCloseCompleted closes the issue as completed, posting the user's text first
	// when there is any. It always asks first.
	ActionCloseCompleted Action = "close_completed"

	// ActionCloseNotPlanned closes the issue as not planned, posting the user's text first
	// when there is any. It always asks first.
	ActionCloseNotPlanned Action = "close_not_planned"
)

// closes reports an action that closes the issue.
func (a Action) closes() bool {
	return a == ActionCloseCompleted || a == ActionCloseNotPlanned
}

// Outcome is what an action came to.
type Outcome string

// The outcomes.
const (
	// OutcomeCommented is a comment GitHub took.
	OutcomeCommented Outcome = "commented"

	// OutcomeClosed is a close GitHub made, with the user's comment first when there was
	// one.
	OutcomeClosed Outcome = "closed"

	// OutcomeCommentedNotClosed is a close whose comment GitHub took and whose close it
	// didn't make: the comment stands, so the sheet drops it from the draft.
	OutcomeCommentedNotClosed Outcome = "commented_not_closed"

	// OutcomeNewActivity is a close refused before anything was sent: someone commented
	// since the user opened the issue.
	OutcomeNewActivity Outcome = "new_activity"

	// OutcomeAlreadyClosed is a close of an issue someone closed first.
	OutcomeAlreadyClosed Outcome = "already_closed"
)

// StandingState is where an issue stands: the sheet's one sentence above its details.
type StandingState string

// The standings, highest first: the first that holds is the issue's.
const (
	StandingGone        StandingState = "gone"
	StandingClosed      StandingState = "closed"
	StandingLocked      StandingState = "locked"
	StandingFixMerged   StandingState = "fix_merged"
	StandingFixOpen     StandingState = "fix_open"
	StandingRepliedLast StandingState = "replied_last"
	StandingAssigned    StandingState = "assigned"
	StandingOpen        StandingState = "open"
)

// StateReason is why an issue is closed, or was reopened, as GitHub says it.
type StateReason string

// The state reasons.
const (
	ReasonNone       StateReason = ""
	ReasonCompleted  StateReason = "COMPLETED"
	ReasonNotPlanned StateReason = "NOT_PLANNED"
	ReasonDuplicate  StateReason = "DUPLICATE"
	ReasonReopened   StateReason = "REOPENED"
)

// RefState is a pull request's or an issue's state.
type RefState string

// The states.
const (
	StateOpen   RefState = "OPEN"
	StateClosed RefState = "CLOSED"
	StateMerged RefState = "MERGED"
)

// EventKind is what one entry of an issue's timeline is.
type EventKind string

// The timeline's kinds.
const (
	EventComment    EventKind = "comment"
	EventMentioned  EventKind = "mentioned"
	EventLinked     EventKind = "linked"
	EventUnlinked   EventKind = "unlinked"
	EventClosed     EventKind = "closed"
	EventReopened   EventKind = "reopened"
	EventAssigned   EventKind = "assigned"
	EventUnassigned EventKind = "unassigned"
	EventLabeled    EventKind = "labeled"
	EventUnlabeled  EventKind = "unlabeled"
	EventRenamed    EventKind = "renamed"
	EventDuplicate  EventKind = "duplicate"
)

// ChecksWord is a pull request's checks, in a word.
type ChecksWord string

// The checks' words.
const (
	ChecksNone    ChecksWord = ""
	ChecksPassing ChecksWord = "passing"
	ChecksFailing ChecksWord = "failing"
	ChecksRunning ChecksWord = "running"
)

// ReviewWord is a pull request's review decision, in a word.
type ReviewWord string

// The review decisions' words.
const (
	ReviewNone             ReviewWord = ""
	ReviewApproved         ReviewWord = "approved"
	ReviewChangesRequested ReviewWord = "changes_requested"
	ReviewRequired         ReviewWord = "review_required"
)

// Issue is one issue as the sheet shows it. Its labels, projects, and counts come from the
// inbox card; the rest from the sheet's last read of GitHub, or are empty before one. No
// list is ever nil.
type Issue struct {
	ID     int64  `json:"id"`
	Repo   string `json:"repo"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	URL    string `json:"url"`

	State       RefState    `json:"state"`
	StateReason StateReason `json:"stateReason"`
	ClosedAt    string      `json:"closedAt"`

	// Author opened it; IsBot says it is a bot, and Mine that it is the user.
	Author string `json:"author"`
	IsBot  bool   `json:"isBot"`
	Mine   bool   `json:"mine"`

	// OpenedAt is when it was opened, and UpdatedAt GitHub's time of its newest activity,
	// as of the read: the window compares the card's with it.
	OpenedAt  string `json:"openedAt"`
	UpdatedAt string `json:"updatedAt"`

	// Assigned reports that it is in the user's Assigned list.
	Assigned bool `json:"assigned"`

	// Locked reports a locked conversation, and LockReason why, as GitHub says it.
	Locked     bool   `json:"locked"`
	LockReason string `json:"lockReason"`

	// Body is the description, BodyTrimmed whether the cache cut it short (its page has
	// all of it), and EditedAt when it was last edited, or "".
	Body        string     `json:"body"`
	BodyTrimmed bool       `json:"bodyTrimmed"`
	EditedAt    string     `json:"editedAt"`
	Reactions   []Reaction `json:"reactions"`

	Type      *IssueType `json:"type"`
	Milestone *Milestone `json:"milestone"`
	Labels    []Label    `json:"labels"`
	Projects  []Project  `json:"projects"`

	// Assignees and Participants are the first few of each, and More how many others.
	Assignees        []Person `json:"assignees"`
	MoreAssignees    int      `json:"moreAssignees"`
	Participants     []Person `json:"participants"`
	MoreParticipants int      `json:"moreParticipants"`

	Parent      *Linked `json:"parent"`
	DuplicateOf *Linked `json:"duplicateOf"`

	// SubIssues are the first sub-issues, of SubIssuesTotal, SubIssuesDone of them done.
	SubIssues      []Linked `json:"subIssues"`
	SubIssuesTotal int      `json:"subIssuesTotal"`
	SubIssuesDone  int      `json:"subIssuesDone"`

	// Fixes are the pull requests that close it once merged, MoreFixes how many others;
	// Mentioned the pull requests and issues that mentioned it, in the timeline read.
	Fixes     []Fix    `json:"fixes"`
	MoreFixes int      `json:"moreFixes"`
	Mentioned []Linked `json:"mentioned"`

	Timeline TimelinePage `json:"timeline"`

	// LastSeenAt is GitHub's time of the newest activity in the read the sheet showed when
	// the user last opened the issue, "" before then: what is newer is new to them.
	LastSeenAt string `json:"lastSeenAt"`

	// Seen is what an action pins: the comments as of the read the sheet shows.
	Seen Seen `json:"seen"`

	Standing Standing `json:"standing"`
	Offers   []Offer  `json:"offers"`
	Primary  Action   `json:"primary"`

	// FetchedAt is when the sheet last read it from GitHub, "" before then; Stale says a
	// read is due.
	FetchedAt string `json:"fetchedAt"`
	Stale     bool   `json:"stale"`
}

// Reaction counts one kind of reaction to the description, by GitHub's name for it.
type Reaction struct {
	Content string `json:"content"`
	Count   int    `json:"count"`
}

// IssueType is an issue's type, with its color as "#rrggbb" or "".
type IssueType struct {
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

// Milestone is the milestone an issue is in. DueOn is its due day at midnight UTC, or "".
type Milestone struct {
	Title    string  `json:"title"`
	URL      string  `json:"url"`
	DueOn    string  `json:"dueOn"`
	State    string  `json:"state"`
	Progress float64 `json:"progress"`
}

// Label is a label with its color as "#rrggbb", or "".
type Label struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// Project is the issue's place in a project, as its card shows it.
type Project struct {
	Title       string         `json:"title"`
	URL         string         `json:"url"`
	Status      string         `json:"status"`
	StatusColor string         `json:"statusColor"`
	Fields      []ProjectField `json:"fields"`
}

// ProjectField is the issue's value in one of a project's fields.
type ProjectField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Color string `json:"color"`
}

// Person is an assignee or a participant: Mine says it is the user.
type Person struct {
	Login string `json:"login"`
	Mine  bool   `json:"mine"`
}

// Linked is a pull request or an issue the sheet names.
type Linked struct {
	Kind        item.Kind   `json:"kind"`
	Repo        string      `json:"repo"`
	Number      int         `json:"number"`
	Title       string      `json:"title"`
	URL         string      `json:"url"`
	State       RefState    `json:"state"`
	StateReason StateReason `json:"stateReason"`
	Draft       bool        `json:"draft"`
}

// Fix is a pull request that closes the issue once merged: where it merges, when it did,
// and its checks and review.
type Fix struct {
	PR       Linked     `json:"pr"`
	Base     string     `json:"base"`
	MergedAt string     `json:"mergedAt"`
	Checks   ChecksWord `json:"checks"`
	Review   ReviewWord `json:"review"`
}

// TimelinePage is a page of the timeline, oldest first. Before is the cursor that reads the
// items before it, "" when none come before it, and Total counts every item of the whole
// timeline the sheet reads, so Total less the items the sheet holds is how many it doesn't.
type TimelinePage struct {
	Items  []TimelineItem `json:"items"`
	Total  int            `json:"total"`
	Before string         `json:"before"`
}

// TimelineItem is one comment or event. Which fields are set depends on Kind.
type TimelineItem struct {
	Kind       EventKind `json:"kind"`
	ID         string    `json:"id"`
	At         string    `json:"at"`
	Actor      string    `json:"actor"`
	ActorIsBot bool      `json:"actorIsBot"`
	Mine       bool      `json:"mine"`

	// A comment's text, page, author's relationship to the repository, and when it was
	// edited; Trimmed says the cache cut the text short. Minimized says someone folded it
	// away, and MinimizedReason why, as an upper-case word.
	Body            string `json:"body"`
	Trimmed         bool   `json:"trimmed"`
	URL             string `json:"url"`
	Association     string `json:"association"`
	EditedAt        string `json:"editedAt"`
	Minimized       bool   `json:"minimized"`
	MinimizedReason string `json:"minimizedReason"`

	// Ref is the other item an event names; WillClose a mention that closes the issue
	// once merged; Commit a commit that closed it; DuplicateOf what a close as a duplicate
	// names.
	Ref         *Linked     `json:"ref"`
	WillClose   bool        `json:"willClose"`
	Commit      string      `json:"commit"`
	DuplicateOf *Linked     `json:"duplicateOf"`
	StateReason StateReason `json:"stateReason"`

	Label    *Label `json:"label"`
	Assignee string `json:"assignee"`

	// From and To are a rename's titles.
	From string `json:"from"`
	To   string `json:"to"`
}

// Seen is the comments the user saw: how many, and the newest by ID and time ("" for
// none). A close checks nobody commented since.
type Seen struct {
	Comments int    `json:"comments"`
	Latest   string `json:"latest"`
	LatestAt string `json:"latestAt"`
}

// Standing is where the issue stands: its state, a sentence, the time it is about ("" for
// none), and Opened when that time is the issue's opening, as in "opened 3 days ago".
type Standing struct {
	State    StandingState `json:"state"`
	Sentence string        `json:"sentence"`
	At       string        `json:"at"`
	Opened   bool          `json:"opened"`
}

// Offer is one action the sheet offers: its words, whether it can run now and why not,
// whether it asks first, whether it posts the user's text as a comment first, and, when
// GitHub can't take that comment now, why.
type Offer struct {
	Action         Action `json:"action"`
	Label          string `json:"label"`
	Enabled        bool   `json:"enabled"`
	Reason         string `json:"reason"`
	Confirm        bool   `json:"confirm"`
	CommentsFirst  bool   `json:"commentsFirst"`
	CommentBlocked string `json:"commentBlocked"`
}

// Reply is a reply from the prompt library the composer offers: its ID, its title, and the
// nickname the menu shows.
type Reply struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Nickname string `json:"nickname"`
}

// ActRequest asks for an action on the issue with ItemID, pinned to Seen, the comments as
// of the read the user saw, with the user's text.
type ActRequest struct {
	ItemID int64  `json:"itemId"`
	Action Action `json:"action"`
	Seen   Seen   `json:"seen"`
	Body   string `json:"body"`
}

// Result is what an action came to: its outcome, the toast's words, why a close didn't
// happen, the steps GitHub made, whether the user's text is on GitHub now (Commented), and
// whether the issue left the inbox.
type Result struct {
	Outcome   Outcome  `json:"outcome"`
	Message   string   `json:"message"`
	Reason    string   `json:"reason"`
	Steps     []string `json:"steps"`
	Commented bool     `json:"commented"`
	LeftInbox bool     `json:"leftInbox"`
}
