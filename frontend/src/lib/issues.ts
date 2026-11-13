// The issue sheet's words and choices, worked out from what the sheet read. They are pure,
// so the sheet, its parts, and their tests share them.

import type {
  InboxItem,
  Issue,
  IssueAction,
  IssueLabel,
  IssueLinked,
  IssueOffer,
  IssueResult,
  IssueStandingState,
  IssueStateReason,
  IssueTimelineItem,
  RefState,
} from '../platform'
import type { Tone } from './signals'

// IssueLook is how an issue's state shows beside its title: the icon's color, and what the
// icon says to a screen reader.
export interface IssueLook {
  color: string
  words: string
}

// issueLook is an issue's state as its icon shows it: open in the success color, closed as
// completed in the info color, and closed as not planned or as a duplicate muted.
export function issueLook(state: RefState, reason: IssueStateReason): IssueLook {
  if (state === 'OPEN') {
    return { color: 'text-success', words: 'Open issue' }
  }

  switch (reason) {
    case 'NOT_PLANNED':
      return { color: 'text-muted', words: 'Closed as not planned' }
    case 'DUPLICATE':
      return { color: 'text-muted', words: 'Closed as a duplicate' }
    default:
      return { color: 'text-info', words: 'Closed as completed' }
  }
}

// standingTones is each standing's color: a fix is good news, a lock a caution, an issue
// GitHub can't find any more an alarm, and the rest plain.
const standingTones: Readonly<Record<IssueStandingState, Tone>> = {
  gone: 'danger',
  closed: 'muted',
  locked: 'warning',
  fix_merged: 'success',
  fix_open: 'success',
  replied_last: 'muted',
  assigned: 'muted',
  open: 'muted',
}

// standingTone is the color of the sentence that says where an issue stands.
export function standingTone(state: IssueStandingState): Tone {
  return standingTones[state]
}

// selectedIssueAction is the action the sheet's button runs: the one the user chose, while
// the sheet still offers it enabled, else the sheet's primary. A close can be the button
// once chosen; it always asks first.
export function selectedIssueAction(issue: Issue, chosen: IssueAction): IssueAction {
  const offer = issue.offers.find((o) => o.action === chosen)

  return chosen !== '' && offer?.enabled === true ? chosen : issue.primary
}

// writeFirst holds a comment with nothing to say back.
export const writeFirst = 'Write something first.'

// hasText reports text that says something, as the backend reads it: anything left once
// the white space trim drops is gone.
function hasText(body: string): boolean {
  return body.trim() !== ''
}

// issueHeldBack says why the window holds an offer's action back, or '': a comment with
// nothing to say, or a close whose comment GitHub can't take now, until the text is
// cleared.
export function issueHeldBack(offer: IssueOffer | undefined, body: string): string {
  if (offer === undefined) {
    return ''
  }

  if (offer.action === 'comment' && !hasText(body)) {
    return writeFirst
  }

  return offer.commentsFirst && hasText(body) ? offer.commentBlocked : ''
}

// closeNote says what a close posts as a comment first, as its offer marks it, or '' when it
// posts nothing: clearing the text skips the comment. While GitHub can't take the comment,
// it says why instead, which holds the button back (issueHeldBack).
export function closeNote(offer: IssueOffer | undefined, body: string): string {
  if (offer?.commentsFirst !== true || !hasText(body)) {
    return ''
  }

  return offer.commentBlocked !== '' ? offer.commentBlocked : 'Your text is posted as a comment first.'
}

// issueTone is how an action's outcome reads in the inbox's toast: a close that didn't
// happen, or happened without the user, is a warning.
export function issueTone(result: IssueResult): 'success' | 'warning' {
  switch (result.outcome) {
    case 'commented_not_closed':
    case 'new_activity':
    case 'already_closed':
      return 'warning'
    default:
      return 'success'
  }
}

// keepsDraft reports an outcome that leaves the user's text in the sheet, body: a close
// stopped, before anything was sent, by a comment they haven't read, and a close someone made
// first, before the text went, which would otherwise be lost.
export function keepsDraft(result: IssueResult, body: string): boolean {
  return (
    result.outcome === 'new_activity' || (result.outcome === 'already_closed' && !result.commented && hasText(body))
  )
}

