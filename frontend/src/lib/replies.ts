// Shared wording for review replies: the prompts the review sheet fills in as a review's
// text. A reply is written for one review (an approval, a request for changes, or a
// comment) and can be the default reply for it, and an approval also for bots' pull
// requests.

import type { PromptReplyAction, ReplySlot } from '../platform'

// ReplyActionChoice is a review a reply can be written for.
export type ReplyActionChoice = Exclude<PromptReplyAction, ''>

// replyGroups are the reviews a reply can be for, in the order the library lists them,
// each with its group's title.
export const replyGroups: readonly { action: ReplyActionChoice; title: string }[] = [
  { action: 'approve', title: 'Approvals' },
  { action: 'request_changes', title: 'Requests for changes' },
  { action: 'comment', title: 'Comments' },
]

// replyActionLabels name each review as the editor's Action choice does.
export const replyActionLabels: Readonly<Record<ReplyActionChoice, string>> = {
  approve: 'Approve',
  request_changes: 'Request changes',
  comment: 'Comment',
}

// replySlotBadges name each default reply in the short form the library's badges show.
export const replySlotBadges: Readonly<Record<ReplySlot, string>> = {
  approve: 'Default for approvals',
  approve_bots: 'Default for bots',
  request_changes: 'Default for changes',
  comment: 'Default for comments',
}

// replySlotPlaces name what each default reply is the default for, as in "the default
// for approvals on bot pull requests".
export const replySlotPlaces: Readonly<Record<ReplySlot, string>> = {
  approve: 'approvals',
  approve_bots: 'approvals on bot pull requests',
  request_changes: 'requests for changes',
  comment: 'comments',
}

// replySlots lists the defaults a reply written for action can be, in the order the
// menu offers them.
export function replySlots(action: PromptReplyAction): ReplySlot[] {
  switch (action) {
    case 'approve':
      return ['approve', 'approve_bots']
    case 'request_changes':
      return ['request_changes']
    case 'comment':
      return ['comment']
    default:
      return []
  }
}

// replyDefaultsText says what a reply is the default for, as in "approvals and approvals
// on bot pull requests".
export function replyDefaultsText(slots: readonly ReplySlot[]): string {
  return slots.map((slot) => replySlotPlaces[slot]).join(' and ')
}

// replyUsageSummary says where a reply is a default, as in "Default for approvals", or
// "Not a default".
export function replyUsageSummary(slots: readonly ReplySlot[]): string {
  return slots.length === 0 ? 'Not a default' : `Default for ${replyDefaultsText(slots)}`
}
