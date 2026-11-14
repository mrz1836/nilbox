import { useEffect, useRef, type ReactNode, type RefObject } from 'react'

import { counted } from '../../lib/format'
import { earlierCount, entryAnchor, newSince, rowKey, timelineRows, type TimelineRow } from '../../lib/timeline'
import type { TimelinePage } from '../../platform'
import { buttonClass } from '../controls'
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
}

// Timeline is a conversation, oldest first: comments and reviews, folded when hidden or a
// bot's, and events on a line, the labels one person changed at once on one. A divider
// marks what is new since the user's last visit, and the conversation scrolls to it once it
// shows; a button reads the entries before the ones shown.
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
}: TimelineProps) {
  const rows = timelineRows(page.items)
  const fresh = newSince(rows, lastSeenAt)
  const divider = useRef<HTMLLIElement>(null)
  const scrolled = useRef(false)
  const earlier = earlierCount(page.total, page.items.length, page.before)

  // A conversation in a hidden Activity runs no effect, so this runs, and scrolls, the first
  // time the conversation shows.
  useEffect(() => {
    if (fresh >= 0 && !scrolled.current) {
      scrolled.current = true
      divider.current?.scrollIntoView({ block: 'center' })
    }
  }, [fresh])

  return (
    <section aria-label="Conversation" className="mx-auto max-w-3xl space-y-3">
      {lead}
      {earlier > 0 && (
        <div className="flex items-center justify-center gap-2">
          <button type="button" disabled={loadingEarlier} onClick={onEarlier} className={buttonClass}>
            {loadingEarlier ? <Spinner className="size-3.5" /> : null}
            Show {counted(earlier, 'earlier entry', 'earlier entries')}
          </button>
        </div>
      )}
      {earlierError !== '' && <ErrorBanner>{earlierError}</ErrorBanner>}
      {rows.length === 0 && earlier === 0 && empty}
      <ol className="space-y-3 empty:hidden">
        {rows.map((row, i) => (
          <TimelineRowItem
            key={rowKey(row)}
            newHere={i === fresh}
            divider={divider}
            row={row}
            repo={repo}
            author={author}
            onQuote={onQuote}
            onJump={onJump}
          />
        ))}
      </ol>
    </section>
  )
}

interface TimelineRowItemProps {
  newHere: boolean
  divider: RefObject<HTMLLIElement | null>
  row: TimelineRow
  repo: string
  author: string
  onQuote: (text: string) => void
  onJump?: ((path: string) => void) | undefined
}

// TimelineRowItem is one row of the conversation, after the New divider when it is the
// first the user hasn't seen. Its anchor lets the sheet scroll to it.
function TimelineRowItem({ newHere, divider, row, repo, author, onQuote, onJump }: TimelineRowItemProps) {
  return (
    <>
      {newHere && (
        <li
          ref={divider}
          role="separator"
          aria-label="New since your last visit"
          className="flex items-center gap-2 text-xs font-medium text-accent"
        >
          <span aria-hidden="true" className="h-px flex-1 bg-accent/60" />
          New
          <span aria-hidden="true" className="h-px flex-1 bg-accent/60" />
        </li>
      )}
      <li id={entryAnchor(rowKey(row))} className="scroll-mt-4">
        <TimelineEntry row={row} repo={repo} author={author} onQuote={onQuote} onJump={onJump} />
      </li>
    </>
  )
}
