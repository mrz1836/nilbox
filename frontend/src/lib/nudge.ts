// When a card offers to nudge the author of a pull request the user asked for changes on,
// and what it says of the wait: pure functions of the item and the time, which the caller
// reads (useNowAs), so a tick renders a card only when what it says changes. A nudge is a
// comment on the pull request's conversation (the inbox's Nudge), which the card's primary
// offers once the author has been quiet for Settings → Reviewing's Offer a nudge after.

import type { InboxItem } from '../platform'
import { formatAge, formatAgo } from './time'

// hourMs is an hour in milliseconds.
const hourMs = 60 * 60 * 1000

// awaitsAuthor reports a pull request someone else opened whose author the user asked for
// changes, that hasn't been pushed to since nor asks for the user's review again: nothing
// is the user's to do until its author answers (nextStep's awaiting_author).
export function awaitsAuthor(item: InboxItem): boolean {
  const d = item.details

  return (
    item.kind === 'pr' &&
    !item.authored &&
    item.reviewRequest === 'none' &&
    d !== null &&
    d.viewerVerdict === 'changes' &&
    !d.pushedSinceReview
  )
}

// nudgeable reports a pull request waiting on its author that a nudge can ask: one a person
// opened, since a bot reads no comment.
export function nudgeable(item: InboxItem): boolean {
  return awaitsAuthor(item) && !item.authorIsBot
}

// quietSince is when the pull request last moved, as RFC 3339: the later of its last
// activity and the user's last nudge, so a nudge just posted starts the wait again.
export function quietSince(item: InboxItem): string {
  if (item.nudgedAt === '') {
    return item.updatedAt
  }

  return Date.parse(item.nudgedAt) > Date.parse(item.updatedAt) ? item.nudgedAt : item.updatedAt
}

// nudgeDue reports a nudgeable pull request quiet for at least afterHours at now; never
// while afterHours is 0 (Off).
export function nudgeDue(item: InboxItem, now: Date, afterHours: number): boolean {
  if (afterHours <= 0 || !nudgeable(item)) {
    return false
  }

  const since = Date.parse(quietSince(item))

  return !Number.isNaN(since) && now.getTime() - since >= afterHours * hourMs
}

// quietWords say how long a pull request has waited on its author, at now: since the
// user's nudge, "you nudged them 5h ago", or else since it last moved, "quiet for 2d".
export function quietWords(item: InboxItem, now: Date): string {
  return item.nudgedAt === ''
    ? `quiet for ${formatAge(now, item.updatedAt)}`
    : `you nudged them ${formatAgo(now, item.nudgedAt)}`
}

// nudgeWords are what a card's story says of the wait, at now, for a pull request waiting
// on its author: that the user nudged them, once they did, or how long it has been quiet,
// once a nudge is due with afterHours; '' otherwise, when the story says only that the
// user requested changes.
export function nudgeWords(item: InboxItem, now: Date, afterHours: number): string {
  if (!awaitsAuthor(item)) {
    return ''
  }

  return item.nudgedAt !== '' || nudgeDue(item, now, afterHours) ? quietWords(item, now) : ''
}
