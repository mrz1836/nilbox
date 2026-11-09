package review

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/github"
)

// The decision model is pure: Decide turns what is known about a pull request into the
// actions the sheet offers, which one its button runs, and how close the pull request is to
// merging. Act decides again, on the same facts, before it sends anything, and refuses an
// action that is not enabled, so the sheet can never ask for what the model would not
// offer. The rules that keep a merge safe:
//
//   - Nothing merges unless GitHub says it can now (CLEAN or HAS_HOOKS), or UNSTABLE after
//     the user accepts the failing checks: a merge never goes around branch protection on
//     its own. Only "merge past the rules" does, offered when GitHub lets the user (as it
//     does a repository admin), never as the sheet's button, and with every rule it skips
//     to accept first.
//   - Nothing merges a draft, a pull request on a merge queue, or without write access.
//   - Nobody approves or requests changes on their own pull request.
//   - A pending review on GitHub blocks every review.

// Facts are what Decide knows about a pull request.
type Facts struct {
	// Mine reports the user's own pull request.
	Mine bool

	Draft bool

	// PendingReview reports a review of the user's started on GitHub and not submitted.
	PendingReview bool

	// MergeState is GitHub's mergeStateStatus: CLEAN, HAS_HOOKS, UNSTABLE, BLOCKED,
	// BEHIND, DIRTY, or UNKNOWN; "" before the sheet read it.
	MergeState string

	// MergeQueue reports a base branch that merges through its merge queue.
	MergeQueue bool

	// CanWrite reports write access to the repository, or more.
	CanWrite bool

	// CanUpdateBranch and CanAutoMerge say whether GitHub lets the user update the branch
	// and turn on auto-merge, and CanBypass whether it lets them merge past the branch's
	// rules, as a repository admin can.
	CanUpdateBranch bool
	CanAutoMerge    bool
	CanBypass       bool

	// AutoMergeOn reports auto-merge already on.
	AutoMergeOn bool

	// Dependabot reports a pull request Dependabot opened, which it keeps up to date
	// itself when asked.
	Dependabot bool

	// ApprovedAtHead reports the user's own approval of the head commit.
	ApprovedAtHead bool

	// ReviewRequired reports that the repository still needs an approving review, and
	// ChangesRequested a request for changes standing.
	ReviewRequired   bool
	ChangesRequested bool

	// Checks count the head commit's checks: those required and failing or pending, and
	// any failing or pending.
	RequiredFailing int
	RequiredPending int
	Failing         int
	Pending         int

	// Sensitive names the sensitive categories of danger level the pull request changes,
	// as in "Workflows".
	Sensitive []string

	// Base is the branch it merges into.
	Base string

	// Methods are the merge methods the repository allows, and ViewerMethod the one
	// GitHub offers the user first.
	Methods      []MergeMethod
	ViewerMethod MergeMethod

	// Closed reports a pull request merged or closed since.
	Closed bool
}

// Plan is what Decide made of the facts.
type Plan struct {
	Offers    []Offer
	Primary   Action
	Readiness Readiness

	// Method is the merge method a merge uses unless the user picks another, and Methods
	// the ones the repository allows.
	Method  MergeMethod
	Methods []MergeMethod
}

// offer returns the plan's offer of action, and whether it has one.
func (p Plan) offer(action Action) (Offer, bool) {
	i := slices.IndexFunc(p.Offers, func(o Offer) bool { return o.Action == action })
	if i < 0 {
		return Offer{}, false
	}

	return p.Offers[i], true
}

// Decide works out the plan for a pull request from its facts and the user's merge method
// preference.
func Decide(f Facts, pref MethodPref) Plan {
	method := chooseMethod(f.Methods, pref, f.ViewerMethod)
	d := decider{f: f, method: method}

	p := Plan{Method: method, Methods: slices.Clone(f.Methods), Readiness: readiness(f)}
	if p.Methods == nil {
		p.Methods = []MergeMethod{}
	}

	if f.Closed {
		return p
	}

	p.Offers = d.offers()
	p.Primary = primary(f, p.Offers)

	return p
}

// decider builds the offers.
type decider struct {
	f      Facts
	method MergeMethod
}

// offers lists every action that applies, enabled or with the reason it isn't, in the
// order the sheet's menu lists them.
func (d decider) offers() []Offer {
	f := d.f
	out := []Offer{d.approveMerge(), d.approveAutoMerge(), d.merge(), d.autoMerge(), d.bypass()}
	out = append(out, d.review(ActionApprove), d.review(ActionRequestChanges), d.review(ActionComment))

	if f.MergeState == github.MergeStateBehind && !f.Dependabot {
		out = append(out, d.updateBranch())
	}

	if f.Dependabot && (f.MergeState == github.MergeStateBehind || f.MergeState == github.MergeStateDirty) {
		out = append(out, Offer{Action: ActionRebaseBot, Label: "Ask Dependabot to rebase", Enabled: true, Warnings: []string{}})
	}

	return slices.DeleteFunc(out, func(o Offer) bool { return o.Action == ActionNone })
}

