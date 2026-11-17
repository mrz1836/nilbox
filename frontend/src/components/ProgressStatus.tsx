import { Spinner } from './Spinner'

// ProgressStep is how far some reading has got, in words, with the share of its phase
// done, from 0 to 1, or null for a phase that counts nothing (lib/worktreeProgress and
// lib/inboxProgress word theirs so).
export interface ProgressStep {
  text: string
  share: number | null
}

// ProgressStatus says a reading is under way, whichever started it, and how far it has
// got: a spinner, the phase in words, and a thin bar for a phase that counts what it reads.
// It is a status, so a screen reader reads each new step.
export function ProgressStatus({ step }: { step: ProgressStep }) {
  return (
    <span role="status" className="flex min-w-0 items-center gap-2 text-muted">
      <Spinner className="size-3.5" />
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
