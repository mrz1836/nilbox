import type { ReactNode } from 'react'

import type { Summary } from '../../lib/worktreeSummary'
import { AlertIcon, CircleCheckIcon, XIcon } from '../icons'

// cardTones are the border, wash, and icon of a result: green for a change done in full,
// amber for one that left something, as the notes under it say.
const cardTones = {
  success: { card: 'border-success/40 bg-success/8', icon: 'text-success' },
  warning: { card: 'border-warning/45 bg-warning/8', icon: 'text-warning' },
} as const

export interface ResultCardProps {
  summary: Summary
  onDismiss: () => void
  // children are the notes under the summary, such as why each worktree was left.
  children?: ReactNode
}

// ResultCard reports what the latest change in the Worktrees view did: a headline, a line
// of detail, and any notes, marked done or done in part, until it is dismissed or the next
// change replaces it. It is announced as it appears.
export function ResultCard({ summary, onDismiss, children }: ResultCardProps) {
  const tone = cardTones[summary.tone === 'success' ? 'success' : 'warning']
  const Icon = summary.tone === 'success' ? CircleCheckIcon : AlertIcon

  return (
    <section
      role="status"
      aria-label={summary.title}
      className={`flex items-start gap-3 rounded-lg border px-4 py-3 text-sm motion-safe:animate-pop-in ${tone.card}`}
    >
      <Icon className={`mt-0.5 size-5 shrink-0 ${tone.icon}`} />
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <p className="font-semibold text-text">{summary.title}</p>
        {summary.detail !== '' && <p className="text-muted">{summary.detail}</p>}
        {children}
      </div>
      <button
        type="button"
        aria-label="Dismiss"
        title="Dismiss"
        onClick={onDismiss}
        className="-mt-1 -mr-2 flex size-7 shrink-0 items-center justify-center rounded-md text-muted transition-colors hover:bg-raised hover:text-text"
      >
        <XIcon className="size-3.5" />
      </button>
    </section>
  )
}