// reviewBlock is why the user can't review now, or "".
func (d decider) reviewBlock(action Action) string {
	f := d.f

	switch {
	case f.PendingReview:
		return "You have a review of this pull request started on GitHub. Submit or discard it there first."
	case f.Mine && action != ActionComment:
		return "GitHub doesn't let you approve or request changes on your own pull request."
	default:
		return ""
	}
}

// review offers one of the three reviews.
func (d decider) review(action Action) Offer {
	o := Offer{Action: action, Label: reviewLabel(action), Enabled: true, Warnings: []string{}}

	if reason := d.reviewBlock(action); reason != "" {
		o.Enabled, o.Reason = false, reason
	} else if action == ActionApprove && d.f.ApprovedAtHead {
		o.Enabled, o.Reason = false, "You already approved this commit."
	}

	return o
}

// reviewLabel names a review.
func reviewLabel(action Action) string {
	if action == ActionRequestChanges {
		return "Request changes"
	}

	if action == ActionComment {
		return "Comment"
	}

	return "Approve"
}

// mergeBlock is why nothing can merge the pull request at all, or "": a draft, a merge
// queue, or no write access.
func (d decider) mergeBlock() string {
	f := d.f

	switch {
	case f.Draft:
		return "It's a draft."
	case f.MergeQueue:
		return fmt.Sprintf("%s merges through its merge queue; add it to the queue on GitHub.", base(f))
	case !f.CanWrite:
		return "Your account can't merge in this repository."
	case len(f.Methods) == 0:
		return "The repository allows no merge method."
	default:
		return ""
	}
}

// mergeableNow reports a merge state GitHub would merge now, UNSTABLE included.
func (d decider) mergeableNow() bool {
	return github.Mergeable(d.f.MergeState) || d.f.MergeState == github.MergeStateUnstable
}

// warnings are what the user must accept before a merge: failing checks that are not
// required, and sensitive files.
func (d decider) warnings(now bool) []string {
	out := []string{}

	if now && d.f.MergeState == github.MergeStateUnstable {
		out = append(out, unstableWarning(d.f))
	}

	if len(d.f.Sensitive) > 0 {
		out = append(out, "It changes sensitive files: "+strings.Join(d.f.Sensitive, ", ")+".")
	}

	return out
}

// merge offers a merge now.
func (d decider) merge() Offer {
	o := Offer{Action: ActionMerge, Label: mergeLabel(d.method), Confirm: true, Warnings: d.warnings(true)}

	switch reason := d.mergeBlock(); {
	case reason != "":
		o.Reason = reason
	case !d.mergeableNow():
		o.Reason = notNowReason(d.f)
	default:
		o.Enabled = true
	}

	return o
}

// approveMerge offers to approve and merge: when the pull request merges now, or only
// waits on the approval.
func (d decider) approveMerge() Offer {
	f := d.f
	o := Offer{
		Action: ActionApproveMerge, Label: "Approve & " + strings.ToLower(mergeLabel(d.method)), Confirm: true,
		Warnings: d.warnings(true),
	}

	waitsOnReview := f.MergeState == github.MergeStateBlocked && f.ReviewRequired && f.RequiredFailing == 0 &&
		f.RequiredPending == 0 && !f.ChangesRequested

	switch reason := d.firstOf(d.reviewBlock(ActionApprove), d.mergeBlock()); {
	case f.ApprovedAtHead:
		return Offer{}
	case reason != "":
		o.Reason = reason
	case d.mergeableNow() || waitsOnReview:
		o.Enabled = true
	default:
		o.Reason = notNowReason(f)
	}

	return o
}

// approveAutoMerge offers to approve and turn on auto-merge: when the pull request waits
// on requirements, such as required checks still running.
func (d decider) approveAutoMerge() Offer {
	f := d.f
	o := Offer{Action: ActionApproveAutoMerge, Label: "Approve & auto-merge", Confirm: true, Warnings: d.warnings(false)}

	switch reason := d.firstOf(d.reviewBlock(ActionApprove), d.mergeBlock(), d.autoMergeBlock()); {
	case f.ApprovedAtHead:
		return Offer{}
	case reason != "":
		o.Reason = reason
	default:
		o.Enabled = true
	}

	return o
}

