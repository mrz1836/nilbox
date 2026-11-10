import { CheckIcon } from '../icons'
import { Spinner } from '../Spinner'

export interface ActionProgressProps {
  // title says what the action is, as in "Approve & squash-merge acme-org/api#212".
  title: string
  // steps are what the action did so far, in order, as the backend reported them; the last
  // is the one it is doing now.
  steps: readonly string[]
}

// ActionProgress covers the review sheet while an action runs: the sheet behind it is dimmed
// and takes no input, and a panel says what the action is, ticks off each step GitHub
// finished, and shows the one under way, so it is clear the sheet is waiting for GitHub. It
// announces each step to assistive technology as it comes.
export function ActionProgress({ title, steps }: ActionProgressProps) {
  const shown = steps.length === 0 ? ['Starting'] : steps

  return (
    <div className="absolute inset-0 z-10 flex items-center justify-center bg-surface/75 backdrop-blur-[2px]">
      <div
        role="status"
        aria-live="polite"
        className="w-[min(28rem,calc(100%-2rem))] rounded-lg border border-border bg-surface p-5 shadow-xl"
      >
        <div className="flex items-center gap-3">
          <Spinner className="size-5" />
          <h3 className="min-w-0 text-sm font-semibold">{title}</h3>
        </div>
        <ol aria-label="Steps" className="mt-4 space-y-2 text-sm">
          {shown.map((step, i) => {
            const current = i === shown.length - 1

            return (
              <li
                key={`${String(i)}-${step}`}
                aria-current={current ? 'step' : undefined}
                className="flex items-center gap-2.5"
              >
                {current ? (
                  <Spinner className="size-3.5" />
                ) : (
                  <CheckIcon aria-hidden="true" className="size-3.5 shrink-0 text-success" />
                )}
                <span className={current ? 'font-medium text-text' : 'text-muted'}>
                  {step}
                  {current && '…'}
                </span>
              </li>
            )
          })}
        </ol>
        <p className="mt-4 text-xs text-muted">
          Waiting for GitHub. This usually takes a few seconds, and the sheet closes once it’s done.
        </p>
      </div>
    </div>
  )
}
