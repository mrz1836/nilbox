package facts

import (
	"slices"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/trust"
)

// Caps on the texts Build keeps, as a prompt's data caps them.
const (
	maxTitleRunes = 200
	maxNameRunes  = 200
	maxPathRunes  = 1000
)

// The reasons a value isn't known.
const (
	whyDetails    = "its details aren't read yet"
	whyAuthorType = "the author's type isn't read yet"
	whyRepo       = "the repository isn't read yet"
	whyNotUpdate  = "the title doesn't read as a dependency update"
	whyIssue      = "it's an issue"
)

// Build makes an item's facts from its row.
func Build(r Row, env Env) Facts {
	b := &builder{row: r, env: env, values: map[catalog.FieldID]Value{}}

	b.item()
	b.repository()

	f := Facts{
		Item: Item{
			ID: r.ID, Kind: r.Kind, Repo: r.Repo.String(), Number: r.Number, Title: line(r.Title, maxTitleRunes),
			URL: r.URL,
		},
		Head:        NotApplicable(whyIssue),
		ChecksDone:  NotApplicable(whyIssue),
		ReviewAsked: moment(r.ReviewRequestedAt, "the inbox doesn't say when your review was asked for"),
		Assigned:    moment(r.AssignedAt, "the inbox doesn't say when it was assigned to you"),
		Updated:     r.UpdatedAt,
	}

	if r.Kind == item.KindPR {
		b.pullRequest()
		b.checks()
		b.files()
		b.dependency()

		f.Head, f.ChecksDone = b.head, b.done
	}

	for _, field := range catalog.Fields() {
		if !field.Applies(r.Kind) {
			b.values[field.ID] = NotApplicable("it's " + articled(r.Kind))
		}
	}

	f.Values = b.values

	return f
}

// builder fills one item's values.
type builder struct {
	row    Row
	env    Env
	values map[catalog.FieldID]Value

	// head and done are the pull request's head commit and when its checks finished.
	head, done Value
}

// set records a field's value.
func (b *builder) set(id catalog.FieldID, v Value) {
	b.values[id] = v
}

// line makes s one line of plain text, cut to n characters.
func line(s string, n int) string {
	return textutil.Clip(textutil.OneLine(s), n)
}

// articled names a kind with its article: "a pull request", "an issue".
func articled(k item.Kind) string {
	if k == item.KindIssue {
		return "an issue"
	}

	return "a pull request"
}

// moment returns a known moment, or unknown with why for nil.
func moment(t *time.Time, why string) Value {
	if t == nil {
		return Unknown(why, NeedNothing)
	}

	return Since(*t)
}

// item fills the item's own fields.
func (b *builder) item() {
	r := b.row

	b.set(catalog.FieldKind, Text(string(r.Kind)))
	b.set(catalog.FieldTitle, Text(line(r.Title, maxTitleRunes)))
	b.set(catalog.FieldAuthor, Text(line(r.Author, maxNameRunes)))
	b.set(catalog.FieldAge, Since(r.CreatedAt))
	b.set(catalog.FieldIdle, Since(r.UpdatedAt))
	b.set(catalog.FieldLists, List(b.lists(), false))
	b.set(catalog.FieldSnoozed, Bool(r.SnoozedUntil != nil && r.SnoozedUntil.After(b.env.Now)))
	b.set(catalog.FieldDismissed, Bool(r.DismissedAt != nil))
	b.set(catalog.FieldPinned, Bool(r.PinnedAt != nil))
	b.set(catalog.FieldTrust, b.trust())

	if r.AuthorType == "" {
		b.set(catalog.FieldAuthorIsBot, Unknown(whyAuthorType, NeedNothing))
	} else {
		b.set(catalog.FieldAuthorIsBot, Bool(r.AuthorType == trust.TypeBot))
	}

	if a := strings.ToUpper(strings.TrimSpace(r.Association)); a != "" {
		b.set(catalog.FieldAssociation, Text(a))
	} else {
		b.set(catalog.FieldAssociation, Unknown("the author's association isn't read yet", NeedNothing))
	}

	if b.env.Viewer == "" {
		b.set(catalog.FieldMine, Unknown("no account is signed in", NeedNothing))
	} else {
		b.set(catalog.FieldMine, Bool(strings.EqualFold(r.Author, b.env.Viewer)))
	}

	b.detailsOr(catalog.FieldLabels, func(d *DetailsRow) Value { return List(labels(d.Labels), false) })
	b.detailsOr(catalog.FieldComments, func(d *DetailsRow) Value { return Number(d.Comments) })
}