// autoMerge offers to turn on auto-merge without a review: for the user's own pull
// request, one they approved, or one that needs no review.
func (d decider) autoMerge() Offer {
	f := d.f
	if !f.Mine && !f.ApprovedAtHead && f.ReviewRequired {
		return Offer{}
	}

	o := Offer{Action: ActionAutoMerge, Label: "Merge when ready", Confirm: true, Warnings: d.warnings(false)}

	// GitHub never lets the author approve, so on their own pull request auto-merge may wait
	// for good: the confirmation says so.
	if f.Mine && f.ReviewRequired {
		o.Warnings = append([]string{
			"It waits for an approval from someone else: GitHub doesn't let you approve your own pull request.",
		}, o.Warnings...)
	}

	if reason := d.firstOf(d.mergeBlock(), d.autoMergeBlock()); reason != "" {
		o.Reason = reason
	} else {
		o.Enabled = true
	}

	return o
}

// bypass offers to merge past the branch's rules, for a user GitHub lets do so, while the
// rules are what hold the merge back. It is never the sheet's button, and its warnings,
// to accept first, name each rule it skips.
func (d decider) bypass() Offer {
	f := d.f
	if !f.CanBypass || (f.MergeState != github.MergeStateBlocked && f.MergeState != github.MergeStateBehind) {
		return Offer{}
	}

	o := Offer{
		Action: ActionMergeBypass, Label: mergeLabel(d.method) + " past the rules", Confirm: true,
		Warnings: append(skipped(f), d.warnings(false)...),
	}

	if reason := d.mergeBlock(); reason != "" {
		o.Reason = reason
	} else {
		o.Enabled = true
	}

	return o
}

// skipped says what a merge past the rules goes around, one sentence each.
func skipped(f Facts) []string {
	out := []string{}

	switch {
	case f.ChangesRequested:
		out = append(out, "Changes were requested, and the request stands.")
	case f.ReviewRequired && f.Mine:
		out = append(out, "It needs an approval from someone else.")
	case f.ReviewRequired:
		out = append(out, "It needs an approval.")
	}

	if f.RequiredFailing > 0 {
		out = append(out, counted(f.RequiredFailing, "required check is failing.", "required checks are failing."))
	}

	if f.RequiredPending > 0 {
		out = append(out, "Required checks are still running.")
	}

	if f.MergeState == github.MergeStateBehind {
		out = append(out, "It's behind "+base(f)+", and the rules want it up to date.")
	}

	if len(out) == 0 {
		out = append(out, "Branch protection blocks it.")
	}

	return out
}

// autoMergeBlock is why auto-merge can't be turned on, or "".
func (d decider) autoMergeBlock() string {
	f := d.f

	switch {
	case f.AutoMergeOn:
		return "Auto-merge is already on."
	case !f.CanAutoMerge:
		return "Auto-merge isn't available for this pull request."
	case f.MergeState != github.MergeStateBlocked:
		return "Auto-merge waits on requirements, and this pull request has none left to wait on."
	default:
		return ""
	}
}

// updateBranch offers to merge the base branch in.
func (d decider) updateBranch() Offer {
	o := Offer{Action: ActionUpdateBranch, Label: "Update branch", Warnings: []string{}}

	if d.f.CanUpdateBranch {
		o.Enabled = true
	} else {
		o.Reason = "Your account can't update this branch."
	}

	return o
}

// firstOf returns the first of reasons that is not "".
func (d decider) firstOf(reasons ...string) string {
	for _, r := range reasons {
		if r != "" {
			return r
		}
	}

	return ""
}

// mergeLabel names a merge with method.
func mergeLabel(method MergeMethod) string {
	switch method {
	case MethodSquash:
		return "Squash-merge"
	case MethodRebase:
		return "Rebase-merge"
	case MethodMerge:
		return "Merge"
	default:
		return "Merge"
	}
}

// notNowReason says why GitHub would not merge now.
func notNowReason(f Facts) string {
	switch f.MergeState {
	case github.MergeStateBehind:
		return fmt.Sprintf("It's behind %s.", base(f))
	case github.MergeStateDirty:
		return fmt.Sprintf("It has conflicts with %s.", base(f))
	case github.MergeStateBlocked:
		if needs := needsOf(f); len(needs) > 0 {
			return "It still needs " + strings.Join(needs, ", ") + "."
		}

		return "Branch protection blocks it."
	default:
		return "GitHub is still working out whether it can merge."
	}
}

// primary picks the offer the sheet's button runs: exactly one enabled offer, or none
// when nothing is enabled.
func primary(f Facts, offers []Offer) Action {
	enabled := func(a Action) bool {
		return slices.ContainsFunc(offers, func(o Offer) bool { return o.Action == a && o.Enabled })
	}

	order := []Action{ActionRebaseBot}

	// A required check failing holds the merge back, so the button says something about it
	// instead. An optional one failing doesn't: GitHub merges, once the user accepts it.
	if f.RequiredFailing > 0 {
		order = append(order, ActionComment)
	}

	if f.ApprovedAtHead {
		order = append(order, ActionMerge, ActionAutoMerge)
	}

	order = append(order, ActionApproveMerge, ActionApproveAutoMerge, ActionUpdateBranch, ActionMerge,
		ActionAutoMerge, ActionApprove, ActionComment, ActionRequestChanges)

	for _, a := range order {
		if enabled(a) {
			return a
		}
	}

	return ActionNone
}

