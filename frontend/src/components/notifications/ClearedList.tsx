import { useEffect, useId, useState } from 'react'

import type { Result } from '../../lib/call'
import {
  clearsIn,
  clearURL,
  clearWhere,
  doneURL,
  settledText,
  shownSettled,
  triggerText,
} from '../../lib/githubThreads'
import { formatAgo, formatFull } from '../../lib/time'
import { useClock, useNowAs } from '../../lib/useNow'
import { openExternal, type GitHubThreadsSnapshot, type QueuedClear, type SettledClear } from '../../platform'
import { captionClass, quietButtonClass } from '../controls'
import { ErrorBanner } from '../ErrorBanner'
import { ExternalIcon, IssueIcon, PullRequestIcon } from '../icons'
import { RelativeTime } from '../RelativeTime'
import { PageNote } from './parts'

// tickMs is how often the clock moves on while a clear counts down: its Undo hold is
// seconds long.
const tickMs = 1000

// useTicking moves the window's clock on every second while active, so a countdown reads
// in seconds; the clock reads the time from its own source, which a test holds still.
function useTicking(active: boolean) {
  const clock = useClock()

  useEffect(() => {
    if (!active) {
      return undefined
    }

    const timer = setInterval(() => {
      clock.read()
    }, tickMs)

    return () => {
      clearInterval(timer)
    }
  }, [active, clock])
}

// ClearIcon is a clear's item's kind.
function ClearIcon({ clear }: { clear: QueuedClear | SettledClear }) {
  return clear.kind === 'pr' ? <PullRequestIcon className="size-4" /> : <IssueIcon className="size-4" />
}

// Countdown says when a queued clear goes: "in 12s".
function Countdown({ sendAfter }: { sendAfter: string }) {
  const text = useNowAs((now) => clearsIn(now, sendAfter))

  return <span className="text-xs text-muted tabular-nums">{text}</span>
}

interface QueuedRowProps {
  clear: QueuedClear
  keeping: boolean
  onUndo: () => void
}

// QueuedRow is a clear waiting to be sent: its item, why it clears, when it goes, and its
// Undo, which keeps the notification.
function QueuedRow({ clear, keeping, onUndo }: QueuedRowProps) {
  return (
    <li className="flex items-start gap-3 rounded-md px-3 py-2.5">
      <span className="mt-0.5 shrink-0 text-muted">
        <ClearIcon clear={clear} />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block truncate text-sm font-medium">{clear.title}</span>
        <span className="block truncate text-xs text-muted">
          {clearWhere(clear)} · {triggerText(clear.trigger)}
        </span>
      </span>
      <span className="flex shrink-0 items-center gap-2">
        <Countdown sendAfter={clear.sendAfter} />
        <button
          type="button"
          disabled={keeping}
          aria-busy={keeping || undefined}
          onClick={onUndo}
          aria-label={`Undo: ${clear.title}`}
          className={quietButtonClass}
        >
          {keeping ? 'Keeping…' : 'Undo'}
        </button>
      </span>
    </li>
  )
}

// SettledRow is a clear that left the queue: its item, why it was chosen, and how it
// settled. A click opens the item on GitHub.
function SettledRow({ clear }: { clear: SettledClear }) {
  const outcome = settledText(clear)
  const failed = clear.state === 'failed'

  return (
    <li>
      <button
        type="button"
        onClick={() => {
          openExternal(clearURL(clear))
        }}
        title="Open on GitHub"
        className="flex w-full items-start gap-3 rounded-md px-3 py-2.5 text-left transition-colors hover:bg-raised focus-visible:bg-raised focus-visible:outline-none"
      >
        <span className="mt-0.5 shrink-0 text-muted">
          <ClearIcon clear={clear} />
        </span>
        <span className="min-w-0 flex-1">
          <span className="flex items-baseline justify-between gap-3">
            <span className={`truncate text-xs font-medium ${failed ? 'text-danger' : 'text-muted'}`}>{outcome}</span>
            <span title={formatFull(clear.at)} className="shrink-0">
              <RelativeTime iso={clear.at} show={formatAgo} className="text-[0.6875rem] text-muted tabular-nums" />
            </span>
          </span>
          <span className="mt-0.5 block truncate text-sm">{clear.title}</span>
          <span className="block truncate text-xs text-muted">
            {clearWhere(clear)} · {triggerText(clear.trigger)}
          </span>
        </span>
      </button>
    </li>
  )
}

export interface ClearedListProps {
  // queued are the clears waiting to be sent, and recent the latest that settled, as the
  // mirror's Snapshot holds them.
  queued: readonly QueuedClear[]
  recent: readonly SettledClear[]
  // keep takes queued clears back, the Undo.
  keep: (clearIDs: readonly number[]) => Promise<Result<GitHubThreadsSnapshot>>
}

// ClearedList is the GitHub tab's Cleared view: what automatic clearing, or the user's own
// clear, is about to do, each with a countdown and Undo, and what it did lately, each with
// how it ended. A skip the backend decided again says nothing that happened, so it isn't
// listed. GitHub's Done view lists every notification marked done.
export function ClearedList({ queued, recent, keep }: ClearedListProps) {
  const id = useId()
  const [keeping, setKeeping] = useState<ReadonlySet<number>>(new Set())
  const [error, setError] = useState('')
  const settled = recent.filter(shownSettled)

  useTicking(queued.length > 0)

  const undo = async (clearID: number) => {
    setError('')
    setKeeping((was) => new Set([...was, clearID]))

    const result = await keep([clearID])

    setKeeping((was) => new Set([...was].filter((other) => other !== clearID)))

    if (!result.ok) {
      setError(result.error)
    }
  }

  return (
    <div className="flex flex-col gap-4 pt-3">
      {error !== '' && <ErrorBanner className="mx-2">Could not undo: {error}</ErrorBanner>}
      {queued.length > 0 && (
        <section aria-labelledby={`${id}-soon`}>
          <h3 id={`${id}-soon`} className={`${captionClass} px-3 pb-1`}>
            Clearing soon
          </h3>
          <ul aria-label="Clearing soon" className="flex flex-col gap-0.5">
            {queued.map((clear) => (
              <QueuedRow
                key={clear.id}
                clear={clear}
                keeping={keeping.has(clear.id)}
                onUndo={() => {
                  void undo(clear.id)
                }}
              />
            ))}
          </ul>
        </section>
      )}
      <section aria-labelledby={`${id}-recent`}>
        <div className="flex items-center justify-between gap-2 px-3 pb-1">
          <h3 id={`${id}-recent`} className={captionClass}>
            Recently cleared
          </h3>
          <button
            type="button"
            onClick={() => {
              openExternal(doneURL)
            }}
            className={quietButtonClass}
          >
            <ExternalIcon className="size-3.5" />
            GitHub’s Done view
          </button>
        </div>
        {settled.length === 0 ? (
          <PageNote title="Nothing cleared lately">
            Notifications cleared once their work is done, or by your hand, show here.
          </PageNote>
        ) : (
          <ul aria-label="Recently cleared" className="flex flex-col gap-0.5">
            {settled.map((clear) => (
              <SettledRow key={clear.id} clear={clear} />
            ))}
          </ul>
        )}
      </section>
    </div>
  )
}
