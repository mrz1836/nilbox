import { memo, type ReactNode } from 'react'

import type { LabelRow, TimelineRow } from '../../lib/issues'
import { formatAgo, formatFull } from '../../lib/time'
import { openExternal, type IssueStateReason, type IssueTimelineItem } from '../../platform'
import { PersonAvatar } from '../Avatar'
import { quietButtonClass } from '../controls'
import { ExternalIcon } from '../icons'
import { ItemLink } from '../ItemLink'
import { LabelChip } from '../ItemSignals'
import { Markdown } from '../Markdown'
import { RelativeTime } from '../RelativeTime'

export interface TimelineEntryProps {
  row: TimelineRow
  // repo is the issue's repository, which names the items its entries link to, and author
  // who opened it, whose comments say so.
  repo: string
  author: string
  // onQuote quotes a comment's text into the user's draft.
  onQuote: (text: string) => void
}

// tagClass is a comment's small tags.
const tagClass = 'rounded-full border border-border px-1.5 py-px text-[0.6875rem]'

// someone names an actor GitHub no longer names, as for a deleted account.
function someone(actor: string): string {
  return actor === '' ? 'ghost' : actor
}

// When says when an entry was made, as how long ago, with the full time on hover.
function When({ at }: { at: string }) {
  return (
    <span title={formatFull(at)}>
      <RelativeTime iso={at} show={formatAgo} />
    </span>
  )
}

// Trimmed says a text was cut short to keep, and opens the whole on GitHub.
export function Trimmed({ url }: { url: string }) {
  return (
    <p className="mt-2 text-xs text-muted">
      This is long, so only its start is kept here.{' '}
      {url !== '' && (
        <button
          type="button"
          onClick={() => {
            openExternal(url)
          }}
          className="text-accent hover:underline"
        >
          Read it all on GitHub
        </button>
      )}
    </p>
  )
}

// CommentEntry is a person's comment, with who wrote it and when, whether it was edited, its
// text, and buttons to quote it in the user's reply and to open it on GitHub. A bot's is
// folded (FoldedComment).
function CommentEntry({
  item,
  author,
  onQuote,
}: {
  item: IssueTimelineItem
  author: string
  onQuote: (t: string) => void
}) {
  const who = someone(item.actor)

  return (
    <article aria-label={`${who}’s comment`} className="rounded-md border border-border">
      <header className="flex items-center gap-2 border-b border-border bg-raised/40 px-3 py-1.5 text-xs text-muted">
        <PersonAvatar login={item.actor} />
        <span className="font-medium text-text">{who}</span>
        {item.actor !== '' && item.actor === author && <span className={tagClass}>Author</span>}
        {item.mine && <span className={tagClass}>You</span>}
        <When at={item.at} />
        {item.editedAt !== '' && <span title={formatFull(item.editedAt)}>· edited</span>}
        <span className="ml-auto flex items-center gap-1">
          <button
            type="button"
            onClick={() => {
              onQuote(item.body)
            }}
            className={quietButtonClass}
          >
            Quote reply
          </button>
          {item.url !== '' && (
            <button
              type="button"
              aria-label="Open the comment on GitHub"
              title="Open the comment on GitHub"
              onClick={() => {
                openExternal(item.url)
              }}
              className={`${quietButtonClass} p-1`}
            >
              <ExternalIcon className="size-3" />
            </button>
          )}
        </span>
      </header>
      <div className="px-3 py-2">
        {item.body.trim() === '' ? (
          <p className="text-sm text-muted italic">No text.</p>
        ) : (
          <Markdown text={item.body} />
        )}
        {item.trimmed && <Trimmed url={item.url} />}
      </div>
    </article>
  )
}

// firstLine is a text's first line that says something, for a folded comment's summary.
function firstLine(text: string): string {
  return text.split('\n').find((line) => line.trim() !== '') ?? ''
}

