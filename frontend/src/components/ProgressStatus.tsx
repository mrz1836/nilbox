import type { ReactNode } from 'react'

import { Spinner } from './Spinner'

// ProgressStep is how far some reading has got, in words, with the share of its phase
// done, from 0 to 1, or null for a phase that counts nothing (lib/worktreeProgress and
// lib/inboxProgress word theirs so).
export interface ProgressStep {
  text: string
  share: number | null
}

export interface ProgressStatusProps {
  step: ProgressStep
  // icon stands in for the spinner while the work waits, as a bulk mark paused does, and
  // warning words the step as a warning.
  icon?: ReactNode
  warning?: boolean
}

// ProgressStatus says a reading is under way, whichever started it, and how far it has
// got: a spinner, the phase in words, and a thin bar for a phase that counts what it reads.
// It is a status, so a screen reader reads each new step.
export function ProgressStatus({ step, icon, warning = false }: ProgressStatusProps) {
  return (
    <span role="status" className={`flex min-w-0 items-center gap-2 ${warning ? 'text-warning' : 'text-muted'}`}>
      {icon ?? <Spinner className="size-3.5" />}
      <span className="truncate">{step.text}</span>
      {step.share !== null && (
        <span aria-hidden="true" className="h-1 w-20 shrink-0 overflow-hidden rounded-full bg-border">
          <span
            className="block h-full rounded-full bg-accent transition-[width] duration-200"
            style={{ width: `${String(Math.round(step.share * 100))}%` }}
          />
        </span>
      )}
    </span>
  )
}
