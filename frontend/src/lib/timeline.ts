// The conversation's rows and words, which the issue sheet and the review sheet share: how
// its entries are listed, which are new to the user, how a quote is written into a reply,
// and how many entries and comments there are. They are pure, so the sheets, their parts,
// and their tests share them.

import type { TimelineEntry, TimelineLabel } from '../platform'

// ms reads an RFC 3339 time, or NaN for '' or one that doesn't read.
function ms(iso: string): number {
  return iso === '' ? Number.NaN : Date.parse(iso)
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
  added: TimelineLabel[]
  removed: TimelineLabel[]
}

// TimelineRow is one entry of the conversation as the sheet lists it: a comment or an
// event as GitHub read it, or labels grouped.
export type TimelineRow = { kind: 'item'; item: TimelineEntry } | LabelRow

// minuteOf is the minute an entry was made in, for grouping labels.
function minuteOf(iso: string): string {
  return iso.slice(0, 16)
}

// timelineRows lists the conversation's entries, the labels one person added and removed in
// the same minute grouped into one.
export function timelineRows(items: readonly TimelineEntry[]): TimelineRow[] {
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

// entryAnchor is the ID of an entry's row in the conversation, from its key (rowKey), which
// the sheet scrolls to.
export function entryAnchor(key: string): string {
  return `entry-${key.replaceAll(/[^A-Za-z0-9_-]/g, '_')}`
}

// newSince is the index of the first row the user hasn't seen: newer than lastSeenAt,
// GitHub's time of the read they last opened the issue or pull request on, and not their
// own. It is -1 on
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

// earlierCount is how many entries come before the ones the sheet holds, or 0 when none do.
export function earlierCount(total: number, held: number, before: string): number {
  return before === '' ? 0 : Math.max(total - held, 1)
}

// commentCount counts what people wrote in the conversation's entries: each comment, each
// review that says something, and each of a review's comments on a line, those GitHub
// holds beyond the ones read included. It reads "12", or "12+" while entries before the
// ones held (before, their cursor) aren't read, which may hold more.
export function commentCount(entries: readonly TimelineEntry[], before: string): string {
  let n = 0

  for (const e of entries) {
    if (e.kind === 'comment') {
      n++
    } else if (e.kind === 'reviewed') {
      n += (e.body.trim() === '' ? 0 : 1) + e.lineComments.length + e.moreLineComments
    }
  }

  return before === '' ? String(n) : `${String(n)}+`
}
