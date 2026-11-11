import { useId, useState } from 'react'

import {
  filterRuns,
  isLive,
  runDuration,
  runFilterOrder,
  runFilters,
  runPlace,
  runWhere,
  type RunFilterID,
} from '../../lib/scriptRuns'
import { formatAgo, formatFull } from '../../lib/time'
import { useMutation } from '../../lib/useMutation'
import type { ScriptRunsState } from '../../lib/useScriptRuns'
import { clearScriptRunHistory, type ScriptRun, type TerminalID } from '../../platform'
import { ConfirmDialog } from '../ConfirmDialog'
import { quietButtonClass, Segmented } from '../controls'
import { ErrorBanner } from '../ErrorBanner'
import { SearchIcon, TrashIcon } from '../icons'
import { RelativeTime } from '../RelativeTime'
import { ErrorText } from '../settings/rows'
import { Spinner } from '../Spinner'
import { RunPanel } from './RunPanel'
import { RunStatus } from './RunStatus'

// filterOptions are the filters as the segmented control offers them.
const filterOptions = runFilterOrder.map((id) => ({ value: id, label: runFilters[id].label }))

// RunRow is one run in the list: its status, title, and when it started, then what it
// ran in, where, how long it took, and how it ended.
function RunRow({
  run,
  terminal,
  current,
  onOpen,
}: {
  run: ScriptRun
  terminal: TerminalID | null
  current: boolean
  onOpen: () => void
}) {
  const took = runDuration(run)
  const details = [runWhere(run), runPlace(run.runsIn, terminal), ...(took === '' ? [] : [took])]

  return (
    <li>
      <button
        type="button"
        aria-current={current ? 'true' : undefined}
        onClick={onOpen}
        className="flex w-full flex-col gap-0.5 rounded-md px-3 py-2 text-left transition-colors hover:bg-raised aria-[current=true]:bg-raised"
      >
        <span className="flex w-full items-center gap-2">
          <RunStatus status={run.status} />
          <span className="min-w-0 truncate text-sm font-medium">{run.title}</span>
          <span className="ml-auto shrink-0 text-xs text-muted" title={formatFull(run.startedAt)}>
            <RelativeTime iso={run.startedAt} show={formatAgo} />
          </span>
        </span>
        <span className="truncate text-xs text-muted">{details.join(' · ')}</span>
        {run.summary !== '' && <span className="truncate text-xs text-text">{run.summary}</span>}
      </button>
    </li>
  )
}

// RunsPageProps are every run, as the tab lists them (runs, error, and reload, from
// useScriptRuns), the terminal chosen for launches, which a run in the terminal runs in,
// or null until the settings have loaded, and the run open in the panel, which the tab
// holds, so another page can open one here (as Recent on Release does).
export interface RunsPageProps extends ScriptRunsState {
  terminal: TerminalID | null
  selected: number | null
  onSelect: (id: number | null) => void
}

// RunsPage is the Scripts tab's Runs page: every run, newest first, filtered by kind and
// searched by repository as the backend would, from the runs the tab listed. A run opens
// in the run panel beside the list. Clear history forgets the runs that have ended, after
// asking; the runs going stay, and so does the panel of one, whatever the filter shows.
export function RunsPage({ runs, error, reload, terminal, selected, onSelect: setSelected }: RunsPageProps) {
  const id = useId()
  const [filter, setFilter] = useState<RunFilterID>('all')
  const [search, setSearch] = useState('')
  const [asking, setAsking] = useState(false)
  const repo = search.trim()
  const shown = runs === null ? null : filterRuns(runs, filter, repo)
  const clear = useMutation(() => clearScriptRunHistory())
  // clearable reports a run that has ended, which Clear history would forget, and keepOpen
  // a run open in the panel that is still going, which it keeps: both of every run, not
  // only those the filter shows.
  const clearable = runs?.some((r) => !isLive(r.status)) === true
  const keepOpen = runs?.some((r) => r.id === selected && isLive(r.status)) === true

  // clearHistory forgets the runs that have ended, closing the panel unless the run in it
  // is still going.
  const clearHistory = async () => {
    setAsking(false)

    const result = await clear.run()

    if (result.ok) {
      if (!keepOpen) {
        setSelected(null)
      }

      await reload()
    }
  }

  return (
    <div className="flex h-full min-h-0">
      <section
        aria-labelledby={`${id}-title`}
        className={`flex min-h-0 flex-col ${selected === null ? 'flex-1' : 'w-[26rem] shrink-0 border-r border-border'}`}
      >
        <div className="flex flex-col gap-2 px-4 pt-4 pb-2">
          <div className="flex items-center justify-between gap-2">
            <h2 id={`${id}-title`} className="text-sm font-semibold">
              Runs
            </h2>
            <button
              type="button"
              disabled={clear.pending || !clearable}
              onClick={() => {
                setAsking(true)
              }}
              className={quietButtonClass}
            >
              <TrashIcon className="size-3.5" />
              Clear history
            </button>
          </div>
          <ErrorText error={clear.error} />
          <div className="flex flex-wrap items-center gap-2">
            <Segmented label="Kind of run" size="sm" value={filter} options={filterOptions} onChange={setFilter} />
            <label htmlFor={`${id}-search`} className="sr-only">
              Search repositories
            </label>
            <div className="flex min-w-40 flex-1 items-center gap-2 rounded-md border border-border bg-background px-2.5 py-1 focus-within:border-accent/60">
              <SearchIcon className="size-3.5 shrink-0 text-muted" />
              <input
                id={`${id}-search`}
                type="search"
                value={search}
                placeholder="Search repositories…"
                onChange={(e) => {
                  setSearch(e.target.value)
                }}
                className="min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-muted/70"
              />
            </div>
          </div>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-4">
          {error !== '' && <ErrorBanner className="mx-2">Couldn’t list the runs: {error}</ErrorBanner>}
          {shown === null ? (
            error === '' && (
              <p className="flex items-center gap-2 px-2 py-2 text-sm text-muted">
                <Spinner />
                Listing the runs…
              </p>
            )
          ) : shown.length === 0 ? (
            <p className="px-2 py-2 text-sm text-muted">
              {repo === '' ? runFilters[filter].none : `No run in a repository matching “${repo}”.`}
            </p>
          ) : (
            <ul aria-label="Runs" className="flex flex-col gap-0.5">
              {shown.map((run) => (
                <RunRow
                  key={run.id}
                  run={run}
                  terminal={terminal}
                  current={run.id === selected}
                  onOpen={() => {
                    setSelected(run.id)
                  }}
                />
              ))}
            </ul>
          )}
        </div>
      </section>
      {selected !== null && (
        <div className="min-w-0 flex-1">
          <RunPanel
            key={selected}
            id={selected}
            terminal={terminal}
            onSelect={setSelected}
            onClose={() => {
              setSelected(null)
            }}
          />
        </div>
      )}
      {asking && (
        <ConfirmDialog
          title="Clear the run history?"
          confirmLabel="Clear history"
          onConfirm={() => {
            void clearHistory()
          }}
          onCancel={() => {
            setAsking(false)
          }}
        >
          This forgets every run that has ended, with its log. Runs still going stay.
        </ConfirmDialog>
      )}
    </div>
  )
}
