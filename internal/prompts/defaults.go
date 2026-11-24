package prompts

import (
	"slices"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// DefaultSlot names one of the library's defaults: the default prompt for pull requests or
// for issues, the default prompt for each situation a pull request can be in, or one of the
// default replies, under its ReplySlot's name.
type DefaultSlot string

// The library's defaults. A situation's slot has the name of its situation in the
// launcher (launcher.Situation).
const (
	DefaultSlotPR    DefaultSlot = "pr"
	DefaultSlotIssue DefaultSlot = "issue"

	// The default prompt for a pull request whose checks fail, that conflicts with its
	// base, that a reviewer asked to change, and that the user is asked to review again.
	DefaultSlotChecksFailing    DefaultSlot = "checks_failing"
	DefaultSlotConflicts        DefaultSlot = "conflicts"
	DefaultSlotChangesRequested DefaultSlot = "changes_requested"
	DefaultSlotRereview         DefaultSlot = "rereview"

	DefaultSlotApprove        DefaultSlot = "approve"
	DefaultSlotApproveBots    DefaultSlot = "approve_bots"
	DefaultSlotRequestChanges DefaultSlot = "request_changes"
	DefaultSlotComment        DefaultSlot = "comment"
	DefaultSlotNudge          DefaultSlot = "nudge"
)

// DefaultSlots returns every default, the kinds' first, then the situations' in
// situationSlots order, then the replies in ReplySlots order.
func DefaultSlots() []DefaultSlot {
	out := make([]DefaultSlot, 0, 2+len(situationSlots())+len(ReplySlots()))
	out = append(out, DefaultSlotPR, DefaultSlotIssue)
	out = append(out, situationSlots()...)

	for _, slot := range ReplySlots() {
		out = append(out, DefaultSlot(slot))
	}

	return out
}

// situationSlots returns the default prompt of each situation a pull request can be in, in
// the order the settings list them.
func situationSlots() []DefaultSlot {
	return []DefaultSlot{
		DefaultSlotChecksFailing, DefaultSlotConflicts, DefaultSlotChangesRequested, DefaultSlotRereview,
	}
}

// When names a situation's slot in a sentence, as in "when checks fail"; "" for every
// other slot.
func (s DefaultSlot) When() string {
	switch s {
	case DefaultSlotChecksFailing:
		return "when checks fail"
	case DefaultSlotConflicts:
		return "when it has conflicts"
	case DefaultSlotChangesRequested:
		return "when changes are requested"
	case DefaultSlotRereview:
		return "when you're asked to review again"
	case DefaultSlotPR, DefaultSlotIssue, DefaultSlotApprove, DefaultSlotApproveBots, DefaultSlotRequestChanges,
		DefaultSlotComment, DefaultSlotNudge:
		return ""
	default:
		return ""
	}
}

// kind returns the kind of item slot is the app's default prompt for, and whether it is
// one. A situation's slot is not: it is a pull request's prompt only in its situation.
func (s DefaultSlot) kind() (item.Kind, bool) {
	switch s {
	case DefaultSlotPR:
		return item.KindPR, true
	case DefaultSlotIssue:
		return item.KindIssue, true
	case DefaultSlotChecksFailing, DefaultSlotConflicts, DefaultSlotChangesRequested, DefaultSlotRereview,
		DefaultSlotApprove, DefaultSlotApproveBots, DefaultSlotRequestChanges, DefaultSlotComment,
		DefaultSlotNudge:
		return "", false
	default:
		return "", false
	}
}

// situation reports whether slot is the default prompt of a situation a pull request can
// be in.
func (s DefaultSlot) situation() bool {
	return s.When() != ""
}

// describe names a situation's slot in a sentence, as in "the default when checks fail".
func (s DefaultSlot) describe() string {
	return "the default " + s.When()
}

// starter is the key of the starter a situation's slot starts on, and "" for every other
// slot.
func (s DefaultSlot) starter() string {
	switch s {
	case DefaultSlotChecksFailing:
		return StarterFixChecks
	case DefaultSlotConflicts:
		return StarterResolveConflicts
	case DefaultSlotChangesRequested:
		return StarterAddressReview
	case DefaultSlotRereview:
		return StarterRereview
	case DefaultSlotPR, DefaultSlotIssue, DefaultSlotApprove, DefaultSlotApproveBots, DefaultSlotRequestChanges,
		DefaultSlotComment, DefaultSlotNudge:
		return ""
	default:
		return ""
	}
}

// takes reports whether a prompt with purpose, written for action and applying to
// appliesTo, can be the slot's default: an agent prompt for the kind, an agent prompt for
// pull requests in a situation's slot, or a reply the reply slot takes (see
// ReplySlot.takes).
func (s DefaultSlot) takes(purpose Purpose, action ReplyAction, appliesTo AppliesTo) bool {
	if kind, ok := s.kind(); ok {
		return purposeOf(purpose) == PurposeAgent && appliesTo.Covers(kind)
	}

	if s.situation() {
		return purposeOf(purpose) == PurposeAgent && appliesTo.Covers(item.KindPR)
	}

	return ReplySlot(s).takes(purposeOf(purpose), action, appliesTo)
}

// Slot returns the ID of slot's default; 0 for none.
func (d DefaultPrompts) Slot(slot DefaultSlot) int64 {
	switch slot {
	case DefaultSlotPR:
		return d.PR
	case DefaultSlotIssue:
		return d.Issue
	case DefaultSlotChecksFailing:
		return d.ChecksFailing
	case DefaultSlotConflicts:
		return d.Conflicts
	case DefaultSlotChangesRequested:
		return d.ChangesRequested
	case DefaultSlotRereview:
		return d.Rereview
	case DefaultSlotApprove, DefaultSlotApproveBots, DefaultSlotRequestChanges, DefaultSlotComment,
		DefaultSlotNudge:
		return d.Replies.For(ReplySlot(slot))
	default:
		return 0
	}
}

// For returns the nickname naming slot's default; "" for none.
func (d *BundleDefaults) For(slot DefaultSlot) string {
	switch slot {
	case DefaultSlotPR:
		return d.PR
	case DefaultSlotIssue:
		return d.Issue
	case DefaultSlotChecksFailing:
		return d.ChecksFailing
	case DefaultSlotConflicts:
		return d.Conflicts
	case DefaultSlotChangesRequested:
		return d.ChangesRequested
	case DefaultSlotRereview:
		return d.Rereview
	case DefaultSlotApprove, DefaultSlotApproveBots, DefaultSlotRequestChanges, DefaultSlotComment,
		DefaultSlotNudge:
		return d.Replies[ReplySlot(slot)]
	default:
		return ""
	}
}

// Set names nickname slot's default.
func (d *BundleDefaults) Set(slot DefaultSlot, nickname string) {
	switch slot {
	case DefaultSlotPR:
		d.PR = nickname
	case DefaultSlotIssue:
		d.Issue = nickname
	case DefaultSlotChecksFailing:
		d.ChecksFailing = nickname
	case DefaultSlotConflicts:
		d.Conflicts = nickname
	case DefaultSlotChangesRequested:
		d.ChangesRequested = nickname
	case DefaultSlotRereview:
		d.Rereview = nickname
	case DefaultSlotApprove, DefaultSlotApproveBots, DefaultSlotRequestChanges, DefaultSlotComment,
		DefaultSlotNudge:
		d.setReply(ReplySlot(slot), nickname)
	}
}

// DefaultOffer is one of a source's defaults that importing its prompt would make the
// library's, as the import dialog offers it.
type DefaultOffer struct {
	// Slot is the default.
	Slot DefaultSlot `json:"slot"`

	// Nickname is the name the source's default will have in the library: the name it is
	// saved under, after a rename, or the name of the prompt that already holds it, when it
	// is skipped.
	Nickname string `json:"nickname"`

	// Current is the nickname of the slot's default now, or "" for none.
	Current string `json:"current"`

	// Entry is the index, among the preview's entries, of the prompt the offer depends on:
	// unticked, it is not imported, and neither is the offer.
	Entry int `json:"entry"`
}

// offers works out the defaults importing files, planned as plans (one per prompt) against
// lib, could set: each default the source names, by a nickname among files, that the prompt
// it ends up as fits, and that is not already the library's. Where two of files share a
// nickname, the first is the one the default names, as Import links it.
func offers(lib Library, plans []planned, files []BundlePrompt, defaults BundleDefaults) []DefaultOffer {
	byID := promptsByID(lib.Prompts)
	out := []DefaultOffer{}

	for _, slot := range DefaultSlots() {
		at := sourcePrompt(files, defaults, slot)
		if at < 0 {
			continue
		}

		e := plans[at]
		becomes := e.stored()
		becomes.ID = e.target

		if e.Outcome == ImportSkip {
			becomes = byID[e.held]
		}

		current := lib.Defaults.Slot(slot)
		if !slot.takes(becomes.Purpose, actionOf(becomes), becomes.AppliesTo) || (becomes.ID != 0 && becomes.ID == current) {
			continue
		}

		out = append(out, DefaultOffer{Slot: slot, Nickname: e.Nickname, Current: byID[current].Nickname, Entry: at})
	}

	return out
}

// sourcePrompt returns the index of the source's prompt that is slot's default in
// defaults: the one with its UUID, else the first with its nickname; -1 for none.
func sourcePrompt(prompts []BundlePrompt, defaults BundleDefaults, slot DefaultSlot) int {
	if u, ok := textutil.CanonicalUUID(defaults.UUIDs[slot]); ok {
		if i := slices.IndexFunc(prompts, func(p BundlePrompt) bool { return p.UUID == u }); i >= 0 {
			return i
		}
	}

	return firstNamed(prompts, defaults.For(slot))
}

// firstNamed returns the index of the first of prompts with nickname, or -1 for none and
// for "".
func firstNamed(prompts []BundlePrompt, nickname string) int {
	if nickname == "" {
		return -1
	}

	for i, p := range prompts {
		if p.Nickname == nickname {
			return i
		}
	}

	return -1
}
