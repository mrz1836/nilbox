import type { ReactNode } from 'react'

import type { Summary } from '../../lib/worktreeSummary'
import { AlertIcon, CheckIcon, XIcon } from '../icons'

export interface ResultCardProps {
  summary: Summary
  onDismiss: () => void
  // children are the notes under the card's line, such as why each worktree was left, set
  // in under the headline; most results have none.
  children?: ReactNode
}

// ResultCard reports what the latest change in the Worktrees view did, on one line: a check
// for a change done in full, or an amber alert, with an amber edge, for one that left
// something; the headline; and the line of detail, cut short to the room left, in full in
// its tooltip; then Dismiss. Notes, when there are any, go under the line. It is announced
// as it appears, and stays until it is dismissed or the next change replaces it.
export function ResultCard({ summary, onDismiss, children }: ResultCardProps) {
  const done = summary.tone === 'success'

  return (
    <section
      role="status"
      aria-label={summary.title}
      className={`flex min-w-0 flex-col rounded-lg border bg-surface py-1 pr-1.5 pl-3 text-[0.8125rem] motion-safe:animate-pop-in ${done ? 'border-border' : 'border-warning/40'}`}
    >
      <div className="flex min-w-0 items-center gap-2">
        {done ? (
          <CheckIcon className="size-4 shrink-0 text-success" />
        ) : (
          <AlertIcon className="size-4 shrink-0 text-warning" />
        )}
        <span title={summary.title} className="min-w-0 truncate font-semibold text-text">
          {summary.title}
        </span>
        <span title={summary.detail} className="min-w-0 flex-1 truncate text-[0.78125rem] text-muted">
          {summary.detail}
        </span>
        <button
          type="button"
          aria-label="Dismiss"
          title="Dismiss"
          onClick={onDismiss}
          className="flex size-6 shrink-0 items-center justify-center rounded-md text-muted transition-colors hover:bg-raised hover:text-text"
        >
          <XIcon className="size-3.5" />
        </button>
      </div>
      {children}
    </section>
  )
}
