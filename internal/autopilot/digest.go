package autopilot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/draft"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// A routine On a schedule whose style is a digest (catalog.ScheduleDigest) tells of what it
// matched at its time once, with the list: the Notify me steps its matches reach in one
// cycle, which its time reaches for them all at once (spec.Validate holds Notify me to the
// first step), are one notice, which counts them by where each stands, "5 pull requests
// need you: 3 failing, 2 conflicting", and lists them; each match's step is done once that
// notice is told. A match that comes later the same day, as an item new to the watch, is
// told in a digest of its own. A manual run tells of its item on its own, as any does.

// maxDigestListed caps the items a digest's body names; the rest are counted.
const maxDigestListed = 5

// quietAfter is how long an issue goes without activity before a digest calls it quiet.
const quietAfter = 7 * 24 * time.Hour

// Digest is the list a digest tells of: the items a scheduled routine matched at one time.
type Digest struct {
	// Items are the items, in the order the routine decided on them, each with where it
	// stands.
	Items []DigestItem

	// Groups count them by where each stands, the most pressing first.
	Groups []DigestGroup

	// Noun names them: "pull requests", "Dependabot pull requests", "issues", "items".
	Noun string

	// RoutineCreated is when the routine was made, which the notice opens it by.
	RoutineCreated time.Time
}

// DigestItem is one item of a digest.
type DigestItem struct {
	MatchID int64
	Item    ItemRef
	Group   string
}

// DigestGroup is a count of a digest's items that stand alike: "3 failing".
type DigestGroup struct {
	Label string
	Count int
}

// The groups a digest counts its items in, the most pressing first.
const (
	digestFailing   = "failing"
	digestConflicts = "conflicting"
	digestChanges   = "with changes requested"
	digestBehind    = "behind"
	digestRunning   = "with checks running"
	digestDraft     = "drafts"
	digestApproved  = "approved"
	digestWaiting   = "waiting for review"
	digestQuiet     = "quiet"
	digestOpen      = "open"
)

// digestOrder is the groups' order.
func digestOrder() []string {
	return []string{
		digestFailing, digestConflicts, digestChanges, digestBehind, digestRunning, digestDraft, digestApproved,
		digestWaiting, digestQuiet, digestOpen,
	}
}

// digestGroup says where an item stands, as a digest counts it: a pull request whose
// checks fail, then one in conflict, with changes requested, behind its base, with checks
// running, a draft, approved, else waiting for review; an issue quiet for a week or more,
// else open. A value not known yet passes to the next.
func digestGroup(f facts.Facts, now time.Time) string {
	if f.Item.Kind == item.KindIssue {
		if !f.Updated.IsZero() && now.Sub(f.Updated) >= quietAfter {
			return digestQuiet
		}

		return digestOpen
	}

	for _, r := range pullGroups() {
		if v := f.Value(r.field); v.Known() && (r.values == nil && v.Bool || slices.Contains(r.values, v.Text)) {
			return r.group
		}
	}

	return digestWaiting
}

// pullGroup is what puts a pull request in a digest's group: a yes-or-no field that is
// yes, or, with values, a choice field that holds one of them.
type pullGroup struct {
	field  catalog.FieldID
	values []string
	group  string
}

// pullGroups are a pull request's groups, the most pressing first.
func pullGroups() []pullGroup {
	return []pullGroup{
		{field: catalog.FieldChecksState, values: []string{"failure", "error"}, group: digestFailing},
		{field: catalog.FieldConflicts, group: digestConflicts},
		{field: catalog.FieldReviewDecision, values: []string{"changesRequested"}, group: digestChanges},
		{field: catalog.FieldBehind, group: digestBehind},
		{field: catalog.FieldChecksState, values: []string{"pending", "expected"}, group: digestRunning},
		{field: catalog.FieldDraft, group: digestDraft},
		{field: catalog.FieldReviewDecision, values: []string{"approved"}, group: digestApproved},
	}
}

// digestEntry is a match a digest tells of, with its item's facts.
type digestEntry struct {
	matchID int64
	item    ItemRef
	facts   facts.Facts
}

// digestOf gathers entries into a digest, as of now.
func digestOf(entries []digestEntry, now time.Time) Digest {
	out := Digest{Items: make([]DigestItem, 0, len(entries)), Groups: []DigestGroup{}}
	counts := map[string]int{}

	for _, e := range entries {
		g := digestGroup(e.facts, now)
		counts[g]++

		out.Items = append(out.Items, DigestItem{MatchID: e.matchID, Item: e.item, Group: g})
	}

	order := digestOrder()

	for _, g := range order {
		if counts[g] > 0 {
			out.Groups = append(out.Groups, DigestGroup{Label: g, Count: counts[g]})
		}
	}

	// The items are listed as the counts are, the most pressing first, each group in the
	// order the routine decided on them.
	slices.SortStableFunc(out.Items, func(a, b DigestItem) int {
		return slices.Index(order, a.Group) - slices.Index(order, b.Group)
	})

	out.Noun = digestNoun(entries)

	return out
}

// digestNoun names a digest's items, in the plural: by their kind, and, when one bot
// opened every one of them, by it.
func digestNoun(entries []digestEntry) string {
	kinds := map[item.Kind]bool{}

	for _, e := range entries {
		kinds[e.item.Kind] = true
	}

	switch {
	case len(kinds) != 1:
		return "items"
	case kinds[item.KindIssue]:
		return "issues"
	}

	if tool := oneBot(entries); tool != "" {
		return textutil.Capitalize(tool) + " pull requests"
	}

	return "pull requests"
}

