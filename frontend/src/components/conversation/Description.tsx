import { formatAgo, formatFull } from '../../lib/time'
import type { TimelineReaction } from '../../platform'
import { PersonAvatar } from '../Avatar'
import { Markdown } from '../Markdown'
import { RelativeTime } from '../RelativeTime'
import { DescriptionSkeleton } from '../review/ReviewLoading'
import { Trimmed } from './TimelineEntry'

export interface DescriptionProps {
  // author opened the item, openedAt says when, and editedAt when the description was last
  // edited; each time '' when not known.
  author: string
  openedAt: string
  editedAt: string
  // body is the description, bodyTrimmed whether the cache cut it short (url, the item's
  // page, has all of it), and reactions how people reacted to it.
  body: string
  bodyTrimmed: boolean
  reactions: readonly TimelineReaction[]
  url: string
  // reading reports the first read from GitHub under way, before which the description is
  // a placeholder, and read that the sheet read the item from GitHub at least once.
  reading: boolean
  read: boolean
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

// Description is an issue's or a pull request's description: who opened it and when,
// whether it was edited, its text, and how people reacted to it.
export function Description({
  author,
  openedAt,
  editedAt,
  body,
  bodyTrimmed,
  reactions,
  url,
  reading,
  read,
}: DescriptionProps) {
  return (
    <article aria-label="Description" className="rounded-md border border-border">
      <header className="flex items-center gap-2 border-b border-border bg-raised/40 px-3 py-1.5 text-xs text-muted">
        <PersonAvatar login={author} />
        <span className="font-medium text-text">{author}</span>
        {openedAt !== '' && (
          <span title={formatFull(openedAt)}>
            opened <RelativeTime iso={openedAt} show={formatAgo} />
          </span>
        )}
        {editedAt !== '' && <span title={formatFull(editedAt)}>· edited</span>}
      </header>
      <div className="px-3 py-2">
        {body.trim() !== '' ? (
          <Markdown text={body} />
        ) : reading ? (
          <DescriptionSkeleton />
        ) : (
          <p className="text-sm text-muted">{read ? 'No description.' : 'Not read from GitHub yet.'}</p>
        )}
        {bodyTrimmed && <Trimmed url={url} />}
        {reactions.length > 0 && (
          <ul aria-label="Reactions" className="mt-2 flex flex-wrap gap-1.5">
            {reactions.map((r) => (
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
