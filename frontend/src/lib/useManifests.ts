import { useEffect, useState } from 'react'

import { listManifests, type ManifestsPage, type ManifestsProgress } from '../platform'
import { useMutation } from './useMutation'
import { useOn } from './useOn'
import { useQuery } from './useQuery'

// ManifestsState is the Deps page's clones as the Scripts tab holds them.
export interface ManifestsState {
  // page is the clones, each with its modules, or null until they have been read. A read
  // that fails while an older page is kept says so in the page's own error instead.
  page: ManifestsPage | null
  // error is why the clones could not be read at all, or ''.
  error: string
  // refreshing reports a read of every clone the page asked for under way, and
  // refreshError why the last one asked for failed, or ''.
  refreshing: boolean
  refreshError: string
  // progress is how far the read under way has got, or null while none is, or before it
  // first says.
  progress: ManifestsProgress | null
  // refresh reads every clone again now, whatever the page's age.
  refresh: () => Promise<void>
}

// useManifests reads the Deps page's clones each time the tab shows (the service serves
// its last read while it is under five minutes old, and reads every clone again after,
// which costs one rev-parse for each clone whose commit is unchanged), and follows how
// far each read has got. While enabled is false, as while the Scripts integration is off,
// nothing is read.
export function useManifests(enabled: boolean): ManifestsState {
  const query = useQuery(() => listManifests(false), [], { enabled })
  const refresher = useMutation(() => listManifests(true))
  const [heard, setHeard] = useState<ManifestsProgress | null>(null)

  useOn(
    'manifests:progress',
    (progress) => {
      setHeard(progress)
    },
    enabled,
  )

  // What it heard is out of date by the time it listens again, so it is dropped as it stops
  // listening.
  useEffect(
    () =>
      enabled
        ? () => {
            setHeard(null)
          }
        : undefined,
    [enabled],
  )

  // refresh drops what was heard of an earlier read first, so the new one's progress never
  // starts from where the last left off.
  const refresh = async () => {
    setHeard(null)

    const result = await refresher.run()

    if (result.ok) {
      query.set(() => result.data)
    }
  }

  // A read is under way while the tab waits for one, or one the page asked for; what was
  // heard of an earlier one says nothing once none is.
  const reading = query.pending || refresher.pending

  return {
    page: query.data ?? null,
    error: query.error,
    refreshing: refresher.pending,
    refreshError: refresher.error,
    progress: reading ? heard : null,
    refresh,
  }
}
