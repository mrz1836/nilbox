import { useEffect, useEffectEvent } from 'react'

import { listScriptRuns, on, type ScriptRun } from '../platform'
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
// and filters what it shows itself: the history keeps at most a few hundred runs.
export function useScriptRuns(): ScriptRunsState {
  const runs = useQuery(() => listScriptRuns({ kinds: [], repo: '' }), [])
  const reloadOnUpdate = useEffectEvent(() => {
    void runs.refetch()
  })

  useEffect(
    () =>
      on('scriptruns:updated', () => {
        reloadOnUpdate()
      }),
    [],
  )

  return { runs: runs.data ?? null, error: runs.error, reload: runs.refetch }
}
