import { useState } from 'react'

import { autopilotActivity, type AutopilotActivityDay, type AutopilotLeftAlone } from '../platform'
import { mergeDays, matchCount } from './autopilot'
import { attempt } from './call'
import { useOnAutopilot } from './useAutopilot'
import { useQuery } from './useQuery'

// pageSize is how many matches a page of Activity holds, and maxReload the most one read
// of the pages loaded may ask for (the backend's own cap).
const pageSize = 100
const maxReload = 500

// Earlier is the days loaded past the first read, and where the next page starts.
interface Earlier {
  days: AutopilotActivityDay[]
  next: number
  more: boolean
}

// ActivityPages is Activity as the tab shows it: the days loaded, newest first, the items
// the routines leave alone, whether there is more, and a way to load it.
export interface ActivityPages {
  days: readonly AutopilotActivityDay[]
  leftAlone: readonly AutopilotLeftAlone[]
  loaded: boolean
  error: string
  more: boolean
  loadingMore: boolean
  loadMore: () => Promise<void>
  // reload reads the pages loaded again now, as after a change the tab made.
  reload: () => void
}

// noDays is Activity until it loads, shared so it stays the same.
const noDays: readonly AutopilotActivityDay[] = []
const noneLeftAlone: readonly AutopilotLeftAlone[] = []

// useAutopilotActivity reads Activity's newest page as the tab opens, earlier pages as the
// user scrolls (loadMore), and the pages loaded again whenever Autopilot says it changed:
// the newest matches in one read of as many as are loaded, up to the backend's cap, and
// the pages past it as they were.
export function useAutopilotActivity(): ActivityPages {
  const [limit, setLimit] = useState(pageSize)
  const [earlier, setEarlier] = useState<Earlier | null>(null)
  const [loadingMore, setLoadingMore] = useState(false)
  const [moreError, setMoreError] = useState('')
  const first = useQuery(() => autopilotActivity(0, limit), [limit])

  useOnAutopilot(() => {
    void first.refetch()
  })

  const page = first.data
  const days = page === undefined ? noDays : earlier === null ? page.days : mergeDays(page.days, earlier.days)
  const more = earlier === null ? (page?.more ?? false) : earlier.more

  const loadMore = async () => {
    if (page === undefined || !more || loadingMore) {
      return
    }

    // While the first read can hold more, it grows; past its cap, pages are read on.
    if (earlier === null && limit < maxReload) {
      setLimit(Math.min(maxReload, limit + pageSize))

      return
    }

    const from = earlier?.next ?? page.next

    setLoadingMore(true)

    const result = await attempt(autopilotActivity(from, pageSize))

    setLoadingMore(false)

    if (!result.ok) {
      setMoreError(result.error)

      return
    }

    setMoreError('')
    setEarlier((was) => ({
      days: mergeDays(was?.days ?? [], result.data.days),
      next: result.data.next,
      more: result.data.more,
    }))
  }

  return {
    days,
    leftAlone: page?.leftAlone ?? noneLeftAlone,
    loaded: page !== undefined,
    error: first.error !== '' ? first.error : moreError,
    more,
    // A first read growing to hold more is loading more too.
    loadingMore: loadingMore || (first.pending && page !== undefined && matchCount(page) < limit),
    loadMore,
    reload: () => {
      void first.refetch()
    },
  }
}
