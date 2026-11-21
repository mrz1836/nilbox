import { useEffect, useState } from 'react'

import { listReleaseCandidates, type ReleaseCandidates, type ReleasesProgress } from '../platform'
import { useMutation } from './useMutation'
import { useOn } from './useOn'
import { useQuery } from './useQuery'

// ReleaseCandidatesState is the Release page's list as the Scripts tab holds it.
export interface ReleaseCandidatesState {
  // list is the repositories, grouped, or null until they have loaded. After a restart it
  // is first the list kept from the last start (list.kept), while GitHub is read again
  // behind it (list.refreshing).
  list: ReleaseCandidates | null
  // error is why they could not be listed at all, or ''. A refresh that fails while an
  // older list is kept says so in the list's own error instead.
  error: string
  // refreshing reports a read of GitHub the page asked for under way, and refreshError why
  // the last asked for failed, or ''.
  refreshing: boolean
  refreshError: string
  // progress is how far the read of GitHub under way has got, or null while none is, or
  // before it first says.
  progress: ReleasesProgress | null
  // refresh reads GitHub again now, whatever the list's age.
  refresh: () => Promise<void>
}

// useReleaseCandidates reads the Release page's list each time the tab shows (the service
// serves its last list however old, reading GitHub in full only when refresh asks), and
// again whenever the shell says a new list landed, as after a release, a merge in the
// review sheet, or a read behind the list kept from the last start. Meanwhile it follows
// how far each read of GitHub has got. While enabled is false, as while mage-x is off,
// nothing is read.
export function useReleaseCandidates(enabled: boolean): ReleaseCandidatesState {
  const query = useQuery(() => listReleaseCandidates(false), [], { enabled })
  const refresher = useMutation(() => listReleaseCandidates(true))
  const [heard, setHeard] = useState<ReleasesProgress | null>(null)

  useOn(
    'releases:updated',
    () => {
      setHeard(null)
      void query.refetch()
    },
    enabled,
  )

  useOn(
    'releases:progress',
    (progress) => {
      setHeard(progress)
    },
    enabled,
  )

  // What it heard is out of date by the time it listens again, so it is dropped as it stops
  // listening, after both listeners have gone.
  useEffect(
    () =>
      enabled
        ? () => {
            setHeard(null)
          }
        : undefined,
    [enabled],
  )

  const refresh = async () => {
    const result = await refresher.run()

    if (result.ok) {
      query.set(() => result.data)
    }
  }

  const list = query.data ?? null
  // A read of GitHub is under way while the page waits for one, or one runs behind the
  // list shown; what was heard of an earlier one says nothing once none is.
  const reading = query.pending || refresher.pending || list?.refreshing === true

  return {
    list,
    error: query.error,
    refreshing: refresher.pending,
    refreshError: refresher.error,
    progress: reading ? heard : null,
    refresh,
  }
}
