import { useEffect, useEffectEvent, useState } from 'react'

import { installAllHooks, listHooks, on, type HookInstallAllResult, type HooksList, type ScriptRun } from '../platform'
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
  // installAll is Install and update all, held here rather than by the page, so that it
  // survives the page giving way to another one of the tab's while it goes.
  installAll: InstallAllState
}

// InstallAllState is Install and update all as it goes, then how it went.
export interface InstallAllState {
  // pending reports it under way; result is its line per clone once it ended, or null;
  // error why it failed, or ''.
  pending: boolean
  result: HookInstallAllResult | null
  error: string
  // run installs or updates the hooks in dirs, one clone at a time; dismiss clears the
  // result and the error.
  run: (dirs: string[]) => Promise<void>
  dismiss: () => void
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
  const installer = useMutation((dirs: string[]) => installAllHooks(dirs))
  const [installed, setInstalled] = useState<HookInstallAllResult | null>(null)
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

  const installEvery = async (dirs: string[]) => {
    setInstalled(null)

    const done = await installer.run(dirs)

    if (done.ok) {
      setInstalled(done.data)
    }
  }

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
    installAll: {
      pending: installer.pending,
      result: installed,
      error: installer.error,
      run: installEvery,
      dismiss: () => {
        setInstalled(null)
        installer.reset()
      },
    },
  }
}
