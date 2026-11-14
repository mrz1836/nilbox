// Package timeline is the conversation the issue sheet and the review sheet show: the
// comments, reviews, and events of an issue or a pull request, one page of entries at a
// time, oldest first. Entries converts what the GitHub client read into the entries the
// window shows, and Cut shortens their texts so a sheet's cache stays within its size.
//
// The two sheets share it and import nothing of each other. It is a leaf: it imports the
// GitHub client's types, the item and clock words, and textutil, and no service or store.
// Each type's JSON is what the window reads and what the issue sheet's cache, written by
// builds before this package, already holds, so a name or a tag here never changes.
package timeline

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// Kind is what one entry of a timeline is.
type Kind string

// The timeline's kinds: both sheets' first, then those only a pull request has.
const (
	KindComment    Kind = "comment"
	KindMentioned  Kind = "mentioned"
	KindLinked     Kind = "linked"
	KindUnlinked   Kind = "unlinked"
	KindClosed     Kind = "closed"
	KindReopened   Kind = "reopened"
	KindAssigned   Kind = "assigned"
	KindUnassigned Kind = "unassigned"
	KindLabeled    Kind = "labeled"
	KindUnlabeled  Kind = "unlabeled"
	KindRenamed    Kind = "renamed"
	KindDuplicate  Kind = "duplicate"

	KindReviewed         Kind = "reviewed"
	KindMerged           Kind = "merged"
	KindForcePushed      Kind = "force_pushed"
	KindReviewRequested  Kind = "review_requested"
	KindReadyForReview   Kind = "ready_for_review"
	KindConvertedToDraft Kind = "converted_to_draft"
)

// RefState is a pull request's or an issue's state.
type RefState string

