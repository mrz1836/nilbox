import { useEffect, useEffectEvent } from 'react'

import { listReleaseCandidates, on, type ReleaseCandidates } from '../platform'
import { useMutation } from './useMutation'
import { useQuery } from './useQuery'

// ReleaseCandidatesState is the Release page's list as the Scripts tab holds it.
export interface ReleaseCandidatesState {
  // list is the repositories, grouped, or null until they have loaded.
  list: ReleaseCandidates | null
  // error is why they could not be listed at all, or ''. A refresh that fails while an
  // older list is kept says so in the list's own error instead.
  error: string
  // refreshing reports a read of GitHub under way, and refreshError why the last asked for
  // failed, or ''.
  refreshing: boolean
  refreshError: string
  // refresh reads GitHub again now, whatever the list's age.
  refresh: () => Promise<void>
}

// useReleaseCandidates reads the Release page's list each time the tab shows (the service
// serves its last list while it is under five minutes old, and reads GitHub again after),
// and again whenever the shell says a new list landed, as after a release. While enabled
// is false, as while mage-x is off, nothing is read.
export function useReleaseCandidates(enabled: boolean): ReleaseCandidatesState {
  const query = useQuery(() => listReleaseCandidates(false), [], { enabled })
  const refresher = useMutation(() => listReleaseCandidates(true))
  const readOnUpdate = useEffectEvent(() => {
    void query.refetch()
  })

  useEffect(() => {
    if (!enabled) {
      return undefined
    }

    return on('releases:updated', () => {
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
