package issues

import (
	"cmp"
	"strconv"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/timeline"
)

// viewOf converts what GitHub read of an issue into the view the sheet keeps, the people
// in it told apart from viewer, the signed-in account.
func viewOf(v *github.IssueView, viewer string) cachedView {
	it := v.Update.Item
	view := cachedView{
		UpdatedAt: stamp(it.UpdatedAt), State: timeline.RefState(it.State), StateReason: timeline.StateReason(v.StateReason),
		ClosedAt: stamp(v.ClosedAt), Body: v.Body, EditedAt: stamp(v.LastEditedAt), Locked: v.Locked,
		LockReason: v.LockReason, CanClose: v.ViewerCanClose, CanUpdate: v.ViewerCanUpdate, Mine: v.ViewerDidAuthor,
		Parent: timeline.RefOf(v.Parent), DuplicateOf: timeline.RefOf(v.DuplicateOf),
		Assignees: textutil.NonNil(v.Assignees.Logins), AssigneesTotal: v.Assignees.Total,
		Participants: textutil.NonNil(v.Participants.Logins), ParticipantsTotal: v.Participants.Total,
		SubIssues: timeline.RefsOf(v.SubIssues), Fixes: fixesOf(v.Fixes), FixCount: v.FixCount,
		Reactions: timeline.ReactionsOf(v.Reactions), Comments: v.Update.Details.Comments,
		Timeline: timeline.Entries(v.Timeline.Items, viewer), Total: v.Timeline.Total, Before: v.Timeline.Before,
	}

	if v.Type.Name != "" {
		view.Type = &IssueType{Name: v.Type.Name, Color: v.Type.Color, Description: v.Type.Description}
	}

	if m := v.Milestone; m != nil {
		view.Milestone = &Milestone{Title: m.Title, URL: m.URL, DueOn: stamp(m.DueOn), State: m.State, Progress: m.Progress}
	}

	if c := v.LatestComment; c != nil {
		view.Latest, view.LatestAt, view.LatestMine = c.ID, stamp(c.CreatedAt), c.Mine || isViewer(c.Author, viewer)
	}

	view.ClosedBy = closedBy(view.Timeline)

	return view
}

// stamp writes t as every time the window reads is written (clock.Format), or "" for the
// zero time.
func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	return clock.Format(t)
}

// refText names a linked item as "owner/name#number".
func refText(l *timeline.Ref) string {
	return l.Repo + "#" + strconv.Itoa(l.Number)
}

// fixesOf converts the pull requests that close the issue.
func fixesOf(pulls []github.LinkedPull) []Fix {
	out := make([]Fix, 0, len(pulls))
	for i := range pulls {
		p := &pulls[i]
		out = append(out, Fix{
			PR: *timeline.RefOf(&p.Ref), Base: p.Base, MergedAt: stamp(p.MergedAt), Checks: checksWord(p.Checks),
			Review: reviewWord(p.ReviewDecision),
		})
	}

	return out
}

// checksWord words a pull request's overall checks result.
func checksWord(state string) ChecksWord {
	switch strings.ToUpper(state) {
	case "SUCCESS":
		return ChecksPassing
	case "FAILURE", "ERROR":
		return ChecksFailing
	case "PENDING", "EXPECTED":
		return ChecksRunning
	default:
		return ChecksNone
	}
}

// reviewWord words a pull request's review decision.
func reviewWord(decision string) ReviewWord {
	switch strings.ToUpper(decision) {
	case "APPROVED":
		return ReviewApproved
	case "CHANGES_REQUESTED":
		return ReviewChangesRequested
	case "REVIEW_REQUIRED":
		return ReviewRequired
	default:
		return ReviewNone
	}
}

// closedBy names what closed the issue, from the newest close in the timeline: the pull
// request or issue that did, else who; "" when the page holds no close.
func closedBy(items []timeline.Entry) string {
	for i := len(items) - 1; i >= 0; i-- {
		it := &items[i]
		if it.Kind != timeline.KindClosed {
			continue
		}

		if it.Ref != nil {
			return refText(it.Ref)
		}

		return it.Actor
	}

	return ""
}

// isViewer reports that login is the signed-in account's, viewer.
func isViewer(login, viewer string) bool {
	return login != "" && strings.EqualFold(login, viewer)
}