// The states.
const (
	StateOpen   RefState = "OPEN"
	StateClosed RefState = "CLOSED"
	StateMerged RefState = "MERGED"
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

// ReviewState is a review's state, as GitHub says it.
type ReviewState string

// The review states the sheet shows; a review still pending is never shown.
const (
	ReviewNone             ReviewState = ""
	ReviewApproved         ReviewState = "APPROVED"
	ReviewChangesRequested ReviewState = "CHANGES_REQUESTED"
	ReviewCommented        ReviewState = "COMMENTED"
	ReviewDismissed        ReviewState = "DISMISSED"
)

// Page is a page of a timeline, oldest first. Before is the cursor that reads the entries
// before it, "" when none come before it, and Total counts every entry of the whole
// timeline the sheet reads, so Total less the entries the sheet holds is how many it
// doesn't.
type Page struct {
	Items  []Entry `json:"items"`
	Total  int     `json:"total"`
	Before string  `json:"before"`
}

// Entry is one comment, review, or event. Which fields are set depends on Kind.
type Entry struct {
	Kind       Kind   `json:"kind"`
	ID         string `json:"id"`
	At         string `json:"at"`
	Actor      string `json:"actor"`
	ActorIsBot bool   `json:"actorIsBot"`
	Mine       bool   `json:"mine"`

	// A comment's or a review's text, page, author's relationship to the repository, and
	// when it was edited; Trimmed says the cache cut the text short. Minimized says someone
	// folded it away, and MinimizedReason why, as an upper-case word. A merge's page is its
	// URL too.
	Body            string `json:"body"`
	Trimmed         bool   `json:"trimmed"`
	URL             string `json:"url"`
	Association     string `json:"association"`
	EditedAt        string `json:"editedAt"`
	Minimized       bool   `json:"minimized"`
	MinimizedReason string `json:"minimizedReason"`

	// Ref is the other item an event names; WillClose a mention that closes the issue once
	// merged; Commit a commit that closed it, or a merge's commit, as a short SHA;
	// DuplicateOf what a close as a duplicate names.
	Ref         *Ref        `json:"ref"`
	WillClose   bool        `json:"willClose"`
	Commit      string      `json:"commit"`
	DuplicateOf *Ref        `json:"duplicateOf"`
	StateReason StateReason `json:"stateReason"`

	Label    *Label `json:"label"`
	Assignee string `json:"assignee"`

	// From and To are a rename's titles, or a force push's commits, before and after it, as
	// short SHAs.
	From string `json:"from"`
	To   string `json:"to"`

	// ReviewState is a review's state, and Outdated says it was left on an earlier commit
	// than the head the sheet read. LineComments are its first line comments, and
	// MoreLineComments how many others it has, which GitHub shows.
	ReviewState      ReviewState   `json:"reviewState"`
	Outdated         bool          `json:"outdated"`
	LineComments     []LineComment `json:"lineComments"`
	MoreLineComments int           `json:"moreLineComments"`

	// Reviewer is whom a review was requested of: a user's login, or a team as "org/team",
	// "" for one the user can't see.
	Reviewer string `json:"reviewer"`

	// Branch is the branch a merge went into.
	Branch string `json:"branch"`
}

// LineComment is one of a review's comments on a line of the diff: the file and the lines
// it is on (StartLine 0 for one line), as the diff it was left on numbers them once it is
// Outdated, the lines having changed since; its text, which Trimmed says the cache cut
// short; and Hunk, the last lines of the diff up to the line, as the diff shows them.
type LineComment struct {
	ID        string   `json:"id"`
	URL       string   `json:"url"`
	At        string   `json:"at"`
	Path      string   `json:"path"`
	Line      int      `json:"line"`
	StartLine int      `json:"startLine"`
	Outdated  bool     `json:"outdated"`
	Body      string   `json:"body"`
	Trimmed   bool     `json:"trimmed"`
	Hunk      []string `json:"hunk"`
}

// Ref is a pull request or an issue the sheet names.
type Ref struct {
	Kind        item.Kind   `json:"kind"`
	Repo        string      `json:"repo"`
	Number      int         `json:"number"`
	Title       string      `json:"title"`
	URL         string      `json:"url"`
	State       RefState    `json:"state"`
	StateReason StateReason `json:"stateReason"`
	Draft       bool        `json:"draft"`
}

// Label is a label with its color as "#rrggbb", or "".
type Label struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// Reaction counts one kind of reaction to the description, by GitHub's name for it.
type Reaction struct {
	Content string `json:"content"`
	Count   int    `json:"count"`
}

const (
	// hunkLines is how many of a diff hunk's last lines a line comment keeps, and
	// hunkLineBytes how long each is kept: a minified file's line runs to megabytes.
	hunkLines     = 4
	hunkLineBytes = 400
)

// kinds names each timeline type the sheets read, by GitHub's name for it.
var kinds = map[string]Kind{ //nolint:gochecknoglobals // a fixed table, read only
	github.TimelineComment: KindComment, github.TimelineCrossReferenced: KindMentioned,
	github.TimelineConnected: KindLinked, github.TimelineDisconnected: KindUnlinked,
	github.TimelineClosed: KindClosed, github.TimelineReopened: KindReopened,
	github.TimelineAssigned: KindAssigned, github.TimelineUnassigned: KindUnassigned,
	github.TimelineLabeled: KindLabeled, github.TimelineUnlabeled: KindUnlabeled,
	github.TimelineRenamed: KindRenamed, github.TimelineMarkedAsDuplicate: KindDuplicate,
	github.TimelineReview: KindReviewed, github.TimelineMerged: KindMerged,
	github.TimelineForcePushed: KindForcePushed, github.TimelineReviewRequested: KindReviewRequested,
	github.TimelineReadyForReview: KindReadyForReview, github.TimelineConvertedToDraft: KindConvertedToDraft,
}

// KindOf names a timeline item by its GitHub type; ok is false for one the sheets don't
// read.
func KindOf(githubType string) (Kind, bool) {
	kind, ok := kinds[githubType]

	return kind, ok
}

// Entries converts a page of a timeline, the viewer's own entries marked, skipping the
// types the sheets don't read. It never returns nil, nor an entry with a nil list.
func Entries(items []github.TimelineItem, viewer string) []Entry {
	out := make([]Entry, 0, len(items))

	for i := range items {
		g := &items[i]

		kind, ok := KindOf(g.Type)
		if !ok {
			continue
		}

		e := Entry{
			Kind: kind, ID: g.ID, At: stamp(g.CreatedAt), Actor: g.Actor, ActorIsBot: g.ActorType == "Bot",
			Mine: g.Mine || isViewer(g.Actor, viewer), Body: g.Body, URL: g.URL,
			Association: g.Association, EditedAt: stamp(g.EditedAt), Minimized: g.Minimized,
			MinimizedReason: g.MinimizedReason, Ref: RefOf(g.Ref), WillClose: g.WillClose, Commit: g.Commit,
			DuplicateOf: RefOf(g.DuplicateOf), StateReason: StateReason(g.StateReason), Label: LabelOf(g.Label),
			Assignee: g.Assignee, From: g.PreviousTitle, To: g.CurrentTitle, ReviewState: ReviewState(g.ReviewState),
			Outdated: g.Outdated, LineComments: lineComments(g.LineComments),
			Reviewer: g.Reviewer, Branch: g.Branch,
		}

		e.MoreLineComments = max(g.LineCommentCount-len(e.LineComments), 0)

		if kind == KindForcePushed {
			e.From, e.To = g.BeforeCommit, g.AfterCommit
		}

		out = append(out, e)
	}

	return out
}

// NonNil returns entries, never nil, each with its lists never nil: what an older build's
// cache holds lacks the lists this build added.
func NonNil(entries []Entry) []Entry {
	out := make([]Entry, len(entries))

	for i, e := range entries {
		e.LineComments = textutil.NonNil(e.LineComments)

		for j := range e.LineComments {
			e.LineComments[j].Hunk = textutil.NonNil(e.LineComments[j].Hunk)
		}

		out[i] = e
	}

	return out
}

// lineComments converts a review's line comments, never returning nil.
func lineComments(comments []github.ReviewLineComment) []LineComment {
	out := make([]LineComment, 0, len(comments))

	for i := range comments {
		c := &comments[i]
		out = append(out, LineComment{
			ID: c.ID, URL: c.URL, At: stamp(c.CreatedAt), Path: c.Path, Line: c.Line, StartLine: c.StartLine,
			Outdated: c.Outdated, Body: c.Body, Hunk: HunkTail(c.DiffHunk),
		})
	}

	return out
}

// HunkTail is the last few lines of a diff hunk (hunkLines), the hunk's header left out and
// each line cut to hunkLineBytes, never nil: the lines a line comment is on, as the diff
// shows them.
func HunkTail(hunk string) []string {
	lines := strings.Split(strings.TrimRight(hunk, "\n"), "\n")
	if len(lines) > 0 && strings.HasPrefix(lines[0], "@@") {
		lines = lines[1:]
	}

	lines = lines[max(len(lines)-hunkLines, 0):]
	out := make([]string, 0, len(lines))

	for _, l := range lines {
		cut, _ := cutText(strings.TrimRight(l, "\r"), hunkLineBytes)
		out = append(out, cut)
	}

	return out
}

// RefOf converts a pull request or an issue GitHub named, nil for none.
func RefOf(r *github.ItemRef) *Ref {
	if r == nil {
		return nil
	}

	kind := item.KindIssue
	if r.IsPR {
		kind = item.KindPR
	}

	return &Ref{
		Kind: kind, Repo: r.Repo, Number: r.Number, Title: r.Title, URL: r.URL, State: RefState(r.State),
		StateReason: StateReason(r.StateReason), Draft: r.IsDraft,
	}
}

// RefsOf converts the pull requests and issues GitHub named, never returning nil.
func RefsOf(refs []github.ItemRef) []Ref {
	out := make([]Ref, 0, len(refs))
	for i := range refs {
		out = append(out, *RefOf(&refs[i]))
	}

	return out
}

// LabelOf converts a label an event names, nil for none.
func LabelOf(l *github.Label) *Label {
	if l == nil {
		return nil
	}

	return &Label{Name: l.Name, Color: l.Color}
}

// ReactionsOf converts a description's reactions, never returning nil.
func ReactionsOf(rs []github.Reaction) []Reaction {
	out := make([]Reaction, 0, len(rs))
	for _, r := range rs {
		out = append(out, Reaction{Content: r.Content, Count: r.Count})
	}

	return out
}

// Cut cuts the description, body, and every text of entries, each comment's and review's
// and each line comment's, longer than limit bytes, marking each one cut (trimmed for the
// description). It returns the entries cut, a copy, leaving the ones given as they were.
// Each text cut keeps its page, where the whole text is; no entry is dropped.
func Cut(body *string, trimmed *bool, entries []Entry, limit int) []Entry {
	if text, cut := cutText(*body, limit); cut {
		*body, *trimmed = text, true
	}

	out := make([]Entry, len(entries))
	for i, e := range entries {
		out[i] = cutEntry(e, limit)
	}

	return out
}

// cutEntry is e with its text and its line comments' texts cut to limit bytes, each one cut
// marked, its line comments a copy.
func cutEntry(e Entry, limit int) Entry {
	if text, cut := cutText(e.Body, limit); cut {
		e.Body, e.Trimmed = text, true
	}

	if len(e.LineComments) == 0 {
		return e
	}

	comments := make([]LineComment, len(e.LineComments))
	copy(comments, e.LineComments)

	for j := range comments {
		if text, cut := cutText(comments[j].Body, limit); cut {
			comments[j].Body, comments[j].Trimmed = text, true
		}
	}

	e.LineComments = comments

	return e
}

// cutText cuts s to at most limit bytes, on a rune boundary, and reports whether it did.
func cutText(s string, limit int) (string, bool) {
	if len(s) <= limit {
		return s, false
	}

	end := max(limit, 0)
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}

	return s[:end], true
}

// stamp writes t as every time the window reads is written (clock.Format), or "" for the
// zero time.
func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	return clock.Format(t)
}

// isViewer reports that login is the signed-in account's, viewer.
func isViewer(login, viewer string) bool {
	return login != "" && strings.EqualFold(login, viewer)
}
