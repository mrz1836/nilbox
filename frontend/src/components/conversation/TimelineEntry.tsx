import { memo, type ReactNode } from 'react'

import { counted } from '../../lib/format'
import { toneText, type Tone } from '../../lib/signals'
import type { LabelRow, TimelineRow } from '../../lib/timeline'
import { formatAgo, formatFull } from '../../lib/time'
import {
  openExternal,
  type IssueStateReason,
  type TimelineEntry as Entry,
  type TimelineLineComment,
  type TimelineReviewState,
} from '../../platform'
import { PersonAvatar } from '../Avatar'
import { quietButtonClass } from '../controls'
import { ExternalIcon } from '../icons'
import { ItemLink } from '../ItemLink'
import { LabelChip } from '../ItemSignals'
import { Markdown } from '../Markdown'
import { RelativeTime } from '../RelativeTime'

export interface TimelineEntryProps {
  row: TimelineRow
  // repo is the item's repository, which names the items its entries link to, and author
  // who opened it, whose comments say so.
  repo: string
  author: string
  // onQuote quotes a comment's text into the user's draft.
  onQuote: (text: string) => void
  // onJump shows the file a review's line comment is on in the sheet's diff; without it,
  // the file is named only.
  onJump?: ((path: string) => void) | undefined
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

// OpenOnGitHub is the small button that opens an entry on GitHub, named by what it is.
function OpenOnGitHub({ url, what }: { url: string; what: string }) {
  if (url === '') {
    return null
  }

  const label = `Open the ${what} on GitHub`

  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      onClick={() => {
        openExternal(url)
      }}
      className={`${quietButtonClass} p-1`}
    >
      <ExternalIcon className="size-3" />
    </button>
  )
}

// QuoteButton quotes text into the user's reply.
function QuoteButton({ text, onQuote }: { text: string; onQuote: (t: string) => void }) {
  return (
    <button
      type="button"
      onClick={() => {
        onQuote(text)
      }}
      className={quietButtonClass}
    >
      Quote reply
    </button>
  )
}

// Who is a comment's or a review's author in its header: the picture, the name, and whether
// they opened the item or are the user.
function Who({ item, author }: { item: Entry; author: string }) {
  return (
    <>
      <PersonAvatar login={item.actor} />
      <span className="font-medium text-text">{someone(item.actor)}</span>
      {item.actor !== '' && item.actor === author && <span className={tagClass}>Author</span>}
      {item.mine && <span className={tagClass}>You</span>}
    </>
  )
}

// Body is a comment's or a review's text, marked when the cache cut it short.
function Body({ item }: { item: Entry }) {
  return (
    <>
      <Markdown text={item.body} />
      {item.trimmed && <Trimmed url={item.url} />}
    </>
  )
}

