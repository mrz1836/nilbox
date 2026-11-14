package prompts

import (
	"github.com/mrz1836/nilbox/internal/item"
)

// DefaultSlot names one of the library's defaults: the default prompt for pull requests or
// for issues, or one of the default replies, under its ReplySlot's name.
type DefaultSlot string

// The library's defaults.
const (
	DefaultSlotPR             DefaultSlot = "pr"
	DefaultSlotIssue          DefaultSlot = "issue"
	DefaultSlotApprove        DefaultSlot = "approve"
	DefaultSlotApproveBots    DefaultSlot = "approve_bots"
	DefaultSlotRequestChanges DefaultSlot = "request_changes"
	DefaultSlotComment        DefaultSlot = "comment"
)

// DefaultSlots returns every default, the kinds' first, then the replies in ReplySlots
// order.
func DefaultSlots() []DefaultSlot {
	out := make([]DefaultSlot, 0, 2+len(ReplySlots()))
	out = append(out, DefaultSlotPR, DefaultSlotIssue)

	for _, slot := range ReplySlots() {
		out = append(out, DefaultSlot(slot))
	}

	return out
}

// kind returns the kind of item slot is the default prompt for, and whether it is one.
func (s DefaultSlot) kind() (item.Kind, bool) {
	switch s {
	case DefaultSlotPR:
		return item.KindPR, true
	case DefaultSlotIssue:
		return item.KindIssue, true
	case DefaultSlotApprove, DefaultSlotApproveBots, DefaultSlotRequestChanges, DefaultSlotComment:
		return "", false
	default:
		return "", false
	}
}

// takes reports whether a prompt with purpose, written for action and applying to
// appliesTo, can be the slot's default: an agent prompt for the kind, or a reply the reply
// slot takes (see ReplySlot.takes).
func (s DefaultSlot) takes(purpose Purpose, action ReplyAction, appliesTo AppliesTo) bool {
	if kind, ok := s.kind(); ok {
		return purposeOf(purpose) == PurposeAgent && appliesTo.Covers(kind)
	}

	return ReplySlot(s).takes(purposeOf(purpose), action, appliesTo)
}

// Slot returns the ID of slot's default; 0 for none.
func (d DefaultPrompts) Slot(slot DefaultSlot) int64 {
	if kind, ok := slot.kind(); ok {
		return d.For(kind)
	}

	return d.Replies.For(ReplySlot(slot))
}

// For returns the nickname naming slot's default; "" for none.
func (d *BundleDefaults) For(slot DefaultSlot) string {
	switch slot {
	case DefaultSlotPR:
		return d.PR
	case DefaultSlotIssue:
		return d.Issue
	case DefaultSlotApprove, DefaultSlotApproveBots, DefaultSlotRequestChanges, DefaultSlotComment:
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
	case DefaultSlotApprove, DefaultSlotApproveBots, DefaultSlotRequestChanges, DefaultSlotComment:
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
		at := firstNamed(files, defaults.For(slot))
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