// ms reads an RFC 3339 time, or NaN for '' or one that doesn't read.
function ms(iso: string): number {
  return iso === '' ? Number.NaN : Date.parse(iso)
}

// seconds reads an RFC 3339 time to the whole second, as GitHub and the read keep times.
function seconds(iso: string): number {
  return Math.floor(ms(iso) / 1000)
}

// changedSince reports an issue that changed since the sheet read it, as its live inbox card
// says, at no cost: newer activity, to the second, or more comments than the read holds.
export function changedSince(issue: Issue, card: InboxItem | undefined): boolean {
  if (card === undefined || issue.fetchedAt === '') {
    return false
  }

  return seconds(card.updatedAt) > seconds(issue.updatedAt) || (card.details?.comments ?? 0) > issue.seen.comments
}

// LabelRow is the labels one person added and removed in one minute, as one entry: mine
// when the person is the user.
export interface LabelRow {
  kind: 'labels'
  id: string
  actor: string
  actorIsBot: boolean
  mine: boolean
  at: string
  added: IssueLabel[]
  removed: IssueLabel[]
}

// TimelineRow is one entry of the conversation as the sheet lists it: a comment or an
// event as GitHub read it, or labels grouped.
export type TimelineRow = { kind: 'item'; item: IssueTimelineItem } | LabelRow

// minuteOf is the minute an entry was made in, for grouping labels.
function minuteOf(iso: string): string {
  return iso.slice(0, 16)
}

// timelineRows lists the conversation's entries, the labels one person added and removed in
// the same minute grouped into one.
export function timelineRows(items: readonly IssueTimelineItem[]): TimelineRow[] {
  const rows: TimelineRow[] = []

  for (const it of items) {
    if ((it.kind !== 'labeled' && it.kind !== 'unlabeled') || it.label === null) {
      rows.push({ kind: 'item', item: it })

      continue
    }

    const last = rows.at(-1)
    const row: LabelRow =
      last?.kind === 'labels' && last.actor === it.actor && minuteOf(last.at) === minuteOf(it.at)
        ? last
        : {
            kind: 'labels',
            id: it.id,
            actor: it.actor,
            actorIsBot: it.actorIsBot,
            mine: it.mine,
            at: it.at,
            added: [],
            removed: [],
          }

    if (row !== last) {
      rows.push(row)
    }

    if (it.kind === 'labeled') {
      row.added.push(it.label)
    } else {
      row.removed.push(it.label)
    }
  }

  return rows
}

// rowKey tells the conversation's rows apart.
export function rowKey(row: TimelineRow): string {
  return row.kind === 'labels' ? row.id : row.item.id
}

// newSince is the index of the first row the user hasn't seen: newer than lastSeenAt,
// GitHub's time of the read they last opened the issue on, and not their own. It is -1 on
// a first visit, and when nothing is new.
export function newSince(rows: readonly TimelineRow[], lastSeenAt: string): number {
  const seen = ms(lastSeenAt)

  if (Number.isNaN(seen)) {
    return -1
  }

  return rows.findIndex((row) => {
    const { mine, at } = row.kind === 'labels' ? row : row.item

    return !mine && ms(at) > seen
  })
}

// quoteInto adds text to body as a quote, each of its lines marked with "> ", and leaves a
// line after it for the reply.
export function quoteInto(body: string, text: string): string {
  const quote = text
    .trim()
    .split('\n')
    .map((line) => (line === '' ? '>' : `> ${line}`))
    .join('\n')
  const before = body.trim() === '' ? '' : `${body.replace(/\s+$/, '')}\n\n`

  return `${before}${quote}\n\n`
}

// refText names a linked item as "owner/name#number".
export function refText(linked: IssueLinked): string {
  return `${linked.repo}#${String(linked.number)}`
}

// earlierCount is how many entries come before the ones the sheet holds, or 0 when none do.
export function earlierCount(total: number, held: number, before: string): number {
  return before === '' ? 0 : Math.max(total - held, 1)
}