// build makes the sheet from the stored issue and its cache, nil when the sheet never read
// it. lastSeenAt is GitHub's time of the read the user last opened it on, and gone says
// GitHub can't find the issue any more.
func build(card StoredIssue, c *cached, lastSeenAt string, gone bool) Issue {
	is := Issue{
		ID: card.ID, Repo: card.ref().Repo.String(), Number: card.Number, Title: card.Title, URL: card.URL,
		State: timeline.StateOpen, Author: card.AuthorLogin, IsBot: strings.EqualFold(card.AuthorType, "Bot"),
		Mine: isViewer(card.AuthorLogin, card.Viewer), OpenedAt: stamp(card.CreatedAt), UpdatedAt: stamp(card.UpdatedAt),
		Assigned: card.Assigned, Reactions: []timeline.Reaction{}, Labels: labelsOf(card.Labels),
		Projects: projectsOf(card.Projects), Assignees: []Person{}, Participants: []Person{}, SubIssues: []timeline.Ref{},
		SubIssuesTotal: card.SubIssues.Total, SubIssuesDone: card.SubIssues.Completed, Fixes: []Fix{},
		Mentioned: []timeline.Ref{}, Timeline: timeline.Page{Items: []timeline.Entry{}}, LastSeenAt: lastSeenAt,
		Seen: Seen{Comments: card.Comments}, Stale: true,
	}

	if c != nil {
		is.fill(card, c)
	}

	plan := Decide(factsOf(card, c, gone))
	is.Standing, is.Offers, is.Primary = plan.Standing, textutil.NonNil(plan.Offers), plan.Primary
	is.CanEdit = canEdit(card, c) && !gone

	return is
}

// fill adds what the cache holds to is.
func (is *Issue) fill(card StoredIssue, c *cached) {
	v := &c.view
	is.State, is.StateReason, is.ClosedAt = cmp.Or(v.State, timeline.StateOpen), v.StateReason, v.ClosedAt
	is.UpdatedAt, is.Mine = cmp.Or(v.UpdatedAt, is.UpdatedAt), is.Mine || v.Mine
	is.Locked, is.LockReason = v.Locked, v.LockReason
	is.Body, is.BodyTrimmed, is.EditedAt, is.Reactions = v.Body, v.BodyTrimmed, v.EditedAt, textutil.NonNil(v.Reactions)
	is.Type, is.Milestone, is.Parent, is.DuplicateOf = v.Type, v.Milestone, v.Parent, v.DuplicateOf
	is.Assignees, is.MoreAssignees = people(v.Assignees, v.AssigneesTotal, card.Viewer)
	is.Participants, is.MoreParticipants = people(v.Participants, v.ParticipantsTotal, card.Viewer)
	is.SubIssues, is.Fixes = textutil.NonNil(v.SubIssues), textutil.NonNil(v.Fixes)
	is.MoreFixes, is.Mentioned = max(v.FixCount-len(v.Fixes), 0), mentioned(v.Timeline, v.Fixes)
	is.Timeline = timeline.Page{Items: timeline.NonNil(v.Timeline), Total: max(v.Total, len(v.Timeline)), Before: v.Before}
	is.Seen, is.Stale = Seen{Comments: v.Comments, Latest: v.Latest, LatestAt: v.LatestAt}, stale(card, c)

	if !c.fetchedAt.IsZero() {
		is.FetchedAt = clock.Format(c.fetchedAt)
	}
}

// stale reports a read due for the cache c of card: the sheet never read the issue, or an
// action put the read out of date, or the inbox card shows activity since it (a later
// update, or more comments).
func stale(card StoredIssue, c *cached) bool {
	if c == nil || c.fetchedAt.IsZero() {
		return true
	}

	at, ok := parseStamp(c.view.UpdatedAt)

	return !ok || card.UpdatedAt.Truncate(time.Second).After(at) || card.Comments > c.view.Comments
}

// parseStamp reads a time stamp wrote; ok is false for "" and for one it can't read.
func parseStamp(s string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, s)

	return t, err == nil
}

// people are the logins of assignees or participants, the user's marked, and how many more
// of total there are.
func people(logins []string, total int, viewer string) ([]Person, int) {
	out := make([]Person, 0, len(logins))
	for _, login := range logins {
		out = append(out, Person{Login: login, Mine: isViewer(login, viewer)})
	}

	return out, max(total-len(out), 0)
}

