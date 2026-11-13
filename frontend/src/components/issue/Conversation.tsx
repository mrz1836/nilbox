import { useEffect, useRef, type RefObject } from 'react'

import { counted } from '../../lib/format'
import { earlierCount, newSince, rowKey, timelineRows, type TimelineRow } from '../../lib/issues'
import { formatAgo, formatFull } from '../../lib/time'
import type { Issue } from '../../platform'
import { PersonAvatar } from '../Avatar'
import { buttonClass } from '../controls'
import { ErrorBanner } from '../ErrorBanner'
import { Markdown } from '../Markdown'
import { RelativeTime } from '../RelativeTime'
import { DescriptionSkeleton } from '../review/ReviewLoading'
import { Spinner } from '../Spinner'
import { TimelineEntry, Trimmed } from './TimelineEntry'

export interface ConversationProps {
  issue: Issue
  // lastSeenAt is GitHub's time of the read the user last opened the issue on, what is
  // newer being new to them, or '' on a first visit.
  lastSeenAt: string
  // reading reports the first read from GitHub under way, before which the description is
  // a placeholder.
  reading: boolean
  // onEarlier reads the entries before the ones shown; loadingEarlier reports a read under
  // way, and earlierError why the last one failed.
  onEarlier: () => void
  loadingEarlier: boolean
  earlierError: string
  // onQuote quotes a comment's text into the user's draft.
  onQuote: (text: string) => void
}

// reactionMarks names each of GitHub's reactions by its emoji.
const reactionMarks: Readonly<Record<string, string>> = {
  THUMBS_UP: '👍',
  THUMBS_DOWN: '👎',
  LAUGH: '😄',
  HOORAY: '🎉',
  CONFUSED: '😕',
  HEART: '❤️',
  ROCKET: '🚀',
  EYES: '👀',
}

// Description is the issue's description: who opened it and when, whether it was edited,
// its text, and how people reacted to it.
function Description({ issue, reading }: { issue: Issue; reading: boolean }) {
  const read = issue.fetchedAt !== ''

  return (
    <article aria-label="Description" className="rounded-md border border-border">
      <header className="flex items-center gap-2 border-b border-border bg-raised/40 px-3 py-1.5 text-xs text-muted">
        <PersonAvatar login={issue.author} />
        <span className="font-medium text-text">{issue.author}</span>
        {issue.openedAt !== '' && (
          <span title={formatFull(issue.openedAt)}>
            opened <RelativeTime iso={issue.openedAt} show={formatAgo} />
          </span>
        )}
        {issue.editedAt !== '' && <span title={formatFull(issue.editedAt)}>· edited</span>}
      </header>
      <div className="px-3 py-2">
        {issue.body.trim() !== '' ? (
          <Markdown text={issue.body} />
        ) : reading ? (
          <DescriptionSkeleton />
        ) : (
          <p className="text-sm text-muted">{read ? 'No description.' : 'Not read from GitHub yet.'}</p>
        )}
        {issue.bodyTrimmed && <Trimmed url={issue.url} />}
        {issue.reactions.length > 0 && (
          <ul aria-label="Reactions" className="mt-2 flex flex-wrap gap-1.5">
            {issue.reactions.map((r) => (
              <li
                key={r.content}
                title={r.content.toLowerCase().replaceAll('_', ' ')}
                className="rounded-full border border-border px-2 py-px text-xs"
              >
                {reactionMarks[r.content] ?? r.content} {r.count}
              </li>
            ))}
          </ul>
        )}
      </div>
    </article>
  )
}

// Conversation is the issue's description and its timeline, oldest first: comments, folded
// when hidden or a bot's, and events on a line, the labels one person changed at once on
// one. A divider marks what is new since the user's last visit, and the conversation
// scrolls to it once; a button reads the entries before the ones shown.
export function Conversation({
  issue,
  lastSeenAt,
  reading,
  onEarlier,
  loadingEarlier,
  earlierError,
  onQuote,
}: ConversationProps) {
  const rows = timelineRows(issue.timeline.items)
  const fresh = newSince(rows, lastSeenAt)
  const divider = useRef<HTMLLIElement>(null)
  const scrolled = useRef(false)
  const earlier = earlierCount(issue.timeline.total, issue.timeline.items.length, issue.timeline.before)

  useEffect(() => {
    if (fresh >= 0 && !scrolled.current) {
      scrolled.current = true
      divider.current?.scrollIntoView({ block: 'center' })
    }
  }, [fresh])

  return (
    <section aria-label="Conversation" className="mx-auto max-w-3xl space-y-3">
      <Description issue={issue} reading={reading} />
      {earlier > 0 && (
        <div className="flex items-center justify-center gap-2">
          <button type="button" disabled={loadingEarlier} onClick={onEarlier} className={buttonClass}>
            {loadingEarlier ? <Spinner className="size-3.5" /> : null}
            Show {counted(earlier, 'earlier entry', 'earlier entries')}
          </button>
        </div>
      )}
      {earlierError !== '' && <ErrorBanner>{earlierError}</ErrorBanner>}
      <ol className="space-y-3">
        {rows.map((row, i) => (
          <TimelineRowItem
            key={rowKey(row)}
            newHere={i === fresh}
            divider={divider}
            row={row}
            repo={issue.repo}
            author={issue.author}
            onQuote={onQuote}
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
}

// TimelineRowItem is one row of the conversation, after the New divider when it is the
// first the user hasn't seen.
function TimelineRowItem({ newHere, divider, row, repo, author, onQuote }: TimelineRowItemProps) {
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
      <li>
        <TimelineEntry row={row} repo={repo} author={author} onQuote={onQuote} />
      </li>
    </>
  )
}
