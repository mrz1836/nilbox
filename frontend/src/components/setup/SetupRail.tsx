import { stepInfo, stepStatus, type StepStatus } from '../../lib/setup'
import type { SetupStep } from '../../platform'
import { AlertIcon, BellIcon, BoxIcon, CheckIcon, FolderIcon, GitHubMarkIcon, TerminalIcon } from '../icons'

// stepIcons are each numbered step's icon on the rail.
const stepIcons: Readonly<Partial<Record<SetupStep, typeof CheckIcon>>> = {
  github: GitHubMarkIcon,
  clones: FolderIcon,
  agents: TerminalIcon,
  notifications: BellIcon,
  tools: BoxIcon,
}

// statusWords says a step's status for assistive technology, after its name.
const statusWords: Readonly<Record<StepStatus, string>> = {
  done: 'done',
  current: 'current step',
  upcoming: 'not yet',
  attention: 'needs attention',
  suggested: 'suggested',
}

export interface SetupRailProps {
  // steps are the numbered steps, in order.
  steps: readonly SetupStep[]
  current: SetupStep
  // reached are the steps the user has been on, each a way back; attention those that
  // need it.
  reached: ReadonlySet<SetupStep>
  attention: ReadonlySet<SetupStep>
  onGo: (step: SetupStep) => void
}

// Badge is a step's mark on the rail: a tick popping in once it is done, its icon while it
// is current or ahead, and an amber dot on a reached step that needs attention.
function Badge({ step, status }: { step: SetupStep; status: StepStatus }) {
  const Icon = stepIcons[step] ?? CheckIcon

  if (status === 'done') {
    return (
      <span className="flex size-7 items-center justify-center rounded-full bg-success/15 text-success motion-safe:animate-pop-in">
        <CheckIcon className="size-3.5" />
      </span>
    )
  }

  if (status === 'attention') {
    return (
      <span className="relative flex size-7 items-center justify-center rounded-full bg-warning/15 text-warning">
        <AlertIcon className="size-3.5" />
      </span>
    )
  }

  return (
    <span
      className={`flex size-7 items-center justify-center rounded-full ${
        status === 'current' ? 'bg-accent text-background' : 'bg-raised text-muted'
      }`}
    >
      <Icon className="size-3.5" />
    </span>
  )
}

// SetupRail lists the setup's numbered steps down its side, each with its icon, its name,
// and how it stands: done with a tick, the current one, still ahead, needing attention, or,
// for Tools, suggested. A step already reached is a button back to it. The rail folds away
// in a narrow window, where the top bar still says where the user stands.
export function SetupRail({ steps, current, reached, attention, onGo }: SetupRailProps) {
  return (
    <nav
      aria-label="Setup steps"
      className="hidden w-60 shrink-0 border-r border-border bg-surface/60 px-3 py-6 md:block"
    >
      <ol className="flex flex-col gap-1">
        {steps.map((step) => {
          const status = stepStatus(step, current, reached, attention)
          const label = stepInfo[step].label
          const body = (
            <>
              <Badge step={step} status={status} />
              <span className="min-w-0 flex-1 truncate">{label}</span>
              {status === 'suggested' && (
                <span className="shrink-0 rounded-full border border-accent/40 px-1.5 text-badge font-semibold tracking-wide text-accent uppercase">
                  Suggested
                </span>
              )}
              <span className="sr-only">, {statusWords[status]}</span>
            </>
          )
          const rowClass = `flex w-full items-center gap-2.5 rounded-md px-2 py-1.5 text-left text-sm transition-colors ${
            status === 'current' ? 'bg-raised font-semibold text-text' : 'text-muted'
          }`

          return (
            <li key={step}>
              {reached.has(step) && status !== 'current' ? (
                <button
                  type="button"
                  onClick={() => {
                    onGo(step)
                  }}
                  className={`${rowClass} hover:bg-raised hover:text-text`}
                >
                  {body}
                </button>
              ) : (
                <span aria-current={status === 'current' ? 'step' : undefined} className={rowClass}>
                  {body}
                </span>
              )}
            </li>
          )
        })}
      </ol>
    </nav>
  )
}