// mentioned lists the pull requests and issues the timeline says mentioned the issue, each
// once, in the timeline's order, leaving out the fixes, which the sheet lists on their own.
func mentioned(items []timeline.Entry, fixes []Fix) []timeline.Ref {
	listed := make(map[string]bool, len(fixes))
	for i := range fixes {
		listed[linkKey(&fixes[i].PR)] = true
	}

	out := []timeline.Ref{}

	for i := range items {
		it := &items[i]
		if it.Kind != timeline.KindMentioned || it.Ref == nil || listed[linkKey(it.Ref)] {
			continue
		}

		listed[linkKey(it.Ref)] = true
		out = append(out, *it.Ref)
	}

	return out
}

// linkKey is a linked item's case-folded "owner/name#number", for telling items apart.
func linkKey(l *timeline.Ref) string {
	return strings.ToLower(refText(l))
}

// labelsOf converts the card's labels, never returning nil.
func labelsOf(labels []github.Label) []timeline.Label {
	out := make([]timeline.Label, 0, len(labels))
	for _, l := range labels {
		out = append(out, timeline.Label{Name: l.Name, Color: l.Color})
	}

	return out
}

// projectsOf converts the card's projects, never returning nil.
func projectsOf(projects []github.ProjectItem) []Project {
	out := make([]Project, 0, len(projects))

	for i := range projects {
		p := &projects[i]
		fields := make([]ProjectField, 0, len(p.Fields))

		for _, f := range p.Fields {
			fields = append(fields, ProjectField{Name: f.Name, Value: f.Value, Color: f.Color})
		}

		out = append(out, Project{Title: p.Title, URL: p.URL, Status: p.Status, StatusColor: p.StatusColor, Fields: fields})
	}

	return out
}

// factsOf gathers what Decide needs from the stored issue and its cache, nil when the sheet
// never read it; gone says GitHub can't find the issue any more.
func factsOf(card StoredIssue, c *cached, gone bool) Facts {
	f := Facts{
		Gone: gone, CanWrite: github.CanWrite(card.ViewerPermission), Assigned: card.Assigned, OpenedAt: stamp(card.CreatedAt),
	}

	if c == nil {
		return f
	}

	v := &c.view
	f.Read, f.Closed, f.StateReason, f.ClosedBy, f.ClosedAt = true, v.State == timeline.StateClosed, v.StateReason, v.ClosedBy,
		v.ClosedAt
	f.Locked, f.LockReason, f.CanClose = v.Locked, v.LockReason, v.CanClose
	f.Fixes, f.MoreFixes = fixFacts(v.Fixes), max(v.FixCount-len(v.Fixes), 0)

	if v.DuplicateOf != nil {
		f.DuplicateOf = refText(v.DuplicateOf)
	}

	if v.LatestMine {
		f.RepliedLast, f.RepliedAt = true, v.LatestAt
	}

	return f
}

// fixFacts is what Decide needs of each pull request that closes the issue.
func fixFacts(fixes []Fix) []FixFacts {
	out := make([]FixFacts, 0, len(fixes))

	for i := range fixes {
		fx := &fixes[i]
		out = append(out, FixFacts{
			Ref: refText(&fx.PR), Base: fx.Base, Merged: fx.PR.State == timeline.StateMerged, MergedAt: fx.MergedAt,
			Open: fx.PR.State == timeline.StateOpen, Draft: fx.PR.Draft, Checks: fx.Checks,
		})
	}

	return out
}

// cardOf is the stored issue as the read u has it: the inbox takes a closed issue out of
// the lists without rewriting its card, so the sheet shows the read's own title, labels,
// and counts beside the read.
func cardOf(card StoredIssue, u *github.ItemUpdate) StoredIssue {
	it, d := &u.Item, &u.Details
	card.Title, card.URL, card.UpdatedAt = it.Title, it.URL, it.UpdatedAt
	card.AuthorLogin, card.AuthorType, card.AuthorAssociation = it.AuthorLogin, it.AuthorType, it.AuthorAssociation
	card.Labels, card.Comments, card.SubIssues, card.Assigned = textutil.NonNil(d.Labels), d.Comments, d.SubIssues, false

	return card
}

// canEdit reports that the user may change the issue's labels and assignees: one the sheet
// has read, so its assignees are known, still open in that read, with triage access or more
// to its repository.
func canEdit(card StoredIssue, c *cached) bool {
	return c != nil && cmp.Or(c.view.State, timeline.StateOpen) == timeline.StateOpen && github.CanTriage(card.ViewerPermission)
}