// chooseMethod picks the merge method: the user's choice where the repository allows it,
// else the one GitHub offers the user first, else squash, merge, then rebase, whichever the
// repository allows first. It is "" when the repository allows none.
func chooseMethod(allowed []MergeMethod, pref MethodPref, viewer MergeMethod) MergeMethod {
	if m := pref.method(); m != "" && slices.Contains(allowed, m) {
		return m
	}

	if slices.Contains(allowed, viewer) {
		return viewer
	}

	for _, m := range []MergeMethod{MethodSquash, MethodMerge, MethodRebase} {
		if slices.Contains(allowed, m) {
			return m
		}
	}

	return ""
}

// readiness says how close the pull request is to merging.
func readiness(f Facts) Readiness {
	needs := needsOf(f)
	r := Readiness{Needs: needs}

	switch {
	case f.Closed:
		r.State, r.Sentence = ReadyClosed, "No longer open"
	case f.Draft:
		r.State, r.Sentence = ReadyDraft, "Draft"
	case f.MergeQueue:
		r.State, r.Sentence = ReadyQueue, "Merges through the merge queue"
	case f.MergeState == github.MergeStateDirty:
		r.State, r.Sentence = ReadyConflicts, "Conflicts with "+base(f)
	case f.MergeState == github.MergeStateBehind:
		r.State, r.Sentence = ReadyBehind, "Behind "+base(f)
	case f.AutoMergeOn:
		r.State, r.Sentence = ReadyAutoMerge, "Auto-merge is on"+waitingOn(needs)
	case github.Mergeable(f.MergeState):
		r.State, r.Sentence = ReadyMerge, "Ready to merge"
	case f.MergeState == github.MergeStateUnstable:
		r.State, r.Sentence = ReadyUnstable, "Ready to merge · "+unstableNote(f)
	case f.MergeState == github.MergeStateBlocked:
		r.State, r.Sentence = ReadyBlocked, blockedSentence(f, needs)
	default:
		r.State, r.Sentence = ReadyUnknown, "Checking whether it can merge"
	}

	return r
}

// optionalFailing counts the failing checks that aren't required.
func optionalFailing(f Facts) int {
	return max(f.Failing-f.RequiredFailing, 0)
}

// unstableNote says what keeps a pull request GitHub would merge from passing, as in "1
// optional check failing", or, with none failing, that optional checks aren't passing.
func unstableNote(f Facts) string {
	if n := optionalFailing(f); n > 0 {
		return counted(n, "optional check failing", "optional checks failing")
	}

	return "optional checks not passing"
}

// unstableWarning is unstableNote as the warning a merge asks the user to accept.
func unstableWarning(f Facts) string {
	switch n := optionalFailing(f); n {
	case 0:
		return "Optional checks aren't passing."
	case 1:
		return "1 optional check is failing."
	default:
		return fmt.Sprintf("%d optional checks are failing.", n)
	}
}

// blockedSentence says what a blocked pull request waits on.
func blockedSentence(f Facts, needs []string) string {
	if len(needs) == 0 {
		return "Blocked by branch protection"
	}

	s := upperFirst(strings.Join(needs, " · "))
	if f.CanAutoMerge && !f.AutoMergeOn && f.RequiredFailing == 0 {
		s += " · auto-merge available"
	}

	return s
}

// waitingOn says what auto-merge waits on, as " · waiting for …", or "".
func waitingOn(needs []string) string {
	if len(needs) == 0 {
		return ""
	}

	return " · waiting for " + strings.Join(needs, ", ")
}

// needsOf lists what the pull request still needs to merge.
func needsOf(f Facts) []string {
	needs := []string{}

	switch {
	case f.ChangesRequested:
		needs = append(needs, "changes requested")
	case f.ReviewRequired && f.Mine:
		needs = append(needs, "an approval from someone else")
	case f.ReviewRequired:
		needs = append(needs, "an approval")
	}

	if f.RequiredFailing > 0 {
		needs = append(needs, counted(f.RequiredFailing, "required check failing", "required checks failing"))
	}

	if f.RequiredPending > 0 {
		needs = append(needs, "checks running")
	}

	return needs
}

// counted writes n with a noun in the singular or the plural.
func counted(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}

	return fmt.Sprintf("%d %s", n, many)
}

// base names the branch the pull request merges into.
func base(f Facts) string {
	if f.Base == "" {
		return "its base branch"
	}

	return f.Base
}

// upperFirst capitalizes the first letter of s.
func upperFirst(s string) string {
	if s == "" {
		return s
	}

	return strings.ToUpper(s[:1]) + s[1:]
}
