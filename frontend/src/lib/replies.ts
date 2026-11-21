// Shared wording for review replies: the prompts the review sheet fills in as a review's
// text. A reply is written for one review (an approval, a request for changes, or a
// comment) and can be the default reply for it, an approval also for bots' pull
// requests, and a comment also for a nudge, the comment a card posts to ask the author of
// a pull request the user requested changes on how it is going. It also lays out the
// Replies menus they are picked from.

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
  nudge: 'Default for nudges',
}

// replySlotPlaces name what each default reply is the default for, as in "the default
// for approvals on bot pull requests".
export const replySlotPlaces: Readonly<Record<ReplySlot, string>> = {
  approve: 'approvals',
  approve_bots: 'approvals on bot pull requests',
  request_changes: 'requests for changes',
  comment: 'comments',
  nudge: 'nudges',
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
      return ['comment', 'nudge']
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

// ReplyChoice is a reply a Replies menu offers: its ID, the nickname it goes by, and its
// name, which its row's title shows.
interface ReplyChoice {
  id: number
  nickname: string
  name: string
}

// ReplyMenuItem is a row of a Replies menu, as the window's menus take one (MenuItem).
export interface ReplyMenuItem {
  label: string
  hint?: string | undefined
  title?: string
  disabled?: boolean
  divided?: boolean
  onSelect?: () => void
}

// ReplyMenu is what a Replies menu offers.
export interface ReplyMenu {
  // replies are the library's replies to pick from, or undefined while they load, and
  // error says why they couldn't load, or ''.
  replies: readonly ReplyChoice[] | undefined
  error: string
  // none says that the library has none to offer, as in "No comment replies yet".
  none: string
  // fallback is the default reply, which its row marks, or 0 for none (the default).
  fallback?: number
  // onPick fills in the reply the user picks, by its ID.
  onPick: (replyId: number) => void
  // onManage opens the replies in the Prompts tab, from the menu's foot; without it the
  // menu has no foot.
  onManage?: (() => void) | undefined
}

// replyMenuItems are the rows of a Replies menu: the library's replies, the default one
// marked, or why there are none to pick (they are loading, couldn't load, or there are
// none), then Manage replies…, set apart at the foot.
export function replyMenuItems({ replies, error, none, fallback = 0, onPick, onManage }: ReplyMenu): ReplyMenuItem[] {
  const list = replies ?? []
  const choices: ReplyMenuItem[] =
    list.length > 0
      ? list.map((r) => ({
          label: r.nickname,
          hint: r.id === fallback ? 'default' : undefined,
          title: r.name,
          onSelect: () => {
            onPick(r.id)
          },
        }))
      : replies === undefined && error !== ''
        ? [{ label: 'Couldn’t load the replies', title: error, disabled: true }]
        : [{ label: replies === undefined ? 'Loading replies…' : none, disabled: true }]

  return [
    ...choices,
    ...(onManage === undefined ? [] : [{ label: 'Manage replies…', divided: true, onSelect: onManage }]),
  ]
}