// lists returns the inbox lists the item is in.
func (b *builder) lists() []string {
	out := make([]string, 0, 3)

	for _, l := range []struct {
		in   bool
		name string
	}{
		{b.row.InReviewRequested, catalog.ListReviewRequested},
		{b.row.InAuthored, catalog.ListAuthored},
		{b.row.InAssigned, catalog.ListAssigned},
	} {
		if l.in {
			out = append(out, l.name)
		}
	}

	return out
}

// trust classifies the author. One who isn't trusted by association, and whose type
// isn't read yet, may still be a trusted bot, so isn't known.
func (b *builder) trust() Value {
	r := b.row
	isPR := r.Kind == item.KindPR
	class := b.env.Trust.Classify(trust.Author{Login: r.Author, Type: r.AuthorType, Association: r.Association},
		isPR, r.CrossRepository)

	if class == trust.ClassOutside && r.AuthorType == "" && (!isPR || !r.CrossRepository) {
		return Unknown(whyAuthorType+", so whether it's a trusted bot isn't known", NeedNothing)
	}

	return Text(string(class))
}

// labels returns the labels, each one line, without repeats ignoring case.
func labels(names []string) []string {
	out := make([]string, 0, len(names))

	for _, n := range names {
		n = line(n, maxNameRunes)
		if n != "" && !slices.ContainsFunc(out, func(o string) bool { return strings.EqualFold(o, n) }) {
			out = append(out, n)
		}
	}

	return out
}

// detailsOr sets a field from the details, or unknown until they are read.
func (b *builder) detailsOr(id catalog.FieldID, value func(*DetailsRow) Value) {
	if b.row.Details == nil {
		b.set(id, Unknown(whyDetails, NeedDetails))

		return
	}

	b.set(id, value(b.row.Details))
}

// repository fills the repository's fields.
func (b *builder) repository() {
	r := b.row
	rr := r.Repository

	b.set(catalog.FieldRepoOwner, Text(r.Repo.Owner))
	b.set(catalog.FieldRepoName, Text(r.Repo.Repo))
	b.set(catalog.FieldRepoFullName, Text(r.Repo.String()))
	b.set(catalog.FieldRepoMuted, Bool(rr.Muted))
	b.set(catalog.FieldRepoPinned, Bool(rr.Pinned))

	b.repoRead(catalog.FieldVisibility, func() Value { return lowerOr(rr.Visibility, whyRepo) })
	b.repoRead(catalog.FieldPermission, func() Value { return lowerOr(rr.Permission, whyRepo) })
	b.repoRead(catalog.FieldFork, func() Value { return Bool(rr.Fork) })
	b.repoRead(catalog.FieldUpdateBranch, func() Value { return Bool(rr.AllowUpdateBranch) })

	if rr.Language != nil {
		b.set(catalog.FieldLanguage, Text(line(*rr.Language, maxNameRunes)))
	} else {
		b.set(catalog.FieldLanguage, Unknown("the repository's language isn't read yet", NeedNothing))
	}

	if rr.HasClone != nil {
		b.set(catalog.FieldHasClone, Bool(*rr.HasClone))
	} else {
		b.set(catalog.FieldHasClone, Unknown("whether you have a clone isn't known yet", NeedNothing))
	}
}

// repoRead sets a field from the repository's facts, or unknown until they are read.
func (b *builder) repoRead(id catalog.FieldID, value func() Value) {
	if !b.row.Repository.Read {
		b.set(id, Unknown(whyRepo, NeedNothing))

		return
	}

	b.set(id, value())
}

// lowerOr returns s lower-cased, or unknown with why when it is "".
func lowerOr(s, why string) Value {
	if s = strings.ToLower(strings.TrimSpace(s)); s == "" {
		return Unknown(why, NeedNothing)
	}

	return Text(s)
}

