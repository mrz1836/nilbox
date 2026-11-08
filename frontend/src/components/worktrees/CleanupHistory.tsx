import { useId, useState, type Ref } from 'react'

import { formatAgo, formatFull } from '../../lib/time'
import { openExternal, type LinkedItem, type WorktreeCleanup } from '../../platform'
import { quietButtonClass } from '../controls'
import { CircleCheckIcon, IssueIcon, PullRequestIcon } from '../icons'

// historyPreview is how many cleanups the history shows before "Show more".
const historyPreview = 5

// itemNoun names an item's kind in a sentence.
function itemNoun(item: LinkedItem): string {
  return item.kind === 'pr' ? 'pull request' : 'issue'
}

// CleanupSummary says how a worktree was cleaned up, what went with it, and the commit it
// was at, which is on GitHub.
function CleanupSummary({ cleanup }: { cleanup: WorktreeCleanup }) {
  const who = cleanup.automatic ? 'Cleaned up automatically' : 'Cleaned up by you'
  const after = cleanup.item === null ? '' : ` after the ${itemNoun(cleanup.item)} ${cleanup.item.state}`

  return (
    <p className="text-xs text-muted">
      {who}
      {after}
      {' · '}
      {cleanup.deletedBranch === '' ? (
        'its branch was kept'
      ) : (
        <>
          deleted <span className="font-medium text-text">{cleanup.deletedBranch}</span>
        </>
      )}
      {cleanup.head !== '' && (
        <>
          {' · was at '}
          <code title={cleanup.head} className="font-mono">
            {cleanup.head.slice(0, 7)}
          </code>
        </>
      )}
    </p>
  )
}

// CleanupRow is one cleaned-up worktree: where it was, what it was for, and when it went.
function CleanupRow({ cleanup, now }: { cleanup: WorktreeCleanup; now: Date }) {
  const { item } = cleanup
  // KindIcon marks what the worktree was for, in the color of work that is done.
  const KindIcon = item === null ? CircleCheckIcon : item.kind === 'pr' ? PullRequestIcon : IssueIcon

  return (
    <li className="flex items-start gap-2.5 border-b border-border px-3 py-2.5 last:border-b-0">
      <KindIcon className="mt-0.5 size-4 shrink-0 text-success" />
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <p className="flex min-w-0 items-baseline gap-2 text-sm">
          <span className="shrink-0 font-medium">{cleanup.repo}</span>
          <span className="shrink-0 text-muted" title={cleanup.displayPath}>
            {cleanup.worktree}
          </span>
          {item !== null && (
            <span className="min-w-0 truncate text-muted">
              <button
                type="button"
                title={`${item.owner}/${item.name}#${String(item.number)}`}
                onClick={() => {
                  openExternal(item.url)
                }}
                className="font-medium text-text hover:underline"
              >
                {item.kind === 'pr' ? 'PR' : 'Issue'} #{item.number}
              </button>{' '}
              {item.title}
            </span>
          )}
        </p>
        <CleanupSummary cleanup={cleanup} />
      </div>
      <time
        dateTime={cleanup.cleanedAt}
        title={formatFull(cleanup.cleanedAt)}
        className="shrink-0 pt-0.5 text-xs text-muted"
      >
        {formatAgo(now, cleanup.cleanedAt)}
      </time>
    </li>
  )
}

export interface CleanupHistoryProps {
  cleanups: readonly WorktreeCleanup[]
  now: Date
  disabled: boolean
  error: string
  onClear: () => void
  sectionRef: Ref<HTMLElement>
}

// CleanupHistory lists the worktrees cleaned up most recently, by hand or automatically,
// so a cleanup whose toast went by unseen can still be found. It shows the newest few, and
// the rest on request.
export function CleanupHistory({ cleanups, now, disabled, error, onClear, sectionRef }: CleanupHistoryProps) {
  const headingId = useId()
  const [expanded, setExpanded] = useState(false)
  const shown = expanded ? cleanups : cleanups.slice(0, historyPreview)
  const more = cleanups.length - historyPreview

  return (
    <section
      ref={sectionRef}
      aria-labelledby={headingId}
      className="scroll-mt-4 rounded-lg border border-border bg-surface"
    >
      <header className="flex items-center gap-3 border-b border-border px-3 py-2">
        <h2 id={headingId} className="text-sm font-semibold">
          Recently cleaned up
        </h2>
        <span className="text-xs text-muted">Everything in them was on GitHub</span>
        <button type="button" disabled={disabled} onClick={onClear} className={`ml-auto ${quietButtonClass}`}>
          Clear
        </button>
      </header>
      {error !== '' && (
        <p role="alert" className="px-3 py-2 text-sm text-danger">
          {error}
        </p>
      )}
      <ul>
        {shown.map((c) => (
          <CleanupRow key={c.id} cleanup={c} now={now} />
        ))}
      </ul>
      {more > 0 && (
        <div className="border-t border-border px-1.5 py-1">
          <button
            type="button"
            aria-expanded={expanded}
            onClick={() => {
              setExpanded(!expanded)
            }}
            className={quietButtonClass}
          >
            {expanded ? 'Show fewer' : `Show ${String(more)} more`}
          </button>
        </div>
      )}
    </section>
  )
}
