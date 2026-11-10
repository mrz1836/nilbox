// The runs as the Scripts tab words them: each status's name, where a run runs, how long
// it took, the Runs page's filters, and a run's whole log as text.

import { brand } from '../generated/brand'
import {
  scriptRunOutput,
  type RunsIn,
  type ScriptRun,
  type ScriptRunKind,
  type ScriptRunStatus,
  type TerminalID,
} from '../platform'
import { plainText } from './ansi'
import { terminalNames } from './terminals'

// runStatusLabels name each status, as a run's chip says it.
export const runStatusLabels: Readonly<Record<ScriptRunStatus, string>> = {
  running: 'Running',
  succeeded: 'Done',
  failed: 'Failed',
  stopped: 'Stopped',
  interrupted: 'Interrupted',
  orphaned: 'Orphaned',
}

// isLive reports a run that has not ended: running, or orphaned, which runs on from
// before the app restarted.
export function isLive(status: ScriptRunStatus): boolean {
  return status === 'running' || status === 'orphaned'
}

// runPlace says where a run runs: in the app, or in the terminal chosen for launches,
// named once the settings have loaded.
export function runPlace(runsIn: RunsIn, terminal: TerminalID | null): string {
  if (runsIn === 'app') {
    return `In ${brand.name}`
  }

  return terminal === null ? 'In your terminal' : `In ${terminalNames[terminal]}`
}

// runWhere is what a run ran in: its repository, or its folder when that isn't one.
export function runWhere(run: Pick<ScriptRun, 'repo' | 'dir'>): string {
  return run.repo === '' ? run.dir : run.repo
}

// formatDuration says how long something took, to the second under an hour: "38 s",
// "2 min 5 s", "1 h 3 min". A negative span, from a clock that moved back, is "0 s".
export function formatDuration(ms: number): string {
  const seconds = Math.max(0, Math.floor(ms / 1000))

  if (seconds < 60) {
    return `${String(seconds)} s`
  }

  const minutes = Math.floor(seconds / 60)

  if (minutes < 60) {
    const rest = seconds % 60

    return rest === 0 ? `${String(minutes)} min` : `${String(minutes)} min ${String(rest)} s`
  }

  const hours = Math.floor(minutes / 60)
  const rest = minutes % 60

  return rest === 0 ? `${String(hours)} h` : `${String(hours)} h ${String(rest)} min`
}

// runDuration is how long an ended run took, or '' for one still going.
export function runDuration(run: Pick<ScriptRun, 'startedAt' | 'finishedAt'>): string {
  const started = Date.parse(run.startedAt)
  const finished = Date.parse(run.finishedAt)

  return Number.isNaN(started) || Number.isNaN(finished) ? '' : formatDuration(finished - started)
}

// RunFilterID is one of the Runs page's filters.
export type RunFilterID = 'all' | 'release' | 'hooks' | 'scripts'

// RunFilter is a filter's name, the kinds of run it lists (none for every kind), and
// what the page says when it lists none.
export interface RunFilter {
  label: string
  kinds: readonly ScriptRunKind[]
  none: string
}

// runFilters are the Runs page's filters. Hooks takes the checks along with the hooks
// actions, since go-pre-commit does both.
export const runFilters: Readonly<Record<RunFilterID, RunFilter>> = {
  all: { label: 'All', kinds: [], none: 'Nothing has run yet. Releases, checks, and scripts you run show here.' },
  release: { label: 'Release', kinds: ['release'], none: 'No release has run yet.' },
  hooks: { label: 'Hooks', kinds: ['hooks', 'checks'], none: 'No hooks actions or checks have run yet.' },
  scripts: { label: 'Scripts', kinds: ['script'], none: 'No script has run yet.' },
}

// runFilterOrder is the order the page offers the filters in.
export const runFilterOrder: readonly RunFilterID[] = ['all', 'release', 'hooks', 'scripts']

// filterRuns picks the runs a filter and a repository search list, in their order, as
// the backend's Filter would: a run of one of the filter's kinds, whose "owner/name"
// holds the search, trimmed and in any case. A run in a folder that isn't a repository
// has no "owner/name", so a search finds none of them. The history keeps at most a few
// hundred runs, so the window filters what it listed rather than ask again.
export function filterRuns(runs: readonly ScriptRun[], filter: RunFilterID, search: string): ScriptRun[] {
  const { kinds } = runFilters[filter]
  const want = search.trim().toLowerCase()

  return runs.filter((run) => (kinds.length === 0 || kinds.includes(run.kind)) && run.repo.toLowerCase().includes(want))
}

// readLog reads a run's whole log, page by page from its start, up to its size as the
// last page found it, as the log view shows it: plain text, every escape gone. A log cut
// down between two pages, as a long one is when its run ends, reads past its end or
// smaller than it was (a log only grows otherwise): it is read again from the start,
// once, and what was read is what it gives if it is cut down again.
export async function readLog(id: number): Promise<string> {
  let offset = 0
  let text = ''
  let size = 0
  let restarted = false

  for (;;) {
    const chunk = await scriptRunOutput(id, offset, 0)

    if (chunk.restart || chunk.size < size) {
      if (restarted) {
        return plainText(text)
      }

      restarted = true
      offset = 0
      text = ''
      size = 0
      continue
    }

    size = chunk.size
    text += chunk.text

    if (chunk.done || chunk.next >= chunk.size || chunk.next <= offset) {
      return plainText(text)
    }

    offset = chunk.next
  }
}