// pullRequest fills a pull request's own fields.
func (b *builder) pullRequest() {
	r := b.row

	b.set(catalog.FieldDraft, Bool(r.Draft))
	b.set(catalog.FieldHead, Text(line(r.HeadRef, maxNameRunes)))
	b.set(catalog.FieldFromFork, Bool(r.CrossRepository))

	b.head = Unknown(whyDetails, NeedDetails)
	if r.Details != nil && r.Details.HeadOID != "" {
		b.head = Text(r.Details.HeadOID)
	}

	for id, value := range detailFields(b.env.Viewer, r.InReviewRequested) {
		b.detailsOr(id, value)
	}
}

// detailFields are the fields read from a pull request's details.
func detailFields(viewer string, inReviewList bool) map[catalog.FieldID]func(*DetailsRow) Value {
	return map[catalog.FieldID]func(*DetailsRow) Value{
		catalog.FieldBase: func(d *DetailsRow) Value {
			return textOr(line(d.BaseRef, maxNameRunes), "the base branch isn't read yet")
		},
		catalog.FieldSize:             func(d *DetailsRow) Value { return Text(catalog.SizeOf(d.Additions + d.Deletions)) },
		catalog.FieldLines:            func(d *DetailsRow) Value { return Number(d.Additions + d.Deletions) },
		catalog.FieldFiles:            func(d *DetailsRow) Value { return Number(d.ChangedFiles) },
		catalog.FieldCommits:          func(d *DetailsRow) Value { return Number(d.Commits) },
		catalog.FieldConflicts:        conflicts,
		catalog.FieldBehind:           behind,
		catalog.FieldReviewDecision:   reviewDecision,
		catalog.FieldApprovals:        func(d *DetailsRow) Value { return Number(d.Approvals) },
		catalog.FieldChangesRequested: func(d *DetailsRow) Value { return Number(d.ChangesRequested) },
		catalog.FieldAutoMerge:        func(d *DetailsRow) Value { return Bool(d.AutoMerge) },
		catalog.FieldAsksMe: func(d *DetailsRow) Value {
			asked := slices.ContainsFunc(d.ReviewUsers, func(u string) bool { return strings.EqualFold(u, viewer) })

			return Bool(inReviewList || (viewer != "" && asked))
		},
		catalog.FieldChecksState:      checksState,
		catalog.FieldChecksFailed:     func(d *DetailsRow) Value { return Number(d.ChecksFailed) },
		catalog.FieldChecksPending:    func(d *DetailsRow) Value { return Number(d.ChecksPending) },
		catalog.FieldChecksPassed:     func(d *DetailsRow) Value { return Number(d.ChecksPassed) },
		catalog.FieldAwaitingApproval: func(d *DetailsRow) Value { return Bool(d.ChecksAwaiting > 0) },
	}
}

// textOr returns s, or unknown with why when it is "".
func textOr(s, why string) Value {
	if s == "" {
		return Unknown(why, NeedDetails)
	}

	return Text(s)
}

// conflicts reads whether the pull request conflicts with its base: unknown while GitHub
// is still working it out.
func conflicts(d *DetailsRow) Value {
	switch strings.ToUpper(d.Mergeable) {
	case "CONFLICTING":
		return Bool(true)
	case "MERGEABLE":
		return Bool(false)
	default:
		return Unknown("GitHub hasn't worked out whether it conflicts yet", NeedDetails)
	}
}

// behind reads whether the pull request is behind its base, from GitHub's merge state.
func behind(d *DetailsRow) Value {
	switch s := strings.ToUpper(d.MergeState); s {
	case "", "UNKNOWN":
		return Unknown("GitHub hasn't said whether it's behind its base yet", NeedDetails)
	default:
		return Bool(s == "BEHIND")
	}
}

// reviewDecision reads the review decision as the catalog spells it.
func reviewDecision(d *DetailsRow) Value {
	choice, ok := map[string]string{
		"APPROVED": "approved", "CHANGES_REQUESTED": "changesRequested", "REVIEW_REQUIRED": "reviewRequired",
		"": catalog.ReviewNone,
	}[strings.ToUpper(d.ReviewDecision)]
	if !ok {
		return Text(strings.ToLower(d.ReviewDecision))
	}

	return Text(choice)
}

// checksState reads the head commit's overall result as the catalog spells it.
func checksState(d *DetailsRow) Value {
	if d.ChecksState == "" {
		return Text(catalog.ChecksNone)
	}

	return Text(strings.ToLower(d.ChecksState))
}