// oneBot returns the dependency bot that opened every one of entries' pull requests, ""
// when none did, or more than one.
func oneBot(entries []digestEntry) string {
	tool := ""

	for _, e := range entries {
		v := e.facts.Value(catalog.FieldDepTool)
		if !v.Known() || v.Text == "" || v.Text == "none" || tool != "" && v.Text != tool {
			return ""
		}

		tool = v.Text
	}

	return tool
}

// Title says a digest's line: "5 pull requests need you", "1 issue needs you".
func (d Digest) Title() string {
	n := len(d.Items)
	if n == 1 {
		return "1 " + singular(d.Noun) + " needs you"
	}

	return strconv.Itoa(n) + " " + d.Noun + " need you"
}

// Body says what follows it: the counts, then the items, the first few by name, and the
// user's own line when the step has one. "3 failing, 2 conflicting: acme-org/web #88,
// acme-org/cli #19, acme-org/api #212, and 2 more."
func (d Digest) Body(note string) string {
	counts := make([]string, 0, len(d.Groups))
	for _, g := range d.Groups {
		counts = append(counts, strconv.Itoa(g.Count)+" "+g.Label)
	}

	names := make([]string, 0, maxDigestListed+1)
	for _, it := range d.Items[:min(len(d.Items), maxDigestListed)] {
		names = append(names, it.Item.Repo+" #"+strconv.Itoa(it.Item.Number))
	}

	if more := len(d.Items) - maxDigestListed; more > 0 {
		names = append(names, strconv.Itoa(more)+" more")
	}

	words := strings.Join(counts, ", ")
	if len(names) > 0 {
		words += ": " + draft.Series(names)
	}

	return joinSentences(words, note)
}

// singular names one of noun's items.
func singular(noun string) string {
	return strings.TrimSuffix(noun, "s")
}

// digestKey is the key of a digest's notice: its routine, when the routine was made, and
// the matches it tells of, so a digest told again after a quit is told once, and one of
// other matches, later the same day, is its own.
func digestKey(routineID int64, created time.Time, d Digest) string {
	ids := make([]string, 0, len(d.Items))
	for _, it := range d.Items {
		ids = append(ids, strconv.FormatInt(it.MatchID, 10))
	}

	sum := sha256.Sum256([]byte(strings.Join(ids, ",")))

	return "autopilot:digest:" + strconv.FormatInt(routineID, 10) + ":" + created.UTC().Format(time.RFC3339Nano) + ":" +
		hex.EncodeToString(sum[:8])
}

// digestNotice is the notice a digest job tells: an alert headed by the routine's name,
// about no one item, which opens the routine.
func digestNotice(job Job) Notice {
	d := *job.Digest

	out := Notice{
		Kind: NoticeAlert, RoutineID: job.RoutineID, Routine: job.RoutineName, CreatedAt: d.RoutineCreated,
		Title: d.Title(), Key: digestKey(job.RoutineID, d.RoutineCreated, d),
	}

	if job.Notices != nil {
		out.Banner = job.Notices.Alert
	}

	if n := job.Step.Notify; n != nil {
		out.Body = d.Body(n.Note)

		if n.Banner != nil {
			out.Banner = n.Banner
		}
	} else {
		out.Body = d.Body("")
	}

	return out
}

// digests takes out of jobs the Notify me steps a digest tells, by routine, and returns
// the rest, and the digests: one job per routine, carrying its digest, with the matches it
// tells of.
func (p *pass) digests(jobs []Job) ([]Job, []digestJob) {
	rest := make([]Job, 0, len(jobs))
	by := map[int64][]Job{}

	var order []int64

	for _, job := range jobs {
		r, ok := p.byID[job.RoutineID]
		if !ok || job.Manual || job.Step.Notify == nil || !r.spec.Trigger.Schedule.Digest() {
			rest = append(rest, job)

			continue
		}

		if _, seen := by[job.RoutineID]; !seen {
			order = append(order, job.RoutineID)
		}

		by[job.RoutineID] = append(by[job.RoutineID], job)
	}

	out := make([]digestJob, 0, len(order))

	for _, id := range order {
		group := by[id]
		entries := make([]digestEntry, 0, len(group))
		matches := make([]int64, 0, len(group))

		for _, job := range group {
			entries = append(entries, digestEntry{matchID: job.MatchID, item: job.Item, facts: p.facts[job.Item.ID]})
			matches = append(matches, job.MatchID)
		}

		d := digestOf(entries, p.now)
		d.RoutineCreated = p.byID[id].row.CreatedAt

		lead := group[0]
		lead.Digest = &d
		out = append(out, digestJob{job: lead, matches: matches})
	}

	return rest, out
}

// digestJob is a digest's notice to tell, and the matches whose Notify me it is.
type digestJob struct {
	job     Job
	matches []int64
}

// dispatchDigest tells a digest through the notices dispatcher on a worker, as one step of
// each of its matches, and records the answer on each, on the loop.
func (e *Engine) dispatchDigest(ctx context.Context, dj digestJob) {
	disp := e.dispatchers[catalog.DispatchNotices]

	for _, id := range dj.matches {
		e.inflight[id] = true
	}

	e.goWork(ctx, func(ctx context.Context) {
		var (
			res Result
			err = ErrOff
		)

		if e.Enabled() {
			res, err = disp.Dispatch(ctx, dj.job)
		}

		e.mail.post(func(ctx context.Context) {
			for _, id := range dj.matches {
				delete(e.inflight, id)
				e.applyDispatch(ctx, id, 0, res, err)
			}

			e.Wake()
		})
	})
}

// digestSummary says a digest's step was told, for each of its matches' records.
func digestSummary(d Digest) string {
	if len(d.Items) == 1 {
		return "Told you, in the routine's digest."
	}

	return fmt.Sprintf("Told you, in a digest of %d.", len(d.Items))
}
