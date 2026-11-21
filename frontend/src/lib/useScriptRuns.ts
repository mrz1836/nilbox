import { listScriptRuns, type ScriptRun } from '../platform'
import { useOn } from './useOn'
import { useQuery } from './useQuery'

// ScriptRunsState is every run, as the Scripts tab lists them.
export interface ScriptRunsState {
  // runs are the runs, newest first, or null until they have loaded.
  runs: readonly ScriptRun[] | null
  // error is why they could not be listed, or ''.
  error: string
  // reload lists them again; it resolves once the list is in.
  reload: () => Promise<void>
}

// useScriptRuns lists every run the history keeps, and lists them again whenever the
// shell says a run started, ended, or changed, or the history did. The tab lists them once
// and filters what it shows itself: the history keeps at most a few hundred runs. While
// enabled is false, as while the Scripts tab is hidden, nothing is listed.
export function useScriptRuns(enabled = true): ScriptRunsState {
  // The agents' runs are the Autopilot tab's, never the Runs page's.
  const runs = useQuery(() => listScriptRuns({ kinds: [], repo: '', withAgents: false }), [], { enabled })

  useOn(
    'scriptruns:updated',
    () => {
      void runs.refetch()
    },
    enabled,
  )

  return { runs: runs.data ?? null, error: runs.error, reload: runs.refetch }
}