// FoldedComment is a comment folded away: one someone hid, which says why, or a bot's,
// which shows its first line. Opening it shows the whole.
function FoldedComment({ item }: { item: IssueTimelineItem }) {
  const who = someone(item.actor)
  const reason = item.minimizedReason.toLowerCase().replaceAll('_', ' ')
  const summary = item.minimized
    ? `${who}’s comment was hidden${reason === '' ? '' : ` as ${reason}`}`
    : `${who} · bot · ${firstLine(item.body)}`

  return (
    <details aria-label={`${who}’s comment`} className="group rounded-md border border-border">
      <summary className="flex cursor-pointer items-center gap-2 px-3 py-1.5 text-xs text-muted">
        <span className="min-w-0 truncate">{summary}</span>
        <span className="shrink-0">
          · <When at={item.at} />
        </span>
      </summary>
      <div className="border-t border-border px-3 py-2">
        <Markdown text={item.body} />
        {item.trimmed && <Trimmed url={item.url} />}
      </div>
    </details>
  )
}

// closedAs says what a close closed the issue as.
function closedAs(reason: IssueStateReason): string {
  switch (reason) {
    case 'NOT_PLANNED':
      return ' as not planned'
    case 'DUPLICATE':
      return ' as a duplicate'
    case 'COMPLETED':
      return ' as completed'
    default:
      return ''
  }
}

// eventWords says what an event did, after who did it.
function eventWords(item: IssueTimelineItem, repo: string): ReactNode {
  const ref = item.ref === null ? null : <ItemLink linked={item.ref} from={repo} titled />

  switch (item.kind) {
    case 'mentioned':
      return (
        <>
          mentioned this in {ref}
          {item.willClose && ', which closes it once merged'}
        </>
      )
    case 'linked':
      return <>linked {ref}</>
    case 'unlinked':
      return <>unlinked {ref}</>
    case 'closed':
      return (
        <>
          closed this{closedAs(item.stateReason)}
          {item.duplicateOf !== null && (
            <>
              {' '}
              of <ItemLink linked={item.duplicateOf} from={repo} />
            </>
          )}
          {ref !== null ? <> in {ref}</> : item.commit !== '' && ` in commit ${item.commit.slice(0, 7)}`}
        </>
      )
    case 'reopened':
      return 'reopened this'
    case 'assigned':
      return item.assignee === item.actor ? 'self-assigned this' : `assigned ${someone(item.assignee)}`
    case 'unassigned':
      return item.assignee === item.actor ? 'removed their assignment' : `unassigned ${someone(item.assignee)}`
    case 'renamed':
      return `changed the title from “${item.from}” to “${item.to}”`
    case 'duplicate':
      return <>marked this and {ref} as duplicates</>
    default:
      return item.kind === 'labeled' ? 'added a label' : 'removed a label'
  }
}

// EventLine is one event, on a line: who did it, what, and when.
function EventLine({ actor, isBot, at, children }: { actor: string; isBot: boolean; at: string; children: ReactNode }) {
  return (
    <p className="flex flex-wrap items-center gap-x-1.5 gap-y-1 px-3 text-xs text-muted">
      <span className="font-medium text-text">{someone(actor)}</span>
      {isBot && <span className={tagClass}>bot</span>}
      {children}
      <span>
        · <When at={at} />
      </span>
    </p>
  )
}

// Labels is the labels one person added and removed in one minute, on one line.
function Labels({ row }: { row: LabelRow }) {
  return (
    <EventLine actor={row.actor} isBot={row.actorIsBot} at={row.at}>
      {row.added.length > 0 && (
        <>
          added
          {row.added.map((l) => (
            <LabelChip key={l.name} label={l} />
          ))}
        </>
      )}
      {row.added.length > 0 && row.removed.length > 0 && 'and'}
      {row.removed.length > 0 && (
        <>
          removed
          {row.removed.map((l) => (
            <LabelChip key={l.name} label={l} />
          ))}
        </>
      )}
    </EventLine>
  )
}

// TimelineEntry is one entry of the conversation: a comment, folded when someone hid it or
// a bot wrote it, or an event on a line. It renders again only when its own entry changes,
// so typing a reply never parses every comment again.
export const TimelineEntry = memo(function TimelineEntry({ row, repo, author, onQuote }: TimelineEntryProps) {
  if (row.kind === 'labels') {
    return <Labels row={row} />
  }

  const { item } = row

  if (item.kind !== 'comment') {
    return (
      <EventLine actor={item.actor} isBot={item.actorIsBot} at={item.at}>
        {eventWords(item, repo)}
      </EventLine>
    )
  }

  return item.minimized || item.actorIsBot ? (
    <FoldedComment item={item} />
  ) : (
    <CommentEntry item={item} author={author} onQuote={onQuote} />
  )
})