// CommentEntry is a person's comment, with who wrote it and when, whether it was edited, its
// text, and buttons to quote it in the user's reply and to open it on GitHub. A bot's is
// folded (FoldedEntry).
function CommentEntry({ item, author, onQuote }: { item: Entry; author: string; onQuote: (t: string) => void }) {
  return (
    <article aria-label={`${someone(item.actor)}’s comment`} className="rounded-md border border-border">
      <header className="flex items-center gap-2 border-b border-border bg-raised/40 px-3 py-1.5 text-xs text-muted">
        <Who item={item} author={author} />
        <When at={item.at} />
        {item.editedAt !== '' && <span title={formatFull(item.editedAt)}>· edited</span>}
        <span className="ml-auto flex items-center gap-1">
          <QuoteButton text={item.body} onQuote={onQuote} />
          <OpenOnGitHub url={item.url} what="comment" />
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

// firstLine is a text's first line that says something, for a folded entry's summary.
function firstLine(text: string): string {
  return text.split('\n').find((line) => line.trim() !== '') ?? ''
}

// reviewTones is each review state's color: an approval is good news, a request for changes
// an alarm, and the rest plain.
const reviewTones: Readonly<Record<TimelineReviewState, Tone>> = {
  '': 'muted',
  APPROVED: 'success',
  CHANGES_REQUESTED: 'danger',
  COMMENTED: 'muted',
  DISMISSED: 'muted',
}

// reviewChips names each review state on its chip.
const reviewChips: Readonly<Record<TimelineReviewState, string>> = {
  '': 'Reviewed',
  APPROVED: 'Approved',
  CHANGES_REQUESTED: 'Changes requested',
  COMMENTED: 'Commented',
  DISMISSED: 'Dismissed',
}

// reviewDid says what a review that holds no words did, after who left it.
const reviewDid: Readonly<Record<TimelineReviewState, string>> = {
  '': 'reviewed',
  APPROVED: 'approved these changes',
  CHANGES_REQUESTED: 'requested changes',
  COMMENTED: 'reviewed',
  DISMISSED: 'left a review, since dismissed',
}

// says reports a review that holds words: its text, or a comment on a line.
function says(item: Entry): boolean {
  return item.body.trim() !== '' || item.lineComments.length > 0 || item.moreLineComments > 0
}

// FoldedEntry is an entry folded away: a comment someone hid, which says why, or a bot's
// comment or review, which shows its first line. Opening it shows the whole.
function FoldedEntry({ item, onJump }: { item: Entry; onJump?: ((path: string) => void) | undefined }) {
  const who = someone(item.actor)
  const reason = item.minimizedReason.toLowerCase().replaceAll('_', ' ')
  const review = item.kind === 'reviewed'
  const what = review ? 'review' : 'comment'
  const lead = firstLine(item.body)
  const state = review ? ` · ${reviewChips[item.reviewState].toLowerCase()}` : ''
  const summary = item.minimized
    ? `${who}’s ${what} was hidden${reason === '' ? '' : ` as ${reason}`}`
    : `${who} · bot${state} · ${lead}`

  return (
    <details aria-label={`${who}’s ${what}`} className="group rounded-md border border-border">
      <summary className="flex cursor-pointer items-center gap-2 px-3 py-1.5 text-xs text-muted">
        <span className="min-w-0 truncate">{summary}</span>
        <span className="shrink-0">
          · <When at={item.at} />
        </span>
      </summary>
      <div className="space-y-2 border-t border-border px-3 py-2">
        {item.body.trim() !== '' && <Body item={item} />}
        {review && <LineComments item={item} onJump={onJump} />}
      </div>
    </details>
  )
}

// hunkTint is a diff line's tint, as the sheet's diff shows it.
function hunkTint(line: string): string {
  if (line.startsWith('+')) {
    return 'bg-success/12'
  }

  return line.startsWith('-') ? 'bg-danger/12' : ''
}

// whereOf names a line comment's place, as in "src/cache.go:12" or "src/cache.go:10–12".
function whereOf(c: TimelineLineComment): string {
  if (c.line === 0) {
    return c.path
  }

  const lines = c.startLine > 0 && c.startLine !== c.line ? `${String(c.startLine)}–${String(c.line)}` : String(c.line)

  return `${c.path}:${lines}`
}

// LineCommentEntry is one of a review's comments on the diff: where it is, a button that
// shows that file in the diff, whether the lines have changed since, the end of the diff it
// was left on, and its text.
function LineCommentEntry({
  comment,
  onJump,
}: {
  comment: TimelineLineComment
  onJump?: ((path: string) => void) | undefined
}) {
  const where = whereOf(comment)

  return (
    <li className="rounded-md border border-border">
      <p className="flex min-w-0 items-center gap-2 border-b border-border bg-raised/30 px-2 py-1 text-xs text-muted">
        {onJump === undefined ? (
          <span className="min-w-0 truncate font-mono">{where}</span>
        ) : (
          <button
            type="button"
            title={`Show ${comment.path} in Files`}
            onClick={() => {
              onJump(comment.path)
            }}
            className="min-w-0 truncate font-mono text-accent hover:underline"
          >
            {where}
          </button>
        )}
        {comment.outdated && <span className={tagClass}>Outdated</span>}
        <span className="ml-auto shrink-0">
          <OpenOnGitHub url={comment.url} what="line comment" />
        </span>
      </p>
      {comment.hunk.length > 0 && (
        <div
          aria-label={`The diff at ${where}`}
          className="overflow-x-auto border-b border-border font-mono text-[0.75rem] leading-5"
        >
          {comment.hunk.map((line, i) => (
            <div key={i} className={`min-w-max px-2 whitespace-pre ${hunkTint(line)}`}>
              {line === '' ? ' ' : line}
            </div>
          ))}
        </div>
      )}
      <div className="px-2 py-1.5">
        {comment.body.trim() === '' ? (
          <p className="text-sm text-muted italic">No text.</p>
        ) : (
          <Markdown text={comment.body} />
        )}
        {comment.trimmed && <Trimmed url={comment.url} />}
      </div>
    </li>
  )
}

// LineComments are a review's comments on the diff, and how many more GitHub holds.
function LineComments({ item, onJump }: { item: Entry; onJump?: ((path: string) => void) | undefined }) {
  if (item.lineComments.length === 0 && item.moreLineComments === 0) {
    return null
  }

  return (
    <>
      {item.lineComments.length > 0 && (
        <ul aria-label="Line comments" className="space-y-2">
          {item.lineComments.map((c) => (
            <LineCommentEntry key={c.id} comment={c} onJump={onJump} />
          ))}
        </ul>
      )}
      {item.moreLineComments > 0 && (
        <p className="text-xs text-muted">
          and {counted(item.moreLineComments, 'more line comment', 'more line comments')}{' '}
          {item.url === '' ? (
            'on GitHub'
          ) : (
            <button
              type="button"
              onClick={() => {
                openExternal(item.url)
              }}
              className="text-accent hover:underline"
            >
              on GitHub
            </button>
          )}
        </p>
      )}
    </>
  )
}

// ReviewEntry is a person's review that says something: who left it and when, its state in
// its color, whether it was of an earlier commit, its text, and its comments on the diff.
function ReviewEntry({
  item,
  author,
  onQuote,
  onJump,
}: {
  item: Entry
  author: string
  onQuote: (t: string) => void
  onJump?: ((path: string) => void) | undefined
}) {
  const text = item.body.trim() !== ''

  return (
    <article aria-label={`${someone(item.actor)}’s review`} className="rounded-md border border-border">
      <header className="flex flex-wrap items-center gap-2 border-b border-border bg-raised/40 px-3 py-1.5 text-xs text-muted">
        <Who item={item} author={author} />
        <span className={`${tagClass} font-medium ${toneText[reviewTones[item.reviewState]]}`}>
          {reviewChips[item.reviewState]}
        </span>
        {item.outdated && <span className={tagClass}>on an earlier commit</span>}
        <When at={item.at} />
        {item.editedAt !== '' && <span title={formatFull(item.editedAt)}>· edited</span>}
        <span className="ml-auto flex items-center gap-1">
          {text && <QuoteButton text={item.body} onQuote={onQuote} />}
          <OpenOnGitHub url={item.url} what="review" />
        </span>
      </header>
      <div className="space-y-2 px-3 py-2">
        {text && <Body item={item} />}
        <LineComments item={item} onJump={onJump} />
      </div>
    </article>
  )
}

// closedAs says what a close closed the item as.
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

// pullEventWords says what an event only a pull request has did, after who did it.
function pullEventWords(item: Entry): ReactNode {
  switch (item.kind) {
    case 'merged':
      return [
        'merged',
        item.commit === '' ? 'this' : `commit ${item.commit.slice(0, 7)}`,
        item.branch === '' ? '' : `into ${item.branch}`,
      ]
        .filter((s) => s !== '')
        .join(' ')
    case 'force_pushed':
      return item.from !== '' && item.to !== ''
        ? `force-pushed the branch from ${item.from.slice(0, 7)} to ${item.to.slice(0, 7)}`
        : 'force-pushed the branch'
    case 'review_requested':
      return item.reviewer === '' ? 'requested a review' : `requested a review from ${item.reviewer}`
    case 'ready_for_review':
      return 'marked this ready for review'
    default:
      return 'marked this as a draft'
  }
}

// eventWords says what an event did, after who did it.
function eventWords(item: Entry, repo: string): ReactNode {
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
    case 'labeled':
      return 'added a label'
    case 'unlabeled':
      return 'removed a label'
    case 'reviewed':
      return (
        <>
          {reviewDid[item.reviewState]}
          {item.outdated && ' on an earlier commit'}
        </>
      )
    default:
      return pullEventWords(item)
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
// a bot wrote it; a review, with its comments on the diff, folded when a bot left it, or on
// a line when it says nothing; or an event on a line. It renders again only when its own
// entry changes, so typing a reply never parses every comment again.
export const TimelineEntry = memo(function TimelineEntry({ row, repo, author, onQuote, onJump }: TimelineEntryProps) {
  if (row.kind === 'labels') {
    return <Labels row={row} />
  }

  const { item } = row
  const written = item.kind === 'comment' || (item.kind === 'reviewed' && says(item))

  if (!written) {
    return (
      <EventLine actor={item.actor} isBot={item.actorIsBot} at={item.at}>
        {eventWords(item, repo)}
      </EventLine>
    )
  }

  if (item.minimized || item.actorIsBot) {
    return <FoldedEntry item={item} onJump={onJump} />
  }

  return item.kind === 'reviewed' ? (
    <ReviewEntry item={item} author={author} onQuote={onQuote} onJump={onJump} />
  ) : (
    <CommentEntry item={item} author={author} onQuote={onQuote} />
  )
})
