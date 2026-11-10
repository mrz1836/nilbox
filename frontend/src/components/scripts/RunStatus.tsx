import type { ReactNode } from 'react'

import { runStatusLabels } from '../../lib/scriptRuns'
import type { ScriptRunStatus } from '../../platform'
import { AlertIcon, CheckIcon, DotIcon, StopIcon, XIcon } from '../icons'
import { Spinner } from '../Spinner'

// statusIcons mark each status beside its name, so none is told by color alone: a run
// going turns, one orphaned from before a restart is a steady dot, and the ended ones
// pass, fail, stop, or warn that their end wasn't seen.
const statusIcons: Readonly<Record<ScriptRunStatus, ReactNode>> = {
  running: <Spinner className="size-3.5" />,
  orphaned: <DotIcon className="size-3.5 shrink-0 text-warning" />,
  succeeded: <CheckIcon className="size-3.5 shrink-0 text-success" />,
  failed: <XIcon className="size-3.5 shrink-0 text-danger" />,
  stopped: <StopIcon className="size-3.5 shrink-0 text-muted" />,
  interrupted: <AlertIcon className="size-3.5 shrink-0 text-warning" />,
}

export interface RunStatusProps {
  status: ScriptRunStatus
  // chip draws it as a chip, as the run panel's header does; the Runs page lists it bare.
  chip?: boolean
}

// RunStatus is a run's status: its icon and its name.
export function RunStatus({ status, chip = false }: RunStatusProps) {
  return (
    <span
      className={`inline-flex shrink-0 items-center gap-1 text-xs font-medium whitespace-nowrap text-text ${chip ? 'rounded-full border border-border px-2 py-0.5' : ''}`.trimEnd()}
    >
      {statusIcons[status]}
      {runStatusLabels[status]}
    </span>
  )
}
