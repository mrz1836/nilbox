import { useEffect, useRef, type ReactNode, type RefObject } from 'react'

import { counted } from '../../lib/format'
import { earlierCount, entryAnchor, newSince, rowKey, timelineRows, type TimelineRow } from '../../lib/timeline'
import type { ConversationOrder, TimelinePage } from '../../platform'
import { buttonClass, Segmented, type SegmentOption } from '../controls'
import { ErrorBanner } from '../ErrorBanner'
import { Spinner } from '../Spinner'
import { TimelineEntry } from './TimelineEntry'

export interface TimelineProps {
  // lead comes before the entries, as the issue's description does.
  lead?: ReactNode
  // page is the conversation the sheet holds, oldest first.
  page: TimelinePage
  // repo is the item's repository, which names the items its entries link to, and author
  // who opened it, whose comments say so.
  repo: string
  author: string
  // lastSeenAt is GitHub's time of the read the user last opened the item on, what is newer
  // being new to them, or '' on a first visit.
  lastSeenAt: string
  // onEarlier reads the entries before the ones shown; loadingEarlier reports a read under
  // way, and earlierError why the last one failed.
  onEarlier: () => void
  loadingEarlier: boolean
  earlierError: string
  // onQuote quotes a comment's text into the user's draft, and onJump shows the file a
  // review's line comment is on.
  onQuote: (text: string) => void
  onJump?: ((path: string) => void) | undefined
  // empty is what shows while the conversation holds no entries and none come before them.
  empty?: ReactNode
  // order is the order the entries are listed in, under the lead, and onOrder hears the
  // user choose another; without it, the conversation offers no choice.
  order?: ConversationOrder
  onOrder?: ((order: ConversationOrder) => void) | undefined
}

// orders are the conversation's orders, as its choice offers them.
const orders: readonly SegmentOption<ConversationOrder>[] = [
  { value: 'oldest', label: 'Oldest first' },
  { value: 'newest', label: 'Newest first' },
]

// Divider is where the New divider goes about its row: before it when the oldest come
// first, and after it when the newest do, so it always marks where the new entries end
// and the ones seen begin.
type Divider = 'before' | 'after' | null

// Timeline is a conversation, its lead first, then its entries, the oldest first unless
// the user chose the newest: comments and reviews, folded when hidden or a bot's, and
// events on a line, the labels one person changed at once on one. A divider marks what is
// new since the user's last visit, and the conversation scrolls to it once it shows; a
// button where the older entries end reads the entries before the ones shown.
export function Timeline({
  lead,
  page,
  repo,
  author,
  lastSeenAt,
  onEarlier,
  loadingEarlier,
  earlierError,
  onQuote,
  onJump,
  empty,
  order = 'oldest',
  onOrder,
}: TimelineProps) {
  const rows = timelineRows(page.items)
  const fresh = newSince(rows, lastSeenAt)
  const divider = useRef<HTMLLIElement>(null)
  const scrolled = useRef(false)
  const earlier = earlierCount(page.total, page.items.length, page.before)
  const newest = order === 'newest'
  const shown = newest ? rows.toReversed() : rows
  // The first new row, where shown lists it: in the newest first, the divider goes after it.
  const at = fresh < 0 ? -1 : newest ? rows.length - 1 - fresh : fresh

  // A conversation in a hidden Activity runs no effect, so this runs, and scrolls, the first
  // time the conversation shows.
  useEffect(() => {
    if (fresh >= 0 && !scrolled.current) {
      scrolled.current = true
      divider.current?.scrollIntoView({ block: 'center' })
    }
  }, [fresh])

  const showEarlier = earlier > 0 && (
    <div className="flex items-center justify-center gap-2">
      <button type="button" disabled={loadingEarlier} onClick={onEarlier} className={buttonClass}>
        {loadingEarlier ? <Spinner className="size-3.5" /> : null}
        Show {counted(earlier, 'earlier entry', 'earlier entries')}
      </button>
    </div>
  )

  return (
    <section aria-label="Conversation" className="mx-auto max-w-3xl space-y-3">
      {lead}
      {onOrder !== undefined && (rows.length > 1 || earlier > 0) && (
        <div className="flex justify-end">
          <Segmented label="Order of the conversation" size="sm" value={order} options={orders} onChange={onOrder} />
        </div>
      )}
      {!newest && showEarlier}
      {earlierError !== '' && <ErrorBanner>{earlierError}</ErrorBanner>}
      {rows.length === 0 && earlier === 0 && empty}
      <ol aria-label="Entries" className="space-y-3 empty:hidden">
        {shown.map((row, i) => (
          <TimelineRowItem
            key={rowKey(row)}
            divider={i !== at ? null : newest ? 'after' : 'before'}
            dividerRef={divider}
            row={row}
            repo={repo}
            author={author}
            onQuote={onQuote}
            onJump={onJump}
          />
        ))}
      </ol>
      {newest && showEarlier}
    </section>
  )
}

interface TimelineRowItemProps {
  divider: Divider
  dividerRef: RefObject<HTMLLIElement | null>
  row: TimelineRow
  repo: string
  author: string
  onQuote: (text: string) => void
  onJump?: ((path: string) => void) | undefined
}

// NewDivider marks where what is new since the user's last visit ends and what they saw
// begins, its arrow pointing to the new.
function NewDivider({ dividerRef, above }: { dividerRef: RefObject<HTMLLIElement | null>; above: boolean }) {
  return (
    <li
      ref={dividerRef}
      role="separator"
      aria-label="New since your last visit"
      className="flex items-center gap-2 text-xs font-medium text-accent"
    >
      <span aria-hidden="true" className="h-px flex-1 bg-accent/60" />
      {above ? 'New ↑' : 'New'}
      <span aria-hidden="true" className="h-px flex-1 bg-accent/60" />
    </li>
  )
}

// TimelineRowItem is one row of the conversation, with the New divider beside it when it is
// where the new entries meet the ones seen. Its anchor lets the sheet scroll to it.
function TimelineRowItem({ divider, dividerRef, row, repo, author, onQuote, onJump }: TimelineRowItemProps) {
  return (
    <>
      {divider === 'before' && <NewDivider dividerRef={dividerRef} above={false} />}
      <li id={entryAnchor(rowKey(row))} className="scroll-mt-4">
        <TimelineEntry row={row} repo={repo} author={author} onQuote={onQuote} onJump={onJump} />
      </li>
      {divider === 'after' && <NewDivider dividerRef={dividerRef} above />}
    </>
  )
}
