import { useEffect, useEffectEvent } from 'react'

import { listHooks, on, type HooksList, type ScriptRun } from '../platform'
import { liveDirs } from './hooks'
import { useMutation } from './useMutation'
import { useQuery } from './useQuery'

// HooksState is the Hooks page's list as the Scripts tab holds it.
export interface HooksState {
  // list is the clones, grouped, or null until they have loaded.
  list: HooksList | null
  // error is why they could not be listed at all, or ''. A scan that fails while an older
  // one is kept says so in the list's own error instead.
  error: string
  // refreshing reports a scan of every clone under way, and refreshError why the last one
  // asked for failed, or ''.
  refreshing: boolean
  refreshError: string
  // refresh scans every clone again now, whatever the list's age.
  refresh: () => Promise<void>
}

// useHooks reads the Hooks page's list each time the tab shows (the service serves its
// last scan while it is under five minutes old, scanning again only the clones an action
// or a check ran in, and every clone after), again whenever the shell says a clone was
// scanned again, and again when a run starts or ends in a clone, which is when what holds
// each clone changes. runs are every run, as the tab lists them. While enabled is false,
// as while go-pre-commit is off or can't be used, nothing is read.
export function useHooks(enabled: boolean, runs: readonly ScriptRun[] | null): HooksState {
  const busy = liveDirs(runs)
  const query = useQuery(() => listHooks(false), [busy], { enabled })
  const refresher = useMutation(() => listHooks(true))
  const readOnUpdate = useEffectEvent(() => {
    void query.refetch()
  })

  useEffect(() => {
    if (!enabled) {
      return undefined
    }

    return on('hooks:updated', () => {
      readOnUpdate()
    })
  }, [enabled])

  const refresh = async () => {
    const result = await refresher.run()

    if (result.ok) {
      query.set(() => result.data)
    }
  }

  return {
    list: query.data ?? null,
    error: query.error,
    refreshing: refresher.pending,
    refreshError: refresher.error,
    refresh,
  }
}
