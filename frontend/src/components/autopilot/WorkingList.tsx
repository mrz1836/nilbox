import { itemText } from '../../lib/autopilot'
import { counted } from '../../lib/format'
import { formatDuration } from '../../lib/scriptRuns'
import { useOnAutopilot } from '../../lib/useAutopilot'
import { useNow } from '../../lib/useNow'
import { useMutation } from '../../lib/useMutation'
import { useOn } from '../../lib/useOn'
import { useQuery } from '../../lib/useQuery'
import {
  autopilotWorking,
  stopAllAutopilotRuns,
  stopAutopilotRun,
  type AutopilotItemRef,
  type AutopilotWorking,
  type AutopilotWorkingList,
} from '../../platform'
import { quietButtonClass } from '../controls'
import { EyeIcon } from '../icons'

// tokensText says a run's tokens so far: "12.3k tokens", "640 tokens".
export function tokensText(n: number): string {
  if (n >= 1000) {
    return `${(n / 1000).toFixed(n >= 100_000 ? 0 : 1)}k tokens`
  }

  return counted(n, 'token', 'tokens')
}

// useWorking reads the agents' runs going, and again whenever Autopilot changes or a run's
// stream moves on.
function useWorking(): { list: AutopilotWorkingList | undefined; reload: () => void } {
  const query = useQuery(() => autopilotWorking(), [], { log: 'could not read the agents working' })
  const reload = () => {
    void query.refetch()
  }

  useOnAutopilot(reload)
  useOn('autopilot:run', () => {
    reload()
  })

  return { list: query.data, reload }
}

export interface WorkingListProps {
  // onOpen shows an item, as Activity's Open does.
  onOpen: (item: AutopilotItemRef, routineId: number) => void
  // onWatch opens a run's view beside Activity, and watching is the run it shows, 0 for
  // none.
  onWatch: (runId: number) => void
  watching: number
  // onToast says what Stop did, or why it couldn't.
  onToast: (text: string, tone?: 'error') => void
}

// WorkingList is Activity's Working: each agent's run going with its latest step, how long
// it has run, its tokens so far, Watch, which opens its run view, and Stop; and Stop all
// for every one of them. The starts queued are Needs you's.
export function WorkingList({ onOpen, onWatch, watching, onToast }: WorkingListProps) {
  const { list, reload } = useWorking()
  const stopAll = useMutation(() => stopAllAutopilotRuns())

  if (list === undefined || list.working.length === 0) {
    return null
  }

  return (
    <section aria-label="Working" className="flex flex-col gap-1">
      <div className="flex items-center gap-2">
        <h3 className="text-xs font-semibold tracking-wide text-muted uppercase">
          Working · {String(list.working.length)}
        </h3>
        <button
          type="button"
          disabled={stopAll.pending}
          onClick={() => {
            void stopAll.run().then((result) => {
              if (result.ok) {
                onToast('Stopping every agent’s run.')
                reload()
              } else {
                onToast(result.error, 'error')
              }
            })
          }}
          className={`${quietButtonClass} ml-auto`}
        >
          Stop all
        </button>
      </div>
      <ul className="flex flex-col gap-1">
        {list.working.map((w) => (
          <WorkingRow
            key={w.runId}
            run={w}
            onOpen={onOpen}
            onWatch={onWatch}
            watched={w.runId === watching}
            onToast={onToast}
            onStopped={reload}
          />
        ))}
      </ul>
    </section>
  )
}

// WorkingRow is one agent's run going.
function WorkingRow({
  run,
  onOpen,
  onWatch,
  watched,
  onToast,
  onStopped,
}: {
  run: AutopilotWorking
  onOpen: WorkingListProps['onOpen']
  onWatch: WorkingListProps['onWatch']
  watched: boolean
  onToast: WorkingListProps['onToast']
  onStopped: () => void
}) {
  const now = useNow()
  const stop = useMutation(() => stopAutopilotRun(run.runId))
  const started = Date.parse(run.startedAt)
  const elapsed = Number.isNaN(started) ? '' : formatDuration(now.getTime() - started)
  const tokens = run.progress.tokensIn + run.progress.tokensOut
  const details = [run.routine, elapsed, tokens > 0 ? tokensText(tokens) : ''].filter((d) => d !== '')
  const stopping = run.status === 'stopping' || stop.pending

  return (
    <li className="flex flex-col gap-0.5 rounded-md px-3 py-2 hover:bg-raised">
      <div className="flex items-center gap-2">
        <span aria-hidden className="size-2 shrink-0 rounded-full bg-accent" />
        <button
          type="button"
          onClick={() => {
            onOpen(run.item, run.routineId)
          }}
          className="min-w-0 truncate text-left text-sm font-medium hover:underline"
          title={run.item.title}
        >
          {itemText(run.item)}
        </button>
        <span className="truncate text-xs text-muted">{details.join(' · ')}</span>
        {run.where === 'background' && (
          <button
            type="button"
            aria-pressed={watched}
            onClick={() => {
              onWatch(run.runId)
            }}
            className={`${quietButtonClass} ml-auto aria-pressed:bg-raised`}
          >
            <EyeIcon className="size-3.5" />
            Watch
          </button>
        )}
        <button
          type="button"
          disabled={stopping}
          onClick={() => {
            void stop.run().then((result) => {
              if (result.ok) {
                onStopped()
              } else {
                onToast(result.error, 'error')
              }
            })
          }}
          className={`${quietButtonClass} ${run.where === 'background' ? '' : 'ml-auto'}`}
        >
          {stopping ? 'Stopping…' : 'Stop'}
        </button>
      </div>
      {run.progress.step !== '' && <span className="truncate pl-4 text-xs text-muted">{run.progress.step}</span>}
    </li>
  )
}
